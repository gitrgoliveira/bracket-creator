package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func appendHistoryLine(t *testing.T, store *state.Store, matchID, door string) {
	t.Helper()
	require.NoError(t, store.WithTransaction(slwComp, func(tx state.StoreTx) error {
		return tx.AppendMatchHistory(slwComp, state.MatchHistoryEntry{
			MatchID: matchID, Door: door, ReceivedAt: time.Now().UnixMilli(),
			Changed: []string{state.GroupPoints}, Outcomes: map[string]string{state.GroupPoints: state.HistoryOutcomeApplied},
		})
	}))
}

// A client-built write with no basis whose applied points only echo the stored
// value says nothing about the older taps, so it does not mark them "no basis".
func TestScoreEvents_NoBasisWriteThatOnlyEchoesLeavesOlderEventsApplied(t *testing.T) {
	eng, store, _ := slwSetup(t)
	t0 := time.Now().UnixMilli() - 60_000
	slwPost(t, eng, slwMatch, slwEvent("ev1", 1, t0, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	other := domain.ScoreEvent{ID: "ev2", Session: "other", Seq: 1, Stamp: t0 + 500, Kind: domain.ScoreIpponAdd, Side: domain.ScoreSideB, Letter: "K"}
	_, err := eng.RecordScoreEvents(slwComp, slwMatch, ScoreEventsRequest{Session: "other", Events: []domain.ScoreEvent{other}})
	require.NoError(t, err)

	w := &state.MatchResult{Status: state.MatchStatusRunning, IpponsA: []string{"M"}, IpponsB: []string{"K"},
		ModifiedAt: t0 + 1000, Changed: []string{state.GroupPoints}, WriteDoor: DoorScore}
	_, err = eng.RecordMatchResultWithIneligibility(slwComp, slwMatch, w)
	require.NoError(t, err)
	require.NotNil(t, w.Merge)
	assert.Equal(t, []string{state.GroupPoints}, w.Merge.Unchanged, "the points only echo the stored value")
	assert.Empty(t, w.Merge.EventsNotApplied)
	history, err := store.LoadMatchHistory(slwComp, slwMatch)
	require.NoError(t, err)
	for _, h := range history {
		assert.NotEqual(t, domain.ScoreOutcomeBeforeBaseline, h.EventOutcome, "no older tap is called unrecorded")
	}
	assertLogIsTheMatch(t, eng, store, slwMatch)
}

// A line that cannot have moved the match's status (a reseat, a displaced
// change, a downstream reopen, a kachinuki step) after the send back does not
// turn "sent back" into "not running".
func TestScoreEvents_SentBackSurvivesLaterLinesThatDoNotChangeStatus(t *testing.T) {
	eng, store, _ := slwSetup(t)
	now := time.Now().UnixMilli()
	slwPost(t, eng, slwMatch, slwEvent("e1", 1, now-2000, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	require.NoError(t, eng.RevertMatchToQueue(slwComp, slwMatch))
	for _, door := range []string{doorReseat, DoorDisplaced, doorDownstreamReopen, doorKachinukiAdvance} {
		appendHistoryLine(t, store, slwMatch, door)
	}
	res := slwPost(t, eng, slwMatch, slwEvent("e2", 2, now, domain.ScoreIpponAdd, domain.ScoreSideA, "K"))
	assert.Equal(t, EventReasonSentBack, res.Answers[0].Reason)
	assert.Equal(t, EventMessageSentBack, res.Answers[0].Message)

	// A status-changing line after it does change the wording.
	appendHistoryLine(t, store, slwMatch, doorReopen)
	res = slwPost(t, eng, slwMatch, slwEvent("e3", 3, now+1, domain.ScoreIpponAdd, domain.ScoreSideA, "D"))
	assert.Equal(t, EventMessageNotRunning, res.Answers[0].Message)
}

// The same event turned away for a new reason is recorded again; an exact
// resend is not.
func TestScoreEvents_TurnedAwayForANewReasonReachesTheHistory(t *testing.T) {
	eng, store, _ := slwSetup(t)
	now := time.Now().UnixMilli()
	post := func() {
		slwPost(t, eng, "Pool A-1", slwEvent("x", 1, now, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	}
	count := func() int {
		history, err := store.LoadMatchHistory(slwComp, "Pool A-1")
		require.NoError(t, err)
		n := 0
		for _, h := range history {
			if h.Event != nil && h.Event.ID == "x" {
				n++
			}
		}
		return n
	}
	post()
	post()
	assert.Equal(t, 1, count(), "an exact resend adds nothing")
	appendHistoryLine(t, store, "Pool A-1", doorRequeue)
	post()
	assert.Equal(t, 2, count(), "the new reason (sent back) is news")
	post()
	assert.Equal(t, 2, count())
}
