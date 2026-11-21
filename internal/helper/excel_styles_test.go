package helper

import (
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

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

// TestCompetitionTagFit pins the competition-name tag's sizing rule, measured
// in LibreOffice (see competitionTagFitConstant): one line shrunk to fit from
// 110pt up to 45 characters, then wrapped at 370/sqrt(characters) points
// rounded down, counting characters, not bytes. A change to either constant
// must be measured again, not just made to pass here.
func TestCompetitionTagFit(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		wrap bool
		size float64
	}{
		{"A", false, 110},
		{strings.Repeat("x", 39), false, 110},
		{strings.Repeat("W", 45), false, 110},
		{strings.Repeat("Ö", 45), false, 110}, // 90 bytes, 45 characters
		{strings.Repeat("W", 46), true, 54},
		{strings.Repeat("x", 60), true, 47},
		{strings.Repeat("x", 100), true, 37},
		{strings.Repeat("x", 200), true, 26},
		{strings.Repeat("x", 1000), true, 11},
	} {
		wrap, size := competitionTagFit(tc.tag)
		assert.Equalf(t, tc.wrap, wrap, "%d characters wraps", len([]rune(tc.tag)))
		assert.Equalf(t, tc.size, size, "%d characters size", len([]rune(tc.tag)))
	}

	// Longer never prints larger: the measured sizes hold because each length
	// gets no more than a shorter one did.
	_, prev := competitionTagFit(strings.Repeat("x", competitionTagOneLineMaxRunes+1))
	for n := competitionTagOneLineMaxRunes + 2; n <= 2000; n++ {
		_, size := competitionTagFit(strings.Repeat("x", n))
		require.LessOrEqualf(t, size, prev, "%d characters", n)
		require.Positivef(t, size, "%d characters", n)
		prev = size
	}
}

// TestGetCompetitionTagStyle pins the tag style: the competitor name's font
// and border, shrinking on one line or wrapping at the fitted size, and one
// cached style per size, so names of one size share it.
func TestGetCompetitionTagStyle(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	short := getCompetitionTagStyle(f, "Men's Individual")
	assert.Equal(t, short, getCompetitionTagStyle(f, "Women's Team"), "one-line tags share one style")
	style, err := f.GetStyle(short)
	require.NoError(t, err)
	assert.True(t, style.Alignment.ShrinkToFit)
	assert.False(t, style.Alignment.WrapText)
	assert.Equal(t, "center", style.Alignment.Horizontal)
	assert.Equal(t, "center", style.Alignment.Vertical)
	assert.Equal(t, "Calibri", style.Font.Family)
	assert.Equal(t, 110.0, style.Font.Size)
	assert.Len(t, style.Border, 4)

	long := getCompetitionTagStyle(f, strings.Repeat("x", 100))
	assert.Equal(t, long, getCompetitionTagStyle(f, strings.Repeat("y", 100)), "wrapped tags of one size share one style")
	assert.NotEqual(t, short, long)
	assert.NotEqual(t, long, getCompetitionTagStyle(f, strings.Repeat("x", 200)), "another size is another style")
	style, err = f.GetStyle(long)
	require.NoError(t, err)
	assert.True(t, style.Alignment.WrapText)
	assert.False(t, style.Alignment.ShrinkToFit)
	assert.Equal(t, 37.0, style.Font.Size)
}

// styleCacheHolds reports whether the style cache still has an entry for the
// workbook fileKey names.
func styleCacheHolds(fileKey weak.Pointer[excelize.File]) bool {
	styleCacheMu.Lock()
	defer styleCacheMu.Unlock()
	_, ok := styleCacheByFile[fileKey]
	return ok
}

// styleOneWorkbook styles a fresh workbook and returns only a weak pointer to
// it, so nothing on the caller's stack keeps the workbook reachable.
//
//go:noinline
func styleOneWorkbook(t *testing.T) weak.Pointer[excelize.File] {
	f := excelize.NewFile()
	require.Positive(t, getTextStyle(f))
	require.NoError(t, f.Close())
	return weak.Make(f)
}

// TestStyleCacheReleasesACollectedWorkbook pins that the style cache never
// keeps a workbook alive: once a styled workbook is unreachable its entry
// goes, with no release call from any caller. The long-running app styles
// one workbook per export. Collection is asynchronous, so this polls GC up to
// a deadline rather than trusting a single cycle.
func TestStyleCacheReleasesACollectedWorkbook(t *testing.T) {
	fileKey := styleOneWorkbook(t)
	require.True(t, styleCacheHolds(fileKey), "styling a workbook caches its style ids")

	deadline := time.Now().Add(5 * time.Second)
	for styleCacheHolds(fileKey) && time.Now().Before(deadline) {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, fileKey.Value() == nil, "the workbook must have been collected")
	assert.False(t, styleCacheHolds(fileKey), "its cache entry must go with it")
}
