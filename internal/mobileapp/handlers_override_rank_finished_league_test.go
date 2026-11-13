package mobileapp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Lots typed wrongly on a team league that has just finished are corrected
// through the same door (decision F2 (a)): PUT .../override-rank answers 200
// on the finished league, the table follows the new order, and the league
// stays finished. GET .../chusen-candidates still lists the recorded lots.
func TestOverrideRank_FinishedTeamLeagueTakesACorrectedLotsOrder(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	const compID = "chusen-finished-league"
	seedChusenCycle(t, store, eng, compID)

	put := func(order ...string) {
		t.Helper()
		ranks := make([]map[string]any, len(order))
		for i, id := range order {
			ranks[i] = map[string]any{"playerId": id, "rank": i + 1}
		}
		w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool%20A/override-rank",
			map[string]any{"ranks": ranks})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	status := func() state.CompetitionStatus {
		t.Helper()
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		return comp.Status
	}
	table := func() []string {
		t.Helper()
		standings, err := eng.LeagueStandings(compID)
		require.NoError(t, err)
		ids := make([]string, len(standings))
		for i, s := range standings {
			ids[i] = s.Player.ID
		}
		return ids
	}

	// The lots, typed in the wrong order, finish the league.
	put("gamma-id", "alpha-id", "beta-id")
	require.Equal(t, state.CompStatusComplete, status())
	require.Equal(t, []string{"gamma-id", "alpha-id", "beta-id"}, table())

	w := sendJSON(t, r, http.MethodGet, "/api/competitions/"+compID+"/chusen-candidates", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	recorded, ok := body["recorded"].([]any)
	require.True(t, ok)
	require.Len(t, recorded, 1, "the finished league still lists its recorded lots")

	// The corrected order is taken; the league stays finished.
	put("alpha-id", "beta-id", "gamma-id")
	assert.Equal(t, []string{"alpha-id", "beta-id", "gamma-id"}, table(), "the table and the podium follow at once")
	assert.Equal(t, state.CompStatusComplete, status(), "the league stays finished")
}

// An individual league that has finished still takes no lots: the finished
// league passes the status rule and the team-only gate refuses it.
func TestOverrideRank_FinishedIndividualLeagueIsRefused(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	const compID = "rank-individual-finished-league"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Status: state.CompStatusComplete, Format: state.CompFormatLeague, Kind: "individual",
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "pool-1", Players: []helper.Player{{ID: "p1", Name: "P1", Dojo: "Dojo P1"}, {ID: "p2", Name: "P2", Dojo: "Dojo P2"}}},
	}))
	w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/pool-1/override-rank",
		map[string]any{"playerId": "p1", "rank": 1})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "rank_override_team_only", body["code"])
}
