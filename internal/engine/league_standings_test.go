package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLeagueStandings_NonLeagueFormat verifies that a competition whose
// Format isn't "league" (e.g. mixed) returns a *NotFoundError so the
// league standings surface never leaks pool data for other formats.
func TestLeagueStandings_NonLeagueFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mixed-comp"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Mixed", Format: state.CompFormatMixed,
	}))

	standings, err := eng.LeagueStandings(compID)
	require.Error(t, err)
	assert.Nil(t, standings)
	var nfe *NotFoundError
	assert.ErrorAs(t, err, &nfe, "non-league competition must return NotFoundError")
}

// TestLeagueStandings_UnknownCompetition verifies that an unknown compID
// returns a *NotFoundError.
func TestLeagueStandings_UnknownCompetition(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	standings, err := eng.LeagueStandings("does-not-exist")
	require.Error(t, err)
	assert.Nil(t, standings)
	var nfe *NotFoundError
	assert.ErrorAs(t, err, &nfe, "unknown competition must return NotFoundError")
}

// TestLeagueStandings_RoundRobinResults verifies that a league competition
// with recorded round-robin results returns a single rank-ordered slice
// covering every participant, with the outright winner (all wins) ranked
// first.
func TestLeagueStandings_RoundRobinResults(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-standings"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:         compID,
		Name:       "League Standings Test",
		Kind:       "individual",
		Format:     state.CompFormatLeague,
		PoolSize:   4,
		RoundRobin: true,
		Courts:     []string{"A"},
		StartTime:  "09:00",
		Status:     "setup",
	}))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	// Alice beats everyone; among the rest, resolve deterministically so the
	// standings order is unambiguous.
	for i, m := range matches {
		var winner string
		switch {
		case m.SideA == "Alice" || m.SideB == "Alice":
			winner = "Alice"
		case m.SideA == "Bob" || m.SideB == "Bob":
			winner = "Bob"
		default:
			winner = m.SideA
		}
		matches[i].Winner = winner
		matches[i].Status = state.MatchStatusCompleted
		matches[i].IpponsA = []string{"M"}
		matches[i].IpponsB = []string{}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	standings, err := eng.LeagueStandings(compID)
	require.NoError(t, err)
	require.Len(t, standings, 4, "league standings must cover the whole roster in one slice")

	assert.Equal(t, "Alice", standings[0].Player.Name, "Alice won every match, must rank first")
	assert.Equal(t, 1, standings[0].Rank)
	assert.Equal(t, 3, standings[0].Wins)

	// Ranks must be assigned in increasing order 1..4 across the single slice.
	for i, s := range standings {
		assert.Equal(t, i+1, s.Rank)
	}
}

// TestLeagueStandings_Drawn verifies the un-started/undrawn case: a league
// competition that has been started but has no scored results still
// returns the full roster as a single rank-ordered slice, all zeros.
func TestLeagueStandings_Drawn(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-drawn"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:         compID,
		Name:       "League Drawn",
		Kind:       "individual",
		Format:     state.CompFormatLeague,
		PoolSize:   3,
		RoundRobin: true,
		Courts:     []string{"A"},
		StartTime:  "09:00",
		Status:     "setup",
	}))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))

	standings, err := eng.LeagueStandings(compID)
	require.NoError(t, err)
	require.Len(t, standings, 3)
	for i, s := range standings {
		assert.Equal(t, 0, s.Wins)
		assert.Equal(t, i+1, s.Rank)
	}
}
