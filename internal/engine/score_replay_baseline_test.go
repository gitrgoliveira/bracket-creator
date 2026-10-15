package engine

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The baseline rules (bc-evts stage 2b): a snapshot write (a previous
// build's running PUT /score, a bulk or quick score, a finish) reaches the
// log as a baseline, and the replay owns what the per-group snapshot merge
// used to decide for it: each group ordered at its own stamp, and a
// baseline's scoring on a finished match judged as an event is (R2, R3, R4,
// S3), in every arrival order.

// snapLine is a baseline of the scoring groups of a snapshot at stamp.
func snapLine(id string, stamp int64, set func(m *state.MatchResult), groups ...string) state.ScoreLogLine {
	m := &state.MatchResult{SideA: "Aka", SideB: "Shiro", SideAID: "pa", SideBID: "pb", Status: state.MatchStatusRunning}
	set(m)
	return baseLine(m, id, stamp, state.ScoreBasis{}, groups...)
}

func finishedKnockout(winnerA bool, a, b []string) *state.MatchResult {
	m := knockoutIndividual()
	m.Status = state.MatchStatusCompleted
	m.IpponsA, m.IpponsB = a, b
	if winnerA {
		m.Winner, m.WinnerID = "Aka", "pa"
	} else {
		m.Winner, m.WinnerID = "Shiro", "pb"
	}
	return m
}

func TestReplay_BaselineGroupsReplayAtTheirOwnStamps(t *testing.T) {
	// The stored match: a point at 100, the start (result) at 300. A snapshot
	// stamped 200 arrives after: per group, its points are newer than the
	// stored points, so they apply, as the merge applied them.
	m := knockoutIndividual()
	m.IpponsA = []string{"M"}
	m.GroupStamps = map[string]int64{state.GroupPoints: 100, state.GroupResult: 300}
	m.ModifiedAt = 300
	snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"M"}, []string{"K"} }, state.GroupPoints)
	r := ReplayScoring(m, append(migrated(m), snap), ReplayShape{Knockout: true})
	assert.Equal(t, []string{"K"}, r.Match.IpponsB, "the snapshot's points are newer than the stored ones")
	assert.Equal(t, int64(200), r.Match.GroupStamps[state.GroupPoints])
	assert.Equal(t, int64(300), r.Match.GroupStamps[state.GroupResult])
	assert.Empty(t, r.Held)

	t.Run("an older snapshot loses its group to the stored one", func(t *testing.T) {
		old := snapLine("old", 50, func(s *state.MatchResult) { s.IpponsB = []string{"K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), old), ReplayShape{Knockout: true})
		assert.Equal(t, []string{"M"}, r.Match.IpponsA)
		assert.Empty(t, r.Match.IpponsB)
	})
}

func TestReplay_SnapshotOnAFinishedMatch(t *testing.T) {
	ko := ReplayShape{Knockout: true}
	t.Run("a snapshot stamped after the finish is turned away", func(t *testing.T) {
		m := finishedKnockout(true, []string{"M"}, nil)
		m.ModifiedAt = 100
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"M"}, []string{"K", "K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Equal(t, state.MatchStatusCompleted, r.Match.Status, "the match stays finished")
		assert.Equal(t, "Aka", r.Match.Winner)
		assert.Equal(t, []string{"M"}, r.Match.IpponsA)
		assert.Empty(t, r.Match.IpponsB)
		assert.Equal(t, int64(100), r.Match.GroupStamps[state.GroupResult])
		require.Contains(t, r.Held, 1)
		assert.Equal(t, []string{state.GroupPoints}, r.Held[1].Groups)
		assert.True(t, r.Held[1].MatchClosed)
		assert.Equal(t, HoldReasonMatchClosed, r.Held[1].Reason)
	})
	t.Run("a snapshot made before the finish is the finish's to decide", func(t *testing.T) {
		m := finishedKnockout(true, []string{"M"}, nil)
		m.ModifiedAt = 200
		snap := snapLine("snap", 150, func(s *state.MatchResult) { s.IpponsB = []string{"K", "K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Equal(t, []string{"M"}, r.Match.IpponsA)
		assert.Empty(t, r.Match.IpponsB, "the finish declared the scoreline it stood on")
		assert.Equal(t, "Aka", r.Match.Winner)
	})
	t.Run("a tie after the finish is turned away: a knockout and a pool alike", func(t *testing.T) {
		m := finishedKnockout(true, []string{"M"}, nil)
		m.ModifiedAt = 100
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"M"}, []string{"K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Empty(t, r.Match.IpponsB)
		assert.Equal(t, "Aka", r.Match.Winner)
		require.Contains(t, r.Held, 1)
		assert.True(t, r.Held[1].MatchClosed)

		p := finishedKnockout(true, []string{"M"}, nil)
		p.ID, p.ModifiedAt = "Pool A-1", 100
		r = ReplayScoring(p, append(migrated(p), snap), ReplayShape{})
		assert.Empty(t, r.Match.Decision, "no draw is made of a closed match")
		assert.Equal(t, "Aka", r.Match.Winner)
		require.Contains(t, r.Held, 1)
	})
	t.Run("a finish arriving after newer scoring closes it out, in both orders", func(t *testing.T) {
		m := knockoutIndividual()
		m.ModifiedAt = 50
		finish := snapLine("finish", 100, func(s *state.MatchResult) {
			s.Status, s.Winner, s.WinnerID = state.MatchStatusCompleted, "Aka", "pa"
			s.IpponsA = []string{"M"}
		}, state.GroupResult, state.GroupPoints)
		later := snapLine("later", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"M"}, []string{"K", "K"} }, state.GroupPoints)
		r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{finish, later}, ko)
		assert.Equal(t, state.MatchStatusCompleted, r.Match.Status)
		assert.Equal(t, "Aka", r.Match.Winner)
		assert.Empty(t, r.Match.IpponsB)
		assert.Len(t, r.Held, 1, "the later scoring is the history's")
	})
	t.Run("a hantei stands: scoring after the finish is turned away", func(t *testing.T) {
		m := finishedKnockout(true, []string{domain.HanteiMark}, nil)
		m.ModifiedAt = 100
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{}, []string{"K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Equal(t, []string{domain.HanteiMark}, r.Match.IpponsA)
		assert.Equal(t, "Aka", r.Match.Winner)
	})
}

func TestReplay_SnapshotOnADecision(t *testing.T) {
	ko := ReplayShape{Knockout: true}
	kiken := func(decision domain.Decision) *state.MatchResult {
		m := finishedKnockout(true, []string{domain.DefaultWinIppon, domain.DefaultWinIppon}, []string{"M"})
		m.Decision, m.DecisionBy, m.ModifiedAt = string(decision), "B", 100
		return m
	}
	t.Run("scoring after a withdrawal is turned away, whatever it gives", func(t *testing.T) {
		for name, score := range map[string]func(s *state.MatchResult){
			"a winner": func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{}, []string{"M", "K"} },
			"a tie":    func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"K"}, []string{"M"} },
		} {
			t.Run(name, func(t *testing.T) {
				m := kiken(domain.DecisionKikenVoluntary)
				snap := snapLine("snap", 200, score, state.GroupPoints)
				r := ReplayScoring(m, append(migrated(m), snap), ko)

				assert.Equal(t, string(domain.DecisionKikenVoluntary), r.Match.Decision)
				assert.Equal(t, "Aka", r.Match.Winner)
				require.Contains(t, r.Held, 1)
				assert.True(t, r.Held[1].MatchClosed)
			})
		}
	})
	t.Run("overtime after a withdrawal is turned away", func(t *testing.T) {
		m := kiken(domain.DecisionKikenVoluntary)
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.Encho = &state.EnchoMetadata{PeriodCount: 1} }, state.GroupEncho)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Nil(t, r.Match.Encho)
		require.Contains(t, r.Held, 1)
		assert.True(t, r.Held[1].MatchClosed)
	})
	t.Run("a fusensho stands whatever is scored", func(t *testing.T) {
		m := kiken(domain.DecisionFusensho)
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{}, []string{"M", "K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Equal(t, string(domain.DecisionFusensho), r.Match.Decision)
		assert.Equal(t, []string{"M"}, r.Match.IpponsB)
		require.Contains(t, r.Held, 1)
		assert.Equal(t, HoldReasonMatchClosed, r.Held[1].Reason)
	})
}

func TestReplay_EngiSnapshotNewestCountDecides(t *testing.T) {
	engi := ReplayShape{Comp: &state.Competition{Engi: true}, Knockout: true}
	m := knockoutIndividual()
	m.ModifiedAt = 50
	finish := snapLine("finish", 100, func(s *state.MatchResult) {
		s.Status, s.Winner, s.WinnerID = state.MatchStatusCompleted, "Aka", "pa"
		s.FlagsA, s.FlagsB = 3, 2
	}, state.GroupResult, state.GroupFlags)
	// The closed-match rule: a count stamped after the finish, valid or not, is
	// turned away; the finish's own count stands.
	for name, flags := range map[string][2]int{"a valid count": {1, 2}, "a count that is not valid": {2, 2}} {
		t.Run(name+" after the finish is turned away, in both orders", func(t *testing.T) {
			recount := snapLine("recount", 200, func(s *state.MatchResult) { s.FlagsA, s.FlagsB = flags[0], flags[1] }, state.GroupFlags)
			r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{finish, recount}, engi)
			assert.Equal(t, "Aka", r.Match.Winner)
			assert.Equal(t, 3, r.Match.FlagsA)
			assert.Equal(t, 2, r.Match.FlagsB)
			assert.Len(t, r.Held, 1)
		})
	}
}

// The older-revision hold reads the log (it replaced the score handler's
// in-memory rev guard): a lower revision of a session the log holds a higher
// one of is older, until the board's run ends (a reopen, a send back to the
// queue, a result that left running).
func TestOlderRevisionThanLogged(t *testing.T) {
	running := func(rev int64, session string) state.ScoreLogLine {
		l := snapLine(fmt.Sprintf("r%d", rev), 100+rev, func(s *state.MatchResult) { s.IpponsA = []string{"M"} }, state.GroupPoints, state.GroupResult)
		l.Baseline.RevSession, l.Baseline.Rev = session, rev
		return l
	}
	ended := func(door string, status state.MatchStatus) state.ScoreLogLine {
		l := snapLine(door, 200, func(s *state.MatchResult) { s.Status = status }, state.GroupResult)
		l.Baseline.Door = door
		return l
	}
	lines := []state.ScoreLogLine{running(5, "s1")}
	assert.True(t, olderRevisionThanLogged(lines, "s1", 3), "a lower revision of the same board")
	assert.False(t, olderRevisionThanLogged(lines, "s1", 5), "the same revision is not older")
	assert.False(t, olderRevisionThanLogged(lines, "s2", 1), "another board's session is its own")
	assert.False(t, olderRevisionThanLogged(lines, "", 1), "no session: unversioned")
	assert.False(t, olderRevisionThanLogged(lines, "s1", 0), "no revision: unversioned")
	for _, end := range []state.ScoreLogLine{
		ended(doorRequeue, state.MatchStatusScheduled),
		ended(doorReopen, state.MatchStatusRunning),
		ended(DoorScore, state.MatchStatusCompleted),
	} {
		assert.False(t, olderRevisionThanLogged(append(slices.Clone(lines), end), "s1", 1), "the run ended at %s", end.Baseline.Door)
	}
}
