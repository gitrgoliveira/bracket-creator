package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A tree page is the WALL CHART for a bracket region, and the Elimination
// Matches sheet carries the score sheets for the very same bouts. The sheet
// bands each bout by the shiaijo it is currently on (CourtPlan.ByMatch, written
// by the operator's reassignments); the page used to be titled purely from the
// draw's regions. One workbook could therefore head a page "Shiaijo D" while
// every score sheet for the bouts printed on it sat under band "Shiaijo A" --
// the same file telling its two operators different things.
//
// These pin the rule that closes it: a page follows its bouts when they AGREE on
// a court, and keeps its drawn region otherwise. The second half matters as much
// as the first, because a page spans a whole region and reassignment is per-bout.
func TestTreePageTitleFollowsAReassignedRegion(t *testing.T) {
	t.Parallel()

	// Renders a two-shiaijo draw with the given live-court map and returns the
	// page titles in page order, plus the match numbers on each page.
	render := func(t *testing.T, byMatch map[int64]string) ([]string, [][]int64) {
		t.Helper()
		// The same synthetic roster the page golden uses, so both files
		// describe the same competition shape.
		pools, err := CreatePools(drawGoldenRoster(4), drawGoldenPoolSize, true)
		require.NoError(t, err)
		require.Len(t, pools, 4)

		f := newRenderTargetFile(t)
		defer func() { require.NoError(t, f.Close()) }()

		poolCoords, playerCoords := AddPoolDataToSheet(f, pools, false, "")
		draw := BuildKnockoutDraw(pools, 2, 2)
		require.NotNil(t, draw)

		plan := CourtPlan{Draw: draw, Courts: []string{"A", "B"}, ByMatch: byMatch}
		_, _, err = RenderKnockoutPages(f, plan, false, pools, poolCoords, playerCoords, nil)
		require.NoError(t, err)

		var titles []string
		for _, sheet := range treePageSheets(f) {
			titles = append(titles, readTreePageCourtLabel(t, f, sheet))
		}
		var nums [][]int64
		for _, subtree := range KnockoutPageSubtrees(draw, false) {
			nums = append(nums, junctionMatchNumbers(subtree))
		}
		return titles, nums
	}

	// The drawn answer, and the match numbers each page carries. Every other
	// case below is judged against this, so a change in the draw's shape shows
	// up as a failure here rather than silently rewriting what the fix means.
	baseline, pageNums := render(t, nil)
	require.Len(t, baseline, 2, "a 2-shiaijo draw prints one page per shiaijo")
	require.Equal(t, []string{"A", "B"}, baseline)
	require.NotEmpty(t, pageNums[1], "page 2 must carry bouts for the move to mean anything")

	t.Run("every bout on the page moved to one shiaijo", func(t *testing.T) {
		moved := map[int64]string{}
		for _, n := range pageNums[1] {
			moved[n] = "A"
		}
		titles, _ := render(t, moved)
		assert.Equal(t, "A", titles[1],
			"shiaijo B's whole region is being fought on A, so its wall chart must not still be headed 'Shiaijo B' while the score sheets for those bouts print under band A")
		assert.Equal(t, "A", titles[0], "the untouched page keeps its drawn shiaijo")
	})

	// The reachable split: a stored bracket records a court for every bout at
	// build time, so a real split is "all recorded, one of them elsewhere".
	t.Run("bouts split across shiaijo keep the drawn title", func(t *testing.T) {
		require.GreaterOrEqual(t, len(pageNums[1]), 2, "needs a page with at least two bouts to be split")
		split := map[int64]string{}
		for _, n := range pageNums[1] {
			split[n] = "B"
		}
		split[pageNums[1][0]] = "A"
		titles, _ := render(t, split)
		assert.Equal(t, "B", titles[1],
			"one bout moved is not the page moving: a region spread over two shiaijo has no single court to be titled after, so it stays on the one it was drawn for")
	})

	t.Run("a page with any unrecorded bout keeps the drawn title", func(t *testing.T) {
		require.GreaterOrEqual(t, len(pageNums[1]), 2, "needs a page with at least two bouts")
		partial := map[int64]string{pageNums[1][0]: "A"}
		titles, _ := render(t, partial)
		assert.Equal(t, "B", titles[1],
			"one bout's worth of evidence must not retitle a whole region's wall chart: an unrecorded court is unknown, not agreement")
	})

	t.Run("a court recorded but unchanged reads as the drawn one", func(t *testing.T) {
		same := map[int64]string{}
		for _, n := range pageNums[1] {
			same[n] = "B"
		}
		titles, _ := render(t, same)
		assert.Equal(t, baseline, titles,
			"a freshly drawn competition stores each bout's court AS its region's, so recording them must not change a single title")
	})
}

// junctionMatchNumbers is the match numbers of the bouts printed on one page:
// its junctions, not its leaves. RenderKnockoutPages has already numbered them
// by the time a caller reads them back.
func junctionMatchNumbers(subtree *Node) []int64 {
	var out []int64
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if (n.Left != nil || n.Right != nil) && n.MatchNum() != 0 {
			out = append(out, n.MatchNum())
		}
		walk(n.Left)
		walk(n.Right)
	}
	walk(subtree)
	return out
}
