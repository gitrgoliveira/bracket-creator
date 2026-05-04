package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func RegisterMatchHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine, hub *Hub) {
	r.PUT("/competitions/:id/matches/:mid/score", func(c *gin.Context) {
		id := c.Param("id")
		mid := c.Param("mid")
		var result state.MatchResult
		if err := c.ShouldBindJSON(&result); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.RecordMatchResult(id, mid, result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Broadcast update
		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        result,
		})

		c.JSON(http.StatusOK, result)
	})

	r.PUT("/competitions/:id/matches/:mid/court", func(c *gin.Context) {
		// Placeholder for moving match to different court
		c.Status(http.StatusOK)
	})
}
