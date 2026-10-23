package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func seqLine(session string, seq int64) state.ScoreLogLine {
	return state.ScoreLogLine{Event: &domain.ScoreEvent{ID: fmt.Sprintf("%s-%d", session, seq), Session: session, Seq: seq}}
}

// The highest logged seq per session; an event with no session or seq, and a
// baseline, say nothing.
func TestScoreLogEventSeqs_HighestPerSession(t *testing.T) {
	assert.Nil(t, ScoreLogEventSeqs(nil))
	lines := []state.ScoreLogLine{
		{Baseline: &state.ScoreBaseline{ID: "m", Door: "migrate"}},
		seqLine("a", 2), seqLine("a", 1), seqLine("b", 5), seqLine("", 9), seqLine("c", 0),
	}
	assert.Equal(t, map[string]int64{"a": 2, "b": 5}, ScoreLogEventSeqs(lines))
}

// Bounded: past MaxEventSeqSessions, the sessions logged latest are kept.
func TestScoreLogEventSeqs_KeepsTheLatestSessions(t *testing.T) {
	var lines []state.ScoreLogLine
	n := state.MaxEventSeqSessions + 3
	for i := 0; i < n; i++ {
		lines = append(lines, seqLine(fmt.Sprintf("s%02d", i), 1))
	}
	// s00 taps again last, so it is among the latest.
	lines = append(lines, seqLine("s00", 2))
	got := ScoreLogEventSeqs(lines)
	require.Len(t, got, state.MaxEventSeqSessions)
	assert.Equal(t, int64(2), got["s00"])
	for _, gone := range []string{"s01", "s02", "s03"} {
		assert.NotContains(t, got, gone)
	}
	assert.Contains(t, got, fmt.Sprintf("s%02d", n-1))
}

// The highest bout an event may name is the last bout a kachinuki encounter
// between the largest teams the app plans for can take.
func TestMaxScoreEventPos_IsTheLastKachinukiBoutOfTheLargestTeam(t *testing.T) {
	assert.Equal(t, domain.KachinukiMaxBouts(MaxTeamSize), domain.MaxScoreEventPos)
}

// The replay's derived write keeps its sessions, never lowering a stored one;
// with none it keeps the stored.
func TestMergeEventSeqs(t *testing.T) {
	assert.Equal(t, map[string]int64{"a": 3}, mergeEventSeqs(map[string]int64{"a": 3}, nil))
	assert.Equal(t, map[string]int64{"a": 4, "b": 1},
		mergeEventSeqs(map[string]int64{"a": 4, "z": 9}, map[string]int64{"a": 2, "b": 1}))
}
