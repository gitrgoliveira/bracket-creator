package mobileapp

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleIndexHTML mirrors the relevant structure of web-mobile/index.html: a
// <head> with the static default <title> and a </head> insertion point.
const sampleIndexHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>Bracket Creator Mobile</title>
</head>
<body><div id="root"></div></body>
</html>`

func TestInjectPreviewMeta_WithTournament(t *testing.T) {
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:  "Spring Kendo Cup",
		Date:  "01-06-2026",
		Venue: "Central Dojo",
		Mode:  state.TournamentModeOfficiated,
	}))

	out := injectPreviewMeta(sampleIndexHTML, store, "https://tora.example.org")

	// Title rewritten to the tournament name.
	assert.Contains(t, out, "<title>Spring Kendo Cup</title>")
	// Open Graph + Twitter tags present with the name.
	assert.Contains(t, out, `<meta property="og:title" content="Spring Kendo Cup">`)
	assert.Contains(t, out, `<meta name="twitter:card" content="summary">`)
	assert.Contains(t, out, `<meta name="twitter:title" content="Spring Kendo Cup">`)
	// Description built from venue + date.
	assert.Contains(t, out, `<meta property="og:description" content="Central Dojo, 01-06-2026">`)
	assert.Contains(t, out, `<meta name="description" content="Central Dojo, 01-06-2026">`)
	// No logo configured → favicon fallback, absolute URL.
	assert.Contains(t, out, `<meta property="og:image" content="https://tora.example.org/favicon.jpeg">`)
	assert.Contains(t, out, `<meta property="og:url" content="https://tora.example.org/">`)
	// Injected before </head>.
	assert.Less(t, strings.Index(out, "og:title"), strings.Index(out, "</head>"))
}

func TestInjectPreviewMeta_WithLogo(t *testing.T) {
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:  "Logo Cup",
		Date:  "01-06-2026",
		Venue: "Dojo",
		Theme: &state.Theme{LogoPath: "logo.png"},
		Mode:  state.TournamentModeOfficiated,
	}))

	out := injectPreviewMeta(sampleIndexHTML, store, "https://tora.example.org")

	assert.Contains(t, out, `<meta property="og:image" content="https://tora.example.org/api/branding/logo">`)
	assert.Contains(t, out, `<meta name="twitter:image" content="https://tora.example.org/api/branding/logo">`)
	assert.NotContains(t, out, "/favicon.jpeg")
}

func TestInjectPreviewMeta_UnknownLogoPathFallsBackToFavicon(t *testing.T) {
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:  "Stale Logo Cup",
		Date:  "01-06-2026",
		Venue: "Dojo",
		Theme: &state.Theme{LogoPath: "logo.gif"}, // not one of the two allowed names
		Mode:  state.TournamentModeOfficiated,
	}))

	out := injectPreviewMeta(sampleIndexHTML, store, "https://tora.example.org")

	assert.Contains(t, out, `content="https://tora.example.org/favicon.jpeg"`)
	assert.NotContains(t, out, "/api/branding/logo")
}

func TestInjectPreviewMeta_NoTournament(t *testing.T) {
	store := newTempStore(t) // no tournament saved → LoadTournament returns nil

	out := injectPreviewMeta(sampleIndexHTML, store, "https://tora.example.org")

	assert.Contains(t, out, "<title>Bracket Creator Mobile</title>")
	assert.Contains(t, out, `<meta property="og:title" content="Bracket Creator Mobile">`)
	assert.Contains(t, out, `content="https://tora.example.org/favicon.jpeg"`)
	// No description tags when nothing to describe.
	assert.NotContains(t, out, "og:description")
	assert.NotContains(t, out, `<meta name="description"`)
}

func TestInjectPreviewMeta_WindowTitleFallback(t *testing.T) {
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:  "", // empty name
		Date:  "01-06-2026",
		Venue: "Dojo",
		Theme: &state.Theme{WindowTitle: "My Custom Title"},
		Mode:  state.TournamentModeOfficiated,
	}))

	out := injectPreviewMeta(sampleIndexHTML, store, "https://tora.example.org")

	assert.Contains(t, out, "<title>My Custom Title</title>")
	assert.Contains(t, out, `<meta property="og:title" content="My Custom Title">`)
}

func TestInjectPreviewMeta_EscapesTournamentName(t *testing.T) {
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:  `Cup "2026" <script>`,
		Date:  "01-06-2026",
		Venue: "Dojo",
		Mode:  state.TournamentModeOfficiated,
	}))

	out := injectPreviewMeta(sampleIndexHTML, store, "https://tora.example.org")

	// Raw injection must not survive: no unescaped script tag, no attribute breakout.
	assert.NotContains(t, out, "<script>")
	assert.Contains(t, out, "&lt;script&gt;")
	assert.Contains(t, out, "&#34;2026&#34;")
}

func TestInjectPreviewMeta_NilStore(t *testing.T) {
	out := injectPreviewMeta(sampleIndexHTML, nil, "https://tora.example.org")
	assert.Contains(t, out, "<title>Bracket Creator Mobile</title>")
	assert.Contains(t, out, `<meta property="og:title" content="Bracket Creator Mobile">`)
}

func TestReplaceTitleContent(t *testing.T) {
	assert.Equal(t,
		"<head><title>New</title></head>",
		replaceTitleContent("<head><title>Old</title></head>", "New"))
	// Escapes the replacement.
	assert.Equal(t,
		"<title>a&amp;b</title>",
		replaceTitleContent("<title>x</title>", "a&b"))
	// Missing tags → unchanged.
	assert.Equal(t, "<head></head>", replaceTitleContent("<head></head>", "New"))
	assert.Equal(t, "<title>x", replaceTitleContent("<title>x", "New"))
}

func TestRequestBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		tls     bool
		headers map[string]string
		want    string
	}{
		{
			name: "plain http from host",
			host: "localhost:8080",
			want: "http://localhost:8080",
		},
		{
			name: "tls sets https",
			host: "localhost:8443",
			tls:  true,
			want: "https://localhost:8443",
		},
		{
			name:    "forwarded proto and host win (TLS proxy)",
			host:    "127.0.0.1:8080",
			headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "tora.icaro.familyds.org"},
			want:    "https://tora.icaro.familyds.org",
		},
		{
			name:    "forwarded header proxy chain takes first token",
			host:    "127.0.0.1:8080",
			headers: map[string]string{"X-Forwarded-Proto": "https, http", "X-Forwarded-Host": "public.example.org, internal"},
			want:    "https://public.example.org",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tc.host
			if tc.tls {
				req.TLS = &tls.ConnectionState{}
			}
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			c.Request = req
			assert.Equal(t, tc.want, requestBaseURL(c))
		})
	}
}
