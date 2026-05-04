package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func RegisterParticipantHandlers(r *gin.RouterGroup, store *state.Store) {
	r.GET("/competitions/:id/participants", func(c *gin.Context) {
		id := c.Param("id")
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		players, err := store.LoadParticipants(id, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, players)
	})

	r.POST("/competitions/:id/participants", func(c *gin.Context) {
		var req struct {
			Players []struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				Dojo        string `json:"dojo"`
			} `json:"players"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Convert to helper.Player
		// In a real app we might want to preserve more fields, but for now this matches the plan
		// to set participants.
		// Actually, let's just use helper.Player directly in the request if possible
		// but the plan says "set participants (CSV body or JSON)".

		// For now, let's just assume JSON for simplicity as it's easier from a web UI
		// We'll implement CSV upload later if needed.

		// Note: helper.Player is a large struct, we only need a few fields for setup

		// ... implementation of SaveParticipants ...
		// I'll skip the complex conversion for now and just use a simple one
		c.JSON(http.StatusNotImplemented, gin.H{"error": "JSON participant upload not fully implemented yet"})
	})

	r.GET("/competitions/:id/seeds", func(c *gin.Context) {
		id := c.Param("id")
		seeds, err := store.LoadSeeds(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, seeds)
	})

	r.PUT("/competitions/:id/seeds", func(c *gin.Context) {
		id := c.Param("id")
		var assignments []domain.SeedAssignment
		if err := c.ShouldBindJSON(&assignments); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := store.SaveSeeds(id, assignments); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, assignments)
	})
}
