package mobileapp

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/pdf"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPrintTestRouter builds a minimal router that wires the print
// handler behind AuthMiddleware, mirroring the production server.go
// setup (adminSmallBody group). The tournament is pre-configured with
// password "secret" so callers can send X-Tournament-Password: secret.
func setupPrintTestRouter(t *testing.T) (*gin.Engine, *state.Store, *engine.Engine, string) {
	t.Helper()

	dir, err := os.MkdirTemp("", "print-handler-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test Tournament",
		Password: "secret",
		Courts:   []string{"A"},
	}))

	eng := engine.New(store)

	gin.SetMode(gin.TestMode)
	r := gin.New()

	admin := r.Group("/api")
	admin.Use(MaxBodyBytes(DefaultMaxBodyBytes))
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterPrintHandlers(admin, eng)

	return r, store, eng, dir
}

// sofficeAvailable returns true when pdf.NewGenerator() succeeds (i.e.
// LibreOffice is installed in the test environment). Tests that require
// a running soffice branch on this helper to decide which assertion to run.
func sofficeAvailable() bool {
	_, err := pdf.NewGenerator()
	return err == nil || !errors.Is(err, pdf.ErrSofficeNotFound)
}

// TestPrintHandler_UnknownType validates that an unrecognised: type
// parameter is rejected with HTTP 400 before the endpoint attempts any
// PDF work.
func TestPrintHandler_UnknownType(t *testing.T) {
	r, _, _, _ := setupPrintTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/print/unknown-type", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "unknown print type")
}

// TestPrintHandler_AuthRequired verifies that the admin password gate is
// enforced. Without the correct X-Tournament-Password header the endpoint
// must return 401 (wrong password) or 403 (no tournament / missing header),
// never 200 or any other success code.
func TestPrintHandler_AuthRequired(t *testing.T) {
	r, _, _, _ := setupPrintTestRouter(t)

	t.Run("missing password returns non-2xx", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/print/all", nil)
		// No X-Tournament-Password header.
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.True(t, w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden,
			"expected 401 or 403, got %d", w.Code)
	})

	t.Run("wrong password returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/print/all", nil)
		req.Header.Set("X-Tournament-Password", "wrong-password")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

// TestPrintHandler_ValidRequest exercises the happy path for each valid
// type selector.
//
// Branch on soffice availability:
//   - soffice absent → expect HTTP 503 with actionable message.
//   - soffice present → expect HTTP 200 + non-empty ZIP with .pdf entries.
func TestPrintHandler_ValidRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soffice-dependent test in short mode")
	}

	// Use a real competition with started brackets so the PDF pipeline has
	// something to work with when soffice IS available. For the soffice-absent
	// branch this is irrelevant, the handler errors before touching workbooks.
	r, store, _, _ := setupPrintTestRouter(t)

	// Seed a minimal started competition. The export path only needs the
	// competition to exist (even if pools/draws are empty) to exercise the
	// full code path to the soffice gate.
	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Test Comp",
		Status: state.CompStatusPools,
	}))

	for _, printType := range []string{"all", "registration", "names", "tags", "pools-trees", "full-bracket"} {
		t.Run(printType, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/print/"+printType, nil)
			req.Header.Set("X-Tournament-Password", "secret")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if !sofficeAvailable() {
				// LibreOffice not installed: expect 503 with actionable install message.
				assert.Equal(t, http.StatusServiceUnavailable, w.Code,
					"type=%s: expected 503 (soffice unavailable), got %d; body=%s",
					printType, w.Code, w.Body.String())
				assert.Contains(t, w.Body.String(), "LibreOffice",
					"type=%s: 503 body should mention LibreOffice", printType)
				return
			}

			// LibreOffice available: expect 200 or 422.
			// 422 is valid when the competition has no bracket pages yet
			// (e.g. pools not drawn); that is a real result, not a failure.
			if w.Code == http.StatusUnprocessableEntity {
				t.Logf("type=%s: 422 (no PDF pages produced), competition has no drawn pools/brackets; skipping ZIP check", printType)
				return
			}

			require.Equal(t, http.StatusOK, w.Code,
				"type=%s: expected 200, got %d; body=%s", printType, w.Code, w.Body.String())
			assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))
			// Filename now includes the type, e.g. "tournament-pdfs-all.zip".
			assert.Contains(t, w.Header().Get("Content-Disposition"), "tournament-pdfs-")
			assert.Contains(t, w.Header().Get("Content-Disposition"), ".zip")

			// Verify the response is a valid non-empty ZIP.
			body := w.Body.Bytes()
			assert.NotEmptyf(t, body, "type=%s: ZIP body must not be empty", printType)

			zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
			require.NoErrorf(t, err, "type=%s: response must be a valid ZIP archive", printType)
			assert.NotEmptyf(t, zr.File, "type=%s: ZIP must contain at least one file", printType)
			for _, f := range zr.File {
				assert.True(t, strings.HasSuffix(f.Name, ".pdf"),
					"type=%s: every ZIP entry must end in .pdf, got %q", printType, f.Name)
			}
		})
	}
}

// TestPrintHandler_SofficeAbsent verifies that when LibreOffice is not
// installed the endpoint returns HTTP 503 with an actionable message,
// regardless of `:type`. This test is always skipped when soffice IS present
// (that case is covered by TestPrintHandler_ValidRequest).
func TestPrintHandler_SofficeAbsent(t *testing.T) {
	if sofficeAvailable() {
		t.Skip("soffice is available; skipping the soffice-absent branch test")
	}

	r, store, _, _ := setupPrintTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "absent-comp",
		Name:   "Absent",
		Status: state.CompStatusPools,
	}))

	for _, printType := range []string{"all", "registration", "full-bracket"} {
		t.Run(printType, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/print/"+printType, nil)
			req.Header.Set("X-Tournament-Password", "secret")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusServiceUnavailable, w.Code,
				"type=%s: expected 503 when soffice is absent", printType)
			assert.Contains(t, w.Body.String(), "LibreOffice")
			// OS-agnostic actionable guidance (no platform-specific install cmd).
			assert.Contains(t, w.Body.String(), "$LIBREOFFICE_PATH")
		})
	}
}

// TestPrintHandler_SwissSkippedAndWarned covers a mixed batch: a Swiss
// competition alongside a renderable one. The Swiss competition has no
// static bracket to print (Engine.ExportCompetitionXlsx's
// ErrSwissExportUnsupported), so ExportTournamentWorkbooks skips it rather
// than aborting the whole booklet -- but the operator must be TOLD, since a
// silent omission would leave them believing the ZIP covers every
// competition. Uses type=registration (fastest group: a single "data" sheet
// per workbook, no bracket/pool rendering needed) to keep the real soffice
// round-trip quick.
func TestPrintHandler_SwissSkippedAndWarned(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soffice-dependent test in short mode")
	}
	if !sofficeAvailable() {
		t.Skip("soffice not available in this environment")
	}

	r, store, _, _ := setupPrintTestRouter(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "renderable-comp",
		Name:   "Renderable Comp",
		Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          "swiss-comp",
		Name:        "Swiss Comp",
		Format:      state.CompFormatSwiss,
		SwissRounds: 2,
		Status:      state.CompStatusSetup,
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/print/registration", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"the renderable competition alone must still produce a booklet; got %d: %s", w.Code, w.Body.String())

	// (a) response header names the skipped competition by ID, not by Name:
	// header values are decoded as latin-1 by clients, so a non-ASCII Name
	// would mangle here (see TestSkippedCompetitionsHeaderValue_ASCIIOnly).
	// The ID is guaranteed ASCII, so it is what the header carries.
	skippedHeader := w.Header().Get("X-Skipped-Competitions")
	assert.Contains(t, skippedHeader, "swiss-comp")
	assert.NotContains(t, skippedHeader, "Swiss Comp")

	// (b) the ZIP itself carries a plain-text entry -- the one an operator
	// actually sees when they open the archive, since a streamed
	// application/zip response has no JSON body to carry a warning in.
	body := w.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	require.NoError(t, err, "response must be a valid ZIP archive")

	var skippedEntry *zip.File
	for _, f := range zr.File {
		if f.Name == "SKIPPED-COMPETITIONS.txt" {
			skippedEntry = f
			break
		}
	}
	require.NotNil(t, skippedEntry, "ZIP must contain SKIPPED-COMPETITIONS.txt when a Swiss competition was skipped")

	rc, err := skippedEntry.Open()
	require.NoError(t, err)
	defer rc.Close()
	content, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Contains(t, string(content), "Swiss Comp")
	assert.Contains(t, string(content), "swiss-comp")
	assert.Contains(t, string(content), "not yet implemented")

	// The renderable competition's own PDF must still be present alongside
	// the warning entry -- the skip must not have swallowed the whole batch.
	var hasPDF bool
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, ".pdf") {
			hasPDF = true
			break
		}
	}
	assert.True(t, hasPDF, "the renderable competition's PDF must still be produced")
}

// TestPrintHandler_AllSkippedReturns422 covers a tournament that is entirely
// Swiss: ExportTournamentWorkbooks skips every competition, so len(sources)
// is 0 with err == nil. Before the fix this fell through to
// gen.GenerateAll/GenerateGroups, which fail with "no source workbooks
// provided" and surface as a misleading HTTP 500 -- the operator never sees
// the skipped-competition detail that exists to explain the omission. The
// handler must instead recognise the all-skipped case itself and return 422
// naming the skipped competition and its reason, without ever reaching the
// PDF generator. The handler calls pdf.NewGenerator() (LibreOffice
// detection) before the export/skip step, so soffice must be present to
// reach this code path at all; skip cleanly when it is absent.
func TestPrintHandler_AllSkippedReturns422(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping soffice-dependent test in short mode")
	}
	if !sofficeAvailable() {
		t.Skip("soffice not available in this environment")
	}

	r, store, _, _ := setupPrintTestRouter(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          "swiss-only",
		Name:        "Swiss Only Comp",
		Format:      state.CompFormatSwiss,
		SwissRounds: 2,
		Status:      state.CompStatusSetup,
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/print/registration", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code,
		"an all-skipped tournament must return 422, not 500; body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "Swiss Only Comp")
	assert.Contains(t, w.Body.String(), "swiss-only")
	assert.NotContains(t, w.Body.String(), "no source workbooks provided",
		"the response must not leak the internal pdf-generator error string")
}

// TestSkippedCompetitionsHeaderValue_ASCIIOnly pins the fix for the
// mojibake finding: a competition Name containing non-ASCII characters
// (kanji, here) must never reach the X-Skipped-Competitions header, since
// HTTP clients decode header values as latin-1 and would mangle it. The ZIP
// entry (writeSkippedCompetitionsEntry), which has no such constraint,
// still carries the full name.
func TestSkippedCompetitionsHeaderValue_ASCIIOnly(t *testing.T) {
	skipped := []engine.SkippedCompetition{
		{ID: "kanji-comp", Name: "剣道大会", Reason: "Swiss export is not yet implemented"},
	}

	header := skippedCompetitionsHeaderValue(skipped)
	for i, r := range header {
		assert.Lessf(t, r, rune(0x80), "header value must be pure ASCII, found %q at byte %d in %q", r, i, header)
	}
	assert.Contains(t, header, "kanji-comp")
	assert.NotContains(t, header, "剣道大会")

	// The ZIP entry, by contrast, is expected to carry the full UTF-8 name.
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	require.NoError(t, writeSkippedCompetitionsEntry(zw, skipped))
	require.NoError(t, zw.Close())

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	require.Len(t, zr.File, 1)
	rc, err := zr.File[0].Open()
	require.NoError(t, err)
	defer rc.Close()
	content, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Contains(t, string(content), "剣道大会")
}
