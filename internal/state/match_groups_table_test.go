package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMatchGroups_SharedTable is the Go half of the shared Go/JS table of
// match write groups (bc-mrgc): a client names the groups its write changes
// by these exact strings, and the score handler refuses one it does not know
// (changedGroupsError), so the client's list (web-mobile/js/match_groups.jsx,
// pinned by match_groups_table.test.jsx) and this one must never drift.
func TestMatchGroups_SharedTable(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "match_groups.json"))
	require.NoError(t, err, "shared Go/JS group table is missing")

	var table struct {
		ScalarGroups []string `json:"scalarGroups"`
		Bouts        []struct {
			Position int    `json:"position"`
			Group    string `json:"group"`
		} `json:"bouts"`
		Invalid []string `json:"invalid"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.ScalarGroups, "table parsed to no scalar groups: it would assert nothing")
	require.NotEmpty(t, table.Bouts, "table parsed to no bout groups: it would assert nothing")
	require.NotEmpty(t, table.Invalid, "table parsed to no invalid names: it would assert nothing")

	assert.Equal(t, table.ScalarGroups, ScalarGroups, "the scalar groups and their order")
	for _, g := range table.ScalarGroups {
		assert.True(t, ValidGroup(g), "%q is a group", g)
	}
	for _, b := range table.Bouts {
		assert.Equal(t, b.Group, BoutGroup(b.Position))
		pos, ok := ParseBoutGroup(b.Group)
		assert.True(t, ok, "%q names a bout", b.Group)
		assert.Equal(t, b.Position, pos)
		assert.True(t, ValidGroup(b.Group), "%q is a group", b.Group)
	}
	for _, g := range table.Invalid {
		assert.False(t, ValidGroup(g), "%q is not a group", g)
	}
}

// GH-T4 (owner review): the fields that belong to ONE write never travel on
// a stored copy the store hands out. The cache is filled from the slice the
// writer saved, so a write's ClearsWithdrawal (or its groups, door or merge
// report) used to come back on every later load, and a writer building its
// write from a loaded match (`u := *match`) inherited them.
func TestStoredCopiesCarryNoRequestFields(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "c"}))
	require.NoError(t, s.SavePoolMatches("c", []MatchResult{{
		ID: "Pool A-0", SideA: "A", SideB: "B", Status: MatchStatusCompleted, Winner: "A",
		ClearsWithdrawal: true, Changed: []string{GroupPoints}, WriteDoor: "score", Merge: &MergeReport{Stamp: 1},
	}}))
	for _, load := range []string{"first load", "second load"} {
		ms, err := s.LoadPoolMatches("c")
		require.NoError(t, err, load)
		require.Len(t, ms, 1)
		assert.False(t, ms[0].ClearsWithdrawal, load)
		assert.Nil(t, ms[0].Changed, load)
		assert.Empty(t, ms[0].WriteDoor, load)
		assert.Nil(t, ms[0].Merge, load)
	}
}

// TestStampGroups_NeverLowersAGroupsStamp pins stampGroups' own invariant
// directly (bc-mrgc Finding 2): a caller that already holds a LATER stamp
// for a group (an engi finish let through as a HeldEcho under an older
// stamp) must never drag that group's stamp backwards, or a change that
// genuinely arrived between the two would wrongly read as applying after a
// group it never actually followed. ModifiedAt already never lowered;
// stampGroups must give each individual group the same guarantee.
func TestStampGroups_NeverLowersAGroupsStamp(t *testing.T) {
	m := &MatchResult{ID: "m", Status: MatchStatusRunning, ModifiedAt: 100}
	m.StampGroups(300, GroupFlags)
	require.Equal(t, int64(300), m.GroupStamp(GroupFlags), "precondition: the group is stamped forward")

	m.StampGroups(150, GroupFlags)
	assert.Equal(t, int64(300), m.GroupStamp(GroupFlags), "an older stamp never lowers the group's recorded stamp")

	m.StampGroups(400, GroupFlags)
	assert.Equal(t, int64(400), m.GroupStamp(GroupFlags), "a genuinely newer stamp still moves it forward")

	// A group stampGroups has never touched starts materialized from
	// ModifiedAt (MaterializedGroupStamps): an older stamp than THAT must
	// not lower it either.
	m.StampGroups(50, GroupResult)
	assert.Equal(t, int64(100), m.GroupStamp(GroupResult), "an older stamp than the match's own start never lowers it")
}
