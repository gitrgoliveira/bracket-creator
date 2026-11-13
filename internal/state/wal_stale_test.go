package state

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
)

// TestStore_AKeptLogLaterWritesOvertookNeverUndoesThem is the review's case
// (operator decision 2026-11-12, W1 (a)): a transaction's Apply fails and its
// log is kept while the server goes on; pools.csv is written again from
// outside the store (the store's own writers finish the kept log first); neither
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

	// The store's own writers finish the kept change first
	// (wal_kept_settle_test.go), so the writer that overtakes the log here is
	// one outside this process: a hand edit, or an older build's process.
	later := []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{{Name: "Later One", Dojo: "D", Number: "1", ID: "id-1"}}}}
	other, _ := poolsTxFixture(t)
	require.NoError(t, other.SavePools(compID, later))
	want, err := os.ReadFile(other.compPath(compID, "pools.csv"))
	require.NoError(t, err)
	path := store.compPath(compID, "pools.csv")
	require.NoError(t, os.WriteFile(path, want, 0o600))

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
	w, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	w.Append(wal.FileIntent{Path: target, Data: []byte("x"), Mode: 0o600})
	require.NoError(t, w.Commit())
	require.NoError(t, os.MkdirAll(target, 0o700))

	_, err = NewStore(store.folder)
	require.ErrorIs(t, err, wal.ErrLogUnchecked)
}

// TestNewStore_RefusesALogItCannotRead pins the W2 (d) ruling: a pending log
// that cannot be read (an access fault, not damaged content) refuses startup,
// naming the file, like a log whose files cannot be checked. Skipping it
// would lose its change; once access is fixed it replays with its stale guard.
func TestNewStore_RefusesALogItCannotRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 is not enforced on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses file mode 000")
	}
	store, _ := poolsTxFixture(t)
	const name = "1700000000000000000-0001.json"
	path := filepath.Join(store.walDir, name)
	require.NoError(t, os.WriteFile(path, []byte(`{"id":"x","intents":[]}`), 0o600))
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, err := NewStore(store.folder)
	require.ErrorIs(t, err, wal.ErrLogUnchecked)
	assert.Contains(t, err.Error(), name)
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr, "the unreadable log is left where it is")

	require.NoError(t, os.Chmod(path, 0o600))
	_, err = NewStore(store.folder)
	require.NoError(t, err, "once access is fixed the log replays")
}

// TestNewStore_SetsADamagedLogAsideOnce pins the W2 (d) ruling for a pending
// log whose content does not parse: it is renamed to end in .invalid, startup
// goes on with one error line, and the next start neither reads nor reports it.
func TestNewStore_SetsADamagedLogAsideOnce(t *testing.T) {
	store, _ := poolsTxFixture(t)
	const name = "1700000000000000000-0002.json"
	path := filepath.Join(store.walDir, name)
	require.NoError(t, os.WriteFile(path, []byte("{not a log"), 0o600))

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	_, err := NewStore(store.folder)
	require.NoError(t, err)
	_, statErr := os.Stat(path + ".invalid")
	require.NoError(t, statErr, "the damaged log is kept, renamed")
	_, statErr = os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
	const said = "may not have landed"
	assert.Equal(t, 1, strings.Count(logs.String(), said), logs.String())

	_, err = NewStore(store.folder)
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(logs.String(), said), "the second start does not report it again")

	// A backup's settle never renames or refuses over it.
	require.NoError(t, os.Rename(path+".invalid", path))
	_, err = store.Snapshot(ScopeTournament, "")
	require.NoError(t, err)
	_, statErr = os.Stat(path)
	assert.NoError(t, statErr, "only startup sets a damaged log aside")
}
