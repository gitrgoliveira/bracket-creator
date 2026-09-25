package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retract_through_bye_test.go pins retractIntoUntouched against a winner that
// was passed through a hidden bye. Knockout rounds count back from the final,
// so a pair whose neighbouring slot pair is empty feeds a pass-through match
// that generation leaves completed and propagateBracketWinner resolves off the
// winner; that winner is seated in the first real match past it. A correction
// that reopens the pair (a pool correction's requalification, or a forced
// knockout correction reopening the next round) must take the winner back out
// of that match too, unwinding the bye, whenever nobody has touched it since.

// sixQualifierBlock is a mixed competition on one court with six pools of two
// and one qualifier each: six qualifiers in an eight-slot block, laid out
// [A, B, C, D, E, F, "", ""]. Pool E-1st v Pool F-1st (m-r1-2) sits beside the
// empty pair, so its winner passes through the hidden bye m-r2-1 into the
// block final m-r3-0. Every pool is played with X1 beating X2, the knockout is
// seated, and E1 beats F1, so the bye carries E1 into the final.
func sixQualifierBlock(t *testing.T, compID string) *rqFix {
	t.Helper()
	pools := [][]string{{"A1", "A2"}, {"B1", "B2"}, {"C1", "C2"}, {"D1", "D2"}, {"E1", "E2"}, {"F1", "F2"}}
	f := newRQFixture(t, compID, 1, pools)

	// Pin the draw shape this test reasons about, so a change to the draw
	// fails here rather than as a confusing assertion below.
	b := f.bracket()
	require.Len(t, b.Rounds, 3)
	pair := b.Rounds[0][2]
	require.Equal(t, "m-r1-2", pair.ID)
	require.Equal(t, [2]string{"Pool E-1st", "Pool F-1st"}, [2]string{pair.PlaceholderA, pair.PlaceholderB})
	empty := b.Rounds[0][3]
	require.Equal(t, [2]string{"", ""}, [2]string{empty.SideA, empty.SideB}, "the pair beside E v F is empty")
	bye := b.Rounds[1][1]
	require.Equal(t, "m-r2-1", bye.ID)
	require.Equal(t, winnerOfPlaceholder(3, 2), bye.SideA)
	require.Empty(t, bye.SideB)
	require.Equal(t, state.MatchStatusCompleted, bye.Status, "generation leaves the hidden bye completed with no winner")
	require.Empty(t, bye.Winner)
	final := b.Rounds[2][0]
	require.Equal(t, "m-r3-0", final.ID)
	require.Equal(t, winnerOfPlaceholder(2, 1), final.SideB)

	for _, pn := range pools {
		pool := "Pool " + pn[0][:1]
		f.scorePool(pool+"-0", pn[0])
	}
	f.resolve()

	require.NoError(t, f.scoreKO("m-r1-2", "E1"))
	b = f.bracket()
	require.Equal(t, "E1", b.Rounds[1][1].Winner, "the bye resolved off E v F's winner")
	require.Equal(t, "E1", b.Rounds[2][0].SideB, "and passed them into the block final")
	require.Equal(t, rqID("E1"), b.Rounds[2][0].SideBID)
	return f
}

// playSemifinalOne plays the other side of the block, so the final names both
// sides: A1 v E1.
func playSemifinalOne(t *testing.T, f *rqFix) {
	t.Helper()
	require.NoError(t, f.scoreKO("m-r1-0", "A1"))
	require.NoError(t, f.scoreKO("m-r1-1", "C1"))
	require.NoError(t, f.scoreKO("m-r2-0", "A1"))
	final := f.bracket().Rounds[2][0]
	require.Equal(t, [2]string{"A1", "E1"}, [2]string{final.SideA, final.SideB})
	require.True(t, bracketMatchPlayable(&final))
}

// A pool correction that moves Pool E's qualifier reopens E v F, and the block
// final past the bye, which nobody has touched, must stop naming E1. Judging
// the bye instead of the final read the bye as played (it is completed, with
// a winner), kept it, and left E1 seated in a scheduled final with both sides
// named: a final that could be started with a competitor who no longer
// qualifies.
func TestRequalify_RetractsThroughAByeFromAnUntouchedFinal(t *testing.T) {
	f := sixQualifierBlock(t, "rtb-requalify")
	playSemifinalOne(t, f)

	err := f.write("Pool E-0", f.poolResult("Pool E-0", "E2"))
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	require.Equal(t, []string{"m-r1-2"}, reopenedIDs(played.Blocking), "only the match E1 fought is named; the bye and the untouched final are not")
	assert.Equal(t, "E1", played.Displaced)

	var reopened []ReopenedMatch
	require.NoError(t, f.write("Pool E-0", f.poolResult("Pool E-0", "E2"), ForceOptions{Force: true, Reopened: &reopened}))
	require.Equal(t, []string{"m-r1-2"}, reopenedIDs(reopened))

	b := f.bracket()
	pair := b.Rounds[0][2]
	assert.Equal(t, state.MatchStatusScheduled, pair.Status)
	assert.Equal(t, "E2", pair.SideA, "the new qualifier is seated in the reopened match")
	assert.Equal(t, rqID("E2"), pair.SideAID)

	bye := b.Rounds[1][1]
	assert.Empty(t, bye.Winner, "the bye no longer passes E1 on")
	assert.Empty(t, bye.WinnerID)
	assert.Equal(t, winnerOfPlaceholder(3, 2), bye.SideA, "the bye's fed side is back to its placeholder")
	assert.Empty(t, bye.SideAID)
	assert.Equal(t, state.MatchStatusCompleted, bye.Status, "a hidden bye stays completed, as generation left it")

	final := b.Rounds[2][0]
	assert.Equal(t, winnerOfPlaceholder(2, 1), final.SideB, "the untouched final waits for E v F again instead of naming E1")
	assert.Empty(t, final.SideBID, "a placeholder carries no id")
	assert.Equal(t, "A1", final.SideA, "the other side is not this correction's")
	assert.Equal(t, state.MatchStatusScheduled, final.Status)
	assert.False(t, bracketMatchPlayable(&final))

	// Starting it (a running-status write through the score door) is refused:
	// a feeder has not finished.
	start := &state.MatchResult{SideA: final.SideA, SideB: final.SideB, SideAID: final.SideAID, SideBID: final.SideBID, Status: state.MatchStatusRunning}
	err = f.write("m-r3-0", start)
	require.Error(t, err, "a final with an unresolved side cannot be started")
	assert.Contains(t, err.Error(), "not ready to score")

	// Fought again, the bye passes the NEW qualifier on exactly as it passed
	// the first one, which is what shows the unwind restored generation's shape.
	require.NoError(t, f.scoreKO("m-r1-2", "E2"))
	b = f.bracket()
	assert.Equal(t, "E2", b.Rounds[1][1].Winner)
	assert.Equal(t, rqID("E2"), b.Rounds[1][1].WinnerID)
	assert.Equal(t, "E2", b.Rounds[2][0].SideB)
	assert.Equal(t, rqID("E2"), b.Rounds[2][0].SideBID)
}

// A final that was already FOUGHT past the bye is one hop further than the
// pool correction reaches: the bye and the final are left exactly as they
// are, and the operator is asked about the final in its own turn, when E v F
// is fought again with a different winner.
func TestRequalify_LeavesAByeBeforeAPlayedFinal(t *testing.T) {
	f := sixQualifierBlock(t, "rtb-requalify-played")
	playSemifinalOne(t, f)
	require.NoError(t, f.scoreKO("m-r3-0", "E1"))

	var reopened []ReopenedMatch
	require.NoError(t, f.write("Pool E-0", f.poolResult("Pool E-0", "E2"), ForceOptions{Force: true, Reopened: &reopened}))
	require.Equal(t, []string{"m-r1-2"}, reopenedIDs(reopened), "the played final is not this correction's hop")

	b := f.bracket()
	bye := b.Rounds[1][1]
	assert.Equal(t, "E1", bye.SideA, "the bye before a played final is not unwound")
	assert.Equal(t, "E1", bye.Winner)
	assert.Equal(t, rqID("E1"), bye.WinnerID)
	final := b.Rounds[2][0]
	assert.Equal(t, "E1", final.SideB)
	assert.Equal(t, rqID("E1"), final.SideBID)
	assert.Equal(t, "E1", final.Winner)
	assert.Equal(t, state.MatchStatusCompleted, final.Status)

	// Its own warning, through the bye it kept: E2 winning E v F would
	// displace E1 from the played final.
	err := f.scoreKO("m-r1-2", "E2")
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	assert.Equal(t, "m-r3-0", played.BlockingMatchID)
	assert.Equal(t, "E1", played.Displaced)
}

// nineDrawFixture is a nine-competitor knockout on one court. P00 v P01
// (m-r1-0) and P02 v P03 (m-r1-1) meet in m-r2-0, whose winner meets nobody in
// m-r3-0 (the other half of that pairing is an empty "" vs "" block), so the
// bye passes them straight to the final m-r4-0. The first round is played and
// P00 wins m-r2-0, so the bye carries P00 into the final, which nobody has
// touched (its other half is unplayed).
func nineDrawFixture(t *testing.T, compID string) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, compID, "knockout", 3)
	players := make([]domain.Player, 9)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%02d", i), Dojo: fmt.Sprintf("D%02d", i)}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	// Pin the draw shape this test reasons about.
	b := loadBracket(t, store, compID)
	require.Len(t, b.Rounds, 4)
	require.Equal(t, [2]string{"P00", "P01"}, [2]string{b.Rounds[0][0].SideA, b.Rounds[0][0].SideB})
	require.Equal(t, [2]string{"P02", "P03"}, [2]string{b.Rounds[0][1].SideA, b.Rounds[0][1].SideB})
	require.Equal(t, [2]string{winnerOfPlaceholder(4, 0), winnerOfPlaceholder(4, 1)}, [2]string{b.Rounds[1][0].SideA, b.Rounds[1][0].SideB})
	bye := b.Rounds[2][0]
	require.Equal(t, "m-r3-0", bye.ID)
	require.Equal(t, winnerOfPlaceholder(3, 0), bye.SideA)
	require.Empty(t, bye.SideB)
	require.Equal(t, state.MatchStatusCompleted, bye.Status, "generation leaves the bye completed with no winner")
	require.Empty(t, bye.Winner)
	require.Equal(t, winnerOfPlaceholder(2, 0), b.Rounds[3][0].SideA)

	scoreBracketMatch(t, eng, store, compID, "m-r1-0", "P00")
	scoreBracketMatch(t, eng, store, compID, "m-r1-1", "P02")
	scoreBracketMatch(t, eng, store, compID, "m-r2-0", "P00")
	b = loadBracket(t, store, compID)
	require.Equal(t, "P00", b.Rounds[2][0].Winner, "the bye resolved off m-r2-0's winner")
	require.Equal(t, "P00", b.Rounds[3][0].SideA, "and passed them into the final")
	require.Equal(t, state.MatchStatusScheduled, b.Rounds[3][0].Status)
	return eng, store
}

// A forced knockout correction reopens the next round it displaced someone
// from (m-r2-0) and takes back what THAT match had sent on: past the bye, the
// untouched final stops naming P00, whose place in it rested on a win the
// correction just cleared.
func TestForcedCorrection_RetractsThroughAByeFromAnUntouchedFinal(t *testing.T) {
	compID := "rtb-correction"
	eng, store := nineDrawFixture(t, compID)
	correction := func() *state.MatchResult {
		m := findBracketMatchInBracket(loadBracket(t, store, compID), "m-r1-0")
		return &state.MatchResult{SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID,
			Status: state.MatchStatusCompleted, Winner: m.SideB, WinnerID: m.SideBID, IpponsB: []string{"M"},
			CorrectionReason: "the wrong winner was entered"}
	}

	_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", correction())
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	require.Equal(t, "m-r2-0", played.BlockingMatchID)

	var reopened []ReopenedMatch
	_, err = eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", correction(), ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	require.Equal(t, []string{"m-r2-0"}, reopenedIDs(reopened))

	b := loadBracket(t, store, compID)
	semi := b.Rounds[1][0]
	assert.Equal(t, state.MatchStatusScheduled, semi.Status)
	assert.Equal(t, [2]string{"P01", "P02"}, [2]string{semi.SideA, semi.SideB})
	assert.Empty(t, semi.Winner)

	bye := b.Rounds[2][0]
	assert.Empty(t, bye.Winner, "the bye no longer passes P00 on")
	assert.Empty(t, bye.WinnerID)
	assert.Equal(t, winnerOfPlaceholder(3, 0), bye.SideA, "the bye's fed side is back to its placeholder")
	assert.Empty(t, bye.SideAID)
	assert.Equal(t, state.MatchStatusCompleted, bye.Status)

	final := b.Rounds[3][0]
	assert.Equal(t, winnerOfPlaceholder(2, 0), final.SideA, "the untouched final waits for m-r2-0 again instead of naming P00")
	assert.Empty(t, final.SideAID, "a placeholder carries no id")
	assert.Equal(t, state.MatchStatusScheduled, final.Status)

	// Fought again, the bye passes the new winner on.
	scoreBracketMatch(t, eng, store, compID, "m-r2-0", "P01")
	b = loadBracket(t, store, compID)
	assert.Equal(t, "P01", b.Rounds[2][0].Winner)
	assert.Equal(t, "P01", b.Rounds[3][0].SideA)
	assert.Equal(t, b.Rounds[1][0].SideAID, b.Rounds[3][0].SideAID)
}
