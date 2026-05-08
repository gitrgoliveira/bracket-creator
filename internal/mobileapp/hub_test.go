package mobileapp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
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
