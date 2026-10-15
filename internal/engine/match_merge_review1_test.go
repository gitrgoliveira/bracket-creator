package engine

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// mmReviewPoint is a running write naming the points group alone.
func mmReviewPoint(h mmHome, at int64, a, b []string) *state.MatchResult {
	w := mmRunning(h, at, state.GroupPoints)
	w.IpponsA, w.IpponsB = a, b
	return w
}

// mmReviewFinish is a finish at the stamp, with the scoreline it stood on.
func mmReviewFinish(h mmHome, at int64, winner string, a, b []string) *state.MatchResult {
	w := mmRunning(h, at, state.GroupPoints, state.GroupResult)
	w.Status = state.MatchStatusCompleted
	w.Winner = winner
	w.IpponsA, w.IpponsB = a, b
	return w
}

func mmReviewState(t *testing.T, h mmHome) string {
	t.Helper()
	g := h.load(t)
	return fmt.Sprintf("st=%s w=%q d=%q A=%v B=%v res@%d pts@%d", g.Status, g.Winner, g.Decision, g.IpponsA, g.IpponsB,
		g.GroupStamp(state.GroupResult), g.GroupStamp(state.GroupPoints))
}

// A daihyosen (or any decision that is not a default win) closes a match on a
// scoreline: a point stored newer than it is ordered against it as against a
// finish (S2), so both arrival orders end in the same state and the verdict
// agrees with the scoreline.
func TestMerge_DaihyosenDecisionIsOrderedAgainstNewerPoints(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		for _, by := range []string{"aka", "shiro"} {
			decide := func(h mmHome) error {
				_, _, err := h.eng.RecordDecision(h.compID, h.matchID, "daihyosen", by, "", nil, mmT1)
				return err
			}
			point := func(h mmHome) error { return h.write(mmReviewPoint(h, mmT2, []string{"M"}, nil)) }

			h1 := mmIndividual(t, knockout)
			require.NoError(t, point(h1))
			require.NoError(t, decide(h1))
			h2 := mmIndividual(t, knockout)
			require.NoError(t, decide(h2))
			require.NoError(t, point(h2))

			s1, s2 := mmReviewState(t, h1), mmReviewState(t, h2)
			assert.Equal(t, s2, s1, "by %s: both arrival orders end alike", by)
			g := h1.load(t)
			assert.Equal(t, state.MatchStatusCompleted, g.Status)
			assert.Equal(t, wrTeamA, g.Winner, "by %s: the verdict agrees with the scoreline Ryu M-0", by)
			assert.Equal(t, []string{"M"}, g.IpponsA)
		}
	})
}

// A hantei finish, then a running write that never saw the mark: the write
// echoes the winner's mark-less zero scoreline. It is an echo, not a hold, and
// both arrival orders end alike.
func TestMerge_HanteiEchoIsNotHeld(t *testing.T) {
	bothBranches(t, func(t *testing.T, knockout bool) {
		h1 := mmIndividual(t, knockout)
		require.NoError(t, h1.write(mmReviewFinish(h1, mmT1, wrTeamA, []string{"Ht"}, nil)))
		w := mmReviewPoint(h1, mmT2, nil, nil)
		require.NoError(t, h1.write(w), "the carried-hantei echo is applied, not superseded")
		if assert.NotNil(t, w.Merge) {
			assert.Empty(t, w.Merge.Held, "an echo is no hold")
		}

		h2 := mmIndividual(t, knockout)
		require.NoError(t, h2.write(mmReviewPoint(h2, mmT2, nil, nil)))
		require.NoError(t, h2.write(mmReviewFinish(h2, mmT1, wrTeamA, []string{"Ht"}, nil)))
		assert.Equal(t, mmReviewState(t, h2), mmReviewState(t, h1))
		assert.Equal(t, []string{"Ht"}, h1.load(t).IpponsA, "the judges' mark stays")
	})
}

// A both-barred hikiwake (a verdict alone) over scoring stored newer takes the
// verdict that scoring gives, as the finish does; a pool tie never needs a winner.
func TestMerge_BareHikiwakeOverNewerScoringTakesItsVerdict(t *testing.T) {
	hik := func(h mmHome) *state.MatchResult {
		return &state.MatchResult{ID: h.matchID, SideA: wrTeamA, SideB: wrTeamB, SideAID: wrTeamAID, SideBID: wrTeamBID,
			Status: state.MatchStatusCompleted, Decision: "hikiwake", ModifiedAt: mmT1,
			Changed: []string{state.GroupResult}, WriteDoor: DoorDecision}
	}
	h1 := mmIndividual(t, false)
	require.NoError(t, h1.write(mmReviewPoint(h1, mmT2, []string{"M"}, nil)))
	w := hik(h1)
	require.NoError(t, h1.write(w), "not held as needing a winner")
	h2 := mmIndividual(t, false)
	require.NoError(t, h2.write(hik(h2)))
	require.NoError(t, h2.write(mmReviewPoint(h2, mmT2, []string{"M"}, nil)))
	assert.Equal(t, mmReviewState(t, h2), mmReviewState(t, h1))
	g := h1.load(t)
	assert.Equal(t, state.MatchStatusCompleted, g.Status)
	assert.Equal(t, wrTeamA, g.Winner, "Ryu leads M-0: the draw gives way to the newer scoring")
}

// A load-time repair fills ids (a winner's, a bout's members) on a match whose
// score log was written without them. The next write must log a quiet repair
// baseline, not a drift ERROR: drift stays for a stored match the log cannot
// explain.
func TestDriftBaseline_IDsFilledByARepairAreNotDrift(t *testing.T) {
	logged := func(run func()) string {
		var buf bytes.Buffer
		log.SetOutput(&buf)
		defer log.SetOutput(os.Stderr)
		run()
		return buf.String()
	}
	cur := state.MatchResult{ID: "Pool A-0", SideA: "Ryu", SideB: "Tora", SideAID: "pa", SideBID: "pb",
		Status: state.MatchStatusCompleted, Winner: "Ryu", IpponsA: []string{"M"}, ModifiedAt: mmT1,
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "Ryu", SideB: "Tora", IpponsA: []string{"M"}, Winner: "Ryu"}}}
	mb := MigrationBaseline(&cur, "m0", mmT1)
	lines := []state.ScoreLogLine{{Baseline: &mb}}

	repaired := cur
	repaired.WinnerID = "pa"
	repaired.SubResults = state.CloneSubResults(cur.SubResults)
	repaired.SubResults[0].SideAMemberID, repaired.SubResults[0].WinnerMemberID = "ma", "ma"

	var line *state.ScoreLogLine
	out := logged(func() {
		line = driftBaseline(&repaired, lines, ReplayShape{Knockout: false, Comp: &state.Competition{Kind: "team", TeamSize: 1}}, mmT2)
	})
	require.NotNil(t, line)
	assert.Equal(t, DoorRepair, line.Baseline.Door)
	assert.NotContains(t, out, "ERROR")

	// A different winner is a stored match the log cannot explain.
	drifted := repaired
	drifted.Winner, drifted.WinnerID = "Tora", "pb"
	out = logged(func() { line = driftBaseline(&drifted, lines, ReplayShape{}, mmT2) })
	require.NotNil(t, line)
	assert.Equal(t, DoorDrift, line.Baseline.Door)
	assert.Contains(t, out, "ERROR")

	// An id the log holds is not one a repair filled.
	moved := repaired
	mb2 := MigrationBaseline(&repaired, "m0", mmT1)
	moved.WinnerID = "pz"
	out = logged(func() { line = driftBaseline(&moved, []state.ScoreLogLine{{Baseline: &mb2}}, ReplayShape{}, mmT2) })
	require.NotNil(t, line)
	assert.Equal(t, DoorDrift, line.Baseline.Door)
	assert.Contains(t, out, "ERROR")
}
