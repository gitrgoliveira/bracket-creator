package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

// TestDecisionConstants verifies FR-030, NFR-011, FR-044: the eight Decision
// wire values exist and Decision.Valid() accepts each (including the empty
// "none" sentinel) while rejecting unknown values.
//
// This is a Red test, domain.Decision and Decision.Valid() do not yet exist.
// The build must fail until the Green implementation (T074) lands.
func TestDecisionConstants(t *testing.T) {
	cases := []struct {
		name string
		c    domain.Decision
		wire string
	}{
		{"none", domain.DecisionNone, ""},
		{"fought", domain.DecisionFought, "fought"},
		{"hikiwake", domain.DecisionHikiwake, "hikiwake"},
		{"kiken", domain.DecisionKiken, "kiken"},
		{"kiken-voluntary", domain.DecisionKikenVoluntary, "kiken-voluntary"},
		{"kiken-injury", domain.DecisionKikenInjury, "kiken-injury"},
		{"fusenpai", domain.DecisionFusenpai, "fusenpai"},
		{"fusensho", domain.DecisionFusensho, "fusensho"},
		{"daihyosen", domain.DecisionDaihyosen, "daihyosen"},
		{"kachinuki-exhaustion", domain.DecisionKachinukiExhaustion, "kachinuki-exhaustion"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equalf(t, tc.wire, string(tc.c), "wire value mismatch for %s", tc.name)
			assert.Truef(t, tc.c.Valid(), "Valid() must return true for %s", tc.name)
		})
	}

	assert.False(t, domain.Decision("bogus").Valid(), "Valid() must reject unknown values")
}

func TestIsKikenDecision(t *testing.T) {
	assert.True(t, domain.IsKikenDecision(domain.DecisionKiken))
	assert.True(t, domain.IsKikenDecision(domain.DecisionKikenVoluntary))
	assert.True(t, domain.IsKikenDecision(domain.DecisionKikenInjury))
	assert.False(t, domain.IsKikenDecision(domain.DecisionFusenpai))
	assert.False(t, domain.IsKikenDecision(domain.DecisionFought))
	assert.False(t, domain.IsKikenDecision(domain.DecisionNone))
}

func TestIsKikenDecisionStr(t *testing.T) {
	assert.True(t, domain.IsKikenDecisionStr("kiken"))
	assert.True(t, domain.IsKikenDecisionStr("kiken-voluntary"))
	assert.True(t, domain.IsKikenDecisionStr("kiken-injury"))
	assert.False(t, domain.IsKikenDecisionStr("fusenpai"))
	assert.False(t, domain.IsKikenDecisionStr("bogus"))
}

// TestDefaultWinHelpers pins the default-win decision class and the maru
// award: one circle per point — the two-point pair in regulation, the
// single deciding point in encho.
func TestDefaultWinHelpers(t *testing.T) {
	for _, d := range []string{"kiken", "kiken-voluntary", "kiken-injury", "fusenpai", "fusensho"} {
		assert.True(t, domain.IsDefaultWinDecisionStr(d), d)
	}
	for _, d := range []string{"", "fought", "hikiwake", "daihyosen", "kachinuki-exhaustion"} {
		assert.False(t, domain.IsDefaultWinDecisionStr(d), d)
	}
	assert.Equal(t, []string{"○", "○"}, domain.DefaultWinIppons(false))
	assert.Equal(t, []string{"○"}, domain.DefaultWinIppons(true))
}

// The hantei-compatible set has TWO enforcers at different layers - the HTTP
// validator on the way in, and the engine's preserve on the way out, which
// mutates a row after validation and is never re-checked. They share this
// predicate precisely so they cannot drift; pin the membership.
func TestIsHanteiCompatibleDecision(t *testing.T) {
	compatible := []domain.Decision{domain.DecisionNone, domain.DecisionFought, domain.DecisionDaihyosen}
	for _, d := range compatible {
		if !domain.IsHanteiCompatibleDecision(d) {
			t.Errorf("decision %q must be compatible with hantei", d)
		}
		if !domain.IsHanteiCompatibleDecisionStr(string(d)) {
			t.Errorf("string form disagrees for %q", d)
		}
	}
	// Anything that already settles the bout another way is incompatible: a
	// hantei declares a winner from a TIED bout.
	for _, d := range []domain.Decision{
		domain.DecisionHikiwake, domain.DecisionKiken, domain.DecisionKikenVoluntary, domain.DecisionKikenInjury,
		domain.DecisionFusenpai, domain.DecisionFusensho, domain.DecisionKachinukiExhaustion,
	} {
		if domain.IsHanteiCompatibleDecision(d) {
			t.Errorf("decision %q must NOT be compatible with hantei", d)
		}
	}
	if domain.IsHanteiCompatibleDecisionStr("not-a-decision") {
		t.Error("an unknown wire value must not be compatible")
	}
}
