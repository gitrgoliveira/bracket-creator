package engine

// bc-mrgc: code review finding 6 on PR #453. The kachinuki advance,
// override-winner and requeue doors used to persist their match write
// through e.store directly and append their match-history entry
// afterwards, outside any transaction: a process dying between the two
// write (or the history append itself failing) left a match write on disk
// with no history line explaining it. Each door's write and its history
// entry now land inside ONE e.store.WithTransaction, exactly like the
// already-converted reopen doors (recordDirectHistory(tx, ...)).
//
// These tests pin that the history entry is only ever recorded alongside a
// write that actually landed: a transaction that COMMITS records BOTH, and
// one that ABORTS (the kachinuki advance's stale re-check failing) records
// NEITHER the write nor the history line.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func TestOverrideBracketWinner_RecordsHistoryAtomicallyWithTheWrite(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-history-atomic"

	createTestCompetition(t, store, compID, "knockout", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	matchID := bracket.Rounds[0][0].ID // Alice vs Bob

	applied, err := eng.OverrideBracketWinner(compID, matchID, "Bob", 0)
	require.NoError(t, err)
	assert.True(t, applied)

	history, err := store.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	require.Len(t, history, 1, "the write and its history entry land together")
	assert.Equal(t, doorOverride, history[0].Door)
	assert.Equal(t, state.HistoryOutcomeApplied, history[0].Outcomes[state.GroupResult])

	reloaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", reloaded.Rounds[0][0].Winner, "the write landed alongside its history entry")
}

func TestRevertMatchToQueue_RecordsHistoryAtomicallyWithTheWrite(t *testing.T) {
	t.Run("pool", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "requeue-history-atomic-pool"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Requeue"}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning,
		}}))

		require.NoError(t, eng.RevertMatchToQueue(compID, "P1-0"))

		history, err := store.LoadMatchHistory(compID, "P1-0")
		require.NoError(t, err)
		require.Len(t, history, 1, "the write and its history entry land together")
		assert.Equal(t, doorRequeue, history[0].Door)
		assert.Equal(t, state.HistoryOutcomeApplied, history[0].Outcomes[state.GroupResult])

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, matches, 1)
		assert.Equal(t, state.MatchStatusScheduled, matches[0].Status, "the write landed alongside its history entry")
		assert.Equal(t, matches[0].GroupStamp(state.GroupResult), history[0].Stamp, "one stamp for both the result group and its history entry")
	})

	t.Run("bracket", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "requeue-history-atomic-bracket"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Requeue"}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
			ID: "B1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning,
		}}}}))

		require.NoError(t, eng.RevertMatchToQueue(compID, "B1"))

		history, err := store.LoadMatchHistory(compID, "B1")
		require.NoError(t, err)
		require.Len(t, history, 1, "the write and its history entry land together")
		assert.Equal(t, doorRequeue, history[0].Door)
		assert.Equal(t, state.HistoryOutcomeApplied, history[0].Outcomes[state.GroupResult])

		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, state.MatchStatusScheduled, b.Rounds[0][0].Status, "the write landed alongside its history entry")
		// requeueBracketMatch used to stamp the match with its OWN
		// serverNowMs() call rather than the one RevertMatchToQueue took for
		// the history entry, so the two could differ by up to a
		// millisecond. The caller now passes its stamp in, so both read the
		// same value.
		assert.Equal(t, b.Rounds[0][0].GroupStamp(state.GroupResult), history[0].Stamp, "one stamp for both the result group and its history entry")
	})
}

// A stale kachinuki advance aborts the whole transaction:
// TestLostUpdate_KachinukiAdvanceDoesNotReopenAFinishThatLanded
// (lost_update_window_test.go) already pins that no bout is appended over
// the finish that landed at the read seam. This pins the OTHER half of the
// same transaction: no history entry is recorded for the aborted attempt
// either, since nothing explains a write that never happened.
func TestAdvanceKachinukiOnce_StaleAbortRecordsNoHistoryEntry(t *testing.T) {
	finish := func(m *state.MatchResult) {
		m.Status = state.MatchStatusCompleted
		m.Winner = "RedTeam"
		m.Decision = string(domain.DecisionKachinukiExhaustion)
		m.ModifiedAt = time.Now().UnixMilli()
	}
	eng, store, _ := setupTestEngine(t)
	compID := "advance-history-stale"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "B-Senpo", Decision: "fought"},
			{Position: 2, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "A-Jiho", Decision: "fought"},
		},
	}}))

	wait := landWriteAtTheSeam(t, eng, compID, func() error {
		_, err := store.UpdatePoolMatchByID(compID, "P1-0", func(m *state.MatchResult) error { finish(m); return nil })
		return err
	})
	advanced, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	wait()
	assert.False(t, advanced, "the advance stands down")

	history, err := store.LoadMatchHistory(compID, "P1-0")
	require.NoError(t, err)
	assert.Empty(t, history, "the aborted attempt's write never landed, so neither does its history entry")
}

// The successful counterpart: when the advance's re-check holds, the
// appended bout and its history entry both land, in the one transaction.
func TestAdvanceKachinukiOnce_SuccessRecordsHistoryEntry(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-history-success"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam",
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "B-Senpo", Decision: "fought"},
			{Position: 2, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "A-Jiho", Decision: "fought"},
		},
	}}))

	advanced, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	require.True(t, advanced, "the next bout is appended")

	history, err := store.LoadMatchHistory(compID, "P1-0")
	require.NoError(t, err)
	require.Len(t, history, 1, "the appended bout and its history entry land together")
	assert.Equal(t, doorKachinukiAdvance, history[0].Door)

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches[0].SubResults, 3, "the write landed alongside its history entry")
}

// A forced OverrideBracketWinner that reopens a downstream match used to call
// restoreForceReopened AFTER its own transaction returned, with the bare
// store rather than the transaction handle the bracket write just used. That
// left a window, between the override's commit and this later call, where a
// process dying left the downstream match reopened with no history entry
// explaining it, and the competitor it un-bars still eligible-false. It now
// runs with the same tx handle inside the override's own transaction, so both
// halves of the downstream reopen -- the eligibility restore and the
// "downstream-reopen" history entry -- commit or fail together with the
// override's own write and its own history entry. The setup is the "forced
// winner override" case of TestForceReopenedDownstreamWithdrawalRestoresEligibility
// (reopen_withdrawal_test.go): an individual knockout final Kuma withdrew
// from (kiken-voluntary), reopened by overriding round 1's winner.
func TestOverrideBracketWinner_ForcedReopenRecordsDownstreamHistoryAtomically(t *testing.T) {
	fought := func(eng *Engine, compID string) {
		_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", &state.MatchResult{
			ID: "m-r1-0", SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamB,
			IpponsB: []string{"M", "K"}, Status: state.MatchStatusCompleted,
		})
		require.NoError(t, err)
	}
	eng, store, compID := seedKnockoutFinalWithdrawal(t, fought)

	// The seed's own kiken-voluntary decision on m-r2-0 already left one
	// "decision" history entry there before the override runs.
	preHistory, err := store.LoadMatchHistory(compID, "m-r2-0")
	require.NoError(t, err)
	require.Len(t, preHistory, 1, "precondition: the seeded withdrawal recorded its own entry")

	var reopened []ReopenedMatch
	applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", wrTeamA, 0, ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	require.True(t, applied)
	require.Len(t, reopened, 1)
	assert.Equal(t, "m-r2-0", reopened[0].ID)

	// The eligibility half of restoreForceReopened landed.
	assert.True(t, wrEligible(t, store, compID, wrTeamCID), "the final's withdrawal is gone, so Kuma can compete again")

	// The history half of restoreForceReopened landed too: the downstream
	// match gets its own "downstream-reopen" entry APPENDED after the
	// seed's decision entry, alongside the override's own entry for the
	// match the operator actually acted on.
	downstreamHistory, err := store.LoadMatchHistory(compID, "m-r2-0")
	require.NoError(t, err)
	require.Len(t, downstreamHistory, 2, "the downstream reopen's own history entry lands with the override")
	assert.Equal(t, doorDownstreamReopen, downstreamHistory[1].Door)

	overrideHistory, err := store.LoadMatchHistory(compID, "m-r1-0")
	require.NoError(t, err)
	require.Len(t, overrideHistory, 2, "the fought-round setup left its own entry before the override's")
	assert.Equal(t, doorOverride, overrideHistory[1].Door)
}
