// Package engine, scoring_tx.go owns the tx-aware twins of
// RecordMatchResult / RecordMatchResultWithIneligibility /
// recordBracketMatchResult / recordIneligibilityFromDecision.
// They accept a state.StoreTx instead of reaching at e.store directly,
// so a caller (typically a HTTP handler) can run them inside a single
// Store.WithTransaction acquire of the per-comp write lock.
//
// Why these exist. Pre-T156 the score and decision handlers called
// engine methods that each acquired their own per-comp lock via
// UpdatePoolMatchByID / UpdateBracket / SetCompetitorStatus.
// The handler's logical "score this match" operation translated to
// 3-5 separate lock acquires, with concurrent writers free to land
// mutations in the gaps. The tx-aware twins collapse all of those into
// ONE acquire so the entire match-write + ineligibility-write sequence
// is indivisible.
//
// Constraint. Methods here MUST call only the tx parameter, NEVER
// e.store directly. The per-comp lock is non-reentrant (sync.RWMutex
// is not recursive on Lock by Lock); a direct e.store.Save* call from
// inside the closure passed to WithTransaction would deadlock.
//
// T156, NFR-010.
package engine

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// withPoolMatchTx is the tx-aware twin of withPoolMatch. Same
// "load + find + mutate + save" semantics; returns errMatchNotFound
// when no pool match has the given ID so callers can fall through to
// withBracketMatchTx.
func (e *Engine) withPoolMatchTx(tx state.StoreTx, compID, matchID string, mutate func(*state.MatchResult)) error {
	found, err := tx.UpdatePoolMatchByID(compID, matchID, mutate)
	if err != nil {
		return err
	}
	if !found {
		return errMatchNotFound
	}
	return nil
}

// recordBracketMatchResultTx is the tx-aware twin of
// recordBracketMatchResult. Identical body modulo the tx.UpdateBracket
// dispatch.
func (e *Engine) recordBracketMatchResultTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult) error {
	return tx.UpdateBracket(compID, func(bracket *state.Bracket) error {
		if bracket == nil {
			return notFoundErrorf("bracket not found for competition %s", compID)
		}

		found := false
		for rIdx, round := range bracket.Rounds {
			for mIdx, m := range round {
				if m.ID == matchID {
					if !bracketMatchPlayable(&bracket.Rounds[rIdx][mIdx]) {
						return validationErrorf("knockout match %s is not ready to score: a feeder pool or match has not finished", matchID)
					}
					// Match identity is fixed at seeding; a score must not
					// rewrite it. Backfill omitted sides; reject a non-empty
					// payload side that disagrees with the resolved pairing.
					if reconcileSides(result, m.SideA, m.SideB) {
						return ErrMatchSideMismatch
					}
					// Timestamp last-write-wins (mp-y3nk); see recordBracketMatchResult.
					if !applyBracketWrite(result, m.ModifiedAt) {
						found = true
						break
					}
					deriveDaihyosenWinner(result)
					bracket.Rounds[rIdx][mIdx].Winner = result.Winner
					status := result.Status
					if status == "" {
						status = state.MatchStatusCompleted
					}
					bracket.Rounds[rIdx][mIdx].Status = status
					if result.ModifiedAt != 0 {
						bracket.Rounds[rIdx][mIdx].ModifiedAt = result.ModifiedAt
					}
					bracket.Rounds[rIdx][mIdx].ScoreA = formatScore(result.IpponsA, result.HansokuA)
					bracket.Rounds[rIdx][mIdx].ScoreB = formatScore(result.IpponsB, result.HansokuB)
					bracket.Rounds[rIdx][mIdx].Decision = result.Decision
					bracket.Rounds[rIdx][mIdx].DecisionBy = result.DecisionBy
					bracket.Rounds[rIdx][mIdx].DecisionReason = result.DecisionReason
					bracket.Rounds[rIdx][mIdx].Encho = result.Encho
					if result.ResultSource != "" {
						bracket.Rounds[rIdx][mIdx].ResultSource = result.ResultSource
					}
					if result.CorrectionReason != "" {
						bracket.Rounds[rIdx][mIdx].CorrectionReason = result.CorrectionReason
					}
					// nil = omitted (preserve stored data); non-nil [] = explicit clear.
					if result.SubResults != nil {
						bracket.Rounds[rIdx][mIdx].SubResults = result.SubResults
					}
					// Project persisted sub-results back so the SSE/HTTP response
					// reflects committed state (see scoring.go for the full
					// rationale, mirrors the DecidedByHantei projection below).
					result.SubResults = bracket.Rounds[rIdx][mIdx].SubResults
					// See scoring.go for the DecidedByHantei *bool semantics.
					if result.DecidedByHantei != nil {
						bracket.Rounds[rIdx][mIdx].DecidedByHantei = *result.DecidedByHantei
					}
					// Project persisted flag back so the SSE/HTTP response
					// reflects committed state (see scoring.go for the full
					// rationale, nil-preserve would otherwise drop the
					// stored true from the same-turn response).
					result.DecidedByHantei = state.HanteiPtr(bracket.Rounds[rIdx][mIdx].DecidedByHantei)
					if result.Court == "" {
						result.Court = m.Court
					}
					if result.ScheduledAt == "" {
						result.ScheduledAt = m.ScheduledAt
					}
					found = true

					if status == state.MatchStatusCompleted {
						e.propagateBracketWinner(bracket, rIdx, mIdx)
					}
					break
				}
			}
			if found {
				break
			}
		}

		// Bronze (3rd-place) playoff lives in Bracket.ThirdPlaceMatch, outside
		// Rounds; resolve it here (twin of recordBracketMatchResult). No
		// propagation out of bronze.
		if !found && bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
			if err := applyBronzeMatchResult(bracket.ThirdPlaceMatch, result); err != nil {
				return err
			}
			found = true
		}

		if !found {
			return notFoundErrorf("bracket match %s not found", matchID)
		}
		return nil
	})
}

// recordIneligibilityFromDecisionTx is the tx-aware twin of
// recordIneligibilityFromDecision. The atomic check-and-set runs
// directly on the supplied tx (no nested WithTransaction), the caller
// already holds the per-comp lock, so the same check-after-write
// guarantee holds without re-acquiring.
func (e *Engine) recordIneligibilityFromDecisionTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error) {
	if result == nil {
		return nil, nil
	}
	if !domain.IsKikenDecisionStr(result.Decision) && result.Decision != string(domain.DecisionFusenpai) {
		return nil, nil
	}
	loser := loserSideName(result)
	if loser == "" {
		return nil, nil
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, nil
	}
	// Engi competitions force the zekken layout (two-column participant CSV)
	// so participants must be parsed with WithZekkenName even when the comp
	// flag is not set via user input; make the effective flag explicit (Finding 10).
	participants, err := tx.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return nil, err
	}
	pool := combinedPlayerPool(comp.Players, participants)
	playerID := lookupPlayerID(pool, loser)
	if playerID == "" {
		return nil, nil
	}
	status := domain.CompetitorStatus{
		PlayerID:      playerID,
		Eligible:      false,
		Reinstateable: result.Decision == string(domain.DecisionKikenInjury),
		Reason:        fmt.Sprintf("%s at %s", result.Decision, matchID),
		MatchID:       matchID,
		RecordedAt:    time.Now().UTC(),
	}
	// K2/CHK047: check-and-set under the existing lock. Pre-T156 this
	// reached for a fresh WithTransaction; under T156 the surrounding
	// tx already holds the per-comp lock, so we serialize correctly
	// without nesting.
	statuses, err := tx.LoadCompetitorStatus(compID)
	if err != nil {
		return nil, err
	}
	if st, ok := statuses[playerID]; ok && !st.Eligible && st.MatchID != matchID {
		return nil, &AlreadyIneligibleError{
			PlayerID: playerID,
			MatchID:  st.MatchID,
			Reason:   st.Reason,
		}
	}
	if err := tx.SetCompetitorStatus(compID, status); err != nil {
		return nil, err
	}
	return &status, nil
}

// RecordMatchResultWithIneligibilityTx is the tx-aware twin of
// RecordMatchResultWithIneligibility. The K3/CHK047 partial-write
// rollback path replays the prior result via the same tx so the
// rollback also runs inside the single lock acquire.
//
// On AlreadyIneligibleError the caller's WithTransaction body should
// propagate the error through to the handler, there's no need (and no
// way) to roll back via a separate tx here because the rollback write
// is part of THIS tx's mutations.
//
// T156.
func (e *Engine) RecordMatchResultWithIneligibilityTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error) {
	result.ID = matchID

	// Engi dispatch seam (tx-aware): a flag-scored competition records via the
	// engi slice through the SAME tx so the write stays inside the caller's
	// single per-comp lock acquire. Engi has no eligibility concept, so the
	// status return is nil.
	comp, loadErr := tx.LoadCompetition(compID)
	if loadErr != nil {
		return nil, fmt.Errorf("RecordMatchResultWithIneligibilityTx: load competition %s: %w", compID, loadErr)
	}
	if comp != nil && comp.Engi {
		rec, recErr := e.recordEngiMatchResultTx(tx, compID, matchID, result.FlagsA, result.FlagsB, result.CorrectionReason)
		if recErr != nil {
			return nil, recErr
		}
		backfillEngiResult(result, rec)
		return nil, nil
	}

	applyHansokuIppons(result)
	deriveDaihyosenWinner(result)

	// Capture the prior result so we can roll back the score on
	// AlreadyIneligibleError. lookupExistingResultTx reads directly from
	// the tx so it sees the state INSIDE the lock (the on-disk state
	// hasn't moved under us, we hold the lock).
	prior, _ := e.lookupExistingResultTx(tx, compID, matchID)

	// mp-e2k1: For mixed competitions, capture the pre-write standings for
	// the match's pool so we can compare after the write and detect whether
	// any qualifying finisher would be displaced from a started knockout match.
	// We only need this for re-scores (prior != nil) in mixed comps.
	// NOTE: the engi early-return above ensures this block only runs for
	// non-engi competitions, so compIsEngi is always false here and is not
	// tracked as a variable.
	var (
		poolRescoredName string   // pool this match belongs to (empty = not a pool match)
		oldTopN          []string // qualifying finisher names BEFORE the write
		poolWinners      int      // EffectivePoolWinners, captured so the post-write block needn't reload the comp
	)
	if prior != nil {
		// mp-e2k1: reuse the comp already loaded (and error-checked) at the
		// engi-dispatch above rather than re-reading config.md from disk — the
		// tx sees no pending config write, so a reload would just re-parse the
		// same bytes. The load error is already returned there, so this path is
		// still fail-closed; a nil comp skips the mixed guard as before.
		if comp != nil && comp.Format == state.CompFormatMixed {
			// Only actual pool matches ("Pool X-…") can change pool finishers.
			// Gate on IsPoolMatchID so a knockout re-score ("m-rN-i"), whose ID
			// would otherwise parse as a pool via poolNameFromMatchID's trailing
			// "-<digits>" rule, skips the standings pre-read entirely.
			if pn, ok := poolNameFromMatchID(matchID); ok && IsPoolMatchID(matchID) {
				poolRescoredName = pn
				poolWinners = comp.EffectivePoolWinners()
				// Fail closed: if we can't establish the pre-write finishers we
				// can't prove the re-score is safe, so abort before writing
				// anything (nothing is staged yet, so returning aborts cleanly).
				preStandings, sErr := e.computeStandingsFrom(tx, compID)
				if sErr != nil {
					return nil, fmt.Errorf("mp-e2k1: pre-write standings for %s pool %q: %w", compID, pn, sErr)
				}
				ps := preStandings[pn]
				for i := 0; i < poolWinners && i < len(ps); i++ {
					oldTopN = append(oldTopN, ps[i].Player.Name)
				}
			}
		}
	}

	var sideMismatch bool
	err := e.withPoolMatchTx(tx, compID, matchID, func(r *state.MatchResult) {
		if reconcileSides(result, r.SideA, r.SideB) {
			sideMismatch = true
			return // leave the stored match untouched
		}
		// Preserve generation-time participant ids + resolve winner id across
		// the whole-struct overwrite below (the /score endpoint scores through
		// this Tx path). See backfillMatchIdentity.
		backfillMatchIdentity(result, r)
		if result.Court == "" {
			result.Court = r.Court
		}
		if result.ScheduledAt == "" {
			result.ScheduledAt = r.ScheduledAt
		}
		result.Round = r.Round
		*r = *result
	})
	if err != nil {
		if !errors.Is(err, errMatchNotFound) {
			return nil, err
		}
		if err := e.recordBracketMatchResultTx(tx, compID, matchID, result); err != nil {
			return nil, err
		}
	} else if sideMismatch {
		// Match identity is fixed at generation; a score payload naming
		// different competitors is rejected (HTTP 409) rather than allowed to
		// overwrite the stored pairing. Returns before any side-effect write.
		return nil, ErrMatchSideMismatch
	}

	// mp-e2k1 guard: after the pool-match write, check whether any
	// qualifying finisher changed. If a displaced finisher already appears
	// in a started/completed bracket match, reject the re-score.
	if err == nil && poolRescoredName != "" && len(oldTopN) > 0 {
		// Fail closed on any verification-read failure past this point: the
		// forward write is already staged, so we restore prior before returning
		// the error, never silently commit a re-score we couldn't prove safe.
		postStandings, sErr := e.computeStandingsFrom(tx, compID)
		if sErr != nil {
			e.rollbackMatchResultTx(tx, compID, matchID, prior)
			return nil, fmt.Errorf("mp-e2k1: post-write standings for %s pool %q: %w", compID, poolRescoredName, sErr)
		}
		ps := postStandings[poolRescoredName]
		// Build new top-N set and find displaced names. poolWinners was
		// captured pre-write, the competition record can't change within
		// this tx, so no reload is needed.
		newSet := make(map[string]struct{}, poolWinners)
		for i := 0; i < poolWinners && i < len(ps); i++ {
			newSet[ps[i].Player.Name] = struct{}{}
		}
		var displaced []string
		for _, name := range oldTopN {
			if _, stillIn := newSet[name]; !stillIn {
				displaced = append(displaced, name)
			}
		}
		if len(displaced) > 0 {
			blockingFinisher, knockoutMatchID, hErr := e.hasStartedKnockoutMatchTx(tx, compID, displaced)
			if hErr != nil {
				e.rollbackMatchResultTx(tx, compID, matchID, prior)
				return nil, fmt.Errorf("mp-e2k1: checking started knockout matches for %s: %w", compID, hErr)
			}
			if blockingFinisher != "" {
				// Reject: restore the prior result so the corrupting re-score
				// never lands. Within a tx, writes are in-memory WAL intents
				// coalesced last-write-wins, so this rollback supersedes the
				// forward write before Commit applies the final state. Report the
				// finisher actually sitting in the blocking match so Finisher and
				// MatchID stay consistent (matters when poolWinners > 1).
				e.rollbackMatchResultTx(tx, compID, matchID, prior)
				return nil, &DownstreamKnockoutScoredError{
					Pool:     poolRescoredName,
					Finisher: blockingFinisher,
					MatchID:  knockoutMatchID,
				}
			}
		}
	}

	status, err := e.recordIneligibilityFromDecisionTx(tx, compID, matchID, result)
	if err != nil {
		var alreadyErr *AlreadyIneligibleError
		if errors.As(err, &alreadyErr) {
			// K3/CHK047: roll back the partial score-write within the
			// same tx. The pool/bracket mutation already landed on disk,
			// but the intended loser is already ineligible from a
			// different match, revert before returning 409.
			if prior != nil {
				e.rollbackMatchResultTx(tx, compID, matchID, prior)
			}
			return nil, err
		}
		log.Printf("engine: recordIneligibilityFromDecisionTx compId=%s matchId=%s: %v", compID, matchID, err)
		return nil, nil
	}
	return status, nil
}

// rollbackMatchResultTx restores prior over a partial score-write within the
// same transaction. Shared by the two reject paths in
// RecordMatchResultWithIneligibilityTx: K3 (AlreadyIneligible) and mp-e2k1
// (downstream knockout already scored). Within a tx, writes are in-memory WAL
// intents coalesced last-write-wins, so this restore supersedes the forward
// write before Commit applies the final state. prior must be non-nil.
//
// It normalizes two nil-collision fields before restoring:
//   - SubResults nil → explicit empty slice, so recordBracketMatchResultTx
//     treats it as "clear sub-results" rather than leaving the partial write.
//   - DecidedByHantei nil → explicit false: lookupExistingResultTx projects the
//     flag through HanteiPtr, which collapses a stored false to nil; nil would
//     hit the nil-preserve branch and leave the partial hantei flag in place.
func (e *Engine) rollbackMatchResultTx(tx state.StoreTx, compID, matchID string, prior *state.MatchResult) {
	if prior.SubResults == nil {
		prior.SubResults = []state.SubMatchResult{}
	}
	if prior.DecidedByHantei == nil {
		clearHantei := false
		prior.DecidedByHantei = &clearHantei
	}
	if rerr := e.recordMatchResultTx(tx, compID, matchID, prior); rerr != nil {
		log.Printf("engine: RecordMatchResultWithIneligibilityTx rollback failed compId=%s matchId=%s: %v", compID, matchID, rerr)
	}
}

// recordMatchResultTx is the tx-aware twin of RecordMatchResult. Used
// exclusively by the K3 partial-write rollback inside
// RecordMatchResultWithIneligibilityTx, the prior result is restored
// byte-for-byte, so applyHansokuIppons is intentionally skipped here.
func (e *Engine) recordMatchResultTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult) error {
	result.ID = matchID
	err := e.withPoolMatchTx(tx, compID, matchID, func(r *state.MatchResult) {
		// Identity reconciliation only backfills here: this path restores a
		// trusted prior snapshot (K3 rollback), not a client payload, so the
		// stored sides always match, the mismatch result is intentionally
		// ignored rather than turned into a rejection.
		_ = reconcileSides(result, r.SideA, r.SideB)
		// Preserve generation-time participant ids + resolve winner id across
		// the whole-struct overwrite below (the /score endpoint scores through
		// this Tx path). See backfillMatchIdentity.
		backfillMatchIdentity(result, r)
		if result.Court == "" {
			result.Court = r.Court
		}
		if result.ScheduledAt == "" {
			result.ScheduledAt = r.ScheduledAt
		}
		result.Round = r.Round
		*r = *result
	})
	if err != nil {
		if !errors.Is(err, errMatchNotFound) {
			return err
		}
		if err := e.recordBracketMatchResultTx(tx, compID, matchID, result); err != nil {
			return err
		}
	}
	if _, err := e.recordIneligibilityFromDecisionTx(tx, compID, matchID, result); err != nil {
		log.Printf("engine: recordIneligibilityFromDecisionTx compId=%s matchId=%s: %v", compID, matchID, err)
	}
	return nil
}

// lookupExistingResultTx is the tx-aware twin of lookupExistingResult.
// Reads pool matches first, falls through to bracket on
// errMatchNotFound (NotFoundError), same shape the non-tx path
// returns.
func (e *Engine) lookupExistingResultTx(tx state.StoreTx, compID, matchID string) (*state.MatchResult, error) {
	poolMatches, err := tx.LoadPoolMatches(compID)
	if err == nil {
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				r := poolMatches[i]
				return &r, nil
			}
		}
	}
	bracket, err := tx.LoadBracket(compID)
	if err == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for i := range round {
				if round[i].ID == matchID {
					return bracketMatchAsResult(&round[i]), nil
				}
			}
		}
		if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
			return bracketMatchAsResult(bracket.ThirdPlaceMatch), nil
		}
	}
	return nil, notFoundErrorf("match %q not found in competition %q", matchID, compID)
}

// lookupMatchSidesTx is the tx-aware twin of lookupMatchSides.
func (e *Engine) lookupMatchSidesTx(tx state.StoreTx, compID, matchID string) (string, string, error) {
	poolMatches, err := tx.LoadPoolMatches(compID)
	if err == nil {
		for _, m := range poolMatches {
			if m.ID == matchID {
				return m.SideA, m.SideB, nil
			}
		}
	}
	bracket, err := tx.LoadBracket(compID)
	if err == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID {
					return bm.SideA, bm.SideB, nil
				}
			}
		}
		if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
			return bracket.ThirdPlaceMatch.SideA, bracket.ThirdPlaceMatch.SideB, nil
		}
	}
	return "", "", notFoundErrorf("match %q not found in competition %q", matchID, compID)
}

// StartMatchTx is the tx-aware FR-035 gate. Same contract as
// StartMatch: returns *IneligibleCompetitorError when any participant
// in matchID is marked ineligible from a *different* match or is
// currently Running in a different match (Phase 2c simultaneity gate).
// The undo-path is permitted (status with MatchID==matchID is skipped).
//
// The score handler wraps RecordMatchResultWithIneligibilityTx with
// this check so a fought / hikiwake score on a match whose
// participants include someone previously ineligible is rejected
// before any disk write. Kiken/fusenpai decisions go through
// RecordDecisionTx, which intentionally bypasses this gate, they ARE
// the act of recording a new withdrawal.
func (e *Engine) StartMatchTx(tx state.StoreTx, compID, matchID string) error {
	if err := e.checkCourtExclusivityTx(tx, compID, matchID); err != nil {
		return err
	}
	if err := e.checkSimultaneousMatchTx(tx, compID, matchID); err != nil {
		return err
	}
	sideA, sideB, err := e.lookupMatchSidesTx(tx, compID, matchID)
	if err != nil {
		return err
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil {
		return err
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := tx.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return err
	}
	pool := combinedPlayerPool(comp.Players, participants)
	ids := []string{lookupPlayerID(pool, sideA), lookupPlayerID(pool, sideB)}

	statuses, err := tx.LoadCompetitorStatus(compID)
	if err != nil {
		return err
	}
	for _, pid := range ids {
		if pid == "" {
			continue
		}
		if st, ok := statuses[pid]; ok && !st.Eligible && st.MatchID != matchID {
			return &IneligibleCompetitorError{PlayerID: pid, Reason: st.Reason}
		}
	}
	return nil
}

// checkSimultaneousMatchTx is the tx-aware twin of checkSimultaneousMatch.
// Returns *IneligibleCompetitorError if either participant in matchID is
// currently Running in a different match within the same competition.
//
// Phase 2c simultaneity gate.
func (e *Engine) checkSimultaneousMatchTx(tx state.StoreTx, compID, matchID string) error {
	sideA, sideB, err := e.lookupMatchSidesTx(tx, compID, matchID)
	if err != nil {
		return nil
	}
	if sideA == "" && sideB == "" {
		return nil
	}

	idA, idB := resolvePlayerIDsTx(tx, compID, sideA, sideB)

	poolMatches, err := tx.LoadPoolMatches(compID)
	if err == nil {
		for _, m := range poolMatches {
			if m.ID == matchID || m.Status != state.MatchStatusRunning {
				continue
			}
			if sideA != "" && (m.SideA == sideA || m.SideB == sideA) {
				return &IneligibleCompetitorError{
					PlayerID: idA,
					Reason:   fmt.Sprintf("already fighting in match %s on court %s", m.ID, m.Court),
				}
			}
			if sideB != "" && (m.SideA == sideB || m.SideB == sideB) {
				return &IneligibleCompetitorError{
					PlayerID: idB,
					Reason:   fmt.Sprintf("already fighting in match %s on court %s", m.ID, m.Court),
				}
			}
		}
	}

	bracket, berr := tx.LoadBracket(compID)
	if berr == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID || bm.Status != state.MatchStatusRunning {
					continue
				}
				if sideA != "" && (bm.SideA == sideA || bm.SideB == sideA) {
					return &IneligibleCompetitorError{
						PlayerID: idA,
						Reason:   fmt.Sprintf("already fighting in match %s on court %s", bm.ID, bm.Court),
					}
				}
				if sideB != "" && (bm.SideA == sideB || bm.SideB == sideB) {
					return &IneligibleCompetitorError{
						PlayerID: idB,
						Reason:   fmt.Sprintf("already fighting in match %s on court %s", bm.ID, bm.Court),
					}
				}
			}
		}
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID != matchID && bm.Status == state.MatchStatusRunning {
			if sideA != "" && (bm.SideA == sideA || bm.SideB == sideA) {
				return &IneligibleCompetitorError{
					PlayerID: idA,
					Reason:   fmt.Sprintf("already fighting in match %s on court %s", bm.ID, bm.Court),
				}
			}
			if sideB != "" && (bm.SideA == sideB || bm.SideB == sideB) {
				return &IneligibleCompetitorError{
					PlayerID: idB,
					Reason:   fmt.Sprintf("already fighting in match %s on court %s", bm.ID, bm.Court),
				}
			}
		}
	}

	return nil
}

// checkCourtExclusivityTx checks that no OTHER match in compID's own pool or
// bracket is already running on the same court. The cross-competition check is
// intentionally omitted here: calling store.RunningMatchOnCourt (which acquires
// read locks on other competitions) while holding compID's write lock via
// WithTransaction risks a circular-wait deadlock if another competition is
// simultaneously in its own WithTransaction. The cross-competition check is
// performed by CheckCrossCompCourtBusy before WithTransaction is entered.
func (e *Engine) checkCourtExclusivityTx(tx state.StoreTx, compID, matchID string) error {
	court, err := lookupMatchCourtTx(tx, compID, matchID)
	if err != nil {
		return err
	}
	if court == "" {
		return nil
	}
	occ, err := courtOccupiedInCompTx(tx, compID, court, matchID)
	if err != nil {
		return err
	}
	if occ != nil {
		return &CourtBusyError{Court: court, MatchID: occ.MatchID, CompID: occ.CompID}
	}
	return nil
}

func lookupMatchCourtTx(tx state.StoreTx, compID, matchID string) (string, error) {
	poolMatches, err := tx.LoadPoolMatches(compID)
	if err != nil {
		return "", err
	}
	for _, m := range poolMatches {
		if m.ID == matchID {
			return m.Court, nil
		}
	}
	bracket, err := tx.LoadBracket(compID)
	if err != nil {
		return "", err
	}
	if bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID {
					return bm.Court, nil
				}
			}
		}
		if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
			return bracket.ThirdPlaceMatch.Court, nil
		}
	}
	return "", notFoundErrorf("match %q not found in competition %q", matchID, compID)
}

// courtOccupiedInCompTx scans compID's pool matches and bracket (via tx)
// for any match, other than skipMatchID, that is Running on court.
func courtOccupiedInCompTx(tx state.StoreTx, compID, court, skipMatchID string) (*state.CourtOccupancy, error) {
	poolMatches, err := tx.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	for _, m := range poolMatches {
		if m.ID == skipMatchID || m.Status != state.MatchStatusRunning {
			continue
		}
		if m.Court == court {
			return &state.CourtOccupancy{CompID: compID, MatchID: m.ID}, nil
		}
	}
	bracket, err := tx.LoadBracket(compID)
	if err != nil {
		return nil, err
	}
	if bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == skipMatchID || bm.Status != state.MatchStatusRunning {
					continue
				}
				if bm.Court == court {
					return &state.CourtOccupancy{CompID: compID, MatchID: bm.ID}, nil
				}
			}
		}
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID != skipMatchID && bm.Status == state.MatchStatusRunning && bm.Court == court {
			return &state.CourtOccupancy{CompID: compID, MatchID: bm.ID}, nil
		}
	}
	return nil, nil
}

func resolvePlayerIDsTx(tx state.StoreTx, compID, sideA, sideB string) (string, string) {
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil {
		return sideA, sideB
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := tx.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return sideA, sideB
	}
	pool := combinedPlayerPool(comp.Players, participants)
	idA := lookupPlayerID(pool, sideA)
	if idA == "" {
		idA = sideA
	}
	idB := lookupPlayerID(pool, sideB)
	if idB == "" {
		idB = sideB
	}
	return idA, idB
}

// checkConcurrentIneligibilityTx is the tx-aware twin of
// checkConcurrentIneligibility. Same logic, same "log and skip on
// lookup failure" behaviour, the T105 guard is best-effort, the
// canonical check-and-set inside recordIneligibilityFromDecisionTx is
// the load-bearing serialisation point.
func (e *Engine) checkConcurrentIneligibilityTx(tx state.StoreTx, compID, matchID, loserName string) error {
	if loserName == "" {
		return nil
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil {
		if err != nil {
			log.Printf("engine: checkConcurrentIneligibilityTx LoadCompetition compId=%s: %v (T105 guard skipped)", compID, err)
		}
		return nil
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := tx.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		log.Printf("engine: checkConcurrentIneligibilityTx LoadParticipants compId=%s: %v (T105 guard skipped)", compID, err)
		return nil
	}
	pool := combinedPlayerPool(comp.Players, participants)
	playerID := lookupPlayerID(pool, loserName)
	if playerID == "" {
		return nil
	}
	statuses, err := tx.LoadCompetitorStatus(compID)
	if err != nil {
		log.Printf("engine: checkConcurrentIneligibilityTx LoadCompetitorStatus compId=%s: %v (T105 guard skipped)", compID, err)
		return nil
	}
	if st, ok := statuses[playerID]; ok && !st.Eligible && st.MatchID != matchID {
		return &AlreadyIneligibleError{
			PlayerID: playerID,
			MatchID:  st.MatchID,
			Reason:   st.Reason,
		}
	}
	return nil
}

// hasDownstreamMatchStartedTx is the tx-aware twin of
// hasDownstreamMatchStarted. Same logic; reads through the tx so the
// state seen here is the state inside the lock the caller already
// holds.
func (e *Engine) hasDownstreamMatchStartedTx(tx state.StoreTx, compID string, playerNames []string, excludeMatchID string) (bool, error) {
	wantSet := make(map[string]struct{}, len(playerNames))
	for _, n := range playerNames {
		if n != "" {
			wantSet[n] = struct{}{}
		}
	}
	if len(wantSet) == 0 {
		return false, nil
	}
	involvesAny := func(a, b string) bool {
		if _, ok := wantSet[a]; ok {
			return true
		}
		_, ok := wantSet[b]
		return ok
	}
	isStarted := func(s state.MatchStatus) bool {
		return s == state.MatchStatusRunning || s == state.MatchStatusCompleted
	}
	poolMatches, err := tx.LoadPoolMatches(compID)
	if err == nil {
		for _, m := range poolMatches {
			if m.ID == excludeMatchID {
				continue
			}
			if isStarted(m.Status) && involvesAny(m.SideA, m.SideB) {
				return true, nil
			}
		}
	}
	bracket, err := tx.LoadBracket(compID)
	if err == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == excludeMatchID {
					continue
				}
				if isStarted(bm.Status) && involvesAny(bm.SideA, bm.SideB) {
					return true, nil
				}
			}
		}
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID != excludeMatchID {
			if isStarted(bm.Status) && involvesAny(bm.SideA, bm.SideB) {
				return true, nil
			}
		}
	}
	return false, nil
}

// hasStartedKnockoutMatchTx reports whether any BRACKET (knockout) match
// with status running or completed currently lists one of playerNames as a
// side. This is the bracket-only twin of hasDownstreamMatchStartedTx,
// pool matches are intentionally NOT scanned because a pool finisher
// legitimately appears in their own completed pool bouts, which must NOT
// trip the guard.
//
// mp-e2k1.
func (e *Engine) hasStartedKnockoutMatchTx(tx state.StoreTx, compID string, playerNames []string) (matchedName, matchID string, err error) {
	wantSet := make(map[string]struct{}, len(playerNames))
	for _, n := range playerNames {
		if n != "" {
			wantSet[n] = struct{}{}
		}
	}
	if len(wantSet) == 0 {
		return "", "", nil
	}
	// matchedSide returns the displaced name found on this match (a or b), or ""
	// if neither side is one of the displaced finishers. Returning the name keeps
	// the caller's error payload consistent: the reported Finisher is the one
	// actually sitting in the blocking match, not just displaced[0].
	matchedSide := func(a, b string) string {
		if _, ok := wantSet[a]; ok {
			return a
		}
		if _, ok := wantSet[b]; ok {
			return b
		}
		return ""
	}
	isStarted := func(s state.MatchStatus) bool {
		return s == state.MatchStatusRunning || s == state.MatchStatusCompleted
	}
	bracket, err := tx.LoadBracket(compID)
	if err != nil {
		// A genuinely absent bracket is NOT an error, LoadBracket maps a
		// missing file to an empty bracket with nil error (parseBracketFile,
		// os.IsNotExist). So a non-nil error here is a real fault (corrupt
		// bracket.json, permission/IO error). Propagate it rather than treating
		// it as "no started knockout match", which would let the caller's guard
		// fail open and allow a re-score that should be blocked.
		return "", "", err
	}
	if bracket == nil {
		return "", "", nil
	}
	for _, round := range bracket.Rounds {
		for _, bm := range round {
			if !isStarted(bm.Status) {
				continue
			}
			if name := matchedSide(bm.SideA, bm.SideB); name != "" {
				return name, bm.ID, nil
			}
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil && isStarted(bm.Status) {
		if name := matchedSide(bm.SideA, bm.SideB); name != "" {
			return name, bm.ID, nil
		}
	}
	return "", "", nil
}

// restoreCompetitorEligibilityTx is the tx-aware twin of
// restoreCompetitorEligibility.
func (e *Engine) restoreCompetitorEligibilityTx(tx state.StoreTx, compID, priorLoser, matchID string) (*domain.CompetitorStatus, error) {
	if priorLoser == "" {
		return nil, nil
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, nil
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := tx.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return nil, err
	}
	pool := combinedPlayerPool(comp.Players, participants)
	playerID := lookupPlayerID(pool, priorLoser)
	if playerID == "" {
		return nil, nil
	}
	status := domain.CompetitorStatus{
		PlayerID:   playerID,
		Eligible:   true,
		MatchID:    matchID,
		RecordedAt: time.Now().UTC(),
	}
	if err := tx.SetCompetitorStatus(compID, status); err != nil {
		return nil, err
	}
	return &status, nil
}

// RecordDecisionTx is the tx-aware twin of RecordDecision. Same
// contract, auto-fills the scoreline, runs the T103 lock + T105
// concurrent-kiken checks, persists the result, restores prior-loser
// eligibility on undo, all inside ONE per-comp lock acquire.
//
// T156.
func (e *Engine) RecordDecisionTx(tx state.StoreTx, compID, matchID, decision, decisionBy, decisionReason string, encho *state.EnchoMetadata, force bool) (*state.MatchResult, *domain.CompetitorStatus, error) {
	if decisionBy != "shiro" && decisionBy != "aka" {
		return nil, nil, validationErrorf("decisionBy must be 'shiro' or 'aka', got %q", decisionBy)
	}
	sideA, sideB, err := e.lookupMatchSidesTx(tx, compID, matchID)
	if err != nil {
		return nil, nil, err
	}
	loserName := sideB
	if decisionBy == "aka" {
		loserName = sideA
	}
	if domain.IsKikenDecisionStr(decision) || decision == string(domain.DecisionFusenpai) {
		if cerr := e.checkConcurrentIneligibilityTx(tx, compID, matchID, loserName); cerr != nil {
			return nil, nil, cerr
		}
	}
	prior, err := e.lookupExistingResultTx(tx, compID, matchID)
	if err != nil {
		return nil, nil, err
	}
	priorLoser := ""
	if prior != nil && (domain.IsKikenDecisionStr(prior.Decision) || prior.Decision == string(domain.DecisionFusenpai)) {
		priorLoser = loserSideName(prior)
	}
	if priorLoser != "" && !force {
		started, err := e.hasDownstreamMatchStartedTx(tx, compID, []string{sideA, sideB}, matchID)
		if err != nil {
			return nil, nil, err
		}
		if started {
			return nil, nil, ErrDecisionLocked
		}
	}
	winningCount := 2
	if encho != nil {
		winningCount = 1
	}
	winIppons := make([]string, winningCount)
	for i := range winIppons {
		winIppons[i] = defaultWinIppon
	}
	result := &state.MatchResult{
		ID:             matchID,
		SideA:          sideA,
		SideB:          sideB,
		Decision:       decision,
		DecisionBy:     decisionBy,
		DecisionReason: decisionReason,
		Encho:          encho,
		Status:         state.MatchStatusCompleted,
	}
	if decisionBy == "shiro" {
		result.IpponsA = winIppons
		result.Winner = sideA
	} else {
		result.IpponsB = winIppons
		result.Winner = sideB
	}
	status, err := e.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, result)
	if err != nil {
		return nil, nil, err
	}
	if priorLoser != "" {
		newLoser := loserSideName(result)
		if priorLoser != newLoser {
			restored, rerr := e.restoreCompetitorEligibilityTx(tx, compID, priorLoser, matchID)
			if rerr == nil && restored != nil {
				status = restored
			}
		}
	}
	return result, status, nil
}
