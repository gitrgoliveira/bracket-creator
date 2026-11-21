package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

func fillMembers(n int, names bool) []domain.TeamMember {
	out := make([]domain.TeamMember, 0, n)
	for i := 1; i <= n; i++ {
		m := domain.TeamMember{ID: "m" + string(rune('0'+i)), Index: i}
		if names {
			m.Name = "Member " + string(rune('0'+i))
		}
		out = append(out, m)
	}
	return out
}

func TestTeamLineup_FilledByMemberNumber(t *testing.T) {
	const (
		senpo   = domain.PosSenpo
		jiho    = domain.PosJiho
		chuken  = domain.PosChuken
		fukusho = domain.PosFukusho
		taisho  = domain.PosTaisho
	)
	tests := []struct {
		name    string
		lineup  domain.TeamLineup
		size    int
		members []domain.TeamMember
		wantPos map[domain.Position]string
		wantIDs map[domain.Position]string
	}{
		{
			name:    "empty lineup, FIK order for five",
			size:    5,
			members: fillMembers(5, true),
			wantPos: map[domain.Position]string{senpo: "Member 1", jiho: "Member 2", chuken: "Member 3", fukusho: "Member 4", taisho: "Member 5"},
			wantIDs: map[domain.Position]string{senpo: "m1", jiho: "m2", chuken: "m3", fukusho: "m4", taisho: "m5"},
		},
		{
			name:    "empty lineup, numbered order for three",
			size:    3,
			members: fillMembers(3, true),
			wantPos: map[domain.Position]string{"1": "Member 1", "2": "Member 2", "3": "Member 3"},
			wantIDs: map[domain.Position]string{"1": "m1", "2": "m2", "3": "m3"},
		},
		{
			name: "partly filled: member 3 at senpo, the rest by number around it",
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{senpo: "Member 3"},
				MemberIDs: map[domain.Position]string{senpo: "m3"},
			},
			size:    5,
			members: fillMembers(5, true),
			wantPos: map[domain.Position]string{senpo: "Member 3", jiho: "Member 1", chuken: "Member 2", fukusho: "Member 4", taisho: "Member 5"},
			wantIDs: map[domain.Position]string{senpo: "m3", jiho: "m1", chuken: "m2", fukusho: "m4", taisho: "m5"},
		},
		{
			name: "empty first place opens with the lowest unplaced member",
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{"2": "Member 2"},
				MemberIDs: map[domain.Position]string{"2": "m2"},
			},
			size:    3,
			members: fillMembers(3, true),
			wantPos: map[domain.Position]string{"1": "Member 1", "2": "Member 2", "3": "Member 3"},
			wantIDs: map[domain.Position]string{"1": "m1", "2": "m2", "3": "m3"},
		},
		{
			name: "a reserve placed blocks nobody and the highest regular is pushed out",
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{"1": "Reserve"},
				MemberIDs: map[domain.Position]string{"1": "m4"},
			},
			size:    3,
			members: fillMembers(4, true),
			wantPos: map[domain.Position]string{"1": "Reserve", "2": "Member 1", "3": "Member 2"},
			wantIDs: map[domain.Position]string{"1": "m4", "2": "m1", "3": "m2"},
		},
		{
			name: "a reserve never fills an empty place",
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{"1": "Member 1"},
				MemberIDs: map[domain.Position]string{"1": "m1"},
			},
			size:    3,
			members: fillMembers(4, true),
			wantPos: map[domain.Position]string{"1": "Member 1", "2": "Member 2", "3": "Member 3"},
			wantIDs: map[domain.Position]string{"1": "m1", "2": "m2", "3": "m3"},
		},
		{
			name: "a typed name with no id keeps its place and blocks no member",
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{"1": "Member 1"},
			},
			size:    3,
			members: fillMembers(3, true),
			wantPos: map[domain.Position]string{"1": "Member 1", "2": "Member 1", "3": "Member 2"},
			wantIDs: map[domain.Position]string{"2": "m1", "3": "m2"},
		},
		{
			name:    "fewer members than the team size leaves the rest empty",
			size:    3,
			members: fillMembers(2, true),
			wantPos: map[domain.Position]string{"1": "Member 1", "2": "Member 2"},
			wantIDs: map[domain.Position]string{"1": "m1", "2": "m2"},
		},
		{
			name:    "blank members fill, by id only",
			size:    3,
			members: fillMembers(3, false),
			wantPos: map[domain.Position]string{},
			wantIDs: map[domain.Position]string{"1": "m1", "2": "m2", "3": "m3"},
		},
		{
			name: "an id-only slot is occupied",
			lineup: domain.TeamLineup{
				MemberIDs: map[domain.Position]string{"1": "m2"},
			},
			size:    3,
			members: fillMembers(3, true),
			wantPos: map[domain.Position]string{"2": "Member 1", "3": "Member 3"},
			wantIDs: map[domain.Position]string{"1": "m2", "2": "m1", "3": "m3"},
		},
		{
			name:    "members out of order are taken by Index",
			size:    3,
			members: []domain.TeamMember{{ID: "m3", Index: 3, Name: "C"}, {ID: "m1", Index: 1, Name: "A"}, {ID: "m2", Index: 2, Name: "B"}},
			wantPos: map[domain.Position]string{"1": "A", "2": "B", "3": "C"},
			wantIDs: map[domain.Position]string{"1": "m1", "2": "m2", "3": "m3"},
		},
		{
			name: "a lineup saved under the places of another size blocks nobody",
			lineup: domain.TeamLineup{
				Positions: map[domain.Position]string{"1": "Member 1", "2": "Member 2", "3": "Member 3"},
				MemberIDs: map[domain.Position]string{"1": "m1", "2": "m2", "3": "m3"},
			},
			size:    5,
			members: fillMembers(5, true),
			wantPos: map[domain.Position]string{
				"1": "Member 1", "2": "Member 2", "3": "Member 3",
				senpo: "Member 1", jiho: "Member 2", chuken: "Member 3", fukusho: "Member 4", taisho: "Member 5",
			},
			wantIDs: map[domain.Position]string{
				"1": "m1", "2": "m2", "3": "m3",
				senpo: "m1", jiho: "m2", chuken: "m3", fukusho: "m4", taisho: "m5",
			},
		},
		{
			name:    "no members leaves the lineup as it was",
			size:    3,
			wantPos: map[domain.Position]string{},
			wantIDs: map[domain.Position]string{},
		},
		{
			name:    "a member with no id is skipped",
			size:    3,
			members: []domain.TeamMember{{Index: 1, Name: "A"}, {ID: "m2", Index: 2, Name: "B"}},
			wantPos: map[domain.Position]string{"1": "B"},
			wantIDs: map[domain.Position]string{"1": "m2"},
		},
		{
			name:    "a member with a zero or negative index is skipped",
			size:    3,
			members: []domain.TeamMember{{ID: "m0", Index: 0, Name: "Zero"}, {ID: "mn", Index: -1, Name: "Minus"}, {ID: "m1", Index: 1, Name: "A"}},
			wantPos: map[domain.Position]string{"1": "A"},
			wantIDs: map[domain.Position]string{"1": "m1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.lineup.FilledByMemberNumber(tt.size, tt.members)
			assert.Equal(t, orEmptyPos(tt.wantPos), orEmptyPos(got.Positions))
			assert.Equal(t, orEmptyPos(tt.wantIDs), orEmptyPos(got.MemberIDs))
		})
	}
}

func orEmptyPos(m map[domain.Position]string) map[domain.Position]string {
	if m == nil {
		return map[domain.Position]string{}
	}
	return m
}

// TestTeamLineup_FilledByMemberNumber_IsACopy pins that the fill never writes
// through to the lineup it was called on, and keeps its other fields.
func TestTeamLineup_FilledByMemberNumber_IsACopy(t *testing.T) {
	l := domain.TeamLineup{
		Positions: map[domain.Position]string{"1": "Member 3"},
		MemberIDs: map[domain.Position]string{"1": "m3"},
		Stamps:    map[domain.Position]int64{"1": 7},
	}
	got := l.FilledByMemberNumber(3, fillMembers(3, true))
	assert.Len(t, l.Positions, 1)
	assert.Len(t, l.MemberIDs, 1)
	assert.Len(t, got.Positions, 3)
	assert.Equal(t, map[domain.Position]int64{"1": 7}, got.Stamps)

	got.Stamps["1"] = 9
	assert.Equal(t, int64(7), l.Stamps["1"])

	// History and Own are cloned too: the contract is that nothing is shared.
	h := domain.TeamLineup{
		Positions: map[domain.Position]string{"1": "Member 3"},
		MemberIDs: map[domain.Position]string{"1": "m3"},
		History:   map[domain.Position][]domain.PositionChange{"1": {{Name: "Old", Stamp: 1}}},
		Own:       []domain.Position{"1"},
	}
	filled := h.FilledByMemberNumber(3, fillMembers(3, true))
	filled.History["1"][0].Name = "Changed"
	filled.History["2"] = nil
	filled.Own[0] = "2"
	assert.Equal(t, "Old", h.History["1"][0].Name)
	assert.Len(t, h.History, 1)
	assert.Equal(t, []domain.Position{"1"}, h.Own)

	var empty domain.TeamLineup
	_ = empty.FilledByMemberNumber(3, fillMembers(3, true))
	assert.Nil(t, empty.Positions)
	assert.Nil(t, empty.MemberIDs)
}
