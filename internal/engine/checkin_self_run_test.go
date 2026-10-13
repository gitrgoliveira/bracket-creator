package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Operator ruling 2026-10-13: "Self run mode has no check-in." The draw, the
// Swiss field and the schedule estimate all ask Competition.CheckInApplies, so
// a competition whose stored CheckInEnabled is true still draws everyone in a
// self-run tournament, while an officiated one keeps filtering.

func saveTournamentMode(t *testing.T, store *state.Store, mode string) {
	t.Helper()
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Cup", Courts: []string{"A"}, Mode: mode,
	}))
}

var checkInRoster = []string{"Alice", "Bob", "Charlie", "Dave", "Eve"}
var checkInFour = map[string]bool{"Alice": true, "Bob": true, "Charlie": true, "Dave": true}

func TestSelfRun_KnockoutDraw_IgnoresCheckIn(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	saveTournamentMode(t, store, state.TournamentModeSelfRun)
	compID := "selfrun-knockout"
	createTestCompetition(t, store, compID, "knockout", 0)
	enableCheckIn(t, store, compID)
	saveParticipantsWithCheckIn(t, store, compID, checkInRoster, checkInFour)

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	names := bracketLeafNames(bracket)
	assert.Len(t, names, 5, "self-run draws the whole roster")
	assert.Contains(t, names, "Eve", "a participant who is not checked in is still drawn")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.True(t, comp.CheckInEnabled, "the stored setting is not rewritten")
}

func TestSelfRun_PoolDraw_IgnoresCheckIn(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	saveTournamentMode(t, store, state.TournamentModeSelfRun)
	compID := "selfrun-pools"
	createTestCompetition(t, store, compID, "league", 3)
	enableCheckIn(t, store, compID)
	saveParticipantsWithCheckIn(t, store, compID,
		[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"},
		map[string]bool{"Alice": true, "Bob": true, "Charlie": true, "Dave": true})

	require.NoError(t, eng.StartCompetition(compID))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.Len(t, poolPlayerNames(pools), 6, "self-run draws the whole roster")
}

func TestOfficiated_KnockoutDraw_StillFiltersByCheckIn(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	saveTournamentMode(t, store, state.TournamentModeOfficiated)
	compID := "officiated-knockout"
	createTestCompetition(t, store, compID, "knockout", 0)
	enableCheckIn(t, store, compID)
	saveParticipantsWithCheckIn(t, store, compID, checkInRoster, checkInFour)

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	names := bracketLeafNames(bracket)
	assert.Len(t, names, 4)
	assert.NotContains(t, names, "Eve")
}

func TestSelfRun_SwissRoundOne_IgnoresCheckIn(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	saveTournamentMode(t, store, state.TournamentModeSelfRun)
	compID := "selfrun-swiss"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Swiss", Kind: "individual", Format: state.CompFormatSwiss,
		SwissRounds: 3, Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
		CheckInEnabled: true,
	}))
	saveParticipantsWithCheckIn(t, store, compID,
		[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"},
		map[string]bool{"Alice": true, "Bob": true, "Charlie": true, "Dave": true})

	matches, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, m := range matches {
		seen[m.SideA] = true
		seen[m.SideB] = true
	}
	assert.Contains(t, seen, "Eve")
	assert.Contains(t, seen, "Frank")
}

func TestSelfRun_ScheduleEstimate_CountsWholeRoster(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	saveTournamentMode(t, store, state.TournamentModeSelfRun)
	compID := "selfrun-estimate"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatKnockout, Kind: "individual",
		Courts: []string{"A"}, StartTime: "09:00", KnockoutMatchDurationSeconds: 300,
		Status: state.CompStatusSetup, CheckInEnabled: true,
	}))
	// 8 registered, 4 checked in: the estimate must count all 8 (7 bouts).
	saveParticipantsWithCheckIn(t, store, compID,
		[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank", "Grace", "Hank"},
		map[string]bool{"Alice": true, "Bob": true, "Charlie": true, "Dave": true})

	est, err := eng.EstimateScheduleForCompetition(compID)
	require.NoError(t, err)

	comp, _ := store.LoadCompetition(compID)
	tourn, _ := store.LoadTournament()
	assert.Equal(t, EstimateForCounts(0, 7, comp, tourn).TotalDurationMinutes, est.TotalDurationMinutes)
}
