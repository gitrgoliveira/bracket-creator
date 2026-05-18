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
	RegisterAuthConfigHandlers(api, verifier)

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
