package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenOutputFile_Error(t *testing.T) {
	// Try to open a file in a non-existent directory
	f, w, err := openOutputFile("/non/existent/dir/output.xlsx")
	assert.Error(t, err)
	assert.Nil(t, f)
	assert.Nil(t, w)
}

func TestOpenOutputFile_TruncatesExisting(t *testing.T) {
	// Regression: openOutputFile used O_APPEND, so re-running a generator with
	// the same -o path appended a second complete workbook to the first,
	// doubling the file each run.
	path := filepath.Join(t.TempDir(), "output.xlsx")
	require.NoError(t, os.WriteFile(path, []byte("stale previous workbook contents"), 0600))

	f, w, err := openOutputFile(path)
	require.NoError(t, err)
	_, err = w.WriteString("new")
	require.NoError(t, err)
	require.NoError(t, w.Flush())
	require.NoError(t, f.Close())

	got, err := os.ReadFile(path) // #nosec G304, temp-dir path
	require.NoError(t, err)
	assert.Equal(t, "new", string(got), "existing file must be truncated, not appended to")
}

func TestProcessEntries_Success(t *testing.T) {
	entries := []string{"John Doe,Dojo1", "Jane Smith,Dojo2"}
	players, err := processEntries(entries, true, false)
	assert.NoError(t, err)
	assert.Len(t, players, 2)
	assert.Equal(t, "John Doe", players[0].Name)
}

func TestProcessEntries_Shuffle(t *testing.T) {
	entries := []string{"1,D1", "2,D2", "3,D3", "4,D4", "5,D5", "6,D6", "7,D7", "8,D8", "9,D9", "10,D10"}
	// This might flakes if shuffle result matches original, but with 10 it's unlikely
	players, err := processEntries(entries, false, false)
	assert.NoError(t, err)
	assert.Len(t, players, 10)
}

func TestProcessEntries_DuplicateError(t *testing.T) {
	entries := []string{"John Doe,Dojo1", "John Doe,Dojo1"}
	players, err := processEntries(entries, true, false)
	assert.Error(t, err)
	assert.Nil(t, players)
	assert.Contains(t, err.Error(), "duplicate participant entries found")
}
