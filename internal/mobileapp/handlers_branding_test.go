package mobileapp

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func brandingTestSetup(t *testing.T) (*http.Handler, string, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "branding-test-*")
	require.NoError(t, err)
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Branding Test Cup", Password: "secret",
	}))
	eng := engine.New(store)
	mockFS := fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html/>")}}
	res := resources.NewResources(nil, mockFS)
	router, _ := NewRouter(store, eng, res, NewFileVerifier(store))
	h := http.Handler(router)
	return &h, tempDir, func() { _ = os.RemoveAll(tempDir) }
}

func buildBrandingUpload(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	if filename != "" {
		fw, err := mw.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = fw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, mw.Close())
	return body, mw.FormDataContentType()
}

func TestGetBrandingLogo_NotFound(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/api/branding/logo", nil)
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPostBrandingLogo_HappyPath_PNG(t *testing.T) {
	h, dir, cleanup := brandingTestSetup(t)
	defer cleanup()
	body, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.FileExists(t, filepath.Join(dir, brandingDirName, "logo.png"))
}

func TestPostBrandingLogo_HappyPath_JPEG(t *testing.T) {
	h, dir, cleanup := brandingTestSetup(t)
	defer cleanup()
	body, ct := buildBrandingUpload(t, "logo.jpg", tinyJPEG)
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.FileExists(t, filepath.Join(dir, brandingDirName, "logo.jpg"))
}

func TestPostBrandingLogo_ReplacesOtherExt(t *testing.T) {
	// Upload PNG then JPEG — PNG must be removed.
	h, dir, cleanup := brandingTestSetup(t)
	defer cleanup()

	body, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	body, ct = buildBrandingUpload(t, "logo.jpg", tinyJPEG)
	req = httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w = httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.FileExists(t, filepath.Join(dir, brandingDirName, "logo.jpg"))
	assert.NoFileExists(t, filepath.Join(dir, brandingDirName, "logo.png"))
}

func TestPostBrandingLogo_RejectsBadType(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()
	// Send plain text — sniff will report text/plain.
	body, ct := buildBrandingUpload(t, "evil.txt", []byte("not an image"))
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
}

func TestPostBrandingLogo_AdminGated(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()
	body, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	// No password.
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetBrandingLogo_ServesPNG(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()

	// Upload a PNG first.
	body, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Now GET it.
	req = httptest.NewRequest(http.MethodGet, "/api/branding/logo", nil)
	w = httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/png", w.Header().Get("Content-Type"))
}

func TestDeleteBrandingLogo_RemovesFile(t *testing.T) {
	h, dir, cleanup := brandingTestSetup(t)
	defer cleanup()

	// Upload first.
	body, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Delete.
	req = httptest.NewRequest(http.MethodDelete, "/api/branding/logo", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w = httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoFileExists(t, filepath.Join(dir, brandingDirName, "logo.png"))

	// GET should now return 404.
	req = httptest.NewRequest(http.MethodGet, "/api/branding/logo", nil)
	w = httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestDeleteBrandingLogo_PreservesWindowTitle verifies that deleting the logo
// does not nil-out the Theme when a windowTitle is configured, so operators
// don't silently lose their window title setting.
func TestDeleteBrandingLogo_PreservesWindowTitle(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()

	// Set a windowTitle via PUT /tournament.
	body := `{"name":"Test Cup","password":"secret","courts":["A"],"theme":{"windowTitle":"My Cup 2026"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Upload a logo.
	logoBody, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
	req = httptest.NewRequest(http.MethodPost, "/api/branding/logo", logoBody)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w = httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Delete the logo.
	req = httptest.NewRequest(http.MethodDelete, "/api/branding/logo", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w = httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Theme must survive with the windowTitle intact.
	req = httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w = httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	theme, _ := got["theme"].(map[string]any)
	require.NotNil(t, theme, "theme must not be nil after logo delete when windowTitle is set")
	assert.Equal(t, "My Cup 2026", theme["windowTitle"], "windowTitle must be preserved after logo delete")
}

func TestDeleteBrandingLogo_NoLogoReturns404(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()
	req := httptest.NewRequest(http.MethodDelete, "/api/branding/logo", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestPutTournament_ThemeColorsAccepted verifies that valid hex theme colors
// round-trip through PUT /api/tournament (mp-scf).
func TestPutTournament_ThemeColorsAccepted(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()
	body := `{"name":"Test Cup","password":"secret","courts":["A"],"theme":{"primaryColor":"#ff0000","accentSoftColor":"#ffe0e0"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	theme, _ := resp["theme"].(map[string]any)
	require.NotNil(t, theme)
	assert.Equal(t, "#ff0000", theme["primaryColor"])
}

// TestPutTournament_ThemeBadColorRejected verifies that an invalid hex color
// is rejected with 400 (mp-scf).
func TestPutTournament_ThemeBadColorRejected(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()
	body := `{"name":"Test Cup","password":"secret","courts":["A"],"theme":{"primaryColor":"red"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestPutTournament_LogoPathPreserved verifies that PUT /api/tournament does
// not clear an existing logo (mp-scf).
func TestPutTournament_LogoPathPreserved(t *testing.T) {
	h, dir, cleanup := brandingTestSetup(t)
	defer cleanup()

	// Upload a logo.
	body, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// PUT tournament without a theme field.
	req = httptest.NewRequest(http.MethodPut, "/api/tournament",
		bytes.NewBufferString(`{"name":"Branding Test Cup","password":"secret","courts":["A"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w = httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Logo file should still exist.
	assert.FileExists(t, filepath.Join(dir, brandingDirName, "logo.png"))

	// GET /api/branding/logo should still return 200.
	req = httptest.NewRequest(http.MethodGet, "/api/branding/logo", nil)
	w = httptest.NewRecorder()
	(*h).ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
