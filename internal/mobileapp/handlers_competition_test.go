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
	"github.com/stretchr/testify/require"
)

func TestCompetitionHandlers_Extended(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("Create Competition with Players and Seeds", func(t *testing.T) {
		comp := state.Competition{
			ID:   "seeded-comp",
			Name: "Seeded Competition",
			Players: []helper.Player{
				{Name: "Seed 1", Seed: 1, Dojo: "Dojo A"},
				{Name: "Seed 2", Seed: 2, Dojo: "Dojo B"},
				{Name: "No Seed", Seed: 0, Dojo: "Dojo C"},
			},
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		// Verify seeds were saved
		seeds, err := store.LoadSeeds("seeded-comp")
		assert.NoError(t, err)
		assert.Len(t, seeds, 2)
	})

	t.Run("Delete Competition", func(t *testing.T) {
		// 1. Success: setup status
		comp := state.Competition{ID: "delete-setup", Status: "setup"}
		store.SaveCompetition(&comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/competitions/delete-setup", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// 2. Success: pending status (new fix)
		comp2 := state.Competition{ID: "delete-pending", Status: "pending"}
		store.SaveCompetition(&comp2)
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("DELETE", "/api/competitions/delete-pending", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// 3. Reject: pools status (in progress) — must be invalidated first.
		comp3 := state.Competition{ID: "delete-started", Status: state.CompStatusPools}
		store.SaveCompetition(&comp3)
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("DELETE", "/api/competitions/delete-started", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)
		assert.Contains(t, w.Body.String(), "in progress")

		// 4. Invalidate the started competition, then deletion succeeds.
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("POST", "/api/competitions/delete-started/invalidate", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		w = httptest.NewRecorder()
		req, _ = http.NewRequest("DELETE", "/api/competitions/delete-started", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// 5. Invalidate rejects a competition that hasn't started.
		comp4 := state.Competition{ID: "invalidate-setup", Status: state.CompStatusSetup}
		store.SaveCompetition(&comp4)
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("POST", "/api/competitions/invalidate-setup/invalidate", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Reserved Slots", func(t *testing.T) {
		comp := state.Competition{ID: "target-comp", Status: "setup"}
		store.SaveCompetition(&comp)
		store.SaveCompetition(&state.Competition{ID: "source-comp"})

		// POST /api/competitions/:id/reserved-slots
		reqBody, _ := json.Marshal(map[string]any{
			"sourceCompID": "source-comp",
			"sourceRank":   1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/target-comp/reserved-slots", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		// GET /api/competitions/:id/reserved-slots
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("GET", "/api/competitions/target-comp/reserved-slots", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var slots []state.ReservedSlot
		json.Unmarshal(w.Body.Bytes(), &slots)
		require.Len(t, slots, 1)

		// DELETE /api/competitions/:id/reserved-slots/:slotID
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("DELETE", "/api/competitions/target-comp/reserved-slots/"+slots[0].ID, nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("Override Rank", func(t *testing.T) {
		comp := state.Competition{ID: "rank-comp"}
		store.SaveCompetition(&comp)

		reqBody, _ := json.Marshal(map[string]any{
			"playerName": "Player 1",
			"rank":       1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-comp/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Override Rank Rejects Invalid Input", func(t *testing.T) {
		comp := state.Competition{ID: "rank-bad-comp"}
		store.SaveCompetition(&comp)

		cases := []struct {
			name string
			body map[string]any
		}{
			{"empty player name", map[string]any{"playerName": "", "rank": 1}},
			{"zero rank", map[string]any{"playerName": "Player 1", "rank": 0}},
			{"negative rank", map[string]any{"playerName": "Player 1", "rank": -3}},
			{"absurdly large rank", map[string]any{"playerName": "Player 1", "rank": 99999}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reqBody, _ := json.Marshal(tc.body)
				w := httptest.NewRecorder()
				req, _ := http.NewRequest("PUT", "/api/competitions/rank-bad-comp/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				assert.Equal(t, http.StatusBadRequest, w.Code)
			})
		}
	})

	t.Run("Save Schedule", func(t *testing.T) {
		comp := state.Competition{ID: "sched-comp"}
		store.SaveCompetition(&comp)

		entries := []state.ScheduleEntry{{MatchRef: "m1", Court: "A"}}
		reqBody, _ := json.Marshal(entries)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/sched-comp/schedule", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Reset Overrides", func(t *testing.T) {
		comp := state.Competition{ID: "reset-comp"}
		store.SaveCompetition(&comp)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/competitions/reset-comp/overrides", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("Unique Competition Names", func(t *testing.T) {
		// 1. Create original
		comp1 := state.Competition{ID: "original", Name: "Kendo Cup"}
		body, _ := json.Marshal(comp1)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		// 2. Create duplicate (case insensitive)
		comp2 := state.Competition{ID: "duplicate", Name: "kendo cup"}
		body, _ = json.Marshal(comp2)
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "already exists")

		// 3. Create another
		comp3 := state.Competition{ID: "another", Name: "Other Cup"}
		body, _ = json.Marshal(comp3)
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)

		// 4. Update to duplicate name
		comp3.Name = "KENDO CUP"
		body, _ = json.Marshal(comp3)
		w = httptest.NewRecorder()
		req, _ = http.NewRequest("PUT", "/api/competitions/another", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "already exists")
	})
}
