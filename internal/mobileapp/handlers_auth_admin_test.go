package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// elevatedHandlerRouter mounts the tournament + admin-password + auth-config
// handlers for a given verifier. No AuthMiddleware — the main-password gate
// is orthogonal to the elevated-password logic under test here.
func elevatedHandlerRouter(t *testing.T, store *state.Store, verifier PasswordVerifier) (*gin.Engine, ElevatedVerifier) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	hub := NewHub()
	ev := defaultElevatedVerifier(verifier, store)
	api := r.Group("/api")
	RegisterTournamentHandlers(api, store, hub, verifier)
	RegisterAdminPasswordHandler(api, store, ev)
	RegisterAuthConfigHandlers(api, verifier, ev)
	return r, ev
}

func seedTournament(t *testing.T, store *state.Store) {
	t.Helper()
	_, err := store.SaveTournamentChanged(&state.Tournament{
		Name: "Seeded", Date: "01-01-2026", Venue: "Hall", Courts: []string{"A"}, Password: "main",
	})
	require.NoError(t, err)
}

func putAdminPassword(r *gin.Engine, body map[string]string) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/auth/admin-password", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminPasswordEndpoint_TOFUThenRotate(t *testing.T) {
	store := setupVerifierTestStore(t)
	seedTournament(t, store)
	r, ev := elevatedHandlerRouter(t, store, NewFileVerifier(store))

	// First-time set: no current password needed (gate inactive).
	require.False(t, ev.GateActive())
	w := putAdminPassword(r, map[string]string{"newPassword": "first-admin"})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, ev.GateActive(), "gate should activate after first set")

	// Rotation without the current password → 401.
	w = putAdminPassword(r, map[string]string{"newPassword": "second-admin"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Rotation with wrong current password → 401.
	w = putAdminPassword(r, map[string]string{"newPassword": "second-admin", "currentPassword": "wrong"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Rotation with correct current password → 200, new value takes effect.
	w = putAdminPassword(r, map[string]string{"newPassword": "second-admin", "currentPassword": "first-admin"})
	assert.Equal(t, http.StatusOK, w.Code)
	ok, err := ev.Verify("second-admin")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, _ = ev.Verify("first-admin")
	assert.False(t, ok, "old admin password must no longer verify")
}

func TestAdminPasswordEndpoint_EmptyNewPasswordRejected(t *testing.T) {
	store := setupVerifierTestStore(t)
	seedTournament(t, store)
	r, _ := elevatedHandlerRouter(t, store, NewFileVerifier(store))
	w := putAdminPassword(r, map[string]string{"newPassword": ""})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAdminPasswordEndpoint_NoTournament409(t *testing.T) {
	store := setupVerifierTestStore(t)
	r, _ := elevatedHandlerRouter(t, store, NewFileVerifier(store))
	w := putAdminPassword(r, map[string]string{"newPassword": "x"})
	assert.Equal(t, http.StatusConflict, w.Code)
}

// Finding 1 regression: GET /tournament must NEVER expose the elevated
// password, even in file mode where the main password IS returned.
func TestTournamentGet_NeverLeaksAdminPassword(t *testing.T) {
	store := setupVerifierTestStore(t)
	seedTournament(t, store)
	setAdminPassword(t, store, "super-secret-admin")
	r, _ := elevatedHandlerRouter(t, store, NewFileVerifier(store))

	req := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.NotContains(t, body, "super-secret-admin", "admin password leaked in GET /tournament")
	assert.NotContains(t, body, "adminPassword")
	assert.NotContains(t, body, "admin_password")
}

// Finding 2 regression: PUT /tournament (main-password gate) must NOT be able
// to set or overwrite the elevated password, and a routine settings save must
// preserve the existing one.
func TestTournamentPut_CannotSetOrWipeAdminPassword(t *testing.T) {
	store := setupVerifierTestStore(t)
	seedTournament(t, store)
	setAdminPassword(t, store, "original-admin")
	r, ev := elevatedHandlerRouter(t, store, NewFileVerifier(store))

	// Attacker attempts to overwrite the elevated password via the bulk PUT,
	// sending both the JSON field name variants.
	body := []byte(`{"name":"Renamed","date":"01-01-2026","venue":"Hall","courts":["A"],"password":"main","adminPassword":"attacker","admin_password":"attacker"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// The elevated password is unchanged: attacker's value does NOT verify,
	// the original still does (preserve-on-write).
	ok, err := ev.Verify("attacker")
	require.NoError(t, err)
	assert.False(t, ok, "PUT /tournament overwrote the admin password — privilege escalation")
	ok, err = ev.Verify("original-admin")
	require.NoError(t, err)
	assert.True(t, ok, "routine PUT /tournament wiped the admin password")

	// And the rename actually took effect (sanity: the PUT did persist).
	tn, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "Renamed", tn.Name)
}

func TestAuthConfig_ElevatedBits_FileMode(t *testing.T) {
	store := setupVerifierTestStore(t)
	seedTournament(t, store)
	r, _ := elevatedHandlerRouter(t, store, NewFileVerifier(store))

	get := func() authConfigResponse {
		req := httptest.NewRequest(http.MethodGet, "/api/auth-config", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp authConfigResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return resp
	}

	// Before setting: editable (file mode) but not required/configured.
	resp := get()
	assert.Equal(t, "file", resp.Mode)
	assert.True(t, resp.ElevatedEditable)
	assert.False(t, resp.ElevatedRequired)
	assert.False(t, resp.ElevatedConfigured)

	// After setting: required + configured.
	require.Equal(t, http.StatusOK, putAdminPassword(r, map[string]string{"newPassword": "abc"}).Code)
	resp = get()
	assert.True(t, resp.ElevatedRequired)
	assert.True(t, resp.ElevatedConfigured)
	assert.True(t, resp.ElevatedEditable)
}

func TestAdminPasswordEndpoint_LockedMode404(t *testing.T) {
	store := setupVerifierTestStore(t)
	seedTournament(t, store)
	// Build a locked main verifier so defaultElevatedVerifier picks the
	// locked branch. The env var is unset → unconfigured elevated verifier,
	// which still reports Mode()=="locked" → endpoint 404s.
	hash := mustBcrypt(t, "main")
	mainV, err := NewBcryptVerifier(hash)
	require.NoError(t, err)
	r, ev := elevatedHandlerRouter(t, store, mainV)
	assert.Equal(t, "locked", ev.Mode())

	w := putAdminPassword(r, map[string]string{"newPassword": "x"})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// Regression for the bypass Copilot caught on PR #193: the elevated gate on
// the dedicated participant endpoints was circumventable via PUT
// /api/competitions/:id, which persists the roster (SaveParticipants/SaveSeeds)
// whenever the body has a non-nil Players field — and that is the SPA's
// primary roster flow. The bulk PUT must now enforce the same gate, but ONLY
// when it carries a roster mutation; settings-only PUTs stay single-factor.
func TestBulkCompetitionPut_RosterMutationIsGated(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	defer os.RemoveAll(dir)

	// Activate the gate (file mode) and create a competition.
	setAdminPassword(t, store, "destroypw")
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "c1", Name: "C1", Format: "playoffs", Courts: []string{"A"}, Status: state.CompStatusSetup,
	}))

	putComp := func(body string, admin string, withAdmin bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "/api/competitions/c1", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		if withAdmin {
			req.Header.Set("X-Admin-Password", admin)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	rosterBody := `{"id":"c1","name":"C1","format":"playoffs","courts":["A"],"players":[{"name":"Alice","dojo":"D"}]}`
	settingsBody := `{"id":"c1","name":"C1 Renamed","format":"playoffs","courts":["A"]}` // no players field → nil

	t.Run("roster mutation without admin header → 401", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, putComp(rosterBody, "", false).Code)
	})
	t.Run("roster mutation with wrong admin header → 401", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, putComp(rosterBody, "wrong", true).Code)
	})
	t.Run("roster mutation with correct admin header → 200 and persists", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, putComp(rosterBody, "destroypw", true).Code)
		players, err := store.LoadParticipants("c1", false)
		require.NoError(t, err)
		require.Len(t, players, 1)
		assert.Equal(t, "Alice", players[0].Name)
	})
	t.Run("settings-only PUT (no players) is NOT gated", func(t *testing.T) {
		// No admin header, gate active — must still succeed because the body
		// carries no roster mutation.
		assert.Equal(t, http.StatusOK, putComp(settingsBody, "", false).Code)
	})
}

func mustBcrypt(t *testing.T, plain string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}
