package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// TestAddPoolsToSheetCellContent renders a small 2-player pool to the Pool
// Draw sheet and reads back specific landmark cells to assert the layout
// matches the documented Pool Draw conventions (B-column-first placement,
// header at row 5, pool name as a sheet-cross-reference formula).
//
// Companion to TestAddPoolsToSheet in excel_data_test.go: that test focuses on
// formula equality across many shapes; this one pins the concrete cell
// coordinates so a layout change (start row, start column, step) trips the
// test before downstream rendering breaks.
func TestAddPoolsToSheetCellContent(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	_, err := f.NewSheet(SheetPoolDraw)
	require.NoError(t, err)
	_, err = f.NewSheet(SheetData)
	require.NoError(t, err)

	// Minimal 2-player pool. The pool name and player names are pinned in the
	// data sheet at known cells so we can assert the cross-sheet references in
	// the Pool Draw sheet.
	players := []Player{
		{Name: "Alice", PoolPosition: 1},
		{Name: "Bob", PoolPosition: 2},
	}
	pools := []Pool{{PoolName: "Pool A", Players: players}}

	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetData, cell: "$A$2"},
	}
	playerCoords := map[string]playerCellCoord{
		playerCoordKey(players[0]): {cellCoord: cellCoord{sheetName: SheetData, cell: "$B$2"}},
		playerCoordKey(players[1]): {cellCoord: cellCoord{sheetName: SheetData, cell: "$B$3"}},
	}

	require.NoError(t, AddPoolsToSheet(f, pools, poolCoords, playerCoords, false))

	t.Run("title formula at B2", func(t *testing.T) {
		got, err := f.GetCellFormula(SheetPoolDraw, "B2")
		require.NoError(t, err)
		want := `IF(data!$B$1="","Tournament Pools",data!$B$1&" - Tournament Pools")`
		assert.Equal(t, strings.ReplaceAll(want, "'", ""), strings.ReplaceAll(got, "'", ""))
	})

	t.Run("pool header lands at B5", func(t *testing.T) {
		// Layout: startRow=5, startCol=B. Single pool goes in column B.
		got, err := f.GetCellFormula(SheetPoolDraw, "B5")
		require.NoError(t, err)
		want := fmt.Sprintf("%s!%s", SheetData, "$A$2")
		assert.Equal(t, strings.ReplaceAll(want, "'", ""), strings.ReplaceAll(got, "'", ""))
	})

	t.Run("first player lands at B6", func(t *testing.T) {
		got, err := f.GetCellFormula(SheetPoolDraw, "B6")
		require.NoError(t, err)
		want := fmt.Sprintf("\"%d. \" & %s!%s", players[0].PoolPosition, SheetData, "$B$2")
		assert.Equal(t, strings.ReplaceAll(want, "'", ""), strings.ReplaceAll(got, "'", ""))
	})

	t.Run("second player lands at B7", func(t *testing.T) {
		got, err := f.GetCellFormula(SheetPoolDraw, "B7")
		require.NoError(t, err)
		want := fmt.Sprintf("\"%d. \" & %s!%s", players[1].PoolPosition, SheetData, "$B$3")
		assert.Equal(t, strings.ReplaceAll(want, "'", ""), strings.ReplaceAll(got, "'", ""))
	})
}
