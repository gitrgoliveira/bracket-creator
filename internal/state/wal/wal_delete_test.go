package wal

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// directRemove is the RemoveFn the tests hand the WAL: a plain os.Remove that
// reports a missing file as it is, so the WAL's own not-exist tolerance is
// what the idempotence tests prove.
func directRemove(path string) error { return os.Remove(path) }

// olderWALFile and olderFileIntent are the shapes a build before the delete
// step decodes a log into, copied as they were: the test reads a new log the
// way that build would.
type olderFileIntent struct {
	Path string      `json:"path"`
	Data []byte      `json:"data"`
	Mode os.FileMode `json:"mode"`
}

type olderWALFile struct {
	ID      string            `json:"id"`
	Intents []olderFileIntent `json:"intents"`
}

func TestWALDeleteIntent_AppliedOnCommitInStagedOrder(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	newFile := filepath.Join(root, "data", "new.yaml")
	oldFile := filepath.Join(root, "data", "old.yaml")
	require.NoError(t, directWrite(oldFile, []byte("old"), 0o600))

	w, err := BeginTx(walDir, "tx-move", directWrite, directRemove)
	require.NoError(t, err)
	require.NoError(t, w.WriteFn()(newFile, []byte("new"), 0o600))
	require.NoError(t, w.RemoveFn()(oldFile))

	// Staged only: nothing on disk moves before Commit and Apply.
	_, statErr := os.Stat(oldFile)
	require.NoError(t, statErr, "a staged delete must not touch the disk")

	require.NoError(t, w.Commit())
	require.NoError(t, w.Apply())
	require.NoError(t, w.Done())

	got, err := os.ReadFile(newFile)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))
	_, statErr = os.Stat(oldFile)
	assert.True(t, os.IsNotExist(statErr), "the delete step must remove the file on Apply")
}

func TestWALDeleteIntent_ReplayedOnStartupAndIdempotent(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	newFile := filepath.Join(root, "data", "new.yaml")
	oldFile := filepath.Join(root, "data", "old.yaml")
	require.NoError(t, directWrite(oldFile, []byte("old"), 0o600))

	w, err := BeginTx(walDir, "tx-replay-delete", directWrite, directRemove)
	require.NoError(t, err)
	require.NoError(t, w.WriteFn()(newFile, []byte("new"), 0o600))
	require.NoError(t, w.RemoveFn()(oldFile))
	require.NoError(t, w.Commit())
	// Crash: committed, never applied.

	pending, err := Scan(walDir, directWrite, directRemove)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	intents := pending[0].Intents()
	require.Len(t, intents, 2, "the replayed log holds the write and the delete, in order")
	assert.False(t, intents[0].Delete)
	assert.True(t, intents[1].Delete)
	assert.Equal(t, oldFile, intents[1].Path)

	require.NoError(t, pending[0].Apply())
	_, statErr := os.Stat(oldFile)
	require.True(t, os.IsNotExist(statErr), "replay must remove the file")
	got, err := os.ReadFile(newFile)
	require.NoError(t, err)
	assert.Equal(t, "new", string(got))

	// A second replay (a crash after Apply, before Done) finds the file
	// already gone: that is success, not an error.
	require.NoError(t, pending[0].Apply(), "removing an already-absent file is success")
	again, err := Scan(walDir, directWrite, directRemove)
	require.NoError(t, err)
	require.Len(t, again, 1)
	require.NoError(t, again[0].Apply())
	require.NoError(t, again[0].Done())
}

// TestWALScan_SetsAsideAStepsLogAnotherWriterOvertook pins the downgrade
// case: a build from before the delete step keeps a version-2 log unapplied
// and goes on writing the files it names, so after the upgrade replaying it
// would put the older content back. Scan sets such a log aside instead.
func TestWALScan_MarksAStepsLogAnotherWriterOvertookStale(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	newFile := filepath.Join(root, "data", "new.yaml")
	oldFile := filepath.Join(root, "data", "old.yaml")
	require.NoError(t, directWrite(oldFile, []byte("old"), 0o600))
	require.NoError(t, directWrite(newFile, []byte("before"), 0o600))

	w, err := BeginTx(walDir, "tx-overtaken", directWrite, directRemove)
	require.NoError(t, err)
	require.NoError(t, w.WriteFn()(newFile, []byte("staged"), 0o600))
	require.NoError(t, w.RemoveFn()(oldFile))
	require.NoError(t, w.Commit())
	// Downgrade: the older build leaves the log and writes the file itself.
	require.NoError(t, directWrite(newFile, []byte("written later"), 0o600))

	pending, err := Scan(walDir, directWrite, directRemove)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, newFile, pending[0].Stale(), "the overtaken file is named")
	_, statErr := os.Stat(filepath.Join(walDir, "tx-overtaken.json"))
	assert.NoError(t, statErr, "Scan itself changes nothing")

	require.NoError(t, pending[0].SetAside())
	_, statErr = os.Stat(filepath.Join(walDir, "tx-overtaken.json.stale"))
	assert.NoError(t, statErr, "the log is kept beside the others")
	again, err := Scan(walDir, directWrite, directRemove)
	require.NoError(t, err)
	assert.Empty(t, again, "a log set aside is not read again")
}

// TestWALScan_MarksAWriteLogAnotherWriterOvertookStale: a writes-only log
// kept after its Apply failed, with the file written again since, is stale
// too; replaying it would put the older content back (W1 (a)).
func TestWALScan_MarksAWriteLogAnotherWriterOvertookStale(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	target := filepath.Join(root, "data", "participants.csv")
	untouched := filepath.Join(root, "data", "pools.csv")
	require.NoError(t, directWrite(target, []byte("roster v1"), 0o600))

	w, err := BeginTx(walDir, "tx-kept", directWrite, directRemove)
	require.NoError(t, err)
	require.NoError(t, w.WriteFn()(target, []byte("roster v2"), 0o600))
	require.NoError(t, w.WriteFn()(untouched, []byte("pools"), 0o600))
	require.NoError(t, w.Commit())

	pending, err := Scan(walDir, directWrite, directRemove)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Empty(t, pending[0].Stale(), "nothing written since: it replays")

	require.NoError(t, directWrite(target, []byte("roster v3, 40 check-ins later"), 0o600))
	pending, err = Scan(walDir, directWrite, directRemove)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, target, pending[0].Stale())
}

// TestWALScan_DoesNotJudgeALogFromBeforeTheField: a log the previous release
// wrote carries no "before"; it is replayed as that release would.
func TestWALScan_DoesNotJudgeALogFromBeforeTheField(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	target := filepath.Join(root, "data", "a.txt")
	require.NoError(t, directWrite(target, []byte("something else"), 0o600))
	logBody := `{"id":"tx-old","intents":[{"path":` + strconv.Quote(target) + `,"data":"QUFB","mode":384}]}`
	require.NoError(t, directWrite(filepath.Join(walDir, "tx-old.json"), []byte(logBody), 0o600))

	pending, err := Scan(walDir, directWrite, directRemove)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Empty(t, pending[0].Stale())
}

// TestWALScan_RefusesALogItCannotCheck: a file the log names cannot be read
// (here a folder stands where it was), so Scan names the log and refuses.
func TestWALScan_RefusesALogItCannotCheck(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	target := filepath.Join(root, "data", "a.txt")
	w, err := BeginTx(walDir, "tx-unreadable", directWrite, directRemove)
	require.NoError(t, err)
	require.NoError(t, w.WriteFn()(target, []byte("AAA"), 0o600))
	require.NoError(t, w.Commit())
	require.NoError(t, os.MkdirAll(target, 0o700))

	_, err = Scan(walDir, directWrite, directRemove)
	require.ErrorIs(t, err, ErrLogUnchecked)
	assert.Contains(t, err.Error(), "tx-unreadable.json")
}

// TestWALCommit_FailedWriteLeavesNoLog: the log write fails after the log is
// in place; Commit removes it, so the failure is an abort (W3 (a)).
func TestWALCommit_FailedWriteLeavesNoLog(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	landsThenFails := func(path string, data []byte, perm os.FileMode) error {
		if err := directWrite(path, data, perm); err != nil {
			return err
		}
		return errors.New("sync failed")
	}
	w, err := BeginTx(walDir, "tx-sync", landsThenFails, directRemove)
	require.NoError(t, err)
	w.Append(FileIntent{Path: filepath.Join(root, "a.txt"), Data: []byte("AAA"), Mode: 0o600})

	err = w.Commit()
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrCommitLeftLog)
	_, statErr := os.Stat(filepath.Join(walDir, "tx-sync.json"))
	assert.ErrorIs(t, statErr, fs.ErrNotExist, "the failed commit's log is removed")
}

// TestWALCommit_FailedWriteWhoseLogStaysSaysSo: the log cannot be removed
// either (a non-empty folder stands at its name), so Commit says the log is
// in place and the change will be replayed.
func TestWALCommit_FailedWriteWhoseLogStaysSaysSo(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	staysThenFails := func(path string, _ []byte, _ os.FileMode) error {
		if err := directWrite(filepath.Join(path, "inside"), []byte("x"), 0o600); err != nil {
			return err
		}
		return errors.New("sync failed")
	}
	w, err := BeginTx(walDir, "tx-stuck", staysThenFails, directRemove)
	require.NoError(t, err)
	w.Append(FileIntent{Path: filepath.Join(root, "a.txt"), Data: []byte("AAA"), Mode: 0o600})

	require.ErrorIs(t, w.Commit(), ErrCommitLeftLog)
}

// A pure-write log keeps the exact bytes the previous release wrote, so a log
// committed by this build is still read by an older one, and a log an older
// one left is read by this one unchanged.
func TestWALCommit_PureWriteLogKeepsThePreviousFormat(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	w, err := BeginTx(walDir, "tx-v1", directWrite, directRemove)
	require.NoError(t, err)
	w.Append(FileIntent{Path: "/data/a.txt", Data: []byte("AAA"), Mode: 0o600})
	require.NoError(t, w.Commit())

	raw, err := os.ReadFile(filepath.Join(walDir, "tx-v1.json"))
	require.NoError(t, err)
	// No version and no "steps": the previous release reads it, ignoring the
	// added "before" key as encoding/json ignores any unknown one.
	assert.Equal(t, `{"id":"tx-v1","intents":[{"path":"/data/a.txt","data":"QUFB","mode":384,"before":"absent"}]}`, string(raw))
	var previous struct {
		Intents []struct {
			Path string
			Data []byte
			Mode os.FileMode
		}
	}
	require.NoError(t, json.Unmarshal(raw, &previous))
	require.Len(t, previous.Intents, 1)
	assert.Equal(t, "AAA", string(previous.Intents[0].Data))
}

func TestWALScan_ReplaysALogThePreviousReleaseWrote(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	require.NoError(t, os.MkdirAll(walDir, 0o750))
	target := filepath.Join(root, "data", "a.txt")
	raw, err := json.Marshal(olderWALFile{ID: "old-release", Intents: []olderFileIntent{{Path: target, Data: []byte("AAA"), Mode: 0o600}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(walDir, "old-release.json"), raw, 0o600))

	pending, err := Scan(walDir, directWrite, directRemove)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.NoError(t, pending[0].Apply())
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "AAA", string(got))
}

// What an older build does with a log holding a delete step: it decodes the
// one intent under "intents", whose empty path its writer refuses (outside the
// data folder), so its Apply fails before it writes any file, it logs the
// failure and keeps the log for the next start. It never writes an empty file
// over the path the delete names, and never drops the log.
func TestWALCommit_ALogWithADeleteCannotBeAppliedByAnOlderBuild(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	keep := filepath.Join(root, "data", "keep.yaml")
	require.NoError(t, directWrite(keep, []byte("keep"), 0o600))

	w, err := BeginTx(walDir, "tx-v2", directWrite, directRemove)
	require.NoError(t, err)
	require.NoError(t, w.WriteFn()(filepath.Join(root, "data", "new.yaml"), []byte("new"), 0o600))
	require.NoError(t, w.RemoveFn()(keep))
	require.NoError(t, w.Commit())

	raw, err := os.ReadFile(filepath.Join(walDir, "tx-v2.json"))
	require.NoError(t, err)
	var older olderWALFile
	require.NoError(t, json.Unmarshal(raw, &older))
	require.Len(t, older.Intents, 1, "an older build sees exactly one step")
	assert.Empty(t, older.Intents[0].Path, "and it names no file, so its writer refuses it before any file is written")
	assert.Empty(t, older.Intents[0].Data)
}

func TestWALScan_RefusesALogOfAnUnknownVersion(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	require.NoError(t, os.MkdirAll(walDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(walDir, "future.json"),
		[]byte(`{"id":"future","version":99,"steps":[],"intents":[{"path":"","data":null,"mode":0}]}`), 0o600))

	_, err := Scan(walDir, directWrite, directRemove)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownLogVersion), "a log this build cannot read must refuse, not be skipped: %v", err)
	assert.Contains(t, err.Error(), "future.json")
}

func TestWALPending_ADeleteReadsAsAbsentAndCoalescesWithWrites(t *testing.T) {
	w, err := BeginTx(t.TempDir(), "tx-pending-delete", directWrite, directRemove)
	require.NoError(t, err)

	require.NoError(t, w.WriteFn()("/p", []byte("x"), 0o600))
	data, ok := w.PendingBytes("/p")
	require.True(t, ok)
	assert.Equal(t, "x", string(data))
	assert.False(t, w.PendingRemove("/p"))

	// A delete after a write of the same path: the last step wins.
	require.NoError(t, w.RemoveFn()("/p"))
	_, ok = w.PendingBytes("/p")
	assert.False(t, ok, "a staged delete has no bytes")
	assert.True(t, w.PendingRemove("/p"), "a staged delete reads as absent")
	require.Len(t, w.Intents(), 1)

	// And a write after it brings the file back.
	require.NoError(t, w.WriteFn()("/p", []byte("y"), 0o600))
	assert.False(t, w.PendingRemove("/p"))
	data, ok = w.PendingBytes("/p")
	require.True(t, ok)
	assert.Equal(t, "y", string(data))
	assert.False(t, w.PendingRemove("/never-staged"))
}

func TestWALApply_PropagatesARemoveError(t *testing.T) {
	boom := errors.New("boom")
	w, err := BeginTx(t.TempDir(), "tx-remove-fails", directWrite, func(string) error { return boom })
	require.NoError(t, err)
	require.NoError(t, w.RemoveFn()("/x"))
	err = w.Apply()
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

func TestBeginTxAndScan_NilRemove(t *testing.T) {
	_, err := BeginTx(t.TempDir(), "tx", directWrite, nil)
	require.Error(t, err)
	_, err = Scan(t.TempDir(), directWrite, nil)
	require.Error(t, err)
}
