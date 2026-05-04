package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
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
}

func (m *mockResponseWriter) CloseNotify() <-chan bool {
	return m.closeChan
}

func (m *mockResponseWriter) Flush() {}

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
	req, _ := http.NewRequest("GET", "/events", nil)

	go func() {
		r.ServeHTTP(w, req)
	}()

	// Wait a bit for headers to be set
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))

	// Broadcast an event and see if it appears in the body
	h.Broadcast(EventMatchUpdated, "test-data")

	time.Sleep(50 * time.Millisecond)

	// Check that data was written
	assert.Contains(t, w.Body.String(), "test-data")

	// Close the connection
	close(closeChan)
}

func TestHub_HandleEvents_Closure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub()

	// We need to capture the channel that HandleEvents subscribes to
	// This is a bit tricky because it's internal.
	// But we can just use the Hub directly.

	w := &mockResponseWriter{
		ResponseRecorder: httptest.NewRecorder(),
		closeChan:        make(chan bool),
	}
	c, _ := gin.CreateTestContext(w)

	// Start HandleEvents in a goroutine
	done := make(chan bool)
	go func() {
		h.HandleEvents()(c)
		done <- true
	}()

	// Wait for subscription to happen
	time.Sleep(50 * time.Millisecond)

	// Now find the channel in h.clients and close it
	h.mu.Lock()
	var internalCh chan string
	for ch := range h.clients {
		internalCh = ch
		break
	}
	h.mu.Unlock()

	if internalCh != nil {
		h.Unsubscribe(internalCh)
	}

	select {
	case <-done:
		// Success: HandleEvents exited
	case <-time.After(200 * time.Millisecond):
		t.Fatal("HandleEvents did not exit after channel closure")
	}
}
