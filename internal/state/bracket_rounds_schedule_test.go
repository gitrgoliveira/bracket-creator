package state_test

// bracket_rounds_schedule_test.go pins what RestampRoundsFromFeeders does to
// scheduled times (bc-tmfn). A court's queue is ordered by time, and every
// release before match-number scheduling gave a court its times in storage
// order, so a stored bracket with byes can list Match 2 before Match 1. On a
// bracket nobody has started, a court whose times still rise in storage order
// has its own times handed out again, earliest to the lowest match number;
// once a real bout has been touched, or when a court's times are not the old
// scheduler's, they stay exactly as stored. Either way this happens once: the
// bracket is then TimesSettled and its times are never examined again.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unstartedV21FiveEntrant is the v2.1.0 fixture with its one recorded result
// (P4 beat P5 M-K) taken back, as if the draw had been generated and nothing
// fought yet: P4 v P5 is scheduled again and P3 waits on its winner.
func unstartedV21FiveEntrant(t *testing.T) *state.Bracket {
	t.Helper()
	b := loadV21FiveEntrant(t)
	m := matchByID(t, b, "m-r1-3")
	m.Status = state.MatchStatusScheduled
	m.Winner, m.WinnerID = "", ""
	m.IpponsA, m.IpponsB = nil, nil
	m.ResultSource = ""
	m.ModifiedAt = 0
	next := matchByID(t, b, "m-r2-1")
	next.SideB, next.SideBID = "Winner of r3-m3", ""
	return b
}

// setRoundNumberTime sets one match's round, number and time on want.
func setRoundNumberTime(t *testing.T, b *state.Bracket, id string, round, number int, at string) {
	t.Helper()
	m := matchByID(t, b, id)
	m.DisplayRound, m.MatchNumber, m.ScheduledAt = round, number, at
}

func TestRestampRoundsFromFeeders_UnstartedBracketIsScheduledInNumberOrder(t *testing.T) {
	got := unstartedV21FiveEntrant(t)
	require.Equal(t, "09:00", matchByID(t, got, "m-r1-0").ScheduledAt, "fixture: v2.1.0 gave its Match 1 the first time")

	changes, err := got.RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Equal(t, []state.BracketRoundChange{
		{ID: "m-r1-0", OldRound: 3, NewRound: 2, OldNumber: 1, NewNumber: 2, OldScheduledAt: "09:00", NewScheduledAt: "09:05"},
		{ID: "m-r1-3", OldRound: 3, NewRound: 3, OldNumber: 2, NewNumber: 1, OldScheduledAt: "09:05", NewScheduledAt: "09:00"},
	}, changes)

	// Match 1 (P4 v P5) now holds the court's first time and Match 2 (P1 v
	// P2) the next. Everything else, the Hidden rows' times and the bronze's
	// included, is exactly as stored.
	want := unstartedV21FiveEntrant(t)
	want.TimesSettled = true
	setRoundNumberTime(t, want, "m-r1-3", 3, 1, "09:00")
	setRoundNumberTime(t, want, "m-r1-0", 2, 2, "09:05")
	setRoundNumberTime(t, want, "m-r2-1", 2, 3, "09:10")
	setRoundNumberTime(t, want, "m-r3-0", 1, 4, "09:15")
	assert.Equal(t, want, got)

	again, err := got.RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Empty(t, again, "a second pass finds nothing to move")
}

// Anything a result write leaves on a real bout keeps the whole bracket's
// times: the court may already be playing in the order they give.
func TestRestampRoundsFromFeeders_TouchedBracketKeepsItsTimes(t *testing.T) {
	touches := []struct {
		name  string
		touch func(m *state.BracketMatch)
	}{
		{"running", func(m *state.BracketMatch) { m.Status = state.MatchStatusRunning }},
		{"completed", func(m *state.BracketMatch) { m.Status = state.MatchStatusCompleted }},
		{"a winner", func(m *state.BracketMatch) { m.Winner = "P3" }},
		{"a winner id", func(m *state.BracketMatch) { m.WinnerID = "d2bcc7a9-0254-474f-9bbe-016464d5a925" }},
		{"an ippon for side A", func(m *state.BracketMatch) { m.IpponsA = []string{"M"} }},
		{"an ippon for side B", func(m *state.BracketMatch) { m.IpponsB = []string{"K"} }},
		{"a hansoku on side A", func(m *state.BracketMatch) { m.HansokuA = 1 }},
		{"a hansoku on side B", func(m *state.BracketMatch) { m.HansokuB = 1 }},
		{"sub-bouts", func(m *state.BracketMatch) { m.SubResults = []state.SubMatchResult{{Position: 1}} }},
		{"a decision", func(m *state.BracketMatch) { m.Decision = "kiken-voluntary" }},
		{"overtime", func(m *state.BracketMatch) { m.Encho = &state.EnchoMetadata{PeriodCount: 1} }},
		{"a write stamp (started, then put back in the queue)", func(m *state.BracketMatch) { m.ModifiedAt = 1790337238130 }},
	}
	for _, tc := range touches {
		t.Run(tc.name, func(t *testing.T) {
			got := unstartedV21FiveEntrant(t)
			tc.touch(matchByID(t, got, "m-r2-1")) // P3 v W(P4 v P5), a real bout
			_, err := got.RestampRoundsFromFeeders()
			require.NoError(t, err)

			assert.Equal(t, 1, matchByID(t, got, "m-r1-3").MatchNumber, "the numbers are corrected regardless")
			assert.Equal(t, "09:05", matchByID(t, got, "m-r1-3").ScheduledAt, "a touched bracket keeps its times")
			assert.Equal(t, "09:00", matchByID(t, got, "m-r1-0").ScheduledAt, "a touched bracket keeps its times")
		})
	}
}

// Renumbering is not what makes the times wrong: every release before
// match-number scheduling gave a court its times in storage order. A bracket
// whose rounds and numbers already follow the rule (as a release before
// v2.0.0 stored this shape) still lists Match 2 (P1 v P2, stored first) at
// 09:00, and gets the same repair.
func TestRestampRoundsFromFeeders_StorageOrderTimesAreRepairedWithoutARenumbering(t *testing.T) {
	got := unstartedV21FiveEntrant(t)
	setRoundNumberTime(t, got, "m-r1-0", 2, 2, "09:00")
	setRoundNumberTime(t, got, "m-r1-3", 3, 1, "09:05")

	changes, err := got.RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Equal(t, []state.BracketRoundChange{
		{ID: "m-r1-0", OldRound: 2, NewRound: 2, OldNumber: 2, NewNumber: 2, OldScheduledAt: "09:00", NewScheduledAt: "09:05"},
		{ID: "m-r1-3", OldRound: 3, NewRound: 3, OldNumber: 1, NewNumber: 1, OldScheduledAt: "09:05", NewScheduledAt: "09:00"},
	}, changes)
}

// A time the operator moved by hand is not undone on the next load: the
// first pass settles the bracket, and a settled bracket's times stay as they
// are. (engine's TestBracketTimesTheOperatorMovedSurviveAReload pins the move
// that leaves a court rising in storage order again, which only the marker
// tells apart from the old scheduling.)
func TestRestampRoundsFromFeeders_HandMovedTimesSurvive(t *testing.T) {
	b := unstartedV21FiveEntrant(t)
	_, err := b.RestampRoundsFromFeeders()
	require.NoError(t, err)
	// P4 v P5 (Match 1) waits for a late competitor.
	matchByID(t, b, "m-r1-3").ScheduledAt = "09:30"
	moved := *b
	moved.Rounds = cloneRounds(b.Rounds)

	changes, err := b.RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Empty(t, changes)
	assert.Equal(t, &moved, b)
}

// Times never cross from one court to another: each court hands out only its
// own.
func TestRestampRoundsFromFeeders_TimesStayOnTheirCourt(t *testing.T) {
	got := unstartedV21FiveEntrant(t)
	// P3 v W(P4 v P5) (to be Match 3) on a second court that opens earlier.
	// Court A keeps P1 v P2 at 09:00, P4 v P5 at 09:05 and the final at 09:15,
	// rising in storage order as the old scheduler left them.
	other := matchByID(t, got, "m-r2-1")
	other.Court, other.ScheduledAt = "B", "08:55"

	_, err := got.RestampRoundsFromFeeders()
	require.NoError(t, err)
	require.Equal(t, 1, matchByID(t, got, "m-r1-3").MatchNumber)
	assert.Equal(t, "09:00", matchByID(t, got, "m-r1-3").ScheduledAt, "Match 1 takes court A's first time")
	assert.Equal(t, "09:05", matchByID(t, got, "m-r1-0").ScheduledAt)
	assert.Equal(t, "09:15", matchByID(t, got, "m-r3-0").ScheduledAt)
	assert.Equal(t, "08:55", matchByID(t, got, "m-r2-1").ScheduledAt, "court B keeps its one time")
}

// A court with a real bout that carries no time, or one that does not read as
// a clock time, is left as it is: handing out the times anyway would invent a
// slot or drop one.
func TestRestampRoundsFromFeeders_CourtWithAnUnreadableTimeIsLeftAlone(t *testing.T) {
	for name, at := range map[string]string{"no time": "", "not a clock time": "soon"} {
		t.Run(name, func(t *testing.T) {
			got := unstartedV21FiveEntrant(t)
			matchByID(t, got, "m-r2-1").ScheduledAt = at

			_, err := got.RestampRoundsFromFeeders()
			require.NoError(t, err)
			assert.Equal(t, 1, matchByID(t, got, "m-r1-3").MatchNumber, "the numbers are corrected regardless")
			assert.Equal(t, "09:05", matchByID(t, got, "m-r1-3").ScheduledAt)
			assert.Equal(t, "09:00", matchByID(t, got, "m-r1-0").ScheduledAt)
			assert.Equal(t, at, matchByID(t, got, "m-r2-1").ScheduledAt)
		})
	}
}

func cloneRounds(rounds [][]state.BracketMatch) [][]state.BracketMatch {
	out := make([][]state.BracketMatch, len(rounds))
	for i := range rounds {
		out[i] = append([]state.BracketMatch(nil), rounds[i]...)
	}
	return out
}
