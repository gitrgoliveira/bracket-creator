package helper

import (
	"fmt"
	"net/url"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// qrSizePx is the raw PNG size in pixels. At ScaleX/Y=0.5 in the sheet this
// renders as ~2.5 cm on screen (100 px @ 96 DPI) while keeping enough modules
// for High error-correction recovery level.
const qrSizePx = 200

// playerTagURL builds the deep-link URL for a numbered competitor tag.
func playerTagURL(publicURL, playerNumber string) string {
	return strings.TrimRight(publicURL, "/") + "/viewer.html?playerNumber=" + url.QueryEscape(playerNumber)
}

// playerTagQRPNG returns a PNG-encoded QR code for the competitor deep-link.
// Returns nil bytes (no error) when either input is empty so callers can
// skip embedding without special-casing the error.
func playerTagQRPNG(publicURL, playerNumber string) ([]byte, error) {
	if publicURL == "" || playerNumber == "" {
		return nil, nil
	}
	link := playerTagURL(publicURL, playerNumber)
	png, err := qrcode.Encode(link, qrcode.High, qrSizePx)
	if err != nil {
		return nil, fmt.Errorf("qr encode %q: %w", playerNumber, err)
	}
	return png, nil
}
