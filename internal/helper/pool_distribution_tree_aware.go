package helper

import (
	"fmt"
	"math"
	"sort"
)

// The region-aware pool distributor (bc-dojo Phase 2), built BESIDE the
// fill+repair pipeline (BuildPoolPhase, tournament.go) rather than in place
// of it: nothing calls this yet, so it changes no existing behaviour. It
// exists so the Phase 3 decision-gate scorecard has something to measure
// against BuildPoolPhase; the swap, if the gate passes, is a later and
// separate change.
//
// The operator's design (bead bc-dojo): set the pool count, place the seeds
// RECORDING their dojo, then distribute the rest in ONE PASS, scoring each
// candidate pool by the knockout meeting it would create against
// already-placed members of the same dojo -- courts play no part in that
// decision at all, only in where the finished pools are printed.

// BuildPoolPhaseTreeAware is BuildPoolPhase's region-aware sibling. It
// returns the same (pools, drawCourts, error) shape, for the same reasons
// (see BuildPoolPhase's own doc comment on why the steps are ordered), but
// replaces the fill-then-repair body with a single forward pass that can see
// the whole knockout tree before it places anyone:
//
//  1. Pool count and target sizes come from poolTargetSizes -- the exact
//     arithmetic CreatePools uses, reused rather than copied.
//  2. Seeds are placed FIRST, by placeSeedIndices -- the exact arithmetic
//     PoolSeeding uses for its own seeded pass, extracted so the two can
//     never drift (pinned byte-identical by
//     TestSeedPlacementEquality_OldVsTreeAware). Each seed's dojo is folded
//     into that pool's occupancy immediately, which is what "recording
//     their dojo" means operationally: the unseeded pass below sees seeded
//     occupants exactly as it sees earlier unseeded ones.
//  3. Every unseeded player is then placed in ONE PASS, largest dojo first
//     (sortUnseededByDojoCluster -- the same clustering order PoolSeeding's
//     own dojo sort uses), by pickTreeAwarePool: among the pools that still
//     have room, the one that pushes this player's earliest possible
//     knockout meeting with an already-placed club-mate the LATEST, ties
//     broken by leastConflictedPool's existing rule. There is no repair
//     pass -- rebalanceDojosAcrossPools exists to fix what a blind,
//     order-dependent fill could not see coming, and that problem does not
//     arise when the fill can see the whole tree before it places anyone.
//  4. ReorderPoolsForCourts runs last, exactly as BuildPoolPhase's does.
//
// numCourts is used ONLY in step 1 (to derive drawCourts, the modulus seed
// placement and ReorderPoolsForCourts both need) and step 4 (the actual
// court assignment). Steps 2 and 3 -- the whole of WHO goes WHERE -- never
// read a court index: the knockout tree's region/crossing structure is the
// same shape whatever the shiaijo count (poolQualifierPaths, Phase 1), so
// distribution is computed once and courts are laid onto the result
// afterwards, never the other way round.
//
// poolWinners is the one addition to BuildPoolPhase's own parameter list:
// the tree shape poolQualifierPaths reads depends on how many qualifiers
// each pool sends up (a pool crossing 2 qualifiers routes its 2nd to the
// partner block; crossing 1 crosses nothing), so the scorer needs it up
// front. BuildPoolPhase's signature and behaviour are untouched by this
// function's existence.
func BuildPoolPhaseTreeAware(players []Player, poolSize int, isMax bool, numCourts int, poolWinners int) ([]Pool, int, error) {
	numPools, targetSizes, err := poolTargetSizes(len(players), poolSize, isMax)
	if err != nil {
		return nil, 0, err
	}
	drawCourts := EffectiveDrawCourts(numPools, numCourts)

	pools := make([]Pool, numPools)
	for i := range pools {
		pools[i].PoolName = poolPositionName(i)
	}

	seeded, unseeded := partitionSeeded(players)

	// Step 2: seeds first, byte-identical to today's pipeline (see
	// placeSeedIndices' own doc comment for how that is verified).
	seedIdx := placeSeedIndices(seeded, numPools, clampCourts(drawCourts), len(players))
	for si, idx := range seedIdx {
		// idx is only ever negative when placeSeedIndices ran out of roster
		// slots to place a seed in at all (more seeds than players); numPools
		// is guaranteed > 0 here whenever idx >= 0, since placeSeedIndices
		// returns every index as -1 up front when numPools <= 0.
		if idx < 0 {
			continue
		}
		p := seeded[si]
		poolIdx := idx % numPools
		p.PoolPosition = int64(len(pools[poolIdx].Players) + 1)
		pools[poolIdx].Players = append(pools[poolIdx].Players, p)
	}

	// Step 3: one pass over the unseeded, largest dojo first -- where a
	// dojo's size counts EVERY member on the roster, seeded ones included,
	// not just its unseeded residue. This deliberately diverges from
	// sortUnseededByDojoCluster (the old pipeline's order, kept verbatim
	// there): under the unseeded-only count, a club whose members are mostly
	// SEEDED sorts its lone unseeded straggler to the back of the pass, by
	// which point only conflicted pools have room -- and with no repair pass
	// behind it, the placement stands. Measured before this ordering: 17 of
	// 6060 gate configs regressed the per-pool optimum through exactly that
	// starvation (e.g. pools=4, clubs=4x3, seeds=2: the lone unseeded C0_3
	// deferred behind nine strangers and forced into a club-mate's pool).
	// Counting seeded members restores the club's true urgency; the old
	// pipeline never needed this because rebalanceDojosAcrossPools repaired
	// the same mistake after the fact.
	sortUnseededByTotalDojoFootprint(unseeded, players)
	qualifierSlots := treeAwareQualifierSlots(targetSizes, poolWinners, drawCourts)
	for _, p := range unseeded {
		best := pickTreeAwarePool(pools, targetSizes, qualifierSlots, p.Dojo)
		if best < 0 {
			// Cannot happen when sum(targetSizes) == len(players), which
			// poolTargetSizes guarantees; kept as a defensive error rather
			// than a panic or a silently dropped player.
			return nil, 0, fmt.Errorf("cannot place player %s: no pool has room", p.Name)
		}
		p.PoolPosition = int64(len(pools[best].Players) + 1)
		pools[best].Players = append(pools[best].Players, p)
	}

	return ReorderPoolsForCourts(pools, drawCourts), drawCourts, nil
}

// treeAwareQualifierSlots is poolQualifierPaths (Phase 1's seam) called
// correctly for a distributor that places players by PRE-reorder pool
// position while the real knockout draw is built from POST-reorder
// position, which are two DIFFERENT index spaces:
//
//   - PRE-reorder: the order CreatePools' straight fill builds pools in,
//     which is the order seed placement's arithmetic (placeSeedIndices) is
//     computed against (index i lands in pool i%numPools) -- BuildPoolPhase
//     itself only reorders as its LAST step, after every player has a pool.
//     This is the space `pools` (and `targetSizes`) is in throughout steps
//     2 and 3 of BuildPoolPhaseTreeAware.
//   - POST-reorder: the CONTIGUOUS-BLOCK-BY-COURT order ReorderPoolsForCourts
//     produces (all court-0 pools first, then court-1's, ...), which is what
//     AssignPoolsToCourts -- and through it BuildKnockoutDraw -- assumes a
//     pool array is already in.
//
// Calling poolQualifierPaths directly on PRE-reorder targetSizes silently
// scores every candidate against the WRONG topology whenever ReorderPoolsForCourts
// would actually move anything (its own activation condition, numPools >
// numCourts): measured in testing (bc-dojo Phase 2/3) to relabel a same-club
// pairing that the real draw puts in round 2 as a round-1 pairing instead,
// because pool position 2 pre-reorder can land adjacent to position 0
// post-reorder while looking two slots apart before the permutation.
//
// This round-trips through the POST-reorder order (matching what the real
// draw will use) and permutes the per-pool answer back into PRE-reorder
// index space, which is the space every caller in this file operates in.
func treeAwareQualifierSlots(targetSizes []int, poolWinners, numCourts int) [][]int {
	numPools := len(targetSizes)
	post := reorderPositions(numPools, numCourts)

	postSizes := make([]int, numPools)
	for preIdx, postIdx := range post {
		postSizes[postIdx] = targetSizes[preIdx]
	}
	postSlots := poolQualifierPaths(postSizes, poolWinners, numCourts)
	if postSlots == nil {
		return nil
	}
	preSlots := make([][]int, numPools)
	for preIdx, postIdx := range post {
		preSlots[preIdx] = postSlots[postIdx]
	}
	return preSlots
}

// reorderPositions mirrors ReorderPoolsForCourts' own grouping arithmetic
// (helper.go) on bare indices instead of Pool structs: post[preIdx] is the
// position pre-reorder index preIdx lands at once ReorderPoolsForCourts
// actually runs, including its own no-op condition (numCourts <= 1 ||
// numPools <= numCourts), which must match EXACTLY or a caller would permute
// when the real function would not. Pinned equal to it by
// TestReorderPositionsMatchesReorderPoolsForCourts so the two can never
// drift silently.
func reorderPositions(numPools, numCourts int) []int {
	post := make([]int, numPools)
	if numCourts <= 1 || numPools <= numCourts {
		for i := range post {
			post[i] = i
		}
		return post
	}
	groups := make([][]int, numCourts)
	for i := 0; i < numPools; i++ {
		c := i % numCourts
		groups[c] = append(groups[c], i)
	}
	pos := 0
	for _, g := range groups {
		for _, i := range g {
			post[i] = pos
			pos++
		}
	}
	return post
}

// pickTreeAwarePool chooses, among the pools with room, the one that pushes
// the given dojo's earliest possible knockout meeting the LATEST
// (earliestMeetingScore), ties broken by leastConflictedPool's existing rule
// (fewest of this dojo in pool, then fewest players, then lowest index).
//
// The tie-break is applied by literally calling leastConflictedPool rather
// than re-deriving its comparator: every pool NOT tied for the best score is
// masked as already full (its target size set to its current length), so
// leastConflictedPool's own room check narrows the search to just the tied
// candidates while still returning a true original index. Returns -1 when no
// pool has room, matching leastConflictedPool's own sentinel.
func pickTreeAwarePool(pools []Pool, targetSizes []int, qualifierSlots [][]int, dojo string) int {
	type candidate struct {
		idx   int
		score int
	}
	candidates := make([]candidate, 0, len(pools))
	bestScore := -1
	for i := range pools {
		if len(pools[i].Players) >= targetSizes[i] {
			continue
		}
		score := earliestMeetingScore(pools, qualifierSlots, i, dojo)
		if score > bestScore {
			bestScore = score
		}
		candidates = append(candidates, candidate{idx: i, score: score})
	}
	if len(candidates) == 0 {
		return -1
	}

	maskedSizes := append([]int(nil), targetSizes...)
	for _, c := range candidates {
		if c.score != bestScore {
			maskedSizes[c.idx] = len(pools[c.idx].Players)
		}
	}
	return leastConflictedPool(pools, maskedSizes, dojo)
}

// earliestMeetingScore is how "safe" pool i is for a new member of dojo,
// PESSIMISTICALLY: the earliest knockout round any of pool i's qualifiers
// could be forced to meet any qualifier of a pool that already holds this
// dojo, taking the WORST case across every qualifier either pool could send
// up (qualifierSlots -- a pool with poolWinners > 1 has more than one path
// out, and any of them could be the one that carries the clash).
//
// A pool that already holds this dojo itself scores 0: an immediate
// pool-phase (round-robin) clash, which is worse than any knockout round --
// dojoMeetRound never returns 0 for two distinct slots, its minimum is 1 --
// so a same-pool option always loses to any pool with no conflict at all.
// This is the one-pass replacement for discoverPool's hard avoidance of a
// same-pool dojo conflict: expressed as the worst possible score rather than
// an exclusion, so the SAME comparison (bigger is safer) covers both the
// pool-phase and the knockout-phase concern.
//
// dojo == "" (never assigned) is unconditionally safe: there is nothing to
// protect.
func earliestMeetingScore(pools []Pool, qualifierSlots [][]int, poolIdx int, dojo string) int {
	if dojo == "" {
		return math.MaxInt
	}
	if countDojoInPool(pools[poolIdx], dojo) > 0 {
		return 0
	}
	if poolIdx >= len(qualifierSlots) {
		return math.MaxInt
	}
	mySlots := qualifierSlots[poolIdx]

	score := math.MaxInt
	for other := range pools {
		if other == poolIdx || countDojoInPool(pools[other], dojo) == 0 {
			continue
		}
		if other >= len(qualifierSlots) {
			continue
		}
		if worst := earliestPairing(mySlots, qualifierSlots[other]); worst < score {
			score = worst
		}
	}
	return score
}

// earliestPairing is the WORST (earliest) dojoMeetRound over every pair of
// slots two pools could send to the knockout: the pessimistic meeting round
// two same-dojo qualifiers from these pools could be forced into, since
// either pool's ACTUAL finisher for a given rank is not known yet.
func earliestPairing(a, b []int) int {
	worst := math.MaxInt
	for _, sa := range a {
		for _, sb := range b {
			if r := dojoMeetRound(sa, sb); r < worst {
				worst = r
			}
		}
	}
	return worst
}

// sortUnseededByTotalDojoFootprint orders unseeded IN PLACE largest dojo
// first, counting a dojo's FULL roster presence (seeded and unseeded alike).
// Same tie-breaks as sortUnseededByDojoCluster: dojo name ascending between
// equal footprints, stable within a dojo, so the order is deterministic.
func sortUnseededByTotalDojoFootprint(unseeded, roster []Player) {
	footprint := make(map[string]int, len(roster))
	for _, p := range roster {
		footprint[p.Dojo]++
	}
	sort.SliceStable(unseeded, func(i, j int) bool {
		fi, fj := footprint[unseeded[i].Dojo], footprint[unseeded[j].Dojo]
		if fi != fj {
			return fi > fj
		}
		if unseeded[i].Dojo != unseeded[j].Dojo {
			return unseeded[i].Dojo < unseeded[j].Dojo
		}
		return false
	})
}
