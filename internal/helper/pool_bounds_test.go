package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPoolBoundsForSubtree_UnevenDistribution is the regression test for the
// uneven-pages-per-court bug: when numSubtrees is not divisible by numCourts,
// the extra pages clamp onto the last court but pageWithinCourt wrongly
// computed 0 for all of them, causing multiple pages to return the same pool
// slice and leaving later pools unreachable.
//
// Scenario: numPools=6, numCourts=3, numSubtrees=4.
// SubtreeCourtIndex maps pages [0,1,2,3] to courts [0,1,2,2].
// The union of all [start, end) slices must cover [0, 6) exactly once.
func TestPoolBoundsForSubtree_UnevenDistribution(t *testing.T) {
	t.Parallel()
	const numPools = 6
	const numCourts = 3
	const numSubtrees = 4

	seen := make([]bool, numPools)
	for idx := 0; idx < numSubtrees; idx++ {
		start, end := PoolBoundsForSubtree(numPools, numCourts, numSubtrees, idx)
		assert.LessOrEqual(t, start, end,
			"page %d: start must be <= end", idx)
		for p := start; p < end; p++ {
			assert.False(t, seen[p],
				"pool %d is covered more than once (found again on page %d [%d,%d))", p, idx, start, end)
			seen[p] = true
		}
	}
	for p := range seen {
		assert.True(t, seen[p], "pool %d is not covered by any page", p)
	}
}

// TestPoolBoundsForSubtree_EvenDivision is the non-regression case: when
// numSubtrees is divisible by numCourts the existing formula was already
// correct and must continue to work.
//
// Scenario: numPools=8, numCourts=2, numSubtrees=4 (2 pages per court).
func TestPoolBoundsForSubtree_EvenDivision(t *testing.T) {
	t.Parallel()
	const numPools = 8
	const numCourts = 2
	const numSubtrees = 4

	seen := make([]bool, numPools)
	for idx := 0; idx < numSubtrees; idx++ {
		start, end := PoolBoundsForSubtree(numPools, numCourts, numSubtrees, idx)
		assert.LessOrEqual(t, start, end,
			"page %d: start must be <= end", idx)
		for p := start; p < end; p++ {
			assert.False(t, seen[p],
				"pool %d is covered more than once (found again on page %d [%d,%d))", p, idx, start, end)
			seen[p] = true
		}
	}
	for p := range seen {
		assert.True(t, seen[p], "pool %d is not covered by any page", p)
	}
}
