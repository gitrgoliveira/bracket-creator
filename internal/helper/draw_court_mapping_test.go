package helper

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
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
//
// Everything here is read back out of a RENDERED workbook (see
// renderedPageViews). A page's title, its roster overlay and its bracket are
// three independent artifacts on the sheet, so comparing them measures the
// shipped renderer; recomputing any of them from the helper that produced it
// would only compare that helper with itself.

// pageCourtView is one rendered tree page's two views of itself, which must now
// agree.
type pageCourtView struct {
	courtLabel   string
	claimedPools []string // the roster overlay printed on the page
	presentPools []string // pools whose qualifiers are actually in its bracket
	leaves       []string
}

// readTreePageOverlayPools returns the pools whose roster the page actually
// overlays, read out of the sheet. AddPoolsToTree writes each overlaid pool's
// data-sheet header as a formula down column A ("'data'!$A$6"), so inverting
// poolCoords turns that formula back into the pool it names. Column A is the
// overlay's own column: PrintLeafNodes starts at 2*depth, so no entrant label
// can be mistaken for one.
func readTreePageOverlayPools(t *testing.T, f *excelize.File, sheet string, poolByHeaderRef map[string]string) []string {
	t.Helper()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	claimed := []string{}
	// +4 covers the trailing rows AddPoolsToTree styles but leaves empty, which
	// GetRows may not report.
	for r := 1; r <= len(rows)+4; r++ {
		formula, err := f.GetCellFormula(sheet, fmt.Sprintf("A%d", r))
		require.NoError(t, err)
		if name, ok := poolByHeaderRef[formula]; ok {
			claimed = append(claimed, name)
		}
	}
	return claimed
}

// renderedPageViews renders a competition through the REAL workbook funnel and
// reads both of each page's views of itself back out of the sheet it produced:
// the roster overlay from the pool-header formulas AddPoolsToTree writes down
// column A, and the bracket from the entrant labels PrintLeafNodes writes into
// the tree columns.
//
// It used to RE-CALL PoolBoundsForSubtree and PageRosterPools to work out the
// overlay, which made every "the page's claim matches its bracket" assertion
// below a TAUTOLOGY. PageRosterPools computes the claim by filtering the
// shiaijo's block down to the pools whose labels are on the page, so a claim it
// produced could not name an absent pool however the helper behaved: the test
// compared a set against a superset of itself and "0 mismatches" proved nothing
// about the shipped overlay. Reading the claim off the rendered sheet measures
// what an operator is handed, so an over-claiming overlay (the defect the
// narrowing exists to prevent) now fails these tests - see
// TestTreePageHomePoolsAlwaysPresent.
//
// Pools come from the golden files' synthetic roster rather than name-only
// stubs because the overlay is written from poolCoords, which
// AddPoolDataToSheet only populates for pools that have players.
func renderedPageViews(t *testing.T, nPools, poolWinners, numCourts int) []pageCourtView {
	t.Helper()

	pools, err := CreatePools(drawGoldenRoster(nPools), drawGoldenPoolSize, true)
	require.NoError(t, err)
	require.Len(t, pools, nPools)

	draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
	require.NotNil(t, draw)
	courts := draw.NumCourts()

	f := newRenderTargetFile(t)
	defer func() { _ = f.Close() }()
	poolCoords, playerCoords := AddPoolDataToSheet(f, pools, false, "")
	poolByHeaderRef := make(map[string]string, len(poolCoords))
	for name, coord := range poolCoords {
		poolByHeaderRef[sheetRef(coord.sheetName, coord.cell)] = name
	}
	require.Len(t, poolByHeaderRef, nPools, "every pool must have a distinct data-sheet header to overlay")

	_, numPages, err := RenderKnockoutPages(f, draw, false, pools, poolCoords, playerCoords, nil)
	require.NoError(t, err)

	sheets := treePageSheets(f)
	require.Equal(t, numPages, len(sheets),
		"the page count RenderKnockoutPages reports must be the page count it renders")
	require.Zero(t, len(sheets)%courts, "pages are an exact multiple of the shiaijo count (R8)")

	views := make([]pageCourtView, 0, len(sheets))
	for _, sheet := range sheets {
		leaves := readTreePageLeaves(t, f, sheet)

		present := []string{}
		for _, l := range leaves {
			name := leafPool(l)
			if name != "" && !slices.Contains(present, name) {
				present = append(present, name)
			}
		}
		slices.Sort(present)

		views = append(views, pageCourtView{
			courtLabel:   readTreePageCourtLabel(t, f, sheet),
			claimedPools: readTreePageOverlayPools(t, f, sheet, poolByHeaderRef),
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
// asserts the two halves of an honest roster overlay, from the RENDERED sheet:
//
//  1. Nothing overlaid is absent. Every pool the printed overlay names has at
//     least one of its qualifiers in the bracket printed on the same page. This
//     is what the narrowing (PageRosterPools) buys, and it only bites on a
//     shiaijo printed across 2 or 4 pages, where one page carries one child
//     subtree of the region and therefore only some of the court's pools.
//  2. Nothing owed is missing, and nothing foreign is added. Across one
//     shiaijo's pages the overlays name EXACTLY that shiaijo's pool block from
//     AssignPoolsToCourts - the same allocation the Pool Matches sheet and the
//     schedule use - so the operator holding that court's pages holds every
//     roster it ran and no other court's.
//
// Half 2 is what makes half 1 worth asserting. On its own, half 1 is also
// satisfied by overlaying NOTHING; the union check is the independent
// cross-reference (AssignPoolsToCourts against the printed sheet) that pins the
// overlay from the other side.
//
// The per-page converse ("nothing from another court appears in the BRACKET")
// is deliberately not asserted: R4 crossing means a page legitimately prints the
// partner court's runners-up, which is the whole point of the rule. Its
// qualifiers just do not get a roster.
func TestTreePageHomePoolsAlwaysPresent(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for nPools := 2; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				name := fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts)
				t.Run(name, func(t *testing.T) {
					views := renderedPageViews(t, nPools, poolWinners, numCourts)
					_, poolNames := makePools(nPools)

					var claimedButAbsent []string
					overlaidByCourt := map[string][]string{}
					for i, p := range views {
						for _, c := range p.claimedPools {
							if !slices.Contains(p.presentPools, c) {
								claimedButAbsent = append(claimedButAbsent,
									fmt.Sprintf("page %d (Shiaijo %s) claims %s, contains %v",
										i+1, p.courtLabel, c, p.presentPools))
							}
							if !slices.Contains(overlaidByCourt[p.courtLabel], c) {
								overlaidByCourt[p.courtLabel] = append(overlaidByCourt[p.courtLabel], c)
							}
						}
					}
					assert.Empty(t, claimedButAbsent,
						"every pool a page's roster overlay claims must have a qualifier on that page")

					// The draw's own court count, not the requested one:
					// EffectiveDrawCourts clamps a request bigger than the pool
					// count (8 shiaijo over 7 pools draws on 4).
					drawCourts := EffectiveDrawCourts(nPools, numCourts)
					assignment, err := AssignPoolsToCourts(nPools, drawCourts)
					require.NoError(t, err)
					wantByCourt := map[string][]string{}
					for pi, court := range assignment {
						label := CourtLabel(court)
						wantByCourt[label] = append(wantByCourt[label], poolNames[pi])
					}
					for label, want := range wantByCourt {
						assert.ElementsMatch(t, want, overlaidByCourt[label],
							"shiaijo %s ran %v, so its pages must overlay exactly those rosters", label, want)
					}
					assert.Len(t, overlaidByCourt, len(wantByCourt),
						"no page may overlay a roster under a shiaijo label that ran no pools")
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

					pages := KnockoutPageSubtrees(draw, false)
					assert.Len(t, pages, draw.NumCourts()*pagesPerCourt)

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

	assert.Len(t, KnockoutPageSubtrees(draw, false), 4)

	single := KnockoutPageSubtrees(draw, true)
	assert.Len(t, single, 1)
	assert.Same(t, draw.Root, single[0],
		"the one page must be the WHOLE bracket, not a region that happens to be first")

	// The one page covers every court, so it names the whole shiaijo range and
	// overlays every pool rather than claiming to be shiaijo A's.
	assert.Equal(t, "Shiaijo A-D", TreePageTitle(1, 4, 0))
	start, end := PoolBoundsForSubtree(8, 4, 1, 0)
	assert.Equal(t, 0, start)
	assert.Equal(t, 8, end)
}
