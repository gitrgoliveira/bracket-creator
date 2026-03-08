package helper

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
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
