package domain_test

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTeamLineup_OrderedMembers_FivePerson verifies that a fully-filled
// 5-person lineup returns slots in the canonical Senpo-to-Taisho order
// regardless of the map iteration order.
func TestTeamLineup_OrderedMembers_FivePerson(t *testing.T) {
	l := domain.TeamLineup{
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "S",
			domain.PosJiho:    "J",
			domain.PosChuken:  "C",
			domain.PosFukusho: "F",
			domain.PosTaisho:  "T",
		},
	}
	got := l.OrderedMembers(5)
	want := []domain.LineupSlot{
		{Position: domain.PosSenpo, Name: "S"},
		{Position: domain.PosJiho, Name: "J"},
		{Position: domain.PosChuken, Name: "C"},
		{Position: domain.PosFukusho, Name: "F"},
		{Position: domain.PosTaisho, Name: "T"},
	}
	assert.Equal(t, want, got)
}

// TestComparePositions_IsTheOrderOrderedMembersWalks pins that the order the
// Kachinuki Detail export labels a fighter's first position by is the order the
// roster walks the lineup in: for every team size, ordering the positions a full
// lineup holds with ComparePositions gives what OrderedMembers returns.
func TestComparePositions_IsTheOrderOrderedMembersWalks(t *testing.T) {
	for size := 1; size <= 12; size++ {
		t.Run(fmt.Sprintf("team of %d", size), func(t *testing.T) {
			positions := map[domain.Position]string{}
			var walked []domain.Position
			if size == 5 {
				for _, p := range []domain.Position{domain.PosTaisho, domain.PosChuken, domain.PosSenpo, domain.PosFukusho, domain.PosJiho} {
					positions[p] = string(p)
				}
			} else {
				for n := size; n >= 1; n-- {
					positions[domain.PositionNumbered(n)] = fmt.Sprintf("fighter-%d", n)
				}
			}
			for _, slot := range (domain.TeamLineup{Positions: positions}).OrderedMembers(size) {
				walked = append(walked, slot.Position)
			}

			ordered := slices.SortedFunc(maps.Keys(positions), domain.ComparePositions)

			assert.Equal(t, walked, ordered)
			assert.Len(t, ordered, size)
		})
	}
}

// TestComparePositions: the FIK names come before the numbered positions, which
// are ordered by number and not as text, and anything else comes last, by name.
func TestComparePositions(t *testing.T) {
	ordered := []domain.Position{
		domain.PosSenpo, domain.PosJiho, domain.PosChuken, domain.PosFukusho, domain.PosTaisho,
		"1", "2", "10", "11",
		"-1", "alpha", "beta",
	}
	for i, a := range ordered {
		for j, b := range ordered {
			assert.Equalf(t, cmp.Compare(i, j), domain.ComparePositions(a, b), "%q against %q", a, b)
		}
	}
}

// TestTeamLineup_ApplyChanges: a save that names the positions it changed lands
// them on a base and leaves every other position as the base holds it.
func TestTeamLineup_ApplyChanges(t *testing.T) {
	base := domain.TeamLineup{
		TeamID: "team", CompetitionID: "comp", MatchID: "Pool A-1", Round: 0,
		Positions: map[domain.Position]string{domain.PosSenpo: "Ito", domain.PosJiho: "Ueno", domain.PosChuken: "Endo"},
		MemberIDs: map[domain.Position]string{domain.PosSenpo: "m-ito", domain.PosJiho: "m-ueno"},
	}
	apply := func(t *testing.T, changed []domain.Position, positions, ids map[domain.Position]string) domain.TeamLineup {
		t.Helper()
		got, err := base.ApplyChanges(changed, positions, ids)
		require.NoError(t, err)
		return got
	}

	t.Run("a name with no id sets the name and drops the id the position had", func(t *testing.T) {
		got := apply(t, []domain.Position{domain.PosSenpo}, map[domain.Position]string{domain.PosSenpo: "Kato"}, nil)

		assert.Equal(t, "Kato", got.Positions[domain.PosSenpo])
		assert.NotContains(t, got.MemberIDs, domain.PosSenpo)
		assert.Equal(t, "m-ueno", got.MemberIDs[domain.PosJiho], "another position keeps its id")
	})

	t.Run("a name and an id set both", func(t *testing.T) {
		got := apply(t, []domain.Position{domain.PosChuken}, map[domain.Position]string{domain.PosChuken: "Kato"}, map[domain.Position]string{domain.PosChuken: "m-kato"})

		assert.Equal(t, "Kato", got.Positions[domain.PosChuken])
		assert.Equal(t, "m-kato", got.MemberIDs[domain.PosChuken])
	})

	t.Run("an id with no name places a member who has none yet", func(t *testing.T) {
		got := apply(t, []domain.Position{domain.PosFukusho}, map[domain.Position]string{domain.PosFukusho: ""}, map[domain.Position]string{domain.PosFukusho: "m-blank"})

		name, held := got.Positions[domain.PosFukusho]
		assert.True(t, held)
		assert.Empty(t, name)
		assert.Equal(t, "m-blank", got.MemberIDs[domain.PosFukusho])
	})

	t.Run("neither a name nor an id clears the position from both maps", func(t *testing.T) {
		got := apply(t, []domain.Position{domain.PosSenpo}, map[domain.Position]string{domain.PosSenpo: ""}, nil)

		assert.NotContains(t, got.Positions, domain.PosSenpo)
		assert.NotContains(t, got.MemberIDs, domain.PosSenpo)
	})

	t.Run("a position that is not changed is left as the base holds it, whatever the save carries", func(t *testing.T) {
		got := apply(t, []domain.Position{domain.PosJiho},
			map[domain.Position]string{domain.PosJiho: "Ueda", domain.PosSenpo: "stale", domain.PosTaisho: "Kato", "nonsense": "x"},
			map[domain.Position]string{domain.PosSenpo: "m-stale", domain.PosTaisho: "m-kato"})

		assert.Equal(t, map[domain.Position]string{domain.PosSenpo: "Ito", domain.PosJiho: "Ueda", domain.PosChuken: "Endo"}, got.Positions)
		assert.Equal(t, map[domain.Position]string{domain.PosSenpo: "m-ito"}, got.MemberIDs)
	})

	t.Run("the identity of the base is kept and nothing is shared with it", func(t *testing.T) {
		got := apply(t, []domain.Position{domain.PosSenpo}, map[domain.Position]string{domain.PosSenpo: "Kato"}, nil)
		got.Positions[domain.PosJiho] = "changed afterwards"

		assert.Equal(t, "team", got.TeamID)
		assert.Equal(t, "Pool A-1", got.MatchID)
		assert.Equal(t, "Ueno", base.Positions[domain.PosJiho])
		assert.Equal(t, "Ito", base.Positions[domain.PosSenpo])
		assert.Equal(t, "m-ito", base.MemberIDs[domain.PosSenpo])
	})

	t.Run("an empty base takes the changes, and clearing the last position leaves an empty map", func(t *testing.T) {
		got, err := domain.TeamLineup{}.ApplyChanges([]domain.Position{domain.PosSenpo}, map[domain.Position]string{domain.PosSenpo: "Ito"}, nil)
		require.NoError(t, err)
		assert.Equal(t, map[domain.Position]string{domain.PosSenpo: "Ito"}, got.Positions)
		assert.Nil(t, got.MemberIDs, "no ids is no map, as a lineup saved without ids has none")

		emptied, err := got.ApplyChanges([]domain.Position{domain.PosSenpo}, map[domain.Position]string{domain.PosSenpo: ""}, nil)
		require.NoError(t, err)
		assert.NotNil(t, emptied.Positions)
		assert.Empty(t, emptied.Positions)
	})

	t.Run("a changed position with no entry in positions is refused, naming it", func(t *testing.T) {
		_, err := base.ApplyChanges([]domain.Position{domain.PosSenpo, domain.PosJiho}, map[domain.Position]string{domain.PosSenpo: "Kato"}, nil)

		require.ErrorIs(t, err, domain.ErrLineupChangedPositionMissing)
		assert.Contains(t, err.Error(), "jiho")
		assert.True(t, strings.HasPrefix(err.Error(), "team_lineup:"), "it classifies as a client error like every lineup refusal")
	})

	t.Run("no changed positions is refused", func(t *testing.T) {
		for _, changed := range [][]domain.Position{nil, {}} {
			_, err := base.ApplyChanges(changed, map[domain.Position]string{domain.PosSenpo: "Kato"}, nil)

			require.ErrorIs(t, err, domain.ErrLineupNoChangedPositions)
		}
	})

	t.Run("a member placed at two positions is left for ValidatePositions to name", func(t *testing.T) {
		got := apply(t, []domain.Position{domain.PosChuken}, map[domain.Position]string{domain.PosChuken: "Ito"}, map[domain.Position]string{domain.PosChuken: "m-ito"})

		err := got.ValidatePositions(5)

		require.ErrorIs(t, err, domain.ErrLineupDuplicateMember)
		assert.Contains(t, err.Error(), "senpo")
		assert.Contains(t, err.Error(), "chuken")
	})
}

// TestTeamLineup_OrderedMembers_ThreePerson verifies the numeric-position
// path: positions "1", "2", "3" are returned in ascending numeric order.
func TestTeamLineup_OrderedMembers_ThreePerson(t *testing.T) {
	l := domain.TeamLineup{
		Positions: map[domain.Position]string{
			domain.PositionNumbered(3): "Three",
			domain.PositionNumbered(1): "One",
			domain.PositionNumbered(2): "Two",
		},
	}
	got := l.OrderedMembers(3)
	want := []domain.LineupSlot{
		{Position: domain.PositionNumbered(1), Name: "One"},
		{Position: domain.PositionNumbered(2), Name: "Two"},
		{Position: domain.PositionNumbered(3), Name: "Three"},
	}
	assert.Equal(t, want, got)
}

// TestTeamLineup_OrderedMembers_Empty verifies that a lineup with no
// filled positions returns an empty (non-nil) slice.
func TestTeamLineup_OrderedMembers_Empty(t *testing.T) {
	l := domain.TeamLineup{Positions: map[domain.Position]string{}}
	got := l.OrderedMembers(5)
	require.NotNil(t, got)
	assert.Empty(t, got)
}

// TestTeamLineup_OrderedMembers_AlignmentPin is THE alignment pin (bc-tmid
// pass 2): a lineup where one occupied position has a MemberIDs entry and a
// SECOND occupied position does NOT (an unrepaired legacy slot) must never
// misalign a name to the wrong id. OrderedMembers reads Name and MemberID
// from the SAME position key in the SAME step, so this is exercised
// directly; a broken implementation that walked Positions and MemberIDs as
// two SEPARATE skip-on-empty traversals would drift the moment one slot's
// id is missing, because the two walks skip at different indices.
func TestTeamLineup_OrderedMembers_AlignmentPin(t *testing.T) {
	l := domain.TeamLineup{
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "Sato",   // repaired: has a member id
			domain.PosJiho:    "Tanaka", // UNREPAIRED: no member id yet
			domain.PosChuken:  "Suzuki", // repaired: has a member id
			domain.PosFukusho: "",       // vacant
			domain.PosTaisho:  "Ito",    // repaired: has a member id
		},
		MemberIDs: map[domain.Position]string{
			domain.PosSenpo:  "id-sato",
			domain.PosChuken: "id-suzuki",
			domain.PosTaisho: "id-ito",
			// domain.PosJiho deliberately absent: Tanaka's slot is unrepaired.
		},
	}

	got := l.OrderedMembers(5)
	require.Len(t, got, 4, "four occupied positions, Fukusho vacant")

	want := []domain.LineupSlot{
		{Position: domain.PosSenpo, Name: "Sato", MemberID: "id-sato"},
		{Position: domain.PosJiho, Name: "Tanaka", MemberID: ""},
		{Position: domain.PosChuken, Name: "Suzuki", MemberID: "id-suzuki"},
		{Position: domain.PosTaisho, Name: "Ito", MemberID: "id-ito"},
	}
	assert.Equal(t, want, got, "each slot's name must stay paired with ITS OWN member id, not a neighbour's")

	// The Name fields alone must come out in the identical order, Tanaka's
	// missing id notwithstanding.
	gotNames := make([]string, len(got))
	for i, m := range got {
		gotNames[i] = m.Name
	}
	assert.Equal(t, []string{"Sato", "Tanaka", "Suzuki", "Ito"}, gotNames)
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

// TestTeamLineupValidatePositions_MemberIDs pins the MemberIDs half of the
// key-only contract (bc-tmid pass 2): an illegal position key in MemberIDs
// is rejected exactly like an illegal key in Positions, and a lineup naming
// the id ahead of the name (a position present in MemberIDs but not yet in
// Positions) is legal, matching the "the id half may arrive first" note on
// ValidatePositions.
func TestTeamLineupValidatePositions_MemberIDs(t *testing.T) {
	cases := []struct {
		name    string
		size    int
		lineup  domain.TeamLineup
		wantErr bool
	}{
		{
			name: "5p legal MemberIDs key ok",
			size: 5,
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{domain.PosSenpo: "a"},
				MemberIDs: map[domain.Position]string{domain.PosSenpo: "id-a"},
			},
		},
		{
			name: "5p MemberIDs key ahead of Positions ok",
			size: 5,
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{},
				MemberIDs: map[domain.Position]string{domain.PosTaisho: "id-e"},
			},
		},
		{
			name: "5p numbered MemberIDs key rejected",
			size: 5,
			lineup: domain.TeamLineup{
				MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): "id-x"},
			},
			wantErr: true,
		},
		{
			name: "3p named senpo MemberIDs key rejected",
			size: 3,
			lineup: domain.TeamLineup{
				MemberIDs: map[domain.Position]string{domain.PosSenpo: "id-a"},
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.lineup.ValidatePositions(tc.size)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestTeamLineupValidatePositions_DuplicateMember pins the duplicate-member
// guard: the same squad member id may not occupy two positions in one
// lineup, whether the positions are named (5-person) or numeric keys. A
// member id present at only ONE position is legal -- that's simply the
// same slot, not a conflict.
func TestTeamLineupValidatePositions_DuplicateMember(t *testing.T) {
	cases := []struct {
		name    string
		size    int
		lineup  domain.TeamLineup
		wantErr bool
	}{
		{
			name: "named positions: same member at two positions rejected",
			size: 5,
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{
					domain.PosSenpo: "Sato",
					domain.PosJiho:  "Sato",
				},
				MemberIDs: map[domain.Position]string{
					domain.PosSenpo: "id-sato",
					domain.PosJiho:  "id-sato",
				},
			},
			wantErr: true,
		},
		{
			name: "numeric positions: same member at two positions rejected",
			size: 3,
			lineup: domain.TeamLineup{
				MemberIDs: map[domain.Position]string{
					domain.PositionNumbered(1): "id-x",
					domain.PositionNumbered(2): "id-x",
				},
			},
			wantErr: true,
		},
		{
			name: "same member id at only one position is fine",
			size: 5,
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{
					domain.PosSenpo: "Sato",
				},
				MemberIDs: map[domain.Position]string{
					domain.PosSenpo: "id-sato",
				},
			},
		},
		{
			name: "distinct members at distinct positions is fine",
			size: 5,
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{
					domain.PosSenpo: "Sato",
					domain.PosJiho:  "Tanaka",
				},
				MemberIDs: map[domain.Position]string{
					domain.PosSenpo: "id-sato",
					domain.PosJiho:  "id-tanaka",
				},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.lineup.ValidatePositions(tc.size)
			if tc.wantErr {
				require.Error(t, err)
				assert.True(t, errors.Is(err, domain.ErrLineupDuplicateMember))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestTeamLineupOrderedMembers_IdOnlySlotIsOccupied pins the bc-dnst rule
// that a squad slot picked by number before it is named still fields a
// fighter: the walk keeps a position carrying only a member id, in order,
// with its empty name, and skips only a position with neither.
func TestTeamLineupOrderedMembers_IdOnlySlotIsOccupied(t *testing.T) {
	lineup := domain.TeamLineup{
		Positions: map[domain.Position]string{domain.PositionNumbered(1): "Aoki", domain.PositionNumbered(2): ""},
		MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): "id-aoki", domain.PositionNumbered(2): "id-blank"},
	}
	got := lineup.OrderedMembers(3)
	require.Len(t, got, 2)
	assert.Equal(t, domain.LineupSlot{Position: domain.PositionNumbered(1), Name: "Aoki", MemberID: "id-aoki"}, got[0])
	assert.Equal(t, domain.LineupSlot{Position: domain.PositionNumbered(2), Name: "", MemberID: "id-blank"}, got[1])
	gotNames := make([]string, len(got))
	for i, m := range got {
		gotNames[i] = m.Name
	}
	assert.Equal(t, []string{"Aoki", ""}, gotNames)
}

// PositionForBout names the position that fights numbered bout n, which the
// team finish gate's refusal uses to label a five-person team's bouts.
func TestPositionForBout(t *testing.T) {
	pos, ok := domain.PositionForBout(5, 4)
	require.True(t, ok)
	assert.Equal(t, domain.PosFukusho, pos)
	pos, ok = domain.PositionForBout(3, 3)
	require.True(t, ok)
	assert.Equal(t, domain.PositionNumbered(3), pos)
	_, ok = domain.PositionForBout(3, 4)
	assert.False(t, ok)
	_, ok = domain.PositionForBout(5, 0)
	assert.False(t, ok)
}

func TestPosition_Label(t *testing.T) {
	tests := []struct {
		pos  domain.Position
		want string
	}{
		{domain.PosSenpo, "Senpo"},
		{domain.PosJiho, "Jiho"},
		{domain.PosChuken, "Chuken"},
		{domain.PosFukusho, "Fukusho"},
		{domain.PosTaisho, "Taisho"},
		{domain.PositionNumbered(3), "3"},
		{"1", "1"},
		{"", ""},
		{"unknown", "Unknown"},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, tc.pos.Label(), "position %q", tc.pos)
	}
}
