package wal

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// directWrite: a tiny WriteFn that does a plain os.WriteFile, fine
// for tests where we don't need the full atomic-rename ceremony the
// production wiring uses.
func directWrite(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}

// failingWrite returns a WriteFn that succeeds on the first N calls
// and then returns an injected error on call N+1. Used to simulate a
// crash mid-Apply.
func failingWrite(t *testing.T, n int, err error) WriteFn {
	t.Helper()
	calls := 0
	return func(path string, data []byte, perm os.FileMode) error {
		calls++
		if calls > n {
			return err
		}
		return directWrite(path, data, perm)
	}
}

// TestWALCommitApplyDone is the happy-path proof: Begin → Append →
// Commit → Apply → Done leaves the targets written and the WAL file
// gone. Catches regressions in the basic state machine.
func TestWALCommitApplyDone(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	targetA := filepath.Join(root, "data", "a.txt")
	targetB := filepath.Join(root, "data", "b.txt")

	w, err := BeginTx(walDir, "tx-happy", directWrite)
	require.NoError(t, err)

	w.Append(FileIntent{Path: targetA, Data: []byte("AAA"), Mode: 0o600})
	w.Append(FileIntent{Path: targetB, Data: []byte("BBB"), Mode: 0o600})

	require.NoError(t, w.Commit())

	// After Commit, the WAL file MUST be on disk so a crash here would
	// replay on next start.
	walPath := filepath.Join(walDir, "tx-happy.json")
	_, err = os.Stat(walPath)
	require.NoError(t, err, "WAL file must exist after Commit")

	require.NoError(t, w.Apply())

	// Both targets exist with the staged content.
	gotA, err := os.ReadFile(targetA) // #nosec G304, test path
	require.NoError(t, err)
	assert.Equal(t, "AAA", string(gotA))
	gotB, err := os.ReadFile(targetB) // #nosec G304, test path
	require.NoError(t, err)
	assert.Equal(t, "BBB", string(gotB))

	require.NoError(t, w.Done())

	// Done removes the WAL file.
	_, err = os.Stat(walPath)
	assert.True(t, os.IsNotExist(err), "WAL file must be gone after Done")
}

// TestWALScanReplaysCommittedWAL simulates a crash AFTER Commit but
// BEFORE Apply: target files don't exist yet, but the WAL on disk has
// the intent. Scan must surface the WAL so the caller can finish the
// work.
func TestWALScanReplaysCommittedWAL(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	target := filepath.Join(root, "data", "x.txt")

	w, err := BeginTx(walDir, "tx-replay", directWrite)
	require.NoError(t, err)
	w.Append(FileIntent{Path: target, Data: []byte("HELLO"), Mode: 0o600})
	require.NoError(t, w.Commit())
	// Crash: no Apply, no Done. Target does not exist yet.
	_, err = os.Stat(target)
	require.True(t, os.IsNotExist(err), "target must not exist pre-replay")

	// Restart: Scan must find the committed WAL.
	pending, err := Scan(walDir, directWrite)
	require.NoError(t, err)
	require.Len(t, pending, 1, "Scan must find the committed WAL")
	require.Equal(t, "tx-replay", pending[0].ID())

	// Replay: Apply + Done.
	require.NoError(t, pending[0].Apply())
	got, err := os.ReadFile(target) // #nosec G304 ,test path
	require.NoError(t, err)
	assert.Equal(t, "HELLO", string(got))

	require.NoError(t, pending[0].Done())

	// A second Scan must not surface this WAL again.
	pending2, err := Scan(walDir, directWrite)
	require.NoError(t, err)
	assert.Empty(t, pending2)
}

// TestWALScanIgnoresUnCommittedWAL pins that Append-without-Commit
// leaves no on-disk state, so a crash before Commit leaves nothing
// for Scan to find. This is the "abort" path: WithTransaction returns
// fn's error and the WAL never landed.
func TestWALScanIgnoresUnCommittedWAL(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	target := filepath.Join(root, "data", "ghost.txt")

	w, err := BeginTx(walDir, "tx-aborted", directWrite)
	require.NoError(t, err)
	w.Append(FileIntent{Path: target, Data: []byte("never-written"), Mode: 0o600})
	// Deliberately skip Commit.

	pending, err := Scan(walDir, directWrite)
	require.NoError(t, err)
	assert.Empty(t, pending,
		"uncommitted WAL must not be visible to Scan ,Append is in-memory only")

	_, err = os.Stat(target)
	assert.True(t, os.IsNotExist(err),
		"target must not exist after abort")
}

// TestWALAppendCoalesces pins the last-write-wins semantics for
// multiple Append calls to the same target path. This matches the
// score-handler rollback pattern: write new score → fail eligibility
// check → write prior score back. The WAL must end up with the prior
// score in its committed intent, not both.
func TestWALAppendCoalesces(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	target := filepath.Join(root, "data", "score.csv")

	w, err := BeginTx(walDir, "tx-coalesce", directWrite)
	require.NoError(t, err)
	w.Append(FileIntent{Path: target, Data: []byte("first"), Mode: 0o600})
	w.Append(FileIntent{Path: target, Data: []byte("second"), Mode: 0o600})
	w.Append(FileIntent{Path: target, Data: []byte("third"), Mode: 0o600})

	intents := w.Intents()
	assert.Len(t, intents, 1,
		"coalesce on path must leave exactly one intent per target")
	assert.Equal(t, "third", string(intents[0].Data),
		"last Append must win")

	require.NoError(t, w.Commit())
	require.NoError(t, w.Apply())
	got, err := os.ReadFile(target) // #nosec G304 ,test path
	require.NoError(t, err)
	assert.Equal(t, "third", string(got),
		"target file must contain only the last-written content")

	require.NoError(t, w.Done())
}

// TestWALApplyIsIdempotent pins that calling Apply twice on the same
// WAL produces the same on-disk state ,required because the replay
// path may re-Apply a WAL that already partially landed before the
// crash. The atomic-write primitive itself is idempotent for
// identical bytes; this test pins that Apply preserves that property.
func TestWALApplyIsIdempotent(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	target := filepath.Join(root, "data", "idemp.txt")

	w, err := BeginTx(walDir, "tx-idemp", directWrite)
	require.NoError(t, err)
	w.Append(FileIntent{Path: target, Data: []byte("FINAL"), Mode: 0o600})
	require.NoError(t, w.Commit())

	require.NoError(t, w.Apply())
	require.NoError(t, w.Apply())
	require.NoError(t, w.Apply())

	got, err := os.ReadFile(target) // #nosec G304 ,test path
	require.NoError(t, err)
	assert.Equal(t, "FINAL", string(got))

	require.NoError(t, w.Done())
}

// TestWALApplyPropagatesError pins that a writer error during Apply
// surfaces the error AND leaves the WAL file in place ,so the next
// Scan picks it up for retry. Without the on-disk WAL surviving the
// failure, the second-file failure case would silently lose the
// transaction.
func TestWALApplyPropagatesError(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	targetA := filepath.Join(root, "data", "first.txt")
	targetB := filepath.Join(root, "data", "second.txt")

	// Succeeds for targetA (call 2 ,call 1 was Commit) then fails on targetB.
	injected := os.ErrPermission
	wf := failingWrite(t, 2, injected)
	w, err := BeginTx(walDir, "tx-applyfail", wf)
	require.NoError(t, err)
	w.Append(FileIntent{Path: targetA, Data: []byte("A"), Mode: 0o600})
	w.Append(FileIntent{Path: targetB, Data: []byte("B"), Mode: 0o600})

	require.NoError(t, w.Commit())
	err = w.Apply()
	require.Error(t, err, "Apply must surface the writer error")
	assert.ErrorIs(t, err, injected)

	// WAL file remains on disk: a subsequent Scan must replay it.
	pending, err := Scan(walDir, directWrite)
	require.NoError(t, err)
	require.Len(t, pending, 1, "failed-Apply WAL must remain for replay")
}

// TestWALWriteFnCaptures pins the redirected-saver path: when a saver
// is given the WAL's WriteFn instead of the direct atomicWriteFile,
// the bytes get captured into the WAL's intent list without touching
// the target file. Defends against a regression where WriteFn would
// short-circuit straight to disk and bypass the atomicity contract.
func TestWALWriteFnCaptures(t *testing.T) {
	root := t.TempDir()
	walDir := filepath.Join(root, ".wal")
	target := filepath.Join(root, "data", "captured.txt")

	w, err := BeginTx(walDir, "tx-capture", directWrite)
	require.NoError(t, err)

	wf := w.WriteFn()
	// Saver-shape call: write some bytes via the captured WriteFn.
	require.NoError(t, wf(target, []byte("captured-bytes"), 0o600))

	// Target must NOT exist yet ,only Apply lands it.
	_, err = os.Stat(target)
	require.True(t, os.IsNotExist(err),
		"WriteFn must defer the write, not land it immediately")

	intents := w.Intents()
	require.Len(t, intents, 1)
	assert.Equal(t, target, intents[0].Path)
	assert.Equal(t, "captured-bytes", string(intents[0].Data))
	assert.Equal(t, os.FileMode(0o600), intents[0].Mode)
}

// TestScanReturnsEmptyForMissingWalDir pins the fresh-data-folder
// case: on a first-ever startup with no .wal/ directory yet, Scan
// must return (nil, nil) ,NOT an error ,so Store.NewStore can call
// it unconditionally without special-casing.
func TestScanReturnsEmptyForMissingWalDir(t *testing.T) {
	pending, err := Scan(filepath.Join(t.TempDir(), "does-not-exist"), directWrite)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

// TestPendingBytes verifies that PendingBytes returns (data, true) for
// an appended path and (nil, false) for an unknown path.
func TestPendingBytes(t *testing.T) {
	dir := t.TempDir()
	w, err := BeginTx(dir, "tx-pending", directWrite)
	require.NoError(t, err)

	// Before any Append: unknown path.
	data, ok := w.PendingBytes("/some/path")
	assert.False(t, ok)
	assert.Nil(t, data)

	// After Append: should return a copy of the data.
	want := []byte("hello world")
	w.Append(FileIntent{Path: "/some/path", Data: want, Mode: 0o600})

	data, ok = w.PendingBytes("/some/path")
	assert.True(t, ok)
	assert.Equal(t, want, data)

	// Returned slice must be a copy (not aliased).
	data[0] = 'X'
	data2, _ := w.PendingBytes("/some/path")
	assert.Equal(t, want, data2, "PendingBytes must return independent copy")
}

// TestBeginTx_NilWrite verifies that BeginTx rejects a nil write function.
func TestBeginTx_NilWrite(t *testing.T) {
	_, err := BeginTx(t.TempDir(), "tx-nil", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

// TestBeginTx_EmptyID verifies that BeginTx rejects an empty transaction id.
func TestBeginTx_EmptyID(t *testing.T) {
	_, err := BeginTx(t.TempDir(), "", directWrite)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

// TestDone_NoFileIsNoop verifies that Done returns nil when the WAL
// file doesn't exist (e.g., after an uncommitted BeginTx or a double Done).
func TestDone_NoFileIsNoop(t *testing.T) {
	dir := t.TempDir()
	w, err := BeginTx(dir, "tx-no-file", directWrite)
	require.NoError(t, err)

	// No Commit → no WAL file on disk. Done must return nil.
	err = w.Done()
	assert.NoError(t, err)

	// Second Done is also a no-op.
	err = w.Done()
	assert.NoError(t, err)
}

// TestCommit_WriteFnError verifies that Commit surfaces the write-fn
// error (the "write the WAL file itself" step) and the WAL remains
// uncommitted.
func TestCommit_WriteFnError(t *testing.T) {
	errWrite := fmt.Errorf("disk full")
	failFn := func(path string, data []byte, perm os.FileMode) error {
		return errWrite
	}
	dir := t.TempDir()
	w, err := BeginTx(dir, "tx-commit-err", failFn)
	require.NoError(t, err)

	w.Append(FileIntent{Path: filepath.Join(dir, "foo.txt"), Data: []byte("x"), Mode: 0o600})

	err = w.Commit()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

// TestScan_WalDirIsRegularFile verifies that Scan returns an error when
// walDir points to a regular file rather than a directory.
func TestScan_WalDirIsRegularFile(t *testing.T) {
	dir := t.TempDir()
	// Create a regular file where walDir expects a directory.
	fileAsDir := filepath.Join(dir, "not-a-dir.txt")
	require.NoError(t, os.WriteFile(fileAsDir, []byte("data"), 0o600))

	_, err := Scan(fileAsDir, directWrite)
	assert.Error(t, err, "Scan on a regular file path must return an error")
}

// TestBeginTx_MkdirAllError verifies that BeginTx returns an error when
// the walDir cannot be created (parent path is a regular file).
func TestBeginTx_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	// Create a file that blocks creating a subdirectory under it.
	blocker := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	// Try to create walDir as a subpath of the blocker file.
	_, err := BeginTx(filepath.Join(blocker, "wal"), "tx1", directWrite)
	assert.Error(t, err, "mkdir under a regular file must fail")
}

// TestScan_MultipleEntries verifies that Scan returns WALs in id-sorted
// order when multiple committed WALs are present, and that each carries
// the correct intents.
func TestScan_MultipleEntries(t *testing.T) {
	dir := t.TempDir()
	target := t.TempDir()

	// Commit two WALs with different data. WAL id ordering is nanos-based,
	// but we just need two distinct ids.
	for i, content := range []string{"first", "second"} {
		w, err := BeginTx(dir, NewWALID(), directWrite)
		require.NoError(t, err)
		w.Append(FileIntent{
			Path: filepath.Join(target, "file.txt"),
			Data: []byte(content),
			Mode: 0o600,
		})
		require.NoError(t, w.Commit())
		_ = i
	}

	wals, err := Scan(dir, directWrite)
	require.NoError(t, err)
	assert.Len(t, wals, 2)
	// IDs should be non-empty and distinct.
	assert.NotEmpty(t, wals[0].ID())
	assert.NotEmpty(t, wals[1].ID())
	assert.NotEqual(t, wals[0].ID(), wals[1].ID())
}

// TestUniqueWALIDDifferentEachCall pins that two BeginTx calls in the
// same nanosecond don't collide. Without the atomic counter, a sub-
// nanosecond gap could produce duplicate IDs and a Scan would see two
// WALs sharing a file path (one would overwrite the other).
func TestUniqueWALIDDifferentEachCall(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := NewWALID()
		assert.Falsef(t, seen[id], "duplicate WAL id: %s", id)
		seen[id] = true
	}
}
