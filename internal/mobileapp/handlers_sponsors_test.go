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
	router, _ := NewRouter(store, eng, res, NewFileVerifier(store))
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

	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
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

	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
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
		"",
	} {
		if badLink == "" {
			continue // empty link is allowed (optional field)
		}
		body, ct := buildSponsorUpload(t, "Bad Link", badLink, "x.png", tinyPNG)
		w := postSponsor(t, router, body, ct, "secret")
		assert.Equal(t, http.StatusBadRequest, w.Code, "link %q must be rejected", badLink)
	}
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

	for i := 0; i < state.MaxSponsors; i++ {
		body, ct := buildSponsorUpload(t, "Sponsor", "", "s.png", tinyPNG)
		w := postSponsor(t, router, body, ct, "secret")
		require.Equal(t, http.StatusCreated, w.Code, "upload %d should succeed", i+1)
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
	require.Equal(t, http.StatusOK, d.Code, "body: %s", d.Body.String())

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
