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
// competition that has completed its pool phase. It replaces the competition's
// placeholder preview bracket with a live, scoreable bracket whose leaf slots
// hold the real pool winners (UUID + Number preserved), and transitions the
// competition from CompStatusPools to CompStatusPlayoffs.
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

	// Load pools to determine the GenerateFinals leaf order.
	pools, err := e.store.LoadPools(compID)
	if err != nil {
		return fmt.Errorf("loading pools for knockout: %w", err)
	}
	if len(pools) == 0 {
		return validationErrorf("competition %s has no pools to build knockout from", compID)
	}

	poolWinners := comp.PoolWinners
	if poolWinners <= 0 {
		poolWinners = 2
	}

	// Generate the leaf placeholder labels in the same order the preview used.
	// This gives us the ORDER of slots: "Pool A-1st", "Pool B-1st", "Pool A-2nd", …
	placeholders := helper.GenerateFinals(pools, poolWinners)
	if len(placeholders) == 0 {
		return validationErrorf("competition %s GenerateFinals returned no leaves", compID)
	}

	// Resolve pool winners from THIS competition's own pools (in-place).
	// resolvePoolWinnersFromSource returns finalists in helper.FinalsSlotOrder —
	// the SAME bracket-leaf order GenerateFinals uses for the placeholders above —
	// so resolvedPlayers[i] is exactly the player for placeholders[i]'s slot.
	// ID and Number are preserved so bracket match sides reference real
	// participant UUIDs.
	resolvedPlayers, err := e.resolvePoolWinnersFromSource(compID)
	if err != nil {
		return fmt.Errorf("resolving pool winners for knockout: %w", err)
	}

	// Sanity: the resolver and GenerateFinals must agree on slot count.
	if len(resolvedPlayers) != len(placeholders) {
		return validationErrorf("mismatch: %d placeholders but %d resolved winners", len(placeholders), len(resolvedPlayers))
	}

	// Replace placeholders with real player names — same slot order as the preview.
	realLeaves := make([]string, len(placeholders))
	for i, p := range resolvedPlayers {
		realLeaves[i] = p.Name
	}

	// Build the live bracket using the same builder the preview used.
	// This produces a structurally identical bracket with the placeholder names
	// replaced by real player names.
	bracket, err := e.buildBracketFromLeaves(comp, realLeaves)
	if err != nil {
		return fmt.Errorf("building knockout bracket: %w", err)
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
