package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A waiting knockout match moved onto a court lands last among the matches
// that court can play, never behind a later round still waiting on it: the
// final it feeds has "Winner of ..." sides and is not a queue entry yet.
func TestUpdateMatchCourt_WaitingKnockoutMoveLandsBeforeTheRoundItFeeds(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compKo := "feeds-ko"
	compPool := "feeds-pool"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compKo, Name: "KO", Format: state.CompFormatKnockout}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compPool, Name: "League", Format: state.CompFormatLeague}))
	require.NoError(t, store.SaveBracket(compKo, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "r1-m0", MatchNumber: 1, DisplayRound: 2, SideA: "Alice", SideAID: "alice", SideB: "Bob", SideBID: "bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
				{ID: "r1-m1", MatchNumber: 2, DisplayRound: 2, SideA: "Carol", SideAID: "carol", SideB: "Dave", SideBID: "dave", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "10:05"},
			},
			{
				{ID: "r2-m0", MatchNumber: 3, DisplayRound: 1, SideA: "Winner of r1-m0", SideB: "Winner of r1-m1", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "11:00"},
			},
		},
		// The bronze match's sides stay empty until both semi-finals are played.
		ThirdPlaceMatch: &state.BracketMatch{
			ID: state.BronzeMatchID, DisplayRound: -1, Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "11:30",
		},
	}))
	require.NoError(t, store.SavePoolMatches(compPool, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Eve", SideAID: "eve", SideB: "Frank", SideBID: "frank", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:30"},
	}))

	require.NoError(t, eng.UpdateMatchCourt(compKo, "r1-m0", "B", CourtMove{}))

	b, err := store.LoadBracket(compKo)
	require.NoError(t, err)
	sf := b.MatchByID("r1-m0")
	require.NotNil(t, sf)
	assert.Equal(t, "B", sf.Court)
	assert.Equal(t, "10:31", sf.ScheduledAt, "after the league match B can play, not after the final it feeds")
	assert.Equal(t, "11:00", b.MatchByID("r2-m0").ScheduledAt, "the final keeps its time")
	assert.Less(t, sf.ScheduledAt, b.MatchByID("r2-m0").ScheduledAt, "the semi-final is queued before the final it feeds")
}

// mixedKnockoutOnB is a pools + knockout competition whose knockout waits on
// court B at 10:00 and 10:05 while its pool bouts still run on A and B. The
// knockout's first match already has its two qualifiers when resolvedFirst is
// set (the pools that feed it have finished), the other one has not.
func mixedKnockoutOnB(t *testing.T, store *state.Store, compID string, resolvedFirst bool) {
	t.Helper()
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Courts = []string{"A", "B"}
		c.StartTime = "09:00"
	})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool C-0", SideA: "P1", SideAID: "p1", SideB: "P2", SideBID: "p2", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "Pool C-1", SideA: "P1", SideAID: "p1", SideB: "P3", SideBID: "p3", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:05"},
		{ID: "Pool D-0", SideA: "P4", SideAID: "p4", SideB: "P5", SideBID: "p5", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "09:00"},
		{ID: "Pool D-1", SideA: "P4", SideAID: "p4", SideB: "P6", SideBID: "p6", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "09:05"},
		{ID: "Pool D-2", SideA: "P5", SideAID: "p5", SideB: "P6", SideBID: "p6", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "09:10"},
	}))
	first := state.BracketMatch{ID: "r1-m0", MatchNumber: 1, DisplayRound: 2, SideA: "Pool A-1st", SideB: "Pool B-2nd", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"}
	if resolvedFirst {
		first.SideA, first.SideAID, first.SideB, first.SideBID = "Q1", "q1", "Q2", "q2"
	}
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				first,
				{ID: "r1-m1", MatchNumber: 2, DisplayRound: 2, SideA: "Pool C-1st", SideB: "Pool D-2nd", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:05"},
			},
		},
	}))
}

// A waiting pool bout moved onto a court lands after that court's pool bouts
// and before its own competition's knockout there, which moves only when the
// pool phase now runs into it: two moves in a row leave it where it was.
func TestUpdateMatchCourt_PoolBoutMovesLeaveTheKnockoutTimes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		resolvedFirst bool
	}{
		{"knockout waiting on its pools", false},
		{"knockout first match already has its qualifiers", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "pool-moves-keep-ko"
			mixedKnockoutOnB(t, store, compID, tc.resolvedFirst)

			require.NoError(t, eng.UpdateMatchCourt(compID, "Pool C-0", "B", CourtMove{}))
			require.NoError(t, eng.UpdateMatchCourt(compID, "Pool C-1", "B", CourtMove{}))

			assert.Equal(t, "09:11", loadPoolMatchByID(t, store, compID, "Pool C-0").ScheduledAt)
			assert.Equal(t, "09:12", loadPoolMatchByID(t, store, compID, "Pool C-1").ScheduledAt)
			b, err := store.LoadBracket(compID)
			require.NoError(t, err)
			assert.Equal(t, "10:00", b.Rounds[0][0].ScheduledAt, "the knockout does not drift with each pool bout moved")
			assert.Equal(t, "10:05", b.Rounds[0][1].ScheduledAt, "the knockout does not drift with each pool bout moved")
		})
	}
}
