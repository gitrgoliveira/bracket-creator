package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// R4(e)'s invariant, measured: "the draw's shape is identical whether an event
// runs on 1 court or several" (bc-draw, operator decision 2026-08-09).
//
// The shape is TreeToLeafArray over the whole bracket -- the pow2 slot array
// the engine builds its bracket from, which fixes every round-1 pairing, every
// named bye and every path to the final. The shiaijo count changes only which
// LEVEL of the block tree is the printable region, so it must not change this.
//
// It holds exactly when AssignPoolsToCourts' 4-way split NESTS inside its own
// 2-way split, which fails for pool counts congruent to 2 mod 4 (6, 10, 14...):
// the allocator front-loads the remainder, so 10 pools go 5/5 on two shiaijo
// but 3/3/2/2 on four, putting six pools in the first half instead of five.
// R3 pins each region to the pools its shiaijo actually ran, so the draw cannot
// paper over that -- the two allocations really are different competitions, and
// closing the gap would mean changing which shiaijo runs which pool rather than
// changing the draw.
//
// It also does not apply at ONE qualifier per pool, and should not. R4(d): at
// one qualifier nothing crosses, so there is no partner structure to emulate
// and planBlocks leaves each shiaijo's region as a single block. That is what
// the EKC individual draws print -- court A ran 5 pools as one ladder with P1
// byeing -- and it is what lets R6 pick the bye from the whole region instead
// of from whichever half the split happened to leave odd. Making the shape
// match across shiaijo counts here would mean re-imposing that split and giving
// the bye away by position; the invariant is the one that gives.
//
// Measured over pool counts 2-16 at 1-4 qualifiers: 67 of 112 comparisons held
// before the pool set was subdivided, 85 of 112 now. The 27 that differ are the
// 1-qualifier draws plus pool counts 6, 10 and 14 at four shiaijo.
//
// This test MEASURES and reports; the assertion is only on the cases where the
// rule claims anything -- 2 or more qualifiers, and allocations that agree.
func TestDrawShapeIsIndependentOfShiaijoCount(t *testing.T) {
	same, differ := 0, 0
	differing := []string{}
	for nPools := 2; nPools <= 16; nPools++ {
		for poolWinners := 1; poolWinners <= 4; poolWinners++ {
			pools, _ := makePools(nPools)
			base := TreeToLeafArray(BuildKnockoutDraw(pools, poolWinners, 1).Root)
			for _, courts := range []int{2, 4} {
				draw := BuildKnockoutDraw(pools, poolWinners, courts)
				require.NotNil(t, draw)
				if draw.NumCourts() != courts {
					continue // clamped: not the same allocation at all
				}
				got := TreeToLeafArray(draw.Root)
				name := fmt.Sprintf("P%02d-W%d-C%d", nPools, poolWinners, courts)
				if assert.ObjectsAreEqual(base, got) {
					same++
					continue
				}
				differ++
				differing = append(differing, name)
				if poolWinners >= 2 && (courts == 2 || nPools%4 != 2) {
					// 2 shiaijo always shares the 1-shiaijo allocation, and so
					// does 4 outside the 2-mod-4 pool counts, so a difference
					// here is a real regression rather than an allocator
					// mismatch. At 1 qualifier the rule makes no claim (R4(d):
					// nothing crosses, so nothing is emulated).
					assert.Equal(t, base, got, "shape must not depend on the shiaijo count (%s)", name)
				}
			}
		}
	}
	t.Logf("R4(e) shape invariant: %d comparisons identical, %d differ: %v", same, differ, differing)
}

// blockPoolCounts reports how many pools each block of the draw plan holds, for
// a given pool count, qualifier count and shiaijo count. It is the partition the
// whole draw is derived from.
func blockPoolCounts(nPools, poolWinners, numCourts int) []int {
	pools, _ := makePools(nPools)
	courts := EffectiveDrawCourts(nPools, numCourts)
	assignment, err := AssignPoolsToCourts(nPools, courts)
	if err != nil {
		return nil
	}
	plan := newDrawPlan(pools, assignment, poolWinners, courts)
	counts := make([]int, plan.numBlocks)
	for _, b := range plan.poolBlock {
		if b >= 0 && b < plan.numBlocks {
			counts[b]++
		}
	}
	return counts
}

// TestBlockPartitionIsTheSameTreeAtEveryShiaijoCount is the invariant's cause,
// isolated from its effect: the block partition itself. From 2 qualifiers up, 1
// and 2 shiaijo must agree, because both reach their blocks by subdividing the
// SAME pool list and planBlocks stops both at the same point (it reads the
// occupant count, which does not depend on the shiaijo count). At 1 qualifier
// there is no subdivision at all, so the partition IS the court allocation and
// the two counts legitimately differ.
func TestBlockPartitionIsTheSameTreeAtEveryShiaijoCount(t *testing.T) {
	for nPools := 2; nPools <= 16; nPools++ {
		for poolWinners := 1; poolWinners <= 4; poolWinners++ {
			one := blockPoolCounts(nPools, poolWinners, 1)
			two := blockPoolCounts(nPools, poolWinners, 2)
			four := blockPoolCounts(nPools, poolWinners, 4)
			t.Logf("%2d pools x %d: 1 shiaijo %v | 2 shiaijo %v | 4 shiaijo %v",
				nPools, poolWinners, one, two, four)
			if poolWinners >= 2 {
				assert.Equal(t, one, two,
					"%d pools x %d qualifiers: 1 and 2 shiaijo must share a block partition",
					nPools, poolWinners)
			}
		}
	}
}
