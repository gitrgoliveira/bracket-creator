package mobileapp

// introducesJudgedMember is the gate in front of the landed member judge
// (landedMembersRefusal): the judge re-reads the staged match under the
// competition's write lock, so it is asked only for a write that INTRODUCES a
// member id on a judged row, against the match as it stood before the write.
// A team editor stamps member ids on every row that has a lineup, so an echo
// of the stored ids is the common case and must read nothing.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
)

func TestIntroducesJudgedMember(t *testing.T) {
	rep := func(a, b string) state.SubMatchResult {
		return state.SubMatchResult{Position: state.DaihyosenSubPosition, SideAMemberID: a, SideBMemberID: b}
	}
	bout := func(position int, a, b, winner string) state.SubMatchResult {
		return state.SubMatchResult{Position: position, SideAMemberID: a, SideBMemberID: b, WinnerMemberID: winner}
	}
	// stored builds the snapshot of a match as it stood before the write.
	stored := func(pairing domain.WinnerAttribution, repBout *state.SubMatchResult, rows ...state.SubMatchResult) matchSnapshot {
		return matchSnapshot{
			Pairing: pairing,
			RepBout: repBout,
			Stored:  &state.MatchResult{SubResults: rows},
		}
	}
	teams := domain.WinnerAttribution{SideAID: "team-a", SideBID: "team-b"}
	repRow := rep("ma", "mb")

	tests := []struct {
		name     string
		subs     []state.SubMatchResult
		snap     matchSnapshot
		numbered bool
		want     bool
	}{
		{
			name: "no rows",
			snap: stored(teams, &repRow, repRow),
			want: false,
		},
		{
			name: "an echo of the stored representative pick",
			subs: []state.SubMatchResult{rep("ma", "mb")},
			snap: stored(teams, &repRow, repRow),
			want: false,
		},
		{
			name: "a representative row that names no member",
			subs: []state.SubMatchResult{rep("", "")},
			snap: stored(teams, &repRow, repRow),
			want: false,
		},
		{
			name: "a representative pick on side A that the stored row does not hold",
			subs: []state.SubMatchResult{rep("other", "mb")},
			snap: stored(teams, &repRow, repRow),
			want: true,
		},
		{
			name: "a representative pick on side B that the stored row does not hold",
			subs: []state.SubMatchResult{rep("ma", "other")},
			snap: stored(teams, &repRow, repRow),
			want: true,
		},
		{
			name: "a representative pick on a match that has no representative bout",
			subs: []state.SubMatchResult{rep("ma", "")},
			snap: stored(teams, nil),
			want: true,
		},
		{
			name: "a representative winner id alone is no pick",
			subs: []state.SubMatchResult{{Position: state.DaihyosenSubPosition, WinnerMemberID: "ma"}},
			snap: stored(teams, nil),
			want: false,
		},
		{
			name:     "an echo of a numbered row's fighters and winner, a participant's write",
			subs:     []state.SubMatchResult{bout(1, "ma", "mb", "ma")},
			snap:     stored(teams, nil, bout(1, "ma", "mb", "ma")),
			numbered: true,
			want:     false,
		},
		{
			name:     "a numbered row's fighter the stored row does not hold, a participant's write",
			subs:     []state.SubMatchResult{bout(1, "ma", "other", "")},
			snap:     stored(teams, nil, bout(1, "ma", "mb", "")),
			numbered: true,
			want:     true,
		},
		{
			name:     "a numbered row's winner the stored row does not hold, a participant's write",
			subs:     []state.SubMatchResult{bout(1, "ma", "mb", "mb")},
			snap:     stored(teams, nil, bout(1, "ma", "mb", "ma")),
			numbered: true,
			want:     true,
		},
		{
			name:     "a numbered row at a position the stored match lacks, a participant's write",
			subs:     []state.SubMatchResult{bout(3, "ma", "", "")},
			snap:     stored(teams, nil, bout(1, "ma", "mb", "")),
			numbered: true,
			want:     true,
		},
		{
			name:     "a numbered row at a position the stored match lacks that names no member",
			subs:     []state.SubMatchResult{bout(3, "", "", "")},
			snap:     stored(teams, nil, bout(1, "ma", "mb", "")),
			numbered: true,
			want:     false,
		},
		{
			name:     "a numbered row's fighter over a stored side that holds none, on a match whose stored sides carry no team id",
			subs:     []state.SubMatchResult{bout(1, "ma", "", "")},
			snap:     stored(domain.WinnerAttribution{}, nil, bout(1, "", "", "")),
			numbered: true,
			want:     true,
		},
		{
			name: "a representative pick over a stored side that holds none, on a match whose stored sides carry no team id",
			subs: []state.SubMatchResult{rep("ma", "")},
			snap: stored(domain.WinnerAttribution{}, &state.SubMatchResult{Position: state.DaihyosenSubPosition}, state.SubMatchResult{Position: state.DaihyosenSubPosition}),
			want: true,
		},
		{
			name: "the organiser's numbered rows are not judged",
			subs: []state.SubMatchResult{bout(1, "other", "other", "other")},
			snap: stored(teams, nil, bout(1, "ma", "mb", "ma")),
			want: false,
		},
		{
			name:     "a write on a snapshot that holds no stored match introduces every id it carries",
			subs:     []state.SubMatchResult{bout(1, "ma", "", "")},
			snap:     matchSnapshot{},
			numbered: true,
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, introducesJudgedMember(tt.subs, tt.snap, tt.numbered))
		})
	}
}
