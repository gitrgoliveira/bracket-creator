package state

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPoolMatchesLocked_MissingFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	// No pool-matches.csv exists; must return empty slice (not error)
	results, err := store.LoadPoolMatchesLocked(compID)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestLoadPoolMatchesLocked_WithData(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	matches := []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
		{ID: "P1-1", SideA: "Charlie", SideB: "Dave", Status: MatchStatusCompleted, Winner: "Charlie"},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	results, err := store.LoadPoolMatchesLocked(compID)
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.Equal(t, "Alice", results[0].SideA)
	assert.Equal(t, "Charlie", results[1].Winner)
}

func TestLoadPoolMatchesLocked_InvalidCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	_, err = store.LoadPoolMatchesLocked("../bad")
	assert.Error(t, err)
}

func TestUpdatePoolMatchByID_Basic(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	matches := []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
		{ID: "P1-1", SideA: "Charlie", SideB: "Dave", Status: MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	found, err := store.UpdatePoolMatchByID(compID, "P1-0", func(m *MatchResult) {
		m.Winner = "Alice"
		m.Status = MatchStatusCompleted
	})
	require.NoError(t, err)
	assert.True(t, found)

	updated, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, updated, 2)
	assert.Equal(t, "Alice", updated[0].Winner)
	assert.Equal(t, MatchStatusCompleted, updated[0].Status)
	// Other match untouched
	assert.Equal(t, MatchStatusScheduled, updated[1].Status)
}

func TestUpdatePoolMatchByID_NotFound(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob"},
	}))

	called := false
	found, err := store.UpdatePoolMatchByID(compID, "nonexistent", func(m *MatchResult) {
		called = true
	})
	require.NoError(t, err)
	assert.False(t, found)
	assert.False(t, called)
}

func TestUpdatePoolMatchByID_InvalidCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	_, err = store.UpdatePoolMatchByID("../bad", "M1", func(m *MatchResult) {})
	assert.Error(t, err)
}

func TestParsePoolMatchesBytes_Empty(t *testing.T) {
	results, err := parsePoolMatchesBytes(nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestParsePoolMatchesBytes_ZeroLength(t *testing.T) {
	results, err := parsePoolMatchesBytes([]byte{})
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestParsePoolMatchesBytes_WithData(t *testing.T) {
	// Write a valid pool-matches CSV into a temp file, read it back,
	// then re-parse via the bytes path to confirm both parsers agree.
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	matches := []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	raw, err := os.ReadFile(store.compPath(compID, "pool-matches.csv"))
	require.NoError(t, err)

	results, err := parsePoolMatchesBytes(raw)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Alice", results[0].SideA)
}

// TestParsePoolMatchesBytes_MalformedCSV covers the csv.ReadAll error path
// in parsePoolMatchesBytes: a bare-quote in a field forces csv.ErrBareQuote.
func TestParsePoolMatchesBytes_MalformedCSV(t *testing.T) {
	// A bare (unescaped) double-quote inside a non-quoted field is a CSV error.
	malformed := []byte("P1,0,Alice,Bob,Alice,M||,,,fought,completed,A\na,\"bad\nquote")
	_, err := parsePoolMatchesBytes(malformed)
	assert.Error(t, err)
}

func TestLoadPools_MissingFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "pools-missing"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.Empty(t, pools)
}

func TestLoadPools_RoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "pools-rt"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "RT"}))

	pools := []helper.Pool{
		{
			PoolName: "Pool A",
			Players: []helper.Player{
				{Name: "Alice", Dojo: "DojoA", Seed: 1},
				{Name: "Bob", Dojo: "DojoB", Seed: 0},
			},
		},
		{
			PoolName: "Pool B",
			Players: []helper.Player{
				{Name: "Charlie", Dojo: "DojoC"},
			},
		},
	}
	require.NoError(t, store.SavePools(compID, pools))

	loaded, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, "Pool A", loaded[0].PoolName)
	assert.Len(t, loaded[0].Players, 2)
	assert.Equal(t, "Alice", loaded[0].Players[0].Name)
	assert.Equal(t, 1, loaded[0].Players[0].Seed)
	assert.Equal(t, "Pool B", loaded[1].PoolName)
	assert.Len(t, loaded[1].Players, 1)
}

func TestLoadPools_InvalidCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	_, err = store.LoadPools("../bad")
	assert.Error(t, err)
}

func TestLoadPools_FreshStore(t *testing.T) {
	// Use a separate store to write, then load via a fresh store so
	// loadCached doesn't skip parsePoolsFile (the write-store populates
	// the cache; a fresh store has no cache and must parse the file).
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	writeStore, err := NewStore(dir)
	require.NoError(t, err)

	compID := "fresh-pools"
	require.NoError(t, writeStore.SaveCompetition(&Competition{ID: compID, Name: "Fresh"}))

	pools := []helper.Pool{
		{
			PoolName: "Pool X",
			Players: []helper.Player{
				{Name: "Alice", Dojo: "DojoA", Seed: 2, Number: "1", DisplayName: "A. Smith"},
				{Name: "Bob", Dojo: "DojoB"},
			},
		},
	}
	require.NoError(t, writeStore.SavePools(compID, pools))

	// Fresh store — no cache, parsePoolsFile will be called
	readStore, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := readStore.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Pool X", loaded[0].PoolName)
	require.Len(t, loaded[0].Players, 2)
	assert.Equal(t, "Alice", loaded[0].Players[0].Name)
	assert.Equal(t, 2, loaded[0].Players[0].Seed)
	assert.Equal(t, "1", loaded[0].Players[0].Number)
	assert.Equal(t, "A. Smith", loaded[0].Players[0].DisplayName)
}

func TestLoadPoolMatches_FreshStore(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	writeStore, err := NewStore(dir)
	require.NoError(t, err)

	compID := "fresh-pm"
	require.NoError(t, writeStore.SaveCompetition(&Competition{ID: compID, Name: "Fresh PM"}))

	matches := []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusCompleted, Winner: "Alice",
			IpponsA: []string{"M"}, IpponsB: []string{}, Court: "A"},
	}
	require.NoError(t, writeStore.SavePoolMatches(compID, matches))

	readStore, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := readStore.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Alice", loaded[0].Winner)
}

func TestCopyMatchResults_WithSubResults(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "pools-sub"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Sub"}))

	matches := []MatchResult{
		{
			ID:      "P1-0",
			SideA:   "TeamA",
			SideB:   "TeamB",
			IpponsA: []string{"M", "K"},
			IpponsB: []string{"D"},
			SubResults: []SubMatchResult{
				{
					Position: 1,
					SideA:    "A1",
					SideB:    "B1",
					IpponsA:  []string{"M"},
					IpponsB:  []string{},
					Winner:   "A1",
				},
			},
			Status: MatchStatusCompleted,
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Len(t, loaded[0].SubResults, 1)
	assert.Equal(t, "A1", loaded[0].SubResults[0].Winner)
	assert.Equal(t, "M", loaded[0].IpponsA[0])
}

func TestLoadPoolMatches_InvalidCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	_, err = store.LoadPoolMatches("../bad")
	assert.Error(t, err)
}
