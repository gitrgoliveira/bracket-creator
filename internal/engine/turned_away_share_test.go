package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/require"
)

// turnedAwayLines logs n turned-away taps for each session, in that order.
func turnedAwayLines(sessions []string, n []int) ([]state.ScoreLogLine, map[int]string) {
	var lines []state.ScoreLogLine
	outcome := map[int]string{}
	for s, session := range sessions {
		for k := range n[s] {
			lines = append(lines, state.ScoreLogLine{Event: &domain.ScoreEvent{ID: fmt.Sprintf("%s-%d", session, k), Session: session, Seq: int64(k + 1)}})
			outcome[len(lines)-1] = domain.ScoreOutcomeBeforeBaseline
		}
	}
	return lines, outcome
}

func countBySession(away map[string]string, sessions []string) map[string]int {
	out := map[string]int{}
	for id := range away {
		for _, s := range sessions {
			if len(id) > len(s) && id[:len(s)+1] == s+"-" {
				out[s]++
			}
		}
	}
	return out
}

// Every device session keeps its share of the match's turned-away list
// (stage 6 review 7: four other sessions' later taps pushed a fifth device's
// ten out entirely): a session holding no more than MaxTurnedAway divided by
// the sessions never loses one, and the list stays within MaxTurnedAway.
func TestTurnedAway_EverySessionKeepsItsShare(t *testing.T) {
	t.Run("a device's ten, then four devices' full lists", func(t *testing.T) {
		sessions := []string{"own", "o1", "o2", "o3", "o4"}
		lines, outcome := turnedAwayLines(sessions, []int{10, 128, 128, 128, 128})
		got := turnedAwayOf(lines, outcome)
		require.LessOrEqual(t, len(got), state.MaxTurnedAway)
		by := countBySession(got, sessions)
		require.Equal(t, 10, by["own"], "the device's own are all kept")
		for _, s := range sessions[1:] {
			require.GreaterOrEqual(t, by[s], state.MaxTurnedAway/len(sessions))
		}
		require.Contains(t, got, "o1-127", "each keeps its latest")
	})
	t.Run("many sessions", func(t *testing.T) {
		var sessions []string
		var n []int
		for i := range 40 {
			sessions = append(sessions, fmt.Sprintf("s%02d", i))
			n = append(n, 40)
		}
		lines, outcome := turnedAwayLines(sessions, n)
		got := turnedAwayOf(lines, outcome)
		require.LessOrEqual(t, len(got), state.MaxTurnedAway)
		for s, c := range countBySession(got, sessions) {
			require.GreaterOrEqualf(t, c, state.MaxTurnedAway/len(sessions), "session %s", s)
		}
		require.Len(t, countBySession(got, sessions), len(sessions), "no session is pushed out")
	})
	t.Run("under the bound, all of them", func(t *testing.T) {
		sessions := []string{"a", "b"}
		lines, outcome := turnedAwayLines(sessions, []int{3, 200})
		got := turnedAwayOf(lines, outcome)
		by := countBySession(got, sessions)
		require.Equal(t, 3, by["a"])
		require.Equal(t, state.MaxTurnedAwayPerSession, by["b"])
	})
}

// More sessions than the list holds, one tap each (stage 6 review 8, m6): the
// share of each is less than one, and the list came out empty. It keeps the
// newest MaxTurnedAway instead, never fewer than it can hold.
func TestTurnedAway_MoreSessionsThanTheListHoldsKeepTheNewest(t *testing.T) {
	var sessions []string
	var n []int
	for i := range state.MaxTurnedAway + 88 {
		sessions = append(sessions, fmt.Sprintf("d%04d", i))
		n = append(n, 1)
	}
	lines, outcome := turnedAwayLines(sessions, n)
	got := turnedAwayOf(lines, outcome)
	require.Len(t, got, state.MaxTurnedAway)
	require.Contains(t, got, fmt.Sprintf("d%04d-0", state.MaxTurnedAway+87), "the newest kept")
	require.NotContains(t, got, "d0000-0", "the oldest goes")
	// Shares that leave room: the list is filled with the newest of the rest.
	sessions, n = []string{"big"}, []int{state.MaxTurnedAwayPerSession}
	for i := range state.MaxTurnedAway {
		sessions = append(sessions, fmt.Sprintf("s%04d", i))
		n = append(n, 1)
	}
	lines, outcome = turnedAwayLines(sessions, n)
	got = turnedAwayOf(lines, outcome)
	require.Len(t, got, state.MaxTurnedAway)
}
