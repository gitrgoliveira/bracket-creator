package mobileapp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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
			if errors.As(err, &notCompleted) {
				c.JSON(http.StatusConflict, gin.H{
					"error": err.Error(),
					"code":  "round_incomplete",
					"round": notCompleted.Round,
				})
				return
			}
			respondEngineError(c, err)
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
// viewer tab broke with "invalid admin password" because the
// admin-only RegisterSwissHandlers was the only registration.
//
// FR-050e.
//
// This form mounts the read with no judge, every caller reading the names as
// stored (tests mount it alone this way); NewRouterWithHub calls
// registerPublicSwissHandlers with the router's shared judge.
func RegisterPublicSwissHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine) {
	registerPublicSwissHandlers(r, store, eng, nil)
}

// registerPublicSwissHandlers wires the Swiss standings read. It judges its
// caller first (publicReadView: a password sent but wrong is 401, the answer is
// no-store) and answers for that audience. A Swiss competition cannot hide
// names, but the route answers for any competition id, so one that does
// (bc-hnam) gives the public each competitor's number where the name is,
// exactly as the league standings read does.
func registerPublicSwissHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine, names *publicNames) {
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
		v, ok := publicReadView(c, names, id)
		if !ok {
			return
		}
		standings, err := eng.SwissStandings(id)
		if err != nil {
			respondEngineError(c, err)
			return
		}
		if v.namesHidden() {
			// A copy: the engine's rows are left as they were.
			standings = append([]state.PlayerStanding(nil), standings...)
			v.standings(standings)
		}
		c.JSON(http.StatusOK, standings)
	})
}
