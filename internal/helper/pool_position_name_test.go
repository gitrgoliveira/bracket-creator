package helper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoolPositionName_UniqueBeyond52 pins bc-drwx item 6: the old
// 'A'+i%26-doubled scheme collided every 26 pools past the first
// double-letter one -- i=26 and i=52 both reduce to i%26==0 and both
// produced "Pool AA" (a 64-pool run gave 12 colliding pairs). Excel-style
// bijective base-26 naming (A..Z, AA..AZ, BA..) never repeats.
func TestPoolPositionName_UniqueBeyond52(t *testing.T) {
	const n = 64
	seen := make(map[string]int, n)
	for i := 0; i < n; i++ {
		name := poolPositionName(i)
		if prev, dup := seen[name]; dup {
			t.Errorf("pool positions %d and %d both named %q", prev, i, name)
		}
		seen[name] = i
	}
	// Spot-check the exact repro pair and the expected values either side
	// of the rollover.
	assert.Equal(t, "Pool Z", poolPositionName(25))
	assert.Equal(t, "Pool AA", poolPositionName(26))
	assert.Equal(t, "Pool AZ", poolPositionName(51))
	assert.Equal(t, "Pool BA", poolPositionName(52))
	assert.NotEqual(t, poolPositionName(26), poolPositionName(52),
		"pool 26 and pool 52 must not share a name")
	// The SECOND rollover, two letters to three (bc-drwx item 9 review: the
	// JS mirror, poolLetterName in web-mobile/js/data.jsx, pins these exact
	// same six positions -- 25/26/51/52/701/702 -- so a regression on
	// either side of the mirror is caught at the identical boundary).
	assert.Equal(t, "Pool ZZ", poolPositionName(701))
	assert.Equal(t, "Pool AAA", poolPositionName(702))
}

// TestPoolPositionName_MatchesFixture is the Go half of the shared Go/JS
// golden table for the pool-name sequence: see the `_comment` in
// testdata/pool_letter_names.json for why the table is shared. JS half:
// poolLetterName in web-mobile/js/data.jsx (a later change reads this same
// file rather than restating the sequence).
type poolLetterNameCase struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
}

func TestPoolPositionName_MatchesFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "pool_letter_names.json"))
	require.NoError(t, err)
	var table struct {
		Cases []poolLetterNameCase `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	// Load-bearing: ranging over an empty table produces zero assertions and
	// no red, so a degraded file needs its own failure.
	require.NotEmpty(t, table.Cases,
		"testdata/pool_letter_names.json parsed to zero cases: the mirror would assert nothing")
	for _, tc := range table.Cases {
		assert.Equal(t, tc.Name, poolPositionName(tc.Index), "index %d", tc.Index)
	}
}
