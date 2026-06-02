package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ReplaceParticipantInDraw cascades a participant name/dojo/displayName change
// through draw artifacts (pools.csv, bracket.json, pool-matches.csv) for a
// draw-ready competition. Called AFTER UpdateParticipant has already updated
// participants.csv and seeds.csv.
//
// Returns warnings (e.g. dojo conflicts) and an error on failure.
//
// Transaction safety: bracket.json and pool-matches.csv are updated atomically
// under a single Store.WithTransaction lock (WAL-staged). pools.csv is written
// outside the transaction via its own per-comp lock.
func (e *Engine) ReplaceParticipantInDraw(
	compID string,
	oldName, oldDojo, oldDisplayName string,
	newName, newDojo, newDisplayName string,
) (warnings []string, err error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}
	if comp.Status != state.CompStatusDrawReady {
		return nil, validationErrorf("competition %s is not in draw-ready state (status: %s)", compID, comp.Status)
	}

	// No-op: nothing to cascade if all fields are unchanged.
	if oldName == newName && oldDojo == newDojo && oldDisplayName == newDisplayName {
		return nil, nil
	}

	// --- pools.csv ---
	// SavePools acquires its own per-comp lock, so it runs outside the tx.
	pools, err := e.store.LoadPools(compID)
	if err != nil {
		return nil, fmt.Errorf("loading pools: %w", err)
	}
	poolsChanged := false
	affectedPools := map[string]bool{}
	for i, pool := range pools {
		for j, player := range pool.Players {
			if player.Name == oldName {
				pools[i].Players[j].Name = newName
				pools[i].Players[j].Dojo = newDojo
				if oldDisplayName != "" || newDisplayName != "" {
					pools[i].Players[j].DisplayName = newDisplayName
				}
				affectedPools[pool.PoolName] = true
				poolsChanged = true
			}
		}
	}
	if poolsChanged {
		if err := e.store.SavePools(compID, pools); err != nil {
			return nil, fmt.Errorf("saving pools: %w", err)
		}
		// Dojo-conflict detection on affected pools after the swap.
		// Warn but do not block — the operator decides whether to proceed.
		for _, pool := range pools {
			if !affectedPools[pool.PoolName] {
				continue
			}
			dojoCount := map[string]int{}
			for _, p := range pool.Players {
				dojoCount[p.Dojo]++
			}
			if count := dojoCount[newDojo]; count > 1 {
				warnings = append(warnings, fmt.Sprintf("dojo conflict: %q appears %d times in %s", newDojo, count, pool.PoolName))
			}
		}
	}

	// --- bracket.json + pool-matches.csv (WAL-staged, one transaction) ---
	var bracketFound, matchesFound bool
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		// Re-verify draw-ready status under the transaction lock to guard against a
		// concurrent StartCompetition that may have transitioned the competition
		// between the initial status check and these writes.
		current, err := tx.LoadCompetition(compID)
		if err != nil {
			return fmt.Errorf("re-checking competition status: %w", err)
		}
		if current == nil || current.Status != state.CompStatusDrawReady {
			status := "unknown"
			if current != nil {
				status = string(current.Status)
			}
			return validationErrorf("competition %s is no longer in draw-ready state (status: %s)", compID, status)
		}

		bracket, err := tx.LoadBracket(compID)
		if err != nil {
			return fmt.Errorf("loading bracket: %w", err)
		}
		bracketChanged := false
		for i, round := range bracket.Rounds {
			for j, match := range round {
				if match.SideA == oldName {
					bracket.Rounds[i][j].SideA = newName
					bracketChanged = true
					bracketFound = true
				}
				if match.SideB == oldName {
					bracket.Rounds[i][j].SideB = newName
					bracketChanged = true
					bracketFound = true
				}
				if match.Winner == oldName {
					bracket.Rounds[i][j].Winner = newName
					bracketChanged = true
					bracketFound = true
				}
			}
		}
		if bracketChanged {
			if err := tx.SaveBracket(compID, bracket); err != nil {
				return fmt.Errorf("saving bracket: %w", err)
			}
		}

		poolMatches, err := tx.LoadPoolMatches(compID)
		if err != nil {
			return fmt.Errorf("loading pool matches: %w", err)
		}
		matchesChanged := false
		for i, m := range poolMatches {
			if m.SideA == oldName {
				poolMatches[i].SideA = newName
				matchesChanged = true
				matchesFound = true
			}
			if m.SideB == oldName {
				poolMatches[i].SideB = newName
				matchesChanged = true
				matchesFound = true
			}
			if m.Winner == oldName {
				poolMatches[i].Winner = newName
				matchesChanged = true
				matchesFound = true
			}
		}
		if matchesChanged {
			if err := tx.SavePoolMatches(compID, poolMatches); err != nil {
				return fmt.Errorf("saving pool matches: %w", err)
			}
		}

		return nil
	})
	if txErr != nil {
		return warnings, txErr
	}

	// If oldName appeared nowhere in the draw AND oldName != newName, the participant
	// was not placed in the draw. This is expected when check-in filtering excluded
	// them — treat as a warning so the caller is not forced to roll back a successful
	// participants.csv update.
	if !poolsChanged && !bracketFound && !matchesFound && oldName != newName {
		warnings = append(warnings, fmt.Sprintf("participant %q not found in draw artifacts (may be excluded by check-in filtering)", oldName))
	}

	// seeds.csv is already renamed by state.UpdateParticipant (which runs
	// before this function), so no seed cascade is needed here.

	return warnings, nil
}
