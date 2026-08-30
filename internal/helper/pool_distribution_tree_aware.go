package helper

import (
	"fmt"
	"math"
	"sort"
)

// The region-aware pool distributor (bc-dojo). Built BESIDE the old
// fill+repair pipeline in Phase 2 so the Phase 3 decision-gate scorecard
// (pool_distribution_gate_test.go) had something to measure it against;
// Phase 4 is the swap this file now IS: BuildPoolPhase and
// BuildPoolPhaseFillBracket (tournament.go) both delegate here, so this is
// production code on both cutting paths, not a shadow implementation.
//
// The operator's design (bead bc-dojo): set the pool count, place the seeds
// RECORDING their dojo, then distribute the rest in ONE PASS, scoring each
// candidate pool by the knockout meeting it would create against
// already-placed members of the same dojo -- courts play no part in that
// decision at all, only in where the finished pools are printed.
//
// Phase 4 also made the distributor MODE-AWARE: production runs three
// different knockout builders depending on
// state.Competition.ExtraQualifiers (standard/uniform, "larger-pools",
// "fill-bracket"), each cutting a differently-shaped tree, and a
// distributor that always scored against the standard tree would optimise
// for a bracket the competition does not actually draw whenever a
// non-standard mode is in play. treeAwareQualifierSlots dispatches on a
// qualifierMode value to the matching skeleton builder
// (poolQualifierPaths / poolQualifierPathsPerPool /
// poolQualifierPathsFillBracket, draw_qualifier_paths.go) so the tree
// scored against is always the tree that mode actually cuts.

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
//     by TOTAL roster footprint (sortUnseededByTotalDojoFootprint -- see its
//     own doc comment for why this counts seeded members too, unlike
//     PoolSeeding's own unseeded-only dojo sort), by pickTreeAwarePool:
//     among the pools that still have room, the one that pushes this
//     player's earliest possible knockout meeting with an already-placed
//     dojo-mate the LATEST, ties broken by leastConflictedPool's existing
//     rule. There is no repair pass: the old pipeline needed one (a
//     post-fill dojo-swap pass, since deleted) to fix what a blind,
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
//
// This is always STANDARD mode (qualifierModeStandard): a caller that knows
// its competition's real extra-qualifiers setting must call
// BuildPoolPhaseTreeAwareWithMode instead, or the distributor scores every
// candidate against the wrong knockout tree whenever that setting is not
// the default. Kept as its own entry point (rather than folded into the
// mode-aware one with a fixed argument) because every one of this file's
// and pool_distribution_gate_test.go's existing tests call it by this exact
// 5-argument signature, pinned before mode-awareness existed.
func BuildPoolPhaseTreeAware(players []Player, poolSize int, isMax bool, numCourts int, poolWinners int) ([]Pool, int, error) {
	numPools, targetSizes, err := poolTargetSizes(len(players), poolSize, isMax)
	if err != nil {
		return nil, 0, err
	}
	return buildPoolPhaseTreeAwareCore(players, numPools, targetSizes, numCourts, poolWinners, qualifierMode{ExtraQualifiers: qualifierModeStandard})
}

// defaultPoolWinners is the pool-winners count BuildPoolPhase falls back to
// when it has no real competition to read one from: the documented default
// both state.Competition.EffectivePoolWinners() and
// engine.ResolveQualifiedPools use (helper/estimate.go mirrors it too, for
// the same reason).
const defaultPoolWinners = 2

// qualifierMode selects which of production's three knockout-draw skeleton
// builders treeAwareQualifierSlots scores candidate placements against
// (bc-dojo Phase 4): the shape of the tree the distributor sees must be the
// shape production actually cuts for that mode, or the region metric it
// optimises does not correspond to the real draw.
//
// ExtraQualifiers mirrors state.Competition.ExtraQualifiers's three values
// ("" / "larger-pools" / "fill-bracket") as a plain string rather than an
// imported type: internal/state imports internal/helper (Competition.
// QualifiersForPool takes a helper.Pool), so helper importing state back
// for this one type would cycle. The three qualifierMode* constants below
// are declared with the IDENTICAL string values on purpose, so a caller
// holding a state.Competition can pass comp.ExtraQualifiers straight
// through with no conversion and no risk of the two vocabularies drifting
// apart silently (a typo'd fourth string here is simply treated as
// standard, exactly as ValidateExtraQualifiers would have already rejected
// it before this seam is ever reached in production).
type qualifierMode struct {
	ExtraQualifiers string
	// MinPoolSize is the minimum per-pool size (state.Competition.PoolSize
	// under minimum-players-per-pool sizing) both non-standard modes key
	// their "oversized"/"has a 2nd to draft" tests off of. Unused in
	// standard mode.
	MinPoolSize int
	// SeedPoolIndex maps a seed's RANK to the pool index it lands in
	// (PRE-reorder space, the same space targetSizes is in), as already
	// computed by this call's own seed-placement step. Only fill-bracket
	// mode reads it (SelectFillBracketDrafts drafts seeded pools in seed
	// order); the other two modes ignore it.
	SeedPoolIndex map[int]int
}

// qualifierMode.ExtraQualifiers values, mirroring
// state.Competition.ExtraQualifiers* -- see qualifierMode's own doc comment
// for why the values are duplicated here rather than imported.
const (
	qualifierModeStandard    = ""
	qualifierModeLargerPools = "larger-pools"
	qualifierModeFillBracket = "fill-bracket"
)

// BuildPoolPhaseTreeAwareWithMode is BuildPoolPhaseTreeAware's mode-aware
// sibling (bc-dojo Phase 4): the real production entry point for a caller
// that knows its competition's actual pool-winners count and
// extra-qualifiers setting (internal/engine/pools.go,
// cmd/create-pools.go), as opposed to BuildPoolPhaseTreeAware's and
// BuildPoolPhase's own fixed defaultPoolWinners/standard-mode behaviour.
//
// minPoolSize is state.Competition.PoolSize (the minimum-mode pool size)
// when extraQualifiers is larger-pools or fill-bracket, ignored for
// standard mode. isMax must be false whenever extraQualifiers is
// non-standard (state.ValidateExtraQualifiers enforces this on every real
// caller before formation runs); this function does not re-validate it,
// matching poolTargetSizes' own trust-the-caller contract.
func BuildPoolPhaseTreeAwareWithMode(players []Player, poolSize int, isMax bool, numCourts int, poolWinners int, extraQualifiers string, minPoolSize int) ([]Pool, int, error) {
	numPools, targetSizes, err := poolTargetSizes(len(players), poolSize, isMax)
	if err != nil {
		return nil, 0, err
	}
	return buildPoolPhaseTreeAwareCore(players, numPools, targetSizes, numCourts, poolWinners, qualifierMode{ExtraQualifiers: extraQualifiers, MinPoolSize: minPoolSize})
}

// BuildPoolPhaseFillBracketTreeAware is BuildPoolPhaseFillBracket's
// region-aware body (bc-dojo Phase 4), mirroring BuildPoolPhaseTreeAware's
// relationship to BuildPoolPhase: the pool COUNT and BASE target sizes come
// from this function's own FillBracketPoolCount formation objective (a
// uniform minSize row, exactly what CreatePoolsForCount cuts to before its
// own remainder spreads), fill-bracket's poolWinners is always 1
// (state.ValidateExtraQualifiers' own gate), and the mode is
// qualifierModeFillBracket throughout -- everything else (seed placement,
// the remainder spread, the one-pass distribution, ReorderPoolsForCourts
// last) is the shared core BuildPoolPhaseTreeAware itself now uses.
func BuildPoolPhaseFillBracketTreeAware(players []Player, minSize int, numCourts int) ([]Pool, int, error) {
	// Same rule 4 supply-side read as BuildPoolPhaseFillBracket's own
	// pre-Phase-4 body: FillBracketPoolCount needs the roster's seed RANKS,
	// not just a count, to know which pool counts a gapped seed set can
	// actually supply drafts for.
	seeded, _ := partitionSeeded(players)
	seedRanks := make([]int, len(seeded))
	for i, p := range seeded {
		seedRanks[i] = p.Seed
	}
	numPools, _, err := FillBracketPoolCount(len(players), minSize, seedRanks)
	if err != nil {
		return nil, 0, err
	}
	base := make([]int, numPools)
	for i := range base {
		base[i] = minSize
	}
	return buildPoolPhaseTreeAwareCore(players, numPools, base, numCourts, 1, qualifierMode{ExtraQualifiers: qualifierModeFillBracket, MinPoolSize: minSize})
}

// buildPoolPhaseTreeAwareCore is BuildPoolPhaseTreeAware's and
// BuildPoolPhaseFillBracketTreeAware's shared body (bc-dojo Phase 4): given
// the pool COUNT and BASE target sizes already resolved by the caller's own
// formation objective, it places seeds, spreads whatever remainder the base
// sizes leave (realTargetSizes -- a no-op for max-mode or an
// exact-multiple roster, the only shapes every sweep in this package
// exercised before this was added), distributes the unseeded in one pass
// against the mode's own knockout skeleton, and reorders for courts.
func buildPoolPhaseTreeAwareCore(players []Player, numPools int, baseTargetSizes []int, numCourts int, poolWinners int, mode qualifierMode) ([]Pool, int, error) {
	drawCourts := EffectiveDrawCourts(numPools, numCourts)

	pools := make([]Pool, numPools)
	for i := range pools {
		pools[i].PoolName = poolPositionName(i)
	}

	targetSizes := realTargetSizes(baseTargetSizes, len(players))

	seeded, unseeded := partitionSeeded(players)

	// Step 2: seeds first, byte-identical to today's pipeline (see
	// placeSeedIndices' own doc comment for how that is verified).
	seedIdx := placeSeedIndices(seeded, numPools, clampCourts(drawCourts), len(players))
	// SeedPoolIndex is only ever consulted by fill-bracket mode
	// (poolQualifierPathsFillBracket), but it is cheap enough (at most one
	// entry per seed) to build unconditionally rather than threading a
	// mode check through this loop too.
	seedPoolIdx := make(map[int]int, len(seeded))
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
		if p.Seed > 0 {
			seedPoolIdx[p.Seed] = poolIdx
		}
	}
	mode.SeedPoolIndex = seedPoolIdx

	// Step 3: one pass over the unseeded, largest dojo first -- where a
	// dojo's size counts EVERY member on the roster, seeded ones included,
	// not just its unseeded residue. This deliberately diverges from
	// sortUnseededByDojoCluster (the old pipeline's order, kept verbatim
	// there): under the unseeded-only count, a dojo whose members are mostly
	// SEEDED sorts its lone unseeded straggler to the back of the pass, by
	// which point only conflicted pools have room -- and with no repair pass
	// behind it, the placement stands. Measured before this ordering: 17 of
	// 6060 gate configs regressed the per-pool optimum through exactly that
	// starvation (e.g. pools=4, dojos=4x3, seeds=2: the lone unseeded C0_3
	// deferred behind nine strangers and forced into a dojo-mate's pool).
	// Counting seeded members restores the dojo's true urgency; the old
	// pipeline (before its own repair pass was deleted alongside this swap)
	// never needed this because that repair pass fixed the same mistake
	// after the fact.
	sortUnseededByTotalDojoFootprint(unseeded, players)
	qualifierSlots := treeAwareQualifierSlots(targetSizes, poolWinners, drawCourts, mode)
	for _, p := range unseeded {
		best := pickTreeAwarePool(pools, targetSizes, qualifierSlots, p.Dojo)
		if best < 0 {
			// Cannot happen when sum(targetSizes) == len(players), which
			// realTargetSizes guarantees; kept as a defensive error rather
			// than a panic or a silently dropped player.
			return nil, 0, fmt.Errorf("cannot place player %s: no pool has room", p.Name)
		}
		p.PoolPosition = int64(len(pools[best].Players) + 1)
		pools[best].Players = append(pools[best].Players, p)
	}

	// Step 4: strictly-improving meeting repairs. The one pass above is
	// greedy per player, and on a MULTI-dojo roster the dojos placed early
	// can box a later dojo into pools whose qualifiers meet in round 1 even
	// though a mutual exchange would suit both dojos -- measured on the real
	// example rosters before this step existed: Team Epsilon (mock_data_small)
	// went from meeting in round 2 to round 1, and Team Xi
	// (mock_data_large_zekken) from round 5 to round 1, both while the
	// roster's OTHER dojos improved. A per-player chooser cannot see those
	// trades because each player is final the moment it is placed; only the
	// finished pools can. The operator's rule is absolute -- a first match
	// against a dojo-mate must not happen where any assignment avoids it --
	// so the finished pools get a repair loop: unseeded-for-unseeded
	// exchanges, accepted only when they strictly improve
	// (fewer dojos meeting in round 1, then a later meeting-sum), never
	// worsen ANY dojo's earliest meeting, never move a seed and never break
	// a dojo's per-pool optimum. Single-dojo rosters are already at their
	// brute-force ceiling (the Phase 3 gate pins 180/180), so this loop is a
	// no-op there, which is what keeps the gate numbers, the goldens and the
	// unique-dojo identity contract exactly where they were.
	improveDojoMeetings(pools, targetSizes, qualifierSlots, players)

	return ReorderPoolsForCourts(pools, drawCourts), drawCourts, nil
}

// earliestDojoMeeting returns the earliest knockout round two of dojo's pools
// are drawn to meet, or math.MaxInt when dojo occupies fewer than two pools.
// pairRound is the precomputed pool-pair meeting matrix (poolPairRounds):
// the repair loop evaluates hundreds of thousands of candidate exchanges,
// and recomputing each pool pair's slot pairing per candidate multiplied the
// whole test suite's runtime by three before the matrix existed.
func earliestDojoMeeting(pools []Pool, pairRound [][]int, dojo string) int {
	earliest := math.MaxInt
	for i := range pools {
		if countDojoInPool(pools[i], dojo) == 0 || i >= len(pairRound) {
			continue
		}
		for j := i + 1; j < len(pools); j++ {
			if countDojoInPool(pools[j], dojo) == 0 || j >= len(pairRound) {
				continue
			}
			if r := pairRound[i][j]; r < earliest {
				earliest = r
			}
		}
	}
	return earliest
}

// poolPairRounds precomputes earliestPairing for every pool pair.
func poolPairRounds(qualifierSlots [][]int) [][]int {
	n := len(qualifierSlots)
	m := make([][]int, n)
	for i := range m {
		m[i] = make([]int, n)
		for j := range m[i] {
			if i == j {
				m[i][j] = math.MaxInt
				continue
			}
			m[i][j] = earliestPairing(qualifierSlots[i], qualifierSlots[j])
		}
	}
	return m
}

// improveDojoMeetings is the multi-dojo repair loop described at its call
// site. Objective, lexicographic and strictly decreasing on every accepted
// exchange (so the loop terminates): first the number of multi-pool dojos
// whose earliest meeting is round 1, then the negated sum of finite meeting
// rounds. An exchange moves one unseeded player of a round-1 dojo out of one
// of its pools in return for an unseeded player of a different dojo, and is
// legal only when afterwards (a) neither exchanged dojo's earliest meeting
// got EARLIER, (b) neither pool holds more of either dojo than the dojo's
// per-pool optimum ceil(total/numPools) allows, so the spread invariants the
// gate pins survive by construction.
func improveDojoMeetings(pools []Pool, targetSizes []int, qualifierSlots [][]int, roster []Player) {
	pairRound := poolPairRounds(qualifierSlots)
	footprint := make(map[string]int, len(roster))
	for _, p := range roster {
		footprint[p.Dojo]++
	}
	numPools := len(pools)
	optimum := func(dojo string) int {
		return (footprint[dojo] + numPools - 1) / numPools
	}

	objective := func() (roundOnes, negSum int) {
		seen := map[string]bool{}
		for i := range pools {
			for _, pl := range pools[i].Players {
				if seen[pl.Dojo] {
					continue
				}
				seen[pl.Dojo] = true
				if m := earliestDojoMeeting(pools, pairRound, pl.Dojo); m != math.MaxInt {
					if m <= 1 {
						roundOnes++
					}
					negSum -= m
				}
			}
		}
		return roundOnes, negSum
	}

	better := func(r1a, nsa, r1b, nsb int) bool {
		if r1a != r1b {
			return r1a < r1b
		}
		return nsa < nsb
	}

	// Bounded belt-and-braces cap; the lexicographic strict improvement is
	// the real termination argument.
	for pass := 0; pass < len(roster)*numPools+1; pass++ {
		curR1, curNS := objective()
		if curR1 == 0 {
			break // nothing the operator's rule forbids remains
		}
		improved := false
		for i := 0; i < numPools && !improved; i++ {
			for ai := 0; ai < len(pools[i].Players) && !improved; ai++ {
				a := pools[i].Players[ai]
				if a.Seed > 0 {
					continue
				}
				if m := earliestDojoMeeting(pools, pairRound, a.Dojo); m > 1 {
					continue // only members of a round-1 dojo are worth moving
				}
				for j := 0; j < numPools && !improved; j++ {
					if j == i {
						continue
					}
					for bi := 0; bi < len(pools[j].Players) && !improved; bi++ {
						b := pools[j].Players[bi]
						if b.Seed > 0 || b.Dojo == a.Dojo {
							continue
						}
						// Spread feasibility after the exchange.
						if countDojoInPool(pools[j], a.Dojo)+1 > optimum(a.Dojo) {
							continue
						}
						if countDojoInPool(pools[i], b.Dojo)+1 > optimum(b.Dojo) {
							continue
						}
						// Only the two exchanged dojos' meetings can move,
						// so the objective is updated by their delta rather
						// than recomputed over every dojo (which made the
						// 2048-config sweep ~6x slower for no extra
						// information).
						beforeA := earliestDojoMeeting(pools, pairRound, a.Dojo)
						beforeB := earliestDojoMeeting(pools, pairRound, b.Dojo)
						pools[i].Players[ai], pools[j].Players[bi] = b, a
						afterA := earliestDojoMeeting(pools, pairRound, a.Dojo)
						afterB := earliestDojoMeeting(pools, pairRound, b.Dojo)
						newR1, newNS := curR1, curNS
						for _, d := range [2][2]int{{beforeA, afterA}, {beforeB, afterB}} {
							bef, aft := d[0], d[1]
							if bef != math.MaxInt {
								if bef <= 1 {
									newR1--
								}
								newNS += bef
							}
							if aft != math.MaxInt {
								if aft <= 1 {
									newR1++
								}
								newNS -= aft
							}
						}
						if afterA >= beforeA && afterB >= beforeB && better(newR1, newNS, curR1, curNS) {
							improved = true
							break
						}
						pools[i].Players[ai], pools[j].Players[bi] = a, b // revert
					}
				}
			}
		}
		if !improved {
			// Pairwise exchanges can stall in a local optimum that a
			// three-way rotation escapes: measured on mock_data_small (five
			// 2-member dojos over three pools), the pairwise fixpoint left
			// two dojos meeting in round 1 that the old pipeline's
			// accidental placement kept at round 2, proving a better
			// assignment existed. Try rotations a->Q, b->R, c->P among
			// unseeded players of three distinct dojos, same guards, same
			// strictly-improving objective; on success resume the pairwise
			// loop. Only attempted at a stall with round-1 dojos remaining,
			// so the common case never pays for it.
			if !rotateForDojoMeetings(pools, pairRound, optimum, curR1, curNS, better) {
				break
			}
		}
	}

	// Exchanges reorder members inside pools; renumber the display positions
	// so they stay 1..n in pool order, exactly as the fill assigns them.
	for i := range pools {
		for k := range pools[i].Players {
			pools[i].Players[k].PoolPosition = int64(k + 1)
		}
	}
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
// numCourts): measured in testing (bc-dojo Phase 2/3) to relabel a same-dojo
// pairing that the real draw puts in round 2 as a round-1 pairing instead,
// because pool position 2 pre-reorder can land adjacent to position 0
// post-reorder while looking two slots apart before the permutation.
//
// This round-trips through the POST-reorder order (matching what the real
// draw will use) and permutes the per-pool answer back into PRE-reorder
// index space, which is the space every caller in this file operates in.
//
// mode (bc-dojo Phase 4) selects WHICH of the three skeleton builders does
// that scoring: production runs a different knockout builder per
// state.Competition.ExtraQualifiers, and the tree scored against must be
// the tree that mode actually cuts (see the file-level comment). overrides
// for larger-pools mode, and the seed-pool map for fill-bracket mode, are
// both derived from POST-reorder sizes/indices here, matching what
// production's own real-pool derivation (extraQualifierOverrides,
// SelectFillBracketDrafts) reads off the ACTUAL stored pools -- which are
// always POST-reorder, since BuildPoolPhase/BuildPoolPhaseFillBracket
// return only after ReorderPoolsForCourts has run.
//
// A mode's skeleton builder returning nil (BuildKnockoutDrawPerPool /
// BuildKnockoutDrawFillBracket refusing an out-of-scope shape) degrades to
// nil here too, which pickTreeAwarePool's caller (earliestMeetingScore)
// reads as "every pool is equally safe": no region information, but no
// guess either. Production's own draw-build step reaches the identical
// out-of-scope refusal independently (buildPoolFedDraw), so the operator is
// told regardless of how the pools were formed.
func treeAwareQualifierSlots(targetSizes []int, poolWinners, numCourts int, mode qualifierMode) [][]int {
	numPools := len(targetSizes)
	post := reorderPositions(numPools, numCourts)

	postSizes := make([]int, numPools)
	for preIdx, postIdx := range post {
		postSizes[postIdx] = targetSizes[preIdx]
	}

	var postSlots [][]int
	switch mode.ExtraQualifiers {
	case qualifierModeLargerPools:
		overrides := extraQualifierOverridesFromSizes(postSizes, mode.MinPoolSize, poolWinners)
		postSlots = poolQualifierPathsPerPool(postSizes, poolWinners, overrides, numCourts)
	case qualifierModeFillBracket:
		postSeedPoolIdx := make(map[int]int, len(mode.SeedPoolIndex))
		for rank, preIdx := range mode.SeedPoolIndex {
			if preIdx >= 0 && preIdx < numPools {
				postSeedPoolIdx[rank] = post[preIdx]
			}
		}
		postSlots = poolQualifierPathsFillBracket(postSizes, mode.MinPoolSize, postSeedPoolIdx, numCourts)
	default:
		postSlots = poolQualifierPaths(postSizes, poolWinners, numCourts)
	}
	if postSlots == nil {
		return nil
	}
	preSlots := make([][]int, numPools)
	for preIdx, postIdx := range post {
		preSlots[preIdx] = postSlots[postIdx]
	}
	return preSlots
}

// extraQualifierOverridesFromSizes mirrors
// state.Competition.QualifiersForPool's larger-pools rule (a pool sends
// poolWinners+1 exactly when it is OVERSIZED, i.e. len(pool.Players) >
// PoolSize) but pre-placement, from target SIZES alone: pool i is oversized
// when sizes[i] > minPoolSize. This is exact, not an approximation --
// QualifiersForPool's own oversized test only ever reads a pool's
// participant COUNT, which is exactly what sizes[i] already promises pool i
// will end up holding once distribution finishes (both the old fill+repair
// pipeline and this one guarantee every pool's FINAL size equals its target
// size; nobody is ever short or over).
//
// minPoolSize <= 0 returns nil (no minimum to be over, matching
// QualifiersForPool's own degrade-to-uniform behaviour for that case) rather
// than marking every pool oversized.
func extraQualifierOverridesFromSizes(sizes []int, minPoolSize, poolWinners int) map[int]int {
	if minPoolSize <= 0 {
		return nil
	}
	var overrides map[int]int
	for i, sz := range sizes {
		if sz > minPoolSize {
			if overrides == nil {
				overrides = make(map[int]int, len(sizes))
			}
			overrides[i] = poolWinners + 1
		}
	}
	return overrides
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

// rotateForDojoMeetings attempts one strictly-improving three-way rotation of
// unseeded players (a: P->Q, b: Q->R, c: R->P), guarded exactly as the
// pairwise exchanges are: no involved dojo's earliest meeting may get
// earlier, every involved dojo stays within its per-pool optimum, seeds are
// never touched. Returns whether a rotation was applied.
func rotateForDojoMeetings(pools []Pool, pairRound [][]int, optimum func(string) int, curR1, curNS int, better func(int, int, int, int) bool) bool {
	numPools := len(pools)
	for i := 0; i < numPools; i++ {
		for ai := range pools[i].Players {
			a := pools[i].Players[ai]
			if a.Seed > 0 {
				continue
			}
			if m := earliestDojoMeeting(pools, pairRound, a.Dojo); m > 1 {
				continue
			}
			for j := 0; j < numPools; j++ {
				if j == i {
					continue
				}
				for bi := range pools[j].Players {
					b := pools[j].Players[bi]
					if b.Seed > 0 || b.Dojo == a.Dojo {
						continue
					}
					for k := 0; k < numPools; k++ {
						if k == i || k == j {
							continue
						}
						for ci := range pools[k].Players {
							c := pools[k].Players[ci]
							if c.Seed > 0 || c.Dojo == a.Dojo || c.Dojo == b.Dojo {
								continue
							}
							// Feasibility: a into j, b into k, c into i.
							if countDojoInPool(pools[j], a.Dojo)+1 > optimum(a.Dojo) ||
								countDojoInPool(pools[k], b.Dojo)+1 > optimum(b.Dojo) ||
								countDojoInPool(pools[i], c.Dojo)+1 > optimum(c.Dojo) {
								continue
							}
							befA := earliestDojoMeeting(pools, pairRound, a.Dojo)
							befB := earliestDojoMeeting(pools, pairRound, b.Dojo)
							befC := earliestDojoMeeting(pools, pairRound, c.Dojo)
							pools[i].Players[ai], pools[j].Players[bi], pools[k].Players[ci] = c, a, b
							aftA := earliestDojoMeeting(pools, pairRound, a.Dojo)
							aftB := earliestDojoMeeting(pools, pairRound, b.Dojo)
							aftC := earliestDojoMeeting(pools, pairRound, c.Dojo)
							newR1, newNS := curR1, curNS
							for _, d := range [3][2]int{{befA, aftA}, {befB, aftB}, {befC, aftC}} {
								bef, aft := d[0], d[1]
								if bef != math.MaxInt {
									if bef <= 1 {
										newR1--
									}
									newNS += bef
								}
								if aft != math.MaxInt {
									if aft <= 1 {
										newR1++
									}
									newNS -= aft
								}
							}
							if aftA >= befA && aftB >= befB && aftC >= befC && better(newR1, newNS, curR1, curNS) {
								return true
							}
							pools[i].Players[ai], pools[j].Players[bi], pools[k].Players[ci] = a, b, c // revert
						}
					}
				}
			}
		}
	}
	return false
}
