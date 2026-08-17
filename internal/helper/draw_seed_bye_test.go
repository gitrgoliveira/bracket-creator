package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// R6 criterion 1 end to end: with seeds actually set, does the block's bye go
// to the block's highest-precedence occupant?
//
// Every other draw test in this package uses makePools, which builds pools with
// no players in them, so poolSeedRank is MaxInt everywhere and criterion 1 can
// never fire. This file drives the production path instead -- PoolSeeding ->
// CreatePools -> ReorderPoolsForCourts, the order engine/pools.go and
// cmd/create-pools.go both use -- because the deinterleave is what decides
// which pool letter a seed lands in, and therefore which block can claim it.
// Skipping it makes the seeds look clustered and the byes look wrong.

// seededDrawPools builds numPools pools of mixed size from drawGoldenRoster with
// seeds 1-4 assigned to the first four players, through the real pipeline.
// seededDrawPoolsN (draw_seed_count_test.go) is the same thing with the seed
// count as a parameter; four is only this file's default, never a requirement.
func seededDrawPools(t *testing.T, numPools, numCourts int) []Pool {
	t.Helper()
	return seededDrawPoolsN(t, numPools, numCourts, 4)
}

// namedBye returns the label of the block's round-1 bye: the occupant paired
// with an empty slot in the block's own leaf array.
//
// A block of ONE occupant has no such pair -- BuildSlotTree collapses it to a
// lone leaf and the free round appears only when the block merges with its
// sibling -- so this returns "" and the caller skips it. That bye is the
// operator's court allocation showing through (the EKC Junior Individual Female
// draw is exactly this: court B ran one pool, so its winner byed), not a choice
// R6 gets to make.
func namedBye(block *Node) string {
	slots := TreeToLeafArray(block)
	for i := 0; i+1 < len(slots); i += 2 {
		if (slots[i] == "") != (slots[i+1] == "") {
			if slots[i] != "" {
				return slots[i]
			}
			return slots[i+1]
		}
	}
	return ""
}

// TestByeGoesToTheHighestPrecedenceOccupantSweep sweeps the shapes an operator
// actually runs and asserts every named bye went to the occupant R6 ranks first.
//
// TestSeededPoolWinnerTakesTheBlockBye (tree_test.go) pins criterion 1 on one
// hand-built block; this drives the production pipeline over the whole range,
// where the pool sizes, the seed-to-pool mapping and the block partition are all
// whatever CreatePools and the deinterleave actually produce.
//
// Which is exactly why this sweep does NOT itself pin criterion 1: the pipeline
// seeds each block's first pool, so criteria 1 and 3 agree at every point it
// visits and it passes with criterion 1 deleted (spec R6(b)). What it covers is
// the rest of the precedence list and the block partition, across a range no
// hand-built case reaches. Do not read a green run here as criterion 1 holding.
//
// This is the defect bc-draw was raised on, and the one the pool-count cap on
// planBlocks reintroduced: at 6 pools and 1 qualifier on two shiaijo the byes
// landed on Pool C and Pool F, the LAST pool of each block, while pools A and D
// held seeds 1 and 2. A bye is worth a round of rest and a round of scouting;
// handing it to the fourth seed over the first inverts the protection seeding
// exists to give.
func TestByeGoesToTheHighestPrecedenceOccupantSweep(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for numPools := 4; numPools <= 12; numPools++ {
			for poolWinners := 1; poolWinners <= 3; poolWinners++ {
				// The seed count is the operator's, so R6 has to hold at all of
				// them, not just at the four that fully determine D6's halves
				// and quarters. At zero seeds criterion 1 never fires and the
				// order falls through to pool size and pool order, which is
				// exactly the path an unseeded club event takes.
				for _, numSeeds := range seedCounts {
					if !rosterHolds(numPools, numSeeds) {
						continue
					}
					name := fmt.Sprintf("%dpools_%dq_%dsj_%dseeds", numPools, poolWinners, numCourts, numSeeds)
					t.Run(name, func(t *testing.T) {
						pools := seededDrawPoolsN(t, numPools, numCourts, numSeeds)
						draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
						require.NotNil(t, draw)

						courts := EffectiveDrawCourts(len(pools), numCourts)
						assignment, err := AssignPoolsToCourts(len(pools), courts)
						require.NoError(t, err)
						plan := newDrawPlan(pools, assignment, poolWinners, courts)
						occupants := plan.route(pools, poolWinners)
						require.Len(t, draw.blocks, len(occupants))

						for b, occ := range occupants {
							got := namedBye(draw.blocks[b])
							if got == "" {
								continue
							}
							best := occ[0]
							for _, o := range occ[1:] {
								if byePrecedenceLess(o, best, pools) {
									best = o
								}
							}
							assert.Equalf(t, best.label, got,
								"block %d byed %s; R6 ranks %s first (seed %d, load %d)",
								b, got, best.label,
								poolSeedRank(pools[best.pool]), poolLoad(pools[best.pool]))
						}
					})
				}
			}
		}
	}
}

// TestSubdivisionNeverStrandsALoneOccupant is the structural rule the sweep
// above cannot see, and it is the one that actually forbids the 6-pool defect.
//
// A block holding ONE occupant byes it unconditionally: there is nobody to
// compare against, so R6's precedence never runs and the free round goes to
// whoever the partition happened to leave alone. At the SHIAIJO level that is
// legitimate -- it is the operator's court allocation, and the EKC Junior
// Individual Female draw prints exactly it -- but a block below that level is
// ours, invented by planBlocks, so manufacturing a bye there is our defect.
//
// Hence: whenever the pool set is subdivided finer than the shiaijo count,
// every resulting block must hold at least two occupants. That is what the
// occupant-count cap buys, and what the old pool-count cap did not: 6 pools at
// 1 qualifier passed the pool test (four blocks, each owning a pool) and still
// left blocks 1 and 3 holding a single winner apiece, who then byed ahead of
// the seeds in blocks 0 and 2.
func TestSubdivisionNeverStrandsALoneOccupant(t *testing.T) {
	for _, numCourts := range []int{1, 2} {
		for numPools := 2; numPools <= 16; numPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				name := fmt.Sprintf("%dpools_%dq_%dsj", numPools, poolWinners, numCourts)
				t.Run(name, func(t *testing.T) {
					pools, _ := makePools(numPools)
					courts := EffectiveDrawCourts(numPools, numCourts)
					assignment, err := AssignPoolsToCourts(numPools, courts)
					require.NoError(t, err)
					plan := newDrawPlan(pools, assignment, poolWinners, courts)
					if plan.numBlocks <= courts {
						return // not subdivided: the allocation is the operator's
					}
					for b, occ := range plan.route(pools, poolWinners) {
						assert.GreaterOrEqualf(t, len(occ), 2,
							"block %d of %d holds %d occupant(s); a subdivided block with one occupant byes it without R6 ever choosing",
							b, plan.numBlocks, len(occ))
					}
				})
			}
		}
	}
}

// TestSixPoolsOneQualifierByesTheTopSeeds is the sweep's headline case written
// out, so the regression is legible without re-deriving the pipeline.
//
// Six pools of 4/4/4/3/3/3 on two shiaijo, seeds 1-4. PoolSeeding plus the
// deinterleave put seed 1 in Pool A and seed 3 in Pool C on shiaijo A, seed 2 in
// Pool D and seed 4 in Pool F on shiaijo B. Each block holds three pool winners
// and so carries exactly one bye, and both must go to the better-seeded pool.
//
// It does NOT discriminate R6 criterion 1 from criterion 3, despite the name:
// PoolSeeding puts each block's top seed in that block's FIRST pool, so "seeds
// first" and "pool order" name the same bye here and this case passes with
// criterion 1 deleted (spec R6(b)). It is a regression guard for the bc-draw
// defect, which is a real job -- just not that one. The witnesses for criterion
// 1 are TestSeededPoolWinnerTakesTheBlockBye (tree_test.go) and
// TestByePrecedenceSeedBeatsPoolOrder (draw_bye_layers_test.go).
func TestSixPoolsOneQualifierByesTheTopSeeds(t *testing.T) {
	pools := seededDrawPools(t, 6, 2)

	seedOf := map[string]int{}
	for _, p := range pools {
		seedOf[p.PoolName] = poolSeedRank(p)
	}
	require.Equal(t, 1, seedOf["Pool A"], "seed 1 must land in Pool A")
	require.Equal(t, 2, seedOf["Pool D"], "seed 2 must land in Pool D")

	draw := BuildKnockoutDraw(pools, 1, 2)
	require.NotNil(t, draw)
	require.Len(t, draw.blocks, 2)

	assert.Equal(t, "Pool A-1st", namedBye(draw.blocks[0]),
		"shiaijo A holds seeds 1 and 3; the bye belongs to seed 1's pool")
	assert.Equal(t, "Pool D-1st", namedBye(draw.blocks[1]),
		"shiaijo B holds seeds 2 and 4; the bye belongs to seed 2's pool")
}
