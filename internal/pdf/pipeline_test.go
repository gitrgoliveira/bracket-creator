package pdf

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireSoffice skips the test when LibreOffice is unavailable, with a clear
// message. PDF tests are environment-dependent; CI runs them in the -pdf image.
func requireSoffice(t *testing.T) *Converter {
	t.Helper()
	c, err := NewConverter()
	if err != nil {
		t.Skipf("skipping: %v (install LibreOffice or run in the -pdf image)", err)
	}
	return c
}

// findExampleXLSX returns a pools example workbook from the repo root, or skips.
func findExampleXLSX(t *testing.T) string {
	t.Helper()
	// internal/pdf -> repo root is two levels up.
	candidate := filepath.Join("..", "..", "pools-example-medium.xlsx")
	if _, err := os.Stat(candidate); err != nil {
		t.Skipf("example workbook not found at %s: %v", candidate, err)
	}
	abs, err := filepath.Abs(candidate)
	require.NoError(t, err)
	return abs
}

func TestConvertAndReadRanges(t *testing.T) {
	conv := requireSoffice(t)
	xlsx := findExampleXLSX(t)
	tmp := t.TempDir()

	pdfPath, err := conv.ConvertToPDF(context.Background(), xlsx, tmp)
	require.NoError(t, err)
	require.FileExists(t, pdfPath)

	ranges, err := SheetRanges(pdfPath)
	require.NoError(t, err)
	require.NotEmpty(t, ranges)

	total, err := PageCount(pdfPath)
	require.NoError(t, err)

	byName := map[string]SheetRange{}
	for _, r := range ranges {
		byName[r.Sheet] = r
		// Spike-found invariant: every range must be bounded and ordered.
		assert.GreaterOrEqual(t, r.PageThru, r.PageFrom, "sheet %q range must be bounded (last-bookmark patch)", r.Sheet)
		assert.LessOrEqual(t, r.PageThru, total, "sheet %q must not exceed total pages", r.Sheet)
	}

	// The example workbook must contain the canonical bracket sheets.
	for _, want := range []string{"data", "Pool Draw"} {
		_, ok := byName[want]
		assert.True(t, ok, "expected sheet %q in %v", want, keysOf(byName))
	}

	// The LAST range is the one pdfcpu leaves with PageThru==0; assert it was
	// patched to reach the final page.
	last := ranges[len(ranges)-1]
	assert.Equal(t, total, last.PageThru, "final sheet %q must extend to last page", last.Sheet)
}

func TestExtractAndMerge(t *testing.T) {
	conv := requireSoffice(t)
	xlsx := findExampleXLSX(t)
	tmp := t.TempDir()

	pdfPath, err := conv.ConvertToPDF(context.Background(), xlsx, tmp)
	require.NoError(t, err)

	ranges, err := SheetRanges(pdfPath)
	require.NoError(t, err)

	g, _ := GroupByType("pools-trees")
	wanted := g.resolveSheets(sheetNames(ranges))
	require.NotEmpty(t, wanted)

	var picked []SheetRange
	wantPages := 0
	for _, r := range ranges {
		for _, w := range wanted {
			if r.Sheet == w {
				picked = append(picked, r)
				wantPages += r.PageThru - r.PageFrom + 1
			}
		}
	}
	require.NotEmpty(t, picked)

	extracted := filepath.Join(tmp, "extracted.pdf")
	require.NoError(t, ExtractPages(pdfPath, picked, extracted))
	gotPages, err := PageCount(extracted)
	require.NoError(t, err)
	assert.Equal(t, wantPages, gotPages, "extracted page count must equal sum of picked ranges")

	merged := filepath.Join(tmp, "merged.pdf")
	require.NoError(t, MergePDFs([]string{extracted, extracted}, merged))
	mergedPages, err := PageCount(merged)
	require.NoError(t, err)
	assert.Equal(t, 2*gotPages, mergedPages, "merging a PDF with itself doubles the page count")
}

func keysOf(m map[string]SheetRange) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sheetNames(rs []SheetRange) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Sheet
	}
	return out
}
