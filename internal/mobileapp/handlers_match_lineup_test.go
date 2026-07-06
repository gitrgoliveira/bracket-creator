// internal/mobileapp/handlers_match_lineup_test.go covers the
// match-scoped lineup endpoints (mp-825):
// /api/competitions/:id/teams/:tid/match-lineups/:matchId. These mirror
// the round-scoped endpoints but key the lineup by matchID so successive
// encounters can be edited independently at any time.
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

// TestMatchLineupPUT_AlwaysEditable: a match-scoped PUT always succeeds,
// including after a first save and even while the match is running.
// Lineups are not locked; operators can correct them at any time.
func TestMatchLineupPUT_AlwaysEditable(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: "teamA", SideB: "teamB", Status: state.MatchStatusRunning},
	}))

	// First PUT: new lineup while match is running must succeed.
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(validPositionsBody()))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "new lineup while match is running must succeed: "+w.Body.String())

	// Second PUT: changing a recorded position while running must also succeed.
	changeBody, _ := json.Marshal(LineupRequest{Positions: map[domain.Position]string{
		domain.PosSenpo:   "p1-substitute",
		domain.PosJiho:    "p2",
		domain.PosChuken:  "p3",
		domain.PosFukusho: "p4",
		domain.PosTaisho:  "p5",
	}})
	req2 := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(changeBody))
	req2.Header.Set("X-Tournament-Password", "secret")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code, "changing a recorded position while running must succeed: "+w2.Body.String())

	// Verify the substitution persisted.
	lineups, err := store.LoadTeamLineups("c1")
	require.NoError(t, err)
	saved, found := findMatchLineup(lineups, "teamA", "PoolA-0")
	require.True(t, found)
	assert.Equal(t, "p1-substitute", saved.Positions[domain.PosSenpo])
}
