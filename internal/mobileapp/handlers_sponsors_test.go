package mobileapp

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Smallest valid PNG: 8-byte signature + IHDR + IDAT + IEND. 1×1 white.
// Source: hand-crafted; verified by `file` reporting "PNG image data, 1×1".
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A,
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
	0x00, 0x00, 0x03, 0x00, 0x01, 0x5B, 0x3F, 0x18,
	0xBD, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
	0x44, 0xAE, 0x42, 0x60, 0x82,
}

// Smallest valid JPEG: SOI + APP0 + EOI. Enough for DetectContentType to
// classify as image/jpeg (it sniffs the FFD8FF prefix).
var tinyJPEG = []byte{
	0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46,
	0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01,
	0x00, 0x01, 0x00, 0x00, 0xFF, 0xD9,
}

func sponsorTestSetup(t *testing.T) (*gin.Engine, string, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "sponsor-test-*")
	require.NoError(t, err)
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Sponsor Test Cup", Password: "secret",
	}))
	eng := engine.New(store)
	mockFS := fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html/>")}}
	res := resources.NewResources(nil, mockFS)
	router, _, _ := NewRouter(store, eng, res, NewFileVerifier(store))
	return router, tempDir, func() { _ = os.RemoveAll(tempDir) }
}

func buildSponsorUpload(t *testing.T, name, link, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	if name != "" {
		require.NoError(t, mw.WriteField("name", name))
	}
	if link != "" {
		require.NoError(t, mw.WriteField("link", link))
	}
	if filename != "" {
		fw, err := mw.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = fw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return body, mw.FormDataContentType()
}

func postSponsor(t *testing.T, router *gin.Engine, body *bytes.Buffer, ct string, password string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sponsors", body)
	req.Header.Set("Content-Type", ct)
	if password != "" {
		req.Header.Set("X-Tournament-Password", password)
	}
	router.ServeHTTP(w, req)
	return w
}

func TestPostSponsor_HappyPath_PNG(t *testing.T) {
	router, tempDir, cleanup := sponsorTestSetup(t)
	defer cleanup()

	body, ct := buildSponsorUpload(t, "Acme Corp", "https://acme.example", "acme.png", tinyPNG)
	w := postSponsor(t, router, body, ct, "secret")

	require.Equalf(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	var got state.Sponsor
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "Acme Corp", got.Name)
	assert.Equal(t, "https://acme.example", got.Link)
	assert.Regexp(t, `^[a-f0-9]{16}\.png$`, got.File)

	// File written to disk under tempDir/sponsors/.
	on, err := os.ReadFile(filepath.Join(tempDir, "sponsors", got.File))
	require.NoError(t, err)
	assert.Equal(t, tinyPNG, on)
}

func TestPostSponsor_HappyPath_JPEG(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	body, ct := buildSponsorUpload(t, "BetaCo", "", "beta.jpg", tinyJPEG)
	w := postSponsor(t, router, body, ct, "secret")

	require.Equalf(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	var got state.Sponsor
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Regexp(t, `^[a-f0-9]{16}\.jpg$`, got.File)
	assert.Empty(t, got.Link, "omitempty link round-trips as empty")
}

func TestPostSponsor_RejectsOversized(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	// Build a "PNG" larger than 1 MB. First bytes are valid signature so
	// the type sniff passes; subsequent garbage trips the size cap.
	big := make([]byte, SponsorMaxFileBytes+1024)
	copy(big, tinyPNG)
	body, ct := buildSponsorUpload(t, "Huge Co", "", "huge.png", big)
	w := postSponsor(t, router, body, ct, "secret")
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func TestPostSponsor_RejectsBadType(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	// PDF magic bytes — sniffer classifies as application/pdf.
	pdfBytes := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("\x00"), 100)...)
	body, ct := buildSponsorUpload(t, "PDF Inc", "", "doc.pdf", pdfBytes)
	w := postSponsor(t, router, body, ct, "secret")
	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
}

func TestPostSponsor_RejectsBadLink(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	for _, badLink := range []string{
		"javascript:alert(1)",
		"ftp://files.example",
		"no-scheme.example",
		"https://user:pass@example.com", // userinfo rejected — would leak credentials in href
		"https://",                      // missing host
	} {
		body, ct := buildSponsorUpload(t, "Bad Link", badLink, "x.png", tinyPNG)
		w := postSponsor(t, router, body, ct, "secret")
		assert.Equalf(t, http.StatusBadRequest, w.Code, "link %q must be rejected", badLink)
	}
}

func TestPostSponsor_RejectsOversizedName(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	body, ct := buildSponsorUpload(t, strings.Repeat("a", state.MaxSponsorNameLen+1), "", "x.png", tinyPNG)
	w := postSponsor(t, router, body, ct, "secret")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPostSponsor_RejectsEmptyName(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	body, ct := buildSponsorUpload(t, "   ", "", "x.png", tinyPNG)
	w := postSponsor(t, router, body, ct, "secret")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPostSponsor_AdminGated(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	body, ct := buildSponsorUpload(t, "Acme", "", "x.png", tinyPNG)
	w := postSponsor(t, router, body, ct, "" /* no password */)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body2, ct2 := buildSponsorUpload(t, "Acme", "", "x.png", tinyPNG)
	w2 := postSponsor(t, router, body2, ct2, "wrong-password")
	assert.Equal(t, http.StatusUnauthorized, w2.Code)
}

func TestPostSponsor_CapEnforced(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	for i := range state.MaxSponsors {
		body, ct := buildSponsorUpload(t, "Sponsor", "", "s.png", tinyPNG)
		w := postSponsor(t, router, body, ct, "secret")
		require.Equalf(t, http.StatusCreated, w.Code, "upload %d should succeed", i+1)
	}
	body, ct := buildSponsorUpload(t, "One Too Many", "", "s.png", tinyPNG)
	w := postSponsor(t, router, body, ct, "secret")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "maximum")
}

func TestGetSponsor_NotFound(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/sponsors/abcdef0123456789.png", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetSponsor_RejectsTraversal(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	for _, bad := range []string{
		"../../etc/passwd",
		"not-hex.png",
		"abcdef0123456789.gif",   // unsupported extension
		"abcdef0123456789",       // no extension
		"ABCDEF0123456789.png",   // uppercase rejected
		"abcdef0123456789.png.x", // double extension
	} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/sponsors/"+bad, nil)
		router.ServeHTTP(w, req)
		// Path traversal with .. resolves at the URL layer — Gin returns
		// 301 or 404. The filename regex covers everything that reaches
		// the handler. Either way, no file is served. 200 must never
		// appear.
		assert.NotEqual(t, http.StatusOK, w.Code, "must not serve %q", bad)
	}
}

func TestGetSponsor_ServesPNG_WithCacheHeaders(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	// Upload first so we have a known filename.
	body, ct := buildSponsorUpload(t, "Acme", "", "x.png", tinyPNG)
	w := postSponsor(t, router, body, ct, "secret")
	require.Equal(t, http.StatusCreated, w.Code)
	var s state.Sponsor
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))

	// Now GET it.
	g := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/sponsors/"+s.File, nil)
	router.ServeHTTP(g, req)

	require.Equal(t, http.StatusOK, g.Code)
	assert.Equal(t, "image/png", g.Header().Get("Content-Type"))
	assert.Contains(t, g.Header().Get("Cache-Control"), "immutable")
	assert.NotEmpty(t, g.Header().Get("ETag"))
	bodyBytes, err := io.ReadAll(g.Body)
	require.NoError(t, err)
	assert.Equal(t, tinyPNG, bodyBytes)
}

func TestDeleteSponsor_RemovesEntryAndFile(t *testing.T) {
	router, tempDir, cleanup := sponsorTestSetup(t)
	defer cleanup()

	// Upload one.
	body, ct := buildSponsorUpload(t, "Acme", "", "x.png", tinyPNG)
	w := postSponsor(t, router, body, ct, "secret")
	require.Equal(t, http.StatusCreated, w.Code)
	var s state.Sponsor
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))

	// Delete index 0.
	d := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/sponsors/0", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	router.ServeHTTP(d, req)
	require.Equalf(t, http.StatusOK, d.Code, "body: %s", d.Body.String())

	// File is gone.
	_, err := os.Stat(filepath.Join(tempDir, "sponsors", s.File))
	assert.True(t, os.IsNotExist(err), "file should be unlinked")

	// YAML entry is gone.
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	tour, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tour)
	assert.Empty(t, tour.Sponsors)
}

// TestDeleteSponsor_HandEditedTraversalFilenameRejected pins the
// defense-in-depth check on DELETE: if tournament.md was hand-edited to
// carry a traversal-shaped file value, the os.Remove must NOT fire and
// the bystander file must survive. The YAML entry is still removed
// (the index check stays intact).
func TestDeleteSponsor_HandEditedTraversalFilenameRejected(t *testing.T) {
	router, tempDir, cleanup := sponsorTestSetup(t)
	defer cleanup()

	// Plant a bystander file outside the sponsors dir that a traversal
	// would target. If the unlink fires, we'll catch it.
	bystander := filepath.Join(tempDir, "secret.txt")
	require.NoError(t, os.WriteFile(bystander, []byte("must survive"), 0o600))

	// Save a tournament with a Sponsors entry whose File is a traversal
	// path. Simulates a hand-edit / corrupt YAML.
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Traversal Test",
		Password: "secret",
		Sponsors: []state.Sponsor{{Name: "Evil", File: "../secret.txt"}},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodDelete, "/api/sponsors/0", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "YAML entry still gets removed; only the file unlink is suppressed")

	// Bystander file must still exist — the unlink was suppressed by
	// the filename allowlist check.
	_, err = os.Stat(bystander)
	assert.NoError(t, err, "traversal-shaped sponsor filename must not trigger an arbitrary file delete")

	// YAML entry is gone (the safe part of delete still ran).
	store2, err := state.NewStore(tempDir)
	require.NoError(t, err)
	tour, err := store2.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tour)
	assert.Empty(t, tour.Sponsors)
}

func TestDeleteSponsor_InvalidIndex(t *testing.T) {
	router, _, cleanup := sponsorTestSetup(t)
	defer cleanup()

	for _, path := range []string{"/api/sponsors/-1", "/api/sponsors/abc", "/api/sponsors/99"} {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodDelete, path, nil)
		req.Header.Set("X-Tournament-Password", "secret")
		router.ServeHTTP(w, req)
		assert.NotEqual(t, http.StatusOK, w.Code, "path %q must not return 200", path)
	}
}

// TestPostSponsor_NoTournament covers the rare-but-real path where the
// admin somehow hits the upload route before tournament.md exists. The
// transform must return errSponsorTournamentNotInit → 404, not 500.
func TestPostSponsor_NoTournament(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sponsor-no-tournament-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	mockFS := fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html/>")}}
	res := resources.NewResources(nil, mockFS)
	router, _, _ := NewRouter(store, eng, res, NewFileVerifier(store))

	body, ct := buildSponsorUpload(t, "Acme", "", "x.png", tinyPNG)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sponsors", body)
	req.Header.Set("Content-Type", ct)
	// No tournament password set (no tournament exists). AuthMiddleware
	// permits unauth POST /api/tournament on a virgin install, but
	// /api/sponsors is NOT on that allowlist. The middleware should
	// reject (4xx, not 200 or 404 from the handler), proving the auth
	// layer fires before the handler can leak the not-init state.
	router.ServeHTTP(w, req)
	assert.True(t, w.Code >= 400 && w.Code < 500,
		"no-tournament + no-password must reject at the auth layer (got %d)", w.Code)
	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.NotEqual(t, http.StatusCreated, w.Code)
}

// TestPostSponsor_SelfRunMode_RejectsAnonymous and Delete equivalent
// pin the self-run authorization contract — sponsor management is
// organiser setup, not operational play, and must not become anonymously
// writable when the tournament is in self-run mode. The route is added
// to isSelfRunMainGatedConfigRoute alongside the other config routes.
func TestPostSponsor_SelfRunMode_RejectsAnonymous(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sponsor-selfrun-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:          "Self-Run Cup",
		Password:      "main-pw",
		AdminPassword: "admin-pw",
		Mode:          state.TournamentModeSelfRun,
	}))
	eng := engine.New(store)
	mockFS := fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html/>")}}
	res := resources.NewResources(nil, mockFS)
	router, _, _ := NewRouter(store, eng, res, NewFileVerifier(store))

	body, ct := buildSponsorUpload(t, "Acme", "", "x.png", tinyPNG)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/sponsors", body)
	req.Header.Set("Content-Type", ct)
	// No password header. In self-run mode the main-pw gate is bypassed
	// for operational routes — but sponsor mutation is config, so 401.
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code, "self-run sponsor upload must require main-password")

	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest(http.MethodDelete, "/api/sponsors/0", nil)
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusUnauthorized, w2.Code, "self-run sponsor delete must require main-password")
}

// TestGetSponsor_RejectsSymlink hardens the GET path against a symlink
// planted in the sponsors dir (e.g. by a shared-host operator or a
// future feature). The strict filename regex already blocks the most
// common traversal vectors, but a valid-looking name pointing at /etc/
// would still be served by c.File. os.Lstat + symlink check closes that.
func TestGetSponsor_RejectsSymlink(t *testing.T) {
	router, tempDir, cleanup := sponsorTestSetup(t)
	defer cleanup()

	sponsorsDir := filepath.Join(tempDir, "sponsors")
	require.NoError(t, os.MkdirAll(sponsorsDir, 0o700))
	// Create a target file outside the sponsors dir.
	target := filepath.Join(tempDir, "secret.txt")
	require.NoError(t, os.WriteFile(target, []byte("not for sharing"), 0o600))
	// Plant a symlink that matches the strict filename regex.
	linkName := "abcdef0123456789.png"
	require.NoError(t, os.Symlink(target, filepath.Join(sponsorsDir, linkName)))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/sponsors/"+linkName, nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code, "symlinked sponsor file must 404, not serve target")
	assert.NotContains(t, w.Body.String(), "not for sharing")
}
