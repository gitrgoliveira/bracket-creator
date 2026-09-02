package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolvePoolOverrideTarget is a direct unit test of the resolver bc-cse
// FIX 1 and FIX 2 changed: NO FALLBACKS for an off-roster playerName (must
// error, never return a resolvable-looking empty id/dojo pair), and a
// playerId/playerName cross-check when both are supplied. Exercised directly
// because the HTTP handler around it (PUT .../override-rank) has its own
// unconditional "playerName is required" gate, which would make "id of A +
// empty name" un-reachable through the handler even though the resolver
// itself must accept it (id alone is a complete identity).
func TestResolvePoolOverrideTarget(t *testing.T) {
	players := []domain.Player{
		{ID: "member-a", Name: "Member A", Dojo: "Dojo A"},
		{ID: "member-b", Name: "Member B", Dojo: "Dojo B"},
	}

	t.Run("FIX 1: off-roster playerName is rejected, not silently empty", func(t *testing.T) {
		id, dojo, err := resolvePoolOverrideTarget(players, "", "Nobody Here", "")
		require.Error(t, err, "an off-roster playerName must error rather than resolve to an unreadable empty key")
		assert.Contains(t, err.Error(), "Nobody Here")
		assert.Empty(t, id)
		assert.Empty(t, dojo)
	})

	t.Run("FIX 2: playerId of A + playerName of B is rejected", func(t *testing.T) {
		id, dojo, err := resolvePoolOverrideTarget(players, "member-a", "Member B", "")
		require.Error(t, err, "a playerId/playerName pair naming two different pool members must be rejected")
		assert.Contains(t, err.Error(), "member-a")
		assert.Contains(t, err.Error(), "Member B")
		assert.Empty(t, id)
		assert.Empty(t, dojo)
	})

	t.Run("FIX 2: playerId of A + empty playerName is accepted", func(t *testing.T) {
		id, dojo, err := resolvePoolOverrideTarget(players, "member-a", "", "")
		require.NoError(t, err, "id alone is a complete identity; an empty playerName must not block it")
		assert.Equal(t, "member-a", id)
		assert.Equal(t, "Dojo A", dojo)
	})

	t.Run("FIX 2: playerId of A + matching playerName of A is accepted", func(t *testing.T) {
		id, dojo, err := resolvePoolOverrideTarget(players, "member-a", "Member A", "")
		require.NoError(t, err, "a matching id/name pair must be accepted")
		assert.Equal(t, "member-a", id)
		assert.Equal(t, "Dojo A", dojo)
	})
}

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

		// 2. Reject: pools status (in progress), must be invalidated first.
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

	t.Run("Override Rank", func(t *testing.T) {
		comp := state.Competition{ID: "rank-comp", Status: state.CompStatusPools}
		store.SaveCompetition(&comp)
		// Seed a pool so the new pool-size validation can find pool-1
		// (rank within a pool is bounded by len(pool.Players)).
		require.NoError(t, store.SavePools("rank-comp", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{Name: "Player 1", Dojo: "Dojo Player 1"}, {Name: "Player 2", Dojo: "Dojo Player 2"}, {Name: "Player 3", Dojo: "Dojo Player 3"},
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
		// Padded names must be resolved (and looked up against the roster)
		// under the TRIMMED name, so the override lands on the same
		// competitor a subsequent read resolves. Since bc-cse the override
		// key itself is the competitor's identity key (helper.CompetitorKey),
		// not the bare name -- "Player Trim" is placed in the roster with a
		// real id/dojo so this also exercises that resolution, not just the
		// trim.
		comp := state.Competition{ID: "rank-trim-comp", Status: state.CompStatusPools}
		store.SaveCompetition(&comp)
		// Seed a pool with at least 7 players (rank=7 below), the 7th being
		// the trim target.
		players := make([]helper.Player, 8)
		for i := range players {
			players[i] = helper.Player{Name: fmt.Sprintf("Player %d", i+1)}
		}
		players[6] = helper.Player{ID: "trim-player-id", Name: "Player Trim", Dojo: "Trim Dojo"}
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

		// Read the persisted override back; key must be the resolved
		// competitor's identity key, built from the TRIMMED name matched
		// against the roster (not the padded request string, and not a bare
		// name).
		overrides, err := store.LoadOverrides("rank-trim-comp")
		require.NoError(t, err)
		require.NotNil(t, overrides)
		trimmedKey := helper.CompetitorKey("trim-player-id", "Player Trim", "Trim Dojo")
		_, hasTrimmed := overrides.PoolRanks["pool-1"][trimmedKey]
		assert.True(t, hasTrimmed, "rank override should be keyed under the resolved competitor's identity key")
		_, hasPadded := overrides.PoolRanks["pool-1"]["  Player Trim  "]
		assert.False(t, hasPadded, "rank override should not be keyed under the padded raw name")
		_, hasBareName := overrides.PoolRanks["pool-1"]["Player Trim"]
		assert.False(t, hasBareName, "rank override should not be keyed under the bare trimmed name either")
	})

	t.Run("Override Rank Rejects Invalid Input", func(t *testing.T) {
		comp := state.Competition{ID: "rank-bad-comp", Status: state.CompStatusPools}
		store.SaveCompetition(&comp)
		// Seed a pool so cases that pass the rank cap checks reach the
		// pool-size validation (rank=99999 fails earlier at the absolute
		// MaxRankOverride cap; rank=4-against-3-player-pool fails the
		// pool-size check).
		require.NoError(t, store.SavePools("rank-bad-comp", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{Name: "Player 1", Dojo: "Dojo Player 1"}, {Name: "Player 2", Dojo: "Dojo Player 2"}, {Name: "Player 3", Dojo: "Dojo Player 3"},
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
		// A bogus: poolId (no matching Pool.PoolName) returns 404.
		// The JS frontend only offers existing pools; this is a
		// defense-in-depth check against hand-crafted API callers.
		comp := state.Competition{ID: "rank-unknown-pool", Status: state.CompStatusPools}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-unknown-pool", []helper.Pool{
			{PoolName: "pool-a", Players: []helper.Player{{Name: "P1", Dojo: "Dojo P1"}}},
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

	t.Run("Override Rank Rejects Wrong Competition Status With 409", func(t *testing.T) {
		validBody, _ := json.Marshal(map[string]any{"playerName": "P1", "rank": 1})
		for _, status := range []state.CompetitionStatus{
			state.CompStatusSetup,
			state.CompStatusKnockout,
			state.CompStatusComplete,
			state.CompStatusInvalid,
		} {
			compID := "rank-status-" + string(status)
			c := state.Competition{ID: compID, Status: status}
			require.NoError(t, store.SaveCompetition(&c))
			require.NoError(t, store.SavePools(compID, []helper.Pool{
				{PoolName: "pool-1", Players: []helper.Player{{Name: "P1", Dojo: "Dojo P1"}}},
			}))
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/pools/pool-1/override-rank", bytes.NewBuffer(validBody))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusConflict, w.Code,
				"status=%q should return 409", status)
			assert.Contains(t, w.Body.String(), "pools stage",
				"error body should mention pools stage for status=%q", status)
		}
	})

	// Same-name-different-dojo disambiguation (bc-cse): two pool members can
	// legally share a display name (operator identity rule is (name, dojo),
	// not name -- helper.CheckDuplicateEntriesByNameDojo only refuses a true
	// (name, dojo) collision). The override-rank endpoint must resolve each
	// request to exactly one of them, never both.
	t.Run("Override Rank Disambiguates Same-Name Different-Dojo By PlayerId", func(t *testing.T) {
		comp := state.Competition{ID: "rank-dup-id", Status: state.CompStatusPools}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-dup-id", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{ID: "dup-tokyo", Name: "Tanaka Kenji", Dojo: "Tokyo"},
				{ID: "dup-osaka", Name: "Tanaka Kenji", Dojo: "Osaka"},
				{ID: "dup-third", Name: "Suzuki Hiro", Dojo: "Nagoya"},
			}},
		}))

		reqBody, _ := json.Marshal(map[string]any{
			"playerId":   "dup-osaka",
			"playerName": "Tanaka Kenji",
			"rank":       1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-dup-id/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		overrides, err := store.LoadOverrides("rank-dup-id")
		require.NoError(t, err)
		osakaKey := helper.CompetitorKey("dup-osaka", "Tanaka Kenji", "Osaka")
		tokyoKey := helper.CompetitorKey("dup-tokyo", "Tanaka Kenji", "Tokyo")
		_, hasOsaka := overrides.PoolRanks["pool-1"][osakaKey]
		assert.True(t, hasOsaka, "the override must land under the id-identified Osaka Tanaka")
		_, hasTokyo := overrides.PoolRanks["pool-1"][tokyoKey]
		assert.False(t, hasTokyo, "the namesake Tokyo Tanaka must not receive an override meant for Osaka")
	})

	t.Run("Override Rank Disambiguates Same-Name Different-Dojo By PlayerDojo", func(t *testing.T) {
		comp := state.Competition{ID: "rank-dup-dojo", Status: state.CompStatusPools}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-dup-dojo", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{ID: "dup2-tokyo", Name: "Tanaka Kenji", Dojo: "Tokyo"},
				{ID: "dup2-osaka", Name: "Tanaka Kenji", Dojo: "Osaka"},
				{ID: "dup2-third", Name: "Suzuki Hiro", Dojo: "Nagoya"},
			}},
		}))

		// No playerId, only playerDojo -- exercises the name+dojo resolution
		// branch a client that only knows the dojo (not the id) would take.
		reqBody, _ := json.Marshal(map[string]any{
			"playerName": "Tanaka Kenji",
			"playerDojo": "Tokyo",
			"rank":       2,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-dup-dojo/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		overrides, err := store.LoadOverrides("rank-dup-dojo")
		require.NoError(t, err)
		tokyoKey := helper.CompetitorKey("dup2-tokyo", "Tanaka Kenji", "Tokyo")
		osakaKey := helper.CompetitorKey("dup2-osaka", "Tanaka Kenji", "Osaka")
		_, hasTokyo := overrides.PoolRanks["pool-1"][tokyoKey]
		assert.True(t, hasTokyo, "the override must land under the dojo-identified Tokyo Tanaka")
		_, hasOsaka := overrides.PoolRanks["pool-1"][osakaKey]
		assert.False(t, hasOsaka, "the namesake Osaka Tanaka must not receive an override meant for Tokyo")
	})

	t.Run("Override Rank Rejects Ambiguous Same-Name Request With 400", func(t *testing.T) {
		comp := state.Competition{ID: "rank-dup-ambiguous", Status: state.CompStatusPools}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-dup-ambiguous", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{ID: "dup3-tokyo", Name: "Tanaka Kenji", Dojo: "Tokyo"},
				{ID: "dup3-osaka", Name: "Tanaka Kenji", Dojo: "Osaka"},
			}},
		}))

		// No playerId, no playerDojo: the request cannot disambiguate which
		// Tanaka Kenji it means, so it must be rejected rather than silently
		// applied to an arbitrary one of them (the exact bug bc-cse closes).
		reqBody, _ := json.Marshal(map[string]any{"playerName": "Tanaka Kenji", "rank": 1})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-dup-ambiguous/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "ambiguous")
	})

	// bc-cse FIX 1: a playerName that matches NO roster entry used to
	// resolve to ("", "", nil), which the handler then stored under
	// helper.CompetitorKey("", name, "") -- a key no read path
	// (lookupPoolRankOverride) ever derives, silently discarding the
	// operator's override. NO FALLBACKS: this must be a 400, and the write
	// must never reach disk.
	t.Run("Override Rank Rejects Off-Roster PlayerName With 400", func(t *testing.T) {
		comp := state.Competition{ID: "rank-off-roster", Status: state.CompStatusPools}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-off-roster", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{ID: "p1", Name: "Player 1", Dojo: "Dojo 1"},
			}},
		}))

		reqBody, _ := json.Marshal(map[string]any{"playerName": "Nobody Here", "rank": 1})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-off-roster/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code,
			"a playerName with no roster match must be refused, not silently stored under an unreadable key")
		assert.Contains(t, w.Body.String(), "Nobody Here",
			"the error should name the unmatched playerName")

		overridesPath := filepath.Join(tempDir, "competitions", "rank-off-roster", "overrides.json")
		_, statErr := os.Stat(overridesPath)
		assert.True(t, os.IsNotExist(statErr), "overrides.json must not be created/modified by a refused override")
	})

	// bc-cse FIX 2: the playerID branch used to return as soon as the id
	// matched a pool member, ignoring the request's playerName entirely --
	// so a confidently-wrong id/name pair silently wrote a rank against the
	// WRONG competitor with a 200. Cross-check id against name whenever
	// both are given.
	t.Run("Override Rank Cross-Checks PlayerId Against PlayerName", func(t *testing.T) {
		comp := state.Competition{ID: "rank-id-name-mismatch", Status: state.CompStatusPools}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-id-name-mismatch", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{ID: "member-a", Name: "Member A", Dojo: "Dojo A"},
				{ID: "member-b", Name: "Member B", Dojo: "Dojo B"},
			}},
		}))

		t.Run("id of A + name of B is rejected", func(t *testing.T) {
			reqBody, _ := json.Marshal(map[string]any{
				"playerId":   "member-a",
				"playerName": "Member B",
				"rank":       1,
			})
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/rank-id-name-mismatch/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"a playerId/playerName pair naming two different pool members must be rejected")
			assert.Contains(t, w.Body.String(), "member-a")
			assert.Contains(t, w.Body.String(), "Member B")

			overrides, err := store.LoadOverrides("rank-id-name-mismatch")
			require.NoError(t, err)
			assert.Empty(t, overrides.PoolRanks["pool-1"], "the mismatched request must not have written any override")
		})

		// "id of A + empty name" is exercised as a unit test of
		// resolvePoolOverrideTarget directly (TestResolvePoolOverrideTarget
		// below), not through this HTTP handler: the handler has its OWN
		// unconditional "playerName is required" gate (checked before
		// resolvePoolOverrideTarget is ever called), so an HTTP request with
		// a valid playerId and no playerName always 400s on that gate, for a
		// reason unrelated to the id/name cross-check this fix adds.

		t.Run("id of A + name of A is accepted", func(t *testing.T) {
			reqBody, _ := json.Marshal(map[string]any{
				"playerId":   "member-a",
				"playerName": "Member A",
				"rank":       2,
			})
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/rank-id-name-mismatch/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "a matching id/name pair must be accepted")

			overrides, err := store.LoadOverrides("rank-id-name-mismatch")
			require.NoError(t, err)
			key := helper.CompetitorKey("member-a", "Member A", "Dojo A")
			assert.Equal(t, 2, overrides.PoolRanks["pool-1"][key])
		})
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
	// endpoints, drop one TrimSpace and a downstream switch on the
	// non-canonical value silently falls through.
	t.Run("All String Fields Trimmed On Create", func(t *testing.T) {
		// bc-symm: PoolSize is incidental to what this test checks (string
		// trimming), but format "mixed" now requires a usable pool size on
		// create (validateMixedPoolSize), so the fixture needs a real value
		// or it 400s before the trim assertions below ever run.
		comp := state.Competition{
			ID: "trim-fields-create", Name: "Trim Fields Create",
			Kind: "  individual  ", Format: "  mixed  ",
			PoolSizeMode: "  min  ", StartTime: "  09:00  ", Date: "  12-05-2026  ",
			PoolSize: 4,
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		stored, err := store.LoadCompetition("trim-fields-create")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "individual", stored.Kind, "Kind should be trimmed on POST")
		assert.Equal(t, "mixed", stored.Format, "Format should be trimmed on POST")
		assert.Equal(t, "min", stored.PoolSizeMode, "PoolSizeMode should be trimmed on POST")
		assert.Equal(t, "09:00", stored.StartTime, "StartTime should be trimmed on POST")
		assert.Equal(t, "12-05-2026", stored.Date, "Date should be trimmed on POST")
	})

	t.Run("All String Fields Trimmed On Update", func(t *testing.T) {
		seed := state.Competition{ID: "trim-fields-update", Name: "Trim Fields Update", Kind: "individual", Format: "mixed"}
		require.NoError(t, store.SaveCompetition(&seed))

		update := state.Competition{
			ID: "trim-fields-update", Name: "Trim Fields Update",
			Kind: "  team  ", Format: "  knockout  ",
			PoolSizeMode: "  exact  ", StartTime: "  10:30  ", Date: "  15-06-2026  ",
			TeamSize: 2,
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
		assert.Equal(t, "knockout", stored.Format, "Format should be trimmed on PUT")
		assert.Equal(t, "exact", stored.PoolSizeMode, "PoolSizeMode should be trimmed on PUT")
		assert.Equal(t, "10:30", stored.StartTime, "StartTime should be trimmed on PUT")
		assert.Equal(t, "15-06-2026", stored.Date, "Date should be trimmed on PUT")
	})

	// Cross-file guard symmetry with handlers_tournament.go: whitespace-only
	// Name must be rejected on both POST and PUT after trim. Without this,
	// a hand-crafted POST with `{id: "foo", name: "   "}` lands as
	// Name="" on disk (slugifyID is bypassed when ID is explicit, and
	// checkUniqueCompName("", ...) passes when no other empty-named
	// competition exists), admin UI then shows a blank competition card.
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
			"2026-05-12", // ISO shape, not accepted
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

			// PUT, body Date is bad; comp must still exist with the seeded date.
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
	// block every subsequent admin Settings save, saveLater re-validates
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
	// bucketing on the label string, duplicates collapse two courts'
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
	// and filter dropdown value, visually blank but structurally
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
	// (same shape as requireValidCompID does for routes with: id
	// in the URL).
	t.Run("POST Rejects Invalid Body ID With 400", func(t *testing.T) {
		// Single-segment payloads that gin will deliver verbatim to the
		// handler (vs traversal payloads which the router may reject).
		// Same set as the Path_Traversal_IDs_Rejected single-segment
		// list, every one violates ValidateCompetitionID's char rule.
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
			assert.Nilf(t, stored, "POST with id=%q must not persist", badID)
		}
	})

	// Tri-review finding: an embedded roster on POST /competitions and the
	// roster-PUT branch of PUT /competitions/:id only ran validatePlayerLengths
	// (max-length), never the blank-name/dojo guard that POST /participants
	// has. A two-column "Name, Dojo" paste in a zekken competition maps to
	// {displayName: dojo, dojo: ""}, so a blank dojo persisted a corrupted
	// competitor while the UI reported success. Both paths now share
	// validatePlayerRequired and must reject the blank field with 400.
	t.Run("Embedded Roster Rejects Blank Name Or Dojo With 400", func(t *testing.T) {
		blankCases := []struct {
			label   string
			player  domain.Player
			wantMsg string
		}{
			{"blank dojo", domain.Player{Name: "Alice", Dojo: "   "}, "dojo must not be blank"},
			{"blank name", domain.Player{Name: "  ", Dojo: "Dojo A"}, "name must not be blank"},
		}
		for _, bc := range blankCases {
			// POST /competitions with an embedded roster.
			comp := state.Competition{
				ID:      "blank-roster-post",
				Name:    "Blank Roster POST",
				Players: []domain.Player{bc.player},
			}
			body, _ := json.Marshal(comp)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equalf(t, http.StatusBadRequest, w.Code,
				"POST embedded roster with %s must return 400 (got %d: %s)", bc.label, w.Code, w.Body.String())
			assert.Containsf(t, w.Body.String(), bc.wantMsg, "POST %s error message", bc.label)
			stored, _ := store.LoadCompetition("blank-roster-post")
			assert.Nilf(t, stored, "POST with %s must not persist", bc.label)

			// PUT /competitions/:id roster branch (Players != nil).
			seed := state.Competition{ID: "blank-roster-put", Name: "Blank Roster PUT", Status: state.CompStatusSetup}
			require.NoError(t, store.SaveCompetition(&seed))
			update := state.Competition{
				ID:      "blank-roster-put",
				Name:    "Blank Roster PUT",
				Players: []domain.Player{bc.player},
			}
			body, _ = json.Marshal(update)
			w = httptest.NewRecorder()
			req, _ = http.NewRequest("PUT", "/api/competitions/blank-roster-put", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equalf(t, http.StatusBadRequest, w.Code,
				"PUT roster with %s must return 400 (got %d: %s)", bc.label, w.Code, w.Body.String())
			assert.Containsf(t, w.Body.String(), bc.wantMsg, "PUT %s error message", bc.label)
		}
	})

	// Path-traversal guard. ValidateCompetitionID was only called at 2 of
	// the 14: id handler sites pre-fix; the requireValidCompID helper now
	// gates every site. A compID like "../../../etc/passwd" would
	// otherwise reach compPath(id, ...) which does filepath.Clean(Join())
	// and cleanly escapes the data dir. Sample a handful of routes
	// (GET / PUT / DELETE / nested), the helper centralises the logic,
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
		//      level, handy as a smoke test that NOTHING returns 200,
		//      but they don't prove requireValidCompID itself ran.
		//
		//   2. Single-segment IDs containing characters outside
		//      [A-Za-z0-9_-] (".", " ", "%2e"). These DO reach the
		//      handler, so the helper is the only thing standing
		//      between them and a 200, perfect for asserting 400.
		//
		// Mix both: traversal payloads sweep for "no 200 ever";
		// single-segment payloads assert the precise 400 from the helper.
		traversalIDs := []string{
			"../../../etc/passwd",
			"..%2F..%2Fetc%2Fpasswd",
			"foo/bar",
		}
		// Single-segment IDs that reach the handler. Gin treats these
		// as one: id value; ValidateCompetitionID rejects each on the
		// invalid-character rule.
		singleSegmentIDs := []string{
			"foo bar",   // space
			"foo.bar",   // period
			"foo%2ebar", // URL-encoded period, gin decodes before match
			"foo+bar",   // plus
			"foo@bar",   // at-sign
		}
		// Representative endpoints across the affected handler set,
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
			{"POST", "/api/competitions/%s/start"},
			{"GET", "/api/competitions/%s/export"},
			{"PUT", "/api/competitions/%s/pools/main/override-rank"},
			{"DELETE", "/api/competitions/%s/overrides"},
			{"GET", "/api/competitions/%s/participants"},
			{"POST", "/api/competitions/%s/participants"},
			{"GET", "/api/competitions/%s/seeds"},
			{"PUT", "/api/competitions/%s/seeds"},
			{"POST", "/api/competitions/%s/generate-draw"},
			{"DELETE", "/api/competitions/%s/draw"},
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
		// requireValidCompID and downstream code 404'd on the bad id,
		// both regressions.
		//
		// Asserting only the status code is vacuous for PUT/POST routes
		// that bind JSON after the ID check: dropping requireValidCompID
		// from such a handler would still return 400 (from ShouldBindJSON
		// on the empty body). To prove the helper itself ran, also
		// require the response body to mention "competition ID", the
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
	// both into "skip save", so the AdminParticipants "clear roster"
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
		assert.Equalf(t, http.StatusOK, w.Code, "PUT with players=[] must succeed: %s", w.Body.String())

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
		assert.Equalf(t, http.StatusOK, w.Code, "PUT with omitted players must succeed: %s", w.Body.String())

		// Roster on disk MUST be unchanged.
		after, err := store.LoadParticipants(cid, false)
		require.NoError(t, err)
		assert.Len(t, after, 2, "PUT with omitted Players must NOT clear the roster")
	})

	// Copilot finding (PR #104 round-9-followup): the PUT handler
	// unconditionally copied every settings field from the request body
	// onto the freshly loaded `current`. The AdminParticipants page
	// sends `{ ...c, players: np }`, where `c` is a possibly stale
	// frontend snapshot, so a roster save would silently revert any
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
		// versions of these, they must NOT land on disk.
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

	// Roster-only PUT, AdminParticipants spreads `{ ...c, players: np }`
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
	// (the PUT didn't touch the settings field, that's the whole point).
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
// TestCompetitionDurationSeconds_PersistAndNormalize covers mp-m5kf: the
// PUT settings path persists the canonical *Seconds fields to disk, and the
// GET read handlers normalize legacy whole-minute / single-field durations
// into *Seconds so the SPA always receives resolved values.
func TestCompetitionDurationSeconds_PersistAndNormalize(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// 1. PUT with a sub-minute seconds value persists it verbatim to disk.
	seed := state.Competition{ID: "sec-comp", Name: "Sec Comp", Date: "12-05-2026", Format: "mixed"}
	require.NoError(t, store.SaveCompetition(&seed))
	body := []byte(`{"id":"sec-comp","name":"Sec Comp","date":"12-05-2026","format":"mixed","poolMatchDurationSeconds":150,"knockoutMatchDurationSeconds":210}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/sec-comp", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
	stored, err := store.LoadCompetition("sec-comp")
	require.NoError(t, err)
	assert.Equal(t, 150, stored.PoolMatchDurationSeconds, "2m30s must persist to disk")
	assert.Equal(t, 210, stored.KnockoutMatchDurationSeconds)

	// 2. A legacy per-phase MINUTE competition is normalized to *Seconds on the
	//    single-GET read path (x60).
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "legacy-min", Name: "Legacy Min", PoolMatchDurationSeconds: 180, KnockoutMatchDurationSeconds: 300,
	}))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions/legacy-min", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var got state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, 180, got.PoolMatchDurationSeconds, "GET must back-fill 3 min -> 180s")
	assert.Equal(t, 300, got.KnockoutMatchDurationSeconds, "GET must back-fill 5 min -> 300s")

	// 3. A legacy SINGLE-field competition (only match_duration) is normalized
	//    to both per-phase *Seconds on the LIST read path, so the SPA's
	//    seconds/minutes resolver never sees a bare legacy value.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "legacy-single", Name: "Legacy Single", MatchDuration: 5,
	}))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var list []state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	var single *state.Competition
	for i := range list {
		if list[i].ID == "legacy-single" {
			single = &list[i]
		}
	}
	require.NotNil(t, single, "legacy-single must appear in the list")
	assert.Equal(t, 300, single.PoolMatchDurationSeconds, "list GET must normalize match_duration 5 -> 300s")
	assert.Equal(t, 300, single.KnockoutMatchDurationSeconds)
}

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

	// Settings-only PUT, body OMITS players. Just renaming.
	body := []byte(`{"id":"with-roster","name":"With Roster Renamed","date":"12-05-2026"}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	// Parse the response, the Players field must be a non-null array
	// reflecting the on-disk roster.
	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Players, "Players must NOT be null in settings-only PUT response")
	assert.Len(t, resp.Players, 2, "Players must contain the on-disk roster")
	assert.Equal(t, "Alice", resp.Players[0].Name)
	assert.Equal(t, "Bob", resp.Players[1].Name)

	// Also verify the response body's JSON literally has a players array,
	// not the string "null", Go's nil slice serializes to "null", which
	// is the bug shape we're guarding against.
	assert.NotContains(t, w.Body.String(), `"players":null`,
		"response must not ship `players: null`, clients merge this into local state")
}

// mp-p7n: Copilot PR #185 round-3 finding, regenerating ids on save
// would orphan CompetitorStatus.PlayerID / team-lineup PlayerIDs that
// reference the original ids. Fix: keep the id verbatim on save; the loader
// (consulting Competition.HasParticipantIDs for the strip decision) handles
// any shape.
//
// This test pins the contract:
//   - The PUT body's non-UUID ids round-trip to the response intact
//     (no regeneration).
//   - Name/Dojo are correctly aligned in the response (no column shift
//     on load, the loader trusts HasParticipantIDs).
//   - A second PUT with the same body produces an idempotent round-trip
//     (ids don't churn).
//
// TestPUTCompetition_RosterPUT_NearDupWarningsAndTier1 pins the
// server-authoritative duplicate handling on the PRIMARY roster-import path
// (PUT /competitions/:id): a perfect (name,dojo) duplicate is rejected 409,
// and a near-duplicate pair is surfaced as non-blocking `warnings` in the
// response. (mp-ljry, Copilot round 2)
func TestPUTCompetition_RosterPUT_NearDupWarningsAndTier1(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "comp-ndw"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "NDW", Date: "12-05-2026",
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	// Perfect (name,dojo) duplicate → 409.
	dup := []byte(`{"id":"comp-ndw","name":"NDW","date":"12-05-2026","format":"knockout","kind":"individual","courts":["A"],
		"players":[{"name":"John Smith","dojo":"Wakaba"},{"name":"john  smith","dojo":"wakaba"}]}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(dup))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusConflict, w.Code, "perfect (name,dojo) dup on roster PUT must 409: %s", w.Body.String())

	// Near-duplicate (token-subset) → 200 with a warnings entry.
	near := []byte(`{"id":"comp-ndw","name":"NDW","date":"12-05-2026","format":"knockout","kind":"individual","courts":["A"],
		"players":[{"name":"Ana Maria Rossi","dojo":"Tora"},{"name":"Ana Rossi","dojo":"Wakaba"}]}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(near))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "near-dup roster PUT must succeed: %s", w.Body.String())

	var resp struct {
		Warnings []helper.NearDupWarning `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Warnings, 1, "near-dup pair must produce exactly one warning")
	assert.Equal(t, "token-subset", resp.Warnings[0].Score)

	// Clean roster → warnings present as an empty array (consistent shape).
	clean := []byte(`{"id":"comp-ndw","name":"NDW","date":"12-05-2026","format":"knockout","kind":"individual","courts":["A"],
		"players":[{"name":"Shudokan A","dojo":"X"},{"name":"Shudokan B","dojo":"Y"}]}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(clean))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"warnings":[]`, "clean roster must serialize warnings as [] not null")
}

func TestPUTCompetition_RosterPUTPreservesIDsAndAlignsColumns(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "asddasd"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Asddasd", Date: "12-05-2026",
		WithZekkenName: false, HasParticipantIDs: true,
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	body := []byte(`{
		"id":"asddasd","name":"Asddasd","date":"12-05-2026",
		"format":"knockout","kind":"individual","courts":["A"],
		"withZekkenName":false,
		"players":[
			{"id":"asddasd-p1","name":"Aaron Adams","dojo":"Team Alpha"},
			{"id":"asddasd-p2","name":"Albus Blake","dojo":"Team Delta"}
		]
	}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Players, 2)

	// Ids preserved, regeneration would orphan dependent stores.
	assert.Equal(t, "asddasd-p1", resp.Players[0].ID,
		"non-UUID id must round-trip intact; regeneration orphans competitor_status refs")
	assert.Equal(t, "asddasd-p2", resp.Players[1].ID)

	// Name and Dojo correctly aligned, no column shift on load.
	assert.Equal(t, "Aaron Adams", resp.Players[0].Name)
	assert.Equal(t, "Team Alpha", resp.Players[0].Dojo)
	assert.Empty(t, resp.Players[0].Metadata,
		"Metadata must be empty, pre-fix column shift dumped Dojo into Metadata[0]")
	assert.Equal(t, "Albus Blake", resp.Players[1].Name)
	assert.Equal(t, "Team Delta", resp.Players[1].Dojo)

	// Idempotent second PUT, ids stay stable.
	body2 := fmt.Appendf(nil, `{
		"id":"asddasd","name":"Asddasd","date":"12-05-2026",
		"format":"knockout","kind":"individual","courts":["A"],
		"withZekkenName":false,
		"players":[
			{"id":%q,"name":"Aaron Adams","dojo":"Team Alpha"},
			{"id":%q,"name":"Albus Blake","dojo":"Team Delta"}
		]
	}`, resp.Players[0].ID, resp.Players[1].ID)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var resp2 state.Competition
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))
	assert.Equal(t, "asddasd-p1", resp2.Players[0].ID, "ids must be stable across save round-trips")
}

// mp-p7n: uppercase-UUID id round-trips intact (no canonicalisation,
// no regeneration). The loader's HasParticipantIDs-based strip handles
// any shape, case isn't load-bearing for the id-strip decision once
// the flag is consulted.
func TestPUTCompetition_RosterPUTPreservesUppercaseUUID(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "upper-uuid"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Upper UUID", Date: "12-05-2026",
		WithZekkenName: false, HasParticipantIDs: true,
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	upperUUID := "85CDEB35-C066-4667-B7FD-43EBAE8A9F13"
	body := fmt.Appendf(nil, `{
		"id":"upper-uuid","name":"Upper UUID","date":"12-05-2026",
		"format":"knockout","kind":"individual","courts":["A"],
		"withZekkenName":false,
		"players":[{"id":%q,"name":"Aaron Adams","dojo":"Team Alpha"}]
	}`, upperUUID)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Players, 1)
	assert.Equal(t, upperUUID, resp.Players[0].ID,
		"client-supplied id must round-trip intact (case preserved, no regeneration)")
	assert.Equal(t, "Aaron Adams", resp.Players[0].Name)
	assert.Equal(t, "Team Alpha", resp.Players[0].Dojo)
}

// mp-p7n / Copilot PR #185 round-9: exercises the round-6
// 500-on-flag-flip-failure branch end-to-end, deterministically.
//
// Round-8 used a watcher-goroutine filesystem race to inject the
// failure between SaveParticipants and the flip; Copilot flagged that
// as nondeterministic (if the watcher lost the race it could observe
// participants.csv after the handler already returned 200, then assert
// 500). Replaced with a package-level test seam: flipHasParticipantIDs
// is a var, so the test swaps in a stub that returns an error with no
// timing dependency.
func TestPUTCompetition_RosterPUT_FlagFlipFailureReturns500(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Inject a deterministic flip failure; restore on exit. These
	// handler tests do not run in parallel, so mutating the package
	// var is safe.
	orig := flipHasParticipantIDs
	flipHasParticipantIDs = func(_ *state.Store, _ string) error {
		return fmt.Errorf("injected flag-flip failure")
	}
	defer func() { flipHasParticipantIDs = orig }()

	cid := "flip-fails"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Flip Fails", Date: "12-05-2026",
		HasParticipantIDs: false,
		Format:            state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	body, _ := json.Marshal(state.Competition{
		ID: cid, Name: "Flip Fails", Date: "12-05-2026",
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
		Players: []domain.Player{
			{ID: "flip-fails-p1", Name: "Aaron Adams", Dojo: "Team Alpha"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Round-6 contract: a flip failure surfaces as 500, NOT a 200 with
	// a stale flag.
	require.Equal(t, http.StatusInternalServerError, w.Code,
		"flip failure must surface as 500 (round-6 contract), got %d: %s", w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "HasParticipantIDs",
		"error body must mention HasParticipantIDs (round-6 flip-failure branch); got %s", w.Body.String())

	// The roster file itself DID land before the flip, confirm the
	// participants were saved (the failure is only in the metadata flip,
	// which is why the operator's retry is idempotent). Read with an
	// explicit HasIDs hint: a no-hint read would reproduce the column
	// shift precisely BECAUSE the flag flip failed (HasParticipantIDs is
	// still false on disk, so the loader auto-detects "no ids" for the
	// non-UUID first column). The hinted read proves the on-disk bytes
	// are correct and a successful retry (which lands the flip) recovers.
	trueP := true
	saved, lerr := store.LoadParticipantsOpt(cid, false, state.LoadParticipantsOpts{HasIDs: &trueP})
	require.NoError(t, lerr)
	require.Len(t, saved, 1)
	assert.Equal(t, "Aaron Adams", saved[0].Name)
	assert.Equal(t, "Team Alpha", saved[0].Dojo)
	assert.Equal(t, "flip-fails-p1", saved[0].ID)
}

// mp-p7n / Copilot PR #185 round-5: when the roster-PUT response
// re-loads participants, the deferred HasParticipantIDs=true flip is
// best-effort (failures only log). If the flip never lands, or simply
// hasn't landed yet, the loader's default branch reads the stale flag
// (false) and falls back to uuidRE-on-row-0, mis-classifying non-UUID
// ids as "no ids" and returning the column-shifted roster.
//
// Simulate the failure mode by forcing HasParticipantIDs=false on
// disk BEFORE the PUT, then asserting the response still surfaces
// correctly-aligned Name/Dojo. The handler now passes HasIDs=&true
// explicitly when re-loading a non-empty roster, so the loader trusts
// the call site (we just saved a non-empty roster, every row has an
// id) regardless of the metadata flag's state.
func TestPUTCompetition_RosterPUTResponseHardenedAgainstStaleFlag(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "stale-flag"
	// Note: HasParticipantIDs explicitly FALSE, simulating either the
	// pre-flip window (race) or a failed deferred flip (logged but not
	// surfaced). The reload must not trust this flag.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Stale Flag", Date: "12-05-2026",
		WithZekkenName: false, HasParticipantIDs: false,
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	body := []byte(`{
		"id":"stale-flag","name":"Stale Flag","date":"12-05-2026",
		"format":"knockout","kind":"individual","courts":["A"],
		"withZekkenName":false,
		"players":[
			{"id":"stale-flag-p1","name":"Aaron Adams","dojo":"Team Alpha"},
			{"id":"stale-flag-p2","name":"Albus Blake","dojo":"Team Delta"}
		]
	}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Players, 2)

	// Response MUST surface correctly-aligned Name/Dojo even if the
	// deferred HasParticipantIDs flip failed. Pre-fix, the reload
	// would have returned Name="Stale-Flag-P1", Dojo="Aaron Adams",
	// metadata=["Team Alpha"] (column shift).
	assert.Equal(t, "Aaron Adams", resp.Players[0].Name,
		"reload must use HasIDs=&true hint, not the stale comp flag, non-UUID id must be stripped from column 0")
	assert.Equal(t, "Team Alpha", resp.Players[0].Dojo)
	assert.Empty(t, resp.Players[0].Metadata)
	assert.Equal(t, "stale-flag-p1", resp.Players[0].ID,
		"original non-UUID id preserved across the round-trip")
	assert.Equal(t, "Aaron Adams", resp.Players[0].Name)
	assert.Equal(t, "Albus Blake", resp.Players[1].Name)
	assert.Equal(t, "Team Delta", resp.Players[1].Dojo)
}

// TestPUTCompetition_DefersHasParticipantIDsOnSaveFailure pins the
// Copilot round-12 finding (#1): the transform used to flip
// HasParticipantIDs=true BEFORE the post-transform SaveParticipants
// call. If SaveParticipants then failed (disk full, EISDIR, etc.) the
// config carried HasParticipantIDs=true while participants.csv
// retained the OLD non-UUID format, the HasIDs-hinted loader would
// then misparse the file. The flag flip is now deferred to AFTER
// SaveParticipants succeeds.
func TestPUTCompetition_DefersHasParticipantIDsOnSaveFailure(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "save-fails-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Save Fails", HasParticipantIDs: false,
	}))

	// Plant a directory where participants.csv should be, the
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

	// HasParticipantIDs must NOT have been flipped to true, the file
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
	// URL is the public viewer route, no auth required, no admin
	// password header.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions/bad%20id", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"invalid id on public viewer detail route should 400 (was 500 pre-fix): %s", w.Body.String())
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
		Format: state.CompFormatKnockout,
		Status: state.CompStatusKnockout,
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

	// "Start" payload, admin tapping Start on the scoring modal.
	body := []byte(`{"id":"r1-m0","sideA":"Alice","sideB":"Bob","status":"running"}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/matches/r1-m0/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

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
		"running match must have no winner, pre-fix the force-completed path also propagated empty winner upstream")
}

// TestValidateCompetitionDurations enforces the shiai band on the per-phase
// clock durations. A fat-fingered 3-second match drives the whole day's
// auto-schedule, so it is rejected outright rather than clamped. The check is a
// flat comparison with no reference to the stored value: state's legacy-duration
// migration clamps into the same band, so no stored duration can be out of range
// and there is nothing to grandfather.
func TestValidateCompetitionDurations(t *testing.T) {
	tests := []struct {
		name    string
		comp    state.Competition
		wantErr bool
	}{
		{"pool below floor", state.Competition{PoolMatchDurationSeconds: 3}, true},
		{"knockout below floor", state.Competition{KnockoutMatchDurationSeconds: state.MinMatchDurationSeconds - 1}, true},
		{"pool at floor", state.Competition{PoolMatchDurationSeconds: state.MinMatchDurationSeconds}, false},
		{"knockout at ceiling", state.Competition{KnockoutMatchDurationSeconds: state.MaxMatchDurationSeconds}, false},
		{"pool above ceiling", state.Competition{PoolMatchDurationSeconds: state.MaxMatchDurationSeconds + 1}, true},
		{"a valid sub-minute value passes", state.Competition{PoolMatchDurationSeconds: 150}, false},
		{"negative is rejected", state.Competition{PoolMatchDurationSeconds: -1}, true},
		// 0 is "unset, use the scheduler default" and must stay accepted:
		// otherwise clearing the field would fail to save.
		{"unset is allowed", state.Competition{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCompetitionDurations(&tt.comp)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
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

// TestPUTCompetition_CheckInEnabledPersists verifies that the settings-only
// PUT handler copies CheckInEnabled from the request body to the stored
// competition.  This was a regression: the settings merge path copied Naginata
// but omitted CheckInEnabled, so toggling check-in tracking appeared to save
// but reverted on refresh.
func TestPUTCompetition_CheckInEnabledPersists(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "checkin-persist"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:   cid,
		Name: "CheckIn Test",
		Date: "12-05-2026",
	}))

	// Settings-only PUT, enable check-in tracking.
	body := []byte(`{"id":"checkin-persist","name":"CheckIn Test","date":"12-05-2026","checkInEnabled":true}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	saved, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.True(t, saved.CheckInEnabled, "checkInEnabled must survive a settings-only PUT")

	// Disable it.
	body = []byte(`{"id":"checkin-persist","name":"CheckIn Test","date":"12-05-2026","checkInEnabled":false}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	saved, err = store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.False(t, saved.CheckInEnabled, "checkInEnabled=false must also persist")
}

// TestPUTCompetition_NaginataStartedGuard is a Finding 8 regression test:
// the naginata flag may only be toggled before the competition starts. After
// start, flipping it would add or remove the bronze match while results are
// in flight, corrupting the bracket. The handler must return 400 and leave
// the flag unchanged.
func TestPUTCompetition_NaginataStartedGuard(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "naginata-guard"
	// Start the competition in an active state (not setup/draw-ready).
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       cid,
		Name:     "Naginata Guard",
		Date:     "12-05-2026",
		Format:   state.CompFormatKnockout,
		Naginata: true,
		Status:   state.CompStatusKnockout, // past setup
	}))

	// Attempt to toggle Naginata=false on a started competition.
	body := []byte(`{"id":"naginata-guard","name":"Naginata Guard","date":"12-05-2026","format":"knockout","naginata":false}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equalf(t, http.StatusBadRequest, w.Code,
		"toggling naginata on a started competition must return 400; body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "naginata",
		"error message must mention naginata")

	// Confirm stored value is unchanged.
	saved, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.True(t, saved.Naginata, "naginata flag must remain unchanged after rejected PUT")

	// Toggle is allowed on a setup (not yet started) competition.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       cid,
		Name:     "Naginata Guard",
		Date:     "12-05-2026",
		Format:   state.CompFormatKnockout,
		Naginata: true,
		Status:   state.CompStatusSetup,
	}))
	body = []byte(`{"id":"naginata-guard","name":"Naginata Guard","date":"12-05-2026","format":"knockout","naginata":false}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equalf(t, http.StatusOK, w.Code,
		"toggling naginata on a setup competition must succeed; body: %s", w.Body.String())

	saved, err = store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.False(t, saved.Naginata, "naginata must be updated to false on setup competition")
}

func TestGenerateDrawHandler(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed 8 players in a knockout competition so GenerateDraw can succeed.
	players := make([]domain.Player, 8)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "Dojo"}
	}
	cid := "gen-draw-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "Gen Draw",
		Format: state.CompFormatKnockout,
		Courts: []string{"A"},
		Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(cid, players))

	t.Run("Success: setup → draw-ready", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/generate-draw", nil)
		r.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, saved.Status)
	})

	t.Run("Reject: already draw-ready", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/generate-draw", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Not found: unknown competition", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/no-such-comp/generate-draw", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestCreateCompetitionEngiTeamExclusion pins Copilot #326: engi (individual
// PAIR paradigm) is mutually exclusive with team competitions. The admin UI
// hides the Engi toggle unless kind=individual, but the server must reject the
// contradictory combination so a hand-crafted POST can't create a team+engi
// comp that routes matches to the wrong scorer.
func TestCreateCompetitionEngiTeamExclusion(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("POST team + engi=true returns 400", func(t *testing.T) {
		comp := state.Competition{Name: "Team Engi", Kind: "team", TeamSize: 3, Engi: true}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "engi")
	})

	t.Run("POST individual + engi=true is accepted (not rejected by the exclusion)", func(t *testing.T) {
		comp := state.Competition{Name: "Indiv Engi", Kind: "individual", Engi: true, Format: "knockout", Date: "12-07-2026", StartTime: "09:00"}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.NotContains(t, w.Body.String(), "engi is only valid", "individual+engi must not trip the team-exclusion guard")
	})
}

func TestCreateCompetitionTeamSizeValidation(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("POST team with teamSize=1 returns 400", func(t *testing.T) {
		comp := state.Competition{
			Name:     "Team Size One",
			Kind:     "team",
			TeamSize: 1,
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("POST team with teamSize=0 returns 400", func(t *testing.T) {
		comp := state.Competition{
			Name:     "Team Size Zero",
			Kind:     "team",
			TeamSize: 0,
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("POST kind omitted with teamSize=1 returns 400", func(t *testing.T) {
		comp := state.Competition{
			Name:     "Ambiguous Size One",
			TeamSize: 1,
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("POST team with teamSize=2 succeeds", func(t *testing.T) {
		comp := state.Competition{
			Name:     "Team Size Two",
			Kind:     "team",
			TeamSize: 2,
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

func TestUpdateCompetitionTeamSizeValidation(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "put-team-size-test",
		Name:     "PUT Team Size",
		Kind:     "team",
		TeamSize: 3,
		Status:   state.CompStatusSetup,
	}))

	t.Run("PUT team with teamSize=1 returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":     "PUT Team Size",
			"kind":     "team",
			"teamSize": 1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/put-team-size-test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("PUT team with teamSize=0 returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":     "PUT Team Size",
			"kind":     "team",
			"teamSize": 0,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/put-team-size-test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("PUT team with teamSize=2 succeeds", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":     "PUT Team Size",
			"kind":     "team",
			"teamSize": 2,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/put-team-size-test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestPUTCompetitionAwards(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "awards-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "Awards Comp",
		Status: state.CompStatusComplete,
	}))

	t.Run("happy path: awards persisted + 200 + SSE emitted", func(t *testing.T) {
		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Fighting Spirit", "recipientName": "Alice Yamada", "recipientDojo": "Shinjuku"},
				{"title": "Best Technique", "recipientName": "Bob Tanaka"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		// Parse response competition and check awards.
		var resp state.Competition
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.FightingSpiritAwards, 2)
		assert.Equal(t, "Fighting Spirit", resp.FightingSpiritAwards[0].Title)
		assert.Equal(t, "Alice Yamada", resp.FightingSpiritAwards[0].RecipientName)
		assert.Equal(t, "Shinjuku", resp.FightingSpiritAwards[0].RecipientDojo)
		assert.Equal(t, "Best Technique", resp.FightingSpiritAwards[1].Title)
		assert.Equal(t, "Bob Tanaka", resp.FightingSpiritAwards[1].RecipientName)
		assert.Equal(t, "", resp.FightingSpiritAwards[1].RecipientDojo)

		// Verify persistence.
		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.Len(t, saved.FightingSpiritAwards, 2)
		assert.Equal(t, "Alice Yamada", saved.FightingSpiritAwards[0].RecipientName)

		// Check SSE tournament_updated was broadcast.
		sawUpdate := false
		for range 4 {
			select {
			case msg := <-ch:
				if strings.Contains(msg.payload, `"type":"tournament_updated"`) {
					sawUpdate = true
				}
			default:
			}
		}
		assert.True(t, sawUpdate, "tournament_updated SSE event must be emitted after award save")
	})

	t.Run("clear awards: empty array persisted", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"fightingSpiritAwards": []any{}})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Empty(t, saved.FightingSpiritAwards, "awards must be cleared")
	})

	t.Run("404 for unknown competition", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": "Ghost"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/no-such-comp/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "competition not found")
	})

	t.Run("400: empty title rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "   ", "recipientName": "Alice"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "title is required")
	})

	t.Run("400: empty recipientName rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": ""},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "recipientName is required")
	})

	t.Run("400: over-cap count (21 awards) rejected", func(t *testing.T) {
		awards := make([]map[string]any, 21)
		for i := range awards {
			awards[i] = map[string]any{"title": "Spirit", "recipientName": fmt.Sprintf("Person %d", i)}
		}
		body, _ := json.Marshal(map[string]any{"fightingSpiritAwards": awards})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "fightingSpiritAwards must not exceed")
	})

	t.Run("400: title exceeds max length", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": strings.Repeat("x", MaxLenPlayerName+1), "recipientName": "Alice"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400: recipientName exceeds max length", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": strings.Repeat("x", MaxLenPlayerName+1)},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400: recipientDojo exceeds max length", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": "Alice", "recipientDojo": strings.Repeat("x", MaxLenPlayerDojo+1)},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing elevated password rejected", func(t *testing.T) {
		// Build a router with an active elevated gate: store has a tournament
		// with a non-empty AdminPassword. The gate becomes active and any
		// request without X-Admin-Password must be rejected.
		tmpDir := t.TempDir()
		s, err := state.NewStore(tmpDir)
		require.NoError(t, err)
		// SaveTournament with AdminPassword set activates the fileElevatedVerifier gate.
		require.NoError(t, s.SaveTournament(&state.Tournament{
			Name:          "T",
			Password:      "mainpw",
			Courts:        []string{"A"},
			AdminPassword: "secretadminpw",
		}))
		require.NoError(t, s.SaveCompetition(&state.Competition{
			ID: "awards-elev", Name: "Elev Test",
		}))

		gin.SetMode(gin.TestMode)
		elev := NewFileElevatedVerifier(s)
		hr := gin.New()
		api := hr.Group("/api")
		RegisterCompetitionHandlers(api, s, nil, NewHub(), elev)

		body2, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": "Alice"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/awards-elev/awards", bytes.NewBuffer(body2))
		req.Header.Set("Content-Type", "application/json")
		// Do NOT set X-Admin-Password, should be rejected with 401 (wrong credential).
		hr.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusUnauthorized, w.Code, "missing elevated password must return 401, got %d: %s", w.Code, w.Body.String())
	})

	t.Run("whitespace trimmed from awards before persisting", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "  Spirit  ", "recipientName": "  Carol  ", "recipientDojo": "  Osaka  "},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.Len(t, saved.FightingSpiritAwards, 1)
		assert.Equal(t, "Spirit", saved.FightingSpiritAwards[0].Title)
		assert.Equal(t, "Carol", saved.FightingSpiritAwards[0].RecipientName)
		assert.Equal(t, "Osaka", saved.FightingSpiritAwards[0].RecipientDojo)
	})

	t.Run("400: missing fightingSpiritAwards field (body {}) rejected", func(t *testing.T) {
		// The field is documented required; a body of `{}` must 400 rather
		// than silently clear the list. An explicit [] (covered above) clears.
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "fightingSpiritAwards is required")
	})

	t.Run("400: malformed JSON body rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBufferString("{not json"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400: invalid competition ID rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": "Alice"},
			},
		})
		w := httptest.NewRecorder()
		// An over-length ID (>64 chars) is a single valid path segment that
		// reaches the handler and is rejected by requireValidCompID before
		// any store access.
		req, _ := http.NewRequest("PUT", "/api/competitions/"+strings.Repeat("x", 65)+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestDiscardDrawHandler(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "discard-draw-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "Discard Draw",
		Format: state.CompFormatKnockout,
		Courts: []string{"A"},
		Status: state.CompStatusDrawReady,
	}))

	t.Run("Success: draw-ready → setup", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/competitions/"+cid+"/draw", nil)
		r.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusNoContent, w.Code, "response: %s", w.Body.String())

		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusSetup, saved.Status)
	})

	t.Run("Reject: not draw-ready (setup)", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/competitions/"+cid+"/draw", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Not found: unknown competition", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/competitions/no-such-comp/draw", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestCompetitionCourtsInvariant verifies that every competition resolves to at
// least one court: empty competition courts inherit the tournament's courts so
// generated matches carry a real court label (otherwise the per-court Shiaijo
// operator view at /admin/shiaijo/:court cannot surface them). See
// resolveCompetitionCourts in handlers_tournament.go.
func TestCompetitionCourtsInvariant(t *testing.T) {
	// readBackCourts POSTs/loads a competition and returns its persisted courts.
	postComp := func(t *testing.T, r *gin.Engine, body map[string]any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusCreated, w.Code, "resp: %s", w.Body.String())
		var got map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		return got
	}
	courtsOf := func(comp map[string]any) []string {
		raw, _ := comp["courts"].([]any)
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			out = append(out, v.(string))
		}
		return out
	}

	t.Run("empty courts inherit the tournament's courts", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		// A 4-shiaijo venue, because the inherited list is now validated like
		// any other: a competition may not reach an illegal allocation (3, 5,
		// 6, ...) by inheriting one. The inheritance invariant this test owns
		// is unchanged; only the venue size is, so it stays a legal allocation.
		// The refusal side is covered by
		// TestCreateCompetitionInheritedCourtsMatchExplicit.
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name: "T", Courts: []string{"A", "B", "C", "D"},
		}))
		comp := postComp(t, r, map[string]any{"name": "No Courts Comp"})
		assert.Equal(t, []string{"A", "B", "C", "D"}, courtsOf(comp),
			"a competition created without courts must inherit the tournament's courts")

		// Confirm it is persisted, not just echoed in the response.
		reloaded, err := store.LoadCompetition(comp["id"].(string))
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B", "C", "D"}, reloaded.Courts)
	})

	t.Run("explicit competition courts are preserved", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name: "T", Courts: []string{"A", "B", "C"},
		}))
		comp := postComp(t, r, map[string]any{"name": "One Court Comp", "courts": []string{"B"}})
		assert.Equal(t, []string{"B"}, courtsOf(comp),
			"an explicit court selection must not be overridden by the tournament's courts")
	})

	t.Run("no tournament falls back to a single default court", func(t *testing.T) {
		r, _, _, _, _ := setupTestRouter(t) // setupTestRouter saves no tournament
		comp := postComp(t, r, map[string]any{"name": "Bootstrap Comp"})
		assert.Equal(t, []string{"A"}, courtsOf(comp),
			"with no tournament configured, an empty-courts competition defaults to court A")
	})
}

// TestCheckUniqueCompFields tests the checkUniqueCompFields helper directly.
func TestCheckUniqueCompFields(t *testing.T) {
	_, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	seed := func(id, name, prefix string) {
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: id, Name: name, NumberPrefix: prefix}))
	}

	t.Run("empty prefix is always exempt", func(t *testing.T) {
		seed("pfx-empty-1", "EmptyPfx1", "")
		seed("pfx-empty-2", "EmptyPfx2", "")
		infraErr, valErr := checkUniqueCompFields(store, "NewComp", "", "")
		require.NoError(t, infraErr)
		assert.NoError(t, valErr)
	})

	t.Run("whitespace-only prefix is exempt", func(t *testing.T) {
		infraErr, valErr := checkUniqueCompFields(store, "AnotherNewComp", "  ", "")
		require.NoError(t, infraErr)
		assert.NoError(t, valErr)
	})

	t.Run("no collision for distinct prefixes", func(t *testing.T) {
		seed("pfx-k", "KendoComp", "K")
		infraErr, valErr := checkUniqueCompFields(store, "DistinctName", "M", "")
		require.NoError(t, infraErr)
		assert.NoError(t, valErr)
	})

	t.Run("collision detected (exact prefix match)", func(t *testing.T) {
		seed("pfx-collision", "CollisionComp", "X")
		_, err := checkUniqueCompFields(store, "UniqueName", "X", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "number prefix")
		assert.Contains(t, err.Error(), "CollisionComp")
	})

	t.Run("collision detected (case-insensitive prefix)", func(t *testing.T) {
		seed("pfx-case", "CaseComp", "Y")
		_, err := checkUniqueCompFields(store, "AnotherUnique", "y", "")
		assert.Error(t, err)
	})

	t.Run("excludeID skips own record (PUT update)", func(t *testing.T) {
		seed("pfx-self", "SelfComp", "Z")
		infraErr, valErr := checkUniqueCompFields(store, "SelfComp", "Z", "pfx-self")
		require.NoError(t, infraErr)
		assert.NoError(t, valErr)
	})

	t.Run("collision detected (duplicate name)", func(t *testing.T) {
		seed("name-col", "DuplicateName", "Q")
		_, err := checkUniqueCompFields(store, "DuplicateName", "W", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "competition name")
	})
}

// TestNumberPrefixUniquenessHandlers tests POST and PUT validation via HTTP.
func TestNumberPrefixUniquenessHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed an existing competition with prefix "K".
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "pfx-existing", Name: "Kendo Open", NumberPrefix: "K",
	}))

	post := func(id, name, prefix string) *httptest.ResponseRecorder {
		comp := state.Competition{ID: id, Name: name, NumberPrefix: prefix}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("POST with duplicate prefix is rejected 400", func(t *testing.T) {
		w := post("pfx-dup-post", "Another Kendo", "K")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "number prefix")
		stored, _ := store.LoadCompetition("pfx-dup-post")
		assert.Nil(t, stored, "duplicate-prefix competition must not be persisted")
	})

	t.Run("POST with case-insensitive duplicate prefix is rejected 400", func(t *testing.T) {
		w := post("pfx-dup-case", "Lower Kendo", "k")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("POST with unique prefix succeeds", func(t *testing.T) {
		w := post("pfx-unique", "Men Open", "M")
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("PUT can keep own prefix", func(t *testing.T) {
		comp := state.Competition{ID: "pfx-existing", Name: "Kendo Open Updated", NumberPrefix: "K"}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/pfx-existing", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("PUT with another competition's prefix is rejected 400", func(t *testing.T) {
		// pfx-unique has prefix "M"; try to update pfx-existing to "M".
		comp := state.Competition{ID: "pfx-existing", Name: "Kendo Open", NumberPrefix: "M"}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/pfx-existing", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "number prefix")
	})
}

// TestNormalizePoolConfig_LeagueAndKnockout verifies the normalizePoolConfig
// pure function directly (no HTTP round-trip needed for the unit assertion).
func TestNormalizePoolConfig_LeagueAndKnockout(t *testing.T) {
	cases := []struct {
		name          string
		format        string
		inPoolSize    int
		inPoolWinners int
		wantPoolSize  int
		wantWinners   int
	}{
		{
			name:          "league zeroes poolSize and poolWinners",
			format:        state.CompFormatLeague,
			inPoolSize:    5,
			inPoolWinners: 2,
			wantPoolSize:  0,
			wantWinners:   0,
		},
		{
			name:          "knockout zeroes poolSize and poolWinners",
			format:        state.CompFormatKnockout,
			inPoolSize:    8,
			inPoolWinners: 3,
			wantPoolSize:  0,
			wantWinners:   0,
		},
		{
			name:          "mixed preserves poolSize and poolWinners",
			format:        state.CompFormatMixed,
			inPoolSize:    4,
			inPoolWinners: 2,
			wantPoolSize:  4,
			wantWinners:   2,
		},
		{
			name:          "swiss preserves poolSize and poolWinners",
			format:        state.CompFormatSwiss,
			inPoolSize:    6,
			inPoolWinners: 1,
			wantPoolSize:  6,
			wantWinners:   1,
		},
		{
			name:          "empty format preserves poolSize and poolWinners",
			format:        "",
			inPoolSize:    4,
			inPoolWinners: 2,
			wantPoolSize:  4,
			wantWinners:   2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comp := &state.Competition{
				Format:      tc.format,
				PoolSize:    tc.inPoolSize,
				PoolWinners: tc.inPoolWinners,
			}
			normalizePoolConfig(comp)
			assert.Equal(t, tc.wantPoolSize, comp.PoolSize, "PoolSize")
			assert.Equal(t, tc.wantWinners, comp.PoolWinners, "PoolWinners")
		})
	}
}

// TestPOSTCompetition_LeaguePoolConfigNormalized verifies that creating a
// league competition with poolSize / poolWinners set results in them being
// silently zeroed on disk (API normalises, does not reject).
func TestPOSTCompetition_LeaguePoolConfigNormalized(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	body, _ := json.Marshal(state.Competition{
		Name:        "League Cup",
		Format:      state.CompFormatLeague,
		Kind:        "individual",
		PoolSize:    6,
		PoolWinners: 3,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	// Verify the stored competition has zeroed pool config.
	stored, err := store.LoadCompetition("league-cup")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 0, stored.PoolSize, "league PoolSize must be zeroed on create")
	assert.Equal(t, 0, stored.PoolWinners, "league PoolWinners must be zeroed on create")
}

// TestPUTCompetition_LeaguePoolConfigNormalized verifies that updating a
// league competition with poolSize / poolWinners set results in them being
// silently zeroed on disk (settings PUT normalises, does not reject).
func TestPUTCompetition_LeaguePoolConfigNormalized(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed a league competition (with zeroed pool config on disk).
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "league-edit",
		Name:   "League Edit",
		Format: state.CompFormatLeague,
		Kind:   "individual",
	}))

	// PUT with poolSize / poolWinners set, they must be zeroed on save.
	body, _ := json.Marshal(state.Competition{
		ID:          "league-edit",
		Name:        "League Edit",
		Format:      state.CompFormatLeague,
		Kind:        "individual",
		PoolSize:    7,
		PoolWinners: 2,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/league-edit", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	stored, err := store.LoadCompetition("league-edit")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 0, stored.PoolSize, "league PoolSize must be zeroed on PUT")
	assert.Equal(t, 0, stored.PoolWinners, "league PoolWinners must be zeroed on PUT")
}

// TestPUTCompetition_LeagueTiebreakConfigImmutableAfterStart verifies that the
// league tie-breaker knobs (leagueTiebreakTopN / leagueTwoThirdPlaces) can be
// changed before the competition starts but are rejected (400) once it has
// progressed past setup, changing them mid-league would alter the
// consequential-tie set and which ties already-played tie-breakers resolve.
func TestPUTCompetition_LeagueTiebreakConfigImmutableAfterStart(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Pre-start (setup): a change is accepted.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                 "lp-setup",
		Name:               "LP Setup",
		Format:             state.CompFormatLeague,
		Kind:               "team",
		TeamSize:           5,
		Status:             state.CompStatusSetup,
		LeagueTiebreakTopN: 3,
	}))
	body, _ := json.Marshal(state.Competition{
		ID: "lp-setup", Name: "LP Setup", Format: state.CompFormatLeague,
		Kind: "team", TeamSize: 5, LeagueTiebreakTopN: 4, LeagueTwoThirdPlaces: true,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/lp-setup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err := store.LoadCompetition("lp-setup")
	require.NoError(t, err)
	assert.Equal(t, 4, stored.LeagueTiebreakTopN, "pre-start change must persist")
	assert.True(t, stored.LeagueTwoThirdPlaces)

	// Started (pools): a change is rejected with 400, on-disk value unchanged.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                 "lp-started",
		Name:               "LP Started",
		Format:             state.CompFormatLeague,
		Kind:               "team",
		TeamSize:           5,
		Status:             state.CompStatusPools,
		LeagueTiebreakTopN: 3,
	}))
	body, _ = json.Marshal(state.Competition{
		ID: "lp-started", Name: "LP Started", Format: state.CompFormatLeague,
		Kind: "team", TeamSize: 5, LeagueTiebreakTopN: 4,
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/lp-started", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	stored, err = store.LoadCompetition("lp-started")
	require.NoError(t, err)
	assert.Equal(t, 3, stored.LeagueTiebreakTopN, "started league config must be unchanged")
}

// TestPUTCompetition_MixedPoolConfigPreserved verifies that a mixed
// competition retains its poolSize / poolWinners after a settings PUT.
func TestPUTCompetition_MixedPoolConfigPreserved(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          "mixed-edit",
		Name:        "Mixed Edit",
		Format:      state.CompFormatMixed,
		Kind:        "individual",
		PoolSize:    5,
		PoolWinners: 2,
	}))

	body, _ := json.Marshal(state.Competition{
		ID:          "mixed-edit",
		Name:        "Mixed Edit",
		Format:      state.CompFormatMixed,
		Kind:        "individual",
		PoolSize:    4,
		PoolWinners: 1,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/mixed-edit", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	stored, err := store.LoadCompetition("mixed-edit")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 4, stored.PoolSize, "mixed PoolSize must be preserved")
	assert.Equal(t, 1, stored.PoolWinners, "mixed PoolWinners must be preserved")
}

func TestPUTCompetition_DrawReadyOutputAffectingGate(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "draw-ready-gate"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          cid,
		Name:        "Draw Ready Gate",
		Format:      state.CompFormatMixed,
		Kind:        "individual",
		Courts:      []string{"A"},
		PoolSize:    4,
		PoolWinners: 2,
		Status:      state.CompStatusDrawReady,
	}))

	t.Run("REJECT output-affecting PoolSize change while draw-ready", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":          cid,
			"name":        "Draw Ready Gate",
			"format":      state.CompFormatMixed,
			"kind":        "individual",
			"courts":      []string{"A"},
			"poolSize":    5, // changed from stored 4, output-affecting
			"poolWinners": 2,
			"roundRobin":  false,
			"mirror":      false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code,
			"changing PoolSize while draw-ready must return 409: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "discard",
			"409 body must mention discarding the draw")

		// Status must remain draw-ready, gate must not mutate state.
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, stored.Status)
	})

	t.Run("REJECT zero-value bypass: format set to empty while draw-ready", func(t *testing.T) {
		// Regression for the Copilot finding: the gate must compare effective
		// values directly (no zero/empty sentinels), else a caller can set an
		// output-affecting field TO its empty value to slip past the gate and
		// corrupt the draw. format:"" is accepted by validateCompetitionFormat.
		body, _ := json.Marshal(map[string]any{
			"id":          cid,
			"name":        "Draw Ready Gate",
			"format":      "", // changed from stored "mixed" TO empty, must still 409
			"kind":        "individual",
			"courts":      []string{"A"},
			"poolSize":    4,
			"poolWinners": 2,
			"roundRobin":  false,
			"mirror":      false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code,
			"setting format to empty while draw-ready must return 409 (no zero-value bypass): %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompFormatMixed, stored.Format,
			"format must be unchanged after a rejected zero-value PUT")
		assert.Equal(t, state.CompStatusDrawReady, stored.Status)
	})

	// NumberPrefix and WithZekkenName reach the Excel generator (POST /create),
	// so they are output-affecting and must be gated while draw-ready.
	for _, tc := range []struct {
		name  string
		field string
		value any
	}{
		{"REJECT numberPrefix change while draw-ready", "numberPrefix", "X"},
		{"REJECT withZekkenName change while draw-ready", "withZekkenName", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{
				"id":          cid,
				"name":        "Draw Ready Gate",
				"format":      state.CompFormatMixed,
				"kind":        "individual",
				"courts":      []string{"A"},
				"poolSize":    4,
				"poolWinners": 2,
				"roundRobin":  false,
				"mirror":      false,
				tc.field:      tc.value, // the only output-affecting change
			})
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equalf(t, http.StatusConflict, w.Code,
				"changing %s while draw-ready must return 409: %s", tc.field, w.Body.String())
			stored, err := store.LoadCompetition(cid)
			require.NoError(t, err)
			assert.Equal(t, state.CompStatusDrawReady, stored.Status)
		})
	}

	t.Run("ALLOW cosmetic Name rename while draw-ready", func(t *testing.T) {
		// All output-affecting fields match the stored comp; only Name differs.
		// The gate compares effective values directly (no sentinels): the real
		// client always PUTs the full config, and the omitted fields here
		// (poolSizeMode/poolFormat/teamSize) match the stored zero values, so no
		// output-affecting change is detected and the rename is allowed.
		body, _ := json.Marshal(map[string]any{
			"id":          cid,
			"name":        "Draw Ready Gate Renamed", // cosmetic change, allowed
			"format":      state.CompFormatMixed,
			"kind":        "individual",
			"courts":      []string{"A"},
			"poolSize":    4,     // same as stored
			"poolWinners": 2,     // same as stored
			"roundRobin":  false, // same as stored
			"mirror":      false, // same as stored
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code,
			"renaming while draw-ready must be allowed: %s", w.Body.String())

		// Rename must have persisted.
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "Draw Ready Gate Renamed", stored.Name,
			"cosmetic Name change must persist through the draw-ready gate")
		// Status must remain draw-ready, a rename does not discard the draw.
		assert.Equal(t, state.CompStatusDrawReady, stored.Status)
	})
}

// TestUpdateCompetition_TeamMatchTypeLockedDrawReady verifies that changing
// teamMatchType while the competition is draw-ready returns 409 (A5, GAP 1).
// Changing this field after the draw is generated would make the running
// format inconsistent with the persisted bracket/pool structure.
func TestUpdateCompetition_TeamMatchTypeLockedDrawReady(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "team-match-type-lock"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            cid,
		Name:          "Team Match Type Lock",
		Format:        state.CompFormatMixed,
		Kind:          "team",
		TeamSize:      5,
		TeamMatchType: state.TeamMatchTypeFixed,
		Courts:        []string{"A"},
		PoolSize:      4,
		PoolWinners:   2,
		Status:        state.CompStatusDrawReady,
	}))

	t.Run("REJECT teamMatchType change from fixed to kachinuki while draw-ready", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":            cid,
			"name":          "Team Match Type Lock",
			"format":        state.CompFormatMixed,
			"kind":          "team",
			"teamSize":      5,
			"teamMatchType": state.TeamMatchTypeKachinuki, // changed from "fixed", output-affecting
			"courts":        []string{"A"},
			"poolSize":      4,
			"poolWinners":   2,
			"roundRobin":    false,
			"mirror":        false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code,
			"changing teamMatchType while draw-ready must return 409: %s", w.Body.String())

		// TeamMatchType must remain "fixed"; draw-ready gate must not mutate state.
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeFixed, stored.TeamMatchType,
			"teamMatchType must not change after 409 rejection")
		assert.Equal(t, state.CompStatusDrawReady, stored.Status,
			"status must remain draw-ready after 409 rejection")
	})

	t.Run("ALLOW teamMatchType unchanged while draw-ready", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":            cid,
			"name":          "Team Match Type Lock",
			"format":        state.CompFormatMixed,
			"kind":          "team",
			"teamSize":      5,
			"teamMatchType": state.TeamMatchTypeFixed, // same as stored, no change
			"courts":        []string{"A"},
			"poolSize":      4,
			"poolWinners":   2,
			"roundRobin":    false,
			"mirror":        false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code,
			"no output-affecting change must be allowed while draw-ready: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, stored.Status,
			"status must remain draw-ready after allowed PUT")
	})
}

// TestUpdateCompetition_TeamMatchTypeDrawReadyOmittedAndLegacy verifies that
// the draw-ready teamMatchType guard uses effective-value comparison rather than
// raw wire comparison (Fix 5 regression coverage):
//   - PUT omitting teamMatchType on a draw-ready kachinuki competition must
//     succeed (omitted wire "" means "keep stored", not "change to fixed").
//   - PUT sending "fixed" against a stored legacy "" TeamMatchType on a
//     draw-ready competition must succeed (both represent the same effective
//     type: fixed).
//
// The guard-still-works case (explicit kachinuki against stored fixed returns
// 409) is already covered by TestUpdateCompetition_TeamMatchTypeLockedDrawReady
// and is not duplicated here.
func TestUpdateCompetition_TeamMatchTypeDrawReadyOmittedAndLegacy(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	// Subtest 1: omitting teamMatchType on a draw-ready kachinuki competition
	// must not trigger a 409 (false positive from raw "" != "kachinuki").
	t.Run("ALLOW omitted teamMatchType on draw-ready kachinuki competition", func(t *testing.T) {
		const cid = "draw-ready-kachinuki-omit"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:            cid,
			Name:          "Draw Ready Kachinuki",
			Format:        state.CompFormatMixed,
			Kind:          "team",
			TeamSize:      5,
			TeamMatchType: state.TeamMatchTypeKachinuki,
			Courts:        []string{"A"},
			PoolSize:      4,
			PoolWinners:   2,
			Status:        state.CompStatusDrawReady,
		}))

		// teamMatchType intentionally omitted so the wire value decodes to ""
		// (json omitempty). The effective meaning is "keep the stored value".
		body, _ := json.Marshal(map[string]any{
			"id":          cid,
			"name":        "Draw Ready Kachinuki",
			"format":      state.CompFormatMixed,
			"kind":        "team",
			"teamSize":    5,
			"courts":      []string{"A"},
			"poolSize":    4,
			"poolWinners": 2,
			"roundRobin":  false,
			"mirror":      false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code,
			"omitted teamMatchType on a draw-ready kachinuki competition must not return 409: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeKachinuki, stored.TeamMatchType,
			"TeamMatchType must remain kachinuki when the field was omitted from the PUT body")
		assert.Equal(t, state.CompStatusDrawReady, stored.Status,
			"status must remain draw-ready after the allowed PUT")
	})

	// Subtest 2: sending "fixed" against a stored legacy "" TeamMatchType on a
	// draw-ready competition must not 409 (both are the same effective type).
	t.Run("ALLOW explicit fixed against stored legacy empty TeamMatchType while draw-ready", func(t *testing.T) {
		const cid = "draw-ready-legacy-empty"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:            cid,
			Name:          "Draw Ready Legacy Empty",
			Format:        state.CompFormatMixed,
			Kind:          "team",
			TeamSize:      5,
			TeamMatchType: "", // legacy on-disk empty == fixed
			Courts:        []string{"A"},
			PoolSize:      4,
			PoolWinners:   2,
			Status:        state.CompStatusDrawReady,
		}))

		body, _ := json.Marshal(map[string]any{
			"id":            cid,
			"name":          "Draw Ready Legacy Empty",
			"format":        state.CompFormatMixed,
			"kind":          "team",
			"teamSize":      5,
			"teamMatchType": state.TeamMatchTypeFixed, // "fixed" is semantically equal to legacy ""
			"courts":        []string{"A"},
			"poolSize":      4,
			"poolWinners":   2,
			"roundRobin":    false,
			"mirror":        false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code,
			"explicit fixed against stored legacy empty must not return 409 while draw-ready: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeFixed, stored.TeamMatchType,
			"TeamMatchType must be persisted as fixed after a successful PUT")
		assert.Equal(t, state.CompStatusDrawReady, stored.Status,
			"status must remain draw-ready after the allowed PUT")
	})
}

// ---------------------------------------------------------------------------
// GET /competitions/:id/chusen-candidates
// ---------------------------------------------------------------------------

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
func TestChusenCandidates_IncludesTeamIdentity(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-identity"

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
	// Fully drawn round robin puts all three teams in one tied group,
	// mirroring engine's TestChusenCandidates_CycleNeedsChusen.
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

	// Score the injected daihyosen bouts into a genuine cycle (no chusen
	// override yet): Alpha>Beta, Beta>Gamma, Gamma>Alpha.
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

// TestUpdateCompetition_TeamMatchTypeLockedWhenStarted verifies that changing
// teamMatchType on a STARTED competition (status past setup, e.g. knockout)
// returns 409. Flipping fixed <-> kachinuki mid-tournament would desync the
// recorded bout structure from the scoring/advancement paradigm. Sibling of
// the Naginata/Engi started-guards; the draw-ready lock alone left started
// comps editable (UAT finding).
func TestUpdateCompetition_TeamMatchTypeLockedWhenStarted(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "team-match-type-started-lock"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            cid,
		Name:          "Team Match Type Started Lock",
		Format:        state.CompFormatMixed,
		Kind:          "team",
		TeamSize:      5,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		Courts:        []string{"A"},
		PoolSize:      4,
		PoolWinners:   2,
		Status:        state.CompStatusKnockout,
	}))

	basePayload := func(tmt state.TeamMatchType) map[string]any {
		return map[string]any{
			"id":            cid,
			"name":          "Team Match Type Started Lock",
			"format":        state.CompFormatMixed,
			"kind":          "team",
			"teamSize":      5,
			"teamMatchType": tmt,
			"courts":        []string{"A"},
			"poolSize":      4,
			"poolWinners":   2,
			"roundRobin":    false,
			"mirror":        false,
		}
	}
	put := func(payload map[string]any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("REJECT kachinuki to fixed flip on a started comp", func(t *testing.T) {
		w := put(basePayload(state.TeamMatchTypeFixed))
		assert.Equal(t, http.StatusConflict, w.Code,
			"changing teamMatchType on a started comp must return 409: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeKachinuki, stored.TeamMatchType,
			"teamMatchType must not change after 409 rejection")
	})

	t.Run("ALLOW save with unchanged teamMatchType on a started comp", func(t *testing.T) {
		w := put(basePayload(state.TeamMatchTypeKachinuki))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("ALLOW save that omits teamMatchType (keep stored value)", func(t *testing.T) {
		payload := basePayload("")
		delete(payload, "teamMatchType")
		w := put(payload)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeKachinuki, stored.TeamMatchType,
			"an omitted teamMatchType must keep the stored value, never reset it")
	})
}

// TestUpdateCompetition_LegacyDurationMigrates is the end-to-end guard for the
// retirement of the whole-minute duration fields. A competition stored as
// `match_duration: 15` is migrated to seconds when the store loads it and
// clamped into the shiai band, so by the time it reaches the API it is already
// in range. That is what lets the band be a flat check with nothing to
// grandfather, and it is why an unrelated edit (a rename) can never be blocked
// by a duration the operator did not touch.
func TestUpdateCompetition_LegacyDurationMigrates(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// A competition configured at 15 minutes, before per-phase seconds existed.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            "legacy-duration",
		Name:          "Legacy Duration",
		Format:        state.CompFormatMixed,
		PoolSize:      3,
		Courts:        []string{"A"},
		MatchDuration: 15,
		Status:        state.CompStatusSetup,
	}))

	t.Run("the stored legacy value is migrated on read and survives intact", func(t *testing.T) {
		got, err := store.LoadCompetition("legacy-duration")
		require.NoError(t, err)
		assert.Equal(t, 900, got.PoolMatchDurationSeconds,
			"15 minutes is inside the 1:00-60:00 band, so it must carry over unchanged")
		assert.Zero(t, got.MatchDuration, "the retired field must be cleared")
	})

	put := func(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/legacy-duration", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	base := func() map[string]any {
		return map[string]any{
			"name": "Legacy Duration", "format": "mixed", "poolSize": 3,
			"courts":                   []string{"A"},
			"poolMatchDurationSeconds": 900,
		}
	}

	t.Run("renaming is never blocked by the migrated duration", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		w := put(t, body)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-duration")
		require.NoError(t, err)
		assert.Equal(t, "Renamed Legacy", got.Name)
		assert.Equal(t, 900, got.PoolMatchDurationSeconds)
	})

	t.Run("an out-of-band duration is rejected", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		body["poolMatchDurationSeconds"] = 5400 // 90 minutes, above the 60:00 ceiling
		w := put(t, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "between 60 and 3600 seconds")
	})

	t.Run("a fat-fingered 3-second duration is rejected", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		body["poolMatchDurationSeconds"] = 3
		w := put(t, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "between 60 and 3600 seconds")
	})

	t.Run("an in-band duration saves", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		body["poolMatchDurationSeconds"] = 150
		w := put(t, body)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-duration")
		require.NoError(t, err)
		assert.Equal(t, 150, got.PoolMatchDurationSeconds)
	})

	t.Run("clearing the duration resets it to the scheduler default", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		body["poolMatchDurationSeconds"] = 0
		w := put(t, body)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-duration")
		require.NoError(t, err)
		assert.Zero(t, got.PoolMatchDurationSeconds, "0 must persist as unset, not fall back to the old value")
	})
}

// TestUpdateCompetition_DurationChangeKeepsDraw pins the requirement that
// retiming a competition never invalidates a generated draw: match duration
// feeds the scheduler only, and reaches neither the pool allocation nor the
// bracket, so it is deliberately absent from the draw-ready outputAffecting set.
func TestUpdateCompetition_DurationChangeKeepsDraw(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                       "draw-ready-duration",
		Name:                     "Draw Ready Duration",
		Format:                   state.CompFormatMixed,
		PoolSize:                 3,
		Courts:                   []string{"A"},
		PoolMatchDurationSeconds: 150,
		Status:                   state.CompStatusDrawReady,
	}))

	body, _ := json.Marshal(map[string]any{
		"name": "Draw Ready Duration", "format": "mixed", "poolSize": 3,
		"courts":                   []string{"A"},
		"poolMatchDurationSeconds": 240,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/draw-ready-duration", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "changing duration must not require discarding the draw: %s", w.Body.String())

	got, err := store.LoadCompetition("draw-ready-duration")
	require.NoError(t, err)
	assert.Equal(t, 240, got.PoolMatchDurationSeconds)
	assert.Equal(t, state.CompStatusDrawReady, got.Status, "the draw must survive a duration change")
}

// TestCompetitionAPI_RejectsOutOfBandNeverClamps pins the separation between the
// two duration jobs: the API REFUSES an out-of-band value, while the store PINS
// one that is already on disk. Getting these backwards is a real hazard, because
// POST used to call state.ApplyCompetitionDefaults on the inbound body BEFORE
// validation; had the band clamp been added there, an out-of-band POST would
// have been silently rewritten to the ceiling and returned 201.
func TestCompetitionAPI_RejectsOutOfBandNeverClamps(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("POST above the ceiling is rejected, not clamped", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"name": "Too Long", "poolMatchDurationSeconds": 99999})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code,
			"an out-of-band duration must 400; silently clamping it to the ceiling would be the exact behaviour this control exists to prevent")
		assert.Contains(t, w.Body.String(), "between 60 and 3600 seconds")
	})

	t.Run("POST below the floor is rejected, not clamped", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"name": "Too Short", "poolMatchDurationSeconds": 3})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("a hand-edited out-of-band config stays fully editable", func(t *testing.T) {
		// The store pins it on load, so the flat band check the API performs
		// never sees an out-of-band value and unrelated edits keep working.
		require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "competitions", "hand-edited"), 0o700))
		require.NoError(t, os.WriteFile(
			filepath.Join(tempDir, "competitions", "hand-edited", "config.md"),
			[]byte("---\nid: hand-edited\nname: Hand Edited\nformat: mixed\npool_size: 3\ncourts:\n    - A\npool_match_duration_seconds: 99999\nstatus: setup\n---\n"),
			0o600))

		loaded, err := store.LoadCompetition("hand-edited")
		require.NoError(t, err)
		require.Equal(t, state.MaxMatchDurationSeconds, loaded.PoolMatchDurationSeconds)

		// Rename it, echoing back the pinned value the way the settings form does.
		b, _ := json.Marshal(map[string]any{
			"name": "Renamed", "format": "mixed", "poolSize": 3, "courts": []string{"A"},
			"poolMatchDurationSeconds": state.MaxMatchDurationSeconds,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/hand-edited", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	})
}

// TestUpdateCompetition_CourtStillRunningMatches pins the competition-level twin
// of the tournament's orphaned-shiaijo guard: a competition cannot drop a
// shiaijo its own live matches are still assigned to, because such a match has
// no operator view left to be run from.
func TestUpdateCompetition_CourtStillRunningMatches(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Court Drop", Date: "20-08-2026", Courts: []string{"A", "B"}, Password: "pw",
	}))

	const cid = "court-drop"
	save := func(status state.CompetitionStatus) {
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Court Drop", Format: state.CompFormatMixed,
			Courts: []string{"A", "B"}, PoolSize: 4, PoolWinners: 2, Status: status,
		}))
	}
	put := func(courts []string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{
			"id": cid, "name": "Court Drop", "format": state.CompFormatMixed,
			"courts": courts, "poolSize": 4, "poolWinners": 2,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("REJECT dropping a shiaijo a scheduled match is on", func(t *testing.T) {
		save(state.CompStatusPools)
		require.NoError(t, store.SavePoolMatches(cid, []state.MatchResult{
			{ID: "Pool A-1", Court: "A", Status: state.MatchStatusCompleted},
			{ID: "Pool B-1", Court: "B", Status: state.MatchStatusScheduled},
		}))

		w := put([]string{"A"})
		assert.Equal(t, http.StatusBadRequest, w.Code,
			"dropping shiaijo B while a bout is scheduled on it must be refused: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "shiaijo B",
			"the refusal must name the shiaijo the operator has to clear")

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B"}, stored.Courts, "the allocation must not change after the refusal")
	})

	t.Run("ALLOW dropping a shiaijo whose bouts are all fought", func(t *testing.T) {
		save(state.CompStatusPools)
		require.NoError(t, store.SavePoolMatches(cid, []state.MatchResult{
			{ID: "Pool A-1", Court: "A", Status: state.MatchStatusScheduled},
			{ID: "Pool B-1", Court: "B", Status: state.MatchStatusCompleted},
		}))

		w := put([]string{"A"})
		assert.Equal(t, http.StatusOK, w.Code,
			"a shiaijo whose bouts are all fought must be free to drop: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, []string{"A"}, stored.Courts)
	})

	t.Run("ALLOW the same edit before the competition starts", func(t *testing.T) {
		save(state.CompStatusSetup)
		require.NoError(t, store.SavePoolMatches(cid, []state.MatchResult{
			{ID: "Pool B-1", Court: "B", Status: state.MatchStatusScheduled},
		}))

		w := put([]string{"A"})
		assert.Equal(t, http.StatusOK, w.Code,
			"nothing is live before the draw, so the allocation is still the operator's to set: %s", w.Body.String())
	})
}

// TestCompetitionPutWritesCanonicalSeedIdentity pins the canonical form
// extractSeeds writes. seeds.csv is resolved against the roster by exact
// (name, dojo) key and is NOT canonicalized on load, while participants.csv IS
// (CreatePlayersFromRecords Title-cases the name and TrimSpaces every field on
// every parse). Writing the raw request name therefore produced a seed row
// that could not resolve against its own participant.
//
// This is the operator-visible half of the bulk-endpoint casing bug: the admin
// roster box's "Apply changes" goes through PUT /competitions/:id (deriving
// seeds.csv fresh from each player's Seed field), NOT the bulk participants
// POST, so retyping a seeded competitor's name in different casing returned
// 200 and silently dropped the seed - the panel just showed "0 seeded".
// Confirmed in the browser before and after the fix.
func TestCompetitionPutWritesCanonicalSeedIdentity(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const compID = "comp-seed-canonical"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Seed Canonical Test", Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Van Der Berg", Dojo: "DojoA"},
		{Name: "Bob Jones", Dojo: "Tora"},
	}))
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: "Van Der Berg", Dojo: "DojoA", SeedRank: 1},
	}))

	// The roster box on "Apply changes": the whole competition, one seeded
	// name retyped in lower case with a padded dojo, seed rank carried along.
	body, _ := json.Marshal(map[string]any{
		"id": compID, "name": "Seed Canonical Test", "status": "setup",
		"players": []map[string]any{
			{"name": "van der berg", "dojo": "  DojoA  ", "seed": 1},
			{"name": "Bob Jones", "dojo": "Tora"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "testpass")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	raw, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	require.Len(t, raw, 1)
	assert.Equal(t, "Van Der Berg", raw[0].Name, "seeds.csv must hold the canonical name the roster reads back as")
	assert.Equal(t, "DojoA", raw[0].Dojo, "both halves of the seed key are canonicalized, not just the name")

	// The point of the canonical form: the seed still resolves onto its own
	// participant on the next load.
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	seeded := map[string]int{}
	for _, p := range players {
		if p.Seed > 0 {
			seeded[p.Name] = p.Seed
		}
	}
	assert.Equal(t, map[string]int{"Van Der Berg": 1}, seeded,
		"a casing-only retype must not silently orphan the seed")
}

// TestUpdateCompetition_FormatKindLockedWhenStarted verifies bc-symm's
// post-start gate on Format and Kind (mirroring the existing
// Naginata/Engi/TeamMatchType started-guards): once a competition has left
// setup status, current.Format and current.Kind steer live scoring paths
// (comp.Format is read in engine/scoring_tx.go's mixed-pool rescoring gate;
// comp.Kind is read throughout internal/engine's scheduling/scoring/PDF
// paths), so flipping either after results already exist must be refused.
//
// Pre-fix, ONLY the draw-ready 409 protected these two fields; a competition
// that had progressed past draw-ready (pools/knockout/completed) accepted a
// format or kind change with a plain 200.
//
// The third subtest is the regression that matters most: the SPA has never
// had editors for format/kind and echoes the stored values back unchanged on
// every single settings save, so the gate must fire ONLY on an actual
// change, never unconditionally.
func TestUpdateCompetition_FormatKindLockedWhenStarted(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "format-kind-started-lock"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          cid,
		Name:        "Format Kind Started Lock",
		Format:      state.CompFormatMixed,
		Kind:        "individual",
		Courts:      []string{"A"},
		PoolSize:    4,
		PoolWinners: 2,
		Status:      state.CompStatusKnockout, // started
	}))

	basePayload := func(overrides map[string]any) map[string]any {
		p := map[string]any{
			"id":          cid,
			"name":        "Format Kind Started Lock",
			"format":      state.CompFormatMixed,
			"kind":        "individual",
			"courts":      []string{"A"},
			"poolSize":    4,
			"poolWinners": 2,
		}
		for k, v := range overrides {
			p[k] = v
		}
		return p
	}
	put := func(payload map[string]any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("REJECT format change on a started comp", func(t *testing.T) {
		w := put(basePayload(map[string]any{"format": state.CompFormatLeague}))
		assert.Equal(t, http.StatusConflict, w.Code,
			"changing format on a started comp must return 409: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompFormatMixed, stored.Format, "format must not change after 409 rejection")
	})

	t.Run("REJECT kind change on a started comp", func(t *testing.T) {
		w := put(basePayload(map[string]any{"kind": "team", "teamSize": 5}))
		assert.Equal(t, http.StatusConflict, w.Code,
			"changing kind on a started comp must return 409: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, "individual", stored.Kind, "kind must not change after 409 rejection")
	})

	t.Run("ALLOW save that echoes unchanged format/kind on a started comp", func(t *testing.T) {
		w := put(basePayload(nil))
		assert.Equal(t, http.StatusOK, w.Code,
			"a save that does not change format/kind must succeed on a started comp: %s", w.Body.String())
	})
}

// TestUpdateCompetition_KachinukiZeroTeamSizeCannotPersist verifies bc-symm's
// fix for a settings PUT that flips a team-kachinuki competition to
// individual (teamSize 0) while omitting teamMatchType. Pre-fix,
// state.ValidateTeamMatchType ran on the WIRE teamMatchType ("" passes
// vacuously for any teamSize) BEFORE the "inherit stored teamMatchType when
// omitted" step, so the write passed validation, then inherited the stored
// "kachinuki" and persisted it alongside teamSize 0 -- exactly the pair
// ValidateTeamMatchType exists to forbid (inert at runtime, since
// IsKachinuki() requires teamSize >= 2, but every LATER settings PUT would
// echo that same pair back on the wire and now fail the WIRE-value check,
// locking the operator out of the settings screen entirely).
//
// The re-check COERCES rather than rejects, which is what this asserts. The
// value it judges is one the write INHERITED, never one a client sent (a
// sent value has already met the wire check in the same branch), so a 400
// would answer the operator's PUT with an error naming a field their
// request never mentioned. Resetting to the fixed default lands exactly
// the record the operator asked for -- an individual competition -- and
// leaves nothing illegal on disk. See the sibling test below for the case
// where rejecting would have been an outright lockout.
func TestUpdateCompetition_KachinukiZeroTeamSizeCannotPersist(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "kachinuki-zero-lockout"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            cid,
		Name:          "Kachinuki Zero Lockout",
		Format:        state.CompFormatMixed,
		Kind:          "team",
		TeamSize:      5,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		Courts:        []string{"A"},
		PoolSize:      4,
		PoolWinners:   2,
		Status:        state.CompStatusSetup,
	}))

	// Flip kind to individual and zero teamSize, omitting teamMatchType --
	// exactly what a kind editor does once it stops presenting the
	// team-only controls.
	body, _ := json.Marshal(map[string]any{
		"id":          cid,
		"name":        "Kachinuki Zero Lockout",
		"format":      state.CompFormatMixed,
		"kind":        "individual",
		"teamSize":    0,
		"courts":      []string{"A"},
		"poolSize":    4,
		"poolWinners": 2,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"the operator asked for an individual competition and may have it; the inherited kachinuki is the server's problem to resolve, not theirs: %s", w.Body.String())

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, state.TeamMatchTypeFixed, stored.TeamMatchType,
		"the pair (kachinuki, teamSize 0) is what ValidateTeamMatchType forbids; it must not reach disk by way of the inherit")
	assert.Equal(t, 0, stored.TeamSize)
	assert.Equal(t, "individual", stored.Kind)

	require.NoError(t, state.ValidateTeamMatchType(stored.TeamMatchType, stored.TeamSize),
		"whatever the route, the record left behind must satisfy the rule the re-check enforces")
}

// The lockout the coercion above exists to prevent, driven from the state a
// PRE-FIX server could leave on disk: (teamMatchType kachinuki, teamSize 0).
// Rejecting an inherited value there is not a one-off 400 -- it is
// permanent. Every later settings PUT from a client that omits
// teamMatchType (a non-SPA API caller, or a browser tab cached from before
// the field shipped) inherits the same stored pair and fails the same way,
// over a field the request never mentions, with nothing on the settings
// screen able to repair it.
//
// CLAUDE.md states the rule this pins: a write answers for what it
// introduces, not for what it inherited. Same shape as stripInvalidHantei
// (engine/scoring.go), which strips an inherited mark and logs rather than
// blaming the operator for state already on disk.
func TestUpdateCompetition_InheritedIllegalTeamMatchTypeIsRepairedNotRejected(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "kachinuki-zero-stored"
	// SaveCompetition writes straight through, so this is the record a
	// pre-fix PUT would have produced -- not a shape any handler accepts
	// today.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            cid,
		Name:          "Stored Illegal Pair",
		Format:        state.CompFormatMixed,
		Kind:          "individual",
		TeamSize:      0,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		Courts:        []string{"A"},
		PoolSize:      4,
		PoolWinners:   2,
		Status:        state.CompStatusSetup,
	}))

	// A rename. Nothing about teams, nothing about match format.
	body, _ := json.Marshal(map[string]any{
		"id":          cid,
		"name":        "Renamed",
		"format":      state.CompFormatMixed,
		"kind":        "individual",
		"teamSize":    0,
		"courts":      []string{"A"},
		"poolSize":    4,
		"poolWinners": 2,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"a rename must not 400 over a stored value the request never mentioned: %s", w.Body.String())

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", stored.Name, "the edit the operator actually asked for must land")
	assert.Equal(t, state.TeamMatchTypeFixed, stored.TeamMatchType,
		"and the write repairs what it inherited on its way past, so the NEXT save is an ordinary one")
}

// TestUpdateCompetition_MixedFormatRequiresUsablePoolSize verifies bc-symm's
// fix for a settings PUT that flips format to mixed without a usable pool
// size. normalizePoolConfig zeroes PoolSize/PoolWinners on the way TO
// league/knockout, but pre-fix nothing required a pool size on the way BACK
// to mixed, so a league/knockout -> mixed switch with an omitted/zero
// poolSize stored PoolSize 0 and the failure only surfaced much later at
// draw time (engine/pools.go: "pool size must be at least 1").
func TestUpdateCompetition_MixedFormatRequiresUsablePoolSize(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "league-to-mixed-no-poolsize"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "League To Mixed",
		Format: state.CompFormatLeague,
		Courts: []string{"A"},
		Status: state.CompStatusSetup,
	}))

	t.Run("REJECT flip to mixed with no usable pool size", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":     cid,
			"name":   "League To Mixed",
			"format": state.CompFormatMixed,
			"courts": []string{"A"},
			// poolSize intentionally omitted -> decodes to 0
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code,
			"a flip to mixed with no usable pool size must be rejected at write time: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompFormatLeague, stored.Format, "rejected write must not touch the stored record")
	})

	t.Run("ALLOW flip to mixed WITH a usable pool size", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":       cid,
			"name":     "League To Mixed",
			"format":   state.CompFormatMixed,
			"courts":   []string{"A"},
			"poolSize": 4,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompFormatMixed, stored.Format)
		assert.Equal(t, 4, stored.PoolSize)
	})
}

// TestCreateCompetition_MixedFormatRequiresUsablePoolSize is the POST twin of
// TestUpdateCompetition_MixedFormatRequiresUsablePoolSize above, and the two
// doors deliberately differ: the PUT guard is change-scoped to protect a
// stored value from a lockout, while POST authors a brand-new record with
// nothing to inherit and so runs unconditionally.
//
// What POST does NOT do is refuse an OMITTED pool size. That body
// (`{"name","format":"mixed","courts"}`) returned 201 for the whole life of
// the endpoint, four other tests in this package send it, and the identical
// competition arriving through /api/tournament/import lands fine because
// that door has always defaulted PoolSize. Both doors now read the same
// defaultPoolSize constant, so an omission cannot succeed at one and fail
// at the other. The guard keeps the case a caller STATES and the draw could
// never build.
func TestCreateCompetition_MixedFormatRequiresUsablePoolSize(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("DEFAULT an omitted pool size rather than refusing the create", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":     "create-mixed-no-poolsize",
			"name":   "Create Mixed No PoolSize",
			"format": state.CompFormatMixed,
			"courts": []string{"A"},
			// poolSize intentionally omitted -> decodes to 0
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code,
			"an omitted pool size must be defaulted, as /api/tournament/import has always defaulted it: %s", w.Body.String())

		stored, err := store.LoadCompetition("create-mixed-no-poolsize")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, defaultPoolSize, stored.PoolSize,
			"and it must be the SAME default the import door uses, or the two authoring doors answer one body differently")
	})

	t.Run("REJECT a stated pool size the draw could never build", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":       "create-mixed-negative-poolsize",
			"name":     "Create Mixed Negative PoolSize",
			"format":   state.CompFormatMixed,
			"courts":   []string{"A"},
			"poolSize": -1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code,
			"a stated impossible pool size is the caller's own value and must be refused: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "mixed format requires a pool size of at least 1")

		stored, err := store.LoadCompetition("create-mixed-negative-poolsize")
		require.NoError(t, err)
		assert.Nil(t, stored, "a rejected create must persist nothing")
	})

	// The default is scoped to "mixed" on this door: normalizePoolConfig
	// runs first and zeroes PoolSize for league/knockout, and it must stay
	// zeroed -- a bare bracket has no pool phase to size.
	t.Run("does not default a pool size for a format that has no pool phase", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":     "create-knockout-no-poolsize",
			"name":   "Create Knockout No PoolSize",
			"format": state.CompFormatKnockout,
			"courts": []string{"A"},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		stored, err := store.LoadCompetition("create-knockout-no-poolsize")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, 0, stored.PoolSize, "normalizePoolConfig zeroed this; the default must not undo it")
	})

	t.Run("ALLOW create with mixed format and a usable pool size", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":       "create-mixed-with-poolsize",
			"name":     "Create Mixed With PoolSize",
			"format":   state.CompFormatMixed,
			"courts":   []string{"A"},
			"poolSize": 4,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		stored, err := store.LoadCompetition("create-mixed-with-poolsize")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, 4, stored.PoolSize)
	})
}

// TestCreateCompetition_RejectsIllegalKind pins the POST-side half of
// state.ValidateCompetitionKind. POST authors a brand-new record, so unlike
// the PUT guard (change-scoped, see TestUpdateCompetition_KindGuard below)
// nothing here is inherited and the check runs unconditionally: a
// hand-crafted "banana" kind must never reach disk, since Kind == "team" is
// the marker engine code uses to route team-vs-individual scoring/generation
// (ValidateCompetitionTeamSize's doc comment names the same split) and an
// unrecognised value silently fell through every one of those checks and ran
// as individual with no error anywhere.
func TestCreateCompetition_RejectsIllegalKind(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("REJECT an unrecognised kind", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":   "create-illegal-kind",
			"name": "Create Illegal Kind",
			"kind": "banana",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code,
			"an unrecognised kind must be rejected on create: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "unknown kind")

		stored, err := store.LoadCompetition("create-illegal-kind")
		require.NoError(t, err)
		assert.Nil(t, stored, "a rejected create must persist nothing")
	})

	t.Run("ALLOW empty kind (legacy/import default meaning individual)", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":   "create-empty-kind",
			"name": "Create Empty Kind",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		stored, err := store.LoadCompetition("create-empty-kind")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "", stored.Kind)
	})
}

// TestUpdateCompetition_KindGuard is the CRITICAL SCOPING pair for
// state.ValidateCompetitionKind on PUT: reject an illegal kind ONLY when the
// value actually CHANGES, never when an already-illegal stored value is
// merely echoed back.
//
// The lockout sub-test is the important one. The settings page always
// round-trips the stored kind on every save (it has no reason to touch a
// field it never renders an editor for, and even once it does, an unrelated
// save still echoes the CURRENT value back). A competition whose config.md
// was hand-edited (or written before this guard existed) to carry an
// unrecognised kind must stay editable for every unrelated field, or an
// unconditional check here would 400 every future save and permanently lock
// the operator out of the settings screen -- the same lockout shape
// CLAUDE.md's "a write answers for what it introduces, not for what it
// inherited" rule exists to prevent, and one this codebase has already had
// to fix twice.
func TestUpdateCompetition_KindGuard(t *testing.T) {
	t.Run("REJECT a change to an illegal kind", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		const cid = "kind-guard-change"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:     cid,
			Name:   "Kind Guard Change",
			Kind:   "individual",
			Status: state.CompStatusSetup,
		}))

		body, _ := json.Marshal(map[string]any{
			"id":   cid,
			"name": "Kind Guard Change",
			"kind": "banana",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code,
			"changing to an unrecognised kind must be rejected: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "unknown kind")

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, "individual", stored.Kind, "the rejected write must not land")
	})

	t.Run("ALLOW a save that echoes an ALREADY-illegal stored kind unchanged (lockout regression)", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		const cid = "kind-guard-lockout"
		// Simulates a hand-edited config.md, or a record written before this
		// guard existed: SaveCompetition writes straight to disk, bypassing
		// the handler's own validation, exactly the way a pre-existing
		// on-disk illegal value would arise in production.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:        cid,
			Name:      "Kind Guard Lockout",
			Kind:      "banana",
			Date:      "12-05-2026",
			StartTime: "09:00",
			Status:    state.CompStatusSetup,
		}))

		// The settings page echoes the stored (illegal) kind back verbatim
		// while editing an entirely unrelated field (StartTime here).
		body, _ := json.Marshal(map[string]any{
			"id":        cid,
			"name":      "Kind Guard Lockout",
			"kind":      "banana",
			"date":      "12-05-2026",
			"startTime": "10:30",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code,
			"a save that does not change an already-illegal kind must succeed: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "banana", stored.Kind, "the illegal kind is preserved, not silently corrected")
		assert.Equal(t, "10:30", stored.StartTime, "the unrelated edit must land")
	})
}
