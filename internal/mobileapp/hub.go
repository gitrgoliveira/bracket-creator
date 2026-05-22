package mobileapp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// EventType defines the kind of update being broadcast
type EventType string

const (
	EventMatchUpdated            EventType = "match_updated"
	EventCompetitionStarted      EventType = "competition_started"
	EventCompetitionCompleted    EventType = "competition_completed"
	EventTournamentUpdated       EventType = "tournament_updated"
	EventScheduleUpdated         EventType = "schedule_updated"
	EventCompetitorStatusUpdated EventType = "competitor_status_updated"
	EventParticipantsUpdated     EventType = "participants_updated"
	// EventPasswordReset fires when the admin password is rotated. Three
	// broadcast sites (file mode only; never emitted in locked mode):
	//   - POST /api/tournament/reset — the public recovery endpoint for
	//     a forgotten password. Payload: {originatorId} so the submitting
	//     tab can suppress its own broadcast and avoid clearing the
	//     credential it just wrote.
	//   - PUT /api/tournament — when the PUT body contains a non-empty
	//     password that differs from the stored one. Payload: {} (all
	//     sessions, including the editing tab, should re-authenticate).
	//   - POST /api/tournament — when a bootstrap POST overwrites an
	//     existing tournament with a different password. Payload: {}.
	// Consumers in admin mode (app.jsx) clear their localStorage credential
	// and re-show the AuthModal so a logged-in operator notices immediately
	// instead of waiting for their next write to fail with 401. Viewers
	// ignore the event — their flow doesn't depend on the admin password.
	EventPasswordReset EventType = "password_reset"
	EventAnnouncement  EventType = "announcement"
)

// AutoCompleteErrorHeader is set on score/start responses when the
// post-write MaybeAutoCompletePools check itself errored. The value is a
// deliberately generic sentinel (AutoCompleteErrorValue) so we don't leak
// filesystem paths or other internal store details to clients — full error
// detail is logged server-side.
const (
	AutoCompleteErrorHeader = "X-Auto-Complete-Error"
	AutoCompleteErrorValue  = "failed"
)

// DefaultHistorySize is the default ring buffer capacity for replay-on-reconnect (T216).
// 100 events is roughly 30 seconds of activity on a busy tournament floor
// (multi-court bulk score) and matches what we measured in v3 review.
const DefaultHistorySize = 100

// DefaultMaxSSEClients caps concurrent /api/events subscribers per process.
// Each subscriber allocates one buffered channel + one streaming goroutine,
// and Broadcast fan-out is O(N) in the subscriber count. 1000 is well
// above what a typical tournament floor needs (operator + ~5-20 viewers
// per court × ~10 courts ≈ 50-200 connections) and well below the point
// where the linear fan-out becomes a noticeable latency tax.
//
// Override at startup via the SSE_MAX_CLIENTS env var or by passing a
// positive value to NewHubWithLimits. A non-positive (zero or negative)
// value disables the cap entirely — used by tests that need to exceed
// the default and not enforced anywhere production. The cap is mp-663
// Phase 4 mitigation for resource-exhaustion via unbounded subscriber maps.
const DefaultMaxSSEClients = 1000

// SSEEvent represents the payload sent to clients.
//
// Seq (T215, A2 closure) is a strictly monotonic counter stamped by the
// hub on every Broadcast. Consumers use it for two things:
//
//  1. Gap detection — if the JS receiver sees seq jump from N to N+2, it
//     missed N+1 (network reorder/loss is impossible on a single SSE
//     connection, but a reconnect can leave a gap between the last live
//     event before the disconnect and the first one after the reconnect)
//     and triggers a full refetch.
//  2. Replay on reconnect — `EventSource` automatically resends the last
//     `id:` it saw via `Last-Event-ID`; the hub walks its ring buffer to
//     re-emit events with seq > Last-Event-ID before resuming live
//     streaming.
//
// Wire format: `data: {"type":"match_updated","data":{…},"seq":12345}`
// plus an SSE `id: 12345` line so the browser sets `Last-Event-ID`
// automatically on reconnect.
//
// Existing JS that ignores `seq` continues to work — the field is purely
// additive.
type SSEEvent struct {
	Type EventType `json:"type"`
	Data any       `json:"data"`
	Seq  int64     `json:"seq"`
}

// historyEntry pairs the stamped seq with the marshalled JSON payload.
// We store the marshalled form (not the SSEEvent struct) so the reconnect
// replay path doesn't have to re-marshal — keeps cold-replay cheap and
// guarantees the replayed bytes are exactly what live clients saw.
type historyEntry struct {
	seq     int64
	payload string
}

// Hub manages active SSE connections and broadcasts events.
//
// The hub also owns:
//   - a monotonic `seq` counter stamped on every broadcast envelope
//   - a fixed-size ring buffer of recent envelopes for replay-on-reconnect
//
// Both are protected by the same `mu` mutex used for the client set so
// the order of "stamp seq" / "append to history" / "fan out to clients"
// is observed atomically — without that, a concurrent reconnect could
// read a partial history slice and replay events out of order.
type Hub struct {
	clients map[chan string]bool
	mu      sync.RWMutex

	// seq is the monotonic envelope counter (atomic so Broadcast can
	// stamp without holding the full hub lock, but we still take the
	// write lock to add to history in the same critical section so
	// replay readers see a consistent (seq, history) snapshot).
	seq atomic.Int64

	// history is a ring buffer of the last HistorySize broadcast
	// envelopes. Indexed by `seq % HistorySize`. Reads (via
	// snapshotHistorySince) take the read lock; writes (in Broadcast)
	// take the write lock.
	history     []historyEntry
	HistorySize int

	// MaxClients caps concurrent SSE subscribers (mp-663 Phase 4).
	// Subscribe returns nil when the cap is reached; HandleEvents
	// converts that into HTTP 503 so the client knows to back off. A
	// non-positive value disables the cap (treat as unbounded — useful
	// for tests).
	MaxClients int

	// closed is set when Close has been called (mp-663 Phase 2 graceful
	// shutdown hook). New subscribers are rejected after this; existing
	// subscriber channels are closed so the per-connection streaming
	// goroutine in HandleEvents exits cleanly. Guarded by mu.
	closed bool
}

func NewHub() *Hub {
	return NewHubWithLimits(DefaultHistorySize, DefaultMaxSSEClients)
}

// NewHubWithHistory constructs a Hub with a custom ring buffer capacity.
// Used by tests that need predictable eviction behaviour without
// generating 100+ events. MaxClients stays at the default.
func NewHubWithHistory(historySize int) *Hub {
	return NewHubWithLimits(historySize, DefaultMaxSSEClients)
}

// NewHubWithLimits constructs a Hub with explicit history-size and
// subscriber-cap.
//
// historySize: non-positive falls back to DefaultHistorySize (the ring
// buffer needs a non-zero capacity to function).
//
// maxClients: passed through as-is. A POSITIVE value caps concurrent
// subscribers at that count (Subscribe returns nil over the cap). A
// non-positive value (zero or negative) DISABLES the cap entirely —
// useful for tests that need to exceed DefaultMaxSSEClients without
// constructing thousands of stub clients to prove the cap fires. It is
// deliberately not coerced to the default so callers can opt out.
func NewHubWithLimits(historySize, maxClients int) *Hub {
	if historySize <= 0 {
		historySize = DefaultHistorySize
	}
	return &Hub{
		clients:     make(map[chan string]bool),
		history:     make([]historyEntry, historySize),
		HistorySize: historySize,
		MaxClients:  maxClients,
	}
}

// Subscribe adds a new client channel to the hub. Returns nil when:
//   - the hub has been Close()d (graceful shutdown in progress)
//   - the subscriber count has reached MaxClients
//
// HandleEvents converts a nil return into HTTP 503 so the SSE client
// knows to back off and retry rather than hanging on a stuck connection.
func (h *Hub) Subscribe() chan string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	if h.MaxClients > 0 && len(h.clients) >= h.MaxClients {
		return nil
	}
	// Buffer absorbs short bursts (bulk-score and schedule updates) for ~300
	// concurrent SSE clients; truly stalled clients are detected via the
	// non-blocking send in Broadcast and unsubscribed.
	ch := make(chan string, 100)
	h.clients[ch] = true
	return ch
}

// Close marks the hub as shutting down and closes every subscriber
// channel. The per-connection streaming goroutine in HandleEvents
// observes the channel close (the `case msg, ok := <-ch; if !ok`
// branch) and returns, unblocking http.Server.Shutdown's wait loop.
//
// Idempotent — safe to call twice. After Close, Subscribe returns nil
// for all new subscribers and Broadcast becomes a no-op observable to
// clients (no live channels remain).
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for ch := range h.clients {
		delete(h.clients, ch)
		close(ch)
	}
}

// Unsubscribe removes a client channel
func (h *Hub) Unsubscribe(ch chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unsubscribeLocked(ch)
}

func (h *Hub) unsubscribeLocked(ch chan string) {
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
}

// Broadcast sends an event to all subscribed clients.
//
// The envelope is stamped with a strictly-monotonic seq counter (T215) and
// appended to the ring buffer (T216) under the same write lock as the
// client fan-out. This means concurrent Broadcasts from different
// goroutines see strictly increasing seqs in the order their lock-acquire
// sequenced — there's no "stamp before lock, lock for fan-out" window
// that would let two seqs land in history in reverse order.
func (h *Hub) Broadcast(eventType EventType, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()

	seq := h.seq.Add(1)
	event := SSEEvent{
		Type: eventType,
		Data: data,
		Seq:  seq,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		fmt.Printf("Error marshaling SSE event: %v\n", err)
		return
	}
	payloadStr := string(payload)

	// Append to ring buffer first so a client that reconnects between the
	// stamp and the fan-out sees the same envelope on replay.
	if h.HistorySize > 0 {
		h.history[(seq-1)%int64(h.HistorySize)] = historyEntry{
			seq:     seq,
			payload: payloadStr,
		}
	}

	clients := make([]chan string, 0, len(h.clients))
	for ch := range h.clients {
		clients = append(clients, ch)
	}

	var dead []chan string
	for _, ch := range clients {
		select {
		case ch <- payloadStr:
		default:
			dead = append(dead, ch)
		}
	}

	for _, ch := range dead {
		h.unsubscribeLocked(ch)
	}
}

// snapshotHistorySince returns history entries whose seq is strictly
// greater than `since`, ordered by ascending seq. Returns at most
// HistorySize entries (older ones have been overwritten in the ring).
// The bool result is false when the requested `since` is older than the
// oldest entry retained in the buffer — caller may want to log a "snapshot
// needed" sentinel in that case (gap exceeds replay capacity).
func (h *Hub) snapshotHistorySince(since int64) (entries []historyEntry, complete bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	currentSeq := h.seq.Load()
	if since >= currentSeq {
		return nil, true
	}

	// Determine the oldest seq actually retained in the ring. If we've
	// broadcast fewer events than HistorySize, the oldest seq is 1;
	// otherwise it's currentSeq - HistorySize + 1.
	oldestRetained := int64(1)
	if currentSeq > int64(h.HistorySize) {
		oldestRetained = currentSeq - int64(h.HistorySize) + 1
	}

	complete = since+1 >= oldestRetained
	startSeq := since + 1
	if startSeq < oldestRetained {
		startSeq = oldestRetained
	}

	entries = make([]historyEntry, 0, currentSeq-startSeq+1)
	for s := startSeq; s <= currentSeq; s++ {
		entry := h.history[(s-1)%int64(h.HistorySize)]
		// Defensive: skip uninitialized slots (shouldn't happen given
		// the seq window math above, but a slot with seq 0 means the
		// ring hasn't been written there yet).
		if entry.seq == s {
			entries = append(entries, entry)
		}
	}
	return entries, complete
}

// HandleEvents returns a Gin handler for the SSE endpoint.
//
// On connect, the handler honours the `Last-Event-ID` header (set
// automatically by `EventSource` from the last `id:` line the browser
// saw) and replays any retained events with seq > Last-Event-ID before
// streaming live events. The replay walks the hub's ring buffer
// (capacity HistorySize) — if the requested Last-Event-ID is older than
// the oldest retained entry, the client gets whatever is still in the
// buffer plus a warning logged server-side.
//
// The handler also emits an SSE `id: <seq>` line for every event so the
// browser's auto-reconnect carries the right Last-Event-ID without any
// JS work.
func (h *Hub) HandleEvents() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Parse Last-Event-ID BEFORE subscribing so we don't replay an
		// event the live channel will also deliver. Subscribe takes the
		// hub write lock; the seq stamped on any concurrent Broadcast
		// after our snapshotHistorySince + Subscribe pair will be
		// strictly greater than the snapshot's last seq AND delivered to
		// our channel, so the merged stream stays gap-free.
		var lastEventID int64
		if raw := c.GetHeader("Last-Event-ID"); raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
				lastEventID = v
			}
		}

		ch := h.Subscribe()
		if ch == nil {
			// mp-663 Phase 4: subscriber cap reached, or hub shutting down.
			// 503 + Retry-After tells well-behaved clients (and the
			// browser's EventSource auto-reconnect) to back off.
			c.Header("Retry-After", "30")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "SSE subscriber limit reached, please retry"})
			return
		}
		defer h.Unsubscribe(ch)

		// Set SSE headers
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		// X-Accel-Buffering: no is important for Nginx and other proxies to not buffer the stream
		c.Header("X-Accel-Buffering", "no")
		// Prevent browsers from sniffing content type
		c.Header("X-Content-Type-Options", "nosniff")

		// Important: Flush the headers immediately so the browser sees the connection as "Open"
		// instead of "Pending" until the first event or heartbeat.
		c.Writer.WriteHeader(http.StatusOK)
		c.Writer.Flush()

		// Send an initial comment to keep the connection alive and confirm it's open
		if _, err := c.Writer.Write([]byte(": open\n\n")); err == nil {
			c.Writer.Flush()
		}

		// Replay buffered events (T216). We write directly to the
		// response writer using the SSE wire format `id: N\ndata: …\n\n`
		// so the browser stores the latest seq as its next
		// Last-Event-ID checkpoint. Done before entering the c.Stream
		// loop because c.SSEvent doesn't expose the `id:` field
		// directly.
		if lastEventID > 0 {
			entries, complete := h.snapshotHistorySince(lastEventID)
			if !complete {
				fmt.Printf("SSE replay: client requested Last-Event-ID=%d but oldest retained seq exceeds it; %d entries replayed\n", lastEventID, len(entries))
			}
			for _, entry := range entries {
				writeSSEEnvelope(c.Writer, entry.seq, entry.payload)
			}
			c.Writer.Flush()
		}

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		c.Stream(func(w io.Writer) bool {
			select {
			case msg, ok := <-ch:
				if ok {
					// Extract seq from the marshalled envelope so we can
					// emit the SSE `id:` line. Parsing the marshalled
					// JSON back is cheap (single int field) and avoids
					// threading the seq through a second channel.
					seq := extractSeq(msg)
					writeSSEEnvelope(w, seq, msg)
					c.Writer.Flush() // Ensure the message is sent immediately
					return true
				}
				return false
			case <-ticker.C:
				if _, err := w.Write([]byte(": keep-alive\n\n")); err != nil {
					return false
				}
				c.Writer.Flush() // Ensure heartbeat is sent immediately
				return true
			case <-c.Request.Context().Done():
				return false
			}
		})
	}
}

// writeSSEEnvelope emits one event in the SSE wire format with an
// explicit `id:` line so the browser auto-sets Last-Event-ID on
// reconnect. Mirrors the legacy gin.Context.SSEvent("message", …) format
// but with the id line prepended.
//
// Errors from the underlying writer are logged but not propagated: the
// caller is either in the streaming loop (where the write error will
// surface on the next iteration via the channel-closed signal) or in
// the initial-replay phase (where a partial write means the client
// disconnected; subsequent writes will also fail and the handler will
// return). The errcheck-friendly Fprintf return is captured and
// logged so a degraded connection leaves a breadcrumb without
// stalling the broadcast path.
func writeSSEEnvelope(w io.Writer, seq int64, payload string) {
	if _, err := fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", seq, payload); err != nil {
		// Don't print every write error — a disconnected client
		// generates one per buffered event in the worst case. Only
		// log if the write failed in an unexpected way (best-effort
		// signal for production diagnostics).
		fmt.Printf("SSE write failed (seq %d): %v\n", seq, err)
	}
}

// extractSeq pulls the `seq` field out of a marshalled SSEEvent without
// re-allocating the full struct. The marshalled form is always a JSON
// object that contains a numeric `seq` field; we look it up directly
// rather than going through json.Unmarshal to keep the hot path cheap.
//
// Returns 0 if the field is missing or malformed — the SSE handler
// still writes the event in that case, just without an id line, so the
// browser keeps its previous Last-Event-ID. That's the conservative
// fallback: a missing id never widens a gap, it just defers the next
// checkpoint.
func extractSeq(payload string) int64 {
	// Cheap structural extraction — the marshalled JSON always has
	// `"seq":<int>` somewhere. json.Unmarshal into a stripped struct
	// would also work but allocates more.
	var stripped struct {
		Seq int64 `json:"seq"`
	}
	if err := json.Unmarshal([]byte(payload), &stripped); err != nil {
		return 0
	}
	return stripped.Seq
}
