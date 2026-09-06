package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportedTagsNumbersMatchActualViewerPayload is the cross-package half
// of bc-pnum A8/[review]: engine.TestExportCompetitionXlsx_PurePlayoffsRendersTagsAndNamesToPrint
// derives its expectation by calling eng.NumberedParticipantsFor -- the very
// function under test -- so it can never catch the viewer surface silently
// deriving a DIFFERENT number for the same competitor; it can only catch the
// export disagreeing with itself. internal/engine cannot import
// internal/mobileapp (mobileapp already imports engine, so the reverse would
// cycle), so the genuine independent-oracle check -- export numbers against
// a REAL viewer HTTP response built by the real handler stack -- has to live
// here instead.
//
// The oracle is GET /api/viewer/competitions/:id, served by the actual
// router (setupTestRouter wires the production handler chain, including
// RegisterViewerHandlers), not a direct call into buildViewerCompetitionPayload
// or numbersFromPools. The export half calls
// eng.ExportCompetitionXlsx directly (as the engine-level test does): the
// export endpoint's own HTTP plumbing is not what bc-pnum A8 is about, only
// the number DERIVATION is, and that derivation is exercised identically
// either way since both paths share the one *engine.Engine instance.
func TestExportedTagsNumbersMatchActualViewerPayload(t *testing.T) {
	r, store, eng, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const compID = "pnum-a8-crosscheck"
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "A8 Crosscheck", Kind: "individual",
		Format: state.CompFormatPlayoffs, Courts: []string{"A"},
		NumberPrefix: "K", Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "Dojo Alice"},
		{Name: "Bob", Dojo: "Dojo Bob"},
		{Name: "Cleo", Dojo: "Dojo Cleo"},
		{Name: "Dan", Dojo: "Dojo Dan"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	// The oracle: the ACTUAL viewer detail payload, served by the real
	// handler chain through the router, exactly as a public spectator page
	// would receive it.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions/"+compID, nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "viewer detail: %s", w.Body.String())
	var detail struct {
		Config state.Competition `json:"config"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	require.Len(t, detail.Config.Players, 4, "premise: all four entrants present on the viewer payload")

	wantNumbers := make(map[string]bool, 4)
	for _, p := range detail.Config.Players {
		require.NotEmptyf(t, p.Number, "viewer payload must carry a number for %q (playoffs-only, prefix set)", p.Name)
		wantNumbers[p.Number] = true
	}
	require.Lenf(t, wantNumbers, 4, "every entrant must carry its OWN distinct number: got %v", wantNumbers)

	// eng.ExportCompetitionXlsx directly: same engine instance the router
	// above holds, so this is the same derivation the viewer request just
	// exercised, not a second independently-configured engine.
	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	tagRows, err := f.GetRows(helper.SheetTags)
	require.NoError(t, err)
	gotTagNumbers := map[string]bool{}
	for _, row := range tagRows {
		for _, cell := range row {
			cell = strings.TrimSpace(cell)
			if cell != "" {
				gotTagNumbers[cell] = true
			}
		}
	}
	for number := range wantNumbers {
		assert.Truef(t, gotTagNumbers[number],
			"Tags sheet must print the viewer's number %q; got cells %v", number, gotTagNumbers)
	}

	var namesSheet string
	for _, s := range f.GetSheetList() {
		if strings.HasPrefix(s, "Names to Print") {
			namesSheet = s
			break
		}
	}
	require.NotEmpty(t, namesSheet, "a playoffs-only competition must still get a Names to Print sheet")
	// The position cell here is a live SetCellFormula reference onto the
	// Tags sheet (see printNameEntries), not a literal value: excelize's
	// GetRows returns the unevaluated formula's empty cached value for a
	// workbook nothing has recalculated (no Excel/LibreOffice has opened
	// it), so this sheet cannot be number-cross-checked the way Tags can
	// without rendering it first (see the A9 LibreOffice render path for
	// that). What CAN be pinned without a render is that the row count
	// tracks the real viewer roster size, so a merge that silently dropped
	// or duplicated an entrant would still be caught here.
	nameRows, err := f.GetRows(namesSheet)
	require.NoError(t, err)
	assert.Len(t, nameRows, len(wantNumbers),
		"Names to Print must carry exactly one row per entrant on the viewer payload")
}
