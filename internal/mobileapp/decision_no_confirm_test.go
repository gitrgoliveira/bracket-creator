package mobileapp

import (
	"net/http"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Recording the withdrawal on the other side applies with no confirm and
// changes no other match, even when the first withdrawer has a later match
// under way (operator ruling 2026-09-27, "do not cascade"). A body that still
// carries the retired `force` key binds and applies the same way.
func TestDecisionHandler_ReRecordingTheWithdrawalAppliesWithNoConfirm(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	compID := "dlck-no-confirm"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "dlck", Status: state.CompStatusPools,
	}))
	aliceID, bobID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: helper.NewUUID4(), Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled},
	}))
	url := "/api/competitions/" + compID + "/matches/Pool A-0/decision"

	w := serveJSON(r, "POST", url, map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// The first withdrawer's later match is put under way.
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range ms {
		if ms[i].ID == "Pool A-1" {
			ms[i].Status = state.MatchStatusRunning
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, ms))

	// The other side withdrew; the retired force key still binds.
	w = serveJSON(r, "POST", url, map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro", "force": true})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), "locked")

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.True(t, statuses[aliceID].Eligible, "the side first marked is eligible again")
	assert.False(t, statuses[bobID].Eligible, "the other side is the one that withdrew")

	ms, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == "Pool A-1" {
			assert.Equal(t, state.MatchStatusRunning, m.Status, "no other match is changed")
			assert.Empty(t, m.Decision)
		}
	}

	// And with no force key at all.
	w = serveJSON(r, "POST", url, map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
