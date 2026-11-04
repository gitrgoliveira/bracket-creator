package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTournamentUpgrade_TwoDayTournamentWithCeremonies(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	tournContent := `---
name: National Championship
date: 01-08-2026
duration_days: 2
opening_block: 30m
closing_block: 30m
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(tournContent), 0o600))

	writeComp := func(id, date, startTime string) {
		compDir := filepath.Join(dir, "competitions", id)
		require.NoError(t, os.MkdirAll(compDir, 0o700))
		compContent := "---\nid: " + id + "\nname: " + id + "\ndate: " + date + "\nstart_time: \"" + startTime + "\"\n---\n"
		require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(compContent), 0o600))
	}

	writeComp("comp-day1-early", "01-08-2026", "09:30")
	writeComp("comp-day1-late", "01-08-2026", "14:15")
	writeComp("comp-day2", "02-08-2026", "09:30")

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	expectedPauses := []Pause{
		{
			Name:   "Opening ceremony",
			Start:  "09:30",
			Length: "30m",
			Day:    1,
		},
		{
			Name:   "Opening ceremony",
			Start:  "09:30",
			Length: "30m",
			Day:    2,
		},
		{
			Name:   "Closing ceremony",
			Start:  "",
			Length: "30m",
			Day:    0,
		},
	}
	assert.Equal(t, expectedPauses, tourn.Pauses)
	assert.Empty(t, tourn.OpeningBlockLegacy)
	assert.Empty(t, tourn.ClosingBlockLegacy)

	raw, err := os.ReadFile(tournPath)
	require.NoError(t, err)
	text := string(raw)
	assert.NotContains(t, text, "opening_block")
	assert.NotContains(t, text, "closing_block")

	// Calling upgradeLegacyCeremonies again writes nothing (file bytes and mtime unchanged)
	statBefore, err := os.Stat(tournPath)
	require.NoError(t, err)
	bytesBefore, err := os.ReadFile(tournPath)
	require.NoError(t, err)

	require.NoError(t, store.upgradeLegacyCeremonies())

	statAfter, err := os.Stat(tournPath)
	require.NoError(t, err)
	bytesAfter, err := os.ReadFile(tournPath)
	require.NoError(t, err)

	assert.Equal(t, bytesBefore, bytesAfter)
	assert.Equal(t, statBefore.ModTime(), statAfter.ModTime())
}

func TestTournamentUpgrade_OneDayTournamentGetsDayZero(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	tournContent := `---
name: Regional Open
date: 01-08-2026
duration_days: 1
opening_block: 30m
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(tournContent), 0o600))

	compDir := filepath.Join(dir, "competitions", "c1")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	compContent := `---
id: c1
name: Comp 1
date: 01-08-2026
start_time: "09:30"
---
`
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(compContent), 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	expected := []Pause{
		{
			Name:   "Opening ceremony",
			Start:  "09:30",
			Length: "30m",
			Day:    0,
		},
	}
	assert.Equal(t, expected, tourn.Pauses)
}

func TestTournamentUpgrade_CompetitionsWithNoDateCountAsDayOne(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	tournContent := `---
name: Multi-day Open
date: 01-08-2026
duration_days: 2
opening_block: 30m
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(tournContent), 0o600))

	compDir := filepath.Join(dir, "competitions", "c1")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	compContent := `---
id: c1
name: Comp 1
start_time: "09:30"
---
`
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(compContent), 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	expected := []Pause{
		{
			Name:   "Opening ceremony",
			Start:  "09:30",
			Length: "30m",
			Day:    1,
		},
	}
	assert.Equal(t, expected, tourn.Pauses)
}

func TestTournamentUpgrade_NoCompetitionsGivesOpeningWithNoStart(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	tournContent := `---
name: Open Tournament
date: 01-08-2026
duration_days: 1
opening_block: 30m
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(tournContent), 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	expected := []Pause{
		{
			Name:   "Opening ceremony",
			Start:  "",
			Length: "30m",
			Day:    0,
		},
	}
	assert.Equal(t, expected, tourn.Pauses)
}

func TestTournamentUpgrade_NoCeremonyKeysNotRewritten(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	rawBefore := []byte(`---
name: Clean Tournament
date: 01-08-2026
venue: Tokyo Budokan
duration_days: 1
courts:
  - A
  - B
---
`)
	require.NoError(t, os.WriteFile(tournPath, rawBefore, 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	rawAfter, err := os.ReadFile(tournPath)
	require.NoError(t, err)

	assert.Equal(t, string(rawBefore), string(rawAfter), "tournament with no ceremony keys must not be rewritten")
	_ = store
}

func TestTournamentUpgrade_FailedWriteReturnsErrorAndLeavesKeysOnDisk(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("skipping test as root; read-only permissions are ignored")
	}

	dir := t.TempDir()
	// Start store in empty folder first
	store, err := NewStore(dir)
	require.NoError(t, err)

	tournPath := filepath.Join(dir, "tournament.md")
	tournContent := `---
name: Permissions Test
date: 01-08-2026
opening_block: 30m
closing_block: 30m
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(tournContent), 0o600))

	// Make data folder read-only
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700)
	})

	err = store.upgradeLegacyCeremonies()
	require.Error(t, err, "upgradeLegacyCeremonies must return an error when file write fails")

	// Restore permissions before reading to verify contents
	require.NoError(t, os.Chmod(dir, 0o700))

	content, err := os.ReadFile(tournPath)
	require.NoError(t, err)
	text := string(content)
	assert.Contains(t, text, "opening_block: 30m")
	assert.Contains(t, text, "closing_block: 30m")
}

func TestUpgradeLegacyCeremonies_ClockParsing(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	tournContent := `---
name: Clock Parsing Championship
date: 01-08-2026
duration_days: 2
opening_block: 30m
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(tournContent), 0o600))

	writeComp := func(id, date, startTime string) {
		compDir := filepath.Join(dir, "competitions", id)
		require.NoError(t, os.MkdirAll(compDir, 0o700))
		compContent := "---\nid: " + id + "\nname: " + id + "\ndate: " + date + "\nstart_time: \"" + startTime + "\"\n---\n"
		require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(compContent), 0o600))
	}

	// Day 1: single-digit hour "9:30" must format to zero-padded "09:30".
	writeComp("comp-day1", "01-08-2026", "9:30")
	// Day 2: unparseable "9am" must be skipped and not become the day's earliest
	// nor leak into Pause.Start, leaving "10:00" as the earliest.
	writeComp("comp-day2-invalid", "02-08-2026", "9am")
	writeComp("comp-day2-valid", "02-08-2026", "10:00")

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	expectedPauses := []Pause{
		{
			Name:   "Opening ceremony",
			Start:  "09:30",
			Length: "30m",
			Day:    1,
		},
		{
			Name:   "Opening ceremony",
			Start:  "10:00",
			Length: "30m",
			Day:    2,
		},
	}
	assert.Equal(t, expectedPauses, tourn.Pauses)
}

func TestTournamentUpgrade_CompetitionConfigFailsToLoad(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	tournContent := `---
name: Corrupt Comp Tournament
date: 01-08-2026
duration_days: 1
opening_block: 30m
closing_block: 30m
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(tournContent), 0o600))

	// comp-corrupt: config.md contains unparseable front-matter (invalid YAML)
	compDir := filepath.Join(dir, "competitions", "comp-corrupt")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(":\n: invalid\n"), 0o600))

	// Startup from NewStore logs the error and continues without failing NewStore.
	store, err := NewStore(dir)
	require.NoError(t, err)

	// Since upgradeLegacyCeremonies errored on comp-corrupt, tournament.md was left
	// as it was: opening_block and closing_block remain on disk, and Pauses is nil/empty.
	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)
	assert.Equal(t, "30m", tourn.OpeningBlockLegacy)
	assert.Equal(t, "30m", tourn.ClosingBlockLegacy)
	assert.Empty(t, tourn.Pauses)

	// Direct call to upgradeLegacyCeremonies returns the error mentioning the competition ID.
	err = store.upgradeLegacyCeremonies()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading competition comp-corrupt")

	// Now verify that a competition directory without a config.md (comp == nil)
	// does not fail: the upgrade carries on and skips it.
	require.NoError(t, os.RemoveAll(compDir))
	emptyCompDir := filepath.Join(dir, "competitions", "comp-missing-config")
	require.NoError(t, os.MkdirAll(emptyCompDir, 0o700))

	require.NoError(t, store.upgradeLegacyCeremonies())
	tournAfter, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Empty(t, tournAfter.OpeningBlockLegacy)
	assert.Empty(t, tournAfter.ClosingBlockLegacy)
	require.Len(t, tournAfter.Pauses, 2)
}

func TestTournamentUpgrade_CompetitionDatedOutsideTournamentDays(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	tournContent := `---
name: Outside Date Championship
date: 01-08-2026
duration_days: 1
opening_block: 30m
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(tournContent), 0o600))

	writeComp := func(id, date, startTime string) {
		compDir := filepath.Join(dir, "competitions", id)
		require.NoError(t, os.MkdirAll(compDir, 0o700))
		compContent := "---\nid: " + id + "\nname: " + id + "\ndate: " + date + "\nstart_time: \"" + startTime + "\"\n---\n"
		require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(compContent), 0o600))
	}

	// comp-outside has an earlier start time (08:00) but its date is outside the tournament's 1-day range.
	// It must be skipped and not set the day's earliest start time.
	writeComp("comp-outside", "05-08-2026", "08:00")
	// comp-valid is on the tournament's date (01-08-2026) starting at 10:00.
	writeComp("comp-valid", "01-08-2026", "10:00")

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	// Since comp-outside is skipped, the opening ceremony starts at 10:00, not 08:00.
	expectedPauses := []Pause{
		{
			Name:   "Opening ceremony",
			Start:  "10:00",
			Length: "30m",
			Day:    0,
		},
	}
	assert.Equal(t, expectedPauses, tourn.Pauses)
}

func TestTournamentUpgrade_ExistingPausesMergedBetweenOpeningAndClosing(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	tournContent := `---
name: Ceremony with Existing Pauses
date: 01-08-2026
duration_days: 1
opening_block: 30m
closing_block: 45m
pauses:
  - name: Lunch Break
    start: "12:00"
    length: 1h
    day: 0
---
`
	require.NoError(t, os.WriteFile(tournPath, []byte(tournContent), 0o600))

	compDir := filepath.Join(dir, "competitions", "c1")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	compContent := `---
id: c1
name: Comp 1
date: 01-08-2026
start_time: "09:30"
---
`
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(compContent), 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)

	// The existing pauses in desired.Pauses must be sandwiched between openingPauses and closingPauses:
	// [0]: Opening ceremony
	// [1]: Lunch Break (existing pause)
	// [2]: Closing ceremony
	expectedPauses := []Pause{
		{
			Name:   "Opening ceremony",
			Start:  "09:30",
			Length: "30m",
			Day:    0,
		},
		{
			Name:   "Lunch Break",
			Start:  "12:00",
			Length: "1h",
			Day:    0,
		},
		{
			Name:   "Closing ceremony",
			Start:  "",
			Length: "45m",
			Day:    0,
		},
	}
	assert.Equal(t, expectedPauses, tourn.Pauses)
	assert.Empty(t, tourn.OpeningBlockLegacy)
	assert.Empty(t, tourn.ClosingBlockLegacy)
}
