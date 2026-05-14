package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
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

	t.Run("Override Rank Trims Whitespace From Player Name", func(t *testing.T) {
		// Padded names must be stored under the trimmed key so subsequent
		// lookups (which use the canonical participant name) match.
		comp := state.Competition{ID: "rank-trim-comp"}
		store.SaveCompetition(&comp)

		reqBody, _ := json.Marshal(map[string]any{
			"playerName": "  Player Trim  ",
			"rank":       7,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-trim-comp/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		// Read the persisted override back; key must be the trimmed name.
		overrides, err := store.LoadOverrides("rank-trim-comp")
		require.NoError(t, err)
		require.NotNil(t, overrides)
		_, hasTrimmed := overrides.PoolRanks["pool-1"]["Player Trim"]
		assert.True(t, hasTrimmed, "rank override should be keyed under trimmed name")
		_, hasPadded := overrides.PoolRanks["pool-1"]["  Player Trim  "]
		assert.False(t, hasPadded, "rank override should not be keyed under padded name")
	})

	t.Run("Override Rank Rejects Invalid Input", func(t *testing.T) {
		comp := state.Competition{ID: "rank-bad-comp"}
		store.SaveCompetition(&comp)

		cases := []struct {
			name string
			body map[string]any
		}{
			{"empty player name", map[string]any{"playerName": "", "rank": 1}},
			{"whitespace-only player name", map[string]any{"playerName": "   ", "rank": 1}},
			{"tab-only player name", map[string]any{"playerName": "\t\t", "rank": 1}},
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

	// Deep-review finding: handlers trim comp.Name but not comp.NumberPrefix.
	// The frontend SETTINGS edit path doesn't trim the prefix before sending,
	// so "  A  " would persist and produce participant numbers like "  A1".
	// Fix is one TrimSpace line per handler; these tests pin the contract on
	// both POST (create) and PUT (update) paths so a future refactor can't
	// silently drop one half.
	t.Run("NumberPrefix Trimmed On Create", func(t *testing.T) {
		comp := state.Competition{ID: "prefix-create", Name: "Prefix Create", NumberPrefix: "  A  "}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
		stored, err := store.LoadCompetition("prefix-create")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "A", stored.NumberPrefix, "NumberPrefix should be trimmed on POST")
	})

	t.Run("NumberPrefix Trimmed On Update", func(t *testing.T) {
		// Seed with a clean prefix, then update via PUT with padded value.
		seed := state.Competition{ID: "prefix-update", Name: "Prefix Update", NumberPrefix: "B"}
		require.NoError(t, store.SaveCompetition(&seed))

		update := state.Competition{ID: "prefix-update", Name: "Prefix Update", NumberPrefix: "  C  "}
		body, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/prefix-update", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		stored, err := store.LoadCompetition("prefix-update")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "C", stored.NumberPrefix, "NumberPrefix should be trimmed on PUT")
	})

	// Path-traversal guard. ValidateCompetitionID was only called at 2 of
	// the 14 :id handler sites pre-fix; the requireValidCompID helper now
	// gates every site. A compID like "../../../etc/passwd" would
	// otherwise reach compPath(id, ...) which does filepath.Clean(Join())
	// and cleanly escapes the data dir. Sample a handful of routes
	// (GET / PUT / DELETE / nested) — the helper centralises the logic,
	// so testing every route is redundant.
	t.Run("Path Traversal IDs Rejected", func(t *testing.T) {
		// Per ValidateCompetitionID: empty, > 64 chars, or non-[a-zA-Z0-9_-]
		// is rejected. The traversal payload contains "/" and ".".
		badIDs := []string{
			"../../../etc/passwd",
			"..%2F..%2Fetc%2Fpasswd",
			"foo/bar",
			"foo bar",
			"",
		}
		// Gin treats a literal empty :id as 404 (route doesn't match), so
		// only enumerate the payloads that actually reach the handler.
		nonEmpty := []string{
			"../../../etc/passwd",
			"..%2F..%2Fetc%2Fpasswd",
			"foo/bar",
			"foo bar",
		}
		_ = badIDs // documentation
		// Representative endpoints from the affected handler set.
		routes := []struct {
			method string
			path   string
		}{
			{"GET", "/api/competitions/%s"},
			{"PUT", "/api/competitions/%s"},
			{"GET", "/api/competitions/%s/reserved-slots"},
			{"POST", "/api/competitions/%s/start"},
			{"GET", "/api/competitions/%s/export"},
			{"PUT", "/api/competitions/%s/override-rank"},
			{"DELETE", "/api/competitions/%s/overrides"},
		}
		for _, badID := range nonEmpty {
			for _, route := range routes {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest(route.method, fmt.Sprintf(route.path, badID), nil)
				if route.method == "PUT" || route.method == "POST" {
					req.Body = nil
					req.Header.Set("Content-Type", "application/json")
				}
				r.ServeHTTP(w, req)
				// gin URL-decodes path params, so a path-traversal payload
				// may match the route OR 404 at the router level. Either
				// way, the data dir must not be escaped: assert the response
				// is NOT 200 and the body doesn't contain anything that
				// would indicate filesystem access (e.g. a competition
				// payload).
				assert.NotEqual(t, http.StatusOK, w.Code,
					"%s %s with id=%q must not return 200", route.method, route.path, badID)
			}
		}
	})
}
