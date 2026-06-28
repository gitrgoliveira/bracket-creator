package pdf

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- SheetRanges ---

func TestSheetRanges_NonExistent(t *testing.T) {
	_, err := SheetRanges("/nonexistent-pdf-path-xyz.pdf")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "open pdf")
}

func TestSheetRanges_InvalidPDF(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-pdf-*.pdf")
	require.NoError(t, err)
	_, werr := f.WriteString("this is not a valid PDF file")
	require.NoError(t, werr)
	require.NoError(t, f.Close())

	_, err = SheetRanges(f.Name())
	// pdfcpu returns an error when the file is not a valid PDF
	assert.Error(t, err)
}

// --- PageCount ---

func TestPageCount_NonExistent(t *testing.T) {
	_, err := PageCount("/nonexistent-pdf-path-xyz.pdf")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "open pdf")
}

func TestPageCount_InvalidPDF(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-pdf-*.pdf")
	require.NoError(t, err)
	_, werr := f.WriteString("this is not a valid PDF file")
	require.NoError(t, werr)
	require.NoError(t, f.Close())

	_, err = PageCount(f.Name())
	assert.Error(t, err)
}

// --- ExtractPages ---

func TestExtractPages_EmptyRanges(t *testing.T) {
	err := ExtractPages("/any/path.pdf", []SheetRange{}, filepath.Join(t.TempDir(), "out.pdf"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no page ranges")
}

func TestExtractPages_NonExistentSrc(t *testing.T) {
	ranges := []SheetRange{{Sheet: "Sheet1", PageFrom: 1, PageThru: 1}}
	err := ExtractPages("/nonexistent-src.pdf", ranges, filepath.Join(t.TempDir(), "out.pdf"))
	assert.Error(t, err)
}

// --- MergePDFs (non-empty list that still fails) ---

func TestMergePDFs_NonExistentFiles(t *testing.T) {
	err := MergePDFs([]string{"/nonexistent-a.pdf", "/nonexistent-b.pdf"}, filepath.Join(t.TempDir(), "out.pdf"))
	assert.Error(t, err)
}

// --- StampPageNumbers ---

func TestStampPageNumbers_NonExistent(t *testing.T) {
	err := StampPageNumbers("/nonexistent-pdf.pdf", filepath.Join(t.TempDir(), "out.pdf"))
	assert.Error(t, err)
}

// --- generate: outDir not creatable ---

func TestGenerate_OutDirNotCreatable(t *testing.T) {
	// Create a regular file and pass its path as outDir.
	// os.MkdirAll fails with ENOTDIR when the path already exists as a file,
	// which is deterministic even when tests run as root.
	f, err := os.CreateTemp(t.TempDir(), "not-a-dir-*")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	g := &Generator{}
	sources := []SourceWorkbook{{Path: "dummy.xlsx"}}
	_, err = g.generate(context.Background(), nil, sources, f.Name())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

// --- makeTitlePage: all-non-word title triggers safe=="title" fallback; write to bad dir errors ---

func TestMakeTitlePage_EmptyTitleFallbackWriteError(t *testing.T) {
	c := &Converter{sofficePath: "/fake/soffice"}
	// Title "---" → after ReplaceAllString → "_" → after Trim → "" → safe = "title"
	_, err := c.makeTitlePage(context.Background(), "---", false, "/nonexistent-dir-xyz", "uid1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write title html")
}
