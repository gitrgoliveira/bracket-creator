package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupLineupTestRouter builds a router that mirrors the production server.go
// layout: GET is on the public api group (no auth), PUT/DELETE are on the
// admin group (AuthMiddleware). Used to verify the auth split is correct.
func setupLineupTestRouter(t *testing.T) (*gin.Engine, *state.Store, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir, err := os.MkdirTemp("", "lineup-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	r := gin.New()

	// Public group — same as production server.go
	api := r.Group("/api")
	RegisterPublicLineupHandlers(api, store)

	// Admin group — AuthMiddleware gates all writes
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterLineupHandlers(admin, store, store, store)

	return r, store, dir
}

// TestPublicLineupGET_NoAuthRequired is the primary regression test for the
// bug where GET /lineups/:round was behind AuthMiddleware. Coaches and
// display surfaces call this endpoint without a password; a password-protected
// tournament must not return 401/403 for the GET.
func TestPublicLineupGET_NoAuthRequired(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "c1",
		TeamSize: 5,
	}))

	t.Run("no lineup returns 404, no auth needed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/competitions/c1/teams/teamA/lineups/1", nil)
		// Deliberately no X-Tournament-Password header
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("persisted lineup is visible without auth", func(t *testing.T) {
		lineup := domain.TeamLineup{
			TeamID:        "teamA",
			CompetitionID: "c1",
			Round:         1,
			Positions: map[domain.Position]string{
				domain.PosSenpo:   "p1",
				domain.PosJiho:    "p2",
				domain.PosChuken:  "p3",
				domain.PosFukusho: "p4",
				domain.PosTaisho:  "p5",
			},
		}
		require.NoError(t, store.SetTeamLineup("c1", lineup, 5))

		req := httptest.NewRequest(http.MethodGet,
			"/api/competitions/c1/teams/teamA/lineups/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var got domain.TeamLineup
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, "teamA", got.TeamID)
		assert.Equal(t, 1, got.Round)
	})
}

// TestPublicLineupGET_RedactsChangeReason is the regression test for the
// public-projection leak: ChangeReason is operator-only audit free-text (it can
// name competitors / carry medical detail, e.g. "Substitution: injury to jiho")
// and must never reach the unauthenticated GET endpoints. Both the round-scoped
// and match-scoped reads must strip it; the field stays persisted for the audit
// trail.
func TestPublicLineupGET_RedactsChangeReason(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	const secret = "Substitution: injury to jiho"
	positions := map[domain.Position]string{
		domain.PosSenpo:   "p1",
		domain.PosJiho:    "p2",
		domain.PosChuken:  "p3",
		domain.PosFukusho: "p4",
		domain.PosTaisho:  "p5",
	}
	// Round-scoped lineup (drives /lineups/:round) and a match-scoped lineup
	// (drives /match-lineups/:matchId) — different storage keys. Force path is
	// how production persists a ChangeReason.
	require.NoError(t, store.SetTeamLineupForce("c1", domain.TeamLineup{
		TeamID: "teamA", CompetitionID: "c1", Round: 1, ChangeReason: secret, Positions: positions,
	}, 5))
	require.NoError(t, store.SetTeamLineupForce("c1", domain.TeamLineup{
		TeamID: "teamA", CompetitionID: "c1", Round: 1, MatchID: "Pool A-0", ChangeReason: secret, Positions: positions,
	}, 5))

	// Sanity: both are persisted WITH the reason (so the GET strip, not a
	// missing write, is what keeps it out of the response).
	stored, err := store.LoadTeamLineups("c1")
	require.NoError(t, err)
	var withReason int
	for _, l := range stored {
		if l.ChangeReason == secret {
			withReason++
		}
	}
	require.Equal(t, 2, withReason, "both lineups should persist the audit reason")

	for _, path := range []string{
		"/api/competitions/c1/teams/teamA/lineups/1",
		"/api/competitions/c1/teams/teamA/match-lineups/Pool%20A-0",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			assert.NotContains(t, w.Body.String(), secret, "ChangeReason leaked to public GET")
			assert.NotContains(t, w.Body.String(), "changeReason", "changeReason key present in public payload")

			var got domain.TeamLineup
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			assert.Empty(t, got.ChangeReason)
			assert.Equal(t, "teamA", got.TeamID) // payload otherwise intact
		})
	}
}

// TestLineupPUT_RequiresAuth confirms that PUT /lineups/:round remains on the
// admin group and is rejected without the password header.
func TestLineupPUT_RequiresAuth(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "c1",
		TeamSize: 5,
	}))

	body, _ := json.Marshal(map[string]any{
		"positions": map[string]string{
			"senpo":   "p1",
			"jiho":    "p2",
			"chuken":  "p3",
			"fukusho": "p4",
			"taisho":  "p5",
		},
	})

	t.Run("no password header returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/api/competitions/c1/teams/teamA/lineups/1",
			bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("correct password header succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/api/competitions/c1/teams/teamA/lineups/1",
			bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestLineupDELETE_RequiresAuth confirms that DELETE /lineups/:round remains
// on the admin group and is rejected without the password header.
func TestLineupDELETE_RequiresAuth(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "c1",
		TeamSize: 5,
	}))

	// Seed a lineup so DELETE has something to act on
	lineup := domain.TeamLineup{
		TeamID:        "teamA",
		CompetitionID: "c1",
		Round:         1,
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "p1",
			domain.PosJiho:    "p2",
			domain.PosChuken:  "p3",
			domain.PosFukusho: "p4",
			domain.PosTaisho:  "p5",
		},
	}
	require.NoError(t, store.SetTeamLineup("c1", lineup, 5))

	t.Run("no password header returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete,
			"/api/competitions/c1/teams/teamA/lineups/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("correct password header succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete,
			"/api/competitions/c1/teams/teamA/lineups/1", nil)
		req.Header.Set("X-Tournament-Password", "secret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}

// TestParseLineupParams_NonIntegerRound verifies that a non-integer round
// parameter returns 400.
func TestParseLineupParams_NonIntegerRound(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	req := httptest.NewRequest(http.MethodGet,
		"/api/competitions/c1/teams/teamA/lineups/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestParseLineupParams_NegativeRound verifies that a negative round returns 400.
func TestParseLineupParams_NegativeRound(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/lineups/-1",
		bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestLineupPUT_NoCompetition verifies that a PUT for an unknown competition
// returns 404.
func TestLineupPUT_NoCompetition(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Password: "secret"}))

	body, _ := json.Marshal(map[string]any{
		"positions": map[string]string{
			"senpo":   "p1",
			"jiho":    "p2",
			"chuken":  "p3",
			"fukusho": "p4",
			"taisho":  "p5",
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/no-such-comp/teams/teamA/lineups/1",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestLineupPUT_ZeroTeamSize verifies that a competition with TeamSize=0
// returns 400 because it's not configured for team play.
func TestLineupPUT_ZeroTeamSize(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 0}))

	body, _ := json.Marshal(map[string]any{
		"positions": map[string]string{"senpo": "p1"},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/lineups/1",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestLineupPUT_ValidationError verifies that a PUT with an invalid position
// KEY (not a recognised FIK name) returns 400. Note: a partial lineup with
// only valid keys (e.g. only jiho set, senpo missing) is accepted —
// completeness is a non-blocking UI warning, not a write-time gate.
func TestLineupPUT_ValidationError(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	// "chudan" is not a valid FIK position name for a 5-person team — key validation must reject it.
	body, _ := json.Marshal(map[string]any{
		"positions": map[string]string{
			"chudan": "p1",
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/lineups/1",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestLineupPUT_InvalidJSON verifies that a malformed body returns 400.
func TestLineupPUT_InvalidJSON(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/lineups/1",
		bytes.NewBufferString("{bad-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
