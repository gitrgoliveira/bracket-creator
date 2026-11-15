package mobileapp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// An engi league's circle goes to another tie-breaker round through the same
// door as any other format's (POST .../tiebreak-round): the pairs are the
// competitors, the bouts are flag bouts, and a second tap adds nothing.
func TestTiebreakRound_EngiCircleGetsAnotherRound(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	const compID = "engi-circle-round-handler"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Engi circle", Kind: "individual", Engi: true, Status: state.CompStatusPools,
		Format: state.CompFormatLeague, Courts: []string{"A"},
	}))
	pairs := []helper.Player{
		{ID: "id-ann", Name: "Ann", Dojo: "Dojo Ann"}, {ID: "id-ben", Name: "Ben", Dojo: "Dojo Ben"}, {ID: "id-cas", Name: "Cas", Dojo: "Dojo Cas"},
	}
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: pairs}}))
	// Each pair beats the next 3-2 on flags: one win and five flags each.
	var regular []state.MatchResult
	for i, a := range pairs {
		b := pairs[(i+1)%len(pairs)]
		regular = append(regular, state.MatchResult{
			ID: "Pool A-" + string(rune('0'+i)), SideA: a.Name, SideAID: a.ID, SideB: b.Name, SideBID: b.ID,
			Court: "A", Status: state.MatchStatusCompleted, FlagsA: 3, FlagsB: 2, Winner: a.Name, WinnerID: a.ID,
		})
	}
	require.NoError(t, store.SavePoolMatches(compID, regular))
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, engine.AutoCompleteTiebreakInjected, outcome, "a circle at 1st owes a round")

	// The round goes in the same circle, so it leaves all three level again.
	beats := map[string]string{"id-ann": "id-ben", "id-ben": "id-cas", "id-cas": "id-ann"}
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var rounds int
	for _, m := range all {
		if !engine.IsTiebreakerMatchID(m.ID) {
			continue
		}
		rounds++
		result := &state.MatchResult{Status: state.MatchStatusCompleted, FlagsA: 1, FlagsB: 2}
		if beats[m.SideAID] == m.SideBID {
			result.FlagsA, result.FlagsB = 2, 1
		}
		_, werr := eng.RecordMatchResultWithIneligibility(compID, m.ID, result)
		require.NoError(t, werr)
	}
	require.Equal(t, 3, rounds, "a bout for every pair of the circle")

	path := "/api/competitions/" + compID + "/pools/Pool%20A/tiebreak-round"
	w := sendJSON(t, r, http.MethodPost, path, map[string]any{"playerIds": []string{"id-cas", "id-ann", "id-ben"}})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var body struct {
		Matches []state.MatchResult `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Matches, 3, "one fresh bout per pair of the circle")
	for _, m := range body.Matches {
		assert.Equal(t, state.MatchStatusScheduled, m.Status, m.ID)
		assert.NotEmpty(t, m.SideAID, m.ID)
		assert.NotEmpty(t, m.SideBID, m.ID)
	}

	w = sendJSON(t, r, http.MethodPost, path, map[string]any{"playerIds": []string{"id-cas", "id-ann", "id-ben"}})
	require.Equal(t, http.StatusBadRequest, w.Code, "a second tap adds nothing: "+w.Body.String())
	after, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Len(t, after, 9, "the regular matches, the first round and the new one")
}
