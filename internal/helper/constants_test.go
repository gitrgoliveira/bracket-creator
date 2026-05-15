package helper

import "testing"

// Pin tests for cross-language constants. Each constant defined here is
// mirrored client-side in web-mobile/js/admin_helpers.jsx and asserted
// there with the same literal value. If you bump a constant on this
// side, the JS pin test breaks; if you bump it on the JS side, this
// pin test breaks. That's the lockstep guarantee — drift fails CI
// rather than waiting for a downstream UX bug to surface.
//
// Do NOT change a literal here without bumping the matching constant
// in admin_helpers.jsx (and vice versa).

func TestPinMaxCourts(t *testing.T) {
	if MaxCourts != 26 {
		t.Fatalf("MaxCourts = %d, want 26 (anchored to A–Z labelling cap; JS MAX_COURTS must match)", MaxCourts)
	}
}

func TestPinMaxRankOverride(t *testing.T) {
	if MaxRankOverride != 1000 {
		t.Fatalf("MaxRankOverride = %d, want 1000 (overflow guard; JS MAX_RANK must match)", MaxRankOverride)
	}
}

func TestPinDateYearBounds(t *testing.T) {
	if MinDateYear != 1900 {
		t.Fatalf("MinDateYear = %d, want 1900 (JS MIN_YEAR must match)", MinDateYear)
	}
	if MaxDateYear != 2100 {
		t.Fatalf("MaxDateYear = %d, want 2100 (JS MAX_YEAR must match)", MaxDateYear)
	}
	if MinDateYear >= MaxDateYear {
		t.Fatalf("MinDateYear (%d) must be < MaxDateYear (%d)", MinDateYear, MaxDateYear)
	}
}
