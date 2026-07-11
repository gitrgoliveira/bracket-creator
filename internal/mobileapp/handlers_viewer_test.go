package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergePoolNumbersIntoPlayers, mp-13y: numbers from pools.csv must be
// merged onto comp.Players so the viewer API carries the numberPrefix-derived
// "K1", "K2", … on every player. The merge is the bridge that lets the TV
// display / streaming overlay / viewer card render the prefix at all
// (participants.csv does NOT persist Number).
func TestMergePoolNumbersIntoPlayers(t *testing.T) {
	t.Run("no-op when numberPrefix is empty", func(t *testing.T) {
		comp := &state.Competition{
			Players: []domain.Player{{ID: "p1", Name: "Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "K1"}}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "", comp.Players[0].Number, "no numberPrefix → never merge")
	})

	t.Run("no-op when pools is empty and format is not playoffs", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Format:       state.CompFormatMixed,
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka"}},
		}
		mergePoolNumbersIntoPlayers(comp, nil)
		assert.Equal(t, "", comp.Players[0].Number)
	})

	t.Run("assigns sequential numbers for playoffs-only with no pools", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "D",
			Format:       state.CompFormatPlayoffs,
			Players: []domain.Player{
				{ID: "p1", Name: "Rossi Marco"},
				{ID: "p2", Name: "Dubois Claire"},
				{ID: "p3", Name: "Santos Ana"},
			},
		}
		mergePoolNumbersIntoPlayers(comp, nil)
		assert.Equal(t, "D1", comp.Players[0].Number)
		assert.Equal(t, "D2", comp.Players[1].Number)
		assert.Equal(t, "D3", comp.Players[2].Number)
	})

	t.Run("playoffs-only: preserves existing non-empty Number", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "D",
			Format:       state.CompFormatPlayoffs,
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Number: "EXISTING"}},
		}
		mergePoolNumbersIntoPlayers(comp, nil)
		assert.Equal(t, "EXISTING", comp.Players[0].Number, "must not overwrite an existing Number")
	})

	t.Run("merges by id when HasParticipantIDs", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players: []domain.Player{
				{ID: "p1", Name: "Tanaka"},
				{ID: "p2", Name: "Suzuki"},
				{ID: "p3", Name: "Yamada"},
			},
		}
		pools := []helper.Pool{
			{PoolName: "Pool A", Players: []domain.Player{
				{ID: "p1", Name: "Tanaka", Number: "K1"},
				{ID: "p3", Name: "Yamada", Number: "K2"},
			}},
			{PoolName: "Pool B", Players: []domain.Player{
				{ID: "p2", Name: "Suzuki", Number: "K3"},
			}},
		}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "K1", comp.Players[0].Number)
		assert.Equal(t, "K3", comp.Players[1].Number)
		assert.Equal(t, "K2", comp.Players[2].Number)
	})

	t.Run("falls back to name when id is empty (legacy roster)", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players: []domain.Player{
				{Name: "Tanaka"}, // no ID
				{Name: "Suzuki"},
			},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{
			{Name: "Tanaka", Number: "K1"},
			{Name: "Suzuki", Number: "K2"},
		}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "K1", comp.Players[0].Number)
		assert.Equal(t, "K2", comp.Players[1].Number)
	})

	t.Run("preserves existing non-empty Number (idempotent)", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Number: "EXISTING"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "K1"}}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "EXISTING", comp.Players[0].Number, "must not overwrite an existing Number")
	})

	t.Run("skips pool players with empty Number", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: ""}}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "", comp.Players[0].Number)
	})
}

func TestViewerHandlers_Standalone(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// 1. GET /api/viewer/tournament - No tournament case: 200 with a null
	// body (a normal bootstrap state, not a 404, so the SPA doesn't log a
	// console error).
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "null", w.Body.String())

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
