package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// boutOneOp is one change of the "copy made, removed, started" residual: a
// save of a lineup's position, a removal of a match's own lineup, or the
// match's start, each stamped at `at` (relative to the env's base).
type boutOneOp struct {
	kind  string // save, remove, start
	match string // "" is the team's starting lineup
	pos   int
	name  string
	at    int64
}

func (o boutOneOp) String() string {
	return fmt.Sprintf("%s:%s:%d=%s@%d", o.kind, o.match, o.pos, o.name, o.at)
}

// playBoutOne lands ops in the given arrival order on a fresh competition. Bout 1 is
// made the moment the start lands, by a tap on it (a start from the queue
// holds no bout row, and the score-events door seats bout 1's fighters in the
// batch that makes it), so every later arrival reseats a bout 1 that exists.
// It answers the lineups as stored and bout 1's Shiro fighter.
func playBoutOne(t *testing.T, ops []boutOneOp, order []int) (string, string) {
	t.Helper()
	e := newCarryEnv(t, true)
	for _, i := range order {
		o := ops[i]
		switch o.kind {
		case "start":
			e.start(o.match, "Kuma", o.at)
			ev := domain.ScoreEvent{ID: "tap-" + o.match, Session: "dev1", Seq: 1, Stamp: e.base + o.at + 1,
				Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "M"}
			rec := e.do(http.MethodPost, "/api/competitions/"+e.comp+"/matches/"+url.PathEscape(o.match)+"/score-events",
				map[string]any{"session": "dev1", "events": []domain.ScoreEvent{ev}})
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			var out struct {
				Outcomes []scoreEventOutcome `json:"outcomes"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out), rec.Body.String())
			require.Len(t, out.Outcomes, 1)
			require.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome, "the tap makes bout 1")
		case "remove":
			e.removeAt(o.match, o.at)
		default:
			e.save(o.match, o.pos, o.name, o.at)
		}
	}
	return e.dump(), e.boutOne("P1-0")
}

// boutOneOrders is every arrival order of n ops.
func boutOneOrders(n int) [][]int {
	xs := make([]int, n)
	for i := range xs {
		xs[i] = i
	}
	var out [][]int
	var rec func(k int)
	rec = func(k int) {
		if k == n {
			out = append(out, slices.Clone(xs))
			return
		}
		for i := k; i < n; i++ {
			xs[k], xs[i] = xs[i], xs[k]
			rec(k + 1)
			xs[k], xs[i] = xs[i], xs[k]
		}
	}
	rec(0)
	return out
}

// The residual "copy made, removed, started" of stage 6 review 8, driven
// through a start that MAKES bout 1 (stage 6 review 9, gate case): a save of
// P1-0's own lineup, that lineup taken away, the match started, and a save of
// the starting lineup before the start and/or of P1-0's own lineup after it.
// In every arrival order bout 1's Shiro fighter, and every lineup as stored,
// is the stamp-order result: the lineup the start kept is the carry rule at
// the start, by stamp, and the removal made before the start takes P1-0's
// own save away from it.
func TestKachinukiBoutOne_ACopyRemovedThenStartedIsTheSameInEveryArrivalOrder(t *testing.T) {
	cases := []struct {
		name  string
		ops   []boutOneOp
		shiro string
	}{
		{"own save after the start", []boutOneOp{
			{kind: "save", match: "P1-0", pos: 1, name: "Doi", at: 10},
			{kind: "remove", match: "P1-0", at: 20},
			{kind: "start", match: "P1-0", at: 30},
			{kind: "save", match: "P1-0", pos: 2, name: "Endo", at: 40},
		}, "Sato"},
		{"starting save before the start", []boutOneOp{
			{kind: "save", match: "P1-0", pos: 1, name: "Doi", at: 10},
			{kind: "remove", match: "P1-0", at: 20},
			{kind: "start", match: "P1-0", at: 30},
			{kind: "save", match: "", pos: 1, name: "Mori", at: 25},
		}, "Mori"},
		{"both", []boutOneOp{
			{kind: "save", match: "P1-0", pos: 1, name: "Doi", at: 10},
			{kind: "remove", match: "P1-0", at: 20},
			{kind: "start", match: "P1-0", at: 30},
			{kind: "save", match: "", pos: 1, name: "Mori", at: 25},
			{kind: "save", match: "P1-0", pos: 2, name: "Endo", at: 40},
		}, "Mori"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			byStamp := make([]int, len(tc.ops))
			for i := range byStamp {
				byStamp[i] = i
			}
			slices.SortFunc(byStamp, func(a, b int) int { return int(tc.ops[a].at - tc.ops[b].at) })
			wantDump, wantShiro := playBoutOne(t, tc.ops, byStamp)
			require.Equal(t, tc.shiro, wantShiro, "stamp order: bout 1 holds the lineup the start kept (%s)", wantDump)
			fails := 0
			for _, perm := range boutOneOrders(len(tc.ops)) {
				dump, shiro := playBoutOne(t, tc.ops, perm)
				if dump == wantDump && shiro == wantShiro {
					continue
				}
				fails++
				if fails <= 3 {
					var names []string
					for _, i := range perm {
						names = append(names, tc.ops[i].String())
					}
					t.Errorf("order %v:\n got  %s bout1=%s\n want %s bout1=%s", names, dump, shiro, wantDump, wantShiro)
				}
			}
			if fails > 0 {
				t.Errorf("%d of %d arrival orders differ from the stamp order", fails, len(boutOneOrders(len(tc.ops))))
			}
		})
	}
}
