package helper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSeedsFile(t *testing.T) {
	// Our test fixtures use these exact paths
	validFile := filepath.Join("..", "..", "tests", "fixtures", "winners.csv")
	duplicateFile := filepath.Join("..", "..", "tests", "fixtures", "winners_duplicate_rank.csv")
	invalidHeaderFile := filepath.Join("..", "..", "tests", "fixtures", "winners_invalid_header.csv")

	t.Run("ValidFile", func(t *testing.T) {
		assignments, err := ParseSeedsFile(validFile)
		assert.NoError(t, err)
		assert.Len(t, assignments, 2)
		assert.Equal(t, 1, assignments[0].SeedRank)
		assert.Equal(t, "Jane Doe", assignments[0].Name)
	})

	t.Run("DuplicateRank", func(t *testing.T) {
		_, err := ParseSeedsFile(duplicateFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate seed rank detected")
	})

	t.Run("InvalidHeader", func(t *testing.T) {
		_, err := ParseSeedsFile(invalidHeaderFile)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing Rank or Name headers")
	})

	t.Run("FileNotExists", func(t *testing.T) {
		_, err := ParseSeedsFile("non_existent.csv")
		assert.Error(t, err)
	})
}

// ReadSeedsFileRaw locates every column by header name, so a file written
// before the ID column existed must still parse -- with an empty id, exactly
// as it would for any other absent column -- and a file carrying the column
// must read it back. Both byte strings are pinned verbatim (not built from
// domain.SeedAssignment structs) because that is literally what is on disk
// in each case: a legacy file this exact test would silently stop covering
// if the fixture were generated from the current writer instead.
func TestReadSeedsFileRaw_IDColumn(t *testing.T) {
	dir := t.TempDir()

	t.Run("legacy file with no ID column parses with an empty id", func(t *testing.T) {
		legacy := "Rank,Name,Dojo\n1,Alice,Wakaba\n2,Bob,Wakaba\n"
		path := filepath.Join(dir, "legacy-seeds.csv")
		require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))

		assignments, err := ReadSeedsFileRaw(path)
		require.NoError(t, err)
		require.Len(t, assignments, 2)
		assert.Equal(t, "", assignments[0].ID)
		assert.Equal(t, "Alice", assignments[0].Name)
		assert.Equal(t, "", assignments[1].ID)
	})

	t.Run("current file with an ID column reads the id", func(t *testing.T) {
		current := "Rank,Name,Dojo,ID\n1,Alice,Wakaba,11111111-1111-1111-1111-111111111111\n2,Bob,Wakaba,\n"
		path := filepath.Join(dir, "current-seeds.csv")
		require.NoError(t, os.WriteFile(path, []byte(current), 0o600))

		assignments, err := ReadSeedsFileRaw(path)
		require.NoError(t, err)
		require.Len(t, assignments, 2)
		assert.Equal(t, "11111111-1111-1111-1111-111111111111", assignments[0].ID)
		assert.Equal(t, "", assignments[1].ID, "an empty ID cell must not be confused with a missing column")
	})
}
