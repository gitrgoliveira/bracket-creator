package mobileapp

import (
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

func setupMiddlewareTest(t *testing.T) (*state.Store, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir, err := os.MkdirTemp("", "middleware-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	r := gin.New()
	r.Use(AuthMiddleware(NewFileVerifier(store), store))
	r.PUT("/api/tournament", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/api/competitions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.POST("/api/competitions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	return store, r
}

func TestAuthMiddleware_NoTournament_AllowsCreateTournament(t *testing.T) {
	_, r := setupMiddlewareTest(t)

	req := httptest.NewRequest(http.MethodPut, "/api/tournament", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_NoTournament_BlocksOtherEndpoints(t *testing.T) {
	_, r := setupMiddlewareTest(t)

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/competitions"},
		{http.MethodPost, "/api/competitions"},
		{http.MethodGet, "/api/tournament"},
	}

	for _, tc := range tests {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}

func TestAuthMiddleware_WithTournament_RequiresPassword(t *testing.T) {
	store, r := setupMiddlewareTest(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret123",
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_WithTournament_WrongPassword(t *testing.T) {
	store, r := setupMiddlewareTest(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret123",
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
	req.Header.Set("X-Tournament-Password", "wrong")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_WithTournament_CorrectPassword(t *testing.T) {
	store, r := setupMiddlewareTest(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret123",
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
	req.Header.Set("X-Tournament-Password", "secret123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_WithTournament_EmptyPassword(t *testing.T) {
	store, r := setupMiddlewareTest(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret123",
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
	req.Header.Set("X-Tournament-Password", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// Defense-in-depth for the F4 sentinel-into-auth-field scenario.
// AuthMiddleware's `password != t.Password` comparison would otherwise
// be satisfied vacuously when both sides are "", an unauthenticated
// client sending no `X-Tournament-Password` header would match an
// empty stored password and reach c.Next(). The POST and PUT handlers
// in handlers_tournament.go now reject writes that would land an
// empty Password, but a legacy install from before that fix (or any
// out-of-band write) could still have empty-Password tournament data
// on disk. The middleware must fail closed in that case rather than
// rely on handler-level guards that may not have existed when the
// data was written.
//
// Two cases to pin:
// - empty header against empty stored Password → 403 (NOT c.Next())
// - non-empty header against empty stored Password → 403 (NOT c.Next())
//
// The "New Tournament" + empty Password literal still goes through
// the uninitialized branch (above) and allows POST/PUT to
// /api/tournament. That's covered by
// TestAuthMiddleware_NoTournament_AllowsCreateTournament.
func TestAuthMiddleware_LegacyEmptyStoredPassword_NoBypass(t *testing.T) {
	store, r := setupMiddlewareTest(t)

	// Simulate legacy on-disk state: real-named tournament with empty
	// Password. Created via direct store call, bypassing the
	// handler-level guards (which would now reject this).
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Legacy Tournament",
		Password: "",
	}))

	t.Run("empty header is rejected (no vacuous pass)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
		// no X-Tournament-Password header set (or set to "")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code,
			"empty header + empty stored password must NOT pass auth")
		assert.Contains(t, w.Body.String(), "misconfigured",
			"error should signal misconfiguration, not invalid request")
	})

	t.Run("non-empty header is also rejected (fail closed)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
		req.Header.Set("X-Tournament-Password", "anything")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code,
			"any header + empty stored password must fail closed")
	})
}

// Locked-mode bootstrap (no tournament yet, bcrypt verifier active)
// must require X-Tournament-Password, anonymous bootstrap on a fresh
// locked deployment would let any network client race-claim the
// initial tournament record. file-mode bootstrap stays anonymous
// (existing behavior, covered by TestAuthMiddleware_NoTournament_AllowsCreateTournament).
func TestAuthMiddleware_LockedMode_BootstrapRequiresAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "middleware-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)

	hash, err := bcrypt.GenerateFromPassword([]byte("kotai-A"), bcrypt.MinCost)
	require.NoError(t, err)
	bcryptV, err := NewBcryptVerifier(string(hash))
	require.NoError(t, err)

	r := gin.New()
	r.Use(AuthMiddleware(bcryptV, store))
	r.POST("/api/tournament", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	t.Run("no header → 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tournament", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"locked-mode bootstrap without X-Tournament-Password must 401")
	})

	t.Run("wrong header → 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tournament", nil)
		req.Header.Set("X-Tournament-Password", "wrong")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("correct env-var password → 201", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/tournament", nil)
		req.Header.Set("X-Tournament-Password", "kotai-A")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code,
			"locked-mode bootstrap with the env-var password must succeed")
	})
}

// In locked mode the stored password is always empty (auth comes from the
// env-var hash). A tournament legitimately named "New Tournament" must NOT
// be treated as uninitialized, the uninitialized sentinel
// (Name == "New Tournament" && Password == "") only applies in file mode.
func TestAuthMiddleware_LockedMode_NewTournamentNameNotSentinel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "middleware-test-locked-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)

	hash, err := bcrypt.GenerateFromPassword([]byte("kotai-A"), bcrypt.MinCost)
	require.NoError(t, err)
	bcryptV, err := NewBcryptVerifier(string(hash))
	require.NoError(t, err)

	// Simulate a locked-mode bootstrap: tournament named "New Tournament"
	// with empty Password (as the POST handler stores in locked mode).
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "New Tournament",
		Password: "",
	}))

	r := gin.New()
	r.Use(AuthMiddleware(bcryptV, store))
	r.GET("/api/tournament", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	t.Run("authenticated GET must succeed (not be blocked as uninitialized)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
		req.Header.Set("X-Tournament-Password", "kotai-A")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code,
			"locked-mode tournament named 'New Tournament' should not collide with uninitialized sentinel")
	})

	t.Run("unauthenticated GET must 401 (not 403 tournament-not-configured)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code,
			"locked-mode requests without header should 401, not 403 tournament-not-configured")
	})
}

func TestAuthMiddleware_LoadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test: root bypasses file permission restrictions")
	}
	store, r := setupMiddlewareTest(t)

	// Create a tournament file first, then make it unreadable to force a read error
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))
	dir := store.GetFolder()
	os.Chmod(dir, 0000)
	defer os.Chmod(dir, 0755) // Clean up for os.RemoveAll

	req := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
