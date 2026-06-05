package engine

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// poolFinalistPlaceholderRE matches a pool-origin finalist placeholder label as
// produced by helper.GenerateFinals: "<PoolName>-<ordinal>", e.g. "Pool A-1st".
// Pool names are auto-generated as "Pool <char>" (helper.CreatePools), so a real
// competitor/team name colliding with this exact shape ("Pool …-Nth") is
// extremely unlikely in practice. NOTE: this regex now gates knockout-match
// scoreability (bracketMatchPlayable), so a participant literally named like a
// placeholder would be misclassified as unresolved. Reserving these patterns at
// the participant-name validation boundary is tracked as a follow-up (mp-igdg).
var poolFinalistPlaceholderRE = regexp.MustCompile(`^Pool .+-\d+(st|nd|rd|th)$`)

// winnerOfPlaceholderRE matches the EXACT next-round feeder placeholder the
// engine emits (buildBracketFromLeaves: "Winner of r<d>-m<i>"). Anchored so a
// real competitor named e.g. "Winner of the 2025 Cup" is NOT mistaken for an
// unresolved feeder (which would make their match unscoreable).
var winnerOfPlaceholderRE = regexp.MustCompile(`^Winner of r\d+-m\d+$`)

// isUnresolvedBracketSide reports whether a bracket side is still a forward
// reference rather than a resolved competitor: an empty structural bye slot, a
// "Winner of rX-mY" feeder, or a pool-origin finalist placeholder that has not
// been seeded yet (its feeder pool is still in progress).
func isUnresolvedBracketSide(side string) bool {
	if side == "" {
		return true
	}
	if winnerOfPlaceholderRE.MatchString(side) {
		return true
	}
	return poolFinalistPlaceholderRE.MatchString(side)
}

// bracketMatchPlayable reports whether a bracket match can be scored: both sides
// must be resolved competitors. This is the per-match replacement for the old
// bracket-wide Preview gate — a knockout match becomes playable as soon as both
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
			if poolFinalistPlaceholderRE.MatchString(m.SideA) || poolFinalistPlaceholderRE.MatchString(m.SideB) {
				return true
			}
		}
	}
	return false
}

// completedPoolNames returns poolName → isComplete for every pool in compID. A
// pool is complete when all of its matches (regular + any tiebreaker/daihyosen)
// are completed with a winner, no further tiebreaker/DH injection is pending for
// it, and — for team competitions — its daihyosen results actually broke the
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
	// complete — otherwise the mixed comp could get stuck in `pools` forever (a
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
// it replaces that pool's finalist placeholders ("Pool A-1st", …) in the bracket
// with the real pool finishers, and resolves any bye those finishers inherit.
// Pools still in progress keep their placeholders. There is NO all-pools gate: a
// knockout match becomes playable the moment both its feeder pools have
// finished, while other pools are still running.
//
// The bracket is generated and scheduled at draw time (generatePoolPreviewBracket
// → assignBracketMatchSlots), so resolving placeholders never changes a match's
// court or time — only its competitor labels. Idempotent: once a placeholder is
// replaced it is gone, so re-running only seeds newly-completed pools.
//
// Returns (resolvedNow, allResolved): how many placeholder labels were replaced
// THIS call, and whether the bracket now has zero pool-origin placeholders left
// (every pool seeded). No-op (0, false, nil) for non-mixed competitions —
// standalone playoffs brackets carry no pool placeholders.
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
		return 0, false, validationErrorf("mixed competition %s has only %d pool(s) — at least 2 are required for a knockout phase; this competition should be 'league' format", compID, len(pools))
	}

	completed, err := e.completedPoolNames(compID, comp)
	if err != nil {
		return 0, false, err
	}
	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return 0, false, err
	}
	poolWinners := comp.PoolWinners
	if poolWinners <= 0 {
		poolWinners = 2
	}

	// Build a label→player resolver for COMPLETED pools only. Incomplete pools
	// contribute nothing, so their placeholders survive untouched.
	resolver := make(map[string]string)
	for _, pool := range pools {
		if !completed[pool.PoolName] {
			continue
		}
		ps := standings[pool.PoolName]
		for rank := 1; rank <= poolWinners; rank++ {
			if rank-1 >= len(ps) {
				return 0, false, validationErrorf("pool %q is marked complete but has only %d ranked finishers (need %d)", pool.PoolName, len(ps), poolWinners)
			}
			key := fmt.Sprintf("%s-%s", pool.PoolName, helper.GetOrdinal(rank))
			resolver[key] = ps[rank-1].Player.Name
		}
	}

	resolvedNow := 0
	allResolved := false
	uerr := e.store.UpdateBracket(compID, func(bracket *state.Bracket) error {
		if bracket == nil || len(bracket.Rounds) == 0 {
			return errMatchNotFound // nothing to resolve; signal no-save
		}
		n := 0
		for ri := range bracket.Rounds {
			for mi := range bracket.Rounds[ri] {
				m := &bracket.Rounds[ri][mi]
				// Resolve SideA/SideB and the Winner field (a bye match pre-fills
				// Winner with the placeholder; later rounds carry the propagated
				// placeholder too). "Winner of …" and "" values are not resolver
				// keys, so they are left untouched.
				if name, ok := resolver[m.SideA]; ok {
					m.SideA = name
					n++
				}
				if name, ok := resolver[m.SideB]; ok {
					m.SideB = name
					n++
				}
				if name, ok := resolver[m.Winner]; ok {
					m.Winner = name
				}
			}
		}
		allResolved = !bracketHasPoolPlaceholders(bracket)
		if n == 0 {
			return errMatchNotFound // no change → skip the rewrite
		}
		resolvedNow = n
		// The bracket is now (partially) live; the legacy global Preview flag is
		// obsolete — playability is per-match from here on.
		bracket.Preview = false
		return nil
	})
	if uerr != nil && !errors.Is(uerr, errMatchNotFound) {
		return 0, false, uerr
	}
	return resolvedNow, allResolved, nil
}
