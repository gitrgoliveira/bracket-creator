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
// actually runs and asserts R6-1's protection property: a block that grants a
// named bye never sends the occupant R6 ranks first into round 1. (Under the
// R6(c) template a block byes several occupants by design, so the older "THE
// bye = the top occupant" form is asserted only where a single bye exists --
// see the loop body.)
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
							// Read byes the way production does, from the round
							// walk. A NAMED bye is a leaf paired with a WINNER
							// slot in round 2 or later: the competitor stands
							// alone in a later column awaiting a bout's
							// winner. A leaf-leaf pair sitting shallow is NOT
							// a trigger -- at 1 qualifier it is a phantom-risen
							// pair (Junior Male's P4 v P5, column 1 on the
							// sheet), at 2+ it is a vacancy block's two byes
							// meeting (2025 Men Team F16), and the tree cannot
							// tell them apart (spec R6(c)).
							//
							// Under the template a block byes SEVERAL
							// occupants by design (its sub-block heads, plus
							// the weakest crossed on a vacancy), so asserting
							// "THE bye = the top occupant" is wrong there. The
							// invariant the whole range shares is R6-1's
							// protection: when a block grants a named bye, the
							// occupant R6 ranks FIRST never plays round 1.
							rounds := BuildEliminationMatchRounds(draw.blocks[b])
							if len(rounds) < 2 {
								continue
							}
							hasNamedBye := false
							for _, round := range rounds[1:] {
								for _, m := range round {
									l := m.Left != nil && m.Left.LeafNode
									r := m.Right != nil && m.Right.LeafNode
									if l != r {
										hasNamedBye = true
									}
								}
							}
							if !hasNamedBye {
								continue
							}
							inRound1 := map[string]bool{}
							for _, m := range rounds[0] {
								for _, side := range []*Node{m.Left, m.Right} {
									if side != nil && side.LeafNode {
										inRound1[side.LeafVal] = true
									}
								}
							}
							best := occ[0]
							for _, o := range occ[1:] {
								if byePrecedenceLess(o, best, pools) {
									best = o
								}
							}
							assert.Falsef(t, inRound1[best.label],
								"block %d grants a named bye but %s, whom R6 ranks first (seed %d, load %d), plays round 1",
								b, best.label,
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
