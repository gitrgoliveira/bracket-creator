package helper

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file pins the page-to-shiaijo mapping (bc-draw R3/R8).
//
// RenderTreePages titles each Excel tree page "Shiaijo <CourtLabel>" from
// SubtreeCourtIndex and overlays the pool rosters PoolBoundsForSubtree hands
// back. Under the court-first draw the bracket printed on that page is a
// genuine subtree of exactly that shiaijo's region, so the title, the roster
// overlay and the competitors all name the same court.
//
// It used to pin the OPPOSITE: the old flat draw scattered every court's
// qualifiers across both halves of the bracket, so a page titled "Shiaijo A"
// and overlaying pools A and B printed Pool C-1st and Pool D-2nd. The sweep
// below asserts that mismatch is now zero for EVERY combination, which is the
// operator-visible half of the whole rewrite.

// pageCourtView is one rendered tree page's two views of itself, which must now
// agree.
type pageCourtView struct {
	courtLabel   string
	claimedPools []string // the roster overlay printed on the page
	presentPools []string // pools whose qualifiers are actually in its bracket
	leaves       []string
}

// renderedPageViews reproduces exactly what RenderTreePages does for page
// labelling and roster overlay, from the same court-first draw the workbook
// renders.
func renderedPageViews(t *testing.T, nPools, poolWinners, numCourts int) []pageCourtView {
	t.Helper()

	pools, _ := makePools(nPools)
	draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
	require.NotNil(t, draw)
	courts := draw.NumCourts()

	subtrees := SubdivideRegions(draw.Regions, KnockoutPagesPerCourt(draw.Regions))
	require.Equal(t, TreePageLayout(draw.Regions, false), len(subtrees),
		"the page count RenderKnockoutPages reports must be the page count it renders")
	require.Zero(t, len(subtrees)%courts, "pages are an exact multiple of the shiaijo count (R8)")

	views := make([]pageCourtView, 0, len(subtrees))
	for i, subtree := range subtrees {
		start, end := PoolBoundsForSubtree(nPools, courts, len(subtrees), i)
		claimed := []string{}
		for _, p := range PageRosterPools(pools[start:end], subtree) {
			claimed = append(claimed, p.PoolName)
		}
		leaves := collectOrderedLeaves(subtree)

		present := []string{}
		for _, l := range leaves {
			name := leafPool(l)
			if name != "" && !slices.Contains(present, name) {
				present = append(present, name)
			}
		}
		slices.Sort(present)

		views = append(views, pageCourtView{
			courtLabel:   CourtLabel(SubtreeCourtIndex(len(subtrees), courts, i)),
			claimedPools: claimed,
			presentPools: present,
			leaves:       leaves,
		})
	}
	return views
}

// TestTreePageCourtLabel_WorkedExample is the worked example from bc-draw,
// inverted: 4 pools x 2 qualifiers on 2 shiaijo. Page 1 is titled "Shiaijo A",
// overlays Pool A and Pool B, and its bracket now holds exactly shiaijo A's
// home winners plus the runners-up that crossed in from its partner court -
// never a competitor whose pool ran on the other shiaijo's region.
func TestTreePageCourtLabel_WorkedExample(t *testing.T) {
	const (
		nPools      = 4
		poolWinners = 2
		numCourts   = 2
	)

	assignments, err := AssignPoolsToCourts(nPools, numCourts)
	require.NoError(t, err)
	require.Equal(t, []int{0, 0, 1, 1}, assignments,
		"pools A,B belong to shiaijo A and C,D to shiaijo B")

	views := renderedPageViews(t, nPools, poolWinners, numCourts)
	require.Len(t, views, 2, "8 entrants on 2 courts render 2 tree pages, one per shiaijo")

	t.Run("page_1_titled_Shiaijo_A", func(t *testing.T) {
		p := views[0]
		assert.Equal(t, "A", p.courtLabel)
		assert.Equal(t, []string{"Pool A", "Pool B"}, p.claimedPools,
			"page 1's roster overlay claims shiaijo A's pools")
		// A's own winners stay; C and D's runners-up cross in from the partner
		// court (R4b). Nothing from A or B's pools appears on the other page.
		assert.ElementsMatch(t,
			[]string{"Pool A-1st", "Pool B-1st", "Pool C-2nd", "Pool D-2nd"}, p.leaves)
	})

	t.Run("page_2_titled_Shiaijo_B", func(t *testing.T) {
		p := views[1]
		assert.Equal(t, "B", p.courtLabel)
		assert.Equal(t, []string{"Pool C", "Pool D"}, p.claimedPools)
		assert.ElementsMatch(t,
			[]string{"Pool C-1st", "Pool D-1st", "Pool A-2nd", "Pool B-2nd"}, p.leaves)
	})
}

// TestTreePageHomePoolsAlwaysPresent sweeps the whole configuration space and
// asserts the property that makes a printed page usable: every pool the page's
// roster overlay claims has at least one of its qualifiers in the bracket
// printed on that page, and every court's HOME winners are on that court's own
// pages.
//
// The converse ("nothing from another court appears") is deliberately NOT
// asserted: R4 crossing means a page legitimately shows the partner court's
// runners-up, which is the whole point of the rule. What the old draw got wrong
// was the first half - a page claiming pools whose competitors were nowhere on
// it - and that is now empty everywhere.
func TestTreePageHomePoolsAlwaysPresent(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for nPools := 2; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				name := fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts)
				t.Run(name, func(t *testing.T) {
					views := renderedPageViews(t, nPools, poolWinners, numCourts)

					var claimedButAbsent []string
					for i, p := range views {
						for _, c := range p.claimedPools {
							if !slices.Contains(p.presentPools, c) {
								claimedButAbsent = append(claimedButAbsent,
									fmt.Sprintf("page %d (Shiaijo %s) claims %s, contains %v",
										i+1, p.courtLabel, c, p.presentPools))
							}
						}
					}
					assert.Empty(t, claimedButAbsent,
						"every pool a page's roster overlay claims must have a qualifier on that page")
				})
			}
		}
	}
}

// TestHomeWinnersStayOnTheirOwnShiaijoPage is R4a stated as a page property: a
// pool's WINNER is always printed on a page belonging to the shiaijo that pool
// ran on. This is what an operator running shiaijo C relies on when they pick
// up shiaijo C's pages.
func TestHomeWinnersStayOnTheirOwnShiaijoPage(t *testing.T) {
	for _, numCourts := range []int{2, 4} {
		for nPools := numCourts; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				name := fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts)
				t.Run(name, func(t *testing.T) {
					pools, poolNames := makePools(nPools)
					draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
					require.NotNil(t, draw)
					assignment, err := AssignPoolsToCourts(nPools, draw.NumCourts())
					require.NoError(t, err)

					for pi, name := range poolNames {
						want := assignment[pi]
						assert.Contains(t, TreeLeafLabels(draw.Regions[want]), name+"-1st",
							"%s ran on shiaijo %s, so its winner belongs to that region",
							name, CourtLabel(want))
					}
				})
			}
		}
	}
}

// TestTreePageCountIsAMultipleOfTheShiaijoCount pins R8's arithmetic directly,
// including the case the old layout could not express at all: a 2-entrant draw
// on 4 shiaijo. TreePageLayout used to return a power of two regardless of the
// tree (4 pages), and SubdivideTree, out of levels, appended the WHOLE TREE as
// a trailing third page that reprinted the only match in the draw.
func TestTreePageCountIsAMultipleOfTheShiaijoCount(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for nPools := 1; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				name := fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts)
				t.Run(name, func(t *testing.T) {
					pools, _ := makePools(nPools)
					draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
					require.NotNil(t, draw)

					pagesPerCourt := KnockoutPagesPerCourt(draw.Regions)
					assert.Contains(t, []int{1, 2, 4}, pagesPerCourt,
						"a shiaijo gets 1, 2 or 4 pages, never anything else (R8)")

					pages := SubdivideRegions(draw.Regions, pagesPerCourt)
					assert.Len(t, pages, draw.NumCourts()*pagesPerCourt)
					assert.Equal(t, TreePageLayout(draw.Regions, false), len(pages))

					// No page reprints a match another page already carries.
					seen := map[string]int{}
					for _, p := range pages {
						for _, l := range TreeLeafLabels(p) {
							seen[l]++
						}
					}
					for l, n := range seen {
						assert.Equal(t, 1, n, "%s is printed on %d pages", l, n)
					}
					assert.Len(t, seen, nPools*poolWinners, "every entrant is printed exactly once")
				})
			}
		}
	}
}

// TestTreePageLayoutSingleTree pins --single-tree: it forces ONE page and wins
// outright. It used to be silently overridden by the court expansion, so
// "--single-tree" on a 4-court event still printed four pages.
func TestTreePageLayoutSingleTree(t *testing.T) {
	pools, _ := makePools(8)
	draw := BuildKnockoutDraw(pools, 2, 4)
	require.NotNil(t, draw)

	assert.Equal(t, 4, TreePageLayout(draw.Regions, false))
	assert.Equal(t, 1, TreePageLayout(draw.Regions, true))

	// The one page covers every court, so it names the whole shiaijo range and
	// overlays every pool rather than claiming to be shiaijo A's.
	assert.Equal(t, "Shiaijo A-D", TreePageTitle(1, 4, 0))
	start, end := PoolBoundsForSubtree(8, 4, 1, 0)
	assert.Equal(t, 0, start)
	assert.Equal(t, 8, end)
}
