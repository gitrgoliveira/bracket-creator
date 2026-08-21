package helper

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// R5's quarter guarantee, swept over the same combinations as the shape golden.
//
// R5 (specs/007-ekc-draw/spec.md): at 3 or more qualifiers per pool, "no two
// qualifiers of one pool in the same quarter". The guarantee is pigeonhole-dead
// from the 5th qualifier onward (a draw has four quarters), so the sweep covers
// exactly the range where the spec promises something: 3 and 4 qualifiers, over
// 2-12 pools on 1, 2 and 4 shiaijo. That is 462 pool-instances.
//
// The rule was attempted twice before it was built in. First by ordering a
// region's occupants (ranks {1,2} laid out before ranks {3,4}, on the
// assumption that the boundary between the two rank blocks was the quarter
// boundary; it is not, and 118 of the 462 clashed). Then by dealing an
// already-built region's occupants into two blocks after the fact, under a
// constraint search (splitRegionQuarters), which brought it to 22. Both were
// fix-up passes over a structure that could not express the rule: below four
// shiaijo the draw plan had only TWO blocks, so the quarter boundary fell
// INSIDE a block and the routing could not aim at it. Subdividing the pool set
// (planBlocks) puts the quarters where routing can see them, and capping that
// subdivision by the OCCUPANT count rather than the pool count takes the last
// 6 residuals with it.

// leafQuarterPaths returns, for each non-empty leaf label under root, the path
// from the root as a slice of child indices (0 = Left, 1 = Right).
func leafQuarterPaths(root *Node) map[string][]int {
	out := map[string][]int{}
	var walk func(n *Node, path []int)
	walk = func(n *Node, path []int) {
		if n == nil {
			return
		}
		if n.LeafNode {
			if n.LeafVal != "" {
				out[n.LeafVal] = append([]int{}, path...)
			}
			return
		}
		walk(n.Left, append(path, 0))
		walk(n.Right, append(path, 1))
	}
	walk(root, nil)
	return out
}

// sharedAncestorDepth is the depth of two leaves' lowest common ancestor, which
// is the number of matches their winner still has to play AFTER meeting: 0 is
// the final, 1 a semifinal, 2 or more a quarterfinal or earlier.
//
// This is the shape-independent reading of "same quarter". Counting leaf-array
// quarters instead would misattribute the occupants of an uneven region,
// because a region whose second half is empty collapses (BuildSlotTree) and its
// leaves are re-padded to a different offset on the way out.
func sharedAncestorDepth(a, b []int) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// samePoolQuarterClashes returns one entry per pool with two or more qualifiers
// inside a single quarter of the draw, formatted "PoolName:pairs".
func samePoolQuarterClashes(root *Node, poolNames []string) []string {
	paths := leafQuarterPaths(root)
	clashes := []string{}
	for _, pool := range poolNames {
		labels := []string{}
		for label := range paths {
			if leafPool(label) == pool {
				labels = append(labels, label)
			}
		}
		sort.Strings(labels)
		pairs := 0
		for i := range labels {
			for j := i + 1; j < len(labels); j++ {
				if sharedAncestorDepth(paths[labels[i]], paths[labels[j]]) >= 2 {
					pairs++
				}
			}
		}
		if pairs > 0 {
			clashes = append(clashes, fmt.Sprintf("%s:%d", pool, pairs))
		}
	}
	return clashes
}

// TestRegionQuarterSeparationSweep is R5's quarter guarantee as a measurement:
// every pool of every draw in the sweep, counted. The expectation is ZERO
// clashes, everywhere.
//
// The rule was attempted twice as a fix-up pass before it was built in. First
// by ordering a region's occupants (ranks {1,2} laid out before ranks {3,4}, on
// the assumption that the boundary between the two rank blocks was the quarter
// boundary; it is not, and 118 of the 462 clashed). Then by dealing an
// already-built region's occupants into two blocks after the fact, under a
// constraint search, which brought it to 22. Both were passes over a structure
// that could not express the rule: below four shiaijo the plan had only TWO
// blocks, so the quarter boundary fell INSIDE a block and routing could not aim
// at it.
//
// Subdividing the pool set (planBlocks) puts the quarters where route can see
// them. Capping that subdivision by the POOL count left 6 residual clashes, all
// at 3 pools and 4 qualifiers, and the spec carried a carve-out for them.
// Capping by the OCCUPANT count instead removes both: 3 pools sending 4
// qualifiers each supply 12 occupants, ample for four blocks, and the carve-out
// was only ever an artefact of counting the wrong thing.
//
// Measured: region ordering 118 of 462, post-hoc region split 22, pool-capped
// subdivision 6, occupant-capped subdivision 0.
func TestRegionQuarterSeparationSweep(t *testing.T) {
	instances, violating := 0, 0
	for _, courts := range []int{1, 2, 4} {
		for nPools := 2; nPools <= 12; nPools++ {
			for _, poolWinners := range []int{3, 4} {
				name := fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, courts)
				t.Run(name, func(t *testing.T) {
					pools, poolNames := makePools(nPools)
					draw := BuildKnockoutDraw(pools, poolWinners, courts)
					require.NotNil(t, draw)

					clashes := samePoolQuarterClashes(draw.Root, poolNames)
					instances += nPools
					violating += len(clashes)

					assert.Empty(t, clashes,
						"pools with two qualifiers in one quarter (R5): %v", clashes)
				})
			}
		}
	}
	t.Logf("R5 quarter sweep: %d pool-instances, %d violating", instances, violating)
}

// TestQualifiersTakeFourDistinctQuarters is the same guarantee stated the way
// the draw now obtains it: R4/D5's rotation puts a pool's 1st, 2nd, 3rd and 4th
// in four DIFFERENT blocks, and with four blocks a block is a quarter. It is a
// structural claim, so it holds identically at 1, 2 and 4 shiaijo, which is the
// point of subdividing the pool set rather than leaning on the shiaijo count.
// Every case here sends 4 qualifiers from at least 4 pools, so there are always
// enough occupants for planBlocks to reach four blocks.
func TestQualifiersTakeFourDistinctQuarters(t *testing.T) {
	for _, courts := range []int{1, 2, 4} {
		for _, nPools := range []int{4, 5, 6, 7, 8, 9, 12} {
			t.Run(fmt.Sprintf("%d_pools_%d_courts", nPools, courts), func(t *testing.T) {
				pools, poolNames := makePools(nPools)
				draw := BuildKnockoutDraw(pools, 4, courts)
				require.NotNil(t, draw)
				paths := leafQuarterPaths(draw.Root)

				for _, name := range poolNames {
					for _, ranks := range [][2]int{{1, 2}, {1, 3}, {1, 4}, {2, 3}, {2, 4}, {3, 4}} {
						a := paths[fmt.Sprintf("%s-%s", name, GetOrdinal(ranks[0]))]
						b := paths[fmt.Sprintf("%s-%s", name, GetOrdinal(ranks[1]))]
						require.NotNil(t, a)
						require.NotNil(t, b)
						assert.Lessf(t, sharedAncestorDepth(a, b), 2,
							"%s's %s and %s share a quarter", name, GetOrdinal(ranks[0]), GetOrdinal(ranks[1]))
					}
				}
			})
		}
	}
}

// TestBlockCountIsCappedByTheOccupantCount pins planBlocks' limit. The
// subdivision doubles towards four blocks and stops while a block would still
// average fewer than two occupants, because a block left holding ONE occupant
// byes it whatever R6's precedence says, and cutting that fine is our decision
// rather than the operator's.
//
// The cap reads the QUALIFIER count, not the pool count. It was first written
// as "a block must own at least one pool", which is both too strict (3 pools at
// 4 qualifiers have 12 occupants, ample for four blocks) and too loose (5 pools
// at 1 qualifier own a pool each and still strand C1 alone in a block, which is
// the 5-pool bye defect bc-draw names). The two rows below marked with it are
// the ones the pool-count rule got wrong.
func TestBlockCountIsCappedByTheOccupantCount(t *testing.T) {
	cases := []struct {
		numPools, poolWinners, numCourts, want int
		why                                    string
	}{
		{1, 2, 1, 2, "R4(e): a lone shiaijo is cut in two, so 2nds have somewhere to cross"},
		{2, 1, 1, 1, "R4(d): at 1 qualifier nothing crosses, so a lone shiaijo is one block"},
		{5, 1, 1, 1, "R4(d): 5 pool winners, one block, R6 picks the bye -- the 5-pool bye defect"},
		{5, 2, 1, 4, "10 occupants over four blocks averages 2.5, so it subdivides"},
		{8, 1, 1, 1, "R4(d) again: no crossing, no subdivision"},
		{12, 1, 1, 1, ""},
		{3, 2, 1, 2, "6 occupants: four blocks would bye pool B's 1st AND its 2nd"},
		{4, 2, 1, 4, ""},
		{3, 4, 1, 4, "12 occupants, three per block: the pool-count rule refused this one"},
		{2, 4, 1, 4, "8 occupants: two pools can carry four blocks"},
		{2, 2, 2, 2, ""},
		{4, 2, 2, 4, ""},
		{12, 3, 2, 4, ""},
		{4, 1, 4, 4, "at 4+ shiaijo the operator's allocation IS the partition"},
		{8, 2, 1, 4, "16 occupants: four blocks of four"},
		{8, 1, 8, 8, ""},
		{16, 1, 16, 16, ""},
		// Legacy shiaijo counts R9 rejects keep one block per court: a court's
		// blocks would otherwise straddle the half boundary (R3).
		{12, 1, 3, 3, ""},
		{12, 1, 6, 6, ""},
	}
	for _, tc := range cases {
		name := fmt.Sprintf("%dpools_%dq_%dcourts", tc.numPools, tc.poolWinners, tc.numCourts)
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, planBlocks(tc.numPools, tc.poolWinners, tc.numCourts), tc.why)
		})
	}
}

// TestBlocksHalveThePoolListRatherThanDividingIt is the reason the subdivision
// is recursive. AssignPoolsToCourts front-loads its remainder, so a direct
// 4-way split of 10 pools gives 3/3/2/2 and puts SIX pools in the first half,
// where the same 10 pools on two shiaijo give 5/5. Halving gives 3/2/3/2, whose
// halves ARE the 2-way split's, so the block tree nests and the shiaijo count
// selects a level of one tree instead of drawing a different bracket.
func TestBlocksHalveThePoolListRatherThanDividingIt(t *testing.T) {
	direct, err := AssignPoolsToCourts(10, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 0, 0, 1, 1, 1, 2, 2, 3, 3}, direct, "the allocator front-loads: 3/3/2/2")

	halved := subdivideCourts(make([]int, 10), 1, 4)
	assert.Equal(t, []int{0, 0, 0, 1, 1, 2, 2, 2, 3, 3}, halved, "halving gives 3/2/3/2")

	// The halves agree with the two-shiaijo allocation; the direct split's do not.
	twoWay, err := AssignPoolsToCourts(10, 2)
	require.NoError(t, err)
	for pi := range halved {
		assert.Equalf(t, twoWay[pi], halved[pi]/2, "pool %d must stay in the same half", pi)
	}
}
