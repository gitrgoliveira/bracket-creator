package engine

import (
	"fmt"
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"

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
// 8) and a pool-matches.csv side (MatchResult.SideAID/SideBID/WinnerID) are
// records that carry an id field, so both are resolved BY ID ONLY (operator
// ruling bc-pnum): a row whose id differs from pid names a DIFFERENT
// competitor and must be left untouched even when its display name still
// matches oldName, and a row with NO id at all is simply never matched --
// there is no (name, dojo) fallback. bracket.json carries no per-side id or
// dojo at all (BracketMatch has neither field, out of scope for bc-pnum), so
// a plain-name match there is unavoidably ambiguous whenever oldName is
// shared by more than one CURRENT participant; see the ambiguity guard
// below, which stays for that bracket branch only.
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
				if !matchesParticipant(player.ID, pid) {
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
			// Dojos are compared under the roster's identity normalisation
			// (case, diacritics, whitespace), the same rule the draw itself
			// applies through helper.dojoKey, so "Mumeishi" and "mumeishi"
			// count as one dojo here exactly as they do when the pools are
			// formed; a raw string compare under-reported the conflict.
			for _, pool := range pools {
				if !affectedPools[pool.PoolName] {
					continue
				}
				dojoCount := map[string]int{}
				for _, p := range pool.Players {
					dojoCount[helper.NormalizeParticipantName(p.Dojo)]++
				}
				if count := dojoCount[helper.NormalizeParticipantName(newDojo)]; count > 1 {
					warnings = append(warnings, fmt.Sprintf("dojo conflict: %q appears %d times in %s", newDojo, count, pool.PoolName))
				}
			}
		}

		// oldNameAmbiguous reports whether the FULL current roster -- never
		// filterCheckedIn's narrower one, since the bracket may have been
		// drawn from a check-in state that has since changed -- holds ANOTHER
		// participant still named oldName, excluded by id (pid) or by
		// (name, dojo) identity. Memoized via sync.OnceValues: bracket.json
		// and the id-less pool-matches branch below both may need the same
		// answer.
		oldNameAmbiguous := sync.OnceValues(func() (bool, error) {
			participants, perr := tx.LoadParticipants(compID, current.EffectiveWithZekkenName())
			if perr != nil {
				return false, fmt.Errorf("loading participants for rename ambiguity check: %w", perr)
			}
			targetKey := helper.CompetitorKey("", oldName, oldDojo)
			for _, p := range participants {
				if p.Name != oldName {
					continue
				}
				if pid != "" && p.ID == pid {
					continue // the participant being renamed herself, by id
				}
				if helper.CompetitorKey("", p.Name, p.Dojo) == targetKey {
					continue // the participant being renamed herself, by (name, dojo)
				}
				return true, nil
			}
			return false, nil
		})

		// --- bracket.json + pool-matches.csv (WAL-staged) ---
		bracket, err := tx.LoadBracket(compID)
		if err != nil {
			return fmt.Errorf("loading bracket: %w", err)
		}
		// bracket.json carries no per-side id AND no per-side dojo (BracketMatch
		// has neither field), so a plain-name match is ambiguous whenever
		// oldName is still held by another CURRENT participant (oldNameAmbiguous
		// above). In the normal flow UpdateParticipant has already renamed the
		// target participant's OWN row to newName before this function runs, so
		// any OTHER participant still named oldName at this point is a
		// DIFFERENT competitor -- rewriting every bracket row named oldName
		// would silently reattribute their match history too (e.g. two "Tanaka
		// Kenji" from different dojos). When ambiguous, presence is still
		// tracked (bracketFound, so the "not found in draw artifacts" fallback
		// warning below doesn't misfire), but nothing is rewritten and the
		// operator is warned instead of a guess being made.
		//
		// Pass 1 below (via forEachBracketSide, the bc-pnum review) collects
		// every name actually appearing in a bracket row (SideA/SideB/Winner,
		// Rounds + ThirdPlaceMatch), purely so the ambiguity check below is
		// only invoked when oldName is a name this bracket could possibly need
		// rewritten -- the empty-bracket case folds into this naturally, since
		// an empty bracket contributes no names. A namesake who exists in the
		// full roster but was never placed in ANY bracket row still triggers
		// the ambiguity guard once oldName itself does appear in the bracket
		// (the pass-1 check does not, and cannot, verify that the SPECIFIC
		// bracket occurrence naming oldName belongs to oldName's own
		// participant rather than the namesake's -- that is exactly the
		// ambiguity bracket.json's lack of ids makes unresolvable).
		// Over-warning in that shape -- refusing a safe rename because an
		// uninvolved namesake merely exists -- is the accepted safe direction:
		// a stale warning costs the operator a manual check, while the
		// silent-corruption direction costs someone else's match history.
		bracketNames := make(map[string]bool)
		forEachBracketSide(bracket, func(s *string) {
			if *s != "" {
				bracketNames[*s] = true
			}
		})
		bracketNameAmbiguous := false
		if bracketNames[oldName] {
			amb, aerr := oldNameAmbiguous()
			if aerr != nil {
				return aerr
			}
			bracketNameAmbiguous = amb
		}
		bracketChanged := false
		forEachBracketSide(bracket, func(s *string) {
			if *s == oldName {
				bracketFound = true
				if !bracketNameAmbiguous {
					*s = newName
					bracketChanged = true
				}
			}
		})
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
			// ID-only (operator ruling bc-pnum): a pool-matches row is a
			// record that carries an id field (SideAID/SideBID/WinnerID), so
			// it is resolved by id only. A row with no id for a given side
			// is simply never renamed here -- there is no name-only
			// fallback: the prior version compared bare names within the
			// rename's own pool (affectedPools) and warned on ambiguity via
			// poolHasNamesake; both the fallback and its guard are removed,
			// not merely made unreachable.
			applySide := func(rowID string, setName func(string)) {
				if rowID != "" && pid != "" && rowID == pid {
					setName(newName)
					matchesChanged = true
					matchesFound = true
				}
			}

			applySide(m.SideAID, func(n string) { poolMatches[i].SideA = n })
			applySide(m.SideBID, func(n string) { poolMatches[i].SideB = n })
			applySide(m.WinnerID, func(n string) { poolMatches[i].Winner = n })
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

// matchesParticipant reports whether a pools.csv row (rowID) is the
// participant being renamed (pid). ID-only (operator ruling bc-pnum): a
// pools.csv row carries an id field (helper.Player.ID, column 8), so it is
// resolved by id only. A row with no id at all never matches -- there is no
// (name, dojo) fallback -- so a row belonging to a DIFFERENT competitor who
// merely shares the old display name and dojo is never rewritten, and an
// id-less legacy row simply cannot be found by this rename cascade.
func matchesParticipant(rowID, pid string) bool {
	return rowID != "" && pid != "" && rowID == pid
}

// forEachBracketSide calls fn once for each of a bracket's per-side name
// fields: every round's SideA/SideB/Winner, plus the ThirdPlaceMatch
// sibling's when present (the bc-pnum review). fn receives a pointer that
// ALIASES the stored match (indexed slice access, never a range-copy), so a
// caller mutating through it edits the bracket in place. Shared by
// ReplaceParticipantInDraw's name-collection pass and its rename pass, which
// used to hand-copy the same 4-line SideA/SideB/Winner/ThirdPlaceMatch
// enumeration twice.
func forEachBracketSide(b *state.Bracket, fn func(*string)) {
	for i := range b.Rounds {
		for j := range b.Rounds[i] {
			fn(&b.Rounds[i][j].SideA)
			fn(&b.Rounds[i][j].SideB)
			fn(&b.Rounds[i][j].Winner)
		}
	}
	if bm := b.ThirdPlaceMatch; bm != nil {
		fn(&bm.SideA)
		fn(&bm.SideB)
		fn(&bm.Winner)
	}
}
