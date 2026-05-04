package mobileapp

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/gin-gonic/gin"
)

// EventType defines the kind of update being broadcast
type EventType string

const (
	EventMatchUpdated       EventType = "match_updated"
	EventCompetitionStarted EventType = "competition_started"
	EventTournamentUpdated  EventType = "tournament_updated"
)

// SSEEvent represents the payload sent to clients
type SSEEvent struct {
	Type EventType   `json:"type"`
	Data interface{} `json:"data"`
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
	ch := make(chan string, 10)
	h.clients[ch] = true
	return ch
}

// Unsubscribe removes a client channel
func (h *Hub) Unsubscribe(ch chan string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
}

// Broadcast sends an event to all subscribed clients
func (h *Hub) Broadcast(eventType EventType, data interface{}) {
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
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- string(payload):
		default:
			// Client slow, skip or buffer?
		}
	}
}

// HandleEvents returns a Gin handler for the SSE endpoint
func (h *Hub) HandleEvents() gin.HandlerFunc {
	return func(c *gin.Context) {
		ch := h.Subscribe()
		defer h.Unsubscribe(ch)

		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")
		c.Header("Transfer-Encoding", "chunked")

		c.Stream(func(w io.Writer) bool {
			if msg, ok := <-ch; ok {
				c.SSEvent("message", msg)
				return true
			}
			return false
		})
	}
}
