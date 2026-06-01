package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViewerHandlers_Standalone(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// 1. GET /api/viewer/tournament - No tournament case
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 2. GET /api/viewer/tournament - With tournament
	tourney := state.Tournament{Name: "Test Tourney", Password: "secret"}
	require.NoError(t, store.SaveTournament(&tourney))

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var respTourney state.Tournament
	json.Unmarshal(w.Body.Bytes(), &respTourney)
	assert.Equal(t, "Test Tourney", respTourney.Name)
	assert.Equal(t, "", respTourney.Password) // Password should be stripped

	// 3. GET /api/viewer/competitions - Empty case
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]", w.Body.String())

	// 4. GET /api/viewer/competitions - With competitions
	comp1 := state.Competition{ID: "c1", Name: "Comp 1"}
	require.NoError(t, store.SaveCompetition(&comp1))
	require.NoError(t, store.SaveParticipants("c1", nil))

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var comps []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &comps)
	assert.Len(t, comps, 1)
	config := comps[0]["config"].(map[string]interface{})
	assert.Equal(t, "c1", config["id"])

	// 5. GET /api/viewer/competitions/:id - Success
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/c1", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var detail map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &detail)
	assert.NotNil(t, detail["config"])
	assert.Contains(t, detail, "pools")
	assert.Contains(t, detail, "bracket")

	// 6. GET /api/viewer/competitions/:id - Not Found
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/nonexistent", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 7. GET /api/viewer/schedule
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/schedule", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestViewerAggregator_StripsPreviewBracket asserts that a Preview bracket
// (pool-origin placeholder leaves on a mixed source competition) is REMOVED
// from the aggregate /api/viewer/competitions payload so the SPA doesn't
// surface "Pool A-1st vs Pool B-2nd" as upcoming matches in Find-My-Matches /
// Watchlist / schedule / TV displays. The per-competition detail endpoint
// (/api/viewer/competitions/:id) must still return it for the Bracket-tab UI.
// Regression guard for mp-9dz.
func TestViewerAggregator_StripsPreviewBracket(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))
	comp := state.Competition{ID: "mixed", Name: "Mixed", Format: state.CompFormatMixed}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("mixed", nil))

	preview := &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Pool A-1st", SideB: "Pool B-2nd", Court: "A", Status: state.MatchStatusScheduled, ScheduledAt: "09:30"},
		}},
	}
	require.NoError(t, store.SaveBracket("mixed", preview))

	// Aggregate endpoint MUST strip the preview bracket.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var comps []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &comps))
	require.Len(t, comps, 1)
	assert.Nil(t, comps[0]["bracket"], "aggregate endpoint must strip Preview brackets (mp-9dz)")

	// Detail endpoint MUST still return it so the Bracket-tab UI renders.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/mixed", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var detail map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	bracketField, ok := detail["bracket"].(map[string]any)
	require.True(t, ok, "detail endpoint must return the preview bracket for the Bracket-tab UI")
	assert.Equal(t, true, bracketField["preview"], "preview flag must be present on the detail payload")
	rounds, _ := bracketField["rounds"].([]any)
	assert.NotEmpty(t, rounds, "preview bracket rounds must be present on the detail payload")
}
