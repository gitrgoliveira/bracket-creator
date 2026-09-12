package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestGetNameIDStyle(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	style := getNameIDStyle(f)
	assert.Positive(t, style, "expected positive style ID")
}

func TestGetNameIDPositionStyle(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	style := getNameIDPositionStyle(f)
	assert.Positive(t, style, "expected positive style ID")
}

// TestNameIDPositionStyle_ShrinkToFit pins bc-pnum A9's Names-to-Print half:
// a 4+ character competitor number (a prefix up to 3 characters, plus a
// multi-digit counter) used to clip at the cell edge, rasterising several
// distinct numbers ("KOR10".."KOR19") byte-identically in the reproduction.
// No width/font change is needed here, unlike the Tags sheet -- this column's
// width already comes from setupNamesToPrintLayout -- so ShrinkToFit alone
// is the fix.
func TestNameIDPositionStyle_ShrinkToFit(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	styleID := getNameIDPositionStyle(f)
	style, err := f.GetStyle(styleID)
	require.NoError(t, err)
	require.NotNil(t, style.Alignment)
	assert.True(t, style.Alignment.ShrinkToFit, "expected the Names-to-Print position style to set ShrinkToFit")
}

func TestGetTimeStyle(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	style := getTimeStyle(f)
	assert.Positive(t, style, "expected positive style ID")
}

func TestGetDurationStyle(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	style := getDurationStyle(f)
	assert.Positive(t, style, "expected positive style ID")
}

// TestNameIDStyle_ShrinkToFit pins the name half of the Names-to-Print ruling:
// the name column never changes size, so a long name must shrink into it.
func TestNameIDStyle_ShrinkToFit(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	style, err := f.GetStyle(buildNameIDStyle(f))
	require.NoError(t, err)
	require.NotNil(t, style.Alignment)
	assert.True(t, style.Alignment.ShrinkToFit, "expected the Names-to-Print name style to set ShrinkToFit")
	assert.False(t, style.Alignment.WrapText, "the name cell must shrink, never wrap")
}

// TestNamesToPrintColumnsFitOnePage pins the operator ruling that the number
// and name columns always sit side by side on one A3 landscape page: their
// fixed widths must not exceed what the page offers at the sheet's margins.
func TestNamesToPrintColumnsFitOnePage(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	const sheet = "Names to Print A"
	_, err := f.NewSheet(sheet)
	require.NoError(t, err)
	setupNamesToPrintLayout(f, sheet)
	a, err := f.GetColWidth(sheet, "A")
	require.NoError(t, err)
	b, err := f.GetColWidth(sheet, "B")
	require.NoError(t, err)
	assert.LessOrEqualf(t, a+b, float64(namesToPrintPageWidthUnits),
		"Names to Print columns A (%v) + B (%v) = %v exceed the %d units an A3 landscape page offers; the sheet would print numbers and names as separate page runs",
		a, b, a+b, namesToPrintPageWidthUnits)
	assert.Equal(t, float64(namesToPrintNumberColWidth), a, "column A width must be the fixed constant")
	assert.Equal(t, float64(namesToPrintNameColWidth), b, "column B width must be the fixed constant")
}
