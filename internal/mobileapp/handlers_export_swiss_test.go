package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportBracketHandler_SwissUnprocessable verifies the blank-template
// bracket export endpoint (GET /api/competitions/:id/export, distinct from
// the sibling /export-results endpoint already covered by
// TestExportResultsHandler_SwissUnprocessable in handlers_export_test.go)
// converges on the same 422 behavior for a Swiss competition: Swiss has no
// pools and no static bracket, so Engine.ExportCompetitionXlsx must reject it
// rather than returning HTTP 200 with an effectively empty workbook.
func TestExportBracketHandler_SwissUnprocessable(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "sw-bracket", Name: "SW Bracket", Kind: "individual", Format: state.CompFormatSwiss,
		SwissRounds: 2, Courts: []string{"A"}, Status: "setup",
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/sw-bracket/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code,
		"Swiss export must be 422, never 500 (unhandled sentinel) or 200 (empty workbook)")
	assert.Contains(t, w.Body.String(), "Swiss")
}
