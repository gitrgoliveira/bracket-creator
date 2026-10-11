package state

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSubMatchResultRecordedRoundTrips pins the kachinuki recorded mark
// (bc-kfnl) on both stores: pool-matches.csv's SubResults column and
// bracket.json each keep it, and a row that never carried it reads as not
// recorded (old data needs no migration).
func TestSubMatchResultRecordedRoundTrips(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	const compID = "recorded-roundtrip"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	subs := []SubMatchResult{
		{Position: 1, SideA: "A1", SideB: "B1", Winner: "A1", Decision: "fought", Recorded: true},
		{Position: 2, SideA: "A1", SideB: "B2"},
	}
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideB: "Tora", Status: MatchStatusRunning, SubResults: CloneSubResults(subs),
	}}))
	pms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, pms, 1)
	require.Len(t, pms[0].SubResults, 2)
	assert.True(t, pms[0].SubResults[0].Recorded)
	assert.False(t, pms[0].SubResults[1].Recorded)

	require.NoError(t, store.SaveBracket(compID, &Bracket{Rounds: [][]BracketMatch{{{
		ID: "r0-m0", SideA: "Ryu", SideB: "Tora", Status: MatchStatusRunning, SubResults: CloneSubResults(subs),
	}}}}))
	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bm := br.MatchByID("r0-m0")
	require.NotNil(t, bm)
	require.Len(t, bm.SubResults, 2)
	assert.True(t, bm.SubResults[0].Recorded)
	assert.False(t, bm.SubResults[1].Recorded)

	// The wire: present when set, absent otherwise.
	b, err := json.Marshal(subs)
	require.NoError(t, err)
	var raw []map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	assert.Equal(t, true, raw[0]["recorded"])
	_, has := raw[1]["recorded"]
	assert.False(t, has, "omitempty: an unrecorded row carries no key")

}

// TestRecordedGroup pins the recorded mark as a group of its own (review round
// 6): a bout's group never holds it, GroupRecorded holds exactly the numbered
// rows' marks, and each copy moves only its own half in either order.
func TestRecordedGroup(t *testing.T) {
	subs := func() []SubMatchResult {
		return []SubMatchResult{
			{Position: 1, SideA: "A1", SideB: "B1", Winner: "A1", Recorded: true},
			{Position: 2, SideA: "A1", SideB: "B2", IpponsA: []string{"M"}},
			{Position: DaihyosenSubPosition, SideA: "Ryu", SideB: "Tora", Recorded: true},
		}
	}
	a := &MatchResult{SubResults: subs()}
	c := &MatchResult{SubResults: subs()}
	c.SubResults[0].Recorded = false

	// Projection: only GroupRecorded sees the mark; the -1 row is no kachinuki
	// bout and never counts.
	assert.False(t, GroupDiffers(a, c, BoutGroup(1)), "the bout's group does not hold the mark")
	assert.True(t, GroupDiffers(a, c, GroupRecorded))
	assert.Equal(t, []int{1}, RecordedPositions(a.SubResults))
	assert.JSONEq(t, `{"positions":[1]}`, string(GroupValue(a, GroupRecorded)))
	assert.JSONEq(t, `{"positions":[]}`, string(GroupValue(c, GroupRecorded)))

	// A server group: valid in the history, never a client's.
	assert.True(t, ValidGroup(GroupRecorded))
	assert.True(t, IsServerGroup(GroupRecorded))
	assert.False(t, IsServerGroup(GroupPoints))
	assert.NotContains(t, ScalarGroups, GroupRecorded)

	// Copying a bout keeps the destination's mark; copying the group moves the
	// marks and nothing else.
	dst := &MatchResult{SubResults: subs()}
	dst.SubResults[0].Recorded = false
	src := &MatchResult{SubResults: subs()}
	src.SubResults[0].Winner = "B1"
	CopyGroup(dst, src, BoutGroup(1))
	assert.Equal(t, "B1", dst.SubResults[0].Winner)
	assert.False(t, dst.SubResults[0].Recorded, "a bout copy keeps the destination's mark")
	CopyGroup(dst, src, GroupRecorded)
	assert.True(t, dst.SubResults[0].Recorded)
	assert.Equal(t, []string{"M"}, dst.SubResults[1].IpponsA, "a recorded copy moves no score")

	// Either order gives the same rows.
	one := &MatchResult{SubResults: subs()[1:2]}
	two := &MatchResult{SubResults: subs()[1:2]}
	CopyGroup(one, a, GroupRecorded)
	CopyGroup(one, a, BoutGroup(1))
	CopyGroup(two, a, BoutGroup(1))
	CopyGroup(two, a, GroupRecorded)
	assert.Equal(t, one.SubResults, two.SubResults)
	assert.Equal(t, []int{1}, RecordedPositions(one.SubResults))
}
