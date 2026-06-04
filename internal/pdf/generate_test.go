package pdf

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateAllProducesGroupedPDFs(t *testing.T) {
	requireSoffice(t)
	xlsx := findExampleXLSX(t)
	outDir := t.TempDir()

	gen, err := NewGenerator()
	require.NoError(t, err)

	sources := []SourceWorkbook{
		{Path: xlsx, Title: "Example Individual Tournament"},
	}
	out, err := gen.GenerateAll(context.Background(), sources, outDir)
	require.NoError(t, err)

	// The example workbook has data/Pool Draw/Tree/etc, so all five groups
	// should produce output.
	for _, typ := range []string{"registration", "names", "tags", "pools-trees", "full-bracket"} {
		path, ok := out[typ]
		require.True(t, ok, "group %q should be produced", typ)
		require.FileExists(t, path)
		n, err := PageCount(path)
		require.NoError(t, err)
		assert.Positive(t, n, "group %q PDF should have pages", typ)
	}

	// No stray temp files left at the destination.
	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no temp artifacts should remain")
	}
}

func TestGenerateTagsSkipsTeamOnlySources(t *testing.T) {
	requireSoffice(t)
	xlsx := findExampleXLSX(t)
	outDir := t.TempDir()

	gen, err := NewGenerator()
	require.NoError(t, err)

	// A single team workbook: the tags group must be skipped entirely.
	sources := []SourceWorkbook{
		{Path: xlsx, Title: "Teams", IsTeam: true},
	}
	out, err := gen.GenerateGroups(context.Background(), []string{"tags"}, sources, outDir)
	require.NoError(t, err)

	_, ok := out["tags"]
	assert.False(t, ok, "tags group must be skipped when only team workbooks are present")
	assert.NoFileExists(t, filepath.Join(outDir, "print_tags.pdf"))
}

func TestGenerateGroupsUnknownType(t *testing.T) {
	requireSoffice(t)
	gen, err := NewGenerator()
	require.NoError(t, err)
	_, err = gen.GenerateGroups(context.Background(), []string{"nonsense"}, []SourceWorkbook{{Path: "x"}}, t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown PDF type")
}
