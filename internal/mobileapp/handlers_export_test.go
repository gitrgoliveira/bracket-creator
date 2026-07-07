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
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// TestExportResultsHandler_ScoredContent is an end-to-end API test: it starts a
// league competition and scores every match through the engine (the real path),
// then GETs /export-results and asserts the DOWNLOADED workbook carries literal
// results, not a blank/collapsed template.
func TestExportResultsHandler_ScoredContent(t *testing.T) {
	dir, err := os.MkdirTemp("", "export-handler-e2e-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret", Courts: []string{"A"}}))

	compID := "e2e"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "E2E", Kind: "individual", Format: state.CompFormatLeague,
		PoolSize: 3, PoolSizeMode: "min", PoolWinners: 1, RoundRobin: true,
		Courts: []string{"A"}, Status: "setup",
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Ann", Dojo: "D"}, {Name: "Bea", Dojo: "D"}, {Name: "Cody", Dojo: "D"},
	}))

	eng := engine.New(store)
	require.NoError(t, eng.StartCompetition(compID))
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	for _, m := range matches {
		res := m
		res.IpponsA = []string{"M", "K"}
		res.IpponsB = nil
		res.Winner = m.SideA
		res.Decision = "fought"
		res.Status = state.MatchStatusCompleted
		require.NoError(t, eng.RecordMatchResult(compID, m.ID, &res))
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(MaxBodyBytes(DefaultMaxBodyBytes))
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterExportResultsHandlers(admin, store, eng)

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/e2e/export-results", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	found := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "MK" {
				found = true
			}
		}
	}
	assert.True(t, found, "downloaded workbook must contain the literal ippon score 'MK' from the scored matches")
}

// TestExportResultsHandler_SwissUnprocessable verifies a Swiss competition (no
// static bracket) returns 422 with a clear message rather than a 500 or an empty file.
func TestExportResultsHandler_SwissUnprocessable(t *testing.T) {
	dir, err := os.MkdirTemp("", "export-handler-swiss-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "sw", Name: "SW", Kind: "individual", Format: state.CompFormatSwiss,
		SwissRounds: 2, Courts: []string{"A"}, Status: "setup",
	}))

	eng := engine.New(store)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(MaxBodyBytes(DefaultMaxBodyBytes))
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterExportResultsHandlers(admin, store, eng)

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/sw/export-results", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Contains(t, w.Body.String(), "Swiss")
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

// TestExportResultsHandler_NotFound maps a well-formed but nonexistent competition
// ID to HTTP 404 (not a generic 500), matching every other competition endpoint.
func TestExportResultsHandler_NotFound(t *testing.T) {
	r := setupExportTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/does-not-exist/export-results", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not found")
}
