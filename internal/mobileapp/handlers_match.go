package mobileapp

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// annotateQueuePositions fills in MatchResult.QueuePosition for each
// element of matches in-place, using state.DeriveQueuePositions.
//
// FR-025, T036: queue positions are derived at serve time rather than
// persisted — a stored value would go stale the instant any match
// transitions and we'd have to recompute on every score write anyway.
// Match-list endpoints (handlers_viewer.go: GET /competitions and
// GET /competitions/:id) call this just before c.JSON so viewers see
// "next up: 3" without any background recomputation job. Score-write
// endpoints return a single MatchResult and intentionally do NOT
// annotate (a single match has no list ordering to derive against).
func annotateQueuePositions(matches []state.MatchResult) {
	if len(matches) == 0 {
		return
	}
	positions := state.DeriveQueuePositions(matches)
	for i := range matches {
		matches[i].QueuePosition = positions[i]
	}
}

// tryAutoCompletePools runs the auto-complete check after a successful score
// write. The score itself has already been recorded, so we don't fail the
// request when the auto-complete check errors; instead we log full details
// server-side and set AutoCompleteErrorHeader to a generic sentinel so
// clients can detect the failure (and refresh) without us leaking
// internal store details. Broadcasts EventCompetitionCompleted when the
// transition actually happens.
//
// Takes the consumer-boundary interfaces (T014) so handler tests can
// stub the engine + hub without spinning up the full state/engine
// stack. Production code passes `*engine.Engine` and `*Hub` which
// satisfy the interfaces by structural match.
func tryAutoCompletePools(c *gin.Context, eng ScoringEngine, hub Broadcaster, compID string) {
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

// RegisterMatchHandlers wires up the score / quick-score / bulk-score /
// court / override-winner / time endpoints under the admin group.
//
// The score endpoint is the Slice 0 / NFR-002 demonstration of the
// interface-based dependency injection pattern (T017): it consumes
// `ScoringEngine` and `Broadcaster` rather than the concrete
// `*engine.Engine` and `*Hub`, plus the `ScoreRequest.Validate()`
// pattern (T015 / NFR-004) for request-shape validation.
//
// The remaining endpoints in this file still hold concrete pointers
// (the function signature accepts the concrete `*engine.Engine` for
// methods not yet on the interface). Later slices migrate those one at
// a time; the concrete `*engine.Engine` remains a drop-in
// implementation of `ScoringEngine` so the `tryAutoCompletePools` and
// score endpoint paths can already accept the interface today.
func RegisterMatchHandlers(r *gin.RouterGroup, eng *engine.Engine, hub *Hub) {
	r.POST("/competitions/:id/matches/bulk-score", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
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
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
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

	// Score endpoint — Slice 0 demonstration of the interface-DI +
	// Validate() pattern (T015 / T017 / NFR-002 / NFR-004). Calls go
	// through ScoringEngine and Broadcaster (the consumer-boundary
	// interfaces from deps.go) rather than the concrete types, and the
	// request body is validated via ScoreRequest.Validate() before any
	// engine call. The closure captures `*engine.Engine` / `*Hub` and
	// adapts them to the interfaces at the call boundary — same wire
	// behaviour as before.
	registerScoreHandler(r, eng, hub)

	r.PUT("/competitions/:id/matches/:mid/court", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
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
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		var req struct {
			WinnerName string `json:"winnerName"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Trim whitespace from the winner name. Downstream comparisons
		// (m.Winner == m.SideA / m.SideB in engine/scoring.go and
		// engine/ranking.go) are exact-string equality, so a padded
		// "  Foo  " won't match the canonical "Foo" from the roster —
		// bracket math silently breaks. The JS prompt site at
		// admin_competition.jsx now trims client-side, but a
		// hand-crafted API call could still hit this. Mirrors the
		// override-rank handler's TrimSpace pattern.
		winnerName := strings.TrimSpace(req.WinnerName)
		if winnerName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "winnerName is required"})
			return
		}

		if err := eng.OverrideBracketWinner(id, mid, winnerName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusOK)
	})

	r.PUT("/competitions/:id/matches/:mid/time", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
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

// registerScoreHandler wires the `PUT /competitions/:id/matches/:mid/score`
// endpoint via the consumer-boundary interfaces (T014/T017) instead of
// the concrete `*engine.Engine` / `*Hub`. This is the Slice 0
// demonstration of the interface-DI pattern (NFR-002): handler tests
// can drive this code path with a stub ScoringEngine + Broadcaster, no
// temp dirs, no real engine wiring.
//
// Behaviour is identical to the pre-Slice-0 version except for the new
// ScoreRequest.Validate() call, which surfaces a 400 with the field
// name when the body is malformed against its own shape rules
// (Status outside the documented enum, Winner not naming either side).
// The engine's preserve-on-empty-side fallback continues to handle the
// "client sends scoring fields only" case.
func registerScoreHandler(r *gin.RouterGroup, eng ScoringEngine, hub Broadcaster) {
	r.PUT("/competitions/:id/matches/:mid/score", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")

		var req ScoreRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := req.Validate(); err != nil {
			// Map ValidationError → 400 with the validator's message.
			// Engine errors below remain 500 (they surface I/O / state
			// failures, not request-shape errors).
			var verr *ValidationError
			if errors.As(err, &verr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error()})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		result := req.AsMatchResult()
		if err := eng.RecordMatchResult(id, mid, result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Broadcast match update with the full (post-merge) result so
		// SSE consumers see the same payload they'd see on a re-fetch.
		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        result,
		})
		tryAutoCompletePools(c, eng, hub, id)

		c.JSON(http.StatusOK, result)
	})
}
