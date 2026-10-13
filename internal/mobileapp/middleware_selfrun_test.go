package mobileapp

// Tests for the self-run tournament mode auth boundary (mp-7h7).
//
// Categories:
//  1. Auth matrix: officiated vs self-run × public vs organiser routes
//  2. No second password: self-run creation needs none, an old one is ignored
//  3. Immutability: POST persists mode; PUT switch → 400; PUT omitting mode → preserved
//  4. ValidateTournamentMode + ApplyTournamentDefaults unit tests
//  5. Locked + self-run: organiser routes need the main password

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// setupSelfRunRouter creates a full NewRouterWithHub with the given verifier
// and a tournament pre-seeded in the store. Returns the router.
func setupSelfRunRouter(t *testing.T, store *state.Store, verifier PasswordVerifier) *gin.Engine {
	t.Helper()
	eng := engine.New(store)
	mockFS := fstest.MapFS{
		"web-mobile/index.html": {Data: []byte("<html>test</html>")},
	}
	res := resources.NewResources(nil, mockFS)
	r, _, limiter := NewRouterWithHub(store, eng, res, verifier, NewHub(), false)
	t.Cleanup(limiter.Close)
	return r
}

// seedSelfRunTournament saves a self-run tournament with a main password.
func seedSelfRunTournament(t *testing.T, store *state.Store) {
	t.Helper()
	err := store.SaveTournament(&state.Tournament{
		Name:     "SelfRun Test",
		Date:     "01-06-2026",
		Venue:    "Dojo",
		Courts:   []string{"A"},
		Password: "main-pw",
		Mode:     state.TournamentModeSelfRun,
	})
	require.NoError(t, err)
}

// seedOfficiatedTournament saves a standard officiated tournament.
func seedOfficiatedTournament(t *testing.T, store *state.Store) {
	t.Helper()
	err := store.SaveTournament(&state.Tournament{
		Name:     "Officiated Test",
		Date:     "01-06-2026",
		Venue:    "Dojo",
		Courts:   []string{"A"},
		Password: "main-pw",
		Mode:     state.TournamentModeOfficiated,
	})
	require.NoError(t, err)
}

func newTempStore(t *testing.T) *state.Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "selfrun-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	return store
}

// jsonReq builds a JSON request.
func jsonReq(method, path string, body any) *http.Request {
	var buf *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	} else {
		buf = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	return req
}

// ---------------------------------------------------------------------------
// §1 Auth matrix: officiated mode (baseline, no regression)
// ---------------------------------------------------------------------------

// In officiated mode every admin route still requires X-Tournament-Password.
func TestSelfRun_OfficiatedMode_RequiresMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	constructiveRoutes := []struct{ method, path string }{
		{http.MethodGet, "/api/tournament"},
		{http.MethodGet, "/api/competitions"},
		{http.MethodPost, "/api/competitions"},
	}

	for _, tc := range constructiveRoutes {
		t.Run("officiated_no_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"officiated mode must require main password on %s %s", tc.method, tc.path)
		})

		t.Run("officiated_correct_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Tournament-Password", "main-pw")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			// Not checking for 200 because competition/tournament resources
			// may not exist, just assert it's not a 401.
			assert.NotEqual(t, http.StatusUnauthorized, w.Code,
				"correct password must clear the main gate on %s %s", tc.method, tc.path)
		})
	}
}

// ---------------------------------------------------------------------------
// §1 Auth matrix: self-run mode
// ---------------------------------------------------------------------------

// Constructive routes are public (no main password needed) in self-run mode.
func TestSelfRun_SelfRunMode_ConstructiveRoutesArePublic(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// Routes that are main-gated in officiated mode and are not organiser
	// routes (isSelfRunMainGatedConfigRoute) become public in self-run. We test a subset because full competition state
	// is complex to seed. The key invariant is: no 401 from the main gate.
	//
	// Note: GET /api/tournament is intentionally NOT in this list. In file
	// mode it returns the Tournament struct including the Password field. If
	// it were public in self-run, an anonymous caller could read the main
	// password and use it to bypass the main gate on gated config routes
	// (Copilot #203 finding 3329406556). It is kept in isSelfRunMainGatedConfigRoute.
	constructiveRoutes := []struct{ method, path string }{
		{http.MethodGet, "/api/competitions"},
		// bc-cmpw: completing a competition is open in self-run, like
		// starting one (operator ruling 2026-10-12). The handler 404s the
		// unknown id; the point is that no gate refused it.
		{http.MethodPost, "/api/competitions/nonexistent-id/complete"},
	}

	for _, tc := range constructiveRoutes {
		t.Run("selfrun_no_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.NotEqual(t, http.StatusUnauthorized, w.Code,
				"self-run constructive route %s %s must not return 401 (public)", tc.method, tc.path)
		})
	}
}

// GET /api/tournament is in the main-gated carve-out for self-run because in
// file mode it returns the Tournament struct including the Password field
// (Copilot #203 fix 3329406556). An anonymous read would leak the credential
// and allow the caller to bypass the PUT /api/tournament gate.
func TestSelfRun_SelfRunMode_GETTournament_RequiresMainPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	t.Run("no_pw_returns_401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"GET /api/tournament must return 401 without main password in self-run mode (file-mode password leak prevention)")
	})

	t.Run("correct_pw_succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
		req.Header.Set("X-Tournament-Password", "main-pw")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code,
			"GET /api/tournament must succeed with the main password in self-run mode")
	})
}

// PUT /api/tournament is a tournament-configuration mutation (password, courts,
// check-in windows). In self-run mode the operational pass-through does NOT
// apply to it, the main gate fires normally so anonymous callers cannot
// tamper with tournament setup. The SPA always sends X-Tournament-Password
// on PUT /api/tournament even in self-run mode, so this is transparent.
func TestSelfRun_SelfRunMode_PUTTournament_RequiresMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	putBody := map[string]any{
		"name":   "SelfRun Test",
		"date":   "01-06-2026",
		"venue":  "Dojo",
		"courts": []string{"A"},
	}

	t.Run("no_pw_returns_401", func(t *testing.T) {
		req := jsonReq(http.MethodPut, "/api/tournament", putBody)
		// No X-Tournament-Password, must be rejected (configuration mutation).
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"PUT /api/tournament must return 401 without main password even in self-run mode")
	})

	t.Run("correct_pw_succeeds", func(t *testing.T) {
		req := jsonReq(http.MethodPut, "/api/tournament", putBody)
		req.Header.Set("X-Tournament-Password", "main-pw")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code,
			"PUT /api/tournament must succeed with correct main password in self-run mode")
	})
}

// Competition configuration mutations (create a competition, edit competition
// config) are organiser setup, NOT operational play, like PUT /api/tournament
// they stay main-gated in self-run mode (mp-7h7 / Copilot #203 sibling audit).
// Without this an anonymous client could create or reconfigure competitions in
// a self-run tournament. These are organiser routes, so the main gate
// is the protection. The organiser's SPA always sends X-Tournament-Password.
func TestSelfRun_SelfRunMode_CompetitionConfigRoutes_RequireMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	configRoutes := []struct{ method, path string }{
		{http.MethodPost, "/api/competitions"},
		{http.MethodPut, "/api/competitions/some-id"},
		// Both xlsx exports are admin/CPU-heavy config routes, not operational
		// play, so they stay main-password gated even in self-run mode (mp-i96p).
		{http.MethodGet, "/api/competitions/some-id/export"},
		{http.MethodGet, "/api/competitions/some-id/export-results"},
		// Team members (bc-tmid): the read and the name clear stay organiser
		// setup. Adding and naming a member are public in self-run, because
		// the public score sheet names fighters through them
		// (self_run_team_writes_test.go).
		{http.MethodGet, "/api/competitions/some-id/team-members"},
		// bc-pnum: clearing a member's name is the operator's "removal".
		{http.MethodDelete, "/api/competitions/some-id/teams/some-team/members/some-member"},
		// bc-mrgc: a match's write history, held values included, is the
		// score editors' organiser view; the public page never reads it.
		{http.MethodGet, "/api/competitions/some-id/matches/some-match/history"},
		// A team league's tie-breaker decisions (run supplementary bouts,
		// remove them, or accept the shared ranks) are the organiser's, as a
		// pool rank override is.
		{http.MethodPost, "/api/competitions/some-id/league-tiebreak"},
		{http.MethodDelete, "/api/competitions/some-id/league-tiebreak"},
		{http.MethodPost, "/api/competitions/some-id/league-tiebreak/finalize"},
	}
	for _, fullPath := range []string{"/api/competitions/:id/league-tiebreak", "/api/competitions/:id/league-tiebreak/finalize"} {
		assert.True(t, isSelfRunMainGatedConfigRoute(http.MethodPost, fullPath), "POST %s must be organiser-only in self-run", fullPath)
	}
	assert.True(t, isSelfRunMainGatedConfigRoute(http.MethodDelete, "/api/competitions/:id/league-tiebreak"),
		"DELETE /api/competitions/:id/league-tiebreak must be organiser-only in self-run")

	for _, tc := range configRoutes {
		t.Run("no_pw_401_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := jsonReq(tc.method, tc.path, map[string]any{"name": "X"})
			// No X-Tournament-Password: a config mutation must be rejected.
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"self-run config route %s %s must return 401 without main password", tc.method, tc.path)
		})

		t.Run("with_pw_not_401_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := jsonReq(tc.method, tc.path, map[string]any{"name": "X"})
			req.Header.Set("X-Tournament-Password", "main-pw")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			// Past the main gate, downstream may 400/404 on the body/id, but
			// never 401 (the invariant under test).
			assert.NotEqual(t, http.StatusUnauthorized, w.Code,
				"correct main password must clear the gate on %s %s (got %d)", tc.method, tc.path, w.Code)
		})
	}
}

// formerlyElevatedOrganiserRoutes lists every route that needed the separate
// destructive-ops password before it was removed (operator ruling 2026-10-12:
// "not re enter admin password to change something. For self managed, no
// admin password necessary"), plus the bulk competition PUT, whose roster path
// asked for it inline. Each is an ORGANISER route now: the admin sign-in alone
// in officiated mode, and organiser-only in self-run
// (isSelfRunMainGatedConfigRoute), so none became public. POST /complete, the
// one other route that password guarded, is public in self-run and is pinned
// by TestSelfRun_Complete_IsPublicAndKeepsTheBracketGuard. The roster add and
// edit were among them too and are open to anyone in self-run (operator
// ruling 2026-10-12: "anyone can edit the roster"): selfRunOpenRosterRoutes.
var formerlyElevatedOrganiserRoutes = []struct{ method, fullPath, path string }{
	{http.MethodDelete, "/api/competitions/:id", "/api/competitions/nonexistent-id"},
	{http.MethodPut, "/api/competitions/:id", "/api/competitions/nonexistent-id"},
	{http.MethodPost, "/api/competitions/:id/invalidate", "/api/competitions/nonexistent-id/invalidate"},
	{http.MethodDelete, "/api/competitions/:id/draw", "/api/competitions/nonexistent-id/draw"},
	{http.MethodPost, "/api/competitions/:id/bracket/quarantine", "/api/competitions/nonexistent-id/bracket/quarantine"},
	{http.MethodDelete, "/api/competitions/:id/overrides", "/api/competitions/nonexistent-id/overrides"},
	{http.MethodPut, "/api/competitions/:id/awards", "/api/competitions/nonexistent-id/awards"},
	{http.MethodPost, "/api/tournament/import", "/api/tournament/import"},
}

// selfRunOpenRosterRoutes are the roster add and edit: the admin sign-in in
// officiated mode, open to an anonymous caller in self-run.
var selfRunOpenRosterRoutes = []struct{ method, fullPath, path string }{
	{http.MethodPost, "/api/competitions/:id/participants", "/api/competitions/nonexistent-id/participants"},
	{http.MethodPut, "/api/competitions/:id/participants/:pid", "/api/competitions/nonexistent-id/participants/some-pid"},
}

// In self-run mode anyone may add and edit a competition's participants: an
// anonymous caller (an EMPTY password, as the public page sends) reaches both
// handlers and the change is stored.
func TestSelfRun_SelfRunMode_RosterAddAndEdit_AreOpenToAnyone(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	for _, tc := range selfRunOpenRosterRoutes {
		assert.False(t, isSelfRunMainGatedConfigRoute(tc.method, tc.fullPath),
			"%s %s must stay out of isSelfRunMainGatedConfigRoute", tc.method, tc.fullPath)
	}

	const compID = "open-roster"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Open Roster", Status: state.CompStatusSetup}))

	add := jsonReq(http.MethodPost, "/api/competitions/"+compID+"/participants", map[string]any{"name": "Alice", "dojo": "Dojo A"})
	add.Header.Set("X-Tournament-Password", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, add)
	require.Less(t, w.Code, 300, "an anonymous self-run caller must add a participant: %d %s", w.Code, w.Body.String())

	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	require.NotEmpty(t, players[0].ID)

	edit := jsonReq(http.MethodPut, "/api/competitions/"+compID+"/participants/"+players[0].ID, map[string]any{"name": "Alice Edited", "dojo": "Dojo A"})
	edit.Header.Set("X-Tournament-Password", "")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, edit)
	require.Less(t, w.Code, 300, "an anonymous self-run caller must edit a participant: %d %s", w.Code, w.Body.String())

	players, err = store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, "Alice Edited", players[0].Name)
}

// In self-run mode every formerly elevated route is organiser-only: refused
// without the main password, and the old admin header alone grants nothing.
func TestSelfRun_SelfRunMode_FormerlyElevatedRoutes_AreOrganiserOnly(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	for _, tc := range formerlyElevatedOrganiserRoutes {
		assert.True(t, isSelfRunMainGatedConfigRoute(tc.method, tc.fullPath),
			"%s %s must be in isSelfRunMainGatedConfigRoute", tc.method, tc.fullPath)

		t.Run("selfrun_no_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"self-run organiser route %s %s must return 401 without the main password", tc.method, tc.path)
		})

		t.Run("selfrun_old_admin_header_alone_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Admin-Password", "main-pw")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"the removed X-Admin-Password header must grant nothing on %s %s", tc.method, tc.path)
		})

		t.Run("selfrun_main_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Tournament-Password", "main-pw")
			req.Header.Set("X-Confirm-Password", "main-pw") // only DELETE /competitions/:id reads it
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			// Past the gate the handler answers for the unknown id or the
			// empty body (400/404); no second password is asked for.
			assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable}, w.Code,
				"the main password alone must reach %s %s (got %d: %s)", tc.method, tc.path, w.Code, w.Body.String())
		})
	}
}

// In officiated mode the signed-in admin performs every formerly elevated
// action with the main password alone.
func TestOfficiatedMode_FormerlyElevatedRoutes_NeedOnlyTheSignIn(t *testing.T) {
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	for _, tc := range append(slices.Clone(formerlyElevatedOrganiserRoutes), selfRunOpenRosterRoutes...) {
		t.Run("officiated_no_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"officiated %s %s must require the sign-in", tc.method, tc.path)
		})
		t.Run("officiated_main_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Tournament-Password", "main-pw")
			req.Header.Set("X-Confirm-Password", "main-pw") // only DELETE /competitions/:id reads it
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.NotContains(t, []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable}, w.Code,
				"the sign-in alone must reach %s %s (got %d: %s)", tc.method, tc.path, w.Code, w.Body.String())
		})
	}

	t.Run("delete a competition with the sign-in and the password typed again", func(t *testing.T) {
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "to-delete", Name: "To Delete"}))
		req := httptest.NewRequest(http.MethodDelete, "/api/competitions/to-delete", nil)
		req.Header.Set("X-Tournament-Password", "main-pw")
		req.Header.Set("X-Confirm-Password", "main-pw")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Less(t, w.Code, 300, "delete must succeed: %d %s", w.Code, w.Body.String())
		gone, err := store.LoadCompetition("to-delete")
		require.NoError(t, err)
		assert.Nil(t, gone, "the competition must be deleted")
	})

	t.Run("save fighting-spirit awards with the sign-in alone", func(t *testing.T) {
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "awards-comp", Name: "Awards"}))
		req := jsonReq(http.MethodPut, "/api/competitions/awards-comp/awards", map[string]any{
			"fightingSpiritAwards": []map[string]any{{"title": "Spirit", "recipientName": "Alice"}},
		})
		req.Header.Set("X-Tournament-Password", "main-pw")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		saved, err := store.LoadCompetition("awards-comp")
		require.NoError(t, err)
		require.Len(t, saved.FightingSpiritAwards, 1)
	})
}

// seedFinishedKnockout saves a knockout competition whose bracket is fully
// played (final and bronze completed) or, with finished false, whose bronze
// match is still to play.
func seedFinishedKnockout(t *testing.T, store *state.Store, compID string, finished bool) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Format: state.CompFormatKnockout, Status: state.CompStatusKnockout,
	}))
	bronze := state.MatchStatusCompleted
	if !finished {
		bronze = state.MatchStatusScheduled
	}
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-final", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		}},
		ThirdPlaceMatch: &state.BracketMatch{ID: "m-bronze", SideA: "Carol", SideB: "Dave", Status: bronze, Winner: "Carol"},
	}))
}

// Completing a competition asks no second password (operator ruling
// 2026-10-12): in self-run it is public, like starting one; the bracket
// guard still refuses a bracket with a match to play.
func TestSelfRun_Complete_IsPublicAndKeepsTheBracketGuard(t *testing.T) {
	assert.False(t, isSelfRunMainGatedConfigRoute(http.MethodPost, "/api/competitions/:id/complete"),
		"POST /complete must stay public in self-run")

	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	seedFinishedKnockout(t, store, "ko-unfinished", false)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/competitions/ko-unfinished/complete", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "all bracket matches")

	seedFinishedKnockout(t, store, "ko-finished", true)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/competitions/ko-finished/complete", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	done, err := store.LoadCompetition("ko-finished")
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, done.Status)
}

// A competition with no knockout rounds to check (a league, a Swiss
// competition) is finished only once every one of its matches is: an
// anonymous self-run caller, who may complete a competition, cannot seal one
// with a match still to play, since completion is irreversible.
func TestSelfRun_Complete_RefusesALeagueWithAMatchToPlay(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	const compID = "league-unfinished"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools,
	}))
	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	complete := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/complete", nil)
		req.Header.Set("X-Tournament-Password", "")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := complete()
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "all matches must be completed")
	stored, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, stored.Status, "a refused completion must leave the competition unfinished")

	matches[1].Status = state.MatchStatusCompleted
	matches[1].Winner = "Carol"
	require.NoError(t, store.SavePoolMatches(compID, matches))
	w = complete()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, stored.Status)
}

// A Swiss competition is finished only once every configured round has been
// drawn and played: the matches of the rounds drawn so far being complete is
// not enough.
func TestSelfRun_Complete_RefusesASwissCompetitionWithARoundToDraw(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	const compID = "swiss-unfinished"
	comp := &state.Competition{
		ID: compID, Name: compID, Format: state.CompFormatSwiss, Status: state.CompStatusPools,
		SwissRounds: 3, SwissCurrentRound: 2,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Swiss R1-M1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "Swiss R2-M1", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusCompleted, Winner: "Carol"},
	}))

	complete := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/complete", nil)
		req.Header.Set("X-Tournament-Password", "")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	w := complete()
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "round")
	stored, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, stored.Status)

	comp.SwissCurrentRound = 3
	require.NoError(t, store.SaveCompetition(comp))
	w = complete()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// In officiated mode completing needs the sign-in, and nothing more.
func TestOfficiatedMode_Complete_NeedsOnlyTheSignIn(t *testing.T) {
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	seedFinishedKnockout(t, store, "ko-finished", true)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/competitions/ko-finished/complete", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "officiated completion needs the sign-in")

	req := httptest.NewRequest(http.MethodPost, "/api/competitions/ko-finished/complete", nil)
	req.Header.Set("X-Tournament-Password", "main-pw")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	done, err := store.LoadCompetition("ko-finished")
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, done.Status)
}

// ---------------------------------------------------------------------------
// §2 No second password
// ---------------------------------------------------------------------------

// Creating a self-run tournament needs no admin password: organiser routes
// are protected by the main password (isSelfRunMainGatedConfigRoute).
func TestSelfRun_Creation_NeedsNoAdminPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	body := map[string]any{
		"name":     "Self-Run Tournament",
		"date":     "01-06-2026",
		"venue":    "Dojo",
		"courts":   []string{"A"},
		"password": "main-pw",
		"mode":     "self-run",
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, jsonReq(http.MethodPost, "/api/tournament", body))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	stored, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, state.TournamentModeSelfRun, stored.Mode)

	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, httptest.NewRequest(http.MethodDelete, "/api/competitions/some-id", nil))
	assert.Equal(t, http.StatusUnauthorized, wDel.Code, "a destructive route stays organiser-only")
}

// An older client may still send the removed `adminPassword` field, of any
// length; it is ignored and never stored.
func TestSelfRun_Creation_IgnoresALegacyAdminPasswordField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	body := map[string]any{
		"name":          "T",
		"date":          "01-06-2026",
		"venue":         "V",
		"courts":        []string{"A"},
		"password":      "main-pw",
		"mode":          "self-run",
		"adminPassword": string(make([]byte, MaxLenTournamentPassword+1)),
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, jsonReq(http.MethodPost, "/api/tournament", body))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	raw, err := os.ReadFile(filepath.Join(dir, "tournament.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "admin_password", "the legacy field must not be stored")
}

// A tournament.md written by an earlier release with admin_password still
// loads and works; the value is ignored and the next save drops it.
func TestLegacyAdminPasswordInTournamentFile_IsIgnored(t *testing.T) {
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	seedOfficiatedTournament(t, store)
	path := filepath.Join(dir, "tournament.md")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	withLegacy := strings.Replace(string(raw), "password: main-pw\n", "password: main-pw\nadmin_password: old-admin-pw\n", 1)
	require.Contains(t, withLegacy, "admin_password: old-admin-pw")
	require.NoError(t, os.WriteFile(path, []byte(withLegacy), 0o600))

	// A fresh store reads the file from disk.
	store2, err := state.NewStore(dir)
	require.NoError(t, err)
	loaded, err := store2.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "main-pw", loaded.Password)

	r := setupSelfRunRouter(t, store2, NewFileVerifier(store2))
	require.NoError(t, store2.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))
	req := httptest.NewRequest(http.MethodDelete, "/api/competitions/c1", nil)
	req.Header.Set("X-Tournament-Password", "main-pw")
	req.Header.Set("X-Confirm-Password", "main-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Less(t, w.Code, 300, "the sign-in and the password typed again delete a competition: %d %s", w.Code, w.Body.String())

	require.NoError(t, store2.SaveTournament(loaded))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(after), "admin_password", "the next save drops the legacy key")
}

// ---------------------------------------------------------------------------
// §3 Immutability
// ---------------------------------------------------------------------------

// POST self-run → GET mode returns "self-run".
// This test exercises a fresh bootstrap (no prior tournament.md) so the
// AuthMiddleware bootstrap branch fires (anonymous POST allowed): (1) fresh
// bootstrap as officiated, verify the store persisted mode=officiated, then
// (2) a fresh self-run bootstrap, verify mode=self-run.
func TestSelfRun_Immutability_POSTPreservesMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// Step 1: fresh bootstrap as officiated (no auth header needed, bootstrap).
	postBody := map[string]any{
		"name":     "My Officiated",
		"date":     "01-06-2026",
		"venue":    "Dojo",
		"courts":   []string{"A"},
		"password": "main-pw",
		"mode":     "officiated",
	}
	req := jsonReq(http.MethodPost, "/api/tournament", postBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusCreated, w.Code, "officiated POST must succeed: %s", w.Body.String())

	// GET the tournament and verify mode = officiated.
	req2 := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	req2.Header.Set("X-Tournament-Password", "main-pw")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var result map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &result))
	assert.Equal(t, "officiated", result["mode"],
		"GET must return mode=officiated after officiated POST")

	// Step 2: verify self-run mode persists when created as a FRESH tournament
	// (no prior record, true first bootstrap). Use a clean store with no
	// existing tournament so POST acts as first-create (not re-bootstrap).
	// Fix 3329416172 makes re-bootstrap preserve the existing mode (immutability),
	// so we must use a fresh store to actually create a self-run tournament via POST.
	store2 := newTempStore(t)
	r2 := setupSelfRunRouter(t, store2, NewFileVerifier(store2))

	postSelfRun := map[string]any{
		"name":     "My Self-Run",
		"date":     "01-06-2026",
		"venue":    "Dojo",
		"courts":   []string{"A"},
		"password": "main-pw",
		"mode":     "self-run",
	}
	// Fresh bootstrap, no X-Tournament-Password needed (file mode, no record).
	req3 := jsonReq(http.MethodPost, "/api/tournament", postSelfRun)
	w3 := httptest.NewRecorder()
	r2.ServeHTTP(w3, req3)
	require.Equalf(t, http.StatusCreated, w3.Code, "self-run POST must succeed: %s", w3.Body.String())

	// GET the tournament and verify mode = self-run.
	// GET /api/tournament is main-gated in self-run (file-mode password leak
	// prevention, fix 3329406556), so send the main password.
	req4 := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	req4.Header.Set("X-Tournament-Password", "main-pw")
	w4 := httptest.NewRecorder()
	r2.ServeHTTP(w4, req4)
	require.Equal(t, http.StatusOK, w4.Code)
	var result2 map[string]any
	require.NoError(t, json.Unmarshal(w4.Body.Bytes(), &result2))
	assert.Equal(t, "self-run", result2["mode"],
		"GET /api/tournament must return mode=self-run after self-run POST")
}

// PUT that tries to switch officiated → self-run must return 400.
func TestSelfRun_Immutability_PUT_SwitchModeRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// Try to change mode from officiated to self-run via PUT.
	putBody := map[string]any{
		"name":   "Officiated Test",
		"date":   "01-06-2026",
		"venue":  "Dojo",
		"courts": []string{"A"},
		"mode":   "self-run", // attempt to change
	}
	req := jsonReq(http.MethodPut, "/api/tournament", putBody)
	req.Header.Set("X-Tournament-Password", "main-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"PUT switching mode must return 400")
	assert.Contains(t, w.Body.String(), "mode cannot be changed")
}

// PUT that omits the mode field preserves the existing mode.
func TestSelfRun_Immutability_PUT_OmittingModePreservesIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// PUT without mode field, should preserve self-run.
	putBody := map[string]any{
		"name":   "SelfRun Test",
		"date":   "01-06-2026",
		"venue":  "Dojo Updated",
		"courts": []string{"A"},
		// mode omitted
	}
	req := jsonReq(http.MethodPut, "/api/tournament", putBody)
	req.Header.Set("X-Tournament-Password", "main-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "PUT omitting mode must succeed: %s", w.Body.String())

	// Reload and verify mode is still self-run.
	t2, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, t2)
	assert.Equal(t, state.TournamentModeSelfRun, t2.Mode,
		"mode must be preserved when omitted from PUT body")
}

// PUT that sends the same mode as stored is a no-op (idempotent).
func TestSelfRun_Immutability_PUT_SameModeIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	putBody := map[string]any{
		"name":   "SelfRun Test",
		"date":   "01-06-2026",
		"venue":  "Dojo",
		"courts": []string{"A"},
		"mode":   "self-run", // same as stored
	}
	req := jsonReq(http.MethodPut, "/api/tournament", putBody)
	req.Header.Set("X-Tournament-Password", "main-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code,
		"PUT with same mode must succeed (idempotent): %s", w.Body.String())
}

// ---------------------------------------------------------------------------
// §4 ValidateTournamentMode + ApplyTournamentDefaults unit tests
// ---------------------------------------------------------------------------

func TestValidateTournamentMode(t *testing.T) {
	cases := []struct {
		mode  string
		valid bool
	}{
		{"", true},               // empty → officiated (backward compat)
		{"officiated", true},     // explicit default
		{"self-run", true},       // self-run
		{"OFFICIATED", false},    // case-sensitive
		{"Self-Run", false},      // case-sensitive
		{"peer-operated", false}, // unknown value
		{"self_run", false},      // wrong separator
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("mode=%q", tc.mode), func(t *testing.T) {
			err := state.ValidateTournamentMode(tc.mode)
			if tc.valid {
				assert.NoErrorf(t, err, "expected %q to be valid", tc.mode)
			} else {
				assert.Errorf(t, err, "expected %q to be invalid", tc.mode)
			}
		})
	}
}

func TestApplyTournamentDefaults_NormalizesEmptyMode(t *testing.T) {
	t.Run("empty mode → officiated", func(t *testing.T) {
		tour := &state.Tournament{
			Name:     "T",
			Password: "p",
			Mode:     "",
		}
		state.ApplyTournamentDefaults(tour)
		assert.Equal(t, state.TournamentModeOfficiated, tour.Mode)
	})

	t.Run("existing mode preserved", func(t *testing.T) {
		tour := &state.Tournament{
			Name:     "T",
			Password: "p",
			Mode:     state.TournamentModeSelfRun,
		}
		state.ApplyTournamentDefaults(tour)
		assert.Equal(t, state.TournamentModeSelfRun, tour.Mode)
	})

	t.Run("nil tournament is a no-op", func(t *testing.T) {
		// Must not panic.
		state.ApplyTournamentDefaults(nil)
	})

	t.Run("idempotent: double apply keeps officiated", func(t *testing.T) {
		tour := &state.Tournament{Name: "T", Password: "p"}
		state.ApplyTournamentDefaults(tour)
		state.ApplyTournamentDefaults(tour)
		assert.Equal(t, state.TournamentModeOfficiated, tour.Mode)
	})
}

// ---------------------------------------------------------------------------
// §5 Locked mode + self-run
// ---------------------------------------------------------------------------

// In locked mode + self-run the formerly elevated routes are organiser-only:
// 401 without the main password, reached with it. A deployment that still
// sets TOURNAMENT_ADMIN_PASSWORD_HASH starts and works; the value is ignored.
func TestSelfRun_LockedMode_OrganiserRoutes_NeedOnlyTheMainPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(retiredAdminPasswordHashEnv, "$2a$10$retiredretiredretiredretiredretiredretiredretiredre")
	store := newTempStore(t)

	// Build a bcrypt verifier (locked mode).
	hashBytes, err := bcrypt.GenerateFromPassword([]byte("main-pw"), bcrypt.MinCost)
	require.NoError(t, err)
	lockedV, err := NewBcryptVerifier(string(hashBytes))
	require.NoError(t, err)

	// Seed a self-run tournament with empty on-disk Password (locked mode).
	err = store.SaveTournament(&state.Tournament{
		Name:     "LockedSelfRun",
		Date:     "01-06-2026",
		Venue:    "Dojo",
		Courts:   []string{"A"},
		Password: "", // irrelevant in locked mode
		Mode:     state.TournamentModeSelfRun,
	})
	require.NoError(t, err)

	r := setupSelfRunRouter(t, store, lockedV)

	req := httptest.NewRequest(http.MethodDelete, "/api/competitions/any-id", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"locked+self-run organiser route must return 401 without the main password: %s", w.Body.String())

	reqPw := httptest.NewRequest(http.MethodDelete, "/api/competitions/any-id", nil)
	reqPw.Header.Set("X-Tournament-Password", "main-pw")
	reqPw.Header.Set("X-Confirm-Password", "main-pw")
	wPw := httptest.NewRecorder()
	r.ServeHTTP(wPw, reqPw)
	// DELETE of an unknown competition is idempotent: 204 from the handler.
	assert.Equal(t, http.StatusNoContent, wPw.Code,
		"locked+self-run organiser route must reach the handler with the main password typed twice: %s", wPw.Body.String())

	reqComplete := httptest.NewRequest(http.MethodPost, "/api/competitions/any-id/complete", nil)
	wComplete := httptest.NewRecorder()
	r.ServeHTTP(wComplete, reqComplete)
	assert.Equal(t, http.StatusNotFound, wComplete.Code,
		"locked+self-run completion must be public (the handler 404s the unknown id): %s", wComplete.Body.String())

	// Constructive route without any header must NOT be 401 in self-run
	// (main gate is skipped). Use GET /api/competitions, GET /api/tournament
	// is main-gated in self-run to prevent file-mode password leaks (fix 3329406556).
	req2 := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.NotEqual(t, http.StatusUnauthorized, w2.Code,
		"locked+self-run constructive route must be public (not 401)")
}

// Regression (mp-7h7): a routine PUT that edits venue/name on a locked-mode
// self-run tournament MUST succeed. An earlier guard (since removed with the
// destructive-ops password) once rejected it and made locked self-run
// tournaments permanently uneditable.
func TestSelfRun_LockedMode_PUTEdit_NotRejectedByFailOpenGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)

	hashBytes, err := bcrypt.GenerateFromPassword([]byte("main-pw"), bcrypt.MinCost)
	require.NoError(t, err)
	lockedV, err := NewBcryptVerifier(string(hashBytes))
	require.NoError(t, err)

	// Locked-mode self-run tournament: empty on-disk Password (inert; the
	// credential comes from the env var).
	err = store.SaveTournament(&state.Tournament{
		Name:     "LockedSelfRun",
		Date:     "01-06-2026",
		Venue:    "Old Dojo",
		Courts:   []string{"A"},
		Password: "",
		Mode:     state.TournamentModeSelfRun,
	})
	require.NoError(t, err)

	r := setupSelfRunRouter(t, store, lockedV)

	// PUT editing the venue. Tournament-configuration routes (PUT /api/tournament)
	// require the main password even in self-run mode (mp-7h7: only operational
	// scoring/check-in routes bypass the main gate). In locked mode the bcrypt
	// verifier requires the env-var password. Password rotation is omitted
	// (disabled in locked mode); mode is omitted (preserved).
	putBody := map[string]any{
		"name":   "LockedSelfRun",
		"date":   "01-06-2026",
		"venue":  "New Dojo",
		"courts": []string{"A"},
	}
	req := jsonReq(http.MethodPut, "/api/tournament", putBody)
	req.Header.Set("X-Tournament-Password", "main-pw") // required: PUT /tournament is always main-gated
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"locked self-run PUT venue edit must succeed: %s", w.Body.String())

	t2, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, t2)
	assert.Equal(t, "New Dojo", t2.Venue, "venue edit must persist")
	assert.Equal(t, state.TournamentModeSelfRun, t2.Mode, "mode must remain self-run")
}

// ---------------------------------------------------------------------------
// §6 POST mode validation
// ---------------------------------------------------------------------------

// POST with invalid mode must return 400.
func TestSelfRun_POST_InvalidMode_Rejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	body := map[string]any{
		"name":     "T",
		"date":     "01-06-2026",
		"venue":    "V",
		"courts":   []string{"A"},
		"password": "pw",
		"mode":     "peer-operated", // invalid
	}
	req := jsonReq(http.MethodPost, "/api/tournament", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"POST with invalid mode must return 400")
}

// POST with no mode → defaults to officiated.
func TestSelfRun_POST_NoMode_DefaultsToOfficiated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	body := map[string]any{
		"name":     "T",
		"date":     "01-06-2026",
		"venue":    "V",
		"courts":   []string{"A"},
		"password": "pw",
		// no mode field
	}
	req := jsonReq(http.MethodPost, "/api/tournament", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusCreated, w.Code, "POST without mode must succeed: %s", w.Body.String())

	t2, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, t2)
	assert.Equal(t, state.TournamentModeOfficiated, t2.Mode,
		"missing mode must default to officiated")
}

// ---------------------------------------------------------------------------
// §sponsors: Self-run auth gate for sponsor management (mp-c38)
// ---------------------------------------------------------------------------
//
// Sponsor POST/DELETE are organiser-owned setup routes (like PUT /api/tournament)
// and must NOT become anonymously writable in self-run mode. Both are added to
// isSelfRunMainGatedConfigRoute. This test pins that contract in the centralised
// self-run auth matrix (the dedicated handler tests in handlers_sponsors_test.go
// cover the full authentication story; these tests ensure the middleware-level
// carve-out wiring is correct for self-run mode specifically).

func TestSelfRun_Sponsors_RequireMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	buildMultipart := func() (*bytes.Buffer, string) {
		buf := &bytes.Buffer{}
		mw := multipart.NewWriter(buf)
		require.NoError(t, mw.WriteField("name", "Acme Corp"))
		fw, err := mw.CreateFormFile("file", "logo.png")
		require.NoError(t, err)
		_, err = fw.Write(tinyPNG) // reuse tinyPNG defined in handlers_sponsors_test.go
		require.NoError(t, err)
		require.NoError(t, mw.Close())
		return buf, mw.FormDataContentType()
	}

	t.Run("POST_without_password_returns_401", func(t *testing.T) {
		body, ct := buildMultipart()
		req := httptest.NewRequest(http.MethodPost, "/api/sponsors", body)
		req.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"POST /api/sponsors must be main-gated in self-run mode")
	})

	t.Run("DELETE_without_password_returns_401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/sponsors/0", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"DELETE /api/sponsors/:index must be main-gated in self-run mode")
	})

	t.Run("GET_sponsor_logo_is_public", func(t *testing.T) {
		// GET /api/sponsors/:file is a public asset endpoint (serves logo to
		// viewer/TV/lobby). It must NOT require authentication in self-run mode.
		// A 400 (invalid filename) is fine, the route is reachable without a
		// password.
		req := httptest.NewRequest(http.MethodGet, "/api/sponsors/abcdef0123456789.png", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusUnauthorized, w.Code,
			"GET /api/sponsors/:file must be public, logo is served to unauthenticated viewer/TV surfaces")
	})
}

// ---------------------------------------------------------------------------
// §branding: Self-run auth gate for the tournament logo (mp-scf)
// ---------------------------------------------------------------------------
//
// The logo POST/DELETE are organiser setup, the same class as the sponsor
// routes above, and are listed in isSelfRunMainGatedConfigRoute. Without that
// entry an anonymous caller in self-run mode could replace or remove the
// tournament logo. GET /api/branding/logo stays public: every viewer surface
// shows it.
func TestSelfRun_BrandingLogo_RequiresMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	buildMultipart := func() (*bytes.Buffer, string) {
		buf := &bytes.Buffer{}
		mw := multipart.NewWriter(buf)
		fw, err := mw.CreateFormFile("file", "logo.png")
		require.NoError(t, err)
		_, err = fw.Write(tinyPNG) // reuse tinyPNG defined in handlers_sponsors_test.go
		require.NoError(t, err)
		require.NoError(t, mw.Close())
		return buf, mw.FormDataContentType()
	}

	t.Run("POST_without_password_returns_401", func(t *testing.T) {
		body, ct := buildMultipart()
		req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
		req.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"POST /api/branding/logo must be main-gated in self-run mode")
		_, err := os.Stat(filepath.Join(store.GetFolder(), state.BrandingDirName))
		assert.True(t, os.IsNotExist(err), "a refused upload must store no logo")
	})

	t.Run("DELETE_without_password_returns_401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/branding/logo", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"DELETE /api/branding/logo must be main-gated in self-run mode")
	})

	t.Run("POST_with_main_password_reaches_the_handler", func(t *testing.T) {
		body, ct := buildMultipart()
		req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
		req.Header.Set("Content-Type", ct)
		req.Header.Set("X-Tournament-Password", "main-pw")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	})

	t.Run("DELETE_with_main_password_reaches_the_handler", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/branding/logo", nil)
		req.Header.Set("X-Tournament-Password", "main-pw")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	})

	t.Run("GET_logo_is_public", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/branding/logo", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusUnauthorized, w.Code,
			"GET /api/branding/logo must be public, every viewer surface shows it")
	})
}

// ---------------------------------------------------------------------------
// §reopen: Self-run auth gate for kachinuki match reopen (mp-gmcg)
// ---------------------------------------------------------------------------
//
// POST .../matches/:mid/reopen mutates a FINALIZED result (clears
// winner/decision, retracts propagated bracket winners), the same organiser
// correction class as PUT .../override-winner. It is listed in
// isSelfRunMainGatedConfigRoute so the CENTRAL AuthMiddleware gate (including
// the F4 empty-stored-password fail-closed branch) applies; the handler
// deliberately carries no hand-rolled self-run check.
func TestSelfRun_ReopenRequiresMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	t.Run("without_password_returns_401", func(t *testing.T) {
		req := jsonReq(http.MethodPost, "/api/competitions/some-comp/matches/m-r1-0/reopen", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"reopen must be main-gated in self-run mode: it rewrites a finalized result")
	})

	t.Run("with_main_password_passes_the_gate", func(t *testing.T) {
		req := jsonReq(http.MethodPost, "/api/competitions/some-comp/matches/m-r1-0/reopen",
			map[string]any{"reason": "wrong winner recorded"})
		req.Header.Set("X-Tournament-Password", "main-pw")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// What matters here is that the credential cleared the middleware (a
		// 401 would mean the gate rejected the main password). The handler
		// itself then answers 404: the fixture competition doesn't exist, so
		// the engine finds no match to reopen.
		assert.NotEqual(t, http.StatusUnauthorized, w.Code, w.Body.String())
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "not found",
			"the request must reach ReopenMatch's own lookup")
	})
}

// mp-gmcg review F2: DELETE kachinuki-bout removes a live pairing, an organiser
// correction like reopen/override-winner — it must NOT be anonymously callable
// in self-run mode. The participant score path self-gates via
// enforceSelfRunPolicy; this route relies on the central allowlist instead, so
// a missing allowlist entry would let the self-run pass-through expose it.
func TestSelfRun_RemoveKachinukiBoutRequiresMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	req := jsonReq(http.MethodDelete, "/api/competitions/some-comp/matches/m-r1-0/kachinuki-bout", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"removing a bout must be main-gated in self-run mode: an anonymous spectator must not delete a live pairing")
}

// mp-gmcg review A4: requeue-blocker-and-reopen requeues a live match AND
// reopens a finalized result — doubly destructive — so it must NOT be
// anonymously callable in self-run mode. Same central-allowlist gate as reopen.
func TestSelfRun_RequeueBlockerAndReopenRequiresMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	req := jsonReq(http.MethodPost, "/api/competitions/some-comp/matches/m-r1-0/requeue-blocker-and-reopen",
		map[string]any{"blockerCompId": "some-comp", "blockerMatchId": "m-r1-1"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"requeue-blocker-and-reopen must be main-gated in self-run mode")
}

// bc-dhas: bulk-score and quick-score write results without the participant
// score path's self-run rules (the decision allowlist, the finished-match
// refusal and the representative bout's hantei guard), and quick-score
// replaces a match's bouts outright. Left public, either would undo all of
// them, so both are main-gated in self-run mode, like reopen. No page calls
// either route; organiser tooling sends the password.
func TestSelfRun_BulkAndQuickScoreRequireMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	routes := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/competitions/some-comp/matches/bulk-score", []any{}},
		{http.MethodPut, "/api/competitions/some-comp/matches/m-r1-0/quick-score", map[string]any{"sideA": "A", "sideB": "B", "teamAWins": 1}},
	}
	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := jsonReq(rt.method, rt.path, rt.body)
			req.Header.Set("X-Tournament-Password", "")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code, "without the password: %s", w.Body.String())

			req = jsonReq(rt.method, rt.path, rt.body)
			req.Header.Set("X-Tournament-Password", "main-pw")
			w = httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.NotEqual(t, http.StatusUnauthorized, w.Code, "the password clears the gate: %s", w.Body.String())
		})
	}
}
