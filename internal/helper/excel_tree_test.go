package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// TestAddPoolsToTreeCellContent renders a small pool (3 players) into the
// Tree sheet and asserts the column-A layout: pool name formula at the first
// content row (TreeTitleRows+1 = row 4), followed by player formulas on
// consecutive rows. This pins the "names along column A" layout invariant
// described in CLAUDE.md / tree.go so changes to row spacing or starting
// offset trip a focused unit test before manifesting as misaligned brackets.
func TestAddPoolsToTreeCellContent(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	sheetName := "Tree 1"
	_, err := f.NewSheet(sheetName)
	require.NoError(t, err)
	_, err = f.NewSheet(SheetData)
	require.NoError(t, err)

	// Minimal 3-player pool, small enough to enumerate every cell by hand,
	// large enough to confirm the row pointer advances across multiple
	// players and applies the post-pool spacer.
	players := []Player{
		{Name: "Alice", PoolPosition: 1},
		{Name: "Bob", PoolPosition: 2},
		{Name: "Carol", PoolPosition: 3},
	}
	pools := []Pool{{PoolName: "Pool A", Players: players}}

	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetData, cell: "$A$2"},
	}
	pCoords := map[string]playerCellCoord{
		playerCoordKey(players[0]): {cellCoord: cellCoord{sheetName: SheetData, cell: "$B$2"}},
		playerCoordKey(players[1]): {cellCoord: cellCoord{sheetName: SheetData, cell: "$B$3"}},
		playerCoordKey(players[2]): {cellCoord: cellCoord{sheetName: SheetData, cell: "$B$4"}},
	}

	lastRow := AddPoolsToTree(f, sheetName, pools, poolCoords, pCoords)

	startRow := TreeTitleRows + 1 // first content row in column A

	// Callers bound the page's print area with the return value, so it must
	// point at the last styled cell: header row + 3 players + the top-border
	// spacer under the box.
	assert.Equal(t, startRow+len(players)+1, lastRow, "AddPoolsToTree must report the last row it touched")

	t.Run("pool header at row 4", func(t *testing.T) {
		got, err := f.GetCellFormula(sheetName, fmt.Sprintf("A%d", startRow))
		require.NoError(t, err)
		want := fmt.Sprintf("%s!%s", SheetData, "$A$2")
		assert.Equal(t, strings.ReplaceAll(want, "'", ""), strings.ReplaceAll(got, "'", ""))
	})

	t.Run("players follow on consecutive rows", func(t *testing.T) {
		for i, p := range players {
			row := startRow + 1 + i
			got, err := f.GetCellFormula(sheetName, fmt.Sprintf("A%d", row))
			require.NoErrorf(t, err, "row %d", row)
			want := fmt.Sprintf("\"%d. \" & %s!%s", p.PoolPosition, SheetData, pCoords[playerCoordKey(p)].cell)
			assert.Equal(t,
				strings.ReplaceAll(want, "'", ""),
				strings.ReplaceAll(got, "'", ""),
				"player %s at row %d", p.Name, row,
			)
		}
	})
}

func TestSetTreeSheetTitle(t *testing.T) {
	tests := []struct {
		name            string
		title           string
		expectedFormula string
	}{
		{
			name:            "Shiaijo A",
			title:           "Shiaijo A",
			expectedFormula: `IF(data!$B$1="","Shiaijo A",data!$B$1&" - Shiaijo A")`,
		},
		{
			name:            "Shiaijo B",
			title:           "Shiaijo B",
			expectedFormula: `IF(data!$B$1="","Shiaijo B",data!$B$1&" - Shiaijo B")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

			_, err := f.NewSheet("Tree 1")
			require.NoError(t, err)
			_, err = f.NewSheet(SheetData)
			require.NoError(t, err)

			SetTreeSheetTitle(f, "Tree 1", tt.title, TreePageLastCol(3))

			formula, err := f.GetCellFormula("Tree 1", "A1")
			require.NoError(t, err)
			assert.Equal(t, tt.expectedFormula, formula)

			// The title merge is the widest thing on a tree page, so it must stop
			// at the last bracket column or it pushes the used range past the
			// print area and spills a near-blank page.
			merged, err := f.GetMergeCells("Tree 1")
			require.NoError(t, err)
			require.Len(t, merged, 1)
			assert.Equal(t, "A1:G1", merged[0][0], "title must merge across exactly the bracket's columns")
		})
	}
}

// TestTreePageGeometry verifies TreePageLastCol/TreePageLastRow empirically:
// render brackets of many sizes with PrintLeafNodes, then scan every cell for
// content OR style (the bracket lines are style-only cells, invisible to
// GetRows) and assert the geometry helpers bound the true used range. These
// bounds drive each page's print area; a bound one cell short would clip the
// bracket off the printed page, one column long would re-introduce the
// near-blank spill page the print area exists to prevent.
//
// Sizes deliberately include non-powers-of-two: those produce UNBALANCED trees
// (byes sit as leaves at shallower levels), and the bound must hold for them
// too. Both helpers are in fact exact for every CreateBalancedTree shape: the
// root's bracket line always lands in column 2*depth+1, and the split
// (mid = len/2) always puts a deepest chain on the right-most descent, whose
// row offsets sum to 2^depth - 1. The ADJUSTED variant is exercised separately
// because treeAdjustment may swap a bye pair, moving the deepest chain off the
// right-most path and leaving the last rows of the envelope unused. (The
// placement pass is applied to the tree here, the way RenderKnockoutPages
// applies it before rendering; PrintLeafNodes itself no longer reorders.)
func TestTreePageGeometry(t *testing.T) {
	for _, leaves := range []int{1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 13, 16} {
		for _, adjusted := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d leaves adjusted=%v", leaves, adjusted), func(t *testing.T) {
				f := excelize.NewFile()
				defer f.Close()
				const sheet = "Tree 1"
				_, err := f.NewSheet(sheet)
				require.NoError(t, err)

				names := make([]string, leaves)
				for i := range names {
					// Pool-style labels so the adjusted variant exercises
					// treeAdjustment's real reordering logic.
					names[i] = fmt.Sprintf("Pool %c-%s", 'A'+rune(i/2), GetOrdinal(i%2+1))
				}
				tree := CreateBalancedTree(names)
				if adjusted {
					ApplyPoolAdjustments(tree)
				}
				depth := CalculateDepth(tree)
				startRow := TreeTitleRows + 1
				PrintLeafNodes(tree, f, sheet, 2*depth, startRow, depth, nil)

				maxRow, maxCol := 0, 0
				for r := 1; r <= 80; r++ {
					for c := 1; c <= 40; c++ {
						cell, cerr := excelize.CoordinatesToCellName(c, r)
						require.NoError(t, cerr)
						styleID, serr := f.GetCellStyle(sheet, cell)
						require.NoError(t, serr)
						val, verr := f.GetCellValue(sheet, cell)
						require.NoError(t, verr)
						if styleID != 0 || val != "" {
							maxRow = max(maxRow, r)
							maxCol = max(maxCol, c)
						}
					}
				}

				if adjusted {
					// treeAdjustment may park a bye above the deep chain, so the
					// render can end short of the envelope, never past it.
					assert.LessOrEqual(t, maxCol, TreePageLastCol(depth), "depth %d: used columns must stay inside the envelope", depth)
					assert.LessOrEqual(t, maxRow, TreePageLastRow(depth, startRow), "depth %d: used rows must stay inside the envelope", depth)
					// The root's bracket line pins the last column regardless.
					assert.Equal(t, TreePageLastCol(depth), maxCol, "depth %d: root bracket line fixes the last column", depth)
				} else {
					assert.Equal(t, TreePageLastCol(depth), maxCol, "depth %d: last used column", depth)
					assert.Equal(t, TreePageLastRow(depth, startRow), maxRow, "depth %d: last used row", depth)
				}
			})
		}
	}
}

// TestWriteTreeValue_ByeLeafLabelSpan pins the label styling of UNBALANCED
// trees. A bye leaf sits at a shallower level, so its value cell is one of the
// narrow bracket columns (E for one level up) instead of the wide label column
// C; styling only that cell rendered a stub underline+fill beneath the last
// few characters of the name. The style must span from column C to the value
// cell so every leaf label looks the same.
func TestWriteTreeValue_ByeLeafLabelSpan(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Tree 1"
	_, err := f.NewSheet(sheet)
	require.NoError(t, err)

	// 3 leaves: CreateBalancedTree splits [a | b,c], so "Pool A-1st" is a bye
	// leaf one level below the root, written at E6 (depth 3, startRow 4).
	tree := CreateBalancedTree([]string{"Pool A-1st", "Pool B-1st", "Pool C-1st"})
	depth := CalculateDepth(tree)
	startRow := TreeTitleRows + 1
	PrintLeafNodes(tree, f, sheet, 2*depth, startRow, depth, nil)

	byeVal, err := f.GetCellValue(sheet, "E6")
	require.NoError(t, err)
	require.Equal(t, "Pool A-1st", byeVal, "fixture: the bye leaf must sit at E6")

	byeStyle, err := f.GetCellStyle(sheet, "E6")
	require.NoError(t, err)
	require.NotZero(t, byeStyle)
	for _, cell := range []string{"C6", "D6"} {
		got, serr := f.GetCellStyle(sheet, cell)
		require.NoError(t, serr)
		assert.Equalf(t, byeStyle, got, "%s must carry the label style so the underline+fill spans the bye label", cell)
	}
	// The span must start at the label column, not bleed into the roster gutter.
	gutter, err := f.GetCellStyle(sheet, "B6")
	require.NoError(t, err)
	assert.Zero(t, gutter, "column B must stay unstyled")

	// A bottom-level leaf keeps its single-cell style (range C:C), unchanged.
	deepVal, err := f.GetCellValue(sheet, "C9")
	require.NoError(t, err)
	require.Equal(t, "Pool B-1st", deepVal, "fixture: a deep leaf must sit at C9")
}

// TestSetTreePageLayout verifies the page setup a tree page gets: a print area
// bounded to the given geometry and a fit-to-one-page-wide scaling, so a page
// never spills a near-blank second sheet of paper and a deep bracket shrinks
// instead of breaking mid-bracket.
func TestSetTreePageLayout(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	const sheet = "Tree 1"
	_, err := f.NewSheet(sheet)
	require.NoError(t, err)

	SetTreePageLayout(f, sheet, 3, 15)

	var area string
	for _, dn := range f.GetDefinedName() {
		if dn.Name == "_xlnm.Print_Area" && dn.Scope == sheet {
			area = dn.RefersTo
		}
	}
	assert.Equal(t, "'Tree 1'!$A$1:$G$15", area)

	layout, err := f.GetPageLayout(sheet)
	require.NoError(t, err)
	require.NotNil(t, layout.FitToWidth)
	assert.Equal(t, 1, *layout.FitToWidth, "page must scale to exactly one page wide")
}

// TestAssignMatchNumbers verifies that AssignMatchNumbers assigns sequential
// numbers starting at 1 and skips nil nodes. Each call restarts the counter
// from 1, overwriting any numbers a previous call assigned (not preserved).
func TestAssignMatchNumbers(t *testing.T) {
	t.Run("sequential numbering skips nil", func(t *testing.T) {
		n1 := &Node{LeafNode: false}
		n2 := &Node{LeafNode: false}
		n3 := &Node{LeafNode: false}
		n4 := &Node{LeafNode: false}

		rounds := [][]*Node{
			{n1, nil, n2}, // round 0: n1=1, nil skipped, n2=2
			{n3, n4},      // round 1: n3=3, n4=4
		}

		AssignMatchNumbers(rounds)

		assert.Equal(t, int64(1), n1.matchNum, "first non-nil node in round 0")
		assert.Equal(t, int64(2), n2.matchNum, "second non-nil node in round 0 (nil skipped)")
		assert.Equal(t, int64(3), n3.matchNum, "first node in round 1")
		assert.Equal(t, int64(4), n4.matchNum, "second node in round 1")
	})

	t.Run("all nil round", func(t *testing.T) {
		n1 := &Node{LeafNode: false}
		rounds := [][]*Node{
			{nil, nil},
			{n1},
		}

		AssignMatchNumbers(rounds)

		assert.Equal(t, int64(1), n1.matchNum, "first real node after all-nil round gets number 1")
	})

	t.Run("single match", func(t *testing.T) {
		n := &Node{LeafNode: false}
		rounds := [][]*Node{{n}}

		AssignMatchNumbers(rounds)

		assert.Equal(t, int64(1), n.matchNum)
	})

	t.Run("empty rounds", func(t *testing.T) {
		// Must not panic
		AssignMatchNumbers([][]*Node{})
		AssignMatchNumbers(nil)
	})
}
