package state

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listTmpOrphans returns every entry in dir whose name contains the
// ".tmp-" marker the helper uses for its sibling file. Used by the
// orphan-cleanup assertions.
func listTmpOrphans(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var out []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestAtomicWriteFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.md")
	data := []byte("---\nname: test\n---\n")

	require.NoError(t, atomicWriteFile(target, data, 0600))

	got, err := os.ReadFile(target) // #nosec G304 — test path.
	require.NoError(t, err)
	assert.Equal(t, data, got, "file content should equal what was written")

	info, err := os.Stat(target)
	require.NoError(t, err)
	// Mode bits include the file-type bits; mask to permission bits.
	assert.Equal(t, fs.FileMode(0600), info.Mode().Perm(), "perm bits should be exactly 0600")

	assert.Empty(t, listTmpOrphans(t, dir), "no .tmp orphan should remain after success")
}

func TestAtomicWriteFile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "data.csv")

	require.NoError(t, os.WriteFile(target, []byte("OLD\n"), 0600))
	require.NoError(t, atomicWriteFile(target, []byte("NEW\n"), 0600))

	got, err := os.ReadFile(target) // #nosec G304 — test path.
	require.NoError(t, err)
	assert.Equal(t, []byte("NEW\n"), got, "file should be replaced with the new content")

	assert.Empty(t, listTmpOrphans(t, dir), "no .tmp orphan should remain after overwrite")
}

func TestAtomicWriteFile_NoPartialOnTmpFailure(t *testing.T) {
	// We can't easily make tmp.Sync or os.Rename fail in a portable way
	// from a test, so the easiest reproducible failure path is to make
	// the open-of-tmp fail by pointing into a non-existent directory.
	// The contract we're verifying: any failure leaves the original
	// target unchanged AND leaves no .tmp orphan.
	dir := t.TempDir()
	target := filepath.Join(dir, "missing-subdir", "x.json")

	err := atomicWriteFile(target, []byte("payload"), 0600)
	require.Error(t, err, "writing into a non-existent directory should error")

	// Original target was absent and remains absent.
	_, statErr := os.Stat(target)
	assert.True(t, os.IsNotExist(statErr), "target should still not exist after failure")

	// No orphan .tmp sibling in either the parent dir or the missing subdir
	// (we didn't even get far enough to create one, but verify
	// defensively).
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			assert.NotContains(t, e.Name(), ".tmp-", "no .tmp orphan should exist in parent dir")
		}
	}
}

func TestAtomicWriteFile_PreservesExistingOnFailure(t *testing.T) {
	// Similar to NoPartialOnTmpFailure but with a pre-existing target:
	// when the write fails, the previous content must remain intact.
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	require.NoError(t, os.WriteFile(target, []byte("KEEP_ME"), 0600))

	// Force failure by making the parent path resolve to something
	// that can't be written: replace `dir` with a path that has a
	// regular-file segment in the middle.
	fileAsParent := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(fileAsParent, []byte("not a dir"), 0600))
	badTarget := filepath.Join(fileAsParent, "child.json")

	err := atomicWriteFile(badTarget, []byte("NEW"), 0600)
	require.Error(t, err, "writing under a non-directory should error")

	// Pre-existing real.json should be untouched.
	got, err := os.ReadFile(target) // #nosec G304 — test path.
	require.NoError(t, err)
	assert.Equal(t, []byte("KEEP_ME"), got, "pre-existing target must be intact on unrelated failure")
}

func TestAtomicWriteFile_CleansTmpOnError(t *testing.T) {
	// Simulate a mid-write failure by pre-creating the .tmp path AS A
	// DIRECTORY so the open (which expects a regular file) fails.
	// However the helper uses pid+nanos in the .tmp suffix to avoid
	// exactly this kind of collision, so to make the test deterministic
	// we instead point at a parent path the OS can't write into and
	// verify the dir is clean after.
	dir := t.TempDir()

	// Make the dir read-only on POSIX so the temp file create fails.
	// On Windows the chmod is a no-op so the test of the cleanup path
	// is best-effort there; skip. Root bypasses POSIX permissions too.
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 isn't enforced on Windows the same way")
	}
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test: root bypasses file permission restrictions")
	}

	require.NoError(t, os.Chmod(dir, 0500))
	// Restore perms so t.TempDir cleanup can remove the directory.
	defer func() { _ = os.Chmod(dir, 0700) }()

	target := filepath.Join(dir, "out.bin")
	err := atomicWriteFile(target, []byte("data"), 0600)
	require.Error(t, err, "write into read-only directory should error")

	// Re-permission so we can list contents for the assertion.
	require.NoError(t, os.Chmod(dir, 0700))
	orphans := listTmpOrphans(t, dir)
	assert.Empty(t, orphans, "no .tmp orphan should remain after a failure during tmp creation")

	// Target should not have been created either.
	_, statErr := os.Stat(target)
	assert.True(t, os.IsNotExist(statErr), "target should not exist after failure")
}

func TestAtomicWriteFile_UniqueSuffixAvoidsCollision(t *testing.T) {
	// Two concurrent atomicWriteFile calls to the SAME target with
	// independent suffixes should both succeed (one wins via rename,
	// the other rewrites on top — the order is racy but neither write
	// should error and the final content must equal one of the two
	// inputs). Catches the failure mode where a fixed ".tmp" suffix
	// would have one call clobber the other's in-flight temp.
	dir := t.TempDir()
	target := filepath.Join(dir, "config.md")

	var wg sync.WaitGroup
	results := make([]error, 2)
	payloads := [][]byte{[]byte("CONTENT_A"), []byte("CONTENT_B")}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = atomicWriteFile(target, payloads[i], 0600)
		}(i)
	}
	wg.Wait()

	for i, err := range results {
		assert.NoError(t, err, "concurrent atomicWriteFile call %d should succeed", i)
	}

	got, err := os.ReadFile(target) // #nosec G304 — test path.
	require.NoError(t, err)
	assert.True(t, string(got) == "CONTENT_A" || string(got) == "CONTENT_B",
		"final content must be one of the two inputs, got %q", string(got))

	assert.Empty(t, listTmpOrphans(t, dir), "no .tmp orphan should remain after concurrent writes")
}

// TestSyncDir_NonExistentPath verifies that syncDir returns an error when the
// path does not exist (covers the os.Open error return branch on non-Windows).
func TestSyncDir_NonExistentPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("syncDir is a no-op on Windows")
	}
	err := syncDir(filepath.Join(t.TempDir(), "nonexistent-subdir"))
	assert.Error(t, err, "syncDir on missing path must return error")
}

// TestAtomicWriteFile_RenameTargetIsDir covers the os.Rename error path:
// if the final target path already exists as a directory, Rename fails on
// POSIX (EISDIR). The temp file must be cleaned up — no orphan should remain.
func TestAtomicWriteFile_RenameTargetIsDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Rename semantics differ on Windows")
	}
	dir := t.TempDir()
	// Create a directory at the intended target path.
	targetDir := filepath.Join(dir, "target")
	require.NoError(t, os.Mkdir(targetDir, 0700))

	err := atomicWriteFile(targetDir, []byte("data"), 0600)
	require.Error(t, err, "atomicWriteFile must fail when target is an existing directory")

	// No orphan .tmp file should remain after the Rename failure.
	assert.Empty(t, listTmpOrphans(t, dir), "no .tmp orphan should remain after Rename failure")
}

// TestAtomicWriteFile_NoDirComponent covers the `if dir == "" { dir = "." }`
// branch (line 77-79). filepath.Split returns an empty dir for a bare filename
// with no path separator, so we chdir to a temp dir and call with a plain name.
func TestAtomicWriteFile_NoDirComponent(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root may bypass expected behaviour in temp dir")
	}
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	data := []byte("hello")
	require.NoError(t, atomicWriteFile("bare-name.txt", data, 0600),
		"atomicWriteFile with no dir component must succeed (falls back to '.')")

	got, err := os.ReadFile(filepath.Join(tmpDir, "bare-name.txt")) // #nosec G304
	require.NoError(t, err)
	assert.Equal(t, data, got)
	assert.Empty(t, listTmpOrphans(t, tmpDir))
}

func TestSaveCompetition_NoPartialOnSuccess(t *testing.T) {
	// End-to-end sanity check: a real Save through the public API
	// should leave the competition dir without .tmp orphan files.
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	c := &Competition{
		ID:   "comp-atomic",
		Name: "Atomicity Test",
	}
	_, err = store.SaveCompetitionChanged(c)
	require.NoError(t, err)

	compDir := store.compPath("comp-atomic")
	entries, err := os.ReadDir(compDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp-",
			"competition dir must not contain .tmp orphans, found %s", e.Name())
	}

	// config.md must be present and parseable.
	loaded, err := store.LoadCompetition("comp-atomic")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "Atomicity Test", loaded.Name)
}
