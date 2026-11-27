package mobileapp

import (
	"errors"
	"net/http"
	"strings"

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
//   - GET .../draw/swap-candidates?id=: for one picked competitor, whether
//     swapping them with each other competitor keeps dojo-mates apart for
//     longer, brings them together earlier, or neither
//     (engine.SwapCandidatesFor).
//   - POST .../draw/swap: two competitors trade places
//     (engine.SwapDrawPlaces), or with dryRun the same answer without saving.
//
// The reads are recomputed from the stored draw on every call, as
// draw-warnings is; nothing is stored for them. All four routes are
// organiser only in self-run too (isSelfRunMainGatedConfigRoute), as their
// OpenAPI entries say; the candidates also take the competition's lock and
// can rebuild the knockout once per competitor. They are plain requests, not
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

	// GET /competitions/:id/draw/swap-suggestions: 200 {suggestions}, an
	// empty list unless the competition is draw-ready with a draw that
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
		c.JSON(http.StatusOK, answer)
	})

	// GET /competitions/:id/draw/swap-candidates?id=<participant id>: 200
	// {pick: {id, place}, candidates: [{id, place, verdict}]}, verdict
	// "better", "worse" or "same", every other drawn competitor in draw order.
	// 400 invalid without an id; 404 for an unknown competition; 409 no_draw,
	// draw_started or swap_not_supported as the swap answers them, and 409
	// not_in_draw for an id the draw does not hold (the page took the id from
	// the draw it shows, so the draw has changed since; the swap answers the
	// same code 400, since its body is the request).
	r.GET("/competitions/:id/draw/swap-candidates", func(c *gin.Context) {
		comp, ok := drawReviewCompetition(c, comps)
		if !ok {
			return
		}
		pick := strings.TrimSpace(c.Query("id"))
		if pick == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": engine.SwapInvalid, "message": swapCandidatesNeedAPick})
			return
		}
		answer, err := eng.SwapCandidatesFor(comp.ID, pick)
		if err != nil {
			var refused *engine.SwapRefusedError
			if errors.As(err, &refused) && refused.Code == engine.SwapNotInDraw {
				c.JSON(http.StatusConflict, gin.H{"error": refused.Code, "message": refused.Message})
				return
			}
			if respondIfSwapRefused(c, err) {
				return
			}
			respondEngineError(c, err)
			return
		}
		if answer.Candidates == nil {
			answer.Candidates = []engine.SwapCandidate{}
		}
		c.JSON(http.StatusOK, answer)
	})

	// POST /competitions/:id/draw/swap: body engine.SwapRequest. 200 with the
	// swap's answer (applied, or applied false with reason already_swapped for
	// a resend of a swap that landed, or dryRun true); a refusal is
	// respondIfSwapRefused's 400 or 409. An applied swap broadcasts
	// draw_generated once, so every view refetches the draw, and, when it moved
	// or removed saved lineups, lineup_updated naming the competition and no team
	// (every team's lineup views read again); a dry run and an already_swapped
	// resend broadcast nothing.
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
			if answer.LineupsMoved+answer.LineupsRemoved > 0 {
				hub.Broadcast(EventLineupUpdated, lineupUpdatedPayload(comp.ID, "", ""))
			}
		}
		c.JSON(http.StatusOK, answer)
	})
}

// swapCandidatesNeedAPick is the swap candidates' answer to a request that
// names no competitor (400 invalid).
const swapCandidatesNeedAPick = "Pick the first competitor of the swap: send their participant id as id."

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
		respondCompetitionNotFound(c)
		return nil, false
	}
	return comp, true
}
