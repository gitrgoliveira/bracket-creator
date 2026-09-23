package helper

import (
	"fmt"
	"net/url"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// qrSizePx is the raw source PNG size in pixels. The embedding scale applied
// in CreateTagsSheet determines the rendered size; keeping the source at 200 px
// preserves enough QR modules for High error-correction at any reasonable scale.
const qrSizePx = 200

// watchlistParam is the viewer's watchlist permalink parameter. It must equal
// WATCHLIST_PARAM in web-mobile/js/watchlist_link.jsx; the shared fixture
// testdata/tag_watch_links.json is read by both languages' suites, so a change
// on one side alone fails the other.
const watchlistParam = "w"

// playerTagURL builds the URL a numbered competitor's tag QR opens: the
// viewer's watchlist permalink holding that one competitor, `?w=<number>`
// (bc-wlpl, operator ruling 2026-09-23). The tag is a one-entry watch link, so
// scanning it goes through the same reader as a shared list, including its
// retry while that competitor's competition roster is still loading.
//
// The number is escaped as JavaScript's encodeURIComponent escapes it, because
// the viewer splits `w` on "," BEFORE decoding and reads a leading ":" as a
// dojo entry (watchlist_link.jsx). url.QueryEscape would not do: it writes a
// space as "+", which decodeURIComponent leaves as a literal "+", and a prefix
// may hold any three runes (helper.ValidateNumberPrefix checks length only).
//
// Composing via url.Parse avoids malformed output when publicURL contains a
// path, query string, or fragment (e.g. "https://host/base?x=1" must not
// have the viewer path appended to its query string component).
func playerTagURL(publicURL, playerNumber string) string {
	base, err := url.Parse(publicURL)
	if err != nil {
		return ""
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return ""
	}
	if base.Host == "" || base.User != nil {
		return ""
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/"
	base.RawQuery = watchlistParam + "=" + encodeURIComponent(playerNumber)
	base.Fragment = ""
	return base.String()
}

// encodeURIComponent escapes s exactly as JavaScript's encodeURIComponent
// does: every UTF-8 byte outside A-Z a-z 0-9 - _ . ! ~ * ' ( ) becomes %XX.
func encodeURIComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z') || ('0' <= c && c <= '9') ||
			strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// playerTagQRPNG returns a PNG-encoded QR code for the competitor deep-link.
// Returns nil bytes (no error) when either input is empty so callers can
// skip embedding without special-casing the error.
func playerTagQRPNG(publicURL, playerNumber string) ([]byte, error) {
	if publicURL == "" || playerNumber == "" {
		return nil, nil
	}
	link := playerTagURL(publicURL, playerNumber)
	if link == "" {
		return nil, fmt.Errorf("cannot build QR URL from %q", publicURL)
	}
	png, err := qrcode.Encode(link, qrcode.High, qrSizePx)
	if err != nil {
		return nil, fmt.Errorf("qr encode %q: %w", playerNumber, err)
	}
	return png, nil
}
