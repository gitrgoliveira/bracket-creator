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

// TestPlaceSeedIndices_DistinctDojoWrappedSeedStaysNatural pins bc-pnum
// review: a wrapped seed relocates ONLY to avoid landing on a DOJO-MATE's
// pool (bc-drwx item 4's exact scope), never merely because a seed-free pool
// happens to exist elsewhere. Seeds 1, 2, 3 and 5 over 4 pools with FOUR
// DISTINCT dojos: nothing here is a dojo-mate of anything else, so seed 5
// (wrapped, rankIdx 4, si 3) must land on its own NATURAL slot -- raw index
// 4 (posInPool 1 * numPools 4 + its own candidateGlobalPool(0) of 0) --
// unperturbed by any relocation pass, for every court count. A "Pass 0"
// that preferred any seed-free pool regardless of dojo (removed by this
// fix) used to relocate seed 5 onto pool 2, 3 and 3 respectively for
// numCourts 1, 2 and 4 (verified by reverting this fix and re-running this
// exact scenario) solely because those pools were seed-free -- reaching
// past bc-drwx item 4's dojo-mate scope and turning the accurate "two seeds
// must never share a pool" warning into a spurious D7 halves complaint.
func TestPlaceSeedIndices_DistinctDojoWrappedSeedStaysNatural(t *testing.T) {
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

			require.GreaterOrEqualf(t, idx[3], 0, "seed 5 was never placed")
			assert.Equalf(t, 4, idx[3],
				"numCourts=%d: seed 5 (wrapped, dojo-mate-free) must land at its own natural slot "+
					"(raw index 4), not be relocated to a different pool just because it was seed-free", numCourts)
		})
	}
}
