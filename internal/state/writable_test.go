package state

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The server refuses to start when it cannot write to its tournament data
// (operator decision 2026-10-04: "test permissions on load"). A folder it
// cannot write is found at startup, not as a score write failing mid-match.
func TestNewStoreRefusesDataItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes through any folder mode")
	}
	for _, tc := range []struct {
		name string
		dir  func(root string) string
	}{
		{"a competition folder", func(root string) string { return filepath.Join(root, "competitions", "individual") }},
		{"a competition's history folder", func(root string) string {
			return filepath.Join(root, "competitions", "individual", matchHistoryDir)
		}},
		{"a competition's score event log folder", func(root string) string {
			return filepath.Join(root, "competitions", "individual", scoreLogDir)
		}},
		{"the transaction log folder", func(root string) string { return filepath.Join(root, ".wal") }},
		{"the branding folder", func(root string) string { return filepath.Join(root, BrandingDirName) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			_, err := NewStore(root)
			require.NoError(t, err)
			dir := tc.dir(root)
			require.NoError(t, os.MkdirAll(dir, 0o700))
			require.NoError(t, os.Chmod(dir, 0o500))
			defer func() { _ = os.Chmod(dir, 0o700) }()

			_, err = NewStore(root)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrDataNotWritable))
			assert.Contains(t, err.Error(), dir, "the error names the folder")
			assert.Contains(t, err.Error(), "permission denied")
		})
	}
}

func TestNewStoreLeavesNoProbeBehind(t *testing.T) {
	root := t.TempDir()
	_, err := NewStore(root)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "competitions", "individual", matchHistoryDir), 0o700))
	_, err = NewStore(root)
	require.NoError(t, err)
	require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		require.NoError(t, err)
		assert.NotContains(t, d.Name(), writeProbePrefix, "the write check removes what it creates")
		return nil
	}))
}

// A folder that can be neither listed nor written is named once.
func TestNewStoreNamesAnUnreadableFolderOnce(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads and writes through any folder mode")
	}
	root := t.TempDir()
	_, err := NewStore(root)
	require.NoError(t, err)
	comps := filepath.Join(root, "competitions")
	require.NoError(t, os.Chmod(comps, 0o000))
	defer func() { _ = os.Chmod(comps, 0o700) }()

	_, err = NewStore(root)
	require.ErrorIs(t, err, ErrDataNotWritable)
	assert.Equal(t, 1, strings.Count(err.Error(), comps+":"), err.Error())
}

// A folder the server saves into that it cannot use is named at startup with
// the remedy that fits why: write access for a folder it cannot write, moving
// a file away when the file sits where the folder should be, and the folder
// itself in place of a link out of the data folder (every save through it
// would be refused, "path escapes from parent").
func TestNewStoreGivesTheRemedyThatFitsTheCause(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges and chmod is not enforced on Windows")
	}
	const (
		access = "give the user running the server write access to it"
		file   = "a file is where this folder should be: move the file away"
		link   = "it links out of the data folder (a link by an absolute path counts, wherever it points)"
	)
	for _, c := range []struct {
		name   string
		entry  string
		make   func(t *testing.T, path string)
		cause  string
		remedy string
	}{
		{"a folder it cannot write", ".wal", func(t *testing.T, path string) {
			if os.Geteuid() == 0 {
				t.Skip("root writes through any folder mode")
			}
			require.NoError(t, os.Chmod(path, 0o500))
			t.Cleanup(func() { require.NoError(t, os.Chmod(path, 0o700)) })
		}, "permission denied", access},
		{"a link inside the data folder to a folder it cannot write", ".wal", func(t *testing.T, path string) {
			if os.Geteuid() == 0 {
				t.Skip("root writes through any folder mode")
			}
			target := filepath.Join(filepath.Dir(path), "read-only")
			require.NoError(t, os.Mkdir(target, 0o500))
			t.Cleanup(func() { require.NoError(t, os.Chmod(target, 0o700)) })
			require.NoError(t, os.RemoveAll(path))
			require.NoError(t, os.Symlink("read-only", path))
		}, "permission denied", access},
		{"a link inside the data folder written as an absolute path", ".wal", func(t *testing.T, path string) {
			target := filepath.Join(filepath.Dir(path), "elsewhere")
			require.NoError(t, os.Mkdir(target, 0o700))
			require.NoError(t, os.RemoveAll(path))
			require.NoError(t, os.Symlink(target, path))
		}, "path escapes from parent", link},
		{"a file where .wal should be", ".wal", func(t *testing.T, path string) {
			require.NoError(t, os.RemoveAll(path))
			require.NoError(t, os.WriteFile(path, nil, 0o600))
		}, "not a directory", file},
		{"a file where competitions should be", "competitions", func(t *testing.T, path string) {
			require.NoError(t, os.RemoveAll(path))
			require.NoError(t, os.WriteFile(path, nil, 0o600))
		}, "not a directory", file},
		{".wal linked out of the data folder", ".wal", func(t *testing.T, path string) {
			require.NoError(t, os.RemoveAll(path))
			require.NoError(t, os.Symlink(t.TempDir(), path))
		}, "path escapes from parent", link},
		{"a history folder linked out of the data folder", filepath.Join("competitions", "c1", matchHistoryDir), func(t *testing.T, path string) {
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.Symlink(t.TempDir(), path))
		}, "path escapes from parent", link},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := NewStore(dir)
			require.NoError(t, err)
			path := filepath.Join(dir, c.entry)
			c.make(t, path)

			_, err = NewStore(dir)
			require.ErrorIs(t, err, ErrDataNotWritable)
			assert.Contains(t, err.Error(), "  "+path+": "+c.cause+"; "+c.remedy, err.Error())
			assert.Equal(t, 1, strings.Count(err.Error(), "  "+path+":"), "each failing folder is named once")
		})
	}
}

// The write check probes the competition folders the store lists, so a link
// under competitions/, which ListCompetitions skips, does not stop the server
// starting, wherever it points.
func TestNewStoreStartsBesideALinkUnderCompetitions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	_, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(dir, "competitions", "elsewhere")))

	s, err := NewStore(dir)
	require.NoError(t, err)
	ids, err := s.ListCompetitions()
	require.NoError(t, err)
	assert.NotContains(t, ids, "elsewhere")
}
