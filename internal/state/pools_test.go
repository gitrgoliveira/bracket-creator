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

// TestPoolMatches_SideIDsRoundTrip verifies the appended SideAID/SideBID
// columns survive a save→load cycle (the league matrix relies on them to
// disambiguate same-name participants from different dojos).
func TestPoolMatches_SideIDsRoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	matches := []MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka Kenji", SideB: "Yamamoto Yuki", SideAID: "uuid-a", SideBID: "uuid-b", Status: MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	results, err := store.LoadPoolMatchesLocked(compID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "uuid-a", results[0].SideAID)
	assert.Equal(t, "uuid-b", results[0].SideBID)
}

// TestPoolMatches_LegacyFileWithoutIDs verifies a pool-matches.csv written
// before the id columns existed (15 columns) still loads, leaving the id
// fields empty so consumers fall back to name matching.
func TestPoolMatches_LegacyFileWithoutIDs(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "legacy-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Legacy"}))

	// 15-column legacy row (header + one data row), no SideAID/SideBID.
	legacy := "PoolName,MatchIdx,SideA,SideB,Winner,IpponsA,IpponsB,HansokuA,HansokuB,Decision,Status,Court,SubResults,ScheduledAt,ResultSource\n" +
		"Pool A,0,Alice,Bob,,,,0,0,,scheduled,A,,09:00,\n"
	require.NoError(t, os.WriteFile(store.compPath(compID, "pool-matches.csv"), []byte(legacy), 0600))

	results, err := store.LoadPoolMatchesLocked(compID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Alice", results[0].SideA)
	assert.Empty(t, results[0].SideAID, "legacy row has no id column → empty")
	assert.Empty(t, results[0].SideBID)
}

// TestPools_PlayerIDRoundTrip verifies the appended participant-id column in
// pools.csv survives a save→load cycle so pool.players carry .ID for the
// league matrix.
func TestPools_PlayerIDRoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	pools := []helper.Pool{{
		PoolName: "Pool A",
		Players: []helper.Player{
			{Name: "Tanaka Kenji", Dojo: "Tokyo", ID: "uuid-1"},
			{Name: "Yamamoto Yuki", Dojo: "Osaka", ID: "uuid-2"},
		},
	}}
	require.NoError(t, store.SavePools(compID, pools))

	loaded, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Len(t, loaded[0].Players, 2)
	assert.Equal(t, "uuid-1", loaded[0].Players[0].ID)
	assert.Equal(t, "uuid-2", loaded[0].Players[1].ID)
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

	hantei := true
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
			Encho:           &EnchoMetadata{PeriodCount: 1},
			DecidedByHantei: &hantei,
			Status:          MatchStatusCompleted,
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Len(t, loaded[0].SubResults, 1)
	assert.Equal(t, "A1", loaded[0].SubResults[0].Winner)
	assert.Equal(t, "M", loaded[0].IpponsA[0])

	// Deep-copy isolation: mutating the returned value (including the Encho
	// and DecidedByHantei pointers and the nested slices) in place must not
	// corrupt the store's cached copy. Mirrors copyMatchResults' deep-copy.
	loaded[0].IpponsA[0] = "MUTATED"
	loaded[0].SubResults[0].SideA = "MUTATED"
	loaded[0].SubResults[0].IpponsA[0] = "MUTATED"
	loaded[0].Encho.PeriodCount = 99
	*loaded[0].DecidedByHantei = false

	fresh, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, fresh, 1)
	assert.Equal(t, "M", fresh[0].IpponsA[0])
	assert.Equal(t, "A1", fresh[0].SubResults[0].SideA)
	assert.Equal(t, "M", fresh[0].SubResults[0].IpponsA[0])
	require.NotNil(t, fresh[0].Encho)
	assert.Equal(t, 1, fresh[0].Encho.PeriodCount)
	require.NotNil(t, fresh[0].DecidedByHantei)
	assert.True(t, *fresh[0].DecidedByHantei)
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

// TestParsePoolsFile_DrawOrderSort verifies that parsePoolsFile restores
// draw order from the persisted col-2 position even when CSV rows are
// written in a different sequence.
func TestParsePoolsFile_DrawOrderSort(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-draw-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	// Write a pools.csv where rows appear out of draw order (P2 before P1).
	// The draw-position column (col 2) records the original 0-indexed order.
	compDir := dir + "/competitions/sort-test"
	require.NoError(t, os.MkdirAll(compDir, 0700))
	csv := "Pool A,P2,1,,,0,\nPool A,P1,0,,,0,\n"
	require.NoError(t, os.WriteFile(compDir+"/pools.csv", []byte(csv), 0600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	// Fake competition so the store doesn't reject the compID.
	require.NoError(t, store.SaveCompetition(&Competition{ID: "sort-test", Name: "Sort Test"}))

	loaded, err := store.LoadPools("sort-test")
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	// P1 (draw pos 0 → PoolPosition 1) must appear before P2 (draw pos 1 → PoolPosition 2)
	require.Len(t, loaded[0].Players, 2)
	assert.Equal(t, "P1", loaded[0].Players[0].Name, "P1 should be first (draw pos 0)")
	assert.Equal(t, "P2", loaded[0].Players[1].Name, "P2 should be second (draw pos 1)")
	assert.Equal(t, int64(1), loaded[0].Players[0].PoolPosition)
	assert.Equal(t, int64(2), loaded[0].Players[1].PoolPosition)
}

// TestParsePoolsFile_InvalidPosition verifies that negative or non-integer
// col-2 values fall back to 1-based append order rather than corrupting
// draw order.
func TestParsePoolsFile_InvalidPosition(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-inv-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	compDir := dir + "/competitions/inv-test"
	require.NoError(t, os.MkdirAll(compDir, 0700))
	// Row 1: negative position (-1) → must use fallback (append order = 1)
	// Row 2: non-integer ("x") → must use fallback (append order = 2)
	// Row 3: valid position (2) → PoolPosition 3 (pos+1)
	csv := "Pool A,P1,-1,,,0,\nPool A,P2,x,,,0,\nPool A,P3,2,,,0,\n"
	require.NoError(t, os.WriteFile(compDir+"/pools.csv", []byte(csv), 0600))

	store, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "inv-test", Name: "Inv Test"}))

	loaded, err := store.LoadPools("inv-test")
	require.NoError(t, err)
	require.Len(t, loaded[0].Players, 3)
	// P1 and P2 get append-order defaults (1, 2); P3 gets pos+1=3.
	// Stable sort: 1 < 2 < 3, so order is P1, P2, P3 — row order preserved.
	assert.Equal(t, "P1", loaded[0].Players[0].Name)
	assert.Equal(t, int64(1), loaded[0].Players[0].PoolPosition)
	assert.Equal(t, "P2", loaded[0].Players[1].Name)
	assert.Equal(t, int64(2), loaded[0].Players[1].PoolPosition)
	assert.Equal(t, "P3", loaded[0].Players[2].Name)
	assert.Equal(t, int64(3), loaded[0].Players[2].PoolPosition)
}

// TestParsePoolsFile_LegacyNoCol2 verifies that legacy CSV files without a
// draw-position column (col 2) load in row order via stable sort on the
// 1-based append-order defaults.
func TestParsePoolsFile_LegacyNoCol2(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-pools-test-legacy-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	compDir := dir + "/competitions/legacy-test"
	require.NoError(t, os.MkdirAll(compDir, 0700))
	// Only pool name + player name columns — no draw-position column.
	csv := "Pool A,Alice\nPool A,Bob\nPool A,Charlie\n"
	require.NoError(t, os.WriteFile(compDir+"/pools.csv", []byte(csv), 0600))

	store, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "legacy-test", Name: "Legacy"}))

	loaded, err := store.LoadPools("legacy-test")
	require.NoError(t, err)
	require.Len(t, loaded[0].Players, 3)
	// Append-order defaults are unique (1, 2, 3), so row order is preserved.
	assert.Equal(t, "Alice", loaded[0].Players[0].Name)
	assert.Equal(t, "Bob", loaded[0].Players[1].Name)
	assert.Equal(t, "Charlie", loaded[0].Players[2].Name)
}
