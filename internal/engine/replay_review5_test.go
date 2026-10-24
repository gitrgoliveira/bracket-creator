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

// A decision keeps the points its loser struck before it (keepLoserPoints),
// so a loser's tap stamped before it lands whichever way it arrives. It is
// answered "recorded" only when the decision's basis held it; one the
// decision never saw is answered as any landed tap is, "applied", since
// "recorded" would claim the decision held it (stage 6 review 5, finding 3).
// The winner's tap is replaced by the decision's circles: recorded when the
// decision held it (or sent no basis, so saw everything stamped before it),
// beforeBaseline only when a sent basis never held it (stage 6 review 6: the
// operator's own tap, held by the decision recorded on the same device, was
// told "Not applied").
func TestReplayScoring_DecisionAnswersALoserTapItNeverSawAsApplied(t *testing.T) {
	shape, err := goldenShape("individual", false)
	require.NoError(t, err)
	match := state.MatchResult{ID: "Pool A-0", SideA: "Aka", SideB: "Shiro", SideAID: "pa", SideBID: "pb",
		Status: state.MatchStatusRunning, IpponsA: []string{}, IpponsB: []string{}}
	start := `{"v":1,"baseline":{"id":"m0","door":"test","stamp":100,"values":{"points":{"ipponsA":[],"ipponsB":[],"hansokuA":0,"hansokuB":0},"result":{"status":"running"}},"basis":{"seen":0}}}`
	loser := `{"v":1,"event":{"id":"lose","session":"s1","seq":1,"stamp":150,"kind":"ippon.add","side":"b","letter":"M"}}`
	winner := `{"v":1,"event":{"id":"win","session":"s1","seq":2,"stamp":160,"kind":"ippon.add","side":"a","letter":"K"}}`
	const decisionHead = `{"v":1,"baseline":{"id":"d","door":"decision","stamp":200,"values":{"points":{"ipponsA":["○","○"],"ipponsB":[],"hansokuA":0,"hansokuB":0},"result":{"status":"completed","winner":"Aka","winnerId":"pa","decision":"kiken-voluntary","decisionBy":"shiro"}}`
	decision := func(seen int) string {
		return decisionHead + `,"basis":{"seen":` + string(rune('0'+seen)) + `},"basisSent":true}}`
	}
	noBasis := decisionHead + `}}`
	for _, tc := range []struct {
		name       string
		lines      []string
		wantLoser  string
		wantWinner string
	}{
		{"the taps arrive after a decision that saw neither", []string{start, decision(1), loser, winner}, domain.ScoreOutcomeApplied, domain.ScoreOutcomeBeforeBaseline},
		{"the taps arrive before a decision that saw neither", []string{start, loser, winner, decision(1)}, domain.ScoreOutcomeApplied, domain.ScoreOutcomeBeforeBaseline},
		{"the decision saw both", []string{start, loser, winner, decision(3)}, domain.ScoreOutcomeRecorded, domain.ScoreOutcomeRecorded},
		{"the decision sent no basis", []string{start, loser, winner, noBasis}, domain.ScoreOutcomeRecorded, domain.ScoreOutcomeRecorded},
		{"the taps arrive after a decision that sent no basis", []string{start, noBasis, loser, winner}, domain.ScoreOutcomeRecorded, domain.ScoreOutcomeRecorded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := ReplayScoring(&match, review5Lines(t, tc.lines...), shape)
			require.Equal(t, tc.wantLoser, review5Outcome(r, "lose"))
			require.Equal(t, tc.wantWinner, review5Outcome(r, "win"))
			require.Equal(t, []string{"M"}, r.Match.IpponsB, "the loser keeps the point it struck")
			require.Equal(t, []string{"○", "○"}, r.Match.IpponsA)
			_, listed := r.Match.TurnedAway["win"]
			require.Equal(t, tc.wantWinner == domain.ScoreOutcomeBeforeBaseline, listed, "turnedAway lists the winner's tap exactly when it was turned away")
		})
	}
}
