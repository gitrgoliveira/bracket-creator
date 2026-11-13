package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// leagueRankNames is a league's standings as team names, place by place.
func leagueRankNames(t *testing.T, eng *Engine, compID string) []string {
	t.Helper()
	standings, err := eng.LeagueStandings(compID)
	require.NoError(t, err)
	names := make([]string, len(standings))
	for i, s := range standings {
		names[i] = s.Player.Name
	}
	return names
}

// Lots typed wrongly on a team league that has just finished stay correctable
// (decision F2 (a)): the recorded chusen is still offered once the league is
// complete, a corrected order is accepted, the table and the podium follow it
// at once, and the league stays finished (no reopen, nothing else moves).
func TestChusen_FinishedLeague_ACorrectedLotsOrderMovesThePodiumAndStaysFinished(t *testing.T) {
	const compID = "league-chusen-finished"
	eng, store := cycleLeagueAt3rd(t, compID)

	_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{teamID("Gamma"), teamID("Delta"), teamID("Epsilon")})
	require.NoError(t, err)
	scoreInjectedDH(t, eng, store, compID, circle)

	// The operator types the lots, but in the wrong order: Delta 3rd.
	changed, err := eng.OverridePoolRanks(compID, "Pool A", []RankOverride{
		{PlayerID: teamID("Delta"), Rank: 3},
		{PlayerID: teamID("Epsilon"), Rank: 4},
		{PlayerID: teamID("Gamma"), Rank: 5},
	})
	require.NoError(t, err)
	require.True(t, changed)
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTransitioned, outcome, "the chusen settles the circle and finishes the league")
	require.Equal(t, []string{"Alpha", "Beta", "Delta", "Epsilon", "Gamma", "Zeta"}, leagueRankNames(t, eng, compID))
	matchesBefore, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	// The finished league still offers the recorded lots for correction.
	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending)
	require.Len(t, report.Recorded, 1, "a finished league's recorded lots stay correctable")
	assert.Equal(t, 3, report.Recorded[0].MinPosition)

	// The lots actually gave Gamma 3rd.
	changed, err = eng.OverridePoolRanks(compID, "Pool A", []RankOverride{
		{PlayerID: teamID("Gamma"), Rank: 3},
		{PlayerID: teamID("Delta"), Rank: 4},
		{PlayerID: teamID("Epsilon"), Rank: 5},
	})
	require.NoError(t, err, "a finished league takes the corrected order")
	require.True(t, changed)
	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "the correction neither reopens nor finishes the league again")

	assert.Equal(t, []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta"}, leagueRankNames(t, eng, compID),
		"the table and the podium follow the corrected lots at once")
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status, "the league stays finished")
	matchesAfter, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, matchesBefore, matchesAfter, "no match is written or reopened")

	report, err = eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Recorded, 1)
	ranks := map[string]int{}
	for i, s := range report.Recorded[0].Teams {
		ranks[s.Player.Name] = report.Recorded[0].Ranks[i]
	}
	assert.Equal(t, map[string]int{"Gamma": 3, "Delta": 4, "Epsilon": 5}, ranks, "the recorded lots carry the corrected order")
}

// An individual league that has finished takes no lots: the rule accepts a
// finished league, and the team-only gate is what refuses an individual one.
func TestChusenStatus_FinishedIndividualLeagueOffersNoLots(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "league-individual-finished"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Individual league", Format: state.CompFormatLeague,
		Status: state.CompStatusComplete, Courts: []string{"A"},
	}))
	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending)
	assert.Empty(t, report.Recorded)
}
