package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupDaihyosenTestRouter(t *testing.T) (*gin.Engine, *state.Store, *engine.Engine, *Hub, string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "daihyosen-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	eng := engine.New(store)
	hub := NewHub()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	RegisterDaihyosenHandlers(api, eng, store, hub)

	return r, store, eng, hub, dir
}

// findMatchForDaihyosen / countEligibleForSides are thin test wrappers that run
// the (production) Tx-aware helpers inside a transaction, so the existing unit
// tests can exercise them directly without each constructing a StoreTx.
func findMatchForDaihyosen(store *state.Store, compID, matchID string) (m *state.MatchResult, found bool, err error) {
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		m, found, err = findMatchForDaihyosenTx(tx, compID, matchID)
		return err
	})
	if err == nil && txErr != nil {
		return nil, false, txErr
	}
	return m, found, err
}

func countEligibleForSides(store *state.Store, compID, sideA, sideB string) (a int, b int, err error) {
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		a, b, err = countEligibleForSidesTx(tx, compID, sideA, sideB)
		return err
	})
	if err == nil && txErr != nil {
		return 0, 0, txErr
	}
	return a, b, err
}

// TestDaihyosenHandler_EngiGuard verifies the daihyosen endpoint rejects engi
// competitions with 400 (not 500): a representative bout has no meaning when
// scoring is by referee flag counts. Mirrors the quick-score / override /
// decision engi guards.
func TestDaihyosenHandler_EngiGuard(t *testing.T) {
	r, store, _, _, _ := setupDaihyosenTestRouter(t)

	cid := "engi-daihyosen"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: cid, Engi: true, TeamSize: 3}))
	require.NoError(t, store.SaveBracket(cid, &state.Bracket{
		Rounds: [][]state.BracketMatch{{{ID: "b1", SideA: "Team A", SideB: "Team B"}}},
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/competitions/"+cid+"/matches/b1/daihyosen", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, "daihyosen on engi comp must return 400; body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "engi")
}

// TestFindMatchForDaihyosen_PoolFound verifies that a pool match is located
// when searched by its "Pool *" ID.
func TestFindMatchForDaihyosen_PoolFound(t *testing.T) {
	dir, err := os.MkdirTemp("", "find-pool-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	compID := "find-pool"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "TeamA", SideB: "TeamB"},
	}))

	match, found, err := findMatchForDaihyosen(store, compID, "Pool A-0")
	require.NoError(t, err)
	assert.True(t, found)
	require.NotNil(t, match)
	assert.Equal(t, "TeamA", match.SideA)
}

// TestFindMatchForDaihyosen_PoolNotFound verifies that a missing pool-stage
// match returns (nil, false, nil).
func TestFindMatchForDaihyosen_PoolNotFound(t *testing.T) {
	dir, err := os.MkdirTemp("", "find-pmiss-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	compID := "find-pmiss"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

	match, found, err := findMatchForDaihyosen(store, compID, "Pool A-0")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, match)
}

// TestFindMatchForDaihyosen_BracketFound verifies that a bracket match is
// located when searched by its bracket ID.
func TestFindMatchForDaihyosen_BracketFound(t *testing.T) {
	dir, err := os.MkdirTemp("", "find-bracket-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	compID := "find-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "B1", SideA: "TeamA", SideB: "TeamB"},
			},
		},
	}))

	match, found, err := findMatchForDaihyosen(store, compID, "B1")
	require.NoError(t, err)
	assert.True(t, found)
	require.NotNil(t, match)
	assert.Equal(t, "B1", match.ID)
	assert.Equal(t, "TeamA", match.SideA)
}

// TestFindMatchForDaihyosen_BracketNotFound verifies that a missing bracket
// match returns (nil, false, nil).
func TestFindMatchForDaihyosen_BracketNotFound(t *testing.T) {
	dir, err := os.MkdirTemp("", "find-bnot-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	compID := "find-bnot"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	match, found, err := findMatchForDaihyosen(store, compID, "no-such-bracket-match")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, match)
}

// TestCountEligibleForSides_AllEligible verifies that all participants are
// counted when no ineligibility records exist.
func TestCountEligibleForSides_AllEligible(t *testing.T) {
	dir, err := os.MkdirTemp("", "eligible-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	compID := "eligible-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	// Use proper UUID-format IDs so SaveParticipants/LoadParticipants round-trips correctly.
	p1ID := "aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	p2ID := "bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: p1ID, Name: "Alice", Dojo: "A"},
		{ID: p2ID, Name: "Bob", Dojo: "B"},
	}))

	a, b, err := countEligibleForSides(store, compID, "TeamA", "TeamB")
	require.NoError(t, err)
	assert.Equal(t, 2, a)
	assert.Equal(t, 2, b)
}

// TestCountEligibleForSides_OneIneligible verifies that an ineligible
// participant is excluded from the eligible count.
func TestCountEligibleForSides_OneIneligible(t *testing.T) {
	dir, err := os.MkdirTemp("", "ineligible-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	compID := "ineligible-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	p1ID := "cccccccc-cccc-4ccc-cccc-cccccccccccc"
	p2ID := "dddddddd-dddd-4ddd-dddd-dddddddddddd"
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: p1ID, Name: "Alice", Dojo: "A"},
		{ID: p2ID, Name: "Bob", Dojo: "B"},
	}))
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: p1ID,
		Eligible: false,
		Reason:   "kiken",
	}))

	a, b, err := countEligibleForSides(store, compID, "TeamA", "TeamB")
	require.NoError(t, err)
	assert.Equal(t, 1, a)
	assert.Equal(t, 1, b)
}

// TestDaihyosenHandler_MatchNotFound verifies that a request for a
// non-existent match returns 404.
func TestDaihyosenHandler_MatchNotFound(t *testing.T) {
	r, store, _, _, _ := setupDaihyosenTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

	req := httptest.NewRequest(http.MethodPost,
		"/api/competitions/c1/matches/no-such-match/daihyosen", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestDaihyosenHandler_HappyPath verifies the full happy-path: a tied bracket
// match with eligible participants results in a daihyosen bout being appended
// and the response containing a subResult.
func TestDaihyosenHandler_HappyPath(t *testing.T) {
	r, store, _, _, _ := setupDaihyosenTestRouter(t)
	compID := "dh-happy"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	// Save one eligible participant (so countEligibleForSides returns > 0).
	p1ID := "11111111-1111-4111-1111-111111111111"
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: p1ID, Name: "Alice", Dojo: "A"},
	}))
	// Bracket match with empty SubResults → IV:0-0, PW:0-0 → tied.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "B1", SideA: "TeamA", SideB: "TeamB", Status: state.MatchStatusRunning},
			},
		},
	}))

	req := httptest.NewRequest(http.MethodPost,
		"/api/competitions/"+compID+"/matches/B1/daihyosen", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotNil(t, resp["subResult"])
}

// TestRemoveDaihyosen covers the DELETE /daihyosen endpoint with four
// subtests: successful removal, 404 when no DH exists, 409 when the DH
// is already scored, and 404 when the match itself is not found.
func TestRemoveDaihyosen(t *testing.T) {
	t.Run("removes an unscored daihyosen", func(t *testing.T) {
		r, store, _, _, _ := setupDaihyosenTestRouter(t)
		compID := "rm-dh-ok"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
		// Bracket match that already has a daihyosen placeholder (Position=-1,
		// no ippons, no winner) alongside one regular sub.
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{
						ID: "B1", SideA: "TeamA", SideB: "TeamB",
						Status: state.MatchStatusRunning,
						SubResults: []state.SubMatchResult{
							{Position: 1, SideA: "Alice", SideB: "Bob", Winner: "Alice"},
							{Position: -1, SideA: "RepA", SideB: "RepB", Decision: "daihyosen"},
						},
					},
				},
			},
		}))

		req := httptest.NewRequest(http.MethodDelete,
			"/api/competitions/"+compID+"/matches/B1/daihyosen", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.NotNil(t, resp["result"])

		// Confirm the DH sub is gone from the persisted bracket.
		bracket, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.NotNil(t, bracket)
		match := bracket.Rounds[0][0]
		assert.Equal(t, state.MatchStatusRunning, match.Status)
		for _, sub := range match.SubResults {
			assert.NotEqual(t, -1, sub.Position, "daihyosen sub must be removed")
		}
	})

	t.Run("404 when no daihyosen", func(t *testing.T) {
		r, store, _, _, _ := setupDaihyosenTestRouter(t)
		compID := "rm-dh-nodh"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{{ID: "B2", SideA: "TeamA", SideB: "TeamB", Status: state.MatchStatusRunning}},
			},
		}))

		req := httptest.NewRequest(http.MethodDelete,
			"/api/competitions/"+compID+"/matches/B2/daihyosen", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)

		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "no_daihyosen", resp["error"])
	})

	t.Run("409 when daihyosen is scored", func(t *testing.T) {
		r, store, _, _, _ := setupDaihyosenTestRouter(t)
		compID := "rm-dh-scored"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
		// DH sub has ippons recorded: treated as scored.
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{
						ID: "B3", SideA: "TeamA", SideB: "TeamB",
						Status: state.MatchStatusRunning,
						SubResults: []state.SubMatchResult{
							{Position: -1, SideA: "RepA", SideB: "RepB", IpponsA: []string{"M"}, Decision: "daihyosen"},
						},
					},
				},
			},
		}))

		req := httptest.NewRequest(http.MethodDelete,
			"/api/competitions/"+compID+"/matches/B3/daihyosen", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusConflict, w.Code)

		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "daihyosen_scored", resp["error"])
	})

	t.Run("409 when daihyosen carries hansoku or a non-placeholder decision", func(t *testing.T) {
		// The scored-guard must treat an acted-on bout as scored even when
		// no winner/ippon is set yet: recorded hansoku penalties, or a
		// sub-Decision that is no longer the bare "daihyosen" placeholder
		// (validateSubBout does not validate sub.Decision).
		cases := []struct {
			name string
			id   string
			sub  state.SubMatchResult
		}{
			{"hansoku on side A", "rm-dh-hansokuA", state.SubMatchResult{Position: -1, SideA: "RepA", SideB: "RepB", HansokuA: 1, Decision: "daihyosen"}},
			{"hansoku on side B", "rm-dh-hansokuB", state.SubMatchResult{Position: -1, SideA: "RepA", SideB: "RepB", HansokuB: 1, Decision: "daihyosen"}},
			{"withdrawal decision, no winner", "rm-dh-withdrawal", state.SubMatchResult{Position: -1, SideA: "RepA", SideB: "RepB", Decision: "kiken-voluntary"}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				r, store, _, _, _ := setupDaihyosenTestRouter(t)
				compID := tc.id
				require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
				require.NoError(t, store.SaveBracket(compID, &state.Bracket{
					Rounds: [][]state.BracketMatch{
						{{ID: "B4", SideA: "TeamA", SideB: "TeamB", Status: state.MatchStatusRunning,
							SubResults: []state.SubMatchResult{tc.sub}}},
					},
				}))

				req := httptest.NewRequest(http.MethodDelete,
					"/api/competitions/"+compID+"/matches/B4/daihyosen", nil)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				assert.Equal(t, http.StatusConflict, w.Code)
				var resp map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, "daihyosen_scored", resp["error"])
			})
		}
	})

	t.Run("404 when match not found", func(t *testing.T) {
		r, store, _, _, _ := setupDaihyosenTestRouter(t)
		compID := "rm-dh-nomatch"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

		req := httptest.NewRequest(http.MethodDelete,
			"/api/competitions/"+compID+"/matches/no-such-match/daihyosen", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)

		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "match not found", resp["error"])
	})
}

// TestDaihyosenHandler_PoolMatchReturnsError verifies that a pool-stage
// match returns 400 with "pool_match" because daihyosen is knockout-only.
func TestDaihyosenHandler_PoolMatchReturnsError(t *testing.T) {
	r, store, _, _, _ := setupDaihyosenTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "Pool A-0", SideA: "TeamA", SideB: "TeamB"},
	}))

	req := httptest.NewRequest(http.MethodPost,
		"/api/competitions/c1/matches/Pool%20A-0/daihyosen", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "pool_match", resp["error"])
}
