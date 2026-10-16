package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Review round 9 of the knockout result lock (K1): every door and every
// arrival of an older result after the next match was played or started is
// HELD (200 superseded, the downstream reason, the values kept in the match's
// history), never refused with a 409 that loses it.

const (
	lkPlayed  = "superseded:downstream_knockout_played"
	lkRunning = "superseded:downstream_knockout_running"
)

func lkWant(mode string) string {
	if mode == "running" {
		return lkRunning
	}
	return lkPlayed
}

// lkHeldLines are the history lines that kept a result held at the stamp.
func lkHeldLines(t *testing.T, h mmHome, at int64) []state.MatchHistoryEntry {
	t.Helper()
	var out []state.MatchHistoryEntry
	for _, e := range h.history(t) {
		if e.Stamp == at && e.Outcomes[state.GroupResult] == state.HistoryOutcomeHeld {
			out = append(out, e)
		}
	}
	return out
}

// lkScenario sends stored (stamped T2), the next match's play or start, then
// older (stamped T1), and checks the older one is held and the stored result
// stands.
func lkScenario(t *testing.T, label, mode string, ko bool, stored, older pmStep, engi bool) {
	t.Helper()
	h := pmHome(t, pmCase{engi: engi}, ko)
	require.Equal(t, "applied", stored.run(h), label)
	before := h.load(t)
	lkNext(t, mode).run(h)
	assert.Equal(t, lkWant(mode), older.run(h), label)
	got := h.load(t)
	assert.Equal(t, before.Winner, got.Winner, label+": the stored winner stands")
	assert.Equal(t, before.Decision, got.Decision, label)
	assert.Equal(t, before.IpponsA, got.IpponsA, label+": and so does its scoreline")
	assert.Equal(t, before.IpponsB, got.IpponsB, label)
	lines := lkHeldLines(t, h, mmT1)
	require.Len(t, lines, 1, label+": the held result is kept in the history")
	assert.Contains(t, lines[0].Held, state.GroupResult, label)
	assert.NotEmpty(t, lines[0].Reason, label)
}

// Fix 1: a basis that does NOT hold the stored result is no sight of it.
func TestResultLockBasisThatDidNotSeeTheResult(t *testing.T) {
	M := []string{"M"}
	for _, mode := range []string{"played", "running"} {
		lkScenario(t, "basis zz-unknown, next "+mode, mode, true,
			pmFin(mmT2, wrTeamA, M, nil), pmFin(mmT1, wrTeamB, nil, M, "zz-unknown"), false)
	}
}

// Fix 2 (and 1): every door x side, both lock modes, for a write that changes
// the winner.
func TestResultLockEveryDoorAndSide(t *testing.T) {
	M := []string{"M"}
	type door struct {
		name string
		step func(at int64) pmStep
		wins string
	}
	finA := door{"finish Ryu", func(at int64) pmStep { return pmFin(at, wrTeamA, M, nil) }, wrTeamA}
	finB := door{"finish Tora", func(at int64) pmStep { return pmFin(at, wrTeamB, nil, M) }, wrTeamB}
	kikenAka := door{"kiken aka (Tora)", func(at int64) pmStep { return pmDec(at, "kiken-voluntary", "aka") }, wrTeamB}
	kikenShiro := door{"kiken shiro (Ryu)", func(at int64) pmStep { return pmDec(at, "kiken-voluntary", "shiro") }, wrTeamA}
	pairs := [][2]door{{finB, finA}, {finA, finB}, {kikenAka, finA}, {kikenShiro, finB}, {finA, kikenAka}, {finB, kikenShiro}}
	for _, p := range pairs {
		older, stored := p[0], p[1]
		require.NotEqual(t, older.wins, stored.wins)
		for _, mode := range []string{"played", "running"} {
			label := fmt.Sprintf("%s older, %s stored, next %s", older.name, stored.name, mode)
			lkScenario(t, label, mode, true, stored.step(mmT2), older.step(mmT1), false)
		}
	}
}

// Fix 3: the engi finish follows the lock too, with its own guard.
func TestResultLockEngiFinish(t *testing.T) {
	for _, mode := range []string{"played", "running"} {
		lkScenario(t, "engi, next "+mode, mode, true, pmEngiFin(mmT2, 3, 0), pmEngiFin(mmT1, 0, 3), true)
	}
}

// Fix 4: the lock covers the stored scoreline, not only its winner.
func TestResultLockCoversTheScoreline(t *testing.T) {
	for _, mode := range []string{"played", "running"} {
		lkScenario(t, "same winner, other scoreline, next "+mode, mode, true,
			pmFin(mmT2, wrTeamA, []string{"M"}, nil), pmFin(mmT1, wrTeamA, []string{"K"}, nil), false)
	}
}

// Fix 5: a resend of a held write adds no second history line.
func TestResultLockHeldResendAddsNoHistoryLine(t *testing.T) {
	for _, noReplay := range []bool{false, true} {
		h := pmHome(t, pmCase{}, true)
		require.Equal(t, "applied", pmFin(mmT2, wrTeamA, []string{"M"}, nil).run(h))
		lkNext(t, "played").run(h)
		send := func() string {
			w := mmRunning(h, mmT1, state.GroupPoints, state.GroupResult)
			w.Status, w.Winner, w.IpponsB = state.MatchStatusCompleted, wrTeamB, []string{"M"}
			w.WriteID = "w-resend"
			_, err := h.eng.RecordMatchResultWithIneligibility(h.compID, h.matchID, w, ForceOptions{NoReplayHistory: noReplay})
			return pmAnswer(err)
		}
		assert.Equal(t, lkPlayed, send())
		assert.Equal(t, lkPlayed, send(), "the resend gets the same answer")
		assert.Equal(t, lkPlayed, send())
		assert.Len(t, lkHeldLines(t, h, mmT1), 1, fmt.Sprintf("three sends, one line (noReplayHistory=%v)", noReplay))
	}
}

// Fix 7: the history reason says what locked the result.
func TestResultLockReasonNamesWhatLockedIt(t *testing.T) {
	for mode, want := range map[string]string{"played": HoldReasonResultLocked, "running": HoldReasonResultLockedRunning} {
		h := pmHome(t, pmCase{}, true)
		require.Equal(t, "applied", pmFin(mmT2, wrTeamA, []string{"M"}, nil).run(h))
		lkNext(t, mode).run(h)
		pmFin(mmT1, wrTeamB, nil, []string{"M"}).run(h)
		lines := lkHeldLines(t, h, mmT1)
		require.Len(t, lines, 1, mode)
		assert.Equal(t, want, lines[0].Reason, mode)
	}
	assert.NotEqual(t, HoldReasonResultLocked, HoldReasonResultLockedRunning)
}

// Fix 6: a rival pool write is held whatever force says; an explicit
// correction keeps force.
func TestPoolResultLockIgnoresForceForARival(t *testing.T) {
	f := newRQFixture(t, "rq-k1-force", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	stamped := func(at int64, winner string) *state.MatchResult {
		r := f.poolResult("Pool A-0", winner)
		r.ModifiedAt, r.WriteDoor, r.Changed = at, DoorScore, []string{state.GroupPoints, state.GroupResult}
		return r
	}
	require.NoError(t, f.write("Pool A-0", stamped(mmT2, "A1")))
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	m1, _ := f.slot("Pool A-1st")
	require.NoError(t, f.scoreKO(m1.ID, "A1"))

	held := f.write("Pool A-0", stamped(mmT1, "A2"), ForceOptions{Force: true})
	require.ErrorIs(t, held, ErrMatchSuperseded)
	assert.Equal(t, state.HeldReasonDownstreamPlayed, HeldReasonOf(held))
	assert.Equal(t, "A1", loadPoolMatchByID(t, f.store, f.compID, "Pool A-0").Winner)
	after, _ := f.slot("Pool A-1st")
	assert.Equal(t, state.MatchStatusCompleted, after.Status, "the played knockout match is not reopened")

	fix := stamped(mmT3, "A2")
	fix.CorrectionReason = "scoresheet was misread"
	assert.NoError(t, f.write("Pool A-0", fix, ForceOptions{Force: true}), "an explicit correction keeps force")
}

// Fix 8: a basis counting lines the log does not hold is no sight of the
// stored result. Over an empty (or unreadable) log the merge reads it as having
// seen none, so a finish stamped after the stored one is a rival and refused,
// not an informed correction. Reverting the cap in mergeMatchWrite alone
// (basis.Seen = min(basis.Seen, len(mc.lines))) reddens this.
func TestMergeBasisBeyondAnEmptyLogIsNotInformed(t *testing.T) {
	stored := &state.MatchResult{
		ID: "m", SideA: wrTeamA, SideB: wrTeamB, Status: state.MatchStatusCompleted,
		Winner: wrTeamA, IpponsA: []string{"M"}, ModifiedAt: mmT2,
		GroupStamps: map[string]int64{state.GroupPoints: mmT2, state.GroupResult: mmT2},
	}
	incoming := &state.MatchResult{
		ID: "m", SideA: wrTeamA, SideB: wrTeamB, Status: state.MatchStatusCompleted,
		Winner: wrTeamB, IpponsB: []string{"M"}, ModifiedAt: mmT3, WriteDoor: DoorScore,
		Changed: []string{state.GroupPoints, state.GroupResult},
		Basis:   &state.ScoreBasis{Seen: 9},
	}
	rep := mergeMatchWrite(stored, incoming, matchWriteForward, mergeCtx{knockout: true})
	assert.True(t, rep.Superseded(), "a basis past the end of an empty log saw nothing: the later finish is refused")
	assert.Equal(t, state.HeldReasonMatchClosed, rep.HeldReason())
}

// Fix 3, mixed engi: a rival engi pool finish is held whatever force says once
// a seated knockout match was played, and the pool row is put back.
func TestPoolResultLockEngiFinish(t *testing.T) {
	f := newRQFixture(t, "rq-k1-engi", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}}, func(c *state.Competition) { c.Engi = true })
	engi := func(matchID string, at int64, flagsA, flagsB int) *state.MatchResult {
		m := loadPoolMatchByID(t, f.store, f.compID, matchID)
		if m == nil {
			if bm := findBracketMatchInBracket(f.bracket(), matchID); bm != nil {
				m = &state.MatchResult{SideA: bm.SideA, SideB: bm.SideB, SideAID: bm.SideAID, SideBID: bm.SideBID}
			}
		}
		require.NotNil(t, m, matchID)
		return &state.MatchResult{
			SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID,
			Status: state.MatchStatusCompleted, FlagsA: flagsA, FlagsB: flagsB,
			ModifiedAt: at, WriteDoor: DoorScore, Changed: []string{state.GroupResult, state.GroupFlags},
		}
	}
	require.NoError(t, f.write("Pool A-0", engi("Pool A-0", mmT2, 3, 0)))
	require.NoError(t, f.write("Pool B-0", engi("Pool B-0", mmT2, 3, 0)))
	f.resolve()
	m1, _ := f.slot("Pool A-1st")
	require.NoError(t, f.write(m1.ID, engi(m1.ID, mmT2, 3, 0)))

	held := f.write("Pool A-0", engi("Pool A-0", mmT1, 0, 3), ForceOptions{Force: true})
	require.ErrorIs(t, held, ErrMatchSuperseded)
	assert.Equal(t, state.HeldReasonDownstreamPlayed, HeldReasonOf(held))
	stored := loadPoolMatchByID(t, f.store, f.compID, "Pool A-0")
	assert.Equal(t, "A1", stored.Winner, "the stored finish stands")
	assert.Equal(t, 3, stored.FlagsA)
	after, _ := f.slot("Pool A-1st")
	assert.Equal(t, state.MatchStatusCompleted, after.Status, "the played knockout match is not reopened")
}
