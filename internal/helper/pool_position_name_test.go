package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
}
