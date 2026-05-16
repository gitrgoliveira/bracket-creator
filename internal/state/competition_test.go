package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestCompetitionLoadsPerPhaseDurations verifies FR-053 / NFR-025:
// the Competition struct round-trips per-phase match durations
// (pool_match_duration and playoff_match_duration) through YAML.
func TestCompetitionLoadsPerPhaseDurations(t *testing.T) {
	original := Competition{
		ID:                   "test-comp",
		Name:                 "Per-Phase Durations",
		PoolMatchDuration:    2,
		PlayoffMatchDuration: 3,
	}

	data, err := yaml.Marshal(&original)
	require.NoError(t, err)

	var loaded Competition
	err = yaml.Unmarshal(data, &loaded)
	require.NoError(t, err)

	assert.Equal(t, 2, loaded.PoolMatchDuration, "PoolMatchDuration should round-trip")
	assert.Equal(t, 3, loaded.PlayoffMatchDuration, "PlayoffMatchDuration should round-trip")
}

// TestCompetitionLegacyMatchDurationFallback verifies FR-054 / NFR-025 / R9:
// when YAML lacks the per-phase fields but carries the legacy match_duration
// field, ApplyCompetitionDefaults populates BOTH per-phase durations with the
// legacy value so older tournaments continue to schedule correctly.
func TestCompetitionLegacyMatchDurationFallback(t *testing.T) {
	legacyYAML := []byte(`id: legacy-comp
name: Legacy Comp
match_duration: 5
`)

	var c Competition
	err := yaml.Unmarshal(legacyYAML, &c)
	require.NoError(t, err)

	// Before applying defaults, per-phase fields are zero.
	require.Equal(t, 0, c.PoolMatchDuration)
	require.Equal(t, 0, c.PlayoffMatchDuration)

	ApplyCompetitionDefaults(&c)

	assert.Equal(t, 5, c.PoolMatchDuration, "legacy match_duration should fall through to PoolMatchDuration")
	assert.Equal(t, 5, c.PlayoffMatchDuration, "legacy match_duration should fall through to PlayoffMatchDuration")
}

// TestLeagueFormatHidesPlayoffs verifies FR-050 / FR-051:
// IsPlayoffEnabled() reports whether the competition's format includes a
// playoff phase. League and pure-pools formats return false; playoffs and
// mixed return true.
func TestLeagueFormatHidesPlayoffs(t *testing.T) {
	cases := []struct {
		format string
		want   bool
	}{
		{format: "league", want: false},
		{format: "pools", want: false},
		{format: "playoffs", want: true},
		{format: "mixed", want: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.format, func(t *testing.T) {
			c := Competition{Format: tc.format}
			assert.Equal(t, tc.want, c.IsPlayoffEnabled(), "format=%q", tc.format)
		})
	}
}
