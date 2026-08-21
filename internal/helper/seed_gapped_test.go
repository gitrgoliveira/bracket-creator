package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PoolSeeding places on RANK, and the set it is handed does not have to be
// contiguous: engine.dropSeedAssignments removes the assignments of seeds who
// did not check in and the survivors keep their raw ranks. A rank far larger
// than the number of slots is therefore a NORMAL state, not a malformed one.
//
// posInPool (si / numPools) then points past the end of the layout, targetIdx is
// out of range on every offset the search tries, and the seed falls through to
// the "last resort: take the first available slot" scan. That looks like a bug
// and has been reported as one. It is not: free indices cycle through the pools
// (a player at index i lands in pool i%numPools), so the scan spreads surplus
// ranks ACROSS pools, which is exactly what R2 asks for. seedPoolRank's own doc
// already names this as the intended D7 degradation.
//
// This test exists because the plausible "fix" -- keeping a surplus rank in the
// pool its rank chose by scanning that pool's other rows -- was written, and it
// puts rank 30 in rank 2's pool while two pools sit empty. These assertions fail
// against that version. The last-resort scan is load-bearing; do not replace it
// with a same-pool search.
func TestPoolSeedingSpreadsSurvivingHighRanks(t *testing.T) {
	t.Parallel()

	roster := func(ranks []int, unseeded int) []Player {
		players := make([]Player, 0, len(ranks)+unseeded)
		for i, r := range ranks {
			players = append(players, Player{Name: fmt.Sprintf("Seed%d", i), Dojo: "Dojo", Seed: r})
		}
		for i := 0; i < unseeded; i++ {
			players = append(players, Player{Name: fmt.Sprintf("Player%d", i), Dojo: "Dojo"})
		}
		return players
	}

	// The seed ranks in each pool. A player at index i is in pool i%numPools:
	// ReorderPoolsForCourts deinterleaves on exactly that.
	seedsByPool := func(out []Player, numPools int) map[int][]int {
		pools := map[int][]int{}
		for i, p := range out {
			if p.Seed > 0 {
				pools[i%numPools] = append(pools[i%numPools], p.Seed)
			}
		}
		return pools
	}

	cases := []struct {
		name     string
		ranks    []int
		unseeded int
		numPools int
		courts   int
	}{
		// 30 seeded, 27 withdrew. Ranks 1 and 2 have rows of their own; rank 30
		// asks for row 7 of a 3-row layout and has none.
		{"one surplus rank among three survivors", []int{1, 2, 30}, 9, 4, 2},
		// Two survivors with rows and two without, over four pools: the case
		// where a same-pool fallback doubles up two pools and empties two.
		{"two surplus ranks", []int{1, 2, 25, 26}, 8, 4, 2},
		{"three seeds, one far out of range", []int{1, 2, 3, 30}, 8, 4, 2},
		{"every survivor out of range but one", []int{1, 2, 3, 50}, 4, 4, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			players := roster(tc.ranks, tc.unseeded)
			require.LessOrEqual(t, len(tc.ranks), tc.numPools,
				"the fixture must have a pool available for every seed, or there is no R2 promise to keep")

			pools := seedsByPool(PoolSeeding(players, tc.numPools, tc.courts), tc.numPools)

			placed := 0
			for pool, ranks := range pools {
				assert.Len(t, ranks, 1,
					"pool %d holds seeds %v: two seeds may never share a pool while a pool without one exists (R2)", pool, ranks)
				placed += len(ranks)
			}
			assert.Equal(t, len(tc.ranks), placed, "every seed must be placed exactly once")
		})
	}
}
