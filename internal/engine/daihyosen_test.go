package engine

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDaihyosenAddableOnlyWhenTied covers T125, the engine-side
// validation gate for adding a daihyosen bout. Each subtest exercises
// one branch of AddDaihyosen so the error → HTTP-status mapping in the
// handler is unambiguous (400 not_tied, 400 pool_match, 409
// insufficient_eligibility).
//
// FR-046, CHK026.
func TestDaihyosenAddableOnlyWhenTied(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	compID := "daihyosen-validate"
	matchID := "r1-m0" // bracket-shaped ID; isPool is passed explicitly

	tests := []struct {
		name          string
		sideA         TeamSummary
		sideB         TeamSummary
		isPool        bool
		sideAEligible int
		sideBEligible int
		wantErr       error
		wantSub       bool
	}{
		{
			name:          "tied IV and PW in knockout returns placeholder",
			sideA:         TeamSummary{IndividualWins: 2, PointsWon: 3},
			sideB:         TeamSummary{IndividualWins: 2, PointsWon: 3},
			isPool:        false,
			sideAEligible: 4,
			sideBEligible: 5,
			wantErr:       nil,
			wantSub:       true,
		},
		{
			name:          "IV differ returns ErrNotTied",
			sideA:         TeamSummary{IndividualWins: 3, PointsWon: 4},
			sideB:         TeamSummary{IndividualWins: 2, PointsWon: 4},
			isPool:        false,
			sideAEligible: 5,
			sideBEligible: 5,
			wantErr:       ErrNotTied,
		},
		{
			name:          "PW differ returns ErrNotTied",
			sideA:         TeamSummary{IndividualWins: 2, PointsWon: 3},
			sideB:         TeamSummary{IndividualWins: 2, PointsWon: 5},
			isPool:        false,
			sideAEligible: 5,
			sideBEligible: 5,
			wantErr:       ErrNotTied,
		},
		{
			name:          "pool match returns ErrPoolMatch",
			sideA:         TeamSummary{IndividualWins: 2, PointsWon: 3},
			sideB:         TeamSummary{IndividualWins: 2, PointsWon: 3},
			isPool:        true,
			sideAEligible: 5,
			sideBEligible: 5,
			wantErr:       ErrPoolMatch,
		},
		{
			name:          "sideA has zero eligible returns ErrInsufficientEligibility",
			sideA:         TeamSummary{IndividualWins: 2, PointsWon: 3},
			sideB:         TeamSummary{IndividualWins: 2, PointsWon: 3},
			isPool:        false,
			sideAEligible: 0,
			sideBEligible: 4,
			wantErr:       ErrInsufficientEligibility,
		},
		{
			name:          "sideB has zero eligible returns ErrInsufficientEligibility",
			sideA:         TeamSummary{IndividualWins: 2, PointsWon: 3},
			sideB:         TeamSummary{IndividualWins: 2, PointsWon: 3},
			isPool:        false,
			sideAEligible: 4,
			sideBEligible: 0,
			wantErr:       ErrInsufficientEligibility,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sub, err := eng.AddDaihyosen(compID, matchID, tc.sideA, tc.sideB, tc.isPool, tc.sideAEligible, tc.sideBEligible)
			if tc.wantErr != nil {
				require.Error(t, err)
				assert.Truef(t, errors.Is(err, tc.wantErr), "want errors.Is == %v, got %v", tc.wantErr, err)
				assert.Nil(t, sub, "no placeholder should be returned on error")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, sub)
			if tc.wantSub {
				assert.Equal(t, -1, sub.Position, "daihyosen sentinel position must be -1")
				assert.Equal(t, string(domain.DecisionDaihyosen), sub.Decision)
			}
		})
	}
}

// TestIsTied covers the equality predicate independently of
// AddDaihyosen so a regression that breaks the tie check is caught
// at the right granularity.
func TestIsTied(t *testing.T) {
	tests := []struct {
		name string
		a, b TeamSummary
		want bool
	}{
		{"both zero", TeamSummary{}, TeamSummary{}, true},
		{"same IV same PW", TeamSummary{2, 3}, TeamSummary{2, 3}, true},
		{"diff IV same PW", TeamSummary{2, 3}, TeamSummary{3, 3}, false},
		{"same IV diff PW", TeamSummary{2, 3}, TeamSummary{2, 4}, false},
		{"diff IV diff PW", TeamSummary{1, 2}, TeamSummary{3, 4}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, IsTied(tc.a, tc.b))
		})
	}
}

// TestIsPoolMatchID covers the prefix-based pool/bracket distinction
// used by the daihyosen handler.
func TestIsPoolMatchID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"Pool A-0", true},
		{"Pool B-12", true},
		{"Pool AAA-1", true},
		{"r1-m0", false},
		{"r2-m3", false},
		{"", false},
		{"PoolA-0", false}, // helper format uses a literal space
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPoolMatchID(tc.id))
		})
	}
}

// TestComputeTeamSummaryFromSubResults validates the aggregation
// helper used by the handler to derive TeamSummary values from a
// completed team match. Daihyosen sentinel rows (Position == -1) are
// skipped so re-validation after an aborted daihyosen attempt is
// idempotent.
func TestComputeTeamSummaryFromSubResults(t *testing.T) {
	sideA := "TeamA"
	sideB := "TeamB"

	subs := []state.SubMatchResult{
		// SideA wins 2-0
		{Position: 1, SideA: sideA, SideB: sideB, IpponsA: []string{"M", "K"}, IpponsB: nil, Winner: sideA},
		// SideB wins 2-1
		{Position: 2, SideA: sideA, SideB: sideB, IpponsA: []string{"M"}, IpponsB: []string{"D", "K"}, Winner: sideB},
		// Draw 1-1
		{Position: 3, SideA: sideA, SideB: sideB, IpponsA: []string{"M"}, IpponsB: []string{"K"}, Winner: ""},
		// Daihyosen placeholder, must be skipped
		{Position: -1, Decision: string(domain.DecisionDaihyosen)},
	}

	a, b := ComputeTeamSummary(subs, sideA, sideB)
	assert.Equal(t, TeamSummary{IndividualWins: 1, PointsWon: 4}, a)
	assert.Equal(t, TeamSummary{IndividualWins: 1, PointsWon: 3}, b)
	assert.True(t, IsTied(TeamSummary{IndividualWins: a.IndividualWins, PointsWon: 0},
		TeamSummary{IndividualWins: b.IndividualWins, PointsWon: 0}),
		"IV should match in this scenario")
}
