package engine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// replayAllOrders replays head followed by every order of tail and asserts
// the match and every outcome come out the same; it returns the replay of
// the first order.
func replayAllOrders(t *testing.T, current *state.MatchResult, head, tail []state.ScoreLogLine, shape ReplayShape) Replay {
	t.Helper()
	var first *Replay
	for _, order := range permutations(len(tail)) {
		lines := append([]state.ScoreLogLine(nil), head...)
		for _, i := range order {
			lines = append(lines, tail[i])
		}
		r := ReplayScoring(current, lines, shape)
		if first == nil {
			first = &r
			continue
		}
		require.Equal(t, stateOf(first.Match), stateOf(r.Match), "order %v", order)
		for _, o := range first.Outcomes {
			require.Equal(t, o.Outcome, r.Outcome(o.ID), "order %v, event %s", order, o.ID)
		}
	}
	return *first
}

func migrated(m *state.MatchResult) []state.ScoreLogLine {
	b := MigrationBaseline(m, "m0", 0)
	return []state.ScoreLogLine{{V: state.ScoreLogFormatVersion, Baseline: &b}}
}

func runningIndividual() *state.MatchResult {
	m := knockoutIndividual()
	m.ModifiedAt = 10
	return m
}

// D2: a removal names its mark, so two devices removing two different marks
// at once remove both.
func TestReplay_ConcurrentRemovalsBothLand(t *testing.T) {
	m := runningIndividual()
	head := append(migrated(m),
		evLine("eM", 20, domain.ScoreIpponAdd, 0, "a", "M"),
		evLine("eK", 21, domain.ScoreIpponAdd, 0, "b", "K"))
	r := replayAllOrders(t, m, head, []state.ScoreLogLine{removeLine("rM", 30, "eM"), removeLine("rK", 31, "eK")}, ReplayShape{Knockout: true})
	assert.Empty(t, r.Match.IpponsA)
	assert.Empty(t, r.Match.IpponsB)
	assert.Nil(t, r.Match.MarkIDs)
	assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("rM"))
	assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("rK"))
	// Removing it again finds nothing.
	r2 := ReplayScoring(m, append(head, removeLine("rM", 30, "eM"), removeLine("rM2", 31, "eM")), ReplayShape{})
	assert.Equal(t, domain.ScoreOutcomeNoTarget, r2.Outcome("rM2"))
}

// D3: a strike past the cap changes nothing; the first side to two decided
// the bout, so the other side's strike is capped too (2-2 is impossible).
func TestReplay_CappedStrikes(t *testing.T) {
	m := runningIndividual()
	lines := append(migrated(m),
		evLine("a1", 20, domain.ScoreIpponAdd, 0, "a", "M"),
		evLine("a2", 21, domain.ScoreIpponAdd, 0, "a", "K"),
		evLine("a3", 22, domain.ScoreIpponAdd, 0, "a", "D"),
		evLine("b1", 23, domain.ScoreIpponAdd, 0, "b", "M"))
	r := ReplayScoring(m, lines, ReplayShape{})
	assert.Equal(t, []string{"M", "K"}, r.Match.IpponsA)
	assert.Empty(t, r.Match.IpponsB)
	assert.Equal(t, domain.ScoreOutcomeCapped, r.Outcome("a3"))
	assert.Equal(t, domain.ScoreOutcomeCapped, r.Outcome("b1"))
}

// D4: the same strike on two devices counts twice.
func TestReplay_SameStrikeOnTwoDevicesCountsTwice(t *testing.T) {
	m := runningIndividual()
	r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{
		evLine("x", 20, domain.ScoreIpponAdd, 0, "a", "M"),
		evLine("y", 20, domain.ScoreIpponAdd, 0, "a", "M"),
	}, ReplayShape{})
	assert.Equal(t, []string{"M", "M"}, r.Match.IpponsA)
	assert.Equal(t, []string{"x", "y"}, r.Match.MarkIDs.A)
}

// The second outstanding foul lands an H for the opponent, named by the foul
// event; removing that id takes the H off.
func TestReplay_FoulLandsHAndItsRemoval(t *testing.T) {
	m := runningIndividual()
	lines := append(migrated(m),
		evLine("f1", 20, domain.ScoreFoulAdd, 0, "b", ""),
		evLine("f2", 21, domain.ScoreFoulAdd, 0, "b", ""))
	r := ReplayScoring(m, lines, ReplayShape{})
	assert.Equal(t, []string{"H"}, r.Match.IpponsA)
	assert.Equal(t, []string{"f2"}, r.Match.MarkIDs.A)
	assert.Equal(t, 0, r.Match.HansokuB)

	lines = append(lines, removeLine("rH", 22, "f2"), evLine("f3", 23, domain.ScoreFoulAdd, 0, "b", ""))
	r = ReplayScoring(m, lines, ReplayShape{})
	assert.Empty(t, r.Match.IpponsA)
	assert.Equal(t, 1, r.Match.HansokuB)
	r = ReplayScoring(m, append(lines, evLine("u1", 24, domain.ScoreFoulRemove, 0, "b", ""), evLine("u2", 25, domain.ScoreFoulRemove, 0, "b", "")), ReplayShape{})
	assert.Equal(t, 0, r.Match.HansokuB)
	assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("u1"))
	assert.Equal(t, domain.ScoreOutcomeNoTarget, r.Outcome("u2"))
}

// An event id already in the log is a duplicate and counts once.
func TestReplay_DuplicateCountsOnce(t *testing.T) {
	m := runningIndividual()
	e := evLine("e", 20, domain.ScoreIpponAdd, 0, "a", "M")
	r := ReplayScoring(m, append(migrated(m), e, e), ReplayShape{})
	assert.Equal(t, []string{"M"}, r.Match.IpponsA)
	require.Len(t, r.Outcomes, 2)
	assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcomes[0].Outcome)
	assert.Equal(t, domain.ScoreOutcomeDuplicate, r.Outcomes[1].Outcome)
}

// The basis: what a baseline was built with is recorded (never folded
// twice); what it never saw and was made before it is beforeBaseline (D5).
func TestReplay_BasisRules(t *testing.T) {
	ev := &domain.ScoreEvent{ID: "e", Session: "S", Seq: 3}
	for _, c := range []struct {
		name  string
		line  int
		basis state.ScoreBasis
		sess  string
		want  bool
	}{
		{"index below seen", 2, state.ScoreBasis{Seen: 3}, "", true},
		{"index at seen", 3, state.ScoreBasis{Seen: 3}, "", false},
		{"own id", 9, state.ScoreBasis{Own: []string{"e"}}, "", true},
		{"own session seq at basis", 9, state.ScoreBasis{Seq: 3}, "S", true},
		{"own session seq past basis", 9, state.ScoreBasis{Seq: 2}, "S", false},
		{"other session", 9, state.ScoreBasis{Seq: 9}, "T", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := &state.ScoreBaseline{Session: c.sess, Basis: c.basis}
			assert.Equal(t, c.want, BasisContains(b, c.line, ev))
		})
	}

	m := runningIndividual()
	board := runningIndividual()
	board.IpponsA = []string{"M"}
	// e1 is on the board the finish was built from (its own tap), e2 was made
	// on another device before the finish and never seen by it.
	finish := board
	finish.Status, finish.Winner, finish.WinnerID = state.MatchStatusCompleted, "Aka", "pa"
	fin := baseLine(finish, "f", 100, state.ScoreBasis{Seen: 1, Own: []string{"e1"}}, state.GroupPoints, state.GroupResult)
	r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{
		evLine("e1", 90, domain.ScoreIpponAdd, 0, "a", "M"),
		evLine("e2", 95, domain.ScoreIpponAdd, 0, "b", "K"),
		fin,
	}, ReplayShape{Knockout: true})
	assert.Equal(t, []string{"M"}, r.Match.IpponsA, "e1 is not counted twice")
	assert.Empty(t, r.Match.IpponsB)
	assert.Equal(t, []string{"e1"}, r.Match.MarkIDs.A, "the finish keeps the id of the mark it echoes")
	assert.Equal(t, domain.ScoreOutcomeRecorded, r.Outcome("e1"))
	assert.Equal(t, domain.ScoreOutcomeBeforeBaseline, r.Outcome("e2"))
	assert.Equal(t, state.MatchStatusCompleted, r.Match.Status)
}

// R2: points scored after a kiken of this match clear it, with a winner; an
// overtime change alone never does; a fusensho stands; a clear that would
// leave a knockout tied waits and, given nothing more, the kiken stands.
func TestReplay_WithdrawalRules(t *testing.T) {
	kiken := func() *state.MatchResult {
		m := knockoutIndividual()
		m.Status, m.Winner, m.WinnerID = state.MatchStatusCompleted, "Aka", "pa"
		m.Decision, m.DecisionBy = string(domain.DecisionKikenVoluntary), "B"
		m.IpponsA = []string{domain.DefaultWinIppon, domain.DefaultWinIppon}
		m.IpponsB = []string{"M"}
		m.ModifiedAt = 100
		return m
	}
	t.Run("a point gives the other side the win and clears it", func(t *testing.T) {
		m := kiken()
		r := ReplayScoring(m, append(migrated(m), evLine("e", 110, domain.ScoreIpponAdd, 0, "b", "K")), ReplayShape{Knockout: true})
		assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("e"))
		assert.True(t, r.ClearedWithdrawal)
		assert.Equal(t, "Shiro", r.Match.Winner)
		assert.Empty(t, r.Match.Decision)
		assert.Empty(t, r.Match.IpponsA, "the circles go with the withdrawal")
		assert.Equal(t, []string{"M", "K"}, r.Match.IpponsB)
		assert.Equal(t, int64(110), r.Match.GroupStamps[state.GroupResult], "the verdict moved")
	})
	t.Run("an overtime change alone does not clear it", func(t *testing.T) {
		m := kiken()
		r := ReplayScoring(m, append(migrated(m), evLine("e", 110, domain.ScoreEnchoAdd, 0, "", "")), ReplayShape{Knockout: true})
		assert.Equal(t, domain.ScoreOutcomeDefaultWinStands, r.Outcome("e"))
		assert.Equal(t, string(domain.DecisionKikenVoluntary), r.Match.Decision)
		assert.False(t, r.ClearedWithdrawal)
		assert.Nil(t, r.Match.Encho)
	})
	t.Run("a tie waits, then a winner clears it in every order", func(t *testing.T) {
		m := kiken()
		r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{
			evLine("tie", 110, domain.ScoreIpponAdd, 0, "a", "M"),
			evLine("win", 111, domain.ScoreIpponAdd, 0, "a", "K"),
		}, ReplayShape{Knockout: true})
		assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("tie"))
		assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("win"))
		assert.Equal(t, "Aka", r.Match.Winner)
		assert.Empty(t, r.Match.Decision)
		assert.True(t, r.ClearedWithdrawal)
		alone := ReplayScoring(m, append(migrated(m), evLine("tie", 110, domain.ScoreIpponAdd, 0, "a", "M")), ReplayShape{Knockout: true})
		assert.Equal(t, domain.ScoreOutcomeDefaultWinStands, alone.Outcome("tie"))
		assert.Equal(t, string(domain.DecisionKikenVoluntary), alone.Match.Decision)
	})
	t.Run("a fusensho stands", func(t *testing.T) {
		m := kiken()
		m.Decision = string(domain.DecisionFusensho)
		r := ReplayScoring(m, append(migrated(m), evLine("e", 110, domain.ScoreIpponAdd, 0, "b", "K")), ReplayShape{Knockout: true})
		assert.Equal(t, domain.ScoreOutcomeDefaultWinStands, r.Outcome("e"))
		assert.Equal(t, []string{"M"}, r.Match.IpponsB)
	})
	t.Run("in a pool a tie is a draw and clears it", func(t *testing.T) {
		m := kiken()
		m.ID = "Pool A-1"
		r := ReplayScoring(m, append(migrated(m), evLine("e", 110, domain.ScoreIpponAdd, 0, "a", "M")), ReplayShape{})
		assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("e"))
		assert.Equal(t, state.DecisionDraw, r.Match.Decision)
		assert.Empty(t, r.Match.Winner)
		assert.True(t, r.ClearedWithdrawal)
	})
}

// S3: the hantei mark stays while its scoreline is tied and goes when a point
// unties it.
func TestReplay_HanteiStaysWhileTied(t *testing.T) {
	ht := func() *state.MatchResult {
		m := knockoutIndividual()
		m.Status, m.Winner, m.WinnerID = state.MatchStatusCompleted, "Aka", "pa"
		m.IpponsA = []string{"M", domain.HanteiMark}
		m.IpponsB = []string{"K"}
		m.ModifiedAt = 100
		return m
	}
	m := ht()
	r := ReplayScoring(m, append(migrated(m), evLine("f", 110, domain.ScoreFoulAdd, 0, "a", "")), ReplayShape{Knockout: true})
	assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("f"))
	assert.Equal(t, []string{"M", domain.HanteiMark}, r.Match.IpponsA, "a foul leaves the tie and its verdict")
	assert.Equal(t, "Aka", r.Match.Winner)

	m = ht()
	r = ReplayScoring(m, append(migrated(m), evLine("k", 110, domain.ScoreIpponAdd, 0, "b", "D")), ReplayShape{Knockout: true})
	assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("k"))
	assert.Equal(t, []string{"M"}, r.Match.IpponsA, "the point untied it: the mark goes")
	assert.Equal(t, []string{"b:m0:0:a:0"}, r.Match.MarkIDs.A)
	assert.Equal(t, "Shiro", r.Match.Winner)
}

// An engi knockout: an invalid count waits for a valid one.
func TestReplay_EngiFlags(t *testing.T) {
	comp := &state.Competition{Engi: true}
	m := knockoutIndividual()
	m.FlagsA, m.FlagsB = 2, 1
	m.Status, m.Winner, m.WinnerID = state.MatchStatusCompleted, "Aka", "pa"
	m.ModifiedAt = 100
	shape := ReplayShape{Comp: comp, Knockout: true}
	r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{
		evLine("b1", 110, domain.ScoreFlagAdd, 0, "b", ""),
		evLine("b2", 111, domain.ScoreFlagAdd, 0, "b", ""),
	}, shape)
	assert.Equal(t, 3, r.Match.FlagsB)
	assert.Equal(t, "Shiro", r.Match.Winner)
	assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("b1"))

	alone := ReplayScoring(m, append(migrated(m), evLine("b1", 110, domain.ScoreFlagAdd, 0, "b", "")), shape)
	assert.Equal(t, domain.ScoreOutcomeNeedsWinner, alone.Outcome("b1"))
	assert.Equal(t, 1, alone.Match.FlagsB)

	running := knockoutIndividual()
	var lines []state.ScoreLogLine
	lines = append(lines, migrated(running)...)
	for i, id := range []string{"1", "2", "3", "4", "5", "6"} {
		lines = append(lines, evLine(id, int64(20+i), domain.ScoreFlagAdd, 0, "a", ""))
	}
	lines = append(lines, evLine("r", 30, domain.ScoreFlagRemove, 0, "b", ""), evLine("k", 31, domain.ScoreIpponAdd, 0, "a", "M"))
	r = ReplayScoring(running, lines, shape)
	assert.Equal(t, domain.MaxEngiFlags, r.Match.FlagsA)
	assert.Equal(t, domain.ScoreOutcomeCapped, r.Outcome("6"))
	assert.Equal(t, domain.ScoreOutcomeNoTarget, r.Outcome("r"))
	assert.Equal(t, domain.ScoreOutcomeNotAllowed, r.Outcome("k"))
}

func replayTeamComp(kachinuki bool) *state.Competition {
	c := &state.Competition{Kind: "team", TeamSize: 3}
	if kachinuki {
		c.TeamMatchType = state.TeamMatchTypeKachinuki
	}
	return c
}

func replayTeamMatch() *state.MatchResult {
	return &state.MatchResult{
		ID: "r0-m0", SideA: "Kyoto", SideB: "Osaka", SideAID: "ta", SideBID: "tb",
		Status: state.MatchStatusRunning, ModifiedAt: 10,
		SubResults: []state.SubMatchResult{{Position: 1}, {Position: 2}, {Position: 3}},
	}
}

// A fixed-order bout: the row's winner is the team, a Tie and a fusensho are
// bout decisions, and clearing a fusensho gives back the score before it.
func TestReplay_FixedOrderBout(t *testing.T) {
	shape := ReplayShape{Comp: replayTeamComp(false), Knockout: true}
	m := replayTeamMatch()
	lines := append(migrated(m),
		evLine("a1", 20, domain.ScoreIpponAdd, 1, "a", "M"),
		evLine("b1", 21, domain.ScoreIpponAdd, 1, "b", "K"))
	r := ReplayScoring(m, lines, shape)
	row := state.SubResultAt(r.Match.SubResults, 1)
	assert.Equal(t, state.DecisionDraw, row.Decision, "a played tied bout is a hikiwake")
	assert.Empty(t, row.Winner)

	fus := state.ScoreLogLine{V: 1, Event: &domain.ScoreEvent{ID: "fz", Stamp: 22, Kind: domain.ScoreBoutDecision, Pos: 1, Side: "a", Value: domain.BoutDecisionFusensho}}
	r = ReplayScoring(m, append(lines, fus), shape)
	row = state.SubResultAt(r.Match.SubResults, 1)
	assert.Equal(t, string(domain.DecisionFusensho), row.Decision)
	assert.Equal(t, []string{domain.DefaultWinIppon, domain.DefaultWinIppon}, row.IpponsA)
	assert.Equal(t, []string{"K"}, row.IpponsB, "the other side keeps the point it struck")
	assert.Equal(t, "Kyoto", row.Winner)
	assert.Equal(t, []string{"fz", "fz:1"}, row.MarkIDs.A)

	clear := state.ScoreLogLine{V: 1, Event: &domain.ScoreEvent{ID: "cl", Stamp: 23, Kind: domain.ScoreBoutDecision, Pos: 1, Value: domain.BoutDecisionClear}}
	r = ReplayScoring(m, append(lines, fus, clear), shape)
	row = state.SubResultAt(r.Match.SubResults, 1)
	assert.Equal(t, []string{"M"}, row.IpponsA, "the score before the fusensho comes back")
	assert.Equal(t, []string{"a1"}, row.MarkIDs.A)
	assert.Equal(t, state.DecisionDraw, row.Decision)

	// Taking a mark off the side the fusensho went against keeps it.
	rk := removeLine("rk", 23, "b1")
	rk.Event.Pos = 1
	r = ReplayScoring(m, append(lines, fus, rk), shape)
	row = state.SubResultAt(r.Match.SubResults, 1)
	assert.Equal(t, string(domain.DecisionFusensho), row.Decision)
	assert.Empty(t, row.IpponsB)

	// A fusensho against a side that already won the bout is not allowed.
	won := append(migrated(m), evLine("x1", 20, domain.ScoreIpponAdd, 2, "a", "M"), evLine("x2", 21, domain.ScoreIpponAdd, 2, "a", "K"),
		state.ScoreLogLine{V: 1, Event: &domain.ScoreEvent{ID: "fb", Stamp: 22, Kind: domain.ScoreBoutDecision, Pos: 2, Side: "b", Value: domain.BoutDecisionFusensho}})
	r = ReplayScoring(m, won, shape)
	assert.Equal(t, domain.ScoreOutcomeNotAllowed, r.Outcome("fb"))

	// A match-level strike on a team encounter is not a thing.
	r = ReplayScoring(m, append(migrated(m), evLine("z", 20, domain.ScoreIpponAdd, 0, "a", "M")), shape)
	assert.Equal(t, domain.ScoreOutcomeNotAllowed, r.Outcome("z"))
	r = ReplayScoring(m, append(migrated(m), evLine("z", 20, domain.ScoreIpponAdd, 9, "a", "M")), shape)
	assert.Equal(t, domain.ScoreOutcomeNoTarget, r.Outcome("z"))
}

// A finished fixed-order knockout: a bout point that ties the encounter
// waits for one that gives it a winner (R4), in every order.
func TestReplay_FinishedTeamEncounterNeedsWinner(t *testing.T) {
	shape := ReplayShape{Comp: replayTeamComp(false), Knockout: true}
	m := replayTeamMatch()
	m.SubResults[0] = state.SubMatchResult{Position: 1, IpponsA: []string{"M"}, Winner: "Kyoto"}
	m.Status, m.Winner, m.WinnerID = state.MatchStatusCompleted, "Kyoto", "ta"
	m.ModifiedAt = 100
	r := replayAllOrders(t, m, migrated(m), []state.ScoreLogLine{
		evLine("t", 110, domain.ScoreIpponAdd, 2, "b", "K"),
		evLine("w", 111, domain.ScoreIpponAdd, 3, "a", "D"),
	}, shape)
	assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("t"))
	assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("w"))
	assert.Equal(t, "Kyoto", r.Match.Winner)
	alone := ReplayScoring(m, append(migrated(m), evLine("t", 110, domain.ScoreIpponAdd, 2, "b", "K")), shape)
	assert.Equal(t, domain.ScoreOutcomeNeedsWinner, alone.Outcome("t"))
	assert.Empty(t, state.SubResultAt(alone.Match.SubResults, 2).IpponsB)
}

// Kachinuki: events land only on rows that exist; a removed pairing's
// tombstone makes a later event noTarget; raising a recorded bout's
// overtime un-records it; a later row's fighter is an event, the first
// row's is the lineup's.
func TestReplay_Kachinuki(t *testing.T) {
	shape := ReplayShape{Comp: replayTeamComp(true)}
	m := replayTeamMatch()
	m.SubResults = []state.SubMatchResult{
		{Position: 1, SideA: "K1", SideB: "O1", SideAMemberID: "k1", SideBMemberID: "o1", IpponsA: []string{"M", "K"}, Winner: "K1", WinnerMemberID: "k1", Recorded: true},
		{Position: 2, SideA: "K1", SideB: "O2", SideAMemberID: "k1", SideBMemberID: "o2"},
	}
	head := migrated(m)
	require.Contains(t, string(head[0].Baseline.Values[state.GroupRecorded]), "1")

	r := ReplayScoring(m, append(head, evLine("n", 20, domain.ScoreIpponAdd, 3, "a", "M")), shape)
	assert.Equal(t, domain.ScoreOutcomeNoTarget, r.Outcome("n"), "Record bout has not appended bout 3")

	r = ReplayScoring(m, append(head, evLine("w", 20, domain.ScoreIpponAdd, 2, "b", "D")), shape)
	row := state.SubResultAt(r.Match.SubResults, 2)
	assert.Equal(t, "O2", row.Winner, "a kachinuki bout's winner is its fighter")
	assert.Equal(t, "o2", row.WinnerMemberID)

	// The removal of pairing 2 is a baseline whose bout:2 is a tombstone.
	removed := replayTeamMatch()
	removed.SubResults = m.SubResults[:1]
	tomb := baseLine(removed, "rm", 30, state.ScoreBasis{Seen: 1}, state.BoutGroup(2))
	r = ReplayScoring(m, append(head, tomb, evLine("late", 40, domain.ScoreIpponAdd, 2, "a", "M")), shape)
	assert.Len(t, r.Match.SubResults, 1)
	assert.Equal(t, domain.ScoreOutcomeNoTarget, r.Outcome("late"))
	assert.Equal(t, int64(30), r.Match.GroupStamps[state.BoutGroup(2)], "the tombstone keeps its stamp")

	r = ReplayScoring(m, append(head, evLine("e", 20, domain.ScoreEnchoAdd, 1, "", "")), shape)
	assert.False(t, state.SubResultAt(r.Match.SubResults, 1).Recorded, "the bout is fought again")
	assert.Equal(t, int64(20), r.Match.GroupStamps[state.GroupRecorded])

	fighter := func(id string, pos int) state.ScoreLogLine {
		return state.ScoreLogLine{V: 1, Event: &domain.ScoreEvent{ID: id, Stamp: 20, Kind: domain.ScoreBoutFighter, Pos: pos, Side: "a", MemberID: "k2", Name: "K2"}}
	}
	r = ReplayScoring(m, append(head, fighter("f2", 2), fighter("f1", 1)), shape)
	assert.Equal(t, "K2", state.SubResultAt(r.Match.SubResults, 2).SideA)
	assert.Equal(t, domain.ScoreOutcomeNotAllowed, r.Outcome("f1"))

	r = ReplayScoring(m, append(head, evLine("x", 20, domain.ScoreEnchoAdd, 0, "", "")), shape)
	assert.Equal(t, domain.ScoreOutcomeNotAllowed, r.Outcome("x"), "no match-level overtime on a kachinuki encounter")
}

// The representative bout: its add and remove are baselines; a pick lands on
// it, a pick after its removal finds nothing; the verdict only on a tie.
func TestReplay_DaihyosenAddRemoveAndPicks(t *testing.T) {
	shape := ReplayShape{Comp: replayTeamComp(false), Knockout: true}
	m := replayTeamMatch()
	head := migrated(m)
	added := replayTeamMatch()
	added.SubResults = append(added.SubResults, state.SubMatchResult{Position: -1, SideA: "Kyoto", SideB: "Osaka", Decision: string(domain.DecisionDaihyosen)})
	add := baseLine(added, "add", 30, state.ScoreBasis{Seen: 1}, state.BoutGroup(-1), state.GroupRepPickA, state.GroupRepPickB)
	pick := state.ScoreLogLine{V: 1, Event: &domain.ScoreEvent{ID: "p", Stamp: 40, Kind: domain.ScoreRepPick, Pos: -1, Side: "a", MemberID: "k3"}}
	verdict := state.ScoreLogLine{V: 1, Event: &domain.ScoreEvent{ID: "v", Stamp: 41, Kind: domain.ScoreDaihyosenVerdict, Pos: -1, Side: "b"}}
	r := ReplayScoring(m, append(head, add, pick, verdict), shape)
	rep := state.SubResultAt(r.Match.SubResults, -1)
	assert.Equal(t, "k3", rep.SideAMemberID)
	assert.Equal(t, []string{domain.HanteiMark}, rep.IpponsB)
	assert.Equal(t, []string{"v"}, rep.MarkIDs.B)
	assert.Equal(t, "Osaka", rep.Winner)
	assert.Equal(t, string(domain.DecisionDaihyosen), rep.Decision)

	// A point unties it: the verdict is not allowed then.
	pt := evLine("pt", 41, domain.ScoreIpponAdd, -1, "a", "M")
	r = ReplayScoring(m, append(head, add, pt, state.ScoreLogLine{V: 1, Event: &domain.ScoreEvent{ID: "v2", Stamp: 42, Kind: domain.ScoreDaihyosenVerdict, Pos: -1, Side: "b"}}), shape)
	assert.Equal(t, domain.ScoreOutcomeNotAllowed, r.Outcome("v2"))

	removed := replayTeamMatch()
	remove := baseLine(removed, "rm", 50, state.ScoreBasis{Seen: 4}, state.BoutGroup(-1), state.GroupRepPickA, state.GroupRepPickB)
	late := state.ScoreLogLine{V: 1, Event: &domain.ScoreEvent{ID: "late", Stamp: 60, Kind: domain.ScoreRepPick, Pos: -1, Side: "b", MemberID: "o3"}}
	r = ReplayScoring(m, append(head, add, pick, remove, late), shape)
	assert.Equal(t, state.SubPositions(r.Match.SubResults), []int{1, 2, 3})
	assert.Equal(t, domain.ScoreOutcomeNoTarget, r.Outcome("late"))
	assert.Equal(t, domain.ScoreOutcomeRecorded, r.Outcome("p"), "the removal had the pick in view")
}

// Lazy migration: a match with no log is the match as stored; its first
// baseline is the stored match at the stamps it holds.
func TestReplay_LazyMigration(t *testing.T) {
	t.Run("no log", func(t *testing.T) {
		m := runningIndividual()
		m.IpponsA = []string{"M"}
		r := ReplayScoring(m, nil, ReplayShape{})
		assert.Equal(t, *m, r.Match)
		assert.Empty(t, r.Outcomes)
	})
	t.Run("legacy unstamped", func(t *testing.T) {
		m := knockoutIndividual()
		m.IpponsA = []string{"M"}
		b := MigrationBaseline(m, "m0", 0)
		assert.Empty(t, b.Stamps)
		r := ReplayScoring(m, append(migrated(m), evLine("e", 5, domain.ScoreIpponAdd, 0, "b", "K")), ReplayShape{})
		assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("e"))
		assert.Equal(t, []string{"b:m0:0:a:0"}, r.Match.MarkIDs.A)
	})
	t.Run("stamped: an older event is before it", func(t *testing.T) {
		m := runningIndividual()
		m.ModifiedAt = 50
		m.GroupStamps = map[string]int64{state.GroupPoints: 50, state.GroupResult: 40}
		r := ReplayScoring(m, append(migrated(m), evLine("old", 45, domain.ScoreIpponAdd, 0, "b", "K"), evLine("new", 55, domain.ScoreIpponAdd, 0, "a", "M")), ReplayShape{})
		assert.Equal(t, domain.ScoreOutcomeBeforeBaseline, r.Outcome("old"))
		assert.Equal(t, domain.ScoreOutcomeApplied, r.Outcome("new"))
		assert.Equal(t, int64(55), r.Match.GroupStamps[state.GroupPoints])
		assert.Equal(t, int64(40), r.Match.GroupStamps[state.GroupResult])
		assert.Equal(t, int64(55), r.Match.ModifiedAt)
	})
	t.Run("a tombstone stays one", func(t *testing.T) {
		m := replayTeamMatch()
		m.SubResults = m.SubResults[:1]
		m.GroupStamps = map[string]int64{state.BoutGroup(1): 10, state.BoutGroup(2): 12}
		b := MigrationBaseline(m, "m0", 0)
		assert.Equal(t, "null", string(b.Values[state.BoutGroup(2)]))
		r := ReplayScoring(m, append(migrated(m), evLine("e", 20, domain.ScoreIpponAdd, 2, "a", "M")), ReplayShape{Comp: replayTeamComp(true)})
		assert.Equal(t, domain.ScoreOutcomeNoTarget, r.Outcome("e"))
		assert.Equal(t, int64(12), r.Match.GroupStamps[state.BoutGroup(2)])
	})
}

// A replay of the log a match was replayed from changes nothing: a baseline
// built from the replayed match (every event in view) holds the same values,
// stamps and mark ids.
func TestReplay_BaselineOfTheReplayIsTheReplay(t *testing.T) {
	m := runningIndividual()
	lines := append(migrated(m),
		evLine("a", 20, domain.ScoreIpponAdd, 0, "a", "M"),
		evLine("f1", 21, domain.ScoreFoulAdd, 0, "a", ""),
		evLine("f2", 22, domain.ScoreFoulAdd, 0, "a", ""))
	r := ReplayScoring(m, lines, ReplayShape{})
	b := BaselineFromMatch(&r.Match, "srv", "test", 30, append([]string(nil), state.ScalarGroups...), state.ScoreBasis{Seen: len(lines)})
	r2 := ReplayScoring(m, append(lines, state.ScoreLogLine{V: 1, Baseline: &b}), ReplayShape{})
	assert.Equal(t, r.Match.IpponsA, r2.Match.IpponsA)
	assert.Equal(t, r.Match.IpponsB, r2.Match.IpponsB)
	assert.Equal(t, r.Match.MarkIDs, r2.Match.MarkIDs)
	for _, id := range []string{"a", "f1", "f2"} {
		assert.Equal(t, domain.ScoreOutcomeRecorded, r2.Outcome(id))
	}
}

// Seq gaps: session S holding 1, 2, 3 and 5 is missing 4.
func TestScoreLogGaps(t *testing.T) {
	ev := func(id, session string, seq int64) state.ScoreLogLine {
		return state.ScoreLogLine{V: 1, Event: &domain.ScoreEvent{ID: id, Session: session, Seq: seq, Stamp: 1, Kind: domain.ScoreEnchoAdd}}
	}
	b := MigrationBaseline(runningIndividual(), "m0", 0)
	gaps := ScoreLogGaps([]state.ScoreLogLine{
		{V: 1, Baseline: &b},
		ev("1", "S", 1), ev("2", "S", 2), ev("3", "S", 3), ev("5", "S", 5),
		ev("t2", "T", 2), ev("t1", "T", 1),
		ev("u", "U", 0),
	})
	assert.Equal(t, map[string][]int64{"S": {4}}, gaps)
	assert.Empty(t, ScoreLogGaps(nil))
	assert.Equal(t, map[string][]int64{"V": {1, 2}}, ScoreLogGaps([]state.ScoreLogLine{ev("v", "V", 3)}))
}

// The log lives beside the history: DiscardDraw removes it, a transaction
// that aborts leaves no line, and a line written after the competition is
// deleted does not bring its folder back.
func TestScoreLog_StoreLifecycle(t *testing.T) {
	line := evLine("e", 20, domain.ScoreIpponAdd, 0, "a", "M")
	t.Run("aborted transaction", func(t *testing.T) {
		h := mmIndividual(t, false)
		boom := errors.New("boom")
		err := h.store.WithTransaction(h.compID, func(tx state.StoreTx) error {
			require.NoError(t, tx.AppendScoreLog(h.compID, h.matchID, line))
			got, err := tx.LoadScoreLog(h.compID, h.matchID)
			require.NoError(t, err)
			require.Len(t, got, 1, "the transaction reads its own line")
			return boom
		})
		require.ErrorIs(t, err, boom)
		got, err := h.store.LoadScoreLog(h.compID, h.matchID)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("committed transaction", func(t *testing.T) {
		h := mmIndividual(t, false)
		require.NoError(t, h.store.WithTransaction(h.compID, func(tx state.StoreTx) error {
			return tx.AppendScoreLog(h.compID, h.matchID, line)
		}))
		got, err := h.store.LoadScoreLog(h.compID, h.matchID)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, state.ScoreLogFormatVersion, got[0].V)
		assert.Equal(t, "e", got[0].Event.ID)
	})
	t.Run("discarded draw", func(t *testing.T) {
		h := mmIndividual(t, false)
		require.NoError(t, h.store.AppendScoreLog(h.compID, h.matchID, line))
		_, err := h.store.UpdateCompetitionChanged(h.compID, func(c *state.Competition) (*state.Competition, error) {
			c.Status = state.CompStatusDrawReady
			return c, nil
		})
		require.NoError(t, err)
		require.NoError(t, h.eng.DiscardDraw(h.compID))
		got, err := h.store.LoadScoreLog(h.compID, h.matchID)
		require.NoError(t, err)
		assert.Empty(t, got, "a draw generated again reuses the match ids")
		_, err = os.Stat(filepath.Join(h.dir, "competitions", h.compID, "events"))
		assert.True(t, os.IsNotExist(err))
	})
	t.Run("deleted competition", func(t *testing.T) {
		h := mmIndividual(t, false)
		require.NoError(t, h.store.DeleteCompetition(h.compID))
		require.Error(t, h.store.AppendScoreLog(h.compID, h.matchID, line))
		_, err := os.Stat(filepath.Join(h.dir, "competitions", h.compID))
		assert.True(t, os.IsNotExist(err), "the directory is not resurrected")
	})
}

// The rollback projection carries the mark ids and the log version, and a
// pool write never lowers the version (only the replay's write carries one).
func TestMarkIDsAndScoreVersionThroughTheWrites(t *testing.T) {
	bm := &state.BracketMatch{ID: "r0-m0", IpponsA: []string{"M"}, MarkIDs: &state.MarkIDs{A: []string{"e1"}}, ScoreVersion: 3}
	p := bracketMatchAsResult(bm)
	assert.Equal(t, bm.MarkIDs, p.MarkIDs)
	assert.Equal(t, 3, p.ScoreVersion)
	p.MarkIDs.A[0] = "x"
	assert.Equal(t, "e1", bm.MarkIDs.A[0], "the projection does not share the ids")

	stored := &state.MatchResult{ID: "Pool A-1", SideA: "A", SideB: "B", Status: state.MatchStatusRunning, ScoreVersion: 5}
	incoming := &state.MatchResult{ID: "Pool A-1", SideA: "A", SideB: "B", Status: state.MatchStatusRunning, IpponsA: []string{"M"}, MarkIDs: &state.MarkIDs{A: []string{"e9"}}}
	_, _, _, err := applyPoolWrite(stored, incoming, matchWriteForward, nil)
	require.NoError(t, err)
	assert.Equal(t, 5, stored.ScoreVersion, "a write without a version keeps the stored one")
	assert.Equal(t, []string{"e9"}, stored.MarkIDs.A)

	restore := &state.MatchResult{ID: "Pool A-1", SideA: "A", SideB: "B", Status: state.MatchStatusRunning, ScoreVersion: 2}
	_, _, _, err = applyPoolWrite(stored, restore, matchWriteRestore, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, stored.ScoreVersion, "a restore puts back the snapshot's")
}
