package helper

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file pins the page-to-shiaijo LABELLING DEFECT (bc-draw Phase 1).
//
// RenderTreePages titles each Excel tree page "Shiaijo <CourtLabel>" from
// SubtreeCourtIndex and overlays the pool rosters PoolBoundsForSubtree hands
// back, but the bracket printed on that page comes from SubdivideTree, which
// splits the draw by TREE POSITION. Because GenerateFinals interleaves each
// court's qualifiers across both halves of the draw, the title and the roster
// describe one shiaijo while the bracket shows another's competitors.
//
// Everything asserted here is CURRENT, WRONG behaviour. It violates bc-draw R3
// ("each shiaijo's pools occupy exactly ONE contiguous region of the draw, and
// that region IS a subtree") and R8 (a tree page must be a genuine subtree of a
// court's region). Do not fix it in this file: it is the "before" side of the
// rewrite's review diff, and the full sweep lives in testdata/draw_shapes.json.

// pageCourtView is one rendered tree page's two disagreeing views of itself.
type pageCourtView struct {
	courtLabel   string
	claimedPools []string // the roster overlay printed on the page
	presentPools []string // pools whose qualifiers are actually in its bracket
	leaves       []string
}

// renderedPageViews reproduces exactly what RenderTreePages does for page
// labelling and roster overlay. The whole-tree placement pass it applies is now
// the only one there is: RenderKnockoutPages runs ApplyPoolAdjustments before
// splitting the tree, so this model and the rendered workbook see the same
// leaves (it used to be the ENGINE's order only, because the Excel path adjusted
// per page subtree instead).
func renderedPageViews(t *testing.T, nPools, poolWinners, numCourts int) []pageCourtView {
	t.Helper()

	pools, poolNames := makePools(nPools)
	tree := buildAdjustedTree(pools, poolWinners)

	numPages, err := TreePageLayout(nPools*poolWinners, numCourts, false)
	require.NoError(t, err)
	// RenderTreePages derives both the court label and the pool bounds from
	// len(subtrees), NOT from the requested page count, so this does too.
	subtrees := SubdivideTree(tree, numPages)

	views := make([]pageCourtView, 0, len(subtrees))
	for i, subtree := range subtrees {
		start, end := PoolBoundsForSubtree(nPools, numCourts, len(subtrees), i)
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
			courtLabel:   CourtLabel(SubtreeCourtIndex(len(subtrees), numCourts, i)),
			claimedPools: append([]string{}, poolNames[start:end]...),
			presentPools: present,
			leaves:       leaves,
		})
	}
	return views
}

// TestTreePageCourtLabelMismatch_CurrentBehaviour is the worked example from
// bc-draw: 4 pools x 2 qualifiers on 2 shiaijo. Page 1 is titled "Shiaijo A"
// and overlays Pool A and Pool B, but its bracket holds Pool C-1st and
// Pool D-2nd, competitors who fought their pools on shiaijo B.
func TestTreePageCourtLabelMismatch_CurrentBehaviour(t *testing.T) {
	const (
		nPools      = 4
		poolWinners = 2
		numCourts   = 2
	)

	// The court blocks themselves are contiguous and correct: pools A/B on
	// shiaijo A, pools C/D on shiaijo B. The defect is entirely in how the
	// bracket is split, not in how pools are allocated to courts.
	assignments, err := AssignPoolsToCourts(nPools, numCourts)
	require.NoError(t, err)
	require.Equal(t, []int{0, 0, 1, 1}, assignments,
		"pools A,B belong to shiaijo A and C,D to shiaijo B")

	views := renderedPageViews(t, nPools, poolWinners, numCourts)
	require.Len(t, views, 2, "8 entrants on 2 courts render 2 tree pages")

	t.Run("page_1_titled_Shiaijo_A", func(t *testing.T) {
		p := views[0]
		assert.Equal(t, "A", p.courtLabel, "page 1 is titled \"Shiaijo A\"")
		assert.Equal(t, []string{"Pool A", "Pool B"}, p.claimedPools,
			"page 1's roster overlay claims shiaijo A's pools")

		// ...but the bracket printed underneath that title holds shiaijo B's
		// qualifiers. This is the defect, stated as an assertion so the
		// rewrite cannot land silently.
		assert.Contains(t, p.leaves, "Pool C-1st",
			"page titled Shiaijo A prints shiaijo B's Pool C winner")
		assert.Contains(t, p.leaves, "Pool D-2nd",
			"page titled Shiaijo A prints shiaijo B's Pool D runner-up")
		assert.Equal(t, []string{"Pool A", "Pool B", "Pool C", "Pool D"}, p.presentPools,
			"page titled Shiaijo A actually contains competitors from ALL FOUR pools")
	})

	t.Run("page_2_titled_Shiaijo_B", func(t *testing.T) {
		p := views[1]
		assert.Equal(t, "B", p.courtLabel, "page 2 is titled \"Shiaijo B\"")
		assert.Equal(t, []string{"Pool C", "Pool D"}, p.claimedPools,
			"page 2's roster overlay claims shiaijo B's pools")
		assert.Contains(t, p.leaves, "Pool A-2nd",
			"page titled Shiaijo B prints shiaijo A's Pool A runner-up")
		assert.Contains(t, p.leaves, "Pool B-1st",
			"page titled Shiaijo B prints shiaijo A's Pool B winner")
		assert.Equal(t, []string{"Pool A", "Pool B", "Pool C", "Pool D"}, p.presentPools,
			"page titled Shiaijo B actually contains competitors from ALL FOUR pools")
	})
}

// TestTreePageCourtLabelMismatch_Sweep pins WHEN the labelling happens to come
// out right on a multi-shiaijo draw. Exactly one combination survives today:
// ONE qualifier per pool with the pool count divisible by the court count. At
// 1 qualifier nothing crosses between pools, so the positional split coincides
// with the court blocks; add a second qualifier and every configuration
// mislabels. bc-draw R3/R4 make the correct case universal.
func TestTreePageCourtLabelMismatch_Sweep(t *testing.T) {
	for _, numCourts := range []int{2, 4} {
		for nPools := 2; nPools <= 12; nPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				name := fmt.Sprintf("%d_pools_%d_winners_%d_courts", nPools, poolWinners, numCourts)
				t.Run(name, func(t *testing.T) {
					views := renderedPageViews(t, nPools, poolWinners, numCourts)

					var mismatched []string
					for i, p := range views {
						claimedButAbsent := false
						for _, c := range p.claimedPools {
							if !slices.Contains(p.presentPools, c) {
								claimedButAbsent = true
							}
						}
						presentButUnclaimed := false
						for _, c := range p.presentPools {
							if !slices.Contains(p.claimedPools, c) {
								presentButUnclaimed = true
							}
						}
						if claimedButAbsent || presentButUnclaimed {
							mismatched = append(mismatched, fmt.Sprintf(
								"page %d (Shiaijo %s) claims %v, contains %v",
								i+1, p.courtLabel, p.claimedPools, p.presentPools))
						}
					}

					wantCorrect := poolWinners == 1 && nPools%numCourts == 0
					if wantCorrect {
						assert.Empty(t, mismatched,
							"1 qualifier per pool with %d pools evenly split over %d shiaijo is the one case that labels correctly today",
							nPools, numCourts)
						return
					}
					assert.NotEmpty(t, mismatched,
						"expected the known page-to-shiaijo labelling defect for %d pools x %d qualifiers on %d shiaijo; if this is now clean, bc-draw R3/R8 landed and this test must be inverted",
						nPools, poolWinners, numCourts)
				})
			}
		}
	}
}

// TestTreePageCountExceedsTreeDepth_CurrentBehaviour pins the second page-layout
// defect: TreePageLayout only ever returns a power of two and never checks the
// tree can be cut that many times, while SubdivideTree, having run out of
// levels, appends the WHOLE TREE as a trailing page. A 2-pool, 1-qualifier draw
// on 4 shiaijo therefore asks for 4 pages and renders 3, the last of which
// duplicates the entire bracket and is titled with the last court.
//
// It is the only combination in the bc-draw sweep (1..4 qualifiers x 2..12
// pools x 1/2/4 courts) where the requested and rendered page counts differ.
func TestTreePageCountExceedsTreeDepth_CurrentBehaviour(t *testing.T) {
	pools, _ := makePools(2)
	tree := buildAdjustedTree(pools, 1)

	numPages, err := TreePageLayout(2, 4, false)
	require.NoError(t, err)
	assert.Equal(t, 4, numPages, "4 shiaijo force 4 pages even for a 2-entrant draw")

	subtrees := SubdivideTree(tree, numPages)
	assert.Len(t, subtrees, 3, "SubdivideTree cannot honour 4 pages on a 2-leaf tree")

	assert.Equal(t, []string{"Pool A-1st"}, collectOrderedLeaves(subtrees[0]))
	assert.Equal(t, []string{"Pool B-1st"}, collectOrderedLeaves(subtrees[1]))
	assert.Equal(t, []string{"Pool A-1st", "Pool B-1st"}, collectOrderedLeaves(subtrees[2]),
		"page 3 is the WHOLE tree again, so the only match in the draw is printed twice")

	// And the duplicate page is labelled as its own shiaijo.
	labels := make([]string, len(subtrees))
	for i := range subtrees {
		labels[i] = CourtLabel(SubtreeCourtIndex(len(subtrees), 4, i))
	}
	assert.Equal(t, []string{"A", "B", "C"}, labels,
		"three pages get three different shiaijo titles for a two-competitor draw")
}
