package mobileapp

// Tests for the self-run tournament mode auth boundary (mp-7h7).
//
// Three categories:
//  1. Auth matrix: officiated vs self-run × constructive vs destructive routes
//  2. Fail-open guard: self-run + file mode + no admin pw → 400 at creation
//  3. Immutability: POST persists mode; PUT switch → 400; PUT omitting mode → preserved
//  4. ValidateTournamentMode + ApplyTournamentDefaults unit tests
//  5. Locked + self-run: elevated gate still fires on destructive routes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
	r, _ := NewRouterWithHub(store, eng, res, verifier, NewHub())
	return r
}

// seedSelfRunTournament saves a self-run tournament with a main password
// AND sets an admin password via the store directly (since we need the
// adminPassword on disk before we can call enforceElevated).
func seedSelfRunTournament(t *testing.T, store *state.Store, adminPw string) {
	t.Helper()
	err := store.SaveTournament(&state.Tournament{
		Name:          "SelfRun Test",
		Date:          "01-06-2026",
		Venue:         "Dojo",
		Courts:        []string{"A"},
		Password:      "main-pw",
		AdminPassword: adminPw,
		Mode:          state.TournamentModeSelfRun,
	})
	require.NoError(t, err)
}

// seedOfficiatedTournament saves a standard officiated tournament.
func seedOfficiatedTournament(t *testing.T, store *state.Store) {
	t.Helper()
	err := store.SaveTournament(&state.Tournament{
		Name:          "Officiated Test",
		Date:          "01-06-2026",
		Venue:         "Dojo",
		Courts:        []string{"A"},
		Password:      "main-pw",
		AdminPassword: "admin-pw",
		Mode:          state.TournamentModeOfficiated,
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
// §1 Auth matrix: officiated mode (baseline — no regression)
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
			// may not exist — just assert it's not a 401.
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
	seedSelfRunTournament(t, store, "admin-pw")
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// Routes that are main-gated-only today (no elevated decorator) become
	// public in self-run. We test a subset because full competition state
	// is complex to seed. The key invariant is: no 401 from the main gate.
	//
	// Note: GET /api/tournament is intentionally NOT in this list. In file
	// mode it returns the Tournament struct including the Password field. If
	// it were public in self-run, an anonymous caller could read the main
	// password and use it to bypass the main gate on gated config routes
	// (Copilot #203 finding 3329406556). It is kept in isSelfRunMainGatedConfigRoute.
	constructiveRoutes := []struct{ method, path string }{
		{http.MethodGet, "/api/competitions"},
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
	seedSelfRunTournament(t, store, "admin-pw")
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
// apply to it — the main gate fires normally so anonymous callers cannot
// tamper with tournament setup. The SPA always sends X-Tournament-Password
// on PUT /api/tournament even in self-run mode, so this is transparent.
func TestSelfRun_SelfRunMode_PUTTournament_RequiresMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store, "admin-pw")
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	putBody := map[string]any{
		"name":   "SelfRun Test",
		"date":   "01-06-2026",
		"venue":  "Dojo",
		"courts": []string{"A"},
	}

	t.Run("no_pw_returns_401", func(t *testing.T) {
		req := jsonReq(http.MethodPut, "/api/tournament", putBody)
		// No X-Tournament-Password — must be rejected (configuration mutation).
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
// config) are organiser setup, NOT operational play — like PUT /api/tournament
// they stay main-gated in self-run mode (mp-7h7 / Copilot #203 sibling audit).
// Without this an anonymous client could create or reconfigure competitions in
// a self-run tournament. These routes are not elevated-gated, so the main gate
// is the protection. The organiser's SPA always sends X-Tournament-Password.
func TestSelfRun_SelfRunMode_CompetitionConfigRoutes_RequireMainPassword(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store, "admin-pw")
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	configRoutes := []struct{ method, path string }{
		{http.MethodPost, "/api/competitions"},
		{http.MethodPut, "/api/competitions/some-id"},
	}

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
			// Past the main gate — downstream may 400/404 on the body/id, but
			// never 401 (the invariant under test).
			assert.NotEqual(t, http.StatusUnauthorized, w.Code,
				"correct main password must clear the gate on %s %s (got %d)", tc.method, tc.path, w.Code)
		})
	}
}

// Destructive routes still require X-Admin-Password in self-run mode.
// The main gate is skipped but RequireElevatedPassword still fires.
func TestSelfRun_SelfRunMode_DestructiveRoutes_Require_AdminPassword(t *testing.T) {
	store := newTempStore(t)
	// Need a competition to exercise DELETE /competitions/:id
	seedSelfRunTournament(t, store, "admin-pw")
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	destructiveRoutes := []struct{ method, path string }{
		{http.MethodDelete, "/api/competitions/nonexistent-id"},
		{http.MethodPost, "/api/competitions/nonexistent-id/invalidate"},
		{http.MethodDelete, "/api/competitions/nonexistent-id/draw"},
		{http.MethodDelete, "/api/competitions/nonexistent-id/overrides"},
		{http.MethodPost, "/api/tournament/import"},
	}

	for _, tc := range destructiveRoutes {
		t.Run("selfrun_no_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			// No X-Tournament-Password and no X-Admin-Password
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"self-run destructive route %s %s must return 401 without admin pw", tc.method, tc.path)
		})

		t.Run("selfrun_wrong_admin_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Admin-Password", "wrong-admin-pw")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code,
				"self-run destructive route %s %s must return 401 with wrong admin pw", tc.method, tc.path)
		})

		t.Run("selfrun_correct_admin_pw_"+tc.method+"_"+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-Admin-Password", "admin-pw")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			// The gate passed (not 401); the downstream handler may return
			// 400/404/422 because we're not providing valid payload / IDs.
			// We only care that RequireElevatedPassword didn't reject it.
			assert.NotEqual(t, http.StatusUnauthorized, w.Code,
				"self-run destructive route %s %s must not 401 with correct admin pw", tc.method, tc.path)
		})
	}
}

// POST /api/competitions/:id/participants is elevated-gated (Path A: mp-7h7
// does NOT relax this endpoint; that's mp-e5j's job). Verify it still
// requires the admin password in self-run mode.
func TestSelfRun_SelfRunMode_ParticipantPOST_StillElevatedGated(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store, "admin-pw")
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// No X-Admin-Password → 401 even in self-run.
	req := jsonReq(http.MethodPost, "/api/competitions/any-id/participants", map[string]any{})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code,
		"POST /competitions/:id/participants must remain elevated-gated in self-run (Path A)")
}

// ---------------------------------------------------------------------------
// §2 Fail-open guard: self-run + file mode without admin password
// ---------------------------------------------------------------------------

// Creating a self-run tournament in file mode without an admin password
// must return 400. (Without the guard, destructive routes would be public.)
func TestSelfRun_FailOpenGuard_CreationWithoutAdminPw_Rejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	// File mode verifier.
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	body := map[string]any{
		"name":     "Self-Run Tournament",
		"date":     "01-06-2026",
		"venue":    "Dojo",
		"courts":   []string{"A"},
		"password": "main-pw",
		"mode":     "self-run",
		// NO admin password set at all
	}
	req := jsonReq(http.MethodPost, "/api/tournament", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"self-run tournament without admin pw must be rejected in file mode")
	assert.Contains(t, w.Body.String(), "admin")
}

// Creating a self-run tournament via the REAL API body path: the POST carries
// a transient `adminPassword` field (the only way the credential can reach the
// server, since Tournament.AdminPassword is json:"-"). The handler must set it
// atomically with creation so the fail-open guard passes, the tournament is
// persisted WITH the credential, and the destructive routes are gated by it.
//
// This exercises the path the browser/SPA actually uses — the prior
// store-seeding test bypassed it, which is why the "creation impossible via
// real API" bug slipped through.
func TestSelfRun_FailOpenGuard_CreationWithBodyAdminPw_Atomic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// Fresh file-mode bootstrap POST: self-run + main password + a transient
	// adminPassword in the SAME body. No header (file-mode bootstrap).
	body := map[string]any{
		"name":          "Self-Run Tournament",
		"date":          "01-06-2026",
		"venue":         "Dojo",
		"courts":        []string{"A"},
		"password":      "main-pw",
		"mode":          "self-run",
		"adminPassword": "destructive-pw",
	}
	req := jsonReq(http.MethodPost, "/api/tournament", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code,
		"self-run creation with adminPassword in body must succeed: %s", w.Body.String())

	// The persisted tournament must have mode=self-run AND a non-empty
	// (correct) admin password on disk.
	stored, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, state.TournamentModeSelfRun, stored.Mode,
		"stored mode must be self-run")
	assert.Equal(t, "destructive-pw", stored.AdminPassword,
		"admin password must be persisted atomically with creation")

	// Verify the destructive-route gate now uses the stored admin password:
	// no X-Admin-Password → 401; correct X-Admin-Password → passes the gate.
	t.Run("destructive route requires admin pw", func(t *testing.T) {
		reqNoPw := httptest.NewRequest(http.MethodDelete, "/api/competitions/some-id", nil)
		wNoPw := httptest.NewRecorder()
		r.ServeHTTP(wNoPw, reqNoPw)
		assert.Equal(t, http.StatusUnauthorized, wNoPw.Code,
			"destructive route must 401 without X-Admin-Password")
	})

	t.Run("destructive route passes gate with correct admin pw", func(t *testing.T) {
		reqWithPw := httptest.NewRequest(http.MethodDelete, "/api/competitions/some-id", nil)
		reqWithPw.Header.Set("X-Admin-Password", "destructive-pw")
		wWithPw := httptest.NewRecorder()
		r.ServeHTTP(wWithPw, reqWithPw)
		// Not 401: the elevated gate passed (downstream handler may 204/404).
		assert.NotEqual(t, http.StatusUnauthorized, wWithPw.Code,
			"destructive route must pass the elevated gate with correct X-Admin-Password (got %d)", wWithPw.Code)
	})

	t.Run("constructive route is public", func(t *testing.T) {
		// Use GET /api/competitions — GET /api/tournament is main-gated in
		// self-run (file-mode password leak prevention, fix 3329406556).
		reqPub := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
		wPub := httptest.NewRecorder()
		r.ServeHTTP(wPub, reqPub)
		assert.NotEqual(t, http.StatusUnauthorized, wPub.Code,
			"constructive route must be public in self-run (no main password)")
	})
}

// Creating a self-run tournament IS allowed when the existing record already
// has an AdminPassword on disk (from a prior setAdminPassword call or
// direct store write). This exercises the preserveFromExisting path where
// t.AdminPassword != "" after the preserve step.
func TestSelfRun_FailOpenGuard_CreationWithExistingAdminPw_Allowed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)

	// Seed a prior record with an admin password already set.
	err := store.SaveTournament(&state.Tournament{
		Name:          "Previous",
		Date:          "01-01-2026",
		Venue:         "Old",
		Courts:        []string{"A"},
		Password:      "old-pw",
		AdminPassword: "existing-admin-pw",
	})
	require.NoError(t, err)

	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// Re-POST (re-bootstrap) with mode:"self-run" in body, but no adminPassword
	// in the body. The fail-open guard passes because the handler's preserve step
	// copies existingForPost.AdminPassword from the prior record.
	// NOTE: Mode is immutable — the preserve step also copies existingForPost.Mode
	// (empty → "officiated"), so the stored record remains officiated regardless
	// of what mode the body requests. This test exercises the fail-open path, not
	// self-run creation; for first-create self-run see TestSelfRun_Immutability_POSTPreservesMode.
	body := map[string]any{
		"name":     "Self-Run Tournament",
		"date":     "01-06-2026",
		"venue":    "Dojo",
		"courts":   []string{"A"},
		"password": "main-pw",
		"mode":     "self-run",
		// adminPassword omitted from body (json:"-"); preserve step picks it up.
	}
	req := jsonReq(http.MethodPost, "/api/tournament", body)
	req.Header.Set("X-Tournament-Password", "old-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// Should succeed (201) because the existing record has an admin password.
	assert.Equal(t, http.StatusCreated, w.Code,
		"re-bootstrap must succeed when existing record has admin pw")

	// The stored mode must be "officiated" — mode immutability preserves the
	// existing record's mode (empty → officiated) and ignores the body's "self-run".
	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, state.TournamentModeOfficiated, loaded.Mode,
		"re-bootstrap must preserve existing mode (officiated), not adopt body mode")
}

// ---------------------------------------------------------------------------
// §3 Immutability
// ---------------------------------------------------------------------------

// POST self-run → GET mode returns "self-run".
// This test exercises a fresh bootstrap (no prior tournament.md) so the
// AuthMiddleware bootstrap branch fires (anonymous POST allowed).
// The fail-open guard for self-run requires an admin password, so we do a
// two-step: (1) fresh bootstrap as officiated to create the record with an
// admin password, (2) verify the store persisted mode=officiated, then
// (3) verify that a self-run can be started fresh when admin pw is pre-set
// directly via the store.
func TestSelfRun_Immutability_POSTPreservesMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// Step 1: fresh bootstrap as officiated (no auth header needed — bootstrap).
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
	require.Equal(t, http.StatusCreated, w.Code, "officiated POST must succeed: %s", w.Body.String())

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
	// (no prior record — true first bootstrap). Use a clean store with no
	// existing tournament so POST acts as first-create (not re-bootstrap).
	// Fix 3329416172 makes re-bootstrap preserve the existing mode (immutability),
	// so we must use a fresh store to actually create a self-run tournament via POST.
	store2 := newTempStore(t)
	r2 := setupSelfRunRouter(t, store2, NewFileVerifier(store2))

	postSelfRun := map[string]any{
		"name":          "My Self-Run",
		"date":          "01-06-2026",
		"venue":         "Dojo",
		"courts":        []string{"A"},
		"password":      "main-pw",
		"mode":          "self-run",
		"adminPassword": "admin-pw", // required for self-run in file mode
	}
	// Fresh bootstrap — no X-Tournament-Password needed (file mode, no record).
	req3 := jsonReq(http.MethodPost, "/api/tournament", postSelfRun)
	w3 := httptest.NewRecorder()
	r2.ServeHTTP(w3, req3)
	require.Equal(t, http.StatusCreated, w3.Code, "self-run POST must succeed: %s", w3.Body.String())

	// GET the tournament and verify mode = self-run.
	// GET /api/tournament is main-gated in self-run (file-mode password leak
	// prevention — fix 3329406556), so send the main password.
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
	seedSelfRunTournament(t, store, "admin-pw")
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	// PUT without mode field — should preserve self-run.
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
	require.Equal(t, http.StatusOK, w.Code, "PUT omitting mode must succeed: %s", w.Body.String())

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
	seedSelfRunTournament(t, store, "admin-pw")
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
				assert.NoError(t, err, "expected %q to be valid", tc.mode)
			} else {
				assert.Error(t, err, "expected %q to be invalid", tc.mode)
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

// In locked mode the elevated gate is always active (GateActive==true);
// destructive routes in self-run mode must still return 401/503.
func TestSelfRun_LockedMode_DestructiveRoutes_StillRequireAdminPw(t *testing.T) {
	gin.SetMode(gin.TestMode)
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
		// No AdminPassword on disk — locked mode reads from env var only.
		Mode: state.TournamentModeSelfRun,
	})
	require.NoError(t, err)

	r := setupSelfRunRouter(t, store, lockedV)

	// Destructive route without admin header → 503 (lockedUnconfigured elevated
	// verifier: GateActive==true, Configured==false → 503).
	req := httptest.NewRequest(http.MethodDelete, "/api/competitions/any-id", nil)
	// Main password provided (required by locked verifier even in self-run? —
	// no: self-run skips the main gate, so we send nothing).
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// 401 (admin pw required but not provided) or 503 (no hash configured).
	// Either is acceptable — both mean "destructive gate is active."
	assert.True(t, w.Code == http.StatusUnauthorized || w.Code == http.StatusServiceUnavailable,
		"locked+self-run destructive route must return 401 or 503, got %d: %s", w.Code, w.Body.String())

	// Constructive route without any header must NOT be 401 in self-run
	// (main gate is skipped). Use GET /api/competitions — GET /api/tournament
	// is main-gated in self-run to prevent file-mode password leaks (fix 3329406556).
	req2 := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.NotEqual(t, http.StatusUnauthorized, w2.Code,
		"locked+self-run constructive route must be public (not 401)")
}

// Regression (mp-7h7): a locked-mode self-run tournament has an empty on-disk
// AdminPassword (the env-var bcrypt hash is authoritative). The self-run
// fail-open guard applies to FILE MODE ONLY — in locked mode the elevated
// gate already fails closed via GateActive(). A routine PUT that edits
// venue/name MUST therefore succeed. Before the fix the PUT transform guard
// fired unconditionally on desired.AdminPassword == "", returning 400
// errSelfRunRequiresAdminPassword and making locked self-run tournaments
// permanently uneditable.
func TestSelfRun_LockedMode_PUTEdit_NotRejectedByFailOpenGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)

	hashBytes, err := bcrypt.GenerateFromPassword([]byte("main-pw"), bcrypt.MinCost)
	require.NoError(t, err)
	lockedV, err := NewBcryptVerifier(string(hashBytes))
	require.NoError(t, err)

	// Locked-mode self-run tournament: empty on-disk Password AND empty
	// on-disk AdminPassword (both inert; credentials come from env vars).
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
	// (disabled in locked mode); mode is omitted (preserved). This must NOT be
	// rejected by the file-mode-only fail-open guard.
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
		"locked self-run PUT venue edit must succeed, not be rejected by the file-mode fail-open guard: %s", w.Body.String())

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

// POST with an oversized adminPassword returns 400 even in locked mode
// (where the field is never persisted). Fix 3331061367: validate the
// transient field whenever present, regardless of server mode.
func TestSelfRun_POST_OversizedAdminPassword_Rejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)

	// Build a bcrypt verifier (locked mode).
	hashBytes, err := bcrypt.GenerateFromPassword([]byte("main-pw"), bcrypt.MinCost)
	require.NoError(t, err)
	lockedV, err := NewBcryptVerifier(string(hashBytes))
	require.NoError(t, err)

	r := setupSelfRunRouter(t, store, lockedV)

	body := map[string]any{
		"name":          "T",
		"date":          "01-06-2026",
		"venue":         "V",
		"courts":        []string{"A"},
		"password":      "main-pw",
		"adminPassword": string(make([]byte, MaxLenTournamentPassword+1)), // 257 chars
	}
	req := jsonReq(http.MethodPost, "/api/tournament", body)
	req.Header.Set("X-Tournament-Password", "main-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"oversized adminPassword must return 400 even in locked mode")
	assert.Contains(t, w.Body.String(), "adminPassword")
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
	require.Equal(t, http.StatusCreated, w.Code, "POST without mode must succeed: %s", w.Body.String())

	t2, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, t2)
	assert.Equal(t, state.TournamentModeOfficiated, t2.Mode,
		"missing mode must default to officiated")
}
