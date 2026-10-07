package mobileapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLineupSetStatus pins the SetTeamLineup error classification: a domain
// lineup validation error (team_lineup: prefix) is a 400, while a YAML/disk
// fault is a 500 (not misreported as a bad request).
func TestLineupSetStatus(t *testing.T) {
	assert.Equal(t, http.StatusBadRequest,
		lineupSetStatus(domain.ErrLineupTeamSizeInvalid), "validation sentinel -> 400")
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
	RegisterPublicLineupHandlers(api, store, store, engine.New(store))

	// Admin group, AuthMiddleware gates all writes
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterLineupHandlers(admin, store, store, store, stubBroadcaster{}, store, NewFileVerifier(store))

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

	t.Run("no lineup answers 200 with an empty lineup, saved false, no auth", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/competitions/c1/teams/teamA/lineups/1", nil)
		// Deliberately no X-Tournament-Password header
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		// Raw body (map[string]any), not a domain.TeamLineup decode: a
		// struct decode would silently miss `saved` going missing, which
		// is exactly the regression this test exists to catch.
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, false, body["saved"], "nothing saved -> saved: false, never omitted")
		assert.Equal(t, map[string]any{}, body["positions"], "empty positions, not null")
		assert.Equal(t, "teamA", body["teamId"])
		assert.Equal(t, "c1", body["competitionId"])
		assert.Equal(t, float64(1), body["round"], "echoes the round asked for")
		assert.NotContains(t, body, "matchId", "the round route never carries a matchId")
		assert.NotContains(t, body, "memberIds")
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
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, true, body["saved"], "a lineup that was actually set answers saved: true")
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
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, true, body["saved"])
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
			"/api/competitions/c1/teams/teamA/lineups/0",
			bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("correct password header succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut,
			"/api/competitions/c1/teams/teamA/lineups/0",
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
		"/api/competitions/no-such-comp/teams/teamA/lineups/0",
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
		"/api/competitions/c1/teams/teamA/lineups/0",
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
		"/api/competitions/c1/teams/teamA/lineups/0",
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
		"/api/competitions/c1/teams/teamA/lineups/0",
		bytes.NewBufferString("{bad-json"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestLineupPUT_RoundsAboveZeroAreRefused: a lineup is saved as the team's
// starting lineup (round 0) or for a match. The Lineups page no longer saves a
// lineup for a later round and a team carries the lineup of its previous match,
// so a PUT for round 1 or later answers 400 in plain words, stores nothing, and
// answers before it reads the body or looks for the competition. GET and
// DELETE for those rounds still answer, for what an older release left.
func TestLineupPUT_RoundsAboveZeroAreRefused(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	const sentence = "A lineup is saved as the team's starting lineup or for a match."

	put := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	const valid = `{"positions":{"senpo":"p1"}}`

	for _, round := range []string{"1", "2", "10"} {
		t.Run("round "+round+" is refused and stores nothing", func(t *testing.T) {
			w := put("/api/competitions/c1/teams/teamA/lineups/"+round, valid)

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, sentence, body["error"])
			lineups, err := store.LoadTeamLineups("c1")
			require.NoError(t, err)
			assert.Empty(t, lineups)
		})
	}

	t.Run("the refusal does not wait for the body or the competition", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, put("/api/competitions/c1/teams/teamA/lineups/1", "{bad-json").Code)
		w := put("/api/competitions/no-such-comp/teams/teamA/lineups/1", valid)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), sentence)
	})

	t.Run("round 0, the starting lineup, is still saved", func(t *testing.T) {
		w := put("/api/competitions/c1/teams/teamA/lineups/0", valid)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		lineups, err := store.LoadTeamLineups("c1")
		require.NoError(t, err)
		assert.Len(t, lineups, 1)
	})
}

// TestLineupPUT_MemberIDsRoundTrip (bc-tmid pass 3): a PUT body carrying
// both "positions" and "memberIds" persists both, and the response (and a
// subsequent GET) return the same memberIds keyed by the same positions.
func TestLineupPUT_MemberIDsRoundTrip(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	body, _ := json.Marshal(map[string]any{
		"positions": map[string]string{
			"senpo": "Sato",
			"jiho":  "Ito",
		},
		"memberIds": map[string]string{
			"senpo": "member-sato",
			"jiho":  "member-ito",
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/lineups/0",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var putResp domain.TeamLineup
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &putResp))
	assert.Equal(t, "member-sato", putResp.MemberIDs[domain.PosSenpo], "PUT response carries memberIds")
	assert.Equal(t, "member-ito", putResp.MemberIDs[domain.PosJiho])

	getReq := httptest.NewRequest(http.MethodGet, "/api/competitions/c1/teams/teamA/lineups/0", nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code)
	var getResp domain.TeamLineup
	require.NoError(t, json.Unmarshal(getW.Body.Bytes(), &getResp))
	assert.Equal(t, "member-sato", getResp.MemberIDs[domain.PosSenpo], "GET returns the persisted memberIds")
	assert.Equal(t, "member-ito", getResp.MemberIDs[domain.PosJiho])
	assert.Equal(t, "Sato", getResp.Positions[domain.PosSenpo], "the name half is unaffected")
}

// TestLineupPUT_MemberIDsOmitted_BehavesAsBefore (bc-tmid pass 3): a PUT
// body that never mentions "memberIds" at all (an older client) must persist
// and round-trip exactly as it did before this field existed: MemberIDs
// stays empty/absent, never invented, and the write is not rejected.
func TestLineupPUT_MemberIDsOmitted_BehavesAsBefore(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	body, _ := json.Marshal(map[string]any{
		"positions": map[string]string{
			"senpo": "Sato",
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/lineups/0",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.NotContains(t, w.Body.String(), "memberIds",
		"omitempty must drop the field entirely for a client that never sent it")

	var putResp domain.TeamLineup
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &putResp))
	assert.Empty(t, putResp.MemberIDs)
	assert.Equal(t, "Sato", putResp.Positions[domain.PosSenpo])
}

// TestLineupPUT_MemberIDsInvalidPositionKey (bc-tmid pass 3): an illegal
// position key inside "memberIds" (not a valid FIK name for a 5-person
// team) is refused with 400, exactly like an illegal "positions" key,
// because both walk through the same ValidatePositions check.
func TestLineupPUT_MemberIDsInvalidPositionKey(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	body, _ := json.Marshal(map[string]any{
		"positions": map[string]string{
			"senpo": "Sato",
		},
		"memberIds": map[string]string{
			"chudan": "member-x",
		},
	})
	req := httptest.NewRequest(http.MethodPut,
		"/api/competitions/c1/teams/teamA/lineups/0",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestPublicLineupGET_RoundIsExact: the round GET reads exactly the round asked
// for. A team with a round-0 lineup and nothing for round 1 answers "nothing
// saved" for round 1, echoing the round asked for and never a lineup swapped in
// from another round: the Lineups page reads its starting lineup (round 0) this
// way. What a team fields at a match is lineup-in-force, not this route.
func TestPublicLineupGET_RoundIsExact(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "c-exact",
		TeamSize: 5,
	}))
	require.NoError(t, store.SetTeamLineup("c-exact", domain.TeamLineup{
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

	get := func(t *testing.T, path string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	// The second query is what an older bundle may still send.
	for _, query := range []string{"", "?fallback=best"} {
		t.Run("an exact miss answers nothing saved"+query, func(t *testing.T) {
			body := get(t, "/api/competitions/c-exact/teams/teamA/lineups/1"+query)
			assert.Equal(t, false, body["saved"])
			assert.Equal(t, map[string]any{}, body["positions"])
			assert.Equal(t, float64(1), body["round"], "echoes the requested round, not another's")
		})
	}

	t.Run("an exact hit is the round asked for", func(t *testing.T) {
		body := get(t, "/api/competitions/c-exact/teams/teamA/lineups/0")
		assert.Equal(t, true, body["saved"])
		assert.Equal(t, float64(0), body["round"])
		assert.Equal(t, "p1", body["positions"].(map[string]any)["senpo"])
	})

	t.Run("a match-scoped lineup of the team never answers a round read", func(t *testing.T) {
		require.NoError(t, store.SetTeamLineup("c-exact", domain.TeamLineup{
			TeamID:  "teamB",
			MatchID: "Pool A-0",
			Positions: map[domain.Position]string{
				domain.PosSenpo: "m1",
			},
		}, 5))
		body := get(t, "/api/competitions/c-exact/teams/teamB/lineups/0")
		assert.Equal(t, false, body["saved"], "a match-scoped entry is not a round's lineup")
	})
}

// TestPublicLineupGET_UnknownCompetition: both lineup GETs 404 with
// "competition not found" when the competition itself does not exist
// (bc-k404). The status alone is already 404 on main (an unreadable
// competition directory falls through to the same "nothing saved" 404 a
// real miss gives), so this asserts the BODY: the distinct message is
// what tells the two apart now that "nothing saved" no longer 404s.
func TestPublicLineupGET_UnknownCompetition(t *testing.T) {
	r, _, _ := setupLineupTestRouter(t)

	for _, tc := range []struct {
		name, path string
	}{
		{"round", "/api/competitions/no-such-comp/teams/teamA/lineups/1"},
		{"match", "/api/competitions/no-such-comp/teams/teamA/match-lineups/m1"},
		{"in force", "/api/competitions/no-such-comp/teams/teamA/lineup-in-force/m1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusNotFound, w.Code)
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "competition not found", body["error"])
		})
	}
}

// TestPublicLineupGET_BadParamsStay400 pins the CHECK ORDER (bc-k404): a
// malformed param 400s before the handler ever asks whether the
// competition exists, on both routes. Already green on main (today's
// handlers have no existence check to race against), which is the
// point -- this test exists to keep that order true now that
// requireExistingCompetition has been added, not to prove something new.
func TestPublicLineupGET_BadParamsStay400(t *testing.T) {
	r, _, _ := setupLineupTestRouter(t)

	for _, tc := range []struct {
		name, path string
	}{
		{"bad competition id format", "/api/competitions/bad.id/teams/teamA/lineups/1"},
		{"empty team id", "/api/competitions/c1/teams//lineups/1"},
		{"non-integer round", "/api/competitions/c1/teams/teamA/lineups/abc"},
		{"negative round", "/api/competitions/c1/teams/teamA/lineups/-1"},
		{"unknown competition and bad round together", "/api/competitions/no-such-comp/teams/teamA/lineups/abc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}

// inForceBody GETs the lineup-in-force route as an anonymous caller and decodes
// the RAW body, so a field going missing fails here rather than reading as its
// zero value.
func inForceBody(t *testing.T, r *gin.Engine, compID, teamID, matchID string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/api/competitions/"+compID+"/teams/"+url.PathEscape(teamID)+"/lineup-in-force/"+url.PathEscape(matchID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

// TestPublicLineupInForceGET answers which lineup a team fields at a match
// (operator ruling 2026-10-05) for the three shapes it can come from, and
// nothing. Public like the other lineup reads, and never a 404 for "nothing
// saved".
func TestPublicLineupInForceGET(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "Pool A-0", SideA: "A", SideAID: "teamA", SideB: "B", SideBID: "teamB"},
		{ID: "Pool A-1", SideA: "A", SideAID: "teamA", SideB: "C", SideBID: "teamC"},
	}))
	save := func(l domain.TeamLineup) {
		l.Positions = map[domain.Position]string{domain.PosSenpo: "p-" + l.MatchID + "-" + string(rune('0'+l.Round))}
		require.NoError(t, store.SetTeamLineup("c1", l, 5))
	}

	t.Run("nothing saved is a 200 with saved false, echoing what was asked", func(t *testing.T) {
		body := inForceBody(t, r, "c1", "teamA", "Pool A-0")

		assert.Equal(t, false, body["saved"], "never omitted")
		assert.Equal(t, map[string]any{}, body["positions"], "empty positions, not null")
		assert.Equal(t, "teamA", body["teamId"])
		assert.Equal(t, "c1", body["competitionId"])
		assert.Equal(t, "Pool A-0", body["matchId"])
		assert.NotContains(t, body, "sourceMatchId")
		assert.NotContains(t, body, "sourceRound")
	})

	t.Run("a Lineups-page lineup names its round, round 0 being the starting lineup", func(t *testing.T) {
		save(domain.TeamLineup{TeamID: "teamA", Round: 0})
		body := inForceBody(t, r, "c1", "teamA", "Pool A-0")

		assert.Equal(t, true, body["saved"])
		assert.Equal(t, float64(0), body["sourceRound"], "round 0 is present, not omitted")
		assert.NotContains(t, body, "sourceMatchId")
		assert.Equal(t, map[string]any{"senpo": "p--0"}, body["positions"])
	})

	t.Run("a lineup saved for the match is its own", func(t *testing.T) {
		save(domain.TeamLineup{TeamID: "teamA", MatchID: "Pool A-0"})
		body := inForceBody(t, r, "c1", "teamA", "Pool A-0")

		assert.Equal(t, true, body["saved"])
		assert.Equal(t, "Pool A-0", body["sourceMatchId"])
		assert.NotContains(t, body, "sourceRound")
		assert.Equal(t, map[string]any{"senpo": "p-Pool A-0-0"}, body["positions"])
	})

	t.Run("the team's next match carries it, and names the match it came from", func(t *testing.T) {
		body := inForceBody(t, r, "c1", "teamA", "Pool A-1")

		assert.Equal(t, true, body["saved"])
		assert.Equal(t, "Pool A-0", body["sourceMatchId"])
		assert.NotContains(t, body, "sourceRound")
		assert.Equal(t, map[string]any{"senpo": "p-Pool A-0-0"}, body["positions"])
	})

	t.Run("another team carries nothing of it", func(t *testing.T) {
		body := inForceBody(t, r, "c1", "teamC", "Pool A-1")

		assert.Equal(t, false, body["saved"])
	})
}

// A damaged match file costs the read the part of the draw it held, never the
// read: the lineups are intact, so a pool match's carried lineup is still
// answered when bracket.json cannot be read, and the 500 stays for a lineups
// file that cannot be (TestPublicLineupInForceGET_ReadFailureIs500).
func TestPublicLineupInForceGET_AnUnreadableBracketStillAnswers(t *testing.T) {
	r, store, dir := setupLineupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "Pool A-0", SideA: "A", SideAID: "teamA", SideB: "B", SideBID: "teamB"},
		{ID: "Pool A-1", SideA: "A", SideAID: "teamA", SideB: "C", SideBID: "teamC"},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "bracket.json"), []byte("{not json"), 0o600))
	require.NoError(t, store.SetTeamLineup("c1", domain.TeamLineup{
		TeamID: "teamA", MatchID: "Pool A-0", Positions: map[domain.Position]string{domain.PosSenpo: "carried"},
	}, 5))

	body := inForceBody(t, r, "c1", "teamA", "Pool A-1")

	assert.Equal(t, true, body["saved"])
	assert.Equal(t, "Pool A-0", body["sourceMatchId"], "carried from the previous match, which the pool file still places")
	assert.Equal(t, map[string]any{"senpo": "carried"}, body["positions"])
}

// TestPublicLineupInForceGET_NoAuthRequired: like the other lineup reads, no
// password is asked even when the tournament has one.
func TestPublicLineupInForceGET_NoAuthRequired(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/c1/teams/teamA/lineup-in-force/m1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestPublicLineupInForceGET_BadParamsStay400(t *testing.T) {
	r, _, _ := setupLineupTestRouter(t)

	for _, tc := range []struct {
		name, path string
	}{
		{"bad competition id format", "/api/competitions/bad.id/teams/teamA/lineup-in-force/m1"},
		{"empty team id", "/api/competitions/c1/teams//lineup-in-force/m1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}

// failingLineupEngine is a LineupEngine whose read fails.
type failingLineupEngine struct{}

func (failingLineupEngine) LineupInForce(string, string, string) (engine.InForceLineup, error) {
	return engine.InForceLineup{}, errors.New("read lineups.yaml: input/output error")
}

// A read that fails is the server's fault: a 500, not an empty "nothing saved"
// that would let the sheet show a blank lineup over one it could not read.
func TestPublicLineupInForceGET_ReadFailureIs500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	r := gin.New()
	RegisterPublicLineupHandlers(r.Group("/api"), store, store, failingLineupEngine{})

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/c1/teams/teamA/lineup-in-force/m1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}
