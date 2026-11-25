package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The draw review (bc-drev): what an organiser reads and changes between
// generating a draw and starting the competition. Three routes, all in the
// admin group:
//
//   - GET .../dojo-separation: every pair of dojo-mates in the draw and the
//     earliest match where they can meet (engine.DojoSeparationFor).
//   - GET .../draw/swap-suggestions: swaps of two unseeded competitors that
//     keep dojo-mates apart for longer (engine.SwapSuggestionsFor).
//   - POST .../draw/swap: two competitors trade places
//     (engine.SwapDrawPlaces), or with dryRun the same answer without saving.
//
// Both reads are recomputed from the stored draw on every call, as
// draw-warnings is; nothing is stored for them. The swap is organiser only in
// self-run too (isSelfRunMainGatedConfigRoute). They are plain requests, not
// outbox doors: the page needs the answer before it shows anything.
func RegisterDrawReviewHandlers(r *gin.RouterGroup, comps CompetitionLoader, eng DrawReviewEngine, hub Broadcaster) {
	// GET /competitions/:id/dojo-separation: 200 {meetings: [...]}, an empty
	// list for a competition with no draw, a league or a Swiss competition;
	// 404 for an unknown competition.
	r.GET("/competitions/:id/dojo-separation", func(c *gin.Context) {
		comp, ok := drawReviewCompetition(c, comps)
		if !ok {
			return
		}
		meetings, err := eng.DojoSeparationFor(comp)
		if err != nil {
			internalError(c, err)
			return
		}
		if meetings == nil {
			meetings = []engine.DojoMeeting{}
		}
		c.JSON(http.StatusOK, gin.H{"meetings": meetings})
	})

	// GET /competitions/:id/draw/swap-suggestions: 200 {suggestions, stuck},
	// both empty lists unless the competition is draw-ready with a draw that
	// has a bracket; 404 for an unknown competition.
	r.GET("/competitions/:id/draw/swap-suggestions", func(c *gin.Context) {
		comp, ok := drawReviewCompetition(c, comps)
		if !ok {
			return
		}
		answer, err := eng.SwapSuggestionsFor(comp)
		if err != nil {
			internalError(c, err)
			return
		}
		if answer == nil {
			answer = &engine.SwapSuggestions{}
		}
		if answer.Suggestions == nil {
			answer.Suggestions = []engine.SwapSuggestion{}
		}
		if answer.Stuck == nil {
			answer.Stuck = []engine.StuckDojo{}
		}
		c.JSON(http.StatusOK, answer)
	})

	// POST /competitions/:id/draw/swap: body engine.SwapRequest. 200 with the
	// swap's answer (applied, or applied false with reason already_swapped for
	// a resend of a swap that landed, or dryRun true); a refusal is
	// respondIfSwapRefused's 400 or 409. An applied swap broadcasts
	// draw_generated once, so every view refetches the draw; a dry run and an
	// already_swapped resend broadcast nothing.
	r.POST("/competitions/:id/draw/swap", func(c *gin.Context) {
		comp, ok := drawReviewCompetition(c, comps)
		if !ok {
			return
		}
		var req engine.SwapRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": engine.SwapInvalid, "message": swapBodyUnreadable})
			return
		}
		answer, err := eng.SwapDrawPlaces(comp.ID, req)
		if err != nil {
			if respondIfSwapRefused(c, err) {
				return
			}
			respondEngineError(c, err)
			return
		}
		if answer.Applied && !answer.DryRun {
			hub.Broadcast(EventDrawGenerated, gin.H{"competitionId": comp.ID})
		}
		c.JSON(http.StatusOK, answer)
	})
}

// drawReviewCompetition reads the competition a draw review route names:
// false, answered, for an invalid id (400), an unknown competition (404) or
// a read that failed (500).
func drawReviewCompetition(c *gin.Context, comps CompetitionLoader) (*state.Competition, bool) {
	id, ok := requireValidCompID(c)
	if !ok {
		return nil, false
	}
	comp, err := comps.LoadCompetition(id)
	if err != nil {
		internalError(c, err)
		return nil, false
	}
	if comp == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
		return nil, false
	}
	return comp, true
}
