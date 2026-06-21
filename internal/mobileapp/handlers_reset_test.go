package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// setupResetTest spins up a router exposing only the reset + auth-config
// public routes, plus the relevant tournament-handler GET for assertions
// that the password actually changed. AuthMiddleware isn't installed in
// this test — the reset endpoint is registered on the unauthenticated
// public group in production, so testing it without auth is faithful.
func setupResetTest(t *testing.T, verifier PasswordVerifier) (*state.Store, *gin.Engine, *Hub) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir, err := os.MkdirTemp("", "reset-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	hub := NewHub()

	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, verifier, hub)
	RegisterAuthConfigHandlers(api, verifier, defaultElevatedVerifier(verifier, store), false)

	return store, r, hub
}

func TestReset_FileMode_Success(t *testing.T) {
	dir, err := os.MkdirTemp("", "reset-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	// Bootstrap an existing tournament.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "MyTournament",
		Password: "old-password",
	}))

	body, _ := json.Marshal(map[string]string{"password": "new-password"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the stored password actually changed.
	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "new-password", loaded.Password)
	// And the rest of the record is preserved.
	assert.Equal(t, "MyTournament", loaded.Name)
}

func TestReset_LockedMode_Returns404(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("envpass"), bcrypt.MinCost)
	require.NoError(t, err)
	bcryptV, err := NewBcryptVerifier(string(hash))
	require.NoError(t, err)

	store, r, _ := setupResetTest(t, bcryptV)

	// Even with a tournament in place, reset must return 404 in locked mode.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "MyTournament",
		Password: "irrelevant",
	}))

	body, _ := json.Marshal(map[string]string{"password": "new-password"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "reset disabled")

	// Stored password must be unchanged.
	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "irrelevant", loaded.Password)
}

func TestReset_NoTournament_Returns409(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	body, _ := json.Marshal(map[string]string{"password": "new-password"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "bootstrap")
}

// Tournament legitimately named "New Tournament" (with a non-empty
// password) must be eligible for reset. Pre-fix the check at
// handlers_reset.go was `t.Name == "New Tournament"` which 409'd this
// case forever; the middleware's sentinel is "default record" =
// (name="New Tournament" AND password=""), and the reset endpoint
// must match it exactly so a real tournament with that name isn't
// locked out of recovery.
func TestReset_NamedNewTournamentWithPassword_AllowsReset(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "New Tournament",
		Password: "real-password",
	}))

	body, _ := json.Marshal(map[string]string{"password": "fresh"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code, "real tournament named 'New Tournament' must allow reset")
	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "fresh", loaded.Password)
}

// Cross-origin defense: a malicious site that an operator visits must
// not be able to POST to /api/tournament/reset and rotate the password.
// The global CORS policy is `*` (for the viewer routes), so the reset
// handler enforces same-origin / no-Origin itself.
func TestReset_CrossOriginPost_Rejected(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "old",
	}))

	body, _ := json.Marshal(map[string]string{"password": "attacker-set"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	req.Host = "tournament.local:8080"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "cross-origin POST must be rejected")
	assert.Contains(t, w.Body.String(), "cross-origin")
	// Stored password must be untouched.
	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "old", loaded.Password, "stored password must not change on rejected request")
}

// Same-origin browser POST (Origin matches Host) is accepted — the
// legitimate path when the operator opens /reset in their browser tab.
func TestReset_SameOriginPost_Accepted(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "old",
	}))

	body, _ := json.Marshal(map[string]string{"password": "newpw"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://tournament.local:8080")
	req.Host = "tournament.local:8080"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// A browser on a LAN IP (non-loopback) with matching Origin/Host is accepted.
// Operators access the server from other devices on the same network; the
// same-origin check (Origin == Host) is sufficient protection here.
func TestReset_LanIPSameOrigin_Accepted(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "old",
	}))

	body, _ := json.Marshal(map[string]string{"password": "newpw"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// LAN IP — Origin matches Host, so same-origin check passes.
	req.Header.Set("Origin", "http://192.168.1.100:8080")
	req.Host = "192.168.1.100:8080"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// A browser on an HTTPS origin that POSTs to an HTTP tournament server
// with the same host is cross-origin per RFC 6454 (scheme differs).
// The request must be rejected even though the host:port matches, so
// an HTTPS page can't use a same-host HTTP server as a CSRF pivot.
func TestReset_SchemeMismatch_Rejected(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "old",
	}))

	// Origin says HTTPS; server is HTTP (req.TLS == nil → expectedScheme = "http").
	body, _ := json.Marshal(map[string]string{"password": "attacker"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://tournament.local:8080")
	req.Host = "tournament.local:8080"
	// req.TLS is nil by default (http) — scheme mismatch with https Origin
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "https Origin against http server must be rejected")
	loaded, _ := store.LoadTournament()
	assert.Equal(t, "old", loaded.Password)
}

// Browsers send `Origin: null` for sandboxed iframes, file:// pages,
// and data: URLs. None of these are legitimate operator contexts for
// /reset — they all indicate a request smuggled in through a
// non-conventional security boundary. Treat the same as cross-origin
// (reject) rather than accepting an empty host string.
func TestReset_OriginNull_Rejected(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "old",
	}))

	body, _ := json.Marshal(map[string]string{"password": "attacker"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "null")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "Origin: null must not pass the same-origin check")
	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "old", loaded.Password)
}

// Malformed Origin (not a valid URL, or no host component) should be
// rejected. Defense-in-depth against a malicious caller sending
// garbage hoping to slip past url.Parse with an empty u.Host that
// then string-equals the empty Host on some misconfigured deployment.
func TestReset_OriginMalformed_Rejected(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "old",
	}))

	cases := []string{
		"not a url",
		"http://", // valid parse, but u.Host == ""
		"://noscheme",
	}
	for _, origin := range cases {
		t.Run(origin, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"password": "attacker"})
			req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equalf(t, http.StatusForbidden, w.Code, "malformed Origin %q must be rejected", origin)
		})
	}
}

// Non-browser caller (no Origin header — curl, scripted clients) is
// accepted. Browsers automatically set Origin on cross-origin POSTs
// but not all clients do, and we don't want to lock out legitimate
// operator-shell access.
func TestReset_NoOriginHeader_Accepted(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "old",
	}))

	body, _ := json.Marshal(map[string]string{"password": "newpw"})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

// On successful reset the server broadcasts both EventTournamentUpdated
// (for viewer refresh) AND EventPasswordReset (so admin sessions clear
// their stale localStorage credential and re-show AuthModal). Without
// the second event, other admins remain in admin mode with a cached
// password until a write fails with 401. The password_reset event
// payload includes the OriginatorId echoed from the request so the
// submitting tab can suppress its own broadcast.
func TestReset_BroadcastsPasswordResetEvent(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	hub := NewHub()
	ch := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(ch) })

	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), hub)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "old",
	}))

	body, _ := json.Marshal(map[string]string{
		"password":     "newpw",
		"originatorId": "client-abc-123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	// Drain the channel and assert both event types fired AND the
	// password_reset event echoes the originatorId from the request.
	// The hub uses a buffered channel; reads here are non-blocking
	// via select.
	seen := map[string]bool{}
	originatorEchoed := false
	for range 4 {
		select {
		case msg := <-ch:
			for _, ev := range []string{"tournament_updated", "password_reset"} {
				if strings.Contains(msg, `"type":"`+ev+`"`) {
					seen[ev] = true
				}
			}
			if strings.Contains(msg, `"type":"password_reset"`) &&
				strings.Contains(msg, `"originatorId":"client-abc-123"`) {
				originatorEchoed = true
			}
		default:
			// channel drained
		}
	}
	assert.True(t, seen["tournament_updated"], "tournament_updated event missing")
	assert.True(t, seen["password_reset"], "password_reset event missing (admins won't auto-logout)")
	assert.True(t, originatorEchoed, "password_reset event must echo the request's originatorId so the submitting tab can suppress its own broadcast")
}

// OriginatorId is opaque to the server but length-capped at 128 bytes
// so an attacker can't pump arbitrary bytes through the SSE channel.
// Oversized values are rejected at the reset endpoint with a 400.
func TestReset_OriginatorIDOversized_Returns400(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "old",
	}))

	body, _ := json.Marshal(map[string]string{
		"password":     "newpw",
		"originatorId": strings.Repeat("x", MaxLenOriginatorID+1),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "originatorId")
}

func TestReset_EmptyPassword_Returns400(t *testing.T) {
	store, _, _ := setupResetTest(t, NewFileVerifier(nil))
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "p",
	}))

	body, _ := json.Marshal(map[string]string{"password": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "required")
}

func TestReset_OversizedPassword_Returns400(t *testing.T) {
	store, _, _ := setupResetTest(t, NewFileVerifier(nil))
	r := gin.New()
	api := r.Group("/api")
	RegisterResetHandlers(api, store, NewFileVerifier(store), NewHub())

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "T",
		Password: "p",
	}))

	huge := strings.Repeat("x", MaxLenTournamentPassword+1)
	body, _ := json.Marshal(map[string]string{"password": huge})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/reset", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthConfig_FileMode(t *testing.T) {
	_, r, _ := setupResetTest(t, &fakeVerifier{mode: "file", reset: true})

	req := httptest.NewRequest(http.MethodGet, "/api/auth-config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp authConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "file", resp.Mode)
	assert.True(t, resp.ResetEnabled)
}

func TestAuthConfig_LockedMode(t *testing.T) {
	_, r, _ := setupResetTest(t, &fakeVerifier{mode: "locked", reset: false})

	req := httptest.NewRequest(http.MethodGet, "/api/auth-config", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp authConfigResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "locked", resp.Mode)
	assert.False(t, resp.ResetEnabled)
}

// fakeVerifier is a stub PasswordVerifier used by the auth-config tests
// where we want to assert the response shape without depending on a
// real bcrypt hash or store.
type fakeVerifier struct {
	mode  string
	reset bool
}

func (f *fakeVerifier) Verify(string) (bool, error)   { return false, nil }
func (f *fakeVerifier) Mode() string                  { return f.mode }
func (f *fakeVerifier) ResetEnabled() bool            { return f.reset }
func (f *fakeVerifier) AllowsFileBootstrap() bool     { return true }
func (f *fakeVerifier) EnforceEmptyStoredGuard() bool { return true }
func (f *fakeVerifier) RedactStoredPassword() bool    { return f.mode == "locked" }
