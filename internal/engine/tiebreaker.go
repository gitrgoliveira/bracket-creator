package engine

import (
	"fmt"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// IsTiebreakerMatchID reports whether matchID identifies a supplementary
// ippon-shobu tiebreaker match (IDs of the form "Pool X-TB-N"). Suffix-anchored
// via hasNumericSuffixAfter (daihyosen.go) for the same reason as its
// IsPoolDaihyosenMatchID sibling: a plain substring match would misclassify a
// regular match in a pool whose name happens to contain "-TB-".
func IsTiebreakerMatchID(matchID string) bool {
	return hasNumericSuffixAfter(matchID, "-TB-")
}

// teamStandingPoints and individualStandingPoints compute the single packed
// ranking score for a standing. The packing encodes the full ORDERED tiebreak
// chain into one integer, so comparing the score is equivalent to comparing
// each criterion in priority order: two competitors with equal scores are tied
// on every official criterion (and a difference in any criterion, however far
// down the chain, produces different scores). This is the single source of
// truth for both the standings sort (scoring.go) and tie detection
// (detectPoolTies), so the two can never disagree on what "tied" means.
//
// Team chain (CLAUDE.md): W, L, T, IV (individual victories), IL, IT,
// PW (points won), PL (points lost).
func teamStandingPoints(s state.PlayerStanding) int {
	return s.Wins*100_000_000_000 - s.Losses*1_000_000_000 + s.Draws*10_000_000 +
		s.IndividualWins*100_000 - s.IndividualLosses*10_000 + s.IndividualDraws*1_000 +
		s.PointsWon*100 - s.PointsLost
}

// individualStandingPoints packs the individual chain: W, L, D, ippons given,
// ippons taken.
func individualStandingPoints(s state.PlayerStanding) int {
	return s.Wins*100_000_000 - s.Losses*1_000_000 + s.Draws*10_000 + s.IpponsGiven*100 - s.IpponsTaken
}

// detectPoolTies walks a sorted (descending Points) standings slice and returns
// the POSITIONS of every group of 2+ competitors that share the same Points
// value. Each inner slice holds the 0-based positions (indices into the passed
// sorted standings) of one tied group, top-to-bottom; e.g. [][]int{{1,2}} means
// the 2nd- and 3rd-placed competitors are tied, and [][]int{{0,1},{3,4}} means
// two separate tied groups. The result is empty (len 0) when there are no ties.
//
// Points encodes the full ordered tiebreak chain (see teamStandingPoints /
// individualStandingPoints), so equal Points means genuinely tied on every
// official criterion, for both team and individual competitions. The caller
// MUST pass standings already sorted by Points descending; the returned indices
// point straight back into that slice.
func detectPoolTies(standings []state.PlayerStanding) [][]int {
	var groups [][]int
	i := 0
	for i < len(standings) {
		j := i + 1
		for j < len(standings) && standings[j].Points == standings[i].Points {
			j++
		}
		if j-i >= 2 {
			g := make([]int, 0, j-i)
			for k := i; k < j; k++ {
				g = append(g, k)
			}
			groups = append(groups, g)
		}
		i = j
	}
	return groups
}

// standingsAt resolves a position group from detectPoolTies back into the
// standings it indexes, preserving order. Out-of-range positions are skipped
// defensively (the caller always passes indices straight from detectPoolTies
// over the same slice, so this is a guard, not an expected path).
func standingsAt(standings []state.PlayerStanding, positions []int) []state.PlayerStanding {
	group := make([]state.PlayerStanding, 0, len(positions))
	for _, idx := range positions {
		if idx >= 0 && idx < len(standings) {
			group = append(group, standings[idx])
		}
	}
	return group
}

// applyTiebreakSort re-orders each tied group in `sorted` (in place) by per-group
// win count from the supplementary bouts whose ID satisfies isSupplementaryID
// (TB ippon-shobu or DH representative). Tied groups are located via
// detectPoolTies, the single source of the Points-equality walk, so the two
// callers (TB, DH) share one implementation. Win counts are scoped to bouts
// between members of the same tied group, so an unrelated group's results never
// bleed across; a group with no decided supplementary bouts is left untouched.
func applyTiebreakSort(sorted []state.PlayerStanding, matches []state.MatchResult, isSupplementaryID func(string) bool) {
	for _, positions := range detectPoolTies(sorted) {
		i := positions[0]
		j := positions[len(positions)-1] + 1
		groupNames := make(map[string]bool, j-i)
		for k := i; k < j; k++ {
			groupNames[sorted[k].Player.Name] = true
		}
		groupWins := map[string]int{}
		for _, m := range matches {
			if !isSupplementaryID(m.ID) || m.Status != state.MatchStatusCompleted || m.Winner == "" {
				continue
			}
			if groupNames[m.SideA] && groupNames[m.SideB] {
				groupWins[m.Winner]++
			}
		}
		if len(groupWins) > 0 {
			sort.SliceStable(sorted[i:j], func(a, b int) bool {
				return groupWins[sorted[i+a].Player.Name] > groupWins[sorted[i+b].Player.Name]
			})
		}
	}
}

// tieAffectsAdvancement reports whether a tied group (identified by its 0-based
// positions from detectPoolTies over the sorted standings) can change the pool
// outcome and therefore warrants a supplementary bout.
//
// A pool seeds its knockout from the top EffectivePoolWinners of each pool, and
// 1st-place finishers get byes (bracket.go ApplyPoolAdjustments), so every
// position in [1..poolWinners] is a distinct, consequential seed. A tied group
// whose BEST position is already past the cutoff (positions[0]+1 > poolWinners)
// sits entirely among eliminated ranks: those teams share that rank and no
// supplementary ippon-shobu / daihyosen is played. This mirrors the rule that a
// supplementary bout is held only "to determine their relative ranking" where
// that ranking matters (running_a_kendo_tournament.md:405/441), and the
// band-aware LeagueTiebreakCandidates gate used for team leagues.
func tieAffectsAdvancement(positions []int, poolWinners int) bool {
	if len(positions) == 0 {
		return false
	}
	// positions[0] is 0-based into the descending-sorted standings; +1 makes it
	// the 1-based finishing rank of the best-placed member of the tied group.
	return positions[0]+1 <= poolWinners
}

// tiebreakerPairKey returns a canonical (order-independent) key for a
// pair of player names, used to detect already-existing TB matches.
func tiebreakerPairKey(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}

// generateTiebreakerMatches creates the round-robin MatchResult entries
// for tiedGroup. existingTBCount is the current number of TB matches in
// the pool (used to produce unique TB-N indices). court is the court
// label assigned to the pool. Pairs already in existingPairs are skipped.
func generateTiebreakerMatches(poolName string, tiedGroup []state.PlayerStanding, existingTBCount int, court string, existingPairs map[string]bool) []state.MatchResult {
	var results []state.MatchResult
	idx := existingTBCount
	for _, a := range tiedGroup {
		for _, b := range tiedGroup {
			if a.Player.Name >= b.Player.Name {
				continue // only generate each pair once (a < b alphabetically)
			}
			key := tiebreakerPairKey(a.Player.Name, b.Player.Name)
			if existingPairs[key] {
				continue
			}
			results = append(results, state.MatchResult{
				ID:     fmt.Sprintf("%s-TB-%d", poolName, idx),
				SideA:  a.Player.Name,
				SideB:  b.Player.Name,
				Status: state.MatchStatusScheduled,
				Court:  court,
			})
			existingPairs[key] = true
			idx++
		}
	}
	return results
}

// InjectTiebreakerMatches inspects all pool standings for compID after
// regular pool matches are complete. For every tied group (same Points
// after the full cascade), it generates a round-robin of ippon-shobu
// tiebreaker matches, appends them to the pool-matches CSV, and
// regenerates the schedule. Returns the newly injected matches (nil
// when there are no ties or all TB pairs already exist).
func (e *Engine) InjectTiebreakerMatches(compID string) ([]state.MatchResult, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}

	// Supplementary ippon-shobu bouts are held only where the tie affects
	// advancement/seeding (see tieAffectsAdvancement): top poolWinners advance.
	poolWinners := comp.EffectivePoolWinners()

	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}

	allMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	// Scan existing TB matches per pool for idempotency and ID sequencing.
	type poolTBInfo struct {
		existingPairs map[string]bool
		count         int
	}
	poolTB := map[string]*poolTBInfo{}
	poolCourt := map[string]string{}
	// regularIncomplete[pool] becomes true if ANY regular (non-TB) match in the
	// pool is not yet completed. Tiebreakers must only be injected once a pool's
	// regular round-robin is finished, otherwise an intermediate, partial-result
	// tie (e.g. everyone 0–0 after one match) would spuriously inject TB matches
	// that a later result then breaks, leaving orphaned scheduled TB matches that
	// never clear. (The pre-incremental caller enforced this via a comp-wide
	// "all regular matches complete" gate; per-pool seeding needs it here.)
	regularIncomplete := map[string]bool{}
	for _, m := range allMatches {
		pn, ok := poolNameFromMatchID(m.ID)
		if !ok {
			continue
		}
		if _, inStandings := standings[pn]; !inStandings {
			continue
		}
		if _, ok := poolCourt[pn]; !ok {
			poolCourt[pn] = m.Court
		}
		if IsTiebreakerMatchID(m.ID) {
			if poolTB[pn] == nil {
				poolTB[pn] = &poolTBInfo{existingPairs: map[string]bool{}}
			}
			poolTB[pn].count++
			poolTB[pn].existingPairs[tiebreakerPairKey(m.SideA, m.SideB)] = true
		} else if m.Status != state.MatchStatusCompleted {
			regularIncomplete[pn] = true
		}
	}

	var injected []state.MatchResult
	for poolName, poolStandings := range standings {
		// Don't inject tiebreakers until the pool's regular matches are all done.
		if regularIncomplete[poolName] {
			continue
		}
		info := poolTB[poolName]
		existingCount := 0
		existingPairs := map[string]bool{}
		if info != nil {
			existingCount = info.count
			existingPairs = info.existingPairs
		}

		for _, positions := range detectPoolTies(poolStandings) {
			// Only break ties that affect who advances / their seed: a tie sitting
			// entirely below the top-poolWinners cut shares its rank with no bout.
			if !tieAffectsAdvancement(positions, poolWinners) {
				continue
			}
			group := standingsAt(poolStandings, positions)
			newMatches := generateTiebreakerMatches(poolName, group, existingCount, poolCourt[poolName], existingPairs)
			existingCount += len(newMatches)
			injected = append(injected, newMatches...)
		}
	}

	if len(injected) == 0 {
		return nil, nil
	}

	allMatches = append(allMatches, injected...)

	// Reassign slots so the new TB matches get ScheduledAt values.
	// Snapshot operator-adjusted times first so they survive the reassignment;
	// only newly injected matches (ScheduledAt == "") should receive new slots.
	existingTimes := make(map[string]string, len(allMatches))
	for _, m := range allMatches {
		if m.ScheduledAt != "" {
			existingTimes[m.ID] = m.ScheduledAt
		}
	}
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}
	state.ApplyTournamentDefaults(tournament)
	state.ApplyCompetitionDefaults(comp)
	allMatches, _ = assignPoolMatchSlots(allMatches, comp, tournament)
	for i := range allMatches {
		if t, ok := existingTimes[allMatches[i].ID]; ok {
			allMatches[i].ScheduledAt = t
		}
	}

	if err := e.store.SavePoolMatches(compID, allMatches); err != nil {
		return nil, err
	}

	// Invalidate the standings cache so the next read reflects the injected matches.
	e.standingsCache.Delete(compID)
	e.standingsFlight.Delete(compID)

	return injected, e.GenerateSchedule(compID)
}
