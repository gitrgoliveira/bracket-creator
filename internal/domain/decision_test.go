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
// This is a Red test — domain.Decision and Decision.Valid() do not yet exist.
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
