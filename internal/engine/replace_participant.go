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

		// oldNameAmbiguous reports whether the FULL current roster holds
		// ANOTHER participant (excluding the one being renamed) still named
		// oldName. bracket.json (no per-side id OR dojo) and an id-less
		// pool-matches.csv row (no per-side dojo) both have to fall back to
		// a plain name match, which is unresolvable whenever another CURRENT
		// participant still answers to oldName -- e.g. two "Tanaka Kenji"
		// from different dojos. the bc-pnum review: this scan used to live
		// ONLY inside the bracket branch, gated on oldName already
		// appearing in the bracket; hoisted here as ONE shared, lazily-
		// computed (and cached) check so the pool-matches branch below can
		// consult the SAME answer instead of trusting an id-less row's name
		// match unconditionally.
		//
		// The candidate pool is the FULL current roster, NOT filterCheckedIn's
		// narrower one (second-Opus-pass finding: an earlier version of this
		// scoped to filterCheckedIn(participants), which is wrong in BOTH
		// directions here). check-in PUT/DELETE and POST /participants are all
		// legal while a competition sits in draw-ready -- the bracket was
		// drawn from whatever check-in state held AT THAT TIME, which is not
		// necessarily today's. filterCheckedIn(participants) answers "would
		// today's roster be placed in a FRESH draw", not "who is actually
		// sitting in THIS bracket" -- the two diverge the moment check-in
		// state changes after the draw, and diverging the wrong way silently
		// corrupts data: a namesake who WAS placed in the bracket and has
		// since been checked OUT drops out of filterCheckedIn's pool, so
		// oldNameAmbiguous goes false and her bracket rows get silently
		// rewritten too -- exactly the corruption this guard exists to stop.
		// The full roster has no such blind spot: checking a participant out
		// doesn't remove her from participants.csv, only from the check-in
		// snapshot.
		//
		// Exclusion of the target herself (the bc-pnum review) checks BOTH:
		// the id-aware path (pid is her real participant id, matches p.ID
		// directly) and the (name, dojo)-identity path via
		// helper.CompetitorKey with id forced empty on both sides. The
		// second form is needed for a legacy id-less roster: SaveParticipants
		// mints a fresh id for ANY id-less row the instant it is next
		// written (marshalParticipantsCSV), so a row that was id-less when
		// the caller captured pid (a "name|dojo" composite key,
		// state.resolveParticipantIndex's own fallback) can carry a
		// brand-new, unrelated real id by the time this scan runs -- an
		// id-to-id comparison against the OLD synthetic pid would then never
		// match her own row, and a dojo-only edit would warn about itself as
		// an "ambiguous namesake". Comparing by (name, dojo) with ids forced
		// empty recognises her regardless of whether a real id has since
		// landed on her row.
		var (
			oldNameAmbiguousComputed bool
			oldNameAmbiguousCached   bool
		)
		oldNameAmbiguous := func() (bool, error) {
			if oldNameAmbiguousComputed {
				return oldNameAmbiguousCached, nil
			}
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
				oldNameAmbiguousCached = true
				break
			}
			oldNameAmbiguousComputed = true
			return oldNameAmbiguousCached, nil
		}

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
		poolMatchesAmbiguous := false
		for i, m := range poolMatches {
			// the bc-pnum review: an id-less side falls back to matching by
			// NAME ALONE (MatchResult carries no per-side dojo). Two guards
			// gate that fallback, on top of the identity check
			// matchesParticipantSide already applies:
			//  1. The row's own pool must be one the pools.csv pass above
			//     actually touched for THIS rename (affectedPools) -- a
			//     same-named row in an UNRELATED pool (e.g. a different
			//     dojo's namesake who was never the rename target, reachable
			//     because pools.csv's own match is (name, dojo)-scoped but
			//     pool-matches rows carry no dojo to repeat that scoping)
			//     must not be rewritten just because the name matches.
			//  2. Even within the right pool, if oldNameAmbiguous AND that
			//     SAME pool (post-rename) still holds another player named
			//     oldName (poolHasNamesake), a name-only row cannot tell the
			//     two apart -- skip it and warn, mirroring the bracket
			//     branch, rather than guessing.
			// An id-carrying side is unaffected by either guard: its
			// identity is already unambiguous.
			pn, pnOK := poolNameFromMatchID(m.ID)
			inAffectedPool := pnOK && affectedPools[pn]

			applySide := func(rowID, rowName string, setName func(string)) error {
				if rowID != "" {
					if matchesParticipantSide(rowID, rowName, pid, oldName) {
						setName(newName)
						matchesChanged = true
						matchesFound = true
					}
					return nil
				}
				if rowName != oldName || !inAffectedPool {
					return nil
				}
				amb, aerr := oldNameAmbiguous()
				if aerr != nil {
					return aerr
				}
				if amb && poolHasNamesake(pools, pn, oldName) {
					matchesFound = true
					poolMatchesAmbiguous = true
					return nil
				}
				setName(newName)
				matchesChanged = true
				matchesFound = true
				return nil
			}

			if err := applySide(m.SideAID, m.SideA, func(n string) { poolMatches[i].SideA = n }); err != nil {
				return err
			}
			if err := applySide(m.SideBID, m.SideB, func(n string) { poolMatches[i].SideB = n }); err != nil {
				return err
			}
			if err := applySide(m.WinnerID, m.Winner, func(n string) { poolMatches[i].Winner = n }); err != nil {
				return err
			}
		}
		if poolMatchesAmbiguous {
			warnings = append(warnings, fmt.Sprintf("pool-match entries named %q are ambiguous within a pool and were left unchanged; correct them manually if needed", oldName))
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
// id-stamped generation) falls back to the pre-identity (name, dojo) match,
// compared via helper.CompetitorKey (the bc-pnum review) so this identity
// match and the dojo-conflict warning above use ONE normalisation (case,
// diacritics, whitespace) rather than this raw string compare disagreeing
// with the warning's helper.NormalizeParticipantName-based one.
func matchesParticipant(rowID, rowName, rowDojo, pid, oldName, oldDojo string) bool {
	if rowID != "" {
		return pid != "" && rowID == pid
	}
	return helper.CompetitorKey("", rowName, rowDojo) == helper.CompetitorKey("", oldName, oldDojo)
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

// poolHasNamesake reports whether pool poolName still holds a player named
// name, evaluated AFTER the pools.csv rename pass has already run (PR #416
// the review finding). Because the rename target's OWN pools.csv row has by this
// point already been rewritten to newName, a remaining player still named
// `name` in the same pool is necessarily a DIFFERENT competitor, not a stale
// read of the just-renamed row -- used by the pool-matches ambiguity guard
// to tell whether an id-less match row's name-only resolution is trustworthy
// within that specific pool.
func poolHasNamesake(pools []helper.Pool, poolName, name string) bool {
	for _, pool := range pools {
		if pool.PoolName != poolName {
			continue
		}
		for _, p := range pool.Players {
			if p.Name == name {
				return true
			}
		}
	}
	return false
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
