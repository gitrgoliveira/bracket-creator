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

	// A bout group holds the mark: a change of it is a change of that bout.
	a := &MatchResult{SubResults: CloneSubResults(subs)}
	c := &MatchResult{SubResults: CloneSubResults(subs)}
	c.SubResults[0].Recorded = false
	assert.True(t, GroupDiffers(a, c, BoutGroup(1)))
	assert.False(t, GroupDiffers(a, c, BoutGroup(2)))
}
