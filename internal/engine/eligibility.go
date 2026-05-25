package engine

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ErrIneligibleCompetitor is the sentinel error matched by
// errors.Is(err, engine.ErrIneligibleCompetitor). Callers use this for
// HTTP 409 mapping; the returned concrete value is an
// *IneligibleCompetitorError that carries PlayerID/Reason for the
// response body.
//
// FR-035, contracts/match-decisions.md §409.
var ErrIneligibleCompetitor = errors.New("ineligible competitor")

// IneligibleCompetitorError wraps ErrIneligibleCompetitor with the
// player that failed the eligibility check.
type IneligibleCompetitorError struct {
	PlayerID string
	Reason   string
}

func (e *IneligibleCompetitorError) Error() string {
	return fmt.Sprintf("ineligible competitor: playerId=%q reason=%q", e.PlayerID, e.Reason)
}

func (e *IneligibleCompetitorError) Is(target error) bool {
	return target == ErrIneligibleCompetitor
}

// AlreadyIneligibleError is returned by RecordDecision when the
// intended loser already carries Eligible:false from a *different*
// match — indicating two operators on different courts concurrently
// tried to kiken/fusenpai the same player (CHK047, T105, NFR-010).
type AlreadyIneligibleError struct {
	PlayerID string
	MatchID  string
	Reason   string
}

func (e *AlreadyIneligibleError) Error() string {
	return fmt.Sprintf("competitor %q already ineligible (match %s)", e.PlayerID, e.MatchID)
}

// checkConcurrentIneligibility returns *AlreadyIneligibleError when
// loserName already has Eligible:false from a different match. Returns
// nil on any lookup failure (non-fatal — missing player IDs, store
// errors) so a degraded-mode run doesn't break the score flow.
//
// CHK047, T105.
func (e *Engine) checkConcurrentIneligibility(compID, matchID, loserName string) error {
	if loserName == "" {
		return nil
	}
	comp, err := e.store.LoadCompetition(compID)
	if err != nil || comp == nil {
		if err != nil {
			log.Printf("engine: checkConcurrentIneligibility LoadCompetition compId=%s: %v (T105 guard skipped)", compID, err)
		}
		return nil
	}
	participants, err := e.store.LoadParticipants(compID, comp.WithZekkenName)
	if err != nil {
		log.Printf("engine: checkConcurrentIneligibility LoadParticipants compId=%s: %v (T105 guard skipped)", compID, err)
		return nil
	}
	pool := combinedPlayerPool(comp.Players, participants)
	playerID := lookupPlayerID(pool, loserName)
	if playerID == "" {
		return nil
	}
	statuses, err := e.store.LoadCompetitorStatus(compID)
	if err != nil {
		log.Printf("engine: checkConcurrentIneligibility LoadCompetitorStatus compId=%s: %v (T105 guard skipped)", compID, err)
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

// CheckEligibility consults the competitor-status store for compID and
// returns *IneligibleCompetitorError for the first playerID found with
// Eligible: false; nil when all playerIDs are eligible (or unknown to
// the store, which means default-eligible per FR-034).
//
// FR-035.
func (e *Engine) CheckEligibility(compID string, playerIDs []string) error {
	statuses, err := e.store.LoadCompetitorStatus(compID)
	if err != nil {
		return err
	}
	for _, pid := range playerIDs {
		if pid == "" {
			continue
		}
		if st, ok := statuses[pid]; ok && !st.Eligible {
			return &IneligibleCompetitorError{PlayerID: pid, Reason: st.Reason}
		}
	}
	return nil
}

// StartMatch gates the scheduled → running transition by checking
// every participant's competitor-status. It returns *IneligibleCompetitorError
// (which matches errors.Is(err, ErrIneligibleCompetitor)) when any
// participant has Eligible: false; nil when the match may proceed.
//
// The status transition itself remains with the score handler — this
// method is the pre-flight gate.
//
// FR-035, T084.
func (e *Engine) StartMatch(compID, matchID string) error {
	ids, err := e.resolveMatchParticipantIDs(compID, matchID)
	if err != nil {
		return err
	}
	return e.checkEligibilityExcludingMatch(compID, ids, matchID)
}

// checkEligibilityExcludingMatch is like CheckEligibility but skips
// CompetitorStatus entries whose source MatchID equals excludeMatchID.
// This lets a match be re-scored (the T103 undo path) even when its
// own prior kiken/fusenpai created the ineligibility — the status was
// recorded BY that match, so it should not block writing back to it.
func (e *Engine) checkEligibilityExcludingMatch(compID string, playerIDs []string, excludeMatchID string) error {
	statuses, err := e.store.LoadCompetitorStatus(compID)
	if err != nil {
		return err
	}
	for _, pid := range playerIDs {
		if pid == "" {
			continue
		}
		if st, ok := statuses[pid]; ok && !st.Eligible && st.MatchID != excludeMatchID {
			return &IneligibleCompetitorError{PlayerID: pid, Reason: st.Reason}
		}
	}
	return nil
}

// RecordDecision auto-fills the scoreline from decision/decisionBy/encho
// and persists the result via RecordMatchResultWithIneligibility. The
// canonical SideA=Aka / SideB=Shiro mapping (CLAUDE.md) is used to
// translate decisionBy → which side loses/forfeits the auto-filled X-0 scoreline.
//
// When the match already has a kiken/fusenpai decision recorded (the
// "undo" path, T103/CHK024) the engine enforces the
// contracts/match-decisions.md §Decision lock & undo rule: if any
// subsequent match involving either prior participant has started
// since the original decision was recorded, the engine returns
// ErrDecisionLocked unless force is true. On a successful overwrite
// where the prior loser is no longer the new loser, the prior loser's
// CompetitorStatus is restored to Eligible: true and surfaced as the
// returned status so the handler can broadcast the change.
//
// Returns the persisted MatchResult and the most-recent
// CompetitorStatus change (new ineligibility OR restored eligibility),
// or nil when no status change applies.
//
// T090, T103, contracts/match-decisions.md §POST /decision.
func (e *Engine) RecordDecision(compID, matchID, decision, decisionBy, decisionReason string, encho *state.EnchoMetadata, force bool) (*state.MatchResult, *domain.CompetitorStatus, error) {
	if decisionBy != "shiro" && decisionBy != "aka" {
		return nil, nil, validationErrorf("decisionBy must be 'shiro' or 'aka', got %q", decisionBy)
	}
	sideA, sideB, err := e.lookupMatchSides(compID, matchID)
	if err != nil {
		return nil, nil, err
	}
	// T105/CHK047: reject concurrent kiken — if the intended loser is
	// already ineligible from a *different* match, two operators are
	// trying to kiken the same player simultaneously. Return 409 so the
	// second operator sees the conflict before any write happens.
	//
	// Only kiken and fusenpai actually mark the loser ineligible; for
	// fusensho/daihyosen this check would surface a misleading
	// "already_ineligible" 409 — the StartMatch eligibility gate is the
	// right place to reject those cases.
	loserName := sideB
	if decisionBy == "aka" {
		loserName = sideA
	}
	if domain.IsKikenDecisionStr(decision) || decision == string(domain.DecisionFusenpai) {
		if cerr := e.checkConcurrentIneligibility(compID, matchID, loserName); cerr != nil {
			return nil, nil, cerr
		}
	}
	// T103: look up the prior result so we know whether this is an
	// overwrite of a kiken/fusenpai (the "undo" path).
	prior, err := e.lookupExistingResult(compID, matchID)
	if err != nil {
		return nil, nil, err
	}
	priorLoser := ""
	if prior != nil && (domain.IsKikenDecisionStr(prior.Decision) || prior.Decision == string(domain.DecisionFusenpai)) {
		priorLoser = loserSideName(prior)
	}
	// T103: downstream-match check. The contract scope is "either
	// participant" — if any subsequent match for either side has been
	// started or completed since the kiken/fusenpai, refuse the undo
	// unless force is set.
	if priorLoser != "" && !force {
		started, err := e.hasDownstreamMatchStarted(compID, []string{sideA, sideB}, matchID)
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
	// shiro=SideB (White, left), aka=SideA (Red, right). The losing
	// side ends with 0 ippons; the surviving side gets the X auto-fill
	// and becomes Winner.
	if decisionBy == "shiro" {
		result.IpponsA = winIppons
		result.Winner = sideA
	} else {
		result.IpponsB = winIppons
		result.Winner = sideB
	}
	status, err := e.RecordMatchResultWithIneligibility(compID, matchID, result)
	if err != nil {
		return nil, nil, err
	}
	// T103: when the prior loser is no longer the new loser (decision
	// type changed away from kiken/fusenpai, or decisionBy flipped),
	// restore the prior loser's eligibility and surface the resulting
	// status so the handler can broadcast it. If RecordMatchResult
	// just wrote a *new* ineligibility for the same player, that wins
	// (the player is still ineligible). Only restore when the prior
	// loser is no longer the current loser.
	if priorLoser != "" {
		newLoser := loserSideName(result)
		if priorLoser != newLoser {
			restored, rerr := e.restoreCompetitorEligibility(compID, priorLoser, matchID)
			if rerr == nil && restored != nil {
				status = restored
			}
		}
	}
	return result, status, nil
}

// lookupExistingResult fetches the currently-persisted MatchResult for
// compID/matchID from either the pool-matches or bracket store. For
// bracket matches the BracketMatch fields are projected onto a
// MatchResult so callers (loserSideName, etc.) see a uniform shape;
// only the fields the kiken-undo path needs are populated. Returns a
// *NotFoundError when the match is unknown.
func (e *Engine) lookupExistingResult(compID, matchID string) (*state.MatchResult, error) {
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err == nil {
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				r := poolMatches[i]
				return &r, nil
			}
		}
	}
	bracket, err := e.store.LoadBracket(compID)
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
						DecidedByHantei: &bm.DecidedByHantei,
					}, nil
				}
			}
		}
	}
	return nil, notFoundErrorf("match %q not found in competition %q", matchID, compID)
}

// hasDownstreamMatchStarted reports whether any pool or bracket match
// other than excludeMatchID has either SideA or SideB matching one of
// playerNames AND has status running or completed. Used by the
// kiken-undo flow (T103) to enforce the decision-lock rule.
func (e *Engine) hasDownstreamMatchStarted(compID string, playerNames []string, excludeMatchID string) (bool, error) {
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
	poolMatches, err := e.store.LoadPoolMatches(compID)
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
	bracket, err := e.store.LoadBracket(compID)
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

// restoreCompetitorEligibility writes a CompetitorStatus{Eligible: true}
// for the player named priorLoser on competition compID. Used by the
// kiken-undo flow (T103) after a previous kiken/fusenpai has been
// overwritten with a different outcome. matchID is the originating
// match (the one being undone) — carried for traceability.
//
// Returns (nil, nil) when the player can't be resolved (unknown name),
// so the caller can fall through to the regular response without
// failing the undo.
func (e *Engine) restoreCompetitorEligibility(compID, priorLoser, matchID string) (*domain.CompetitorStatus, error) {
	if priorLoser == "" {
		return nil, nil
	}
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	participants, err := e.store.LoadParticipants(compID, comp.WithZekkenName)
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
	if err := e.store.SetCompetitorStatus(compID, status); err != nil {
		return nil, err
	}
	return &status, nil
}

// resolveMatchParticipantIDs finds the match (pool or bracket) and
// resolves SideA/SideB names to player IDs via the competition's
// participants list.
func (e *Engine) resolveMatchParticipantIDs(compID, matchID string) ([]string, error) {
	sideA, sideB, err := e.lookupMatchSides(compID, matchID)
	if err != nil {
		return nil, err
	}
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	participants, err := e.store.LoadParticipants(compID, comp.WithZekkenName)
	if err != nil {
		return nil, err
	}
	pool := combinedPlayerPool(comp.Players, participants)
	return []string{lookupPlayerID(pool, sideA), lookupPlayerID(pool, sideB)}, nil
}

func (e *Engine) lookupMatchSides(compID, matchID string) (string, string, error) {
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err == nil {
		for _, m := range poolMatches {
			if m.ID == matchID {
				return m.SideA, m.SideB, nil
			}
		}
	}
	bracket, err := e.store.LoadBracket(compID)
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

func lookupPlayerID(players []domain.Player, name string) string {
	if name == "" {
		return ""
	}
	for _, p := range players {
		if p.Name == name {
			return p.ID
		}
	}
	return ""
}

// combinedPlayerPool merges comp.Players and freshly-loaded participants
// into a single []domain.Player suitable for lookupPlayerID. Several
// engine code paths need to resolve a Name → ID against both the
// in-memory competition snapshot and the participants.csv on disk
// (the two can diverge briefly during config edits).
//
// After T154, both inputs are already []domain.Player; the function
// just concatenates them (NFR-007).
func combinedPlayerPool(compPlayers []domain.Player, participants []domain.Player) []domain.Player {
	out := make([]domain.Player, 0, len(compPlayers)+len(participants))
	out = append(out, compPlayers...)
	out = append(out, participants...)
	return out
}

// recordIneligibilityFromDecision is the T085 engine-side side effect.
// When a top-level match result records a kiken or fusenpai decision,
// the losing player (the side opposite of result.Winner, with an
// ippon-count fallback) becomes ineligible for subsequent matches in
// this competition.
//
// Returns the persisted CompetitorStatus when a status was written
// (so the handler layer can broadcast the corresponding
// `competitor-status-updated` SSE event), or (nil, nil) when no
// status change applies (non-kiken/fusenpai decision, unresolvable
// loser, or unknown player).
//
// FR-036, contracts/match-decisions.md §side-effects.
func (e *Engine) recordIneligibilityFromDecision(compID, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error) {
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
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	participants, err := e.store.LoadParticipants(compID, comp.WithZekkenName)
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
	// K2/CHK047: atomic check-and-set under WithTransaction closes the
	// TOCTOU window between the pre-RecordDecision check and this write.
	// Two concurrent kiken writes on the same player from different
	// matches will serialize on the per-comp lock; the second to acquire
	// it sees the first's record and returns *AlreadyIneligibleError
	// instead of silently overwriting.
	if txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		statuses, err := tx.LoadCompetitorStatus(compID)
		if err != nil {
			return err
		}
		if st, ok := statuses[playerID]; ok && !st.Eligible && st.MatchID != matchID {
			return &AlreadyIneligibleError{
				PlayerID: playerID,
				MatchID:  st.MatchID,
				Reason:   st.Reason,
			}
		}
		return tx.SetCompetitorStatus(compID, status)
	}); txErr != nil {
		return nil, txErr
	}
	return &status, nil
}

// loserSideName returns the name of the losing side for a
// kiken/fusenpai. It prefers result.Winner (the canonical surviving
// side, set by the score handler after T077 validation) and falls
// back to the ippon-count heuristic only when Winner is unset.
//
// Returns "" when neither path is conclusive — callers must treat
// that as "no ineligibility recorded" and the operator will need to
// fix the request shape before the eligibility gate works.
func loserSideName(result *state.MatchResult) string {
	if result.Winner != "" {
		switch result.Winner {
		case result.SideA:
			return result.SideB
		case result.SideB:
			return result.SideA
		}
	}
	aEmpty := len(result.IpponsA) == 0
	bEmpty := len(result.IpponsB) == 0
	switch {
	case aEmpty && !bEmpty:
		return result.SideA
	case !aEmpty && bEmpty:
		return result.SideB
	}
	return ""
}

// ReinstateCompetitor restores eligibility for a competitor who was
// withdrawn via kiken-injury (FIK Art. 30). The status must exist,
// be Eligible: false, and have Reinstateable: true (set by
// kiken-injury). Voluntary kiken (Art. 31) and fusenpai statuses
// are not reinstateable — the endpoint returns an error.
//
// The check-and-set runs under WithTransaction (K2/CHK047) to close
// the TOCTOU window between reading the Reinstateable flag and writing
// the reinstated status.
func (e *Engine) ReinstateCompetitor(compID, playerID string) (*domain.CompetitorStatus, error) {
	if playerID == "" {
		return nil, validationErrorf("playerID is required")
	}
	var out *domain.CompetitorStatus
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		statuses, err := tx.LoadCompetitorStatus(compID)
		if err != nil {
			return err
		}
		st, ok := statuses[playerID]
		if !ok || st.Eligible {
			return validationErrorf("competitor %q is not ineligible", playerID)
		}
		if !st.Reinstateable {
			return validationErrorf("competitor %q is not reinstateable (voluntary kiken or fusenpai)", playerID)
		}
		status := domain.CompetitorStatus{
			PlayerID:   playerID,
			Eligible:   true,
			MatchID:    st.MatchID,
			Reason:     fmt.Sprintf("reinstated (was: %s)", st.Reason),
			RecordedAt: time.Now().UTC(),
		}
		if err := tx.SetCompetitorStatus(compID, status); err != nil {
			return err
		}
		out = &status
		return nil
	})
	return out, err
}
