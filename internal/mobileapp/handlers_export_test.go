package mobileapp

import (
	"archive/zip"
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// setupExportTestRouter wires the results-export handler behind the same
// admin auth group as production (adminSmallBody), with a competition that
// has pools so the export produces a real workbook. Password is "secret".
func setupExportTestRouter(t *testing.T) *gin.Engine {
	t.Helper()

	dir, err := os.MkdirTemp("", "export-handler-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test Tournament",
		Password: "secret",
		Courts:   []string{"A"},
	}))

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Test Competition",
		Courts: []string{"A"},
	}))
	p1 := helper.Player{ID: "Alice", Name: "Alice", Dojo: "Dojo"}
	p2 := helper.Player{ID: "Bob", Name: "Bob", Dojo: "Dojo"}
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{p1, p2}, Matches: []helper.Match{{SideA: &p1, SideB: &p2}}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	eng := engine.New(store)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(MaxBodyBytes(DefaultMaxBodyBytes))
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterExportResultsHandlers(admin, store, eng)
	return r
}

// TestExportResultsHandler_AuthRequired verifies the admin password gate.
func TestExportResultsHandler_AuthRequired(t *testing.T) {
	r := setupExportTestRouter(t)

	t.Run("missing password returns non-2xx", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/competitions/test-comp/export-results", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.True(t, w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden,
			"expected 401 or 403, got %d", w.Code)
	})

	t.Run("wrong password returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/competitions/test-comp/export-results", nil)
		req.Header.Set("X-Tournament-Password", "wrong-password")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

// TestExportResultsHandler_Success verifies a valid request streams a real
// XLSX with the download headers set.
func TestExportResultsHandler_Success(t *testing.T) {
	r := setupExportTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/test-comp/export-results", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "results-test-comp.xlsx")

	// Body must be a valid XLSX (zip archive).
	body := w.Body.Bytes()
	require.NotEmpty(t, body)
	_, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	assert.NoError(t, err, "response body must be a valid XLSX (zip) file")
}

// TestExportResultsHandler_InvalidID rejects a malformed competition ID with 400.
func TestExportResultsHandler_InvalidID(t *testing.T) {
	r := setupExportTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/bad.id/export-results", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
