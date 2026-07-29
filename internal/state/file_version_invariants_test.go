package state

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The version counter is only a sound cache-validity token while EVERY path
// that changes a file's effective content advances it. Two such paths carry
// nothing but a comment saying so, which a refactor can delete without failing
// a single test: the WAL abort in invalidateCachesForWALIntents, and
// DeleteCompetitionFile. These tests pin both.

func TestFileVersionAdvancesOnTransactionAbort(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "aborted"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Aborted"}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: MatchStatusScheduled},
	}))

	// The savers publish staged results into the file cache before the commit,
	// so an abort leaves that cache holding data that never reached disk. Any
	// downstream cache keyed on the version must be forced to recompute.
	//
	// The baseline is sampled INSIDE the closure, after the staged save. A
	// baseline taken before WithTransaction would prove nothing: tx.SavePoolMatches
	// bumps the counter itself, so the version has already moved by the time the
	// abort runs and the assertion would hold with the abort's bump deleted.
	// FileVersion reads a sync.Map and takes no per-competition lock, so sampling
	// it while the transaction holds that lock is safe.
	var staged uint64
	sentinel := errors.New("roll it back")
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SavePoolMatches(compID, []MatchResult{
			{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", Status: MatchStatusCompleted},
		}); err != nil {
			return err
		}
		staged = store.FileVersion(compID, "pool-matches.csv")
		return sentinel
	})
	require.ErrorIs(t, txErr, sentinel)
	require.Positive(t, staged, "premise: the staged save inside the transaction moved the counter")

	assert.Greater(t, store.FileVersion(compID, "pool-matches.csv"), staged,
		"the abort itself must advance the version again: it discards a staged write that was already published to the file cache")

	// And the rollback must be observable, not merely token-deep.
	got, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Empty(t, got[0].Winner, "the aborted winner must not survive the rollback")
}

func TestFileVersionAdvancesOnDeleteCompetitionFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "discarded"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Discarded"}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: MatchStatusScheduled},
	}))

	before := store.FileVersion(compID, "pool-matches.csv")

	require.NoError(t, store.DeleteCompetitionFile(compID, "pool-matches.csv"))

	assert.Greater(t, store.FileVersion(compID, "pool-matches.csv"), before,
		"discarding a draw artifact changes derived state just as a write does, so the version must advance")
}
