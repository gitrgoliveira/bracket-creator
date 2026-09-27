package mobileapp

import (
	"net/http"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-sbq: the SPA's startPatch sends startOnly and no scoreline, and
// the /score handler must hand that flag to the engine so the stored score
// of a queued match survives its start (engine.keepQueuedScore). The same
// payload without the flag is an operator clearing every mark.
func TestScoreHandler_StartOnlyKeepsTheQueuedScore(t *testing.T) {
	const compID = "start-only-wire"
	r, store := setupKachinukiScoreServer(t, compID)
	// An individual competition: a kachinuki bout log is kept by its own
	// merge, so it could not tell whether the flag arrived.
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, IpponsA: []string{"M"}, HansokuB: 1},
		{ID: "P1-1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, IpponsA: []string{"K"}, HansokuB: 1},
	}))
	// The two payloads the SPA sends: startPatch's (startOnly, no scoreline,
	// toBackendMatchResult) and an editor board cleared to nothing.
	start := func(id, a, b string, flag bool) {
		payload := map[string]any{"sideA": a, "sideB": b, "status": "running", "startOnly": true}
		if !flag {
			payload = map[string]any{"sideA": a, "sideB": b, "status": "running", "ipponsA": []string{}, "ipponsB": []string{}, "hansokuA": 0, "hansokuB": 0}
		}
		w := putScore(t, r, compID, id, payload)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	start("P1-0", "Alice", "Bob", true)
	kept := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusRunning, kept.Status)
	assert.Equal(t, []string{"M"}, kept.IpponsA, "a start keeps the queued point")
	assert.Equal(t, 1, kept.HansokuB, "a start keeps the queued penalty")

	start("P1-1", "Carol", "Dave", false)
	cleared := loadPoolMatch(t, store, compID, "P1-1")
	assert.Equal(t, state.MatchStatusRunning, cleared.Status)
	assert.Empty(t, cleared.IpponsA, "without the flag the empty board is the operator's word")
	assert.Equal(t, 0, cleared.HansokuB)
}

// startOnly swaps the stored score in after validation, so a write that also
// completes the match would store a winner on a scoreline nothing checked.
func TestScoreHandler_StartOnlyIsOnlyForAStart(t *testing.T) {
	const compID = "start-only-completes"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, IpponsB: []string{"M"}},
	}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "status": "completed", "winner": "Alice",
		"ipponsA": []string{"M", "K"}, "ipponsB": []string{}, "startOnly": true,
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusScheduled, m.Status, "nothing was stored")
	assert.Empty(t, m.Winner)
}
