package engine

import (
	"fmt"

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
// 8), a pool-matches.csv side (MatchResult.SideAID/SideBID/WinnerID), and,
// since bc-brid, a bracket.json side (BracketMatch.SideAID/SideBID/WinnerID)
// are all records that carry an id field, so each is resolved BY ID ONLY
// (operator ruling bc-pnum): a row whose id differs from pid names a
// DIFFERENT competitor and must be left untouched even when its display name
// still matches oldName. bracket.json still carries no per-side DOJO field,
// so a row a repair could not stamp with an id (an unrepaired legacy row)
// falls back to the SAME ambiguity-guarded plain-name match this function
// has always applied there; see oldNameAmbiguous and the rename pass below,
// now scoped to id-less rows only.
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
		// (name, dojo) identity. A plain closure: the bracket branch below is
		// its only caller now that the pool-matches branch resolves by id
		// only and no longer needs this same answer.
		oldNameAmbiguous := func() (bool, error) {
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
		}

		// --- bracket.json + pool-matches.csv (WAL-staged) ---
		bracket, err := tx.LoadBracket(compID)
		if err != nil {
			return fmt.Errorf("loading bracket: %w", err)
		}
		// ID-first (bc-brid), name as the fallback ONLY for a row a repair
		// could not stamp. Pass 1: rename every (name, id) side whose id is
		// pid -- unambiguous, whatever the display name currently reads, so
		// it renames even a side whose text has already drifted from
		// oldName. This is the SAME rule pools.csv/pool-matches.csv already
		// apply (matchesParticipant), now extended to bracket.json now that
		// it carries ids too.
		bracketChanged := false
		forEachBracketSideWithID(bracket, func(name, id *string) {
			if !matchesParticipant(*id, pid) {
				return
			}
			bracketFound = true
			if *name != newName {
				*name = newName
				bracketChanged = true
			}
		})

		// Pass 2: the pre-bc-brid fallback, now scoped to ID-LESS sides only
		// -- a side that DOES carry an id, and it is not pid's (checked
		// above), belongs to a DIFFERENT competitor and must never be
		// touched by a name guess, whatever it happens to read. A plain-name
		// match is ambiguous whenever oldName is still held by another
		// CURRENT participant (oldNameAmbiguous below). In the normal flow
		// UpdateParticipant has already renamed the target participant's OWN
		// row to newName before this function runs, so any OTHER participant
		// still named oldName at this point is a DIFFERENT competitor --
		// rewriting every id-less bracket row named oldName would silently
		// reattribute their match history too (e.g. two "Tanaka Kenji" from
		// different dojos, one of whose bracket rows predates ids). When
		// ambiguous, presence is still tracked (bracketFound, so the "not
		// found in draw artifacts" fallback warning below doesn't misfire),
		// but nothing is rewritten and the operator is warned instead of a
		// guess being made.
		//
		// bracketNames below collects every id-less name actually appearing
		// in a bracket row, purely so the ambiguity check is only invoked
		// when oldName is a name an UNREPAIRED row could still need
		// rewritten -- an id-less namesake who exists in the full roster but
		// was never placed in any id-less bracket row still triggers the
		// ambiguity guard once oldName itself appears there (the collection
		// pass does not, and cannot, verify that the SPECIFIC occurrence
		// naming oldName belongs to oldName's own participant rather than
		// the namesake's -- that is exactly the ambiguity an id-less row
		// makes unresolvable). Over-warning in that shape -- refusing a safe
		// rename because an uninvolved namesake merely exists -- is the
		// accepted safe direction: a stale warning costs the operator a
		// manual check, while the silent-corruption direction costs someone
		// else's match history.
		bracketNames := make(map[string]bool)
		forEachBracketSideWithID(bracket, func(name, id *string) {
			if *id == "" && *name != "" {
				bracketNames[*name] = true
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
		forEachBracketSideWithID(bracket, func(name, id *string) {
			if *id != "" {
				// Carries an id that is NOT pid's (pass 1 already renamed
				// every pid match): a different competitor, full stop --
				// never fall back to a name guess for a row already known
				// to belong to someone else.
				return
			}
			if *name != oldName {
				return
			}
			bracketFound = true
			if !bracketNameAmbiguous {
				*name = newName
				bracketChanged = true
			}
		})
		if bracketNameAmbiguous && bracketFound {
			// Scoped to what pass 2 actually left alone: any bracket row
			// already carrying this participant's OWN id was renamed by pass
			// 1, above, before this check ever runs. Only the ID-LESS rows
			// merely named oldName -- which cannot be told apart from a
			// same-name namesake's -- are the ones left unchanged here
			// (bc-pnum review: the old wording claimed the whole bracket was
			// untouched, which is wrong whenever pid's own rows carried an
			// id).
			warnings = append(warnings, fmt.Sprintf("id-less bracket entries named %q are ambiguous across dojos and were left unchanged; any bracket row already carrying this participant's id was renamed. Correct the id-less ones manually if needed", oldName))
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
				if matchesParticipant(rowID, pid) {
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

// forEachBracketSideWithID calls fn once for each of a bracket's three
// (name, id) side pairs -- SideA/SideAID, SideB/SideBID, Winner/WinnerID --
// across every round, plus the ThirdPlaceMatch sibling's when present. Both
// pointers ALIAS the stored match (indexed slice access, never a
// range-copy), so a caller mutating through them edits the bracket in
// place. Shared by ReplaceParticipantInDraw's id-based rename pass, its
// id-less name-collection pass, and its name-based fallback rename pass,
// which would otherwise hand-copy the same enumeration three times
// (bc-brid; this replaced the pre-bc-brid, name-only forEachBracketSide,
// whose two callers both needed the id half once bracket.json grew one).
func forEachBracketSideWithID(b *state.Bracket, fn func(name, id *string)) {
	for i := range b.Rounds {
		for j := range b.Rounds[i] {
			m := &b.Rounds[i][j]
			fn(&m.SideA, &m.SideAID)
			fn(&m.SideB, &m.SideBID)
			fn(&m.Winner, &m.WinnerID)
		}
	}
	if bm := b.ThirdPlaceMatch; bm != nil {
		fn(&bm.SideA, &bm.SideAID)
		fn(&bm.SideB, &bm.SideBID)
		fn(&bm.Winner, &bm.WinnerID)
	}
}
