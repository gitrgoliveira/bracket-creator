package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsPoolDaihyosenMatchID covers the ID-recognition helper. Moved here
// from internal/engine (bc-cse/11a): this grammar's one owner is state
// (engine.IsPoolDaihyosenMatchID is a thin delegate), so its exhaustive
// suffix-anchoring cases belong on the owner, not on a one-line wrapper.
func TestIsPoolDaihyosenMatchID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"Pool A-DH-0", true},
		{"Pool A-DH-1", true},
		{"Pool B-DH-42", true},
		{"My-Pool A-DH-0", true},   // hyphenated pool name, strings.Contains handles correctly
		{"Pool A-East-DH-0", true}, // realistic hyphenated pool name
		{"Pool A-0", false},
		{"Pool A-TB-0", false},
		{"Pool A-DH", false},    // no index after DH (no trailing dash)
		{"Pool A-D-0", false},   // different prefix
		{"Pool A-DHx-0", false}, // wrong prefix
		{"DH-0", false},         // no pool separator
		{"", false},
		// A pool literally named "Pool A-DH-East" produces regular match ids
		// like "Pool A-DH-East-0". A plain strings.Contains(id, "-DH-") would
		// misclassify these as daihyosen bouts; the suffix after the LAST
		// "-DH-" here is "East-0", not all-digits, so it must be false.
		{"Pool A-DH-East-0", false},
		{"Pool A-DH-East-12", false},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPoolDaihyosenMatchID(tc.id))
		})
	}
}

// TestIsTiebreakerMatchID covers the ID-recognition helper. Moved here from
// internal/engine (bc-cse/11a); see TestIsPoolDaihyosenMatchID's doc.
func TestIsTiebreakerMatchID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"Pool A-TB-0", true},
		{"Pool A-TB-1", true},
		{"Pool B-TB-42", true},
		{"Pool A-East-TB-0", true}, // hyphenated pool name
		{"Pool A-0", false},
		{"Pool A-1", false},
		{"Pool A-TB", false},    // no index after TB
		{"Pool A-T-0", false},   // different prefix
		{"Pool A-TBx-0", false}, // wrong prefix
		{"TB-0", false},         // no pool name separator
		{"", false},
		// Same sibling scenario as IsPoolDaihyosenMatchID: a pool literally
		// named "Pool A-TB-East" must not have its regular match ids
		// misclassified as tiebreaker bouts.
		{"Pool A-TB-East-0", false},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			assert.Equal(t, tc.want, IsTiebreakerMatchID(tc.id))
		})
	}
}
