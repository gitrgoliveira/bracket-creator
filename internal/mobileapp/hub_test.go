package mobileapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHub(t *testing.T) {
	h := NewHub()
	assert.NotNil(t, h)

	ch := h.Subscribe()
	assert.NotNil(t, ch)
	assert.Len(t, h.clients, 1)

	h.Broadcast(EventTournamentUpdated, map[string]string{"foo": "bar"})

	select {
	case msg := <-ch:
		var event SSEEvent
		err := json.Unmarshal([]byte(msg), &event)
		assert.NoError(t, err)
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
	time.Sleep(50 * time.Millisecond)

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

	// Simulate reconnect with Last-Event-ID: 2 — should replay 3, 4, 5.
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
	time.Sleep(50 * time.Millisecond)
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

// T216: ring-buffer eviction. With HistorySize=3 the hub keeps only the
// last 3 envelopes. Broadcasting 5 events then reconnecting with
// Last-Event-ID=1 means event 2 has been overwritten and the gap is
// unsatisfiable — since B1 (mp-gpra), the hub emits resync_required at
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

	time.Sleep(50 * time.Millisecond)
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

// T216: snapshotHistorySince contract — the helper underlying the
// replay path. Tests the edges directly so a regression in the ring
// index math fails here instead of in the integration test where the
// gin response writer obscures the cause.
func TestSnapshotHistorySince(t *testing.T) {
	h := NewHubWithHistory(5)
	for i := 0; i < 3; i++ {
		h.Broadcast(EventMatchUpdated, map[string]int{"i": i})
	}

	t.Run("since beyond current returns nothing", func(t *testing.T) {
		entries, complete := h.snapshotHistorySince(10)
		assert.Empty(t, entries)
		assert.True(t, complete)
	})

	t.Run("since equals current returns nothing", func(t *testing.T) {
		entries, complete := h.snapshotHistorySince(3)
		assert.Empty(t, entries)
		assert.True(t, complete)
	})

	t.Run("since zero returns all retained in order", func(t *testing.T) {
		entries, complete := h.snapshotHistorySince(0)
		require.Len(t, entries, 3)
		assert.True(t, complete)
		for i, e := range entries {
			assert.Equal(t, int64(i+1), e.seq)
		}
	})

	t.Run("eviction marks snapshot incomplete", func(t *testing.T) {
		// Push past capacity (5) — broadcast 4 more → seqs 4..7, total 7.
		// Buffer holds seqs 3..7. Asking for since=1 means we wanted
		// seq 2 too, but it's been overwritten → complete=false.
		for i := 0; i < 4; i++ {
			h.Broadcast(EventMatchUpdated, map[string]int{"i": i})
		}
		entries, complete := h.snapshotHistorySince(1)
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
	time.Sleep(50 * time.Millisecond)
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
	var internalCh chan string
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

	time.Sleep(50 * time.Millisecond)
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

	// Partial entries must NOT be replayed — only the resync frame for headSeq.
	// Seqs 3/4 are retained but must not appear before the resync frame.
	assert.NotContains(t, body, "id: 3\n", "partial entries must not be replayed on unsatisfiable gap")
	assert.NotContains(t, body, "id: 4\n", "partial entries must not be replayed on unsatisfiable gap")
}

// B1 restart case: client's Last-Event-ID is greater than the server's
// current head seq (server restarted, seq counter reset). Must emit
// resync_required rather than replaying nothing silently.
func TestHandleEvents_ResyncOnServerRestart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Fresh hub — head seq is 0. Broadcast one event so head is 1.
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
	// Client saw seq 9999 before the server restarted — far ahead of head.
	req.Header.Set("Last-Event-ID", "9999")

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
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
	// Last-Event-ID=2 is within the ring (ring holds 100 by default) → satisfiable.
	req.Header.Set("Last-Event-ID", "2")

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
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
// bytes it actually emits — so it fails if the handler stops sending heartbeats
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

	// Let several heartbeat ticks fire through the real handler.
	time.Sleep(80 * time.Millisecond)
	cancel()
	close(closeChan)
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("handler did not finish")
	}

	body := w.BodyString()
	require.Contains(t, body, "data: {\"type\":\"heartbeat\"}\n\n", "HandleEvents must emit a heartbeat data frame")
	// No events were broadcast, so the only frames are heartbeats — none may
	// carry an id: line (which would perturb the browser's Last-Event-ID).
	assert.NotContains(t, body, "id:", "heartbeat frames must not contain an id: line")

	// The emitted payload parses to {"type":"heartbeat"}.
	dataLine := body[strings.Index(body, "data: ")+len("data: "):]
	dataLine = dataLine[:strings.Index(dataLine, "\n")]
	var payload struct {
		Type string `json:"type"`
	}
	require.NoError(t, json.Unmarshal([]byte(dataLine), &payload), "heartbeat data must be valid JSON")
	assert.Equal(t, "heartbeat", payload.Type)
}

// mp-gpra: when the server has broadcast nothing yet (head seq 0) and a client
// reconnects with a stale positive Last-Event-ID (restart with no new activity),
// the hub MUST still emit resync_required — otherwise a client that keeps an
// in-memory lastSeq across reconnects drops the first post-restart event (seq=1)
// as a stale duplicate and goes silently stale. The frame must carry NO id: line
// so the browser's Last-Event-ID isn't forced to "0".
func TestHandleEvents_ResyncOnRestartWithNoEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub() // head seq is 0 — no Broadcast.

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

	time.Sleep(50 * time.Millisecond)
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
