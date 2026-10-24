package engine

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// startLineOf names the write that put the match under way NOW: a match
// started, sent back to the queue and started again is known by its second
// start, so a resend of that start (its answer lost) is judged against its own
// line and not against the first start's (stage 6 review 13, m2).
func TestStartLineOf_IsTheLastLineOutOfScheduled(t *testing.T) {
	line := func(id string, status state.MatchStatus) state.ScoreLogLine {
		raw := state.GroupValue(&state.MatchResult{Status: status}, state.GroupResult)
		return state.ScoreLogLine{Baseline: &state.ScoreBaseline{
			ID: id, Door: "score", Values: map[string]json.RawMessage{state.GroupResult: raw},
		}}
	}
	lines := []state.ScoreLogLine{
		line("start-1", state.MatchStatusRunning),
		line("back-1", state.MatchStatusScheduled),
		line("start-2", state.MatchStatusRunning),
		line("finish-2", state.MatchStatusCompleted),
	}
	b, status, ok := startLineOf(lines)
	require.True(t, ok)
	require.Equal(t, "start-2", b.ID, "the start now in force, not the first one")
	require.Equal(t, state.MatchStatusRunning, status)

	// A match started once keeps its one start through later running and
	// finishing lines.
	once := []state.ScoreLogLine{
		line("start-1", state.MatchStatusRunning),
		line("running-again", state.MatchStatusRunning),
		line("finish", state.MatchStatusCompleted),
	}
	b, _, ok = startLineOf(once)
	require.True(t, ok)
	require.Equal(t, "start-1", b.ID)

	// Sent back and not started again: the match has no start under way, but
	// the one it had is still the line a resend of it is known by.
	back := []state.ScoreLogLine{
		line("start-1", state.MatchStatusRunning),
		line("back-1", state.MatchStatusScheduled),
	}
	b, _, ok = startLineOf(back)
	require.True(t, ok)
	require.Equal(t, "start-1", b.ID)
}
