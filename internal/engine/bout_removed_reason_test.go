package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Only a tap made AFTER a kachinuki bout was removed is told the bout was
// removed (stage 6 review 8, m9); one made before the removal is a change the
// removal never saw, answered beforeBaseline with no such reason. The board's
// fold says the same (score_events.jsx foldBout,
// pending_tap_removed_bout.test.jsx).
func TestScoreEvents_ATapMadeBeforeABoutsRemovalIsNotToldTheBoutWasRemoved(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "op-remove"
	openerComp(t, store, c, state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusRunning, SubResults: openerRows()})
	t0 := time.Now().UnixMilli() - 60_000
	_, err := eng.RecordScoreEvents(c, "P1-0", ScoreEventsRequest{Session: "s1", Events: []domain.ScoreEvent{
		{ID: "f1", Session: "s1", Seq: 1, Stamp: t0 + 1, Kind: domain.ScoreBoutFighter, Pos: 2, Side: "a", Name: "Aoki"},
		{ID: "f2", Session: "s1", Seq: 2, Stamp: t0 + 2, Kind: domain.ScoreBoutFighter, Pos: 2, Side: "b", Name: "Goto"},
	}})
	require.NoError(t, err)
	_, err = eng.RemoveTrailingKachinukiBout(c, "P1-0")
	require.NoError(t, err)
	m := openerMatch(t, store, c, "P1-0")
	tomb := m.GroupStamps[state.BoutGroup(2)]
	require.Positive(t, tomb)
	r, err := eng.RecordScoreEvents(c, "P1-0", ScoreEventsRequest{Session: "s2", Events: []domain.ScoreEvent{
		{ID: "early", Session: "s2", Seq: 1, Stamp: t0 + 3, Kind: domain.ScoreIpponAdd, Pos: 2, Side: "b", Letter: "M"}}})
	require.NoError(t, err)
	require.NotEqual(t, EventReasonBoutRemoved, r.Answers[0].Reason)
	r, err = eng.RecordScoreEvents(c, "P1-0", ScoreEventsRequest{Session: "s2", Events: []domain.ScoreEvent{
		{ID: "late", Session: "s2", Seq: 2, Stamp: tomb + 5, Kind: domain.ScoreIpponAdd, Pos: 2, Side: "b", Letter: "M"}}})
	require.NoError(t, err)
	require.Equal(t, domain.ScoreOutcomeNoTarget, r.Answers[0].Outcome)
	require.Equal(t, EventReasonBoutRemoved, r.Answers[0].Reason)

	// The rule itself: the bout's tombstone, and a tap made after it.
	gone := &state.MatchResult{GroupStamps: map[string]int64{state.BoutGroup(2): 500}}
	require.False(t, onRemovedBout(gone, domain.ScoreEvent{Pos: 2, Stamp: 400}))
	require.False(t, onRemovedBout(gone, domain.ScoreEvent{Pos: 2, Stamp: 500}))
	require.True(t, onRemovedBout(gone, domain.ScoreEvent{Pos: 2, Stamp: 600}))
}
