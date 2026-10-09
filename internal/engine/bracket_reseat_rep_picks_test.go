package engine

// A team knockout match whose side is given ANOTHER team loses the
// representative it held on that side: the pick is a member of the old team,
// and the picker offers only the seated team's members (GroupRepPicks,
// state/match_groups.go). seatBracketSide is the one owner; these tests drive
// it through the doors that re-seat a side with no store handle of their own
// (a forced score correction, override-winner) and pin what must NOT change: a
// correction that stores the same winner, and the side nobody re-seated.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rpPickA  = "pick-ryu"
	rpPickC  = "pick-kuma"
	rpNextID = "m-r2-0"
)

// rpRow is the representative bout of Ryu v Kuma with a pick on both sides.
func rpRow() state.SubMatchResult {
	return state.SubMatchResult{
		Position: state.DaihyosenSubPosition, SideA: wrTeamA, SideB: wrTeamC,
		SideAMemberID: rpPickA, SideBMemberID: rpPickC,
		Decision: string(domain.DecisionDaihyosen),
	}
}

// rpSetup stores a team knockout: m-r1-0 Ryu v Tora is played and Ryu advanced
// into m-r2-0 side A, Kuma sits on side B (a bye through m-r1-1). m-r2-0 holds
// a representative bout with both picks, dated mmT1 while the match itself was
// last written at mmT2 (a point). played makes m-r2-0 a finished match whose
// rep bout Ryu's pick won.
func rpSetup(t *testing.T, played bool) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	const compID = "rp-reseat"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "rp", Kind: "team", TeamSize: 3, Status: state.CompStatusKnockout,
	}))
	wrSaveTeams(t, store, compID)
	next := state.BracketMatch{
		ID: rpNextID, MatchNumber: 3,
		SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamC, SideBID: wrTeamCID,
		Status:     state.MatchStatusScheduled,
		SubResults: []state.SubMatchResult{rpRow()},
		ModifiedAt: mmT2,
		GroupStamps: map[string]int64{
			state.GroupRepPicks:                         mmT1,
			state.BoutGroup(state.DaihyosenSubPosition): mmT1,
			state.GroupPoints:                           mmT2,
		},
	}
	if played {
		next.Status = state.MatchStatusCompleted
		next.Winner, next.WinnerID = wrTeamA, wrTeamAID
		next.SubResults[0].Winner = wrTeamA
		next.SubResults[0].WinnerMemberID = rpPickA
		next.SubResults[0].IpponsA = []string{"M"}
		next.GroupStamps[state.GroupResult] = mmT2
	}
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{ID: "m-r1-0", MatchNumber: 1, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
				Status: state.MatchStatusCompleted, Winner: wrTeamA, WinnerID: wrTeamAID,
				SubResults: []state.SubMatchResult{wrBout1("M")}},
			{ID: "m-r1-1", MatchNumber: 2, SideA: wrTeamC, SideAID: wrTeamCID,
				Status: state.MatchStatusCompleted, Winner: wrTeamC, WinnerID: wrTeamCID},
		},
		{next},
	}}))
	return eng, store, compID
}

// rpCorrect is the team editor's correction of m-r1-0 naming winner (Ryu or
// Tora), with the bout that team's fighter won.
func rpCorrect(winner string) *state.MatchResult {
	id, bout := wrTeamAID, state.SubMatchResult{Position: 1, SideA: "r1", SideB: "t1", Winner: "r1", IpponsA: []string{"K"}}
	if winner == wrTeamB {
		id, bout = wrTeamBID, state.SubMatchResult{Position: 1, SideA: "r1", SideB: "t1", Winner: "t1", IpponsB: []string{"K"}}
	}
	return &state.MatchResult{
		ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Winner: winner, WinnerID: id, IpponsA: []string{}, IpponsB: []string{},
		Status: state.MatchStatusCompleted, CorrectionReason: "Scoring error: wrong waza entered",
		SubResults: []state.SubMatchResult{bout},
	}
}

func rpNext(t *testing.T, store *state.Store, compID string) state.BracketMatch {
	t.Helper()
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := b.MatchByID(rpNextID)
	require.NotNil(t, m)
	return *m
}

// rpOldPickReplay is the write that seated both picks, replayed: it names the
// picks alone, at the stamp they were made at, and carries no sides (the stored
// pairing fills them).
func rpOldPickReplay() *state.MatchResult {
	return &state.MatchResult{
		ID: rpNextID, Status: state.MatchStatusScheduled, ModifiedAt: mmT1,
		Changed: []string{state.GroupRepPicks}, WriteDoor: DoorScore,
		SubResults: []state.SubMatchResult{rpRow()},
	}
}

// assertSideAPickGone checks what every re-seat of side A must leave: side A is
// Tora, its pick is gone, Kuma's pick stands, and the picks are dated after the
// ones the old team's member was made at and not before the match itself.
func assertSideAPickGone(t *testing.T, next state.BracketMatch, priorModifiedAt int64) {
	t.Helper()
	assert.Equal(t, wrTeamB, next.SideA)
	assert.Equal(t, wrTeamBID, next.SideAID)
	a, c := next.RepPicks()
	assert.Empty(t, a, "side A was given another team: Ryu's member no longer stands for it")
	assert.Equal(t, rpPickC, c, "side B was not re-seated: its pick stays")
	assert.Greater(t, next.GroupStamp(state.GroupRepPicks), mmT1, "dated after the stamp the picks held")
	assert.GreaterOrEqual(t, next.GroupStamp(state.GroupRepPicks), priorModifiedAt, "and never before the match")
	assert.GreaterOrEqual(t, next.ModifiedAt, next.GroupStamp(state.GroupRepPicks))
}

func TestReseat_ScheduledDownstreamLosesTheReseatedSidesPick(t *testing.T) {
	eng, store, compID := rpSetup(t, false)
	prior := rpNext(t, store, compID)

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", rpCorrect(wrTeamB), ForceOptions{})
		return err
	}))
	assertSideAPickGone(t, rpNext(t, store, compID), prior.ModifiedAt)

	// The write that made the old pick, replayed from a queue, is older than the
	// clear: it is kept in the history, not put back.
	_, err := eng.RecordMatchResultWithIneligibility(compID, rpNextID, rpOldPickReplay())
	require.ErrorIs(t, err, ErrMatchSuperseded)
	assert.Equal(t, []string{state.GroupRepPicks}, HeldGroupsOf(err))
	after := rpNext(t, store, compID)
	a, c := after.RepPicks()
	assert.Empty(t, a, "the replay did not seat Ryu's member on Tora's side")
	assert.Equal(t, rpPickC, c)
}

func TestReseat_PlayedDownstreamLosesThePickAndItsReopenLineNamesIt(t *testing.T) {
	eng, store, compID := rpSetup(t, true)
	prior := rpNext(t, store, compID)

	var reopened []ReopenedMatch
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", rpCorrect(wrTeamB),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	}))
	require.Equal(t, []string{rpNextID}, reopenedIDs(reopened))
	assert.True(t, reopened[0].RepPickCleared, "the reopened match is told its pick went with the team")

	next := rpNext(t, store, compID)
	assertSideAPickGone(t, next, prior.ModifiedAt)
	assert.Equal(t, state.MatchStatusScheduled, next.Status, "its verdict went")
	assert.Empty(t, next.Winner)
	assert.Empty(t, next.SubResults[0].WinnerMemberID, "the rep bout's winner id went with the verdict and the pick")

	entries, err := store.LoadMatchHistory(compID, rpNextID)
	require.NoError(t, err)
	var reopenLine *state.MatchHistoryEntry
	for i := range entries {
		if entries[i].Door == doorDownstreamReopen {
			reopenLine = &entries[i]
		}
	}
	require.NotNil(t, reopenLine, "the played match gets a line for its reopen")
	assert.Contains(t, reopenLine.Changed, state.GroupRepPicks)
	assert.Contains(t, reopenLine.Changed, state.GroupResult)
}

func TestReseat_PlayedDownstreamReopenLineNamesNoPicksWhenNoneWentWithIt(t *testing.T) {
	eng, store, compID := rpSetup(t, true)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := b.MatchByID(rpNextID)
	m.SubResults[0].SideAMemberID, m.SubResults[0].SideBMemberID = "", ""
	require.NoError(t, store.SaveBracket(compID, b))

	var reopened []ReopenedMatch
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", rpCorrect(wrTeamB),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	}))
	require.Equal(t, []string{rpNextID}, reopenedIDs(reopened))
	assert.False(t, reopened[0].RepPickCleared)
	entries, err := store.LoadMatchHistory(compID, rpNextID)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Changed, state.GroupRepPicks, "there was no pick to clear")
	}
}

func TestReseat_OverrideWinnerLosesTheReseatedSidesPick(t *testing.T) {
	eng, store, compID := rpSetup(t, false)
	prior := rpNext(t, store, compID)

	applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", wrTeamB, 0)
	require.NoError(t, err)
	require.True(t, applied)
	assertSideAPickGone(t, rpNext(t, store, compID), prior.ModifiedAt)
}

// A correction that stores the winner already recorded re-propagates the same
// team: nothing about the downstream match moves, its picks and their date
// included. Green before the owner existed too; it is the no-op guard.
func TestReseat_CorrectionStoringTheSameWinnerLeavesThePicksAlone(t *testing.T) {
	eng, store, compID := rpSetup(t, false)
	prior := rpNext(t, store, compID)

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", rpCorrect(wrTeamA), ForceOptions{})
		return err
	}))
	next := rpNext(t, store, compID)
	a, c := next.RepPicks()
	assert.Equal(t, rpPickA, a)
	assert.Equal(t, rpPickC, c)
	assert.Equal(t, mmT1, next.GroupStamp(state.GroupRepPicks), "the picks keep their date")
	assert.Equal(t, prior.ModifiedAt, next.ModifiedAt, "and the match is not touched")
}
