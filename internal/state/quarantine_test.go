package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuarantineCompetitionFile(t *testing.T) {
	newComp := func(t *testing.T) (*Store, string) {
		t.Helper()
		dir := t.TempDir()
		s, err := NewStore(dir)
		require.NoError(t, err)
		require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))
		require.NoError(t, s.atomicWrite(s.compPath("c", "bracket.json"), []byte("broken{"), 0600))
		return s, dir
	}

	t.Run("renames rather than deletes, and the bytes survive", func(t *testing.T) {
		s, dir := newComp(t)
		name, err := s.QuarantineCompetitionFile("c", "bracket.json")
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(name, "bracket.json.corrupt-"), "got %q", name)

		_, statErr := os.Stat(s.compPath("c", "bracket.json"))
		assert.True(t, os.IsNotExist(statErr), "the original name is free again")

		kept, err := os.ReadFile(filepath.Join(dir, "competitions", "c", name)) // #nosec G304
		require.NoError(t, err)
		assert.Equal(t, "broken{", string(kept))
	})

	t.Run("a second quarantine in the same second does not overwrite the first", func(t *testing.T) {
		// The one thing this function exists to prevent. Second granularity
		// reads well to an operator but collides trivially, so the probe for a
		// free name is load-bearing rather than defensive.
		s, dir := newComp(t)
		first, err := s.QuarantineCompetitionFile("c", "bracket.json")
		require.NoError(t, err)
		require.NoError(t, s.atomicWrite(s.compPath("c", "bracket.json"), []byte("broken again{"), 0600))
		second, err := s.QuarantineCompetitionFile("c", "bracket.json")
		require.NoError(t, err)
		require.NotEqual(t, first, second)

		a, err := os.ReadFile(filepath.Join(dir, "competitions", "c", first)) // #nosec G304
		require.NoError(t, err)
		b, err := os.ReadFile(filepath.Join(dir, "competitions", "c", second)) // #nosec G304
		require.NoError(t, err)
		assert.Equal(t, "broken{", string(a), "the first copy is still intact")
		assert.Equal(t, "broken again{", string(b))
	})

	t.Run("only allowed artifacts, and no path traversal", func(t *testing.T) {
		s, _ := newComp(t)
		for _, name := range []string{"", "tournament.md", "../tournament.md", "competitions/c/bracket.json"} {
			_, err := s.QuarantineCompetitionFile("c", name)
			assert.Error(t, err, "filename %q must be refused", name)
		}
	})

	t.Run("a missing file is an error, not a silent success", func(t *testing.T) {
		// Unlike the idempotent delete: quarantining nothing means the caller's
		// premise (this file is broken) was already false.
		s, _ := newComp(t)
		_, err := s.QuarantineCompetitionFile("c", "pools.csv")
		require.Error(t, err)
		assert.True(t, os.IsNotExist(err), "got %v", err)
	})

	t.Run("moves the file version on, so derived caches invalidate", func(t *testing.T) {
		s, _ := newComp(t)
		before := s.FileVersion("c", "bracket.json")
		_, err := s.QuarantineCompetitionFile("c", "bracket.json")
		require.NoError(t, err)
		assert.Greater(t, s.FileVersion("c", "bracket.json"), before,
			"losing an artifact changes derived state exactly as a write does")
	})
}
