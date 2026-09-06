package helper

import (
	"testing"

	excelize "github.com/xuri/excelize/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateTagsSheetQR verifies that when publicURL and player.Number are set,
// AddPictureFromBytes is called and both tag copies (rows 1 and 2) contain an
// embedded picture.
func TestCreateTagsSheetQR(t *testing.T) {
	f := excelize.NewFile()
	pools := []Pool{
		{
			PoolName: "Pool A",
			Players: []Player{
				{Name: "Alice", PoolPosition: 1, Number: "K1", Dojo: "Dojo Alice"},
			},
		},
	}
	require.NoError(t, CreateTagsSheet(f, pools, "https://example.com", "K"))
	for _, cell := range []string{"A1", "A2"} {
		pics, err := f.GetPictures(SheetTags, cell)
		require.NoError(t, err)
		assert.NotEmptyf(t, pics, "expected QR picture in cell %s, got none", cell)
	}
}

func TestCreateTagsSheet(t *testing.T) {
	// 1. Setup
	f := excelize.NewFile()
	// bc-pnum G5/G6: the tag IS the competitor's Number now, with no
	// pool-letter fallback (CreateTagsSheet no longer composes one). Set
	// Number directly on the fixture, as NumberPools would have before
	// this sheet is ever built.
	pools := []Pool{
		{
			PoolName: "Pool A",
			Players: []Player{
				{Name: "Player 1", PoolPosition: 1, Dojo: "Dojo Player 1", Number: "K1"},
				{Name: "Player 2", PoolPosition: 2, Dojo: "Dojo Player 2", Number: "K2"},
				{Name: "Player 3", PoolPosition: 3, Dojo: "Dojo Player 3", Number: "K3"},
			},
		},
	}

	// 2. Execution
	require.NoError(t, CreateTagsSheet(f, pools, "", "K"))

	sheetName := SheetTags

	// 3. Verification - Page Layout (A4 portrait)
	opts, err := f.GetPageLayout(sheetName)
	require.NoError(t, err)
	require.NotNil(t, opts.Size)
	assert.Equal(t, 9, *opts.Size, "expected Page Size 9 (A4)")
	require.NotNil(t, opts.Orientation)
	assert.Equal(t, "portrait", *opts.Orientation)

	// 4. Verification - Row Height (409pt = excelize max, ~half A4 portrait)
	height, err := f.GetRowHeight(sheetName, 1)
	require.NoError(t, err)
	assert.Equal(t, float64(409), height)

	// 5. Verification - each tag appears twice consecutively (same A4 page)
	// Player 1 (K1): rows 1 and 2
	// Player 2 (K2): rows 3 and 4
	// Player 3 (K3): rows 5 and 6
	expected := map[string]string{
		"A1": "K1", "A2": "K1",
		"A3": "K2", "A4": "K2",
		"A5": "K3", "A6": "K3",
	}
	for cell, want := range expected {
		got, err := f.GetCellValue(sheetName, cell)
		require.NoError(t, err)
		assert.Equalf(t, want, got, "cell %s", cell)
	}
}

// TestCreateTagsSheet_ClippingFix pins bc-pnum A9: rendered with LibreOffice,
// a 4+ character tag (now reachable everywhere the prefix can be up to 3
// characters) used to be sheared at the page edge and spilled blank overflow
// pages. A unit test cannot rasterise a PDF, so this pins the things a unit
// test CAN see directly: the narrowed column width, a non-clipping style on
// the tag, and a print area confining the sheet to what was actually written
// (see the orchestrator's browser/LibreOffice render check for the
// rasterised confirmation). "KOR19" carries a three-letter prefix, so since
// the bc-pnum stacked-number ruling it takes the WRAP style (letters over
// digits) rather than the shrink style; a single-letter prefix's own
// clipping fix (ShrinkToFit) is pinned separately in
// TestCreateTagsSheet_StackedNumberLayout.
func TestCreateTagsSheet_ClippingFix(t *testing.T) {
	f := excelize.NewFile()
	pools := []Pool{
		{PoolName: "Pool A", Players: []Player{
			{Name: "Player 1", PoolPosition: 1, Dojo: "Dojo Player 1", Number: "KOR19"},
		}},
	}
	require.NoError(t, CreateTagsSheet(f, pools, "", "KOR"))

	width, err := f.GetColWidth(SheetTags, "A")
	require.NoError(t, err)
	assert.Equal(t, float64(88), width, "expected column A width 88 (fits the A4-portrait printable width)")

	styleID, err := f.GetCellStyle(SheetTags, "A1")
	require.NoError(t, err)
	style, err := f.GetStyle(styleID)
	require.NoError(t, err)
	require.NotNil(t, style.Alignment)
	assert.True(t, style.Alignment.WrapText, "expected the three-letter-prefix tag style to set WrapText")

	dn := f.GetDefinedName()
	found := false
	for _, d := range dn {
		if d.Name == "_xlnm.Print_Area" && d.Scope == SheetTags {
			found = true
			assert.Equal(t, "'Tags'!$A$1:$A$2", d.RefersTo, "one player -> 2 rows")
		}
	}
	assert.True(t, found, "expected a _xlnm.Print_Area defined name scoped to the Tags sheet")
}

// TestCreateTagsSheet_ZeroPlayersNoPrintArea pins a review finding on top of
// bc-pnum A9: with zero players (reachable -- an export of a mixed
// competition still in setup, before any pool has a member, or a
// competition with pools but every pool empty) the write loop never runs, so
// `row` stays at its initial 1 and row-1 is 0. SetPrintArea(f, sheet, 1, 0)
// would define the INVALID range "$A$1:$A$0" (row 0 does not exist). Must
// be skipped entirely, not merely tolerated: no players means nothing was
// written, so there is nothing to scope a print area to.
func TestCreateTagsSheet_ZeroPlayersNoPrintArea(t *testing.T) {
	f := excelize.NewFile()
	require.NoError(t, CreateTagsSheet(f, nil, "", "K"))
	for _, d := range f.GetDefinedName() {
		assert.Falsef(t, d.Name == "_xlnm.Print_Area" && d.Scope == SheetTags,
			"expected NO print area for a zero-player Tags sheet, got %q", d.RefersTo)
	}
}

// TestCreateTagsSheet_EmptyNumber pins D1 (bc-pnum): a player with no Number
// gets an EMPTY tag, never the pool-letter substitute ("A1", "A2", ...)
// CreateTagsSheet used to compose when Number was blank. That fallback has
// been removed outright (tag is now always exactly player.Number); if it
// were reintroduced, these cells would read "A1"/"A2" instead of "" and this
// test would go red. No prior test exercised the empty-Number path: every
// existing fixture set Number, so the fallback's removal was previously
// unpinned.
func TestCreateTagsSheet_EmptyNumber(t *testing.T) {
	f := excelize.NewFile()
	pools := []Pool{
		{
			PoolName: "Pool A",
			Players: []Player{
				{Name: "Player 1", PoolPosition: 1, Dojo: "Dojo Player 1"},
				{Name: "Player 2", PoolPosition: 2, Dojo: "Dojo Player 2"},
			},
		},
	}

	require.NoError(t, CreateTagsSheet(f, pools, "", "K"))

	sheetName := SheetTags
	for _, cell := range []string{"A1", "A2", "A3", "A4"} {
		got, err := f.GetCellValue(sheetName, cell)
		require.NoError(t, err)
		assert.Emptyf(t, got, "cell %s: no Number, no fallback", cell)
	}
}

// TestCreateTagsSheet_QRSitsBelowTheNumber pins the QR placement the bc-pnum
// review settled by rendering: the code sits in the bottom-left band of the
// tag, below the number, at 0.6 scale (about 3.2 cm on paper), in both the
// single-line and stacked number layouts. The old placement (OffsetX 57,
// OffsetY 242 at the number's vertical centre) assumed white space left of a
// short number; a shrink-to-fit "KOR20" fills the column and the code landed
// on its first letter.
func TestCreateTagsSheet_QRSitsBelowTheNumber(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	pools := []Pool{{PoolName: "Pool A", Players: []Player{{Name: "Alice", Dojo: "Seishin", Number: "KOR20"}}}}
	require.NoError(t, CreateTagsSheet(f, pools, "https://example.org/viewer", "KOR"))
	pics, err := f.GetPictures(SheetTags, "A1")
	require.NoError(t, err)
	require.Len(t, pics, 1, "expected one QR picture on the first tag")
	got := pics[0].Format
	require.NotNil(t, got, "QR picture has no graphic options")
	assert.Equal(t, 8, got.OffsetX, "QR must sit in the bottom-left band below the number")
	assert.Equal(t, 415, got.OffsetY, "QR must sit in the bottom-left band below the number")
	assert.Equal(t, float64(0.6), got.ScaleX, "QR scale must be 0.6 (about 3.2 cm on paper)")
	assert.Equal(t, float64(0.6), got.ScaleY, "QR scale must be 0.6 (about 3.2 cm on paper)")
}

// TestCreateTagsSheet_StackedNumberLayout pins the bc-pnum operator ruling: a
// competition number prefix of more than one CHARACTER prints as TWO stacked
// lines (letters over digits), decided from the competition's own
// numberPrefix argument, while a one-character prefix keeps the single-line
// layout and value.
func TestCreateTagsSheet_StackedNumberLayout(t *testing.T) {
	t.Run("two-letter prefix stacks", func(t *testing.T) {
		f := excelize.NewFile()
		defer func() { _ = f.Close() }()
		pools := []Pool{{PoolName: "Pool A", Players: []Player{
			{Name: "Alice", Dojo: "Seishin", Number: "KO20"},
			{Name: "Bob", Dojo: "Seishin", Number: "KO21"},
		}}}
		require.NoError(t, CreateTagsSheet(f, pools, "", "KO"))
		got, err := f.GetCellValue(SheetTags, "A1")
		require.NoError(t, err)
		assert.Equal(t, "KO\n20", got)

		styleID, err := f.GetCellStyle(SheetTags, "A1")
		require.NoError(t, err)
		style, err := f.GetStyle(styleID)
		require.NoError(t, err)
		require.NotNil(t, style.Alignment)
		assert.True(t, style.Alignment.WrapText, "expected the stacked style to set WrapText")
		assert.False(t, style.Alignment.ShrinkToFit, "expected the stacked style NOT to shrink to fit")
		assert.Equal(t, "top", style.Alignment.Vertical, "expected the stacked style to align top (free band for the QR)")
		require.NotNil(t, style.Font)
		assert.Equal(t, float64(160), style.Font.Size, "expected the stacked style at 160pt")

		// The SECOND player's own digits reach the cell too: the sheet-wide
		// decision is stacked, but each tag still carries its own number.
		got3, err := f.GetCellValue(SheetTags, "A3")
		require.NoError(t, err)
		assert.Equal(t, "KO\n21", got3)
	})

	t.Run("one-letter prefix stays single-line", func(t *testing.T) {
		f := excelize.NewFile()
		defer func() { _ = f.Close() }()
		pools := []Pool{{PoolName: "Pool A", Players: []Player{{Name: "Alice", Dojo: "Seishin", Number: "K20"}}}}
		require.NoError(t, CreateTagsSheet(f, pools, "", "K"))
		got, err := f.GetCellValue(SheetTags, "A1")
		require.NoError(t, err)
		assert.Equal(t, "K20", got)

		styleID, err := f.GetCellStyle(SheetTags, "A1")
		require.NoError(t, err)
		style, err := f.GetStyle(styleID)
		require.NoError(t, err)
		require.NotNil(t, style.Alignment)
		assert.False(t, style.Alignment.WrapText, "expected the single-line style NOT to set WrapText")
		assert.True(t, style.Alignment.ShrinkToFit, "expected the single-line style to shrink to fit")
		require.NotNil(t, style.Font)
		assert.Equal(t, float64(250), style.Font.Size, "expected the single-line style at 250pt")
	})

	// bc-pnum review: "Ö" is ONE character but two UTF-8 bytes, so a
	// byte-length "more than one letter" check wrongly stacked a one-letter
	// accented prefix. A rune count keeps this single-line, like "K20".
	t.Run("single accented-letter prefix stays single-line (rune count, not byte count)", func(t *testing.T) {
		f := excelize.NewFile()
		defer func() { _ = f.Close() }()
		pools := []Pool{{PoolName: "Pool A", Players: []Player{{Name: "Alice", Dojo: "Seishin", Number: "Ö20"}}}}
		require.NoError(t, CreateTagsSheet(f, pools, "", "Ö"))
		got, err := f.GetCellValue(SheetTags, "A1")
		require.NoError(t, err)
		assert.Equal(t, "Ö20", got)

		styleID, err := f.GetCellStyle(SheetTags, "A1")
		require.NoError(t, err)
		style, err := f.GetStyle(styleID)
		require.NoError(t, err)
		require.NotNil(t, style.Alignment)
		assert.False(t, style.Alignment.WrapText, "expected the single-line style NOT to set WrapText")
	})
}

// TestCreateTagsSheet_DigitBearingPrefixSplitsAtThePrefix pins bc-pnum
// review H1/H2: DefaultNumberPrefix can legitimately derive a digit-bearing
// prefix ("KO2", from DefaultNumberPrefix("Kendo Open", []string{"K","KO5"})
// -- see TestDefaultNumberPrefix_DigitBearingPrefix in numbers_test.go), and
// under it competitor 1's number is "KO21". The split must be driven by the
// SHEET's own known prefix ("KO2"), not guessed from the first ASCII digit
// in the number itself -- the old first-digit rule misread this as prefix
// "KO" over counter "21" (competitor 21 of "KO"), when the correct read is
// prefix "KO2" over counter "1".
func TestCreateTagsSheet_DigitBearingPrefixSplitsAtThePrefix(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	pools := []Pool{{PoolName: "Pool A", Players: []Player{{Name: "Alice", Dojo: "Seishin", Number: "KO21"}}}}
	require.NoError(t, CreateTagsSheet(f, pools, "", "KO2"))
	got, err := f.GetCellValue(SheetTags, "A1")
	require.NoError(t, err)
	assert.Equal(t, "KO2\n1", got, "must split at the sheet's own prefix (KO2/1), not the first digit (KO/21)")
}

// TestCreateTagsSheet_NumberNotCarryingPrefixStaysSingleLine pins the
// report-over-fabricate rule (D1, extended by bc-pnum review H1/H2 to the
// print split itself): a hand-edited or legacy number that does not
// actually start with the sheet's own prefix is never guessed at with a
// fabricated cut -- it renders whole, on one line, even though the sheet's
// prefix would otherwise call for a stacked layout.
func TestCreateTagsSheet_NumberNotCarryingPrefixStaysSingleLine(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	pools := []Pool{{PoolName: "Pool A", Players: []Player{{Name: "Alice", Dojo: "Seishin", Number: "XYZ"}}}}
	require.NoError(t, CreateTagsSheet(f, pools, "", "KO"))
	got, err := f.GetCellValue(SheetTags, "A1")
	require.NoError(t, err)
	assert.Equal(t, "XYZ", got, "a number not carrying the sheet's prefix must never be split")
}
