package engine

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The permutation matrix (bc-evts stage 2b, review round 3). The core promise
// of the merge is that ARRIVAL ORDER NEVER CHANGES THE OUTCOME: the same set of
// stamped writes, delivered in any order, leaves the same status, winner,
// decision, scoreline and group stamps, and the stored match equals a replay of
// its own log. Three review rounds fixed one pinned order at a time and each
// then failed a larger matrix, so this table generates EVERY arrival order of
// each case's write set (individual and team, pool and knockout) and compares
// them all. A case the OLD tree (9b8a0a5f) also left order-dependent is tagged
// byDesign with the rule that makes it so; it is still run and logged, never
// omitted.

func pmState(h mmHome, t *testing.T) string {
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
		subs += fmt.Sprintf("[%d %v-%v w=%q d=%q]", s.Position, s.IpponsA, s.IpponsB, s.Winner, s.Decision)
	}
	return fmt.Sprintf("st=%s w=%q d=%q by=%q A=%v B=%v subs=%s stamps=%s", g.Status, g.Winner, g.Decision, g.DecisionBy, g.IpponsA, g.IpponsB, subs, st)
}

// pmReplay reports how the stored match differs from a replay of its log.
func pmReplay(h mmHome, t *testing.T) string {
	lines, err := h.store.LoadScoreLog(h.compID, h.matchID)
	if err != nil {
		return "logerr " + err.Error()
	}
	m, inPool, err := h.eng.lookupExistingResultIn(h.store, h.compID, h.matchID)
	if err != nil {
		return "lookuperr " + err.Error()
	}
	comp, _ := h.store.LoadCompetition(h.compID)
	rp := ReplayScoring(m, lines, ReplayShape{Comp: comp, Knockout: !inPool})
	out := ""
	if d := replayGroupsDiffer(m, &rp.Match); len(d) > 0 {
		out += fmt.Sprintf("REPLAYDIFF=%v", d)
	}
	for _, l := range lines {
		if l.Baseline != nil && l.Baseline.Door == DoorDrift {
			out += " DRIFT"
		}
	}
	return out
}

type pmStep func(h mmHome) error

func pmPt(at int64, a, b []string) pmStep {
	return func(h mmHome) error {
		w := mmRunning(h, at, state.GroupPoints)
		w.IpponsA, w.IpponsB = a, b
		return h.write(w)
	}
}

func pmFin(at int64, win string, a, b []string) pmStep {
	return func(h mmHome) error {
		w := mmRunning(h, at, state.GroupPoints, state.GroupResult)
		w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, win, a, b
		return h.write(w)
	}
}

// pmBareInd is a finish whose payload carries the points but names result alone.
func pmBareInd(at int64, win string, a, b []string) pmStep {
	return func(h mmHome) error {
		w := mmRunning(h, at, state.GroupResult)
		w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, win, a, b
		return h.write(w)
	}
}

// pmBareTeam is a team finish whose payload carries bouts but names result alone.
func pmBareTeam(at int64, win string, subs ...state.SubMatchResult) pmStep {
	return func(h mmHome) error {
		w := mmRunning(h, at, state.GroupResult)
		w.Status, w.Winner, w.SubResults = state.MatchStatusCompleted, win, subs
		return h.write(w)
	}
}

func pmDec(at int64, d, by string) pmStep {
	return func(h mmHome) error {
		_, _, e := h.eng.RecordDecision(h.compID, h.matchID, d, by, "", nil, at)
		return e
	}
}

func pmHik(at int64) pmStep {
	return func(h mmHome) error {
		return h.write(&state.MatchResult{ID: h.matchID, SideA: wrTeamA, SideB: wrTeamB, SideAID: wrTeamAID, SideBID: wrTeamBID,
			Status: state.MatchStatusCompleted, Decision: "hikiwake", ModifiedAt: at,
			Changed: []string{state.GroupResult}, WriteDoor: DoorDecision})
	}
}

func pmBout(at int64, pos int, a, b []string, win string) pmStep {
	return func(h mmHome) error {
		w := mmRunning(h, at, state.BoutGroup(pos))
		w.SubResults = []state.SubMatchResult{{Position: pos, SideA: wrTeamA, SideB: wrTeamB, IpponsA: a, IpponsB: b, Winner: win}}
		return h.write(w)
	}
}

func pmSeq(steps ...pmStep) pmStep {
	return func(h mmHome) error {
		for _, s := range steps {
			if e := s(h); e != nil {
				return e
			}
		}
		return nil
	}
}

// pmTeamFin finishes a team match, naming the result and each bout it carries.
func pmTeamFin(at int64, win string, subs ...state.SubMatchResult) pmStep {
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

func pmPerms(n int) [][]int {
	if n == 1 {
		return [][]int{{0}}
	}
	var out [][]int
	for _, p := range pmPerms(n - 1) {
		for i := 0; i <= len(p); i++ {
			q := append([]int{}, p[:i]...)
			q = append(q, n-1)
			q = append(q, p[i:]...)
			out = append(out, q)
		}
	}
	return out
}

// The rules that make an arrival order matter on purpose. Each is one the OLD
// tree (9b8a0a5f) also followed: the order-dependent outcome is the rule, not a
// regression, so the case is run and logged but its convergence is not asserted.
const (
	r2Why           = "R2: a withdrawal older than stored points is held (withdrawalOutranked), a newer scoring change clears an older withdrawal only with the winner it gives; a fusensho stands whatever is scored"
	finishAtomicWhy = "finish atomicity (HoldReasonFinishAtomic): a result older than the stored one is held WHOLE with its scoreline, a newer one replaces it"
	s3Why           = "S3: a hantei mark is the result's; points that do not claim the result keep it while tied, so which Ht survives follows the order the marks arrive"
	r4DecisionWhy   = "R2/R4: a decision's closing points and a newer tying scoreline on a knockout match are ordered by what each leaves, not by the log alone"
)

type pmCase struct {
	name  string
	team  bool
	pre   pmStep
	steps []pmStep
	// byDesign, when set, names the rule that makes the arrival orders differ
	// (and so excuses the convergence check, never the replay check). where
	// limits it to "pool" or "ko"; empty means both.
	byDesign string
	where    string
}

func pmCases() []pmCase {
	M, K := []string{"M"}, []string{"K"}
	MK := []string{"M", "K"}
	b := func(pos int, a, bb []string, w string) state.SubMatchResult {
		return state.SubMatchResult{Position: pos, SideA: wrTeamA, SideB: wrTeamB, IpponsA: a, IpponsB: bb, Winner: w}
	}
	preInd := pmPt(mmT0+1000, M, nil)
	preTeam := pmSeq(pmBout(mmT0+1000, 1, M, nil, wrTeamA), pmBout(mmT0+2000, 2, nil, M, wrTeamB))
	preB1 := pmBout(mmT0+1000, 1, M, nil, wrTeamA)
	return []pmCase{
		// Item 1-4 of the round-3 review: bare finishes over newer scoring.
		{name: "ind bareFin Ryu(M-0) T1 / pt MK-0 T2 (still Ryu)", pre: preInd, steps: []pmStep{pmBareInd(mmT1, wrTeamA, M, nil), pmPt(mmT2, MK, nil)}},
		{name: "ind bareFin Ryu(M-0) T1 / pt M-MK T2 (Tora)", pre: preInd, steps: []pmStep{pmBareInd(mmT1, wrTeamA, M, nil), pmPt(mmT2, M, MK)}},
		{name: "ind bareFin Ryu(M-0) T1 / pt M-M T2 (tie)", pre: preInd, steps: []pmStep{pmBareInd(mmT1, wrTeamA, M, nil), pmPt(mmT2, M, M)}},
		{name: "team bareFin Ryu(payload bouts) T1 / b3 B T2", team: true, pre: preTeam, steps: []pmStep{pmBareTeam(mmT1, wrTeamA, b(1, M, nil, wrTeamA), b(2, nil, M, wrTeamB)), pmBout(mmT2, 3, nil, K, wrTeamB)}},
		{name: "team bareFin Ryu(no bouts) T1 / b3 B T2", team: true, pre: preTeam, steps: []pmStep{pmTeamFin(mmT1, wrTeamA), pmBout(mmT2, 3, nil, K, wrTeamB)}},
		{name: "team bareFin Ryu(no bouts) T1 / b3 A T2 (Ryu)", team: true, pre: preTeam, steps: []pmStep{pmTeamFin(mmT1, wrTeamA), pmBout(mmT2, 3, K, nil, wrTeamA)}},
		// The 3-write team sequences: a finish, a tying newer bout, a third write.
		{name: "team fin Ryu(b1A) T1, b2 B T2 (tie), b3 B T3 (Tora)", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(mmT1, wrTeamA, b(1, M, nil, wrTeamA)), pmBout(mmT2, 2, nil, M, wrTeamB), pmBout(mmT3, 3, nil, K, wrTeamB)}},
		{name: "team fin Ryu(b1A) T1, b2 B T2 (tie), b2 A T3 (Ryu)", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(mmT1, wrTeamA, b(1, M, nil, wrTeamA)), pmBout(mmT2, 2, nil, M, wrTeamB), pmBout(mmT3, 2, K, nil, wrTeamA)}},
		{name: "team fin Ryu(b1A) T1, b2 B T2 (tie), b3 empty T3", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(mmT1, wrTeamA, b(1, M, nil, wrTeamA)), pmBout(mmT2, 2, nil, M, wrTeamB), pmBout(mmT3, 3, nil, nil, "")}},
		{name: "team fin Ryu(b1A) T1, b2 B T2 (tie), b1 AK T3", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(mmT1, wrTeamA, b(1, M, nil, wrTeamA)), pmBout(mmT2, 2, nil, M, wrTeamB), pmBout(mmT3, 1, MK, nil, wrTeamA)}},
		{name: "team fin Ryu(b1A) T1, b2 0-M T2 (tie), b2 0-MK T3 (Tora by PW)", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(mmT1, wrTeamA, b(1, M, nil, wrTeamA)), pmBout(mmT2, 2, nil, M, wrTeamB), pmBout(mmT3, 2, nil, MK, wrTeamB)}},
		{name: "ind fin Ryu(M-0) T1, pt M-M T2 (tie), pt M-MK T3 (Tora)", pre: preInd,
			steps: []pmStep{pmFin(mmT1, wrTeamA, M, nil), pmPt(mmT2, M, M), pmPt(mmT3, M, MK)}},
		{name: "ind fin Ryu(M-0) T1, pt M-M T2 (tie), pt MK-M T3 (Ryu)", pre: preInd,
			steps: []pmStep{pmFin(mmT1, wrTeamA, M, nil), pmPt(mmT2, M, M), pmPt(mmT3, MK, M)}},
		{name: "team dai shiro T1 / b1 A T2 / b2 B T3", team: true,
			steps: []pmStep{pmDec(mmT1, "daihyosen", "shiro"), pmBout(mmT2, 1, M, nil, wrTeamA), pmBout(mmT3, 2, nil, M, wrTeamB)}},
		// Two-write pairs: decisions against scoring, finishes against decisions.
		{name: "dai aka T1 / pt M-0 T2", steps: []pmStep{pmDec(mmT1, "daihyosen", "aka"), pmPt(mmT2, M, nil)}},
		{name: "dai shiro T1 / pt M-0 T2", steps: []pmStep{pmDec(mmT1, "daihyosen", "shiro"), pmPt(mmT2, M, nil)}},
		{name: "dai aka T1 / pt M-K T2 (tie)", steps: []pmStep{pmDec(mmT1, "daihyosen", "aka"), pmPt(mmT2, M, K)}, byDesign: r4DecisionWhy, where: "ko"},
		{name: "dai aka T1 / pt 0-0 T2", steps: []pmStep{pmDec(mmT1, "daihyosen", "aka"), pmPt(mmT2, nil, nil)}},
		{name: "dai aka T1 / fin Ryu M-0 T2", steps: []pmStep{pmDec(mmT1, "daihyosen", "aka"), pmFin(mmT2, wrTeamA, M, nil)}, byDesign: finishAtomicWhy},
		{name: "fin Ryu M-0 T1 / dai aka T2", steps: []pmStep{pmFin(mmT1, wrTeamA, M, nil), pmDec(mmT2, "daihyosen", "aka")}, byDesign: finishAtomicWhy},
		{name: "pt M-0 T1 / dai aka T2", steps: []pmStep{pmPt(mmT1, M, nil), pmDec(mmT2, "daihyosen", "aka")}, byDesign: finishAtomicWhy},
		{name: "kiken aka T1 / pt 0-M T2", steps: []pmStep{pmDec(mmT1, "kiken-voluntary", "aka"), pmPt(mmT2, nil, M)}, byDesign: r2Why},
		{name: "kiken aka T1 / pt M-0 T2", steps: []pmStep{pmDec(mmT1, "kiken-voluntary", "aka"), pmPt(mmT2, M, nil)}, byDesign: r2Why},
		{name: "fusensho aka T1 / pt M-0 T2", steps: []pmStep{pmDec(mmT1, "fusensho", "aka"), pmPt(mmT2, M, nil)}, byDesign: r2Why},
		{name: "fin Ryu M-0 T1 / fusensho shiro T2", steps: []pmStep{pmFin(mmT1, wrTeamA, M, nil), pmDec(mmT2, "fusensho", "shiro")}},
		{name: "hik T1 / pt M-0 T2", steps: []pmStep{pmHik(mmT1), pmPt(mmT2, M, nil)}, where: "pool"},
		{name: "hik T1 / pt M-K T2", steps: []pmStep{pmHik(mmT1), pmPt(mmT2, M, K)}, where: "pool"},
		{name: "fin Ht-0 T1 / pt 0-0 T2", steps: []pmStep{pmFin(mmT1, wrTeamA, []string{"Ht"}, nil), pmPt(mmT2, nil, nil)}},
		{name: "fin Ht-0 T1 / pt 0-Ht T2", steps: []pmStep{pmFin(mmT1, wrTeamA, []string{"Ht"}, nil), pmPt(mmT2, nil, []string{"Ht"})}, byDesign: s3Why},
		{name: "pt 0-0 T1 / fin Ht T2", pre: pmPt(mmT0+1, M, nil), steps: []pmStep{pmPt(mmT1, nil, nil), pmFin(mmT2, wrTeamA, []string{"Ht"}, nil)}},
		{name: "team dai aka T1 / bout1 A M T2", team: true, steps: []pmStep{pmDec(mmT1, "daihyosen", "aka"), pmBout(mmT2, 1, M, nil, wrTeamA)}},
		{name: "team dai shiro T1 / bout1 B M T2", team: true, steps: []pmStep{pmDec(mmT1, "daihyosen", "shiro"), pmBout(mmT2, 1, nil, M, wrTeamB)}},
		{name: "team bouts favour B, dai shiro T1 / bout3 B T2", team: true, pre: preTeam, steps: []pmStep{pmDec(mmT1, "daihyosen", "shiro"), pmBout(mmT2, 3, nil, K, wrTeamB)}},
		{name: "team kiken aka T1 / bout1 B T2", team: true, steps: []pmStep{pmDec(mmT1, "kiken-voluntary", "aka"), pmBout(mmT2, 1, nil, M, wrTeamB)}, byDesign: r2Why},
		{name: "team kiken aka T1 / bout1 A T2", team: true, steps: []pmStep{pmDec(mmT1, "kiken-voluntary", "aka"), pmBout(mmT2, 1, M, nil, wrTeamA)}, byDesign: r2Why},
		{name: "team fin Ryu(b1 A) T1 / b2 B T2", team: true, steps: []pmStep{pmTeamFin(mmT1, wrTeamA, b(1, M, nil, wrTeamA)), pmBout(mmT2, 2, nil, M, wrTeamB)}},
		{name: "team fin Ryu(b1 A) T1 / b1 B T2 flip", team: true, steps: []pmStep{pmTeamFin(mmT1, wrTeamA, b(1, M, nil, wrTeamA)), pmBout(mmT2, 1, nil, K, wrTeamB)}},
		{name: "team pre b1A b2B; fin Ryu(dec only) T1 / b3 B T2", team: true, pre: preTeam, steps: []pmStep{pmTeamFin(mmT1, wrTeamA), pmBout(mmT2, 3, nil, K, wrTeamB)}},
		{name: "team fusensho aka T1 / b1 A T2", team: true, steps: []pmStep{pmDec(mmT1, "fusensho", "aka"), pmBout(mmT2, 1, M, nil, wrTeamA)}, byDesign: r2Why},
		// Two decisions.
		{name: "kiken aka T1 -> dai shiro T2", steps: []pmStep{pmDec(mmT1, "kiken-injury", "aka"), pmDec(mmT2, "daihyosen", "shiro")}},
		{name: "dai shiro T1 -> dai aka T2 (correction)", steps: []pmStep{pmDec(mmT1, "daihyosen", "shiro"), pmDec(mmT2, "daihyosen", "aka")}},
		{name: "team kiken aka T1 -> dai shiro T2", team: true, steps: []pmStep{pmDec(mmT1, "kiken-injury", "aka"), pmDec(mmT2, "daihyosen", "shiro")}, byDesign: r2Why},
	}
}

func TestMatchWritePermutationsConverge(t *testing.T) {
	cases, perms := 0, 0
	for _, c := range pmCases() {
		for _, ko := range []bool{false, true} {
			if (c.where == "pool" && ko) || (c.where == "ko" && !ko) {
				continue
			}
			c, ko := c, ko
			kind := "pool"
			if ko {
				kind = "ko"
			}
			cases++
			t.Run(fmt.Sprintf("%s/%s", kind, c.name), func(t *testing.T) {
				states := map[string][]string{}
				for _, p := range pmPerms(len(c.steps)) {
					var h mmHome
					if c.team {
						h = mmTeam(t, ko)
					} else {
						h = mmIndividual(t, ko)
					}
					if c.pre != nil {
						if e := c.pre(h); e != nil {
							t.Fatalf("pre: %v", e)
						}
					}
					errs := ""
					for _, i := range p {
						errs += fmt.Sprintf("%v;", c.steps[i](h))
					}
					perms++
					if r := pmReplay(h, t); r != "" {
						t.Errorf("order %v: stored match differs from a replay of its log: %s", p, r)
					}
					s := pmState(h, t)
					states[s] = append(states[s], fmt.Sprintf("%v errs=%s", p, errs))
				}
				if len(states) > 1 {
					var sb strings.Builder
					for s, ps := range states {
						sb.WriteString("\n   " + s)
						for _, p := range ps {
							sb.WriteString("\n      " + p)
						}
					}
					if c.byDesign != "" {
						t.Logf("byDesign (%s): %d outcomes%s", c.byDesign, len(states), sb.String())
						return
					}
					t.Errorf("arrival order changed the outcome (%d outcomes):%s", len(states), sb.String())
				} else if c.byDesign != "" {
					t.Errorf("tagged byDesign (%s) but every order converges: drop the tag", c.byDesign)
				}
			})
		}
	}
	t.Logf("matrix: %d cases, %d permutations", cases, perms)
}
