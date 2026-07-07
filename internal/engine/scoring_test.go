package engine

import (
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
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
	err = eng.OverrideBracketWinner(compID, "M1", "Alice", 0)
	require.NoError(t, err)

	// Verify bracket updated and propagated
	updated, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", updated.Rounds[0][0].Winner)
	assert.True(t, updated.Rounds[0][0].IsOverridden)
	assert.Equal(t, "Alice", updated.Rounds[1][0].SideA)

	// Override M2 winner to Charlie
	err = eng.OverrideBracketWinner(compID, "M2", "Charlie", 0)
	require.NoError(t, err)

	updated, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Charlie", updated.Rounds[1][0].SideB)

	// Test non-existent match
	err = eng.OverrideBracketWinner(compID, "M99", "Nobody", 0)
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

func TestTeamStandings_EmptySubSidesDrawNotFalseWin(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-empty-sub-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "empty-sub-draw"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Team", TeamSize: 3,
		Format: state.CompFormatLeague, Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "PoolA", Players: []helper.Player{{Name: "TeamA"}, {Name: "TeamB"}}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "PoolA-1", SideA: "TeamA", SideB: "TeamB",
			Winner: "", Decision: "hikiwake",
			Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{
				{Position: 1, Winner: "TeamA"},
				{Position: 2, Winner: "TeamB"},
				{Position: 3, Winner: ""},
			},
		},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	pool := standings["PoolA"]
	require.Len(t, pool, 2)

	var teamA, teamB state.PlayerStanding
	for _, s := range pool {
		switch s.Player.Name {
		case "TeamA":
			teamA = s
		case "TeamB":
			teamB = s
		}
	}
	assert.Equal(t, 1, teamA.IndividualWins, "sub with Winner=TeamA → IV for A")
	assert.Equal(t, 1, teamA.IndividualLosses, "sub with Winner=TeamB → IL for A")
	assert.Equal(t, 1, teamA.IndividualDraws, "sub with empty Winner+empty SideA → draw, not false win")

	assert.Equal(t, 1, teamB.IndividualWins)
	assert.Equal(t, 1, teamB.IndividualLosses)
	assert.Equal(t, 1, teamB.IndividualDraws, "sub with empty Winner+empty SideB → draw, not false win")
}

// TestTeamStandings_PlaceholderIpponsDontInflatePoints pins that a completed
// team bout that still carries "•" unfilled-slot placeholders (or empty
// entries) alongside real ippons counts ONLY the real ippons toward the pool
// standings PointsWon/PointsLost. Using len() would count the placeholders and
// inflate the tie-break points, and diverge from the wire teamResult PW (which
// state.TeamResultFrom already computes via countScoringIppons).
func TestTeamStandings_PlaceholderIpponsDontInflatePoints(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-placeholder-pts-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "placeholder-pts"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Team", TeamSize: 2,
		Format: state.CompFormatLeague, Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "PoolA", Players: []helper.Player{{Name: "TeamA"}, {Name: "TeamB"}}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "PoolA-1", SideA: "TeamA", SideB: "TeamB",
			Winner: "TeamA", Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{
				// TeamA scored one real ippon ("M") plus an unfilled "•" slot
				// and an empty slot; only the "M" is a real point.
				{Position: 1, Winner: "TeamA", IpponsA: []string{"M", "•", ""}, IpponsB: []string{"•"}},
				// TeamB scored one real ippon ("K"); TeamA has two "•" placeholders.
				{Position: 2, Winner: "TeamB", IpponsA: []string{"•", "•"}, IpponsB: []string{"K"}},
			},
		},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	pool := standings["PoolA"]
	require.Len(t, pool, 2)

	var teamA, teamB state.PlayerStanding
	for _, s := range pool {
		switch s.Player.Name {
		case "TeamA":
			teamA = s
		case "TeamB":
			teamB = s
		}
	}
	// TeamA (SideA): real ippons across subs = 1 ("M"); placeholders excluded.
	assert.Equal(t, 1, teamA.PointsWon, "only the real 'M' ippon counts, not the '•'/empty slots")
	// TeamA points lost = TeamB's real ippons = 1 ("K"); TeamB's "•" excluded.
	assert.Equal(t, 1, teamA.PointsLost, "only TeamB's real 'K' ippon counts against A")
	assert.Equal(t, 1, teamB.PointsWon, "only the real 'K' ippon counts for B")
	assert.Equal(t, 1, teamB.PointsLost, "only TeamA's real 'M' ippon counts against B")
}

func TestMaybeAutoCompletePools(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-autocomplete-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	// Use league format, league auto-completes after all pool matches.
	// Mixed format no longer auto-completes: it stays in pools status until
	// pool finishers are seeded into the knockout incrementally as pools complete.
	compID := "auto-complete"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Auto", Format: state.CompFormatLeague, Status: state.CompStatusPools,
	}))

	t.Run("no transition while a pool match is still scheduled", func(t *testing.T) {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-1", Status: state.MatchStatusCompleted, Winner: "Alice", SideA: "Alice", SideB: "Bob"},
			{ID: "P1-2", Status: state.MatchStatusScheduled, SideA: "Alice", SideB: "Charlie"},
		}))
		outcome, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteNoChange, outcome)
		comp, _ := store.LoadCompetition(compID)
		assert.Equal(t, state.CompStatusPools, comp.Status)
	})

	t.Run("transitions to complete when all pool matches are completed", func(t *testing.T) {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-1", Status: state.MatchStatusCompleted, Winner: "Alice", SideA: "Alice", SideB: "Bob"},
			{ID: "P1-2", Status: state.MatchStatusCompleted, Winner: "Alice", SideA: "Alice", SideB: "Charlie"},
		}))
		outcome, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteTransitioned, outcome)
		comp, _ := store.LoadCompetition(compID)
		assert.Equal(t, state.CompStatusComplete, comp.Status)
	})

	t.Run("is a no-op once already complete (idempotent)", func(t *testing.T) {
		outcome, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteNoChange, outcome)
	})

	t.Run("ignored for playoffs-format competitions", func(t *testing.T) {
		koID := "auto-complete-ko"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: koID, Name: "KO", Format: state.CompFormatPlayoffs, Status: state.CompStatusPlayoffs,
		}))
		require.NoError(t, store.SavePoolMatches(koID, []state.MatchResult{
			{ID: "M1", Status: state.MatchStatusCompleted, Winner: "X", SideA: "X", SideB: "Y"},
		}))
		outcome, err := eng.MaybeAutoCompletePools(koID)
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteNoChange, outcome)
		comp, _ := store.LoadCompetition(koID)
		assert.Equal(t, state.CompStatusPlayoffs, comp.Status)
	})

	// (Mixed-format MaybeAutoCompletePools behavior is covered in
	// knockout_test.go: TestMaybeAutoCompletePools_MixedFormat_StaysInPools_WhileScheduled
	// and TestMaybeAutoCompletePools_MixedFormat_AutoStartsKnockout, auto-start
	// fires when pools are clean, no transition while a match is pending.)

	t.Run("transitions when there are zero pool matches", func(t *testing.T) {
		// e.g. a single-participant league comp where no match was generated.
		// Without this branch the competition would be stuck in `pools` forever.
		emptyID := "auto-complete-empty"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: emptyID, Name: "Empty", Format: state.CompFormatLeague, Status: state.CompStatusPools,
		}))
		require.NoError(t, store.SavePoolMatches(emptyID, []state.MatchResult{}))
		outcome, err := eng.MaybeAutoCompletePools(emptyID)
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteTransitioned, outcome)
		comp, _ := store.LoadCompetition(emptyID)
		assert.Equal(t, state.CompStatusComplete, comp.Status)
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

// TestRecordMatchResult_RejectsSideRewrite pins the match-identity guard:
// a score payload may carry the result but must never rewrite WHO is in the
// match. A payload naming a different competitor is rejected with
// ErrMatchSideMismatch and the stored pairing is left untouched. This is the
// guard against the cross-pool corruption vector (a stored bout overwritten
// with competitors from another pool).
func TestRecordMatchResult_RejectsSideRewrite(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-sidemismatch-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "mismatch-test"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Mismatch"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool E-0", SideA: "Benjamin Evans", SideB: "Sebastian Allen", Status: state.MatchStatusScheduled, Court: "A"},
	}))

	t.Run("payload with foreign competitors is rejected and stored pairing preserved", func(t *testing.T) {
		bad := &state.MatchResult{
			SideA:   "Arthur Conan",    // from another pool
			SideB:   "Herman Melville", // from another pool
			Winner:  "Arthur Conan",
			IpponsA: []string{"M"},
			Status:  state.MatchStatusCompleted,
		}
		err := eng.RecordMatchResult(compID, "Pool E-0", bad)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMatchSideMismatch)

		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		// Identity untouched; the bogus result was not persisted.
		assert.Equal(t, "Benjamin Evans", stored[0].SideA)
		assert.Equal(t, "Sebastian Allen", stored[0].SideB)
		assert.Equal(t, state.MatchStatusScheduled, stored[0].Status)
		assert.Empty(t, stored[0].Winner)
	})

	t.Run("payload that omits sides backfills from the stored pairing and scores", func(t *testing.T) {
		patch := &state.MatchResult{
			Winner:  "Benjamin Evans",
			IpponsA: []string{"M"},
			Status:  state.MatchStatusCompleted,
		}
		require.NoError(t, eng.RecordMatchResult(compID, "Pool E-0", patch))
		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		assert.Equal(t, "Benjamin Evans", stored[0].SideA)
		assert.Equal(t, "Sebastian Allen", stored[0].SideB)
		assert.Equal(t, "Benjamin Evans", stored[0].Winner)
		assert.Equal(t, state.MatchStatusCompleted, stored[0].Status)
	})

	t.Run("payload echoing the correct sides scores normally", func(t *testing.T) {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool E-1", SideA: "Benjamin Evans", SideB: "Kurt Vonnegut", Status: state.MatchStatusScheduled},
		}))
		patch := &state.MatchResult{
			SideA:   "Benjamin Evans",
			SideB:   "Kurt Vonnegut",
			Winner:  "Kurt Vonnegut",
			IpponsB: []string{"K"},
			Status:  state.MatchStatusCompleted,
		}
		require.NoError(t, eng.RecordMatchResult(compID, "Pool E-1", patch))
		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		var m1 state.MatchResult
		for _, s := range stored {
			if s.ID == "Pool E-1" {
				m1 = s
			}
		}
		assert.Equal(t, "Kurt Vonnegut", m1.Winner)
		assert.Equal(t, state.MatchStatusCompleted, m1.Status)
	})
}

// TestRecordMatchResult_ConcurrentScoresNotLost pins the TOCTOU fix for
// the live-scoring path. Pre-atomic-primitive, withPoolMatch did
// LoadPoolMatches → mutate target match → SavePoolMatches sequentially
// with no lock held between Load and Save. Two operators scoring
// DIFFERENT matches on DIFFERENT courts could each load the full pool-
// matches slice into a separate copy, mutate their target, and save,
// the later save would overwrite the earlier save's mutation with stale
// data for the OTHER match. One operator's score: silently lost.
//
// Now that withPoolMatch delegates to state.Store.UpdatePoolMatchByID,
// the entire load + find + mutate + save sequence runs under the
// per-competition lock. Both mutations land regardless of arrival
// order or how the goroutines interleave.
//
// Runs many iterations to exercise the scheduler. With the fix, every
// iteration must end with both M1 and M2 marked completed.
func TestRecordMatchResult_ConcurrentScoresNotLost(t *testing.T) {
	const iterations = 20

	for i := range iterations {
		dir, err := os.MkdirTemp("", "engine-concurrent-*")
		require.NoError(t, err)
		// Register cleanup immediately so a `require.*` failure later
		// in the iteration doesn't leak the directory. Was previously
		// only removed at the end of the loop body.
		t.Cleanup(func() { os.RemoveAll(dir) })

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		eng := New(store)

		compID := "concurrent-test"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Concurrent"}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A"},
			{ID: "Pool-2", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B"},
		}))

		var wg sync.WaitGroup
		wg.Add(2)

		// Operator on Court A scores Pool-1: Alice wins.
		go func() {
			defer wg.Done()
			res := &state.MatchResult{
				Winner:  "Alice",
				IpponsA: []string{"M"},
				Status:  state.MatchStatusCompleted,
			}
			err := eng.RecordMatchResult(compID, "Pool-1", res)
			assert.NoErrorf(t, err, "iter %d: Pool-1 score should succeed", i)
		}()

		// Operator on Court B scores Pool-2: Dave wins.
		go func() {
			defer wg.Done()
			res := &state.MatchResult{
				Winner:  "Dave",
				IpponsB: []string{"K"},
				Status:  state.MatchStatusCompleted,
			}
			err := eng.RecordMatchResult(compID, "Pool-2", res)
			assert.NoErrorf(t, err, "iter %d: Pool-2 score should succeed", i)
		}()
		wg.Wait()

		// Both mutations must have landed on disk regardless of which
		// goroutine acquired the per-competition lock first. Pre-fix:
		// one of the two saves would silently lose its mutation because
		// it read pool-matches.csv before the OTHER goroutine wrote
		// it, then saved a slice with the other match in its original
		// (scheduled, no-winner) state.
		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Lenf(t, stored, 2, "iter %d: both pool matches must still exist", i)

		var p1, p2 *state.MatchResult
		for idx := range stored {
			switch stored[idx].ID {
			case "Pool-1":
				p1 = &stored[idx]
			case "Pool-2":
				p2 = &stored[idx]
			}
		}
		require.NotNilf(t, p1, "iter %d: Pool-1 must exist on disk", i)
		require.NotNilf(t, p2, "iter %d: Pool-2 must exist on disk", i)
		assert.Equalf(t, "Alice", p1.Winner, "iter %d: Pool-1 winner must be Alice (Operator A's score)", i)
		assert.Equalf(t, state.MatchStatusCompleted, p1.Status, "iter %d: Pool-1 must be completed", i)
		assert.Equalf(t, "Dave", p2.Winner, "iter %d: Pool-2 winner must be Dave (Operator B's score)", i)
		assert.Equalf(t, state.MatchStatusCompleted, p2.Status, "iter %d: Pool-2 must be completed", i)
		// Cleanup registered via t.Cleanup at iteration start.
	}
}

// Same shape as the pool-match concurrent test, but for the bracket
// path. Two operators scoring different elimination-round matches in
// the same competition: both winners must land.
func TestRecordMatchResult_ConcurrentBracketScoresNotLost(t *testing.T) {
	const iterations = 20

	for i := range iterations {
		dir, err := os.MkdirTemp("", "engine-concurrent-bracket-*")
		require.NoError(t, err)
		t.Cleanup(func() { os.RemoveAll(dir) })

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		eng := New(store)

		compID := "concurrent-bracket"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Concurrent Bracket"}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{ID: "QF1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A"},
					{ID: "QF2", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B"},
				},
			},
		}))

		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			res := &state.MatchResult{
				Winner:  "Alice",
				IpponsA: []string{"M"},
				Status:  state.MatchStatusCompleted,
			}
			err := eng.RecordMatchResult(compID, "QF1", res)
			assert.NoErrorf(t, err, "iter %d: QF1 score should succeed", i)
		}()
		go func() {
			defer wg.Done()
			res := &state.MatchResult{
				Winner:  "Dave",
				IpponsB: []string{"K"},
				Status:  state.MatchStatusCompleted,
			}
			err := eng.RecordMatchResult(compID, "QF2", res)
			assert.NoErrorf(t, err, "iter %d: QF2 score should succeed", i)
		}()
		wg.Wait()

		stored, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.Len(t, stored.Rounds, 1)
		require.Len(t, stored.Rounds[0], 2)

		var qf1, qf2 *state.BracketMatch
		for idx := range stored.Rounds[0] {
			switch stored.Rounds[0][idx].ID {
			case "QF1":
				qf1 = &stored.Rounds[0][idx]
			case "QF2":
				qf2 = &stored.Rounds[0][idx]
			}
		}
		require.NotNilf(t, qf1, "iter %d: QF1 must exist", i)
		require.NotNilf(t, qf2, "iter %d: QF2 must exist", i)
		assert.Equalf(t, "Alice", qf1.Winner, "iter %d: QF1 winner must be Alice", i)
		assert.Equalf(t, state.MatchStatusCompleted, qf1.Status, "iter %d: QF1 must be completed", i)
		assert.Equalf(t, "Dave", qf2.Winner, "iter %d: QF2 winner must be Dave", i)
		assert.Equalf(t, state.MatchStatusCompleted, qf2.Status, "iter %d: QF2 must be completed", i)
		// Cleanup registered via t.Cleanup at iteration start.
	}
}

func TestRecordMatchResult_BracketResultSourcePropagated(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-bracket-resultsource-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "rs-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "RS Bracket"}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "SF1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled}},
		},
	}))

	err = eng.RecordMatchResult(compID, "SF1", &state.MatchResult{
		Winner:       "Alice",
		IpponsA:      []string{"M", "K"},
		Status:       state.MatchStatusCompleted,
		ResultSource: "self-reported",
	})
	require.NoError(t, err)

	stored, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, stored.Rounds[0], 1)
	assert.Equal(t, "self-reported", stored.Rounds[0][0].ResultSource)
}

// TestMaybeAutoCompletePools_ConcurrentInvalidateNotLost pins the
// TOCTOU fix in engine.MaybeAutoCompletePools. Pre-atomic-primitive,
// the LoadCompetition + status check + SaveCompetitionChanged
// sequence had a window where a concurrent admin invalidate (POST
// /invalidate) could land between the read and the write, admin's
// "invalid" status would then be silently overwritten back to
// "complete" by the auto-complete save.
//
// The fix wraps the status read + status set + save in
// state.Store.UpdateCompetitionChanged. The transform re-checks
// `current.Status == Pools` UNDER the lock; if the admin's
// invalidate already moved Status to Invalid, the auto-complete
// transform sees the new value and returns (nil, nil), no save.
func TestMaybeAutoCompletePools_ConcurrentInvalidateNotLost(t *testing.T) {
	const iterations = 20

	for i := range iterations {
		dir, err := os.MkdirTemp("", "engine-autocomplete-race-*")
		require.NoError(t, err)
		t.Cleanup(func() { os.RemoveAll(dir) })

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		eng := New(store)

		compID := "auto-vs-invalidate"
		// Use league format: league auto-completes after all pool matches.
		// Mixed format does not auto-complete after pools; the knockout fills in incrementally.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "Auto vs Invalidate",
			Format: state.CompFormatLeague, Status: state.CompStatusPools,
		}))
		// All matches already completed, auto-complete is eligible.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-1", Status: state.MatchStatusCompleted, Winner: "Alice", SideA: "Alice", SideB: "Bob"},
		}))

		// Capture each operation's "did I actually commit a change?"
		// return value. The race contract is:
		//   - At most ONE goroutine can report changed=true. If both
		//     reported true, the second one's transform either ran
		//     against the first's already-committed Status (auto-complete
		//     sees Status=Invalid → returns (nil, nil) → changed=false),
		//     or the invalidate sees Status=Complete → returns (nil, nil)
		//     → changed=false. Both reporting changed=true means a save
		//     was silently lost.
		//   - The final disk Status MUST match the winner's commit:
		//       autoCompleted=true  → stored.Status == Complete
		//       invalidated=true    → stored.Status == Invalid
		//     Pre-fix this assertion would accept Complete even when
		//     invalidate landed first, exactly the regression Copilot
		//     flagged. Linking final status to the winner's return value
		//     forces the test to fail if auto-complete overwrites a
		//     committed invalidate.
		var autoCompleted bool
		var invalidated bool
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			outcome, err := eng.MaybeAutoCompletePools(compID)
			assert.NoErrorf(t, err, "iter %d: MaybeAutoCompletePools error", i)
			autoCompleted = outcome == AutoCompleteTransitioned
		}()
		go func() {
			defer wg.Done()
			// Admin invalidate: simulate via direct UpdateCompetitionChanged
			// (mirrors what the POST /invalidate handler does).
			c, err := store.UpdateCompetitionChanged(compID, func(current *state.Competition) (*state.Competition, error) {
				if current == nil || (current.Status != state.CompStatusPools && current.Status != state.CompStatusPlayoffs) {
					return nil, nil
				}
				current.Status = state.CompStatusInvalid
				return current, nil
			})
			assert.NoErrorf(t, err, "iter %d: invalidate error", i)
			invalidated = c
		}()
		wg.Wait()

		stored, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		require.NotNil(t, stored)

		// Contract 1: at most one goroutine committed. Both committing
		// means a save was lost (the racing transform should have
		// returned (nil, nil) after observing the other's update).
		assert.False(t, autoCompleted && invalidated,
			"iter %d: both operations reported changed=true, one must have observed the other's update and returned (nil, nil)",
			i)

		// Contract 2: final status matches the winner. This is what
		// Copilot's review caught: previously the test accepted Complete
		// even when invalidate had won, so an auto-complete-clobbers-
		// invalidate regression would silently slip through.
		switch {
		case autoCompleted && !invalidated:
			assert.Equal(t, state.CompStatusComplete, stored.Status,
				"iter %d: auto-complete reported changed=true so final status must be Complete", i)
		case invalidated && !autoCompleted:
			assert.Equal(t, state.CompStatusInvalid, stored.Status,
				"iter %d: invalidate reported changed=true so final status must be Invalid", i)
		case !autoCompleted && !invalidated:
			// Both transforms returned (nil, nil). Status should still
			// be Pools (neither side committed). This is a rare but
			// legal outcome if both transforms read Pools, both decided
			// to write, but both saw the other's commit before their
			// own SaveCompetitionChanged content-equality check.
			// Actually with the changed=bool contract, this case means
			// nobody committed at all, which can't happen here
			// because at least one must succeed (the matches ARE
			// completed and the comp IS in Pools). Treat as test bug
			// rather than a tolerated case.
			t.Fatalf("iter %d: neither operation committed, pre-condition broken (status stayed %q)",
				i, stored.Status)
		}
		// Cleanup registered via t.Cleanup at iteration start.
	}
}

// TestMaybeLockTeamLineupsForRound_TeamComp verifies that recording a
// running/completed result for a team competition (TeamSize > 0) exercises
// the maybeLockTeamLineupsForRound code path without panicking. We don't
// assert lineup lock state because the lock is a best-effort write (no
// LineupID known until Slice 7.C lands), so we just verify no error.
func TestMaybeLockTeamLineupsForRound_TeamComp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "team-lock-lineups"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Team Lock",
		Kind:     "team",
		Format:   state.CompFormatMixed,
		TeamSize: 3,
		Status:   state.CompStatusPools,
		Courts:   []string{"A"},
	}))

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "P1-0", SideA: "TeamA", SideB: "TeamB", Status: state.MatchStatusScheduled},
	}))

	result := &state.MatchResult{
		ID:     "P1-0",
		SideA:  "TeamA",
		SideB:  "TeamB",
		Winner: "TeamA",
		Status: state.MatchStatusCompleted,
	}
	err := eng.RecordMatchResult(compID, "P1-0", result)
	assert.NoError(t, err)
}

func TestApplyHansokuIppons(t *testing.T) {
	t.Run("nil result is a no-op", func(t *testing.T) {
		applyHansokuIppons(nil) // must not panic
	})

	cases := []struct {
		name        string
		hansokuA    int
		hansokuB    int
		ipponsA     []string
		ipponsB     []string
		wantIpponsA []string
		wantIpponsB []string
	}{
		{
			name:        "HansokuA=1 no award",
			hansokuA:    1,
			wantIpponsB: nil,
		},
		{
			name:        "HansokuA=2 awards 1 H to IpponsB",
			hansokuA:    2,
			wantIpponsB: []string{"H"},
		},
		{
			name:        "HansokuA=4 awards 2 H to IpponsB",
			hansokuA:    4,
			wantIpponsB: []string{"H", "H"},
		},
		{
			name:        "HansokuA=2 with existing H, no duplicate",
			hansokuA:    2,
			ipponsB:     []string{"H"},
			wantIpponsB: []string{"H"},
		},
		{
			name:        "HansokuA=2 existing non-H ippons preserved",
			hansokuA:    2,
			ipponsB:     []string{"M"},
			wantIpponsB: []string{"M", "H"},
		},
		{
			name:        "HansokuB=2 awards 1 H to IpponsA",
			hansokuB:    2,
			wantIpponsA: []string{"H"},
		},
		{
			name:        "HansokuB=4 awards 2 H to IpponsA",
			hansokuB:    4,
			wantIpponsA: []string{"H", "H"},
		},
		{
			name:        "HansokuB=2 with existing H, no duplicate",
			hansokuB:    2,
			ipponsA:     []string{"H"},
			wantIpponsA: []string{"H"},
		},
		{
			name:        "both sides accumulate a pair simultaneously",
			hansokuA:    2,
			hansokuB:    2,
			wantIpponsA: []string{"H"},
			wantIpponsB: []string{"H"},
		},
		{
			name:        "hansoku reduced from 4 to 2 strips excess H",
			hansokuA:    2,
			ipponsB:     []string{"M", "H", "H"},
			wantIpponsB: []string{"M", "H"},
		},
		{
			name:        "hansoku reduced to 0 strips all H entries",
			hansokuA:    0,
			ipponsB:     []string{"M", "H", "H"},
			wantIpponsB: []string{"M"},
		},
		{
			name:        "hansoku reduced to 1 strips interleaved H entries",
			hansokuA:    1,
			ipponsB:     []string{"H", "M", "H"},
			wantIpponsB: []string{"M"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &state.MatchResult{
				HansokuA: tc.hansokuA,
				HansokuB: tc.hansokuB,
				IpponsA:  tc.ipponsA,
				IpponsB:  tc.ipponsB,
			}
			applyHansokuIppons(r)
			assert.Equal(t, tc.wantIpponsA, r.IpponsA)
			assert.Equal(t, tc.wantIpponsB, r.IpponsB)
		})
	}

	t.Run("sub-results also get hansoku auto-award", func(t *testing.T) {
		r := &state.MatchResult{
			SubResults: []state.SubMatchResult{
				{HansokuA: 2, IpponsB: []string{"M"}},
				{HansokuB: 4, IpponsA: []string{"K"}},
			},
		}
		applyHansokuIppons(r)
		assert.Equal(t, []string{"M", "H"}, r.SubResults[0].IpponsB)
		assert.Equal(t, []string{"K", "H", "H"}, r.SubResults[1].IpponsA)
	})
}

// TestRecordMatchResult_HansokuAutoAward verifies that saving a pool match
// with HansokuA=2 via RecordMatchResult persists IpponsB=["H"] to the store.
func TestRecordMatchResult_HansokuAutoAward(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	compID := "hansoku-award"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Hansoku"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "P1-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
	}))

	t.Run("HansokuA=2 persists H ippon in IpponsB", func(t *testing.T) {
		patch := &state.MatchResult{
			Winner:   "Alice",
			HansokuA: 2,
			IpponsA:  []string{"M"},
			Status:   state.MatchStatusCompleted,
		}
		require.NoError(t, eng.RecordMatchResult(compID, "P1-1", patch))

		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		assert.Equal(t, []string{"H"}, stored[0].IpponsB)
		assert.Equal(t, []string{"M"}, stored[0].IpponsA)
	})

	t.Run("HansokuB=2 persists H ippon in IpponsA", func(t *testing.T) {
		patch := &state.MatchResult{
			Winner:   "Bob",
			HansokuB: 2,
			IpponsB:  []string{"K"},
			Status:   state.MatchStatusCompleted,
		}
		require.NoError(t, eng.RecordMatchResult(compID, "P1-1", patch))

		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		assert.Equal(t, []string{"H"}, stored[0].IpponsA)
		assert.Equal(t, []string{"K"}, stored[0].IpponsB)
	})
}

// TestHansokuCarriesIntoEncho pins FIK Article 17/20: hansoku are cumulative
// for the duration of the shiai, including encho. applyHansokuIppons must
// apply the 2-hansoku→ippon rule regardless of the Encho field value.
func TestHansokuCarriesIntoEncho(t *testing.T) {
	encho1 := &state.EnchoMetadata{PeriodCount: 1}
	encho2 := &state.EnchoMetadata{PeriodCount: 2}

	cases := []struct {
		name        string
		hansokuA    int
		hansokuB    int
		ipponsA     []string
		ipponsB     []string
		encho       *state.EnchoMetadata
		wantIpponsA []string
		wantIpponsB []string
	}{
		{
			name:        "regulation: HansokuA=1 no ippon",
			hansokuA:    1,
			encho:       nil,
			wantIpponsB: nil,
		},
		{
			name:        "encho begins: HansokuA=1 still no ippon, count preserved",
			hansokuA:    1,
			encho:       encho1,
			wantIpponsB: nil,
		},
		{
			name:        "2nd hansoku in encho period 1 fires ippon",
			hansokuA:    2,
			encho:       encho1,
			wantIpponsB: []string{"H"},
		},
		{
			name:        "cumulative: 2nd hansoku in encho period 2 fires ippon",
			hansokuA:    2,
			encho:       encho2,
			wantIpponsB: []string{"H"},
		},
		{
			name:        "4 hansoku across encho periods awards 2 ippons",
			hansokuA:    4,
			encho:       encho2,
			wantIpponsB: []string{"H", "H"},
		},
		{
			name:        "both sides accumulate during encho",
			hansokuA:    2,
			hansokuB:    2,
			encho:       encho1,
			wantIpponsA: []string{"H"},
			wantIpponsB: []string{"H"},
		},
		{
			name:        "HansokuB=2 in encho fires ippon for SideA",
			hansokuB:    2,
			encho:       encho1,
			wantIpponsA: []string{"H"},
		},
		{
			name:        "existing regulation ippons preserved through encho transition",
			hansokuA:    2,
			ipponsB:     []string{"M"},
			encho:       encho1,
			wantIpponsB: []string{"M", "H"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Snapshot Encho fields before the call to detect field mutation.
			var enchoSnap *state.EnchoMetadata
			if tc.encho != nil {
				snap := *tc.encho
				enchoSnap = &snap
			}
			r := &state.MatchResult{
				HansokuA: tc.hansokuA,
				HansokuB: tc.hansokuB,
				IpponsA:  tc.ipponsA,
				IpponsB:  tc.ipponsB,
				Encho:    tc.encho,
			}
			applyHansokuIppons(r)
			assert.Equal(t, tc.wantIpponsA, r.IpponsA)
			assert.Equal(t, tc.wantIpponsB, r.IpponsB)
			assert.Equal(t, tc.hansokuA, r.HansokuA)
			assert.Equal(t, tc.hansokuB, r.HansokuB)
			// Pointer identity: applyHansokuIppons must not replace the Encho pointer.
			require.True(t, tc.encho == r.Encho, "Encho pointer identity must be preserved")
			// Field immutability: applyHansokuIppons must not mutate EnchoMetadata fields.
			if enchoSnap != nil {
				assert.Equal(t, *enchoSnap, *r.Encho, "Encho fields must not be mutated")
			}
		})
	}

	// Verify the actual regulation→encho boundary: same MatchResult receives a
	// 2nd hansoku after Encho is set; the 1st hansoku from regulation is retained.
	t.Run("regulation→encho transition: same struct retains hansoku count", func(t *testing.T) {
		r := &state.MatchResult{HansokuA: 1, Encho: nil}
		applyHansokuIppons(r)
		require.Nil(t, r.IpponsB) // 1 hansoku in regulation, no ippon yet

		r.HansokuA = 2
		r.Encho = encho1
		enchoSnap := *encho1
		applyHansokuIppons(r)
		assert.Equal(t, []string{"H"}, r.IpponsB) // cumulative 2nd hansoku fires in encho
		require.True(t, r.Encho == encho1, "Encho pointer identity must be preserved")
		assert.Equal(t, enchoSnap, *r.Encho, "Encho fields must not be mutated")
	})

	t.Run("team match sub-results carry hansoku through encho", func(t *testing.T) {
		enchoSnap := *encho1
		r := &state.MatchResult{
			Encho: encho1,
			SubResults: []state.SubMatchResult{
				{HansokuA: 2, IpponsB: []string{"M"}},
				{HansokuB: 2, IpponsA: []string{"K"}},
			},
		}
		applyHansokuIppons(r)
		assert.Equal(t, []string{"M", "H"}, r.SubResults[0].IpponsB)
		assert.Equal(t, []string{"K", "H"}, r.SubResults[1].IpponsA)
		require.True(t, r.Encho == encho1, "Encho pointer identity must be preserved")
		assert.Equal(t, enchoSnap, *r.Encho, "Encho fields must not be mutated")
	})
}

// TestDeriveDaihyosenWinner covers the helper that auto-fills result.Winner
// from a completed daihyosen sub-result (Position=-1) when the operator has
// not explicitly set the bracket match winner.
func TestDeriveDaihyosenWinner(t *testing.T) {
	t.Run("winner already set, no change", func(t *testing.T) {
		r := &state.MatchResult{
			SideA: "TeamA", SideB: "TeamB", Winner: "TeamA",
			SubResults: []state.SubMatchResult{
				{Position: -1, SideA: "PlayerA1", SideB: "PlayerB1", Winner: "PlayerA1"},
			},
		}
		deriveDaihyosenWinner(r)
		assert.Equal(t, "TeamA", r.Winner)
	})

	t.Run("sub-result winner is representative player name (SideA side)", func(t *testing.T) {
		r := &state.MatchResult{
			SideA: "TeamA", SideB: "TeamB",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "P-A1", SideB: "P-B1", Winner: "P-A1"},  // regular bout
				{Position: -1, SideA: "P-A2", SideB: "P-B2", Winner: "P-A2"}, // daihyosen
			},
		}
		deriveDaihyosenWinner(r)
		assert.Equal(t, "TeamA", r.Winner)
	})

	t.Run("sub-result winner is representative player name (SideB side)", func(t *testing.T) {
		r := &state.MatchResult{
			SideA: "TeamA", SideB: "TeamB",
			SubResults: []state.SubMatchResult{
				{Position: -1, SideA: "P-A1", SideB: "P-B1", Winner: "P-B1"},
			},
		}
		deriveDaihyosenWinner(r)
		assert.Equal(t, "TeamB", r.Winner)
	})

	t.Run("sub-result winner is team name directly", func(t *testing.T) {
		r := &state.MatchResult{
			SideA: "TeamA", SideB: "TeamB",
			SubResults: []state.SubMatchResult{
				{Position: -1, SideA: "P-A1", SideB: "P-B1", Winner: "TeamB"},
			},
		}
		deriveDaihyosenWinner(r)
		assert.Equal(t, "TeamB", r.Winner)
	})

	t.Run("daihyosen sub-result has no winner yet, no change", func(t *testing.T) {
		r := &state.MatchResult{
			SideA: "TeamA", SideB: "TeamB",
			SubResults: []state.SubMatchResult{
				{Position: -1, SideA: "P-A1", SideB: "P-B1", Winner: ""},
			},
		}
		deriveDaihyosenWinner(r)
		assert.Equal(t, "", r.Winner)
	})

	t.Run("no daihyosen sub-result, no change", func(t *testing.T) {
		r := &state.MatchResult{
			SideA: "TeamA", SideB: "TeamB",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "P-A1", SideB: "P-B1", Winner: "P-A1"},
			},
		}
		deriveDaihyosenWinner(r)
		assert.Equal(t, "", r.Winner)
	})

	t.Run("nil result, no panic", func(t *testing.T) {
		deriveDaihyosenWinner(nil)
	})
}

// TestRecordBracketMatchResult_DaihyosenWinnerDerived verifies the end-to-end
// path: when a bracket team match is scored with a daihyosen sub-result but
// no explicit Winner, the engine derives the bracket match winner and
// propagates it to the next round.
func TestRecordBracketMatchResult_DaihyosenWinnerDerived(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "dh-bracket-winner"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "DH Bracket", Format: state.CompFormatPlayoffs,
		Status: state.CompStatusPlayoffs, TeamSize: 3,
	}))

	// Two-round bracket: r0m0 feeds winner into r1m0.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "r0m0", SideA: "TeamA", SideB: "TeamB", Status: state.MatchStatusScheduled},
			},
			{
				{ID: "r1m0", SideA: "", SideB: "TeamC", Status: state.MatchStatusScheduled},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	// Score r0m0 with a daihyosen sub-result, Winner field intentionally blank.
	result := &state.MatchResult{
		ID:     "r0m0",
		SideA:  "TeamA",
		SideB:  "TeamB",
		Status: state.MatchStatusCompleted,
		// No top-level Winner, engine must derive it from the daihyosen sub-result.
		SubResults: []state.SubMatchResult{
			{Position: -1, SideA: "PlayerA", SideB: "PlayerB", Winner: "PlayerB",
				Decision: "daihyosen"},
		},
	}

	_, err := eng.RecordMatchResultWithIneligibility(compID, "r0m0", result)
	require.NoError(t, err)

	saved, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "TeamB", saved.Rounds[0][0].Winner, "r0m0 winner must be TeamB")
	assert.Equal(t, "TeamB", saved.Rounds[1][0].SideA, "TeamB must propagate to next round")
}

// TestPreviewBracket_RejectsAllMutations verifies that all bracket mutation
// paths (scoring, override, court/time reassignment) return an error when
// bracket.Preview is true (mp-9dz). The UI disables scoring for preview
// brackets, but server-side enforcement prevents direct API calls or stale
// cached clients from persisting bogus winners/scores into bracket.json.
// TestUnresolvedKnockoutMatch_ScoringGated verifies the per-match playability
// gate that replaced the old bracket-wide Preview gate: a knockout match whose
// sides are still pool-origin placeholders cannot be SCORED or have its winner
// OVERRIDDEN, but CAN still be rescheduled (court/time) so operators can
// pre-arrange the draw before the feeder pools finish.
func TestUnresolvedKnockoutMatch_ScoringGated(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-unresolved-ko-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "unresolved-ko"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Unresolved KO"}))
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))

	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Pool A-1st", SideB: "Pool B-2nd", Status: state.MatchStatusScheduled},
		}},
	}))

	result := &state.MatchResult{Winner: "Pool A-1st", Status: state.MatchStatusCompleted}

	t.Run("RecordMatchResult rejected (not ready)", func(t *testing.T) {
		err := eng.RecordMatchResult(compID, "m-r1-0", result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not ready to score")
	})

	t.Run("RecordMatchResultWithIneligibility rejected (not ready)", func(t *testing.T) {
		_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", result)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not ready to score")
	})

	t.Run("OverrideBracketWinner rejected (not ready)", func(t *testing.T) {
		err := eng.OverrideBracketWinner(compID, "m-r1-0", "Pool A-1st", 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not ready to override")
	})

	// Scheduling a not-yet-resolved match IS allowed, operators may pre-assign
	// courts/times before the feeder pools finish.
	t.Run("UpdateMatchCourt allowed on unresolved match", func(t *testing.T) {
		require.NoError(t, eng.UpdateMatchCourt(compID, "m-r1-0", "B"))
	})
	t.Run("UpdateMatchTime allowed on unresolved match", func(t *testing.T) {
		require.NoError(t, eng.UpdateMatchTime(compID, "m-r1-0", "10:00"))
	})

	// The match must NOT have been scored, but the scheduling edits must have
	// landed.
	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusScheduled, loaded.Rounds[0][0].Status, "match must not have been scored")
	assert.Empty(t, loaded.Rounds[0][0].Winner, "winner must not have been set")
	assert.Equal(t, "B", loaded.Rounds[0][0].Court, "court reschedule must have landed")
	assert.Equal(t, "10:00", loaded.Rounds[0][0].ScheduledAt, "time reschedule must have landed")
}

// TestBackfillMatchIdentity directly exercises backfillMatchIdentity, in
// particular the same-name-head-to-head scoreline-inference branch that the
// admin score editor relies on (it picks a winner by NAME, sending no
// WinnerSide hint). The two pre-existing PreservesSideIDs tests both set
// WinnerSide:"A", so this branch, and the equal-count "leave WinnerID empty"
// tie, were previously uncovered (mp-jvzy tri-review finding).
func TestBackfillMatchIdentity(t *testing.T) {
	const (
		idA = "11111111-1111-4111-8111-111111111111"
		idB = "22222222-2222-4222-8222-222222222222"
	)

	tests := []struct {
		name       string
		result     state.MatchResult
		wantWinner string
	}{
		{
			name:       "WinnerSide A hint wins over everything",
			result:     state.MatchResult{Winner: "Tanaka Kenji", WinnerSide: "A"},
			wantWinner: idA,
		},
		{
			name:       "WinnerSide B hint",
			result:     state.MatchResult{Winner: "Tanaka Kenji", WinnerSide: "B"},
			wantWinner: idB,
		},
		{
			name:       "unambiguous name match resolves to SideA",
			result:     state.MatchResult{SideA: "Alice", SideB: "Bob", Winner: "Alice"},
			wantWinner: idA,
		},
		{
			name:       "unambiguous name match resolves to SideB",
			result:     state.MatchResult{SideA: "Alice", SideB: "Bob", Winner: "Bob"},
			wantWinner: idB,
		},
		{
			name:       "same-name head-to-head: SideA has more ippons",
			result:     state.MatchResult{SideA: "Tanaka Kenji", SideB: "Tanaka Kenji", Winner: "Tanaka Kenji", IpponsA: []string{"M", "K"}, IpponsB: []string{"D"}},
			wantWinner: idA,
		},
		{
			name:       "same-name head-to-head: SideB has more ippons (symmetry)",
			result:     state.MatchResult{SideA: "Tanaka Kenji", SideB: "Tanaka Kenji", Winner: "Tanaka Kenji", IpponsA: []string{"D"}, IpponsB: []string{"M", "K"}},
			wantWinner: idB,
		},
		{
			name:       "same-name head-to-head: equal ippons is undecidable, WinnerID empty",
			result:     state.MatchResult{SideA: "Tanaka Kenji", SideB: "Tanaka Kenji", Winner: "Tanaka Kenji", IpponsA: []string{"M"}, IpponsB: []string{"D"}},
			wantWinner: "",
		},
		{
			name:       "scoreline count ignores the • placeholder",
			result:     state.MatchResult{SideA: "Tanaka Kenji", SideB: "Tanaka Kenji", Winner: "Tanaka Kenji", IpponsA: []string{"M", "•"}, IpponsB: []string{"•"}},
			wantWinner: idA,
		},
		{
			name:       "explicit WinnerID preserved (early return, no inference)",
			result:     state.MatchResult{Winner: "Tanaka Kenji", WinnerID: "explicit-id", IpponsA: []string{"M", "K"}},
			wantWinner: "explicit-id",
		},
		{
			name:       "draw (empty Winner) leaves WinnerID empty",
			result:     state.MatchResult{SideA: "Tanaka Kenji", SideB: "Tanaka Kenji", Winner: "", IpponsA: []string{"M"}, IpponsB: []string{"M"}},
			wantWinner: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.result
			// `stored` carries the generation-time ids; `result` (the incoming
			// score) starts without them, mirrors the real write path.
			stored := &state.MatchResult{SideAID: idA, SideBID: idB}
			backfillMatchIdentity(&result, stored)

			assert.Equal(t, tc.wantWinner, result.WinnerID, "WinnerID")
			// Side ids are always backfilled from `stored` (runs before the
			// WinnerID early-return), even in the explicit-WinnerID case.
			assert.Equal(t, idA, result.SideAID, "SideAID backfilled")
			assert.Equal(t, idB, result.SideBID, "SideBID backfilled")
		})
	}
}

// TestBackfillMatchIdentity_RepPlayers pins the daihyosen rep-player preserve-
// on-empty rule (mp-62vr): a score write that omits the rep players must NOT
// wipe a previously-recorded pick, but an explicit value always overrides.
func TestBackfillMatchIdentity_RepPlayers(t *testing.T) {
	t.Run("empty result preserves stored rep players", func(t *testing.T) {
		result := state.MatchResult{} // a re-score that only re-sends the ippons
		stored := &state.MatchResult{RepPlayerA: "Sato Ren", RepPlayerB: "Yamada Taro"}
		backfillMatchIdentity(&result, stored)
		assert.Equal(t, "Sato Ren", result.RepPlayerA, "preserved on empty")
		assert.Equal(t, "Yamada Taro", result.RepPlayerB, "preserved on empty")
	})

	t.Run("explicit rep players override stored", func(t *testing.T) {
		result := state.MatchResult{RepPlayerA: "Ito Kenji", RepPlayerB: "Mori Aki"}
		stored := &state.MatchResult{RepPlayerA: "Sato Ren", RepPlayerB: "Yamada Taro"}
		backfillMatchIdentity(&result, stored)
		assert.Equal(t, "Ito Kenji", result.RepPlayerA, "operator change wins")
		assert.Equal(t, "Mori Aki", result.RepPlayerB, "operator change wins")
	})

	t.Run("one side set, other preserved", func(t *testing.T) {
		result := state.MatchResult{RepPlayerA: "Ito Kenji"} // only Aka changed
		stored := &state.MatchResult{RepPlayerA: "Sato Ren", RepPlayerB: "Yamada Taro"}
		backfillMatchIdentity(&result, stored)
		assert.Equal(t, "Ito Kenji", result.RepPlayerA)
		assert.Equal(t, "Yamada Taro", result.RepPlayerB, "untouched side preserved")
	})
}
