package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEngiFieldPersists verifies the Engi bool round-trips through YAML
// front-matter and is omitted (omitempty) when false.
func TestEngiFieldPersists(t *testing.T) {
	t.Run("engi true round-trips", func(t *testing.T) {
		original := Competition{ID: "engi-comp", Name: "Engi Test", Engi: true}
		data, err := writeFrontMatter(&original)
		require.NoError(t, err)
		assert.Contains(t, string(data), "engi: true")

		var loaded Competition
		require.NoError(t, parseFrontMatter(data, &loaded))
		assert.True(t, loaded.Engi)
	})

	t.Run("engi absent defaults to false", func(t *testing.T) {
		yamlText := []byte("---\nid: kendo-comp\nname: Kendo Comp\n---\n")
		var c Competition
		require.NoError(t, parseFrontMatter(yamlText, &c))
		assert.False(t, c.Engi)
	})

	t.Run("engi false omitted from YAML", func(t *testing.T) {
		original := Competition{ID: "kendo-comp", Name: "Kendo Comp", Engi: false}
		data, err := writeFrontMatter(&original)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "engi")
	})

	t.Run("engi independent of naginata", func(t *testing.T) {
		original := Competition{ID: "c", Name: "C", Engi: true, Naginata: false}
		data, err := writeFrontMatter(&original)
		require.NoError(t, err)
		var loaded Competition
		require.NoError(t, parseFrontMatter(data, &loaded))
		assert.True(t, loaded.Engi)
		assert.False(t, loaded.Naginata)
	})
}

// TestPoolMatchFlagsPersist verifies FlagsA/FlagsB survive a pool-matches.csv
// round-trip as append-only trailing columns, and that older files lacking the
// columns load them as 0.
func TestPoolMatchFlagsPersist(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "engi-flags"
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Engi", Format: CompFormatLeague, Engi: true,
	}))

	results := []MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			FlagsA: 3, FlagsB: 2, Status: MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Charlie", Winner: "Charlie",
			FlagsA: 0, FlagsB: 5, Status: MatchStatusCompleted},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 2)

	byID := map[string]MatchResult{}
	for _, m := range loaded {
		byID[m.ID] = m
	}
	assert.Equal(t, 3, byID["Pool A-0"].FlagsA)
	assert.Equal(t, 2, byID["Pool A-0"].FlagsB)
	assert.Equal(t, 0, byID["Pool A-1"].FlagsA)
	assert.Equal(t, 5, byID["Pool A-1"].FlagsB)
}

// TestPoolMatchFlagsBackwardCompat verifies a pool-matches.csv written WITHOUT
// the FlagsA/FlagsB columns loads them as 0 (older-file compatibility).
func TestPoolMatchFlagsBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "legacy-flags"
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Legacy", Format: CompFormatLeague,
	}))

	// Write a CSV with only the pre-engi columns (through RepPlayerB, 22 cols).
	header := "PoolName,MatchIdx,SideA,SideB,Winner,IpponsA,IpponsB,HansokuA,HansokuB,Decision,Status,Court,SubResults,ScheduledAt,ResultSource,Round,SideAID,SideBID,WinnerID,CorrectionReason,RepPlayerA,RepPlayerB\n"
	row := "Pool A,0,Alice,Bob,Alice,M,,0,0,,completed,A,,,,,,,,,,\n"
	path := store.compPath(compID, "pool-matches.csv")
	require.NoError(t, store.directWrite(path, []byte(header+row), 0600))

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, 0, loaded[0].FlagsA, "missing FlagsA column → 0")
	assert.Equal(t, 0, loaded[0].FlagsB, "missing FlagsB column → 0")
}
