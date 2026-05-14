package engine

import (
	"os"
	"sync"
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
		ID: compID, Name: "Auto", Format: state.CompFormatPools, Status: state.CompStatusPools,
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
			ID: koID, Name: "KO", Format: state.CompFormatPlayoffs, Status: state.CompStatusPlayoffs,
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

	t.Run("transitions when there are zero pool matches", func(t *testing.T) {
		// e.g. a single-participant pools comp where no match was generated.
		// Without this branch the competition would be stuck in `pools` forever.
		emptyID := "auto-complete-empty"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: emptyID, Name: "Empty", Format: state.CompFormatPools, Status: state.CompStatusPools,
		}))
		require.NoError(t, store.SavePoolMatches(emptyID, []state.MatchResult{}))
		done, err := eng.MaybeAutoCompletePools(emptyID)
		require.NoError(t, err)
		assert.True(t, done)
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

// TestRecordMatchResult_ConcurrentScoresNotLost pins the TOCTOU fix for
// the live-scoring path. Pre-atomic-primitive, withPoolMatch did
// LoadPoolMatches → mutate target match → SavePoolMatches sequentially
// with no lock held between Load and Save. Two operators scoring
// DIFFERENT matches on DIFFERENT courts could each load the full pool-
// matches slice into a separate copy, mutate their target, and save —
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
			assert.NoError(t, err, "iter %d: Pool-1 score should succeed", i)
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
			assert.NoError(t, err, "iter %d: Pool-2 score should succeed", i)
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
		require.Len(t, stored, 2, "iter %d: both pool matches must still exist", i)

		var p1, p2 *state.MatchResult
		for idx := range stored {
			switch stored[idx].ID {
			case "Pool-1":
				p1 = &stored[idx]
			case "Pool-2":
				p2 = &stored[idx]
			}
		}
		require.NotNil(t, p1, "iter %d: Pool-1 must exist on disk", i)
		require.NotNil(t, p2, "iter %d: Pool-2 must exist on disk", i)
		assert.Equal(t, "Alice", p1.Winner, "iter %d: Pool-1 winner must be Alice (Operator A's score)", i)
		assert.Equal(t, state.MatchStatusCompleted, p1.Status, "iter %d: Pool-1 must be completed", i)
		assert.Equal(t, "Dave", p2.Winner, "iter %d: Pool-2 winner must be Dave (Operator B's score)", i)
		assert.Equal(t, state.MatchStatusCompleted, p2.Status, "iter %d: Pool-2 must be completed", i)

		os.RemoveAll(dir)
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
			assert.NoError(t, err, "iter %d: QF1 score should succeed", i)
		}()
		go func() {
			defer wg.Done()
			res := &state.MatchResult{
				Winner:  "Dave",
				IpponsB: []string{"K"},
				Status:  state.MatchStatusCompleted,
			}
			err := eng.RecordMatchResult(compID, "QF2", res)
			assert.NoError(t, err, "iter %d: QF2 score should succeed", i)
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
		require.NotNil(t, qf1, "iter %d: QF1 must exist", i)
		require.NotNil(t, qf2, "iter %d: QF2 must exist", i)
		assert.Equal(t, "Alice", qf1.Winner, "iter %d: QF1 winner must be Alice", i)
		assert.Equal(t, state.MatchStatusCompleted, qf1.Status, "iter %d: QF1 must be completed", i)
		assert.Equal(t, "Dave", qf2.Winner, "iter %d: QF2 winner must be Dave", i)
		assert.Equal(t, state.MatchStatusCompleted, qf2.Status, "iter %d: QF2 must be completed", i)

		os.RemoveAll(dir)
	}
}

// TestMaybeAutoCompletePools_ConcurrentInvalidateNotLost pins the
// TOCTOU fix in engine.MaybeAutoCompletePools. Pre-atomic-primitive,
// the LoadCompetition + status check + SaveCompetitionChanged
// sequence had a window where a concurrent admin invalidate (POST
// /invalidate) could land between the read and the write — admin's
// "invalid" status would then be silently overwritten back to
// "complete" by the auto-complete save.
//
// The fix wraps the status read + status set + save in
// state.Store.UpdateCompetitionChanged. The transform re-checks
// `current.Status == Pools` UNDER the lock; if the admin's
// invalidate already moved Status to Invalid, the auto-complete
// transform sees the new value and returns (nil, nil) — no save.
func TestMaybeAutoCompletePools_ConcurrentInvalidateNotLost(t *testing.T) {
	const iterations = 20

	for i := range iterations {
		dir, err := os.MkdirTemp("", "engine-autocomplete-race-*")
		require.NoError(t, err)

		store, err := state.NewStore(dir)
		require.NoError(t, err)
		eng := New(store)

		compID := "auto-vs-invalidate"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "Auto vs Invalidate",
			Format: state.CompFormatPools, Status: state.CompStatusPools,
		}))
		// All matches already completed — auto-complete is eligible.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-1", Status: state.MatchStatusCompleted, Winner: "Alice", SideA: "Alice", SideB: "Bob"},
		}))

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			// Auto-complete tries to set Status=Complete.
			_, _ = eng.MaybeAutoCompletePools(compID)
		}()
		go func() {
			defer wg.Done()
			// Admin invalidate: simulate via direct UpdateCompetitionChanged
			// (mirrors what the POST /invalidate handler does).
			_, _ = store.UpdateCompetitionChanged(compID, func(current *state.Competition) (*state.Competition, error) {
				if current == nil || (current.Status != state.CompStatusPools && current.Status != state.CompStatusPlayoffs) {
					return nil, nil
				}
				current.Status = state.CompStatusInvalid
				return current, nil
			})
		}()
		wg.Wait()

		stored, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		require.NotNil(t, stored)
		// Final status is whichever transition won. Crucially it
		// must NOT be Pools (the original) — that would mean both
		// writes either failed silently or one was silently lost.
		assert.Contains(t, []state.CompetitionStatus{state.CompStatusInvalid, state.CompStatusComplete}, stored.Status,
			"iter %d: Status must be Invalid or Complete (got %q — race lost a write)",
			i, stored.Status)

		os.RemoveAll(dir)
	}
}
