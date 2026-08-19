package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// walkLeafOffsets is the ONE traversal that turns a draw tree back into slot
// positions, and two exported readers depend on it disagreeing with nobody:
// RegionSpans decides which slots a shiaijo's region owns, NodeCourts decides
// which shiaijo a bout prints under, and SlotRoundMatches locates a bout's
// first-round window. Its own doc comment says a disagreement puts the operator
// console and the printed running order on different courts with nothing to
// catch it, so the geometry is pinned here directly rather than only through
// whichever draws today's builders happen to produce.
//
// The property under test is the definition of the geometry, not a restatement
// of the arithmetic: the BAND a node is reported to occupy must be exactly that
// node's own slots, i.e. SlotArray(root)[band] == SlotArray(node), element for
// element. Anything that reports a wrong offset or a wrong width fails it.
//
// The cases below all carry a LEADING empty half, which is what produces
// Node.risenBefore (BuildSlotTree collapses the empty side and marks the
// survivor). That branch reported the CONTENT offset with the BAND width, so a
// node with risenBefore=1 and width 8 claimed [4, 12) for slots it owned
// [0, 8) -- handing the next region's first four slots to this court. It is
// reachable only through BuildSlotTree today, which is exactly why it needs a
// test of its own: no fixture draw in the suite starts with an empty half, so
// nothing else would ever have noticed.
func TestWalkLeafOffsetsBandIsExactlyTheNodesOwnSlots(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		slots []string
	}{
		// No rises at all: the baseline every existing draw already exercises.
		{"full array, no rises", []string{"A", "B", "C", "D"}},
		// Trailing empty half -> risenAfter: content at the START of the band.
		{"trailing empty half", []string{"A", "B", "", ""}},
		// Leading empty half -> risenBefore: content at the END of the band.
		{"leading empty half", []string{"", "", "A", "B"}},
		{"leading empty half, two levels", []string{"", "", "", "", "A", "B", "C", "D"}},
		// A rise on each LEAF rather than on a junction.
		{"leading empty quarter under each half", []string{"", "A", "", "B"}},
		// Leading and trailing empties in the same array, so the two rise
		// directions appear on sibling subtrees of one root.
		{"leading empties one side, trailing the other", []string{"", "", "A", "B", "C", "D", "", ""}},
		// A double rise: an empty half whose surviving side also collapses.
		{"nested leading collapse", []string{"", "", "", "A"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := BuildSlotTree(tc.slots)
			require.NotNil(t, root, "fixture must build a tree")
			rootSlots := SlotArray(root)

			seen := 0
			walkLeafOffsets(root, 0, func(n *Node, bandOffset, contentOffset, width int) {
				seen++
				label := nodeLabelForTest(n)

				require.LessOrEqualf(t, bandOffset+width, len(rootSlots),
					"node %s: reported band [%d,%d) runs past the %d-slot array",
					label, bandOffset, bandOffset+width, len(rootSlots))
				require.GreaterOrEqualf(t, bandOffset, 0, "node %s: negative band offset", label)

				assert.Equalf(t, SlotArray(n), rootSlots[bandOffset:bandOffset+width],
					"node %s: the band reported for it is not the slots it actually owns", label)

				// The content sub-range is where the node's OWN entrants sit,
				// which SlotRoundMatches reads as a bout's first-round window.
				// SlotArray pads a rise onto the FRONT of the band for a
				// leading collapse and onto the BACK for a trailing one, so
				// the content is the band with this node's own pad stripped
				// from the corresponding end. (A CHILD's rise leaves empties
				// inside the content; those belong to the child, not here,
				// which is why this compares sub-arrays rather than asserting
				// the first content slot is non-empty.)
				content := width >> (n.risenAfter + n.risenBefore)
				assert.GreaterOrEqualf(t, contentOffset, bandOffset,
					"node %s: content offset %d is before its band start %d", label, contentOffset, bandOffset)
				require.LessOrEqualf(t, contentOffset+content, bandOffset+width,
					"node %s: content [%d,%d) runs past its band end %d", label, contentOffset, contentOffset+content, bandOffset+width)

				own := SlotArray(n)
				wantContent := own[:content]
				if n.risenBefore > 0 {
					wantContent = own[width-content:]
				}
				assert.Equalf(t, wantContent, rootSlots[contentOffset:contentOffset+content],
					"node %s: the content window reported for it is not where its own entrants sit", label)
			})
			assert.Positive(t, seen, "the walk visited no nodes")
		})
	}
}

// TestRegionSpansTileTheSlotArray is the reader-level consequence of the
// property above: regions are contiguous and aligned by construction (R3), so
// their spans must tile the leaf array with no overlap and no gap. An
// overshooting band shows up here as one region's span reaching into the next
// region's slots, which is the failure an operator would actually see -- a bout
// printed under the wrong shiaijo.
func TestRegionSpansDoNotOverlap(t *testing.T) {
	t.Parallel()

	// Enough pools to give every shiaijo a region, across the court counts the
	// draw supports.
	for _, courts := range []int{1, 2, 4} {
		for _, numPools := range []int{4, 5, 6, 8, 11} {
			t.Run(fmt.Sprintf("%dpools_%dcourts", numPools, courts), func(t *testing.T) {
				t.Parallel()
				pools := make([]Pool, numPools)
				for i := range pools {
					pools[i] = Pool{PoolName: fmt.Sprintf("Pool %c", 'A'+i)}
					for j := 0; j < 3; j++ {
						pools[i].Players = append(pools[i].Players, Player{Name: fmt.Sprintf("P%d-%d", i, j)})
					}
				}
				draw := BuildKnockoutDraw(pools, 1, courts)
				require.NotNil(t, draw)

				spans := draw.RegionSpans()
				total := len(TreeToLeafArray(draw.Root))
				for i, s := range spans {
					if s[1] == s[0] {
						continue // a court with no region gets a zero-width span
					}
					assert.LessOrEqualf(t, s[1], total,
						"region %d span %v runs past the %d-slot array", i, s, total)
					for j, other := range spans {
						if j <= i || other[1] == other[0] {
							continue
						}
						overlaps := s[0] < other[1] && other[0] < s[1]
						assert.Falsef(t, overlaps,
							"regions %d %v and %d %v overlap: a bout in the shared slots would print under two shiaijo",
							i, s, j, other)
					}
				}
			})
		}
	}
}

func nodeLabelForTest(n *Node) string {
	if n.LeafNode {
		return fmt.Sprintf("leaf %q (before=%d after=%d)", n.LeafVal, n.risenBefore, n.risenAfter)
	}
	return fmt.Sprintf("junction val=%d (before=%d after=%d)", n.Val, n.risenBefore, n.risenAfter)
}
