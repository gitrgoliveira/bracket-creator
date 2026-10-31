package pdf

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTitleOrStem(t *testing.T) {
	t.Run("uses Title when set", func(t *testing.T) {
		s := SourceWorkbook{Path: "/some/path/file.xlsx", Title: "My Tournament"}
		assert.Equal(t, "My Tournament", s.titleOrStem())
	})

	t.Run("falls back to filename stem when Title is empty", func(t *testing.T) {
		s := SourceWorkbook{Path: "/some/path/individual-men.xlsx"}
		assert.Equal(t, "individual-men", s.titleOrStem())
	})
}

func TestRangeFor(t *testing.T) {
	conv := converted{
		ranges: []SheetRange{
			{Sheet: "data", PageFrom: 1, PageThru: 2},
			{Sheet: "Pool Draw", PageFrom: 3, PageThru: 5},
		},
	}

	t.Run("found", func(t *testing.T) {
		r, ok := conv.rangeFor("Pool Draw")
		assert.True(t, ok)
		assert.Equal(t, 3, r.PageFrom)
		assert.Equal(t, 5, r.PageThru)
	})

	t.Run("not found", func(t *testing.T) {
		_, ok := conv.rangeFor("Missing Sheet")
		assert.False(t, ok)
	})
}

func TestPublishAtomic(t *testing.T) {
	t.Run("copies src to dst atomically", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "src.pdf")
		require.NoError(t, os.WriteFile(src, []byte("pdf-content"), 0o600))

		outDir := t.TempDir()
		dst := filepath.Join(outDir, "output.pdf")

		require.NoError(t, publishAtomic(src, dst))

		got, err := os.ReadFile(dst) // #nosec G304, test-only temp path
		require.NoError(t, err)
		assert.Equal(t, "pdf-content", string(got))

		// No stray temp files.
		entries, err := os.ReadDir(outDir)
		require.NoError(t, err)
		for _, e := range entries {
			assert.NotContains(t, e.Name(), ".tmp")
		}
	})

	t.Run("overwrites existing dst", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "new.pdf")
		require.NoError(t, os.WriteFile(src, []byte("new-content"), 0o600))

		outDir := t.TempDir()
		dst := filepath.Join(outDir, "output.pdf")
		require.NoError(t, os.WriteFile(dst, []byte("old-content"), 0o600))

		require.NoError(t, publishAtomic(src, dst))

		got, err := os.ReadFile(dst) // #nosec G304, test-only temp path
		require.NoError(t, err)
		assert.Equal(t, "new-content", string(got))
	})

	t.Run("errors when src does not exist", func(t *testing.T) {
		err := publishAtomic("/nonexistent/src.pdf", filepath.Join(t.TempDir(), "dst.pdf"))
		assert.Error(t, err)
	})

	t.Run("errors when dst directory does not exist", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "src.pdf")
		require.NoError(t, os.WriteFile(src, []byte("pdf-content"), 0o600))
		err := publishAtomic(src, "/nonexistent-dir-xyz/output.pdf")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "create temp output")
	})

	// noTempLeft asserts a failed publish took its temp file away.
	noTempLeft := func(t *testing.T, dir string) {
		t.Helper()
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		for _, e := range entries {
			assert.NotContains(t, e.Name(), ".tmp")
		}
	}

	t.Run("a failed copy removes the temp file", func(t *testing.T) {
		// A directory opens but cannot be read as a file.
		src := t.TempDir()
		outDir := t.TempDir()
		err := publishAtomic(src, filepath.Join(outDir, "output.pdf"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "copy generated pdf")
		noTempLeft(t, outDir)
	})

	t.Run("a failed publish removes the temp file", func(t *testing.T) {
		src := filepath.Join(t.TempDir(), "src.pdf")
		require.NoError(t, os.WriteFile(src, []byte("pdf-content"), 0o600))
		outDir := t.TempDir()
		// A non-empty directory at dst can be neither renamed onto nor removed.
		dst := filepath.Join(outDir, "output.pdf")
		require.NoError(t, os.MkdirAll(filepath.Join(dst, "child"), 0o700))
		err := publishAtomic(src, dst)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "publish output")
		noTempLeft(t, outDir)
	})
}

func TestMergePDFs_EmptyList(t *testing.T) {
	err := MergePDFs(context.Background(), []string{}, filepath.Join(t.TempDir(), "out.pdf"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no PDFs to merge")
}

func TestGenerateGroups_UnknownType(t *testing.T) {
	// Constructing a Generator requires soffice; check the unknown-type
	// validation path via a direct call on a zero-value Generator. The
	// function returns before touching the converter.
	g := &Generator{}
	_, err := g.GenerateGroups(context.TODO(), []string{"nonexistent-type"}, nil, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown PDF type")
}

func TestGenerate_NoSources(t *testing.T) {
	g := &Generator{}
	_, err := g.generate(context.TODO(), nil, nil, t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no source workbooks")
}
