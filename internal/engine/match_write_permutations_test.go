package engine

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

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
// refused and kept in the history. THE FIRST RECORDED RESULT STANDS (operator
// decision 2026-10-16, I1): in stamp order, a later finish or decision from a
// device that did not see it is refused (match_closed) in every arrival order;
// only a correction that saw it (a reason, clearWithdrawal, a basis holding it)
// replaces it. A TEAM match closed by a match-level kiken, fusenpai or
// fusensho merges BY BOUT (T1, landsAfter): the bouts scored elsewhere keep
// their scores. Everything stamped BEFORE the result keeps the D5 rule
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
	// pmCorr is a result from a screen that showed the recorded one (a
	// correction reason, clearWithdrawal, a basis holding it): the newer one stands, as before.
	pmCorr = "correction"
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
		// A stamp on a group that holds nothing orders nothing (a decision's
		// echo of "no overtime"), so it is not part of the outcome.
		if bytes.Equal(state.GroupValue(&g, k), state.GroupValue(&state.MatchResult{}, k)) {
			continue
		}
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
	// ev is the tap itself (pmEv): the group it changes and its side decide
	// the one outcome the rule gives it before a result (pmWantOutcome).
	ev domain.ScoreEvent
	// For a result (pmRes, pmCorr): the groups it sets, the events its basis
	// holds, and (a decision) the side that lost, "aka" or "shiro".
	sets []string
	own  []string
	by   string
	dec  bool
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

// pmWrite sends a score write and names its answer: applied, partial (applied
// with groups held) or superseded, each with the heldReason it carries.
func pmWrite(h mmHome, w *state.MatchResult) string {
	_, err := h.eng.RecordMatchResultWithIneligibility(h.compID, h.matchID, w)
	return pmWriteAnswer(w, err)
}

func pmWriteAnswer(w *state.MatchResult, err error) string {
	if err != nil {
		return pmAnswer(err)
	}
	if w.Merge != nil && len(w.Merge.HeldGroups()) > 0 {
		return "partial:" + w.Merge.HeldReason()
	}
	return "applied"
}

func pmPt(at int64, a, b []string) pmStep {
	return pmStep{at: at, role: pmTap, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupPoints)
		w.IpponsA, w.IpponsB = a, b
		return pmWrite(h, w)
	}}
}

func pmFin(at int64, win string, a, b []string, own ...string) pmStep {
	return pmStep{at: at, role: pmRes, own: own, sets: []string{state.GroupPoints, state.GroupResult}, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupPoints, state.GroupResult)
		w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, win, a, b
		if own != nil {
			w.Basis = &state.ScoreBasis{Own: own}
		}
		return pmWrite(h, w)
	}}
}

// pmBareInd is a finish whose payload carries the points but names result alone.
func pmBareInd(at int64, win string, a, b []string) pmStep {
	return pmStep{at: at, role: pmRes, sets: []string{state.GroupResult}, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupResult)
		w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, win, a, b
		return pmWrite(h, w)
	}}
}

// pmBareFin is a finish that names result alone and carries no scoreline: it
// stands on the taps in its basis.
func pmBareFin(at int64, win string, own ...string) pmStep {
	return pmStep{at: at, role: pmRes, own: own, sets: []string{state.GroupResult}, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupResult)
		w.Status, w.Winner = state.MatchStatusCompleted, win
		w.Basis = &state.ScoreBasis{Own: own}
		return pmWrite(h, w)
	}}
}

// pmBareTeam is a team finish whose payload carries bouts but names result alone.
func pmBareTeam(at int64, win string, subs ...state.SubMatchResult) pmStep {
	return pmStep{at: at, role: pmRes, sets: []string{state.GroupResult}, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupResult)
		w.Status, w.Winner, w.SubResults = state.MatchStatusCompleted, win, subs
		return pmWrite(h, w)
	}}
}

func pmDec(at int64, d, by string) pmStep {
	return pmStep{at: at, role: pmRes, dec: true, by: by,
		sets: []string{state.GroupPoints, state.GroupResult, state.GroupEncho, pmAnyBout},
		run: func(h mmHome) string {
			res, _, e := h.eng.RecordDecision(h.compID, h.matchID, d, by, "", nil, at)
			return pmWriteAnswer(res, e)
		}}
}

func pmHik(at int64) pmStep {
	return pmStep{at: at, role: pmRes, sets: []string{state.GroupResult}, run: func(h mmHome) string {
		return pmAnswer(h.write(&state.MatchResult{ID: h.matchID, SideA: wrTeamA, SideB: wrTeamB, SideAID: wrTeamAID, SideBID: wrTeamBID,
			Status: state.MatchStatusCompleted, Decision: "hikiwake", ModifiedAt: at,
			Changed: []string{state.GroupResult}, WriteDoor: DoorDecision}))
	}}
}

// pmEngiFin is an engi finish: the flag count and the verdict it decides.
func pmEngiFin(at int64, flagsA, flagsB int, own ...string) pmStep {
	return pmStep{at: at, role: pmRes, own: own, sets: []string{state.GroupResult, state.GroupFlags}, run: func(h mmHome) string {
		w := mmEngiFinish(h, at, flagsA, flagsB)
		if own != nil {
			w.Basis = &state.ScoreBasis{Own: own}
		}
		return pmWrite(h, w)
	}}
}

// pmMarkID is the id the server's match gives side A's first mark now: what a
// device's answer shows it, and what a later removal names.
func pmMarkID(h mmHome) string {
	m, err := h.eng.lookupExistingResult(h.store, h.compID, h.matchID)
	if err != nil || m.MarkIDs == nil || len(m.MarkIDs.A) == 0 {
		return "no-mark"
	}
	return m.MarkIDs.A[0]
}

func pmBout(at int64, pos int, a, b []string, win string) pmStep {
	return pmStep{at: at, role: pmTap, run: func(h mmHome) string {
		w := mmRunning(h, at, state.BoutGroup(pos))
		w.SubResults = []state.SubMatchResult{{Position: pos, SideA: wrTeamA, SideB: wrTeamB, IpponsA: a, IpponsB: b, Winner: win}}
		return pmWrite(h, w)
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
	sets := []string{state.GroupResult}
	for _, s := range subs {
		sets = append(sets, state.BoutGroup(s.Position))
	}
	return pmStep{at: at, role: pmRes, own: own, sets: sets, run: func(h mmHome) string {
		w := mmRunning(h, at, sets...)
		w.Status, w.Winner, w.SubResults = state.MatchStatusCompleted, win, subs
		if own != nil {
			w.Basis = &state.ScoreBasis{Own: own}
		}
		return pmWrite(h, w)
	}}
}

// pmEvent is one ippon tap through the score-events door.
func pmEvent(id, sess string, seq int64, at int64, pos int, side, letter string) pmStep {
	return pmEventOf(domain.ScoreEvent{ID: id, Session: sess, Seq: seq, Stamp: at, Kind: domain.ScoreIpponAdd, Pos: pos, Side: side, Letter: letter})
}

// pmEventOf is a tap of any kind on the score-events door; its stamp is the
// step's.
func pmEventOf(ev domain.ScoreEvent) pmStep {
	return pmStep{at: ev.Stamp, role: pmEv, id: ev.ID, ev: ev, run: func(h mmHome) string {
		if err := ev.Validate(); err != nil {
			return "invalid:" + err.Error()
		}
		r, err := h.eng.RecordScoreEvents(h.compID, h.matchID, ScoreEventsRequest{Session: ev.Session, Events: []domain.ScoreEvent{ev}})
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
	// landsAfter marks a team match closed by a match-level default-win
	// decision (operator decision 2026-10-16, T1): the bouts a device that
	// never saw it scored keep their scores, whichever way they arrive, so
	// a bout row or tap stamped after the decision LANDS.
	landsAfter bool
	// causal lists pairs [i, j] of steps where j was made on a screen that
	// showed i (a correction after the result it corrects), so j never
	// arrives before i.
	causal [][2]int
	// engi runs the case in an engi (flag-scored) competition.
	engi bool
	// outs pins the final replay outcome of named taps, on top of the rule
	// pmWantOutcome derives for every tap.
	outs map[string]string
}

// pmHome is a fresh match for a case: a team knockout gets the same next round
// an individual one has, so the winner advancing can be checked, and a team
// match may start with its bout rows drawn.
func pmHome(t *testing.T, c pmCase, ko bool) mmHome {
	var h mmHome
	switch {
	case c.team:
		h = mmTeam(t, ko)
	case c.engi:
		h = mmEngi(t, ko)
	default:
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
func pmArrivalCheck(c pmCase, s pmStep, arrived []pmStep, ans string) string {
	closed := false
	var results []pmStep
	for _, a := range arrived {
		if !pmIsResult(a) {
			continue
		}
		closed = closed || a.at < s.at
		results = append(results, a)
	}
	switch s.role {
	case pmEv:
		// A team match closed by a default-win decision keeps the bouts a
		// device that never saw it scored (landsAfter): the tap lands.
		if c.landsAfter {
			// On a team match with no bout rows drawn yet, a tap that comes
			// before the withdrawal has no bout to land on (noTarget); the
			// withdrawal then pads the rows and the replay lands it.
			beforeAnyResult := len(results) == 0 && !c.rows && ans == "ev:"+domain.ScoreOutcomeNoTarget
			if ans != "ev:"+domain.ScoreOutcomeApplied && !beforeAnyResult {
				return fmt.Sprintf("a tap stamped %d (landsAfter) answered %q", s.at, ans)
			}
			break
		}
		if closed != (ans == "ev:"+domain.ScoreOutcomeMatchClosed) {
			return fmt.Sprintf("a tap stamped %d with a result before it arrived=%v answered %q", s.at, closed, ans)
		}
	case pmTap:
		if c.landsAfter {
			if ans != "applied" {
				return fmt.Sprintf("a bout row stamped %d (landsAfter) answered %q", s.at, ans)
			}
			break
		}
		if closed != (ans == "superseded:"+state.HeldReasonMatchClosed) {
			return fmt.Sprintf("a scoring write stamped %d with a result before it arrived=%v answered %q", s.at, closed, ans)
		}
	case pmRes, pmCorr:
		// The FIRST recorded result stands (I1): in stamp order a later
		// result from a device that did not see it is refused; only a
		// correction that saw it replaces it.
		stands := pmStanding(append(results, s))
		refused := ans == "superseded:"+state.HeldReasonMatchClosed || strings.HasPrefix(ans, "partial")
		if (stands.at == s.at && stands.role == s.role) == refused {
			return fmt.Sprintf("a result stamped %d arriving after %d result(s): standing=%v answered %q", s.at, len(results), stands.at == s.at, ans)
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
		want := pmWantOutcome(c, s)
		if pinned, ok := c.outs[s.id]; ok {
			// The case's own table: it states the rule's outcome for a tap
			// whose kind has a target of its own (noTarget) and must agree
			// with the derived one everywhere else.
			if pinned != want && pinned != domain.ScoreOutcomeNoTarget {
				bad = append(bad, fmt.Sprintf("tap %s: the case pins %s, the rule gives %s", s.id, pinned, want))
			}
			want = pinned
		}
		if got := outs[s.id]; got != want {
			bad = append(bad, fmt.Sprintf("tap %s should end %s, ended %q", s.id, want, got))
		}
	}
	return bad
}

// pmAnyBout stands for every numbered bout's group in a result's sets: a team
// decision pads whichever bouts have no result.
const pmAnyBout = "*bout"

// pmWantOutcome is the ONE outcome the rule gives a tap, whichever order the
// writes arrive in. The FIRST recorded result (by stamp) governs it: stamped
// before the tap, it closes the match (matchClosed; a team match closed by a
// withdrawal keeps the bouts scored elsewhere, landsAfter, so the tap is
// applied). A tap stamped before it, in a group the result does not set, stands
// (applied). In a group it sets: around a decision the loser's own side is kept
// (recorded) and the winner's is replaced (beforeBaseline); otherwise the
// result's basis decides (recorded when it holds the tap, else beforeBaseline).
func pmWantOutcome(c pmCase, tap pmStep) string {
	var first *pmStep
	for i := range c.steps {
		if a := &c.steps[i]; pmIsResult(*a) && (first == nil || a.at < first.at) {
			first = a
		}
	}
	if first == nil {
		return domain.ScoreOutcomeApplied
	}
	if first.at < tap.at {
		if c.landsAfter {
			return domain.ScoreOutcomeApplied
		}
		return domain.ScoreOutcomeMatchClosed
	}
	g := ScoreEventGroup(&tap.ev)
	sets := slices.Contains(first.sets, g) || (strings.HasPrefix(g, "bout:") && slices.Contains(first.sets, pmAnyBout))
	if !sets {
		return domain.ScoreOutcomeApplied
	}
	if first.dec && g == state.GroupPoints && (tap.ev.Side == domain.ScoreSideA || tap.ev.Side == domain.ScoreSideB) {
		loser := map[string]string{"aka": domain.ScoreSideA, "shiro": domain.ScoreSideB}[first.by]
		if loser != "" {
			if tap.ev.Side == loser {
				return domain.ScoreOutcomeRecorded
			}
			return domain.ScoreOutcomeBeforeBaseline
		}
	}
	if slices.Contains(first.own, tap.id) {
		return domain.ScoreOutcomeRecorded
	}
	return domain.ScoreOutcomeBeforeBaseline
}

// pmHistoryCheck asserts the match's history after one arrival order: every
// stamped write left a line of its own (the door, its outcome, its reason), an
// event's lines chain from its answer to its final outcome with one line per
// move, and a matchClosed line always carries the closed-match reason.
func pmHistoryCheck(c pmCase, h mmHome, t *testing.T, order []int, answers []string) []string {
	entries := h.history(t)
	outs := pmOutcomes(h)
	var bad []string
	for n, i := range order {
		s, ans := c.steps[i], answers[n]
		if s.at == 0 {
			continue
		}
		var mine []state.MatchHistoryEntry
		for _, e := range entries {
			switch {
			case s.role == pmEv && e.Event != nil && e.Event.ID == s.id:
				mine = append(mine, e)
			case s.role != pmEv && e.Event == nil && e.Stamp == s.at:
				mine = append(mine, e)
			}
		}
		if len(mine) == 0 {
			bad = append(bad, fmt.Sprintf("step %d (stamp %d) left no history line, answered %q", i, s.at, ans))
			continue
		}
		if s.role == pmEv {
			if e := mine[0]; !strings.HasSuffix(ans, ":"+e.EventOutcome) || e.Door != DoorEvent {
				bad = append(bad, fmt.Sprintf("tap %s answered %q but its first line reads door=%s outcome=%s", s.id, ans, e.Door, e.EventOutcome))
			}
			if last := mine[len(mine)-1]; last.EventOutcome != outs[s.id] {
				bad = append(bad, fmt.Sprintf("tap %s ended %q but its last history line reads %q (%d lines)", s.id, outs[s.id], last.EventOutcome, len(mine)))
			}
			for k := 1; k < len(mine); k++ {
				if mine[k].EventOutcome == mine[k-1].EventOutcome {
					bad = append(bad, fmt.Sprintf("tap %s has two lines in a row reading %q", s.id, mine[k].EventOutcome))
				}
			}
			for _, e := range mine {
				if e.EventOutcome == domain.ScoreOutcomeMatchClosed && e.Reason != state.HeldReasonMatchClosed {
					bad = append(bad, fmt.Sprintf("tap %s has a matchClosed line with reason %q, want %q", s.id, e.Reason, state.HeldReasonMatchClosed))
				}
			}
			continue
		}
		refused := ans == "superseded:"+state.HeldReasonMatchClosed
		applied, echoed, why := false, false, true
		for _, e := range mine {
			for _, o := range e.Outcomes {
				applied = applied || o == state.HistoryOutcomeApplied
				echoed = echoed || o == state.HistoryOutcomeUnchanged
			}
			why = why && e.Reason != ""
		}
		switch {
		case refused && applied:
			bad = append(bad, fmt.Sprintf("step %d (stamp %d) was refused (%q) but its history line reads applied", i, s.at, ans))
		case refused && !why:
			bad = append(bad, fmt.Sprintf("step %d (stamp %d) was refused (%q) and its history line gives no reason", i, s.at, ans))
		case ans == "applied" && !applied && !echoed:
			bad = append(bad, fmt.Sprintf("step %d (stamp %d) was applied but its history line reads held", i, s.at))
		}
	}
	return bad
}

// pmBeforeResultOutcomes is the final outcome of each tap stamped before some
// result and after none.
func pmBeforeResultOutcomes(c pmCase, h mmHome) map[string]string {
	outs := pmOutcomes(h)
	got := map[string]string{}
	for _, s := range c.steps {
		if s.role != pmEv {
			continue
		}
		closed, later := false, false
		for _, a := range c.steps {
			if pmIsResult(a) {
				closed = closed || a.at < s.at
				later = later || a.at > s.at
			}
		}
		if !closed && later {
			got[s.id] = outs[s.id]
		}
	}
	return got
}

// pmIsResult is a step that records a finish or a decision.
func pmIsResult(s pmStep) bool { return s.role == pmRes || s.role == pmCorr }

// pmCausal reports whether an arrival order keeps every causal pair: a
// correction made on a screen that showed a result never arrives before it.
func pmCausal(c pmCase, order []int) bool {
	for _, pair := range c.causal {
		if slices.Index(order, pair[0]) > slices.Index(order, pair[1]) {
			return false
		}
	}
	return true
}

// pmStanding is the result that stands among results (the first recorded in
// stamp order, a correction that saw it replacing it): the closed-match rule's
// "first recorded result stands" (operator decision 2026-10-16, I1).
func pmStanding(results []pmStep) pmStep {
	rs := slices.Clone(results)
	slices.SortStableFunc(rs, func(a, b pmStep) int { return int(a.at - b.at) })
	stands := rs[0]
	for _, r := range rs[1:] {
		if r.role == pmCorr {
			stands = r
		}
	}
	return stands
}

// pmDecC is a decision from a screen that showed the recorded result: it
// carries a correction reason, so it is an explicit correction, not a rival.
func pmDecC(at int64, d, by string) pmStep {
	return pmStep{at: at, role: pmCorr, run: func(h mmHome) string {
		res, _, e := h.eng.RecordDecisionWithOptions(h.compID, h.matchID, d, by, "", nil, ForceOptions{Basis: &state.ScoreBasis{Seen: 1 << 20}}, at)
		return pmWriteAnswer(res, e)
	}}
}

// pmFinC is a finish from a screen that showed the recorded result: it
// carries a correction reason (and, with clear, clearWithdrawal).
func pmFinC(at int64, win string, a, b []string, clear bool) pmStep {
	return pmStep{at: at, role: pmCorr, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupPoints, state.GroupResult)
		w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, win, a, b
		w.CorrectionReason, w.ClearsWithdrawal = "operator correction", clear
		return pmWrite(h, w)
	}}
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
	for _, c := range append(pmCases(), pmKindCases()...) {
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
				tapOuts := map[string]string{}
				for _, p := range pmPerms(len(c.steps)) {
					if !pmCausal(c, p) {
						continue
					}
					h := pmHome(t, c, ko)
					if c.pre.run != nil {
						require.Equal(t, "applied", c.pre.run(h), "pre")
					}
					var arrived []pmStep
					answers := ""
					var answerList []string
					for _, i := range p {
						s := c.steps[i]
						ans := s.run(h)
						answers += ans + ";"
						answerList = append(answerList, ans)
						if why := pmArrivalCheck(c, s, arrived, ans); why != "" {
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
					for _, bad := range pmHistoryCheck(c, h, t, p, answerList) {
						t.Errorf("order %v: history: %s", p, bad)
					}
					// A tap stamped before the result has ONE outcome, whichever
					// order the writes arrive in.
					for id, got := range pmBeforeResultOutcomes(c, h) {
						if first, seen := tapOuts[id]; seen && first != got {
							t.Errorf("order %v: tap %s ended %q, another order ended %q", p, id, got, first)
						}
						tapOuts[id] = got
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
		// A finish over a decision, and the other way round: the first recorded one stands.
		{name: "dai aka T1 / fin Ryu M-0 T2", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmFin(t2, wrTeamA, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora"`, `d="daihyosen" by="aka"`}},
		{name: "fin Ryu M-0 T1 / dai aka T2", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), pmDec(t2, "daihyosen", "aka")}, refused: []int{1}, want: []string{`st=completed`, ryu, `d="" by=""`, `A=[M] B=[]`}},
		{name: "pt M-0 T1 / dai aka T2", steps: []pmStep{pmPt(t1, M, nil), pmDec(t2, "daihyosen", "aka")}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		// A withdrawal closes the match: points after it are refused.
		{name: "kiken aka T1 / pt 0-M T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmPt(t2, nil, M)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `cs=[`}},
		{name: "kiken aka T1 / pt M-0 T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmPt(t2, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`}},
		{name: "fusensho aka T1 / pt M-0 T2", steps: []pmStep{pmDec(t1, "fusensho", "aka"), pmPt(t2, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="fusensho" by="aka"`}},
		{name: "fin Ryu M-0 T1 / fusensho shiro T2", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), pmDec(t2, "fusensho", "shiro")}, refused: []int{1}, want: []string{`st=completed`, ryu, `d="" by=""`, `A=[M] B=[]`}},
		{name: "hik T1 / pt M-0 T2", steps: []pmStep{pmHik(t1), pmPt(t2, M, nil)}, refused: []int{1}, want: tie, where: "pool"},
		{name: "hik T1 / pt M-K T2", steps: []pmStep{pmHik(t1), pmPt(t2, M, K)}, refused: []int{1}, want: tie, where: "pool"},
		{name: "fin Ht-0 T1 / pt 0-0 T2", steps: []pmStep{pmFin(t1, wrTeamA, []string{"Ht"}, nil), pmPt(t2, nil, nil)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "fin Ht-0 T1 / pt 0-Ht T2", steps: []pmStep{pmFin(t1, wrTeamA, []string{"Ht"}, nil), pmPt(t2, nil, []string{"Ht"})}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "pt 0-0 T1 / fin Ht T2", pre: pmPt(mmT0+1, M, nil), steps: []pmStep{pmPt(t1, nil, nil), pmFin(t2, wrTeamA, []string{"Ht"}, nil)}, want: []string{`st=completed`, ryu}},
		{name: "team dai aka T1 / bout1 A M T2", team: true, steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmBout(t2, 1, M, nil, wrTeamA)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "team dai shiro T1 / bout1 B M T2", team: true, steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmBout(t2, 1, nil, M, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "team bouts favour B, dai shiro T1 / bout3 B T2", team: true, pre: preTeam, steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmBout(t2, 3, nil, K, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		// Operator decision 2026-10-16 (T1), a team match closed by a match-level
		// default-win decision merges BY BOUT: a bout scored on a device that never
		// saw the decision keeps its score, the decision credits the bouts still
		// without a result and decides the match.
		{name: "team kiken aka T1 / bout1 B T2 (the bout lands)", team: true, landsAfter: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmBout(t2, 1, nil, M, wrTeamB)}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 []-[M] w="Tora" d=""]`}},
		{name: "team kiken aka T1 / bout1 A T2 (the bout lands)", team: true, landsAfter: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmBout(t2, 1, M, nil, wrTeamA)}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 [M]-[] w="Ryu" d=""]`}},
		{name: "team kiken aka T1 / b1 A T2 / b2 B T3 (both bouts land)", team: true, landsAfter: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmBout(t2, 1, M, nil, wrTeamA), pmBout(t3, 2, nil, M, wrTeamB)}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 [M]-[] w="Ryu" d=""]`, `[2 []-[M] w="Tora" d=""]`}},
		{name: "team kiken aka T1 / teamFin Ryu(b1 A) T2 (result refused, bout lands)", team: true, landsAfter: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmTeamFin(t2, wrTeamA, nil, b(1, M, nil, wrTeamA))}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 [M]-[] w="Ryu" d=""]`}},
		{name: "team fusenpai aka T1 / teamFin Ryu(b1 A, b2 A) T2", team: true, landsAfter: true, steps: []pmStep{pmDec(t1, "fusenpai", "aka"), pmTeamFin(t2, wrTeamA, nil, b(1, M, nil, wrTeamA), b(2, M, nil, wrTeamA))}, want: []string{`st=completed`, `w="Tora" d="fusenpai" by="aka"`, `[1 [M]-[] w="Ryu" d=""]`, `[2 [M]-[] w="Ryu" d=""]`}},
		{name: "team kiken aka T1 / teamFin Tora(b1 B) T2", team: true, landsAfter: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmTeamFin(t2, wrTeamB, nil, b(1, nil, M, wrTeamB))}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 []-[M] w="Tora" d=""]`}},
		{name: "team fin Ryu(b1 A) T1 / b2 B T2", team: true, steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmBout(t2, 2, nil, M, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "team fin Ryu(b1 A) T1 / b1 B T2 flip", team: true, steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmBout(t2, 1, nil, K, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "team pre b1A b2B; fin Ryu(dec only) T1 / b3 B T2", team: true, pre: preTeam, steps: []pmStep{pmTeamFin(t1, wrTeamA, nil), pmBout(t2, 3, nil, K, wrTeamB)}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "team fusensho aka T1 / b1 A T2 (the bout lands)", team: true, landsAfter: true, steps: []pmStep{pmDec(t1, "fusensho", "aka"), pmBout(t2, 1, M, nil, wrTeamA)}, want: []string{`st=completed`, `w="Tora" d="fusensho" by="aka"`, `[1 [M]-[] w="Ryu" d=""]`}},
		// Two results: the first recorded one stands, whichever arrives first.
		{name: "kiken aka T1 -> dai shiro T2", steps: []pmStep{pmDec(t1, "kiken-injury", "aka"), pmDec(t2, "daihyosen", "shiro")}, refused: []int{1}, want: []string{`st=completed`, `d="kiken-injury" by="aka"`, `cs=[`}},
		{name: "dai shiro T1 -> dai aka T2 (correction, saw it)", steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmDecC(t2, "daihyosen", "aka")}, causal: [][2]int{{0, 1}}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "team kiken aka T1 -> dai shiro T2", team: true, steps: []pmStep{pmDec(t1, "kiken-injury", "aka"), pmDec(t2, "daihyosen", "shiro")}, refused: []int{1}, want: []string{`st=completed`, `d="kiken-injury" by="aka"`, `cs=[`}},
		// The older decision arriving after the newer one (round-4 finding 8): it is the first result, so it stands.
		{name: "kiken aka T2 / dai shiro T1 (the first stands)", steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmDec(t2, "kiken-injury", "aka")}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`, `cs=[]`}},
		{name: "dai shiro T2 / dai aka T1 (the first stands)", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmDec(t2, "daihyosen", "shiro")}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "team kiken aka T2 / dai shiro T1 (the first stands)", team: true, steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmDec(t2, "kiken-injury", "aka")}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`, `cs=[]`}},
		{name: "team dai shiro T2 / dai aka T1 (the first stands)", team: true, steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmDec(t2, "daihyosen", "shiro")}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},

		// ---- Mixed: taps through the score-events door and finishes or
		// decisions that carry a basis, the production mix after stage 3. ----
		// Finding 1: a decision stamped before taps that arrived first must not double their marks.
		{name: "mix ind dai shiro T1 / e a:M T2 / e b:K T3", steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmEvent("e1", "d1", 1, t2, 0, "a", "M"), pmEvent("e2", "d1", 2, t3, 0, "b", "K")}, refused: []int{1, 2}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "mix ind dai aka T1 / e a:M T2", steps: []pmStep{pmDec(t1, "daihyosen", "aka"), pmEvent("e1", "d1", 1, t2, 0, "a", "M")}, refused: []int{1}, want: []string{`st=completed`, `d="daihyosen" by="aka"`}},
		{name: "mix team dai shiro T1 / e b1 a:M T2 / e b2 b:K T3", team: true, rows: true, steps: []pmStep{pmDec(t1, "daihyosen", "shiro"), pmEvent("e1", "d1", 1, t2, 1, "a", "M"), pmEvent("e2", "d1", 2, t3, 2, "b", "K")}, refused: []int{1, 2}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "mix ind kiken aka T1 / e a:M T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d1", 1, t2, 0, "a", "M")}, refused: []int{1}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`, `cs=[`}},
		{name: "mix ind fusensho shiro T1 / e a:M T2", steps: []pmStep{pmDec(t1, "fusensho", "shiro"), pmEvent("e1", "d1", 1, t2, 0, "a", "M")}, refused: []int{1}, want: []string{`st=completed`, `d="fusensho" by="shiro"`}},
		{name: "mix team kiken aka T1 / e b1 a:M T2 (the bout lands)", team: true, rows: true, landsAfter: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d1", 1, t2, 1, "a", "M")}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 [M]-[] w="Ryu" d=""]`}},
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
		{name: "mix team kiken aka T1, e b1 a:M T2, e b2 b:M T3 (both bouts land)", team: true, rows: true, landsAfter: true, steps: []pmStep{
			pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d1", 1, t2, 1, "a", "M"), pmEvent("e2", "d1", 2, t3, 2, "b", "M")},
			want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 [M]-[] w="Ryu" d=""]`, `[2 []-[M] w="Tora" d=""]`}},
		{name: "mix team dai shiro T1, e b1 a:M T2, e b2 b:M T3", team: true, rows: true, steps: []pmStep{
			pmDec(t1, "daihyosen", "shiro"), pmEvent("e1", "d1", 1, t2, 1, "a", "M"), pmEvent("e2", "d1", 2, t3, 2, "b", "M")},
			refused: []int{1, 2}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		// Round 5: a tap stamped before a decision that did not see it ends the
		// same whichever arrives first: the loser's side is in the stored
		// scoreline (recorded), the winner's is replaced by the decision's marks.
		{name: "mix ind e a:M T1 / kiken aka T2 (loser's tap)", steps: []pmStep{pmEvent("e1", "d1", 1, t1, 0, "a", "M"), pmDec(t2, "kiken-voluntary", "aka")}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`, `A=[M]`}},
		{name: "mix ind e b:M T1 / kiken aka T2 (winner's tap)", steps: []pmStep{pmEvent("e1", "d1", 1, t1, 0, "b", "M"), pmDec(t2, "kiken-voluntary", "aka")}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`}},
		{name: "mix ind e b:M T1 / dai shiro T2 (loser's tap)", steps: []pmStep{pmEvent("e1", "d1", 1, t1, 0, "b", "M"), pmDec(t2, "daihyosen", "shiro")}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		{name: "mix ind e a:M T1 / dai shiro T2 (winner's tap)", steps: []pmStep{pmEvent("e1", "d1", 1, t1, 0, "a", "M"), pmDec(t2, "daihyosen", "shiro")}, want: []string{`st=completed`, `d="daihyosen" by="shiro"`}},
		// Two results with a tap between them.
		// Operator decision 2026-10-16 (I1): the FIRST recorded result stands. A
		// finish or decision stamped after it from a device that did not see it
		// is refused (match_closed) and kept in the history, in every arrival
		// order. A write that saw the result (a correction) replaces it as before.
		{name: "I1 kiken aka T1 / fin Ryu M-0 T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmFin(t2, wrTeamA, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `cs=[`}},
		{name: "I1 kiken aka T1 / fin Tora 0-M T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmFin(t2, wrTeamB, nil, M)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `cs=[`}},
		{name: "I1 fusenpai aka T1 / fin Ryu M-0 T2", steps: []pmStep{pmDec(t1, "fusenpai", "aka"), pmFin(t2, wrTeamA, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="fusenpai" by="aka"`, `cs=[`}},
		{name: "I1 fusensho aka T1 / fin Ryu M-0 T2", steps: []pmStep{pmDec(t1, "fusensho", "aka"), pmFin(t2, wrTeamA, M, nil)}, refused: []int{1}, want: []string{`st=completed`, `w="Tora" d="fusensho" by="aka"`}},
		{name: "I1 fusensho shiro T1 / fin Tora 0-M T2", steps: []pmStep{pmDec(t1, "fusensho", "shiro"), pmFin(t2, wrTeamB, nil, M)}, refused: []int{1}, want: []string{`st=completed`, `w="Ryu" d="fusensho" by="shiro"`}},
		{name: "I1 fin Ryu M-0 T1 / fin Tora 0-M T2 (two finishes)", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), pmFin(t2, wrTeamB, nil, M)}, refused: []int{1}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "I1 fin Ryu M-0 T1 / fin Ryu MK-0 T2 (same winner)", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), pmFin(t2, wrTeamA, MK, nil)}, refused: []int{1}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "I1 kiken aka T1 / e b:K T1.5 / fin Ryu M-0 T2", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d1", 1, half(1), 0, "b", "K"), pmFin(t2, wrTeamA, M, nil)}, refused: []int{1, 2}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `cs=[`}},
		{name: "I1 e a:M T0.5 / kiken aka T1 / e b:K T1.5 / fin Ryu M-0 T2", steps: []pmStep{pmEvent("e0", "d0", 1, mmT0+30_000, 0, "a", "M"), pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d1", 1, half(1), 0, "b", "K"), pmFin(t2, wrTeamA, M, nil)}, refused: []int{2, 3}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `A=[M]`}},
		{name: "I1 kiken aka T1 / fin Ryu M-0 T2 / pt M-K T3", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmFin(t2, wrTeamA, M, nil), pmPt(t3, M, K)}, refused: []int{1, 2}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `cs=[`}},
		{name: "I1 kiken aka T1 / correction fin Ryu M-0 T2 keeps the kiken", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmFinC(t2, wrTeamA, M, nil, false)}, causal: [][2]int{{0, 1}}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `cs=[`}},
		{name: "I1 kiken aka T1 / correction fin Ryu M-0 clearWithdrawal T2 replaces it", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmFinC(t2, wrTeamA, M, nil, true)}, causal: [][2]int{{0, 1}}, want: []string{`st=completed`, ryu, `d="" by=""`, `cs=[]`}},
		{name: "I1 fin Ryu M-0 T1 / correction fin Tora 0-M T2", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), pmFinC(t2, wrTeamB, nil, M, false)}, causal: [][2]int{{0, 1}}, want: []string{`st=completed`, `w="Tora"`, `A=[] B=[M]`}},
		{name: "I1 team fin Ryu(b1 A) T1 / teamFin Tora(b2 B) T2 (a finish closes the bouts)", team: true, steps: []pmStep{pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, wrTeamA)), pmTeamFin(t2, wrTeamB, nil, b(2, nil, M, wrTeamB))}, refused: []int{1}, want: []string{`st=completed`, ryu}},
		{name: "T1 team pre b1 A; kiken aka T1 / b2 B T2", team: true, landsAfter: true, pre: preB1, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmBout(t2, 2, nil, M, wrTeamB)}, want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 [M]-[] w="Ryu" d=""]`, `[2 []-[M] w="Tora" d=""]`}},
		{name: "mix ind kiken aka T1 / e b:M T2 / dai shiro T3", steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), pmEvent("e1", "d2", 1, t2, 0, "b", "M"), pmDec(t3, "daihyosen", "shiro")}, refused: []int{1, 2}, want: []string{`st=completed`, `d="kiken-voluntary" by="aka"`, `cs=[`}},
	}
}

// pmKindCases are the matrix cases for every kind of tap beyond a strike
// (ippon.remove by mark id, foul, overtime, engi flag, team bout decision), each
// around a finish or alone, plus the cases of review round 6: a mark's id
// across arrival orders and a tap that reaches a match with no bout rows
// before the withdrawal that pads them.
func pmKindCases() []pmCase {
	M := []string{"M"}
	b := func(pos int, a, bb []string, w string) state.SubMatchResult {
		return state.SubMatchResult{Position: pos, SideA: fmt.Sprintf("A%d", pos), SideB: fmt.Sprintf("B%d", pos), IpponsA: a, IpponsB: bb, Winner: w}
	}
	t1, t2, t3 := mmT1, mmT2, mmT3
	e := func(id string, seq, at int64, kind domain.ScoreEventKind, pos int, side, letter, target, value string) pmStep {
		return pmEventOf(domain.ScoreEvent{ID: id, Session: "d1", Seq: seq, Stamp: at, Kind: kind, Pos: pos, Side: side, Letter: letter, Target: target, Value: value})
	}
	ryu := `w="Ryu"`
	seen := new(string)
	markA := pmStep{at: t2, role: pmTap, run: func(h mmHome) string {
		a := pmPt(t2, M, nil).run(h)
		*seen = pmMarkID(h)
		return a
	}}
	removeSeen := func(at int64) pmStep {
		ev := domain.ScoreEvent{ID: "r1", Session: "d2", Seq: 1, Stamp: at, Kind: domain.ScoreIpponRemove, Target: "unset"}
		s := pmEventOf(ev)
		s.run = func(h mmHome) string {
			ev := ev
			ev.Target = *seen
			return pmEventOf(ev).run(h)
		}
		return s
	}
	hID := new(string)
	secondFoul := e("e2", 2, t2, domain.ScoreFoulAdd, 0, "a", "", "", "")
	foulRun := secondFoul.run
	secondFoul.run = func(h mmHome) string {
		a := foulRun(h)
		if m, err := h.eng.lookupExistingResult(h.store, h.compID, h.matchID); err == nil && m.MarkIDs != nil && len(m.MarkIDs.B) > 0 {
			*hID = m.MarkIDs.B[0]
		}
		return a
	}
	removeH := func(at int64) pmStep {
		s := pmEventOf(domain.ScoreEvent{ID: "r1", Session: "d2", Seq: 1, Stamp: at, Kind: domain.ScoreIpponRemove, Target: "unset"})
		s.run = func(h mmHome) string {
			return pmEventOf(domain.ScoreEvent{ID: "r1", Session: "d2", Seq: 1, Stamp: at, Kind: domain.ScoreIpponRemove, Target: *hID}).run(h)
		}
		return s
	}
	return []pmCase{
		// The H a second foul awards is a mark like any other: removed by its id.
		{name: "kind ind foul a T1 / foul a T2 (H to b) / remove the H T3 by its id", steps: []pmStep{e("e1", 1, t1, domain.ScoreFoulAdd, 0, "a", "", "", ""), secondFoul, removeH(t3)},
			causal: [][2]int{{0, 1}, {1, 2}}, want: []string{`st=running`, `B=[]`}, outs: map[string]string{"r1": "applied"}},
		// A team kiken, then a bout decision of a device that never saw it.
		{name: "kind team kiken aka T1 / hikiwake b1 T2 (the bout lands)", team: true, rows: true, landsAfter: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), e("e1", 1, t2, domain.ScoreBoutDecision, 1, "", "", "", domain.BoutDecisionHikiwake)},
			want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 []-[] w="" d="hikiwake"]`, `[2 []-[] w="" d=""]`}, outs: map[string]string{"e1": "applied"}},
		// ippon.remove by the id of a mark.
		{name: "kind ind e1 add a:M T1, e2 remove e1 T2", steps: []pmStep{e("e1", 1, t1, domain.ScoreIpponAdd, 0, "a", "M", "", ""), e("e2", 2, t2, domain.ScoreIpponRemove, 0, "", "", "e1", "")},
			want: []string{`st=running`, `A=[] B=[]`}, outs: map[string]string{"e1": "applied", "e2": "applied"}},
		{name: "kind ind e1 add a:M T1, e2 remove e1 T2, fin Ryu M-0 T3 own e1 e2", steps: []pmStep{e("e1", 1, t1, domain.ScoreIpponAdd, 0, "a", "M", "", ""), e("e2", 2, t2, domain.ScoreIpponRemove, 0, "", "", "e1", ""), pmFin(t3, wrTeamA, M, nil, "e1", "e2")},
			want: []string{`st=completed`, ryu, `A=[M]`}, outs: map[string]string{"e1": "recorded", "e2": "recorded"}},
		{name: "kind ind e1 add a:M T1, e2 remove e1 T2, fin Ryu M-0 T3 (no basis)", steps: []pmStep{e("e1", 1, t1, domain.ScoreIpponAdd, 0, "a", "M", "", ""), e("e2", 2, t2, domain.ScoreIpponRemove, 0, "", "", "e1", ""), pmFin(t3, wrTeamA, M, nil)},
			want: []string{`st=completed`, ryu, `A=[M]`}, outs: map[string]string{"e1": "beforeBaseline", "e2": "beforeBaseline"}},
		{name: "kind ind fin Ryu M-0 T1, e1 add a:K T2, e2 remove e1 T3", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), e("e1", 1, t2, domain.ScoreIpponAdd, 0, "a", "K", "", ""), e("e2", 2, t3, domain.ScoreIpponRemove, 0, "", "", "e1", "")},
			refused: []int{1, 2}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}, outs: map[string]string{"e1": "matchClosed", "e2": "matchClosed"}},
		// A mark's id is the same in every arrival order, so a removal made
		// from the screen that showed it finds it even when an OLDER save of
		// the same mark arrives later.
		{name: "kind ind pt M T1 / pt M T2 / remove the mark T3 by its id", steps: []pmStep{pmPt(t1, M, nil), markA, removeSeen(t3)},
			causal: [][2]int{{1, 2}}, want: []string{`st=running`, `A=[] B=[]`}, outs: map[string]string{"r1": "applied"}},
		// Fouls.
		{name: "kind ind e1 foul a T1, e2 foul a T2 (a second foul), fin Ryu M-0 T3 own e1 e2", steps: []pmStep{e("e1", 1, t1, domain.ScoreFoulAdd, 0, "a", "", "", ""), e("e2", 2, t2, domain.ScoreFoulAdd, 0, "a", "", "", ""), pmFin(t3, wrTeamA, M, nil, "e1", "e2")},
			want: []string{`st=completed`, ryu}, outs: map[string]string{"e1": "recorded", "e2": "recorded"}},
		{name: "kind ind fin Ryu M-0 T1, foul a T2, foul remove a T3", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), e("e1", 1, t2, domain.ScoreFoulAdd, 0, "a", "", "", ""), e("e2", 2, t3, domain.ScoreFoulRemove, 0, "a", "", "", "")},
			refused: []int{1, 2}, want: []string{`st=completed`, ryu, `A=[M] B=[]`}},
		{name: "kind ind foul a T1, foul remove a T2, foul a T3", steps: []pmStep{e("e1", 1, t1, domain.ScoreFoulAdd, 0, "a", "", "", ""), e("e2", 2, t2, domain.ScoreFoulRemove, 0, "a", "", "", ""), e("e3", 3, t3, domain.ScoreFoulAdd, 0, "a", "", "", "")},
			want: []string{`st=running`}, outs: map[string]string{"e1": "applied", "e2": "applied", "e3": "applied"}},
		// Two fouls land an H on the opponent and reset the count. (A removal
		// stamped after them would find no outstanding foul, but one that
		// reached the server before the second foul applied and moved
		// ModifiedAt to its own stamp, which never lowers: reported, not
		// pinned here.)
		{name: "kind ind foul a T1, foul a T2 (a second foul: H to b)", steps: []pmStep{e("e1", 1, t1, domain.ScoreFoulAdd, 0, "a", "", "", ""), e("e2", 2, t2, domain.ScoreFoulAdd, 0, "a", "", "", "")},
			want: []string{`st=running`, `B=[H]`}, outs: map[string]string{"e1": "applied", "e2": "applied"}},
		// Overtime.
		{name: "kind ind encho add T1, fin Ryu M-0 T2 (overtime is not the finish's)", steps: []pmStep{e("e1", 1, t1, domain.ScoreEnchoAdd, 0, "", "", "", ""), pmFin(t2, wrTeamA, M, nil)},
			want: []string{`st=completed`, ryu}, outs: map[string]string{"e1": "applied"}},
		{name: "kind ind fin Ryu M-0 T1, encho add T2, encho remove T3", steps: []pmStep{pmFin(t1, wrTeamA, M, nil), e("e1", 1, t2, domain.ScoreEnchoAdd, 0, "", "", "", ""), e("e2", 2, t3, domain.ScoreEnchoRemove, 0, "", "", "", "")},
			refused: []int{1, 2}, want: []string{`st=completed`, ryu}},
		{name: "kind ind encho add T1, encho add T2, encho remove T3", steps: []pmStep{e("e1", 1, t1, domain.ScoreEnchoAdd, 0, "", "", "", ""), e("e2", 2, t2, domain.ScoreEnchoAdd, 0, "", "", "", ""), e("e3", 3, t3, domain.ScoreEnchoRemove, 0, "", "", "", "")},
			want: []string{`st=running`}},
		// Engi flags.
		{name: "kind engi flag a T1, flag a T2, fin 3-0 T3 own e1 e2", engi: true, steps: []pmStep{e("e1", 1, t1, domain.ScoreFlagAdd, 0, "a", "", "", ""), e("e2", 2, t2, domain.ScoreFlagAdd, 0, "a", "", "", ""), pmEngiFin(t3, 3, 0, "e1", "e2")},
			want: []string{`st=completed`, ryu}, outs: map[string]string{"e1": "recorded", "e2": "recorded"}},
		{name: "kind engi fin 3-0 T1, flag b T2, flag remove a T3", engi: true, steps: []pmStep{pmEngiFin(t1, 3, 0), e("e1", 1, t2, domain.ScoreFlagAdd, 0, "b", "", "", ""), e("e2", 2, t3, domain.ScoreFlagRemove, 0, "a", "", "", "")},
			refused: []int{1, 2}, want: []string{`st=completed`, ryu}},
		// Team bout decisions.
		{name: "kind team e1 hikiwake b1 T1, e2 fusensho b2 T2, fin Ryu(b1 b2) T3 own e1 e2", team: true, rows: true, steps: []pmStep{
			e("e1", 1, t1, domain.ScoreBoutDecision, 1, "", "", "", domain.BoutDecisionHikiwake), e("e2", 2, t2, domain.ScoreBoutDecision, 2, "b", "", "", domain.BoutDecisionFusensho), pmTeamFin(t3, wrTeamA, []string{"e1", "e2"}, b(1, M, nil, "A1"), b(2, M, nil, "A2"))},
			want: []string{`st=completed`, ryu}, outs: map[string]string{"e1": "recorded", "e2": "recorded"}},
		{name: "kind team fin Ryu(b1) T1, hikiwake b2 T2, clear b2 T3", team: true, rows: true, steps: []pmStep{
			pmTeamFin(t1, wrTeamA, nil, b(1, M, nil, "A1")), e("e1", 1, t2, domain.ScoreBoutDecision, 2, "", "", "", domain.BoutDecisionHikiwake), e("e2", 2, t3, domain.ScoreBoutDecision, 2, "", "", "", domain.BoutDecisionClear)},
			refused: []int{1, 2}, want: []string{`st=completed`, ryu}},
		// A tap stamped after a team withdrawal reaches a match whose bout rows
		// are not drawn yet: it lands whichever arrives first, and the history
		// says so.
		{name: "kind team kiken aka T1 / e b1 a:M T2 (no rows drawn)", team: true, landsAfter: true, steps: []pmStep{pmDec(t1, "kiken-voluntary", "aka"), e("e1", 1, t2, domain.ScoreIpponAdd, 1, "a", "M", "", "")},
			want: []string{`st=completed`, `w="Tora" d="kiken-voluntary" by="aka"`, `[1 [M]-[] w="Ryu" d=""]`}, outs: map[string]string{"e1": "applied"}},
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

// Kachinuki (bc-evts stage 2b, operator decision 2026-10-16, T1): its bouts are
// a LOG with no numbered position to credit, so a closed kachinuki match is
// closed whole, like an individual one -- the closest consistent reading of the
// merge-by-bout rule, which credits only numbered bouts still without a result.
// A bout appended after a kiken from a board that never saw it is turned away
// (and kept in the history), whichever way the two arrive.
func TestMatchWriteKachinukiClosedMatchStaysWhole(t *testing.T) {
	run := func(t *testing.T, kikenFirst bool) (string, string) {
		eng, store, _ := setupKachinukiComp(t, "kach-closed", 3)
		require.NoError(t, store.SavePoolMatches("kach-closed", []state.MatchResult{{
			ID: "Pool A-0", SideA: "Red", SideB: "White", Status: state.MatchStatusRunning, ModifiedAt: mmT0,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "Red", SideB: "White", SideAMemberID: "r1", SideBMemberID: "w1", WinnerMemberID: "r1", Winner: "r1", IpponsA: []string{"M"}}},
		}}))
		h := mmHome{eng: eng, store: store, compID: "kach-closed", matchID: "Pool A-0"}
		kiken := pmDec(mmT1, "kiken-voluntary", "shiro")
		bout := pmStep{at: mmT2, role: pmTap, run: func(h mmHome) string {
			w := &state.MatchResult{ID: "Pool A-0", SideA: "Red", SideB: "White", Status: state.MatchStatusRunning, ModifiedAt: mmT2,
				Changed: []string{state.BoutGroup(2)}, WriteDoor: DoorScore,
				SubResults: []state.SubMatchResult{{Position: 2, SideA: "Red", SideB: "White", SideAMemberID: "r1", SideBMemberID: "w2", WinnerMemberID: "r1", Winner: "r1", IpponsA: []string{"K"}}}}
			return pmWrite(h, w)
		}}
		steps := []pmStep{kiken, bout}
		if !kikenFirst {
			steps = []pmStep{bout, kiken}
		}
		answers := ""
		for _, s := range steps {
			answers += s.run(h) + ";"
		}
		m := wrPoolMatch(t, store, "kach-closed")
		return fmt.Sprintf("%s %s %v", m.Status, m.Decision, state.SubPositions(m.SubResults)), answers
	}
	first, a1 := run(t, true)
	second, a2 := run(t, false)
	assert.Equal(t, first, second, "arrival order never changes the match (%s | %s)", a1, a2)
	assert.Equal(t, "completed kiken-voluntary [1]", first, "the bout appended after the kiken is not in the match")
	assert.Contains(t, a1, "superseded:"+state.HeldReasonMatchClosed, "turned away as match_closed when it arrives second")
}

// TestMatchWriteReopenFastClockTapConverges: a reopen is stamped by the
// server's clock, so a tap whose device clock runs ahead of it is stamped
// LATER than the reopen however it arrives. Whatever order the kiken's reopen
// and the two taps (one stamped between the reopen and the fast clock, one at
// the fast clock) reach the server in, both taps land on the reopened match
// and the reopen's log baseline is not bent to the tap's stamp.
func TestMatchWriteReopenFastClockTapConverges(t *testing.T) {
	base := time.Now().UnixMilli() - 3_600_000
	soon, fut := time.Now().UnixMilli()+60_000, time.Now().UnixMilli()+120_000
	steps := []pmStep{
		pmEvent("e1", "d1", 1, fut, 0, "a", "K"),
		{role: "reopen", run: func(h mmHome) string {
			_, err := h.eng.ReopenMatch(h.compID, h.matchID, "")
			return "reopen:" + pmAnswer(err)
		}},
		pmEvent("e2", "d2", 1, soon, 0, "b", "M"),
	}
	for _, ko := range []bool{false, true} {
		seen := map[string][]string{}
		for _, p := range pmPerms(len(steps)) {
			h := pmHome(t, pmCase{}, ko)
			if ko {
				b, err := h.store.LoadBracket(h.compID)
				require.NoError(t, err)
				b.Rounds[0][0].ModifiedAt = base
				require.NoError(t, h.store.SaveBracket(h.compID, b))
			} else {
				ms, err := h.store.LoadPoolMatches(h.compID)
				require.NoError(t, err)
				ms[0].ModifiedAt = base
				require.NoError(t, h.store.SavePoolMatches(h.compID, ms))
			}
			require.Equal(t, "applied", pmDec(base+60_000, "kiken-voluntary", "aka").run(h))
			for _, i := range p {
				steps[i].run(h)
			}
			if r := pmReplay(h, t); r != "" {
				t.Errorf("ko=%v order %v: stored match differs from a replay of its log: %s", ko, p, r)
			}
			m := h.load(t)
			outs := pmOutcomes(h)
			got := fmt.Sprintf("st=%s A=%v B=%v e1=%s e2=%s", m.Status, m.IpponsA, m.IpponsB, outs["e1"], outs["e2"])
			seen[got] = append(seen[got], fmt.Sprint(p))
			lines, err := h.store.LoadScoreLog(h.compID, h.matchID)
			require.NoError(t, err)
			for _, l := range lines {
				if l.Baseline != nil && l.Baseline.Door == "reopen" && l.Baseline.Stamp >= soon {
					t.Errorf("ko=%v order %v: the reopen's baseline is stamped %d ms into the future", ko, p, l.Baseline.Stamp-time.Now().UnixMilli())
				}
			}
		}
		if len(seen) != 1 {
			t.Errorf("ko=%v: arrival order changed the outcome: %v", ko, seen)
		}
	}
}
