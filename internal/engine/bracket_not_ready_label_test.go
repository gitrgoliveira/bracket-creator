package engine

// bc-cse item 14: "knockout match %s is not ready to score/override..."
// used to interpolate the raw match id, which IS sent to the operator (both
// via respondEngineError's 400 body and RecordMatchResultWithIneligibility's
// caller in mobileapp). It must name the match the way the operator sees it
// instead, exactly like DownstreamKnockoutPlayedError already does.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScoring_BracketMatchNotReadyToScore_MessageNamesOperatorLabelNotRawID(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "not-ready-score-label"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			// SideB is an unresolved feeder placeholder, so the match is not
			// yet playable.
			{{ID: "m-r1-0", SideA: "Alice", SideB: "Winner of r0-m0",
				Status: state.MatchStatusScheduled, MatchNumber: 1, DisplayRound: 1}},
		},
	}))

	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", &state.MatchResult{
			ID: "m-r1-0", Status: state.MatchStatusRunning,
		}, ForceOptions{})
		return err
	})
	require.Error(t, txErr)
	assert.NotContains(t, txErr.Error(), "m-r1-0", "the raw match id must not reach the operator")
	assert.Contains(t, txErr.Error(), "Match 1 (Final)", "named by its operator label instead")
	assert.Contains(t, txErr.Error(), "is not ready to score")
}

func TestOverrideBracketWinner_NotReadyToOverride_MessageNamesOperatorLabelNotRawID(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "not-ready-override-label"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Alice", SideB: "Winner of r0-m0",
				Status: state.MatchStatusScheduled, MatchNumber: 3, DisplayRound: 1}},
		},
	}))

	_, err := eng.OverrideBracketWinner(compID, "m-r1-0", "Alice", 0)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "m-r1-0", "the raw match id must not reach the operator")
	assert.Contains(t, err.Error(), "Match 3 (Final)", "named by its operator label instead")
	assert.Contains(t, err.Error(), "is not ready to override")
}

func TestScoring_BracketCompletedWithNoWinner_MessageNamesOperatorLabelNotRawID(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "no-winner-label"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Alice", SideB: "Bob",
				Status: state.MatchStatusRunning, MatchNumber: 5, DisplayRound: 1}},
		},
	}))

	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", &state.MatchResult{
			ID: "m-r1-0", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusCompleted, // no Winner
		}, ForceOptions{})
		return err
	})
	require.Error(t, txErr)
	assert.NotContains(t, txErr.Error(), "m-r1-0", "the raw match id must not reach the operator")
	assert.Contains(t, txErr.Error(), "Match 5 (Final)", "named by its operator label instead")
	assert.Contains(t, txErr.Error(), "cannot mark completed with no winner")
}
