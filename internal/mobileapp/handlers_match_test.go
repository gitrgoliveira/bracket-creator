package mobileapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		// PoolA-1 was finalized by the previous subtest; re-scoring it is a
		// correction and must carry a reason to pass the audit gate.
		body, _ := json.Marshal([]state.MatchResult{
			{ID: "PoolA-1", SideA: "P1", SideB: "P2", Winner: "P1", Status: state.MatchStatusCompleted, CorrectionReason: "re-score after review"},
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

// TestBulkScoreHandler_CorrectionGate verifies bulk-score enforces the same
// correction-reason audit gate as PUT /score: overwriting an already-completed
// result without a reason is rejected (per-item error, original preserved),
// while a first completion and a reason-carrying correction both succeed.
func TestBulkScoreHandler_CorrectionGate(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	store.SaveCompetition(&state.Competition{ID: "cg"})
	store.SavePoolMatches("cg", []state.MatchResult{
		{ID: "PoolA-1", SideA: "P1", SideB: "P2", Winner: "P1", Status: state.MatchStatusCompleted},
		{ID: "PoolA-2", SideA: "P3", SideB: "P4"},
	})

	post := func(t *testing.T, payload []state.MatchResult) (int, struct {
		Succeeded int `json:"succeeded"`
		Errors    []struct {
			MatchID string `json:"matchId"`
			Error   string `json:"error"`
		} `json:"errors"`
	}) {
		t.Helper()
		body, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/cg/matches/bulk-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var resp struct {
			Succeeded int `json:"succeeded"`
			Errors    []struct {
				MatchID string `json:"matchId"`
				Error   string `json:"error"`
			} `json:"errors"`
		}
		assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return w.Code, resp
	}

	loadWinner := func(t *testing.T, id string) string {
		t.Helper()
		ms, err := store.LoadPoolMatches("cg")
		assert.NoError(t, err)
		for _, m := range ms {
			if m.ID == id {
				return m.Winner
			}
		}
		t.Fatalf("match %s not found", id)
		return ""
	}

	t.Run("overwrite completed without reason is rejected, original preserved", func(t *testing.T) {
		code, resp := post(t, []state.MatchResult{
			{ID: "PoolA-1", SideA: "P1", SideB: "P2", Winner: "P2", Status: state.MatchStatusCompleted},
		})
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, 0, resp.Succeeded)
		assert.Len(t, resp.Errors, 1)
		assert.Equal(t, "PoolA-1", resp.Errors[0].MatchID)
		assert.Contains(t, resp.Errors[0].Error, "correctionReason")
		assert.Equal(t, "P1", loadWinner(t, "PoolA-1"), "rejected correction must not overwrite the finalized result")
	})

	t.Run("first completion does not require a reason", func(t *testing.T) {
		code, resp := post(t, []state.MatchResult{
			{ID: "PoolA-2", SideA: "P3", SideB: "P4", Winner: "P3", Status: state.MatchStatusCompleted},
		})
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, 1, resp.Succeeded)
		assert.Empty(t, resp.Errors)
		assert.Equal(t, "P3", loadWinner(t, "PoolA-2"))
	})

	t.Run("correction with reason succeeds", func(t *testing.T) {
		code, resp := post(t, []state.MatchResult{
			{ID: "PoolA-1", SideA: "P1", SideB: "P2", Winner: "P2", Status: state.MatchStatusCompleted, CorrectionReason: "scores recorded in wrong columns"},
		})
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, 1, resp.Succeeded)
		assert.Empty(t, resp.Errors)
		assert.Equal(t, "P2", loadWinner(t, "PoolA-1"))
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

		// Sub-bouts must NOT carry team names, they're individual bout
		// slots without known competitors in quick-score mode.
		for _, sub := range result.SubResults {
			assert.Empty(t, sub.SideA, "sub-bout SideA must be empty, not team name")
			assert.Empty(t, sub.SideB, "sub-bout SideB must be empty, not team name")
		}

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

	t.Run("negative bout counts rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"sideA": "TeamA", "sideB": "TeamB", "teamAWins": -1, "teamBWins": 1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/quick-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "non-negative")
	})

	t.Run("excessive bout count rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"sideA": "TeamA", "sideB": "TeamB", "teamAWins": 50, "teamBWins": 51,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/quick-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "exceeds maximum")
	})
}

// TestScoreHandler_RevGuard validates the C2 monotonic-revision guard for
// "running" autosave writes:
//
//   - A stale running write (rev < stored high-water) is silently no-op'd
//     (HTTP 200 with {"stale":true}); the stored result is unchanged.
//   - A higher rev advances the mark and the write proceeds normally.
//   - Rev==0 (unversioned) writes always proceed regardless of the mark.
//   - Completed writes are never blocked by a stale rev.
func TestScoreHandler_RevGuard(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "rg1", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("rg1", []state.MatchResult{
		{ID: "PoolA-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
	}))

	// Reset the global rev store so tests are isolated even when run as
	// part of the full test suite (other test functions may have advanced
	// the mark for the same key).
	runningRevStore.Delete("rg1:PoolA-1")

	// The guard is scoped to a non-empty RevSession, so the helper sends one;
	// all subtests below operate within this single session.
	scoreRunning := func(rev int64) (int, map[string]any) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"sideA": "Alice", "sideB": "Bob",
			"ipponsA": []string{"M"}, "ipponsB": []string{},
			"status":     "running",
			"rev":        rev,
			"revSession": "rg-sess",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rg1/matches/PoolA-1/score", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	// scoreRunningNoSession sends Rev>0 but no RevSession, the guard must treat
	// it as unversioned and always proceed (defends against partial-rollout /
	// older clients collapsing into the "" session).
	scoreRunningNoSession := func(rev int64) (int, map[string]any) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"sideA": "Alice", "sideB": "Bob",
			"ipponsA": []string{"M"}, "ipponsB": []string{},
			"status": "running",
			"rev":    rev,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rg1/matches/PoolA-1/score", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	scoreCompleted := func() int {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"sideA": "Alice", "sideB": "Bob",
			"winner": "Alice", "ipponsA": []string{"M", "K"}, "ipponsB": []string{},
			"status": "completed",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rg1/matches/PoolA-1/score", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}

	loadWinner := func() string {
		t.Helper()
		ms, err := store.LoadPoolMatches("rg1")
		require.NoError(t, err)
		for _, m := range ms {
			if m.ID == "PoolA-1" {
				return m.Winner
			}
		}
		t.Fatal("match PoolA-1 not found")
		return ""
	}

	t.Run("rev=0 (unversioned) always proceeds", func(t *testing.T) {
		code, body := scoreRunning(0)
		assert.Equal(t, http.StatusOK, code)
		assert.Nil(t, body["stale"], "unversioned write must not be marked stale")
	})

	t.Run("higher rev advances the mark and write proceeds", func(t *testing.T) {
		code, body := scoreRunning(5)
		assert.Equal(t, http.StatusOK, code)
		assert.Nil(t, body["stale"], "higher rev should proceed, not be stale")
	})

	t.Run("same rev is not stale (equal rev always proceeds)", func(t *testing.T) {
		code, body := scoreRunning(5)
		assert.Equal(t, http.StatusOK, code)
		assert.Nil(t, body["stale"], "equal rev should proceed")
	})

	t.Run("stale running write is dropped with stale=true, stored result unchanged", func(t *testing.T) {
		// Send rev=3 after rev=5 is the stored mark, should be stale.
		code, body := scoreRunning(3)
		assert.Equal(t, http.StatusOK, code)
		stale, ok := body["stale"].(bool)
		assert.True(t, ok && stale, "stale running write must return {stale:true}")
	})

	t.Run("rev>0 without a RevSession is unversioned (always proceeds)", func(t *testing.T) {
		// Stored mark for session "rg-sess" is 5. A sessionless write with a
		// lower rev=1 must NOT be compared against it, missing RevSession is
		// treated as unversioned, so it proceeds rather than being dropped.
		code, body := scoreRunningNoSession(1)
		assert.Equal(t, http.StatusOK, code)
		assert.Nil(t, body["stale"], "a Rev without a RevSession must never be marked stale")
	})

	t.Run("completed write is never blocked by stale rev guard", func(t *testing.T) {
		// Stored mark is 5. Completed writes carry no rev gate.
		code := scoreCompleted()
		assert.Equal(t, http.StatusOK, code)
		assert.Equal(t, "Alice", loadWinner(), "completed write must persist even after stale guard is set")
	})
}

// TestScoreHandler_RevGuard_SessionTakeover validates that a write from a NEW
// RevSession always proceeds even when the stored high-water mark (from a prior
// session) has a higher Rev. This models a page reload or a different device
// starting a fresh session at rev=1, it must never be dropped as stale.
func TestScoreHandler_RevGuard_SessionTakeover(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "rgs1", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("rgs1", []state.MatchResult{
		{ID: "PoolS-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
	}))
	// Isolate from any other test runs that may have touched this key.
	runningRevStore.Delete("rgs1:PoolS-1")

	scoreRunningWithSession := func(revSession string, rev int64) (int, map[string]any) {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"sideA": "Alice", "sideB": "Bob",
			"ipponsA": []string{"M"}, "ipponsB": []string{},
			"status":     "running",
			"rev":        rev,
			"revSession": revSession,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rgs1/matches/PoolS-1/score", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}

	t.Run("session A advances to rev=5", func(t *testing.T) {
		code, body := scoreRunningWithSession("session-A", 5)
		assert.Equal(t, http.StatusOK, code)
		assert.Nil(t, body["stale"], "initial write must proceed")
	})

	t.Run("session A rev=3 is stale (same session, lower rev)", func(t *testing.T) {
		code, body := scoreRunningWithSession("session-A", 3)
		assert.Equal(t, http.StatusOK, code)
		stale, ok := body["stale"].(bool)
		assert.True(t, ok && stale, "lower rev in same session must be stale")
	})

	t.Run("session B rev=1 proceeds (new session takes over despite A being at rev=5)", func(t *testing.T) {
		code, body := scoreRunningWithSession("session-B", 1)
		assert.Equal(t, http.StatusOK, code)
		assert.Nil(t, body["stale"], "a new session must never be dropped as stale")
	})

	t.Run("session B rev=0 is unversioned and always proceeds", func(t *testing.T) {
		// Even though session B's stored mark is rev=1, a rev=0 write is
		// unversioned (the guard is gated on Rev > 0), so it must proceed
		// rather than be dropped as stale.
		code, body := scoreRunningWithSession("session-B", 0)
		assert.Equal(t, http.StatusOK, code)
		assert.Nil(t, body["stale"], "rev=0 is unversioned and must always proceed")
	})
}

// TestScoreHandler_RevGuard_PrunesOnCompletion verifies that the per-match
// runningRevStore entry is deleted once the match leaves the running state, so
// the process-lifetime map does not accumulate dead high-water marks.
func TestScoreHandler_RevGuard_PrunesOnCompletion(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "rgp1", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("rgp1", []state.MatchResult{
		{ID: "PoolP-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
	}))
	revKey := "rgp1:PoolP-1"
	runningRevStore.Delete(revKey)

	put := func(body map[string]any) int {
		t.Helper()
		payload, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rgp1/matches/PoolP-1/score", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}

	// A running write with a session populates the rev store.
	require.Equal(t, http.StatusOK, put(map[string]any{
		"sideA": "Alice", "sideB": "Bob",
		"ipponsA": []string{"M"}, "ipponsB": []string{},
		"status": "running", "rev": 3, "revSession": "sess-p",
	}))
	_, present := runningRevStore.Load(revKey)
	require.True(t, present, "running write must populate the rev store")

	// A completed write must prune the entry.
	require.Equal(t, http.StatusOK, put(map[string]any{
		"sideA": "Alice", "sideB": "Bob",
		"winner": "Alice", "ipponsA": []string{"M", "K"}, "ipponsB": []string{},
		"status": "completed",
	}))
	_, present = runningRevStore.Load(revKey)
	assert.False(t, present, "completed write must prune the rev store entry")
}

// TestScoreHandler_MidLengthCap verifies that the score endpoint rejects a
// match ID that exceeds MaxLenMatchID bytes with HTTP 400.
func TestScoreHandler_MidLengthCap(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "midcap1", Courts: []string{"A"}}))

	longMid := strings.Repeat("x", MaxLenMatchID+1) // 129 'x' characters

	payload, _ := json.Marshal(map[string]any{
		"sideA": "Alice", "sideB": "Bob",
		"status": "running",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/midcap1/matches/"+longMid+"/score", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	// Shared validateMaxLen helper → consistent ValidationError body that
	// names the field and includes the limit.
	assert.Contains(t, body["error"], "matchId")
	assert.Contains(t, body["error"], fmt.Sprintf("must be <= %d", MaxLenMatchID))
}

// TestScoreHandlers_RejectSideMismatch pins the HTTP 409 mapping for the
// match-identity guard: a score/quick-score payload naming competitors that
// differ from the stored pairing is rejected, and the stored match is left
// untouched. Exercises the production TX /score path and the /quick-score
// path end-to-end.
func TestScoreHandlers_RejectSideMismatch(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	store.SaveCompetition(&state.Competition{ID: "c1", Courts: []string{"A"}})
	store.SavePools("c1", []helper.Pool{
		{PoolName: "PoolE", Players: []helper.Player{{Name: "Benjamin Evans"}, {Name: "Sebastian Allen"}}},
	})
	store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolE-0", SideA: "Benjamin Evans", SideB: "Sebastian Allen", Status: state.MatchStatusScheduled},
	})

	assertPairingUntouched := func(t *testing.T) {
		t.Helper()
		stored, err := store.LoadPoolMatches("c1")
		assert.NoError(t, err)
		assert.Len(t, stored, 1)
		assert.Equal(t, "Benjamin Evans", stored[0].SideA)
		assert.Equal(t, "Sebastian Allen", stored[0].SideB)
		assert.Equal(t, state.MatchStatusScheduled, stored[0].Status)
	}

	t.Run("score endpoint rejects foreign competitors with 409", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id": "PoolE-0", "sideA": "Arthur Conan", "sideB": "Herman Melville",
			"winner": "Arthur Conan", "ipponsA": []string{"M"}, "ipponsB": []string{},
			"status": "completed",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolE-0/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "side_mismatch")
		assertPairingUntouched(t)
	})

	t.Run("quick-score endpoint rejects foreign competitors with 409", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"sideA": "Arthur Conan", "sideB": "Herman Melville",
			"teamAWins": 2, "teamBWins": 1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolE-0/quick-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "side_mismatch")
		assertPairingUntouched(t)
	})

	t.Run("score endpoint accepts the correct pairing", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id": "PoolE-0", "sideA": "Benjamin Evans", "sideB": "Sebastian Allen",
			"winner": "Benjamin Evans", "ipponsA": []string{"M"}, "ipponsB": []string{},
			"status": "completed",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/PoolE-0/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		stored, err := store.LoadPoolMatches("c1")
		assert.NoError(t, err)
		assert.Equal(t, "Benjamin Evans", stored[0].Winner)
		assert.Equal(t, state.MatchStatusCompleted, stored[0].Status)
	})
}

// TestOverrideWinner_EngiGuard verifies the override-winner endpoint rejects
// engi competitions with 400: a manual winner override sets Winner without
// FlagsA/FlagsB, which would leave a completed engi match with a 0-0 flag
// total that violates the {1,3,5} invariant. Flag scoring is the only engi
// result path. Mirrors TestDecisionHandler_EngiGuard.
func TestOverrideWinner_EngiGuard(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "engi-override"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: cid, Engi: true}))
	require.NoError(t, store.SaveBracket(cid, &state.Bracket{
		Rounds: [][]state.BracketMatch{{{ID: "b1", SideA: "P1", SideB: "P2"}}},
	}))

	body, _ := json.Marshal(map[string]string{"winnerName": "P1"})
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/matches/b1/override-winner", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "override-winner on engi comp must return 400; body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "engi")

	// The bracket winner must remain unset (the guard returns before the engine).
	stored, err := store.LoadBracket(cid)
	require.NoError(t, err)
	assert.Empty(t, stored.Rounds[0][0].Winner, "engi override must not set a winner")
}

// TestOverrideWinner_FailsClosedOnLoadError verifies the engi guard on the
// override-winner endpoint fails CLOSED: when LoadCompetition faults we can't
// tell whether the comp is engi, so the override is rejected with 500 rather
// than slipping through into the inconsistent flag-less state the guard exists
// to prevent. Mirrors the quick-score / daihyosen / decision guards.
func TestOverrideWinner_FailsClosedOnLoadError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "override-fail-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	realStore, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(realStore)
	hub := NewHub()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, failingCompetitionStore{err: errors.New("disk on fire")}, realStore, hub, NewFileVerifier(realStore), realStore)

	body, _ := json.Marshal(map[string]string{"winnerName": "P1"})
	req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/b1/override-winner", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equalf(t, http.StatusInternalServerError, w.Code, "body=%s", w.Body.String())
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

	// Sibling of the rank-override TrimSpace test in
	// handlers_competition_test.go. Downstream bracket math compares
	// m.Winner to roster names by exact string equality, so padded
	// "  Foo  " won't match canonical "Foo", pin the trim contract.
	t.Run("Override Bracket Winner Trims Whitespace", func(t *testing.T) {
		bracket := &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{{ID: "b-trim", SideA: "P1", SideB: "P2"}},
			},
		}
		store.SaveBracket("c1", bracket)

		reqBody, _ := json.Marshal(map[string]string{"winnerName": "  P1  "})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/b-trim/override-winner", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		stored, err := store.LoadBracket("c1")
		require.NoError(t, err)
		require.NotNil(t, stored)
		var found *state.BracketMatch
		for i := range stored.Rounds {
			for j := range stored.Rounds[i] {
				if stored.Rounds[i][j].ID == "b-trim" {
					found = &stored.Rounds[i][j]
				}
			}
		}
		require.NotNil(t, found, "bracket match b-trim not found")
		assert.Equal(t, "P1", found.Winner, "winner should be trimmed before propagation")
	})

	t.Run("Override Bracket Winner Rejects Whitespace-Only", func(t *testing.T) {
		// Whitespace-only winnerName trims to empty, same shape as the
		// rank-override empty-after-trim rejection.
		reqBody, _ := json.Marshal(map[string]string{"winnerName": "   "})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/b1/override-winner", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "winnerName is required")
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

// TestScoreHandler_CompletionBroadcastContract verifies that scoring the final
// pool match emits EventCompetitionCompleted exactly once, and that scoring a
// non-final match does not emit it.
// NOTE: uses league format, mixed format no longer auto-completes after pools
// (it stays in pools status; the knockout fills in incrementally as each pool finishes).
func TestScoreHandler_CompletionBroadcastContract(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	comp := state.Competition{
		ID:     "pools1",
		Format: state.CompFormatLeague,
		Status: state.CompStatusPools,
	}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("pools1", []domain.Player{
		{Name: "P1"}, {Name: "P2"}, {Name: "P3"},
	}))
	require.NoError(t, store.SavePoolMatches("pools1", []state.MatchResult{
		{ID: "PoolA-1", SideA: "P1", SideB: "P2"},
		{ID: "PoolA-2", SideA: "P1", SideB: "P3"},
	}))

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	// Omit sideA/sideB from the patch so the engine preserves the stored
	// participants. Hardcoding "P1"/"P2" for every match would mutate
	// PoolA-2 (which has P1 vs P3) and mask side-preservation bugs.
	scoreMatch := func(mid, winner string) {
		body, _ := json.Marshal(state.MatchResult{
			ID:     mid,
			Winner: winner,
			Status: state.MatchStatusCompleted,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/pools1/matches/"+mid+"/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}

	// Partial completion, no EventCompetitionCompleted
	scoreMatch("PoolA-1", "P1")
	partial := drainHubEvents(t, ch, 30*time.Millisecond)
	assert.Equal(t, 0, countCompletedEvents(t, partial, "pools1"),
		"partial completion must not broadcast competition_completed")

	// Final match, EventCompetitionCompleted must be emitted exactly once
	scoreMatch("PoolA-2", "P1")
	final := drainHubEvents(t, ch, 100*time.Millisecond)
	assert.Equal(t, 1, countCompletedEvents(t, final, "pools1"),
		"final match must emit competition_completed exactly once")
}

// drainHubEvents pulls every queued event off the given hub-subscriber
// channel within d, decoding each into SSEEvent for inspection.
func drainHubEvents(t *testing.T, ch <-chan string, d time.Duration) []SSEEvent {
	t.Helper()
	var events []SSEEvent
	deadline := time.After(d)
	for {
		select {
		case msg := <-ch:
			var e SSEEvent
			require.NoError(t, json.Unmarshal([]byte(msg), &e))
			events = append(events, e)
		case <-deadline:
			return events
		}
	}
}

// countCompletedEvents counts EventCompetitionCompleted events and asserts
// each carries the expected competitionId.
func countCompletedEvents(t *testing.T, events []SSEEvent, wantCompID string) int {
	t.Helper()
	n := 0
	for _, e := range events {
		if e.Type != EventCompetitionCompleted {
			continue
		}
		n++
		data, isMap := e.Data.(map[string]any)
		require.True(t, isMap, "competition_completed data must be a map")
		assert.Equal(t, wantCompID, data["competitionId"])
	}
	return n
}

// TestBulkScoreHandler_CompletionBroadcastContract verifies that bulk-scoring
// the last remaining pool matches emits EventCompetitionCompleted exactly
// once (and partial bulk completion does not).
// NOTE: uses league format, mixed format no longer auto-completes after pools.
func TestBulkScoreHandler_CompletionBroadcastContract(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "bulk1", Format: state.CompFormatLeague, Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SaveParticipants("bulk1", []domain.Player{
		{Name: "P1"}, {Name: "P2"}, {Name: "P3"},
	}))
	require.NoError(t, store.SavePoolMatches("bulk1", []state.MatchResult{
		{ID: "PoolA-1", SideA: "P1", SideB: "P2"},
		{ID: "PoolA-2", SideA: "P1", SideB: "P3"},
		{ID: "PoolA-3", SideA: "P2", SideB: "P3"},
	}))

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	bulkScore := func(results []state.MatchResult) {
		body, _ := json.Marshal(results)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/bulk1/matches/bulk-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}

	// Partial bulk completion (1 of 3), no competition_completed
	bulkScore([]state.MatchResult{
		{ID: "PoolA-1", Winner: "P1", Status: state.MatchStatusCompleted},
	})
	partial := drainHubEvents(t, ch, 30*time.Millisecond)
	assert.Equal(t, 0, countCompletedEvents(t, partial, "bulk1"),
		"partial bulk completion must not broadcast competition_completed")

	// Final batch closes out the comp, exactly one competition_completed
	bulkScore([]state.MatchResult{
		{ID: "PoolA-2", Winner: "P1", Status: state.MatchStatusCompleted},
		{ID: "PoolA-3", Winner: "P2", Status: state.MatchStatusCompleted},
	})
	final := drainHubEvents(t, ch, 100*time.Millisecond)
	assert.Equal(t, 1, countCompletedEvents(t, final, "bulk1"),
		"final bulk-score batch must emit competition_completed exactly once")
}

// TestQuickScoreHandler_CompletionBroadcastContract verifies that
// quick-scoring the last remaining pool match emits EventCompetitionCompleted
// exactly once, and a non-final quick-score does not.
// NOTE: uses league format with TeamSize=3, mixed format no longer auto-completes
// after pools (it stays in pools status; the knockout fills in incrementally as each pool finishes).
func TestQuickScoreHandler_CompletionBroadcastContract(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "qs1", Format: state.CompFormatLeague, Status: state.CompStatusPools, TeamSize: 3,
	}))
	require.NoError(t, store.SavePools("qs1", []helper.Pool{
		{PoolName: "PoolA", Players: []helper.Player{{Name: "TeamA"}, {Name: "TeamB"}, {Name: "TeamC"}}},
	}))
	require.NoError(t, store.SavePoolMatches("qs1", []state.MatchResult{
		{ID: "PoolA-1", SideA: "TeamA", SideB: "TeamB"},
		{ID: "PoolA-2", SideA: "TeamA", SideB: "TeamC"},
	}))

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	quickScore := func(mid, sideA, sideB string, aWins, bWins, draws int) {
		body, _ := json.Marshal(map[string]any{
			"sideA": sideA, "sideB": sideB,
			"teamAWins": aWins, "teamBWins": bWins, "draws": draws,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/qs1/matches/"+mid+"/quick-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
	}

	// First match, no completion
	quickScore("PoolA-1", "TeamA", "TeamB", 2, 1, 0)
	partial := drainHubEvents(t, ch, 30*time.Millisecond)
	assert.Equal(t, 0, countCompletedEvents(t, partial, "qs1"),
		"partial quick-score must not broadcast competition_completed")

	// Final match, TeamA wins 3-0 so TeamC gets 0 IV (vs TeamB's 1 IV),
	// avoiding a tie that would defer completion via tiebreaker injection.
	quickScore("PoolA-2", "TeamA", "TeamC", 3, 0, 0)
	final := drainHubEvents(t, ch, 100*time.Millisecond)
	assert.Equal(t, 1, countCompletedEvents(t, final, "qs1"),
		"final quick-score must emit competition_completed exactly once")
}

// TestTryAutoCompletePools_SanitizesErrorHeader locks in the contract that
// when MaybeAutoCompletePools fails, the response carries the generic
// AutoCompleteErrorValue sentinel, never the raw error string, which can
// contain filesystem paths or other internal store details.
func TestTryAutoCompletePools_SanitizesErrorHeader(t *testing.T) {
	_, _, eng, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	// An ID containing "/" trips ValidateCompetitionID inside
	// LoadCompetition, exercising the error path with a deterministic
	// non-I/O failure (no need to fault-inject the filesystem).
	tryAutoCompletePools(c, eng, hub, "../bad")

	got := w.Header().Get(AutoCompleteErrorHeader)
	assert.Equal(t, AutoCompleteErrorValue, got,
		"header must be the generic sentinel, not the raw error")
	assert.NotContains(t, got, "competition ID",
		"raw validation error text must not leak into the response header")
	assert.NotContains(t, got, "invalid",
		"raw validation error text must not leak into the response header")
}

// TestPostScoreKikenAutoFillsRegulation, T086: POST /score with
// decision=kiken, decisionBy=shiro, encho=null and a 0-2 scoreline
// returns 200 and the persisted match round-trips the decision metadata.
//
// FR-031, contracts/match-decisions.md.
func TestPostScoreKikenAutoFillsRegulation(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "kiken-reg"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-1", SideA: "Alice", SideB: "Bob"},
	}))

	body, _ := json.Marshal(state.MatchResult{
		ID:         "PoolA-1",
		Decision:   "kiken",
		DecisionBy: "shiro",
		Winner:     "Alice",
		IpponsA:    []string{"M", "M"},
		IpponsB:    nil,
		Status:     state.MatchStatusCompleted,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "kiken-voluntary", stored[0].Decision)
	assert.Equal(t, "shiro", stored[0].DecisionBy)
	assert.Equal(t, "Alice", stored[0].Winner)
}

// TestPostScoreKikenInEncho, T087: POST /score with
// decision=kiken, decisionBy=shiro, encho.periodCount=1 and a 0-1
// scoreline returns 200.
func TestPostScoreKikenInEncho(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "kiken-encho"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-1", SideA: "Alice", SideB: "Bob"},
	}))

	body, _ := json.Marshal(state.MatchResult{
		ID:         "PoolA-1",
		Decision:   "kiken",
		DecisionBy: "shiro",
		Winner:     "Alice",
		IpponsA:    []string{"M"},
		IpponsB:    nil,
		Encho:      &state.EnchoMetadata{PeriodCount: 1},
		Status:     state.MatchStatusCompleted,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	stored, _ := store.LoadPoolMatches(compID)
	require.Len(t, stored, 1)
	require.NotNil(t, stored[0].Encho)
	assert.Equal(t, 1, stored[0].Encho.PeriodCount)
}

// TestPostScoreKikenInvalidScoreline, T088: POST /score with
// decision=kiken, encho=null, and a 0-1 scoreline (regulation requires
// 2-0) returns 400 with the validator's field message.
func TestPostScoreKikenInvalidScoreline(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "kiken-bad"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-1", SideA: "Alice", SideB: "Bob"},
	}))

	body, _ := json.Marshal(state.MatchResult{
		ID:         "PoolA-1",
		Decision:   "kiken",
		DecisionBy: "shiro",
		Winner:     "Alice",
		IpponsA:    []string{"M"},
		IpponsB:    nil,
		Status:     state.MatchStatusCompleted,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "scoreline")
}

// failingCompetitionStore returns the configured error from
// LoadCompetition. Used to drive the fail-closed path in
// enforceEnchoCap / bulk-score when config.md can't be loaded.
type failingCompetitionStore struct{ err error }

func (f failingCompetitionStore) LoadCompetition(string) (*state.Competition, error) {
	return nil, f.err
}

func (f failingCompetitionStore) LoadPoolMatches(string) ([]state.MatchResult, error) {
	return nil, f.err
}

func (f failingCompetitionStore) LoadBracket(string) (*state.Bracket, error) {
	return nil, f.err
}

// TestEnchoExceedsCap covers the pure predicate. Force, missing comp,
// 0 cap, and within-cap all return false; only an over-cap count with
// !force returns true.
func TestEnchoExceedsCap(t *testing.T) {
	cases := []struct {
		name  string
		encho *state.EnchoMetadata
		comp  *state.Competition
		force bool
		want  bool
	}{
		{name: "nil encho", encho: nil, comp: &state.Competition{MaxEnchoPeriods: 2}, want: false},
		{name: "zero period count", encho: &state.EnchoMetadata{PeriodCount: 0}, comp: &state.Competition{MaxEnchoPeriods: 2}, want: false},
		{name: "nil comp", encho: &state.EnchoMetadata{PeriodCount: 5}, comp: nil, want: false},
		{name: "zero cap means unlimited", encho: &state.EnchoMetadata{PeriodCount: 99}, comp: &state.Competition{MaxEnchoPeriods: 0}, want: false},
		{name: "within cap", encho: &state.EnchoMetadata{PeriodCount: 2}, comp: &state.Competition{MaxEnchoPeriods: 2}, want: false},
		{name: "at cap boundary", encho: &state.EnchoMetadata{PeriodCount: 2}, comp: &state.Competition{MaxEnchoPeriods: 3}, want: false},
		{name: "over cap without force", encho: &state.EnchoMetadata{PeriodCount: 3}, comp: &state.Competition{MaxEnchoPeriods: 2}, want: true},
		{name: "over cap with force", encho: &state.EnchoMetadata{PeriodCount: 3}, comp: &state.Competition{MaxEnchoPeriods: 2}, force: true, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := enchoExceedsCap(tc.encho, tc.comp, tc.force)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestEnforceEnchoCap_ScoreHandler covers the gin wrapper as wired
// into the single-score endpoint: 500 on store failure (the bug this
// fix closes), 400 with limit echoed on cap exceeded, and 200 when
// the cap is unset.
func TestEnforceEnchoCap_ScoreHandler(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "encho-cap-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	realStore, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(realStore)
	hub := NewHub()

	compID := "encho-cap-test"
	require.NoError(t, realStore.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
		MaxEnchoPeriods: 2,
	}))
	require.NoError(t, realStore.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"},
	}))
	require.NoError(t, realStore.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-1", SideA: "Alice", SideB: "Bob"},
	}))

	score := func(periodCount int) []byte {
		body, _ := json.Marshal(state.MatchResult{
			ID: "PoolA-1", SideA: "Alice", SideB: "Bob",
			Winner: "Alice", IpponsA: []string{"M"},
			Encho:  &state.EnchoMetadata{PeriodCount: periodCount},
			Status: state.MatchStatusCompleted,
		})
		return body
	}

	t.Run("load failure returns 500", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		admin := r.Group("/api")
		// Wire the score handler with a failing CompetitionStore so the
		// cap check exercises the new fail-closed branch. The engine
		// keeps the real store but never gets called, enforceEnchoCap
		// aborts the request first.
		registerScoreHandler(admin, eng, failingCompetitionStore{err: errors.New("disk on fire")}, realStore, hub, NewFileVerifier(realStore), realStore)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(score(1)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equalf(t, http.StatusInternalServerError, w.Code, "body=%s", w.Body.String())
		assert.Contains(t, w.Body.String(), "failed to validate encho limits")
	})

	t.Run("over cap returns 400 with limit", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		admin := r.Group("/api")
		registerScoreHandler(admin, eng, realStore, realStore, hub, NewFileVerifier(realStore), realStore)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(score(3)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equalf(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
		var resp struct {
			Error string `json:"error"`
			Limit int    `json:"limit"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "max_encho_exceeded", resp.Error)
		assert.Equal(t, 2, resp.Limit)
	})

	t.Run("force bypasses cap", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		admin := r.Group("/api")
		registerScoreHandler(admin, eng, realStore, realStore, hub, NewFileVerifier(realStore), realStore)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score?force=true", bytes.NewBuffer(score(3)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	})
}

// TestEnforceEnchoCapWithSubs covers the sub-bout encho cap path added in mp-4pc.
// anySubBoutEnchoExceedsCap inspects each subResults[].encho.periodCount; the
// single-score and bulk-score handlers must both enforce it (same cap, same 400
// response shape). force=true bypasses the cap for both paths.
func TestEnforceEnchoCapWithSubs(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sub-encho-cap-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	realStore, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(realStore)
	hub := NewHub()

	compID := "sub-encho-cap-test"
	require.NoError(t, realStore.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
		MaxEnchoPeriods: 2,
	}))
	require.NoError(t, realStore.SaveParticipants(compID, []domain.Player{
		{Name: "TeamA"}, {Name: "TeamB"},
	}))
	require.NoError(t, realStore.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-1", SideA: "TeamA", SideB: "TeamB"},
	}))

	overCapSubResult := state.SubMatchResult{
		Position: -1, SideA: "TeamA", SideB: "TeamB",
		IpponsA: []string{"M"}, Winner: "TeamA",
		Encho: &state.EnchoMetadata{PeriodCount: 3}, // exceeds cap of 2; daihyosen is the only sub-bout with encho
	}

	t.Run("single-score: sub-bout encho over cap returns 400", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		admin := r.Group("/api")
		registerScoreHandler(admin, eng, realStore, realStore, hub, NewFileVerifier(realStore), realStore)

		body, _ := json.Marshal(state.MatchResult{
			ID: "PoolA-1", SideA: "TeamA", SideB: "TeamB",
			Winner: "TeamA", Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{overCapSubResult},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equalf(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
		var resp struct {
			Error string `json:"error"`
			Limit int    `json:"limit"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "max_encho_exceeded", resp.Error)
		assert.Equal(t, 2, resp.Limit)
	})

	t.Run("single-score: force=true bypasses sub-bout encho cap", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		admin := r.Group("/api")
		registerScoreHandler(admin, eng, realStore, realStore, hub, NewFileVerifier(realStore), realStore)

		body, _ := json.Marshal(state.MatchResult{
			ID: "PoolA-1", SideA: "TeamA", SideB: "TeamB",
			Winner: "TeamA", Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{overCapSubResult},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score?force=true", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	})

	t.Run("bulk-score: sub-bout encho over cap is recorded as per-item error", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		admin := r.Group("/api")
		RegisterMatchHandlers(admin, eng, realStore, realStore, hub, NewFileVerifier(realStore), realStore)

		body, _ := json.Marshal([]state.MatchResult{
			{
				ID: "PoolA-1", SideA: "TeamA", SideB: "TeamB",
				Winner: "TeamA", Status: state.MatchStatusCompleted,
				SubResults: []state.SubMatchResult{overCapSubResult},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/matches/bulk-score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		// Bulk-score always returns 200; cap violations land in the errors array.
		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
		var resp struct {
			Succeeded int `json:"succeeded"`
			Errors    []struct {
				MatchID string `json:"matchId"`
				Error   string `json:"error"`
			} `json:"errors"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, 0, resp.Succeeded)
		require.Len(t, resp.Errors, 1)
		assert.Equal(t, "PoolA-1", resp.Errors[0].MatchID)
		assert.Equal(t, "max_encho_exceeded", resp.Errors[0].Error)
	})
}

// TestBulkScore_FailsClosedOnLoadError, when the cap-check load
// fails for a bulk-score request, the entire batch is rejected with
// 500 rather than silently bypassing the MaxEnchoPeriods cap on every
// entry.
func TestBulkScore_FailsClosedOnLoadError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "bulk-cap-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	realStore, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(realStore)
	hub := NewHub()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	// RegisterMatchHandlers takes the concrete *Hub; the cap check
	// uses the CompetitionStore parameter (which we fault here) and
	// returns 500 before any handler reaches the hub or engine.
	RegisterMatchHandlers(admin, eng, failingCompetitionStore{err: errors.New("disk on fire")}, realStore, hub, NewFileVerifier(realStore), realStore)

	body, _ := json.Marshal([]state.MatchResult{
		{ID: "PoolA-1", SideA: "P1", SideB: "P2", Winner: "P1", Status: state.MatchStatusCompleted},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/c1/matches/bulk-score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equalf(t, http.StatusInternalServerError, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "failed to validate encho limits")
}

// TestAnnotateQueuePositions_NonEmpty verifies that annotateQueuePositions
// fills in per-court queue positions for a non-empty match list.
func TestAnnotateQueuePositions_NonEmpty(t *testing.T) {
	matches := []state.MatchResult{
		{ID: "m1", Court: "A", Status: state.MatchStatusScheduled},
		{ID: "m2", Court: "A", Status: state.MatchStatusScheduled},
		{ID: "m3", Court: "B", Status: state.MatchStatusScheduled},
	}
	annotateQueuePositions(matches)
	// Positions within a court should be monotonically increasing from 1.
	assert.Equal(t, 1, matches[0].QueuePosition)
	assert.Equal(t, 2, matches[1].QueuePosition)
	assert.Equal(t, 1, matches[2].QueuePosition)
}

// TestAnnotateQueuePositions_Empty verifies that annotateQueuePositions is
// a no-op for an empty slice.
func TestAnnotateQueuePositions_Empty(t *testing.T) {
	annotateQueuePositions(nil)
	annotateQueuePositions([]state.MatchResult{})
}

// TestAnnotateQueuePositions_ScheduledAtOrder verifies that the annotator
// sorts per-court by ScheduledAt rather than relying on slice order.
// UpdateMatchTime / UpdateMatchCourt mutate the underlying CSV in place
// without reordering rows, so the server-side annotation must derive
// ordering from the data, not the storage order, to agree with the
// viewer's render order and the client-side SSE recompute. Copilot
// flagged this on PR #124 (web-mobile/js/patch.jsx:110).
func TestAnnotateQueuePositions_ScheduledAtOrder(t *testing.T) {
	matches := []state.MatchResult{
		// Storage order is out of schedule order: the 09:30 row sits
		// before the 09:00 row on the same court.
		{ID: "m1", Court: "A", ScheduledAt: "09:30", Status: state.MatchStatusScheduled},
		{ID: "m2", Court: "A", ScheduledAt: "09:00", Status: state.MatchStatusScheduled},
		{ID: "m3", Court: "B", ScheduledAt: "09:15", Status: state.MatchStatusScheduled},
	}
	annotateQueuePositions(matches)
	// m2 (09:00) is up-next on court A, m1 (09:30) is queue-position 2.
	assert.Equal(t, 2, matches[0].QueuePosition) // m1 at 09:30
	assert.Equal(t, 1, matches[1].QueuePosition) // m2 at 09:00
	assert.Equal(t, 1, matches[2].QueuePosition) // m3 on court B
}

// TestAnnotateQueuePositions_StaleReset verifies that non-scheduled rows
// have their QueuePosition forced to 0 even if a stale persisted value
// were present, mirroring the bracket-variant guard so omitempty drops
// the field cleanly on the wire.
func TestAnnotateQueuePositions_StaleReset(t *testing.T) {
	matches := []state.MatchResult{
		{ID: "m1", Court: "A", Status: state.MatchStatusRunning, QueuePosition: 99},
		{ID: "m2", Court: "A", Status: state.MatchStatusScheduled, QueuePosition: 77},
	}
	annotateQueuePositions(matches)
	assert.Equal(t, 0, matches[0].QueuePosition)
	assert.Equal(t, 1, matches[1].QueuePosition)
}

// TestAnnotateBracketQueuePositions_* mirrors the annotateQueuePositions tests
// for the bracket variant (BracketMatch), covering multiple courts, mixed
// statuses, nil/empty inputs, and stale-value reset.
func TestAnnotateBracketQueuePositions_Nil(t *testing.T) {
	annotateBracketQueuePositions(nil) // must not panic
}

func TestAnnotateBracketQueuePositions_Empty(t *testing.T) {
	annotateBracketQueuePositions(&state.Bracket{})
	annotateBracketQueuePositions(&state.Bracket{Rounds: [][]state.BracketMatch{}})
}

func TestAnnotateBracketQueuePositions_MultipleCourts(t *testing.T) {
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "r1m1", Court: "A", Status: state.MatchStatusScheduled},
				{ID: "r1m2", Court: "B", Status: state.MatchStatusScheduled},
			},
			{
				{ID: "r2m1", Court: "A", Status: state.MatchStatusScheduled},
			},
		},
	}
	annotateBracketQueuePositions(b)

	assert.Equal(t, 1, b.Rounds[0][0].QueuePosition, "r1m1: first on court A")
	assert.Equal(t, 1, b.Rounds[0][1].QueuePosition, "r1m2: first on court B")
	assert.Equal(t, 2, b.Rounds[1][0].QueuePosition, "r2m1: second on court A")
}

func TestAnnotateBracketQueuePositions_MixedStatuses(t *testing.T) {
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m1", Court: "A", Status: state.MatchStatusRunning, QueuePosition: 99},
				{ID: "m2", Court: "A", Status: state.MatchStatusScheduled},
				{ID: "m3", Court: "A", Status: state.MatchStatusCompleted, QueuePosition: 77},
				{ID: "m4", Court: "A", Status: state.MatchStatusScheduled},
			},
		},
	}
	annotateBracketQueuePositions(b)

	assert.Equal(t, 0, b.Rounds[0][0].QueuePosition, "running: stale value reset to 0")
	assert.Equal(t, 1, b.Rounds[0][1].QueuePosition, "first scheduled on court A")
	assert.Equal(t, 0, b.Rounds[0][2].QueuePosition, "completed: stale value reset to 0")
	assert.Equal(t, 2, b.Rounds[0][3].QueuePosition, "second scheduled on court A")
}

// TestAnnotateBracketQueuePositions_ScheduledAtOrdering verifies that queue
// positions follow per-court ScheduledAt ordering, not storage order.
// Mirrors the viewer's ScheduleViewer per-court sort so the "N before yours"
// label is consistent with the row order the user actually sees.
func TestAnnotateBracketQueuePositions_ScheduledAtOrdering(t *testing.T) {
	// Round 0 holds an early match scheduled at 11:00 on court A; round 1
	// holds an earlier-scheduled match (10:30) on the same court. The
	// later round should rank first because its scheduledAt is earlier.
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "r0m0", Court: "A", ScheduledAt: "11:00", Status: state.MatchStatusScheduled},
				{ID: "r0m1", Court: "A", ScheduledAt: "11:30", Status: state.MatchStatusScheduled},
			},
			{
				{ID: "r1m0", Court: "A", ScheduledAt: "10:30", Status: state.MatchStatusScheduled},
			},
		},
	}
	annotateBracketQueuePositions(b)

	assert.Equal(t, 2, b.Rounds[0][0].QueuePosition, "r0m0 @ 11:00 = 2nd on court A")
	assert.Equal(t, 3, b.Rounds[0][1].QueuePosition, "r0m1 @ 11:30 = 3rd on court A")
	assert.Equal(t, 1, b.Rounds[1][0].QueuePosition, "r1m0 @ 10:30 = 1st on court A despite later round")
}

// TestAnnotateBracketQueuePositions_RunningRanksFirst verifies that a
// running match counts ahead of scheduled siblings in the per-court sort
// (status priority dominates ScheduledAt), matching the viewer.
func TestAnnotateBracketQueuePositions_RunningRanksFirst(t *testing.T) {
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				// Scheduled match at 10:00, but a running match at 11:00
				// should be ordered first because status=running has the
				// highest priority in the viewer's sort.
				{ID: "sched", Court: "A", ScheduledAt: "10:00", Status: state.MatchStatusScheduled},
				{ID: "live", Court: "A", ScheduledAt: "11:00", Status: state.MatchStatusRunning},
			},
		},
	}
	annotateBracketQueuePositions(b)

	// Only the scheduled match gets a 1-indexed position; the running
	// match keeps QueuePosition=0 (it isn't "in queue"). But because the
	// running match ranks ahead in the sort, the counter doesn't
	// increment past it before reaching the scheduled match, so the
	// scheduled match's position is still 1, not 2.
	assert.Equal(t, 0, b.Rounds[0][1].QueuePosition, "running has no queue position")
	assert.Equal(t, 1, b.Rounds[0][0].QueuePosition, "lone scheduled = position 1")
}

// TestAnnotateBracketQueuePositions_EmptyScheduledAt verifies that matches
// without a ScheduledAt fall to the end of their per-court bucket (matches
// the JS fallback to "99:99" in ScheduleViewer's sort).
func TestAnnotateBracketQueuePositions_EmptyScheduledAt(t *testing.T) {
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "no-time", Court: "A", ScheduledAt: "", Status: state.MatchStatusScheduled},
				{ID: "has-time", Court: "A", ScheduledAt: "10:00", Status: state.MatchStatusScheduled},
			},
		},
	}
	annotateBracketQueuePositions(b)

	assert.Equal(t, 1, b.Rounds[0][1].QueuePosition, "has-time = 1st")
	assert.Equal(t, 2, b.Rounds[0][0].QueuePosition, "no-time = 2nd (sinks to end)")
}

// TestAnnotateBracketQueuePositions_ThirdPlaceMatch is a Finding 7 regression
// test: the bronze Naginata match is a sibling of Rounds (bracket.ThirdPlaceMatch)
// and must receive a queue position so operators see it in the schedule feed.
func TestAnnotateBracketQueuePositions_ThirdPlaceMatch(t *testing.T) {
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-sf1", Court: "A", Status: state.MatchStatusCompleted},
				{ID: "m-sf2", Court: "A", Status: state.MatchStatusCompleted},
			},
			{
				{ID: "m-final", Court: "A", Status: state.MatchStatusScheduled},
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:     "m-bronze",
			Court:  "A",
			Status: state.MatchStatusScheduled,
		},
	}
	annotateBracketQueuePositions(b)

	// Final and bronze are both scheduled on court A with blank scheduledAt. The
	// bronze is conventionally played JUST BEFORE the final (viewer_awards: "the
	// bronze is normally played first"), so it sorts first (round=finalRound,
	// position -1), then the final.
	assert.Equal(t, 1, b.ThirdPlaceMatch.QueuePosition, "bronze must be 1st scheduled (plays before the final)")
	assert.Equal(t, 2, b.Rounds[1][0].QueuePosition, "final must be 2nd scheduled")
	// Completed semis get no queue position.
	assert.Equal(t, 0, b.Rounds[0][0].QueuePosition, "completed sf1 must have position 0")
	assert.Equal(t, 0, b.Rounds[0][1].QueuePosition, "completed sf2 must have position 0")
}

// setupSelfRunScoreRouter creates a minimal gin router with just the score
// endpoint, wired to a fresh store that has a self-run tournament and one
// pool match pre-seeded.
func setupSelfRunScoreRouter(t *testing.T, mainPw string) (*gin.Engine, *state.Store, string) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "selfrun-score-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "SelfRun",
		Password: mainPw,
		Courts:   []string{"A"},
		Mode:     state.TournamentModeSelfRun,
	}))
	compID := "c1"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-1", SideA: "Alice", SideB: "Bob"},
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	verifier := NewFileVerifier(store)
	RegisterMatchHandlers(admin, eng, store, store, hub, verifier, store)

	return r, store, compID
}

// TestSelfRunScoreHandler verifies the decision allowlist, finalized guard,
// and resultSource provenance for self-run tournaments.
func TestSelfRunScoreHandler(t *testing.T) {
	t.Run("anonymous PUT score with fought decision returns 200 and self-reported source", func(t *testing.T) {
		r, store, compID := setupSelfRunScoreRouter(t, "secret")

		body, _ := json.Marshal(state.MatchResult{
			ID:      "PoolA-1",
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Alice",
			IpponsA: []string{"M", "K"},
			Status:  state.MatchStatusCompleted,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var result state.MatchResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Equal(t, "self-reported", result.ResultSource)

		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		assert.Equal(t, "self-reported", stored[0].ResultSource)
	})

	t.Run("anonymous PUT score with hikiwake returns 200 and self-reported source", func(t *testing.T) {
		r, _, compID := setupSelfRunScoreRouter(t, "secret")

		body, _ := json.Marshal(state.MatchResult{
			ID:     "PoolA-1",
			SideA:  "Alice",
			SideB:  "Bob",
			Status: state.MatchStatusCompleted,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var result state.MatchResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Equal(t, "self-reported", result.ResultSource)
	})

	t.Run("anonymous PUT score with kiken-voluntary returns 400", func(t *testing.T) {
		r, _, compID := setupSelfRunScoreRouter(t, "secret")

		body, _ := json.Marshal(state.MatchResult{
			ID:         "PoolA-1",
			SideA:      "Alice",
			SideB:      "Bob",
			Winner:     "Alice",
			IpponsA:    []string{"M", "K"},
			Status:     state.MatchStatusCompleted,
			Decision:   "kiken-voluntary",
			DecisionBy: "shiro",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
		assert.Contains(t, w.Body.String(), "decision type not allowed")
	})

	t.Run("anonymous PUT score with fusenpai returns 400", func(t *testing.T) {
		r, _, compID := setupSelfRunScoreRouter(t, "secret")

		body, _ := json.Marshal(state.MatchResult{
			ID:         "PoolA-1",
			SideA:      "Alice",
			SideB:      "Bob",
			Winner:     "Alice",
			IpponsA:    []string{"M", "K"},
			Status:     state.MatchStatusCompleted,
			Decision:   "fusenpai",
			DecisionBy: "shiro",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
		assert.Contains(t, w.Body.String(), "decision type not allowed")
	})

	t.Run("anonymous overwrite of finalized result returns 409", func(t *testing.T) {
		r, store, compID := setupSelfRunScoreRouter(t, "secret")

		// First, establish a finalized result.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID:      "PoolA-1",
				SideA:   "Alice",
				SideB:   "Bob",
				Winner:  "Alice",
				Status:  state.MatchStatusCompleted,
				IpponsA: []string{"M", "K"},
			},
		}))

		// Anonymous attempt to overwrite.
		body, _ := json.Marshal(state.MatchResult{
			ID:      "PoolA-1",
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Bob",
			IpponsB: []string{"M", "K"},
			Status:  state.MatchStatusCompleted,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusConflict, w.Code, "body=%s", w.Body.String())
		assert.Contains(t, w.Body.String(), "result_finalized")
	})

	t.Run("admin PUT score with valid password returns 200 and admin source", func(t *testing.T) {
		r, store, compID := setupSelfRunScoreRouter(t, "secret")

		body, _ := json.Marshal(state.MatchResult{
			ID:      "PoolA-1",
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Alice",
			IpponsA: []string{"M", "K"},
			Status:  state.MatchStatusCompleted,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var result state.MatchResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Equal(t, "admin", result.ResultSource)

		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		assert.Equal(t, "admin", stored[0].ResultSource)
	})

	t.Run("admin can overwrite finalized result with valid password and correctionReason", func(t *testing.T) {
		r, store, compID := setupSelfRunScoreRouter(t, "secret")

		// First, establish a finalized result.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID:      "PoolA-1",
				SideA:   "Alice",
				SideB:   "Bob",
				Winner:  "Alice",
				Status:  state.MatchStatusCompleted,
				IpponsA: []string{"M", "K"},
			},
		}))

		// Admin overwrite with valid password and correctionReason succeeds.
		body, _ := json.Marshal(state.MatchResult{
			ID:               "PoolA-1",
			SideA:            "Alice",
			SideB:            "Bob",
			Winner:           "Bob",
			IpponsB:          []string{"M", "K"},
			Status:           state.MatchStatusCompleted,
			CorrectionReason: "Scoring error: scores were recorded in wrong columns",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var result state.MatchResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Equal(t, "admin", result.ResultSource)
	})

	t.Run("admin overwrite without correctionReason returns 400", func(t *testing.T) {
		r, store, compID := setupSelfRunScoreRouter(t, "secret")

		// Establish a finalized result.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID:      "PoolA-1",
				SideA:   "Alice",
				SideB:   "Bob",
				Winner:  "Alice",
				Status:  state.MatchStatusCompleted,
				IpponsA: []string{"M", "K"},
			},
		}))

		// Correction attempt without a reason must be rejected.
		body, _ := json.Marshal(state.MatchResult{
			ID:      "PoolA-1",
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Bob",
			IpponsB: []string{"M", "K"},
			Status:  state.MatchStatusCompleted,
			// CorrectionReason deliberately omitted.
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
		assert.Contains(t, w.Body.String(), "correctionReason")
	})

	t.Run("withdrawal decision cannot overwrite a finalized result without correctionReason", func(t *testing.T) {
		r, store, compID := setupSelfRunScoreRouter(t, "secret")

		// Establish a finalized result.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID:      "PoolA-1",
				SideA:   "Alice",
				SideB:   "Bob",
				Winner:  "Alice",
				Status:  state.MatchStatusCompleted,
				IpponsA: []string{"M", "K"},
			},
		}))

		// A withdrawal (kiken) overwrite, valid decision payload (decisionBy +
		// 2-0 scoreline + winner) but no correctionReason, must still be
		// rejected: the decision field must not bypass the correction gate.
		body, _ := json.Marshal(state.MatchResult{
			ID:         "PoolA-1",
			SideA:      "Alice",
			SideB:      "Bob",
			Winner:     "Bob", // shiro survives; aka withdrew
			Decision:   "kiken-voluntary",
			DecisionBy: "aka",
			IpponsB:    []string{"M", "K"},
			Status:     state.MatchStatusCompleted,
			// CorrectionReason deliberately omitted.
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
		assert.Contains(t, w.Body.String(), "correctionReason")
	})

	t.Run("first completion does not require correctionReason", func(t *testing.T) {
		r, _, compID := setupSelfRunScoreRouter(t, "secret")

		// The pre-seeded match has no status, completing it is a first
		// submission, not a correction, so no correctionReason is needed.
		body, _ := json.Marshal(state.MatchResult{
			ID:      "PoolA-1",
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Alice",
			IpponsA: []string{"M", "K"},
			Status:  state.MatchStatusCompleted,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	})

	t.Run("correctionReason on a first completion is dropped, not persisted", func(t *testing.T) {
		r, store, compID := setupSelfRunScoreRouter(t, "secret")

		// First completion (the pre-seeded match has no status) carrying a stray
		// correctionReason, it must NOT be persisted (the reason is only for a
		// completed→completed correction).
		body, _ := json.Marshal(state.MatchResult{
			ID:               "PoolA-1",
			SideA:            "Alice",
			SideB:            "Bob",
			Winner:           "Alice",
			IpponsA:          []string{"M", "K"},
			Status:           state.MatchStatusCompleted,
			CorrectionReason: "Scoring error: should be dropped on first completion",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		pms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		var found bool
		for _, m := range pms {
			if m.ID == "PoolA-1" {
				found = true
				assert.Empty(t, m.CorrectionReason, "correctionReason must not persist on a first completion")
			}
		}
		require.True(t, found, "PoolA-1 must be present after the write")
	})

	t.Run("correction with reason persists to storage", func(t *testing.T) {
		r, store, compID := setupSelfRunScoreRouter(t, "secret")

		// Establish a finalized result.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID:      "PoolA-1",
				SideA:   "Alice",
				SideB:   "Bob",
				Winner:  "Alice",
				Status:  state.MatchStatusCompleted,
				IpponsA: []string{"M", "K"},
			},
		}))

		wantReason := "Data entry: scores entered for wrong match"
		body, _ := json.Marshal(state.MatchResult{
			ID:               "PoolA-1",
			SideA:            "Alice",
			SideB:            "Bob",
			Winner:           "Bob",
			IpponsB:          []string{"M", "K"},
			Status:           state.MatchStatusCompleted,
			CorrectionReason: wantReason,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		// Verify the reason survived the write and reload.
		stored, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		assert.Equal(t, wantReason, stored[0].CorrectionReason)
	})

	t.Run("officiated mode PUT score sets resultSource admin", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "officiated-*")
		require.NoError(t, err)
		t.Cleanup(func() { os.RemoveAll(tempDir) })
		store, err := state.NewStore(tempDir)
		require.NoError(t, err)
		eng := engine.New(store)
		hub := NewHub()

		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name:     "Officiated",
			Password: "pw",
			Courts:   []string{"A"},
			Mode:     state.TournamentModeOfficiated,
		}))
		compID := "off1"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:     compID,
			Format: state.CompFormatMixed,
			Status: state.CompStatusPools,
		}))
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{Name: "Alice"}, {Name: "Bob"},
		}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "PoolA-1", SideA: "Alice", SideB: "Bob"},
		}))

		gin.SetMode(gin.TestMode)
		rr := gin.New()
		admin := rr.Group("/api")
		RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)

		body, _ := json.Marshal(state.MatchResult{
			ID:      "PoolA-1",
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Alice",
			IpponsA: []string{"M", "K"},
			Status:  state.MatchStatusCompleted,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		rr.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

		var result state.MatchResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		assert.Equal(t, "admin", result.ResultSource)
	})
}

// TestScoreHandler_FabricatedMid_TxNotFound verifies that a running-write for a
// fabricated (non-existent) match ID returns 404 AND prunes any runningRevStore
// entry that the request pre-populated, preventing unbounded growth from
// unauthenticated self-run callers.
//
// The test exercises the txErr NotFoundError path: a competition with courts is
// saved (so CheckCrossCompCourtBusy runs and looks up the match's court), but
// the target match is never saved, CheckCrossCompCourtBusy's court-lookup
// returns NotFoundError which propagates as txErr.
func TestScoreHandler_FabricatedMid_TxNotFound(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "fabricated-comp"
	fakeMid := "fake-mid-xyz"
	matchKey := compID + ":" + fakeMid

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Courts: []string{"A"}}))
	// Intentionally do NOT save a match with ID fakeMid.

	// Pre-seed the rev store to simulate the guard having already stored an
	// entry (ensures the Delete is exercised, not just an absence check).
	runningRevStore.Store(matchKey, runningRev{Session: "pre-seed", Rev: 1})

	epoch := time.Now().UnixMilli()
	payload, _ := json.Marshal(map[string]any{
		"sideA": "Alice", "sideB": "Bob",
		"ipponsA": []string{"M"}, "ipponsB": []string{},
		"status":     "running",
		"rev":        int64(2),
		"revSession": fmt.Sprintf("%d-x", epoch),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/"+fakeMid+"/score", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, "fabricated mid must return 404")
	_, present := runningRevStore.Load(matchKey)
	assert.False(t, present, "runningRevStore entry must be pruned on txErr NotFound")
}

// TestScoreHandler_RunningWriteCannotRevertCompleted verifies the bracket-
// integrity guard that prevents a stale running-status autosave from reverting
// an already-completed match result:
//
//  1. PUT a completed write (winner=Alice) → HTTP 200, match stored as completed.
//  2. PUT a running write (status=running, with a rev + revSession) for the
//     same match → expect HTTP 200 with body {"stale":true}.
//  3. The stored match status must STILL be "completed" (not reverted).
//  4. runningRevStore must have NO lingering entry for the match key.
func TestScoreHandler_RunningWriteCannotRevertCompleted(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "rv-revert"
	matchID := "PoolA-1"
	matchKey := compID + ":" + matchID

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: matchID, SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
	}))

	// Ensure the rev-store is clean for this key.
	runningRevStore.Delete(matchKey)

	// Step 1: finalize the match.
	completedPayload, _ := json.Marshal(map[string]any{
		"sideA":   "Alice",
		"sideB":   "Bob",
		"winner":  "Alice",
		"ipponsA": []string{"M", "K"},
		"ipponsB": []string{},
		"status":  "completed",
	})
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/"+matchID+"/score", bytes.NewBuffer(completedPayload))
	req1.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code, "completed write must succeed")

	// Verify the match is completed on disk.
	loadStatus := func() state.MatchStatus {
		t.Helper()
		ms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range ms {
			if m.ID == matchID {
				return m.Status
			}
		}
		t.Fatalf("match %s not found", matchID)
		return ""
	}
	assert.Equal(t, state.MatchStatusCompleted, loadStatus(), "match must be completed after step 1")

	// Step 2: attempt a stale running autosave (simulating a write queued
	// before Finish that was flushed after).
	epoch := time.Now().UnixMilli()
	runningPayload, _ := json.Marshal(map[string]any{
		"sideA":      "Alice",
		"sideB":      "Bob",
		"ipponsA":    []string{"M"},
		"ipponsB":    []string{},
		"status":     "running",
		"rev":        int64(1),
		"revSession": fmt.Sprintf("%d-x", epoch),
	})
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/"+matchID+"/score", bytes.NewBuffer(runningPayload))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	// Step 3: the server must return 200 with stale=true.
	assert.Equal(t, http.StatusOK, w2.Code, "stale running write must return 200")
	var body map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &body))
	stale, ok := body["stale"].(bool)
	assert.True(t, ok && stale, "stale running write must return {stale:true}")

	// Step 4: match must still be completed, not reverted to running.
	assert.Equal(t, state.MatchStatusCompleted, loadStatus(), "running write must not revert a completed match")

	// Step 5: no lingering runningRevStore entry.
	_, revPresent := runningRevStore.Load(matchKey)
	assert.False(t, revPresent, "runningRevStore must have no entry after stale running write on completed match")
}

// TestScoreHandler_ScheduledWriteCannotRevertCompleted verifies the bracket
// integrity guard: a "scheduled"-status write onto an already-completed match
// must be silently no-op'd (HTTP 200 with {"stale":true}) and must not mutate
// the stored completed result. This mirrors TestScoreHandler_RunningWriteCannotRevertCompleted
// but for the scheduled status variant.
func TestScoreHandler_ScheduledWriteCannotRevertCompleted(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "rv-sched-revert"
	matchID := "PoolA-1"
	matchKey := compID + ":" + matchID

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: matchID, SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
	}))

	// Ensure the rev-store is clean for this key.
	runningRevStore.Delete(matchKey)

	// Step 1: finalize the match.
	completedPayload, _ := json.Marshal(map[string]any{
		"sideA":   "Alice",
		"sideB":   "Bob",
		"winner":  "Alice",
		"ipponsA": []string{"M", "K"},
		"ipponsB": []string{},
		"status":  "completed",
	})
	w1 := httptest.NewRecorder()
	req1, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/"+matchID+"/score", bytes.NewBuffer(completedPayload))
	req1.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code, "completed write must succeed")

	// Verify the match is completed on disk.
	loadMatch := func() state.MatchResult {
		t.Helper()
		ms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range ms {
			if m.ID == matchID {
				return m
			}
		}
		t.Fatalf("match %s not found", matchID)
		return state.MatchResult{}
	}
	before := loadMatch()
	require.Equal(t, state.MatchStatusCompleted, before.Status, "match must be completed after step 1")
	require.Equal(t, "Alice", before.Winner, "winner must be set after step 1")

	// Step 2: attempt a scheduled-status write (simulating a stale requeue
	// that raced with match completion).
	scheduledPayload, _ := json.Marshal(map[string]any{
		"sideA":   "Alice",
		"sideB":   "Bob",
		"ipponsA": []string{},
		"ipponsB": []string{},
		"status":  "scheduled",
	})
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/"+matchID+"/score", bytes.NewBuffer(scheduledPayload))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	// Step 3: the server must return 200 with stale=true.
	assert.Equal(t, http.StatusOK, w2.Code, "stale scheduled write must return 200")
	var body map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &body))
	stale, ok := body["stale"].(bool)
	assert.True(t, ok && stale, "stale scheduled write must return {stale:true}")

	// Step 4: match must still be completed with the original winner intact.
	after := loadMatch()
	assert.Equal(t, state.MatchStatusCompleted, after.Status, "scheduled write must not revert a completed match")
	assert.Equal(t, "Alice", after.Winner, "winner must not be cleared by a stale scheduled write")
}

// TestSelfRunPolicy_NoRevMetadata verifies that an anonymous self-run write
// has its Rev and RevSession zeroed by enforceSelfRunPolicy before the
// rev-guard runs. This prevents a malicious participant from injecting a
// near-future epoch into runningRevStore and poisoning the guard so that
// every subsequent legitimate admin autosave looks "older" and gets dropped.
//
// Requires a non-empty tournament password so that a request with no
// X-Tournament-Password header is treated as an anonymous caller (not admin).
func TestSelfRunPolicy_NoRevMetadata(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Self-run tournament with a non-empty password. An unauthenticated
	// request (no X-Tournament-Password) is the anonymous self-run path.
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret", Mode: "self-run", Courts: []string{"A"}}))
	compID := "sr-epoch"
	matchID := "PoolA-1"
	matchKey := compID + ":" + matchID
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: matchID, SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
	}))
	runningRevStore.Delete(matchKey)

	// Attempt to inject a near-future epoch via an anonymous self-run running
	// write. If the policy does NOT zero Rev/RevSession, the rev-guard stores
	// this entry and subsequent admin autosaves from an older epoch get dropped.
	futureEpoch := time.Now().UnixMilli() + 60_000 // 60s in the future
	payload, _ := json.Marshal(map[string]any{
		"sideA":      "Alice",
		"sideB":      "Bob",
		"ipponsA":    []string{"M"},
		"ipponsB":    []string{},
		"status":     "running",
		"rev":        int64(9999),
		"revSession": fmt.Sprintf("%d-attacker", futureEpoch),
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/"+matchID+"/score", bytes.NewBuffer(payload))
	// Deliberately omit X-Tournament-Password to be the anonymous path.
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// The runningRevStore must NOT contain the attacker's epoch/rev.
	// If enforceSelfRunPolicy zeroed Rev and RevSession the guard ran with
	// Rev==0, which is the "unversioned" opt-out, no entry is stored.
	val, present := runningRevStore.Load(matchKey)
	if present {
		stored := val.(runningRev)
		assert.Equal(t, int64(0), stored.Rev, "self-run write must not populate runningRevStore with attacker rev")
		assert.Empty(t, stored.Session, "self-run write must not populate runningRevStore with attacker session")
	}
	// (If not present at all, the guard skipped storage, also correct.)
}
