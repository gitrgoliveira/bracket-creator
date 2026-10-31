package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTestRoot opens a dataRoot on dir, for a test that writes without a Store.
func openTestRoot(t *testing.T, dir string) *dataRoot {
	t.Helper()
	root, err := openDataRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.close()) })
	return root
}

// A symlink inside the data folder that points out of it is refused by the
// operating system, whatever the path the store built looks like.
func TestDataRoot_RefusesASymlinkOutOfTheDataFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)

	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "config.md"), []byte("---\nid: evil\n---\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "competitions", "evil")))

	path := s.compPath("evil", "config.md")
	_, err = s.root.readFile(path)
	require.Error(t, err)
	var pe *fs.PathError
	require.True(t, errors.As(err, &pe), "a refused read is a PathError: %v", err)
	assert.Equal(t, path, pe.Path, "the error names the path the store built")

	_, err = s.root.openFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	require.Error(t, err, "a write through the link is refused too")
}

// A data folder that is itself a symlink is followed when the root is opened,
// so the store works on the folder it points at.
func TestDataRoot_FollowsASymlinkedDataFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.Symlink(target, link))

	s, err := NewStore(link)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))

	got, err := s.LoadCompetition("c1")
	require.NoError(t, err)
	assert.Equal(t, "C1", got.Name)
	_, err = os.Stat(filepath.Join(target, "competitions", "c1", "config.md"))
	assert.NoError(t, err, "the file lands in the folder the link points at")
}

// An error from the root names the file as the store built it, not the
// folder-relative name the root was given.
func TestDataRoot_ErrorsNameThePathTheStoreBuilt(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)

	missing := filepath.Join(dir, "competitions", "nope.json")
	_, err := root.readFile(missing)
	require.ErrorIs(t, err, fs.ErrNotExist)
	var pe *fs.PathError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, missing, pe.Path)

	err = root.rename(missing, filepath.Join(dir, "other.json"))
	require.ErrorIs(t, err, fs.ErrNotExist)
	var le *os.LinkError
	require.True(t, errors.As(err, &le))
	assert.Equal(t, missing, le.Old)
	assert.Equal(t, filepath.Join(dir, "other.json"), le.New)
}

// An error from the root reads word for word as the same call by path did
// before the store had a root: "open <path>: ...", never "openat <name>: ...".
func TestDataRoot_ErrorsNameTheOperationAsByPath(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)
	missing := filepath.Join(dir, "competitions", "nope")
	underMissing := filepath.Join(missing, "child")

	for _, c := range []struct {
		op   string
		call func() error
	}{
		{"open", func() error { _, err := root.readFile(missing); return err }},
		{"open", func() error { _, err := root.open(missing); return err }},
		{"open", func() error { _, err := root.openFile(underMissing, os.O_WRONLY|os.O_CREATE, 0o600); return err }},
		{"open", func() error { _, err := root.readDir(missing); return err }},
		{"stat", func() error { _, err := root.stat(missing); return err }},
		{"mkdir", func() error { return root.mkdir(underMissing, 0o700) }},
		{"remove", func() error { return root.remove(missing) }},
	} {
		err := c.call()
		require.ErrorIs(t, err, fs.ErrNotExist)
		var pe *fs.PathError
		require.True(t, errors.As(err, &pe), "%v", err)
		assert.Equal(t, c.op, pe.Op, "%v", err)
		assert.Contains(t, err.Error(), c.op+" "+filepath.Join(dir, "competitions")+string(filepath.Separator), "%v", err)
	}

	err := root.rename(missing, filepath.Join(dir, "other"))
	var le *os.LinkError
	require.True(t, errors.As(err, &le), "%v", err)
	assert.Equal(t, "rename", le.Op)
	assert.Equal(t, "rename "+missing+" "+filepath.Join(dir, "other")+": "+le.Err.Error(), err.Error())
}

// A path that cannot be put relative to the data folder (a relative path
// against an absolute folder) is refused before it reaches the root.
func TestDataRoot_RefusesAPathOutsideTheFolder(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)
	const rel = "competitions/c1/config.md"

	_, err := root.readFile(rel)
	require.Error(t, err)
	var pe *fs.PathError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, rel, pe.Path)

	_, err = root.open(rel)
	assert.Error(t, err)
	_, err = root.openFile(rel, os.O_RDONLY, 0)
	assert.Error(t, err)
	_, err = root.stat(rel)
	assert.Error(t, err)
	_, err = root.readDir(rel)
	assert.Error(t, err)
	assert.Error(t, root.mkdir(rel, 0o700))
	assert.Error(t, root.mkdirAll(rel, 0o700))
	assert.Error(t, root.remove(rel))
	assert.Error(t, root.removeAll(rel))
	assert.Error(t, root.rename(rel, filepath.Join(dir, "x")))
	assert.Error(t, root.rename(filepath.Join(dir, "x"), rel))

	err = root.rename(filepath.Join(dir, "..", "escape"), filepath.Join(dir, "x"))
	assert.Error(t, err, "a name that climbs out of the folder is refused by the root")
}
