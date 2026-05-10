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
		id := c.Param("id")
		mid := c.Param("mid")

		var req struct {
			Court string `json:"court"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.UpdateMatchCourt(id, mid, req.Court); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"court":         req.Court,
		})

		c.Status(http.StatusOK)
	})

	r.PUT("/competitions/:id/matches/:mid/override-winner", func(c *gin.Context) {
		id := c.Param("id")
		mid := c.Param("mid")
		var req struct {
			WinnerName string `json:"winnerName"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.OverrideBracketWinner(id, mid, req.WinnerName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusOK)
	})

	r.PUT("/competitions/:id/matches/:mid/time", func(c *gin.Context) {
		id := c.Param("id")
		mid := c.Param("mid")
		var req struct {
			ScheduledAt string `json:"scheduledAt"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.UpdateMatchTime(id, mid, req.ScheduledAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventScheduleUpdated, nil)
		c.Status(http.StatusOK)
	})
}
