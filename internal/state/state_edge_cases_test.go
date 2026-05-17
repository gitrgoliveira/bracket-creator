package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadSchedule_MalformedCSV covers the parseScheduleFile csv.ReadAll
// error path by writing invalid CSV to schedule.csv.
func TestLoadSchedule_MalformedCSV(t *testing.T) {
	dir := t.TempDir()
	compID := "sched-bad"
	store, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	path := filepath.Join(dir, "competitions", compID, "schedule.csv")
	// Bare quote mid-field forces a csv.ErrBareQuote.
	require.NoError(t, os.WriteFile(path, []byte("a,b\na,\"bad\nquote"), 0o600))

	_, err = store.LoadSchedule(compID)
	assert.Error(t, err)
}

// TestCopySchedule_Nil verifies that copySchedule(nil) returns nil.
func TestCopySchedule_Nil(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, store.copySchedule(nil))
}

// TestSerializeSchedule_WithBreakAndLabel verifies that schedule entries
// with IsBreak=true and a non-empty Label round-trip through
// serializeSchedule correctly.
func TestSerializeSchedule_WithBreakAndLabel(t *testing.T) {
	entries := []ScheduleEntry{
		{MatchType: "break", MatchRef: "", Court: "A", ScheduledAt: "12:00", Status: "scheduled", IsBreak: true, Label: "Lunch"},
		{MatchType: "pool", MatchRef: "P1-0", Court: "B", ScheduledAt: "09:00", Status: "scheduled", IsBreak: false, Label: ""},
	}
	data, err := serializeSchedule(entries)
	require.NoError(t, err)
	assert.Contains(t, string(data), "true")
	assert.Contains(t, string(data), "Lunch")
}

// TestSavePoolMatches_InvalidDir covers the error path in savePoolMatchesLocked
// when the competition directory cannot be created.
func TestSavePools_InvalidDir(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	// Block the competitions dir for compID by placing a regular file there.
	compID := "blocked-comp"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", compID), []byte("x"), 0o600))

	err = store.SavePoolMatches(compID, []MatchResult{})
	assert.Error(t, err)
}

// TestApplyCompetitionDefaults_Nil verifies that ApplyCompetitionDefaults
// handles a nil receiver without panicking.
func TestApplyCompetitionDefaults_Nil(t *testing.T) {
	// Should not panic.
	ApplyCompetitionDefaults(nil)
}

// TestAtomicWrite_OutsideDataDir covers the path-traversal guard in
// atomicWrite: a path outside the store's data folder returns an error.
func TestAtomicWrite_OutsideDataDir(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	// Request a write to /tmp — outside the store folder.
	err = store.atomicWrite(os.TempDir()+"/test.txt", []byte("x"), 0o600)
	assert.Error(t, err, "write outside data dir must return error")
}

// TestSaveCompetitionChanged_MkdirAllFails covers the os.MkdirAll error
// branch in saveCompetitionChangedLocked by blocking directory creation.
func TestSaveCompetitionChanged_MkdirAllFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "blocked-mkdirall"
	// Place a regular FILE where the competition directory should go;
	// MkdirAll cannot create a directory at a path occupied by a file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", compID), []byte("x"), 0o600))

	_, err = store.SaveCompetitionChanged(&Competition{ID: compID, Name: "will-fail"})
	assert.Error(t, err, "saveCompetitionChangedLocked must surface MkdirAll failure")
}

// TestSaveTournamentChanged_NoChange verifies that saving the same tournament
// struct twice returns changed=false on the second call (bytes.Equal path).
func TestSaveTournamentChanged_NoChange(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	tourney := &Tournament{Name: "Same Tourney", Venue: "Same Venue"}
	changed1, err := store.SaveTournamentChanged(tourney)
	require.NoError(t, err)
	assert.True(t, changed1)

	changed2, err := store.SaveTournamentChanged(tourney)
	require.NoError(t, err)
	assert.False(t, changed2, "identical second save must be a no-op")
}

// TestUpdateTournamentChanged_NilCurrent verifies the first-ever save path:
// when tournament.md does not exist yet, transform receives nil as current.
func TestUpdateTournamentChanged_NilCurrent(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	var sawNilCurrent bool
	changed, err := store.UpdateTournamentChanged(
		&Tournament{Name: "First Save", Venue: "V"},
		func(current, desired *Tournament) error {
			sawNilCurrent = current == nil
			return nil
		},
	)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.True(t, sawNilCurrent, "transform must receive nil current for first-ever save")

	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "First Save", loaded.Name)
}

// TestSaveScheduleChanged_NoChange verifies the bytes.Equal early-exit path in
// SaveScheduleChanged: saving identical entries twice returns changed=false.
func TestSaveScheduleChanged_NoChange(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	compID := "sched-no-change"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	entries := []ScheduleEntry{
		{MatchType: "pool", MatchRef: "P1-0", Court: "A", Status: "scheduled"},
	}
	changed1, err := store.SaveScheduleChanged(compID, entries)
	require.NoError(t, err)
	assert.True(t, changed1)

	changed2, err := store.SaveScheduleChanged(compID, entries)
	require.NoError(t, err)
	assert.False(t, changed2, "identical second save must be a no-op")
}

// TestSaveScheduleChanged_NilEntries verifies that a nil entries slice is
// stored as an empty slice (the nil-init guard).
func TestSaveScheduleChanged_NilEntries(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	compID := "sched-nil"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	changed, err := store.SaveScheduleChanged(compID, nil)
	require.NoError(t, err)
	assert.True(t, changed)

	loaded, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	assert.NotNil(t, loaded)
}

// TestLoadBracket_MalformedJSON covers the loadCached error path in LoadBracket
// by writing invalid JSON to bracket.json.
func TestLoadBracket_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "bracket-bad"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", compID, "bracket.json"),
		[]byte("{not valid json"), 0o600))

	_, err = store.LoadBracket(compID)
	assert.Error(t, err)
}

// TestLoadPools_MalformedCSV covers the loadCached error path in LoadPools
// by writing a bare-quote CSV to pools.csv.
func TestLoadPools_MalformedCSV(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "pools-bad"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", compID, "pools.csv"),
		[]byte("a,b\na,\"bad\nquote"), 0o600))

	_, err = store.LoadPools(compID)
	assert.Error(t, err)
}

// TestLoadPoolMatches_MalformedCSV covers the loadCached error path in
// LoadPoolMatches by writing a bare-quote CSV to pool-matches.csv.
func TestLoadPoolMatches_MalformedCSV(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "pm-bad"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", compID, "pool-matches.csv"),
		[]byte("a,b\na,\"bad\nquote"), 0o600))

	_, err = store.LoadPoolMatches(compID)
	assert.Error(t, err)
}

// TestLoadReservedSlots_MalformedJSON covers the loadCached error path in
// LoadReservedSlots by writing invalid JSON.
func TestLoadReservedSlots_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "slots-bad"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", compID, "reserved-slots.json"),
		[]byte("{not valid json"), 0o600))

	_, err = store.LoadReservedSlots(compID)
	assert.Error(t, err)
}

// TestSaveReservedSlots_NilSlice verifies the nil-guard inside
// saveReservedSlotsLocked: saving a nil slice persists "null" JSON but the
// cache is populated with an empty (non-nil) slice.
func TestSaveReservedSlots_NilSlice(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	compID := "slots-nil"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	require.NoError(t, store.SaveReservedSlots(compID, nil))

	loaded, err := store.LoadReservedSlots(compID)
	require.NoError(t, err)
	assert.NotNil(t, loaded)
}

// TestUpdateTournamentChanged_NoChange verifies the bytes.Equal no-change path:
// calling UpdateTournamentChanged with data identical to what's on disk must
// return (false, nil) without writing.
func TestUpdateTournamentChanged_NoChange(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	tourney := &Tournament{Name: "Same", Venue: "V"}
	require.NoError(t, store.SaveTournament(tourney))

	changed, err := store.UpdateTournamentChanged(
		&Tournament{Name: "Same", Venue: "V"},
		func(current, desired *Tournament) error { return nil },
	)
	require.NoError(t, err)
	assert.False(t, changed, "UpdateTournamentChanged with identical data must return changed=false")
}

// TestUpdateTournamentChanged_ParseFailureFallback verifies that when
// tournament.md has invalid front-matter, UpdateTournamentChanged falls back
// to a default Tournament as `current` (same as LoadTournament's fallback).
func TestUpdateTournamentChanged_ParseFailureFallback(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	// Write a file with invalid front matter.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "tournament.md"),
		[]byte("invalid content — no front-matter"), 0o600))

	var sawDefault bool
	_, err = store.UpdateTournamentChanged(
		&Tournament{Name: "new"},
		func(current, desired *Tournament) error {
			sawDefault = current != nil && current.Name == "New Tournament"
			return nil
		},
	)
	require.NoError(t, err)
	assert.True(t, sawDefault, "transform must receive default Tournament when front-matter is corrupt")
}

// TestSetTeamLineup_BracketParseErrorFromRoundCheck verifies that a malformed
// bracket.json causes setTeamLineupLocked to propagate the parse error from
// roundHasLiveOrCompletedMatchLocked rather than silently succeeding.
func TestSetTeamLineup_BracketParseErrorFromRoundCheck(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "lineup-bad-bracket"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Write malformed bracket.json so parseBracketFile fails.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", compID, "bracket.json"),
		[]byte("{not valid json"), 0o600))

	err = store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5)
	assert.Error(t, err, "malformed bracket.json must propagate as error from SetTeamLineup")
}

// TestLoadTeamLineups_MalformedYAML covers the loadCached error path in
// LoadTeamLineups by writing invalid YAML to the lineups file.
func TestLoadTeamLineups_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "lineups-bad"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", compID, teamLineupFilename),
		[]byte(":\t:bad yaml:"), 0o600))

	_, err = store.LoadTeamLineups(compID)
	assert.Error(t, err)
}

// TestLockTeamLineupsForRound_MalformedLineupFile verifies that a malformed
// lineups YAML file propagates an error from LockTeamLineupsForRound.
func TestLockTeamLineupsForRound_MalformedLineupFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "lock-bad"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", compID, teamLineupFilename),
		[]byte(":\t:bad yaml:"), 0o600))

	err = store.LockTeamLineupsForRound(compID, 0, time.Now())
	assert.Error(t, err)
}

// TestSetCompetitorStatusLocked_InvalidStatus covers the Validate error
// branch inside setCompetitorStatusLocked (called directly to bypass the
// duplicate guard in SetCompetitorStatus).
func TestSetCompetitorStatusLocked_InvalidStatus(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	compID := "cs-invalid"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Empty PlayerID → Validate returns an error.
	err := store.setCompetitorStatusLocked(compID, domain.CompetitorStatus{PlayerID: ""}, store.directWrite)
	assert.Error(t, err)
}

// TestCopyTournament_WithCourts verifies that copyTournament performs a deep
// copy of the Courts slice — mutation of the copy must not affect the original.
func TestCopyTournament_WithCourts(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	orig := &Tournament{Name: "T", Courts: []string{"A", "B", "C"}}
	cp := store.copyTournament(orig)
	require.NotNil(t, cp)
	require.Equal(t, orig.Courts, cp.Courts)

	// Mutate the copy; original must be unaffected.
	cp.Courts[0] = "Z"
	assert.Equal(t, "A", orig.Courts[0], "copyTournament must deep-copy the Courts slice")
}

// TestCopyTournament_Nil verifies that copyTournament(nil) returns nil without
// panicking, covering the nil-return branch.
func TestCopyTournament_Nil(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	assert.Nil(t, store.copyTournament(nil))
}

// TestLockTeamLineupsForRound_InvalidCompID covers the ValidateCompetitionID
// error branch in LockTeamLineupsForRound.
func TestLockTeamLineupsForRound_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	err = store.LockTeamLineupsForRound("", 0, time.Now())
	assert.Error(t, err)
}

// TestLoadTeamLineups_InvalidCompID covers the ValidateCompetitionID error
// branch in LoadTeamLineups.
func TestLoadTeamLineups_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	_, err = store.LoadTeamLineups("")
	assert.Error(t, err)
}

// TestLoadCompetition_InvalidCompID covers the ValidateCompetitionID error
// path in the public LoadCompetition method (wraps the error with context).
func TestLoadCompetition_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	_, err = store.LoadCompetition("")
	assert.Error(t, err)
}

// TestSetTeamLineup_InvalidCompID covers the ValidateCompetitionID error
// branch at the top of the public SetTeamLineup method.
func TestSetTeamLineup_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	err = store.SetTeamLineup("", fiveStarter("t", 0), 5)
	assert.Error(t, err)
}

// TestDeleteTeamLineup_InvalidCompID covers the ValidateCompetitionID error
// branch in DeleteTeamLineup.
func TestDeleteTeamLineup_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	err = store.DeleteTeamLineup("", "team-x", 0)
	assert.Error(t, err)
}

// TestAddReservedSlot_DuplicateSlot verifies the idempotent guard in
// AddReservedSlot: adding the same (sourceCompID, sourceRank) pair twice must
// return the existing slot rather than creating a duplicate.
func TestAddReservedSlot_DuplicateSlot(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	compID := "dup-slot-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	first, err := store.AddReservedSlot(compID, "src-comp", 1, false)
	require.NoError(t, err)
	require.NotNil(t, first)

	second, err := store.AddReservedSlot(compID, "src-comp", 1, false)
	require.NoError(t, err)
	require.NotNil(t, second)
	assert.Equal(t, first.ID, second.ID, "duplicate AddReservedSlot must return the existing slot")

	slots, err := store.LoadReservedSlots(compID)
	require.NoError(t, err)
	assert.Len(t, slots, 1, "exactly one slot must exist after two identical AddReservedSlot calls")
}

// TestRemoveReservedSlot_NotFound verifies that RemoveReservedSlot returns an
// error when the slot ID does not exist.
func TestRemoveReservedSlot_NotFound(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	compID := "remove-notfound"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	err = store.RemoveReservedSlot(compID, "nonexistent-slot-id", false)
	assert.Error(t, err)
}

// TestRemoveReservedSlot_HappyPath verifies that a slot added via
// AddReservedSlot can be removed, and neither the slot nor the placeholder
// participant remains afterward.
func TestRemoveReservedSlot_HappyPath(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	compID := "remove-ok"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	slot, err := store.AddReservedSlot(compID, "src", 2, false)
	require.NoError(t, err)

	require.NoError(t, store.RemoveReservedSlot(compID, slot.ID, false))

	slots, err := store.LoadReservedSlots(compID)
	require.NoError(t, err)
	assert.Empty(t, slots, "slot must be gone after RemoveReservedSlot")
}
