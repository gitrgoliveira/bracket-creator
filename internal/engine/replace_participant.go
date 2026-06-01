package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ReplaceParticipantInDraw cascades a participant name/dojo/displayName change
// through all draw artifacts (pools.csv, bracket.json, pool-matches.csv, seeds.csv)
// for a draw-ready competition. It is called AFTER the participant record in
// participants.csv has already been updated via UpdateParticipant.
//
// Returns a list of human-readable warnings (dojo conflicts, seed transfers) and
// an error on failure. A non-nil warnings slice with a nil error means the swap
// succeeded but the operator should review the warnings.
//
// Transaction safety: bracket.json and pool-matches.csv are updated atomically
// under a single Store.WithTransaction lock (both are WAL-staged). pools.csv and
// seeds.csv are not WAL-staged so they are written outside the transaction via
// their own per-comp lock acquisitions — consistent with their existing save paths.
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

	// If oldName appeared nowhere in the draw AND oldName != newName (a real name
	// change was requested), the participant wasn't placed in the draw — report it.
	if !poolsChanged && !bracketFound && !matchesFound && oldName != newName {
		return nil, notFoundErrorf("participant %q not found in draw artifacts for competition %s", oldName, compID)
	}

	// --- seeds.csv (SaveSeeds acquires its own per-comp lock) ---
	seeds, err := e.store.LoadSeeds(compID)
	if err != nil {
		return warnings, fmt.Errorf("loading seeds: %w", err)
	}
	seedsChanged := false
	for i, a := range seeds {
		if a.Name == oldName && (a.Dojo == oldDojo || a.Dojo == "") {
			seeds[i].Name = newName
			seeds[i].Dojo = newDojo
			seedsChanged = true
			warnings = append(warnings, fmt.Sprintf("seed rank %d transferred to %q", a.SeedRank, newName))
		}
	}
	if seedsChanged {
		if err := e.store.SaveSeeds(compID, seeds); err != nil {
			return warnings, fmt.Errorf("saving seeds: %w", err)
		}
	}

	return warnings, nil
}
