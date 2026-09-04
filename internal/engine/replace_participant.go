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
// pid is the participant's own id (the handler already has it, from the
// route param), used to disambiguate two participants who currently share
// oldName -- e.g. two "Tanaka Kenji" from different dojos, legal per
// CheckDuplicateEntriesByNameDojo. A pools.csv row (helper.Player.ID, column
// 8) or a pool-matches.csv side (MatchResult.SideAID/SideBID/WinnerID) that
// CARRIES an id is matched ONLY by id == pid, never by falling back to name:
// a row whose id differs from pid names a DIFFERENT competitor and must be
// left untouched even when its display name still matches oldName. Only a
// row with NO id at all (legacy data predating id-stamped generation) falls
// back to matching by (oldName, oldDojo). bracket.json carries no per-side id
// or dojo at all (BracketMatch has neither field), so a plain-name match
// there is unavoidably ambiguous whenever oldName is shared by more than one
// CURRENT participant; see the ambiguity guard below.
//
// Returns warnings (e.g. dojo conflicts, an ambiguous bracket rename skipped)
// and an error on failure.
//
// Transaction safety: all three files (pools.csv, bracket.json, pool-matches.csv)
// are updated under a single Store.WithTransaction lock. bracket.json and
// pool-matches.csv are WAL-staged. pools.csv is written directly (not WAL-staged)
// but still under the same lock, so no concurrent StartCompetition can interleave
// between any of the writes.
func (e *Engine) ReplaceParticipantInDraw(
	compID string,
	pid string,
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

	// All three files are updated under one transaction lock so a concurrent
	// StartCompetition cannot interleave between the pools, bracket, and
	// pool-matches writes.
	var poolsChanged, bracketFound, matchesFound bool
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

		// --- pools.csv ---
		pools, err := tx.LoadPools(compID)
		if err != nil {
			return fmt.Errorf("loading pools: %w", err)
		}
		affectedPools := map[string]bool{}
		for i, pool := range pools {
			for j, player := range pool.Players {
				if !matchesParticipant(player.ID, player.Name, player.Dojo, pid, oldName, oldDojo) {
					continue
				}
				pools[i].Players[j].Name = newName
				pools[i].Players[j].Dojo = newDojo
				if oldDisplayName != "" || newDisplayName != "" {
					pools[i].Players[j].DisplayName = newDisplayName
				}
				affectedPools[pool.PoolName] = true
				poolsChanged = true
			}
		}
		if poolsChanged {
			if err := tx.SavePools(compID, pools); err != nil {
				return fmt.Errorf("saving pools: %w", err)
			}
			// Dojo-conflict detection on affected pools after the swap.
			// Warn but do not block, the operator decides whether to proceed.
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

		// --- bracket.json + pool-matches.csv (WAL-staged) ---
		bracket, err := tx.LoadBracket(compID)
		if err != nil {
			return fmt.Errorf("loading bracket: %w", err)
		}
		// bracket.json carries no per-side id AND no per-side dojo (BracketMatch
		// has neither field), so a plain-name match is ambiguous whenever
		// oldName is still held by another CURRENT participant. In the normal
		// flow UpdateParticipant has already renamed the target participant's
		// OWN row to newName before this function runs, so any participant
		// still named oldName at this point is a DIFFERENT competitor --
		// rewriting every bracket row named oldName would silently
		// reattribute their match history too (e.g. two "Tanaka Kenji" from
		// different dojos). The pid check below excludes the participant
		// actually BEING renamed from that count (defense in depth for a
		// caller invoked before, or without, the participants.csv rename): a
		// row that is simply HER OWN unchanged record is not "another"
		// namesake. When ambiguous, presence is still tracked (bracketFound,
		// so the "not found in draw artifacts" fallback warning below doesn't
		// misfire), but nothing is rewritten and the operator is warned
		// instead of a guess being made.
		//
		// The candidate pool is deliberately NOT the full roster: a namesake
		// who was excluded from the draw (filterCheckedIn's opt-in check-in
		// semantics, the same mechanism the "not found in draw artifacts"
		// warning below already names) can never physically occupy a bracket
		// row, so counting her toward ambiguity produces a false warning and
		// blocks a perfectly safe rename cascade. filterCheckedIn is the same
		// function GenerateDraw itself runs, so "would this participant have
		// been placed in the draw" is answered identically here and there.
		// Skipped entirely when the bracket is empty (no rounds, no bronze
		// match): there is nothing to rename or warn about, so the
		// participants.csv read is avoided outright.
		bracketNameAmbiguous := false
		if len(bracket.Rounds) > 0 || bracket.ThirdPlaceMatch != nil {
			participants, perr := tx.LoadParticipants(compID, current.EffectiveWithZekkenName())
			if perr != nil {
				return fmt.Errorf("loading participants for bracket ambiguity check: %w", perr)
			}
			for _, p := range filterCheckedIn(participants) {
				if p.Name != oldName {
					continue
				}
				if pid != "" && p.ID == pid {
					continue // the participant being renamed herself, not "another" namesake
				}
				bracketNameAmbiguous = true
				break
			}
		}
		bracketChanged := false
		for i, round := range bracket.Rounds {
			for j, match := range round {
				if match.SideA == oldName {
					bracketFound = true
					if !bracketNameAmbiguous {
						bracket.Rounds[i][j].SideA = newName
						bracketChanged = true
					}
				}
				if match.SideB == oldName {
					bracketFound = true
					if !bracketNameAmbiguous {
						bracket.Rounds[i][j].SideB = newName
						bracketChanged = true
					}
				}
				if match.Winner == oldName {
					bracketFound = true
					if !bracketNameAmbiguous {
						bracket.Rounds[i][j].Winner = newName
						bracketChanged = true
					}
				}
			}
		}
		// The single-3rd-place (bronze) match is a sibling of bracket.Rounds,
		// not an element of it, so the loop above never reaches it. Rename the
		// participant here too, mirroring the Rounds rename, so a replacement
		// does not leave a stale name in ThirdPlaceMatch.SideA/SideB/Winner.
		if bm := bracket.ThirdPlaceMatch; bm != nil {
			if bm.SideA == oldName {
				bracketFound = true
				if !bracketNameAmbiguous {
					bm.SideA = newName
					bracketChanged = true
				}
			}
			if bm.SideB == oldName {
				bracketFound = true
				if !bracketNameAmbiguous {
					bm.SideB = newName
					bracketChanged = true
				}
			}
			if bm.Winner == oldName {
				bracketFound = true
				if !bracketNameAmbiguous {
					bm.Winner = newName
					bracketChanged = true
				}
			}
		}
		if bracketNameAmbiguous && bracketFound {
			warnings = append(warnings, fmt.Sprintf("bracket entries named %q are ambiguous across dojos and were left unchanged; correct them manually if needed", oldName))
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
			// MatchResult carries an id per side (SideAID/SideBID/WinnerID,
			// stamped at generation, mirrors pools.go) but no per-side dojo, so
			// the id-carrying branch of matchesParticipantSide is the only
			// disambiguator available here; an id-less row falls back to name.
			if matchesParticipantSide(m.SideAID, m.SideA, pid, oldName) {
				poolMatches[i].SideA = newName
				matchesChanged = true
				matchesFound = true
			}
			if matchesParticipantSide(m.SideBID, m.SideB, pid, oldName) {
				poolMatches[i].SideB = newName
				matchesChanged = true
				matchesFound = true
			}
			if matchesParticipantSide(m.WinnerID, m.Winner, pid, oldName) {
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
	// them, treat as a warning so the caller is not forced to roll back a successful
	// participants.csv update.
	if !poolsChanged && !bracketFound && !matchesFound && oldName != newName {
		warnings = append(warnings, fmt.Sprintf("participant %q not found in draw artifacts (may be excluded by check-in filtering)", oldName))
	}

	// seeds.csv is already renamed by state.UpdateParticipant (which runs
	// before this function), so no seed cascade is needed here.

	return warnings, nil
}

// matchesParticipant reports whether a pools.csv row (rowID, rowName,
// rowDojo) is the participant being renamed (pid, oldName, oldDojo). A row
// that CARRIES an id (rowID != "") matches ONLY by id -- never by falling
// back to name+dojo, even when those happen to agree -- so a row belonging
// to a DIFFERENT competitor who merely shares the old display name and dojo
// is never rewritten. A row with NO id at all (legacy data predating
// id-stamped generation) falls back to the pre-identity (name, dojo) match.
func matchesParticipant(rowID, rowName, rowDojo, pid, oldName, oldDojo string) bool {
	if rowID != "" {
		return pid != "" && rowID == pid
	}
	return rowName == oldName && rowDojo == oldDojo
}

// matchesParticipantSide is matchesParticipant's counterpart for a single
// side of a pool-matches.csv row (SideA/SideB/Winner + their *ID sibling).
// MatchResult carries a per-side id but no per-side dojo, so an id-carrying
// side matches only by id; an id-less side falls back to name alone (there
// is no dojo on the row left to disambiguate with).
func matchesParticipantSide(rowID, rowName, pid, oldName string) bool {
	if rowID != "" {
		return pid != "" && rowID == pid
	}
	return rowName == oldName
}
