package mobileapp

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func RegisterParticipantHandlers(r *gin.RouterGroup, store *state.Store) {
	r.GET("/competitions/:id/participants", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
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
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		var req struct {
			Players []struct {
				Name        string   `json:"name"`
				DisplayName string   `json:"displayName"`
				Dojo        string   `json:"dojo"`
				Metadata    []string `json:"metadata"`
				Tag         string   `json:"tag"`
			} `json:"players"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Per-player length caps — defense-in-depth against unbounded
		// participants.csv inflation. Reject the whole batch on the
		// first offender (matches the all-or-nothing semantics
		// SaveParticipants already enforces on write).
		for i, p := range req.Players {
			if err := validatePlayerLengths(p.Name, p.DisplayName, p.Dojo, p.Tag, p.Metadata); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("players[%d]: %s", i, err.Error())})
				return
			}
		}

		players := make([]domain.Player, 0, len(req.Players))
		for i, p := range req.Players {
			players = append(players, domain.Player{
				Name:         p.Name,
				DisplayName:  p.DisplayName,
				Dojo:         p.Dojo,
				Metadata:     p.Metadata,
				Tag:          p.Tag,
				PoolPosition: int64(i),
			})
		}

		if err := store.SaveParticipants(id, players); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save participants: " + err.Error()})
			return
		}

		c.JSON(http.StatusOK, players)
	})

	r.GET("/competitions/:id/seeds", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		seeds, err := store.LoadSeeds(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, seeds)
	})

	r.PUT("/competitions/:id/seeds", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
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
