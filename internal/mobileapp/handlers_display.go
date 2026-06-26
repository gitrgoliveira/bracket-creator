package mobileapp

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RegisterDisplayHandlers wires the public, no-auth display surfaces used by
// non-browser streaming integrations (vMix, OBS plugins, scoreboards). The
// only route today is GET /api/viewer/court/:court/current — a one-shot polled
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
	r.GET("/court/:court/current", func(c *gin.Context) {
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
				// Found the current match — build the current payload.
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
					"status": "current",
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

	// GET /api/viewer/court/:court/matches — the court-scoped match feed for
	// the operator console. Returns {court, competitions:[…]} where each entry
	// is the SAME public per-competition payload the aggregate GET /competitions
	// returns ({config, poolMatches, bracket}), but ONLY for competitions that
	// have at least one real (both-sides-resolved) match — pool or non-preview
	// bracket — physically placed on this court right now.
	//
	// The operator console is court-first AND cross-competition by design: the
	// running bout stays put across comps (AC7), the switch nudge watches other
	// comps on the court (AC6), and Submit+Next advances within the submitted
	// comp — all need every competition on the court, not just the selected one.
	// Keying on actual match placement (not comp.courts config) keeps the page
	// correct after an operator MOVES a match to another shiaijo. The per-comp
	// match data is full (not court-filtered) so client-side derivations like
	// "Match N of M" pool counts stay correct; the right-sizing is by
	// competition COUNT (only comps on this court, not the whole tournament).
	r.GET("/court/:court/matches", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Header("Access-Control-Allow-Origin", "*")

		court := strings.ToUpper(strings.TrimSpace(c.Param("court")))

		tour, err := store.LoadTournament()
		if err != nil || tour == nil {
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

		ids, _ := store.ListCompetitions()
		comps := make([]gin.H, 0, len(ids))
		for _, compID := range ids {
			comp, _ := store.LoadCompetition(compID)
			if comp == nil {
				continue
			}
			// A setup competition exposes no public matches (parity with
			// compMatches in viewer_utils.jsx, which returns [] for setup),
			// so it never appears on the court feed.
			if comp.Status == state.CompStatusSetup {
				continue
			}
			if !competitionHasMatchOnCourt(store, compID, court) {
				continue
			}
			if payload := buildViewerCompetitionPayload(store, compID); payload != nil {
				comps = append(comps, payload)
			}
		}

		c.JSON(http.StatusOK, gin.H{"court": court, "competitions": comps})
	})
}

// bracketPlaceholderRE / poolOriginPlaceholderRE mirror the same-named regexes
// in web-mobile/js/admin_helpers.jsx. A side matching either is an unresolved
// placeholder ("Winner of r2-m1", "Pool A-1st"), not a real fighter.
var (
	bracketPlaceholderRE    = regexp.MustCompile(`^Winner of r\d+-m\d+$`)
	poolOriginPlaceholderRE = regexp.MustCompile(`^Pool .+-\d+(st|nd|rd|th)$`)
)

// courtMatchSidesReal mirrors hasBothSides (web-mobile/js/admin_helpers.jsx):
// a match counts only when both sides are present AND neither is a bracket
// "Winner of…" or pool-origin "Pool A-1st" placeholder. Keeping this in lockstep
// with the JS predicate ensures the court→competitions index lists exactly the
// competitions the operator selector derives from the aggregate today.
func courtMatchSidesReal(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if bracketPlaceholderRE.MatchString(a) || bracketPlaceholderRE.MatchString(b) {
		return false
	}
	if poolOriginPlaceholderRE.MatchString(a) || poolOriginPlaceholderRE.MatchString(b) {
		return false
	}
	return true
}

// competitionHasMatchOnCourt reports whether the competition has at least one
// real match (pool or non-preview bracket) physically placed on the given
// court. A preview bracket (mixed-comp placeholder structure) is skipped — it
// is read-only and never played, mirroring the aggregate viewer's preview
// strip in handlers_viewer.go.
func competitionHasMatchOnCourt(store *state.Store, compID, court string) bool {
	poolMatches, _ := store.LoadPoolMatches(compID)
	for _, m := range poolMatches {
		if strings.EqualFold(m.Court, court) && courtMatchSidesReal(m.SideA, m.SideB) {
			return true
		}
	}

	bracket, _ := store.LoadBracket(compID)
	if bracket != nil && !bracket.Preview {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if strings.EqualFold(bm.Court, court) && courtMatchSidesReal(bm.SideA, bm.SideB) {
					return true
				}
			}
		}
	}
	return false
}

// buildSide turns a participant name (which is what MatchResult.SideA/SideB
// carries — see state/models.go) into the per-side payload defined by the
// court-current contract. When the participants list cannot resolve the name
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
// field is never empty for a current match (the contract documents the field
// is always present on current payloads).
func phaseFromMatchID(id string) string {
	if i := strings.LastIndex(id, "-"); i > 0 {
		return id[:i]
	}
	return id
}
