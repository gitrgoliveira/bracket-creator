package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// seedIndividualCircle saves an individual league of three, level at 1st on
// every criterion, whose ippon-shobu round went in a circle too (aki beat bea,
// bea beat cal, cal beat aki, in the regular matches and in the round).
func seedIndividualCircle(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Individual circle", Status: state.CompStatusPools, Format: state.CompFormatLeague, Courts: []string{"A"},
	}))
	names := map[string]string{"aki": "Aki", "bea": "Bea", "cal": "Cal"}
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{
		{ID: "aki", Name: "Aki", Dojo: "Dojo Aki"}, {ID: "bea", Name: "Bea", Dojo: "Dojo Bea"}, {ID: "cal", Name: "Cal", Dojo: "Dojo Cal"},
	}}}))
	circle := [][2]string{{"aki", "bea"}, {"bea", "cal"}, {"cal", "aki"}}
	var matches []state.MatchResult
	for _, kind := range []string{"", "TB-"} {
		for i, p := range circle {
			matches = append(matches, state.MatchResult{
				ID: fmt.Sprintf("Pool A-%s%d", kind, i), Status: state.MatchStatusCompleted, Court: "A",
				SideA: names[p[0]], SideB: names[p[1]], SideAID: p[0], SideBID: p[1], Winner: names[p[0]], WinnerID: p[0],
			})
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
}

// An individual tie a finished ippon-shobu round left level (decision F5):
// GET .../chusen-candidates names it, with the run another round would fight
// (stillLevel), and PUT .../override-rank takes the lots the operator chose.
func TestOverrideRank_IndividualCircleTakesTheLots(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	const compID = "individual-circle-lots"
	seedIndividualCircle(t, store, compID)

	w := sendJSON(t, r, http.MethodGet, "/api/competitions/"+compID+"/chusen-candidates", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Candidates []struct {
			PoolName   string `json:"poolName"`
			Teams      []competitorRef
			StillLevel [][]competitorRef `json:"stillLevel"`
		} `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Candidates, 1, "the circle is offered")
	require.Len(t, body.Candidates[0].StillLevel, 1)
	assert.Len(t, body.Candidates[0].StillLevel[0], 3, "the whole circle fights again, if the operator chooses")

	ranks := []map[string]any{{"playerId": "cal", "rank": 1}, {"playerId": "aki", "rank": 2}, {"playerId": "bea", "rank": 3}}
	w = sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool%20A/override-rank", map[string]any{"ranks": ranks})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status, "the lots settle the circle and the league finishes")
}

// A tie no tie-breaker round has left level still takes no lots: it is decided
// by an ippon-shobu.
func TestOverrideRank_IndividualTieBeforeItsRoundIsRefused(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	const compID = "individual-tie-unfought"
	seedIndividualCircle(t, store, compID)
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NoError(t, store.SavePoolMatches(compID, all[:3]), "only the regular matches: the round is still to come")

	w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool%20A/override-rank",
		map[string]any{"ranks": []map[string]any{{"playerId": "aki", "rank": 1}, {"playerId": "bea", "rank": 2}, {"playerId": "cal", "rank": 3}}})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var refusal map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
	assert.Equal(t, "rank_override_team_only", refusal["code"])
}

// "Run another tie-breaker round" (decision F5): POST .../tiebreak-round adds a
// fresh bout for every pair of the run the chusen candidates name, answers
// 201 with them, and a second tap (the round is no longer finished, so the
// group is no longer left level) is a 400 with the sentence to show, adding
// nothing.
func TestTiebreakRound_AddsAFreshRoundAmongTheCircle(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	const compID = "individual-circle-round"
	seedIndividualCircle(t, store, compID)
	path := "/api/competitions/" + compID + "/pools/Pool%20A/tiebreak-round"

	w := sendJSON(t, r, http.MethodPost, path, map[string]any{"playerIds": []string{"cal", "aki", "bea"}})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var body struct {
		Matches []state.MatchResult `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Matches, 3, "one bout per pair of the circle")
	for _, m := range body.Matches {
		assert.Equal(t, state.MatchStatusScheduled, m.Status, m.ID)
		assert.NotEmpty(t, m.SideAID, m.ID)
		assert.NotEmpty(t, m.SideBID, m.ID)
	}
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Len(t, all, 9, "the new round is stored beside the regular matches and the first round")

	w = sendJSON(t, r, http.MethodPost, path, map[string]any{"playerIds": []string{"cal", "aki", "bea"}})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	all, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Len(t, all, 9, "a second tap adds nothing")

	w = sendJSON(t, r, http.MethodPost, path, map[string]any{"playerIds": []string{"aki"}})
	assert.Equal(t, http.StatusBadRequest, w.Code, "a run is at least two competitors")
	w = sendJSON(t, r, http.MethodPost, "/api/competitions/no-such-comp/pools/Pool%20A/tiebreak-round", map[string]any{"playerIds": []string{"aki", "bea"}})
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}
