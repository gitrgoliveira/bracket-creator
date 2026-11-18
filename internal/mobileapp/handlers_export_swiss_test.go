package mobileapp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportBracketHandler_SwissUnprocessable verifies the stored-draw
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

// exportSheetText joins every cell of every sheet of an exported workbook, so a
// test can ask whether a competitor's name was printed anywhere.
func exportSheetText(t *testing.T, data []byte) string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	var sb strings.Builder
	for _, sheet := range f.GetSheetList() {
		rows, err := f.GetRows(sheet)
		require.NoError(t, err)
		for _, row := range rows {
			sb.WriteString(strings.Join(row, "\t"))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// TestExportBracketHandler_NoDrawServesBlankLayout: a competition with no draw
// (setup, or a draw another device discarded) is answered 200 with the blank
// layout the draw would print, sized by the larger of the expected number and
// the registered roster, with no competitor's name, never refused 409.
func TestExportBracketHandler_NoDrawServesBlankLayout(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "nodraw", Name: "No Draw", Kind: "individual", Format: state.CompFormatMixed,
		PoolSize: 3, PoolSizeMode: "min", PoolWinners: 2, RoundRobin: true,
		ExpectedParticipants: 8,
		Courts:               []string{"A"}, Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants("nodraw", []domain.Player{
		{Name: "Zelda Zeroname", Dojo: "Dojo Zed"}, {Name: "Yuki Yoname", Dojo: "Dojo Why"},
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/nodraw/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", w.Header().Get("Content-Type"))
	text := exportSheetText(t, w.Body.Bytes())
	for _, name := range []string{"Zelda Zeroname", "Yuki Yoname", "Dojo Zed", "Dojo Why"} {
		assert.NotContains(t, text, name, "a blank layout prints no competitor")
	}
	// Nothing was drawn or written by asking for the template.
	got, err := store.LoadCompetition("nodraw")
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusSetup, got.Status)
	pools, err := store.LoadPools("nodraw")
	require.NoError(t, err)
	assert.Empty(t, pools)
}

// TestExportBracketHandler_NoDrawTooFewIs422: with no draw and nothing to size
// the layout from (no expected number, fewer than two registered), the door
// answers 422 with the sentence that names the fix, not 409 or 500.
func TestExportBracketHandler_NoDrawTooFewIs422(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "nosize", Name: "No Size", Kind: "individual", Format: state.CompFormatMixed,
		Courts: []string{"A"}, Status: state.CompStatusSetup,
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/nosize/export", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "expected number of participants")
	assert.Contains(t, w.Body.String(), "Settings")
}
