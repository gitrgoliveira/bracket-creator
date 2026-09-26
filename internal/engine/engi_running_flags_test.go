package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Engi flags are saved as they are entered (operator ruling 2026-09-26): a
// running write carries the flags so far, and a panel part-way through its
// count has no valid total yet, so only a completing write is judged on the
// flag total and decides a winner.

func TestEngiRunningWrite_SavesThePoolFlagsSoFar(t *testing.T) {
	const compID = "engi-running-pool"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Engi", Format: state.CompFormatMixed, Courts: []string{"A"}, Engi: true})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Pair One", SideB: "Pair Two", Court: "A", Status: state.MatchStatusRunning,
	}}))

	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", &state.MatchResult{Status: state.MatchStatusRunning, FlagsA: 1, FlagsB: 1})
	require.NoError(t, err, "an even count so far is not a finished panel")

	m := poolMatch(t, store, compID, "Pool A-0")
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, 1, m.FlagsA)
	assert.Equal(t, 1, m.FlagsB)
	assert.Empty(t, m.Winner, "nobody has won yet")
}

func TestEngiRunningWrite_SavesTheBracketFlagsSoFar(t *testing.T) {
	const compID = "engi-running-ko"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Engi KO", Format: state.CompFormatKnockout, Courts: []string{"A"}, Engi: true})
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", SideA: "Pair One", SideB: "Pair Two", Court: "A", Status: state.MatchStatusRunning},
	}}}))

	_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", &state.MatchResult{Status: state.MatchStatusRunning, FlagsA: 2, FlagsB: 0})
	require.NoError(t, err)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := b.Rounds[0][0]
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, 2, m.FlagsA)
	assert.Equal(t, 0, m.FlagsB)
	assert.Empty(t, m.Winner)
}

func TestEngiCompletingWrite_IsStillJudgedOnTheTotal(t *testing.T) {
	const compID = "engi-complete-judged"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Engi", Format: state.CompFormatMixed, Courts: []string{"A"}, Engi: true})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Pair One", SideB: "Pair Two", Court: "A", Status: state.MatchStatusRunning,
	}}))

	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", &state.MatchResult{Status: state.MatchStatusCompleted, FlagsA: 1, FlagsB: 1})
	var verr *ValidationError
	require.ErrorAs(t, err, &verr, "a finished panel cannot draw")
}

func TestEngiFlags_SendBackAndStartKeepsThem(t *testing.T) {
	// The whole operator path on a knockout: flags entered, the match sent
	// back to the queue, started again from the court console.
	const compID = "engi-flags-roundtrip"
	eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Engi KO", Format: state.CompFormatKnockout, Courts: []string{"A"}, Engi: true})
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", SideA: "Pair One", SideB: "Pair Two", Court: "A", Status: state.MatchStatusRunning},
	}}}))
	_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", &state.MatchResult{Status: state.MatchStatusRunning, FlagsA: 2, FlagsB: 1})
	require.NoError(t, err)

	require.NoError(t, eng.RevertMatchToQueue(compID, "m-r1-0"))
	_, err = eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", startWrite(), ForceOptions{StartOnly: true})
	require.NoError(t, err)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := b.Rounds[0][0]
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, 2, m.FlagsA)
	assert.Equal(t, 1, m.FlagsB)
}
