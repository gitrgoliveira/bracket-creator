package cmd

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// TestExampleWorkbooks_ColumnsHoldTheirLongestText pins the column widths that
// were measured against the committed examples' longest text in Arial (the
// tree's qualifier labels, the Elimination Matches names with their source, and
// the data sheet's labels and display names), so a later narrowing that would
// cut them off again is seen here rather than on paper.
func TestExampleWorkbooks_ColumnsHoldTheirLongestText(t *testing.T) {
	f, err := excelize.OpenFile(filepath.Join("..", "pools-example-medium.xlsx"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	for _, tc := range []struct {
		sheet, col string
		want       float64
	}{
		{"Tree 1", "B", 4},
		{"Tree 1", "C", 33},
		{helper.SheetEliminationMatches, "A", 34},
		{helper.SheetEliminationMatches, "G", 34},
		{helper.SheetEliminationMatches, "I", 34},
		{helper.SheetEliminationMatches, "O", 34},
		{helper.SheetPoolMatches, "A", 30},
		{helper.SheetPoolMatches, "G", 30},
		{helper.SheetData, "A", 10.5},
		{helper.SheetData, "D", 15},
		{helper.SheetData, "E", 15},
		{helper.SheetData, "F", 12},
	} {
		got, err := f.GetColWidth(tc.sheet, tc.col)
		require.NoError(t, err)
		assert.InDelta(t, tc.want, got, 0.01, "%s column %s", tc.sheet, tc.col)
	}
}
