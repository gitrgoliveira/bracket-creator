package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLeagueStandings_NotFound verifies the two *NotFoundError paths: a
// competition whose Format isn't "league" (so the league standings surface
// never leaks pool data for other formats) and an unknown compID.
func TestLeagueStandings_NotFound(t *testing.T) {
	tests := []struct {
		name   string
		compID string
		setup  func(t *testing.T, store *state.Store)
	}{
		{
			name:   "non-league format",
			compID: "mixed-comp",
			setup: func(t *testing.T, store *state.Store) {
				createTestCompetition(t, store, "mixed-comp", state.CompFormatMixed, 3)
			},
		},
		{
			name:   "unknown competition",
			compID: "does-not-exist",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			if tc.setup != nil {
				tc.setup(t, store)
			}

			standings, err := eng.LeagueStandings(tc.compID)
			require.Error(t, err)
			assert.Nil(t, standings)
			var nfe *NotFoundError
			assert.ErrorAs(t, err, &nfe, "%s must return NotFoundError", tc.name)
		})
	}
}

// TestLeagueStandings_RoundRobinResults verifies that a league competition
// with recorded round-robin results returns a single rank-ordered slice
// covering every participant, with the outright winner (all wins) ranked
// first.
func TestLeagueStandings_RoundRobinResults(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-standings"

	createTestCompetition(t, store, compID, state.CompFormatLeague, 4)
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
		// Score the winner's actual side so points-scored/points-lost data
		// stays consistent with the recorded winner (a fixed IpponsA would
		// hand the point to the loser whenever the winner sat on SideB,
		// a state the engine cannot produce).
		if m.SideA == winner {
			matches[i].IpponsA = []string{"M"}
			matches[i].IpponsB = []string{}
		} else {
			matches[i].IpponsA = []string{}
			matches[i].IpponsB = []string{"M"}
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	standings, err := eng.LeagueStandings(compID)
	require.NoError(t, err)
	require.Len(t, standings, 4, "league standings must cover the whole roster in one slice")

	assert.Equal(t, "Alice", standings[0].Player.Name, "Alice won every match, must rank first")
	assert.Equal(t, 3, standings[0].Wins)
	assert.Equal(t, "Bob", standings[1].Player.Name,
		"Bob won both non-Alice matches (2 wins), must rank second")

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

	createTestCompetition(t, store, compID, state.CompFormatLeague, 3)
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
