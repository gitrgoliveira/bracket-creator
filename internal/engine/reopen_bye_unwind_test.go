package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A five-competitor knockout is the smallest real draw with a bye PAST the
// first round: the draw pairs P00-P01 in m-r1-0, whose winner meets nobody in
// the semifinal m-r2-0 (the other half of that pairing is an empty "" vs ""
// slot), so the bye passes them straight to the final m-r3-0. A withdrawal
// recorded by mistake on m-r1-0 therefore has a winner who went through a bye,
// and the reopen that removes it has to unwind the bye rather than be refused
// by it (a correction keeps the withdrawal, so no other door removes it).
func byeDrawFixture(t *testing.T, compID string) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, compID, "knockout", 3)
	players := make([]domain.Player, 5)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%02d", i), Dojo: fmt.Sprintf("D%02d", i)}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	// Pin the draw shape this test reasons about, so a change to the draw
	// fails here rather than as a confusing assertion below.
	b := loadBracket(t, store, compID)
	require.Equal(t, [2]string{"P00", "P01"}, [2]string{b.Rounds[0][0].SideA, b.Rounds[0][0].SideB})
	bye := b.Rounds[1][0]
	require.Equal(t, winnerOfPlaceholder(3, 0), bye.SideA)
	require.Empty(t, bye.SideB)
	require.Equal(t, state.MatchStatusCompleted, bye.Status, "generation leaves a latent bye completed with no winner")
	require.Empty(t, bye.Winner)
	require.Equal(t, winnerOfPlaceholder(2, 0), b.Rounds[2][0].SideA)
	return eng, store
}

func loadBracket(t *testing.T, store *state.Store, compID string) *state.Bracket {
	t.Helper()
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	return b
}

// scoreBracketMatch completes matchID for winner, one men, through the
// engine's score door.
func scoreBracketMatch(t *testing.T, eng *Engine, store *state.Store, compID, matchID, winner string) {
	t.Helper()
	m := findBracketMatchInBracket(loadBracket(t, store, compID), matchID)
	require.NotNil(t, m, matchID)
	r := &state.MatchResult{SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID, Status: state.MatchStatusCompleted}
	switch winner {
	case m.SideA:
		r.Winner, r.WinnerID, r.IpponsA = m.SideA, m.SideAID, []string{"M"}
	case m.SideB:
		r.Winner, r.WinnerID, r.IpponsB = m.SideB, m.SideBID, []string{"M"}
	default:
		t.Fatalf("%s is not in %s (%s vs %s)", winner, matchID, m.SideA, m.SideB)
	}
	_, err := eng.RecordMatchResultWithIneligibility(compID, matchID, r)
	require.NoError(t, err)
}

// recordMistakenKiken records P00 (aka) withdrawing from m-r1-0, so P01 wins
// and the bye passes P01 into the final.
func recordMistakenKiken(t *testing.T, eng *Engine, store *state.Store, compID string) {
	t.Helper()
	_, st, err := eng.RecordDecision(compID, "m-r1-0", "kiken-voluntary", "aka", "", nil, false)
	require.NoError(t, err)
	require.NotNil(t, st)
	b := loadBracket(t, store, compID)
	require.Equal(t, "P01", b.Rounds[1][0].Winner, "the bye resolved off the kiken's winner")
	require.Equal(t, "P01", b.Rounds[2][0].SideA)
}

// assertByeUnwound checks the semifinal bye is back to the shape generation
// gave it and the final's slot to the bye's placeholder.
func assertByeUnwound(t *testing.T, b *state.Bracket) {
	t.Helper()
	bye := b.Rounds[1][0]
	assert.Equal(t, winnerOfPlaceholder(3, 0), bye.SideA)
	assert.Empty(t, bye.SideAID)
	assert.Empty(t, bye.Winner)
	assert.Empty(t, bye.WinnerID)
	assert.Equal(t, state.MatchStatusCompleted, bye.Status)
	assert.Equal(t, winnerOfPlaceholder(2, 0), b.Rounds[2][0].SideA)
	assert.Empty(t, b.Rounds[2][0].SideAID)
}

func TestReopenUnwindsAByeTheWinnerWentThrough(t *testing.T) {
	compID := "bye-unwind"
	eng, store := byeDrawFixture(t, compID)
	recordMistakenKiken(t, eng, store, compID)

	restored, err := eng.ReopenMatch(compID, "m-r1-0", "the withdrawal was recorded on the wrong match")
	require.NoError(t, err, "the bye was never fought, so it cannot refuse the reopen")
	require.NotNil(t, restored, "the withdrawal the reopen removed bars nobody")

	b := loadBracket(t, store, compID)
	assert.Equal(t, state.MatchStatusRunning, b.Rounds[0][0].Status)
	assert.Empty(t, b.Rounds[0][0].Decision)
	assertByeUnwound(t, b)

	// Fought again, P00 wins this time: the bye passes the NEW winner on.
	scoreBracketMatch(t, eng, store, compID, "m-r1-0", "P00")
	b = loadBracket(t, store, compID)
	assert.Equal(t, "P00", b.Rounds[1][0].SideA)
	assert.Equal(t, "P00", b.Rounds[1][0].Winner)
	assert.Equal(t, b.Rounds[0][0].SideAID, b.Rounds[1][0].WinnerID)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[1][0].Status)
	assert.Equal(t, "P00", b.Rounds[2][0].SideA)
	assert.Equal(t, b.Rounds[0][0].SideAID, b.Rounds[2][0].SideAID)
}

// playToFinal plays everything up to the final: P03 beats P04, P02 beats
// P03 in the other semifinal, so the final is P01 (through the bye) vs P02.
func playToFinal(t *testing.T, eng *Engine, store *state.Store, compID string) {
	t.Helper()
	scoreBracketMatch(t, eng, store, compID, "m-r1-3", "P03")
	scoreBracketMatch(t, eng, store, compID, "m-r2-1", "P02")
	final := loadBracket(t, store, compID).Rounds[2][0]
	require.Equal(t, [2]string{"P01", "P02"}, [2]string{final.SideA, final.SideB})
}

// Past the bye, the final answers exactly as the next round after any
// reopened match does: played, the operator is told and may proceed.
func TestReopenThroughAByeWarnsForAPlayedFinalThenReopensIt(t *testing.T) {
	compID := "bye-unwind-played"
	eng, store := byeDrawFixture(t, compID)
	recordMistakenKiken(t, eng, store, compID)
	playToFinal(t, eng, store, compID)
	scoreBracketMatch(t, eng, store, compID, "m-r3-0", "P01")

	_, err := eng.ReopenMatch(compID, "m-r1-0", "the withdrawal was recorded on the wrong match")
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	require.Len(t, played.Blocking, 1)
	assert.Equal(t, "m-r3-0", played.BlockingMatchID)
	assert.Equal(t, "P01", played.Displaced, "the competitor the bye passed on is the one displaced")
	b := loadBracket(t, store, compID)
	assert.Equal(t, "P01", b.Rounds[1][0].Winner, "a refused reopen changes nothing")
	assert.Equal(t, "P01", b.Rounds[2][0].Winner)

	var reopened []ReopenedMatch
	_, err = eng.ReopenMatch(compID, "m-r1-0", "the withdrawal was recorded on the wrong match",
		ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	require.Len(t, reopened, 1)
	assert.Equal(t, "m-r3-0", reopened[0].ID)
	b = loadBracket(t, store, compID)
	final := b.Rounds[2][0]
	assert.Equal(t, state.MatchStatusScheduled, final.Status)
	assert.Empty(t, final.Winner)
	assertByeUnwound(t, b)
	assert.Equal(t, "P02", final.SideB, "the other semifinal's winner keeps their place")
}

// A final being fought past the bye refuses the reopen, as the next round
// after any reopened match does, and nothing is unwound.
func TestReopenThroughAByeIsRefusedWhileTheFinalIsFought(t *testing.T) {
	compID := "bye-unwind-running"
	eng, store := byeDrawFixture(t, compID)
	recordMistakenKiken(t, eng, store, compID)
	playToFinal(t, eng, store, compID)
	b := loadBracket(t, store, compID)
	b.Rounds[2][0].Status = state.MatchStatusRunning
	require.NoError(t, store.SaveBracket(compID, b))

	_, err := eng.ReopenMatch(compID, "m-r1-0", "the withdrawal was recorded on the wrong match")
	var running *DownstreamKnockoutRunningError
	require.ErrorAs(t, err, &running)
	require.Len(t, running.Running, 1)
	assert.Equal(t, "m-r3-0", running.Running[0].ID)
	b = loadBracket(t, store, compID)
	assert.Equal(t, "P01", b.Rounds[1][0].Winner)
	assert.Equal(t, "P01", b.Rounds[2][0].SideA)
	assert.Equal(t, "kiken-voluntary", b.Rounds[0][0].Decision)
}

// playedFinalThroughByeFixture plays the whole draw with P00 winning m-r1-0
// outright, through the bye, and then the final against P02.
func playedFinalThroughByeFixture(t *testing.T, compID string) (*Engine, *state.Store) {
	t.Helper()
	eng, store := byeDrawFixture(t, compID)
	scoreBracketMatch(t, eng, store, compID, "m-r1-0", "P00")
	scoreBracketMatch(t, eng, store, compID, "m-r1-3", "P03")
	scoreBracketMatch(t, eng, store, compID, "m-r2-1", "P02")
	scoreBracketMatch(t, eng, store, compID, "m-r3-0", "P00")
	return eng, store
}

// A CORRECTION that changes who won m-r1-0 re-resolves the bye with the new
// winner and so reaches the final, already fought. Looking only one hop down
// it saw the bye (no result of its own) and repainted the played final in
// silence: P01 was seated in it while its recorded winner stayed P00. Past
// the bye it is named like any played next round, and confirming reopens it.
func TestCorrectionThroughAByeWarnsForAPlayedFinalThenReopensIt(t *testing.T) {
	compID := "bye-correction"
	eng, store := playedFinalThroughByeFixture(t, compID)
	correction := func() *state.MatchResult {
		m := findBracketMatchInBracket(loadBracket(t, store, compID), "m-r1-0")
		return &state.MatchResult{SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID,
			Status: state.MatchStatusCompleted, Winner: m.SideB, WinnerID: m.SideBID, IpponsB: []string{"M"},
			CorrectionReason: "the wrong winner was entered"}
	}

	_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", correction())
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	require.Len(t, played.Blocking, 1)
	assert.Equal(t, "m-r3-0", played.BlockingMatchID)
	assert.Equal(t, "P00", played.Displaced)
	b := loadBracket(t, store, compID)
	assert.Equal(t, "P00", b.Rounds[0][0].Winner, "refused until confirmed")
	assert.Equal(t, "P00", b.Rounds[2][0].SideA)
	assert.Equal(t, "P00", b.Rounds[2][0].Winner)

	var reopened []ReopenedMatch
	_, err = eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", correction(), ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	require.Len(t, reopened, 1)
	assert.Equal(t, "m-r3-0", reopened[0].ID)
	b = loadBracket(t, store, compID)
	assert.Equal(t, "P01", b.Rounds[1][0].Winner, "the bye passes the corrected winner on")
	final := b.Rounds[2][0]
	assert.Equal(t, "P01", final.SideA)
	assert.Equal(t, state.MatchStatusScheduled, final.Status)
	assert.Empty(t, final.Winner)
	assert.Empty(t, final.IpponsA)
}

// The manual winner override is the same correction through another door.
func TestOverrideThroughAByeWarnsForAPlayedFinal(t *testing.T) {
	compID := "bye-override"
	eng, store := playedFinalThroughByeFixture(t, compID)

	applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", "P01", 0)
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	assert.False(t, applied)
	assert.Equal(t, "m-r3-0", played.BlockingMatchID)
	assert.Equal(t, "P00", played.Displaced)
	assert.Equal(t, "P00", loadBracket(t, store, compID).Rounds[2][0].SideA, "refused until confirmed")
}
