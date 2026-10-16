package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The knockout result lock (bc-evts stage 2b, operator decision K1, 2026-10-16).
// Under I1 the first recorded result (by stamp) stands, so an older finish
// arriving late displaces a newer one. Once the NEXT knockout match has been
// played or started, though, the stored result is LOCKED: an older write that
// did not see it is HELD, kept in the match history with its values, and
// answered superseded with the downstream reason. Before that step I1 applies
// as it always did. The matrix models writes only, so "the next match was
// played / started" is a step of the sequence here.

// lkNext plays ("played") or starts ("running") the match the first round feeds.
func lkNext(t *testing.T, mode string) pmStep {
	return pmStep{role: "next", run: func(h mmHome) string {
		b, err := h.store.LoadBracket(h.compID)
		require.NoError(t, err)
		n := &b.Rounds[1][0]
		if mode == "played" {
			n.Status, n.Winner, n.WinnerID = state.MatchStatusCompleted, n.SideB, n.SideBID
			n.IpponsB = []string{"M"}
		} else {
			n.Status = state.MatchStatusRunning
		}
		require.NoError(t, h.store.SaveBracket(h.compID, b))
		return mode
	}}
}

func lkOrders(first, second, next int) [][]int {
	// every arrival order in which the next match can only follow a result
	return [][]int{{first, second, next}, {second, first, next}, {first, next, second}, {second, next, first}}
}

func TestKnockoutResultLock(t *testing.T) {
	M := []string{"M"}
	const (
		played  = "superseded:downstream_knockout_played"
		running = "superseded:downstream_knockout_running"
	)
	type tc struct {
		name  string
		mode  string
		steps func() []pmStep // 0 = the older write, 1 = the newer, 2 = next match
	}
	cases := []tc{
		{"kiken then older finish, next played", "played", func() []pmStep {
			return []pmStep{pmFin(mmT1, wrTeamA, M, nil), pmDec(mmT2, "kiken-voluntary", "aka"), lkNext(t, "played")}
		}},
		{"kiken then older finish, next started", "running", func() []pmStep {
			return []pmStep{pmFin(mmT1, wrTeamA, M, nil), pmDec(mmT2, "kiken-voluntary", "aka"), lkNext(t, "running")}
		}},
		{"finish then older finish of the other side, next played", "played", func() []pmStep {
			return []pmStep{pmFin(mmT1, wrTeamA, M, nil), pmFin(mmT2, wrTeamB, nil, M), lkNext(t, "played")}
		}},
	}
	for _, c := range cases {
		for _, order := range lkOrders(0, 1, 2) {
			h := pmHome(t, pmCase{}, true)
			steps := c.steps()
			var answers []string
			var nextAt = -1
			var storedBeforeOlder, olderAt = state.MatchResult{}, -1
			for i, s := range order {
				if s == 2 {
					nextAt = i
				}
				if s == 0 && nextAt >= 0 && i > nextAt {
					storedBeforeOlder, olderAt = h.load(t), i
				}
				answers = append(answers, steps[s].run(h))
			}
			label := c.name + " order=" + fmtOrder(order)
			if olderAt >= 0 {
				// The older write arrived after the next match: it is HELD.
				want := played
				if c.mode == "running" {
					want = running
				}
				assert.Equal(t, want, answers[olderAt], label)
				got := h.load(t)
				assert.Equal(t, storedBeforeOlder.Winner, got.Winner, label)
				assert.Equal(t, storedBeforeOlder.Decision, got.Decision, label)
				var held *state.MatchHistoryEntry
				for _, e := range h.history(t) {
					e := e
					if e.Stamp == mmT1 && e.Outcomes[state.GroupResult] == state.HistoryOutcomeHeld {
						held = &e
					}
				}
				require.NotNil(t, held, label+": the held finish is kept in the history")
				assert.NotEmpty(t, held.Reason, label)
				assert.Contains(t, held.Held, state.GroupResult, label)
				continue
			}
			// Before the next match was played I1 applies: the older stamp stands.
			for i, s := range order {
				if s == 2 {
					continue
				}
				assert.NotContains(t, answers[i], "downstream_knockout", label)
			}
			assert.Equal(t, wrTeamA, h.load(t).Winner, label)
		}
	}
}

func fmtOrder(o []int) string {
	s := ""
	for _, i := range o {
		s += string(rune('0' + i))
	}
	return s
}
