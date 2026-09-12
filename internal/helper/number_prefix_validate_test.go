package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateNumberPrefix pins the ONE owner of the numberPrefix length
// check (PR #416 finding 4): the count is RUNES, not bytes, so a
// multi-byte-but-few-character value like "ÖÖ" (2 runes, 4 bytes) must pass
// a 3-rune cap even though it exceeds 3 BYTES. Trimming is also this
// function's job: surrounding whitespace must not count toward the cap and
// must not survive into the returned value.
func TestValidateNumberPrefix(t *testing.T) {
	tests := []struct {
		name    string
		val     string
		want    string
		wantErr bool
	}{
		{name: "empty: ok", val: "", want: ""},
		{name: "ASCII exactly at cap: ok", val: "ABC", want: "ABC"},
		{name: "ASCII one over cap: rejected", val: "ABCD", wantErr: true},
		{name: "2-rune, 4-byte value under the 3-rune cap: ok", val: "ÖÖ", want: "ÖÖ"},
		{name: "3-rune, 6-byte value exactly at cap: ok", val: "ÖÖÖ", want: "ÖÖÖ"},
		{name: "4-rune value over cap: rejected even though runes, not bytes, are counted", val: "ÖÖÖÖ", wantErr: true},
		{name: "surrounding whitespace is trimmed before the cap is measured", val: " K ", want: "K"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ValidateNumberPrefix(tt.val)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
