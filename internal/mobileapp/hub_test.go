package mobileapp

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// decodeHubEvent unmarshals a subscriber-channel entry's payload into an
// SSEEvent, confining knowledge of the hub's internal entry shape to this
// helper (plus drainHubEvents in handlers_match_test.go, which builds on
// it) rather than every asserting test.
func decodeHubEvent(t *testing.T, msg historyEntry) SSEEvent {
	t.Helper()
	var e SSEEvent
	require.NoError(t, json.Unmarshal([]byte(msg.payload), &e))
	return e
}

// TestSubscribeWithReplay_AtomicSnapshotBoundary pins the invariant that
// makes the SSE stream duplicate-free by construction: subscribeWithReplay
// registers the channel and snapshots the replay window under ONE lock
// acquisition, so every event is either in the returned entries (stamped
// before registration) or delivered on the channel (after), never both.
func TestSubscribeWithReplay_AtomicSnapshotBoundary(t *testing.T) {
	h := NewHub()
	defer h.Close()

	for i := 0; i < 5; i++ {
		h.Broadcast(EventTournamentUpdated, map[string]int{"n": i})
	}

	ch, entries, complete, headSeq := h.subscribeWithReplay(2)
	require.NotNil(t, ch)
	assert.True(t, complete)
	assert.Equal(t, int64(5), headSeq)
	require.Len(t, entries, 3, "replay must cover (since, head]")
	assert.Equal(t, int64(3), entries[0].seq)
	assert.Equal(t, int64(5), entries[2].seq)
	assert.Empty(t, ch,
		"events stamped before registration must appear only in the replay entries, never on the channel")

	h.Broadcast(EventTournamentUpdated, map[string]string{"post": "subscribe"})
	require.Len(t, ch, 1, "an event stamped after registration must arrive on the channel")
	msg := <-ch
	assert.Equal(t, int64(6), msg.seq)
	assert.Equal(t, int64(6), decodeHubEvent(t, msg).Seq)

	// since == 0 is a fresh connect: no replay, head still reported.
	ch2, entries2, complete2, head2 := h.subscribeWithReplay(0)
	require.NotNil(t, ch2)
	assert.Nil(t, entries2)
	assert.True(t, complete2)
	assert.Equal(t, int64(6), head2)
}

func TestHub(t *testing.T) {
	h := NewHub()
	assert.NotNil(t, h)

	ch := h.Subscribe()
	assert.NotNil(t, ch)
	assert.Len(t, h.clients, 1)

	h.Broadcast(EventTournamentUpdated, map[string]string{"foo": "bar"})

	select {
	case msg := <-ch:
		event := decodeHubEvent(t, msg)
		assert.Equal(t, EventTournamentUpdated, event.Type)
		data := event.Data.(map[string]interface{})
		assert.Equal(t, "bar", data["foo"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for broadcast")
	}

	h.Unsubscribe(ch)
	assert.Len(t, h.clients, 0)
}

func TestHub_Broadcast_MarshalError(t *testing.T) {
	hub := NewHub()
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	// Channels cannot be marshaled to JSON
	hub.Broadcast(EventTournamentUpdated, make(chan int))

	select {
	case <-ch:
		t.Fatal("should not have received message")
	case <-time.After(10 * time.Millisecond):
		// OK
	}
}

type mockResponseWriter struct {
	*httptest.ResponseRecorder
	closeChan chan bool
	mu        sync.Mutex
}

func (m *mockResponseWriter) Header() http.Header {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ResponseRecorder.Header()
}

func (m *mockResponseWriter) Write(b []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ResponseRecorder.Write(b)
}

func (m *mockResponseWriter) WriteString(s string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ResponseRecorder.WriteString(s)
}

func (m *mockResponseWriter) CloseNotify() <-chan bool {
	return m.closeChan
}

func (m *mockResponseWriter) Flush() {}

func (m *mockResponseWriter) BodyString() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Body == nil {
		return ""
	}
	return m.Body.String()
}

func (m *mockResponseWriter) HeaderGet(key string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ResponseRecorder.Header().Get(key)
}

func TestHubHandleEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub()
	r := gin.New()
	r.GET("/events", h.HandleEvents())

	closeChan := make(chan bool)
	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        closeChan,
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/events", nil)

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	// Wait for subscription
	timeout := time.After(1 * time.Second)
	subscribed := false
	for !subscribed {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for subscription")
		default:
			h.mu.RLock()
			if len(h.clients) > 0 {
				subscribed = true
			}
			h.mu.RUnlock()
			if !subscribed {
				time.Sleep(10 * time.Millisecond)
			}
		}
	}

	// Broadcast an event and see if it appears in the body
	h.Broadcast(EventMatchUpdated, "test-data")

	// Wait a bit for processing
	waitForFrame(t, w, "test-data")

	// Close the connection
	cancel()
	close(closeChan)

	// Wait for handler to finish
	select {
	case <-done:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for handler to finish")
	}

	// Now assertions are safe
	assert.Contains(t, w.HeaderGet("Content-Type"), "text/event-stream")
	assert.Contains(t, w.BodyString(), "test-data")
}

// T216: ring-buffer replay on reconnect.
//
// Connect a synthetic SSE client, broadcast 5 events, capture the seqs
// the live client sees, then simulate a reconnect with
// `Last-Event-ID: 2` and assert events 3/4/5 are replayed in order
// (before any further live events). The replay path uses the SSE wire
// format `id: N\ndata: …` so the browser's auto-reconnect carries the
// right Last-Event-ID without JS work.
func TestHubReplaysOnReconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub()

	// Broadcast 5 events. Seqs will be 1..5.
	for i := 0; i < 5; i++ {
		h.Broadcast(EventMatchUpdated, map[string]int{"i": i})
	}

	// Simulate reconnect with Last-Event-ID: 2, should replay 3, 4, 5.
	r := gin.New()
	r.GET("/events", h.HandleEvents())

	closeChan := make(chan bool)
	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        closeChan,
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/events", nil)
	req.Header.Set("Last-Event-ID", "2")

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	// Wait for the handler to finish replay. We can't easily poll the
	// recorder's body while the stream is open, so close the context
	// after a brief settle period and inspect the recorded body.
	waitForFrame(t, w, "id: 5\n")
	cancel()
	close(closeChan)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not finish after context cancel")
	}

	body := w.BodyString()
	// Each replayed envelope must appear in order with its id: line.
	// We assert that the substring `id: 3` appears before `id: 4`
	// before `id: 5`, and that `id: 1` / `id: 2` do NOT appear
	// (already-seen events must be skipped).
	idx3 := strings.Index(body, "id: 3\n")
	idx4 := strings.Index(body, "id: 4\n")
	idx5 := strings.Index(body, "id: 5\n")
	require.NotEqual(t, -1, idx3, "missing replay of seq 3 in body: %q", body)
	require.NotEqual(t, -1, idx4, "missing replay of seq 4 in body: %q", body)
	require.NotEqual(t, -1, idx5, "missing replay of seq 5 in body: %q", body)
	assert.Less(t, idx3, idx4, "seq 3 should appear before seq 4")
	assert.Less(t, idx4, idx5, "seq 4 should appear before seq 5")
	assert.NotContains(t, body, "id: 1\n", "already-acked seq 1 should not be replayed")
	assert.NotContains(t, body, "id: 2\n", "already-acked seq 2 should not be replayed")
}

// The SPA reconnects by opening a new EventSource, which carries no
// Last-Event-ID header, so it sends the last id it saw as the lastEventId
// query parameter. The hub replays from it exactly as from the header, and the
// header wins when a request carries both.
func TestHubReplaysFromLastEventIDQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, url, header string
		replayed, skipped []string
	}{
		{"query alone", "/events?lastEventId=2", "", []string{"id: 3\n", "id: 4\n", "id: 5\n"}, []string{"id: 1\n", "id: 2\n"}},
		{"header wins over query", "/events?lastEventId=1", "4", []string{"id: 5\n"}, []string{"id: 2\n", "id: 3\n", "id: 4\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHub()
			for i := 0; i < 5; i++ {
				h.Broadcast(EventMatchUpdated, map[string]int{"i": i})
			}
			r := gin.New()
			r.GET("/events", h.HandleEvents())

			closeChan := make(chan bool)
			w := &mockResponseWriter{ResponseRecorder: httptest.NewRecorder(), closeChan: closeChan}
			ctx, cancel := context.WithCancel(context.Background())
			req, _ := http.NewRequestWithContext(ctx, "GET", tc.url, nil)
			if tc.header != "" {
				req.Header.Set("Last-Event-ID", tc.header)
			}
			done := make(chan struct{})
			go func() {
				r.ServeHTTP(w, req)
				close(done)
			}()
			waitForFrame(t, w, "id: 5\n")
			cancel()
			close(closeChan)
			select {
			case <-done:
			case <-time.After(1 * time.Second):
				t.Fatal("handler did not finish after context cancel")
			}

			body := w.BodyString()
			for _, frame := range tc.replayed {
				assert.Contains(t, body, frame)
			}
			for _, frame := range tc.skipped {
				assert.NotContains(t, body, frame)
			}
		})
	}
}

// T216: ring-buffer eviction. With HistorySize=3 the hub keeps only the
// last 3 envelopes. Broadcasting 5 events then reconnecting with
// Last-Event-ID=1 means event 2 has been overwritten and the gap is
// unsatisfiable, since B1 (mp-gpra), the hub emits resync_required at
// the head seq (5) instead of partially replaying surviving entries.
func TestHubRingBufferEvicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHubWithHistory(3)

	for i := 0; i < 5; i++ {
		h.Broadcast(EventMatchUpdated, map[string]int{"i": i})
	}
	headSeq := h.seq.Load() // 5

	r := gin.New()
	r.GET("/events", h.HandleEvents())
	closeChan := make(chan bool)
	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        closeChan,
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/events", nil)
	req.Header.Set("Last-Event-ID", "1")

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	waitForFrame(t, w, "resync_required")
	cancel()
	close(closeChan)
	<-done

	body := w.BodyString()
	// Since B1: unsatisfiable gaps emit resync_required at head seq, not
	// partial replay. The surviving entries (3/4) must NOT be individually
	// replayed; instead a single resync frame at seq 5 is emitted.
	assert.Contains(t, body, fmt.Sprintf("id: %d\n", headSeq), "resync frame must carry id: <headSeq>")
	assert.Contains(t, body, "resync_required", "unsatisfiable gap must emit resync_required")
	assert.NotContains(t, body, "id: 1\n")
	assert.NotContains(t, body, "id: 2\n")
	assert.NotContains(t, body, "id: 3\n", "surviving entries must not be partially replayed on unsatisfiable gap")
	assert.NotContains(t, body, "id: 4\n", "surviving entries must not be partially replayed on unsatisfiable gap")
}

// T216: snapshotHistorySinceLocked contract, the helper underlying the
// replay path (subscribeWithReplay calls it under the hub lock). Tests the
// edges directly so a regression in the ring index math fails here instead
// of in the integration test where the gin response writer obscures the
// cause.
func TestSnapshotHistorySince(t *testing.T) {
	h := NewHubWithHistory(5)
	for i := 0; i < 3; i++ {
		h.Broadcast(EventMatchUpdated, map[string]int{"i": i})
	}
	snapshot := func(since int64) ([]historyEntry, bool) {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return h.snapshotHistorySinceLocked(since)
	}

	t.Run("since beyond current returns nothing", func(t *testing.T) {
		entries, complete := snapshot(10)
		assert.Empty(t, entries)
		assert.True(t, complete)
	})

	t.Run("since equals current returns nothing", func(t *testing.T) {
		entries, complete := snapshot(3)
		assert.Empty(t, entries)
		assert.True(t, complete)
	})

	t.Run("since zero returns all retained in order", func(t *testing.T) {
		entries, complete := snapshot(0)
		require.Len(t, entries, 3)
		assert.True(t, complete)
		for i, e := range entries {
			assert.Equal(t, int64(i+1), e.seq)
		}
	})

	t.Run("eviction marks snapshot incomplete", func(t *testing.T) {
		// Push past capacity (5), broadcast 4 more → seqs 4..7, total 7.
		// Buffer holds seqs 3..7. Asking for since=1 means we wanted
		// seq 2 too, but it's been overwritten → complete=false.
		for i := 0; i < 4; i++ {
			h.Broadcast(EventMatchUpdated, map[string]int{"i": i})
		}
		entries, complete := snapshot(1)
		assert.False(t, complete, "since=1 < oldest retained should report incomplete")
		require.NotEmpty(t, entries)
		// First returned entry is the oldest retained (seq 3), not the
		// requested since+1 (seq 2).
		assert.Equal(t, int64(3), entries[0].seq)
	})
}

// T215: the SSE wire format must include `id: <seq>` lines so the
// browser's auto-reconnect populates Last-Event-ID without JS. This
// test connects, receives one event, and asserts the wire bytes carry
// the id line.
func TestHandleEventsEmitsIDLine(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub()

	r := gin.New()
	r.GET("/events", h.HandleEvents())

	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        make(chan bool),
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/events", nil)

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	// Wait for subscription
	timeout := time.After(1 * time.Second)
	for {
		h.mu.RLock()
		ready := len(h.clients) > 0
		h.mu.RUnlock()
		if ready {
			break
		}
		select {
		case <-timeout:
			t.Fatal("timeout waiting for subscription")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	h.Broadcast(EventMatchUpdated, map[string]string{"foo": "bar"})
	waitForFrame(t, w, "id: 1\n")
	cancel()
	close(w.closeChan)
	<-done

	body := w.BodyString()
	assert.Contains(t, body, fmt.Sprintf("id: %d\n", 1), "stream must emit an id: line with the seq")
	assert.Contains(t, body, "\"seq\":1", "envelope JSON must carry the seq field")
}

func TestHub_HandleEvents_Closure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub()

	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        make(chan bool),
	}
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/events", nil)

	// Start HandleEvents in a goroutine
	done := make(chan bool)
	go func() {
		h.HandleEvents()(c)
		done <- true
	}()

	// Wait for subscription to happen
	timeout := time.After(1 * time.Second)
	var internalCh chan historyEntry
	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for subscription")
		default:
			h.mu.Lock()
			for ch := range h.clients {
				internalCh = ch
				break
			}
			h.mu.Unlock()
			if internalCh != nil {
				goto Subscribed
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

Subscribed:
	h.Unsubscribe(internalCh)

	select {
	case <-done:
		// Success: HandleEvents exited
	case <-time.After(200 * time.Millisecond):
		t.Fatal("HandleEvents did not exit after channel closure")
	}
}

// B1: resync_required is emitted when replay is unsatisfiable due to ring
// buffer eviction (client's Last-Event-ID is older than oldest retained).
// The frame must carry id: <headSeq> and a JSON payload with type "resync_required".
// Partial entries must NOT be replayed.
func TestHandleEvents_ResyncOnEviction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Ring holds only 3 entries; broadcast 5 → oldest retained is seq 3.
	h := NewHubWithHistory(3)
	for i := 0; i < 5; i++ {
		h.Broadcast(EventMatchUpdated, map[string]int{"i": i})
	}
	headSeq := h.seq.Load() // 5

	r := gin.New()
	r.GET("/events", h.HandleEvents())
	closeChan := make(chan bool)
	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        closeChan,
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/events", nil)
	// Client last saw seq 1, which has been evicted → replay is unsatisfiable.
	req.Header.Set("Last-Event-ID", "1")

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	waitForFrame(t, w, "resync_required")
	cancel()
	close(closeChan)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not finish")
	}

	body := w.BodyString()

	// Must contain exactly one resync frame at the head seq.
	expectedIDLine := fmt.Sprintf("id: %d\n", headSeq)
	require.Contains(t, body, expectedIDLine, "resync frame must carry id: <headSeq>")

	// Extract and parse the data line from the resync frame.
	// Wire: "id: 5\nevent: message\ndata: {...}\n\n"
	var resyncPayload struct {
		Type string `json:"type"`
		Seq  int64  `json:"seq"`
	}
	// Find the data line after the expected id line.
	idIdx := strings.Index(body, expectedIDLine)
	require.NotEqual(t, -1, idIdx, "resync id line not found")
	after := body[idIdx:]
	dataPrefix := "data: "
	dataIdx := strings.Index(after, dataPrefix)
	require.NotEqual(t, -1, dataIdx, "resync data line not found")
	dataLine := after[dataIdx+len(dataPrefix):]
	endIdx := strings.Index(dataLine, "\n")
	require.NotEqual(t, -1, endIdx)
	dataLine = dataLine[:endIdx]
	err := json.Unmarshal([]byte(dataLine), &resyncPayload)
	require.NoError(t, err, "resync payload must be valid JSON")
	assert.Equal(t, "resync_required", resyncPayload.Type)
	assert.Equal(t, headSeq, resyncPayload.Seq)

	// Partial entries must NOT be replayed, only the resync frame for headSeq.
	// Seqs 3/4 are retained but must not appear before the resync frame.
	assert.NotContains(t, body, "id: 3\n", "partial entries must not be replayed on unsatisfiable gap")
	assert.NotContains(t, body, "id: 4\n", "partial entries must not be replayed on unsatisfiable gap")
}

// B1 restart case: client's Last-Event-ID is greater than the server's
// current head seq (server restarted, seq counter reset). Must emit
// resync_required rather than replaying nothing silently.
func TestHandleEvents_ResyncOnServerRestart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Fresh hub, head seq is 0. Broadcast one event so head is 1.
	h := NewHub()
	h.Broadcast(EventMatchUpdated, map[string]int{"i": 0})
	headSeq := h.seq.Load() // 1

	r := gin.New()
	r.GET("/events", h.HandleEvents())
	closeChan := make(chan bool)
	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        closeChan,
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/events", nil)
	// Client saw seq 9999 before the server restarted, far ahead of head.
	req.Header.Set("Last-Event-ID", "9999")

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	waitForFrame(t, w, "resync_required")
	cancel()
	close(closeChan)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not finish")
	}

	body := w.BodyString()
	expectedIDLine := fmt.Sprintf("id: %d\n", headSeq)
	require.Contains(t, body, expectedIDLine, "resync frame must carry id: <headSeq>")

	var resyncPayload struct {
		Type string `json:"type"`
	}
	idIdx := strings.Index(body, expectedIDLine)
	after := body[idIdx:]
	dataPrefix := "data: "
	dataIdx := strings.Index(after, dataPrefix)
	require.NotEqual(t, -1, dataIdx, "resync data line not found")
	dataLine := after[dataIdx+len(dataPrefix):]
	endIdx := strings.Index(dataLine, "\n")
	require.NotEqual(t, -1, endIdx)
	dataLine = dataLine[:endIdx]
	err := json.Unmarshal([]byte(dataLine), &resyncPayload)
	require.NoError(t, err)
	assert.Equal(t, "resync_required", resyncPayload.Type)
}

// B1 satisfiable case: a recent Last-Event-ID still replays the buffered
// entries (existing behavior preserved, not replaced by resync_required).
func TestHandleEvents_SatisfiableReplayUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub()
	for i := 0; i < 5; i++ {
		h.Broadcast(EventMatchUpdated, map[string]int{"i": i})
	}

	r := gin.New()
	r.GET("/events", h.HandleEvents())
	closeChan := make(chan bool)
	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        closeChan,
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/events", nil)
	// Last-Event-ID=2 is within the ring (ring holds 200 by default) → satisfiable.
	req.Header.Set("Last-Event-ID", "2")

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	waitForFrame(t, w, "id: 5\n")
	cancel()
	close(closeChan)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not finish")
	}

	body := w.BodyString()
	// Seqs 3/4/5 must be replayed; 1/2 must not; no resync_required.
	assert.Contains(t, body, "id: 3\n", "seq 3 must be replayed")
	assert.Contains(t, body, "id: 4\n", "seq 4 must be replayed")
	assert.Contains(t, body, "id: 5\n", "seq 5 must be replayed")
	assert.NotContains(t, body, "id: 1\n", "seq 1 already acked must not replay")
	assert.NotContains(t, body, "id: 2\n", "seq 2 already acked must not replay")
	assert.NotContains(t, body, "resync_required", "satisfiable replay must not emit resync_required")
}

// B2: the heartbeat frame must be a real data line with no id: line, so the
// browser's Last-Event-ID is unchanged and the frame fires onmessage. This
// drives HandleEvents end-to-end with a short HeartbeatInterval and asserts the
// bytes it actually emits, so it fails if the handler stops sending heartbeats
// or changes the wire format.
func TestHandleEvents_HeartbeatFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub()
	h.HeartbeatInterval = 20 * time.Millisecond // fast heartbeat for the test

	r := gin.New()
	r.GET("/events", h.HandleEvents())
	closeChan := make(chan bool)
	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        closeChan,
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/events", nil)

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	// Wait deterministically for the first heartbeat frame rather than a fixed
	// sleep (which is flaky on slow/contended CI). BodyString() is concurrency-safe.
	require.Eventually(t, func() bool {
		return strings.Contains(w.BodyString(), "data: {\"type\":\"heartbeat\"")
	}, 2*time.Second, 5*time.Millisecond, "HandleEvents must emit a heartbeat data frame")
	cancel()
	close(closeChan)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not finish")
	}

	body := w.BodyString()
	require.Contains(t, body, "data: {\"type\":\"heartbeat\"", "HandleEvents must emit a heartbeat data frame")
	// No events were broadcast, so the only frames are heartbeats, none may
	// carry an id: line (which would perturb the browser's Last-Event-ID).
	assert.NotContains(t, body, "id:", "heartbeat frames must not contain an id: line")

	// The emitted payload parses to {"type":"heartbeat","nowMs":<unix ms>}.
	dataLine := body[strings.Index(body, "data: ")+len("data: "):]
	dataLine = dataLine[:strings.Index(dataLine, "\n")]
	var payload struct {
		Type  string `json:"type"`
		NowMs *int64 `json:"nowMs"`
	}
	require.NoError(t, json.Unmarshal([]byte(dataLine), &payload), "heartbeat data must be valid JSON")
	assert.Equal(t, "heartbeat", payload.Type)

	// bc-cse: the frame carries the server clock so a client can notice its own
	// clock drifting (a tripwire only, never adopted as an offset: a one-way
	// push has no round trip to correct against). Pointer so an omitted field is
	// distinguishable from a zero one.
	require.NotNil(t, payload.NowMs, "the heartbeat must carry the server time")
	assert.InDelta(t, time.Now().UnixMilli(), *payload.NowMs, float64(60*time.Second/time.Millisecond),
		"nowMs must be a plausible unix-ms server clock reading, not seconds or a zero value")
}

// mp-gpra: when the server has broadcast nothing yet (head seq 0) and a client
// reconnects with a stale positive Last-Event-ID (restart with no new activity),
// the hub MUST still emit resync_required, otherwise a client that keeps an
// in-memory lastSeq across reconnects drops the first post-restart event (seq=1)
// as a stale duplicate and goes silently stale. The frame must carry NO id: line
// so the browser's Last-Event-ID isn't forced to "0".
func TestHandleEvents_ResyncOnRestartWithNoEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub() // head seq is 0, no Broadcast.

	r := gin.New()
	r.GET("/events", h.HandleEvents())
	closeChan := make(chan bool)
	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        closeChan,
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", "/events", nil)
	req.Header.Set("Last-Event-ID", "9999") // stale id from before a restart

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	waitForFrame(t, w, "resync_required")
	cancel()
	close(closeChan)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not finish")
	}

	body := w.BodyString()
	assert.Contains(t, body, "resync_required", "head-seq-0 restart must still emit resync_required")
	assert.Contains(t, body, `"type":"resync_required"`, "resync payload must be present")
	// No id: line at all: head seq is 0 (no replay) and the resync is emitted
	// without an id so the browser's Last-Event-ID is not forced to "0".
	assert.NotContains(t, body, "id:", "head-seq-0 resync must NOT carry an id: line")
}

// waitForFrame waits until the recorded SSE body contains want, then returns. A
// deterministic replacement for a fixed sleep before cancelling an SSE request:
// BodyString() is concurrency-safe and the initial replay/resync/heartbeat bytes
// are written synchronously on connect, so this avoids CI flakiness under load.
func waitForFrame(t *testing.T, w *mockResponseWriter, want string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return strings.Contains(w.BodyString(), want)
	}, 2*time.Second, 5*time.Millisecond, "expected SSE frame %q not observed", want)
}

// --- Per-subscriber streams (bc-hnam) ----------------------------------------

// TestSSEEventCatalogue_ClassifiesEveryEventType holds the catalogue to every
// EventType constant declared in the package: a new event type must be
// classified (masked, passes or organiser-only) before it can reach a stream
// as broadcast.
func TestSSEEventCatalogue_ClassifiesEveryEventType(t *testing.T) {
	fset := token.NewFileSet()
	dir, err := os.ReadDir(".")
	require.NoError(t, err)
	declared := map[EventType]bool{}
	for _, de := range dir {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".go") || strings.HasSuffix(de.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, de.Name(), nil, 0)
		require.NoError(t, err)
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "EventType" {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					require.True(t, ok, "an EventType constant is a string literal")
					declared[EventType(strings.Trim(lit.Value, `"`))] = true
				}
			}
		}
	}
	require.Len(t, declared, 15, "the package declares 15 event types")
	for et := range declared {
		spec, ok := sseEventCatalogue[et]
		if assert.True(t, ok, "event type %q is not classified in sseEventCatalogue", et) {
			assert.NotZero(t, spec.class, et)
			if spec.class == sseEventMasked {
				assert.NotNil(t, spec.mask, "masked event %q has a mask", et)
			}
		}
	}
	assert.Len(t, sseEventCatalogue, len(declared), "the catalogue names only declared event types")
}

// hubWithNames is a hub with the fixture's judge and masks attached.
func hubWithNames(f pnFixture) *Hub {
	h := NewHub()
	h.attachPublicNames(f.pn)
	return h
}

// lastEntry is the newest event in the hub's history.
func lastEntry(t *testing.T, h *Hub) *historyEntry {
	t.Helper()
	h.mu.RLock()
	defer h.mu.RUnlock()
	seq := h.seq.Load()
	require.Positive(t, seq)
	return &h.history[(seq-1)%int64(h.HistorySize)]
}

func juniorsResult() *state.MatchResult {
	return &state.MatchResult{
		ID: "Pool A-0", SideA: "Alice Arai", SideAID: pnAlice, SideB: "Bob Baba", SideBID: pnBob,
		Winner: "Alice Arai", WinnerID: pnAlice, Status: state.MatchStatusCompleted,
	}
}

func broadcastJuniorsResult(h *Hub) {
	h.Broadcast(EventMatchUpdated, gin.H{"competitionId": "juniors", "matchId": "Pool A-0", "result": matchPtrForBroadcast(juniorsResult())})
}

// warmMask builds the fixture's mask twice: the first build may run the
// store's load-time repair, which rewrites the files it was built from.
func warmMask(t *testing.T, f pnFixture) {
	t.Helper()
	for range 2 {
		_, err := f.pn.view(f.comp, audience{})
		require.NoError(t, err)
	}
}

func TestHubProject_SelfRunStreamKeepsNames(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	warmMask(t, f)
	h := hubWithNames(f)
	tour, err := f.store.LoadTournament()
	require.NoError(t, err)
	tour.Mode = state.TournamentModeSelfRun
	require.NoError(t, f.store.SaveTournament(tour))

	broadcastJuniorsResult(h)
	e := lastEntry(t, h)
	assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}), "a self-run stream carries the names")
}

func TestHubProject_MasksMatchUpdatedForThePublic(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	warmMask(t, f)
	h := hubWithNames(f)
	broadcastJuniorsResult(h)
	e := lastEntry(t, h)
	assert.Equal(t, "juniors", e.compID, "a masked event keeps the competition it names")
	assert.Equal(t, EventMatchUpdated, e.typ)

	public := h.project(e, &sseSubscriber{})
	assert.NotContains(t, public, "Alice Arai")
	assert.NotContains(t, public, "Bob Baba")
	assert.Contains(t, public, `"sideA":"K1"`)
	assert.Contains(t, public, `"sideB":"K2"`)
	assert.Contains(t, public, `"namesHidden":true`)
	assert.Contains(t, public, `"seq":`+fmt.Sprint(e.seq))
	var env SSEEvent
	require.NoError(t, json.Unmarshal([]byte(public), &env), "a masked copy is a well-formed envelope")
	assert.Equal(t, EventMatchUpdated, env.Type)

	require.NotNil(t, e.masked)
	assert.True(t, e.masked.done, "the public copy is built once and shared")
	assert.Equal(t, public, h.project(e, &sseSubscriber{}))

	org := h.project(e, &sseSubscriber{aud: organiserAudience()})
	assert.Equal(t, e.payload, org, "the organiser is sent the event as broadcast")
	assert.Contains(t, org, "Alice Arai")

	// The option is judged when the event is sent: shown again, the same
	// stored event goes to the public as broadcast.
	f.comp.PublicNames = state.PublicNamesShown
	require.NoError(t, f.store.SaveCompetition(f.comp))
	assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}))
}

func TestHubProject_FailsClosed(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	h := hubWithNames(f)

	// A masked event that names no competition.
	h.Broadcast(EventMatchUpdated, gin.H{"result": matchPtrForBroadcast(juniorsResult())})
	e := lastEntry(t, h)
	out := h.project(e, &sseSubscriber{})
	assert.NotContains(t, out, "Alice Arai")
	assert.Contains(t, out, `"data":null`)
	assert.Contains(t, out, `"type":"match_updated"`, "its type and seq still arrive, so no gap is seen")

	// A competition that does not exist.
	h.Broadcast(EventMatchUpdated, gin.H{"competitionId": "ghost", "result": matchPtrForBroadcast(juniorsResult())})
	assert.Contains(t, h.project(lastEntry(t, h), &sseSubscriber{}), `"data":null`)

	// An event type nobody classified.
	h.Broadcast(EventType("brand_new_event"), gin.H{"competitionId": "juniors", "name": "Alice Arai"})
	e = lastEntry(t, h)
	assert.Contains(t, h.project(e, &sseSubscriber{}), `"data":null`)
	assert.Contains(t, h.project(e, &sseSubscriber{aud: organiserAudience()}), "Alice Arai")

	// An event that passes is sent as broadcast.
	h.Broadcast(EventAnnouncement, []string{"Court A next"})
	e = lastEntry(t, h)
	assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}))

	// A bare hub (no judge attached) sends everything as broadcast.
	bare := NewHub()
	broadcastJuniorsResult(bare)
	be := lastEntry(t, bare)
	assert.Equal(t, be.payload, bare.project(be, &sseSubscriber{}))
}

func TestHubProject_RevealJudgedOnEveryEvent(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesCompetition)
	warmMask(t, f)
	h := hubWithNames(f)
	broadcastJuniorsResult(h)
	e := lastEntry(t, h)

	link := &sseSubscriber{aud: audience{competitions: map[string]bool{"juniors": true}}}
	assert.Equal(t, e.payload, h.project(e, link), "the competition link's stream sees the names")

	// Past the links' expiry (an hour after the tournament's last day) the
	// same stream sees numbers.
	f.pn.now = func() time.Time { return f.now.Add(72 * time.Hour) }
	assert.NotContains(t, h.project(e, link), "Alice Arai")
	f.pn.now = func() time.Time { return f.now }

	// Moved to per-person links: a competition link reveals nothing.
	f.comp.PublicNames = state.PublicNamesPerson
	require.NoError(t, f.store.SaveCompetition(f.comp))
	assert.NotContains(t, h.project(e, link), "Alice Arai")

	// A competitor's own link reveals that competitor alone.
	own := &sseSubscriber{aud: audience{competitors: map[string]map[string]bool{"juniors": {pnAlice: true}}}}
	out := h.project(e, own)
	assert.Contains(t, out, "Alice Arai")
	assert.NotContains(t, out, "Bob Baba")
	assert.Contains(t, out, `"namesHidden":true`)
	assert.NotContains(t, h.project(e, &sseSubscriber{}), "Alice Arai", "the shared public copy is not the overlay")
}

// ticketRouter mounts the stream and its ticket on a router whose hub has
// the fixture's judge.
func ticketRouter(f pnFixture) (*gin.Engine, *Hub) {
	gin.SetMode(gin.TestMode)
	h := hubWithNames(f)
	r := gin.New()
	r.GET("/api/events", h.HandleEvents())
	r.POST("/api/events/ticket", h.HandleEventTicket())
	return r, h
}

func askTicket(t *testing.T, r *gin.Engine, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/events/ticket", nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestEventTicket_JudgedOnceAndSpent(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	r, h := ticketRouter(f)

	w := askTicket(t, r, map[string]string{"X-Tournament-Password": "wrong"})
	assert.Equal(t, http.StatusUnauthorized, w.Code, "a password sent but wrong gets no ticket")

	w = askTicket(t, r, map[string]string{"X-Tournament-Password": "secret-pw"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var body struct {
		Ticket    string `json:"ticket"`
		ExpiresAt int64  `json:"expiresAt"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Ticket, 2*eventTicketBytes)
	assert.Positive(t, body.ExpiresAt)

	sub, ok := h.redeemTicket(body.Ticket)
	require.True(t, ok)
	assert.True(t, sub.aud.isOrganiser(), "the ticket carries the audience it was judged for")
	_, ok = h.redeemTicket(body.Ticket)
	assert.False(t, ok, "a ticket is used once")
	_, ok = h.redeemTicket("not-a-ticket")
	assert.False(t, ok)

	// Expired before it is used.
	now := time.Now()
	h.tickets.now = func() time.Time { return now }
	tk, _, err := h.issueTicket(sseSubscriber{})
	require.NoError(t, err)
	h.tickets.now = func() time.Time { return now.Add(eventTicketTTL) }
	_, ok = h.redeemTicket(tk)
	assert.False(t, ok, "a ticket past its time is refused")

	// A reveal header is kept for the stream to be judged again.
	key := "juniors:" + strings.Repeat("ab", 32)
	w = askTicket(t, r, map[string]string{revealKeyHeader: key})
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	sub, ok = h.redeemTicket(body.Ticket)
	require.True(t, ok)
	assert.Equal(t, key, sub.reveal)
	assert.Equal(t, audiencePublic, sub.aud.key(), "a hidden competition's key reveals nothing")
}

func TestEventTicket_BoundedAndBareHub(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	h := hubWithNames(f)
	now := time.Now()
	h.tickets.now = func() time.Time { return now }
	h.tickets.m = map[string]eventTicket{}
	for i := range maxEventTickets {
		h.tickets.m[fmt.Sprint(i)] = eventTicket{expires: now.Add(time.Second)}
	}
	_, _, err := h.issueTicket(sseSubscriber{})
	assert.ErrorIs(t, err, errTooManyEventTickets, "no new ticket while every one waiting is live")
	h.tickets.now = func() time.Time { return now.Add(2 * time.Second) }
	_, _, err = h.issueTicket(sseSubscriber{})
	assert.NoError(t, err, "expired tickets make room")
	assert.Len(t, h.tickets.m, 1)

	gin.SetMode(gin.TestMode)
	bare := NewHub()
	r := gin.New()
	r.POST("/api/events/ticket", bare.HandleEventTicket())
	w := askTicket(t, r, nil)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleEvents_RefusesAnUnusableTicket(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	r, h := ticketRouter(f)
	tk, _, err := h.issueTicket(sseSubscriber{aud: organiserAudience()})
	require.NoError(t, err)
	_, ok := h.redeemTicket(tk)
	require.True(t, ok)

	for name, query := range map[string]string{"spent": tk, "unknown": "nope", "empty": ""} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/events?ticket="+query, nil))
		assert.Equal(t, http.StatusUnauthorized, w.Code, name)
		assert.Contains(t, w.Body.String(), codeEventTicketInvalid, name)
		assert.NotContains(t, w.Body.String(), ": open", "a %s ticket opens no stream, not even a public one", name)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	assert.Empty(t, h.clients)
}

func TestHandleEvents_StreamIsProjectedForItsTicket(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	warmMask(t, f)
	r, h := ticketRouter(f)
	broadcastJuniorsResult(h)

	open := func(query string) (*mockResponseWriter, context.CancelFunc, chan struct{}) {
		ctx, cancel := context.WithCancel(context.Background())
		w := &mockResponseWriter{ResponseRecorder: httptest.NewRecorder(), closeChan: make(chan bool)}
		req := httptest.NewRequest(http.MethodGet, "/api/events"+query, nil).WithContext(ctx)
		done := make(chan struct{})
		go func() {
			r.ServeHTTP(w, req)
			close(done)
		}()
		return w, cancel, done
	}
	tk, _, err := h.issueTicket(sseSubscriber{aud: organiserAudience()})
	require.NoError(t, err)
	orgW, orgCancel, orgDone := open("?lastEventId=0&ticket=" + tk)
	pubW, pubCancel, pubDone := open("")
	require.Eventually(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients) == 2
	}, 2*time.Second, 5*time.Millisecond)

	broadcastJuniorsResult(h)
	waitForFrame(t, orgW, "Alice Arai")
	waitForFrame(t, pubW, `"sideA":"K1"`)
	orgCancel()
	pubCancel()
	<-orgDone
	<-pubDone
	assert.NotContains(t, pubW.BodyString(), "Alice Arai")
	assert.Equal(t, "no-store", pubW.HeaderGet("Cache-Control"))
}

func TestRejudgeRevealStreams_EndsOnlyChangedStreams(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesCompetition)
	warmMask(t, f)
	h := hubWithNames(f)
	header := "juniors:" + state.CompetitionRevealKey(f.comp)
	judged := f.pn.revealAudience(header)
	require.True(t, judged.revealsAll("juniors"))

	link, _, _, _ := h.subscribeWithReplayFor(0, &sseSubscriber{aud: judged, reveal: header})
	public, _, _, _ := h.subscribeWithReplayFor(0, &sseSubscriber{})
	require.NotNil(t, link)
	require.NotNil(t, public)

	// Nothing changed: a tournament_updated leaves the reveal stream open.
	h.Broadcast(EventTournamentUpdated, nil)
	<-link
	h.rejudges.Wait()
	h.mu.RLock()
	_, stillOpen := h.clients[link]
	h.mu.RUnlock()
	assert.True(t, stillOpen, "a reveal stream whose keys unlock the same stays open")

	// The option moves to hidden: the link now unlocks nothing, so its
	// stream ends after the event that told it.
	f.comp.PublicNames = state.PublicNamesHidden
	require.NoError(t, f.store.SaveCompetition(f.comp))
	h.Broadcast(EventTournamentUpdated, nil)
	h.rejudges.Wait()
	msg, ok := <-link
	require.True(t, ok, "the event is delivered before the stream ends")
	assert.Equal(t, EventTournamentUpdated, msg.typ)
	_, ok = <-link
	assert.False(t, ok, "the reveal stream ended so it asks for a new ticket")

	h.mu.RLock()
	_, publicOpen := h.clients[public]
	h.mu.RUnlock()
	assert.True(t, publicOpen, "a public stream is never ended for an option change")
	h.Close()
}

// A caller with no credential is the public, which opens the stream with no
// ticket, so it is handed none and cannot fill the tickets waiting.
func TestEventTicket_RefusesACallerWithNoCredential(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	r, h := ticketRouter(f)

	w := askTicket(t, r, nil)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), codeEventTicketNotNeeded)
	assert.Empty(t, h.tickets.m, "no ticket was kept for the public")

	w = askTicket(t, r, map[string]string{"X-Tournament-Password": "secret-pw"})
	assert.Equal(t, http.StatusOK, w.Code, "the organiser still gets one")
	w = askTicket(t, r, map[string]string{revealKeyHeader: "juniors:" + strings.Repeat("ab", 32)})
	assert.Equal(t, http.StatusOK, w.Code, "so does a caller holding a reveal key")
	assert.Len(t, h.tickets.m, 2)
}

// A header with no well-formed reveal key is no credential: it earns no
// ticket, and a ticket keeps only the well-formed entries of the header.
func TestEventTicket_RefusesAMalformedRevealKey(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	r, h := ticketRouter(f)

	for _, junk := range []string{"x", "juniors:short", "bad id:" + strings.Repeat("0", 64), ",,"} {
		w := askTicket(t, r, map[string]string{revealKeyHeader: junk})
		assert.Equal(t, http.StatusBadRequest, w.Code, junk)
		assert.Contains(t, w.Body.String(), codeEventTicketNotNeeded, junk)
	}
	assert.Empty(t, h.tickets.m, "a malformed header filled no ticket")

	key := "juniors:" + strings.Repeat("ab", 32)
	w := askTicket(t, r, map[string]string{revealKeyHeader: "junk, " + key + ", " + key})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Ticket string `json:"ticket"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	sub, ok := h.redeemTicket(body.Ticket)
	require.True(t, ok)
	assert.Equal(t, key, sub.reveal, "only the well-formed entry is kept, once")
}

// Callers holding only a reveal key fill the waiting tickets no further than
// maxEventTickets less the organiser's reserve, so the organiser's stream can
// still open.
func TestEventTicket_KeepsRoomForTheOrganiser(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	h := hubWithNames(f)
	now := time.Now()
	h.tickets.now = func() time.Time { return now }
	h.tickets.m = map[string]eventTicket{}
	for i := range maxEventTickets - organiserTicketReserve {
		h.tickets.m[fmt.Sprint(i)] = eventTicket{expires: now.Add(time.Second)}
	}
	_, _, err := h.issueTicket(sseSubscriber{reveal: "juniors:" + strings.Repeat("0", 64)})
	assert.ErrorIs(t, err, errTooManyEventTickets, "a reveal-key caller leaves the reserve alone")
	_, _, err = h.issueTicket(sseSubscriber{aud: organiserAudience()})
	assert.NoError(t, err, "the organiser still gets a ticket")
}

// A stream is judged once, when it opens, so a changed admin password ends
// the organiser's streams (after the event that tells them) and leaves the
// public's open.
func TestBroadcast_PasswordResetEndsOrganiserStreams(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	h := hubWithNames(f)
	organiser, _, _, _ := h.subscribeWithReplayFor(0, &sseSubscriber{aud: organiserAudience()})
	public, _, _, _ := h.subscribeWithReplayFor(0, &sseSubscriber{})
	require.NotNil(t, organiser)
	require.NotNil(t, public)

	h.Broadcast(EventPasswordReset, passwordResetEventData{})
	msg, ok := <-organiser
	require.True(t, ok, "the event is delivered before the stream ends")
	assert.Equal(t, EventPasswordReset, msg.typ)
	_, ok = <-organiser
	assert.False(t, ok, "the organiser's stream ended so it asks for a new ticket")

	h.mu.RLock()
	_, publicOpen := h.clients[public]
	h.mu.RUnlock()
	assert.True(t, publicOpen, "a public stream is never ended for a password change")
	h.Close()
}

// A masked copy built before the competition was written is built again after
// it, while the names are still hidden: a changed number reads as the new one.
func TestHubProject_PublicCopyFollowsAWrittenCompetition(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	warmMask(t, f)
	h := hubWithNames(f)
	broadcastJuniorsResult(h)
	e := lastEntry(t, h)
	assert.Contains(t, h.project(e, &sseSubscriber{}), `"sideA":"K1"`)

	pools, err := f.store.LoadPools(f.comp.ID)
	require.NoError(t, err)
	for i := range pools[0].Players {
		pools[0].Players[i].Number = "Z" + string(rune('1'+i))
	}
	require.NoError(t, f.store.SavePools(f.comp.ID, pools))
	warmMask(t, f)

	public := h.project(e, &sseSubscriber{})
	assert.Contains(t, public, `"sideA":"Z1"`, "the copy is built again once the competition was written")
	assert.NotContains(t, public, "Alice Arai")
}

// The competition a masked event names is read and judged once for every
// stream, and again once the competition has been written.
func TestHubProject_JudgesTheCompetitionOncePerWrite(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	warmMask(t, f)
	h := hubWithNames(f)
	broadcastJuniorsResult(h)
	e := lastEntry(t, h)
	require.Nil(t, e.masked.judged)

	h.project(e, &sseSubscriber{})
	first := e.masked.judged
	require.NotNil(t, first)
	assert.True(t, first.comp.HidesNames(), "the kept competition hides its names")
	h.project(e, &sseSubscriber{aud: audience{competitors: map[string]map[string]bool{"juniors": {pnAlice: true}}}})
	assert.Same(t, first, e.masked.judged, "a second stream reuses the judgement")

	f.comp.PublicNames = state.PublicNamesShown
	require.NoError(t, f.store.SaveCompetition(f.comp))
	h.project(e, &sseSubscriber{})
	assert.NotSame(t, first, e.masked.judged, "a written competition is judged again")
	assert.False(t, e.masked.judged.comp.HidesNames())
}

// A tournament saved into another mode while a competition is kept in place
// (a restore does this) does not move the competition's write token, so the
// judgement kept for a replayed event must not carry the mode with it: an
// event judged while the tournament was self-run, and so sent with names,
// is masked again once the tournament is officiated.
func TestHubProject_ReplayedEventFollowsATournamentModeChange(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	warmMask(t, f)
	h := hubWithNames(f)
	tour, err := f.store.LoadTournament()
	require.NoError(t, err)
	tour.Mode = state.TournamentModeSelfRun
	require.NoError(t, f.store.SaveTournament(tour))

	broadcastJuniorsResult(h)
	e := lastEntry(t, h)
	assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}), "self-run: the names are shown")
	require.NotNil(t, e.masked.judged)
	token := f.store.CompetitionWriteToken(f.comp.ID)
	require.Equal(t, token, e.masked.judged.token)

	tour.Mode = state.TournamentModeOfficiated
	require.NoError(t, f.store.SaveTournament(tour))
	require.Equal(t, token, f.store.CompetitionWriteToken(f.comp.ID), "the competition was not written")

	public := h.project(e, &sseSubscriber{})
	assert.NotContains(t, public, "Alice Arai", "officiated again: the replayed event is masked")
}
