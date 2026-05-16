package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestLegacyBoolMigrates verifies NFR-025 / R6 backward-compatibility:
// historical YAML saves stored `decision` as a Go bool (true == draw flag).
// After Slice 5.A the field becomes a Decision string, and legacy values
// must migrate on unmarshal — `true` -> DecisionHikiwake, `false` (when the
// match was completed) -> DecisionFought. A new-format string value must
// also round-trip unchanged.
//
// This is a Red test that build-fails until T074 (domain.Decision type +
// constants), T075 (legacy bool migration on UnmarshalYAML), and T076
// (MatchResult.Decision field) all land.
func TestLegacyBoolMigrates(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected domain.Decision
	}{
		{
			name:     "legacy bool true migrates to hikiwake",
			yaml:     "decision: true\n",
			expected: domain.DecisionHikiwake,
		},
		{
			name:     "legacy bool false (completed) migrates to fought",
			yaml:     "decision: false\n",
			expected: domain.DecisionFought,
		},
		{
			name:     "new-format string decision round-trips",
			yaml:     "decision: \"kiken\"\n",
			expected: domain.DecisionKiken,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var r domain.MatchResult
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), &r))
			assert.Equal(t, tc.expected, r.Decision)
		})
	}
}
