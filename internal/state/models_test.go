package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
