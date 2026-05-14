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

	// Cross-file guard symmetry with handlers_import.go. The import path
	// already trims Kind / Format / PoolSizeMode / StartTime / Date. The
	// admin UI's POST/PUT path now trims them too (defense against
	// hand-crafted API requests sending padded values that would slip
	// past dropdowns / time / date pickers). Pin the contract on both
	// endpoints — drop one TrimSpace and a downstream switch on the
	// non-canonical value silently falls through.
	t.Run("All String Fields Trimmed On Create", func(t *testing.T) {
		comp := state.Competition{
			ID: "trim-fields-create", Name: "Trim Fields Create",
			Kind: "  individual  ", Format: "  pools  ",
			PoolSizeMode: "  min  ", StartTime: "  09:00  ", Date: "  2026-05-12  ",
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
		stored, err := store.LoadCompetition("trim-fields-create")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "individual", stored.Kind, "Kind should be trimmed on POST")
		assert.Equal(t, "pools", stored.Format, "Format should be trimmed on POST")
		assert.Equal(t, "min", stored.PoolSizeMode, "PoolSizeMode should be trimmed on POST")
		assert.Equal(t, "09:00", stored.StartTime, "StartTime should be trimmed on POST")
		assert.Equal(t, "2026-05-12", stored.Date, "Date should be trimmed on POST")
	})

	t.Run("All String Fields Trimmed On Update", func(t *testing.T) {
		seed := state.Competition{ID: "trim-fields-update", Name: "Trim Fields Update", Kind: "individual", Format: "pools"}
		require.NoError(t, store.SaveCompetition(&seed))

		update := state.Competition{
			ID: "trim-fields-update", Name: "Trim Fields Update",
			Kind: "  team  ", Format: "  playoffs  ",
			PoolSizeMode: "  exact  ", StartTime: "  10:30  ", Date: "  2026-06-15  ",
		}
		body, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/trim-fields-update", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		stored, err := store.LoadCompetition("trim-fields-update")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "team", stored.Kind, "Kind should be trimmed on PUT")
		assert.Equal(t, "playoffs", stored.Format, "Format should be trimmed on PUT")
		assert.Equal(t, "exact", stored.PoolSizeMode, "PoolSizeMode should be trimmed on PUT")
		assert.Equal(t, "10:30", stored.StartTime, "StartTime should be trimmed on PUT")
		assert.Equal(t, "2026-06-15", stored.Date, "Date should be trimmed on PUT")
	})

	// Cross-file guard symmetry with handlers_tournament.go: whitespace-only
	// Name must be rejected on both POST and PUT after trim. Without this,
	// a hand-crafted POST with `{id: "foo", name: "   "}` lands as
	// Name="" on disk (slugifyID is bypassed when ID is explicit, and
	// checkUniqueCompName("", ...) passes when no other empty-named
	// competition exists) — admin UI then shows a blank competition card.
	t.Run("Whitespace-Only Name Rejected On Create", func(t *testing.T) {
		comp := state.Competition{ID: "blank-name", Name: "   "}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code,
			"POST /competitions with whitespace-only Name must return 400")
		assert.Contains(t, w.Body.String(), "competition name is required",
			"rejection should explain the empty-name reason")
		// Confirm it didn't land on disk.
		stored, _ := store.LoadCompetition("blank-name")
		assert.Nil(t, stored, "blank-name competition should not have been persisted")
	})

	t.Run("Whitespace-Only Name Rejected On Update", func(t *testing.T) {
		seed := state.Competition{ID: "blank-name-update", Name: "Original"}
		require.NoError(t, store.SaveCompetition(&seed))

		update := state.Competition{ID: "blank-name-update", Name: "   "}
		body, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/blank-name-update", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code,
			"PUT /competitions/:id with whitespace-only Name must return 400")
		assert.Contains(t, w.Body.String(), "competition name is required",
			"rejection should explain the empty-name reason")
		// Confirm the persisted name is unchanged.
		stored, err := store.LoadCompetition("blank-name-update")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "Original", stored.Name,
			"PUT must not clobber Name when validation fails")
	})

	// Copilot round-4 finding on PR #104: POST /competitions with a
	// non-empty but invalid `id` (e.g. "../../etc/passwd", "foo bar",
	// "foo.bar") skipped the derive-from-name block, hit
	// LoadCompetition which silently dropped the validation error,
	// then SaveCompetitionChanged returned "invalid competition ID"
	// mapped to a 500. The fix validates `id` upfront with a 400
	// (same shape as requireValidCompID does for routes with :id
	// in the URL).
	t.Run("POST Rejects Invalid Body ID With 400", func(t *testing.T) {
		// Single-segment payloads that gin will deliver verbatim to the
		// handler (vs traversal payloads which the router may reject).
		// Same set as the Path_Traversal_IDs_Rejected single-segment
		// list — every one violates ValidateCompetitionID's char rule.
		invalidIDs := []string{
			"foo bar",
			"foo.bar",
			"foo+bar",
			"foo@bar",
			"_leading-underscore",
			"-leading-dash",
		}
		for _, badID := range invalidIDs {
			comp := state.Competition{ID: badID, Name: "Invalid ID Test"}
			body, _ := json.Marshal(comp)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"POST with id=%q must return 400 (got %d: %s)", badID, w.Code, w.Body.String())
			// Confirm no half-baked record landed on disk under the
			// invalid ID (the validation must fail before SaveCompetition).
			stored, _ := store.LoadCompetition(badID)
			assert.Nil(t, stored, "POST with id=%q must not persist", badID)
		}
	})

	// Path-traversal guard. ValidateCompetitionID was only called at 2 of
	// the 14 :id handler sites pre-fix; the requireValidCompID helper now
	// gates every site. A compID like "../../../etc/passwd" would
	// otherwise reach compPath(id, ...) which does filepath.Clean(Join())
	// and cleanly escapes the data dir. Sample a handful of routes
	// (GET / PUT / DELETE / nested) — the helper centralises the logic,
	// so testing every route is redundant.
	t.Run("Path Traversal IDs Rejected", func(t *testing.T) {
		// Per ValidateCompetitionID (regex ^[a-zA-Z0-9][a-zA-Z0-9_-]*$):
		// empty, > 64 chars, any character outside [a-zA-Z0-9_-], or a
		// non-alphanumeric leading character is rejected (so "_foo" and
		// "-foo" are invalid even though "_" / "-" are allowed elsewhere).
		// Two classes of bad IDs:
		//
		//   1. Multi-segment / path-traversal payloads (contain "/" or
		//      URL-encoded "/"). These may match no route at the gin
		//      level — handy as a smoke test that NOTHING returns 200,
		//      but they don't prove requireValidCompID itself ran.
		//
		//   2. Single-segment IDs containing characters outside
		//      [A-Za-z0-9_-] (".", " ", "%2e"). These DO reach the
		//      handler, so the helper is the only thing standing
		//      between them and a 200 — perfect for asserting 400.
		//
		// Mix both: traversal payloads sweep for "no 200 ever";
		// single-segment payloads assert the precise 400 from the helper.
		traversalIDs := []string{
			"../../../etc/passwd",
			"..%2F..%2Fetc%2Fpasswd",
			"foo/bar",
		}
		// Single-segment IDs that reach the handler. Gin treats these
		// as one :id value; ValidateCompetitionID rejects each on the
		// invalid-character rule.
		singleSegmentIDs := []string{
			"foo bar",   // space
			"foo.bar",   // period
			"foo%2ebar", // URL-encoded period, gin decodes before match
			"foo+bar",   // plus
			"foo@bar",   // at-sign
		}
		// Representative endpoints across the affected handler set —
		// competition, participants/seeds, AND at least one match route
		// (handlers_match.go also uses requireValidCompID; without a
		// match-route case here, a regression there would slip past).
		// The override-rank route mounts at
		// /competitions/:id/pools/:poolId/override-rank; the test path
		// must include /pools/main/ or gin returns 404 before the
		// handler runs.
		routes := []struct {
			method string
			path   string
		}{
			{"GET", "/api/competitions/%s"},
			{"PUT", "/api/competitions/%s"},
			{"GET", "/api/competitions/%s/reserved-slots"},
			{"POST", "/api/competitions/%s/start"},
			{"GET", "/api/competitions/%s/export"},
			{"PUT", "/api/competitions/%s/pools/main/override-rank"},
			{"DELETE", "/api/competitions/%s/overrides"},
			{"GET", "/api/competitions/%s/participants"},
			{"POST", "/api/competitions/%s/participants"},
			{"GET", "/api/competitions/%s/seeds"},
			{"PUT", "/api/competitions/%s/seeds"},
			// Match endpoints from handlers_match.go. Without a match
			// route in this set, a regression that drops requireValidCompID
			// from match.go would still ship green.
			{"POST", "/api/competitions/%s/matches/bulk-score"},
			{"PUT", "/api/competitions/%s/matches/m1/score"},
			{"PUT", "/api/competitions/%s/matches/m1/court"},
		}
		// Sweep 1: traversal payloads must NEVER return 200, regardless
		// of whether they reach the handler or 404 at the router.
		for _, badID := range traversalIDs {
			for _, route := range routes {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest(route.method, fmt.Sprintf(route.path, badID), nil)
				if route.method == "PUT" || route.method == "POST" {
					req.Body = nil
					req.Header.Set("Content-Type", "application/json")
				}
				r.ServeHTTP(w, req)
				assert.NotEqual(t, http.StatusOK, w.Code,
					"%s %s with id=%q must not return 200", route.method, route.path, badID)
			}
		}
		// Sweep 2: single-segment payloads reach the handler. The helper
		// must produce a 400. A 404 here would mean either the route
		// shape is wrong (router miss) OR the handler skipped
		// requireValidCompID and downstream code 404'd on the bad id —
		// both regressions.
		//
		// Asserting only the status code is vacuous for PUT/POST routes
		// that bind JSON after the ID check: dropping requireValidCompID
		// from such a handler would still return 400 (from ShouldBindJSON
		// on the empty body). To prove the helper itself ran, also
		// require the response body to mention "competition ID" — the
		// substring is unique to ValidateCompetitionID's error message
		// ("competition ID contains invalid characters (allowed: ...)").
		// ShouldBindJSON's empty-body error looks like
		// "invalid request" / "EOF" / "unexpected end of JSON input",
		// none of which contain that substring, so a regression that
		// drops the helper would fail this assertion.
		for _, badID := range singleSegmentIDs {
			for _, route := range routes {
				w := httptest.NewRecorder()
				req, _ := http.NewRequest(route.method, fmt.Sprintf(route.path, badID), nil)
				if route.method == "PUT" || route.method == "POST" {
					req.Body = nil
					req.Header.Set("Content-Type", "application/json")
				}
				r.ServeHTTP(w, req)
				assert.Equal(t, http.StatusBadRequest, w.Code,
					"%s %s with id=%q must return 400 from requireValidCompID, got %d",
					route.method, route.path, badID, w.Code)
				assert.Contains(t, w.Body.String(), "competition ID",
					"%s %s with id=%q must return ValidateCompetitionID's error message, got %q",
					route.method, route.path, badID, w.Body.String())
			}
		}
	})
}
