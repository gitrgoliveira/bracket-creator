package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// The bronze prints in ITS shiaijo's band like any other bout, which means
// PrintBronzeBlockWithPrintArea has to turn a court NAME into a band INDEX. All
// three answers it can reach are pinned here, because the arithmetic underneath
// (1 + band*CourtsColumnsPerCourt) turns a wrong index into a wrong column and a
// negative one into a column excelize cannot name at all.
func TestBronzeBlockBandSelection(t *testing.T) {
	t.Parallel()

	// The block's own left-hand column, read back off the sheet: the "Round N"
	// header cell PrintThirdPlaceBlock merges is the widest thing it writes, so
	// the merge's start column is the band it landed in.
	bandStartColumn := func(t *testing.T, bands []string, bronzeCourt string) int {
		t.Helper()
		f := excelize.NewFile()
		defer func() { require.NoError(t, f.Close()) }()
		_, err := f.NewSheet(SheetEliminationMatches)
		require.NoError(t, err)

		PrintBronzeBlockWithPrintArea(f, 2, 0, false, false, bands, bronzeCourt, nil, nil)

		merged, err := f.GetMergeCells(SheetEliminationMatches)
		require.NoError(t, err)
		require.NotEmpty(t, merged, "the bronze block writes at least one merged header")
		col, _, err := excelize.CellNameToCoordinates(merged[0].GetStartAxis())
		require.NoError(t, err)
		return col
	}

	colOfBand := func(i int) int { return 1 + i*CourtsColumnsPerCourt }

	t.Run("a recorded court takes its own band", func(t *testing.T) {
		assert.Equal(t, colOfBand(2), bandStartColumn(t, []string{"A", "B", "C"}, "C"),
			"a bronze moved to the third shiaijo must print under that shiaijo's header, not the leftmost")
	})

	t.Run("no court recorded takes the first band", func(t *testing.T) {
		// The CLI's case: a blank workbook with no stored bracket behind it,
		// so there is no live assignment to follow.
		assert.Equal(t, colOfBand(0), bandStartColumn(t, []string{"A", "B", "C"}, ""))
	})

	t.Run("a court absent from the bands takes the last one", func(t *testing.T) {
		// Kept distinct from the no-court case on purpose: falling back to the
		// leftmost band is what once printed the bronze under another shiaijo's
		// header, and it looks identical to the legitimate CLI answer.
		assert.Equal(t, colOfBand(1), bandStartColumn(t, []string{"A", "B"}, "D"))
	})

	t.Run("no bands at all still writes a real column", func(t *testing.T) {
		// Unreachable from the two production callers (usedCourtBands never
		// returns an empty set), but this is exported: without the length guard
		// "the last band" is index -1, which asks excelize for column -7.
		assert.Equal(t, colOfBand(0), bandStartColumn(t, nil, "D"))
	})
}
