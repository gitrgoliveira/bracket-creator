package mobileapp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
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
	//   - POST /api/tournament/reset, the public recovery endpoint for
	//     a forgotten password. Payload: {originatorId} so the submitting
	//     tab can suppress its own broadcast and avoid clearing the
	//     credential it just wrote.
	//   - PUT /api/tournament, when the PUT body contains a non-empty
	//     password that differs from the stored one. Payload: {} (all
	//     sessions, including the editing tab, should re-authenticate).
	//   - POST /api/tournament, when a bootstrap POST overwrites an
	//     existing tournament with a different password. Payload: {}.
	// Consumers in admin mode (app.jsx) clear their localStorage credential
	// and re-show the AuthModal so a logged-in operator notices immediately
	// instead of waiting for their next write to fail with 401. Viewers
	// ignore the event, their flow doesn't depend on the admin password.
	EventPasswordReset EventType = "password_reset"
	EventAnnouncement  EventType = "announcement"
	EventDrawGenerated EventType = "draw_generated"
	EventDrawDiscarded EventType = "draw_discarded"
	// EventBracketQuarantined: a bracket.json that would not parse was renamed
	// aside and the knockout stage rebuilt. Every device is showing a wedged
	// competition when this happens, so they all need to reload.
	EventBracketQuarantined EventType = "bracket_quarantined"
	// EventLineupUpdated: a team's lineups or members changed. Payload:
	// {competitionId, teamId} and, for a lineup saved for a match, matchId
	// (lineupUpdatedPayload builds it for every writer).
	EventLineupUpdated  EventType = "lineup_updated"
	EventResyncRequired EventType = "resync_required"
	// EventSwissRoundGenerated: a new Swiss round's matches were generated.
	// Clients refresh their match list and competition view (the bumped
	// swissCurrentRound is in the payload so callers need no separate GET).
	// FR-050d.
	EventSwissRoundGenerated EventType = "swiss_round_generated"
)

// AutoCompleteErrorHeader is set on score/start responses when the
// post-write MaybeAutoCompletePools check itself errored. The value is a
// deliberately generic sentinel (AutoCompleteErrorValue) so we don't leak
// filesystem paths or other internal store details to clients, full error
// detail is logged server-side.
const (
	AutoCompleteErrorHeader = "X-Auto-Complete-Error"
	AutoCompleteErrorValue  = "failed"
)

// DefaultHistorySize is the default ring buffer capacity for replay-on-reconnect (T216).
// The v3 review measured 100 events at roughly 30 seconds of activity on a
// busy tournament floor (multi-court bulk score); 200 holds about a minute,
// so a device off the wifi that long still catches up by replay.
const DefaultHistorySize = 200

// DefaultMaxSSEClients caps concurrent /api/events subscribers per process.
// Each subscriber allocates one buffered channel (100-element historyEntry
// buffer) plus one streaming goroutine. Base struct/goroutine overhead per
// client:
//   - Channel buffer: 100 entries × 64 B (int64 seq, two string headers,
//     the event type and a pointer to the shared public copy) ≈ 6.4 KB
//   - Channel struct overhead: ~100 B
//   - Goroutine stack: 2–8 KB (starts small, grows on demand)
//   - Base total: ~8–15 KB resident per client
//
// Note: this is the base overhead only. Each queued (buffered) message also
// keeps its payload bytes alive on the heap until the client drains it. Under
// bursty broadcasts or slow/stalled clients a fully-backlogged channel could
// add up to ~100 × (payload size) of additional memory per client, e.g. a
// 2 KB JSON payload × 100 slots = ~200 KB worst-case. Stalled clients are
// dropped by the non-blocking send (select/default) before the buffer fills
// in practice, but the extra allocation is real during the drop window.
//
// At 5000 active (draining) clients the base cost is ~40–75 MB, comfortably
// within the RAM budget of a single-core VPS or RPi-class hardware used for
// large-scale deployments (tens of teams, 1000+ live spectators).
//
// Broadcast fan-out is O(N) in the subscriber count and serialises under
// h.mu.Lock(). At 5000 clients the lock window is bounded by the
// non-blocking channel send (select/default), so stalled clients are
// dropped rather than blocking the broadcaster.
//
// Override at startup via the SSE_MAX_CLIENTS env var or by passing a
// positive value to NewHubWithLimits. A non-positive (zero or negative)
// value disables the cap entirely, used by tests that need to exceed
// the default and not enforced anywhere in production. The cap is mp-663
// Phase 4 mitigation for resource-exhaustion via unbounded subscriber maps.
// Raised from 1000 → 5000 by mp-9afd to support large-scale events (1000+
// viewers). A real hardware load test (goroutine/fd/memory budget at 5000
// clients) is still required before a large live deployment.
const DefaultMaxSSEClients = 5000

// SSEEvent represents the payload sent to clients.
//
// Seq (T215, A2 closure) is a strictly monotonic counter stamped by the
// hub on every Broadcast. Consumers use it for two things:
//
//  1. Gap detection, if the JS receiver sees seq jump from N to N+2, it
//     missed N+1 (network reorder/loss is impossible on a single SSE
//     connection, but a reconnect can leave a gap between the last live
//     event before the disconnect and the first one after the reconnect)
//     and triggers a full refetch.
//  2. Replay on reconnect, `EventSource` automatically resends the last
//     `id:` it saw via `Last-Event-ID`; the hub walks its ring buffer to
//     re-emit events with seq > Last-Event-ID before resuming live
//     streaming.
//
// Wire format: `data: {"type":"match_updated","data":{…},"seq":12345}`
// plus an SSE `id: 12345` line so the browser sets `Last-Event-ID`
// automatically on reconnect.
//
// Existing JS that ignores `seq` continues to work, the field is purely
// additive.
type SSEEvent struct {
	Type EventType `json:"type"`
	Data any       `json:"data"`
	Seq  int64     `json:"seq"`
}

// historyEntry pairs the stamped seq with the marshalled JSON payload.
// We store the marshalled form (not the SSEEvent struct) so the reconnect
// replay path doesn't have to re-marshal, keeps cold-replay cheap and
// guarantees the replayed bytes are exactly what live clients saw.
// Subscriber channels carry the same pair so the streaming loop can emit
// the SSE `id:` line without re-parsing the payload.
//
// payload is the event as broadcast, with every name in it (bc-hnam): the
// organiser's stream sends it as it is, and the stream of anyone else is
// projected from it per subscriber when it is sent (Hub.project), using the
// competition's option at that moment. typ and compID say what the event is
// and which competition it is about, so the projection needs no decode to
// tell whether it has anything to hide. The projection always decodes these
// stored bytes, never a pointer the broadcaster kept, so a later change to
// the broadcaster's value can never reach a stream.
type historyEntry struct {
	seq     int64
	payload string
	typ     EventType
	compID  string
	// masked is the shared memo of the event's public projection, computed
	// by the first stream that needs it and reused by every other one, so a
	// masked event is marshalled once per audience class rather than once per
	// subscriber. nil for an event that is never masked.
	masked *maskedPayload
}

// maskedPayload memoises an event's public projection (no names revealed).
type maskedPayload struct {
	mu      sync.Mutex
	done    bool
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
// is observed atomically, without that, a concurrent reconnect could
// read a partial history slice and replay events out of order.
type Hub struct {
	// clients maps each subscriber's channel to who it is answered for
	// (sseSubscriber): the channel carries the event as broadcast, and the
	// streaming loop projects it for that subscriber when it sends it.
	clients map[chan historyEntry]*sseSubscriber
	mu      sync.RWMutex

	// seq is the monotonic envelope counter (atomic so Broadcast can
	// stamp without holding the full hub lock, but we still take the
	// write lock to add to history in the same critical section so
	// replay readers see a consistent (seq, history) snapshot).
	seq atomic.Int64

	// history is a ring buffer of the last HistorySize broadcast
	// envelopes. Indexed by `seq % HistorySize`. Reads (via
	// snapshotHistorySinceLocked) hold the lock; writes (in Broadcast)
	// take the write lock.
	history     []historyEntry
	HistorySize int

	// MaxClients caps concurrent SSE subscribers (mp-663 Phase 4).
	// Subscribe returns nil when the cap is reached; HandleEvents
	// converts that into HTTP 503 so the client knows to back off. A
	// non-positive value disables the cap (treat as unbounded, useful
	// for tests).
	MaxClients int

	// closed is set when Close has been called (mp-663 Phase 2 graceful
	// shutdown hook). New subscribers are rejected after this; existing
	// subscriber channels are closed so the per-connection streaming
	// goroutine in HandleEvents exits cleanly. Guarded by mu.
	closed bool

	// HeartbeatInterval is the SSE keep-alive cadence (default 15s, set by
	// NewHubWithLimits). Exposed so tests can drive HandleEvents with a short
	// interval and observe a real emitted heartbeat frame rather than asserting
	// a hard-coded literal.
	HeartbeatInterval time.Duration

	// heldPushes is the match-update coalescer RegisterMatchHandlers attaches;
	// broadcastMatchNow reaches it (through pushMatchNow) from the handlers
	// that broadcast a match_updated of their own. Set once at registration, before serving.
	heldPushes *matchBroadcastCoalescer
	// pushClock is the clock RegisterMatchHandlers builds that coalescer
	// with (newMatchBroadcastCoalescerWithClock); nil is time.Now. Set
	// before registration, never after, so no timer reads it as it changes.
	pushClock func() time.Time

	// names is the audience judge and the name masks the router shares with
	// the public reads (bc-hnam), attached once before serving
	// (attachPublicNames). With none attached (a bare hub in a test) every
	// stream is sent the events as broadcast and no ticket can be had.
	names *publicNames
	// tickets holds the event-stream tickets handed out and not yet used.
	tickets eventTickets
	// rejudges counts the reveal re-judgements a tournament_updated started
	// (rejudgeRevealStreams), so Close waits for them.
	rejudges sync.WaitGroup
}

// sseSubscriber is who one event stream is answered for: the audience its
// ticket was judged for (the public without one), and the X-Reveal-Key
// header a non-organiser's ticket was asked with, kept so a change of a
// competition's option can be judged again (rejudgeRevealStreams).
type sseSubscriber struct {
	aud    audience
	reveal string
}

// attachPublicNames gives the hub the router's audience judge and name
// masks. Called once, before serving.
func (h *Hub) attachPublicNames(names *publicNames) {
	h.names = names
}

// pushMatchNow runs send under the attached coalescer's lock for matchKey
// (compID:matchID), after cancelling the running push it holds; without a
// coalescer it just sends. See broadcastMatchNow.
func (h *Hub) pushMatchNow(matchKey string, send func()) {
	if h.heldPushes != nil {
		h.heldPushes.SendNow(matchKey, send)
		return
	}
	send()
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
// non-positive value (zero or negative) DISABLES the cap entirely,
// useful for tests that need to exceed DefaultMaxSSEClients without
// constructing thousands of stub clients to prove the cap fires. It is
// deliberately not coerced to the default so callers can opt out.
func NewHubWithLimits(historySize, maxClients int) *Hub {
	if historySize <= 0 {
		historySize = DefaultHistorySize
	}
	return &Hub{
		clients:           make(map[chan historyEntry]*sseSubscriber),
		history:           make([]historyEntry, historySize),
		HistorySize:       historySize,
		MaxClients:        maxClients,
		HeartbeatInterval: 15 * time.Second,
	}
}

// Subscribe adds a new client channel to the hub. Returns nil when:
//   - the hub has been Close()d (graceful shutdown in progress)
//   - the subscriber count has reached MaxClients
//
// HandleEvents converts a nil return into HTTP 503 so the SSE client
// knows to back off and retry rather than hanging on a stuck connection.
//
// The channel carries every event as broadcast, names included: it is the
// in-process view (tests, internal listeners). A stream is only ever sent
// what Hub.project gives its subscriber.
func (h *Hub) Subscribe() chan historyEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.subscribeLocked(&sseSubscriber{})
}

// subscribeLocked registers a new subscriber channel for sub. Caller holds
// h.mu.
func (h *Hub) subscribeLocked(sub *sseSubscriber) chan historyEntry {
	if h.closed {
		return nil
	}
	if h.MaxClients > 0 && len(h.clients) >= h.MaxClients {
		return nil
	}
	// Buffer absorbs short bursts (bulk-score and schedule updates) for ~300
	// concurrent SSE clients; truly stalled clients are detected via the
	// non-blocking send in Broadcast and unsubscribed.
	ch := make(chan historyEntry, 100)
	h.clients[ch] = sub
	return ch
}

// subscribeWithReplay atomically registers a subscriber and snapshots the
// replay window under ONE write-lock acquisition, so the returned channel
// can only ever deliver events stamped after headSeq: every event is either
// in entries (broadcast before registration) or arrives on ch (after),
// never both. Registering and snapshotting in separate lock acquisitions
// would leave an overlap window in which a concurrent Broadcast lands in
// both the ring and the channel, forcing downstream duplicate filtering.
//
// since == 0 means "fresh connect, no replay wanted": entries is nil and
// complete is true. headSeq is the newest stamped seq at registration time
// (for the resync path). A nil ch means the hub is closed or at MaxClients,
// the same contract as Subscribe.
func (h *Hub) subscribeWithReplay(since int64) (ch chan historyEntry, entries []historyEntry, complete bool, headSeq int64) {
	return h.subscribeWithReplayFor(since, &sseSubscriber{})
}

// subscribeWithReplayFor is subscribeWithReplay for the stream of sub.
func (h *Hub) subscribeWithReplayFor(since int64, sub *sseSubscriber) (ch chan historyEntry, entries []historyEntry, complete bool, headSeq int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch = h.subscribeLocked(sub)
	headSeq = h.seq.Load()
	complete = true
	if ch != nil && since > 0 {
		entries, complete = h.snapshotHistorySinceLocked(since)
	}
	return ch, entries, complete, headSeq
}

// Close marks the hub as shutting down and closes every subscriber
// channel. The per-connection streaming goroutine in HandleEvents
// observes the channel close (the `case msg, ok:= <-ch; if !ok`
// branch) and returns, unblocking http.Server.Shutdown's wait loop.
//
// Idempotent, safe to call twice. After Close, Subscribe returns nil
// for all new subscribers and Broadcast becomes a no-op observable to
// clients (no live channels remain).
func (h *Hub) Close() {
	// Stop the held running pushes first, outside h.mu: a trailing send
	// already under way broadcasts, which takes h.mu.
	if h.heldPushes != nil {
		h.heldPushes.Close()
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	for ch := range h.clients {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
	// No re-judgement starts once closed is set (Broadcast adds to rejudges
	// under h.mu, and only while the hub is open), so this wait is final.
	h.rejudges.Wait()
}

// Unsubscribe removes a client channel
func (h *Hub) Unsubscribe(ch chan historyEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unsubscribeLocked(ch)
}

func (h *Hub) unsubscribeLocked(ch chan historyEntry) {
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
// sequenced, there's no "stamp before lock, lock for fan-out" window
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
	entry := historyEntry{
		seq:     seq,
		payload: string(payload),
		typ:     eventType,
	}
	if sseEventClassOf(eventType) == sseEventMasked {
		entry.compID = eventCompetitionID(data, payload)
		entry.masked = &maskedPayload{}
		if entry.compID == "" {
			// Every masked event names its competition; one that does not
			// is sent to everyone but the organiser with no data at all.
			slog.Warn("mobile-app: a masked event names no competition; it is sent without its data", "type", eventType, "seq", seq)
		}
	}
	if eventType == EventTournamentUpdated && h.names != nil && !h.closed {
		// A competition's option may have changed: judge the reveal streams
		// again once the event is out (rejudgeRevealStreams takes h.mu, so
		// it runs only after this fan-out has released it). Added under h.mu
		// while the hub is open, so Close's wait for it is final.
		safeGo(&h.rejudges, nil, h.rejudgeRevealStreams)
	}

	// Append to ring buffer first so a client that reconnects between the
	// stamp and the fan-out sees the same envelope on replay.
	if h.HistorySize > 0 {
		h.history[(seq-1)%int64(h.HistorySize)] = entry
	}

	// Fan out under the already-held write lock. Dead (full-buffer) clients
	// are collected and unsubscribed after the range so the map is never
	// mutated mid-iteration; no snapshot copy of the client set is needed.
	var dead []chan historyEntry
	for ch := range h.clients {
		select {
		case ch <- entry:
		default:
			dead = append(dead, ch)
		}
	}

	for _, ch := range dead {
		h.unsubscribeLocked(ch)
	}
}

// snapshotHistorySinceLocked returns history entries whose seq is strictly
// greater than `since`, ordered by ascending seq. Returns at most
// HistorySize entries (older ones have been overwritten in the ring).
// The bool result is false when the requested `since` is older than the
// oldest entry retained in the buffer, caller may want to log a "snapshot
// needed" sentinel in that case (gap exceeds replay capacity).
// Caller holds h.mu (read or write).
func (h *Hub) snapshotHistorySinceLocked(since int64) (entries []historyEntry, complete bool) {
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
// (capacity HistorySize), if the requested Last-Event-ID is older than
// the oldest retained entry, the client gets whatever is still in the
// buffer plus a warning logged server-side.
//
// The handler also emits an SSE `id: <seq>` line for every event so the
// browser's auto-reconnect carries the right Last-Event-ID without any
// JS work. The SPA never relies on that auto-reconnect: it closes the
// source on an error and opens a new one, which carries no header, so it
// sends the last id it saw as the `lastEventId` query parameter instead.
// The header wins when both are present.
func (h *Hub) HandleEvents() gin.HandlerFunc {
	return func(c *gin.Context) {
		var lastEventID int64
		raw := c.GetHeader("Last-Event-ID")
		if raw == "" {
			raw = c.Query("lastEventId")
		}
		if raw != "" {
			if v, err := strconv.ParseInt(raw, 10, 64); err == nil && v > 0 {
				lastEventID = v
			}
		}

		// Who the stream is answered for (bc-hnam): the audience of the
		// ticket it was opened with, or the public without one. A ticket
		// that is spent, expired or unknown is refused, never taken as the
		// public: a signed-in console must never land silently on a masked
		// stream, so it asks for a new ticket instead.
		sub := &sseSubscriber{}
		if raw, has := c.GetQuery("ticket"); has {
			judged, ok := h.redeemTicket(raw)
			if !ok {
				c.Header("Cache-Control", "no-store")
				c.JSON(http.StatusUnauthorized, gin.H{
					"error": "This event-stream ticket has been used or has expired. Ask for a new one.",
					"code":  codeEventTicketInvalid,
				})
				return
			}
			sub = &judged
		}

		// Registration and the replay snapshot happen atomically under one
		// hub lock, so the merged replay+live stream is both gap-free and
		// duplicate-free by construction: every event lands in exactly one
		// of the two (matters for non-deduping SSE consumers; the SPA also
		// dedups via checkSeqGap, external integrations may not).
		ch, entries, complete, headSeq := h.subscribeWithReplayFor(lastEventID, sub)
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
		// no-store, not no-cache: what the stream carries depends on who
		// opened it (bc-hnam), so no copy of it may be kept.
		c.Header("Cache-Control", "no-store")
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
		// The stream's hello (bc-evts F2): the first data frame names the
		// server's build, so a tab running an older bundle can tell it must
		// reload. It is shaped as a heartbeat (no id:, no seq consumed), which
		// every client already takes as liveness only.
		if hello, err := json.Marshal(sseHello{Type: "heartbeat", Hello: true, NowMs: time.Now().UnixMilli(), Build: AppBuild()}); err == nil {
			if _, werr := fmt.Fprintf(c.Writer, "data: %s\n\n", hello); werr == nil {
				c.Writer.Flush()
			}
		}

		// Replay buffered events (T216). We write directly to the
		// response writer using the SSE wire format `id: N\ndata: …\n\n`
		// so the browser stores the latest seq as its next
		// Last-Event-ID checkpoint. Done before entering the c.Stream
		// loop because c.SSEvent doesn't expose the `id:` field
		// directly.
		if lastEventID > 0 {
			currentHeadSeq := headSeq
			// Unsatisfiable when the ring rolled past the client's Last-Event-ID
			// (eviction) OR the server restarted and its in-memory seq reset below
			// the client's Last-Event-ID, INCLUDING a fresh restart at head seq 0,
			// before any event has been broadcast (the client's stale Last-Event-ID
			// is still ahead of 0). That last case must still resync: otherwise a
			// client that keeps an in-memory lastSeq across reconnects would treat
			// the first post-restart event (seq=1) as a stale duplicate and drop it,
			// leaving the screen silently stale.
			unsatisfiable := !complete || lastEventID > currentHeadSeq
			if unsatisfiable {
				// A gap-free replay is impossible. Tell the client to resync (reset
				// its in-memory lastSeq + full refetch). Fall through to the normal
				// streaming loop afterwards.
				fmt.Printf("SSE replay: client Last-Event-ID=%d not satisfiable (complete=%v, headSeq=%d); sending resync_required\n", lastEventID, complete, currentHeadSeq)
				resyncEvent := SSEEvent{Type: EventResyncRequired, Seq: currentHeadSeq}
				resyncPayload, err := json.Marshal(resyncEvent)
				if err != nil {
					fmt.Printf("SSE resync marshal error: %v\n", err)
				} else if currentHeadSeq > 0 {
					// Stamp id: <head> so the browser's Last-Event-ID advances to head
					// and the client won't immediately re-gap.
					writeSSEEnvelope(c.Writer, currentHeadSeq, string(resyncPayload))
					c.Writer.Flush()
				} else {
					// Fresh restart, head seq 0: emit the resync WITHOUT an id: line so
					// the browser's Last-Event-ID isn't forced to "0" (a real id:0 would
					// corrupt later reconnect checkpoints). The data frame still fires
					// onmessage, so the client resets its lastSeq and refetches.
					if _, werr := fmt.Fprintf(c.Writer, "data: %s\n\n", resyncPayload); werr != nil {
						fmt.Printf("SSE resync write failed: %v\n", werr)
					}
					c.Writer.Flush()
				}
			} else {
				for i := range entries {
					writeSSEEnvelope(c.Writer, entries[i].seq, h.project(&entries[i], sub))
				}
				c.Writer.Flush()
			}
		}

		hbInterval := h.HeartbeatInterval
		if hbInterval <= 0 {
			hbInterval = 15 * time.Second
		}
		ticker := time.NewTicker(hbInterval)
		defer ticker.Stop()

		c.Stream(func(w io.Writer) bool {
			select {
			case msg, ok := <-ch:
				if ok {
					writeSSEEnvelope(w, msg.seq, h.project(&msg, sub))
					c.Writer.Flush() // Ensure the message is sent immediately
					return true
				}
				return false
			case <-ticker.C:
				// Emit a real data frame (no id: line) so client JS can
				// observe the heartbeat via onmessage. SSE comment lines
				// (": keep-alive") are silently swallowed by the browser
				// EventSource API and are invisible to JS handlers.
				// Omitting id: means the browser's Last-Event-ID is
				// unchanged, the heartbeat neither consumes a seq nor
				// creates a gap.
				//
				// nowMs is the server clock at emit time, carried so a client
				// can NOTICE its own clock drifting without asking. It is a
				// TRIPWIRE ONLY and must never be adopted as an offset: a
				// one-way push carries no round trip to correct against, so a
				// frame delayed by a slow network or a busy tab reads as a
				// server clock in the past and would drag the client's frame
				// BACKWARDS, manufacturing exactly the skew the guard fences.
				// A client that sees a large divergence relearns the offset
				// properly via GET /api/time, which is RTT-corrected.
				if _, err := fmt.Fprintf(w, "data: {\"type\":\"heartbeat\",\"nowMs\":%d}\n\n", time.Now().UnixMilli()); err != nil {
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
// sseHello is the stream's first data frame (bc-evts F2): a heartbeat that
// also names the server's build (AppBuild).
type sseHello struct {
	Type  string `json:"type"`
	Hello bool   `json:"hello"`
	NowMs int64  `json:"nowMs"`
	Build string `json:"build"`
}

func writeSSEEnvelope(w io.Writer, seq int64, payload string) {
	if _, err := fmt.Fprintf(w, "id: %d\nevent: message\ndata: %s\n\n", seq, payload); err != nil {
		// Don't print every write error, a disconnected client
		// generates one per buffered event in the worst case. Only
		// log if the write failed in an unexpected way (best-effort
		// signal for production diagnostics).
		fmt.Printf("SSE write failed (seq %d): %v\n", seq, err)
	}
}

// --- Per-subscriber streams (bc-hnam, Q4 a) ---------------------------------
//
// The stream hides names the way the public reads do, per subscriber. A
// browser cannot send a header on an EventSource, so a caller with
// credentials (the password, reveal keys) first asks POST /api/events/ticket
// for a single-use ticket bound to its audience and opens
// /api/events?ticket=..., so no secret ever sits in a URL. No ticket is the
// public.
//
// Every event is classified in sseEventCatalogue. One that can name a
// competitor is masked: it names its competition, and each stream not
// entitled to that competition's names is sent a masked copy, built from the
// stored bytes when it is sent and judged on the competition's option and
// reveal expiry at that moment, so changing the option needs no reset of the
// replay history. The public copy is built once per event and shared
// (historyEntry.masked); only a stream holding competitors' own links has a
// copy built for it alone.

// codeEventTicketInvalid: /api/events was opened with a ticket that is
// spent, expired or unknown (401). The client asks for a new ticket.
const codeEventTicketInvalid = "event_ticket_invalid"

// eventTicketTTL is how long a ticket waits to be used.
const eventTicketTTL = 60 * time.Second

// maxEventTickets bounds the tickets waiting to be used. Past it, expired
// ones are dropped; while every one is still live, no new ticket is given
// (503), so a flood of ticket requests cannot grow the map without bound.
const maxEventTickets = 10000

// eventTicketBytes is the size of a ticket's random part (hex on the wire).
const eventTicketBytes = 32

// sseEventClass is what a stream not entitled to every name is sent for an
// event.
type sseEventClass int

const (
	// sseEventPasses: the event names no competitor, so every stream is sent
	// it as broadcast.
	sseEventPasses sseEventClass = iota + 1
	// sseEventMasked: the event can name competitors of the competition it
	// names (data.competitionId), so a stream not entitled to that
	// competition's names is sent a masked copy.
	sseEventMasked
	// sseEventOrganiserOnly: only the organiser's stream is sent its data;
	// every other stream is sent the event with no data, so its seq still
	// arrives and no gap is seen.
	sseEventOrganiserOnly
)

// sseEventSpec classifies one event type. mask rewrites a masked event's
// data object in place through v.
type sseEventSpec struct {
	class sseEventClass
	mask  func(data map[string]json.RawMessage, v maskView) error
}

// sseEventCatalogue classifies EVERY event type the hub sends; a type missing
// from it is sent to no one but the organiser with its data (fail closed).
// TestSSEEventCatalogue_ClassifiesEveryEventType holds it to every EventType
// constant in the package.
var sseEventCatalogue = map[EventType]sseEventSpec{
	// A match's result: its sides, winner, bout rows and representative-bout
	// fighters name competitors and team members.
	EventMatchUpdated: {class: sseEventMasked, mask: maskMatchUpdatedData},
	// The rest carry ids, numbers of rounds or nothing at all.
	EventCompetitionStarted:      {class: sseEventPasses},
	EventCompetitionCompleted:    {class: sseEventPasses},
	EventTournamentUpdated:       {class: sseEventPasses},
	EventScheduleUpdated:         {class: sseEventPasses},
	EventCompetitorStatusUpdated: {class: sseEventPasses}, // a player id and a reason code
	EventParticipantsUpdated:     {class: sseEventPasses},
	EventPasswordReset:           {class: sseEventPasses}, // an originator id, or nothing
	// Free text the organiser typed: shown to everyone as typed (E6; the
	// announcement box warns while a competition hides names).
	EventAnnouncement:       {class: sseEventPasses},
	EventDrawGenerated:      {class: sseEventPasses},
	EventDrawDiscarded:      {class: sseEventPasses},
	EventBracketQuarantined: {class: sseEventPasses},
	EventLineupUpdated:      {class: sseEventPasses}, // competition, team and match ids
	EventResyncRequired:     {class: sseEventPasses},
	// Swiss never hides names (Q-swiss), and the event carries counts only.
	EventSwissRoundGenerated: {class: sseEventPasses},
}

// sseEventClassOf is t's class; an unknown type is organiser-only.
func sseEventClassOf(t EventType) sseEventClass {
	if spec, ok := sseEventCatalogue[t]; ok {
		return spec.class
	}
	return sseEventOrganiserOnly
}

// eventCompetitionID is the competition a masked event names: its data's
// competitionId, read from the value when it is a map and from the
// marshalled envelope otherwise.
func eventCompetitionID(data any, payload []byte) string {
	switch d := data.(type) {
	case gin.H:
		id, _ := d["competitionId"].(string)
		return id
	case map[string]any:
		id, _ := d["competitionId"].(string)
		return id
	case map[string]string:
		return d["competitionId"]
	}
	var probe struct {
		Data struct {
			CompetitionID string `json:"competitionId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return ""
	}
	return probe.Data.CompetitionID
}

// project is what sub's stream is sent for e: the event as broadcast for the
// organiser, for an event that names no one, and for a competition that
// shows names or whose names sub may see; a masked copy otherwise. Whatever
// cannot be judged (an unknown event type, a masked event naming no
// competition, a competition that cannot be read, a copy that cannot be
// built) is sent with no data, never as broadcast.
func (h *Hub) project(e *historyEntry, sub *sseSubscriber) string {
	names := h.names
	if names == nil || sub == nil || sub.aud.isOrganiser() {
		return e.payload
	}
	spec, known := sseEventCatalogue[e.typ]
	if !known || spec.class == sseEventOrganiserOnly {
		return withoutData(e)
	}
	if spec.class == sseEventPasses {
		return e.payload
	}
	if e.compID == "" {
		return withoutData(e)
	}
	comp, err := names.store.LoadCompetition(e.compID)
	if err != nil || comp == nil {
		return withoutData(e)
	}
	if !comp.HidesNames() {
		return e.payload
	}
	aud := streamRevealFor(names, comp, sub.aud)
	if aud.revealsAll(comp.ID) {
		return e.payload
	}
	if len(aud.revealedCompetitors(comp.ID)) > 0 {
		// A stream holding competitors' own links: a copy for it alone.
		out, ok := maskEvent(names, comp, aud, e, spec)
		if !ok {
			return withoutData(e)
		}
		return out
	}
	return e.publicCopy(func() (string, bool) { return maskEvent(names, comp, audience{}, e, spec) })
}

// publicCopy is e's public projection, built by build the first time a
// stream needs it and shared after. A copy build could not finish (a file
// that could not be read: the mask failed closed) is sent but not kept, so
// the next stream builds it again.
func (e *historyEntry) publicCopy(build func() (string, bool)) string {
	if e.masked == nil {
		if out, ok := build(); ok {
			return out
		}
		return withoutData(e)
	}
	e.masked.mu.Lock()
	defer e.masked.mu.Unlock()
	if e.masked.done {
		return e.masked.payload
	}
	out, ok := build()
	if !ok {
		return withoutData(e)
	}
	e.masked.payload, e.masked.done = out, true
	return out
}

// streamRevealFor is what aud's reveal links unlock in comp NOW: a stream
// keeps the audience its ticket was judged for, so the option and the
// expiry are judged again on every event. A competition link counts only
// while the competition gives competition links, competitors' own links only
// while it gives per-person links, and either only while its links are open
// (state.RevealOpen).
func streamRevealFor(names *publicNames, comp *state.Competition, aud audience) audience {
	wantAll := aud.competitions[comp.ID]
	own := aud.revealedCompetitors(comp.ID)
	if !wantAll && len(own) == 0 {
		return audience{}
	}
	t, err := names.store.LoadTournament()
	if err != nil || t == nil || !state.RevealOpen(comp, t, names.now()) {
		return audience{}
	}
	switch {
	case comp.PublicNames == state.PublicNamesCompetition && wantAll:
		return audience{competitions: map[string]bool{comp.ID: true}}
	case comp.PublicNames == state.PublicNamesPerson && len(own) > 0:
		return audience{competitors: map[string]map[string]bool{comp.ID: own}}
	}
	return audience{}
}

// sseEnvelope is the wire envelope, read back from the stored bytes to be
// masked. Field order matches SSEEvent's.
type sseEnvelope struct {
	Type EventType                  `json:"type"`
	Data map[string]json.RawMessage `json:"data"`
	Seq  int64                      `json:"seq"`
}

// maskEvent builds e's masked copy for aud from the stored bytes. The second
// return is false when no copy could be built. A copy built through a mask
// that failed closed (a file that could not be read) is still a copy, sent
// with every unknown name as "?", but is reported as not finished, so it is
// not shared.
func maskEvent(names *publicNames, comp *state.Competition, aud audience, e *historyEntry, spec sseEventSpec) (string, bool) {
	v, viewErr := names.view(comp, aud)
	if viewErr != nil {
		slog.Warn("mobile-app: event stream: names masked with what could be read", "competition", comp.ID, "err", viewErr)
	}
	var env sseEnvelope
	if err := json.Unmarshal([]byte(e.payload), &env); err != nil || env.Data == nil {
		return "", false
	}
	if err := spec.mask(env.Data, v); err != nil {
		slog.Warn("mobile-app: event stream: an event could not be masked; sent without its data", "type", e.typ, "competition", comp.ID, "err", err)
		return "", false
	}
	if v.namesHidden() {
		env.Data["namesHidden"] = json.RawMessage("true")
	}
	out, err := json.Marshal(env)
	if err != nil {
		return "", false
	}
	if viewErr != nil {
		return string(out), false
	}
	return string(out), true
}

// maskMatchUpdatedData masks a match_updated's result: the stored match the
// push carried (pool, league or knockout, as a MatchResult). A push that
// carries no result, or a null one, names no one.
func maskMatchUpdatedData(data map[string]json.RawMessage, v maskView) error {
	raw, ok := data["result"]
	if !ok || string(raw) == "null" {
		return nil
	}
	var m state.MatchResult
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("match_updated result: %w", err)
	}
	v.match(&m)
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("match_updated result: %w", err)
	}
	data["result"] = b
	return nil
}

// withoutData is e with its data dropped: its type and seq still reach the
// stream, so the client sees no gap and refetches what it reads.
func withoutData(e *historyEntry) string {
	out, err := json.Marshal(SSEEvent{Type: e.typ, Seq: e.seq})
	if err != nil {
		return fmt.Sprintf(`{"type":%q,"data":null,"seq":%d}`, e.typ, e.seq)
	}
	return string(out)
}

// eventTickets holds the event-stream tickets handed out and not yet used.
type eventTickets struct {
	mu sync.Mutex
	m  map[string]eventTicket
	// now is the clock tickets are timed by; nil is time.Now.
	now func() time.Time
}

type eventTicket struct {
	sub     sseSubscriber
	expires time.Time
}

func (t *eventTickets) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// errTooManyEventTickets: maxEventTickets live tickets are waiting.
var errTooManyEventTickets = errors.New("too many event-stream tickets waiting")

// issueTicket hands out a single-use ticket for sub, valid eventTicketTTL.
func (h *Hub) issueTicket(sub sseSubscriber) (string, time.Time, error) {
	var b [eventTicketBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("event ticket: %w", err)
	}
	ticket := hex.EncodeToString(b[:])
	t := &h.tickets
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock()
	if t.m == nil {
		t.m = map[string]eventTicket{}
	}
	if len(t.m) >= maxEventTickets {
		for k, e := range t.m {
			if !now.Before(e.expires) {
				delete(t.m, k)
			}
		}
		if len(t.m) >= maxEventTickets {
			return "", time.Time{}, errTooManyEventTickets
		}
	}
	expires := now.Add(eventTicketTTL)
	t.m[ticket] = eventTicket{sub: sub, expires: expires}
	return ticket, expires, nil
}

// redeemTicket spends ticket: the subscriber it was handed out for, and
// whether it was waiting and unexpired. A ticket is gone once asked for,
// whatever the answer.
func (h *Hub) redeemTicket(ticket string) (sseSubscriber, bool) {
	t := &h.tickets
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.m[ticket]
	if !ok {
		return sseSubscriber{}, false
	}
	delete(t.m, ticket)
	if !t.clock().Before(e.expires) {
		return sseSubscriber{}, false
	}
	return e.sub, true
}

// HandleEventTicket is POST /api/events/ticket: the caller's credentials,
// sent as headers (X-Tournament-Password, X-Reveal-Key), are judged as for a
// public read, and a single-use ticket bound to that audience is answered,
// {ticket, expiresAt}, to open /api/events?ticket=... within eventTicketTTL.
// A password sent but wrong is 401, as on every public read. A reveal key
// unlocks names on the stream only, never a write.
func (h *Hub) HandleEventTicket() gin.HandlerFunc {
	return func(c *gin.Context) {
		names := h.names
		if names == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "event-stream tickets are not available"})
			return
		}
		aud, ok := names.judge(c)
		if !ok {
			return
		}
		sub := sseSubscriber{aud: aud}
		if !aud.isOrganiser() {
			// Kept even when it reveals nothing now: a later change of a
			// competition's option is judged against it again.
			sub.reveal = c.GetHeader(revealKeyHeader)
		}
		ticket, expires, err := h.issueTicket(sub)
		if errors.Is(err, errTooManyEventTickets) {
			c.Header("Retry-After", "5")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Too many event-stream tickets are waiting; retry shortly"})
			return
		}
		if err != nil {
			slog.Error("mobile-app: event-stream ticket not issued", "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ticket": ticket, "expiresAt": expires.UnixMilli()})
	}
}

// rejudgeRevealStreams judges every stream opened with reveal keys again,
// after a tournament_updated (a competition's option, its expiry or its
// format may have changed), and ends each one whose keys now unlock
// something else. Its client opens a new stream with a new ticket and reads
// again, so it shows what the keys unlock now. Streams whose keys unlock the
// same are left open: what they are sent is judged on every event anyway
// (streamRevealFor). The event that started this is already in every
// stream's channel, and an ended stream sends what its channel holds before
// it ends.
func (h *Hub) rejudgeRevealStreams() {
	names := h.names
	if names == nil {
		return
	}
	type held struct {
		ch  chan historyEntry
		sub *sseSubscriber
	}
	var reveal []held
	h.mu.RLock()
	for ch, sub := range h.clients {
		if sub != nil && sub.reveal != "" && !sub.aud.isOrganiser() {
			reveal = append(reveal, held{ch, sub})
		}
	}
	h.mu.RUnlock()
	if len(reveal) == 0 {
		return
	}
	judged := map[string]string{}
	var ended []chan historyEntry
	for _, r := range reveal {
		key, ok := judged[r.sub.reveal]
		if !ok {
			key = names.revealAudience(r.sub.reveal).key()
			judged[r.sub.reveal] = key
		}
		if key != r.sub.aud.key() {
			ended = append(ended, r.ch)
		}
	}
	if len(ended) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range ended {
		h.unsubscribeLocked(ch)
	}
}
