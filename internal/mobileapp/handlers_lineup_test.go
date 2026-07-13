package mobileapp

import (
	"bytes"
	"encoding/json"
	"errors"
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

// TestLineupSetStatus pins the SetTeamLineup error classification: a domain
// lineup validation error (team_lineup: prefix) is a 400, while a YAML/disk
// fault is a 500 (not misreported as a bad request).
func TestLineupSetStatus(t *testing.T) {
	assert.Equal(t, http.StatusBadRequest,
		lineupSetStatus(domain.ErrLineupMissingSenpo), "validation sentinel -> 400")
	assert.Equal(t, http.StatusBadRequest,
		lineupSetStatus(errors.New("team_lineup: position \"x\" not allowed in 5-person team")), "validation error -> 400")
	assert.Equal(t, http.StatusInternalServerError,
		lineupSetStatus(errors.New("open lineups.yaml: permission denied")), "I/O error -> 500")
	assert.Equal(t, http.StatusInternalServerError,
		lineupSetStatus(errors.New("yaml: line 3: mapping values are not allowed")), "YAML parse error -> 500")
}

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

	// Public group, same as production server.go
	api := r.Group("/api")
	RegisterPublicLineupHandlers(api, store)

	// Admin group, AuthMiddleware gates all writes
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterLineupHandlers(admin, store, store, store, stubBroadcaster{})

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

// TestPublicLineupGET_PayloadIntact verifies that GET /lineups/:round and
// GET /match-lineups/:matchId return the full lineup payload (teamID, positions)
// to unauthenticated callers. Both round-scoped and match-scoped reads are
// covered.
func TestPublicLineupGET_PayloadIntact(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	positions := map[domain.Position]string{
		domain.PosSenpo:   "p1",
		domain.PosJiho:    "p2",
		domain.PosChuken:  "p3",
		domain.PosFukusho: "p4",
		domain.PosTaisho:  "p5",
	}
	require.NoError(t, store.SetTeamLineup("c1", domain.TeamLineup{
		TeamID: "teamA", CompetitionID: "c1", Round: 1, Positions: positions,
	}, 5))
	require.NoError(t, store.SetTeamLineup("c1", domain.TeamLineup{
		TeamID: "teamA", CompetitionID: "c1", Round: 1, MatchID: "Pool A-0", Positions: positions,
	}, 5))

	for _, tc := range []struct {
		path string
	}{
		{"/api/competitions/c1/teams/teamA/lineups/1"},
		{"/api/competitions/c1/teams/teamA/match-lineups/Pool%20A-0"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			var got domain.TeamLineup
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			assert.Equal(t, "teamA", got.TeamID)
			assert.Equal(t, "p1", got.Positions[domain.PosSenpo])
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
// only valid keys (e.g. only jiho set, senpo missing) is accepted,
// completeness is a non-blocking UI warning, not a write-time gate.
func TestLineupPUT_ValidationError(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	// "chudan" is not a valid FIK position name for a 5-person team, key validation must reject it.
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

// TestPublicLineupGET_FallbackBest: the scoring modal is the client-side
// twin of AMENDMENT 1. Operators typically save one round-0 lineup for
// the whole day, but a knockout final asks for its own round index (1+),
// and an exact-only GET 404s, leaving the modal with no names (UAT: the
// final's bootstrapped bout 1 was submitted with empty sides). With
// ?fallback=best the handler resolves via the FindBestLineup round tiers
// (highest round <= requested, else highest overall). Without the param
// the exact + 404 semantics are unchanged (the lineup editor relies on
// 404 meaning "no lineup submitted for THIS round").
func TestPublicLineupGET_FallbackBest(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "c-fb",
		TeamSize: 5,
	}))
	require.NoError(t, store.SetTeamLineup("c-fb", domain.TeamLineup{
		TeamID: "teamA",
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "p1",
			domain.PosJiho:    "p2",
			domain.PosChuken:  "p3",
			domain.PosFukusho: "p4",
			domain.PosTaisho:  "p5",
		},
	}, 5))

	t.Run("exact miss with fallback=best returns the round-0 lineup", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/competitions/c-fb/teams/teamA/lineups/1?fallback=best", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var got domain.TeamLineup
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, "teamA", got.TeamID)
		assert.Equal(t, 0, got.Round, "round-0 lineup resolved for the round-1 request")
		assert.Equal(t, "p1", got.Positions[domain.PosSenpo])
	})

	t.Run("exact miss without the param still 404s", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/competitions/c-fb/teams/teamA/lineups/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("fallback=best with no lineup at all still 404s", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/competitions/c-fb/teams/teamB/lineups/1?fallback=best", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("exact hit ignores the param", func(t *testing.T) {
		require.NoError(t, store.SetTeamLineup("c-fb", domain.TeamLineup{
			TeamID: "teamA",
			Round:  1,
			Positions: map[domain.Position]string{
				domain.PosSenpo:   "q1",
				domain.PosJiho:    "q2",
				domain.PosChuken:  "q3",
				domain.PosFukusho: "q4",
				domain.PosTaisho:  "q5",
			},
		}, 5))
		req := httptest.NewRequest(http.MethodGet,
			"/api/competitions/c-fb/teams/teamA/lineups/1?fallback=best", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var got domain.TeamLineup
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, 1, got.Round, "exact round-1 lineup wins over fallback")
	})
}
