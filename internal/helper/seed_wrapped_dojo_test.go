package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPlaceSeedIndices_WrappedSeedAvoidsDojoMatePool pins bc-drwx item 4's
// exact repro: 16 players, 4 pools, seeds 1..5 with seed 1 and seed 5
// sharing a dojo. With nSeeds > numPools, seedPoolRank's out-of-range
// fallback (`rankIdx % numPools`) gives the WRAPPED 5th seed (rankIdx 4) the
// identical remainder to the UNWRAPPED 1st seed (rankIdx 0) whenever
// numPools divides evenly into rankIdx's difference (4-0=4, numPools=4) --
// before the fix this silently doubled seed 5 onto seed 1's own pool, even
// though pools 2-4 (also already holding a seed, but NOT seed 1's dojo)
// were equally valid landing spots.
//
// With 5 seeds and 4 pools some pool MUST end up holding two seeds
// (pigeonhole -- state.SeedPlacementWarnings' own "two seeds ignored"
// mechanism documents this as an accepted, warned-about degradation, not a
// bug in itself). What must NOT happen is that double-up landing on a
// DOJO-MATE pair when an alternative, non-dojo-mate pool was available to
// double up on instead.
func TestPlaceSeedIndices_WrappedSeedAvoidsDojoMatePool(t *testing.T) {
	seeded := []Player{
		{Name: "S1", Dojo: "DojoX", Seed: 1},
		{Name: "S2", Dojo: "DojoY", Seed: 2},
		{Name: "S3", Dojo: "DojoZ", Seed: 3},
		{Name: "S4", Dojo: "DojoW", Seed: 4},
		{Name: "S5", Dojo: "DojoX", Seed: 5}, // wraps, shares DojoX with S1
	}
	for _, numCourts := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("numCourts=%d", numCourts), func(t *testing.T) {
			numPools := 4
			totalLen := 16
			idx := placeSeedIndices(seeded, numPools, numCourts, totalLen)

			poolOf := make([]int, len(seeded))
			for si, ti := range idx {
				if ti < 0 {
					t.Fatalf("seed %d was never placed", seeded[si].Seed)
				}
				poolOf[si] = ti % numPools
			}

			// Sanity: seed 1 (si=0) and seed 5 (si=4) really are the pair
			// this test is about.
			assert.Equal(t, "DojoX", seeded[0].Dojo)
			assert.Equal(t, "DojoX", seeded[4].Dojo)

			assert.NotEqualf(t, poolOf[0], poolOf[4],
				"numCourts=%d: seed 1 and seed 5 share a dojo AND a pool (pool %d), but a "+
					"non-dojo-mate pool was available to double up on instead", numCourts, poolOf[0])
		})
	}
}

// TestPlaceSeedIndices_WrappedSeedTakesTheSeedFreePool pins the recorded
// contract for more seeds than pools (operator ruling, bc-pnum): the extra
// (wrapped) seed takes the next pool with room holding NO seed and NO
// dojo-mate, and stays there -- it is never moved by a later pass. Seeds 1,
// 2, 3 and 5 over 4 pools with FOUR DISTINCT dojos: seeds 1-3 land on their
// own natural (unwrapped) slots, unaffected by any wrapped-seed pass, and
// wrapped seed 5 lands in whichever of the 4 pools none of seeds 1-3
// occupies -- the one genuinely seed-free pool -- not merely a pool that
// happens not to hold a DOJO-MATE (every pool here qualifies on that
// narrower test alone, since none of the four dojos repeat).
func TestPlaceSeedIndices_WrappedSeedTakesTheSeedFreePool(t *testing.T) {
	seeded := []Player{
		{Name: "S1", Dojo: "DojoA", Seed: 1},
		{Name: "S2", Dojo: "DojoB", Seed: 2},
		{Name: "S3", Dojo: "DojoC", Seed: 3},
		{Name: "S5", Dojo: "DojoD", Seed: 5}, // wraps; no dojo-mate anywhere
	}
	for _, numCourts := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("numCourts=%d", numCourts), func(t *testing.T) {
			numPools := 4
			totalLen := 16
			idx := placeSeedIndices(seeded, numPools, numCourts, totalLen)

			poolOf := make([]int, len(seeded))
			for si, ti := range idx {
				require.GreaterOrEqualf(t, ti, 0, "seed %d was never placed", seeded[si].Seed)
				poolOf[si] = ti % numPools
			}

			seedFree := map[int]bool{0: true, 1: true, 2: true, 3: true}
			for _, p := range poolOf[:3] {
				delete(seedFree, p)
			}
			require.Lenf(t, seedFree, 1, "numCourts=%d: seeds 1-3 must occupy exactly 3 distinct pools, leaving exactly one free", numCourts)
			var wantPool int
			for p := range seedFree {
				wantPool = p
			}
			assert.Equalf(t, wantPool, poolOf[3],
				"numCourts=%d: wrapped seed 5 must take the one seed-free pool, not merely a non-dojo-mate one", numCourts)
		})
	}
}
