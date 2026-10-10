package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// Each side's representative of the representative bout is a change of its own
// (GroupRepPickA, GroupRepPickB), apart from the other side's and from the bout
// row they sit on (bout:-1): a pick added or cleared differs in its side's pick
// and not in the bout or the other side's, a point differs in the bout and not
// in the picks, and each copy moves only its own fields, in any order.
func TestRepPicks_AreAChangeApartFromTheBout(t *testing.T) {
	bout := BoutGroup(DaihyosenSubPosition)
	row := func(a, b string, ippons ...string) SubMatchResult {
		return SubMatchResult{Position: DaihyosenSubPosition, SideA: "TeamA", SideB: "TeamB", Decision: "daihyosen",
			SideAMemberID: a, SideBMemberID: b, IpponsA: ippons}
	}
	match := func(r SubMatchResult) *MatchResult { return &MatchResult{SubResults: []SubMatchResult{r}} }

	// Side B's pick is a change of side B's group and of nothing else.
	picked, cleared, scored := match(row("", "rep-b")), match(row("", "")), match(row("", "", "M"))
	assert.True(t, GroupDiffers(picked, cleared, GroupRepPickB), "a side B pick cleared is a change of side B's pick")
	assert.False(t, GroupDiffers(picked, cleared, GroupRepPickA), "and not of side A's")
	assert.False(t, GroupDiffers(picked, cleared, bout), "and not of the bout")
	assert.True(t, GroupDiffers(cleared, scored, bout), "a point is a change of the bout")
	assert.False(t, GroupDiffers(cleared, scored, GroupRepPickA), "and not of either pick")
	assert.False(t, GroupDiffers(cleared, scored, GroupRepPickB))
	assert.JSONEq(t, `{"sideBMemberId":"rep-b"}`, string(GroupValue(picked, GroupRepPickB)), "a side's value holds that side's id alone")
	assert.JSONEq(t, `{"sideAMemberId":""}`, string(GroupValue(picked, GroupRepPickA)))
	assert.JSONEq(t, `null`, string(GroupValue(&MatchResult{}, bout)), "a removed bout has no value")
	assert.JSONEq(t, `{"sideAMemberId":""}`, string(GroupValue(&MatchResult{}, GroupRepPickA)), "no bout, no picks")
	assert.JSONEq(t, `{"sideBMemberId":""}`, string(GroupValue(&MatchResult{}, GroupRepPickB)), "no bout, no picks")

	// And side A's pick of side A's group alone.
	pickedA := match(row("rep-a", ""))
	assert.True(t, GroupDiffers(pickedA, cleared, GroupRepPickA))
	assert.False(t, GroupDiffers(pickedA, cleared, GroupRepPickB))

	// Picks from one match, the bout from another: each copy takes only its own
	// fields, so the result is the same in any order.
	orders := [][]string{
		{GroupRepPickA, GroupRepPickB, bout},
		{bout, GroupRepPickA, GroupRepPickB},
		{GroupRepPickB, bout, GroupRepPickA},
	}
	for i, order := range orders {
		dst, stored := match(row("carol", "gus", "K")), match(row("dana", "hal", "M"))
		for _, g := range order {
			CopyGroup(dst, stored, g)
		}
		assert.Equal(t, []string{"M"}, dst.SubResults[0].IpponsA, "order %d", i)
		assert.Equal(t, "dana", dst.SubResults[0].SideAMemberID, "order %d", i)
		assert.Equal(t, "hal", dst.SubResults[0].SideBMemberID, "order %d", i)
	}
	// One side's copy moves only that side.
	dst := match(row("carol", "gus"))
	CopyGroup(dst, match(row("dana", "hal")), GroupRepPickB)
	assert.Equal(t, "carol", dst.SubResults[0].SideAMemberID, "side A was not copied")
	assert.Equal(t, "hal", dst.SubResults[0].SideBMemberID)
	// And a bout copy alone leaves the picks where they are.
	dst = match(row("carol", "gus", "K"))
	CopyGroup(dst, match(row("dana", "hal", "M")), bout)
	assert.Equal(t, "carol", dst.SubResults[0].SideAMemberID, "the bout's copy leaves the picks alone")
	assert.Equal(t, "gus", dst.SubResults[0].SideBMemberID)

	// A copy of the bout onto a match with no such row brings the row whole,
	// picks included; a copy of a pick onto no row lands nowhere.
	empty := &MatchResult{}
	CopyGroup(empty, picked, GroupRepPickA)
	CopyGroup(empty, picked, GroupRepPickB)
	assert.Empty(t, empty.SubResults, "a pick has no bout to land on")
	CopyGroup(empty, picked, bout)
	require.Len(t, empty.SubResults, 1)
	assert.Equal(t, "rep-b", empty.SubResults[0].SideBMemberID)
	// A removal removes the row, picks and all.
	CopyGroup(empty, &MatchResult{}, bout)
	assert.Empty(t, empty.SubResults)

	// RepPickGroup names a side's group, and no other value has one.
	assert.Equal(t, GroupRepPickA, RepPickGroup(domain.MatchSideA))
	assert.Equal(t, GroupRepPickB, RepPickGroup(domain.MatchSideB))
	assert.Empty(t, RepPickGroup(domain.MatchSideNone))
}

// A match written before the picks had a stamp of their own dates each side's
// pick with the bout row they sit on, and that date is fixed the first time the
// map is materialized, so a later stamp of the bout does not move it.
func TestRepPicks_LegacyStampFollowsTheBoutOnceThenStandsAlone(t *testing.T) {
	bout := BoutGroup(DaihyosenSubPosition)
	m := &MatchResult{ModifiedAt: 50, GroupStamps: map[string]int64{bout: 40, GroupPoints: 50}}
	for _, g := range []string{GroupRepPickA, GroupRepPickB} {
		assert.Equal(t, int64(40), m.GroupStamp(g), "%s: no entry yet: the bout row's date", g)
		assert.Equal(t, int64(0), (&MatchResult{GroupStamps: map[string]int64{}}).GroupStamp(g), "%s: never written", g)
	}

	m.StampGroups(90, bout)
	assert.Equal(t, int64(90), m.GroupStamp(bout))
	for _, g := range []string{GroupRepPickA, GroupRepPickB} {
		assert.Equal(t, int64(40), m.GroupStamp(g), "%s: the point did not date the picks", g)
	}

	// Stamping one side's pick leaves the other on the bout's old date.
	m.StampGroups(95, GroupRepPickA)
	assert.Equal(t, int64(95), m.GroupStamp(GroupRepPickA))
	assert.Equal(t, int64(40), m.GroupStamp(GroupRepPickB), "the other side is not dated by it")

	legacy := &MatchResult{ModifiedAt: 30, SubResults: []SubMatchResult{{Position: DaihyosenSubPosition}}}
	for _, g := range []string{GroupRepPickA, GroupRepPickB} {
		assert.Equal(t, int64(30), legacy.GroupStamp(g), "%s: a match with no map reads ModifiedAt", g)
	}
	legacy.StampGroups(60, bout)
	for _, g := range []string{GroupRepPickA, GroupRepPickB} {
		assert.Equal(t, int64(30), legacy.GroupStamp(g), "%s: materialized with every other group", g)
	}
}

// A stamp map from this branch's earlier commits dated both picks as one group,
// "repPicks". It reads as BOTH sides' stamp (over the bout row's, which it
// outranks as the picks' own date), and the first materialization converts it to
// a key per side and drops it, so it cannot linger in the newest-stamp scans.
func TestRepPicks_DevStampOfBothPicksIsConvertedPerSide(t *testing.T) {
	bout := BoutGroup(DaihyosenSubPosition)
	m := &MatchResult{ModifiedAt: 90, GroupStamps: map[string]int64{"repPicks": 40, bout: 90}}
	assert.Equal(t, int64(40), m.GroupStamp(GroupRepPickA), "both sides read the legacy stamp")
	assert.Equal(t, int64(40), m.GroupStamp(GroupRepPickB))

	m.StampGroups(100, GroupRepPickA)
	assert.Equal(t, int64(100), m.GroupStamps[GroupRepPickA])
	assert.Equal(t, int64(40), m.GroupStamps[GroupRepPickB], "side B keeps the legacy date")
	assert.NotContains(t, m.GroupStamps, "repPicks", "converted, not left to linger")
	assert.Equal(t, int64(90), m.GroupStamps[bout])

	// A side with a key of its own is not overridden by a legacy key beside it.
	both := &MatchResult{GroupStamps: map[string]int64{"repPicks": 40, GroupRepPickB: 70}}
	assert.Equal(t, int64(40), both.GroupStamp(GroupRepPickA))
	assert.Equal(t, int64(70), both.GroupStamp(GroupRepPickB))
	assert.NotContains(t, MaterializedGroupStamps(both.GroupStamps, 0, nil), "repPicks")
}

// A legacy (map-less) match is given the pick entries when it materializes only
// if it holds a representative bout row: the picks live on that row, so a match
// without one has nothing to date and gains no key it never held.
func TestMaterializedGroupStamps_RepPicksOnlyWithARepresentativeRow(t *testing.T) {
	t.Run("a match without the row gets every other scalar group, not the pick groups", func(t *testing.T) {
		got := MaterializedGroupStamps(nil, 70, []int{1, 2})
		assert.NotContains(t, got, GroupRepPickA)
		assert.NotContains(t, got, GroupRepPickB)
		for _, g := range ScalarGroups {
			if g != GroupRepPickA && g != GroupRepPickB {
				assert.Equal(t, int64(70), got[g], g)
			}
		}
		assert.Equal(t, int64(70), got[BoutGroup(1)])
		assert.Equal(t, int64(70), got[BoutGroup(2)])
	})
	t.Run("a match with the row gets both pick groups at its ModifiedAt", func(t *testing.T) {
		got := MaterializedGroupStamps(nil, 70, []int{1, DaihyosenSubPosition})
		assert.Equal(t, int64(70), got[GroupRepPickA])
		assert.Equal(t, int64(70), got[GroupRepPickB])
		assert.Equal(t, int64(70), got[BoutGroup(DaihyosenSubPosition)])
	})
	t.Run("a stamp a write named is kept on a match with no row", func(t *testing.T) {
		m := &MatchResult{ModifiedAt: 70}
		m.StampGroups(90, GroupRepPickA)
		assert.Equal(t, int64(90), m.GroupStamps[GroupRepPickA], "naming the group stamps it, row or not")
		assert.Equal(t, int64(90), m.GroupStamp(GroupRepPickA))
		assert.NotContains(t, m.GroupStamps, GroupRepPickB, "and only that side")
	})
}
