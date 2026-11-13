package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
)

// TestStore_AKeptLogLaterWritesOvertookNeverUndoesThem is the review's case
// (operator decision 2026-11-12, W1 (a)): a transaction's Apply fails and its
// log is kept while the server goes on; pools.csv is written again; neither
// a backup's settle nor the next start may replay the older log over it, and
// the start sets the log aside as .stale.
func TestStore_AKeptLogLaterWritesOvertookNeverUndoesThem(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 is not enforced on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory mode 0500")
	}
	store, compID := poolsTxFixture(t)
	compDir := store.compPath(compID)

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SavePools(compID, renamedPools()); err != nil {
			return err
		}
		return os.Chmod(compDir, 0o500)
	})
	require.NoError(t, os.Chmod(compDir, 0o700))
	require.ErrorIs(t, txErr, ErrTxCommitted)

	later := []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{{Name: "Later One", Dojo: "D", Number: "1", ID: "id-1"}}}}
	require.NoError(t, store.SavePools(compID, later))
	path := store.compPath(compID, "pools.csv")
	want, err := os.ReadFile(path)
	require.NoError(t, err)

	_, err = store.Snapshot(ScopeCompetition, compID)
	require.NoError(t, err)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), "a backup does not settle the overtaken log")

	_, err = NewStore(store.folder)
	require.NoError(t, err)
	got, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(want), string(got), "the restart does not replay the overtaken log")
	stale, err := filepath.Glob(filepath.Join(store.walDir, "*.json.stale"))
	require.NoError(t, err)
	assert.Len(t, stale, 1, "the log is kept beside the others")
	live, err := filepath.Glob(filepath.Join(store.walDir, "*.json"))
	require.NoError(t, err)
	assert.Empty(t, live)
}

// TestNewStore_RefusesALogItCannotCheck (operator decision 2026-11-12,
// W2 (b)): a file a pending log names cannot be read, so the store does not
// start, rather than replay or drop the log.
func TestNewStore_RefusesALogItCannotCheck(t *testing.T) {
	store, compID := poolsTxFixture(t)
	target := store.compPath(compID, "notes.txt")
	w, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL)
	require.NoError(t, err)
	w.Append(wal.FileIntent{Path: target, Data: []byte("x"), Mode: 0o600})
	require.NoError(t, w.Commit())
	require.NoError(t, os.MkdirAll(target, 0o700))

	_, err = NewStore(store.folder)
	require.ErrorIs(t, err, wal.ErrLogUnchecked)
}
