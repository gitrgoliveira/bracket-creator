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

	// Minimal 3-player pool — small enough to enumerate every cell by hand,
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

	AddPoolsToTree(f, sheetName, pools, poolCoords, pCoords)

	startRow := TreeTitleRows + 1 // first content row in column A

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

			SetTreeSheetTitle(f, "Tree 1", tt.title)

			formula, err := f.GetCellFormula("Tree 1", "A1")
			require.NoError(t, err)
			assert.Equal(t, tt.expectedFormula, formula)
		})
	}
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
