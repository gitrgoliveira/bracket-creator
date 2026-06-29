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
// match-scoped PUT is refused with 409 ONLY when it changes an already-recorded
// position. A NEW lineup (no prior entry) while the match is running must
// succeed, that is the normal "live table entry" flow.
func TestMatchLineupPUT_409WhenMatchLive(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: "teamA", SideB: "teamB", Status: state.MatchStatusRunning},
	}))

	// First PUT: no prior lineup, new lineup while running must succeed.
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(validPositionsBody()))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "new lineup while match is running must succeed: "+w.Body.String())

	// Second PUT: CHANGING a recorded position while running → 409.
	changeBody, _ := json.Marshal(LineupRequest{Positions: map[domain.Position]string{
		domain.PosSenpo:   "p1-substitute", // change recorded senpo
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
	assert.Equal(t, http.StatusConflict, w2.Code, w2.Body.String())
	// The 409 must use a match-accurate message, not the sentinel's
	// "round has started" text.
	assert.Contains(t, w2.Body.String(), "match has started")
	assert.NotContains(t, w2.Body.String(), "round has started")
}

// TestMatchLineupPUT_ForceOverridesLiveLock: with force=true and a changeReason
// the operator can still set a lineup on a running match (officiated-mode
// override), the same request that 409s without force succeeds with it.
func TestMatchLineupPUT_ForceOverridesLiveLock(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: "teamA", SideB: "teamB", Status: state.MatchStatusRunning},
	}))

	body, _ := json.Marshal(LineupRequest{
		Force:        true,
		ChangeReason: "Substitution: injury to jiho",
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1", domain.PosJiho: "p2", domain.PosChuken: "p3",
			domain.PosFukusho: "p4", domain.PosTaisho: "p5",
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "PoolA-0")
}

// TestMatchLineupPUT_ForceRequiresChangeReason: force=true without changeReason
// must return 400.
func TestMatchLineupPUT_ForceRequiresChangeReason(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: "teamA", SideB: "teamB", Status: state.MatchStatusRunning},
	}))

	body, _ := json.Marshal(LineupRequest{
		Force: true, // no ChangeReason
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1", domain.PosJiho: "p2", domain.PosChuken: "p3",
			domain.PosFukusho: "p4", domain.PosTaisho: "p5",
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "changeReason")
}

// TestMatchLineupPUT_ForceChangeReasonPersisted: the changeReason submitted
// with a force=true lineup is persisted and survives a reload.
func TestMatchLineupPUT_ForceChangeReasonPersisted(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: "teamA", SideB: "teamB", Status: state.MatchStatusRunning},
	}))

	wantReason := "Substitution: injury to jiho"
	body, _ := json.Marshal(LineupRequest{
		Force:        true,
		ChangeReason: wantReason,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1", domain.PosJiho: "p2", domain.PosChuken: "p3",
			domain.PosFukusho: "p4", domain.PosTaisho: "p5",
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Verify the reason survived the write and reload.
	lineups, err := store.LoadTeamLineups("c1")
	require.NoError(t, err)
	saved, found := findMatchLineup(lineups, "teamA", "PoolA-0")
	require.True(t, found, "lineup must be present after force-write")
	assert.Equal(t, wantReason, saved.ChangeReason)
}

// TestMatchLineupPUT_ForcePreMatchRejected: force=true is rejected (400) when
// the target match has not started, the override path is mid-match only, so a
// client cannot use it (or persist an audit reason) on a normal pre-match edit.
func TestMatchLineupPUT_ForcePreMatchRejected(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: "teamA", SideB: "teamB", Status: state.MatchStatusScheduled},
	}))

	body, _ := json.Marshal(LineupRequest{
		Force:        true,
		ChangeReason: "should not be allowed pre-match",
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1", domain.PosJiho: "p2", domain.PosChuken: "p3",
			domain.PosFukusho: "p4", domain.PosTaisho: "p5",
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// TestMatchLineupPUT_NoCompetition: PUT to a missing competition → 404.
func TestMatchLineupPUT_NoCompetition(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))

	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/ghost/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(validPositionsBody()))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// TestMatchLineupPUT_ZeroTeamSize: a non-team competition (teamSize 0) → 400.
func TestMatchLineupPUT_ZeroTeamSize(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 0}))

	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(validPositionsBody()))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "team play")
}

// TestMatchLineupPUT_ValidationError: a lineup with an INVALID position key
// (not a recognised FIK name for a 5-person team) → 400. Note: a partial
// lineup with only valid keys (e.g. only Jiho set) is accepted, completeness
// is not enforced at write time (non-blocking UI warning only).
func TestMatchLineupPUT_ValidationError(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	// "chudan" is not a valid FIK position name for a 5-person team.
	body, _ := json.Marshal(LineupRequest{Positions: map[domain.Position]string{"chudan": "p2"}})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
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

// TestMatchLineupDELETE_RequiresAuth: the match-scoped DELETE is on the
// admin group. A regression that registered it outside the admin group
// would let a no-password DELETE through, this asserts it doesn't.
func TestMatchLineupDELETE_RequiresAuth(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	req := httptest.NewRequest(http.MethodDelete,
		"/api/competitions/c1/teams/teamA/match-lineups/PoolA-0", nil)
	// No password header.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
