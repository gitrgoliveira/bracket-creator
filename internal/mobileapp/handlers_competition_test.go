package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
			Players: []domain.Player{
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

		// 2. Reject: pools status (in progress) — must be invalidated first.
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
		// Seed a pool so the new pool-size validation can find pool-1
		// (rank within a pool is bounded by len(pool.Players)).
		require.NoError(t, store.SavePools("rank-comp", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{Name: "Player 1"}, {Name: "Player 2"}, {Name: "Player 3"},
			}},
		}))

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
		// Seed a pool with at least 7 players (rank=7 below).
		players := make([]helper.Player, 8)
		for i := range players {
			players[i] = helper.Player{Name: fmt.Sprintf("Player %d", i+1)}
		}
		require.NoError(t, store.SavePools("rank-trim-comp", []helper.Pool{
			{PoolName: "pool-1", Players: players},
		}))

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
		// Seed a pool so cases that pass the rank cap checks reach the
		// pool-size validation (rank=99999 fails earlier at the absolute
		// MaxRankOverride cap; rank=4-against-3-player-pool fails the
		// pool-size check).
		require.NoError(t, store.SavePools("rank-bad-comp", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{Name: "Player 1"}, {Name: "Player 2"}, {Name: "Player 3"},
			}},
		}))

		cases := []struct {
			name string
			body map[string]any
		}{
			{"empty player name", map[string]any{"playerName": "", "rank": 1}},
			{"whitespace-only player name", map[string]any{"playerName": "   ", "rank": 1}},
			{"tab-only player name", map[string]any{"playerName": "\t\t", "rank": 1}},
			{"zero rank", map[string]any{"playerName": "Player 1", "rank": 0}},
			{"negative rank", map[string]any{"playerName": "Player 1", "rank": -3}},
			{"absurdly large rank (over MaxRankOverride)", map[string]any{"playerName": "Player 1", "rank": 99999}},
			{"rank exceeds pool size", map[string]any{"playerName": "Player 1", "rank": 4}},
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

	t.Run("Override Rank Rejects Unknown Pool With 404", func(t *testing.T) {
		// Pool-size validation requires looking up the pool by name.
		// A bogus :poolId (no matching Pool.PoolName) returns 404.
		// The JS frontend only offers existing pools; this is a
		// defense-in-depth check against hand-crafted API callers.
		comp := state.Competition{ID: "rank-unknown-pool"}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-unknown-pool", []helper.Pool{
			{PoolName: "pool-a", Players: []helper.Player{{Name: "P1"}}},
		}))

		reqBody, _ := json.Marshal(map[string]any{"playerName": "P1", "rank": 1})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-unknown-pool/pools/pool-z/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code,
			"override-rank against an unknown pool name must 404")
		assert.Contains(t, w.Body.String(), "pool",
			"error message should identify the missing pool")
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
			PoolSizeMode: "  min  ", StartTime: "  09:00  ", Date: "  12-05-2026  ",
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
		assert.Equal(t, "12-05-2026", stored.Date, "Date should be trimmed on POST")
	})

	t.Run("All String Fields Trimmed On Update", func(t *testing.T) {
		seed := state.Competition{ID: "trim-fields-update", Name: "Trim Fields Update", Kind: "individual", Format: "pools"}
		require.NoError(t, store.SaveCompetition(&seed))

		update := state.Competition{
			ID: "trim-fields-update", Name: "Trim Fields Update",
			Kind: "  team  ", Format: "  playoffs  ",
			PoolSizeMode: "  exact  ", StartTime: "  10:30  ", Date: "  15-06-2026  ",
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
		assert.Equal(t, "15-06-2026", stored.Date, "Date should be trimmed on PUT")
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

	// Date must be in DD-MM-YYYY canonical format (frontend converts the
	// HTML date picker's ISO output before sending; direct API callers
	// must send DMY). Reject ISO YYYY-MM-DD shape and semantically
	// invalid days (Feb 31 etc.) on both POST and PUT.
	t.Run("Non-DMY Date Rejected On Create And Update", func(t *testing.T) {
		// Seed an existing comp for the PUT case.
		seed := state.Competition{ID: "date-fmt-test", Name: "Date Fmt Test", Date: "01-01-2026"}
		require.NoError(t, store.SaveCompetition(&seed))

		badDates := []string{
			"2026-05-12", // ISO shape — not accepted
			"31-02-2026", // Feb 31 semantically invalid
			"32-01-2026", // day 32 invalid
			"12-13-2026", // month 13 invalid
			"not a date",
		}
		for _, badDate := range badDates {
			// POST
			post := state.Competition{ID: "date-post-" + badDate[0:2], Name: "Date Post " + badDate[0:2], Date: badDate}
			body, _ := json.Marshal(post)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"POST /competitions with Date=%q must return 400", badDate)
			assert.Contains(t, w.Body.String(), "date must be DD-MM-YYYY")

			// PUT — body Date is bad; comp must still exist with the seeded date.
			put := state.Competition{ID: "date-fmt-test", Name: "Date Fmt Test", Date: badDate}
			body, _ = json.Marshal(put)
			w = httptest.NewRecorder()
			req, _ = http.NewRequest("PUT", "/api/competitions/date-fmt-test", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"PUT /competitions/date-fmt-test with Date=%q must return 400", badDate)
		}
		// Confirm seed wasn't clobbered.
		stored, _ := store.LoadCompetition("date-fmt-test")
		require.NotNil(t, stored)
		assert.Equal(t, "01-01-2026", stored.Date, "seed date untouched by failed PUTs")
	})

	// validateDateDMY must reject years outside minDateYear..maxDateYear
	// (mirroring JS MIN_YEAR/MAX_YEAR). Without matching server bounds,
	// a direct API call landing e.g. "01-01-1800" on a competition would
	// block every subsequent admin Settings save — saveLater re-validates
	// the stored date on every PUT.
	t.Run("Year Out Of Range Rejected On Create And Update", func(t *testing.T) {
		seed := state.Competition{ID: "year-range-test", Name: "Year Range Test", Date: "01-01-2026"}
		require.NoError(t, store.SaveCompetition(&seed))

		outOfRange := []string{"01-01-1800", "31-12-1899", "01-01-2101", "01-01-3000"}
		for _, badDate := range outOfRange {
			post := state.Competition{ID: "year-post-" + badDate[6:10], Name: "Year Post " + badDate[6:10], Date: badDate}
			body, _ := json.Marshal(post)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"POST /competitions with Date=%q must return 400 (year out of range)", badDate)
			assert.Contains(t, w.Body.String(), "date year must be between")

			put := state.Competition{ID: "year-range-test", Name: "Year Range Test", Date: badDate}
			body, _ = json.Marshal(put)
			w = httptest.NewRecorder()
			req, _ = http.NewRequest("PUT", "/api/competitions/year-range-test", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"PUT /competitions/year-range-test with Date=%q must return 400 (year out of range)", badDate)
		}
		stored, _ := store.LoadCompetition("year-range-test")
		require.NotNil(t, stored)
		assert.Equal(t, "01-01-2026", stored.Date, "seed date untouched by failed year-range PUTs")
	})

	// validateCompetitionCourts must reject duplicate court labels.
	// The frontend keys per-court rendering and `byCourt[m.court]`
	// bucketing on the label string — duplicates collapse two courts'
	// matches into one lane and trigger React duplicate-key warnings.
	t.Run("Duplicate Court Labels Rejected On Create And Update", func(t *testing.T) {
		seed := state.Competition{ID: "dup-courts-test", Name: "Dup Courts Test", Date: "01-01-2026", Courts: []string{"A", "B"}}
		require.NoError(t, store.SaveCompetition(&seed))

		dupCases := [][]string{{"A", "A"}, {"A", "B", "A"}, {"C", "C", "C"}}
		for i, dupCourts := range dupCases {
			post := state.Competition{ID: fmt.Sprintf("dup-courts-post-%d", i), Name: fmt.Sprintf("Dup Courts Post %d", i), Date: "01-01-2026", Courts: dupCourts}
			body, _ := json.Marshal(post)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"POST /competitions with Courts=%v must return 400 (duplicate labels)", dupCourts)
			assert.Contains(t, w.Body.String(), "duplicate court label")

			put := state.Competition{ID: "dup-courts-test", Name: "Dup Courts Test", Date: "01-01-2026", Courts: dupCourts}
			body, _ = json.Marshal(put)
			w = httptest.NewRecorder()
			req, _ = http.NewRequest("PUT", "/api/competitions/dup-courts-test", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"PUT /competitions/dup-courts-test with Courts=%v must return 400 (duplicate labels)", dupCourts)
		}
		stored, _ := store.LoadCompetition("dup-courts-test")
		require.NotNil(t, stored)
		assert.Equal(t, []string{"A", "B"}, stored.Courts, "seed courts untouched by failed duplicate-label PUTs")
	})

	// Copilot round-15 finding: validateCourtLabels accepted single-
	// whitespace labels because `label == ""` is false and
	// `len([]rune(" ")) == 1`. Such a label persists to disk and becomes
	// a React `key={cc}` value, schedule `byCourt[m.court]` bucket key,
	// and filter dropdown value — visually blank but structurally
	// distinct from "". Each whitespace shape (space, tab, NBSP) needs
	// rejection.
	t.Run("Whitespace-Only Court Labels Rejected On Create And Update", func(t *testing.T) {
		seed := state.Competition{ID: "ws-courts-test", Name: "WS Courts Test", Date: "01-01-2026", Courts: []string{"A", "B"}}
		require.NoError(t, store.SaveCompetition(&seed))

		wsCases := [][]string{
			{" "},      // single ASCII space
			{"\t"},     // tab
			{" "},      // non-breaking space (still single rune)
			{"A", " "}, // mixed: valid + whitespace-only
			{"　"},      // ideographic space
		}
		for i, wsCourts := range wsCases {
			post := state.Competition{ID: fmt.Sprintf("ws-courts-post-%d", i), Name: fmt.Sprintf("WS Courts Post %d", i), Date: "01-01-2026", Courts: wsCourts}
			body, _ := json.Marshal(post)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"POST /competitions with Courts=%v must return 400 (whitespace-only label)", wsCourts)
			assert.Contains(t, w.Body.String(), "whitespace-only")

			put := state.Competition{ID: "ws-courts-test", Name: "WS Courts Test", Date: "01-01-2026", Courts: wsCourts}
			body, _ = json.Marshal(put)
			w = httptest.NewRecorder()
			req, _ = http.NewRequest("PUT", "/api/competitions/ws-courts-test", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"PUT /competitions/ws-courts-test with Courts=%v must return 400 (whitespace-only label)", wsCourts)
		}
		stored, _ := store.LoadCompetition("ws-courts-test")
		require.NotNil(t, stored)
		assert.Equal(t, []string{"A", "B"}, stored.Courts, "seed courts untouched by failed whitespace-label PUTs")
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

	// PUT contract: distinguish omitted Players (settings-only PUT) from
	// explicit empty Players (clear roster). Pre-fix the handler keyed
	// the participants save on `len(comp.Players) > 0`, which collapsed
	// both into "skip save" — so the AdminParticipants "clear roster"
	// flow showed "Saved 0 participants" while the prior roster stayed
	// on disk. Post-fix the gate is `comp.Players != nil`: omitted is
	// nil → skip, explicit [] is non-nil empty → save empty CSV.
	t.Run("PUT Empty Players Clears Roster", func(t *testing.T) {
		const cid = "empty-players-clear"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:                cid,
			Name:              "Empty Players Source",
			HasParticipantIDs: true,
		}))
		require.NoError(t, store.SaveParticipants(cid, []domain.Player{
			{Name: "Alice", Dojo: "Dojo A"},
			{Name: "Bob", Dojo: "Dojo B"},
		}))
		// Confirm the roster is on disk before the clear.
		prior, err := store.LoadParticipants(cid, false)
		require.NoError(t, err)
		require.Len(t, prior, 2, "preconditions: roster must be populated before clear")

		// PUT with `players: []` (explicit empty, NOT omitted). Use
		// json.RawMessage to force the field to render rather than
		// relying on the encoder dropping nil slices.
		clearBody := []byte(`{"id":"empty-players-clear","name":"Empty Players Source","players":[]}`)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(clearBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "PUT with players=[] must succeed: %s", w.Body.String())

		// Verify the roster on disk is now empty.
		after, err := store.LoadParticipants(cid, false)
		require.NoError(t, err)
		assert.Len(t, after, 0, "PUT with explicit empty Players must clear the roster")
	})

	// Symmetric to the test above: PUT with the Players field OMITTED
	// (AdminSettings.saveNow's allowlist) must NOT touch participants.csv.
	t.Run("PUT Omitted Players Preserves Roster", func(t *testing.T) {
		const cid = "omitted-players-preserve"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:                cid,
			Name:              "Omitted Players Source",
			HasParticipantIDs: true,
		}))
		require.NoError(t, store.SaveParticipants(cid, []domain.Player{
			{Name: "Alice", Dojo: "Dojo A"},
			{Name: "Bob", Dojo: "Dojo B"},
		}))
		// Settings-only PUT: no players field in body. AdminSettings's
		// saveNow allowlist produces this shape.
		settingsBody := []byte(`{"id":"omitted-players-preserve","name":"Renamed Comp"}`)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(settingsBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "PUT with omitted players must succeed: %s", w.Body.String())

		// Roster on disk MUST be unchanged.
		after, err := store.LoadParticipants(cid, false)
		require.NoError(t, err)
		assert.Len(t, after, 2, "PUT with omitted Players must NOT clear the roster")
	})

	// Copilot finding (PR #104 round-9-followup): the PUT handler
	// unconditionally copied every settings field from the request body
	// onto the freshly loaded `current`. The AdminParticipants page
	// sends `{ ...c, players: np }` — where `c` is a possibly stale
	// frontend snapshot — so a roster save would silently revert any
	// concurrent settings change (poolSize, courts, startTime, etc.)
	// that landed on the server after the page loaded its `c` snapshot.
	//
	// Fix: when the body carries the `players` field (present, possibly
	// empty), treat the PUT as roster-only and skip the settings copy.
	// Settings updates use AdminSettings which OMITS `players` and
	// takes the settings-merge branch.
	t.Run("PUT With Players Does NOT Overwrite Concurrent Settings", func(t *testing.T) {
		const cid = "roster-save-preserve-settings"
		// Seed the disk record with the server-side "current" settings
		// (post-concurrent-change). The roster-save body carries STALE
		// versions of these — they must NOT land on disk.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:           cid,
			Name:         "Server Current Name",
			PoolSize:     5,
			PoolWinners:  3,
			Courts:       []string{"A", "B"},
			NumberPrefix: "SERVER",
			StartTime:    "10:30",
		}))

		// Simulate AdminParticipants's `{ ...c, players: np }` body
		// where `c` has STALE settings (pre-concurrent-change values).
		// Pre-fix, the transform would copy these stale values onto
		// `current`, reverting the server's newer ones.
		body, _ := json.Marshal(map[string]any{
			"id":           cid,
			"name":         "Stale Name From Snapshot",
			"poolSize":     2,
			"poolWinners":  1,
			"courts":       []string{"X"},
			"numberPrefix": "STALE",
			"startTime":    "08:00",
			"date":         "01-01-2026",
			"players": []map[string]any{
				{"Name": "New Player", "Dojo": "New Dojo"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code,
			"roster-only PUT must succeed: %s", w.Body.String())

		// Verify settings on disk match the SERVER's pre-PUT state,
		// NOT the stale body. The body's settings must have been ignored.
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "Server Current Name", stored.Name,
			"server's Name must be preserved when body carries stale snapshot")
		assert.Equal(t, 5, stored.PoolSize,
			"server's PoolSize must be preserved")
		assert.Equal(t, 3, stored.PoolWinners,
			"server's PoolWinners must be preserved")
		assert.Equal(t, []string{"A", "B"}, stored.Courts,
			"server's Courts must be preserved")
		assert.Equal(t, "SERVER", stored.NumberPrefix,
			"server's NumberPrefix must be preserved")
		assert.Equal(t, "10:30", stored.StartTime,
			"server's StartTime must be preserved")

		// And the roster save DID happen.
		parts, err := store.LoadParticipants(cid, false)
		require.NoError(t, err)
		assert.Len(t, parts, 1, "roster body must have landed")
		assert.Equal(t, "New Player", parts[0].Name)
		// HasParticipantIDs flipped to true (populated roster path).
		assert.True(t, stored.HasParticipantIDs)
	})
}

// TestPUTCompetition_RosterPUTBypassesSettingsValidation pins the
// Copilot round-12 finding (#4): settings-specific validators
// (validateDateDMY, validateCompetitionCourts, empty-name check) used
// to run BEFORE the transform's branch decision, so a roster-only PUT
// from AdminParticipants (`{ ...c, players: np }` spread) carrying a
// stale settings field would fail with "date must be DD-MM-YYYY" even
// though the field was about to be ignored by the transform. Now those
// validators only run when comp.Players == nil (settings-only PUT).
func TestPUTCompetition_RosterPUTBypassesSettingsValidation(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed a competition with a NON-DMY date (simulating legacy state
	// from before the canonical-format cleanup landed). Direct
	// SaveCompetition bypasses the handler's validation so we can plant
	// the legacy shape.
	cid := "legacy-date-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:   cid,
		Name: "Legacy",
		Date: "2026-05-12", // ISO format, would fail validateDateDMY
	}))

	// Roster-only PUT — AdminParticipants spreads `{ ...c, players: np }`
	// where c.date is the on-disk legacy ISO date.
	body, _ := json.Marshal(state.Competition{
		ID:   cid,
		Name: "Legacy",
		Date: "2026-05-12", // stale ISO from c.date
		Players: []domain.Player{
			{ID: "p1-uuid", Name: "P1", Dojo: "D1"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code,
		"roster-only PUT must succeed even with non-DMY date in body: %s", w.Body.String())

	// Verify the roster landed and the legacy date is preserved on disk
	// (the PUT didn't touch the settings field — that's the whole point).
	parts, _ := store.LoadParticipants(cid, false)
	assert.Len(t, parts, 1, "roster must have landed")
	stored, _ := store.LoadCompetition(cid)
	assert.Equal(t, "2026-05-12", stored.Date, "legacy date untouched by roster PUT")
}

// TestPUTCompetition_SettingsOnlyResponseIncludesPlayers pins the
// Copilot round-12 finding (#5): settings-only PUTs used to return
// `players: null` in the response because LoadCompetition doesn't
// populate Players from participants.csv. admin.jsx's
// `{ ...c, ...updated }` merge then pushed null into local state,
// crashing render paths that read `c.players.length`. The handler now
// loads the on-disk roster for the response when comp.Players == nil.
func TestPUTCompetition_SettingsOnlyResponseIncludesPlayers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "with-roster"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                cid,
		Name:              "With Roster",
		Date:              "12-05-2026",
		HasParticipantIDs: true,
	}))
	// Use real UUID v4 IDs so the auto-detect / hinted loader recognises
	// the format and Names parse correctly on LoadParticipants.
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "Dojo X"},
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob", Dojo: "Dojo Y"},
	}))

	// Settings-only PUT — body OMITS players. Just renaming.
	body := []byte(`{"id":"with-roster","name":"With Roster Renamed","date":"12-05-2026"}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	// Parse the response — the Players field must be a non-null array
	// reflecting the on-disk roster.
	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Players, "Players must NOT be null in settings-only PUT response")
	assert.Len(t, resp.Players, 2, "Players must contain the on-disk roster")
	assert.Equal(t, "Alice", resp.Players[0].Name)
	assert.Equal(t, "Bob", resp.Players[1].Name)

	// Also verify the response body's JSON literally has a players array,
	// not the string "null" — Go's nil slice serializes to "null", which
	// is the bug shape we're guarding against.
	assert.NotContains(t, w.Body.String(), `"players":null`,
		"response must not ship `players: null` — clients merge this into local state")
}

// TestPlayoff_ResponseIncludesPlayers pins the Copilot round-12
// finding (#6) server-side: POST /playoffs used to ship `players: null`
// in the create response (Go nil slice → JSON null). admin.jsx's
// refreshCompsAfterCreate fallback appends the response directly into
// local state, and render paths that read `c.players.length` crash.
// The handler now loads the placeholder roster for the response.
func TestPlayoff_ResponseIncludesPlayers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Source pools competition with 2 participants → 1 pool → 2 winners
	// → 2 reserved-slot placeholders on the playoff.
	src := state.Competition{
		ID:          "src",
		Name:        "Source",
		Format:      state.CompFormatPools,
		Status:      state.CompStatusPools,
		PoolSize:    3,
		PoolWinners: 2,
	}
	require.NoError(t, store.SaveCompetition(&src))
	require.NoError(t, store.SaveParticipants("src", []domain.Player{
		{ID: "p1", Name: "P1", Dojo: "D1"},
		{ID: "p2", Name: "P2", Dojo: "D2"},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/src/playoffs", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "response: %s", w.Body.String())

	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Players, "Players must NOT be null in POST /playoffs response")
	assert.NotContains(t, w.Body.String(), `"players":null`,
		"response must not ship `players: null` — client appends this into local state")
	// The actual content is reserved-slot placeholders; we just care
	// that the field isn't null.
	assert.GreaterOrEqual(t, len(resp.Players), 1,
		"placeholder participants must be present in the response")
}

// TestPUTCompetition_DefersHasParticipantIDsOnSaveFailure pins the
// Copilot round-12 finding (#1): the transform used to flip
// HasParticipantIDs=true BEFORE the post-transform SaveParticipants
// call. If SaveParticipants then failed (disk full, EISDIR, etc.) the
// config carried HasParticipantIDs=true while participants.csv
// retained the OLD non-UUID format — the HasIDs-hinted loader would
// then misparse the file. The flag flip is now deferred to AFTER
// SaveParticipants succeeds.
func TestPUTCompetition_DefersHasParticipantIDsOnSaveFailure(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "save-fails-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Save Fails", HasParticipantIDs: false,
	}))

	// Plant a directory where participants.csv should be — the
	// SaveParticipants -> WriteFile call will fail with EISDIR.
	plantedDir := filepath.Join(tempDir, "competitions", cid, "participants.csv")
	require.NoError(t, os.MkdirAll(plantedDir, 0o700))

	body, _ := json.Marshal(state.Competition{
		ID:   cid,
		Name: "Save Fails",
		Players: []domain.Player{
			{ID: "p1-uuid", Name: "P1", Dojo: "D1"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code,
		"save failure must surface as 500 (got %s): %s", w.Code, w.Body.String())

	// HasParticipantIDs must NOT have been flipped to true — the file
	// save failed, so the metadata flag stays in sync with the still-
	// missing file.
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.False(t, stored.HasParticipantIDs,
		"HasParticipantIDs must NOT flip to true when SaveParticipants fails")
}

// TestPublicViewerCompetitionDetail_InvalidIDReturns400 pins the
// Copilot round-13 finding (#7): the public viewer GET
// /competitions/:id used to call store.LoadCompetition(id) directly
// without requireValidCompID, so invalid IDs surfaced as 500 instead
// of the documented 400. Aligning to 400 matches the OpenAPI spec
// (CompetitionId parameter description) and the path-traversal
// defense rationale.
func TestPublicViewerCompetitionDetail_InvalidIDReturns400(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Traversal-shaped ID via a literal slash inside the path component
	// is normalised by the router into a different route, so use an
	// invalid character that ValidateCompetitionID would reject (a
	// space). Pre-fix: 500. Post-fix: 400.
	// URL is the public viewer route — no auth required, no admin
	// password header.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions/bad%20id", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"invalid :id on public viewer detail route should 400 (was 500 pre-fix): %s", w.Body.String())
}

// TestRecordBracketMatchResult_PreservesRunningStatus pins the
// Copilot round-13 finding (#6): recordBracketMatchResult used to
// unconditionally set the bracket match status to Completed, so the
// scoring modal's "Start match" tap (which sends
// `{status: "running"}`) immediately persisted the match as completed
// with no winner. Now the status from the result is preserved (with
// Completed as the backward-compat default for empty), and
// propagateBracketWinner only fires when the match is actually
// completed.
func TestRecordBracketMatchResult_PreservesRunningStatus(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "bracket-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "Bracket",
		Format: state.CompFormatPlayoffs,
		Status: state.CompStatusPlayoffs,
	}))
	// Seed a single bracket match.
	require.NoError(t, store.SaveBracket(cid, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:     "r1-m0",
					SideA:  "Alice",
					SideB:  "Bob",
					Status: state.MatchStatusScheduled,
				},
			},
		},
	}))

	// "Start" payload — admin tapping Start on the scoring modal.
	body := []byte(`{"id":"r1-m0","sideA":"Alice","sideB":"Bob","status":"running"}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/matches/r1-m0/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	// Re-load bracket and verify the match is RUNNING, not COMPLETED.
	br, err := store.LoadBracket(cid)
	require.NoError(t, err)
	require.NotNil(t, br)
	require.Len(t, br.Rounds, 1)
	require.Len(t, br.Rounds[0], 1)
	m := br.Rounds[0][0]
	assert.Equal(t, state.MatchStatusRunning, m.Status,
		"bracket match must reflect the incoming `running` status, not be forced to completed")
	assert.Equal(t, "", m.Winner,
		"running match must have no winner — pre-fix the force-completed path also propagated empty winner upstream")
}

// TestValidateCompetitionDurations_Negative verifies that a negative duration
// is rejected.
func TestValidateCompetitionDurations_Negative(t *testing.T) {
	err := validateCompetitionDurations(&state.Competition{PoolMatchDuration: -1})
	assert.Error(t, err)
	err = validateCompetitionDurations(&state.Competition{PlayoffMatchDuration: -1})
	assert.Error(t, err)
	err = validateCompetitionDurations(&state.Competition{MatchDuration: -1})
	assert.Error(t, err)
}

// TestValidateCompetitionFormat_UnknownFormat verifies that unknown format
// strings are rejected.
func TestValidateCompetitionFormat_UnknownFormat(t *testing.T) {
	code, err := validateCompetitionFormat("garbage", "")
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, code)
}

// TestValidateCompetitionFormat_UnknownPoolFormat verifies that unknown pool
// format strings are rejected.
func TestValidateCompetitionFormat_UnknownPoolFormat(t *testing.T) {
	code, err := validateCompetitionFormat("", "garbage")
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, code)
}
