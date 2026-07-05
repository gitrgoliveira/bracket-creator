package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /competitions/:id/league/standings is public (no auth header needed).
func leagueStandingsReq(compID string) *http.Request {
	req, _ := http.NewRequest("GET", fmt.Sprintf("/api/competitions/%s/league/standings", compID), nil)
	return req
}

// A drawn league (no matches scored yet) returns the full roster as a single
// rank-ordered slice, all zeros.
func TestLeagueStandings_Drawn(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "league-comp", Name: "League", Kind: "individual",
		Format: state.CompFormatLeague, PoolFormat: "full", PoolSize: 4,
		Courts: []string{"A"}, StartTime: "09:00", PoolMatchDuration: 3,
		Status: state.CompStatusSetup,
	}))
	players := make([]domain.Player, 4)
	for i := range players {
		players[i] = domain.Player{ID: helper.NewUUID4(), Name: fmt.Sprintf("P%d", i+1), Seed: i + 1, Dojo: fmt.Sprintf("D%d", i+1)}
	}
	require.NoError(t, store.SaveParticipants("league-comp", players))
	require.NoError(t, eng.StartCompetition("league-comp"))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, leagueStandingsReq("league-comp"))
	require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var standings []state.PlayerStanding
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &standings))
	require.Len(t, standings, 4)
	for i, s := range standings {
		assert.Equalf(t, 0, s.Wins, "standings[%d] wins should be 0", i)
		assert.Equalf(t, i+1, s.Rank, "standings[%d] should be rank %d", i, i+1)
	}
}

// A non-league competition 404s: the league endpoint must not leak pool data.
func TestLeagueStandings_NonLeague404(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "pools-comp", Name: "Pools", Format: state.CompFormatMixed,
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, leagueStandingsReq("pools-comp"))
	assert.Equalf(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
}

// A missing competition 404s.
func TestLeagueStandings_Missing404(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, leagueStandingsReq("does-not-exist"))
	assert.Equal(t, http.StatusNotFound, w.Code)
}
