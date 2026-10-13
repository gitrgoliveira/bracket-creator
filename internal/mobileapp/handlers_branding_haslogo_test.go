package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-lgnf: the admin page learns whether a custom logo exists from the
// tournament it already reads (theme.hasLogo), instead of probing
// HEAD /api/branding/logo, which answers 404 while none is set and made the
// browser log a console error.
func TestTournament_ThemeHasLogoFollowsTheUploadedLogo(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()

	getTheme := func() map[string]any {
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
	write := func(method string) {
		t.Helper()
		var req *http.Request
		if method == http.MethodPost {
			body, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
			req = httptest.NewRequest(method, "/api/branding/logo", body)
			req.Header.Set("Content-Type", ct)
		} else {
			req = httptest.NewRequest(method, "/api/branding/logo", nil)
		}
		req.Header.Set("X-Tournament-Password", "secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	assert.NotContains(t, getTheme(), "hasLogo", "no logo: the field is absent")

	write(http.MethodPost)
	theme := getTheme()
	assert.Equal(t, true, theme["hasLogo"], "an uploaded logo is reported")
	assert.NotContains(t, theme, "logoPath", "the disk filename stays off the wire")

	write(http.MethodDelete)
	assert.NotContains(t, getTheme(), "hasLogo", "a removed logo is no longer reported")
}

// A client that sends the theme back with hasLogo (it read it from GET) must
// neither lose the logo nor be able to set one this way.
func TestPutTournament_HasLogoOnTheWireIsIgnored(t *testing.T) {
	h, _, cleanup := brandingTestSetup(t)
	defer cleanup()

	put := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/tournament", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	getTheme := func() map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
		req.Header.Set("X-Tournament-Password", "secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		theme, _ := body["theme"].(map[string]any)
		return theme
	}

	// No logo stored: a client cannot set one by sending hasLogo.
	put(`{"name":"Branding Test Cup","password":"secret","courts":["A"],"theme":{"primaryColor":"#112233","hasLogo":true}}`)
	theme := getTheme()
	assert.Equal(t, "#112233", theme["primaryColor"])
	assert.NotContains(t, theme, "hasLogo", "hasLogo is derived from the stored logo, never set by a client")

	// A logo stored: a client cannot lose it by sending hasLogo false, or by omitting it.
	body, ct := buildBrandingUpload(t, "logo.png", tinyPNG)
	req := httptest.NewRequest(http.MethodPost, "/api/branding/logo", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, true, getTheme()["hasLogo"], "precondition: the uploaded logo is reported")

	put(`{"name":"Branding Test Cup","password":"secret","courts":["A"],"theme":{"primaryColor":"#112233","hasLogo":false}}`)
	assert.Equal(t, true, getTheme()["hasLogo"], "a PUT carrying hasLogo false does not lose the logo")

	put(`{"name":"Branding Test Cup","password":"secret","courts":["A"],"theme":{"primaryColor":"#112233"}}`)
	assert.Equal(t, true, getTheme()["hasLogo"], "a PUT that omits hasLogo does not lose the logo")
}
