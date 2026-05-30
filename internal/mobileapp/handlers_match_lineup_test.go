// internal/mobileapp/handlers_match_lineup_test.go covers the
// match-scoped lineup endpoints (mp-825):
// /api/competitions/:id/teams/:tid/match-lineups/:matchId. These mirror
// the round-scoped endpoints but key the lineup by matchID so successive
// encounters edit and lock independently.
package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validPositionsBody() []byte {
	b, _ := json.Marshal(LineupRequest{Positions: map[domain.Position]string{
		domain.PosSenpo:   "p1",
		domain.PosJiho:    "p2",
		domain.PosChuken:  "p3",
		domain.PosFukusho: "p4",
		domain.PosTaisho:  "p5",
	}})
	return b
}

// TestMatchLineupPUTGET_RoundTrip: a match-scoped PUT persists and the
// public GET returns it, carrying the matchID.
func TestMatchLineupPUTGET_RoundTrip(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	// PUT (admin).
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(validPositionsBody()))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var put domain.TeamLineup
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &put))
	assert.Equal(t, "PoolA-0", put.MatchID)

	// GET (public, no auth).
	greq := httptest.NewRequest(http.MethodGet,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", nil)
	gw := httptest.NewRecorder()
	r.ServeHTTP(gw, greq)
	require.Equal(t, http.StatusOK, gw.Code)
	var got domain.TeamLineup
	require.NoError(t, json.Unmarshal(gw.Body.Bytes(), &got))
	assert.Equal(t, "PoolA-0", got.MatchID)
	assert.Equal(t, "p1", got.Positions[domain.PosSenpo])
}

// TestMatchLineupGET_404WhenAbsent: GET returns 404 so callers can fall
// back to the round-scoped endpoint.
func TestMatchLineupGET_404WhenAbsent(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	req := httptest.NewRequest(http.MethodGet,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-9", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestMatchLineupPUT_409WhenMatchLive: once the match is running, the
// match-scoped PUT is refused with 409 (frozen).
func TestMatchLineupPUT_409WhenMatchLive(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: "teamA", SideB: "teamB", Status: state.MatchStatusRunning},
	}))

	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(validPositionsBody()))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
}

// TestMatchLineupDELETE: a match-scoped DELETE removes the entry and
// returns 204.
func TestMatchLineupDELETE(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SetTeamLineup("c1", domain.TeamLineup{
		TeamID:  "teamA",
		MatchID: "PoolA-0",
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1", domain.PosJiho: "p2", domain.PosChuken: "p3",
			domain.PosFukusho: "p4", domain.PosTaisho: "p5",
		},
	}, 5))

	req := httptest.NewRequest(http.MethodDelete,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	lineups, err := store.LoadTeamLineups("c1")
	require.NoError(t, err)
	_, found := findMatchLineup(lineups, "teamA", "PoolA-0")
	assert.False(t, found, "entry must be gone after delete")
}

// TestMatchLineupPUT_RequiresAuth: the match-scoped PUT is on the admin
// group and rejects requests without the password.
func TestMatchLineupPUT_RequiresAuth(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(validPositionsBody()))
	// No password header.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
