package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func postIn(t *testing.T, eng *Engine, compID, matchID string, events ...domain.ScoreEvent) *ScoreEventsResult {
	t.Helper()
	res, err := eng.RecordScoreEvents(compID, matchID, ScoreEventsRequest{Session: "evs", Events: events})
	require.NoError(t, err)
	return res
}

// The engi finish (engi recorder) leaves its baseline; flags tapped after it
// fold onto the finished match.
func TestScoreEvents_EngiFinishIsABaseline(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "slw-engi"
	setupEngiComp(t, store, compID, state.CompFormatLeague)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Aka", SideB: "Shiro", Court: "A", Status: state.MatchStatusRunning},
	}))
	t0 := time.Now().UnixMilli() - 60_000
	flag := func(id string, seq, stamp int64, kind domain.ScoreEventKind, side string) domain.ScoreEvent {
		return domain.ScoreEvent{ID: id, Session: "evs", Seq: seq, Stamp: stamp, Kind: kind, Side: side}
	}
	res := postIn(t, eng, compID, "Pool A-0",
		flag("f1", 1, t0, domain.ScoreFlagAdd, domain.ScoreSideA),
		flag("f2", 2, t0+1, domain.ScoreFlagAdd, domain.ScoreSideA),
		flag("f3", 3, t0+2, domain.ScoreFlagAdd, domain.ScoreSideB))
	assert.Equal(t, 2, res.Match.FlagsA)
	assert.Equal(t, 1, res.Match.FlagsB)
	assertLogIsTheMatchIn(t, eng, store, compID, "Pool A-0")

	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", &state.MatchResult{
		SideA: "Aka", SideB: "Shiro", Status: state.MatchStatusCompleted, FlagsA: 2, FlagsB: 1,
		ModifiedAt: t0 + 1000, WriteDoor: DoorScore,
	})
	require.NoError(t, err)
	lines := assertLogIsTheMatchIn(t, eng, store, compID, "Pool A-0")
	require.NotNil(t, lines[len(lines)-1].Baseline, "the engi finish is a baseline")

	res = postIn(t, eng, compID, "Pool A-0",
		flag("f4", 4, t0+2000, domain.ScoreFlagAdd, domain.ScoreSideA),
		flag("f5", 5, t0+2001, domain.ScoreFlagAdd, domain.ScoreSideA))
	// The finish closed the match: flags counted after it are turned away.
	assert.Equal(t, domain.ScoreOutcomeMatchClosed, res.Answers[0].Outcome)
	assert.Equal(t, domain.ScoreOutcomeMatchClosed, res.Answers[1].Outcome)
	assert.Equal(t, 2, res.Match.FlagsA)
	assert.Equal(t, state.MatchStatusCompleted, res.Match.Status)
	assertLogIsTheMatchIn(t, eng, store, compID, "Pool A-0")
}

// Record bout's advance and the removal of the pairing it appended leave
// baselines; an event on the bout a write created replays the same twice.
func TestScoreEvents_KachinukiAdvanceAndRemoveAreBaselines(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "slw-kachi"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "B-Senpo", Decision: "fought", IpponsB: []string{"M"}, IpponsA: []string{}},
			{Position: 2, SideA: "A-Jiho", SideB: "B-Chuken", IpponsA: []string{}, IpponsB: []string{}},
		},
	}}))
	t0 := time.Now().UnixMilli() - 60_000
	bout := func(id string, seq, stamp int64, pos int, side, letter string) domain.ScoreEvent {
		return domain.ScoreEvent{ID: id, Session: "evs", Seq: seq, Stamp: stamp, Kind: domain.ScoreIpponAdd, Pos: pos, Side: side, Letter: letter}
	}
	res := postIn(t, eng, compID, "P1-0", bout("k1", 1, t0, 2, domain.ScoreSideA, "M"), bout("k1b", 3, t0+1, 2, domain.ScoreSideA, "K"))
	assert.Equal(t, domain.ScoreOutcomeApplied, res.Answers[1].Outcome)
	require.Len(t, res.Match.SubResults, 2)
	assert.Equal(t, "A-Jiho", res.Match.SubResults[1].Winner, "two points win the bout")
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")

	advanced, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	require.True(t, advanced)
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")

	_, err = eng.RemoveTrailingKachinukiBout(compID, "P1-0")
	require.NoError(t, err)
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")

	advanced, _, err = eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	require.True(t, advanced)
	res = postIn(t, eng, compID, "P1-0", bout("k2", 4, time.Now().UnixMilli(), 3, domain.ScoreSideB, "K"))
	assert.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome, "an event lands on the bout the advance appended")
	assertLogIsTheMatchIn(t, eng, store, compID, "P1-0")
}

// The knockout override-winner door leaves its baseline.
func TestScoreEvents_OverrideWinnerIsABaseline(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "slw-ko"
	createTestCompetition(t, store, compID, "knockout", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bm := bracket.Rounds[0][0]
	require.NoError(t, eng.RecordMatchResult(compID, bm.ID, &state.MatchResult{
		SideA: bm.SideA, SideB: bm.SideB, Status: state.MatchStatusRunning, IpponsA: []string{}, IpponsB: []string{},
		ModifiedAt: time.Now().UnixMilli() - 60_000, WriteDoor: DoorScore, Changed: []string{state.GroupResult},
	}))
	res := postIn(t, eng, compID, bm.ID, domain.ScoreEvent{ID: "o1", Session: "evs", Seq: 1,
		Stamp: time.Now().UnixMilli() - 50_000, Kind: domain.ScoreIpponAdd, Side: domain.ScoreSideA, Letter: "M"})
	assert.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome)
	assertLogIsTheMatchIn(t, eng, store, compID, bm.ID)

	applied, err := eng.OverrideBracketWinner(compID, bm.ID, bm.SideB, 0)
	require.NoError(t, err)
	require.True(t, applied)
	assertLogIsTheMatchIn(t, eng, store, compID, bm.ID)
}
