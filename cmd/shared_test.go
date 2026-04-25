package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenOutputFile_CreatesFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "output.xlsx")

	f, w, err := openOutputFile(path)
	require.NoError(t, err)
	require.NotNil(t, f)
	require.NotNil(t, w)

	_, writeErr := w.WriteString("hello")
	require.NoError(t, writeErr)
	require.NoError(t, w.Flush())
	require.NoError(t, f.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestOpenOutputFile_InvalidPath(t *testing.T) {
	t.Parallel()

	_, _, err := openOutputFile("/nonexistent/dir/output.xlsx")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open output file")
}

func TestProcessEntries_RejectsDuplicates(t *testing.T) {
	t.Parallel()

	entries := []string{"Alice", "Bob", "Alice", "Charlie"}
	_, err := processEntries(entries, true, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate participant entries")
	assert.Contains(t, err.Error(), "Alice")
}

// TestProcessEntries_StripsBlankLines confirms that empty lines from CSV
// input are silently dropped (they're not "duplicates" to surface to the
// user, just whitespace).
func TestProcessEntries_StripsBlankLines(t *testing.T) {
	t.Parallel()

	entries := []string{"Alice", "", "Bob", "", "Charlie"}
	players, err := processEntries(entries, true, false)
	require.NoError(t, err)
	require.Len(t, players, 3)

	names := make([]string, len(players))
	for i, p := range players {
		names[i] = p.Name
	}
	sort.Strings(names)
	assert.Equal(t, []string{"Alice", "Bob", "Charlie"}, names)
}

func TestProcessEntries_Determined_PreservesOrder(t *testing.T) {
	t.Parallel()

	entries := []string{"Zebra", "Apple", "Mango"}
	players, err := processEntries(entries, true /* determined */, false)
	require.NoError(t, err)
	require.Len(t, players, 3)

	assert.Equal(t, "Zebra", players[0].Name)
	assert.Equal(t, "Apple", players[1].Name)
	assert.Equal(t, "Mango", players[2].Name)
}

func TestProcessEntries_Empty(t *testing.T) {
	t.Parallel()

	players, err := processEntries([]string{}, true, false)
	require.NoError(t, err)
	assert.Empty(t, players)
}
