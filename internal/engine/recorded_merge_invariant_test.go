package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestMerge_ADisplacedRowNeverBringsTheWritesRecordedMark pins that a client
// can never set a kachinuki bout's recorded mark, on the one merge path that
// copies a row from the write's OWN payload into the merged match: S2's
// displacement (mergeHold.displaceNewerScoring copies the finish's scoreline
// over newer stored scoring that would leave a knockout tied). Here the
// stored match removed bout 2 after the finish was made (a tombstone newer
// than it), so the merged match lacks the row and the displacement brings the
// finish's own row back in, the row the client sent with recorded: true.
func TestMerge_ADisplacedRowNeverBringsTheWritesRecordedMark(t *testing.T) {
	comp := &state.Competition{Kind: "team", TeamSize: 2}
	stored := &state.MatchResult{
		ID: "r1-m1", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "A1", SideB: "B1", Decision: "hikiwake"},
		},
		ModifiedAt: 300,
		GroupStamps: map[string]int64{
			state.GroupPoints: 100, state.GroupResult: 100,
			state.BoutGroup(1): 100, state.BoutGroup(2): 300,
		},
	}
	finish := &state.MatchResult{
		ID: "r1-m1", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusCompleted, Winner: "Ryu",
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "A1", SideB: "B1", Decision: "hikiwake"},
			{Position: 2, SideA: "A1", SideB: "B2", Winner: "A1", IpponsA: []string{"M"}, Recorded: true},
		},
		ModifiedAt: 200,
		Changed:    []string{state.GroupResult, state.BoutGroup(1), state.BoutGroup(2)},
		WriteDoor:  DoorScore,
	}

	rep := mergeMatchWrite(stored, finish, matchWriteForward, mergeCtx{comp: comp, knockout: true})
	require.NotNil(t, rep)
	require.NotEmpty(t, rep.Displaced, "the newer removal of bout 2 is moved to the history (S2's displacement)")
	row := state.SubResultAt(finish.SubResults, 2)
	require.Equal(t, 2, row.Position, "the finish's own bout 2 is back in the match")
	assert.Equal(t, "Ryu", finish.Winner)
	assert.False(t, row.Recorded, "the mark the client sent never lands")
	assert.Empty(t, state.RecordedPositions(finish.SubResults))
}

// TestMerge_TheStoredRecordedMarksAlwaysLand pins the other half: a row the
// stored match marked keeps its mark whichever copy of it the merge kept, and
// a row the write marked does not gain one.
func TestMerge_TheStoredRecordedMarksAlwaysLand(t *testing.T) {
	comp := &state.Competition{Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki}
	stored := &state.MatchResult{
		ID: "Pool A-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "A1", SideB: "B1", Winner: "A1", IpponsA: []string{"M", "K"}, Recorded: true},
			{Position: 2, SideA: "A1", SideB: "B2"},
		},
		ModifiedAt: 100,
	}
	write := &state.MatchResult{
		ID: "Pool A-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "A1", SideB: "B1", Winner: "A1", IpponsA: []string{"M", "K"}},
			{Position: 2, SideA: "A1", SideB: "B2", IpponsB: []string{"D"}, Recorded: true},
		},
		ModifiedAt: 200,
		Changed:    []string{state.BoutGroup(2)},
		WriteDoor:  DoorScore,
	}
	mergeMatchWrite(stored, write, matchWriteForward, mergeCtx{comp: comp, nilSubsClear: true})
	assert.Equal(t, []int{1}, state.RecordedPositions(write.SubResults))
	assert.Equal(t, []string{"D"}, state.SubResultAt(write.SubResults, 2).IpponsB, "the point itself applies")
}
