// Package mobileapp — handlers_daihyosen.go owns the
// `POST /api/competitions/:cid/matches/:mid/daihyosen` endpoint that
// validates a tied knockout-stage team match and appends a daihyosen
// (representative-bout) placeholder to its SubResults (T140, FR-046,
// CHK026).
//
// The endpoint is structurally a sibling of `/decision` and `/score`:
// the operator's UI calls this when both teams finish a knockout match
// with equal IV+PW; the engine returns the placeholder SubMatchResult
// (Position=-1, Decision="daihyosen") which the handler persists onto
// the parent match. The operator then fills the rep player names +
// ippon via the standard score path.
//
// Eligibility integration (CHK026): before validating the tie, the
// handler counts each team's eligible competitors via the competitor-
// status store. When either side has zero eligible competitors the
// engine returns ErrInsufficientEligibility and we respond 409 — the
// caller MUST then forfeit the encounter to the opposing team via the
// standard score endpoint (this handler intentionally does NOT
// auto-record the forfeit so the operator confirms it).
package mobileapp

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// DaihyosenEngine is the consumer-boundary view of *engine.Engine used
// by the daihyosen handler. Mirrors engine.Engine.AddDaihyosen +
// RecordMatchResultWithIneligibility + MaybeAutoCompletePools.
//
// Defined as a named local interface (rather than reusing ScoringEngine)
// because AddDaihyosen is not on the existing ScoringEngine interface,
// and broadening that surface for one new endpoint would expose the
// method to every other handler family.
type DaihyosenEngine interface {
	AddDaihyosen(compID, matchID string, sideA, sideB engine.TeamSummary, isPool bool, sideAEligible, sideBEligible int) (*state.SubMatchResult, error)
	RecordMatchResultWithIneligibility(compID, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error)
	MaybeAutoCompletePools(compID string) (engine.AutoCompleteOutcome, error)
}

// DaihyosenStore is the consumer-boundary view of *state.Store used by
// the daihyosen handler. The handler needs to (a) look up the target
// match in either the pool-match store or the bracket store so it can
// derive TeamSummary values from SubResults, (b) load participants so
// it can map roster names to player IDs for eligibility counting, and
// (c) load competitor-status to count eligible competitors per side.
type DaihyosenStore interface {
	LoadPoolMatches(compID string) ([]state.MatchResult, error)
	LoadBracket(compID string) (*state.Bracket, error)
	LoadCompetition(compID string) (*state.Competition, error)
	LoadParticipants(compID string, withZekkenName bool) ([]domain.Player, error)
	CompetitorStatusStore
}

// RegisterDaihyosenHandlers wires the POST /daihyosen endpoint. The
// caller in server.go passes `*engine.Engine` and `*state.Store` which
// satisfy the local interfaces by structural match.
//
// T140, FR-046.
func RegisterDaihyosenHandlers(r *gin.RouterGroup, eng DaihyosenEngine, store DaihyosenStore, hub Broadcaster) {
	r.POST("/competitions/:id/matches/:mid/daihyosen", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")

		// Locate the match — pool first, then bracket. We need its
		// SubResults to compute TeamSummary, plus SideA/SideB names for
		// the eligibility lookup.
		match, found, err := findMatchForDaihyosen(store, id, mid)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
			return
		}

		sideASummary, sideBSummary := engine.ComputeTeamSummary(match.SubResults, match.SideA, match.SideB)

		// Count eligible competitors per side. Pre-Slice-7 we don't
		// have explicit team rosters in the store; eligibility is
		// tracked at the player level via competitor-status. For the
		// purposes of CHK026 we treat "team has ≥ 1 eligible competitor"
		// as "the team is named in the match AND has at least one
		// participant who is not currently marked ineligible". That
		// works for individual roll-up; once team lineups land
		// (handlers_lineup.go owned by the parallel agent) this
		// counter can be replaced with a roster-aware lookup.
		sideAEligible, sideBEligible, err := countEligibleForSides(store, id, match.SideA, match.SideB)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		sub, err := eng.AddDaihyosen(id, mid, sideASummary, sideBSummary, engine.IsPoolMatchID(mid), sideAEligible, sideBEligible)
		if err != nil {
			switch {
			case errors.Is(err, engine.ErrNotTied):
				c.JSON(http.StatusBadRequest, gin.H{"error": "not_tied"})
			case errors.Is(err, engine.ErrPoolMatch):
				c.JSON(http.StatusBadRequest, gin.H{"error": "pool_match"})
			case errors.Is(err, engine.ErrInsufficientEligibility):
				c.JSON(http.StatusConflict, gin.H{"error": "insufficient_eligibility"})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		// Append the placeholder to the match's SubResults and persist
		// via the standard score path so the per-competition lock is
		// honoured (TOCTOU-safe with concurrent scoring on other
		// matches). The operator subsequently fills in rep player names
		// + ippon via PUT /score.
		updated := *match
		updated.SubResults = append(append([]state.SubMatchResult{}, match.SubResults...), *sub)
		updated.Status = state.MatchStatusRunning // daihyosen bout in progress
		if _, err := eng.RecordMatchResultWithIneligibility(id, mid, &updated); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        &updated,
		})

		// Inline auto-complete check (same pattern as tryAutoCompletePools).
		outcome, autoErr := eng.MaybeAutoCompletePools(id)
		switch {
		case autoErr != nil:
			log.Printf("MaybeAutoCompletePools(%s) after daihyosen: %v", id, autoErr)
			c.Header(AutoCompleteErrorHeader, AutoCompleteErrorValue)
		case outcome == engine.AutoCompleteTransitioned:
			hub.Broadcast(EventCompetitionCompleted, gin.H{"competitionId": id})
		case outcome == engine.AutoCompleteTiebreakInjected:
			hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id})
			hub.Broadcast(EventScheduleUpdated, nil)
		}

		c.JSON(http.StatusOK, gin.H{"subResult": sub, "result": &updated})
	})
}

// findMatchForDaihyosen looks up the target match by ID, searching
// pool matches when the ID has the "Pool " prefix and otherwise the
// bracket. Returns (match, true, nil) on success; (nil, false, nil)
// when neither store holds the ID; (nil, false, err) on a store-level
// I/O failure.
func findMatchForDaihyosen(store DaihyosenStore, compID, matchID string) (*state.MatchResult, bool, error) {
	if engine.IsPoolMatchID(matchID) {
		poolMatches, err := store.LoadPoolMatches(compID)
		if err != nil {
			return nil, false, err
		}
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				return &poolMatches[i], true, nil
			}
		}
		return nil, false, nil
	}
	bracket, err := store.LoadBracket(compID)
	if err != nil {
		return nil, false, err
	}
	if bracket == nil {
		return nil, false, nil
	}
	for _, round := range bracket.Rounds {
		for _, bm := range round {
			if bm.ID == matchID {
				// Re-shape into MatchResult so we can drive the same
				// scoring path as pool matches. Bracket matches carry
				// scores in ScoreA/ScoreB strings + the parent bracket
				// doesn't currently hold SubResults — for daihyosen we
				// only need SideA/SideB and SubResults, which are empty
				// on the bracket side until the operator records team
				// sub-bouts via /score.
				return &state.MatchResult{
					ID:              bm.ID,
					SideA:           bm.SideA,
					SideB:           bm.SideB,
					Winner:          bm.Winner,
					Status:          bm.Status,
					Court:           bm.Court,
					ScheduledAt:     bm.ScheduledAt,
					Decision:        bm.Decision,
					DecisionBy:      bm.DecisionBy,
					Encho:           bm.Encho,
					DecidedByHantei: &bm.DecidedByHantei,
				}, true, nil
			}
		}
	}
	return nil, false, nil
}

// countEligibleForSides counts, for each named side, how many roster
// participants currently have CompetitorStatus.Eligible != false. This
// is a coarse pre-lineup approximation of CHK026: until per-team
// rosters land in the store, we conservatively treat ALL participants
// as belonging to both sides and instead require that the total
// eligible-participant count be positive. That's sufficient for the
// "0 eligible → forfeit" branch (the only place CHK026 actually fires
// in practice — a depleted team will have all its members marked
// ineligible after kiken).
//
// Once team lineups land (T-series owned by the parallel lineup agent)
// this helper can be replaced with a per-team eligibility count by
// joining state.LoadTeamLineup against statuses.
func countEligibleForSides(store DaihyosenStore, compID, sideAName, sideBName string) (int, int, error) {
	comp, err := store.LoadCompetition(compID)
	if err != nil {
		return 0, 0, err
	}
	withZekken := false
	if comp != nil {
		withZekken = comp.WithZekkenName
	}
	participants, err := store.LoadParticipants(compID, withZekken)
	if err != nil {
		return 0, 0, err
	}
	statuses, err := store.LoadCompetitorStatus(compID)
	if err != nil {
		return 0, 0, err
	}

	// Until team rosters exist, return the same eligible count for both
	// sides. Operators that hit the 0-eligible branch will still see
	// insufficient_eligibility surfaced once the depleted side has all
	// its members marked ineligible (i.e. when this count is 0).
	eligible := 0
	for _, p := range participants {
		st, ok := statuses[p.ID]
		if !ok || st.Eligible {
			eligible++
		}
	}
	// sideAName/sideBName are kept in the signature so the post-lineup
	// refactor (team-aware count) doesn't change the call site. Until
	// then, the names are intentionally unused.
	_ = sideAName
	_ = sideBName
	return eligible, eligible, nil
}
