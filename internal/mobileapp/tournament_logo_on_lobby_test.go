package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func uninitTournamentTestSetup(t *testing.T) (*gin.Engine, func()) {
	t.Helper()
	tempDir, err := os.MkdirTemp("", "logo-lobby-test-*")
	require.NoError(t, err)
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	mockFS := fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html/>")}}
	res := resources.NewResources(nil, mockFS)
	router, _, limiter := NewRouter(store, eng, res, NewFileVerifier(store))
	t.Cleanup(limiter.Close)
	return router, func() { _ = os.RemoveAll(tempDir) }
}

func getTournamentTheme(t *testing.T, h *gin.Engine) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	theme, _ := body["theme"].(map[string]any)
	return theme
}

func putTournamentBody(t *testing.T, h *gin.Engine, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/tournament", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// TestPutTournament_LogoOnLobby_TickThenUntickNoColours covers the regression
// trap where unticking the checkbox sends theme: {"logoOnLobby": false} with
// no colours: an early nil-out before the preserve-merge would drop the theme
// and cause the merge to copy the stored Theme back, leaving logoOnLobby true.
func TestPutTournament_LogoOnLobby_TickThenUntickNoColours(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()

	putTournamentBody(t, h, `{"name":"Branding Test Cup","password":"secret","courts":["A"],"theme":{"logoOnLobby":true}}`)
	theme := getTournamentTheme(t, h)
	require.NotNil(t, theme)
	assert.Equal(t, true, theme["logoOnLobby"])

	putTournamentBody(t, h, `{"name":"Branding Test Cup","password":"secret","courts":["A"],"theme":{"logoOnLobby":false}}`)
	themeAfter := getTournamentTheme(t, h)
	assert.Nil(t, themeAfter, "unticking with no colours and no logo nils out the empty theme")
}

// TestPutTournament_LogoOnLobby_NameOnlySavePreservesFlagAndColours verifies
// that omitting theme on a PUT preserves both logoOnLobby and configured colours.
func TestPutTournament_LogoOnLobby_NameOnlySavePreservesFlagAndColours(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()

	putTournamentBody(t, h, `{"name":"Branding Test Cup","password":"secret","courts":["A"],"theme":{"primaryColor":"#112233","logoOnLobby":true}}`)
	theme := getTournamentTheme(t, h)
	require.NotNil(t, theme)
	assert.Equal(t, true, theme["logoOnLobby"])
	assert.Equal(t, "#112233", theme["primaryColor"])

	// Name-only save (theme omitted).
	putTournamentBody(t, h, `{"name":"Renamed Cup","password":"secret","courts":["A"]}`)
	themeAfter := getTournamentTheme(t, h)
	require.NotNil(t, themeAfter)
	assert.Equal(t, true, themeAfter["logoOnLobby"], "omitting theme preserves logoOnLobby")
	assert.Equal(t, "#112233", themeAfter["primaryColor"], "omitting theme preserves primaryColor")
}

// TestPutTournament_LogoOnLobby_UntickWithLogoUploadedKeepsLogo verifies that
// when a logo is uploaded and no colours are set, unticking logoOnLobby keeps
// the uploaded logo (hasLogo stays true) and the Theme.
func TestPutTournament_LogoOnLobby_UntickWithLogoUploadedKeepsLogo(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()

	// Upload a logo.
	logoBody, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", logoBody)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Tick logoOnLobby with no colours.
	putTournamentBody(t, h, `{"name":"Branding Test Cup","password":"secret","courts":["A"],"theme":{"logoOnLobby":true}}`)
	theme := getTournamentTheme(t, h)
	require.NotNil(t, theme)
	assert.Equal(t, true, theme["logoOnLobby"])
	assert.Equal(t, true, theme["hasLogo"])

	// Untick logoOnLobby with no colours.
	putTournamentBody(t, h, `{"name":"Branding Test Cup","password":"secret","courts":["A"],"theme":{"logoOnLobby":false}}`)
	themeAfter := getTournamentTheme(t, h)
	require.NotNil(t, themeAfter, "theme must survive because a logo is still uploaded")
	assert.Equal(t, true, themeAfter["hasLogo"], "hasLogo must stay true after unticking logoOnLobby")
	assert.NotContains(t, themeAfter, "logoOnLobby", "logoOnLobby is absent when false")
}

// TestPostTournament_LogoOnLobby_PreservedWithNoColours verifies that creating
// a tournament via POST with theme: {logoOnLobby: true} and no colours keeps the flag.
func TestPostTournament_LogoOnLobby_PreservedWithNoColours(t *testing.T) {
	h, cleanup := uninitTournamentTestSetup(t)
	defer cleanup()

	body := `{"name":"New Cup","date":"01-06-2026","venue":"Dojo","password":"secret","courts":["A"],"theme":{"logoOnLobby":true}}`
	req := httptest.NewRequest(http.MethodPost, "/api/tournament", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	theme := getTournamentTheme(t, h)
	require.NotNil(t, theme, "theme must not be nilled out on POST when logoOnLobby is true")
	assert.Equal(t, true, theme["logoOnLobby"])
}
