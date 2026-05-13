package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MaybeAutoCompletePools transitions a pools-format competition from
// CompStatusPools to CompStatusComplete when every pool match has been
// recorded as completed. It is a no-op for any other format or status,
// or when at least one pool match is still scheduled/running.
//
// Returns true if the transition was performed. Callers should broadcast
// EventCompetitionCompleted when true.
func (e *Engine) MaybeAutoCompletePools(compID string) (bool, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return false, err
	}
	if comp == nil || comp.Format != state.CompFormatPools || comp.Status != state.CompStatusPools {
		return false, nil
	}

	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return false, err
	}
	// A pools competition with zero matches has nothing left to score, so we
	// treat it as complete rather than leaving it stuck in CompStatusPools.
	// This is a corner case (single-participant pool, or any started pools
	// comp that legitimately generated zero matches), not the hot path.
	for _, m := range matches {
		if m.Status != state.MatchStatusCompleted {
			return false, nil
		}
	}

	comp.Status = state.CompStatusComplete
	// Use SaveCompetitionChanged so that concurrent score submissions that both
	// see all matches completed only broadcast once: the second writer finds the
	// file bytes unchanged and gets changed=false, returning false to its caller.
	changed, err := e.store.SaveCompetitionChanged(comp)
	if err != nil {
		return false, err
	}
	return changed, nil
}

func (e *Engine) StartCompetition(id string) error {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", id)
	}

	if comp.Status != state.CompStatusSetup && comp.Status != "" && comp.Status != state.CompStatusPending {
		return validationErrorf("competition %s already started", id)
	}

	if comp.Kind == "team" && comp.TeamSize == 0 {
		comp.TeamSize = 5 // Default for Kendo
	}

	// Only pass HasIDs hint when explicitly true; false means unset (let
	// auto-detect run) to avoid misclassifying UUID-prefixed files.
	var hasIDsHint *bool
	if comp.HasParticipantIDs {
		t := true
		hasIDsHint = &t
	}
	players, err := e.store.LoadParticipantsOpt(id, comp.WithZekkenName, state.LoadParticipantsOpts{
		WithSeeds: true,
		HasIDs:    hasIDsHint,
	})
	if err != nil {
		return err
	}
	if len(players) == 0 {
		return validationErrorf("no participants found for competition %s", id)
	}

	seeds, err := e.store.LoadSeeds(id)
	if err != nil {
		return err
	}

	// Resolve any cross-competition reserved slots before generation.
	players, err = e.resolveReservedSlots(id, players)
	if err != nil {
		return err
	}

	// Generate Pools or Bracket
	if comp.Format == state.CompFormatPools {
		if err := e.generatePools(comp, players, seeds); err != nil {
			return err
		}
		comp.Status = state.CompStatusPools
	} else {
		if err := e.generatePlayoffs(comp, players, seeds); err != nil {
			return err
		}
		comp.Status = state.CompStatusPlayoffs
	}

	if err := e.store.SaveCompetition(comp); err != nil {
		return err
	}

	if err := e.store.SaveParticipants(id, players); err != nil {
		return err
	}

	return e.GenerateSchedule(id)
}
