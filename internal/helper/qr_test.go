package helper

import (
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
			name:      "basic",
			publicURL: "https://kendo.example.com",
			number:    "K1",
			want:      "https://kendo.example.com/viewer.html?playerNumber=K1",
		},
		{
			name:      "trailing slash is trimmed",
			publicURL: "https://kendo.example.com/",
			number:    "K1",
			want:      "https://kendo.example.com/viewer.html?playerNumber=K1",
		},
		{
			name:      "number with special chars is query-escaped",
			publicURL: "https://example.com",
			number:    "A+B",
			want:      "https://example.com/viewer.html?playerNumber=A%2BB",
		},
		{
			name:      "number with space is query-escaped",
			publicURL: "https://example.com",
			number:    "K 1",
			want:      "https://example.com/viewer.html?playerNumber=K+1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, playerTagURL(tc.publicURL, tc.number))
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
