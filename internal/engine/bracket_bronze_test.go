package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createBronzeTestCompetition creates a playoffs competition with the naginata
// flag set (or not) so the bronze-match generation gate can be exercised.
func createBronzeTestCompetition(t *testing.T, store *state.Store, id string, naginata bool) {
	t.Helper()
	comp := &state.Competition{
		ID:           id,
		Name:         "Bronze Test",
		Kind:         "individual",
		Format:       state.CompFormatPlayoffs,
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
		Naginata:     naginata,
	}
	require.NoError(t, store.SaveCompetition(comp))
}

// TestBronze_GeneratedForNaginataWithSemifinal verifies a bronze match exists
// for a naginata bracket with ≥4 effective players (a real semifinal round).
func TestBronze_GeneratedForNaginataWithSemifinal(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-nag-4"

	createBronzeTestCompetition(t, store, compID, true)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(bracket.Rounds), 2, "4 players should give >=2 rounds")

	require.NotNil(t, bracket.ThirdPlaceMatch, "naginata 4-player bracket must have a bronze match")
	assert.Equal(t, "m-bronze", bracket.ThirdPlaceMatch.ID)
	assert.Equal(t, -1, bracket.ThirdPlaceMatch.DisplayRound, "bronze uses DisplayRound -1 sentinel")
	assert.Equal(t, state.MatchStatusScheduled, bracket.ThirdPlaceMatch.Status)
	assert.Empty(t, bracket.ThirdPlaceMatch.SideA, "sides start empty (filled from SF losers)")
	assert.Empty(t, bracket.ThirdPlaceMatch.SideB)
}

// TestBronze_AbsentForKendo verifies a kendo (non-naginata) bracket has no
// bronze match even with 4 players.
func TestBronze_AbsentForKendo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-kendo-4"

	createBronzeTestCompetition(t, store, compID, false)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Nil(t, bracket.ThirdPlaceMatch, "kendo bracket must not have a bronze match")
}

// TestBronze_AbsentForTwoPlayerNaginata verifies no bronze for a 2-player
// naginata bracket (single round, no semifinal).
func TestBronze_AbsentForTwoPlayerNaginata(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-nag-2"

	createBronzeTestCompetition(t, store, compID, true)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Less(t, len(bracket.Rounds), 2, "2 players should give a single round")
	assert.Nil(t, bracket.ThirdPlaceMatch, "2-player naginata bracket must not have a bronze match")
}

// TestBronze_SemifinalLosersFeedBronze verifies that scoring both semifinals
// writes the two losers into the bronze match (first → SideA, second → SideB),
// and the winners advance to the final.
func TestBronze_SemifinalLosersFeedBronze(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-feed"

	createBronzeTestCompetition(t, store, compID, true)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)

	// SF round is the round before the final (index len-2).
	sfIdx := len(bracket.Rounds) - 2
	require.GreaterOrEqual(t, sfIdx, 0)
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2, "expected exactly 2 semifinals for 4 players")

	sf0WinnerSide := sf[0].SideA
	sf0Loser := sf[0].SideB
	sf1Winner := sf[1].SideB
	sf1Loser := sf[1].SideA

	require.NoError(t, eng.RecordMatchResult(compID, sf[0].ID, &state.MatchResult{
		Winner: sf0WinnerSide,
		Status: state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, sf[1].ID, &state.MatchResult{
		Winner: sf1Winner,
		Status: state.MatchStatusCompleted,
	}))

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	assert.Equal(t, sf0Loser, bracket.ThirdPlaceMatch.SideA, "first SF loser → bronze SideA")
	assert.Equal(t, sf1Loser, bracket.ThirdPlaceMatch.SideB, "second SF loser → bronze SideB")
	assert.True(t, bracketMatchPlayable(bracket.ThirdPlaceMatch), "bronze playable once both SF losers set")
}

// TestBronze_ScoreResolvesBronze verifies the bronze match can be scored via the
// record path (resolved through Bracket.ThirdPlaceMatch, not Rounds) with no
// spurious propagation.
func TestBronze_ScoreResolvesBronze(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-score"

	createBronzeTestCompetition(t, store, compID, true)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]

	require.NoError(t, eng.RecordMatchResult(compID, sf[0].ID, &state.MatchResult{
		Winner: sf[0].SideA, Status: state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, sf[1].ID, &state.MatchResult{
		Winner: sf[1].SideB, Status: state.MatchStatusCompleted,
	}))

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	bronzeWinner := bracket.ThirdPlaceMatch.SideA

	require.NoError(t, eng.RecordMatchResult(compID, "m-bronze", &state.MatchResult{
		Winner:  bronzeWinner,
		IpponsA: []string{"M"},
		Status:  state.MatchStatusCompleted,
	}))

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, bronzeWinner, bracket.ThirdPlaceMatch.Winner)
	assert.Equal(t, state.MatchStatusCompleted, bracket.ThirdPlaceMatch.Status)
}

// TestBronze_ScheduleIncludesBronze verifies GenerateSchedule counts the bronze
// match as an extra bracket bout.
func TestBronze_ScheduleIncludesBronze(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-sched"

	createBronzeTestCompetition(t, store, compID, true)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	schedule, err := store.LoadSchedule(compID)
	require.NoError(t, err)

	found := false
	for _, s := range schedule {
		if s.MatchRef == "Mm-bronze" {
			found = true
		}
	}
	assert.True(t, found, "schedule should include the bronze match (MatchRef Mm-bronze)")
}

// TestBronze_RoundTripPersistsThirdPlaceMatch verifies the bronze field survives
// a bracket save/load round-trip and is deep-copied (no aliasing).
func TestBronze_RoundTripPersistsThirdPlaceMatch(t *testing.T) {
	_, store, _ := setupTestEngine(t)
	compID := "bronze-roundtrip"
	createBronzeTestCompetition(t, store, compID, true)

	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "A", SideB: "B"}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:           "m-bronze",
			SideA:        "C",
			SideB:        "D",
			Status:       state.MatchStatusScheduled,
			DisplayRound: -1,
		},
	}
	require.NoError(t, store.SaveBracket(compID, b))

	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, loaded.ThirdPlaceMatch)
	assert.Equal(t, "m-bronze", loaded.ThirdPlaceMatch.ID)
	assert.Equal(t, "C", loaded.ThirdPlaceMatch.SideA)
	assert.Equal(t, "D", loaded.ThirdPlaceMatch.SideB)
	assert.Equal(t, -1, loaded.ThirdPlaceMatch.DisplayRound)

	// Mutating the loaded copy must not corrupt the cached bracket.
	loaded.ThirdPlaceMatch.Winner = "C"
	again, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Empty(t, again.ThirdPlaceMatch.Winner, "loaded bronze must be a deep copy")
}

// TestBronze_UpdateMatchCourtPersists verifies that UpdateMatchCourt works on
// "m-bronze" (finding 1+2: withBracketMatch must fall through to ThirdPlaceMatch).
func TestBronze_UpdateMatchCourtPersists(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-court"

	createBronzeTestCompetition(t, store, compID, true)
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Alice", SideB: "Bob"}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:           "m-bronze",
			SideA:        "Charlie",
			SideB:        "Dave",
			Status:       state.MatchStatusScheduled,
			Court:        "A",
			DisplayRound: -1,
		},
	}
	require.NoError(t, store.SaveBracket(compID, b))

	require.NoError(t, eng.UpdateMatchCourt(compID, "m-bronze", "B"))

	updated, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, updated.ThirdPlaceMatch)
	assert.Equal(t, "B", updated.ThirdPlaceMatch.Court, "UpdateMatchCourt must update bronze match court")
}

// TestBronze_UpdateMatchTimePersists verifies that UpdateMatchTime works on
// "m-bronze" (finding 1+2: withBracketMatch must fall through to ThirdPlaceMatch).
func TestBronze_UpdateMatchTimePersists(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-time"

	createBronzeTestCompetition(t, store, compID, true)
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Alice", SideB: "Bob"}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:           "m-bronze",
			SideA:        "Charlie",
			SideB:        "Dave",
			Status:       state.MatchStatusScheduled,
			DisplayRound: -1,
		},
	}
	require.NoError(t, store.SaveBracket(compID, b))

	require.NoError(t, eng.UpdateMatchTime(compID, "m-bronze", "14:30"))

	updated, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, updated.ThirdPlaceMatch)
	assert.Equal(t, "14:30", updated.ThirdPlaceMatch.ScheduledAt, "UpdateMatchTime must update bronze match time")
}

// TestBronze_OverrideBracketWinnerOnBronze verifies that OverrideBracketWinner
// works on "m-bronze" (finding 5: OverrideBracketWinner must fall through to
// ThirdPlaceMatch and set winner + IsOverridden without downstream propagation).
func TestBronze_OverrideBracketWinnerOnBronze(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-override"

	createBronzeTestCompetition(t, store, compID, true)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]

	// Score both semifinals to populate the bronze match sides.
	require.NoError(t, eng.RecordMatchResult(compID, sf[0].ID, &state.MatchResult{
		Winner: sf[0].SideA, Status: state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, sf[1].ID, &state.MatchResult{
		Winner: sf[1].SideB, Status: state.MatchStatusCompleted,
	}))

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	bronzeWinner := bracket.ThirdPlaceMatch.SideA

	// Override the bronze match winner.
	require.NoError(t, eng.OverrideBracketWinner(compID, "m-bronze", bronzeWinner))

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	assert.Equal(t, bronzeWinner, bracket.ThirdPlaceMatch.Winner, "OverrideBracketWinner must set bronze winner")
	assert.True(t, bracket.ThirdPlaceMatch.IsOverridden, "OverrideBracketWinner must set IsOverridden on bronze")
	assert.Equal(t, state.MatchStatusCompleted, bracket.ThirdPlaceMatch.Status, "OverrideBracketWinner must complete bronze")
}

// TestBronze_OverrideBracketWinnerNotReadyRejected verifies that overriding the
// bronze match before both SF losers are resolved returns a validation error.
func TestBronze_OverrideBracketWinnerNotReadyRejected(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-override-noready"

	createBronzeTestCompetition(t, store, compID, true)
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Alice", SideB: "Bob"}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:           "m-bronze",
			SideA:        "",
			SideB:        "",
			Status:       state.MatchStatusScheduled,
			DisplayRound: -1,
		},
	}
	require.NoError(t, store.SaveBracket(compID, b))

	err := eng.OverrideBracketWinner(compID, "m-bronze", "Alice")
	assert.Error(t, err, "overriding an unresolved bronze match must return an error")
}
