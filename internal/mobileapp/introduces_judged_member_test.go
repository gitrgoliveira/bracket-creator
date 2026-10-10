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
	"github.com/stretchr/testify/require"
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

// TestJudgedRows pins the ONE selection the gate (introducesJudgedMember, on the
// payload's rows) and the judge (membersOutsideTeams, on the landed rows) both
// read: which rows are judged, and against which stored row. Each used to walk
// the rows and choose for itself, tied only by a comment, so a row or a field
// added to one was silently missing from the other.
func TestJudgedRows(t *testing.T) {
	t.Parallel()

	rep := func(a, b string) state.SubMatchResult {
		return state.SubMatchResult{Position: state.DaihyosenSubPosition, SideAMemberID: a, SideBMemberID: b}
	}
	bout := func(position int, a, b string) state.SubMatchResult {
		return state.SubMatchResult{Position: position, SideAMemberID: a, SideBMemberID: b}
	}
	storedRep := rep("sa", "sb")
	before := matchSnapshot{
		RepBout: &storedRep,
		Stored: &state.MatchResult{SubResults: []state.SubMatchResult{
			bout(1, "m1a", "m1b"), bout(2, "m2a", "m2b"), storedRep,
		}},
	}

	t.Run("the representative bout is judged for anyone, against the stored representative row", func(t *testing.T) {
		got := judgedRows([]state.SubMatchResult{bout(1, "x", "y"), rep("p", "q")}, before, false)
		require.Len(t, got, 1, "a numbered row is left out unless numbered")
		assert.True(t, got[0].rep)
		assert.Equal(t, rep("p", "q"), got[0].row)
		assert.Equal(t, storedRep, got[0].stored)
	})

	t.Run("a match with no stored representative row compares the pick with the empty row", func(t *testing.T) {
		got := judgedRows([]state.SubMatchResult{rep("p", "")}, matchSnapshot{}, false)
		require.Len(t, got, 1)
		assert.True(t, got[0].rep)
		assert.Equal(t, state.SubMatchResult{}, got[0].stored)
	})

	t.Run("numbered rows are judged for a participant, each against the stored row at its position", func(t *testing.T) {
		got := judgedRows([]state.SubMatchResult{bout(2, "x", "y"), rep("p", "q"), bout(9, "z", "")}, before, true)
		require.Len(t, got, 3, "rows come in the order they were given")
		assert.False(t, got[0].rep)
		assert.Equal(t, bout(2, "m2a", "m2b"), got[0].stored)
		assert.True(t, got[1].rep)
		assert.Equal(t, storedRep, got[1].stored)
		assert.False(t, got[2].rep)
		assert.Equal(t, state.SubMatchResult{}, got[2].stored, "a position the stored match lacks holds no id")
	})

	t.Run("a snapshot with no stored match judges every numbered row against the empty row", func(t *testing.T) {
		got := judgedRows([]state.SubMatchResult{bout(1, "x", "")}, matchSnapshot{}, true)
		require.Len(t, got, 1)
		assert.Equal(t, state.SubMatchResult{}, got[0].stored)
	})
}
