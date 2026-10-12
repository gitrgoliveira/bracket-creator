package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bc-xrpl: ExactScoreReplay tells the resend of a recorded finish (its
// answer lost) from a correction made under the same stamp.
func TestExactScoreReplay(t *testing.T) {
	const at = 1_700_000_000_000
	stored := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "Pool A-0", SideA: "Alice", SideAID: "a", SideB: "Bob", SideBID: "b",
			Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: "a",
			IpponsA: []string{"M", "K"}, IpponsB: []string{}, ModifiedAt: at,
		}
	}
	resend := func() *state.MatchResult {
		return &state.MatchResult{
			SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice",
			IpponsA: []string{"M", "K"}, IpponsB: []string{}, ModifiedAt: at,
		}
	}
	for _, tc := range []struct {
		name   string
		change func(s, w *state.MatchResult)
		want   bool
	}{
		{name: "the same write, ids filled in from the stored sides", change: func(_, _ *state.MatchResult) {}, want: true},
		{name: "naming only groups that are equal", change: func(_, w *state.MatchResult) { w.Changed = []string{state.GroupPoints, state.GroupResult} }, want: true},
		{name: "a stored correction reason the resend omits", change: func(s, _ *state.MatchResult) { s.CorrectionReason = "typo" }, want: true},
		{name: "another scoreline", change: func(_, w *state.MatchResult) { w.IpponsA = []string{"M", "D"} }},
		{name: "another winner", change: func(_, w *state.MatchResult) { w.Winner = "Bob" }},
		{name: "an older stamp", change: func(_, w *state.MatchResult) { w.ModifiedAt = at - 1 }},
		{name: "a newer stamp", change: func(_, w *state.MatchResult) { w.ModifiedAt = at + 1 }},
		{name: "no stamp", change: func(_, w *state.MatchResult) { w.ModifiedAt = 0 }},
		{name: "a running write", change: func(_, w *state.MatchResult) { w.Status = state.MatchStatusRunning }},
		{name: "a match not finished", change: func(s, _ *state.MatchResult) { s.Status = state.MatchStatusRunning }},
		{name: "another competitor", change: func(_, w *state.MatchResult) { w.SideB = "Carol" }},
		{name: "a winner id naming neither side", change: func(_, w *state.MatchResult) { w.WinnerID = "z" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, w := stored(), resend()
			tc.change(s, w)
			before := *w
			assert.Equal(t, tc.want, ExactScoreReplay(s, w, true))
			assert.Equal(t, before, *w, "the write is judged on a copy")
		})
	}
	assert.False(t, ExactScoreReplay(nil, resend(), true))
}
