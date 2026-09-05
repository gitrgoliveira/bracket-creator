package helper

import "testing"

// TestQualifiersForOversizedPool pins bc-drwx item 13's shared "oversized
// pool sends one extra qualifier" rule directly: state.Competition.
// QualifiersForPool and extraQualifierOverridesFromSizes both delegate to
// this function so the two can never independently drift.
func TestQualifiersForOversizedPool(t *testing.T) {
	cases := []struct {
		name                       string
		size, minPoolSize, winners int
		want                       int
	}{
		{"no minimum: always uniform", 10, 0, 2, 2},
		{"negative minimum: always uniform", 10, -1, 2, 2},
		{"at the minimum: not oversized", 3, 3, 2, 2},
		{"under the minimum: not oversized", 2, 3, 2, 2},
		{"one over the minimum: oversized, +1", 4, 3, 2, 3},
		{"well over the minimum: still just +1", 8, 3, 2, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := QualifiersForOversizedPool(tc.size, tc.minPoolSize, tc.winners)
			if got != tc.want {
				t.Errorf("QualifiersForOversizedPool(%d, %d, %d) = %d, want %d",
					tc.size, tc.minPoolSize, tc.winners, got, tc.want)
			}
		})
	}
}
