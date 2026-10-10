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
		assert.Equal(t, int64(400), bm.GroupStamp(state.BoutGroup(state.DaihyosenSubPosition)), "a name write dates nothing")
		assert.Equal(t, int64(500), bm.ModifiedAt)
		assert.Equal(t, "Ryu II", bm.SubResults[0].SideA, "the row follows the rename")
		assert.Equal(t, "Kuma", bm.SubResults[0].SideB, "the other side's name is not touched")
		assert.Equal(t, "pa", bm.SubResults[0].WinnerMemberID, "a rename keeps the pick and the id that rides on it")
	})

	t.Run("another team on side A clears that pick only, and the winner id that rode on it", func(t *testing.T) {
		bm := seatFixture()
		seatBracketSide(bm, domain.MatchSideA, "Tora", "b")
		assert.Equal(t, "Tora", bm.SideA)
		assert.Equal(t, "b", bm.SideAID)
		assert.Equal(t, "Tora", bm.SubResults[0].SideA, "the row names the team now seated")
		assert.Equal(t, "Kuma", bm.SubResults[0].SideB, "the other side's name is not touched")
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
		assert.Equal(t, "Tora", bm.SubResults[0].SideB, "the row names the team now seated")
		assert.Equal(t, "Ryu", bm.SubResults[0].SideA, "the other side's name is not touched")
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
		assert.Equal(t, "Winner of r1-m0", bm.SubResults[0].SideA, "the row names whatever the match does, a placeholder included")
		a, _ := bm.RepPicks()
		assert.Empty(t, a)
	})

	t.Run("a slot cleared to nothing empties the row's name too, and clears the pick", func(t *testing.T) {
		bm := seatFixture()
		seatBracketSide(bm, domain.MatchSideB, "", "")
		assert.Empty(t, bm.SideB)
		assert.Empty(t, bm.SideBID)
		assert.Empty(t, bm.SubResults[0].SideB, "one rule, no special case for the empty string")
		_, c := bm.RepPicks()
		assert.Empty(t, c)
		assert.Equal(t, "Ryu", bm.SubResults[0].SideA)
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
		assert.Equal(t, "Ryu", shared[0].SideA, "nor is the name the re-seat writes")
	})

	t.Run("the row another copy of the match shares is not written through by a rename either", func(t *testing.T) {
		bm := seatFixture()
		shared := bm.SubResults
		seatBracketSide(bm, domain.MatchSideA, "Ryu II", "a")
		assert.Equal(t, "Ryu", shared[0].SideA)
		assert.Equal(t, "Ryu II", bm.SubResults[0].SideA)
	})

	t.Run("a name that already matches the row leaves the row's list alone", func(t *testing.T) {
		bm := seatFixture()
		before := &bm.SubResults[0]
		seatBracketSide(bm, domain.MatchSideA, "Ryu", "a")
		assert.Same(t, before, &bm.SubResults[0], "no copy when there is nothing to write")
	})

	// "Given another team" is judged by id when both sides have one, by name
	// otherwise (the client's rule in admin_scoring_team.jsx: another team only
	// when BOTH the side's lookup key and the resolved team changed). The cases
	// below are legacy, id-less data; the id comparison alone got both wrong.
	t.Run("an id-less side re-seated under another name is another team: the pick goes", func(t *testing.T) {
		bm := seatFixture()
		bm.SideAID = ""
		seatBracketSide(bm, domain.MatchSideA, "Tora", "")
		assert.Equal(t, "Tora", bm.SideA)
		assert.Empty(t, bm.SideAID)
		assert.Equal(t, "Tora", bm.SubResults[0].SideA)
		a, c := bm.RepPicks()
		assert.Empty(t, a, "the team changed although neither side has an id")
		assert.Equal(t, "pc", c)
		assert.Greater(t, bm.GroupStamp(state.GroupRepPickA), int64(400))
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickB))
	})

	t.Run("an id-less side re-seated under the same name keeps the pick and every stamp", func(t *testing.T) {
		bm := seatFixture()
		bm.SideAID = ""
		seatBracketSide(bm, domain.MatchSideA, "Ryu", "")
		a, _ := bm.RepPicks()
		assert.Equal(t, "pa", a)
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickA))
		assert.Equal(t, int64(500), bm.ModifiedAt)
	})

	t.Run("a writer that does not know the id, naming the same team, neither clears the pick nor blanks the id", func(t *testing.T) {
		bm := seatFixture()
		seatBracketSide(bm, domain.MatchSideA, "Ryu", "")
		assert.Equal(t, "Ryu", bm.SideA)
		assert.Equal(t, "a", bm.SideAID, "a resolved id is never thrown away for a writer that did not know it")
		a, _ := bm.RepPicks()
		assert.Equal(t, "pa", a)
		assert.Equal(t, "pa", bm.SubResults[0].WinnerMemberID)
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickA))
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickB))
		assert.Equal(t, int64(500), bm.ModifiedAt)
	})

	t.Run("a side that carried only its team's name gaining the team's id is not another team", func(t *testing.T) {
		bm := seatFixture()
		bm.SideAID = ""
		seatBracketSide(bm, domain.MatchSideA, "Ryu", "a")
		assert.Equal(t, "a", bm.SideAID, "the id is written")
		a, _ := bm.RepPicks()
		assert.Equal(t, "pa", a, "the pick stays")
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickA))
		assert.Equal(t, int64(500), bm.ModifiedAt)
	})

	t.Run("an id-less placeholder resolved to a team is another team, with nothing to clear", func(t *testing.T) {
		bm := seatFixture()
		bm.SideA, bm.SideAID = "Winner of r1-m2", ""
		bm.SubResults[0].SideA = "Winner of r1-m2"
		bm.SubResults[0].SideAMemberID, bm.SubResults[0].WinnerMemberID = "", ""
		seatBracketSide(bm, domain.MatchSideA, "Ryu", "a")
		assert.Equal(t, "Ryu", bm.SideA)
		assert.Equal(t, "a", bm.SideAID)
		assert.Equal(t, "Ryu", bm.SubResults[0].SideA, "the row follows the match from the placeholder to the team")
		assert.Equal(t, int64(400), bm.GroupStamp(state.GroupRepPickA), "no pick, so nothing is dated")
		assert.Equal(t, int64(500), bm.ModifiedAt)
	})

	t.Run("two different ids are another team whatever the names say", func(t *testing.T) {
		bm := seatFixture()
		seatBracketSide(bm, domain.MatchSideA, "Ryu", "b")
		assert.Equal(t, "b", bm.SideAID)
		a, _ := bm.RepPicks()
		assert.Empty(t, a)
	})

	t.Run("a stored id and an empty one under another name is another team: the id goes with the pick", func(t *testing.T) {
		bm := seatFixture()
		seatBracketSide(bm, domain.MatchSideA, "Winner of r1-m2", "")
		assert.Empty(t, bm.SideAID)
		a, _ := bm.RepPicks()
		assert.Empty(t, a)
	})
}

// A semifinal re-scored: the final's and the 3rd-place match's sides both get
// another team, and each loses the pick it held for the team that left it.
func TestPropagateBracketWinner_ReseatsTheNextAndTheBronzeSidesAndTheirPicks(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	row := func(nameA, nameB, a, b string) []state.SubMatchResult {
		return []state.SubMatchResult{{
			Position: state.DaihyosenSubPosition, SideA: nameA, SideB: nameB,
			SideAMemberID: a, SideBMemberID: b,
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
					Status: state.MatchStatusScheduled, SubResults: row("Tora", "Kuma", "pb", "pk")},
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: "bronze", SideA: "Ryu", SideAID: "a", SideB: "Hawk", SideBID: "d",
			Status: state.MatchStatusScheduled, SubResults: row("Ryu", "Hawk", "pa", "ph"),
		},
	}

	eng.propagateBracketWinner(bracket, 0, 0)

	final := bracket.Rounds[1][0]
	assert.Equal(t, "Ryu", final.SideA)
	fa, fb := final.RepPicks()
	assert.Empty(t, fa, "Tora left the final's side A")
	assert.Equal(t, "pk", fb, "Kuma did not")
	assert.Equal(t, final.SideA, final.SubResults[0].SideA, "the final's row names the team now seated")
	assert.Equal(t, final.SideB, final.SubResults[0].SideB, "and the side that was not re-seated")
	bronze := bracket.ThirdPlaceMatch
	assert.Equal(t, "Tora", bronze.SideA)
	ba, bb := bronze.RepPicks()
	assert.Empty(t, ba, "Ryu left the bronze match's side A")
	assert.Equal(t, "ph", bb, "Hawk did not")
	assert.Equal(t, bronze.SideA, bronze.SubResults[0].SideA, "the bronze match's row names the team now seated")
	assert.Equal(t, bronze.SideB, bronze.SubResults[0].SideB)
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
		require.GreaterOrEqual(t, state.DaihyosenSubIndex(next.SubResults), 0)
		assert.Equal(t, next.SideA, next.SubResults[state.DaihyosenSubIndex(next.SubResults)].SideA, "the row names the placeholder the slot went back to")
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
				Position: state.DaihyosenSubPosition, SideA: "Ryu", SideB: "Tora",
				SideAMemberID: "pa", SideBMemberID: "pb",
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
		assert.Equal(t, "Kuma", m.SubResults[0].SideA, "the row names the competitor painted over the placeholder")
		assert.Equal(t, "Tora", m.SubResults[0].SideB)
		assert.Greater(t, m.GroupStamp(state.GroupRepPickA), int64(400))
		assert.Equal(t, int64(400), m.GroupStamp(state.GroupRepPickB), "side B's pick keeps its date")
	})

	t.Run("running", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		bracket := build(state.MatchStatusRunning)
		eng.resolveSlots(bracket, resolver)
		m := bracket.Rounds[0][0]
		assert.Equal(t, "Ryu", m.SideA, "a running match keeps its competitor")
		assert.Equal(t, "Ryu", m.SubResults[0].SideA, "and its row keeps the name")
		a, b := m.RepPicks()
		assert.Equal(t, "pa", a)
		assert.Equal(t, "pb", b)
		assert.Equal(t, int64(400), m.GroupStamp(state.GroupRepPickA))
		assert.Equal(t, int64(400), m.GroupStamp(state.GroupRepPickB))
	})
}

// A team renamed while the competition is draw-ready: the add of a
// representative bout commits the running match in its own transaction and
// starts the competition after it, and a failed start is only logged, so a
// draw-ready competition can hold a representative bout. The hantei mark is
// placed on the winner's side by comparing the winner to that row's names, so
// the row's names and its Winner follow the rename (the row carries no pick
// and no winner member id here: either would credit the side by id and make
// the assertion green without the rename).
func TestReplaceParticipantInDraw_ARenamedTeamKeepsTheRepresentativeRowInStep(t *testing.T) {
	const compID = "rename-rep-row"
	// The representative bout was decided for side B, the team renamed.
	build := func(sideBID string) *state.Bracket {
		return &state.Bracket{Rounds: [][]state.BracketMatch{{{
			ID:    "m1",
			SideA: "Ryu", SideAID: "id-a", SideB: "Tora", SideBID: sideBID,
			Status: state.MatchStatusRunning, ModifiedAt: 500,
			SubResults: []state.SubMatchResult{{
				Position: state.DaihyosenSubPosition, SideA: "Ryu", SideB: "Tora",
				IpponsA: []string{}, IpponsB: []string{domain.HanteiMark},
				Winner: "Tora", Decision: "daihyosen",
			}},
		}}}}
	}
	for _, tc := range []struct {
		name    string
		sideBID string // "" is a legacy side no id was ever stamped on
		pid     string
	}{
		{name: "a side carrying the team's id", sideBID: "id-b", pid: "id-b"},
		{name: "a legacy side with no id", sideBID: "", pid: "id-elsewhere"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID: compID, Name: "Rename", Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed,
				Format: state.CompFormatKnockout, Courts: []string{"A"}, StartTime: "09:00",
				Status: state.CompStatusDrawReady,
			}))
			require.NoError(t, store.SaveBracket(compID, build(tc.sideBID)))

			_, err := eng.ReplaceParticipantInDraw(compID, tc.pid, "Tora", "Dojo", "", "Lion", "Dojo", "")
			require.NoError(t, err)

			bracket, err := store.LoadBracket(compID)
			require.NoError(t, err)
			m := bracket.Rounds[0][0]
			require.Equal(t, "Lion", m.SideB, "the match side is renamed")
			row := m.SubResults[state.DaihyosenSubIndex(m.SubResults)]
			assert.Equal(t, "Lion", row.SideB, "the representative row follows the match's name")
			assert.Equal(t, "Lion", row.Winner, "and so does its winner, which the mark is placed by")
			assert.Equal(t, "Ryu", row.SideA, "side A is untouched")
			assert.Equal(t, domain.MatchSideB, state.SubBoutWinnerSide(row, m.SideA, m.SideB), "side B is still credited")
		})
	}
}

// A rename never clears a representative pick: the side keeps its team. Pass 2
// of the rename (a side with no id) writes the name directly and must not read
// "Tora" -> "Lion" as another team, which seatBracketSide would for an id-less
// side.
func TestReplaceParticipantInDraw_ARenamedIdlessSideKeepsItsPick(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "rename-keeps-pick"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Rename", Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed,
		Format: state.CompFormatKnockout, Courts: []string{"A"}, StartTime: "09:00",
		Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID:    "m1",
		SideA: "Ryu", SideAID: "id-a", SideB: "Tora",
		Status: state.MatchStatusRunning, ModifiedAt: 500,
		GroupStamps: map[string]int64{state.GroupRepPickB: 400},
		SubResults: []state.SubMatchResult{{
			Position: state.DaihyosenSubPosition, SideA: "Ryu", SideB: "Tora", SideBMemberID: "pb",
		}},
	}}}}))

	_, err := eng.ReplaceParticipantInDraw(compID, "id-elsewhere", "Tora", "Dojo", "", "Lion", "Dojo", "")
	require.NoError(t, err)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := bracket.Rounds[0][0]
	_, pickB := m.RepPicks()
	assert.Equal(t, "pb", pickB, "the renamed side keeps its pick")
	assert.Equal(t, "Lion", m.SubResults[0].SideB)
	assert.Equal(t, int64(400), m.GroupStamp(state.GroupRepPickB), "and its date")
}

// Pass 2 of ReplaceParticipantInDraw renames an id-less side directly and hands
// the representative row the match's old name (the one it matched on), so a row
// that names no one still carries the winner recorded under that name.
func TestReplaceParticipantInDraw_AnIdlessRenameCarriesTheWinnerOfARowThatNamesNoOne(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "rename-carries-winner"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Rename", Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed,
		Format: state.CompFormatKnockout, Courts: []string{"A"}, StartTime: "09:00",
		Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID:    "m1",
		SideA: "Ryu", SideAID: "id-a", SideB: "Tora",
		Status: state.MatchStatusRunning, ModifiedAt: 500,
		SubResults: []state.SubMatchResult{{
			Position: state.DaihyosenSubPosition, SideA: "Ryu", SideB: "",
			IpponsB: []string{domain.HanteiMark}, Winner: "Tora", Decision: "daihyosen",
		}},
	}}}}))

	_, err := eng.ReplaceParticipantInDraw(compID, "id-elsewhere", "Tora", "Dojo", "", "Lion", "Dojo", "")
	require.NoError(t, err)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := bracket.Rounds[0][0]
	assert.Equal(t, "Lion", m.SideB)
	assert.Equal(t, "Lion", m.SubResults[0].SideB)
	assert.Equal(t, "Lion", m.SubResults[0].Winner, "the winner follows the match's old name")
	assert.Equal(t, domain.MatchSideB, state.SubBoutWinnerSide(m.SubResults[0], m.SideA, m.SideB))
}

// The same id under a new name is the same team: the representative row's
// winner follows the row's name, or the mark would be placed on nobody. A side
// given another team leaves the winner alone (the row keeps what was decided).
func TestSeatBracketSide_ARenameCarriesTheRowsWinnerAndAnotherTeamDoesNot(t *testing.T) {
	build := func() *state.BracketMatch {
		return &state.BracketMatch{
			ID:    "m",
			SideA: "Ryu", SideAID: "a", SideB: "Tora", SideBID: "b",
			SubResults: []state.SubMatchResult{{
				Position: state.DaihyosenSubPosition, SideA: "Ryu", SideB: "Tora",
				IpponsB: []string{domain.HanteiMark}, Winner: "Tora", Decision: "daihyosen",
			}},
		}
	}

	t.Run("the same id under a new name", func(t *testing.T) {
		bm := build()
		seatBracketSide(bm, domain.MatchSideB, "Lion", "b")
		assert.Equal(t, "Lion", bm.SubResults[0].SideB)
		assert.Equal(t, "Lion", bm.SubResults[0].Winner, "the winner follows the name it was recorded under")
		assert.Equal(t, domain.MatchSideB, state.SubBoutWinnerSide(bm.SubResults[0], bm.SideA, bm.SideB))
	})
	t.Run("the other side's rename leaves a winner who is not it", func(t *testing.T) {
		bm := build()
		seatBracketSide(bm, domain.MatchSideA, "Dragon", "a")
		assert.Equal(t, "Dragon", bm.SubResults[0].SideA)
		assert.Equal(t, "Tora", bm.SubResults[0].Winner)
	})
	t.Run("another team", func(t *testing.T) {
		bm := build()
		seatBracketSide(bm, domain.MatchSideB, "Kuma", "c")
		assert.Equal(t, "Kuma", bm.SubResults[0].SideB)
		assert.Equal(t, "Tora", bm.SubResults[0].Winner, "a decision made for the team that left is left as it was")
	})

	// A row's side name can be blank or stale: AddDaihyosen stamps the names and
	// adoptCurrentSideName only rewrites a row carrying the OLD name, never
	// fills a blank, while the merge lands a payload row's names as sent, so a
	// writer that does not restate them (rows written before the names rule,
	// hand-edited data) leaves them blank. The Winner names the team by the
	// match's name, so the rename carries it from there too.
	t.Run("a row that names no one carries the winner the match's old name recorded", func(t *testing.T) {
		bm := build()
		bm.SubResults[0].SideB = ""
		seatBracketSide(bm, domain.MatchSideB, "Lion", "b")
		assert.Equal(t, "Lion", bm.SubResults[0].SideB)
		assert.Equal(t, "Lion", bm.SubResults[0].Winner, "the winner follows the match's old name")
		assert.Equal(t, domain.MatchSideB, state.SubBoutWinnerSide(bm.SubResults[0], bm.SideA, bm.SideB))
	})
	t.Run("a row that names a stale team carries the winner the match's old name recorded", func(t *testing.T) {
		bm := build()
		bm.SubResults[0].SideB = "Tigre"
		seatBracketSide(bm, domain.MatchSideB, "Lion", "b")
		assert.Equal(t, "Lion", bm.SubResults[0].SideB)
		assert.Equal(t, "Lion", bm.SubResults[0].Winner)
	})
	// The carry is not skipped for a row that already reads the new name: a writer
	// that restated the rename on the row (the merge lands a payload row's names as
	// sent) can leave the Winner on the match's old name.
	t.Run("a row that already reads the new name carries the winner the match's old name recorded", func(t *testing.T) {
		bm := build()
		bm.SubResults[0].SideB = "Lion"
		seatBracketSide(bm, domain.MatchSideB, "Lion", "b")
		assert.Equal(t, "Lion", bm.SubResults[0].SideB)
		assert.Equal(t, "Lion", bm.SubResults[0].Winner, "the winner follows the match's old name")
		assert.Equal(t, domain.MatchSideB, state.SubBoutWinnerSide(bm.SubResults[0], bm.SideA, bm.SideB))
	})
	// PIN, green by design: a row that already reads the new name and whose Winner
	// already follows it has nothing to carry, so it is not copied (the list a
	// row's other holders share is replaced only when a row changes).
	t.Run("a row that already reads the new name and winner is untouched and not copied", func(t *testing.T) {
		bm := build()
		bm.SubResults[0].SideB, bm.SubResults[0].Winner = "Lion", "Lion"
		before := &bm.SubResults[0]
		seatBracketSide(bm, domain.MatchSideB, "Lion", "b")
		assert.Same(t, before, &bm.SubResults[0], "the list is the one it was")
		assert.Equal(t, "Lion", bm.SubResults[0].Winner)
	})
	// PIN, green by design: the match's old name carries the Winner only for a
	// side that keeps its team, as the row's own old name does.
	t.Run("a row that names no one, given another team, leaves the winner", func(t *testing.T) {
		bm := build()
		bm.SubResults[0].SideB = ""
		seatBracketSide(bm, domain.MatchSideB, "Kuma", "c")
		assert.Equal(t, "Kuma", bm.SubResults[0].SideB)
		assert.Equal(t, "Tora", bm.SubResults[0].Winner, "a decision made for the team that left is left as it was")
	})
	// PIN, green by design: the other side's rename must not take a winner that
	// names this side, even through the match's old name.
	t.Run("a row that names no one, the other side renamed, leaves a winner who is not it", func(t *testing.T) {
		bm := build()
		bm.SubResults[0].SideA = ""
		seatBracketSide(bm, domain.MatchSideA, "Dragon", "a")
		assert.Equal(t, "Dragon", bm.SubResults[0].SideA)
		assert.Equal(t, "Tora", bm.SubResults[0].Winner)
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

// DELETE .../overrides removes a pool's hand-set order without asking the
// planner (it only writes overrides.json), so the knockout still seats the
// order that was removed until the next auto-complete repaints it through
// ResolveQualifiedPools, which no confirmation or reopen answers for. A match
// sent back to the queue keeps its representative bout's picks and is not
// locked, so the repaint can take a pick from it; it records the re-seat in the
// match's history like every other door that does (bracket_seat_audit.go).
func TestResolveQualifiedPools_ARepaintTheOverridesRemovalLeftRecordsTheReseat(t *testing.T) {
	f := newRQFixture(t, "rq-overrides-removed", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	// Pool A's order is set by hand: the planner seats A2.
	_, err := f.eng.OverridePoolRanks(f.compID, "Pool A", []RankOverride{{PlayerID: rqID("A2"), Rank: 1}}, ForceOptions{})
	require.NoError(t, err)
	m, side := f.slot("Pool A-1st")
	name, _ := sideOf(m, side)
	require.Equal(t, "A2", name, "the hand-set order is seated")

	// The match was sent back to the queue holding A2's representative pick.
	b := f.bracket()
	queued := findBracketMatchInBracket(b, m.ID)
	require.Equal(t, state.MatchStatusScheduled, queued.Status)
	pick := state.SubMatchResult{Position: state.DaihyosenSubPosition}
	pickGroup := state.GroupRepPickB
	if side == "A" {
		pick.SideAMemberID, pickGroup = "pick-a2", state.GroupRepPickA
	} else {
		pick.SideBMemberID = "pick-a2"
	}
	queued.SubResults = []state.SubMatchResult{pick}
	require.NoError(t, f.store.SaveBracket(f.compID, b))

	changed, err := f.store.ResetOverridesChanged(f.compID)
	require.NoError(t, err)
	require.True(t, changed)
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)

	got := findBracketMatchInBracket(f.bracket(), m.ID)
	name, _ = sideOf(*got, side)
	require.Equal(t, "A1", name, "the natural order is seated again")
	a, c := got.RepPicks()
	assert.Empty(t, a)
	assert.Empty(t, c)

	entries, err := f.store.LoadMatchHistory(f.compID, m.ID)
	require.NoError(t, err)
	var recorded bool
	for _, e := range entries {
		if e.Door == doorReseat && slices.Contains(e.Changed, pickGroup) {
			recorded = true
		}
	}
	assert.True(t, recorded, "the re-seat that took the pick is in the match's history, not only in a log line")
}
