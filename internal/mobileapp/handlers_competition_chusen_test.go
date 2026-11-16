package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChusenCandidates_EmptyOnNoTie verifies that a competition in the pools
// stage with no unresolved chusen groups returns 200 and an empty candidates
// array.
func TestChusenCandidates_EmptyOnNoTie(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	comp := state.Competition{
		ID:       "chusen-comp",
		Name:     "Chusen Test",
		Status:   state.CompStatusPools,
		Kind:     "team",
		TeamSize: 5,
		Format:   state.CompFormatMixed,
	}
	require.NoError(t, store.SaveCompetition(&comp))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/competitions/chusen-comp/chusen-candidates", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	cands, ok := body["candidates"].([]any)
	require.True(t, ok, "candidates field must be a JSON array")
	assert.Empty(t, cands)
}

// TestChusenCandidates_NotFound verifies that a missing competition returns 404.
func TestChusenCandidates_NotFound(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/competitions/no-such-comp/chusen-candidates", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestChusenCandidates_IncludesTeamIdentity verifies that a non-empty chusen
// candidate carries a "teams" array (id/name/dojo per member), not just the
// legacy "teamNames" strings (bc-cse). The SPA's chusen resolver needs the id
// (or dojo) to call PUT .../override-rank unambiguously: two teams CAN
// legally share a display name from different dojos (operator identity
// rule), and teamNames alone cannot tell them apart.
// seedChusenCycle stores a three-team league pool (Alpha, Beta, Gamma, ids
// "<name>-id") whose every encounter was drawn, injects the daihyosen round and
// scores it into the cycle Alpha > Beta > Gamma > Alpha: a tie only a chusen
// settles, mirroring engine's TestChusenCandidates_CycleNeedsChusen.
func seedChusenCycle(t *testing.T, store *state.Store, eng *engine.Engine, compID string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Chusen Identity", Status: state.CompStatusPools,
		Kind: "team", TeamSize: 2, Format: state.CompFormatLeague, Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "alpha-id", Name: "Alpha", Dojo: "Dojo A"},
			{ID: "beta-id", Name: "Beta", Dojo: "Dojo B"},
			{ID: "gamma-id", Name: "Gamma", Dojo: "Dojo C"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alpha", SideB: "Beta", SideAID: "alpha-id", SideBID: "beta-id",
			Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake), Court: "A"},
		{ID: "Pool A-1", SideA: "Alpha", SideB: "Gamma", SideAID: "alpha-id", SideBID: "gamma-id",
			Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake), Court: "A"},
		{ID: "Pool A-2", SideA: "Beta", SideB: "Gamma", SideAID: "beta-id", SideBID: "gamma-id",
			Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake), Court: "A"},
	}))

	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	cycleBeats := map[string]string{"Alpha": "Beta", "Beta": "Gamma", "Gamma": "Alpha"}
	for i := range all {
		if !engine.IsPoolDaihyosenMatchID(all[i].ID) {
			continue
		}
		all[i].Status = state.MatchStatusCompleted
		if cycleBeats[all[i].SideA] == all[i].SideB {
			all[i].Winner = all[i].SideA
		} else {
			all[i].Winner = all[i].SideB
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))
}

// TestChusenCandidates_ReportsTheRecordedChusen: once the operator records a
// chusen through PUT .../override-rank, GET .../chusen-candidates stops
// offering the tie and lists it under "recorded" instead, in the recorded
// order and with the recorded ranks, so a chusen recorded in the wrong order
// can be changed from the UI.
func TestChusenCandidates_ReportsTheRecordedChusen(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-recorded"
	seedChusenCycle(t, store, eng, compID)

	get := func() (cands, recorded []any) {
		t.Helper()
		w := sendJSON(t, r, http.MethodGet, "/api/competitions/"+compID+"/chusen-candidates", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		cands, ok := body["candidates"].([]any)
		require.True(t, ok, "candidates must be a JSON array")
		recorded, ok = body["recorded"].([]any)
		require.True(t, ok, "recorded must be a JSON array, never null")
		return cands, recorded
	}
	cands, recorded := get()
	require.Len(t, cands, 1)
	assert.Empty(t, recorded, "a tie waiting for its chusen is not a recorded one")
	_, hasRanks := cands[0].(map[string]any)["ranks"]
	assert.False(t, hasRanks, "a pending group carries no recorded ranks")

	for rank, id := range []string{"gamma-id", "alpha-id", "beta-id"} {
		w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool%20A/override-rank",
			map[string]any{"playerId": id, "rank": rank + 1})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	// The lots settle the league's last open item, so it finished by itself; a
	// reopen puts it back in match play, where the chusen can be changed.
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusComplete, comp.Status)
	comp.Status = state.CompStatusPools
	require.NoError(t, store.SaveCompetition(comp))

	cands, recorded = get()
	assert.Empty(t, cands, "the recorded chusen clears the candidate")
	require.Len(t, recorded, 1, "the tie the chusen settled is listed as recorded")
	group := recorded[0].(map[string]any)
	assert.Equal(t, "Pool A", group["poolName"])
	assert.EqualValues(t, 1, group["minPosition"])
	teams := group["teams"].([]any)
	require.Len(t, teams, 3)
	ids := make([]string, len(teams))
	for i, raw := range teams {
		ids[i] = raw.(map[string]any)["id"].(string)
	}
	assert.Equal(t, []string{"gamma-id", "alpha-id", "beta-id"}, ids, "teams arrive in the recorded order")
	assert.Equal(t, []any{float64(1), float64(2), float64(3)}, group["ranks"])
}

// TestOverrideRank_ChusenCompletesALeagueWhoseDaihyosenWasDrawn: a team league
// whose last open item is a drawn daihyosen round finishes by itself once the
// operator records the chusen (the lots settle the drawn bouts), without a
// manual Complete. Until every team is ranked the league stays in pools.
func TestOverrideRank_ChusenCompletesALeagueWhoseDaihyosenWasDrawn(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-league-drawn-dh"
	seedChusenCycle(t, store, eng, compID)

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range all {
		if engine.IsPoolDaihyosenMatchID(all[i].ID) {
			all[i].Winner, all[i].WinnerID = "", "" // every daihyosen bout drawn
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))

	status := func() state.CompetitionStatus {
		t.Helper()
		comp, lerr := store.LoadCompetition(compID)
		require.NoError(t, lerr)
		return comp.Status
	}
	for i, id := range []string{"gamma-id", "alpha-id", "beta-id"} {
		require.Equal(t, state.CompStatusPools, status(), "the league holds open until every team is ranked")
		w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool%20A/override-rank",
			map[string]any{"playerId": id, "rank": i + 1})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		if i == 1 {
			// Two of the three teams the daihyosen round links are ranked: the
			// lots do not yet cover the group, so a manual Complete is refused too.
			w = sendJSON(t, r, http.MethodPost, "/api/competitions/"+compID+"/complete", nil)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		}
	}
	assert.Equal(t, state.CompStatusComplete, status(), "the recorded chusen settles the drawn daihyosen round and finishes the league")
}

// TestDeleteOverrides_ReopensALeagueTheLotsHadSettled: clearing the recorded
// lots of a finished league whose tie only they settled puts the league back
// in play, as the PUT door's pass would, instead of leaving it completed with
// an undecided tie.
func TestDeleteOverrides_ReopensALeagueTheLotsHadSettled(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-league-delete-lots"
	seedChusenCycle(t, store, eng, compID)

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range all {
		if engine.IsPoolDaihyosenMatchID(all[i].ID) {
			all[i].Winner, all[i].WinnerID = "", ""
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))

	for i, id := range []string{"gamma-id", "alpha-id", "beta-id"} {
		w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool%20A/override-rank",
			map[string]any{"playerId": id, "rank": i + 1})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusComplete, comp.Status, "the lots finish the league")

	// Reopening the league is warned about first, like every result door: nothing
	// is cleared until the request confirms.
	w := sendJSON(t, r, http.MethodDelete, "/api/competitions/"+compID+"/overrides", nil)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var refusal map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
	assert.Equal(t, "league_finished", refusal["error"])
	assert.Equal(t, LeagueFinishedLotsMessage, refusal["message"])
	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status, "the warning leaves the league finished")
	ranks, err := store.LoadPoolRanks(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, ranks, "and the lots stand")

	w = sendJSON(t, r, http.MethodDelete, "/api/competitions/"+compID+"/overrides", map[string]any{"confirmFinished": true})
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "without the lots the tie is undecided again, so the league is back in play")
}

// TestDeleteOverrides_CorruptOverridesOnAFinishedLeagueStillRepairs: the reset is
// the repair door for a corrupt overrides.json, so the finished-league warning's
// look at the lots (which reads that same file) must not turn it into a 500: the
// reset runs and leaves a file that parses.
func TestDeleteOverrides_CorruptOverridesOnAFinishedLeagueStillRepairs(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-league-delete-corrupt"
	seedChusenCycle(t, store, eng, compID)

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range all {
		if engine.IsPoolDaihyosenMatchID(all[i].ID) {
			all[i].Winner, all[i].WinnerID = "", ""
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))
	for i, id := range []string{"gamma-id", "alpha-id", "beta-id"} {
		w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool%20A/override-rank",
			map[string]any{"playerId": id, "rank": i + 1})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusComplete, comp.Status, "the lots finish the league")

	overridesPath := filepath.Join(store.GetFolder(), "competitions", compID, "overrides.json")
	require.NoError(t, os.WriteFile(overridesPath, []byte("{not valid json"), 0o600))

	w := sendJSON(t, r, http.MethodDelete, "/api/competitions/"+compID+"/overrides", nil)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	repaired, err := store.LoadOverrides(compID)
	require.NoError(t, err, "overrides.json must parse cleanly after the repair")
	assert.Empty(t, repaired.PoolRanks)
}

// TestDeleteOverrides_NoWarningForAFinishedLeagueHoldingNoLots: a finished league
// that is unsettled without lots and holds none (one finished before the tie
// rule, left complete by design) has nothing for the reset to reopen, so the
// reset is a plain no-op and not the warning that the league finished on lots.
func TestDeleteOverrides_NoWarningForAFinishedLeagueHoldingNoLots(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-league-delete-no-lots"
	seedChusenCycle(t, store, eng, compID)

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range all {
		if engine.IsPoolDaihyosenMatchID(all[i].ID) {
			all[i].Winner, all[i].WinnerID = "", ""
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))
	for i, id := range []string{"gamma-id", "alpha-id", "beta-id"} {
		w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool%20A/override-rank",
			map[string]any{"playerId": id, "rank": i + 1})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	// The league stays complete with its lots gone, as a league finished by an
	// earlier release does.
	require.NoError(t, store.SaveOverrides(compID, &state.Overrides{}))
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusComplete, comp.Status)

	w := sendJSON(t, r, http.MethodDelete, "/api/competitions/"+compID+"/overrides", nil)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status, "the reset changed nothing")
}

// Clearing lots from a league that has not finished needs no confirmation:
// there is nothing to put back into match play.
func TestDeleteOverrides_NothingToWarnAboutBeforeTheLeagueFinishes(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-league-delete-lots-moot"
	seedChusenCycle(t, store, eng, compID)

	w := sendJSON(t, r, http.MethodDelete, "/api/competitions/"+compID+"/overrides", nil)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String(), "no lots recorded: nothing to warn about")
}

// TestDeleteOverrides_ReopensALeagueWhoseDecisiveCircleOnlyTheLotsOrdered: the
// ordinary case lots are offered for, a true A>B>C>A daihyosen circle with every
// bout fought and won. Every pool row is played, so only the league's verdict
// (PoolsChusenOwed) tells that clearing the lots leaves it undecided.
func TestDeleteOverrides_ReopensALeagueWhoseDecisiveCircleOnlyTheLotsOrdered(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-league-delete-lots-circle"
	seedChusenCycle(t, store, eng, compID)

	for i, id := range []string{"gamma-id", "alpha-id", "beta-id"} {
		w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool%20A/override-rank",
			map[string]any{"playerId": id, "rank": i + 1})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusComplete, comp.Status, "the lots finish the league")

	w := sendJSON(t, r, http.MethodDelete, "/api/competitions/"+compID+"/overrides", map[string]any{"confirmFinished": true})
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "the circle is level again without its lots, so the league is back in play")
}

func TestChusenCandidates_IncludesTeamIdentity(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-identity"
	seedChusenCycle(t, store, eng, compID)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/competitions/"+compID+"/chusen-candidates", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	cands, ok := body["candidates"].([]any)
	require.True(t, ok)
	require.Len(t, cands, 1, "the 3-way cycle must surface as one chusen candidate")

	group := cands[0].(map[string]any)
	teams, ok := group["teams"].([]any)
	require.True(t, ok, "candidate must carry a teams array")
	require.Len(t, teams, 3)

	byName := make(map[string]map[string]any, len(teams))
	for _, raw := range teams {
		team := raw.(map[string]any)
		byName[team["name"].(string)] = team
	}
	assert.Equal(t, "alpha-id", byName["Alpha"]["id"])
	assert.Equal(t, "Dojo A", byName["Alpha"]["dojo"])
	assert.Equal(t, "beta-id", byName["Beta"]["id"])
	assert.Equal(t, "Dojo B", byName["Beta"]["dojo"])
}

// TestDeleteOverridesHandler_CorruptOverrides_ForceRepairs is bc-pnum gap 2:
// DELETE /api/competitions/:id/overrides is documented (specs/openapi.yaml)
// as the repair door for a corrupt overrides.json, but before this fix it
// called Store.ResetOverridesChanged, which -- like every other override
// writer -- loads and parses the existing file before saving, so it failed
// identically against the very file it was supposed to repair (500, and the
// operator had no way to clear it short of deleting the file by hand). The
// fix falls back to Store.ResetOverridesForce (the load-free repair
// primitive, internal/state/overrides.go) whenever ResetOverridesChanged
// reports state.ErrCorruptOverrides, so the endpoint now always succeeds
// (204) and leaves overrides.json parsing as a fresh empty value.
func TestDeleteOverridesHandler_CorruptOverrides_ForceRepairs(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "corrupt-ov-reset"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	overridesPath := filepath.Join(store.GetFolder(), "competitions", compID, "overrides.json")
	require.NoError(t, os.WriteFile(overridesPath, []byte("{not valid json"), 0o600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/api/competitions/"+compID+"/overrides", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, "body: %s", w.Body.String())

	// The file must now parse as a fresh, empty Overrides value -- the repair
	// actually landed, not just "the handler didn't crash".
	repaired, err := store.LoadOverrides(compID)
	require.NoError(t, err, "overrides.json must parse cleanly after the repair")
	assert.Empty(t, repaired.PoolRanks)
}

// TestChusenCandidatesHandler_CorruptOverrides_TerminalError is bc-pnum gap 3:
// GET /api/competitions/:id/chusen-candidates calls engine.ChusenStatus,
// which loads overrides.json directly (and also indirectly via
// CalculatePoolStandings). Before this fix an unmapped state.ErrCorruptOverrides
// fell through to the handler's own opaque-500 branch; the fix maps it to the
// same terminal 422 the score/decision endpoints answer with via the shared
// respondIfCorruptOverrides helper.
func TestChusenCandidatesHandler_CorruptOverrides_TerminalError(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "corrupt-ov-chusen"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, TeamSize: 3, Status: state.CompStatusPools,
	}))

	overridesPath := filepath.Join(store.GetFolder(), "competitions", compID, "overrides.json")
	require.NoError(t, os.WriteFile(overridesPath, []byte("{not valid json"), 0o600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/competitions/"+compID+"/chusen-candidates", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "corrupt_overrides")
}

// TestCompleteHandler_CorruptOverrides_TerminalError: POST /complete on a
// league judges its pool matches by the recorded chusen ranks once a completed
// representative bout exists, so a corrupt overrides.json is the same terminal
// 422 the other endpoints answer with, not an opaque 500.
func TestCompleteHandler_CorruptOverrides_TerminalError(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "corrupt-ov-complete"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatLeague, TeamSize: 3, Kind: "team", Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "a", Name: "TeamA", Dojo: "DojoA"}, {ID: "b", Name: "TeamB", Dojo: "DojoB"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "TeamA", SideAID: "a", SideB: "TeamB", SideBID: "b",
			Winner: "TeamA", WinnerID: "a", Status: state.MatchStatusCompleted},
		{ID: "Pool A-DH-0", SideA: "TeamA", SideAID: "a", SideB: "TeamB", SideBID: "b",
			Winner: "TeamA", WinnerID: "a", Status: state.MatchStatusCompleted},
	}))

	overridesPath := filepath.Join(store.GetFolder(), "competitions", compID, "overrides.json")
	require.NoError(t, os.WriteFile(overridesPath, []byte("{not valid json"), 0o600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/complete", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "corrupt_overrides")
}
