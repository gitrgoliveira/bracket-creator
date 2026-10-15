package engine

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The permutation matrix (bc-evts stage 2b, review rounds 3 and 4). The core
// promise of the merge is that ARRIVAL ORDER NEVER CHANGES THE OUTCOME: the
// same set of stamped writes, delivered in any order, leaves the same status,
// winner, decision, scoreline, competitor status, group stamps and ModifiedAt,
// the knockout winner is in the next match, and the stored match equals a
// replay of its own log. Every case lists its writes in stamp order, runs EVERY
// arrival order, and asserts ONE expected outcome (want) for all of them; no
// case is excused.
//
// THE CLOSED-MATCH RULE (operator, 2026-10-15) decides what that outcome is: in
// stamp order, once a finish or a decision (kiken, fusenpai, fusensho, hantei,
// daihyosen, hikiwake) is recorded, a tap or a scoring write stamped AFTER it is
// refused and kept in the history. A newer finish or decision over an older one
// is the newer one. Everything stamped BEFORE the result keeps the D5 rule
// (recorded when the result's basis holds it, else beforeBaseline). So a case
// names its refused writes (refused): the stored match must equal the match a
// device that never sent them would have, whichever order they arrived in.
//
// Each write is also asserted on its own answer: a tap or scoring write
// arriving once a result stamped before it is stored is refused (matchClosed on
// the events door, superseded with heldReason match_closed on the score door),
// one arriving before it is applied; a result arriving once a newer result is
// stored is superseded.

const (
	pmRes = "result" // a finish or a decision: closes the match
	pmTap = "tap"    // a scoring write: refused once an earlier result is stored
	pmEv  = "event"  // a tap on the score-events door
)

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
	statuses, err := h.store.LoadCompetitorStatus(h.compID)
	require.NoError(t, err)
	cs := []string{}
	for id, s := range statuses {
		if !s.Eligible {
			cs = append(cs, fmt.Sprintf("%s:out(%s)", id, s.MatchID))
		}
	}
	sort.Strings(cs)
	next := ""
	if h.matchID == "m-r1-0" {
		b, err := h.store.LoadBracket(h.compID)
		require.NoError(t, err)
		next = fmt.Sprintf("%q/%s", b.Rounds[1][0].SideA, b.Rounds[1][0].SideAID)
	}
	return fmt.Sprintf("st=%s w=%q d=%q by=%q A=%v B=%v subs=%s mod=%.1f stamps=%s cs=%v next=%s",
		g.Status, g.Winner, g.Decision, g.DecisionBy, g.IpponsA, g.IpponsB, subs,
		float64(g.ModifiedAt-mmT0)/60000, st, cs, next)
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

// pmOutcomes is the final outcome of every tap, from a replay of the stored log.
func pmOutcomes(h mmHome) map[string]string {
	lines, err := h.store.LoadScoreLog(h.compID, h.matchID)
	if err != nil {
		return nil
	}
	m, inPool, err := h.eng.lookupExistingResultIn(h.store, h.compID, h.matchID)
	if err != nil {
		return nil
	}
	comp, _ := h.store.LoadCompetition(h.compID)
	rp := ReplayScoring(m, lines, ReplayShape{Comp: comp, Knockout: !inPool})
	out := map[string]string{}
	for _, o := range rp.Outcomes {
		out[o.ID] = o.Outcome
	}
	return out
}

// pmStep is one stamped write of a case.
type pmStep struct {
	at   int64
	role string
	id   string // the tap's id, for pmEv
	run  func(h mmHome) string
}

// pmAnswer names what a write was answered with.
func pmAnswer(err error) string {
	switch {
	case err == nil:
		return "applied"
	case errors.Is(err, ErrMatchSuperseded):
		return "superseded:" + HeldReasonOf(err)
	}
	return "error:" + err.Error()
}

func pmPt(at int64, a, b []string) pmStep {
	return pmStep{at: at, role: pmTap, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupPoints)
		w.IpponsA, w.IpponsB = a, b
		return pmAnswer(h.write(w))
	}}
}

func pmFin(at int64, win string, a, b []string, own ...string) pmStep {
	return pmStep{at: at, role: pmRes, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupPoints, state.GroupResult)
		w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, win, a, b
		if own != nil {
			w.Basis = &state.ScoreBasis{Own: own}
		}
		return pmAnswer(h.write(w))
	}}
}

// pmBareInd is a finish whose payload carries the points but names result alone.
func pmBareInd(at int64, win string, a, b []string) pmStep {
	return pmStep{at: at, role: pmRes, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupResult)
		w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, win, a, b
		return pmAnswer(h.write(w))
	}}
}

// pmBareFin is a finish that names result alone and carries no scoreline: it
// stands on the taps in its basis.
func pmBareFin(at int64, win string, own ...string) pmStep {
	return pmStep{at: at, role: pmRes, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupResult)
		w.Status, w.Winner = state.MatchStatusCompleted, win
		w.Basis = &state.ScoreBasis{Own: own}
		return pmAnswer(h.write(w))
	}}
}

// pmBareTeam is a team finish whose payload carries bouts but names result alone.
func pmBareTeam(at int64, win string, subs ...state.SubMatchResult) pmStep {
	return pmStep{at: at, role: pmRes, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupResult)
		w.Status, w.Winner, w.SubResults = state.MatchStatusCompleted, win, subs
		return pmAnswer(h.write(w))
	}}
}

func pmDec(at int64, d, by string) pmStep {
	return pmStep{at: at, role: pmRes, run: func(h mmHome) string {
		_, _, e := h.eng.RecordDecision(h.compID, h.matchID, d, by, "", nil, at)
		return pmAnswer(e)
	}}
}

func pmHik(at int64) pmStep {
	return pmStep{at: at, role: pmRes, run: func(h mmHome) string {
		return pmAnswer(h.write(&state.MatchResult{ID: h.matchID, SideA: wrTeamA, SideB: wrTeamB, SideAID: wrTeamAID, SideBID: wrTeamBID,
			Status: state.MatchStatusCompleted, Decision: "hikiwake", ModifiedAt: at,
			Changed: []string{state.GroupResult}, WriteDoor: DoorDecision}))
	}}
}

func pmBout(at int64, pos int, a, b []string, win string) pmStep {
	return pmStep{at: at, role: pmTap, run: func(h mmHome) string {
		w := mmRunning(h, at, state.BoutGroup(pos))
		w.SubResults = []state.SubMatchResult{{Position: pos, SideA: wrTeamA, SideB: wrTeamB, IpponsA: a, IpponsB: b, Winner: win}}
		return pmAnswer(h.write(w))
	}}
}

// pmSeq runs steps one after the other (a case's setup).
func pmSeq(steps ...pmStep) pmStep {
	return pmStep{run: func(h mmHome) string {
		for _, s := range steps {
			if a := s.run(h); a != "applied" {
				return a
			}
		}
		return "applied"
	}}
}

func pmTeamFin(at int64, win string, own []string, subs ...state.SubMatchResult) pmStep {
	return pmStep{at: at, role: pmRes, run: func(h mmHome) string {
		ch := []string{state.GroupResult}
		for _, s := range subs {
			ch = append(ch, state.BoutGroup(s.Position))
		}
		w := mmRunning(h, at, ch...)
		w.Status, w.Winner, w.SubResults = state.MatchStatusCompleted, win, subs
		if own != nil {
			w.Basis = &state.ScoreBasis{Own: own}
		}
		return pmAnswer(h.write(w))
	}}
}

// pmEvent is one ippon tap through the score-events door.
func pmEvent(id, sess string, seq int64, at int64, pos int, side, letter string) pmStep {
	return pmStep{at: at, role: pmEv, id: id, run: func(h mmHome) string {
		ev := domain.ScoreEvent{ID: id, Session: sess, Seq: seq, Stamp: at, Kind: domain.ScoreIpponAdd, Pos: pos, Side: side, Letter: letter}
		if err := ev.Validate(); err != nil {
			return "invalid:" + err.Error()
		}
		r, err := h.eng.RecordScoreEvents(h.compID, h.matchID, ScoreEventsRequest{Session: sess, Events: []domain.ScoreEvent{ev}})
		if err != nil {
			return "error:" + err.Error()
		}
		if r.WriteErr != nil {
			return "error:" + r.WriteErr.Error()
		}
		return "ev:" + r.Answers[0].Outcome
	}}
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

type pmCase struct {
	name  string
	team  bool
	rows  bool // a team match with its three bout rows already drawn
	pre   pmStep
	steps []pmStep
	// refused names the steps the closed-match rule turns away whichever order
	// they arrive in: the stored match must equal the match without them.
	refused []int
	// want holds the substrings of the stored state every arrival order must
	// leave (status, winner, decision, scoreline).
	want []string
	// where limits the case to "pool" or "ko" (empty means both).
	where string
}

// pmHome is a fresh match for a case: a team knockout gets the same next round
// an individual one has, so the winner advancing can be checked, and a team
// match may start with its bout rows drawn.
func pmHome(t *testing.T, c pmCase, ko bool) mmHome {
	var h mmHome
	if c.team {
		h = mmTeam(t, ko)
	} else {
		h = mmIndividual(t, ko)
	}
	if c.team && ko {
		b, err := h.store.LoadBracket(h.compID)
		require.NoError(t, err)
		b.Rounds = [][]state.BracketMatch{
			{b.Rounds[0][0], {ID: "m-r1-1", SideA: wrTeamC, SideAID: wrTeamCID, Status: state.MatchStatusCompleted, Winner: wrTeamC, WinnerID: wrTeamCID, MatchNumber: 2}},
			{{ID: "m-r2-0", SideA: winnerOfPlaceholder(2, 0), SideB: wrTeamC, SideBID: wrTeamCID, MatchNumber: 3}},
		}
		require.NoError(t, h.store.SaveBracket(h.compID, b))
	}
	if c.team && c.rows {
		rows := []state.SubMatchResult{}
		for i := 1; i <= 3; i++ {
			rows = append(rows, state.SubMatchResult{Position: i, SideA: fmt.Sprintf("A%d", i), SideB: fmt.Sprintf("B%d", i), IpponsA: []string{}, IpponsB: []string{}})
		}
		if ko {
			b, err := h.store.LoadBracket(h.compID)
			require.NoError(t, err)
			b.Rounds[0][0].SubResults = rows
			require.NoError(t, h.store.SaveBracket(h.compID, b))
		} else {
			ms, err := h.store.LoadPoolMatches(h.compID)
			require.NoError(t, err)
			ms[0].SubResults = rows
			require.NoError(t, h.store.SavePoolMatches(h.compID, ms))
		}
	}
	return h
}

// pmArrivalCheck is the one predicate for a write's own answer: it asks what
// has arrived already, and by the stamps alone says whether the write is
// turned away.
func pmArrivalCheck(s pmStep, arrived []pmStep, ans string) string {
	closed, newerResult := false, false
	for _, a := range arrived {
		if a.role != pmRes {
			continue
		}
		closed = closed || a.at < s.at
		newerResult = newerResult || a.at > s.at
	}
	switch s.role {
	case pmEv:
		if closed != (ans == "ev:"+domain.ScoreOutcomeMatchClosed) {
			return fmt.Sprintf("a tap stamped %d with a result before it arrived=%v answered %q", s.at, closed, ans)
		}
	case pmTap:
		if closed != (ans == "superseded:"+state.HeldReasonMatchClosed) {
			return fmt.Sprintf("a scoring write stamped %d with a result before it arrived=%v answered %q", s.at, closed, ans)
		}
	case pmRes:
		if newerResult != strings.HasPrefix(ans, "superseded") {
			return fmt.Sprintf("a result stamped %d with a newer result arrived=%v answered %q", s.at, newerResult, ans)
		}
	}
	return ""
}

// pmFinalCheck says what each tap's final outcome must be, by the stamps alone.
func pmFinalCheck(c pmCase, h mmHome) []string {
	outs := pmOutcomes(h)
	var bad []string
	for _, s := range c.steps {
		if s.role != pmEv {
			continue
		}
		closed, later := false, false
		for _, a := range c.steps {
			if a.role == pmRes {
				closed = closed || a.at < s.at
				later = later || a.at > s.at
			}
		}
		got := outs[s.id]
		switch {
		case closed && got != domain.ScoreOutcomeMatchClosed:
			bad = append(bad, fmt.Sprintf("tap %s should end matchClosed, ended %q", s.id, got))
		case !closed && later && got != domain.ScoreOutcomeRecorded && got != domain.ScoreOutcomeBeforeBaseline:
			bad = append(bad, fmt.Sprintf("tap %s should end recorded or beforeBaseline, ended %q", s.id, got))
		case !closed && !later && got != domain.ScoreOutcomeApplied:
			bad = append(bad, fmt.Sprintf("tap %s should end applied, ended %q", s.id, got))
		}
	}
	return bad
}

// pmAssertAdvanced checks the knockout winner is in the next match.
func pmAssertAdvanced(t *testing.T, h mmHome, order []int) {
	if h.matchID != "m-r1-0" {
		return
	}
	m := h.load(t)
	b, err := h.store.LoadBracket(h.compID)
	require.NoError(t, err)
	next := b.Rounds[1][0]
	if m.Winner != "" {
		assert.Equal(t, m.Winner, next.SideA, "order %v: the winner advances into the next match", order)
		assert.Equal(t, m.WinnerID, next.SideAID, "order %v: the winner's id advances too", order)
	} else {
		assert.Equal(t, winnerOfPlaceholder(2, 0), next.SideA, "order %v: no winner, so nobody advanced", order)
	}
}

func TestMatchWritePermutationsConverge(t *testing.T) {
	cases, perms := 0, 0
	for _, c := range pmCases() {
		require.NotEmpty(t, c.want, "%s: a case asserts its outcome", c.name)
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
				// The oracle: the match a device that never sent the refused
				// writes would hold.
				oracle := ""
				if len(c.refused) > 0 {
					h := pmHome(t, c, ko)
					if c.pre.run != nil {
						require.Equal(t, "applied", c.pre.run(h))
					}
					for i, s := range c.steps {
						if !slices.Contains(c.refused, i) {
							s.run(h)
						}
					}
					oracle = pmState(h, t)
				}
				states := map[string][]string{}
				for _, p := range pmPerms(len(c.steps)) {
					h := pmHome(t, c, ko)
					if c.pre.run != nil {
						require.Equal(t, "applied", c.pre.run(h), "pre")
					}
					var arrived []pmStep
					answers := ""
					for _, i := range p {
						s := c.steps[i]
						ans := s.run(h)
						answers += ans + ";"
						if why := pmArrivalCheck(s, arrived, ans); why != "" {
							t.Errorf("order %v: %s", p, why)
						}
						arrived = append(arrived, s)
					}
					perms++
					if r := pmReplay(h, t); r != "" {
						t.Errorf("order %v: stored match differs from a replay of its log: %s", p, r)
					}
					for _, bad := range pmFinalCheck(c, h) {
						t.Errorf("order %v: %s", p, bad)
					}
					pmAssertAdvanced(t, h, p)
					s := pmState(h, t)
					for _, w := range c.want {
						if !strings.Contains(s, w) {
							t.Errorf("order %v: want %q in %s", p, w, s)
						}
					}
					if oracle != "" && s != oracle {
						t.Errorf("order %v: the refused writes left a mark:\n   got    %s\n   oracle %s", p, s, oracle)
					}
					states[s] = append(states[s], fmt.Sprintf("%v answers=%s", p, answers))
				}
				if len(states) > 1 {
					var sb strings.Builder
					for s, ps := range states {
						sb.WriteString("\n   " + s)
						for _, p := range ps {
							sb.WriteString("\n      " + p)
						}
					}
					t.Errorf("arrival order changed the outcome (%d outcomes):%s", len(states), sb.String())
				}
			})
		}
	}
	t.Logf("matrix: %d cases, %d permutations", cases, perms)
}

func pmCases() []pmCase {
	M, K := []string{"M"}, []string{"K"}
	MK := []string{"M", "K"}
	b := func(pos int, a, bb []string, w string) state.SubMatchResult {
		return state.SubMatchResult{Position: pos, SideA: wrTeamA, SideB: wrTeamB, IpponsA: a, IpponsB: bb, Winner: w}
	}
	r := func(pos int, a, bb []string, w string) state.SubMatchResult {
		return state.SubMatchResult{Position: pos, SideA: fmt.Sprintf("A%d", pos), SideB: fmt.Sprintf("B%d", pos), IpponsA: a, IpponsB: bb, Winner: w}
	}
	preInd := pmPt(mmT0+1000, M, nil)
	preTeam := pmSeq(pmBout(mmT0+1000, 1, M, nil, wrTeamA), pmBout(mmT0+2000, 2, nil, M, wrTeamB))
	preB1 := pmBout(mmT0+1000, 1, M, nil, wrTeamA)
	ryu := `w="Ryu"`
	tie := []string{`st=completed w="" d="hikiwake"`}
	half := func(n int64) int64 { return mmT0 + n*60_000 + 30_000 }
	t1, t2, t3, t4 := mmT1, mmT2, mmT3, mmT4
	return []pmCase{
		// A finish, then scoring stamped after it: refused, whichever arrives first.
		{name: "ind bareFin Ryu(M-0) T1 / pt MK-0 T2", pre: preInd, steps: []pmStep{pmBareInd(t1, wrTeamA, M, nil), pmPt(t2, MK, nil)}, refused: []int{1}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "ind bareFin Ryu(M-0) T1 / pt M-MK T2", pre: preInd, steps: []pmStep{pmBareInd(t1, wrTeamA, M, nil), pmPt(t2, M, MK)}, refused: []int{1}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "ind bareFin Ryu(M-0) T1 / pt M-M T2 (tie)", pre: preInd, steps: []pmStep{pmBareInd(t1, wrTeamA, M, nil), pmPt(t2, M, M)}, refused: []int{1}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "team bareFin Ryu(payload bouts) T1 / b3 B T2", team: true, pre: preTeam, steps: []pmStep{pmBareTeam(t1, wrTeamA, b(1, M, nil, wrTeamA), b(2, nil, M, wrTeamB)), pmBout(t2, 3, nil, K, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "team bareFin Ryu(no bouts) T1 / b3 B T2", team: true, pre: preTeam, steps: []pmStep{pmTeamFin(t1, wrTeamA, nil), pmBout(t2, 3, nil, K, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "team bareFin Ryu(no bouts) T1 / b3 A T2", team: true, pre: preTeam, steps: []pmStep{pmTeamFin(t1, wrTeamA, nil), pmBout(t2, 3, K, nil, wrTeamA)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		// The 3-write team sequences: a finish, a tying newer bout, a third write.
		{name: "team fin Ryu(b1A) T1, b2 B T2 (tie), b3 B T3", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmBout(t2, 2, nil, M, wrTeamB), pmBout(t3, 3, nil, K, wrTeamB)}, refused: []int{1, 2}, want: []string{`st=completed`, ryu}},
		{name: "team fin Ryu(b1A) T1, b2 B T2 (tie), b2 A T3", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmBout(t2, 2, nil, M, wrTeamB), pmBout(t3, 2, K, nil, wrTeamA)}, refused: []int{1, 2}, want: []string{`st=completed`, ryu}},
		{name: "team fin Ryu(b1A) T1, b2 B T2 (tie), b3 empty T3", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmBout(t2, 2, nil, M, wrTeamB), pmBout(t3, 3, nil, nil, "")}, refused: []int{1, 2}, want: []string{`st=completed`, ryu}},
		{name: "team fin Ryu(b1A) T1, b2 B T2 (tie), b1 AK T3", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmBout(t2, 2, nil, M, wrTeamB), pmBout(t3, 1, MK, nil, wrTeamA)}, refused: []int{1, 2}, want: []string{`st=completed`, ryu}},
		{name: "team fin Ryu(b1A) T1, b2 0-M T2 (tie), b2 0-MK T3", team: true, pre: preB1,
			steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmBout(t2, 2, nil, M, wrTeamB), pmBout(t3, 2, nil, MK, wrTeamB)}, refused: []int{1, 2}, want: []string{`st=completed`, ryu}},
		{name: "ind fin Ryu(M-0) T1, pt M-M T2 (tie), pt M-MK T3", pre: preInd,
			steps: []pmStep{pmFin(t1, wrTeamA, M, nil), pmPt(t2, M, M), pmPt(t3, M, MK)}, refused: []int{1, 2}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "ind fin Ryu(M-0) T1, pt M-M T2 (tie), pt MK-M T3", pre: preInd,
			steps: []pmStep{pmFin(t1, wrTeamA, M, nil), pmPt(t2, M, M), pmPt(t3, MK, M)}, refused: []int{1, 2}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "team dai shiro T1 / b1 A T2 / b2 B T3", team: true,
			steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmBout(t2, 1, M, nil, wrTeamA), pmBout(t3, 2, nil, M, wrTeamB)}, refused: []int{1, 2}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		// Two-write pairs: decisions against scoring, finishes against decisions.
		{name: "dai aka T1 / pt M-0 T2", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmPt(t2, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "dai shiro T1 / pt M-0 T2", steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmPt(t2, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "dai aka T1 / pt M-K T2 (tie)", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmPt(t2, M, K)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "dai aka T1 / pt 0-0 T2", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmPt(t2, nil, nil)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		// A newer finish over an older decision, and the other way round: the newer one stands.
		{name: "dai aka T1 / fin Ryu M-0 T2", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmFin(t2, wrTeamA, M, nil)}, want: []string{`st=completed`, ryu, `d="" by=""`, `A=[M] B=[]`}},
		{name: "fin Ryu M-0 T1 / dai aka T2", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), pmDec(t2, "daihyosen", "aka")}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "pt M-0 T1 / dai aka T2", steps: []pmStep{pmPt(t1, M, nil), pmDec(t2, "daihyosen", "aka")}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		// A withdrawal closes the match: points after it are refused.
		{name: "kiken aka T1 / pt 0-M T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmPt(t2, nil, M)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `cs=[`}},
		{name: "kiken aka T1 / pt M-0 T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmPt(t2, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`}},
		{name: "fusensho aka T1 / pt M-0 T2", steps: []pmStep{pmDec(t1, "fusensho", "aka"), pmPt(t2, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="fusensho" by="aka"`}},
		{name: "fin Ryu M-0 T1 / fusensho shiro T2", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), pmDec(t2, "fusensho", "shiro")}, want: []string{`st=completed`, `w="Ryu" d="fusensho" by="shiro"`}},
		{name: "hik T1 / pt M-0 T2", steps: []pmStep{pmHik(t1), pmPt(t2, M, nil)}, refused: []int{1}, want: tie, where: "pool"},
		{name: "hik T1 / pt M-K T2", steps: []pmStep{pmHik(t1), pmPt(t2, M, K)}, refused: []int{1}, want: tie, where: "pool"},
		{name: "fin Ht-0 T1 / pt 0-0 T2", steps: []pmStep{pmFin(t1, wrTeamA, []string{"Ht"}, nil), pmPt(t2, nil, nil)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "fin Ht-0 T1 / pt 0-Ht T2", steps: []pmStep{pmFin(t1, wrTeamA, []string{"Ht"}, nil), pmPt(t2, nil, []string{"Ht"})}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "pt 0-0 T1 / fin Ht T2", pre: pmPt(mmT0+1, M, nil), steps: []pmStep{pmPt(t1, nil, nil), pmFin(t2, wrTeamA, []string{"Ht"}, nil)}, want: []string{`st=completed`, ryu}},
		{name: "team dai aka T1 / bout1 A M T2", team: true, steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmBout(t2, 1, M, nil, wrTeamA)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "team dai shiro T1 / bout1 B M T2", team: true, steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmBout(t2, 1, nil, M, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "team bouts favour B, dai shiro T1 / bout3 B T2", team: true, pre: preTeam, steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmBout(t2, 3, nil, K, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "team kiken aka T1 / bout1 B T2", team: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmBout(t2, 1, nil, M, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`}},
		{name: "team kiken aka T1 / bout1 A T2", team: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmBout(t2, 1, M, nil, wrTeamA)}, refused: []int{1}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`}},
		{name: "team fin Ryu(b1 A) T1 / b2 B T2", team: true, steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmBout(t2, 2, nil, M, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "team fin Ryu(b1 A) T1 / b1 B T2 flip", team: true, steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmBout(t2, 1, nil, K, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "team pre b1A b2B; fin Ryu(dec only) T1 / b3 B T2", team: true, pre: preTeam, steps: []pmStep{pmTeamFin(t1, wrTeamA, nil), pmBout(t2, 3, nil, K, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "team fusensho aka T1 / b1 A T2", team: true, steps: []pmStep{pmDec(t1, "fusensho", "aka"), pmBout(t2, 1, M, nil, wrTeamA)}, refused: []int{1}, want: []string{`st=completed`, `d="fusensho" by="aka"`}},
		// Two results: the newer one stands, whichever arrives first.
		{name: "kiken aka T1 -> dai shiro T2", steps: []pmStep{pmDec(t1, "kiken-injury", "aka"), pmDec(t2, "daihyosen", "shiro")}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`, `cs=[]`}},
		{name: "dai shiro T1 -> dai aka T2 (correction)", steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmDec(t2, "daihyosen", "aka")}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "team kiken aka T1 -> dai shiro T2", team: true, steps: []pmStep{pmDec(t1, "kiken-injury", "aka"), pmDec(t2, "daihyosen", "shiro")}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`, `cs=[]`}},
		// The older decision arriving after the newer one (round-4 finding 8).
		{name: "kiken aka T2 / dai shiro T1 (stale dai)", steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmDec(t2, "kiken-injury", "aka")}, want: []string{`st=completed`, `d="kiken-injury" by="aka"`, `cs=[`}},
		{name: "dai shiro T2 / dai aka T1 (stale)", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmDec(t2, "daihyosen", "shiro")}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "team kiken aka T2 / dai shiro T1 (stale dai)", team: true, steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmDec(t2, "kiken-injury", "aka")}, want: []string{`st=completed`, `d="kiken-injury" by="aka"`, `cs=[`}},
		{name: "team dai shiro T2 / dai aka T1 (stale)", team: true, steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmDec(t2, "daihyosen", "shiro")}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},

		// ---- Mixed: taps through the score-events door and finishes or
		// decisions that carry a basis, the production mix after stage 3. ----
		// Finding 1: a decision stamped before taps that arrived first must not double their marks.
		{name: "mix ind dai shiro T1 / e a:M T2 / e b:K T3", steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmEvent("e1", "d1", 1, t2, 0, "a", "M"), pmEvent("e2", "d1", 2, t3, 0, "b", "K")}, refused: []int{1, 2}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "mix ind dai aka T1 / e a:M T2", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmEvent("e1", "d1", 1, t2, 0, "a", "M")}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "mix team dai shiro T1 / e b1 a:M T2 / e b2 b:K T3", team: true, rows: true, steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmEvent("e1", "d1", 1, t2, 1, "a", "M"), pmEvent("e2", "d1", 2, t3, 2, "b", "K")}, refused: []int{1, 2}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "mix ind kiken aka T1 / e a:M T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d1", 1, t2, 0, "a", "M")}, refused: []int{1}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`, `cs=[`}},
		{name: "mix ind fusensho shiro T1 / e a:M T2", steps: []pmStep{pmDec(t1, "fusensho", "shiro"), pmEvent("e1", "d1", 1, t2, 0, "a", "M")}, refused: []int{1}, want: []string{`st=completed`, `d="fusensho" by="shiro"`}},
		{name: "mix team kiken aka T1 / e b1 a:M T2", team: true, rows: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d1", 1, t2, 1, "a", "M")}, refused: []int{1}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`}},
		{name: "mix ind e a:M T1 / kiken aka T2 / e b:M T3", steps: []pmStep{pmEvent("e1", "d1", 1, t1, 0, "a", "M"), pmDec(t2, "kiken-voluntary", "aka"), pmEvent("e2", "d2", 1, t3, 0, "b", "M")}, refused: []int{2}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`}},
		{name: "mix ind e a:M T1 / fusensho shiro T2", steps: []pmStep{pmEvent("e1", "d1", 1, t1, 0, "a", "M"), pmDec(t2, "fusensho", "shiro")}, want: []string{`st=completed`, `d="fusensho" by="shiro"`}},
		// Findings 4 and 5: taps around a finish.
		{name: "mix ind e a:M T1, fin Ryu M-0 T2 own e1, e b:M T3, e b:K T4", steps: []pmStep{
			pmEvent("e1", "d1", 1, t1, 0, "a", "M"), pmFin(t2, wrTeamA, M, nil, "e1"), pmEvent("e2", "d3", 1, t3, 0, "b", "M"), pmEvent("e3", "d2", 1, t4, 0, "b", "K")},
			refused: []int{2, 3}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "mix ind fin Ryu M-0 T2 (no taps before), e b:K T4, e b:M T3", steps: []pmStep{
			pmFin(t2, wrTeamA, M, nil), pmEvent("e3", "d2", 1, t4, 0, "b", "K"), pmEvent("e2", "d3", 1, t3, 0, "b", "M")},
			refused: []int{1, 2}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "mix ind e a:M T1 d1, bareFin Ryu T2 own e1, e b:M T3", steps: []pmStep{
			pmEvent("e1", "d1", 1, t1, 0, "a", "M"), pmBareFin(t2, wrTeamA, "e1"), pmEvent("e2", "d3", 1, t3, 0, "b", "M")},
			refused: []int{2}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "mix ind e a:M T1, bareFin Ryu T2 own e1, e b:M T3, e b:K T4", steps: []pmStep{
			pmEvent("e1", "d1", 1, t1, 0, "a", "M"), pmBareFin(t2, wrTeamA, "e1"), pmEvent("e2", "d2", 1, t3, 0, "b", "M"), pmEvent("e3", "d2", 2, t4, 0, "b", "K")},
			refused: []int{2, 3}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "mix ind e a:M T1, fin T2 own e1, e b:M T3 (tie), e a:K T4 (untie)", steps: []pmStep{
			pmEvent("e1", "d1", 1, t1, 0, "a", "M"), pmFin(t2, wrTeamA, M, nil, "e1"), pmEvent("e2", "d2", 1, t3, 0, "b", "M"), pmEvent("e3", "d2", 2, t4, 0, "a", "K")},
			refused: []int{2, 3}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "mix ind e a:M T1, e b:M T1.5 unseen, fin Ryu M-0 T2 own e1", steps: []pmStep{
			pmEvent("e1", "d1", 1, t1, 0, "a", "M"), pmEvent("e2", "d2", 1, half(1), 0, "b", "M"), pmFin(t2, wrTeamA, M, nil, "e1")},
			want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "mix ind dai aka T1 / e a:M T2 / e b:K T3", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmEvent("e1", "d1", 1, t2, 0, "a", "M"), pmEvent("e2", "d1", 2, t3, 0, "b", "K")}, refused: []int{1, 2}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "mix ind kiken aka T1 / e b:M T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d2", 1, t2, 0, "b", "M")}, refused: []int{1}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`}},
		// Team: bout rows already drawn, taps by bout.
		{name: "mix team e1 b1 a:M T1, fin Ryu(b1) T2 own e1, e2 b2 b:M T3 (tie)", team: true, rows: true, steps: []pmStep{
			pmEvent("e1", "d1", 1, t1, 1, "a", "M"), pmTeamFin(t2, wrTeamA, []string{"e1"}, r(1, M, nil, "A1")), pmEvent("e2", "d2", 1, t3, 2, "b", "M")},
			refused: []int{2}, want: []string{`st=completed`, ryu, `[1 [M]-[]`}},
		{name: "mix team e1 b1 a:M T1, fin Ryu(b1) T2 own e1, e2 b2 b:M T3, e3 b3 b:K T4", team: true, rows: true, steps: []pmStep{
			pmEvent("e1", "d1", 1, t1, 1, "a", "M"), pmTeamFin(t2, wrTeamA, []string{"e1"}, r(1, M, nil, "A1")), pmEvent("e2", "d2", 1, t3, 2, "b", "M"), pmEvent("e3", "d2", 2, t4, 3, "b", "K")},
			refused: []int{2, 3}, want: []string{`st=completed`, ryu, `[1 [M]-[]`}},
		{name: "mix team fin Ryu(b1 M) T2, e b3 b:K T4, e b2 b:M T3", team: true, rows: true, steps: []pmStep{
			pmTeamFin(t2, wrTeamA, nil, r(1, M, nil, "A1")), pmEvent("e3", "d2", 1, t4, 3, "b", "K"), pmEvent("e2", "d3", 1, t3, 2, "b", "M")},
			refused: []int{1, 2}, want: []string{`st=completed`, ryu, `[1 [M]-[]`}},
		{name: "mix team e1 b1 a:M T1, e2 b2 b:M T1.5 unseen, fin Ryu(b1) T2 own e1", team: true, rows: true, steps: []pmStep{
			pmEvent("e1", "d1", 1, t1, 1, "a", "M"), pmEvent("e2", "d2", 1, half(1), 2, "b", "M"), pmTeamFin(t2, wrTeamA, []string{"e1"}, r(1, M, nil, "A1"))},
			want: []string{`st=completed`, ryu, `[1 [M]-[]`}},
		{name: "mix team e1 b1 a:M T1, bareFin Ryu T2 own e1, e2 b2 b:M T3, e3 b3 b:K T4", team: true, rows: true, steps: []pmStep{
			pmEvent("e1", "d1", 1, t1, 1, "a", "M"), pmTeamFin(t2, wrTeamA, []string{"e1"}), pmEvent("e2", "d2", 1, t3, 2, "b", "M"), pmEvent("e3", "d2", 2, t4, 3, "b", "K")},
			refused: []int{2, 3}, want: []string{`st=completed`, ryu, `[1 [M]-[]`}},
		{name: "mix team kiken aka T1, e b1 a:M T2, e b2 b:M T3", team: true, rows: true, steps: []pmStep{
			pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d1", 1, t2, 1, "a", "M"), pmEvent("e2", "d1", 2, t3, 2, "b", "M")},
			refused: []int{1, 2}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`}},
		{name: "mix team dai shiro T1, e b1 a:M T2, e b2 b:M T3", team: true, rows: true, steps: []pmStep{
			pmDec(t1, "daihyosen", "shiro"), pmEvent("e1", "d1", 1, t2, 1, "a", "M"), pmEvent("e2", "d1", 2, t3, 2, "b", "M")},
			refused: []int{1, 2}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		// Two results with a tap between them.
		{name: "mix ind kiken aka T1 / e b:M T2 / dai shiro T3", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d2", 1, t2, 0, "b", "M"), pmDec(t3, "daihyosen", "shiro")}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`, `cs=[]`}},
	}
}

// TestMatchWriteExactResendIsRecorded: a finish or a decision sent again (its
// answer was lost) is answered as recorded: nothing is written, the log does
// not grow and the match is as it was.
func TestMatchWriteExactResendIsRecorded(t *testing.T) {
	M := []string{"M"}
	for _, ko := range []bool{false, true} {
		for _, team := range []bool{false, true} {
			kind := fmt.Sprintf("team=%v/ko=%v", team, ko)
			finish := func(writeID string) func(h mmHome) error {
				return func(h mmHome) error {
					w := mmRunning(h, mmT2, state.GroupPoints, state.GroupResult)
					w.Status, w.Winner, w.IpponsA, w.WriteID = state.MatchStatusCompleted, wrTeamA, M, writeID
					if team {
						w.Changed = []string{state.GroupResult, state.BoutGroup(1)}
						w.IpponsA = nil
						w.SubResults = []state.SubMatchResult{{Position: 1, SideA: wrTeamA, SideB: wrTeamB, IpponsA: M, Winner: wrTeamA}}
					}
					return h.write(w)
				}
			}
			decision := func(d, by string) func(h mmHome) error {
				return func(h mmHome) error {
					_, _, e := h.eng.RecordDecision(h.compID, h.matchID, d, by, "", nil, mmT2)
					return e
				}
			}
			for name, do := range map[string]func(h mmHome) error{
				"finish with a write id":    finish("w-fin-1"),
				"finish without a write id": finish(""),
				"daihyosen decision":        decision("daihyosen", "aka"),
				"kiken decision":            decision("kiken-voluntary", "aka"),
			} {
				do := do
				t.Run(kind+"/"+name, func(t *testing.T) {
					h := pmHome(t, pmCase{team: team}, ko)
					require.NoError(t, do(h))
					before := pmState(h, t)
					lines, err := h.store.LoadScoreLog(h.compID, h.matchID)
					require.NoError(t, err)
					require.NoError(t, do(h), "the resend is answered as recorded")
					assert.Equal(t, before, pmState(h, t), "the match is as it was")
					after, err := h.store.LoadScoreLog(h.compID, h.matchID)
					require.NoError(t, err)
					assert.Len(t, after, len(lines), "the log did not grow")
					assert.Empty(t, pmReplay(h, t))
				})
			}
		}
	}
}
