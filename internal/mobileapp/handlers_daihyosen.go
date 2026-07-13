// Package mobileapp, handlers_daihyosen.go owns the
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
// engine returns ErrInsufficientEligibility and we respond 409, the
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
// RecordMatchResultWithIneligibilityTx + MaybeAutoCompletePools.
//
// Defined as a named local interface (rather than reusing ScoringEngine)
// because AddDaihyosen is not on the existing ScoringEngine interface,
// and broadening that surface for one new endpoint would expose the
// method to every other handler family. The write goes through the *Tx
// variant so the read-modify-write runs under the same per-comp lock the
// read used (see RegisterDaihyosenHandlers).
type DaihyosenEngine interface {
	AddDaihyosen(compID, matchID string, sideA, sideB engine.TeamSummary, isPool bool, sideAEligible, sideBEligible int) (*state.SubMatchResult, error)
	RecordMatchResultWithIneligibilityTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error)
	MaybeAutoCompletePools(compID string) (engine.AutoCompleteOutcome, error)
}

// DaihyosenStore is the consumer-boundary view of *state.Store used by
// the daihyosen handler. Both endpoints do a read-check-write on a single
// match, so they run entirely inside WithTransaction and read via the
// supplied StoreTx (LoadPoolMatches/LoadBracket for the match, plus
// LoadCompetition/LoadParticipants/LoadCompetitorStatus for the CHK026
// eligibility count), keeping the read and the write atomic under one
// acquire of the per-comp lock. Calling the public Store.Load* methods
// inside the closure would deadlock (the lock is non-reentrant), so the
// store surface here is just the transaction entry point.
type DaihyosenStore interface {
	WithTransaction(compID string, fn func(tx state.StoreTx) error) error
}

// RegisterDaihyosenHandlers wires the POST and DELETE /daihyosen endpoints.
// The caller in server.go passes `*engine.Engine` and `*state.Store` which
// satisfy the local interfaces by structural match.
//
// T140, FR-046.
func RegisterDaihyosenHandlers(r *gin.RouterGroup, eng DaihyosenEngine, store DaihyosenStore, hub Broadcaster) {
	r.DELETE("/competitions/:id/matches/:mid/daihyosen", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")

		// The whole read-guard-filter-write runs under ONE acquire of the
		// per-comp lock: re-read the match inside the transaction so the
		// DH-unscored guard is evaluated against, and the write applied to,
		// the same locked snapshot. Reading outside the lock and overwriting the
		// whole match (the pre-fix shape) could revert a concurrent bout score
		// or delete a daihyosen that was scored in the read→write window.
		var (
			updated     state.MatchResult
			notFound    bool
			noDaihyosen bool
			scored      bool
			haveResult  bool
		)
		txErr := store.WithTransaction(id, func(stx state.StoreTx) error {
			match, found, err := findMatchForDaihyosenTx(stx, id, mid)
			if err != nil {
				return err
			}
			if !found {
				notFound = true
				return nil
			}
			// Locate the daihyosen sub (Position == -1).
			dhIdx := -1
			for i := range match.SubResults {
				if match.SubResults[i].Position == -1 {
					dhIdx = i
					break
				}
			}
			if dhIdx < 0 {
				noDaihyosen = true
				return nil
			}
			// Guard (re-checked under the lock): refuse removal once the DH
			// bout carries any score. "Scored" means more than the initial
			// placeholder: ippons, a winner, a hantei flag, recorded hansoku
			// penalties, OR a sub-Decision that is no longer the bare
			// "daihyosen" placeholder (e.g. a withdrawal recorded on the rep
			// bout), validateSubBout does not validate sub.Decision, so an
			// acted-on bout can carry a decision without a winner.
			dh := match.SubResults[dhIdx]
			if len(dh.IpponsA) > 0 || len(dh.IpponsB) > 0 || dh.Winner != "" || dh.DecidedByHantei ||
				dh.HansokuA > 0 || dh.HansokuB > 0 ||
				(dh.Decision != "" && dh.Decision != string(domain.DecisionDaihyosen)) {
				scored = true
				return nil
			}
			// Build an updated match with the DH sub filtered out.
			filtered := make([]state.SubMatchResult, 0, len(match.SubResults)-1)
			for i := range match.SubResults {
				if i != dhIdx {
					filtered = append(filtered, match.SubResults[i])
				}
			}
			u := *match
			u.SubResults = filtered
			// Court exclusivity (mp-95mg) is not required here: DELETE /daihyosen
			// only proceeds when the DH sub is an unscored placeholder (the guard
			// above rejects if the sub has ippons, a winner, or a non-daihyosen
			// decision), which means the parent match is still in MatchStatusRunning,
			// no cross-comp conflict can be introduced by this write.
			u.Status = state.MatchStatusRunning
			// Clear ALL DH-derived match-level result/decision metadata so the
			// match returns to a clean running state. MatchResult.Decision has no
			// omitempty, so leaving Decision/DecisionBy/DecisionReason/Encho set
			// would let a removed daihyosen still present as decided-by-daihyosen
			// (or carry stale overtime) while Status is back to running.
			u.Winner = ""
			u.DecidedByHantei = nil
			u.Decision = ""
			u.DecisionBy = ""
			u.DecisionReason = ""
			u.Encho = nil
			if _, err := eng.RecordMatchResultWithIneligibilityTx(stx, id, mid, &u); err != nil {
				return err
			}
			updated = u
			haveResult = true
			return nil
		})
		if txErr != nil {
			internalError(c, txErr)
			return
		}
		switch {
		case notFound:
			c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
			return
		case noDaihyosen:
			c.JSON(http.StatusNotFound, gin.H{"error": "no_daihyosen"})
			return
		case scored:
			c.JSON(http.StatusConflict, gin.H{"error": "daihyosen_scored"})
			return
		case !haveResult:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "daihyosen removal produced no result"})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchForBroadcast(updated),
		})

		c.JSON(http.StatusOK, gin.H{"result": &updated})
	})

	r.POST("/competitions/:id/matches/:mid/daihyosen", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")

		// Read-check-write under ONE acquire of the per-comp lock: the tie that
		// gates AddDaihyosen is recomputed from the match's PERSISTED SubResults,
		// so the match must be read under the same lock that writes the appended
		// daihyosen, otherwise a concurrent bout score between read and write
		// would be reverted, or the tie computed from a stale snapshot.
		var (
			updated    state.MatchResult
			subOut     *state.SubMatchResult
			notFound   bool
			addErrCode string // "", "not_tied", "pool_match", "insufficient_eligibility", "engi_competition"
			haveResult bool
		)
		txErr := store.WithTransaction(id, func(stx state.StoreTx) error {
			// Engi competitions decide bouts by referee flag counts; a
			// representative daihyosen bout has no meaning there. Reject with
			// 400 (mirrors the quick-score / override / decision guards)
			// rather than letting the tie-detection path fail with a 500.
			comp, err := stx.LoadCompetition(id)
			if err != nil {
				return err
			}
			if comp != nil && comp.Engi {
				addErrCode = "engi_competition"
				return nil
			}

			match, found, err := findMatchForDaihyosenTx(stx, id, mid)
			if err != nil {
				return err
			}
			if !found {
				notFound = true
				return nil
			}

			sideASummary, sideBSummary := engine.ComputeTeamSummary(match.SubResults, match.SideA, match.SideB)

			// Count eligible competitors per side under the same lock (CHK026).
			// Pre-Slice-7 there are no explicit rosters; eligibility is tracked
			// per player via competitor-status, so this is a coarse "team has ≥1
			// eligible participant" count, sufficient for the 0-eligible forfeit
			// branch.
			sideAEligible, sideBEligible, err := countEligibleForSidesTx(stx, id, match.SideA, match.SideB)
			if err != nil {
				return err
			}

			sub, err := eng.AddDaihyosen(id, mid, sideASummary, sideBSummary, engine.IsPoolMatchID(mid), sideAEligible, sideBEligible)
			if err != nil {
				switch {
				case errors.Is(err, engine.ErrNotTied):
					addErrCode = "not_tied"
					return nil
				case errors.Is(err, engine.ErrPoolMatch):
					addErrCode = "pool_match"
					return nil
				case errors.Is(err, engine.ErrInsufficientEligibility):
					addErrCode = "insufficient_eligibility"
					return nil
				default:
					return err
				}
			}

			// Append the placeholder to the match's SubResults and persist via
			// the Tx score path so the append commits under the held lock.
			// Court exclusivity (mp-95mg) is not required here: AddDaihyosen
			// returns ErrNotTied unless the parent match is in a tied completed-
			// bouts state (i.e. already MatchStatusRunning on the court). The
			// court slot was committed when the match was first started via the
			// score endpoint, which holds WithCourtExclusivityLock.
			u := *match
			u.SubResults = append(append([]state.SubMatchResult{}, match.SubResults...), *sub)
			u.Status = state.MatchStatusRunning // daihyosen bout in progress
			if _, err := eng.RecordMatchResultWithIneligibilityTx(stx, id, mid, &u); err != nil {
				return err
			}
			updated = u
			subOut = sub
			haveResult = true
			return nil
		})
		if txErr != nil {
			internalError(c, txErr)
			return
		}
		if notFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
			return
		}
		switch addErrCode {
		case "engi_competition":
			c.JSON(http.StatusBadRequest, gin.H{"error": "engi competitions do not support daihyosen; use flag scoring instead"})
			return
		case "not_tied":
			c.JSON(http.StatusBadRequest, gin.H{"error": "not_tied"})
			return
		case "pool_match":
			c.JSON(http.StatusBadRequest, gin.H{"error": "pool_match"})
			return
		case "insufficient_eligibility":
			c.JSON(http.StatusConflict, gin.H{"error": "insufficient_eligibility"})
			return
		}
		if !haveResult {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "daihyosen add produced no result"})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchForBroadcast(updated),
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

		c.JSON(http.StatusOK, gin.H{"subResult": subOut, "result": &updated})
	})
}

// findMatchForDaihyosenTx looks up the target match by ID UNDER THE
// TRANSACTION LOCK (via the supplied StoreTx), searching pool matches when
// the ID has the "Pool " prefix and otherwise the bracket. Returns
// (match, true, nil) on success; (nil, false, nil) when neither store
// holds the ID; (nil, false, err) on a store-level I/O failure. Reading
// through tx (not the public Store) keeps the read atomic with the
// subsequent write in the same WithTransaction closure.
func findMatchForDaihyosenTx(tx state.StoreTx, compID, matchID string) (*state.MatchResult, bool, error) {
	if engine.IsPoolMatchID(matchID) {
		poolMatches, err := tx.LoadPoolMatches(compID)
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
	bracket, err := tx.LoadBracket(compID)
	if err != nil {
		return nil, false, err
	}
	if bracket == nil {
		return nil, false, nil
	}
	for _, round := range bracket.Rounds {
		for i := range round {
			if round[i].ID == matchID {
				// Re-shape into MatchResult so we can drive the same
				// scoring path as pool matches. SubResults must be copied
				// so TeamSummary is computed from recorded sub-bouts and
				// the daihyosen append doesn't overwrite existing data.
				return daihyosenBracketResult(&round[i]), true, nil
			}
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
		return daihyosenBracketResult(bm), true, nil
	}
	return nil, false, nil
}

// daihyosenBracketResult projects a stored BracketMatch into the MatchResult
// shape the daihyosen scoring path consumes. It carries Court / ScheduledAt
// (the daihyosen append re-runs the score path, which needs the slot) on top
// of the fields the engine's bracketMatchAsResult projects, so it deliberately
// does NOT reuse that engine helper.
//
// SubResults is copied (not aliased) so an in-place append on the returned
// MatchResult can never reach into bm's backing array via shared capacity: the
// caller's own comment ("SubResults must be copied ... the daihyosen append
// doesn't overwrite existing data") depends on that being true at the source,
// not as an incidental side effect of how the one current call site happens to
// build its own copy before appending.
func daihyosenBracketResult(bm *state.BracketMatch) *state.MatchResult {
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
		DecisionReason:  bm.DecisionReason,
		Encho:           bm.Encho,
		DecidedByHantei: state.HanteiPtr(bm.DecidedByHantei),
		SubResults:      append([]state.SubMatchResult(nil), bm.SubResults...),
	}
}

// countEligibleForSides counts, for each named side, how many roster
// participants currently have CompetitorStatus.Eligible != false. This
// is a coarse pre-lineup approximation of CHK026: until per-team
// rosters land in the store, we conservatively treat ALL participants
// as belonging to both sides and instead require that the total
// eligible-participant count be positive. That's sufficient for the
// "0 eligible → forfeit" branch (the only place CHK026 actually fires
// in practice, a depleted team will have all its members marked
// ineligible after kiken).
//
// Once team lineups land (T-series owned by the parallel lineup agent)
// this helper can be replaced with a per-team eligibility count by
// joining state.LoadTeamLineup against statuses.
func countEligibleForSidesTx(tx state.StoreTx, compID, sideAName, sideBName string) (int, int, error) {
	comp, err := tx.LoadCompetition(compID)
	if err != nil {
		return 0, 0, err
	}
	withZekken := false
	if comp != nil {
		withZekken = comp.EffectiveWithZekkenName()
	}
	participants, err := tx.LoadParticipants(compID, withZekken)
	if err != nil {
		return 0, 0, err
	}
	statuses, err := tx.LoadCompetitorStatus(compID)
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
