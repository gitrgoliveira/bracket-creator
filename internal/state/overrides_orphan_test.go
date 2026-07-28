package state

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// saveOverridesLocked used to call os.MkdirAll on the competition directory
// before writing, so an override save that landed after DeleteCompetition
// recreated competitions/<id>/ containing nothing but overrides.json.
//
// That orphan is not merely clutter. ListCompetitions returns every directory
// under competitions/, so the deleted competition keeps showing up; and because
// IDs are deterministic name slugs, recreating a competition under the same
// name adopts the dead one's rank and winner overrides. That is the same
// delete-then-recreate hazard the version counters address, arriving by a
// different route.
//
// No competition directory is created by any other overrides path:
// atomicWriteFile opens its temp file with O_CREATE but never creates the
// parent, so removing the MkdirAll is what makes resurrection impossible.

func TestSaveOverridesDoesNotResurrectDeletedCompetition(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "deleted-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Deleted Comp"}))
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", "A1", 1))

	require.NoError(t, store.DeleteCompetition(compID))
	compDir := filepath.Join(dir, "competitions", compID)
	require.NoDirExists(t, compDir, "premise: the delete removed the directory")

	// The save must fail rather than rebuild the directory around its write.
	err = store.SaveRankOverride(compID, "Pool A", "A1", 2)
	assert.Error(t, err, "saving overrides for a deleted competition must fail, not recreate it")
	assert.NoDirExists(t, compDir,
		"an override save must never resurrect a deleted competition's directory")
}

// TestSaveOverridesConcurrentWithDeleteLeavesNoOrphan is the racing form. The
// sequential test above pins the common case; this one pins that no
// interleaving produces a half-competition either.
func TestSaveOverridesConcurrentWithDeleteLeavesNoOrphan(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const iterations = 200
	for i := 0; i < iterations; i++ {
		compID := "racer"
		require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Racer"}))

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = store.DeleteCompetition(compID)
		}()
		go func() {
			defer wg.Done()
			// Error is expected whenever the delete won; what must never
			// happen is a surviving directory without its config.
			_ = store.SaveRankOverride(compID, "Pool A", "A1", 1)
		}()
		wg.Wait()

		compDir := filepath.Join(dir, "competitions", compID)
		if _, statErr := os.Stat(compDir); statErr == nil {
			// The competition survived, which is a legitimate outcome when the
			// save won the race. It must then be a WHOLE competition.
			require.FileExists(t, filepath.Join(compDir, "config.md"),
				"iteration %d: competition directory survived without its config.md", i)
			require.NoError(t, os.RemoveAll(compDir))
		}
	}
}
