package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// assertFullCoverage verifies that PoolBoundsForSubtree produces a partition of
// [0, numPools) across numSubtrees pages: each pool is covered exactly once, no
// gaps, no overlaps, and every page range has start <= end.
func assertFullCoverage(t *testing.T, numPools, numCourts, numSubtrees int) {
	t.Helper()
	seen := make([]bool, numPools)
	for idx := 0; idx < numSubtrees; idx++ {
		start, end := PoolBoundsForSubtree(numPools, numCourts, numSubtrees, idx)
		assert.LessOrEqual(t, start, end, "page %d: start must be <= end", idx)
		for p := start; p < end; p++ {
			assert.False(t, seen[p], "pool %d covered more than once (page %d [%d,%d))", p, idx, start, end)
			seen[p] = true
		}
	}
	for p := range seen {
		assert.True(t, seen[p], "pool %d not covered by any page", p)
	}
}

// TestPoolBoundsForSubtree_Coverage exercises both the uneven-pages-per-court
// regression case and the clean-division baseline.
func TestPoolBoundsForSubtree_Coverage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                             string
		numPools, numCourts, numSubtrees int
	}{
		{"uneven distribution (regression)", 6, 3, 4},
		{"even division", 8, 2, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFullCoverage(t, tc.numPools, tc.numCourts, tc.numSubtrees)
		})
	}
}

// TestPoolBoundsForSubtree_MorePagesThanPools guards against inverted ranges:
// when a court has more tree pages than pools (e.g. 2 pools split across 4
// pages), the overflow pages must yield an empty range (start == end), never
// start > end, which would make pools[start:end] slicing panic.
func TestPoolBoundsForSubtree_MorePagesThanPools(t *testing.T) {
	numPools, numCourts, numSubtrees := 2, 1, 4
	covered := 0
	for i := range numSubtrees {
		start, end := PoolBoundsForSubtree(numPools, numCourts, numSubtrees, i)
		if start > end {
			t.Fatalf("page %d: inverted range start=%d > end=%d", i, start, end)
		}
		covered += end - start
	}
	if covered != numPools {
		t.Fatalf("pages must cover every pool exactly once: covered %d of %d", covered, numPools)
	}
}
