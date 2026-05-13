package mobileapp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// EventType defines the kind of update being broadcast
type EventType string

const (
	EventMatchUpdated         EventType = "match_updated"
	EventCompetitionStarted   EventType = "competition_started"
	EventCompetitionCompleted EventType = "competition_completed"
	EventTournamentUpdated    EventType = "tournament_updated"
	EventScheduleUpdated      EventType = "schedule_updated"
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

// SSEEvent represents the payload sent to clients
type SSEEvent struct {
	Type EventType `json:"type"`
	Data any       `json:"data"`
}

// Hub manages active SSE connections and broadcasts events
type Hub struct {
	clients map[chan string]bool
	mu      sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[chan string]bool),
	}
}

// Subscribe adds a new client channel to the hub
func (h *Hub) Subscribe() chan string {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Buffer absorbs short bursts (bulk-score and schedule updates) for ~300
	// concurrent SSE clients; truly stalled clients are detected via the
	// non-blocking send in Broadcast and unsubscribed.
	ch := make(chan string, 100)
	h.clients[ch] = true
	return ch
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

// Broadcast sends an event to all subscribed clients
func (h *Hub) Broadcast(eventType EventType, data any) {
	event := SSEEvent{
		Type: eventType,
		Data: data,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		fmt.Printf("Error marshaling SSE event: %v\n", err)
		return
	}

	h.mu.RLock()
	clients := make([]chan string, 0, len(h.clients))
	for ch := range h.clients {
		clients = append(clients, ch)
	}
	h.mu.RUnlock()

	var dead []chan string
	for _, ch := range clients {
		select {
		case ch <- string(payload):
		default:
			dead = append(dead, ch)
		}
	}

	if len(dead) > 0 {
		h.mu.Lock()
		for _, ch := range dead {
			h.unsubscribeLocked(ch)
		}
		h.mu.Unlock()
	}
}

// HandleEvents returns a Gin handler for the SSE endpoint
func (h *Hub) HandleEvents() gin.HandlerFunc {
	return func(c *gin.Context) {
		ch := h.Subscribe()
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

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		c.Stream(func(w io.Writer) bool {
			select {
			case msg, ok := <-ch:
				if ok {
					c.SSEvent("message", msg)
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
