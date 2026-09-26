package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-sbq (operator ruling 2026-09-26): a match sent back to the queue keeps
// its score, and starting it again carries on from there. The court console's
// Start match sends empty ippon arrays and zero counts (the wire has no "no
// score sent"), so a StartOnly write keeps the stored score instead of
// replacing it; an unflagged write with the same empty payload is an operator
// clearing every mark and still clears them.

// startWrite is the payload the SPA's startPatch sends.
func startWrite() *state.MatchResult {
	return &state.MatchResult{Status: state.MatchStatusRunning, IpponsA: []string{}, IpponsB: []string{}}
}

func setupStartComp(t *testing.T, comp *state.Competition) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(comp))
	return eng, store
}

func poolMatch(t *testing.T, store *state.Store, compID, id string) state.MatchResult {
	t.Helper()
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("pool match %s not found", id)
	return state.MatchResult{}
}

func TestStartOnly_PoolIndividualKeepsTheQueuedScore(t *testing.T) {
	const compID = "start-keeps-ind"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Ind", Format: state.CompFormatMixed, Courts: []string{"A"}})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Court: "A", Status: state.MatchStatusScheduled,
		IpponsA: []string{"M"}, HansokuB: 1, Encho: &state.EnchoMetadata{PeriodCount: 1},
	}}))

	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", startWrite(), ForceOptions{StartOnly: true})
	require.NoError(t, err)

	m := poolMatch(t, store, compID, "Pool A-0")
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, []string{"M"}, m.IpponsA, "the kept point is still there")
	assert.Equal(t, 1, m.HansokuB, "the kept penalty is still there")
	require.NotNil(t, m.Encho)
	assert.Equal(t, 1, m.Encho.PeriodCount)
}

func TestStartOnly_UnflaggedEmptyWriteStillClears(t *testing.T) {
	// The editor's own Start sends its board. An operator who cleared every
	// kept mark on it sends exactly the empty payload, and that must clear.
	const compID = "start-unflagged-clears"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Ind", Format: state.CompFormatMixed, Courts: []string{"A"}})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Court: "A", Status: state.MatchStatusScheduled,
		IpponsA: []string{"M"}, HansokuB: 1,
	}}))

	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", startWrite())
	require.NoError(t, err)

	m := poolMatch(t, store, compID, "Pool A-0")
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Empty(t, m.IpponsA, "the operator's cleared board is their word")
	assert.Equal(t, 0, m.HansokuB)
}

func TestStartOnly_DoesNotWipeAMatchAlreadyRunning(t *testing.T) {
	// A court list a moment behind shows the match as scheduled; another
	// device has already started it and struck a point.
	const compID = "start-keeps-running"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Ind", Format: state.CompFormatMixed, Courts: []string{"A"}})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Court: "A", Status: state.MatchStatusRunning,
		IpponsB: []string{"K"},
	}}))

	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", startWrite(), ForceOptions{StartOnly: true})
	require.NoError(t, err)

	assert.Equal(t, []string{"K"}, poolMatch(t, store, compID, "Pool A-0").IpponsB)
}

func TestStartOnly_PoolTeamKeepsItsBouts(t *testing.T) {
	const compID = "start-keeps-team"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Teams", Format: state.CompFormatMixed, Courts: []string{"A"}, TeamSize: 3})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Red", SideB: "White", Court: "A", Status: state.MatchStatusScheduled,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R1", SideB: "W1", IpponsA: []string{"M", "K"}, Winner: "R1", Decision: "fought"},
			{Position: 2, SideA: "R2", SideB: "W2", IpponsB: []string{"D"}},
		},
	}}))

	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", startWrite(), ForceOptions{StartOnly: true})
	require.NoError(t, err)

	m := poolMatch(t, store, compID, "Pool A-0")
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	require.Len(t, m.SubResults, 2, "the fought bouts are still there")
	assert.Equal(t, []string{"M", "K"}, m.SubResults[0].IpponsA)
	assert.Equal(t, []string{"D"}, m.SubResults[1].IpponsB)
}

func TestStartOnly_EngiKeepsItsFlags(t *testing.T) {
	const compID = "start-keeps-engi"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Engi", Format: state.CompFormatMixed, Courts: []string{"A"}, Engi: true})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Pair One", SideB: "Pair Two", Court: "A", Status: state.MatchStatusScheduled,
		FlagsA: 2, FlagsB: 1,
	}}))

	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", startWrite(), ForceOptions{StartOnly: true})
	require.NoError(t, err)

	m := poolMatch(t, store, compID, "Pool A-0")
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, 2, m.FlagsA)
	assert.Equal(t, 1, m.FlagsB)
}

func TestStartOnly_BracketKeepsTheQueuedScore(t *testing.T) {
	const compID = "start-keeps-ko"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "KO", Format: state.CompFormatKnockout, Courts: []string{"A"}, TeamSize: 3})
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", SideA: "Carol", SideB: "Dave", Court: "A", Status: state.MatchStatusScheduled,
			IpponsA: []string{"M"}, HansokuB: 1,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "C1", SideB: "D1", IpponsA: []string{"M"}}}},
	}}}))

	_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", startWrite(), ForceOptions{StartOnly: true})
	require.NoError(t, err)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := b.Rounds[0][0]
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, []string{"M"}, m.IpponsA, "the kept point is still there")
	assert.Equal(t, 1, m.HansokuB, "the kept penalty is still there")
	require.Len(t, m.SubResults, 1, "the kept bout is still there")
}

func TestSendBackThenStart_RoundTripKeepsTheScore(t *testing.T) {
	// The whole operator path: a running match is sent back to the queue and
	// started again from the court console.
	const compID = "requeue-roundtrip"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Ind", Format: state.CompFormatMixed, Courts: []string{"A"}})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Court: "A", Status: state.MatchStatusRunning,
		IpponsA: []string{"M"}, HansokuB: 1,
	}}))

	require.NoError(t, eng.RevertMatchToQueue(compID, "Pool A-0"))
	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", startWrite(), ForceOptions{StartOnly: true})
	require.NoError(t, err)

	m := poolMatch(t, store, compID, "Pool A-0")
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, []string{"M"}, m.IpponsA)
	assert.Equal(t, 1, m.HansokuB)
}
