package state

import (
	"errors"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pools.csv is a staged file of a store transaction, exactly like
// pool-matches.csv: a transaction that saves pools and then fails leaves the
// file and every reader as they were (operator decision 2026-11-12).

func poolsTxFixture(t *testing.T) (*Store, string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID = "pools-tx"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Pools Tx"}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{{
		PoolName: "Pool A",
		Players:  []helper.Player{{Name: "Old One", Dojo: "D", Number: "1", ID: "id-1"}},
	}}))
	return store, compID
}

func renamedPools() []helper.Pool {
	return []helper.Pool{{
		PoolName: "Pool A",
		Players:  []helper.Player{{Name: "New One", Dojo: "D", Number: "K1", ID: "id-1"}},
	}}
}

func TestStoreTx_SavePoolsThenAbortLeavesPoolsUnchanged(t *testing.T) {
	store, compID := poolsTxFixture(t)
	path := store.compPath(compID, "pools.csv")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	// Warm the cache so a body published at stage time would be served.
	_, err = store.LoadPools(compID)
	require.NoError(t, err)

	var staged uint64
	sentinel := errors.New("a later step failed")
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SavePools(compID, renamedPools()); err != nil {
			return err
		}
		staged = store.FileVersion(compID, "pools.csv")
		return sentinel
	})
	require.ErrorIs(t, txErr, sentinel)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "an aborted transaction must leave pools.csv byte-identical")

	assert.Greater(t, store.FileVersion(compID, "pools.csv"), staged,
		"the abort must move the version past the staged save's")

	got, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Len(t, got[0].Players, 1)
	assert.Equal(t, "Old One", got[0].Players[0].Name, "the cache must not serve the aborted pools")
	assert.Equal(t, "1", got[0].Players[0].Number)
}

// TestStoreTx_SavePoolsCommitFailureLeavesCachesClean pins that a
// transaction whose log cannot be written is an abort for the caches too:
// SavePools publishes its staged body at stage time, and a Commit that fails
// must not leave that body served while pools.csv still holds the old pools.
func TestStoreTx_SavePoolsCommitFailureLeavesCachesClean(t *testing.T) {
	store, compID := poolsTxFixture(t)
	path := store.compPath(compID, "pools.csv")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	_, err = store.LoadPools(compID)
	require.NoError(t, err)

	var staged uint64
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SavePools(compID, renamedPools()); err != nil {
			return err
		}
		staged = store.FileVersion(compID, "pools.csv")
		// Put a regular file where the log directory was, so Commit cannot
		// create the log file inside it.
		require.NoError(t, os.RemoveAll(store.walDir))
		require.NoError(t, os.WriteFile(store.walDir, []byte("not a directory"), 0o600))
		return nil
	})
	require.NoError(t, os.Remove(store.walDir))
	require.NoError(t, os.MkdirAll(store.walDir, 0o750))
	require.Error(t, txErr)
	assert.Contains(t, txErr.Error(), "Commit")

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a failed commit must leave pools.csv byte-identical")
	assert.Greater(t, store.FileVersion(compID, "pools.csv"), staged,
		"the failed commit must move the version past the staged save's")

	got, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Len(t, got[0].Players, 1)
	assert.Equal(t, "Old One", got[0].Players[0].Name, "the cache must not serve pools whose commit failed")
}

func TestStoreTx_LoadPoolsReadsWhatTheTransactionSaved(t *testing.T) {
	store, compID := poolsTxFixture(t)
	path := store.compPath(compID, "pools.csv")
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	sentinel := errors.New("abort after reading")
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SavePools(compID, renamedPools()); err != nil {
			return err
		}
		onDisk, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, string(before), string(onDisk), "a staged save must not reach the disk before the commit")

		got, err := tx.LoadPools(compID)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Len(t, got[0].Players, 1)
		assert.Equal(t, "New One", got[0].Players[0].Name)
		assert.Equal(t, "K1", got[0].Players[0].Number)
		assert.Equal(t, "id-1", got[0].Players[0].ID)
		assert.EqualValues(t, 1, got[0].Players[0].PoolPosition)

		// A copy: mutating it must not change what the next load answers.
		got[0].Players[0].Name = "mutated"
		again, err := tx.LoadPools(compID)
		require.NoError(t, err)
		assert.Equal(t, "New One", again[0].Players[0].Name)
		return sentinel
	})
	require.ErrorIs(t, txErr, sentinel)
}

func TestStoreTx_SavePoolsCommitLandsPools(t *testing.T) {
	store, compID := poolsTxFixture(t)
	before := store.FileVersion(compID, "pools.csv")

	require.NoError(t, store.WithTransaction(compID, func(tx StoreTx) error {
		return tx.SavePools(compID, renamedPools())
	}))

	assert.Greater(t, store.FileVersion(compID, "pools.csv"), before)
	got, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Len(t, got[0].Players, 1)
	assert.Equal(t, "New One", got[0].Players[0].Name)

	// And from disk, not only the cache.
	fresh, err := NewStore(store.folder)
	require.NoError(t, err)
	got, err = fresh.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "New One", got[0].Players[0].Name)
}
