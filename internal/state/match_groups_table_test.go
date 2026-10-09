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

// TestApplyMergedGroupStamps_CanLowerAStampAndRecomputesModifiedAt pins the
// contract ApplyMergedGroupStamps gives the engi recorder (bc-mrgc Fix 1),
// in deliberate contrast with StampGroups' never-lower rule above: it
// applies exactly the stamp mergeMatchWrite already decided for a group,
// even when that is OLDER than what the match currently records (S2's
// displaceNewerScoring moves a stored, invalid count's stamp out and
// replaces it with a finish's own, earlier one), and ModifiedAt is
// recomputed as the newest stamp actually left on the match -- which can
// itself move backward when the group that had been keeping it high is
// the one just lowered.
func TestApplyMergedGroupStamps_CanLowerAStampAndRecomputesModifiedAt(t *testing.T) {
	m := &MatchResult{ID: "m", Status: MatchStatusRunning, ModifiedAt: 100}
	m.StampGroups(400, GroupFlags) // as if a recount had been accepted at 400
	require.Equal(t, int64(400), m.GroupStamp(GroupFlags), "precondition")
	require.Equal(t, int64(400), m.ModifiedAt, "precondition")

	m.ApplyMergedGroupStamps(map[string]int64{GroupResult: 200, GroupFlags: 200}, GroupResult, GroupFlags)
	assert.Equal(t, int64(200), m.GroupStamp(GroupFlags), "the merge's own decision may lower a group's stamp")
	assert.Equal(t, int64(200), m.GroupStamp(GroupResult))
	assert.Equal(t, int64(200), m.ModifiedAt, "recomputed as the newest stamp left on the match, lower than before")
	assert.Equal(t, int64(100), m.GroupStamp(GroupPoints), "a group not named in decided keeps its materialized stamp")
}

// TestMergeReport_HeldDecisionRequiresTheDefaultWinStandsReason pins
// bc-mrgc Fix 4: HeldDecision must agree with HeldReason, which also
// requires something actually held (len(Held)+len(Displaced) > 0). Before
// the fix, HeldDecision checked DefaultWinStands alone, so a report that
// set it without holding anything answered heldDecision with no
// heldReason, contradicting openapi.
func TestMergeReport_HeldDecisionRequiresTheDefaultWinStandsReason(t *testing.T) {
	// DefaultWinStands set, but nothing held: HeldReason reads "" (nothing
	// to explain), so HeldDecision must read "" too.
	rep := &MergeReport{DefaultWinStands: true, StandingDecision: "fusensho"}
	assert.Empty(t, rep.HeldReason(), "precondition: nothing was held")
	assert.Empty(t, rep.HeldDecision(), "no reason means no decision either")

	// DefaultWinStands set AND something genuinely held: both read through.
	rep2 := &MergeReport{DefaultWinStands: true, StandingDecision: "kiken-voluntary", Held: []string{GroupPoints}}
	assert.Equal(t, HeldReasonDefaultWinStands, rep2.HeldReason())
	assert.Equal(t, "kiken-voluntary", rep2.HeldDecision())

	// NeedsWinner takes priority over DefaultWinStands in HeldReason, and a
	// report in that shape never names a decision.
	rep3 := &MergeReport{NeedsWinner: true, DefaultWinStands: true, StandingDecision: "fusensho", Held: []string{GroupPoints}}
	assert.Equal(t, HeldReasonNeedsWinner, rep3.HeldReason())
	assert.Empty(t, rep3.HeldDecision(), "needs_winner, not default_win_stands")

	assert.Empty(t, (*MergeReport)(nil).HeldDecision(), "a nil report names no decision")
}

// The two representatives of the representative bout are a change of their own
// (GroupRepPicks), apart from the bout row they sit on (bout:-1): a pick added
// or cleared differs in the picks and not in the bout, a point differs in the
// bout and not in the picks, and each copy moves only its own fields, in
// either order.
func TestRepPicks_AreAChangeApartFromTheBout(t *testing.T) {
	bout := BoutGroup(DaihyosenSubPosition)
	row := func(a, b string, ippons ...string) SubMatchResult {
		return SubMatchResult{Position: DaihyosenSubPosition, SideA: "TeamA", SideB: "TeamB", Decision: "daihyosen",
			SideAMemberID: a, SideBMemberID: b, IpponsA: ippons}
	}
	match := func(r SubMatchResult) *MatchResult { return &MatchResult{SubResults: []SubMatchResult{r}} }

	picked, cleared, scored := match(row("", "rep-b")), match(row("", "")), match(row("", "", "M"))
	assert.True(t, GroupDiffers(picked, cleared, GroupRepPicks), "a pick cleared is a change of the picks")
	assert.False(t, GroupDiffers(picked, cleared, bout), "and not of the bout")
	assert.True(t, GroupDiffers(cleared, scored, bout), "a point is a change of the bout")
	assert.False(t, GroupDiffers(cleared, scored, GroupRepPicks), "and not of the picks")
	assert.JSONEq(t, `{"sideAMemberId":"","sideBMemberId":"rep-b"}`, string(GroupValue(picked, GroupRepPicks)))
	assert.JSONEq(t, `null`, string(GroupValue(&MatchResult{}, bout)), "a removed bout has no value")
	assert.JSONEq(t, `{"sideAMemberId":"","sideBMemberId":""}`, string(GroupValue(&MatchResult{}, GroupRepPicks)), "no bout, no picks")

	// Picks from one match, the bout from another: each copy takes only its own
	// fields, so the result is the same in either order.
	for _, picksFirst := range []bool{true, false} {
		dst, stored := match(row("carol", "", "K")), match(row("dana", "", "M"))
		if picksFirst {
			CopyGroup(dst, stored, GroupRepPicks)
			CopyGroup(dst, stored, bout)
		} else {
			CopyGroup(dst, stored, bout)
			CopyGroup(dst, stored, GroupRepPicks)
		}
		assert.Equal(t, []string{"M"}, dst.SubResults[0].IpponsA, "picksFirst=%v", picksFirst)
		assert.Equal(t, "dana", dst.SubResults[0].SideAMemberID, "picksFirst=%v", picksFirst)
	}
	// And a bout copy alone leaves the picks where they are.
	dst := match(row("carol", "", "K"))
	CopyGroup(dst, match(row("dana", "", "M")), bout)
	assert.Equal(t, "carol", dst.SubResults[0].SideAMemberID, "the bout's copy leaves the picks alone")

	// A copy of the bout onto a match with no such row brings the row whole,
	// picks included; a copy of the picks onto no row lands nowhere.
	empty := &MatchResult{}
	CopyGroup(empty, picked, GroupRepPicks)
	assert.Empty(t, empty.SubResults, "a pick has no bout to land on")
	CopyGroup(empty, picked, bout)
	require.Len(t, empty.SubResults, 1)
	assert.Equal(t, "rep-b", empty.SubResults[0].SideBMemberID)
	// A removal removes the row, picks and all.
	CopyGroup(empty, &MatchResult{}, bout)
	assert.Empty(t, empty.SubResults)
}

// A match written before the picks had a stamp of their own dates them with
// the bout row they sit on, and that date is fixed the first time the map is
// materialized, so a later stamp of the bout does not move it.
func TestRepPicks_LegacyStampFollowsTheBoutOnceThenStandsAlone(t *testing.T) {
	bout := BoutGroup(DaihyosenSubPosition)
	m := &MatchResult{ModifiedAt: 50, GroupStamps: map[string]int64{bout: 40, GroupPoints: 50}}
	assert.Equal(t, int64(40), m.GroupStamp(GroupRepPicks), "no entry yet: the bout row's date")
	assert.Equal(t, int64(0), (&MatchResult{GroupStamps: map[string]int64{}}).GroupStamp(GroupRepPicks), "never written")

	m.StampGroups(90, bout)
	assert.Equal(t, int64(90), m.GroupStamp(bout))
	assert.Equal(t, int64(40), m.GroupStamp(GroupRepPicks), "the point did not date the picks")

	legacy := &MatchResult{ModifiedAt: 30, SubResults: []SubMatchResult{{Position: DaihyosenSubPosition}}}
	assert.Equal(t, int64(30), legacy.GroupStamp(GroupRepPicks), "a match with no map reads ModifiedAt")
	legacy.StampGroups(60, bout)
	assert.Equal(t, int64(30), legacy.GroupStamp(GroupRepPicks), "materialized with every other group")
}
