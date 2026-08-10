package engine

import (
	"errors"
	"fmt"
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// isUnresolvedBracketSide reports whether a bracket side is still a forward
// reference rather than a resolved competitor: an empty structural bye slot, a
// "Winner of rX-mY" feeder, or a pool-origin finalist placeholder that has not
// been seeded yet (its feeder pool is still in progress).
//
// The two placeholder patterns are defined authoritatively in
// helper.IsReservedParticipantName (mp-igdg); knockout.go no longer
// duplicates them.
func isUnresolvedBracketSide(side string) bool {
	if side == "" {
		return true
	}
	return helper.IsReservedParticipantName(side)
}

// bracketMatchPlayable reports whether a bracket match can be scored: both sides
// must be resolved competitors. This is the per-match replacement for the old
// bracket-wide Preview gate, a knockout match becomes playable as soon as both
// its feeder pools (or feeder matches) have produced real competitors, with NO
// wait for the rest of the pool phase. Standalone (knockout-only) competitions
// satisfy this from draw time because their round-1 leaves are real players.
func bracketMatchPlayable(m *state.BracketMatch) bool {
	return !isUnresolvedBracketSide(m.SideA) && !isUnresolvedBracketSide(m.SideB)
}

// bracketHasPoolPlaceholders reports whether any side anywhere in the bracket is
// still an unseeded pool-origin finalist placeholder. Used to decide when every
// pool has been folded into the knockout (status pools → playoffs).
func bracketHasPoolPlaceholders(b *state.Bracket) bool {
	if b == nil {
		return false
	}
	for _, round := range b.Rounds {
		for _, m := range round {
			if helper.IsPoolFinalistPlaceholder(m.SideA) || helper.IsPoolFinalistPlaceholder(m.SideB) {
				return true
			}
		}
	}
	return false
}

// completedPoolNames returns poolName → isComplete for every pool in compID. A
// pool is complete when all of its matches (regular + any tiebreaker/daihyosen)
// are completed with a winner, no further tiebreaker/DH injection is pending for
// it, and, for team competitions, its daihyosen results actually broke the
// ties (no cycle). Tiebreaker/DH injection runs comp-wide first (idempotent).
func (e *Engine) completedPoolNames(compID string, comp *state.Competition) (map[string]bool, error) {
	isTeam := comp != nil && comp.TeamSize > 0

	// Inject supplementary tie-break matches for any tied pools. Both injectors
	// are idempotent and only add matches for pools that need them, so a pool
	// that just became tied flips to "not complete" on the next call.
	if isTeam {
		if _, err := e.InjectPoolDaihyosenMatches(compID); err != nil {
			return nil, err
		}
	} else {
		if _, err := e.InjectTiebreakerMatches(compID); err != nil {
			return nil, err
		}
	}

	pools, err := e.store.LoadPools(compID)
	if err != nil {
		return nil, err
	}
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	playerCount := make(map[string]int, len(pools))
	done := make(map[string]bool, len(pools))
	seen := make(map[string]bool, len(pools))
	for _, p := range pools {
		done[p.PoolName] = true // optimistic; cleared below on any incomplete match
		playerCount[p.PoolName] = len(p.Players)
	}
	for _, m := range matches {
		pn, ok := poolNameFromMatchID(m.ID)
		if !ok {
			continue
		}
		seen[pn] = true
		complete := m.Status == state.MatchStatusCompleted
		if (IsTiebreakerMatchID(m.ID) || IsPoolDaihyosenMatchID(m.ID)) && m.Winner == "" {
			complete = false
		}
		if !complete {
			if _, known := done[pn]; known {
				done[pn] = false
			}
		}
	}
	// A pool with NO matches on disk is "complete" ONLY when it has exactly one
	// participant: round-robin (and partial) match generation skips pools of size
	// 0/1, so a lone qualifier legitimately produces zero matches and is already
	// decided (they are the pool's 1st place). A 0-participant pool, or a ≥2-player
	// pool with no matches yet (draw not generated / mid-generation), is NOT
	// complete, otherwise the mixed comp could get stuck in `pools` forever (a
	// single-competitor pool's placeholder would never resolve).
	for pn := range done {
		if !seen[pn] {
			done[pn] = playerCount[pn] == 1
		}
	}

	// Team competitions: a pool whose daihyosen results produced a cycle (ties
	// not broken) must not be treated as resolvable.
	if isTeam {
		standings, serr := e.CalculatePoolStandings(compID)
		if serr != nil {
			return nil, serr
		}
		overrides, _ := e.store.LoadOverrides(compID)
		var poolRanks map[string]map[string]int
		if overrides != nil {
			poolRanks = overrides.PoolRanks
		}
		for pn, ok := range done {
			if !ok {
				continue
			}
			scoped := map[string][]state.PlayerStanding{pn: standings[pn]}
			if dhCycleExists(scoped, matches, poolRanks) {
				done[pn] = false
			}
		}
	}
	return done, nil
}

// ResolveQualifiedPools incrementally seeds the in-place knockout bracket of a
// mixed (Pools + Knockout) competition. For EVERY pool whose results are final
// it writes that pool's real finishers into the bracket slots their finalist
// placeholders ("Pool A-1st", …) occupy, and resolves any bye those finishers
// inherit. Pools still in progress keep their placeholders. There is NO all-pools
// gate: a knockout match becomes playable the moment both its feeder pools have
// finished, while other pools are still running.
//
// Resolution is RE-SEEDABLE, not a one-shot string replace. Each match carries
// the label its slot held at draw time (PlaceholderA/B/Winner, written once by
// buildBracketFromLeaves), and THAT is what the resolver keys on. So if an
// operator re-scores a completed pool match after that pool was already seeded,
// changing the 1st/2nd finisher, the new finisher overwrites the stale name in
// the same slot instead of being silently dropped: the live side no longer holds
// the placeholder string, but the match's own record of it is immutable. The
// bracket's court/time slots are assigned at draw time and never change here,
// only competitor labels.
//
// It used to RECOMPUTE that template instead, rerunning the whole draw and
// buildBracketFromLeaves on every call and matching it against the live
// bracket BY POSITION. That was correct only while the placement algorithm never
// changed: an operator who upgraded between a competition's draw and the end of
// its pool phase (ordinary for a two-day event) would have had qualifiers written
// into the WRONG slots of a live knockout, with nothing to detect it — the
// structural guards it carried only caught differing round/match COUNTS, which a
// placement change does not alter. Reading the persisted placeholder removes the
// dependency on the draw algorithm entirely, for this transition and every future
// one, and takes a full bracket rebuild off a path that runs on every pool-match
// completion. Do not reintroduce the recompute.
//
// A bracket drawn BEFORE those fields existed carries none, which is exactly how
// such a file is detected; it is reconstructed once from the frozen v1 builder
// (legacy_template_v1.go) and the fields are backfilled and saved on first use.
//
// Known limitation (mp-e2k1): re-seeding repaints round-0 leaves and
// bye-propagated sides, but does NOT invalidate a DOWNSTREAM knockout match that
// was already scored during the pool phase if its feeder pool is later re-scored
// to a different finisher.
//
// Returns (resolvedNow, allResolved): how many bracket sides changed THIS call,
// and whether the bracket now has zero pool-origin placeholders left (every pool
// seeded). No-op (0, false, nil) for non-mixed competitions, standalone playoffs
// brackets carry no pool placeholders.
func (e *Engine) ResolveQualifiedPools(compID string) (int, bool, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return 0, false, err
	}
	if comp == nil || comp.Format != state.CompFormatMixed {
		return 0, false, nil
	}

	pools, err := e.store.LoadPools(compID)
	if err != nil {
		return 0, false, err
	}
	// Mixed requires ≥2 pools by invariant (enforced at draw in generatePools);
	// defend against legacy/hand-edited data so we never seed a degenerate
	// single-pool "knockout".
	if len(pools) < 2 {
		return 0, false, validationErrorf("mixed competition %s has only %d pool(s), at least 2 are required for a knockout phase; this competition should be 'league' format", compID, len(pools))
	}

	completed, err := e.completedPoolNames(compID, comp)
	if err != nil {
		return 0, false, err
	}
	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return 0, false, err
	}
	poolWinners := comp.EffectivePoolWinners()

	// Build a label→player resolver for COMPLETED pools only. Incomplete pools
	// contribute nothing, so their placeholders survive untouched.
	resolver := make(map[string]string)
	for _, pool := range pools {
		if !completed[pool.PoolName] {
			continue
		}
		ps := standings[pool.PoolName]
		for rank := 1; rank <= poolWinners; rank++ {
			key := fmt.Sprintf("%s-%s", pool.PoolName, helper.GetOrdinal(rank))
			if rank-1 >= len(ps) {
				// Degenerate pool (hand-edited data / legacy import): fewer
				// finishers than PoolWinners. Map the unfillable placeholder
				// to "" (bye) so the bracket slot auto-resolves. Draw-time
				// validation prevents this in supported flows.
				log.Printf("engine.ResolveQualifiedPools: pool %q has only %d ranked finisher(s) but PoolWinners=%d; treating rank %d as bye", pool.PoolName, len(ps), poolWinners, rank)
				resolver[key] = ""
				continue
			}
			resolver[key] = ps[rank-1].Player.Name
		}
	}

	poolNames := make([]string, len(pools))
	for i, p := range pools {
		poolNames[i] = p.PoolName
	}

	resolvedNow := 0
	allResolved := false
	backfilled := false
	uerr := e.store.UpdateBracket(compID, func(bracket *state.Bracket) error {
		if bracket == nil || len(bracket.Rounds) == 0 {
			return errMatchNotFound // nothing to resolve; signal no-save
		}
		// Pre-Phase-4 bracket: no slot remembers its draw-time label. Rebuild
		// them ONCE from the frozen v1 draw and write them in, so from here on
		// this competition resolves off its own record like any new bracket.
		// The write is part of this same UpdateBracket mutation, so a backfill
		// is never persisted without the resolution it enabled, nor lost by a
		// call that resolved nothing (see the save condition below).
		if !bracketHasDrawPlaceholders(bracket) {
			backfilled = backfillDrawPlaceholdersV1(bracket, poolNames, poolWinners)
		}
		n := 0
		resolveMatch := func(m *state.BracketMatch) {
			// PlaceholderA/B/Winner hold the ORIGINAL draw labels (or
			// "Winner of …"/""), stable across re-scores. Only completed-pool
			// placeholders are resolver keys; "Winner of" and "" never are, so
			// already-scored knockout sides and unresolved feeders are untouched.
			// Compare against the current value so an unchanged re-run is a no-op.
			if name, ok := resolver[m.PlaceholderA]; ok && m.SideA != name {
				m.SideA = name
				n++
			}
			if name, ok := resolver[m.PlaceholderB]; ok && m.SideB != name {
				m.SideB = name
				n++
			}
			if name, ok := resolver[m.PlaceholderWinner]; ok && m.Winner != name {
				m.Winner = name
				n++ // count Winner-only changes so a bye-propagated Winner fix is persisted
			}
		}
		for ri := range bracket.Rounds {
			for mi := range bracket.Rounds[ri] {
				resolveMatch(&bracket.Rounds[ri][mi])
			}
		}
		if bracket.ThirdPlaceMatch != nil {
			// Inert in every current draw: the bronze is fed by semifinal
			// losers, so its placeholders are empty and no lookup can hit.
			// Present so a bronze that ever DOES carry one is not the single
			// slot the resolver skips.
			resolveMatch(bracket.ThirdPlaceMatch)
		}
		// Auto-complete newly created bye matches: a resolver mapping to ""
		// (degenerate pool) leaves a match with one empty side still
		// Scheduled. Mirror buildBracketFromLeaves's bye logic and
		// propagate winners so downstream matches become playable.
		// Guard: only auto-complete when the non-empty side is a resolved
		// competitor, NOT a still-unresolved placeholder (its feeder pool
		// may still be in progress).
		for ri := range bracket.Rounds {
			for mi := range bracket.Rounds[ri] {
				m := &bracket.Rounds[ri][mi]
				if m.Status != state.MatchStatusScheduled {
					continue
				}
				aEmpty := m.SideA == ""
				bEmpty := m.SideB == ""
				aResolved := !aEmpty && !isUnresolvedBracketSide(m.SideA)
				bResolved := !bEmpty && !isUnresolvedBracketSide(m.SideB)
				if aEmpty && bResolved {
					m.Winner = m.SideB
					m.Status = state.MatchStatusCompleted
					e.propagateBracketWinner(bracket, ri, mi)
					n++
				} else if bEmpty && aResolved {
					m.Winner = m.SideA
					m.Status = state.MatchStatusCompleted
					e.propagateBracketWinner(bracket, ri, mi)
					n++
				} else if aEmpty && bEmpty {
					m.Status = state.MatchStatusCompleted
					e.propagateBracketWinner(bracket, ri, mi)
					n++
				}
			}
		}

		allResolved = !bracketHasPoolPlaceholders(bracket)
		if n == 0 && !backfilled {
			return errMatchNotFound // no effective change → skip the rewrite
		}
		resolvedNow = n
		if n > 0 {
			// The bracket is now (partially) live; the legacy global Preview flag
			// is obsolete, playability is per-match from here on. A backfill-only
			// call resolved nothing, so it must not flip this.
			bracket.Preview = false
		}
		return nil
	})
	if uerr != nil && !errors.Is(uerr, errMatchNotFound) {
		return 0, false, uerr
	}
	return resolvedNow, allResolved, nil
}
