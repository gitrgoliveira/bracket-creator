package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// barredTiebreakLeague is an individual league whose top two, Ayu and Ben, drew
// each other and each beat Cal, so an ippon-shobu bout between them is owed. It
// returns the engine, the store and that bout's id once the scan has added it.
func barredTiebreakLeague(t *testing.T, compID string) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	players := []helper.Player{
		{ID: "ayu-id", Name: "Ayu", Dojo: "Dojo A"},
		{ID: "ben-id", Name: "Ben", Dojo: "Dojo B"},
		{ID: "cal-id", Name: "Cal", Dojo: "Dojo C"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ayu", SideAID: "ayu-id", SideB: "Ben", SideBID: "ben-id", Court: "A",
			Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)},
		{ID: "Pool A-1", SideA: "Ayu", SideAID: "ayu-id", SideB: "Cal", SideBID: "cal-id", Court: "A",
			Status: state.MatchStatusCompleted, Winner: "Ayu", WinnerID: "ayu-id", IpponsA: []string{"M"}},
		{ID: "Pool A-2", SideA: "Ben", SideAID: "ben-id", SideB: "Cal", SideBID: "cal-id", Court: "A",
			Status: state.MatchStatusCompleted, Winner: "Ben", WinnerID: "ben-id", IpponsA: []string{"M"}},
	}))
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTiebreakInjected, outcome, "the tie owes its ippon-shobu bout")
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range all {
		if IsTiebreakerMatchID(m.ID) {
			return eng, store, m.ID
		}
	}
	t.Fatal("no tie-break bout was added")
	return nil, nil, ""
}

func barCompetitor(t *testing.T, store *state.Store, compID, id string, barred bool) {
	t.Helper()
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: id, Eligible: !barred, Reason: "kiken-voluntary: withdrew", MatchID: "Pool A-1", RecordedAt: time.Now(),
	}))
}

// An individual league's ippon-shobu bout between two barred competitors is
// recorded as the draw the decision door writes for it, and settles while both
// stay barred, so the league can finish with the place shared (plan item 6).
func TestIndividualLeague_BothBarredTiebreakDrawSettles(t *testing.T) {
	const compID = "ind-barred-tb"
	eng, store, tbID := barredTiebreakLeague(t, compID)

	_, err := eng.RecordMatchResultWithIneligibility(compID, tbID, &state.MatchResult{
		Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
		Changed: []string{state.GroupResult}, WriteDoor: DoorDecision,
	})
	require.NoError(t, err, "the draw nobody can fight is not refused")

	barCompetitor(t, store, compID, "ayu-id", true)
	assert.Equal(t, PoolsMatchOpen, poolsVerdictNow(t, eng, store, compID), "one side still able to fight: the draw settles nothing")

	barCompetitor(t, store, compID, "ben-id", true)
	assert.Equal(t, PoolsSettled, poolsVerdictNow(t, eng, store, compID), "both sides barred: nobody can fight it")
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "the league finishes")

	barCompetitor(t, store, compID, "ben-id", false)
	assert.Equal(t, PoolsMatchOpen, poolsVerdictNow(t, eng, store, compID), "a side reinstated opens the bout again")
}

// An ordinary draw on the same bout stays refused, bars or not.
func TestIndividualLeague_PlainTiebreakDrawStaysRefused(t *testing.T) {
	const compID = "ind-plain-tb"
	eng, store, tbID := barredTiebreakLeague(t, compID)
	barCompetitor(t, store, compID, "ayu-id", true)
	barCompetitor(t, store, compID, "ben-id", true)

	_, err := eng.RecordMatchResultWithIneligibility(compID, tbID, &state.MatchResult{
		Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
	})
	require.ErrorIs(t, err, ErrTiebreakNeedsWinner)
}

// A representative bout between two barred teams keeps its refusal: a team
// league's way on is accepting the shared ranks.
func TestDrawnTiebreakOfBarredSides_OnlyAnIpponShobuBout(t *testing.T) {
	statuses := map[string]domain.CompetitorStatus{
		"a": {PlayerID: "a", Eligible: false, Reason: "kiken-voluntary: x", MatchID: "Pool A-0"},
		"b": {PlayerID: "b", Eligible: false, Reason: "kiken-voluntary: x", MatchID: "Pool A-0"},
	}
	drawn := func(id string) *state.MatchResult {
		return &state.MatchResult{ID: id, SideAID: "a", SideBID: "b", Status: state.MatchStatusCompleted}
	}
	assert.True(t, drawnTiebreakOfBarredSides(drawn("Pool A-TB-0"), statuses))
	assert.False(t, drawnTiebreakOfBarredSides(drawn("Pool A-DH-0"), statuses), "a representative bout is Remove and Accept")
	assert.False(t, drawnTiebreakOfBarredSides(drawn("Pool A-0"), statuses), "a regular match is not a tie-breaker")
	won := drawn("Pool A-TB-0")
	won.Winner = "a"
	assert.False(t, drawnTiebreakOfBarredSides(won, statuses), "a bout with a winner is not drawn")
	assert.False(t, drawnTiebreakOfBarredSides(drawn("Pool A-TB-0"), nil), "no statuses, nobody barred")
}
