package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTournamentPauses_LoadTournamentMigratesLunchBlock(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	content := `---
name: National Championship
date: 01-08-2026
venue: Tokyo Budokan
lunch_block: 1h
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(content), 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	expected := []Pause{
		{
			Name:   "Lunch",
			Start:  "12:00",
			Length: "1h",
			Day:    0,
		},
	}
	assert.Equal(t, expected, tourn.Pauses)
	assert.Empty(t, tourn.LunchBlockLegacy)
}

func TestTournamentPauses_UpdateTournamentChangedSeesMigratedLunchBlock(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	content := `---
name: National Championship
date: 01-08-2026
venue: Tokyo Budokan
lunch_block: 1h
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(content), 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	var seenPauses []Pause
	var seenLegacy string
	_, err = store.UpdateTournamentChanged(&Tournament{}, func(current *Tournament, desired *Tournament) error {
		require.NotNil(t, current)
		seenPauses = current.Pauses
		seenLegacy = current.LunchBlockLegacy
		*desired = *store.copyTournament(current)
		return nil
	})
	require.NoError(t, err)

	expected := []Pause{
		{
			Name:   "Lunch",
			Start:  "12:00",
			Length: "1h",
			Day:    0,
		},
	}
	assert.Equal(t, expected, seenPauses)
	assert.Empty(t, seenLegacy)
}

func TestTournamentPauses_SaveDropsLunchBlockKeyAndHasPauses(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	content := `---
name: National Championship
date: 01-08-2026
venue: Tokyo Budokan
lunch_block: 1h
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(content), 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	require.NoError(t, store.SaveTournament(tourn))

	raw, err := os.ReadFile(tournPath)
	require.NoError(t, err)
	text := string(raw)

	assert.NotContains(t, text, "lunch_block")
	assert.Contains(t, text, "pauses:")
	assert.Contains(t, text, "name: Lunch")
	assert.Contains(t, text, "12:00")
}

func TestTournamentPauses_BothLunchBlockAndPausesKeepsOnlyStoredPauses(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	content := `---
name: National Championship
date: 01-08-2026
venue: Tokyo Budokan
lunch_block: 1h
pauses:
  - name: Coffee Break
    start: "10:30"
    length: "15m"
    day: 1
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(content), 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	expected := []Pause{
		{
			Name:   "Coffee Break",
			Start:  "10:30",
			Length: "15m",
			Day:    1,
		},
	}
	assert.Equal(t, expected, tourn.Pauses)
	assert.Empty(t, tourn.LunchBlockLegacy)

	require.NoError(t, store.SaveTournament(tourn))
	raw, err := os.ReadFile(tournPath)
	require.NoError(t, err)
	text := string(raw)
	assert.NotContains(t, text, "lunch_block")
	assert.NotContains(t, text, "Lunch")
}

// A lunch v2.3 scheduled as nothing (zero or negative) is dropped with no loss.
// One time.ParseDuration cannot read is kept, never deleted:
// TestTournamentUpgrade_UnreadableCeremonyLengthIsKept.
func TestTournamentPauses_ZeroLunchBlockAddsNothingAndDropsKey(t *testing.T) {
	for _, tc := range []struct {
		name       string
		lunchBlock string
	}{
		{name: "zero", lunchBlock: "0m"},
		{name: "negative", lunchBlock: "-15m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tournPath := filepath.Join(dir, "tournament.md")
			content := "---\nname: Test\ndate: 01-08-2026\nlunch_block: " + tc.lunchBlock + "\n---\n"
			require.NoError(t, os.WriteFile(tournPath, []byte(content), 0o600))

			store, err := NewStore(dir)
			require.NoError(t, err)

			tourn, err := store.LoadTournament()
			require.NoError(t, err)
			require.NotNil(t, tourn)

			assert.Empty(t, tourn.Pauses)
			assert.Empty(t, tourn.LunchBlockLegacy)

			require.NoError(t, store.SaveTournament(tourn))
			raw, err := os.ReadFile(tournPath)
			require.NoError(t, err)
			assert.NotContains(t, string(raw), "lunch_block")
		})
	}
}

func TestTournamentPauses_CopyTournamentDeepCopiesPauses(t *testing.T) {
	store := &Store{}
	orig := &Tournament{
		Name: "Test",
		Pauses: []Pause{
			{Name: "Lunch", Start: "12:00", Length: "1h", Day: 0},
		},
	}

	cp := store.copyTournament(orig)
	require.NotNil(t, cp)
	require.Len(t, cp.Pauses, 1)

	cp.Pauses[0].Name = "Grading"
	assert.Equal(t, "Lunch", orig.Pauses[0].Name, "modifying copy slice must not mutate original")
	assert.Equal(t, "Grading", cp.Pauses[0].Name)
}
