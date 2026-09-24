package engine

// Operator ruling 2026-09-24: "Everything should be able to be fixed, in case
// of a wrong entry." When a write REPLACES a recorded withdrawal with a result
// that is not one, the eligibility record follows the ruling: the competitor
// the withdrawal barred is eligible again, whichever kind of withdrawal it
// was, and the restored status is returned so the handler can broadcast it.
// A score sheet's correction never replaces one (KeepsWithdrawalRuling keeps
// it for "" and "hikiwake"; see withdrawal_ruling_kept_test.go), and the
// operator's way to remove one recorded by mistake is a reopen
// (reopen_withdrawal_test.go). What replaces it here is a decision: POST
// /decision's fusensho or daihyosen, or any decision outside that allowlist.
// These pin the rule (RecordMatchResultWithIneligibilityTx,
// restoreIfWithdrawalRemoved, restoreEligibilityRecordedByMatch) on both
// doors.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func wrEligible(t *testing.T, store *state.Store, compID, playerID string) bool {
	t.Helper()
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	st, ok := statuses[playerID]
	return !ok || st.Eligible
}

// An individual editor's fought rescore over a withdrawal (no bout rows)
// replaces the ruling and restores the competitor it barred.
// F1: POST /decision with fusensho or daihyosen on a match a team withdrawal
// decided. preserveLoserScore carries the fought bout rows onto the decision
// write, so a bout-row test kept the kiken while recordDecisionTx's default
// arm still restored the loser: a stored kiken with an eligible loser. The
// allowlist makes such a decision REPLACE the withdrawal, and the restore then
// describes what is stored.
func TestWithdrawalRemoved_DecisionReplacingATeamWithdrawalRestores(t *testing.T) {
	for _, decision := range []string{"fusensho", "daihyosen"} {
		t.Run(decision, func(t *testing.T) {
			eng, store, compID, _ := seedPoolWithdrawal(t, "kiken-voluntary")
			require.False(t, wrEligible(t, store, compID, wrTeamAID), "precondition: the kiken barred Ryu")
			require.NotEmpty(t, wrPoolMatch(t, store, compID).SubResults, "precondition: the kiken kept the fought bout")

			_, status, err := eng.RecordDecision(compID, "Pool A-0", decision, "aka", "", nil, false)
			require.NoError(t, err)

			m := wrPoolMatch(t, store, compID)
			assert.Equal(t, decision, m.Decision, "the decision replaces the withdrawal")
			require.NotNil(t, status, "the restore is returned so the handler can broadcast it")
			assert.Equal(t, wrTeamAID, status.PlayerID)
			assert.True(t, status.Eligible)
			assert.True(t, wrEligible(t, store, compID, wrTeamAID), "a replaced withdrawal bars nobody")
			require.Len(t, m.SubResults, 1, "the bout fought before it survives (preserveLoserScore)")
		})
	}
}

// The same rule on the bracket branch, through the Tx door.
// The same rule on the bracket branch, through the Tx door, for a decision a
// score sheet cannot send ("fought", outside KeepsWithdrawalRuling's allowlist).
func TestWithdrawalRemoved_BracketTxDoorRestores(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "wr-ko-ind"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "wr", Kind: "individual", Status: state.CompStatusKnockout,
	}))
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusRunning,
	}}}}))
	_, _, err := eng.RecordDecision(compID, "m-r1-0", "fusenpai", "shiro", "", nil, false)
	require.NoError(t, err)
	require.False(t, wrEligible(t, store, compID, wrTeamBID))

	var status any
	err = inTx(t, store, compID, func(tx state.StoreTx) error {
		st, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", &state.MatchResult{
			ID: "m-r1-0", SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamA, Decision: "fought",
			IpponsA: []string{"M"}, IpponsB: []string{},
			Status: state.MatchStatusCompleted, CorrectionReason: "Wrong entry: it was fought",
		})
		if st != nil {
			status = st.PlayerID
		}
		return e
	})
	require.NoError(t, err)
	assert.Equal(t, wrTeamBID, status)
	assert.True(t, wrEligible(t, store, compID, wrTeamBID))
}

// wrFoughtBouts is a complete 3-bout sheet Tora wins 2-1.
func wrFoughtBouts() []state.SubMatchResult {
	return []state.SubMatchResult{
		{Position: 1, SideA: "r1", SideB: "t1", Winner: "r1", IpponsA: []string{"K"}},
		{Position: 2, SideA: "r2", SideB: "t2", Winner: "t2", IpponsB: []string{"M"}},
		{Position: 3, SideA: "r3", SideB: "t3", Winner: "t3", IpponsB: []string{"D"}},
	}
}

// A write that does not land restores nothing: a superseded write that would
// replace the withdrawal leaves it and the competitor's status as recorded.
func TestWithdrawalRemoved_SupersededWriteRestoresNothing(t *testing.T) {
	eng, store, compID, dir := seedPoolWithdrawal(t, "kiken-voluntary")
	// Re-record the same withdrawal with a stamp, so there is a time to lose to.
	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "aka", "knee", nil, false, 1_900_000_000_000)
	require.NoError(t, err)
	before := string(readStatusFile(t, dir, compID))
	stored := wrPoolMatch(t, store, compID)
	require.NotZero(t, stored.ModifiedAt)

	_, err = eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", &state.MatchResult{
		ID: "Pool A-0", SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamB, Decision: "fought",
		Status: state.MatchStatusCompleted, ModifiedAt: stored.ModifiedAt - 1000,
		SubResults: wrFoughtBouts(),
	})
	require.ErrorIs(t, err, ErrMatchSuperseded)
	assert.Equal(t, before, string(readStatusFile(t, dir, compID)))
	assert.Equal(t, "kiken-voluntary", wrPoolMatch(t, store, compID).Decision)
}

// RecordMatchResult (writeMatchResult) is the other forward door with an
// eligibility side effect, and follows the same rule.
func TestWithdrawalRemoved_RecordMatchResultDoorRestores(t *testing.T) {
	eng, store, compID, _ := seedPoolWithdrawal(t, "kiken-voluntary")
	require.False(t, wrEligible(t, store, compID, wrTeamAID))

	require.NoError(t, eng.RecordMatchResult(compID, "Pool A-0", &state.MatchResult{
		ID: "Pool A-0", SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamB, Decision: "fought",
		Status: state.MatchStatusCompleted, SubResults: wrFoughtBouts(),
	}))
	assert.True(t, wrEligible(t, store, compID, wrTeamAID))
	assert.Equal(t, "fought", wrPoolMatch(t, store, compID).Decision)
}
