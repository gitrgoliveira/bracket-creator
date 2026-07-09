package mobileapp

import (
	"html"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// defaultAppTitle is the fallback <title> / og:site_name used when no
// tournament name is configured. It mirrors the static title baked into
// web-mobile/index.html.
const defaultAppTitle = "Bracket Creator Mobile"

// serveIndexHTML writes web-mobile/index.html with server-rendered Open Graph
// and Twitter Card meta tags injected into <head>, populated from the live
// tournament state.
//
// Link-preview crawlers (Slack, WhatsApp, iMessage, Discord, Facebook, ...) do
// NOT execute JavaScript, so the SPA's client-side title/logo never reaches
// them; without these static tags a shared link resolves to the generic app
// name and bundled icon instead of the tournament's name and logo (mp-p9o8).
//
// index.html is intentionally served uncached here: the injected tags depend on
// mutable tournament state (name, venue, logo), so a renamed tournament must not
// keep surfacing a stale preview.
func serveIndexHTML(c *gin.Context, indexHTML []byte, store *state.Store) {
	page := injectPreviewMeta(string(indexHTML), store, requestBaseURL(c))
	// no-store (not no-cache): the injected tags depend on mutable tournament
	// state, so no intermediary should store this response at all, not merely
	// revalidate it, or a crawler could be served a stale preview from a proxy.
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}

// injectPreviewMeta rewrites the <title> and inserts link-preview meta tags into
// the given index.html markup. base is the absolute scheme://host prefix used to
// build absolute image/url values (crawlers require absolute URLs). A nil store
// or an absent tournament yields the default title with no description and the
// bundled favicon as the preview image.
func injectPreviewMeta(pageHTML string, store *state.Store, base string) string {
	title := defaultAppTitle
	description := ""

	var t *state.Tournament
	if store != nil {
		// A load error is non-fatal: fall back to defaults so the page always
		// serves. The preview is cosmetic; a broken tournament read must not
		// take down the SPA shell.
		t, _ = store.LoadTournament()
	}
	if t != nil {
		switch {
		case t.Name != "":
			title = t.Name
		case t.Theme != nil && t.Theme.WindowTitle != "":
			title = t.Theme.WindowTitle
		}
		parts := make([]string, 0, 2)
		if t.Venue != "" {
			parts = append(parts, t.Venue)
		}
		if t.Date != "" {
			parts = append(parts, t.Date)
		}
		description = strings.Join(parts, ", ")
	}

	// Prefer the uploaded tournament logo (served unauthenticated at
	// /api/branding/logo); fall back to the bundled app icon. Guard on the two
	// known filenames so a stale LogoPath never points the preview at a missing
	// image (mirrors RegisterPublicBrandingHandlers).
	imageURL := base + "/favicon.jpeg"
	if t != nil && t.Theme != nil && (t.Theme.LogoPath == "logo.png" || t.Theme.LogoPath == "logo.jpg") {
		imageURL = base + "/api/branding/logo"
	}

	var b strings.Builder
	b.WriteString("\n    <!-- Server-rendered link-preview metadata (mp-p9o8). -->\n")
	writeMeta := func(attrName, attrVal, content string) {
		b.WriteString(`    <meta ` + attrName + `="` + attrVal + `" content="` + html.EscapeString(content) + `">` + "\n")
	}
	writeMeta("property", "og:type", "website")
	writeMeta("property", "og:site_name", defaultAppTitle)
	writeMeta("property", "og:title", title)
	writeMeta("property", "og:url", base+"/")
	writeMeta("property", "og:image", imageURL)
	writeMeta("name", "twitter:card", "summary")
	writeMeta("name", "twitter:title", title)
	writeMeta("name", "twitter:image", imageURL)
	if description != "" {
		writeMeta("property", "og:description", description)
		writeMeta("name", "twitter:description", description)
		writeMeta("name", "description", description)
	}

	pageHTML = replaceTitleContent(pageHTML, title)
	if idx := strings.Index(pageHTML, "</head>"); idx != -1 {
		pageHTML = pageHTML[:idx] + b.String() + pageHTML[idx:]
	}
	return pageHTML
}

// replaceTitleContent swaps the text between the first <title> and </title> for
// the (HTML-escaped) newTitle. If either tag is missing the markup is returned
// unchanged.
func replaceTitleContent(pageHTML, newTitle string) string {
	const openTag = "<title>"
	const closeTag = "</title>"
	start := strings.Index(pageHTML, openTag)
	if start == -1 {
		return pageHTML
	}
	rel := strings.Index(pageHTML[start:], closeTag)
	if rel == -1 {
		return pageHTML
	}
	end := start + rel
	return pageHTML[:start+len(openTag)] + html.EscapeString(newTitle) + pageHTML[end:]
}

// requestBaseURL derives the absolute scheme://host prefix for the current
// request. It honors X-Forwarded-Proto / X-Forwarded-Host so absolute preview
// URLs come out as https://<public-host> when the app runs behind a TLS proxy
// (the production deployment), rather than the plaintext internal origin.
func requestBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if proto := firstCSV(c.GetHeader("X-Forwarded-Proto")); proto != "" {
		scheme = proto
	}
	host := c.Request.Host
	if fwd := firstCSV(c.GetHeader("X-Forwarded-Host")); fwd != "" {
		host = fwd
	}
	return scheme + "://" + host
}

// firstCSV returns the first comma-separated token, trimmed. Forwarded headers
// may carry a proxy chain (e.g. "https, http"); the client-facing value is first.
func firstCSV(v string) string {
	if i := strings.IndexByte(v, ','); i != -1 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}
