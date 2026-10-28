package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverriddenBracketMatch_LoadsAndGuardsDownstreamCorrection(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "overridden-bracket-load"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Overridden Load Test",
		Status: state.CompStatusKnockout,
	}))

	// Fixture containing a feeder match (r0) with a normal scoreline, and a downstream
	// match (r1) that carries isOverridden: true, a winner, but NO scoreline at all
	// (representing a match overridden in an earlier release).
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:           "m-r1-0",
					SideA:        "Alice",
					SideB:        "Bob",
					SideAID:      "alice",
					SideBID:      "bob",
					Winner:       "Alice",
					WinnerID:     "alice",
					Status:       state.MatchStatusCompleted,
					IpponsA:      []string{"M"},
					MatchNumber:  1,
					DisplayRound: 2,
				},
			},
			{
				{
					ID:           "m-r2-0",
					SideA:        "Alice",
					SideB:        "Charlie",
					SideAID:      "alice",
					SideBID:      "charlie",
					Winner:       "Alice",
					WinnerID:     "alice",
					Status:       state.MatchStatusCompleted,
					IsOverridden: true,
					MatchNumber:  2,
					DisplayRound: 1,
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	// 1. Assert the bracket loads back through the store and IsOverridden reads true.
	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	mFinal := loaded.MatchByID("m-r2-0")
	require.NotNil(t, mFinal)
	assert.True(t, mFinal.IsOverridden, "IsOverridden flag must read back true from storage")
	assert.Equal(t, "Alice", mFinal.Winner)
	assert.Empty(t, mFinal.IpponsA, "fixture carries no scoreline")
	assert.Empty(t, mFinal.IpponsB, "fixture carries no scoreline")

	// 2. Assert that the downstream guard treats this match as a recorded result:
	// A correction to m-r1-0 that flips the winner to Bob would repaint m-r2-0's SideA;
	// without force, it must be refused with DownstreamKnockoutPlayedError.
	correction := &state.MatchResult{
		ID:               "m-r1-0",
		SideA:            "Alice",
		SideB:            "Bob",
		SideAID:          "alice",
		SideBID:          "bob",
		Winner:           "Bob",
		WinnerID:         "bob",
		IpponsB:          []string{"K"},
		Status:           state.MatchStatusCompleted,
		CorrectionReason: "scorer error",
	}

	_, err = eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", correction)
	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &dkErr, "downstream guard must refuse unforced correction that would repaint an overridden match")
	assert.Equal(t, "m-r2-0", dkErr.BlockingMatchID)

	// Bracket must be untouched on refusal.
	reloaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", reloaded.Rounds[0][0].Winner)
	assert.Equal(t, "Alice", reloaded.Rounds[1][0].SideA)
	assert.True(t, reloaded.Rounds[1][0].IsOverridden)

	// 3. Forced correction reopens the downstream match and clears IsOverridden.
	var reopened []ReopenedMatch
	_, err = eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", correction, ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	require.Len(t, reopened, 1)
	assert.Equal(t, "m-r2-0", reopened[0].ID)

	afterForce, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", afterForce.Rounds[0][0].Winner)
	assert.Equal(t, "Bob", afterForce.Rounds[1][0].SideA)
	assert.Equal(t, state.MatchStatusScheduled, afterForce.Rounds[1][0].Status)
	assert.False(t, afterForce.Rounds[1][0].IsOverridden, "reopening clears IsOverridden")
}
