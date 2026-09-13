package state

import "testing"

// TestEffectiveTwoThirdPlaces_BehaviourPreservation pins the four cases
// bc-3rdp's acceptance bar names, each asserting the SAME answer
// RequiresSingleThirdPlace / applyJointThirdRanks (internal/engine) gave
// before TwoThirdPlaces existed, computed here directly against the two
// resolvers state.Competition now exposes: EffectiveTwoThirdPlaces (the one
// named rule) and RequiresSingleThirdPlace (its exact negation, the
// knockout-facing predicate). Every case below leaves TwoThirdPlaces nil
// (never explicitly set), which is exactly the shape of every competition
// record written before this field existed -- the fallback chain in
// EffectiveTwoThirdPlaces exists to answer these old records identically to
// how the pre-bc-3rdp code answered them.
func TestEffectiveTwoThirdPlaces_BehaviourPreservation(t *testing.T) {
	t.Run("naginata knockout requires a single 3rd (bronze match)", func(t *testing.T) {
		c := Competition{Format: CompFormatKnockout, Naginata: true}
		if got := c.RequiresSingleThirdPlace(); !got {
			t.Fatalf("RequiresSingleThirdPlace() = %v, want true (bronze match for naginata knockout)", got)
		}
		if got := c.EffectiveTwoThirdPlaces(); got {
			t.Fatalf("EffectiveTwoThirdPlaces() = %v, want false (naginata knockout has no joint 3rd)", got)
		}
	})

	t.Run("non-naginata knockout does not require a single 3rd (no bronze match)", func(t *testing.T) {
		c := Competition{Format: CompFormatKnockout, Naginata: false}
		if got := c.RequiresSingleThirdPlace(); got {
			t.Fatalf("RequiresSingleThirdPlace() = %v, want false (kendo knockout awards joint 3rd, no bronze)", got)
		}
		if got := c.EffectiveTwoThirdPlaces(); !got {
			t.Fatalf("EffectiveTwoThirdPlaces() = %v, want true (kendo knockout convention)", got)
		}
	})

	t.Run("league with LeagueTwoThirdPlaces resolves to joint 3rds (shared ranks)", func(t *testing.T) {
		c := Competition{Format: CompFormatLeague, LeagueTwoThirdPlaces: true}
		if got := c.EffectiveTwoThirdPlaces(); !got {
			t.Fatalf("EffectiveTwoThirdPlaces() = %v, want true (league legacy fallback reads LeagueTwoThirdPlaces=true)", got)
		}
		// A league never builds a bracket, so RequiresSingleThirdPlace is
		// never the caller that matters here, but it must still agree with
		// the negation contract.
		if got := c.RequiresSingleThirdPlace(); got {
			t.Fatalf("RequiresSingleThirdPlace() = %v, want false", got)
		}
	})

	t.Run("league without LeagueTwoThirdPlaces resolves to a single 3rd", func(t *testing.T) {
		c := Competition{Format: CompFormatLeague, LeagueTwoThirdPlaces: false}
		if got := c.EffectiveTwoThirdPlaces(); got {
			t.Fatalf("EffectiveTwoThirdPlaces() = %v, want false (league legacy fallback reads LeagueTwoThirdPlaces=false)", got)
		}
		if got := c.RequiresSingleThirdPlace(); !got {
			t.Fatalf("RequiresSingleThirdPlace() = %v, want true", got)
		}
	})
}

// TestEffectiveTwoThirdPlaces_TrapCase is the one thing bc-3rdp must not
// break: a non-naginata knockout whose stored LeagueTwoThirdPlaces is
// absent/false must NOT gain a bronze match. Before TwoThirdPlaces was added
// as a *bool, a naive unification (e.g. reading a single shared bool field
// across every format) could not tell "operator explicitly chose false" from
// "this field has never meant anything for this format" -- and a knockout
// competition's LeagueTwoThirdPlaces is exactly the latter: the field is
// league-only and is dead data for every other format (harmlessly present or
// absent on disk, per admin_setup.jsx's "send it for every format" history,
// but never READ for a non-league record either before or after bc-3rdp).
// This test pins that EffectiveTwoThirdPlaces's format-dependent fallback
// (step 2: "format is league") is what keeps that dead data from leaking
// into the knockout-side rule (step 3: "!Naginata").
func TestEffectiveTwoThirdPlaces_TrapCase(t *testing.T) {
	t.Run("absent LeagueTwoThirdPlaces on a non-naginata knockout", func(t *testing.T) {
		c := Competition{Format: CompFormatKnockout, Naginata: false}
		// LeagueTwoThirdPlaces left at its Go zero value (false/absent).
		if got := c.RequiresSingleThirdPlace(); got {
			t.Fatalf("RequiresSingleThirdPlace() = %v, want false: a non-naginata knockout must not gain a bronze match merely because a league-only legacy field is unset", got)
		}
	})

	t.Run("LeagueTwoThirdPlaces=true on a non-naginata knockout is still ignored", func(t *testing.T) {
		// Dead data: every create-form submission sends leagueTwoThirdPlaces
		// for every format (see COMPETITION_DEFAULTS' history), so a real
		// on-disk knockout/mixed competition can carry LeagueTwoThirdPlaces:
		// true even though it was never meaningful for that format. It must
		// not leak into the knockout-side rule either.
		c := Competition{Format: CompFormatKnockout, Naginata: false, LeagueTwoThirdPlaces: true}
		if got := c.RequiresSingleThirdPlace(); got {
			t.Fatalf("RequiresSingleThirdPlace() = %v, want false: LeagueTwoThirdPlaces must never be read for a non-league format", got)
		}
	})

	t.Run("mixed format follows the same knockout-side rule as knockout", func(t *testing.T) {
		c := Competition{Format: CompFormatMixed, Naginata: true}
		if got := c.RequiresSingleThirdPlace(); !got {
			t.Fatalf("RequiresSingleThirdPlace() = %v, want true for a naginata mixed competition", got)
		}
	})
}

// TestEffectiveTwoThirdPlaces_ExplicitOverride confirms the operator's
// explicit choice (TwoThirdPlaces set) always wins over both legacy
// fallbacks, in both directions: overriding a naginata knockout back to
// joint 3rds, and overriding a league's legacy joint-3rd flag off.
func TestEffectiveTwoThirdPlaces_ExplicitOverride(t *testing.T) {
	trueVal := true
	falseVal := false

	t.Run("naginata knockout explicitly opted into joint 3rds", func(t *testing.T) {
		c := Competition{Format: CompFormatKnockout, Naginata: true, TwoThirdPlaces: &trueVal}
		if got := c.EffectiveTwoThirdPlaces(); !got {
			t.Fatalf("EffectiveTwoThirdPlaces() = %v, want true (explicit override beats !Naginata fallback)", got)
		}
		if got := c.RequiresSingleThirdPlace(); got {
			t.Fatalf("RequiresSingleThirdPlace() = %v, want false", got)
		}
	})

	t.Run("league explicitly opted out of joint 3rds despite legacy flag", func(t *testing.T) {
		c := Competition{Format: CompFormatLeague, LeagueTwoThirdPlaces: true, TwoThirdPlaces: &falseVal}
		if got := c.EffectiveTwoThirdPlaces(); got {
			t.Fatalf("EffectiveTwoThirdPlaces() = %v, want false (explicit override beats the legacy LeagueTwoThirdPlaces fallback)", got)
		}
	})
}
