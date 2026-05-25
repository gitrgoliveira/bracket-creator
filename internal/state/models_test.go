package state

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTournamentDefaults_ZeroValues(t *testing.T) {
	tour := &Tournament{}
	ApplyTournamentDefaults(tour)
	assert.Equal(t, 1.5, tour.ClockToElapsedMultiplier)
	assert.Equal(t, 10, tour.SlowestCourtBufferPct)
}

func TestApplyTournamentDefaults_NonZeroPreserved(t *testing.T) {
	tour := &Tournament{
		ClockToElapsedMultiplier: 2.0,
		SlowestCourtBufferPct:    20,
	}
	ApplyTournamentDefaults(tour)
	assert.Equal(t, 2.0, tour.ClockToElapsedMultiplier)
	assert.Equal(t, 20, tour.SlowestCourtBufferPct)
}

func TestApplyTournamentDefaults_Nil(t *testing.T) {
	// Must not panic
	ApplyTournamentDefaults(nil)
}

func TestApplyTournamentDefaults_Idempotent(t *testing.T) {
	tour := &Tournament{}
	ApplyTournamentDefaults(tour)
	ApplyTournamentDefaults(tour)
	assert.Equal(t, 1.5, tour.ClockToElapsedMultiplier)
	assert.Equal(t, 10, tour.SlowestCourtBufferPct)
}

func TestHanteiPtr(t *testing.T) {
	assert.Nil(t, HanteiPtr(false), "false should map to nil so omitempty omits the field")
	got := HanteiPtr(true)
	require.NotNil(t, got)
	assert.True(t, *got, "true should map to a non-nil pointer to true")
}

// TestMatchResult_HanteiOmitempty pins the wire contract: a MatchResult
// projected from a non-hantei BracketMatch using HanteiPtr must NOT emit
// the field. Regression for the bug where `&bm.DecidedByHantei` (always
// non-nil) leaked `"decidedByHantei": false` into every SSE payload and
// every HTTP response, defeating the omitempty contract.
func TestMatchResult_HanteiOmitempty(t *testing.T) {
	t.Run("non-hantei projection omits field", func(t *testing.T) {
		mr := &MatchResult{ID: "m1", DecidedByHantei: HanteiPtr(false)}
		b, err := json.Marshal(mr)
		require.NoError(t, err)
		assert.NotContains(t, string(b), "decidedByHantei", "wire payload must omit the field for non-hantei matches")
	})
	t.Run("hantei projection emits true", func(t *testing.T) {
		mr := &MatchResult{ID: "m1", DecidedByHantei: HanteiPtr(true)}
		b, err := json.Marshal(mr)
		require.NoError(t, err)
		assert.Contains(t, string(b), `"decidedByHantei":true`)
	})
}

func TestValidateTeamMatchType(t *testing.T) {
	tests := []struct {
		name     string
		t        TeamMatchType
		teamSize int
		wantErr  bool
	}{
		{"empty is fixed default", "", 0, false},
		{"fixed explicit", TeamMatchTypeFixed, 0, false},
		{"kachinuki with size 2", TeamMatchTypeKachinuki, 2, false},
		{"kachinuki with size 5", TeamMatchTypeKachinuki, 5, false},
		{"kachinuki with size 1 errors", TeamMatchTypeKachinuki, 1, true},
		{"kachinuki with size 0 errors", TeamMatchTypeKachinuki, 0, true},
		{"unknown value errors", "bogus", 5, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTeamMatchType(tc.t, tc.teamSize)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
