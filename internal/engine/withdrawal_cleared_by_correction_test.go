package engine

// Operator ruling 2026-10-03: an operator can fix any mistake, but the fix
// leaves the match resolved. A withdrawal recorded by mistake on a match that
// was fought to a result is therefore removed by ONE completed correction
// carrying the real result and ClearsWithdrawal (the score handler's
// `clearWithdrawal`): the match stays finished, never takes the court, and
// the competitor the withdrawal barred is eligible again. Without the flag the
// same correction keeps the ruling (withdrawal_ruling_kept_test.go), which is
// what makes each "without" subtest below the control for its "with".

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clearsSubtest names a subtest by whether its write carries the flag; the
// "without" run is the control for the "with" one.
func clearsSubtest(clears bool) string {
	if clears {
		return "with clearWithdrawal"
	}
	return "without"
}

func TestWithdrawalClearedByCorrection_IndividualWin(t *testing.T) {
	for _, clears := range []bool{true, false} {
		t.Run(clearsSubtest(clears), func(t *testing.T) {
			eng, store, compID := seedIndividualWithdrawal(t, "kiken-voluntary", nil)
			_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", &state.MatchResult{
				ID: "Pool A-0", SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamA, WinnerID: wrTeamAID,
				IpponsA: []string{"M", "K"}, IpponsB: []string{"K"},
				Status: state.MatchStatusCompleted, ClearsWithdrawal: clears,
			})
			require.NoError(t, err)

			m := wrPoolMatch(t, store, compID)
			assert.Equal(t, state.MatchStatusCompleted, m.Status, "the match stays finished either way")
			if !clears {
				assert.Equal(t, "kiken-voluntary", m.Decision, "a correction without the flag keeps the ruling")
				assert.Equal(t, wrTeamB, m.Winner)
				assert.False(t, wrEligible(t, store, compID, wrTeamAID))
				return
			}
			assert.Equal(t, "", m.Decision)
			assert.Equal(t, wrTeamA, m.Winner, "the real result stands")
			assert.Equal(t, []string{"M", "K"}, m.IpponsA)
			assert.Equal(t, []string{"K"}, m.IpponsB, "no default-win circles are left behind")
			assert.True(t, wrEligible(t, store, compID, wrTeamAID), "Ryu never withdrew")
		})
	}
}

// A real draw replaces the ruling too. "hikiwake" alone is a score sheet's
// draw toggle and keeps the withdrawal, so this is the case the flag exists
// for rather than a decision value.
func TestWithdrawalClearedByCorrection_Draw(t *testing.T) {
	for _, clears := range []bool{true, false} {
		t.Run(clearsSubtest(clears), func(t *testing.T) {
			eng, store, compID := seedIndividualWithdrawal(t, "kiken-injury", nil)
			_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", &state.MatchResult{
				ID: "Pool A-0", SideA: wrTeamA, SideB: wrTeamB, Decision: "hikiwake",
				IpponsA: []string{"M"}, IpponsB: []string{"K"},
				Status: state.MatchStatusCompleted, ClearsWithdrawal: clears,
			})
			require.NoError(t, err)

			m := wrPoolMatch(t, store, compID)
			if !clears {
				assert.Equal(t, "kiken-injury", m.Decision)
				assert.False(t, wrEligible(t, store, compID, wrTeamAID))
				return
			}
			assert.Equal(t, "hikiwake", m.Decision)
			assert.Empty(t, m.Winner)
			assert.True(t, wrEligible(t, store, compID, wrTeamAID))
		})
	}
}

// A team match: the bouts sent are the record. The withdrawal's default-win
// padding is not added, because nothing about the match is a default win any
// more (the handler's team finish gate asks for a result on every bout first).
func TestWithdrawalClearedByCorrection_TeamMatchIsNotPadded(t *testing.T) {
	eng, store, compID, _ := seedPoolWithdrawal(t, "kiken-voluntary")
	require.Len(t, wrPoolMatch(t, store, compID).SubResults, 3, "precondition: the kiken padded bouts 2 and 3")

	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", &state.MatchResult{
		ID: "Pool A-0", SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamB, WinnerID: wrTeamBID,
		IpponsA: []string{}, IpponsB: []string{},
		Status: state.MatchStatusCompleted, ClearsWithdrawal: true,
		SubResults: wrFoughtBouts(),
	})
	require.NoError(t, err)

	m := wrPoolMatch(t, store, compID)
	assert.Equal(t, "", m.Decision)
	assert.Equal(t, wrTeamB, m.Winner)
	assert.Equal(t, wrFoughtBouts(), m.SubResults, "the bouts fought, nothing credited")
	assert.True(t, wrEligible(t, store, compID, wrTeamAID))
}

// A knockout match: the real winner goes on to the next match, through the
// same downstream checks as any knockout correction. Here the final was
// already fought with the competitor the withdrawal sent through, so the fix
// is confirmed (force) and the final is reopened for the real winner.
func TestWithdrawalClearedByCorrection_KnockoutSeatsTheRealWinner(t *testing.T) {
	eng, store, compID, _, matchID := seedBracketWithdrawal(t, false)
	fix := func() *state.MatchResult {
		return &state.MatchResult{
			ID: matchID, SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamA, WinnerID: wrTeamAID,
			IpponsA: []string{}, IpponsB: []string{},
			Status: state.MatchStatusCompleted, ClearsWithdrawal: true,
			SubResults: []state.SubMatchResult{
				wrBout1("M"),
				{Position: 2, SideA: "r2", SideB: "t2", Winner: "r2", IpponsA: []string{"K"}},
				{Position: 3, SideA: "r3", SideB: "t3", Winner: "t3", IpponsB: []string{"D"}},
			},
		}
	}

	_, err := eng.RecordMatchResultWithIneligibility(compID, matchID, fix())
	require.ErrorIs(t, err, ErrDownstreamKnockoutPlayed, "the final Tora fought is named before anything changes")
	assert.False(t, wrEligible(t, store, compID, wrTeamAID))

	_, err = eng.RecordMatchResultWithIneligibility(compID, matchID, fix(), ForceOptions{Force: true})
	require.NoError(t, err)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	r1 := b.Rounds[0][0]
	assert.Equal(t, state.MatchStatusCompleted, r1.Status, "the fixed match stays finished")
	assert.Equal(t, "", r1.Decision)
	assert.Equal(t, wrTeamA, r1.Winner)
	assert.Equal(t, wrTeamA, b.Rounds[1][0].SideA, "Ryu goes on to the final")
	assert.Empty(t, b.Rounds[1][0].Winner, "the final is reopened for re-entry")
	assert.True(t, wrEligible(t, store, compID, wrTeamAID))
}

// A default win (fusensho) recorded on a match because its competitor was
// barred by ANOTHER match records no bar of its own, so clearing it restores
// nobody: the real result is saved and the competitor stays barred by the
// match that barred them.
func TestWithdrawalClearedByCorrection_DefaultWinKeepsTheBarFromElsewhere(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "wr-fusensho"
	createTestCompetition(t, store, compID, "league", 3)
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID, Status: state.MatchStatusRunning},
		{ID: "Pool A-1", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamC, SideBID: wrTeamCID, Status: state.MatchStatusRunning},
	}))
	// Ryu withdraws in Pool A-1, then Tora is given the default win in Pool A-0.
	_, _, err := eng.RecordDecision(compID, "Pool A-1", "kiken-voluntary", "aka", "", nil, false)
	require.NoError(t, err)
	_, _, err = eng.RecordDecision(compID, "Pool A-0", "fusensho", "aka", "", nil, false)
	require.NoError(t, err)
	require.Equal(t, "fusensho", wrPoolMatch(t, store, compID).Decision)

	_, err = eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", &state.MatchResult{
		ID: "Pool A-0", SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamA, WinnerID: wrTeamAID,
		IpponsA: []string{"M", "K"}, IpponsB: []string{"K"},
		Status: state.MatchStatusCompleted, ClearsWithdrawal: true,
	})
	require.NoError(t, err)

	m := wrPoolMatch(t, store, compID)
	assert.Equal(t, "", m.Decision, "the default win is replaced by the real result")
	assert.Equal(t, wrTeamA, m.Winner)
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.Contains(t, statuses, wrTeamAID)
	assert.False(t, statuses[wrTeamAID].Eligible, "Ryu is still barred")
	assert.Equal(t, "Pool A-1", statuses[wrTeamAID].MatchID, "by the match that barred them")
}
