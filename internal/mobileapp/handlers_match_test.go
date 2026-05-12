package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
)

func TestBulkScoreHandler(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	comp := state.Competition{ID: "c1"}
	store.SaveCompetition(&comp)
	store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-1", SideA: "P1", SideB: "P2"},
		{ID: "PoolA-2", SideA: "P1", SideB: "P3"},
	})

	t.Run("all succeed", func(t *testing.T) {
		body, _ := json.Marshal([]state.MatchResult{
			{ID: "PoolA-1", SideA: "P1", SideB: "P2", Winner: "P1", Status: state.MatchStatusCompleted},
			{ID: "PoolA-2", SideA: "P1", SideB: "P3", Winner: "P3", Status: state.MatchStatusCompleted},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/c1/matches/bulk-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Succeeded int `json:"succeeded"`
			Errors    any `json:"errors"`
		}
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 2, resp.Succeeded)
		assert.Nil(t, resp.Errors)
	})

	t.Run("partial failure", func(t *testing.T) {
		body, _ := json.Marshal([]state.MatchResult{
			{ID: "PoolA-1", SideA: "P1", SideB: "P2", Winner: "P1", Status: state.MatchStatusCompleted},
			{ID: "not-exists", SideA: "P1", SideB: "P2", Winner: "P1"},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/c1/matches/bulk-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Succeeded int `json:"succeeded"`
			Errors    []struct {
				MatchID string `json:"matchId"`
				Error   string `json:"error"`
			} `json:"errors"`
		}
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 1, resp.Succeeded)
		assert.Len(t, resp.Errors, 1)
		assert.Equal(t, "not-exists", resp.Errors[0].MatchID)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/c1/matches/bulk-score", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestQuickScoreHandler(t *testing.T) {
	r, store, eng, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	comp := state.Competition{ID: "c1", TeamSize: 3}
	store.SaveCompetition(&comp)
	store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-1", SideA: "TeamA", SideB: "TeamB"},
	})
	store.SavePools("c1", []helper.Pool{
		{PoolName: "PoolA", Players: []helper.Player{{Name: "TeamA"}, {Name: "TeamB"}}},
	})

	t.Run("team A wins", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"sideA": "TeamA", "sideB": "TeamB",
			"teamAWins": 3, "teamBWins": 1, "draws": 1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/quick-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var result state.MatchResult
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Equal(t, "TeamA", result.Winner)
		assert.Len(t, result.SubResults, 5) // 3+1+1

		standings, err := eng.CalculatePoolStandings("c1")
		assert.NoError(t, err)
		pool := standings["PoolA"]
		assert.Equal(t, 3, pool[0].IndividualWins)
		assert.Equal(t, 1, pool[0].IndividualDraws)
	})

	t.Run("draw when wins equal", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"sideA": "TeamA", "sideB": "TeamB",
			"teamAWins": 2, "teamBWins": 2, "draws": 1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/quick-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var result state.MatchResult
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Equal(t, "", result.Winner)
	})

	t.Run("missing sideA", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"sideB": "TeamB", "teamAWins": 1})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/quick-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/quick-score", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("match not found", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"sideA": "TeamA", "sideB": "TeamB", "teamAWins": 2, "teamBWins": 1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/not-exists/quick-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestMatchHandlers_Extended(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Setup competition
	comp := state.Competition{ID: "c1", Status: "setup", Courts: []string{"A"}}
	store.SaveCompetition(&comp)
	store.SavePoolMatches("c1", []state.MatchResult{{ID: "PoolA-1", SideA: "P1", SideB: "P2"}})

	t.Run("Update Match Court", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]string{"court": "B"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/court", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Override Bracket Winner", func(t *testing.T) {
		bracket := &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{{ID: "b1", SideA: "P1", SideB: "P2"}},
			},
		}
		store.SaveBracket("c1", bracket)

		reqBody, _ := json.Marshal(map[string]string{"winnerName": "P1"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/b1/override-winner", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Update Match Time", func(t *testing.T) {
		reqBody, _ := json.Marshal(map[string]string{"scheduledAt": "10:00"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/time", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/court", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Match Score - Invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/score", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Override Winner - Invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/b1/override-winner", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Update Time - Invalid JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/time", bytes.NewBufferString("{invalid"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Update Court - Engine Error", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/nonexistent/matches/m1/court", bytes.NewBufferString(`{"court": "A"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
