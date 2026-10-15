package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// --- test builders -----------------------------------------------------

func evLine(id string, stamp int64, kind domain.ScoreEventKind, pos int, side, letter string) state.ScoreLogLine {
	return state.ScoreLogLine{V: state.ScoreLogFormatVersion, Event: &domain.ScoreEvent{
		ID: id, Session: "s-" + id, Seq: 1, Stamp: stamp, Kind: kind, Pos: pos, Side: side, Letter: letter,
	}}
}

func removeLine(id string, stamp int64, target string) state.ScoreLogLine {
	return state.ScoreLogLine{V: state.ScoreLogFormatVersion, Event: &domain.ScoreEvent{
		ID: id, Session: "s-" + id, Seq: 1, Stamp: stamp, Kind: domain.ScoreIpponRemove, Target: target,
	}}
}

// baseLine is a baseline setting groups of m at stamp, built as a door
// builds one (GroupValue per group).
func baseLine(m *state.MatchResult, id string, stamp int64, basis state.ScoreBasis, groups ...string) state.ScoreLogLine {
	b := BaselineFromMatch(m, id, "test", stamp, groups, basis)
	return state.ScoreLogLine{V: state.ScoreLogFormatVersion, Baseline: &b}
}

func outcomeOf(r Replay, id string) string {
	for _, o := range r.Outcomes {
		if o.ID == id {
			return o.Outcome
		}
	}
	return ""
}

func knockoutIndividual() *state.MatchResult {
	return &state.MatchResult{
		ID: "r0-m0", SideA: "Aka", SideB: "Shiro", SideAID: "pa", SideBID: "pb",
		Status: state.MatchStatusRunning,
	}
}

// permutations returns every order of n items, as index slices.
func permutations(n int) [][]int {
	var out [][]int
	var rec func(cur []int, used []bool)
	rec = func(cur []int, used []bool) {
		if len(cur) == n {
			out = append(out, append([]int(nil), cur...))
			return
		}
		for i := 0; i < n; i++ {
			if !used[i] {
				used[i] = true
				rec(append(cur, i), used)
				used[i] = false
			}
		}
	}
	rec(nil, make([]bool, n))
	return out
}

// matchState is what "identical match" means for the order tests.
type matchState struct {
	Status   state.MatchStatus
	Winner   string
	WinnerID string
	Decision string
	IpponsA  []string
	IpponsB  []string
	Stamps   map[string]int64
	MarkIDs  *state.MarkIDs
	Subs     []state.SubMatchResult
	FlagsA   int
	FlagsB   int
}

func stateOf(m state.MatchResult) matchState {
	return matchState{m.Status, m.Winner, m.WinnerID, m.Decision, m.IpponsA, m.IpponsB, m.GroupStamps, m.MarkIDs, m.SubResults, m.FlagsA, m.FlagsB}
}

// --- the headline -------------------------------------------------------

// The headline (bc-evts): a finished knockout match, e1 a point that ties it
// (made after the finish), e2 a point that gives it its winner again. In every
// order the three can arrive, the match comes out the same, and e1 and e2 are
// both in it. Without e2, e1 is held as needing a winner in every order.
func TestReplayScoring_HeadlineEveryArrivalOrder(t *testing.T) {
	shape := ReplayShape{Knockout: true}
	start := knockoutIndividual()
	start.IpponsA = []string{"M"}
	start.ModifiedAt = 50
	m0 := MigrationBaseline(start, "m0", 0)

	finished := knockoutIndividual()
	finished.IpponsA = []string{"M"}
	finished.Status, finished.Winner, finished.WinnerID = state.MatchStatusCompleted, "Aka", "pa"
	fin := baseLine(finished, "f", 100, state.ScoreBasis{Seen: 1}, state.GroupPoints, state.GroupResult, state.GroupEncho)
	e1 := evLine("e1", 101, domain.ScoreIpponAdd, 0, "b", "K")
	e2 := evLine("e2", 102, domain.ScoreIpponAdd, 0, "a", "D")

	// THE CLOSED-MATCH RULE: the finish closes the match in stamp order, so a
	// tap stamped after it is turned away, whichever order the three reach
	// the server in.
	t.Run("taps after the finish are turned away in every order", func(t *testing.T) {
		tail := []state.ScoreLogLine{fin, e1, e2}
		var first *matchState
		for _, order := range permutations(len(tail)) {
			lines := []state.ScoreLogLine{{V: 1, Baseline: &m0}}
			for _, i := range order {
				lines = append(lines, tail[i])
			}
			r := ReplayScoring(start, lines, shape)
			got := stateOf(r.Match)
			assert.Equal(t, state.MatchStatusCompleted, got.Status, "order %v", order)
			assert.Equal(t, "Aka", got.Winner, "order %v", order)
			assert.Equal(t, "pa", got.WinnerID, "order %v", order)
			assert.Equal(t, []string{"M"}, got.IpponsA, "order %v", order)
			assert.Empty(t, got.IpponsB, "order %v", order)
			require.NotNil(t, got.MarkIDs, "order %v", order)
			assert.Equal(t, []string{"b:m0:0:a:0"}, got.MarkIDs.A, "order %v", order)
			assert.Equal(t, int64(100), got.Stamps[state.GroupPoints], "order %v", order)
			assert.Equal(t, int64(100), got.Stamps[state.GroupResult], "order %v", order)
			assert.Equal(t, domain.ScoreOutcomeMatchClosed, outcomeOf(r, "e1"), "order %v", order)
			assert.Equal(t, domain.ScoreOutcomeMatchClosed, outcomeOf(r, "e2"), "order %v", order)
			assert.Equal(t, len(lines), r.Match.ScoreVersion, "order %v", order)
			if first == nil {
				first = &got
			} else {
				assert.Equal(t, *first, got, "order %v", order)
			}
		}
	})

	t.Run("a tap stamped before the finish and outside its basis is held out of it", func(t *testing.T) {
		early := evLine("early", 90, domain.ScoreIpponAdd, 0, "b", "K")
		tail := []state.ScoreLogLine{early, fin}
		for _, order := range permutations(len(tail)) {
			lines := []state.ScoreLogLine{{V: 1, Baseline: &m0}}
			for _, i := range order {
				lines = append(lines, tail[i])
			}
			r := ReplayScoring(start, lines, shape)
			assert.Equal(t, domain.ScoreOutcomeBeforeBaseline, outcomeOf(r, "early"), "order %v", order)
			assert.Empty(t, r.Match.IpponsB, "order %v", order)
			assert.Equal(t, "Aka", r.Match.Winner, "order %v", order)
		}
	})
}
