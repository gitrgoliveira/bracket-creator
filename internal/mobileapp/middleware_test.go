package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	r.Use(AuthMiddleware(store))
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

func TestAuthMiddleware_LoadError(t *testing.T) {
	store, r := setupMiddlewareTest(t)

	// Make the tournament file unreadable to cause ReadFile to fail with permission denied
	path := filepath.Join(store.GetFolder(), "tournament.md")
	os.Chmod(path, 0000)
	defer os.Chmod(path, 0644) // Clean up for os.RemoveAll

	req := httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
