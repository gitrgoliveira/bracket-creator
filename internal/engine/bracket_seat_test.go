package engine

// seatBracketSide's contract, and every writer that routes through it:
// propagateBracketWinner (next match and 3rd-place match), clearPropagatedSlots
// (reached by a reopen) and resolveSlots' qualifier paint (reached by a pool
// requalification). The doors that carry a score correction or an override are
// in bracket_reseat_rep_picks_test.go.

import (
	"slices"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seatFixture is a match whose rep bout holds a pick on each side, dated 400,
// while the match itself was last written at 500.
func seatFixture() *state.BracketMatch {
	return &state.BracketMatch{
		ID:    "m",
		SideA: "Ryu", SideAID: "a", SideB: "Kuma", SideBID: "c",
		ModifiedAt: 500,
		GroupStamps: map[string]int64{
			state.GroupRepPickA:                         400,
			state.GroupRepPickB:                         400,
			state.BoutGroup(state.DaihyosenSubPosition): 400,
			state.GroupPoints:                           500,
		},
		SubResults: []state.SubMatchResult{{
			Position: state.DaihyosenSubPosition, SideA: "Ryu", SideB: "Kuma",
			SideAMemberID: "pa", SideBMemberID: "pc",
			Winner: "Ryu", WinnerMemberID: "pa",
		}},
	}
}

func TestSeatBracketSide(t *testing.T) {
	t.Run("the same team under another name keeps the pick and every stamp", func(t *testing.T) {
		bm := seatFixture()
		seatBracketSide(bm, domain.MatchSideA, "Ryu II", "a")
		assert.Equal(t, "Ryu II", bm.SideA)
		assert.Equal(t, "a", bm.SideAID)
		a, c := bm.RepPicks()
		assert.Equal(t, "pa", a)
		assert.Equal(t, "pc", c)
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickA))
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickB))
		assert.Equal(t, int64(500), bm.ModifiedAt)
	})

	t.Run("another team on side A clears that pick only, and the winner id that rode on it", func(t *testing.T) {
		bm := seatFixture()
		seatBracketSide(bm, domain.MatchSideA, "Tora", "b")
		assert.Equal(t, "Tora", bm.SideA)
		assert.Equal(t, "b", bm.SideAID)
		a, c := bm.RepPicks()
		assert.Empty(t, a)
		assert.Equal(t, "pc", c, "the other side was not re-seated")
		assert.Empty(t, bm.SubResults[0].WinnerMemberID, "the winner id is derived from the picks that stand")
		stamp := bm.GroupStamp(state.GroupRepPickA)
		assert.Greater(t, stamp, int64(400))
		assert.GreaterOrEqual(t, stamp, int64(500))
		assert.Equal(t, stamp, bm.ModifiedAt, "ModifiedAt is the newest group stamp")
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickB), "the other side's pick keeps its date")
		assert.Equal(t, int64(400), bm.GroupStamp(state.BoutGroup(state.DaihyosenSubPosition)), "the bout row is not what changed")
	})

	t.Run("another team on side B clears that pick only", func(t *testing.T) {
		bm := seatFixture()
		seatBracketSide(bm, domain.MatchSideB, "Tora", "b")
		a, c := bm.RepPicks()
		assert.Equal(t, "pa", a)
		assert.Empty(t, c)
		assert.Equal(t, "pa", bm.SubResults[0].WinnerMemberID, "the winner's pick stands")
		assert.Greater(t, bm.GroupStamp(state.GroupRepPickB), int64(400))
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickA), "the other side's pick keeps its date")
	})

	t.Run("a slot going back to a placeholder clears the pick", func(t *testing.T) {
		bm := seatFixture()
		seatBracketSide(bm, domain.MatchSideA, "Winner of r1-m0", "")
		assert.Equal(t, "Winner of r1-m0", bm.SideA)
		assert.Empty(t, bm.SideAID)
		a, _ := bm.RepPicks()
		assert.Empty(t, a)
	})

	t.Run("the cleared pick is dated above a stamp ahead of the server clock", func(t *testing.T) {
		bm := seatFixture()
		ahead := serverNowMs() + 3_600_000
		bm.GroupStamps[state.GroupRepPickA] = ahead
		bm.ModifiedAt = ahead
		seatBracketSide(bm, domain.MatchSideA, "Tora", "b")
		assert.Equal(t, ahead+1, bm.GroupStamp(state.GroupRepPickA))
	})

	t.Run("a placeholder resolved to a team has no pick to lose and changes nothing else", func(t *testing.T) {
		bm := seatFixture()
		bm.SideA, bm.SideAID = "Winner of r1-m0", ""
		bm.SubResults[0].SideAMemberID, bm.SubResults[0].WinnerMemberID = "", ""
		seatBracketSide(bm, domain.MatchSideA, "Ryu", "a")
		assert.Equal(t, "Ryu", bm.SideA)
		assert.Equal(t, "a", bm.SideAID)
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickA))
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickB))
		assert.Equal(t, int64(500), bm.ModifiedAt)
	})

	t.Run("a side with no pick leaves the other side's pick and every stamp", func(t *testing.T) {
		bm := seatFixture()
		bm.SubResults[0].SideAMemberID = ""
		seatBracketSide(bm, domain.MatchSideA, "Tora", "b")
		_, c := bm.RepPicks()
		assert.Equal(t, "pc", c)
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickA))
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickB))
		assert.Equal(t, int64(500), bm.ModifiedAt)
	})

	t.Run("a match with no rep bout row gains no stamp", func(t *testing.T) {
		bm := seatFixture()
		bm.SubResults = nil
		bm.GroupStamps = nil
		seatBracketSide(bm, domain.MatchSideA, "Tora", "b")
		assert.Equal(t, "b", bm.SideAID)
		assert.Nil(t, bm.GroupStamps)
		assert.Equal(t, int64(500), bm.ModifiedAt)
	})

	t.Run("the row another copy of the match shares is not written through", func(t *testing.T) {
		bm := seatFixture()
		shared := bm.SubResults
		seatBracketSide(bm, domain.MatchSideA, "Tora", "b")
		assert.Equal(t, "pa", shared[0].SideAMemberID)
		assert.Equal(t, "pa", shared[0].WinnerMemberID)
	})
}

// A semifinal re-scored: the final's and the 3rd-place match's sides both get
// another team, and each loses the pick it held for the team that left it.
func TestPropagateBracketWinner_ReseatsTheNextAndTheBronzeSidesAndTheirPicks(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	row := func(a, b string) []state.SubMatchResult {
		return []state.SubMatchResult{{
			Position: state.DaihyosenSubPosition, SideAMemberID: a, SideBMemberID: b,
		}}
	}
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "sf0", SideA: "Ryu", SideAID: "a", SideB: "Tora", SideBID: "b",
					Status: state.MatchStatusCompleted, Winner: "Ryu", WinnerID: "a"},
				{ID: "sf1", SideA: "Kuma", SideAID: "c", SideB: "Hawk", SideBID: "d",
					Status: state.MatchStatusCompleted, Winner: "Kuma", WinnerID: "c"},
			},
			{
				{ID: "final", SideA: "Tora", SideAID: "b", SideB: "Kuma", SideBID: "c",
					Status: state.MatchStatusScheduled, SubResults: row("pb", "pk")},
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: "bronze", SideA: "Ryu", SideAID: "a", SideB: "Hawk", SideBID: "d",
			Status: state.MatchStatusScheduled, SubResults: row("pa", "ph"),
		},
	}

	eng.propagateBracketWinner(bracket, 0, 0)

	final := bracket.Rounds[1][0]
	assert.Equal(t, "Ryu", final.SideA)
	fa, fb := final.RepPicks()
	assert.Empty(t, fa, "Tora left the final's side A")
	assert.Equal(t, "pk", fb, "Kuma did not")
	bronze := bracket.ThirdPlaceMatch
	assert.Equal(t, "Tora", bronze.SideA)
	ba, bb := bronze.RepPicks()
	assert.Empty(t, ba, "Ryu left the bronze match's side A")
	assert.Equal(t, "ph", bb, "Hawk did not")
}

// A reopen retracts the winner it had sent on: the slot goes back to its
// "Winner of" placeholder, and the pick the advanced team's member held goes
// with it.
func TestReopenMatch_RetractionTakesThePickOfTheClearedSlot(t *testing.T) {
	// A reopen is for a match a withdrawal decided: Tora withdrew from m-r1-0.
	withdrawn := func(t *testing.T, store *state.Store, compID string) {
		t.Helper()
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		first := b.MatchByID("m-r1-0")
		first.Decision, first.DecisionBy = string(domain.DecisionKikenVoluntary), "B"
		require.NoError(t, store.SaveBracket(compID, b))
	}
	t.Run("a match nobody has played", func(t *testing.T) {
		eng, store, compID := rpSetup(t, false)
		withdrawn(t, store, compID)
		prior := rpNext(t, store, compID)

		_, err := eng.ReopenMatch(compID, "m-r1-0", "wrong waza")
		require.NoError(t, err)

		next := rpNext(t, store, compID)
		assert.Empty(t, next.SideAID, "the slot is a placeholder again")
		assert.NotEqual(t, wrTeamA, next.SideA)
		a, c := next.RepPicks()
		assert.Empty(t, a)
		assert.Equal(t, rpPickC, c)
		assert.Greater(t, next.GroupStamp(state.GroupRepPickA), mmT1)
		assert.GreaterOrEqual(t, next.GroupStamp(state.GroupRepPickA), prior.ModifiedAt)
		assert.Equal(t, mmT1, next.GroupStamp(state.GroupRepPickB), "side B's pick keeps its date")
	})

	t.Run("a match already played and reopened with it", func(t *testing.T) {
		eng, store, compID := rpSetup(t, true)
		withdrawn(t, store, compID)
		var reopened []ReopenedMatch
		_, err := eng.ReopenMatch(compID, "m-r1-0", "wrong waza", ForceOptions{Force: true, Reopened: &reopened})
		require.NoError(t, err)
		require.Equal(t, []string{rpNextID}, reopenedIDs(reopened))
		assert.Equal(t, []string{state.GroupRepPickA}, reopened[0].RepPicksCleared, "judged after the retraction, which comes after the reopen")

		a, c := func() (string, string) { n := rpNext(t, store, compID); return n.RepPicks() }()
		assert.Empty(t, a)
		assert.Equal(t, rpPickC, c)
		entries, err := store.LoadMatchHistory(compID, rpNextID)
		require.NoError(t, err)
		var named, namedB bool
		for _, e := range entries {
			if e.Door == doorDownstreamReopen && slices.Contains(e.Changed, state.GroupRepPickA) {
				named = true
			}
			if e.Door == doorDownstreamReopen && slices.Contains(e.Changed, state.GroupRepPickB) {
				namedB = true
			}
		}
		assert.True(t, named, "the reopen line names the cleared side's pick group")
		assert.False(t, namedB, "and not the side that kept its pick")
	})
}

// The qualifier paint gives a match's side another team: its pick goes. A
// match that is running, or has its own result, is not repainted at all, so
// its picks stay (the side is not changed).
func TestResolveSlots_PaintedSideLosesItsPickAndALockedMatchKeepsIt(t *testing.T) {
	build := func(status state.MatchStatus) *state.Bracket {
		return &state.Bracket{Rounds: [][]state.BracketMatch{{{
			ID: "m", PlaceholderA: "Pool A-1st", PlaceholderB: "Pool B-1st",
			SideA: "Ryu", SideAID: "a", SideB: "Tora", SideBID: "b",
			Status: status, ModifiedAt: 500,
			GroupStamps: map[string]int64{state.GroupRepPickA: 400, state.GroupRepPickB: 400, state.BoutGroup(state.DaihyosenSubPosition): 400},
			SubResults: []state.SubMatchResult{{
				Position: state.DaihyosenSubPosition, SideAMemberID: "pa", SideBMemberID: "pb",
			}},
		}}}}
	}
	resolver := map[string]resolvedFinisher{
		"Pool A-1st": {Name: "Kuma", ID: "c"}, // Pool A's winner changed
		"Pool B-1st": {Name: "Tora", ID: "b"}, // Pool B's did not
	}

	t.Run("scheduled", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		bracket := build(state.MatchStatusScheduled)
		eng.resolveSlots(bracket, resolver)
		m := bracket.Rounds[0][0]
		assert.Equal(t, "Kuma", m.SideA)
		assert.Equal(t, "c", m.SideAID)
		a, b := m.RepPicks()
		assert.Empty(t, a, "side A was given another team")
		assert.Equal(t, "pb", b, "side B was not")
		assert.Greater(t, m.GroupStamp(state.GroupRepPickA), int64(400))
		assert.Equal(t, int64(400), m.GroupStamp(state.GroupRepPickB), "side B's pick keeps its date")
	})

	t.Run("running", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		bracket := build(state.MatchStatusRunning)
		eng.resolveSlots(bracket, resolver)
		m := bracket.Rounds[0][0]
		assert.Equal(t, "Ryu", m.SideA, "a running match keeps its competitor")
		a, b := m.RepPicks()
		assert.Equal(t, "pa", a)
		assert.Equal(t, "pb", b)
		assert.Equal(t, int64(400), m.GroupStamp(state.GroupRepPickA))
		assert.Equal(t, int64(400), m.GroupStamp(state.GroupRepPickB))
	})
}

// A pool requalification that reopens a played knockout match and repaints its
// slot with another competitor: the reopen line names the picks group.
func TestRequalify_ForcedReopenTakesThePickAndItsLineNamesIt(t *testing.T) {
	f := newRQFixture(t, "rq-picks", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	m1, s1 := f.slot("Pool A-1st")
	require.NoError(t, f.scoreKO(m1.ID, "A1"))

	// The played match's rep bout holds a pick for Pool A's winner.
	b := f.bracket()
	played := findBracketMatchInBracket(b, m1.ID)
	pick := state.SubMatchResult{Position: state.DaihyosenSubPosition}
	if s1 == "A" {
		pick.SideAMemberID = "pick-a1"
	} else {
		pick.SideBMemberID = "pick-a1"
	}
	played.SubResults = []state.SubMatchResult{pick}
	require.NoError(t, f.store.SaveBracket(f.compID, b))

	var reopened []ReopenedMatch
	require.NoError(t, f.write("Pool A-0", f.poolResult("Pool A-0", "A2"), ForceOptions{Force: true, Reopened: &reopened}))
	require.Len(t, reopened, 1)
	assert.Equal(t, m1.ID, reopened[0].ID)
	// The pick sat on the side Pool A's winner holds.
	pickGroup, keptGroup := state.GroupRepPickB, state.GroupRepPickA
	if s1 == "A" {
		pickGroup, keptGroup = state.GroupRepPickA, state.GroupRepPickB
	}
	assert.Equal(t, []string{pickGroup}, reopened[0].RepPicksCleared)

	got := findBracketMatchInBracket(f.bracket(), m1.ID)
	name, _ := sideOf(*got, s1)
	assert.Equal(t, "A2", name)
	a, c := got.RepPicks()
	assert.Empty(t, a)
	assert.Empty(t, c)

	entries, err := f.store.LoadMatchHistory(f.compID, m1.ID)
	require.NoError(t, err)
	var named, namedKept bool
	for _, e := range entries {
		if e.Door == doorDownstreamReopen && slices.Contains(e.Changed, pickGroup) {
			named = true
		}
		if e.Door == doorDownstreamReopen && slices.Contains(e.Changed, keptGroup) {
			namedKept = true
		}
	}
	assert.True(t, named, "the reopen line names the cleared side's pick group")
	assert.False(t, namedKept, "and not the side that held no pick")
}
