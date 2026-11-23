package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
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
		Format: state.CompFormatLeague, PoolSize: 4,
		Courts: []string{"A"}, StartTime: "09:00", PoolMatchDurationSeconds: 180,
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

// A league that hides names (bc-hnam) answers its public standings with each
// competitor's number where the name was, never cached; the organiser reads
// the names, and a stale password is 401.
func TestLeagueStandings_HideNamesFromThePublic(t *testing.T) {
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Cup", Date: "01-06-2030", Password: "secret-pw", Courts: []string{"A"},
		Mode: state.TournamentModeOfficiated,
	}))
	eng := engine.New(store)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "league-comp", Name: "League", Kind: "individual",
		Format: state.CompFormatLeague, PoolSize: 3,
		Courts: []string{"A"}, StartTime: "09:00", PoolMatchDurationSeconds: 180,
		Status: state.CompStatusSetup, NumberPrefix: "L", PublicNames: state.PublicNamesHidden,
	}))
	players := make([]domain.Player, 3)
	for i := range players {
		players[i] = domain.Player{ID: helper.NewUUID4(), Name: fmt.Sprintf("Hidden Person%d", i+1), Dojo: fmt.Sprintf("D%d", i+1)}
	}
	require.NoError(t, store.SaveParticipants("league-comp", players))
	require.NoError(t, eng.StartCompetition("league-comp"))

	r := gin.New()
	registerPublicLeagueHandlers(r.Group("/api"), eng, newPublicNames(store, NewFileVerifier(store)))
	get := func(password string) *httptest.ResponseRecorder {
		req := leagueStandingsReq("league-comp")
		if password != "" {
			req.Header.Set("X-Tournament-Password", password)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := get("")
	require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	assert.NotContains(t, w.Body.String(), "Hidden Person")
	var standings []state.PlayerStanding
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &standings))
	require.Len(t, standings, 3)
	for _, s := range standings {
		assert.NotEmpty(t, s.Player.ID, "ids stay")
		assert.Regexp(t, `^L\d+$`, s.Player.Name, "the number stands where the name was")
		assert.Equal(t, s.Player.Name, s.Player.DisplayName)
	}

	w = get("secret-pw")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Hidden Person1")

	assert.Equal(t, http.StatusUnauthorized, get("stale").Code)
}
