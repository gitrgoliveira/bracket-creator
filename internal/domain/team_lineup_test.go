package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTeamLineup_OrderedRoster_FivePerson verifies that a fully-filled
// 5-person lineup returns player names in the canonical Senpo-to-Taisho
// order regardless of the map iteration order.
func TestTeamLineup_OrderedRoster_FivePerson(t *testing.T) {
	l := domain.TeamLineup{
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "S",
			domain.PosJiho:    "J",
			domain.PosChuken:  "C",
			domain.PosFukusho: "F",
			domain.PosTaisho:  "T",
		},
	}
	got := l.OrderedRoster(5)
	assert.Equal(t, []string{"S", "J", "C", "F", "T"}, got)
}

// TestTeamLineup_OrderedRoster_FivePersonWithVacancy verifies that a
// vacant position (empty string) is skipped; the result carries only
// the four filled names in canonical order.
func TestTeamLineup_OrderedRoster_FivePersonWithVacancy(t *testing.T) {
	l := domain.TeamLineup{
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "S",
			domain.PosJiho:    "", // vacant
			domain.PosChuken:  "C",
			domain.PosFukusho: "F",
			domain.PosTaisho:  "T",
		},
	}
	got := l.OrderedRoster(5)
	assert.Equal(t, []string{"S", "C", "F", "T"}, got)
}

// TestTeamLineup_OrderedRoster_ThreePerson verifies the numeric-position
// path: positions "1", "2", "3" are returned in ascending numeric order.
func TestTeamLineup_OrderedRoster_ThreePerson(t *testing.T) {
	l := domain.TeamLineup{
		Positions: map[domain.Position]string{
			domain.PositionNumbered(3): "Three",
			domain.PositionNumbered(1): "One",
			domain.PositionNumbered(2): "Two",
		},
	}
	got := l.OrderedRoster(3)
	assert.Equal(t, []string{"One", "Two", "Three"}, got)
}

// TestTeamLineup_OrderedRoster_Empty verifies that a lineup with no
// filled positions returns an empty (non-nil) slice.
func TestTeamLineup_OrderedRoster_Empty(t *testing.T) {
	l := domain.TeamLineup{Positions: map[domain.Position]string{}}
	got := l.OrderedRoster(5)
	require.NotNil(t, got)
	assert.Empty(t, got)
}

// TestTeamLineupValidatePositions pins the key-only contract (mp-gmcg):
// position KEYS must fit the team size, but vacancies are irrelevant and
// never rejected. Team sizes are unregulated and lineups are entered
// incrementally, so any subset of positions (including none, and
// including a lineup with no Senpo or Taisho) must be persistable. The
// former FIK back-fill/DQ rule (Validate/validateFive) is deliberately
// gone; there is no completeness enforcement at any layer.
func TestTeamLineupValidatePositions(t *testing.T) {
	pos := func(m map[domain.Position]string) domain.TeamLineup {
		return domain.TeamLineup{Positions: m}
	}

	cases := []struct {
		name     string
		size     int
		lineup   domain.TeamLineup
		wantErr  error
		wantSome bool
	}{
		{
			name: "5p all filled ok",
			size: 5,
			lineup: pos(map[domain.Position]string{
				domain.PosSenpo: "a", domain.PosJiho: "b", domain.PosChuken: "c",
				domain.PosFukusho: "d", domain.PosTaisho: "e",
			}),
		},
		{
			name:   "5p empty lineup ok (vacancies never block)",
			size:   5,
			lineup: pos(map[domain.Position]string{}),
		},
		{
			name: "5p missing Senpo and Taisho ok (no completeness rule)",
			size: 5,
			lineup: pos(map[domain.Position]string{
				domain.PosChuken: "c",
			}),
		},
		{
			name: "5p numbered key not allowed",
			size: 5,
			lineup: pos(map[domain.Position]string{
				domain.PosSenpo:            "a",
				domain.PositionNumbered(1): "x",
			}),
			wantSome: true,
		},
		{
			name: "3p numbered ok",
			size: 3,
			lineup: pos(map[domain.Position]string{
				domain.PositionNumbered(1): "a",
				domain.PositionNumbered(3): "c",
			}),
		},
		{
			name: "3p named senpo key rejected",
			size: 3,
			lineup: pos(map[domain.Position]string{
				domain.PosSenpo: "a",
			}),
			wantSome: true,
		},
		{
			name: "3p position out of range rejected",
			size: 3,
			lineup: pos(map[domain.Position]string{
				domain.PositionNumbered(4): "d",
			}),
			wantSome: true,
		},
		{
			name:    "zero teamSize rejected",
			size:    0,
			lineup:  pos(map[domain.Position]string{}),
			wantErr: domain.ErrLineupTeamSizeInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.lineup.ValidatePositions(tc.size)
			switch {
			case tc.wantErr != nil:
				require.ErrorIs(t, err, tc.wantErr)
			case tc.wantSome:
				require.Error(t, err)
			default:
				assert.NoError(t, err)
			}
		})
	}
}
