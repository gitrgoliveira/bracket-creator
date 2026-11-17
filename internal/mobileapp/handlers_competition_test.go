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

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoolHasPlayerID is a direct unit test of the validator. ID-only
// (operator ruling bc-pnum): playerId is REQUIRED and is the ONLY selector --
// the old playerName/playerDojo narrowing (bc-cse FIX 1/FIX 2) is gone, since
// a pool-rank override is a record resolved by id only.
func TestPoolHasPlayerID(t *testing.T) {
	players := []domain.Player{
		{ID: "member-a", Name: "Member A", Dojo: "Dojo A"},
		{ID: "member-b", Name: "Member B", Dojo: "Dojo B"},
	}

	t.Run("empty playerId is rejected", func(t *testing.T) {
		err := poolHasPlayerID(players, "")
		require.Error(t, err, "playerId is required")
	})

	t.Run("off-roster playerId is rejected, not silently empty", func(t *testing.T) {
		err := poolHasPlayerID(players, "no-such-id")
		require.Error(t, err, "an off-roster playerId must error rather than resolve to an unreadable empty key")
		assert.Contains(t, err.Error(), "no-such-id")
	})

	t.Run("on-roster playerId is accepted", func(t *testing.T) {
		err := poolHasPlayerID(players, "member-a")
		require.NoError(t, err)
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
		comp := state.Competition{ID: "rank-comp", Status: state.CompStatusPools, Kind: "team", TeamSize: 2}
		store.SaveCompetition(&comp)
		// Seed a pool so the new pool-size validation can find pool-1
		// (rank within a pool is bounded by len(pool.Players)). playerId is
		// required (operator ruling bc-pnum), so Player 1 carries a real id.
		require.NoError(t, store.SavePools("rank-comp", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{ID: "rank-comp-p1", Name: "Player 1", Dojo: "Dojo Player 1"}, {Name: "Player 2", Dojo: "Dojo Player 2"}, {Name: "Player 3", Dojo: "Dojo Player 3"},
			}},
		}))

		reqBody, _ := json.Marshal(map[string]any{
			"playerId":   "rank-comp-p1",
			"playerName": "Player 1",
			"rank":       1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-comp/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("Override Rank Ignores PlayerName Padding, Selection Is By PlayerId", func(t *testing.T) {
		// playerId selects (operator ruling bc-pnum); playerName is accepted
		// only for backward-compatible display and never affects the
		// override's identity key, which is helper.CompetitorKey(id, name,
		// dojo) and degrades to "id:"+id once an id is known -- so a padded
		// playerName must have no effect on where the override lands.
		comp := state.Competition{ID: "rank-trim-comp", Status: state.CompStatusPools, Kind: "team", TeamSize: 2}
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
			"playerId":   "trim-player-id",
			"playerName": "  Player Trim  ",
			"rank":       7,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-trim-comp/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		// The persisted key is derived from the id (CompetitorKey degrades
		// to "id:"+id once an id is known), never from the raw or padded
		// playerName.
		overrides, err := store.LoadOverrides("rank-trim-comp")
		require.NoError(t, err)
		require.NotNil(t, overrides)
		idKey := helper.CompetitorKey("trim-player-id", "Player Trim", "Trim Dojo")
		_, hasIDKey := overrides.PoolRanks["pool-1"][idKey]
		assert.True(t, hasIDKey, "rank override should be keyed under the id-derived identity key")
		_, hasPadded := overrides.PoolRanks["pool-1"]["  Player Trim  "]
		assert.False(t, hasPadded, "rank override should not be keyed under the padded raw name")
		_, hasBareName := overrides.PoolRanks["pool-1"]["Player Trim"]
		assert.False(t, hasBareName, "rank override should not be keyed under the bare trimmed name either")
	})

	t.Run("Override Rank PlayerName Is Ignored, Not Validated", func(t *testing.T) {
		// playerName is DEPRECATED (bc-pnum item 8): it is bound for older
		// clients but never validated, never used to select, and never
		// persisted -- the override is keyed and resolved by playerId
		// alone. A client sending a playerName that names a DIFFERENT
		// competitor, or one exceeding the old MaxLenPlayerName cap, must
		// not affect the outcome: this was a behaviour change (the old
		// handler 400'd an over-length playerName via validateMaxLen even
		// though the value was never stored anywhere real).
		comp := state.Competition{ID: "rank-ignores-name", Status: state.CompStatusPools, Kind: "team", TeamSize: 2}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-ignores-name", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{ID: "ignore-name-p1", Name: "Player 1", Dojo: "Dojo Player 1"},
				{Name: "Player 2", Dojo: "Dojo Player 2"},
			}},
		}))

		overLong := strings.Repeat("x", 5000)
		reqBody, _ := json.Marshal(map[string]any{
			"playerId":   "ignore-name-p1",
			"playerName": overLong,
			"rank":       1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-ignores-name/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "an over-length playerName must not be rejected: it is never validated, response: %s", w.Body.String())

		overrides, err := store.LoadOverrides("rank-ignores-name")
		require.NoError(t, err)
		idKey := helper.CompetitorKey("ignore-name-p1", "", "")
		assert.Equal(t, 1, overrides.PoolRanks["pool-1"][idKey],
			"the override must land under the id key regardless of what playerName was sent")
	})

	t.Run("Override Rank Rejects Invalid Input", func(t *testing.T) {
		comp := state.Competition{ID: "rank-bad-comp", Status: state.CompStatusPools, Kind: "team", TeamSize: 2}
		store.SaveCompetition(&comp)
		// Seed a pool so cases that pass the rank cap checks reach the
		// pool-size validation (rank=99999 fails earlier at the absolute
		// MaxRankOverride cap; rank=4-against-3-player-pool fails the
		// pool-size check). Every case carries a valid playerId (rank-bad-p1)
		// so the rank-specific validation under test is actually reached,
		// rather than short-circuiting on the (also-400) "playerId is
		// required" gate.
		require.NoError(t, store.SavePools("rank-bad-comp", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{ID: "rank-bad-p1", Name: "Player 1", Dojo: "Dojo Player 1"}, {Name: "Player 2", Dojo: "Dojo Player 2"}, {Name: "Player 3", Dojo: "Dojo Player 3"},
			}},
		}))

		cases := []struct {
			name string
			body map[string]any
		}{
			{"zero rank", map[string]any{"playerId": "rank-bad-p1", "rank": 0}},
			{"negative rank", map[string]any{"playerId": "rank-bad-p1", "rank": -3}},
			{"absurdly large rank (over MaxRankOverride)", map[string]any{"playerId": "rank-bad-p1", "rank": 99999}},
			{"rank exceeds pool size", map[string]any{"playerId": "rank-bad-p1", "rank": 4}},
			{"empty playerId", map[string]any{"playerId": "", "rank": 1}},
			{"whitespace-only playerId", map[string]any{"playerId": "   ", "rank": 1}},
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

	t.Run("Override Rank Rejects Off-Roster PlayerId With 400", func(t *testing.T) {
		comp := state.Competition{ID: "rank-off-roster", Status: state.CompStatusPools, Kind: "team", TeamSize: 2}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-off-roster", []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{
				{ID: "p1", Name: "Player 1", Dojo: "Dojo 1"},
			}},
		}))

		// bc-pnum: a playerId with no roster match is a 400, never silently
		// stored under an unreadable key (the same "NO FALLBACKS" rule that
		// used to be enforced for an off-roster playerName, now id-only).
		reqBody, _ := json.Marshal(map[string]any{"playerId": "no-such-id", "rank": 1})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-off-roster/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code,
			"a playerId with no roster match must be refused, not silently stored under an unreadable key")
		assert.Contains(t, w.Body.String(), "no-such-id",
			"the error should name the unmatched playerId")

		overridesPath := filepath.Join(tempDir, "competitions", "rank-off-roster", "overrides.json")
		_, statErr := os.Stat(overridesPath)
		assert.True(t, os.IsNotExist(statErr), "overrides.json must not be created/modified by a refused override")
	})

	t.Run("Override Rank Rejects Unknown Pool With 404", func(t *testing.T) {
		// Pool-size validation requires looking up the pool by name.
		// A bogus: poolId (no matching Pool.PoolName) returns 404.
		// The JS frontend only offers existing pools; this is a
		// defense-in-depth check against hand-crafted API callers.
		comp := state.Competition{ID: "rank-unknown-pool", Status: state.CompStatusPools, Kind: "team", TeamSize: 2}
		store.SaveCompetition(&comp)
		require.NoError(t, store.SavePools("rank-unknown-pool", []helper.Pool{
			{PoolName: "pool-a", Players: []helper.Player{{ID: "p1", Name: "P1", Dojo: "Dojo P1"}}},
		}))

		reqBody, _ := json.Marshal(map[string]any{"playerId": "p1", "rank": 1})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rank-unknown-pool/pools/pool-z/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code,
			"override-rank against an unknown pool name must 404")
		assert.Contains(t, w.Body.String(), "pool",
			"error message should identify the missing pool")
	})

	t.Run("Override Rank Refuses An Individual Competition With 409", func(t *testing.T) {
		// Lots are drawn only over a team tie a daihyosen left level; an
		// individual tie is fought again by ippon-shobu, so no rank is set by
		// hand there, and overrides.json is left untouched.
		const compID = "rank-individual"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Status: state.CompStatusPools, Kind: "individual"}))
		require.NoError(t, store.SavePools(compID, []helper.Pool{
			{PoolName: "pool-1", Players: []helper.Player{{ID: "p1", Name: "P1", Dojo: "Dojo P1"}, {ID: "p2", Name: "P2", Dojo: "Dojo P2"}}},
		}))
		reqBody, _ := json.Marshal(map[string]any{"playerId": "p1", "rank": 1})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/pools/pool-1/override-rank", bytes.NewBuffer(reqBody))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		var body map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "rank_override_not_level", body["code"])
		assert.NotEmpty(t, body["error"])
		_, statErr := os.Stat(filepath.Join(tempDir, "competitions", compID, "overrides.json"))
		assert.True(t, os.IsNotExist(statErr), "a refused override writes nothing")
	})

	t.Run("Override Rank Rejects Wrong Competition Status With 409", func(t *testing.T) {
		validBody, _ := json.Marshal(map[string]any{"playerId": "p1", "rank": 1})
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
				{PoolName: "pool-1", Players: []helper.Player{{ID: "p1", Name: "P1", Dojo: "Dojo P1"}}},
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

	// Same-name-different-dojo disambiguation (operator ruling bc-pnum): two
	// pool members can legally share a display name (operator identity rule
	// is (name, dojo), not name -- helper.CheckDuplicateEntriesByNameDojo
	// only refuses a true (name, dojo) collision). The override-rank
	// endpoint resolves each request to exactly one of them by playerId
	// ONLY; there is no playerDojo narrowing left (that fallback existed
	// only for a client with no id, which can no longer select at all).
	t.Run("Override Rank Disambiguates Same-Name Different-Dojo By PlayerId", func(t *testing.T) {
		comp := state.Competition{ID: "rank-dup-id", Status: state.CompStatusPools, Kind: "team", TeamSize: 2}
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

	// TestOverrideRank_DisambiguatesByPlayerDojo and
	// TestOverrideRank_RejectsAmbiguousSameNameRequest used to live here,
	// pinning the removed playerDojo-narrowing and no-id "ambiguous" 400
	// paths (bc-cse). The operator ruling bc-pnum removed both: playerId is
	// now REQUIRED, so a request naming a same-name pair by playerDojo alone
	// (no playerId) or by bare playerName alone now 400s on "playerId is
	// required" before ever reaching the removed disambiguation logic --
	// already covered by "Override Rank Rejects Invalid Input" above and by
	// TestResolvePoolOverrideTarget's own "empty playerId is rejected" case,
	// so there is no distinct scenario left for a converted test to add.

	//
	// TestOverrideRank_CrossChecksPlayerIdAgainstPlayerName also used to live
	// here, pinning the removed playerId/playerName cross-check (bc-cse FIX
	// 2): a mismatched pair used to 400, and a matching pair used to 200.
	// playerName no longer selects or is compared against anything at all
	// (see the request struct's own doc comment in handlers_competition.go),
	// so a mismatched pair like "id of A + name of B" is now silently
	// ACCEPTED -- the id alone decides, and there is no cross-check left to
	// pin a converted test on.

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

	// validateCompetitionLengths counted numberPrefix by BYTES
	// (validateMaxLen), but the message says "characters", the admin UI's
	// maxLength="3" counts UTF-16 units, and the printed Tags/Names-to-Print
	// sheets count RUNES -- so a 2-character, 4-byte prefix like "ÖÖ" was
	// refused as > 3 "characters" even though every other measure of it
	// agrees it's 2. helper.ValidateNumberPrefix (rune count) fixes this for
	// numberPrefix specifically; validateMaxLen itself is UNCHANGED (its
	// other ~45 callers size a byte-cost cap on purpose).
	t.Run("NumberPrefix accepts a 2-rune, 4-byte prefix on Create", func(t *testing.T) {
		comp := state.Competition{ID: "prefix-runes-create", Name: "Prefix Runes Create", NumberPrefix: "ÖÖ"}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusCreated, w.Code, "response: %s", w.Body.String())
		stored, err := store.LoadCompetition("prefix-runes-create")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "ÖÖ", stored.NumberPrefix, "a 2-RUNE prefix must be stored verbatim, byte length is not the right measure")
	})

	t.Run("NumberPrefix accepts a 2-rune, 4-byte prefix on Update", func(t *testing.T) {
		seed := state.Competition{ID: "prefix-runes-update", Name: "Prefix Runes Update"}
		require.NoError(t, store.SaveCompetition(&seed))

		// A different 2-rune prefix than the Create subtest's "ÖÖ" (shared
		// store across subtests in this parent test): distinct values so
		// this assertion is about the rune-length cap, not a collision.
		update := state.Competition{ID: "prefix-runes-update", Name: "Prefix Runes Update", NumberPrefix: "ÅÅ"}
		body, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/prefix-runes-update", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
		stored, err := store.LoadCompetition("prefix-runes-update")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "ÅÅ", stored.NumberPrefix)
	})

	t.Run("NumberPrefix still rejects a 4-rune prefix", func(t *testing.T) {
		comp := state.Competition{ID: "prefix-too-long", Name: "Prefix Too Long", NumberPrefix: "ÖÖÖÖ"}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusBadRequest, w.Code, "the rune cap must still refuse a genuinely too-long prefix")
		assert.Contains(t, w.Body.String(), "numberPrefix")
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
			PoolSizeMode: "  max  ", StartTime: "  10:30  ", Date: "  15-06-2026  ",
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
		assert.Equal(t, "max", stored.PoolSizeMode, "PoolSizeMode should be trimmed on PUT")
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
		// (the match handlers also use requireValidCompID; without a
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
			// Match endpoints from handlers_match*.go (bulk-score, court,
			// score). Without a match route in this
			// set, a regression that drops requireValidCompID from a match
			// handler would still ship green.
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
