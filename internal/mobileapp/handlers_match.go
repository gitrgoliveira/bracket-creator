package mobileapp

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// tryAutoCompletePools runs the auto-complete check after a successful score
// write. The score itself has already been recorded, so we don't fail the
// request when the auto-complete check errors; instead we log full details
// server-side and set AutoCompleteErrorHeader to a generic sentinel so
// clients can detect the failure (and refresh) without us leaking
// internal store details. Broadcasts EventCompetitionCompleted when the
// transition actually happens.
func tryAutoCompletePools(c *gin.Context, eng *engine.Engine, hub *Hub, compID string) {
	autoCompleted, err := eng.MaybeAutoCompletePools(compID)
	if err != nil {
		log.Printf("MaybeAutoCompletePools(%s): %v", compID, err)
		c.Header(AutoCompleteErrorHeader, AutoCompleteErrorValue)
		return
	}
	if autoCompleted {
		hub.Broadcast(EventCompetitionCompleted, gin.H{"competitionId": compID})
	}
}

func RegisterMatchHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine, hub *Hub) {
	r.POST("/competitions/:id/matches/bulk-score", func(c *gin.Context) {
		id := c.Param("id")
		var results []state.MatchResult
		if err := c.ShouldBindJSON(&results); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		type scoreError struct {
			MatchID string `json:"matchId"`
			Error   string `json:"error"`
		}
		var errs []scoreError
		// Only successfully-recorded results go into the SSE broadcast so
		// clients never patch with values the engine rejected.
		var successful []state.MatchResult
		for i := range results {
			if err := eng.RecordMatchResult(id, results[i].ID, &results[i]); err != nil {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: err.Error()})
			} else {
				successful = append(successful, results[i])
			}
		}

		if len(successful) > 0 {
			hub.Broadcast(EventMatchUpdated, gin.H{
				"competitionId": id,
				"results":       successful,
			})
			tryAutoCompletePools(c, eng, hub, id)
		}
		c.JSON(http.StatusOK, gin.H{"succeeded": len(successful), "errors": errs})
	})

	r.PUT("/competitions/:id/matches/:mid/quick-score", func(c *gin.Context) {
		id := c.Param("id")
		mid := c.Param("mid")
		var req struct {
			SideA     string `json:"sideA"`
			SideB     string `json:"sideB"`
			TeamAWins int    `json:"teamAWins"`
			TeamBWins int    `json:"teamBWins"`
			Draws     int    `json:"draws"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.SideA == "" || req.SideB == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sideA and sideB are required"})
			return
		}

		// Determine team winner per kendo rules: most individual wins wins.
		winner := ""
		switch {
		case req.TeamAWins > req.TeamBWins:
			winner = req.SideA
		case req.TeamBWins > req.TeamAWins:
			winner = req.SideB
		}

		// Synthesise SubResults so standings IV/IL/IT counts are correct.
		// SideA/SideB must be set so the empty-Winner draw case doesn't
		// accidentally match `sub.Winner == sub.SideA` in computeStandings.
		subResults := make([]state.SubMatchResult, 0, req.TeamAWins+req.TeamBWins+req.Draws)
		pos := 1
		for range req.TeamAWins {
			subResults = append(subResults, state.SubMatchResult{Position: pos, SideA: req.SideA, SideB: req.SideB, Winner: req.SideA})
			pos++
		}
		for range req.TeamBWins {
			subResults = append(subResults, state.SubMatchResult{Position: pos, SideA: req.SideA, SideB: req.SideB, Winner: req.SideB})
			pos++
		}
		for range req.Draws {
			subResults = append(subResults, state.SubMatchResult{Position: pos, SideA: req.SideA, SideB: req.SideB, Winner: ""})
			pos++
		}

		result := state.MatchResult{
			ID:         mid,
			SideA:      req.SideA,
			SideB:      req.SideB,
			Winner:     winner,
			Status:     state.MatchStatusCompleted,
			SubResults: subResults,
		}
		if err := eng.RecordMatchResult(id, mid, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id, "matchId": mid})
		tryAutoCompletePools(c, eng, hub, id)
		c.JSON(http.StatusOK, result)
	})

	r.PUT("/competitions/:id/matches/:mid/score", func(c *gin.Context) {
		id := c.Param("id")
		mid := c.Param("mid")
		var result state.MatchResult
		if err := c.ShouldBindJSON(&result); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.RecordMatchResult(id, mid, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Broadcast update
		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        result,
		})
		tryAutoCompletePools(c, eng, hub, id)

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
