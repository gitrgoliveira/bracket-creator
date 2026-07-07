package mobileapp

import "testing"

// applyByTimestamp is the core of mp-y3nk timestamp reconciliation: a write only
// overwrites the stored value when it is not older, under server-relative
// timestamps. Unstamped writes (0) keep the previous arrival-order behavior so
// the rollout is backward compatible.
func TestApplyByTimestamp(t *testing.T) {
	cases := []struct {
		name             string
		incoming, stored int64
		want             bool
	}{
		{"newer incoming applies", 200, 100, true},
		{"older incoming is dropped", 100, 200, false},
		{"equal timestamps apply (tie -> last processed wins)", 150, 150, true},
		{"unstamped incoming always applies (back-compat)", 0, 500, true},
		{"stamped incoming over unstamped stored applies", 500, 0, true},
		{"both unstamped apply (legacy arrival-order)", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyByTimestamp(tc.incoming, tc.stored); got != tc.want {
				t.Fatalf("applyByTimestamp(%d, %d) = %v, want %v", tc.incoming, tc.stored, got, tc.want)
			}
		})
	}
}
