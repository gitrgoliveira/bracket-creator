package engine

// Operator ruling 2026-09-24: "Everything should be able to be fixed, in case
// of a wrong entry." Reopening a kachinuki match clears its decision, so
// reopening one a withdrawal ended removes the withdrawal, and the
// eligibility record follows the ruling exactly as on the score path: the
// team the withdrawal barred is eligible again, in the reopen's own
// transaction, and the restored status is returned for the handler to
// broadcast. These pin that on both reopen doors and both branches, and pin
// that a reopen with no withdrawal restores nothing.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedKachinukiPoolWithdrawal builds a kachinuki pool with Ryu v Tora on
// court A, bout 1 fought, then ends it by `decision` against Ryu (aka)
// through the real decision path.
func seedKachinukiPoolWithdrawal(t *testing.T, compID, decision string) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupKachinukiComp(t, compID, 3)
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Court: "A", Status: state.MatchStatusRunning, SubResults: []state.SubMatchResult{wrBout1("M")},
	}}))
	_, _, err := eng.RecordDecision(compID, "Pool A-0", decision, "aka", "knee", nil, false)
	require.NoError(t, err)
	require.False(t, wrEligible(t, store, compID, wrTeamAID), "precondition: the withdrawal barred Ryu")
	return eng, store
}

func TestReopenKachinuki_WithdrawalRemovedRestoresEligibility(t *testing.T) {
	for _, decision := range []string{"kiken-voluntary", "kiken-injury", "fusenpai"} {
		t.Run(decision, func(t *testing.T) {
			const compID = "kr-pool"
			eng, store := seedKachinukiPoolWithdrawal(t, compID, decision)

			status, err := eng.ReopenMatch(compID, "Pool A-0", "Wrong entry: nobody withdrew")
			require.NoError(t, err)
			require.NotNil(t, status, "the restore is returned so the handler can broadcast it")
			assert.Equal(t, wrTeamAID, status.PlayerID)
			assert.True(t, status.Eligible)
			assert.True(t, wrEligible(t, store, compID, wrTeamAID), "a removed withdrawal bars nobody")

			m := wrPoolMatch(t, store, compID)
			assert.Equal(t, state.MatchStatusRunning, m.Status)
			assert.Equal(t, "", m.Decision)
			require.Len(t, m.SubResults, 1, "the reopen keeps the bout log")
		})
	}
}

// The requeue door runs the same shared body, so it restores too.
func TestRequeueAndReopenKachinuki_WithdrawalRemovedRestoresEligibility(t *testing.T) {
	const compID = "kr-requeue"
	eng, store := seedKachinukiPoolWithdrawal(t, compID, "kiken-voluntary")
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	ms = append(ms, state.MatchResult{
		ID: "Pool A-1", SideA: wrTeamB, SideAID: wrTeamBID, SideB: wrTeamC, SideBID: wrTeamCID,
		Court: "A", Status: state.MatchStatusRunning,
	})
	require.NoError(t, store.SavePoolMatches(compID, ms))

	status, err := eng.RequeueBlockerAndReopen(compID, "Pool A-0", compID, "Pool A-1", "")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, wrTeamAID, status.PlayerID)
	assert.True(t, wrEligible(t, store, compID, wrTeamAID))
}

// The bracket branch.
func TestReopenKachinuki_BracketWithdrawalRemovedRestoresEligibility(t *testing.T) {
	const compID = "kr-ko"
	eng, store, _ := setupKachinukiComp(t, compID, 3)
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusRunning,
	}}}}))
	_, _, err := eng.RecordDecision(compID, "m-r1-0", "fusenpai", "shiro", "", nil, false)
	require.NoError(t, err)
	require.False(t, wrEligible(t, store, compID, wrTeamBID), "precondition: the no-show barred Tora")

	status, err := eng.ReopenMatch(compID, "m-r1-0", "")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, wrTeamBID, status.PlayerID)
	assert.True(t, wrEligible(t, store, compID, wrTeamBID))
}

// A reopen of a match no withdrawal ended removes none, so it restores
// nothing and returns no status to broadcast. An unrelated ineligibility
// recorded by ANOTHER match stays exactly as it was.
func TestReopenKachinuki_NoWithdrawalRestoresNothing(t *testing.T) {
	const compID = "kr-none"
	eng, store, _ := setupKachinukiComp(t, compID, 3)
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusCompleted, Winner: wrTeamA, WinnerID: wrTeamAID,
			Decision:   "kachinuki-exhaustion",
			SubResults: []state.SubMatchResult{wrBout1("M")},
		},
		{
			ID: "Pool A-1", SideA: wrTeamC, SideAID: wrTeamCID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusRunning,
		},
	}))
	_, _, err := eng.RecordDecision(compID, "Pool A-1", "kiken-voluntary", "aka", "", nil, false)
	require.NoError(t, err)
	require.False(t, wrEligible(t, store, compID, wrTeamCID))

	status, err := eng.ReopenMatch(compID, "Pool A-0", "")
	require.NoError(t, err)
	assert.Nil(t, status, "nothing was withdrawn, so nothing is restored")
	assert.False(t, wrEligible(t, store, compID, wrTeamCID), "another match's withdrawal is untouched")
}
