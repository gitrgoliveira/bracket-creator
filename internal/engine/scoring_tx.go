// Package engine — scoring_tx.go owns the tx-aware twins of
// RecordMatchResult / RecordMatchResultWithIneligibility /
// recordBracketMatchResult / recordIneligibilityFromDecision /
// maybeLockTeamLineupsForRound. They accept a state.StoreTx instead of
// reaching at e.store directly, so a caller (typically a HTTP handler)
// can run them inside a single Store.WithTransaction acquire of the
// per-comp write lock.
//
// Why these exist. Pre-T156 the score and decision handlers called
// engine methods that each acquired their own per-comp lock via
// UpdatePoolMatchByID / UpdateBracket / SetCompetitorStatus /
// LockTeamLineupsForRound. The handler's logical "score this match"
// operation translated to 3-5 separate lock acquires, with concurrent
// writers free to land mutations in the gaps. The tx-aware twins
// collapse all of those into ONE acquire so the entire
// match-write + ineligibility-write + lineup-freeze sequence is
// indivisible.
//
// Constraint. Methods here MUST call only the tx parameter — NEVER
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
					if result.SideA == "" {
						result.SideA = m.SideA
					}
					if result.SideB == "" {
						result.SideB = m.SideB
					}
					deriveDaihyosenWinner(result)
					bracket.Rounds[rIdx][mIdx].Winner = result.Winner
					status := result.Status
					if status == "" {
						status = state.MatchStatusCompleted
					}
					bracket.Rounds[rIdx][mIdx].Status = status
					bracket.Rounds[rIdx][mIdx].ScoreA = formatScore(result.IpponsA, result.HansokuA)
					bracket.Rounds[rIdx][mIdx].ScoreB = formatScore(result.IpponsB, result.HansokuB)
					bracket.Rounds[rIdx][mIdx].Decision = result.Decision
					bracket.Rounds[rIdx][mIdx].DecisionBy = result.DecisionBy
					bracket.Rounds[rIdx][mIdx].DecisionReason = result.DecisionReason
					bracket.Rounds[rIdx][mIdx].Encho = result.Encho
					// See scoring.go for the full-replacement contract note on
					// DecidedByHantei and non-pointer bool semantics.
					bracket.Rounds[rIdx][mIdx].DecidedByHantei = result.DecidedByHantei
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

		if !found {
			return notFoundErrorf("bracket match %s not found", matchID)
		}
		return nil
	})
}

// recordIneligibilityFromDecisionTx is the tx-aware twin of
// recordIneligibilityFromDecision. The atomic check-and-set runs
// directly on the supplied tx (no nested WithTransaction) — the caller
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
	participants, err := tx.LoadParticipants(compID, comp.WithZekkenName)
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

// maybeLockTeamLineupsForRoundTx is the tx-aware twin of
// maybeLockTeamLineupsForRound. Same gating logic, same "log and
// swallow" failure mode — the score-write has already landed and we
// don't want to fail the request on a side-effect failure.
func (e *Engine) maybeLockTeamLineupsForRoundTx(tx state.StoreTx, compID string, result *state.MatchResult) {
	if result == nil {
		return
	}
	if result.Status != state.MatchStatusRunning && result.Status != state.MatchStatusCompleted {
		return
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil || comp.TeamSize <= 0 {
		return
	}
	const round = 0
	if err := tx.LockTeamLineupsForRound(compID, round, time.Now().UTC()); err != nil {
		log.Printf("engine: LockTeamLineupsForRound compId=%s round=%d: %v", compID, round, err)
	}
}

// RecordMatchResultWithIneligibilityTx is the tx-aware twin of
// RecordMatchResultWithIneligibility. The K3/CHK047 partial-write
// rollback path replays the prior result via the same tx so the
// rollback also runs inside the single lock acquire.
//
// On AlreadyIneligibleError the caller's WithTransaction body should
// propagate the error through to the handler — there's no need (and no
// way) to roll back via a separate tx here because the rollback write
// is part of THIS tx's mutations.
//
// T156.
func (e *Engine) RecordMatchResultWithIneligibilityTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error) {
	result.ID = matchID
	applyHansokuIppons(result)
	deriveDaihyosenWinner(result)

	// Capture the prior result so we can roll back the score on
	// AlreadyIneligibleError. lookupExistingResultTx reads directly from
	// the tx so it sees the state INSIDE the lock (the on-disk state
	// hasn't moved under us — we hold the lock).
	prior, _ := e.lookupExistingResultTx(tx, compID, matchID)

	err := e.withPoolMatchTx(tx, compID, matchID, func(r *state.MatchResult) {
		if result.SideA == "" {
			result.SideA = r.SideA
		}
		if result.SideB == "" {
			result.SideB = r.SideB
		}
		if result.Court == "" {
			result.Court = r.Court
		}
		if result.ScheduledAt == "" {
			result.ScheduledAt = r.ScheduledAt
		}
		*r = *result
	})
	if err != nil {
		if !errors.Is(err, errMatchNotFound) {
			return nil, err
		}
		if err := e.recordBracketMatchResultTx(tx, compID, matchID, result); err != nil {
			return nil, err
		}
	}
	status, err := e.recordIneligibilityFromDecisionTx(tx, compID, matchID, result)
	if err != nil {
		var alreadyErr *AlreadyIneligibleError
		if errors.As(err, &alreadyErr) {
			// K3/CHK047: roll back the partial score-write within the
			// same tx. The pool/bracket mutation already landed on disk,
			// but the intended loser is already ineligible from a
			// different match — revert before returning 409.
			if prior != nil {
				if rerr := e.recordMatchResultTx(tx, compID, matchID, prior); rerr != nil {
					log.Printf("engine: RecordMatchResultWithIneligibilityTx rollback failed compId=%s matchId=%s: %v", compID, matchID, rerr)
				}
			}
			return nil, err
		}
		log.Printf("engine: recordIneligibilityFromDecisionTx compId=%s matchId=%s: %v", compID, matchID, err)
		return nil, nil
	}
	e.maybeLockTeamLineupsForRoundTx(tx, compID, result)
	return status, nil
}

// recordMatchResultTx is the tx-aware twin of RecordMatchResult. Used
// exclusively by the K3 partial-write rollback inside
// RecordMatchResultWithIneligibilityTx — the prior result is restored
// byte-for-byte, so applyHansokuIppons is intentionally skipped here.
func (e *Engine) recordMatchResultTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult) error {
	result.ID = matchID
	err := e.withPoolMatchTx(tx, compID, matchID, func(r *state.MatchResult) {
		if result.SideA == "" {
			result.SideA = r.SideA
		}
		if result.SideB == "" {
			result.SideB = r.SideB
		}
		if result.Court == "" {
			result.Court = r.Court
		}
		if result.ScheduledAt == "" {
			result.ScheduledAt = r.ScheduledAt
		}
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
	e.maybeLockTeamLineupsForRoundTx(tx, compID, result)
	return nil
}

// lookupExistingResultTx is the tx-aware twin of lookupExistingResult.
// Reads pool matches first, falls through to bracket on
// errMatchNotFound (NotFoundError) — same shape the non-tx path
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
			for _, bm := range round {
				if bm.ID == matchID {
					return &state.MatchResult{
						ID:              bm.ID,
						SideA:           bm.SideA,
						SideB:           bm.SideB,
						Winner:          bm.Winner,
						Status:          bm.Status,
						Decision:        bm.Decision,
						DecisionBy:      bm.DecisionBy,
						DecisionReason:  bm.DecisionReason,
						Encho:           bm.Encho,
						DecidedByHantei: bm.DecidedByHantei,
					}, nil
				}
			}
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
	}
	return "", "", notFoundErrorf("match %q not found in competition %q", matchID, compID)
}

// StartMatchTx is the tx-aware FR-035 gate. Same contract as
// StartMatch: returns *IneligibleCompetitorError when any participant
// in matchID is marked ineligible from a *different* match. The
// undo-path is permitted (status with MatchID==matchID is skipped).
//
// The score handler wraps RecordMatchResultWithIneligibilityTx with
// this check so a fought / hikiwake score on a match whose
// participants include someone previously ineligible is rejected
// before any disk write. Kiken/fusenpai decisions go through
// RecordDecisionTx, which intentionally bypasses this gate — they ARE
// the act of recording a new withdrawal.
func (e *Engine) StartMatchTx(tx state.StoreTx, compID, matchID string) error {
	sideA, sideB, err := e.lookupMatchSidesTx(tx, compID, matchID)
	if err != nil {
		return err
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil {
		return err
	}
	participants, err := tx.LoadParticipants(compID, comp.WithZekkenName)
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

// checkConcurrentIneligibilityTx is the tx-aware twin of
// checkConcurrentIneligibility. Same logic, same "log and skip on
// lookup failure" behaviour — the T105 guard is best-effort, the
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
	participants, err := tx.LoadParticipants(compID, comp.WithZekkenName)
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
	}
	return false, nil
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
	participants, err := tx.LoadParticipants(compID, comp.WithZekkenName)
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
// contract — auto-fills the scoreline, runs the T103 lock + T105
// concurrent-kiken checks, persists the result, restores prior-loser
// eligibility on undo — all inside ONE per-comp lock acquire.
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
		winIppons[i] = "M"
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
