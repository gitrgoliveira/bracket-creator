package engine

import (
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
	t.Run("R3: a later point applies and the winner is worked out again", func(t *testing.T) {
		m := finishedKnockout(true, []string{"M"}, nil)
		m.ModifiedAt = 100
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"M"}, []string{"K", "K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Equal(t, state.MatchStatusCompleted, r.Match.Status, "the match stays finished")
		assert.Equal(t, "Shiro", r.Match.Winner)
		assert.Equal(t, "pb", r.Match.WinnerID)
		assert.Equal(t, int64(200), r.Match.GroupStamps[state.GroupResult], "the verdict moved with the point")
		assert.Empty(t, r.Held)
	})
	t.Run("R3: a snapshot made before the finish is the finish's to decide", func(t *testing.T) {
		m := finishedKnockout(true, []string{"M"}, nil)
		m.ModifiedAt = 200
		snap := snapLine("snap", 150, func(s *state.MatchResult) { s.IpponsB = []string{"K", "K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Equal(t, []string{"M"}, r.Match.IpponsA)
		assert.Empty(t, r.Match.IpponsB, "the finish declared the scoreline it stood on")
		assert.Equal(t, "Aka", r.Match.Winner)
	})
	t.Run("R4: a knockout tie is not applied, the finish stands", func(t *testing.T) {
		m := finishedKnockout(true, []string{"M"}, nil)
		m.ModifiedAt = 100
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"M"}, []string{"K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Empty(t, r.Match.IpponsB)
		assert.Equal(t, "Aka", r.Match.Winner)
		require.Contains(t, r.Held, 1)
		assert.Equal(t, []string{state.GroupPoints}, r.Held[1].Groups)
		assert.True(t, r.Held[1].NeedsWinner)
		assert.Equal(t, HoldReasonKnockoutNeedsWinner, r.Held[1].Reason)
	})
	t.Run("R4 in a pool: a tie is a draw", func(t *testing.T) {
		m := finishedKnockout(true, []string{"M"}, nil)
		m.ID, m.ModifiedAt = "Pool A-1", 100
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"M"}, []string{"K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ReplayShape{})
		assert.Equal(t, state.DecisionDraw, r.Match.Decision)
		assert.Empty(t, r.Match.Winner)
		assert.Empty(t, r.Held)
	})
	t.Run("S2: a finish arriving after newer scoring takes that scoring's verdict, in both orders", func(t *testing.T) {
		m := knockoutIndividual()
		m.ModifiedAt = 50
		finish := snapLine("finish", 100, func(s *state.MatchResult) {
			s.Status, s.Winner, s.WinnerID = state.MatchStatusCompleted, "Aka", "pa"
			s.IpponsA = []string{"M"}
		}, state.GroupResult, state.GroupPoints)
		later := snapLine("later", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"M"}, []string{"K", "K"} }, state.GroupPoints)
		r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{finish, later}, ko)
		assert.Equal(t, state.MatchStatusCompleted, r.Match.Status)
		assert.Equal(t, "Shiro", r.Match.Winner)
		assert.Equal(t, []string{"K", "K"}, r.Match.IpponsB)
	})
	t.Run("S2 with R4: newer scoring that would tie the knockout stays out, in both orders", func(t *testing.T) {
		m := knockoutIndividual()
		m.ModifiedAt = 50
		finish := snapLine("finish", 100, func(s *state.MatchResult) {
			s.Status, s.Winner, s.WinnerID = state.MatchStatusCompleted, "Aka", "pa"
			s.IpponsA = []string{"M"}
		}, state.GroupResult, state.GroupPoints)
		later := snapLine("later", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"M"}, []string{"K"} }, state.GroupPoints)
		r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{finish, later}, ko)
		assert.Equal(t, "Aka", r.Match.Winner)
		assert.Empty(t, r.Match.IpponsB)
		assert.Len(t, r.Held, 1, "the tying change is the history's")
	})
	t.Run("S3: points that do not claim the result keep the judges' decision", func(t *testing.T) {
		m := finishedKnockout(true, []string{domain.HanteiMark}, nil)
		m.ModifiedAt = 100
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{}, []string{} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Equal(t, []string{domain.HanteiMark}, r.Match.IpponsA)
		assert.Equal(t, "Aka", r.Match.Winner)
		snap = snapLine("snap2", 300, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{}, []string{"K"} }, state.GroupPoints)
		r = ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Empty(t, r.Match.IpponsA, "a point that unties it takes the mark off")
		assert.Equal(t, "Shiro", r.Match.Winner)
	})
}

func TestReplay_SnapshotOnADecision(t *testing.T) {
	ko := ReplayShape{Knockout: true}
	kiken := func(decision domain.Decision) *state.MatchResult {
		m := finishedKnockout(true, []string{domain.DefaultWinIppon, domain.DefaultWinIppon}, []string{"M"})
		m.Decision, m.DecisionBy, m.ModifiedAt = string(decision), "B", 100
		return m
	}
	t.Run("R2: scoring that gives a winner clears a withdrawal", func(t *testing.T) {
		m := kiken(domain.DecisionKikenVoluntary)
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{}, []string{"M", "K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.True(t, r.ClearedWithdrawal)
		assert.Empty(t, r.Match.Decision)
		assert.Equal(t, "Shiro", r.Match.Winner)
		assert.Empty(t, r.Held)
	})
	t.Run("R2: scoring that leaves the knockout tied leaves the withdrawal standing", func(t *testing.T) {
		m := kiken(domain.DecisionKikenVoluntary)
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{"K"}, []string{"M"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.False(t, r.ClearedWithdrawal)
		assert.Equal(t, string(domain.DecisionKikenVoluntary), r.Match.Decision)
		require.Contains(t, r.Held, 1)
		assert.True(t, r.Held[1].DefaultWinStands)
		assert.Equal(t, string(domain.DecisionKikenVoluntary), r.Held[1].Decision)
	})
	t.Run("R2: overtime alone says nothing about the withdrawal", func(t *testing.T) {
		m := kiken(domain.DecisionKikenVoluntary)
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.Encho = &state.EnchoMetadata{PeriodCount: 1} }, state.GroupEncho)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Nil(t, r.Match.Encho)
		require.Contains(t, r.Held, 1)
		assert.True(t, r.Held[1].DefaultWinStands)
	})
	t.Run("a fusensho stands whatever is scored", func(t *testing.T) {
		m := kiken(domain.DecisionFusensho)
		snap := snapLine("snap", 200, func(s *state.MatchResult) { s.IpponsA, s.IpponsB = []string{}, []string{"M", "K"} }, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Equal(t, string(domain.DecisionFusensho), r.Match.Decision)
		assert.Equal(t, []string{"M"}, r.Match.IpponsB)
		require.Contains(t, r.Held, 1)
		assert.Equal(t, defaultWinStandsReason(string(domain.DecisionFusensho)), r.Held[1].Reason)
	})
	t.Run("an echo of the stored scoring is not judged", func(t *testing.T) {
		m := kiken(domain.DecisionFusensho)
		snap := snapLine("snap", 200, func(s *state.MatchResult) {
			s.IpponsA, s.IpponsB = []string{domain.DefaultWinIppon, domain.DefaultWinIppon}, []string{"M"}
		}, state.GroupPoints)
		r := ReplayScoring(m, append(migrated(m), snap), ko)
		assert.Empty(t, r.Held)
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
	t.Run("a newer valid count decides, in both orders", func(t *testing.T) {
		recount := snapLine("recount", 200, func(s *state.MatchResult) { s.FlagsA, s.FlagsB = 1, 2 }, state.GroupFlags)
		r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{finish, recount}, engi)
		assert.Equal(t, "Shiro", r.Match.Winner)
		assert.Equal(t, 2, r.Match.FlagsB)
	})
	t.Run("a newer count that is not valid stays out, in both orders", func(t *testing.T) {
		recount := snapLine("recount", 200, func(s *state.MatchResult) { s.FlagsA, s.FlagsB = 2, 2 }, state.GroupFlags)
		r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{finish, recount}, engi)
		assert.Equal(t, "Aka", r.Match.Winner)
		assert.Equal(t, 3, r.Match.FlagsA)
		assert.Len(t, r.Held, 1)
	})
}
