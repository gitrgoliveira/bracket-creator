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
// Last-Event-ID=1 means events 2 has been overwritten (only 3,4,5
// survive), so the client gets 3/4/5 replayed and a server-side warning
// is logged (we just assert the surviving entries appear).
func TestHubRingBufferEvicts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHubWithHistory(3)

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
	// 2 was evicted (only 3,4,5 are retained); 3/4/5 must be replayed.
	assert.Contains(t, body, "id: 3\n", "seq 3 should be replayed (oldest retained)")
	assert.Contains(t, body, "id: 4\n")
	assert.Contains(t, body, "id: 5\n")
	assert.NotContains(t, body, "id: 1\n")
	assert.NotContains(t, body, "id: 2\n", "seq 2 evicted before reconnect should NOT be replayed")
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
