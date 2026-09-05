package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRealTargetSizes_SpreadsRemainderPastFirstRound pins bc-drwx item 5's
// exact repro: 14 entrants at minimum pool size 5 forms only 2 pools
// (poolSize=5 > totalPools=2), so the shortfall (14 - 2*5 = 4) EXCEEDS the
// number of pools -- forcePoolSizeFromCounts' outer-to-inner walk used to
// have no criterion beyond "still at or under its own base target" and fell
// through to an unconditional "return 0" once every pool had already
// received its first extra seat (round 1 done), piling the rest onto pool
// 0: [5,5] -> [6,5] -> [6,6] -> [7,6] -> [8,6] instead of the evenly spread
// [7,7].
func TestRealTargetSizes_SpreadsRemainderPastFirstRound(t *testing.T) {
	got := realTargetSizes([]int{5, 5}, 14)
	assert.Equal(t, []int{7, 7}, got, "the remainder must spread evenly once every pool has had its first extra seat")
}

// TestForcePoolSizeFromCounts_MultipleRounds is the same fix at the unit
// level: a remainder large enough to need THREE full outer-to-inner rounds
// over 3 pools (9 extra seats over a base of [2,2,2] -- 3 per pool) must
// land exactly 3 on each, not pile the excess onto pool 0 the moment every
// pool has had its first extra seat.
func TestForcePoolSizeFromCounts_MultipleRounds(t *testing.T) {
	base := []int{2, 2, 2}
	counts := append([]int(nil), base...)
	for r := 0; r < 9; r++ {
		counts[forcePoolSizeFromCounts(counts, base)]++
	}
	assert.Equal(t, []int{5, 5, 5}, counts, "9 extra seats over 3 pools of base 2 must spread 3 to each pool")
}

// TestRealTargetSizes_SumMatchesNumPlayers is a broader property sweep: for
// any base/numPlayers combination where a remainder exists, the returned
// counts must (a) sum to exactly numPlayers and (b) never differ from each
// other by more than 1 -- an evenly-spread remainder, the property the old
// "return 0" fallback broke as soon as the remainder exceeded len(base).
func TestRealTargetSizes_SumMatchesNumPlayers(t *testing.T) {
	cases := []struct {
		base       []int
		numPlayers int
	}{
		{[]int{5, 5}, 14},
		{[]int{5, 5}, 19}, // remainder 9, more than 4x len(base)
		{[]int{3, 3, 3}, 20},
		{[]int{4}, 11}, // single pool: everything piles onto it, by definition
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("base=%v n=%d", tc.base, tc.numPlayers), func(t *testing.T) {
			got := realTargetSizes(tc.base, tc.numPlayers)
			require.Len(t, got, len(tc.base))
			sum := 0
			minV, maxV := got[0], got[0]
			for _, v := range got {
				sum += v
				if v < minV {
					minV = v
				}
				if v > maxV {
					maxV = v
				}
			}
			assert.Equal(t, tc.numPlayers, sum, "returned sizes must sum to numPlayers")
			assert.LessOrEqual(t, maxV-minV, 1, "sizes must never differ by more than 1 once spread")
		})
	}
}
