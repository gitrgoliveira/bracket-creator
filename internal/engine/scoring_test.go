package engine

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
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

func TestScoreSummary_Individual(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-summary-ind-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "ind-summary"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Ind", TeamSize: 0}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "PoolA", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "PoolA-1", SideA: "Alice", SideB: "Bob",
			Winner: "Alice", IpponsA: []string{"M", "K"}, IpponsB: []string{"D"},
			Status: state.MatchStatusCompleted,
		},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	pool := standings["PoolA"]
	require.Len(t, pool, 2)

	alice := pool[0]
	assert.Equal(t, "Alice", alice.Player.Name)
	assert.Equal(t, "W:1 L:0 D:0 | P:2-1", alice.ScoreSummary)

	bob := pool[1]
	assert.Equal(t, "Bob", bob.Player.Name)
	assert.Equal(t, "W:0 L:1 D:0 | P:1-2", bob.ScoreSummary)
}

func TestScoreSummary_Team(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-summary-team-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "team-summary"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Team", TeamSize: 3}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "PoolA", Players: []helper.Player{{Name: "TeamA"}, {Name: "TeamB"}}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "PoolA-1", SideA: "TeamA", SideB: "TeamB",
			Winner: "TeamA", Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{
				{Position: 1, Winner: "TeamA"},
				{Position: 2, Winner: "TeamA"},
				{Position: 3, Winner: "TeamB"},
			},
		},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	pool := standings["PoolA"]
	require.Len(t, pool, 2)

	teamA := pool[0]
	assert.Equal(t, "TeamA", teamA.Player.Name)
	assert.Equal(t, "W:1 L:0 D:0 | IV:2 IL:1 IT:0 | PW:0 PL:0", teamA.ScoreSummary)

	teamB := pool[1]
	assert.Equal(t, "TeamB", teamB.Player.Name)
	assert.Equal(t, "W:0 L:1 D:0 | IV:1 IL:2 IT:0 | PW:0 PL:0", teamB.ScoreSummary)
}

func TestMaybeAutoCompletePools(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-autocomplete-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "auto-complete"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Auto", Format: "pools", Status: state.CompStatusPools,
	}))

	t.Run("no transition while a pool match is still scheduled", func(t *testing.T) {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-1", Status: state.MatchStatusCompleted, Winner: "Alice", SideA: "Alice", SideB: "Bob"},
			{ID: "P1-2", Status: state.MatchStatusScheduled, SideA: "Alice", SideB: "Charlie"},
		}))
		done, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		assert.False(t, done)
		comp, _ := store.LoadCompetition(compID)
		assert.Equal(t, state.CompStatusPools, comp.Status)
	})

	t.Run("transitions to complete when all pool matches are completed", func(t *testing.T) {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-1", Status: state.MatchStatusCompleted, Winner: "Alice", SideA: "Alice", SideB: "Bob"},
			{ID: "P1-2", Status: state.MatchStatusCompleted, Winner: "Alice", SideA: "Alice", SideB: "Charlie"},
		}))
		done, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		assert.True(t, done)
		comp, _ := store.LoadCompetition(compID)
		assert.Equal(t, state.CompStatusComplete, comp.Status)
	})

	t.Run("is a no-op once already complete (idempotent)", func(t *testing.T) {
		done, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		assert.False(t, done)
	})

	t.Run("ignored for playoffs-format competitions", func(t *testing.T) {
		koID := "auto-complete-ko"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: koID, Name: "KO", Format: "playoffs", Status: state.CompStatusPlayoffs,
		}))
		require.NoError(t, store.SavePoolMatches(koID, []state.MatchResult{
			{ID: "M1", Status: state.MatchStatusCompleted, Winner: "X", SideA: "X", SideB: "Y"},
		}))
		done, err := eng.MaybeAutoCompletePools(koID)
		require.NoError(t, err)
		assert.False(t, done)
		comp, _ := store.LoadCompetition(koID)
		assert.Equal(t, state.CompStatusPlayoffs, comp.Status)
	})
}

// Regression test for the bug where scoring a match cleared its court and
// scheduledAt because the UI payload omits those fields. RecordMatchResult
// must preserve them when the incoming MatchResult has empty values.
func TestRecordMatchResult_PreservesCourtAndScheduledAt(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-preserve-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "preserve-test"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Preserve"}))

	t.Run("pool match preserves Court and ScheduledAt", func(t *testing.T) {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID: "P1-1", SideA: "Alice", SideB: "Bob",
				Status: state.MatchStatusScheduled,
				Court:  "A", ScheduledAt: "09:30",
			},
		}))

		// Scoring UI sends a patch with no Court / ScheduledAt.
		patch := &state.MatchResult{
			Winner:  "Alice",
			IpponsA: []string{"M"},
			IpponsB: []string{},
			Status:  state.MatchStatusCompleted,
		}
		require.NoError(t, eng.RecordMatchResult(compID, "P1-1", patch))

		// Persisted match keeps the original scheduling fields.
		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		assert.Equal(t, "A", stored[0].Court)
		assert.Equal(t, "09:30", stored[0].ScheduledAt)
		// Patch is also mutated in place so the broadcast carries the merged value.
		assert.Equal(t, "A", patch.Court)
		assert.Equal(t, "09:30", patch.ScheduledAt)
	})

	t.Run("bracket match preserves Court and ScheduledAt", func(t *testing.T) {
		bracket := &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{{ID: "B1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:15"}},
			},
		}
		require.NoError(t, store.SaveBracket(compID, bracket))

		patch := &state.MatchResult{
			Winner:  "Bob",
			IpponsB: []string{"K"},
			Status:  state.MatchStatusCompleted,
		}
		require.NoError(t, eng.RecordMatchResult(compID, "B1", patch))

		stored, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, "B", stored.Rounds[0][0].Court)
		assert.Equal(t, "10:15", stored.Rounds[0][0].ScheduledAt)
		// Patch is mutated in place so the SSE broadcast can echo the
		// scheduling fields the scoring UI never sent.
		assert.Equal(t, "B", patch.Court)
		assert.Equal(t, "10:15", patch.ScheduledAt)
	})
}
