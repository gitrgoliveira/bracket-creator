package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertCourtBlocks verifies that PoolBoundsForSubtree hands every page the
// whole pool block of the shiaijo that page belongs to: the ranges are never
// inverted, every page of one court reports the same block, and the blocks over
// all courts partition [0, numPools).
func assertCourtBlocks(t *testing.T, numPools, numCourts, numSubtrees int) {
	t.Helper()
	perCourt := map[int][2]int{}
	for idx := 0; idx < numSubtrees; idx++ {
		start, end := PoolBoundsForSubtree(numPools, numCourts, numSubtrees, idx)
		assert.LessOrEqual(t, start, end, "page %d: start must be <= end", idx)
		court := SubtreeCourtIndex(numSubtrees, numCourts, idx)
		if prev, seen := perCourt[court]; seen {
			assert.Equal(t, prev, [2]int{start, end},
				"every page of shiaijo %d must offer that court's whole block", court)
			continue
		}
		perCourt[court] = [2]int{start, end}
	}

	seen := make([]bool, numPools)
	for _, r := range perCourt {
		for p := r[0]; p < r[1]; p++ {
			assert.False(t, seen[p], "pool %d covered by more than one shiaijo", p)
			seen[p] = true
		}
	}
	for p := range seen {
		assert.True(t, seen[p], "pool %d belongs to no shiaijo", p)
	}
}

// TestPoolBoundsForSubtree_CourtBlocks exercises one page per court and the
// multi-page-per-court case, which is where the retired page-within-court split
// used to hand a page pools its bracket did not contain.
func TestPoolBoundsForSubtree_CourtBlocks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                             string
		numPools, numCourts, numSubtrees int
	}{
		{"one page per court", 8, 2, 2},
		{"two pages per court", 8, 2, 4},
		{"four pages per court", 12, 2, 8},
		{"uneven pools per court", 7, 4, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertCourtBlocks(t, tc.numPools, tc.numCourts, tc.numSubtrees)
		})
	}
}

// TestPoolBoundsForSubtree_SingleTree pins the one case where a page covers
// more than one shiaijo: --single-tree renders the whole draw on one page, so
// that page offers every pool rather than court A's alone.
func TestPoolBoundsForSubtree_SingleTree(t *testing.T) {
	start, end := PoolBoundsForSubtree(8, 4, 1, 0)
	assert.Equal(t, 0, start)
	assert.Equal(t, 8, end)
}

// TestPoolBoundsForSubtree_Degenerate pins the guards: a zero court or page
// count yields an empty range rather than dividing by zero or inverting.
func TestPoolBoundsForSubtree_Degenerate(t *testing.T) {
	for _, tc := range []struct{ pools, courts, subtrees, idx int }{
		{8, 0, 4, 0},
		{8, 2, 0, 0},
		{0, 2, 2, 0},
	} {
		start, end := PoolBoundsForSubtree(tc.pools, tc.courts, tc.subtrees, tc.idx)
		assert.LessOrEqual(t, start, end)
		assert.Equal(t, 0, start)
		assert.Equal(t, 0, end)
	}
}

// TestPageRosterPools is the narrowing that makes a roster overlay honest: a
// page only ever overlays the pools it actually prints a qualifier of.
func TestPageRosterPools(t *testing.T) {
	pools, _ := makePools(4)
	draw := BuildKnockoutDraw(pools, 2, 2)
	require.NotNil(t, draw)

	// One page per court: every home pool's winner is on its own court's page,
	// so narrowing is the identity.
	for c, region := range draw.Regions {
		start, end := PoolBoundsForSubtree(4, 2, 2, c)
		assert.Equal(t, pools[start:end], PageRosterPools(pools[start:end], region),
			"one page per shiaijo overlays the court's whole block")
	}

	// Two pages per court: each page keeps only the pools it prints. The
	// expectation is spelled out rather than re-derived from the page's own
	// leaves, which is the whole point - "every pool it kept is printed on the
	// page" is how PageRosterPools DEFINES its output, so asserting it back
	// cannot fail. Shiaijo A's block is [Pool A, Pool B] on both of A's pages,
	// and each page carries one of them plus a crossed-in runner-up whose pool
	// is outside the block and therefore gets no roster.
	pages := SubdivideRegions(draw.Regions, 2)
	require.Len(t, pages, 4)
	wantClaims := [][]string{{"Pool A"}, {"Pool B"}, {"Pool C"}, {"Pool D"}}
	wantLeaves := [][]string{
		{"Pool A-1st", "Pool C-2nd"},
		{"Pool B-1st", "Pool D-2nd"},
		{"Pool C-1st", "Pool A-2nd"},
		{"Pool D-1st", "Pool B-2nd"},
	}
	for i, page := range pages {
		start, end := PoolBoundsForSubtree(4, 2, len(pages), i)
		got := []string{}
		for _, p := range PageRosterPools(pools[start:end], page) {
			got = append(got, p.PoolName)
		}
		assert.Equalf(t, wantLeaves[i], TreeLeafLabels(page), "page %d bracket", i+1)
		assert.Equalf(t, wantClaims[i], got, "page %d roster overlay", i+1)
	}

	assert.Nil(t, PageRosterPools(nil, draw.Root))
	assert.Nil(t, PageRosterPools(pools, nil))
}

// TestPageOverlayNeverClaimsAnAbsentPool sweeps the narrowing over the whole
// configuration space at the HELPER level: PoolBoundsForSubtree +
// PageRosterPools over the real page subtrees, with the page's contents
// re-derived from its leaves.
//
// Its reach is limited by construction and that limit is the reason the name no
// longer says "rendered": both sides key on the same leaf labels, so it catches
// a narrowing that filters on the wrong THING (this is how it earns its keep)
// but not one that filters on the wrong RANGE. A page handed every court's
// pools still only keeps the ones it prints, so this sweep passes while the
// operator gets a page overlaying rosters from a shiaijo they are not running.
// TestTreePageHomePoolsAlwaysPresent (draw_court_mapping_test.go) closes that
// off the rendered workbook.
func TestPageOverlayNeverClaimsAnAbsentPool(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for nPools := 1; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				t.Run(fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts), func(t *testing.T) {
					pools, _ := makePools(nPools)
					draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
					require.NotNil(t, draw)
					courts := draw.NumCourts()
					pages := SubdivideRegions(draw.Regions, KnockoutPagesPerCourt(draw.Regions))

					for i, page := range pages {
						start, end := PoolBoundsForSubtree(nPools, courts, len(pages), i)
						overlay := PageRosterPools(pools[start:end], page)
						printed := map[string]bool{}
						for _, l := range TreeLeafLabels(page) {
							name, _ := splitPoolNameAndRank(l)
							printed[name] = true
						}
						for _, p := range overlay {
							assert.Truef(t, printed[p.PoolName],
								"page %d overlays %s with no qualifier of it on the page", i+1, p.PoolName)
						}
					}
				})
			}
		}
	}
}
