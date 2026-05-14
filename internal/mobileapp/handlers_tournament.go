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
		// Trim string fields so padded input from older clients (or
		// hand-crafted API calls) doesn't persist with surrounding
		// whitespace. Date is included for cross-file guard symmetry
		// with handlers_import.go (which trims competition.Date) and
		// handlers_competition.go (which now trims the same competition
		// string fields uniformly).
		t.Name = strings.TrimSpace(t.Name)
		t.Venue = strings.TrimSpace(t.Venue)
		t.Date = strings.TrimSpace(t.Date)

		// Reject whitespace-only names. The current EditTournament UI
		// (admin_setup.jsx) validates trimmed name client-side before
		// submit, but older cached clients (and direct API callers)
		// can still send "   "; without this guard, the trim above
		// silently persists Name == "" — admin UI then shows a blank
		// tournament title and the persisted record fails the
		// documented "tournament has a name" invariant.
		// Cross-file guard symmetry with the POST handler below and
		// (after this commit) the competition write paths in
		// handlers_competition.go + handlers_import.go.
		if t.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tournament name is required"})
			return
		}

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
		// See PUT handler above. The current CreateTournament UI in
		// app.jsx trims client-side before submit, but older clients
		// (cached builds with the pre-trim form) and direct API callers
		// can still send padded values — keep the server-side trim as
		// the defense layer so persisted records are always canonical.
		t.Name = strings.TrimSpace(t.Name)
		t.Venue = strings.TrimSpace(t.Venue)
		t.Date = strings.TrimSpace(t.Date)

		// Same empty-after-trim guard as the PUT handler. POST is the
		// first-time-setup entry point; if both Name == "" and
		// Password == "" land here, AuthMiddleware's password check
		// vacuously passes for any client (empty header == empty
		// stored password), exposing /api/* unauthenticated. The PUT
		// handler's guard above and this one together ensure that
		// failure mode can't be reached via the normal write paths.
		if t.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tournament name is required"})
			return
		}

		if _, err := store.SaveTournamentChanged(&t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusCreated, t)
	})
}
