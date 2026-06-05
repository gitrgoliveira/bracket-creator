package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// allPoolMatchesCompleteForComp returns true when every pool match (including
// supplementary tiebreaker/DH matches) for compID is complete AND resolved
// (tiebreaker/DH matches must have a non-empty Winner). It also ensures no
// outstanding tiebreaker/DH round is waiting to be played.
//
// This is the same completeness predicate used by MaybeAutoCompletePools, so
// StartKnockout and MaybeAutoCompletePools agree on what "pools are done" means.
// Extracted as a shared helper to keep both callers in sync.
func (e *Engine) allPoolMatchesCompleteForComp(compID string) (bool, error) {
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return false, err
	}
	allComplete := true
	hasIncompleteTB := false
	hasCompleteTB := false
	hasIncompleteDH := false
	hasCompleteDH := false
	for _, m := range matches {
		switch {
		case IsTiebreakerMatchID(m.ID):
			if m.Status != state.MatchStatusCompleted || m.Winner == "" {
				hasIncompleteTB = true
			} else {
				hasCompleteTB = true
			}
		case IsPoolDaihyosenMatchID(m.ID):
			if m.Status != state.MatchStatusCompleted || m.Winner == "" {
				hasIncompleteDH = true
			} else {
				hasCompleteDH = true
			}
		default:
			if m.Status != state.MatchStatusCompleted {
				allComplete = false
			}
		}
	}
	if !allComplete || hasIncompleteTB || hasIncompleteDH {
		return false, nil
	}
	// All regular + any outstanding supplementary matches are done.
	// Check for pending tiebreaker injection (same guard as MaybeAutoCompletePools).
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return false, err
	}
	isTeamComp := comp != nil && comp.TeamSize > 0
	if isTeamComp && !hasCompleteDH {
		injected, injErr := e.InjectPoolDaihyosenMatches(compID)
		if injErr != nil {
			return false, injErr
		}
		if len(injected) > 0 {
			return false, nil // ties found, DH injected — not complete yet
		}
	} else if !isTeamComp && !hasCompleteTB {
		injected, injErr := e.InjectTiebreakerMatches(compID)
		if injErr != nil {
			return false, injErr
		}
		if len(injected) > 0 {
			return false, nil // ties found, TB injected — not complete yet
		}
	}
	// For team competitions where DH matches have been played: verify the DH
	// results actually broke all ties (same cycle check as MaybeAutoCompletePools).
	if isTeamComp && hasCompleteDH {
		freshMatches, ferr := e.store.LoadPoolMatches(compID)
		if ferr != nil {
			return false, ferr
		}
		standings, standErr := e.CalculatePoolStandings(compID)
		if standErr != nil {
			return false, standErr
		}
		overridesObj, _ := e.store.LoadOverrides(compID)
		var poolRanks map[string]map[string]int
		if overridesObj != nil {
			poolRanks = overridesObj.PoolRanks
		}
		if dhCycleExists(standings, freshMatches, poolRanks) {
			return false, nil
		}
	}
	return true, nil
}

// StartKnockout resolves the in-place knockout for a mixed (Pools + Knockout)
// competition that has completed its pool phase. It replaces every pool-origin
// placeholder label ("Pool A-1st", …) in the persisted preview bracket with the
// real participant Name from pool standings — and clears the Preview flag — so
// the bracket becomes live and scoreable while preserving the previewed
// topology (court assignment, cross-seed slots, bye placement) byte-for-byte.
// Bracket sides are name strings; participant UUIDs/Numbers continue to live
// on the roster (state.Participants) and are looked up by name as needed. The
// competition transitions from CompStatusPools to CompStatusPlayoffs.
//
// Preconditions (any failure returns a ValidationError mapped to HTTP 409):
//   - The competition exists.
//   - Format == mixed.
//   - Status == pools.
//   - ALL pool matches are complete (including any tiebreaker/DH resolution).
//
// The bracket structure is IDENTICAL to the preview bracket (same GenerateFinals
// leaf order, same buildBracketFromLeaves call) so what was previewed is exactly
// what gets played. StandardSeedingFull is NOT re-applied.
//
// Idempotent: calling StartKnockout on a competition already in CompStatusPlayoffs
// is rejected by the precondition check (returns validation error), but the
// bracket written is deterministic from the pool results, so a retry after a
// partial failure can safely rebuild.
func (e *Engine) StartKnockout(compID string) error {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", compID)
	}

	// Precondition: must be a mixed competition.
	if comp.Format != state.CompFormatMixed {
		return validationErrorf("StartKnockout requires a mixed (Pools + Knockout) competition (got %q)", comp.Format)
	}

	// Precondition: must be in pools status.
	if comp.Status != state.CompStatusPools {
		return validationErrorf("StartKnockout requires competition %s to be in pools status (got %q)", compID, comp.Status)
	}

	// Precondition: all pool matches must be complete.
	complete, err := e.allPoolMatchesCompleteForComp(compID)
	if err != nil {
		return err
	}
	if !complete {
		return validationErrorf("StartKnockout requires all pool matches to be complete for competition %s", compID)
	}

	// Resolve the EXISTING preview bracket in place: replace each pool-origin
	// placeholder ("Pool A-1st", …) with the real pool-standings finisher,
	// preserving the bracket's topology (cross-seed interleaving, bye placement
	// via ApplyPoolAdjustments, court/schedule assignment) EXACTLY as previewed.
	// Resolving the persisted preview — rather than rebuilding from scratch —
	// guarantees the live bracket matches the preview slot-for-slot and cannot
	// drift from generatePoolPreviewBracket's seeding logic.
	bracket, err := e.store.LoadBracket(compID)
	if err != nil {
		return fmt.Errorf("loading preview bracket for knockout: %w", err)
	}
	if bracket == nil || len(bracket.Rounds) == 0 {
		return validationErrorf("competition %s has no preview bracket to resolve", compID)
	}

	resolver, err := e.buildFinalistResolver(comp)
	if err != nil {
		return err
	}

	// Replace placeholder labels across ALL rounds. Round 0 holds the pool-origin
	// placeholders; a finalist that drew a bye is auto-advanced, so its placeholder
	// also appears as a later round's side AND as the bye match's Winner — resolve
	// all three fields. "Winner of rX-mY" and "" (bye) values are not in the
	// resolver and are left untouched.
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			m := &bracket.Rounds[ri][mi]
			if name, ok := resolver[m.SideA]; ok {
				m.SideA = name
			}
			if name, ok := resolver[m.SideB]; ok {
				m.SideB = name
			}
			if name, ok := resolver[m.Winner]; ok {
				m.Winner = name
			}
		}
	}
	bracket.Preview = false

	// Persist the live bracket.
	if err := e.store.SaveBracket(compID, bracket); err != nil {
		return fmt.Errorf("saving knockout bracket: %w", err)
	}

	// Atomically transition from pools → playoffs, re-checking preconditions
	// under the lock so a concurrent action can't race us.
	_, err = e.store.UpdateCompetitionChanged(compID, func(current *state.Competition) (*state.Competition, error) {
		if current == nil {
			return nil, notFoundErrorf("competition %s not found (deleted during StartKnockout)", compID)
		}
		if current.Format != state.CompFormatMixed {
			return nil, validationErrorf("StartKnockout: competition %s is no longer mixed format", compID)
		}
		if current.Status != state.CompStatusPools {
			return nil, validationErrorf("StartKnockout: competition %s is no longer in pools status (concurrent modification?)", compID)
		}
		current.Status = state.CompStatusPlayoffs
		return current, nil
	})
	if err != nil {
		return err
	}

	// Generate the bracket schedule now that the bracket is live.
	return e.GenerateSchedule(compID)
}

// buildFinalistResolver maps each pool-origin finalist placeholder label
// ("<PoolName>-<ordinal>", e.g. "Pool A-1st") to the real participant name that
// finished at that pool rank, using the competition's OWN pool standings. The
// label format mirrors helper.GenerateFinals exactly so the keys match the
// placeholders in the preview bracket. Fails fast if any pool has fewer ranked
// finishers than PoolWinners (pool results incomplete), so an unresolved
// "Pool A-2nd" placeholder can never leak into the live bracket as a player name.
func (e *Engine) buildFinalistResolver(comp *state.Competition) (map[string]string, error) {
	pools, err := e.store.LoadPools(comp.ID)
	if err != nil {
		return nil, fmt.Errorf("loading pools for %q: %w", comp.ID, err)
	}
	if len(pools) == 0 {
		return nil, validationErrorf("competition %s has no pools to resolve finalists from", comp.ID)
	}
	poolWinners := comp.PoolWinners
	if poolWinners <= 0 {
		poolWinners = 2
	}
	standings, err := e.CalculatePoolStandings(comp.ID)
	if err != nil {
		return nil, fmt.Errorf("calculating pool standings for %q: %w", comp.ID, err)
	}
	resolver := make(map[string]string, len(pools)*poolWinners)
	for _, pool := range pools {
		ps := standings[pool.PoolName]
		if len(ps) < poolWinners {
			return nil, validationErrorf("pool %q has only %d ranked finishers, need %d", pool.PoolName, len(ps), poolWinners)
		}
		for rank := 1; rank <= poolWinners; rank++ {
			key := fmt.Sprintf("%s-%s", pool.PoolName, helper.GetOrdinal(rank))
			resolver[key] = ps[rank-1].Player.Name
		}
	}
	return resolver, nil
}
