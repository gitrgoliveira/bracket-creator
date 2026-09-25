package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PUT .../override-rank also takes a whole group of ranks ({"ranks": [...]}),
// the form the chusen panel sends: every rank is recorded and answered for as
// ONE change, so the knockout check sees only the order the operator entered.
// The group form must answer exactly as the single form does (same refusals,
// same confirmation, same receipt) and take back EVERY rank on a refusal.

func sendRankGroup(t *testing.T, r *gin.Engine, compID string, force bool, ranks ...map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool A/override-rank", map[string]any{
		"ranks": ranks, "forceDownstreamReopen": force,
	})
}

func rankEntry(playerID string, rank int) map[string]any {
	return map[string]any{"playerId": playerID, "rank": rank}
}

func poolARanks(t *testing.T, store *state.Store, compID string) map[string]int {
	t.Helper()
	o, err := store.LoadOverrides(compID)
	require.NoError(t, err)
	return o.PoolRanks["Pool A"]
}

func TestPoolRankOverrideGroup_RecordsTheWholeOrder(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	compID := "rank-group-ok"
	seedMixedCompWithSeatedKnockout(t, store, compID, state.MatchStatusScheduled)

	w := sendRankGroup(t, r, compID, false, rankEntry("a2-id", 1), rankEntry("a1-id", 2))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"reopenedMatches":[]}`, w.Body.String())
	assert.Equal(t, map[string]int{
		helper.CompetitorKey("a2-id", "", ""): 1,
		helper.CompetitorKey("a1-id", "", ""): 2,
	}, poolARanks(t, store, compID))
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "A2", b.Rounds[0][0].SideA, "the unplayed match is repainted with the new 1st")
	assert.Equal(t, "a2-id", b.Rounds[0][0].SideAID)
}

func TestPoolRankOverrideGroup_RefusesLikeTheSingleForm(t *testing.T) {
	t.Run("played", func(t *testing.T) {
		// The same order through both forms: A2 1st (A1 2nd).
		rs, singleStore, _, _, _ := setupTestRouter(t)
		seedMixedCompWithSeatedKnockout(t, singleStore, "rank-single-played", state.MatchStatusCompleted)
		single := sendJSON(t, rs, http.MethodPut, "/api/competitions/rank-single-played/pools/Pool A/override-rank", map[string]any{
			"playerId": "a2-id", "rank": 1,
		})
		require.Equal(t, http.StatusConflict, single.Code, single.Body.String())

		r, store, _, _, _ := setupTestRouter(t)
		compID := "rank-group-played"
		seedMixedCompWithSeatedKnockout(t, store, compID, state.MatchStatusCompleted)
		w := sendRankGroup(t, r, compID, false, rankEntry("a2-id", 1), rankEntry("a1-id", 2))
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.JSONEq(t, single.Body.String(), w.Body.String(), "the group form refuses with the single form's body")
		assert.Contains(t, w.Body.String(), `"error":"downstream_knockout_played"`)
		assert.Empty(t, poolARanks(t, store, compID), "the refusal takes back every rank of the group")

		w = sendRankGroup(t, r, compID, true, rankEntry("a2-id", 1), rankEntry("a1-id", 2))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var receipt struct {
			ReopenedMatches []struct {
				ID string `json:"id"`
			} `json:"reopenedMatches"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
		require.Len(t, receipt.ReopenedMatches, 1)
		assert.Equal(t, "m-r1-0", receipt.ReopenedMatches[0].ID)
		assert.Len(t, poolARanks(t, store, compID), 2)
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, state.MatchStatusScheduled, b.Rounds[0][0].Status)
		assert.Equal(t, "A2", b.Rounds[0][0].SideA)
	})

	t.Run("running", func(t *testing.T) {
		for _, force := range []bool{false, true} {
			r, store, _, _, _ := setupTestRouter(t)
			compID := fmt.Sprintf("rank-group-running-%v", force)
			seedMixedCompWithSeatedKnockout(t, store, compID, state.MatchStatusRunning)
			w := sendRankGroup(t, r, compID, force, rankEntry("a2-id", 1), rankEntry("a1-id", 2))
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"error":"downstream_knockout_running"`, "force=%v", force)
			assert.Empty(t, poolARanks(t, store, compID), "force=%v: nothing recorded", force)
		}
	})
}

func TestPoolRankOverrideGroup_Validation(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{"both forms", map[string]any{"playerId": "a1-id", "rank": 1, "ranks": []any{rankEntry("a2-id", 2)}},
			"send either ranks or playerId and rank, not both"},
		{"a player twice", map[string]any{"ranks": []any{rankEntry("a1-id", 1), rankEntry("a1-id", 2)}},
			`playerId "a1-id" is given more than one rank`},
		{"a rank twice", map[string]any{"ranks": []any{rankEntry("a1-id", 1), rankEntry("a2-id", 1)}},
			"rank 1 is given to more than one player"},
		{"an entry with no player", map[string]any{"ranks": []any{rankEntry("a1-id", 1), rankEntry(" ", 2)}},
			"playerId is required"},
		{"a rank that is not positive", map[string]any{"ranks": []any{rankEntry("a1-id", 1), rankEntry("a2-id", 0)}},
			"rank must be a positive integer"},
		{"a rank past the absolute cap", map[string]any{"ranks": []any{rankEntry("a1-id", 1), rankEntry("a2-id", helper.MaxRankOverride+1)}},
			fmt.Sprintf("rank must be a positive integer ≤ %d", helper.MaxRankOverride)},
		{"a rank past the pool size", map[string]any{"ranks": []any{rankEntry("a1-id", 1), rankEntry("a2-id", 3)}},
			"rank 3 exceeds pool size 2"},
		{"a player from another pool", map[string]any{"ranks": []any{rankEntry("a1-id", 1), rankEntry("b1-id", 2)}},
			`playerId "b1-id" not found in this pool`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			compID := "rank-group-invalid"
			seedMixedCompWithSeatedKnockout(t, store, compID, state.MatchStatusScheduled)
			w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool A/override-rank", tc.body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var body struct {
				Error string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.want, body.Error)
			assert.Empty(t, poolARanks(t, store, compID), "a refused group records nothing")
		})
	}
}
