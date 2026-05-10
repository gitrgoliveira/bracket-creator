package engine

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoring_OverrideBracketWinner(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-override-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "test-override"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Test"}))

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "M1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
				{ID: "M2", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled},
			},
			{
				{ID: "M3", SideA: "", SideB: "", Status: state.MatchStatusScheduled},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	// Override M1 winner to Alice
	err = eng.OverrideBracketWinner(compID, "M1", "Alice")
	require.NoError(t, err)

	// Verify bracket updated and propagated
	updated, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", updated.Rounds[0][0].Winner)
	assert.True(t, updated.Rounds[0][0].IsOverridden)
	assert.Equal(t, "Alice", updated.Rounds[1][0].SideA)

	// Override M2 winner to Charlie
	err = eng.OverrideBracketWinner(compID, "M2", "Charlie")
	require.NoError(t, err)

	updated, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Charlie", updated.Rounds[1][0].SideB)

	// Test non-existent match
	err = eng.OverrideBracketWinner(compID, "M99", "Nobody")
	assert.Error(t, err)
}

func TestUpdateMatchCourt(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "test-court"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Test"}))

	// Setup pool match
	matches := []state.MatchResult{
		{ID: "P1-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A"},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
	require.NoError(t, store.SaveSchedule(compID, []state.ScheduleEntry{{MatchRef: "P1-1", Court: "A"}}))

	// Update court
	err = eng.UpdateMatchCourt(compID, "P1-1", "B")
	require.NoError(t, err)

	// Verify updated
	updatedMatches, _ := store.LoadPoolMatches(compID)
	assert.Equal(t, "B", updatedMatches[0].Court)
	schedule, _ := store.LoadSchedule(compID)
	assert.Equal(t, "B", schedule[0].Court)

	// Setup bracket match
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{{{ID: "B1", SideA: "Alice", SideB: "Bob", Court: "A"}}},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))
	// Save both entries to avoid overwriting
	require.NoError(t, store.SaveSchedule(compID, []state.ScheduleEntry{
		{MatchRef: "P1-1", Court: "B"},
		{MatchRef: "R1-MB1", Court: "A"},
	}))

	err = eng.UpdateMatchCourt(compID, "B1", "C")
	require.NoError(t, err)

	updatedBracket, _ := store.LoadBracket(compID)
	assert.Equal(t, "C", updatedBracket.Rounds[0][0].Court)
	schedule, _ = store.LoadSchedule(compID)
	assert.Equal(t, "C", schedule[1].Court)
}

func TestUpdateMatchTime(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-time-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "test-time"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Test"}))

	// Pool match
	matches := []state.MatchResult{{ID: "P1-1", Status: state.MatchStatusScheduled}}
	require.NoError(t, store.SavePoolMatches(compID, matches))
	err = eng.UpdateMatchTime(compID, "P1-1", "10:00")
	require.NoError(t, err)
	updated, _ := store.LoadPoolMatches(compID)
	assert.Equal(t, "10:00", updated[0].ScheduledAt)

	// Bracket match
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{{{ID: "B1", Status: state.MatchStatusScheduled}}}}
	require.NoError(t, store.SaveBracket(compID, bracket))
	err = eng.UpdateMatchTime(compID, "B1", "11:00")
	require.NoError(t, err)
	updatedB, _ := store.LoadBracket(compID)
	assert.Equal(t, "11:00", updatedB.Rounds[0][0].ScheduledAt)
}
