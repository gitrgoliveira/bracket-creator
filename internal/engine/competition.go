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
//
// Atomic: the status check and the save run inside
// state.Store.UpdateCompetitionChanged so a concurrent
// invalidate-vs-auto-complete pair can't lose either mutation. Pre-
// atomic-primitive, LoadCompetition + SaveCompetitionChanged ran
// sequentially without a shared lock — a concurrent admin invalidate
// could land between Load and Save, and our late save would clobber
// the "invalid" status back to "complete" (or vice versa). Even
// though SaveCompetitionChanged's content-equality check prevented
// duplicate broadcasts for the "two concurrent auto-completes" case,
// it offered no protection against admin-action interference.
func (e *Engine) MaybeAutoCompletePools(compID string) (bool, error) {
	// Load pool matches OUTSIDE the lock: we only need a consistent
	// snapshot of completedness for the early-skip decision. The
	// authoritative check is re-done inside the transform under the
	// per-comp lock (against the same matches file). Concurrent
	// match writes are already serialized via the per-comp lock on
	// the pool-matches.csv path; we trust the snapshot's "all
	// completed" verdict by the time we acquire the comp lock.
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return false, err
	}
	allCompleted := true
	for _, m := range matches {
		if m.Status != state.MatchStatusCompleted {
			allCompleted = false
			break
		}
	}

	changed, err := e.store.UpdateCompetitionChanged(compID, func(comp *state.Competition) (*state.Competition, error) {
		// Re-check preconditions INSIDE the lock. A concurrent
		// invalidate could have moved Status off Pools between our
		// outer match-load and acquiring the comp lock.
		if comp == nil || comp.Format != state.CompFormatPools || comp.Status != state.CompStatusPools {
			return nil, nil
		}
		if !allCompleted {
			return nil, nil
		}
		comp.Status = state.CompStatusComplete
		return comp, nil
	})
	return changed, err
}

// StartCompetition runs the competition-start pipeline: validate
// status, load participants/seeds, generate pools or bracket, commit
// the new Status atomically, save participants, generate schedule.
//
// The final comp-status commit is wrapped in
// state.Store.UpdateCompetitionChanged so two concurrent
// StartCompetition calls can't both write the new Status (or, more
// importantly, one's "start" can't clobber the other's already-applied
// status change). The status re-check inside the transform aborts if
// another writer moved Status off Setup between our outer Load and
// the atomic commit.
//
// Pipeline limitations (pre-existing, NOT addressed by this fix):
//   - Pool/bracket generation (writes pools.csv / bracket.json) runs
//     OUTSIDE the comp-config lock. Two concurrent starts could each
//     generate and overwrite each other's pools.csv before the
//     atomic Status commit serializes them. The later start's
//     pools.csv wins; users see the second start's player ordering.
//     A full fix would require holding the comp lock across the
//     entire pipeline, which would conflict with the generator's
//     internal use of the same lock for pools.csv / bracket.json
//     writes. Left as a follow-up — needs a deeper refactor that
//     either threads "lock already held" through the generators or
//     restructures the lock granularity.
//   - SaveParticipants + GenerateSchedule (steps after the comp
//     commit) also have their own lock acquisitions. A failure
//     mid-pipeline leaves partial state on disk. Pre-existing.
func (e *Engine) StartCompetition(id string) error {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", id)
	}

	// Best-effort early validation outside the lock — fast-fails the
	// obviously-not-startable case (admin clicks start twice). The
	// authoritative re-check is inside the atomic commit below.
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

	// Generate Pools or Bracket. These calls write to other files
	// (pools.csv / bracket.json) via their own per-comp lock
	// acquisitions, so they run OUTSIDE the UpdateCompetitionChanged
	// transform below (re-entering the lock would deadlock).
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

	// Atomic commit of the modified competition. The transform
	// re-validates Status under the per-comp lock — if a concurrent
	// StartCompetition won the race and already moved Status to
	// Pools/Playoffs, we abort here with a validation error rather
	// than clobbering their result with ours. Note: our generated
	// pools.csv may have already overwritten theirs (see pipeline
	// limitations in the function comment); this fix is narrow to
	// the comp-config Status race.
	_, err = e.store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
		if current == nil {
			return nil, notFoundErrorf("competition %s not found (deleted during start)", id)
		}
		if current.Status != state.CompStatusSetup && current.Status != "" && current.Status != state.CompStatusPending {
			return nil, validationErrorf("competition %s started concurrently by another writer", id)
		}
		// Copy our pipeline's modifications onto the freshly-read
		// `current`. This preserves any unrelated fields that may
		// have been changed by other writers (Name, Date, Venue,
		// etc.) between our outer Load and this atomic commit.
		current.TeamSize = comp.TeamSize
		current.Status = comp.Status
		current.HasParticipantIDs = comp.HasParticipantIDs
		return current, nil
	})
	if err != nil {
		return err
	}

	if err := e.store.SaveParticipants(id, players); err != nil {
		return err
	}

	return e.GenerateSchedule(id)
}
