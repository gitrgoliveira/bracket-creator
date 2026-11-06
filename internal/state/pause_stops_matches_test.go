package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pause saved before the "Matches stop for this pause" box existed carries
// no matches_go_on key and still stops matches; a save writes no key for it,
// so an old tournament.md needs no write-back.
func TestPause_SavedWithoutTheFieldStopsMatchesAndSavesWithoutIt(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	content := `---
name: Cup
date: 01-08-2026
venue: Hall
pauses:
  - name: Lunch
    start: "12:00"
    length: 1h
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(content), 0o600))
	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.Len(t, tourn.Pauses, 1)
	assert.True(t, tourn.Pauses[0].StopsMatches())

	require.NoError(t, store.SaveTournament(tourn))
	raw, err := os.ReadFile(tournPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "matches_go_on")
}

// An unticked pause round-trips through tournament.md and the wire.
func TestPause_MatchesGoOnRoundTrips(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&Tournament{
		Name: "Cup",
		Pauses: []Pause{
			{Name: "Opening ceremony", Start: "09:00", Length: "30m", MatchesGoOn: true},
			{Name: "Lunch", Start: "12:00", Length: "1h"},
		},
	}))

	raw, err := os.ReadFile(filepath.Join(dir, "tournament.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "matches_go_on: true")

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.Len(t, tourn.Pauses, 2)
	assert.False(t, tourn.Pauses[0].StopsMatches())
	assert.True(t, tourn.Pauses[1].StopsMatches())

	wire, err := json.Marshal(tourn.Pauses)
	require.NoError(t, err)
	assert.Contains(t, string(wire), `"matchesGoOn":true`)
	var back []Pause
	require.NoError(t, json.Unmarshal(wire, &back))
	assert.Equal(t, tourn.Pauses, back)
}
