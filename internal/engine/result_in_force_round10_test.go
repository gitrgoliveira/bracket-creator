package engine

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Round 10: ONE owner answers "was a result in force, and did this write's
// basis see THAT line?" (ResultInForceAt, score_replay.go). These tests walk a
// match through result -> next match played -> reopen (or requeue) -> two or
// three devices, in every arrival order, because every earlier lock test
// stopped before the reopen.

// r10Scene is a match closed by a fusensho, optionally with the match it feeds
// played, then reopened (or sent back to the queue).
type r10Scene struct {
	h      mmHome
	logLen int   // lines the log holds right after the reopen
	reopen int64 // a device's clock reading right after it
}

func r10Setup(t *testing.T, ko, requeue bool) r10Scene {
	t.Helper()
	now := time.Now().UnixMilli()
	base := now - 3_600_000
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
	var err error
	if ko {
		// A knockout match has no reopen once it is finished: the walk is a
		// point, then the match sent back to the queue.
		require.Equal(t, "applied", pmPt(base+60_000, []string{"M"}, nil).run(h))
		require.NoError(t, h.eng.RevertMatchToQueue(h.compID, h.matchID))
	} else {
		require.Equal(t, "applied", pmDec(base+60_000, "fusensho", "aka").run(h))
		_, err = h.eng.ReopenMatch(h.compID, h.matchID, "")
		require.NoError(t, err)
		if requeue {
			require.NoError(t, h.eng.RevertMatchToQueue(h.compID, h.matchID))
		}
	}
	lines, err := h.store.LoadScoreLog(h.compID, h.matchID)
	require.NoError(t, err)
	return r10Scene{h: h, logLen: len(lines), reopen: time.Now().UnixMilli()}
}

// r10Finish is a finish at s.reopen+off whose basis, when seen is set, holds
// the first seen lines of the log.
func (s r10Scene) finish(off int64, winner string, a, b []string, seen int) pmStep {
	at := s.reopen + off
	return pmStep{at: at, role: pmRes, sets: []string{state.GroupPoints, state.GroupResult}, run: func(h mmHome) string {
		w := mmRunning(h, at, state.GroupPoints, state.GroupResult)
		w.Status, w.Winner, w.IpponsA, w.IpponsB = state.MatchStatusCompleted, winner, a, b
		if seen > 0 {
			w.Basis = &state.ScoreBasis{Seen: seen}
		}
		return pmWrite(h, w)
	}}
}

func (s r10Scene) point(off int64, a, b []string) pmStep { return pmPt(s.reopen+off, a, b) }

// settled describes where the match ended: its state and every value the
// history kept (held, or displaced out of the match) for the writes made after
// the reopen, whichever line carries it and whatever order the lines came in.
func (s r10Scene) settled(t *testing.T) string {
	t.Helper()
	m := s.h.load(t)
	kept := map[string]bool{}
	for _, e := range s.h.history(t) {
		for g, v := range e.Held {
			kept[fmt.Sprintf("%s=%s", g, v)] = true
		}
	}
	var ks []string
	for k := range kept {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return fmt.Sprintf("st=%s win=%s A=%v B=%v kept {%s}", m.Status, m.Winner, m.IpponsA, m.IpponsB, strings.Join(ks, " "))
}

// TestMatchWriteReopenSequencesConverge is the permutation matrix's reopen
// block: a result, the match reopened or sent back to the queue, then three
// devices (a finish at +3s with no basis, a point at +5s, a finish at +7s whose
// basis saw the reopen and nothing after it) in every arrival order, on both
// branches. The first recorded result by stamp stands whichever way the three
// arrive; nothing is answered with a downstream refusal, because no result is
// in force to lock; the stored match is what its log replays to.
func TestMatchWriteReopenSequencesConverge(t *testing.T) {
	M, K := []string{"M"}, []string{"K"}
	for _, ko := range []bool{false, true} {
		for _, requeue := range []bool{false, true} {
			if ko && requeue {
				continue // the knockout walk always ends in the queue
			}
			seen := map[string][]string{}
			for _, p := range pmPerms(3) {
				s := r10Setup(t, ko, requeue)
				steps := []pmStep{
					s.finish(3000, wrTeamA, M, nil, 0),
					s.point(5000, nil, K),
					s.finish(7000, wrTeamB, nil, K, s.logLen),
				}
				var answers []string
				for _, i := range p {
					a := steps[i].run(s.h)
					answers = append(answers, a)
					assert.NotContains(t, a, "downstream_knockout", "ko=%v requeue=%v order=%v: no result is in force to lock", ko, requeue, p)
				}
				if r := pmReplay(s.h, t); r != "" {
					t.Errorf("ko=%v requeue=%v order=%v: stored match differs from a replay of its log: %s", ko, requeue, p, r)
				}
				got := s.settled(t)
				seen[got] = append(seen[got], fmt.Sprint(p))
				assert.Equal(t, "st=completed win="+wrTeamA+" A=[M] B=[]", strings.SplitN(got, " kept", 2)[0],
					"ko=%v requeue=%v order=%v answers=%v: the first recorded result stands", ko, requeue, p, answers)
			}
			if len(seen) != 1 {
				t.Errorf("ko=%v requeue=%v: arrival order changed the outcome: %v", ko, requeue, seen)
			}
		}
	}
}

// poolScene is the mixed-pool form of the same walk: a fusensho in Pool A, the
// knockout match it seats a qualifier in played, then the pool match reopened.
type r10Pool struct {
	f      *rqFix
	win    string
	other  string
	reopen int64
	logLen int
}

func r10PoolSetup(t *testing.T, id string, requeue bool) r10Pool {
	t.Helper()
	now := time.Now().UnixMilli()
	f := newRQFixture(t, id, 2, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	_, _, err := f.eng.RecordDecision(f.compID, "Pool A-0", "fusensho", "aka", "", nil, now-3_000_000)
	require.NoError(t, err)
	win := loadPoolMatchByID(t, f.store, f.compID, "Pool A-0").Winner
	other := "A1"
	if win == "A1" {
		other = "A2"
	}
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	m1, _ := f.slot("Pool A-1st")
	require.NoError(t, f.scoreKO(m1.ID, win))
	_, err = f.eng.ReopenMatch(f.compID, "Pool A-0", "")
	require.NoError(t, err)
	if requeue {
		require.NoError(t, f.eng.RevertMatchToQueue(f.compID, "Pool A-0"))
	}
	lines, err := f.store.LoadScoreLog(f.compID, "Pool A-0")
	require.NoError(t, err)
	return r10Pool{f: f, win: win, other: other, reopen: time.Now().UnixMilli(), logLen: len(lines)}
}

func (p r10Pool) finish(off int64, winner string, seen int) func() error {
	return func() error {
		r := p.f.poolResult("Pool A-0", winner)
		r.ModifiedAt, r.WriteDoor, r.Changed = p.reopen+off, DoorScore, []string{state.GroupPoints, state.GroupResult}
		if seen > 0 {
			r.Basis = &state.ScoreBasis{Seen: seen}
		}
		return p.f.write("Pool A-0", r)
	}
}

func (p r10Pool) point(off int64) func() error {
	return func() error {
		m := loadPoolMatchByID(p.f.t, p.f.store, p.f.compID, "Pool A-0")
		r := &state.MatchResult{SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID, Status: state.MatchStatusRunning, IpponsB: []string{"M"}}
		r.ModifiedAt, r.WriteDoor, r.Changed = p.reopen+off, DoorScore, []string{state.GroupPoints}
		return p.f.write("Pool A-0", r)
	}
}

func (p r10Pool) lockedLines() int {
	n := 0
	entries, err := p.f.store.LoadMatchHistory(p.f.compID, "Pool A-0")
	require.NoError(p.f.t, err)
	for _, e := range entries {
		if e.Reason == HoldReasonResultLocked || e.Reason == HoldReasonResultLockedRunning {
			n++
		}
	}
	return n
}

// A reopened match holds no result, so a finish that displaces nothing but a
// point is not locked by the knockout match the old result fed: it meets the
// ordinary downstream guard, in either arrival order, and the point lands.
func TestResultLockNeedsAResultInForce(t *testing.T) {
	for _, requeue := range []bool{false, true} {
		for _, order := range [][]int{{0, 1}, {1, 0}} {
			p := r10PoolSetup(t, fmt.Sprintf("r10-force-%v-%d%d", requeue, order[0], order[1]), requeue)
			steps := []func() error{p.point(5000), p.finish(3000, p.other, 0)}
			var errs []error
			for _, i := range order {
				errs = append(errs, steps[i]())
			}
			label := fmt.Sprintf("requeue=%v order=%v errs=%v", requeue, order, errs)
			finErr := errs[0]
			if order[0] == 0 {
				finErr = errs[1]
			}
			var played *DownstreamKnockoutPlayedError
			require.ErrorAs(t, finErr, &played, label+": the finish meets the downstream guard, not a hold")
			assert.NotErrorIs(t, finErr, ErrMatchSuperseded, label)
			m := loadPoolMatchByID(t, p.f.store, p.f.compID, "Pool A-0")
			assert.NotEqual(t, state.MatchStatusCompleted, m.Status, label+": nothing is held forever; the match is still open")
			assert.Equal(t, []string{"M"}, m.IpponsB, label+": the point landed")
			assert.Zero(t, p.lockedLines(), label+": nothing was held as locked")
		}
	}
}

// A basis is judged against the result in force, not against any completed line
// of the log. Here a device that saw the old fusensho and the reopen, but not
// the second device's new finish, writes an older finish over it: it did not
// see the result in force, so with the next round played it is HELD (200,
// superseded, in the history), never turned into the downstream 409 an informed
// correction gets. Sent first, with nothing stored to displace, it meets that
// guard as any finish would; either way the stored result is the new finish.
func TestResultLockBasisMustHoldTheLineInForce(t *testing.T) {
	for _, order := range [][]int{{0, 1}, {1, 0}} { // 0 = the new finish (+5s), 1 = the older one (+3s)
		p := r10PoolSetup(t, fmt.Sprintf("r10-basis-%d%d", order[0], order[1]), false)
		steps := []func() error{p.finish(5000, p.win, 0), p.finish(3000, p.other, p.logLen)}
		var errs []error
		for _, i := range order {
			errs = append(errs, steps[i]())
		}
		label := fmt.Sprintf("order=%v errs=%v", order, errs)
		older, newer := errs[1], errs[0]
		if order[0] == 1 {
			older, newer = errs[0], errs[1]
		}
		require.NoError(t, newer, label)
		if order[0] == 0 {
			require.ErrorIs(t, older, ErrMatchSuperseded, label+": held, not refused")
			assert.Equal(t, state.HeldReasonDownstreamPlayed, HeldReasonOf(older), label)
			assert.Equal(t, 1, p.lockedLines(), label+": kept in the history")
		} else {
			var played *DownstreamKnockoutPlayedError
			require.ErrorAs(t, older, &played, label)
		}
		m := loadPoolMatchByID(t, p.f.store, p.f.compID, "Pool A-0")
		assert.Equal(t, state.MatchStatusCompleted, m.Status, label)
		assert.Equal(t, p.win, m.Winner, label+": the new finish stands")
	}
}

// A sent basis that saw nothing is no more a correction of the stored ruling
// than no basis at all: the finish pads nothing of its own. One that holds the
// line of the ruling in force is a correction of it, and pads as it always did.
func TestRivalFinishWithABasisThatSawNothingPadsNothing(t *testing.T) {
	comp := &state.Competition{ID: "c", Kind: "team", TeamSize: 3, Format: state.CompFormatLeague}
	for _, tc := range []struct {
		name  string
		basis *state.ScoreBasis
		want  int
	}{
		{"no basis", nil, 1},
		{"a basis that holds no line", &state.ScoreBasis{Own: []string{"x"}}, 1},
		{"a basis past the log", &state.ScoreBasis{Seen: 9}, 3}, // capped to the one line the log holds: the ruling
		{"a basis that holds the ruling", &state.ScoreBasis{Seen: 1}, 3},
	} {
		prior := &state.MatchResult{ID: "Pool A-0", SideA: "Kenshikan", SideB: "Sanshukan", Status: state.MatchStatusCompleted, Decision: "fusensho", Winner: "Sanshukan"}
		mb := MigrationBaseline(prior, MigrationDoor+"-0", 0)
		lines := []state.ScoreLogLine{{Baseline: &mb}}
		res := &state.MatchResult{ID: "Pool A-0", SideA: "Kenshikan", SideB: "Sanshukan", Status: state.MatchStatusCompleted, Winner: "Kenshikan", ModifiedAt: 5, WriteDoor: DoorScore,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "A1", SideB: "B1"}}, Basis: tc.basis}
		require.NoError(t, shapeScoreWrite(comp, prior, res, "Pool A-0", lines))
		assert.Len(t, res.SubResults, tc.want, tc.name)
	}
}

// Two devices holding the very same value under the very same stamp are two
// writes: a held line is deduped against a resend of itself (same session),
// never against another device's.
func TestResultLockHeldLinesAreKeptPerSession(t *testing.T) {
	h := pmHome(t, pmCase{}, true)
	require.Equal(t, "applied", pmFin(mmT2, wrTeamA, []string{"M"}, nil).run(h))
	lkNext(t, "played").run(h)
	send := func(session string) string {
		w := mmRunning(h, mmT1, state.GroupPoints, state.GroupResult)
		w.Status, w.Winner, w.IpponsB = state.MatchStatusCompleted, wrTeamB, []string{"M"}
		w.RevSession = session
		_, err := h.eng.RecordMatchResultWithIneligibility(h.compID, h.matchID, w)
		return pmAnswer(err)
	}
	for _, session := range []string{"s1", "s2", "s1", "s2", ""} {
		assert.Equal(t, lkPlayed, send(session))
	}
	assert.Len(t, lkHeldLines(t, h, mmT1), 3, "s1, s2 and the anonymous device, each once")
}

// A rival finish turned away as match_closed leaves its baseline in the score
// log as it was sent, whichever door it came through: an engi rival is logged
// as a score or decision rival is (recordSupersededWriteHistory).
func TestRivalFinishIsLoggedLikeAnyDoor(t *testing.T) {
	logged := func(engi bool, first, rival pmStep) []string {
		h := pmHome(t, pmCase{engi: engi}, true)
		require.Equal(t, "applied", first.run(h))
		require.Equal(t, "superseded:match_closed", rival.run(h))
		lines, err := h.store.LoadScoreLog(h.compID, h.matchID)
		require.NoError(t, err)
		var out []string
		for _, l := range lines {
			if b := l.Baseline; b != nil && b.Stamp == mmT2 {
				out = append(out, b.Door)
			}
		}
		return out
	}
	score := logged(false, pmFin(mmT1, wrTeamA, []string{"M"}, nil), pmFin(mmT2, wrTeamB, nil, []string{"M"}))
	require.Equal(t, []string{DoorScore}, score)
	assert.Equal(t, []string{doorEngi}, logged(true, pmEngiFin(mmT1, 3, 0), pmEngiFin(mmT2, 0, 3)),
		"the engi rival is in the log as the score rival is")
}
