package state

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The data folder is made absolute once, in NewStore, so the paths a log
// records do not depend on how the folder was typed or where the process was
// started.

func TestNewStore_AStoreOpenedOnTheCurrentFolderCanWrite(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	store, err := NewStore(".")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(store.GetFolder()), "the folder is kept absolute")

	require.NoError(t, store.SaveCompetition(&Competition{ID: "dot-folder", Name: "Dot Folder"}))
	_, err = os.Stat(filepath.Join(dir, "competitions", "dot-folder", "config.md"))
	require.NoError(t, err, "the write landed in the folder the store was opened on")
}

// A log an earlier build wrote while the folder was typed relative names its
// files relative to the folder the process was started in; it still replays
// when the store is opened under the absolute spelling from that folder.
func TestNewStore_ALogWithRelativePathsReplaysUnderTheAbsoluteSpelling(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	store, err := NewStore("data")
	require.NoError(t, err)
	const compID = "relative-log"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Relative Log"}))

	relTarget := filepath.Join("data", "competitions", compID, "landed.txt")
	w, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	w.Append(wal.FileIntent{Path: relTarget, Data: []byte("landed"), Mode: 0o600})
	require.NoError(t, w.Commit())
	require.Len(t, liveWALs(t, store), 1)

	reopened, err := NewStore(filepath.Join(parent, "data"))
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(parent, relTarget))
	require.NoError(t, err, "the replay landed the file the relative log named")
	assert.Equal(t, "landed", string(got))
	assert.Empty(t, liveWALs(t, reopened))
}

// A log whose files lie outside the data folder was written under another
// path of it: startup says so, rather than asking for access to be fixed.
func TestNewStore_ALogWrittenUnderAnotherDataFolderNamesTheCause(t *testing.T) {
	first := t.TempDir()
	store, err := NewStore(first)
	require.NoError(t, err)
	const compID = "moved-folder"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Moved Folder"}))
	w, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	w.Append(wal.FileIntent{Path: store.compPath(compID, "landed.txt"), Data: []byte("x"), Mode: 0o600})
	require.NoError(t, w.Commit())

	second := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(second, ".wal"), 0o750))
	for _, src := range liveWALs(t, store) {
		data, rerr := os.ReadFile(src)
		require.NoError(t, rerr)
		require.NoError(t, os.WriteFile(filepath.Join(second, ".wal", filepath.Base(src)), data, 0o600))
	}

	_, err = NewStore(second)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "written under another data folder path")
	assert.Contains(t, err.Error(), walRemedyAbsolute, "a log naming absolute paths asks for the folder it was written under")
	assert.NotContains(t, err.Error(), walRemedyRelative)
	assert.NotContains(t, err.Error(), "fix access")
}

// writeEarlierBuildLog writes, by hand, a log as an earlier build wrote it:
// version 0, writes only under "intents", and no "before" on any step, so the
// stale guard has nothing to judge. It answers the log's path in .wal.
func writeEarlierBuildLog(t *testing.T, walDir, target string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"id":      "1-earlier",
		"intents": []map[string]any{{"path": target, "data": []byte("landed"), "mode": 0o600}},
	})
	require.NoError(t, err)
	logPath := filepath.Join(walDir, "1-earlier.json")
	require.NoError(t, os.WriteFile(logPath, raw, 0o600))
	return logPath
}

// An earlier build's log names its files as the folder was typed, relative to
// the folder the process started in. Started from another folder, those paths
// lie outside the data folder: startup refuses and names the cause, rather
// than starting with the log failing unrecorded at every start.
func TestNewStore_AnEarlierBuildsRelativeLogStartedFromAnotherFolderRefusesStartup(t *testing.T) {
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "data")
	store, err := NewStore(dataDir)
	require.NoError(t, err)
	const compID = "earlier-relative"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Earlier Relative"}))
	relTarget := filepath.Join("data", "competitions", compID, "landed.txt")
	logPath := writeEarlierBuildLog(t, store.walDir, relTarget)
	before, err := os.ReadFile(logPath)
	require.NoError(t, err)

	t.Chdir(t.TempDir())
	_, err = NewStore(dataDir)
	require.Error(t, err)
	assert.ErrorIs(t, err, errOutsideDataFolder)
	assert.Contains(t, err.Error(), "written under another data folder path")
	assert.Contains(t, err.Error(), walRemedyRelative, "a relative log asks for the folder the app was started from, which -f cannot name")
	assert.NotContains(t, err.Error(), walRemedyAbsolute)

	after, rerr := os.ReadFile(logPath)
	require.NoError(t, rerr, "the log is left in .wal")
	assert.Equal(t, before, after, "the log is left untouched")
	_, serr := os.Stat(filepath.Join(dataDir, "competitions", compID, "landed.txt"))
	assert.True(t, os.IsNotExist(serr), "nothing of the log was applied")
}

// A log whose files lie inside the data folder but under no competition folder
// (an earlier build's relative log started from the data folder itself) has no
// competition to retry it with: when it cannot be applied, startup refuses
// rather than starting with it unrecorded.
func TestNewStore_ALogNamingNoCompetitionThatCannotApplyRefusesStartup(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	require.NoError(t, err)
	// competitions/ under a "data" folder that does not exist: the write fails.
	target := filepath.Join(dataDir, "data", "competitions", "x", "landed.txt")
	logPath := writeEarlierBuildLog(t, store.walDir, target)

	_, err = NewStore(dataDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no competition folder")
	assert.Contains(t, err.Error(), walRemedyAbsolute)
	assert.NotContains(t, err.Error(), walRemedyRelative)
	_, serr := os.Stat(logPath)
	assert.NoError(t, serr, "the log is left in .wal")
}

// An earlier build's relative log, started from the data folder itself,
// resolves inside the folder but under no competitions/ folder: its Apply
// fails and no competition can retry it. The refusal asks for the folder the
// app was started from, since the data folder path alone cannot cure it.
func TestNewStore_ARelativeLogNamingNoCompetitionAsksForTheStartFolder(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	require.NoError(t, err)
	logPath := writeEarlierBuildLog(t, store.walDir, filepath.Join("data", "competitions", "x", "landed.txt"))

	t.Chdir(dataDir)
	_, err = NewStore(dataDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no competition folder")
	assert.Contains(t, err.Error(), walRemedyRelative)
	assert.NotContains(t, err.Error(), walRemedyAbsolute)
	_, serr := os.Stat(logPath)
	assert.NoError(t, serr, "the log is left in .wal")
}

// Readers that all saw a settle due queue on the competition's lock; only the
// first tries it, the rest find it failed moments ago and read on.
func TestStore_ConcurrentReadsTryAFailedKeptChangeOncePerWait(t *testing.T) {
	skipUnlessChmodEnforced(t)
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID, matchID = "kept-concurrent", "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Kept Concurrent"}))
	keptMatchAndHistory(t, store, compID, matchID)

	v, ok := store.keptWAL.Load(compID)
	require.True(t, ok)
	k := v.(*keptChange)
	now := time.Unix(1_800_000_000, 0)
	earlier := now.Add(-keptSettleReadBackoff)
	k.failedAt.Store(&earlier) // a failure one wait ago: a read is due

	const readers = 8
	var clockCalls atomic.Int32
	allChecked := make(chan struct{})
	var once sync.Once
	store.clock = func() time.Time {
		if clockCalls.Add(1) >= readers {
			once.Do(func() { close(allChecked) })
		}
		return now
	}

	// Hold the lock so every reader passes the lock-free check first.
	mu := store.lockComp(compID)
	var wg sync.WaitGroup
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, rerr := store.LoadMatchHistory(compID, matchID)
			assert.NoError(t, rerr)
		}()
	}
	<-allChecked
	mu.Unlock()
	wg.Wait()

	assert.Equal(t, int32(1), k.attempts.Load(), "one settle per wait, however many readers queued")
}

// The wait is measured on the monotonic clock: the time stored for a failed
// settle carries its monotonic reading, so a wall clock stepped back cannot
// stretch it.
func TestStore_TheReadWaitIsMeasuredOnTheMonotonicClock(t *testing.T) {
	skipUnlessChmodEnforced(t)
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID, matchID = "kept-mono", "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Kept Mono"}))
	keptMatchAndHistory(t, store, compID, matchID)

	_, err = store.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	v, ok := store.keptWAL.Load(compID)
	require.True(t, ok)
	failed := v.(*keptChange).failedAt.Load()
	require.NotNil(t, failed)
	assert.Contains(t, failed.String(), "m=", "the stored time keeps its monotonic reading")
}

// At startup, once a competition's log cannot be applied, its later logs wait
// behind it: applying them first would leave the earlier one stale, and it
// would be set aside with its change lost. They land, in order, with the
// first. The refusal and the warning name the log that cannot land.
func TestNewStore_ACompetitionsLaterLogsWaitBehindOneThatCannotApply(t *testing.T) {
	folder := t.TempDir()
	store, err := NewStore(folder)
	require.NoError(t, err)
	const compID = "replay-in-order"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Replay In Order"}))

	// The first log writes shared.txt, then a file under a folder that is
	// missing, so its Apply lands shared.txt and fails. The second, made
	// after that partial apply, writes shared.txt again.
	shared := store.compPath(compID, "shared.txt")
	missing := store.compPath(compID, "nope", "x")
	first, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	first.Append(wal.FileIntent{Path: shared, Data: []byte("first"), Mode: 0o600})
	first.Append(wal.FileIntent{Path: missing, Data: []byte("x"), Mode: 0o600})
	require.NoError(t, first.Commit())
	require.Error(t, first.Apply())
	time.Sleep(time.Millisecond) // the second log's id sorts after the first's
	second, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	second.Append(wal.FileIntent{Path: shared, Data: []byte("second"), Mode: 0o600})
	require.NoError(t, second.Commit())

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	reopened, err := NewStore(folder)
	require.NoError(t, err)
	got, err := os.ReadFile(shared)
	require.NoError(t, err)
	assert.Equal(t, "first", string(got), "the later log of the competition was not applied over the kept one")
	assert.Len(t, liveWALs(t, reopened), 2, "both logs wait")
	_, kept := reopened.keptWAL.Load(compID)
	require.True(t, kept)

	_, err = reopened.lockCompWrite(compID)
	require.ErrorIs(t, err, ErrKeptChangeUnsettled)
	assert.Contains(t, err.Error(), first.ID(), "the refusal names the log that cannot land")
	warn := ""
	for line := range strings.SplitSeq(logs.String(), "\n") {
		if strings.Contains(line, "level=WARN") && strings.Contains(line, "kept in the transaction log") {
			warn = line
		}
	}
	require.NotEmpty(t, warn, "the first failed settle is warned")
	assert.Contains(t, warn, first.ID(), "the warning names the log")

	require.NoError(t, os.Mkdir(reopened.compPath(compID, "nope"), 0o700))
	mu, err := reopened.lockCompWrite(compID)
	require.NoError(t, err)
	mu.Unlock()
	got, err = os.ReadFile(shared)
	require.NoError(t, err)
	assert.Equal(t, "second", string(got), "both logs landed, in order")
	_, err = os.Stat(missing)
	require.NoError(t, err, "the first log's change was not lost")
	assert.Empty(t, liveWALs(t, reopened))
}
