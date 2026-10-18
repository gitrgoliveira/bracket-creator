package engine

import (
	"encoding/json"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/require"
)

// review5Lines reads a score log written as JSON lines, the way the golden
// table holds them.
func review5Lines(t *testing.T, raw ...string) []state.ScoreLogLine {
	t.Helper()
	out := make([]state.ScoreLogLine, 0, len(raw))
	for _, r := range raw {
		var l state.ScoreLogLine
		require.NoError(t, json.Unmarshal([]byte(r), &l), r)
		require.NoError(t, l.Validate(), r)
		out = append(out, l)
	}
	return out
}

func review5Outcome(r Replay, id string) string {
	for _, o := range r.Outcomes {
		if o.ID == id {
			return o.Outcome
		}
	}
	return ""
}

// A replay stopped before a finish never reaches it, so the finish turns no
// tap away there: closingResults honours stopAt (stage 6 review 5, finding 6).
func TestReplayScoring_StoppedBeforeAFinishTurnsNoTapAway(t *testing.T) {
	shape, err := goldenShape("individual", false)
	require.NoError(t, err)
	match := state.MatchResult{ID: "Pool A-0", SideA: "Aka", SideB: "Shiro", SideAID: "pa", SideBID: "pb",
		Status: state.MatchStatusRunning, IpponsA: []string{}, IpponsB: []string{}}
	lines := review5Lines(t,
		`{"v":1,"baseline":{"id":"m0","door":"test","stamp":100,"values":{"points":{"ipponsA":[],"ipponsB":[],"hansokuA":0,"hansokuB":0},"result":{"status":"running"}},"basis":{"seen":0}}}`,
		`{"v":1,"event":{"id":"e1","session":"s1","seq":1,"stamp":150,"kind":"ippon.add","side":"a","letter":"M"}}`,
		`{"v":1,"baseline":{"id":"f","door":"score","stamp":200,"values":{"result":{"status":"completed","winner":"Aka","winnerId":"pa"}},"basis":{"seen":1},"basisSent":true}}`,
	)
	whole, _ := replayScoring(&match, lines, shape, nil)
	require.Equal(t, domain.ScoreOutcomeBeforeBaseline, review5Outcome(whole, "e1"), "the whole log: the finish never saw the tap")
	stopped, _ := replayScoring(&match, lines, shape, &replayItem{stamp: 180, baseline: true})
	require.Equal(t, domain.ScoreOutcomeApplied, review5Outcome(stopped, "e1"), "stopped before the finish: nothing has closed the match")
	require.Equal(t, []string{"M"}, stopped.Match.IpponsA)
}
