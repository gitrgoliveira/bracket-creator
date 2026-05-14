package mobileapp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func RegisterTournamentHandlers(r *gin.RouterGroup, store *state.Store, hub *Hub) {
	r.GET("/tournament", func(c *gin.Context) {
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "tournament not initialized"})
			return
		}
		c.JSON(http.StatusOK, t)
	})

	r.PUT("/tournament", func(c *gin.Context) {
		var t state.Tournament
		if err := c.ShouldBindJSON(&t); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Trim Name and Venue so padded input from older clients (or
		// hand-crafted API calls) doesn't persist with surrounding
		// whitespace. Mirrors handlers_competition.go's TrimSpace
		// pattern on comp.Name + comp.NumberPrefix.
		t.Name = strings.TrimSpace(t.Name)
		t.Venue = strings.TrimSpace(t.Venue)

		changed, err := store.SaveTournamentChanged(&t)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.JSON(http.StatusOK, t)
	})

	r.POST("/tournament", func(c *gin.Context) {
		var t state.Tournament
		if err := c.ShouldBindJSON(&t); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// See PUT handler above. The CreateTournament UI in app.jsx
		// uses `if (!name || !pass)` which is truthy for whitespace,
		// so an untrimmed name on the wire would round-trip.
		t.Name = strings.TrimSpace(t.Name)
		t.Venue = strings.TrimSpace(t.Venue)

		if _, err := store.SaveTournamentChanged(&t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusCreated, t)
	})
}
