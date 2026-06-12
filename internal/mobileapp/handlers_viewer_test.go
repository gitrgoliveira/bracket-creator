package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergePoolNumbersIntoPlayers — mp-13y: numbers from pools.csv must be
// merged onto comp.Players so the viewer API carries the numberPrefix-derived
// "K1", "K2", … on every player. The merge is the bridge that lets the TV
// display / streaming overlay / viewer card render the prefix at all
// (participants.csv does NOT persist Number).
func TestMergePoolNumbersIntoPlayers(t *testing.T) {
	t.Run("no-op when numberPrefix is empty", func(t *testing.T) {
		comp := &state.Competition{
			Players: []domain.Player{{ID: "p1", Name: "Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "K1"}}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "", comp.Players[0].Number, "no numberPrefix → never merge")
	})

	t.Run("no-op when pools is empty", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka"}},
		}
		mergePoolNumbersIntoPlayers(comp, nil)
		assert.Equal(t, "", comp.Players[0].Number)
	})

	t.Run("merges by id when HasParticipantIDs", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players: []domain.Player{
				{ID: "p1", Name: "Tanaka"},
				{ID: "p2", Name: "Suzuki"},
				{ID: "p3", Name: "Yamada"},
			},
		}
		pools := []helper.Pool{
			{PoolName: "Pool A", Players: []domain.Player{
				{ID: "p1", Name: "Tanaka", Number: "K1"},
				{ID: "p3", Name: "Yamada", Number: "K2"},
			}},
			{PoolName: "Pool B", Players: []domain.Player{
				{ID: "p2", Name: "Suzuki", Number: "K3"},
			}},
		}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "K1", comp.Players[0].Number)
		assert.Equal(t, "K3", comp.Players[1].Number)
		assert.Equal(t, "K2", comp.Players[2].Number)
	})

	t.Run("falls back to name when id is empty (legacy roster)", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players: []domain.Player{
				{Name: "Tanaka"}, // no ID
				{Name: "Suzuki"},
			},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{
			{Name: "Tanaka", Number: "K1"},
			{Name: "Suzuki", Number: "K2"},
		}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "K1", comp.Players[0].Number)
		assert.Equal(t, "K2", comp.Players[1].Number)
	})

	t.Run("preserves existing non-empty Number (idempotent)", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Number: "EXISTING"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "K1"}}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "EXISTING", comp.Players[0].Number, "must not overwrite an existing Number")
	})

	t.Run("skips pool players with empty Number", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: ""}}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "", comp.Players[0].Number)
	})
}

func TestViewerHandlers_Standalone(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// 1. GET /api/viewer/tournament - No tournament case
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 2. GET /api/viewer/tournament - With tournament
	tourney := state.Tournament{Name: "Test Tourney", Password: "secret"}
	require.NoError(t, store.SaveTournament(&tourney))

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var respTourney state.Tournament
	json.Unmarshal(w.Body.Bytes(), &respTourney)
	assert.Equal(t, "Test Tourney", respTourney.Name)
	assert.Equal(t, "", respTourney.Password) // Password should be stripped

	// 3. GET /api/viewer/competitions - Empty case
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]", w.Body.String())

	// 4. GET /api/viewer/competitions - With competitions
	comp1 := state.Competition{ID: "c1", Name: "Comp 1"}
	require.NoError(t, store.SaveCompetition(&comp1))
	require.NoError(t, store.SaveParticipants("c1", nil))

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var comps []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &comps)
	assert.Len(t, comps, 1)
	config := comps[0]["config"].(map[string]interface{})
	assert.Equal(t, "c1", config["id"])

	// 5. GET /api/viewer/competitions/:id - Success
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/c1", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var detail map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &detail)
	assert.NotNil(t, detail["config"])
	assert.Contains(t, detail, "pools")
	assert.Contains(t, detail, "bracket")

	// 6. GET /api/viewer/competitions/:id - Not Found
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/nonexistent", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 7. GET /api/viewer/schedule
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/schedule", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestViewerAggregator_StripsPreviewBracket asserts that a Preview bracket
// (pool-origin placeholder leaves on a mixed source competition) is REMOVED
// from the aggregate /api/viewer/competitions payload so the SPA doesn't
// surface "Pool A-1st vs Pool B-2nd" as upcoming matches in Find-My-Matches /
// Watchlist / schedule / TV displays. The per-competition detail endpoint
// (/api/viewer/competitions/:id) must still return it for the Bracket-tab UI.
// Regression guard for mp-9dz.
func TestViewerAggregator_StripsPreviewBracket(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))
	comp := state.Competition{ID: "mixed", Name: "Mixed", Format: state.CompFormatMixed}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("mixed", nil))

	preview := &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Pool A-1st", SideB: "Pool B-2nd", Court: "A", Status: state.MatchStatusScheduled, ScheduledAt: "09:30"},
		}},
	}
	require.NoError(t, store.SaveBracket("mixed", preview))

	// Aggregate endpoint MUST strip the preview bracket.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var comps []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &comps))
	require.Len(t, comps, 1)
	assert.Nil(t, comps[0]["bracket"], "aggregate endpoint must strip Preview brackets (mp-9dz)")

	// Detail endpoint MUST still return it so the Bracket-tab UI renders.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/mixed", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var detail map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	bracketField, ok := detail["bracket"].(map[string]any)
	require.True(t, ok, "detail endpoint must return the preview bracket for the Bracket-tab UI")
	assert.Equal(t, true, bracketField["preview"], "preview flag must be present on the detail payload")
	rounds, _ := bracketField["rounds"].([]any)
	assert.NotEmpty(t, rounds, "preview bracket rounds must be present on the detail payload")
}

// TestShiaijoMatches_CrossCompAggregation covers GET /shiaijo/:court/matches
// (mp-c2yr): pool + bracket matches across competitions, filtered to one court
// and ordered running → scheduled → completed then scheduledAt.
func TestShiaijoMatches_CrossCompAggregation(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Courts: []string{"A", "B"},
	}))

	// Comp 1 (pools): one scheduled match on A, one on B.
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Name: "Seniors", Status: state.CompStatusPools, Courts: []string{"A", "B"}}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "p-A-0905", SideA: "P1", SideB: "P2", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:05"},
		{ID: "p-B", SideA: "P3", SideB: "P4", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "09:00"},
	}))

	// Comp 2 (playoffs): one running + one completed on A (different competition).
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c2", Name: "Juniors", Status: state.CompStatusPlayoffs, Courts: []string{"A"}}))
	require.NoError(t, store.SaveBracket("c2", &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "b-run", SideA: "Q1", SideB: "Q2", Status: state.MatchStatusRunning, Court: "A", ScheduledAt: "09:10"},
		{ID: "b-done", SideA: "Q3", SideB: "Q4", Status: state.MatchStatusCompleted, Court: "A", ScheduledAt: "08:50"},
	}}}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/shiaijo/A/matches", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

	var resp struct {
		Court   string `json:"court"`
		Matches []struct {
			ID     string `json:"id"`
			CompID string `json:"compId"`
			Court  string `json:"court"`
			Status string `json:"status"`
			Phase  string `json:"phase"`
		} `json:"matches"`
		Players map[string]any `json:"players"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "A", resp.Court)
	// Court A only — the court-B pool match must be excluded.
	ids := make([]string, len(resp.Matches))
	for i, m := range resp.Matches {
		ids[i] = m.ID
		assert.Equal(t, "A", m.Court)
	}
	assert.NotContains(t, ids, "p-B", "court-B match must not appear on court A")
	// Order: running (b-run) → scheduled (p-A-0905) → completed (b-done).
	assert.Equal(t, []string{"b-run", "p-A-0905", "b-done"}, ids)
	// Cross-competition: both comps contributed.
	assert.Equal(t, "c2", resp.Matches[0].CompID)
	assert.Equal(t, "pool", resp.Matches[1].Phase)
	assert.Equal(t, "bracket", resp.Matches[0].Phase)
}

// TestShiaijoMatches_UnknownCourt returns an empty list (200), not 404.
func TestShiaijoMatches_UnknownCourt(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Status: state.CompStatusPools, Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "m1", SideA: "P1", SideB: "P2", Status: state.MatchStatusScheduled, Court: "A"},
	}))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/shiaijo/Z/matches", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Matches []any `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.Matches)
}

// TestShiaijoMatches_FilterPlaceholders asserts that pool/bracket matches whose
// sides are placeholder strings ("Winner of rX-mY", "Pool A-1st") or empty
// (structural byes) are excluded from GET /shiaijo/:court/matches, while a
// normal both-sides match on the same court is included. Regression guard for
// the shiaijoPlayable / Fix A change (mirrors hasBothSides in the frontend).
func TestShiaijoMatches_FilterPlaceholders(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "cx", Name: "Mixed", Status: state.CompStatusPools, Courts: []string{"A"},
	}))

	// Pool matches: one normal, one with placeholder side, one with empty side.
	require.NoError(t, store.SavePoolMatches("cx", []state.MatchResult{
		{ID: "real-pool", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A"},
		{ID: "placeholder-pool", SideA: "Alice", SideB: "Pool A-1st", Status: state.MatchStatusScheduled, Court: "A"},
		{ID: "empty-pool", SideA: "Charlie", SideB: "", Status: state.MatchStatusScheduled, Court: "A"},
	}))

	// Bracket matches: one normal, one with winner-of placeholder.
	require.NoError(t, store.SaveBracket("cx", &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "real-bracket", SideA: "Dave", SideB: "Eve", Status: state.MatchStatusScheduled, Court: "A"},
			{ID: "placeholder-bracket", SideA: "Winner of r1-m0", SideB: "Frank", Status: state.MatchStatusScheduled, Court: "A"},
		}},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/shiaijo/A/matches", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

	var resp struct {
		Matches []struct {
			ID string `json:"id"`
		} `json:"matches"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	ids := make([]string, len(resp.Matches))
	for i, m := range resp.Matches {
		ids[i] = m.ID
	}
	assert.Contains(t, ids, "real-pool", "normal pool match must be included")
	assert.Contains(t, ids, "real-bracket", "normal bracket match must be included")
	assert.NotContains(t, ids, "placeholder-pool", "pool match with placeholder side must be excluded")
	assert.NotContains(t, ids, "placeholder-bracket", "bracket match with winner-of placeholder must be excluded")
	assert.NotContains(t, ids, "empty-pool", "pool match with empty side must be excluded")
}

// TestShiaijoSortHelpers covers the pure ordering helpers behind the shiaijo
// aggregator: statusPriority (running → scheduled → completed → other) and
// schedSortKey (untimed matches sort after timed ones).
func TestShiaijoSortHelpers(t *testing.T) {
	assert.Equal(t, 0, statusPriority("running"))
	assert.Equal(t, 1, statusPriority("scheduled"))
	assert.Equal(t, 2, statusPriority("completed"))
	assert.Equal(t, 99, statusPriority(""))
	assert.Equal(t, 99, statusPriority("bogus"))

	// Timed keys sort before the untimed fallback.
	assert.Equal(t, "09:00", schedSortKey("09:00"))
	assert.Equal(t, "99:99", schedSortKey(""))
	assert.Less(t, schedSortKey("09:05"), schedSortKey(""), "timed match sorts before untimed")
}

// TestMatchToMap confirms a match struct round-trips to a flat JSON object the
// aggregator can decorate, and that an unmarshalable value returns an error.
func TestMatchToMap(t *testing.T) {
	m, err := matchToMap(state.MatchResult{ID: "m1", SideA: "P1", SideB: "P2", Status: state.MatchStatusScheduled, Court: "A"})
	require.NoError(t, err)
	assert.Equal(t, "m1", m["id"])
	assert.Equal(t, "A", m["court"])

	_, err = matchToMap(func() {}) // functions are not JSON-marshalable
	assert.Error(t, err)
}
