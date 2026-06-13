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

// playerTagURL builds the deep-link URL for a numbered competitor tag.
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
	base.RawQuery = url.Values{"playerNumber": {playerNumber}}.Encode()
	base.Fragment = ""
	return base.String()
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
