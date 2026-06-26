package mobileapp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RegisterDisplayHandlers wires the public, no-auth display surfaces used by
// non-browser streaming integrations (vMix, OBS plugins, scoreboards). The
// only route today is GET /api/viewer/court/:court/live — a one-shot polled
// view of the currently-running match on a court. Browser clients should
// continue to use SSE (/api/events); the polled surface exists for clients
// that cannot subscribe.
//
// Per NFR-002 the handler depends on the *state.Store concrete type (the
// same boundary handlers_viewer.go uses); there is no snug-fit interface
// in deps.go that covers the read methods we need here, and inventing one
// for a single consumer would be premature. If a second polled surface
// lands later we can hoist a DisplayStore interface then.
func RegisterDisplayHandlers(r *gin.RouterGroup, store *state.Store) {
	r.GET("/court/:court/live", func(c *gin.Context) {
		// Streaming clients poll this on a 1-2s cadence; never let an
		// upstream cache them. The router-level CORS already exposes
		// Access-Control-Allow-Origin, but the contract pins it here too
		// so the polled-surface guarantee survives router refactors.
		c.Header("Cache-Control", "no-store")
		c.Header("Access-Control-Allow-Origin", "*")

		court := strings.ToUpper(strings.TrimSpace(c.Param("court")))

		tour, err := store.LoadTournament()
		if err != nil || tour == nil {
			// 503 distinguishes "tournament not loaded yet" from a
			// genuine 4xx — clients can retry without reporting it as
			// an error to the operator.
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no_active_tournament"})
			return
		}

		validCourt := false
		for _, ct := range tour.Courts {
			if strings.EqualFold(ct, court) {
				validCourt = true
				break
			}
		}
		if !validCourt {
			c.JSON(http.StatusNotFound, gin.H{"error": "court_not_found", "court": court})
			return
		}

		// Scan competitions in listing order; the first running match on
		// this court wins. Two running matches on the same court would
		// already be a tournament-data error (one court runs one match
		// at a time per R8) and we surface whichever appears first.
		ids, _ := store.ListCompetitions()
		for _, compID := range ids {
			comp, _ := store.LoadCompetition(compID)
			if comp == nil {
				continue
			}

			poolMatches, _ := store.LoadPoolMatches(compID)
			for _, m := range poolMatches {
				if !strings.EqualFold(m.Court, court) {
					continue
				}
				if m.Status != state.MatchStatusRunning {
					continue
				}
				// Found the live match — build the live payload.
				// LoadParticipantsOpt is the canonical read so we
				// pick up DisplayName/Dojo even on legacy competitions
				// that predate the HasParticipantIDs flag.
				var hasIDsHint *bool
				if comp.HasParticipantIDs {
					t := true
					hasIDsHint = &t
				}
				players, _ := store.LoadParticipantsOpt(compID, comp.WithZekkenName, state.LoadParticipantsOpts{WithSeeds: false, HasIDs: hasIDsHint})
				// mp-13y: merge numberPrefix-derived numbers from pools.csv
				// directly onto the slice so buildSide can include "number"
				// in the polled OBS/vMix overlay payload. Skip the pools.csv
				// read entirely when no prefix is configured (the common
				// case).
				if comp.NumberPrefix != "" {
					pools, _ := store.LoadPools(compID)
					mergePoolNumbersIntoPlayersSlice(comp.NumberPrefix, players, pools, comp.Format)
				}

				sideA := buildSide(m.SideA, players, comp.WithZekkenName)
				sideB := buildSide(m.SideB, players, comp.WithZekkenName)

				c.JSON(http.StatusOK, gin.H{
					"court":  court,
					"status": "live",
					"competition": gin.H{
						"id":   comp.ID,
						"name": comp.Name,
					},
					"phase":    phaseFromMatchID(m.ID),
					"sideA":    sideA,
					"sideB":    sideB,
					"ipponsA":  m.IpponsA,
					"ipponsB":  m.IpponsB,
					"hansokuA": m.HansokuA,
					"hansokuB": m.HansokuB,
					// Pool daihyosen/tiebreaker rep bouts carry team names in
					// sideA/sideB; the representative fighter for each side lives
					// here. Empty for every regular match (mp-62vr).
					"repPlayerA": m.RepPlayerA,
					"repPlayerB": m.RepPlayerB,
				})
				return
			}
		}

		// No running match on this court.
		c.JSON(http.StatusOK, gin.H{"court": court, "status": "idle"})
	})
}

// buildSide turns a participant name (which is what MatchResult.SideA/SideB
// carries — see state/models.go) into the per-side payload defined by the
// court-live contract. When the participants list cannot resolve the name
// we fall back to a name-only side so the overlay can still render
// "Player vs Player" rather than blanking out.
func buildSide(name string, players []domain.Player, withZekkenName bool) gin.H {
	displayName := name
	dojo := ""
	playerID := ""
	number := ""
	for i := range players {
		if players[i].Name == name {
			if withZekkenName && players[i].DisplayName != "" {
				displayName = players[i].DisplayName
			}
			dojo = players[i].Dojo
			playerID = players[i].ID
			number = players[i].Number
			break
		}
	}
	return gin.H{
		"playerId":    playerID,
		"name":        name,
		"displayName": displayName,
		"dojo":        dojo,
		"number":      number,
	}
}

// phaseFromMatchID derives a human-readable phase label from a match ID.
// Pool match IDs are formatted "PoolName-Idx" by engine/pools.go; we strip
// the suffix and return the pool name verbatim. Bracket matches will land
// in a later slice — for now we return the raw ID as a fallback so the
// field is never empty for a live match (the contract documents the field
// is always present on live payloads).
func phaseFromMatchID(id string) string {
	if i := strings.LastIndex(id, "-"); i > 0 {
		return id[:i]
	}
	return id
}
