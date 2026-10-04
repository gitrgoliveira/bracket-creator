package state

import (
	"errors"
	"os"
	"path/filepath"
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
