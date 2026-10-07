package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noCascadeSetup(t *testing.T, compID string) (*Engine, *state.Store, string, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, compID, "league", 3)
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideAID: aliceID, SideB: "Bob", SideBID: bobID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideAID: aliceID, SideB: "Carol", SideBID: carolID, Status: state.MatchStatusScheduled},
	}))
	return eng, store, aliceID, bobID
}

// A match already closed with a fusensho because of the first withdrawal keeps
// it when the withdrawal moves to the other side: nothing cascades.
func TestRecordDecision_MovingAWithdrawalDoesNotReopenMatchesClosedByIt(t *testing.T) {
	eng, store, aliceID, bobID := noCascadeSetup(t, "dlck-pool-no-cascade")
	compID := "dlck-pool-no-cascade"

	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "aka", "", nil)
	require.NoError(t, err)
	// Carol's match against the withdrawn Alice is closed by a fusensho.
	_, _, err = eng.RecordDecision(compID, "Pool A-1", "fusensho", "aka", "", nil)
	require.NoError(t, err)

	_, _, err = eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "shiro", "", nil)
	require.NoError(t, err, "no confirm: a later match is already closed")

	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == "Pool A-1" {
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
			assert.Equal(t, "fusensho", string(m.Decision), "closed by the first withdrawal, it keeps its decision")
			assert.Equal(t, "Carol", m.Winner)
		}
	}
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.True(t, statuses[aliceID].Eligible)
	assert.False(t, statuses[bobID].Eligible)
}

// A competitor with a fusenpai recorded on another match stays withdrawn
// because of that match when the first withdrawal moves to the other side.
func TestRecordDecision_ChainedFusenpaiKeepsTheBarWhenTheWithdrawalMoves(t *testing.T) {
	eng, store, aliceID, bobID := noCascadeSetup(t, "dlck-chained")
	compID := "dlck-chained"

	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "aka", "", nil)
	require.NoError(t, err)
	_, _, err = eng.RecordDecision(compID, "Pool A-1", "fusenpai", "aka", "", nil)
	require.NoError(t, err)

	_, _, err = eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "shiro", "", nil)
	require.NoError(t, err)

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.False(t, statuses[aliceID].Eligible, "the fusenpai on Pool A-1 still bars Alice")
	assert.Equal(t, "Pool A-1", statuses[aliceID].MatchID)
	assert.False(t, statuses[bobID].Eligible)
}
