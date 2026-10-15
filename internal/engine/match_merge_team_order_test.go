package engine

import (
	"fmt"
	"sort"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// toState renders what a team match holds after a write sequence: status,
// winner, decision, bouts and every group stamp (in minutes past mmT0).
func toState(h mmHome, t *testing.T) string {
	t.Helper()
	g := h.load(t)
	ks := []string{}
	for k := range g.GroupStamps {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	st := ""
	for _, k := range ks {
		st += fmt.Sprintf("%s@%d ", k, (g.GroupStamps[k]-mmT0)/60000)
	}
	subs := ""
	for _, s := range g.SubResults {
		subs += fmt.Sprintf("[%d %v-%v w=%q]", s.Position, s.IpponsA, s.IpponsB, s.Winner)
	}
	return fmt.Sprintf("st=%s w=%q d=%q by=%q subs=%s stamps=%s", g.Status, g.Winner, g.Decision, g.DecisionBy, subs, st)
}

type toStep func(h mmHome) error

func toBout(at int64, pos int, a, b []string, win string) toStep {
	return func(h mmHome) error {
		w := mmRunning(h, at, state.BoutGroup(pos))
		w.SubResults = []state.SubMatchResult{{Position: pos, SideA: wrTeamA, SideB: wrTeamB, IpponsA: a, IpponsB: b, Winner: win}}
		return h.write(w)
	}
}

func toFinish(at int64, win string, subs ...state.SubMatchResult) toStep {
	return func(h mmHome) error {
		ch := []string{state.GroupResult}
		for _, s := range subs {
			ch = append(ch, state.BoutGroup(s.Position))
		}
		w := mmRunning(h, at, ch...)
		w.Status, w.Winner, w.SubResults = state.MatchStatusCompleted, win, subs
		return h.write(w)
	}
}

func toDecision(at int64, d, by string) toStep {
	return func(h mmHome) error {
		_, _, e := h.eng.RecordDecision(h.compID, h.matchID, d, by, "", nil, at)
		return e
	}
}

// A team match two writes reach in either arrival order ends the same:
// the older write is judged against the newer scoring the other way round
// (S2/R4), whether the newer scoring is a stored bout baseline or a live
// line.
func TestTeamMatchWriteOrderConverges(t *testing.T) {
	M, K := []string{"M"}, []string{"K"}
	b := func(pos int, a, bb []string, w string) state.SubMatchResult {
		return state.SubMatchResult{Position: pos, SideA: wrTeamA, SideB: wrTeamB, IpponsA: a, IpponsB: bb, Winner: w}
	}
	cases := []struct {
		name string
		pre  func(h mmHome) error
		x, y toStep
	}{
		{"finish Ryu with bout 1 T1, bout 2 Tora T2", nil, toFinish(mmT1, wrTeamA, b(1, M, nil, wrTeamA)), toBout(mmT2, 2, nil, M, wrTeamB)},
		{"finish Ryu with bout 1 T1, bout 1 flipped T2", nil, toFinish(mmT1, wrTeamA, b(1, M, nil, wrTeamA)), toBout(mmT2, 1, nil, K, wrTeamB)},
		// A finish naming its verdict alone over newer stored scoring is not in
		// this table on purpose: S2 holds it (needs_winner), nothing can replay it.
		{"daihyosen aka T1, bout 1 A T2", nil, toDecision(mmT1, "daihyosen", "aka"), toBout(mmT2, 1, M, nil, wrTeamA)},
	}
	for _, c := range cases {
		for _, ko := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/ko=%v", c.name, ko), func(t *testing.T) {
				run := func(first, second toStep) string {
					h := mmTeam(t, ko)
					if c.pre != nil {
						if e := c.pre(h); e != nil {
							t.Fatalf("pre: %v", e)
						}
					}
					_ = first(h)
					_ = second(h)
					return toState(h, t)
				}
				xy, yx := run(c.x, c.y), run(c.y, c.x)
				if xy != yx {
					t.Errorf("arrival order changes the match\n  x then y: %s\n  y then x: %s", xy, yx)
				}
			})
		}
	}
}
