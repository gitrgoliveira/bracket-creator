package mobileapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

	// Sibling of the rank-override TrimSpace test in
	// handlers_competition_test.go. Downstream bracket math compares
	// m.Winner to roster names by exact string equality, so padded
	// "  Foo  " won't match canonical "Foo" — pin the trim contract.
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
		// Whitespace-only winnerName trims to empty — same shape as the
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
func TestScoreHandler_CompletionBroadcastContract(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	comp := state.Competition{
		ID:     "pools1",
		Format: state.CompFormatPools,
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

	// Partial completion — no EventCompetitionCompleted
	scoreMatch("PoolA-1", "P1")
	partial := drainHubEvents(t, ch, 30*time.Millisecond)
	assert.Equal(t, 0, countCompletedEvents(t, partial, "pools1"),
		"partial completion must not broadcast competition_completed")

	// Final match — EventCompetitionCompleted must be emitted exactly once
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
func TestBulkScoreHandler_CompletionBroadcastContract(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "bulk1", Format: state.CompFormatPools, Status: state.CompStatusPools,
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

	// Partial bulk completion (1 of 3) — no competition_completed
	bulkScore([]state.MatchResult{
		{ID: "PoolA-1", Winner: "P1", Status: state.MatchStatusCompleted},
	})
	partial := drainHubEvents(t, ch, 30*time.Millisecond)
	assert.Equal(t, 0, countCompletedEvents(t, partial, "bulk1"),
		"partial bulk completion must not broadcast competition_completed")

	// Final batch closes out the comp — exactly one competition_completed
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
func TestQuickScoreHandler_CompletionBroadcastContract(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "qs1", Format: state.CompFormatPools, Status: state.CompStatusPools, TeamSize: 3,
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

	// First match — no completion
	quickScore("PoolA-1", "TeamA", "TeamB", 2, 1, 0)
	partial := drainHubEvents(t, ch, 30*time.Millisecond)
	assert.Equal(t, 0, countCompletedEvents(t, partial, "qs1"),
		"partial quick-score must not broadcast competition_completed")

	// Final match — exactly one completion
	quickScore("PoolA-2", "TeamA", "TeamC", 2, 1, 0)
	final := drainHubEvents(t, ch, 100*time.Millisecond)
	assert.Equal(t, 1, countCompletedEvents(t, final, "qs1"),
		"final quick-score must emit competition_completed exactly once")
}

// TestTryAutoCompletePools_SanitizesErrorHeader locks in the contract that
// when MaybeAutoCompletePools fails, the response carries the generic
// AutoCompleteErrorValue sentinel — never the raw error string, which can
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

// TestPostScoreKikenAutoFillsRegulation — T086: POST /score with
// decision=kiken, decisionBy=shiro, encho=null and a 0-2 scoreline
// returns 200 and the persisted match round-trips the decision metadata.
//
// FR-031, contracts/match-decisions.md.
func TestPostScoreKikenAutoFillsRegulation(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "kiken-reg"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatPools, Status: state.CompStatusPools,
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
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "kiken", stored[0].Decision)
	assert.Equal(t, "shiro", stored[0].DecisionBy)
	assert.Equal(t, "Alice", stored[0].Winner)
}

// TestPostScoreKikenInEncho — T087: POST /score with
// decision=kiken, decisionBy=shiro, encho.periodCount=1 and a 0-1
// scoreline returns 200.
func TestPostScoreKikenInEncho(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "kiken-encho"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatPools, Status: state.CompStatusPools,
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
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	stored, _ := store.LoadPoolMatches(compID)
	require.Len(t, stored, 1)
	require.NotNil(t, stored[0].Encho)
	assert.Equal(t, 1, stored[0].Encho.PeriodCount)
}

// TestPostScoreKikenInvalidScoreline — T088: POST /score with
// decision=kiken, encho=null, and a 0-1 scoreline (regulation requires
// 2-0) returns 400 with the validator's field message.
func TestPostScoreKikenInvalidScoreline(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "kiken-bad"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatPools, Status: state.CompStatusPools,
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
		ID: compID, Format: state.CompFormatPools, Status: state.CompStatusPools,
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
		// keeps the real store but never gets called — enforceEnchoCap
		// aborts the request first.
		registerScoreHandler(admin, eng, failingCompetitionStore{err: errors.New("disk on fire")}, realStore, hub)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(score(1)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusInternalServerError, w.Code, "body=%s", w.Body.String())
		assert.Contains(t, w.Body.String(), "failed to validate encho limits")
	})

	t.Run("over cap returns 400 with limit", func(t *testing.T) {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		admin := r.Group("/api")
		registerScoreHandler(admin, eng, realStore, realStore, hub)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score", bytes.NewBuffer(score(3)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
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
		registerScoreHandler(admin, eng, realStore, realStore, hub)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/PoolA-1/score?force=true", bytes.NewBuffer(score(3)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	})
}

// TestBulkScore_FailsClosedOnLoadError — when the cap-check load
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
	RegisterMatchHandlers(admin, eng, failingCompetitionStore{err: errors.New("disk on fire")}, realStore, hub)

	body, _ := json.Marshal([]state.MatchResult{
		{ID: "PoolA-1", SideA: "P1", SideB: "P2", Winner: "P1", Status: state.MatchStatusCompleted},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/c1/matches/bulk-score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code, "body=%s", w.Body.String())
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
