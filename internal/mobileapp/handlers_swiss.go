package mobileapp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// EventSwissRoundGenerated is the SSE event broadcast when a new
// Swiss round's matches are generated. Clients refresh their match
// list and competition view on this event (the bumped
// swissCurrentRound is included in the payload so callers don't
// need a separate GET).
//
// FR-050d.
const EventSwissRoundGenerated EventType = "swiss_round_generated"

// RegisterSwissHandlers wires the Swiss-format-specific endpoints
// onto the admin router group:
//
//	POST /api/competitions/:id/swiss/generate-round  , generate next round
//	GET  /api/competitions/:id/swiss/standings       , cumulative standings
//
// Both endpoints sit inside the admin group (same as the rest of the
// competition write/read endpoints). The standings GET is read-only
// but matches the existing pattern of pool-standings being inside the
// admin group as well, viewer-side standings reuse the same Engine
// helper via the public viewer handlers when needed.
//
// FR-050d, FR-050e.
func RegisterSwissHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine, hub *Hub) {
	// POST /competitions/:id/swiss/generate-round
	//
	// Pre-conditions:
	//   - competition must exist (404)
	//   - competition.Format must equal "swiss" (400)
	//   - all matches in the current Swiss round must be completed (409)
	//   - swissCurrentRound must be < swissRounds (400)
	//
	// On success returns 201 with the new round's matches and the
	// bumped swissCurrentRound. Broadcasts EventSwissRoundGenerated so
	// admin/viewer surfaces refresh in real time.
	//
	// FR-050d, contracts/competitor-status.md style.
	r.POST("/competitions/:id/swiss/generate-round", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		newMatches, nextRound, err := eng.AdvanceSwissRound(id)
		if err != nil {
			var notCompleted *engine.SwissRoundNotCompletedError
			var notFound *engine.NotFoundError
			var validation *engine.ValidationError
			switch {
			case errors.As(err, &notCompleted):
				c.JSON(http.StatusConflict, gin.H{
					"error": err.Error(),
					"code":  "round_incomplete",
					"round": notCompleted.Round,
				})
			case errors.As(err, &notFound):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validation):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		// Broadcast: bare event-type signal lets every client decide
		// whether to refetch. Carry the round number so the admin
		// "Generate next round" button can update its label without
		// a roundtrip.
		hub.Broadcast(EventSwissRoundGenerated, gin.H{
			"competitionId":     id,
			"swissCurrentRound": nextRound,
			"matchCount":        len(newMatches),
		})
		c.JSON(http.StatusCreated, gin.H{
			"round":             nextRound,
			"matches":           newMatches,
			"swissCurrentRound": nextRound,
		})
	})

}

// RegisterPublicSwissHandlers wires read-only Swiss endpoints under the
// public /api group. Standings are derived from completed match results,
// which are themselves public via the viewer endpoint, so spectators,
// coaches, and TV displays need them without admin credentials.
// Discovered during the post-merge browser UAT pass: the SwissStandings
// viewer tab broke with "invalid tournament password" because the
// admin-only RegisterSwissHandlers was the only registration.
//
// FR-050e.
func RegisterPublicSwissHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine) {
	// GET /competitions/:id/swiss/standings
	//
	// Returns the cumulative Swiss standings ranked by wins → points
	// scored → head-to-head → name (stable). Always 200 with an array
	// (possibly empty for a competition that has not yet started).
	r.GET("/competitions/:id/swiss/standings", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		standings, err := eng.SwissStandings(id)
		if err != nil {
			var notFound *engine.NotFoundError
			switch {
			case errors.As(err, &notFound):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}
		c.JSON(http.StatusOK, standings)
	})
}
