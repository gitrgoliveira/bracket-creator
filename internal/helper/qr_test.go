package helper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlayerTagURL(t *testing.T) {
	tests := []struct {
		name      string
		publicURL string
		number    string
		want      string
	}{
		{
			name:      "non-http scheme returns empty",
			publicURL: "javascript:alert(1)",
			number:    "K1",
			want:      "",
		},
		{
			name:      "file scheme returns empty",
			publicURL: "file:///etc/passwd",
			number:    "K1",
			want:      "",
		},
		{
			name:      "empty host returns empty",
			publicURL: "https://",
			number:    "K1",
			want:      "",
		},
		{
			name:      "userinfo in URL returns empty",
			publicURL: "https://user:pass@kendo.example.com",
			number:    "K1",
			want:      "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, playerTagURL(tc.publicURL, tc.number))
		})
	}
}

// TestPlayerTagURL_SharedFixture pins the tag QR's URL against the table the
// viewer's suite also reads (watchlist_link.test.jsx), so the Go encoder and
// the JS decoder of `?w=` agree on every row. See the fixture's _comment.
func TestPlayerTagURL_SharedFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "tag_watch_links.json"))
	require.NoError(t, err)
	var doc struct {
		Cases []struct {
			PublicURL string `json:"publicURL"`
			Number    string `json:"number"`
			URL       string `json:"url"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Cases, "an empty table would assert nothing")
	for _, tc := range doc.Cases {
		t.Run(tc.Number, func(t *testing.T) {
			assert.Equal(t, tc.URL, playerTagURL(tc.PublicURL, tc.Number))
		})
	}
}

func TestPlayerTagQRPNG(t *testing.T) {
	t.Run("empty publicURL returns nil without error", func(t *testing.T) {
		png, err := playerTagQRPNG("", "K1")
		require.NoError(t, err)
		assert.Nil(t, png)
	})

	t.Run("empty playerNumber returns nil without error", func(t *testing.T) {
		png, err := playerTagQRPNG("https://example.com", "")
		require.NoError(t, err)
		assert.Nil(t, png)
	})

	t.Run("both empty returns nil without error", func(t *testing.T) {
		png, err := playerTagQRPNG("", "")
		require.NoError(t, err)
		assert.Nil(t, png)
	})

	t.Run("valid inputs produce a PNG", func(t *testing.T) {
		png, err := playerTagQRPNG("https://kendo.example.com", "K1")
		require.NoError(t, err)
		require.NotNil(t, png)
		require.GreaterOrEqual(t, len(png), 4, "PNG too short to contain magic bytes")
		// PNG magic bytes: 0x89 P N G
		assert.Equal(t, byte(0x89), png[0])
		assert.Equal(t, byte('P'), png[1])
		assert.Equal(t, byte('N'), png[2])
		assert.Equal(t, byte('G'), png[3])
	})

	t.Run("trailing slash is normalised (same output)", func(t *testing.T) {
		a, err1 := playerTagQRPNG("https://example.com/", "K1")
		b, err2 := playerTagQRPNG("https://example.com", "K1")
		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.Equal(t, a, b)
	})
}
