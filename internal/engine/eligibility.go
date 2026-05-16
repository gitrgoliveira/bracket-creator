package engine

import (
	"errors"
	"fmt"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
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
	return e.CheckEligibility(compID, ids)
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
	pool := append([]helper.Player{}, comp.Players...)
	pool = append(pool, participants...)
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

func lookupPlayerID(players []helper.Player, name string) string {
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

// recordIneligibilityFromDecision is the T085 engine-side side effect.
// When a top-level match result records a kiken or fusenpai decision,
// the losing player (the side with zero ippons, which the request-body
// validator already enforces) becomes ineligible for subsequent
// matches in this competition.
//
// The handler layer is responsible for broadcasting the
// `competitor-status-updated` SSE event after this returns.
//
// FR-036, contracts/match-decisions.md §side-effects.
func (e *Engine) recordIneligibilityFromDecision(compID, matchID string, result *state.MatchResult) error {
	if result == nil {
		return nil
	}
	if result.Decision != string(domain.DecisionKiken) && result.Decision != string(domain.DecisionFusenpai) {
		return nil
	}
	loser := loserSideName(result)
	if loser == "" {
		return nil
	}
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return err
	}
	participants, err := e.store.LoadParticipants(compID, comp.WithZekkenName)
	if err != nil {
		return err
	}
	pool := append([]helper.Player{}, comp.Players...)
	pool = append(pool, participants...)
	playerID := lookupPlayerID(pool, loser)
	if playerID == "" {
		return nil
	}
	status := domain.CompetitorStatus{
		PlayerID:   playerID,
		Eligible:   false,
		Reason:     fmt.Sprintf("%s at %s", result.Decision, matchID),
		MatchID:    matchID,
		RecordedAt: time.Now().UTC(),
	}
	return e.store.SetCompetitorStatus(compID, status)
}

// loserSideName returns the SideA or SideB name of the side with zero
// ippons (the loser in a kiken/fusenpai). Returns "" when the scoreline
// is ambiguous (e.g. both empty or both populated).
func loserSideName(result *state.MatchResult) string {
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
