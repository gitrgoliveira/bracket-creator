package engine

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Round 11: the reopen matrix's kachinuki row. A kachinuki match closed by a
// fusensho, reopened (and on request sent back to the queue), then two
// devices finish it and a third taps a bout, with and without a basis, in
// every arrival order, on both branches. The first recorded result by stamp
// stands, the stored match is what its log replays to, and no write is
// answered with a downstream refusal (no result is in force to lock).
func r11KachinukiSetup(t *testing.T, ko, requeue bool) r10Scene {
	t.Helper()
	now := time.Now().UnixMilli()
	base := now - 3_600_000
	h := mmTeam(t, ko)
	c, err := h.store.LoadCompetition(h.compID)
	require.NoError(t, err)
	c.TeamMatchType = state.TeamMatchTypeKachinuki
	require.NoError(t, h.store.SaveCompetition(c))
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
	require.Equal(t, "applied", pmDec(base+60_000, "fusensho", "aka").run(h))
	_, err = h.eng.ReopenMatch(h.compID, h.matchID, "")
	require.NoError(t, err)
	if requeue {
		require.NoError(t, h.eng.RevertMatchToQueue(h.compID, h.matchID))
	}
	require.NotEqual(t, state.MatchStatusCompleted, h.load(t).Status)
	lines, err := h.store.LoadScoreLog(h.compID, h.matchID)
	require.NoError(t, err)
	require.NotEmpty(t, lines)
	return r10Scene{h: h, logLen: len(lines), reopen: time.Now().UnixMilli()}
}

// r11KFinish is a kachinuki finish: bout 1 decided for the winner's fighter,
// the match closed by exhaustion. seen > 0 sends a basis holding that many
// lines of the log.
func r11KFinish(s r10Scene, off int64, winner string, seen int) pmStep {
	at := s.reopen + off
	sets := []string{state.GroupResult, state.BoutGroup(1)}
	return pmStep{at: at, role: pmRes, sets: sets, run: func(h mmHome) string {
		w := mmRunning(h, at, sets...)
		row := state.SubMatchResult{Position: 1, SideA: "r1", SideB: "t1"}
		if winner == wrTeamA {
			row.Winner, row.IpponsA = "r1", []string{"M"}
		} else {
			row.Winner, row.IpponsB = "t1", []string{"K"}
		}
		w.Status, w.Winner, w.Decision = state.MatchStatusCompleted, winner, string(domain.DecisionKachinukiExhaustion)
		w.SubResults = []state.SubMatchResult{row}
		if seen > 0 {
			w.Basis = &state.ScoreBasis{Seen: seen}
		}
		return pmWrite(h, w)
	}}
}

// r11KTap scores bout 1 for one side without finishing anything.
func r11KTap(s r10Scene, off int64, a, b []string) pmStep {
	at := s.reopen + off
	return pmStep{at: at, role: pmTap, run: func(h mmHome) string {
		w := mmRunning(h, at, state.BoutGroup(1))
		w.SubResults = []state.SubMatchResult{{Position: 1, SideA: "r1", SideB: "t1", IpponsA: a, IpponsB: b}}
		return pmWrite(h, w)
	}}
}

func r11KSettled(t *testing.T, s r10Scene) string {
	t.Helper()
	m := s.h.load(t)
	var rows []string
	for _, r := range m.SubResults {
		rows = append(rows, fmt.Sprintf("%d:%s %v-%v", r.Position, r.Winner, r.IpponsA, r.IpponsB))
	}
	return fmt.Sprintf("st=%s win=%s d=%q bouts=[%s]", m.Status, m.Winner, m.Decision, strings.Join(rows, " "))
}

func TestMatchWriteReopenSequencesConverge_Kachinuki(t *testing.T) {
	sets := []struct {
		name  string
		steps func(s r10Scene) []pmStep
		want  string
	}{
		{"first finish without basis", func(s r10Scene) []pmStep {
			return []pmStep{r11KFinish(s, 3000, wrTeamA, 0), r11KTap(s, 5000, nil, []string{"K"}), r11KFinish(s, 7000, wrTeamB, s.logLen)}
		}, `st=completed win=` + wrTeamA + ` d="kachinuki-exhaustion" bouts=[1:r1 [M]-[]]`},
		{"first finish with basis", func(s r10Scene) []pmStep {
			return []pmStep{r11KFinish(s, 3000, wrTeamB, s.logLen), r11KTap(s, 5000, []string{"M"}, nil), r11KFinish(s, 7000, wrTeamA, 0)}
		}, `st=completed win=` + wrTeamB + ` d="kachinuki-exhaustion" bouts=[1:t1 []-[K]]`},
	}
	runs := 0
	for _, set := range sets {
		for _, ko := range []bool{false, true} {
			for _, requeue := range []bool{false, true} {
				seen := map[string][]string{}
				for _, p := range pmPerms(3) {
					runs++
					s := r11KachinukiSetup(t, ko, requeue)
					steps := set.steps(s)
					var answers []string
					for _, i := range p {
						a := steps[i].run(s.h)
						answers = append(answers, a)
						assert.NotContains(t, a, "downstream_knockout", "%s ko=%v requeue=%v order=%v", set.name, ko, requeue, p)
						assert.NotContains(t, a, "error:", "%s ko=%v requeue=%v order=%v", set.name, ko, requeue, p)
					}
					if r := pmReplay(s.h, t); r != "" {
						t.Errorf("%s ko=%v requeue=%v order=%v: stored match differs from a replay of its log: %s", set.name, ko, requeue, p, r)
					}
					got := r11KSettled(t, s)
					seen[got] = append(seen[got], fmt.Sprint(p))
					assert.Equal(t, set.want, got, "%s ko=%v requeue=%v order=%v answers=%v: the first recorded result stands", set.name, ko, requeue, p, answers)
				}
				if len(seen) != 1 {
					t.Errorf("%s ko=%v requeue=%v: arrival order changed the outcome: %v", set.name, ko, requeue, seen)
				}
			}
		}
	}
	t.Logf("kachinuki reopen matrix: %d runs", runs)
}

// A load-time repair (a winner's id the log lacks) is logged by the merge as a
// repair baseline appended to the lines it replays. Every caller asking what
// result is in force, and whether a basis saw it, reads those same lines: the
// /score handler's reason check and the padding gate (ResultInForceAt) and the
// merge's result lock agree (bc-evts review round 11).
func TestResultInForceAgreesWithTheMergeAfterARepair(t *testing.T) {
	cur := state.MatchResult{ID: "Pool A-0", SideA: "Ryu", SideB: "Tora", SideAID: "pa", SideBID: "pb",
		Status: state.MatchStatusCompleted, Winner: "Ryu", IpponsA: []string{"M"}, ModifiedAt: mmT1}
	fin := BaselineFromMatch(&cur, "s0", DoorScore, mmT1, []string{state.GroupPoints, state.GroupResult}, state.ScoreBasis{})
	lines := []state.ScoreLogLine{{Baseline: &fin}}
	comp := &state.Competition{}
	shape := ReplayShape{Comp: comp}

	// The merge's view: the found lines with the repair (or drift) line the
	// merge appends and logs.
	mergeView := func(stored *state.MatchResult, basis *state.ScoreBasis) ResultInForce {
		drift := driftBaseline(stored, lines, shape, mmT2)
		require.NotNil(t, drift)
		return resultInForceIn(stored, append(slices.Clone(lines), *drift), shape, nil, basis, len(lines))
	}

	repaired := cur
	repaired.WinnerID = "pa"
	for _, seen := range []int{0, 1} {
		basis := &state.ScoreBasis{Seen: seen}
		handler := ResultInForceAt(&repaired, lines, comp, false, 0, basis)
		assert.Equal(t, mergeView(&repaired, basis), handler, "repair, seen=%d: the handler and the merge read one log", seen)
		assert.True(t, handler.InForce)
		assert.Equal(t, 0, handler.Line, "a repair line (ids only) never takes over the line that set the result")
		assert.Equal(t, seen == 1, handler.Saw, "repair, seen=%d: a basis holding the finish saw the result", seen)
		// The same at the stamp of a finish made after it (the handler's ask).
		assert.Equal(t, handler, ResultInForceAt(&repaired, lines, comp, false, mmT2+1, basis))
	}

	// A drift (a different winner the log cannot explain) is a write the log
	// missed: its line sets the result, and no device can have seen it.
	drifted := cur
	drifted.Winner, drifted.WinnerID, drifted.IpponsA, drifted.IpponsB = "Tora", "pb", nil, []string{"K"}
	for _, seen := range []int{0, 1, 5} {
		basis := &state.ScoreBasis{Seen: seen}
		handler := ResultInForceAt(&drifted, lines, comp, false, 0, basis)
		assert.Equal(t, mergeView(&drifted, basis), handler, "drift, seen=%d", seen)
		assert.Equal(t, ResultInForce{InForce: true, Line: 1}, handler, "drift, seen=%d: the drift line is in force and unseen", seen)
	}
}

// The rival test caps a basis by the log's own line count, not by the lines
// the replay holds: on an empty log the merge mints the stored match as its
// migration baseline, which no device saw, so a later finish claiming to have
// seen lines is still a rival (bc-evts review round 11). The merge also caps
// the basis it logs; this pins the replay's own cap without it.
func TestRivalResultCapsTheBasisByTheLogsOwnLines(t *testing.T) {
	stored := state.MatchResult{ID: "Pool A-0", SideA: "Ryu", SideB: "Tora", SideAID: "pa", SideBID: "pb",
		Status: state.MatchStatusCompleted, Winner: "Ryu", WinnerID: "pa", IpponsA: []string{"M"}, ModifiedAt: mmT1}
	mb := MigrationBaseline(&stored, MigrationDoor+"-0", mmT1)
	rival := stored
	rival.Winner, rival.WinnerID, rival.IpponsA, rival.IpponsB = "Tora", "pb", nil, []string{"K"}
	bl := BaselineFromMatch(&rival, "s1", DoorScore, mmT2, []string{state.GroupPoints, state.GroupResult}, state.ScoreBasis{Seen: 5})
	bl.BasisSent = true
	all := []state.ScoreLogLine{{Baseline: &mb}, {Baseline: &bl}}
	shape := ReplayShape{Comp: &state.Competition{}}

	got := ReplayScoring(&stored, all, shape.withLogLines(0))
	assert.Equal(t, "Ryu", got.Match.Winner, "the first recorded result stands")
	h, held := got.Held[1]
	require.True(t, held, "a basis cannot have seen a line the log does not hold")
	assert.True(t, h.MatchClosed)

	// Without the log's own count, the minted baseline reads as seen.
	open := ReplayScoring(&stored, all, shape)
	assert.Equal(t, "Tora", open.Match.Winner, "control: counting every line lets the basis see the minted one")
}
