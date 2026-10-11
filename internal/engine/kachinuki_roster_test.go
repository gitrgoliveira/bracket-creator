package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rosterTeams is a kachinuki competition's two teams as the store holds them,
// each with named members (ids minted by the store) and, when asked, a
// starting lineup that fields them in order.
type rosterTeams struct {
	aID, bID string
	a, b     []domain.TeamMember
}

func seedRosterTeams(t *testing.T, store *state.Store, compID string, size int, aNames, bNames []string, lineupA, lineupB bool) rosterTeams {
	t.Helper()
	rt := rosterTeams{aID: helper.NewUUID4(), bID: helper.NewUUID4()}
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: rt.aID, Name: "Ryu", Dojo: "DojoR"},
		{ID: rt.bID, Name: "Tora", Dojo: "DojoT"},
	}))
	add := func(teamID string, names []string) []domain.TeamMember {
		out := make([]domain.TeamMember, 0, len(names))
		for _, n := range names {
			m, err := store.AddTeamMember(compID, teamID, n)
			require.NoError(t, err)
			out = append(out, m)
		}
		return out
	}
	rt.a = add(rt.aID, aNames)
	rt.b = add(rt.bID, bNames)
	save := func(teamID string, members []domain.TeamMember) {
		l := domain.TeamLineup{TeamID: teamID, Round: 0, Positions: map[domain.Position]string{}, MemberIDs: map[domain.Position]string{}}
		for i, m := range members {
			l.Positions[domain.PositionNumbered(i+1)] = m.Name
			l.MemberIDs[domain.PositionNumbered(i+1)] = m.ID
		}
		require.NoError(t, store.SetTeamLineup(compID, l, size))
	}
	if lineupA {
		save(rt.aID, rt.a)
	}
	if lineupB {
		save(rt.bID, rt.b)
	}
	return rt
}

// rosterBout is a numbered bout row between two members, with the winner (by
// member id, as the editor records it) or a hikiwake; neither is a bout not
// fought yet.
func rosterBout(pos int, a, b domain.TeamMember, winner *domain.TeamMember, hikiwake bool) state.SubMatchResult {
	s := state.SubMatchResult{Position: pos, SideA: a.Name, SideAMemberID: a.ID, SideB: b.Name, SideBMemberID: b.ID}
	switch {
	case hikiwake:
		s.Decision = "hikiwake"
	case winner != nil:
		s.Winner, s.WinnerMemberID, s.Decision = winner.Name, winner.ID, "fought"
	}
	return s
}

func rosterIDs(side KachinukiRosterSide) []string {
	out := make([]string, 0, len(side.Remaining))
	for _, f := range side.Remaining {
		out = append(out, f.MemberID)
	}
	return out
}

func memberIDs(ms ...domain.TeamMember) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

// onID is the member id of the side's fighter on, "" when nobody is on.
func onID(side KachinukiRosterSide) string {
	if side.On == nil {
		return ""
	}
	return side.On.MemberID
}

// A win retires the loser, a hikiwake retires both, and the live bout (the
// last numbered row) names each side's fighter on. Remaining is what is left
// behind the fighter on, in lineup order: the next fighter first.
func TestKachinukiRoster_SplitsTheFighterOnFromTheQueue(t *testing.T) {
	// Four a side: a five-person team's positions are the FIK names, and
	// this pins the queue, not the position keys.
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-basic", 4,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 4,
		[]string{"A1", "A2", "A3", "A4"}, []string{"B1", "B2", "B3", "B4"}, true, true)
	a, b := rt.a, rt.b
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			rosterBout(1, a[0], b[0], &a[0], false),
			rosterBout(2, a[0], b[1], nil, true),
			rosterBout(3, a[1], b[2], nil, false),
		},
	}}))

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.True(t, got.SideA.LineupFound)
	assert.True(t, got.SideB.LineupFound)
	require.NotNil(t, got.SideA.On)
	assert.Equal(t, "A2", got.SideA.On.Name)
	assert.Equal(t, a[1].ID, onID(got.SideA))
	assert.Equal(t, b[2].ID, onID(got.SideB))
	assert.Equal(t, memberIDs(a[2:]...), rosterIDs(got.SideA), "A1 drew and retired; A2 is on")
	assert.Equal(t, memberIDs(b[3:]...), rosterIDs(got.SideB), "B1 lost, B2 drew; B3 is on")
}

// The fighter on is taken out of the queue by the engine's identity rule
// (IsMemberRetired), not by a second one: a live row that names the fighter
// without the member id the lineup slot carries still matches that slot by
// its unambiguous name, so the fighter on is never counted again or named as
// next.
func TestKachinukiRoster_FighterOnWithoutAnIdIsMatchedByName(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-on-no-id", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 3,
		[]string{"A1", "Ueda", "A3"}, []string{"B1", "B2", "B3"}, true, true)
	a, b := rt.a, rt.b
	live := rosterBout(2, a[1], b[0], nil, false)
	live.SideAMemberID = "" // the row names Ueda only
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{rosterBout(1, a[0], b[0], &b[0], false), live},
	}}))

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	require.NotNil(t, got.SideA.On)
	assert.Equal(t, "Ueda", got.SideA.On.Name)
	assert.Equal(t, memberIDs(a[2]), rosterIDs(got.SideA), "Ueda is on, not next; A3 is next")
}

// Two opposing fighters may share a display name. Who retired is settled by
// the bout row's member ids, never by the name the two share.
func TestKachinukiRoster_SameNameIsSettledByMemberID(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-same-name", 2,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 2,
		[]string{"Yamada", "Ito"}, []string{"Yamada", "Kudo"}, true, true)
	a, b := rt.a, rt.b
	// Shiro's (side B's) Yamada wins the recorded bout: Aka's Yamada retires,
	// and only he does.
	recorded := rosterBout(1, a[0], b[0], &b[0], false)
	recorded.Recorded = true
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{recorded},
	}}))

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.Nil(t, got.SideA.On, "the recorded bout retired Aka's Yamada")
	assert.Equal(t, memberIDs(a[1]), rosterIDs(got.SideA), "Ito is next")
	assert.Equal(t, b[0].ID, onID(got.SideB), "Shiro's Yamada stays on")
	assert.Equal(t, memberIDs(b[1]), rosterIDs(got.SideB))
}

// The last bout row's own result says who is still on, for every reader: a
// winner takes the loser off (a bout recorded when a side has nobody left
// appends nothing, so this is how the line learns it is over), a hikiwake
// takes both off, and a row with no result keeps both on.
func TestKachinukiRoster_TheBoutBeingFoughtKeepsBothFightersOn(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-live", 2,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 2,
		[]string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
	a, b := rt.a, rt.b
	read := func(last state.SubMatchResult) KachinukiRoster {
		t.Helper()
		saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{rosterBout(1, a[0], b[0], &a[0], false), last}, false)
		got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
		require.NoError(t, err)
		return got
	}

	// The running autosave writes a 1-0 lead's winner and a 1-1's hikiwake on
	// the live row. Neither is a result until Record bout records the bout.
	for name, last := range map[string]state.SubMatchResult{
		"no score":   rosterBout(2, a[0], b[1], nil, false),
		"a 1-0 lead": rosterBout(2, a[0], b[1], &a[0], false),
		"1-1":        rosterBout(2, a[0], b[1], nil, true),
	} {
		got := read(last)
		assert.Equal(t, a[0].ID, onID(got.SideA), "%s: A1 is on", name)
		assert.Equal(t, b[1].ID, onID(got.SideB), "%s: B2 is on", name)
		assert.Equal(t, memberIDs(a[1]), rosterIDs(got.SideA), "%s", name)
		assert.Empty(t, got.SideB.Remaining, "%s: B2 is B's last fighter", name)
		assert.True(t, got.SideB.LineupFound, "%s", name)
	}
}

func TestKachinukiRoster_RecordBoutAtExhaustionTakesTheLoserOff(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := "pool"
		if bracket {
			name = "bracket"
		}
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-exhausted-"+name, 2,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 2,
				[]string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
			a, b := rt.a, rt.b
			// A1 beat B1, and wins bout 2 against B2, B's last fighter.
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
				rosterBout(1, a[0], b[0], &a[0], false),
				rosterBout(2, a[0], b[1], &a[0], false),
			}, bracket)

			// Record bout: nobody left on B, so nothing is appended, but the
			// bout is marked recorded in a write of its own.
			appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			assert.False(t, appended, "nobody left on B: nothing is appended")
			require.NotNil(t, post, "the recorded mark is a write the answer carries")
			require.Len(t, post.BoutLog, 2)
			assert.True(t, post.BoutLog[1].Recorded, "the bout Record bout closed is recorded")
			assert.False(t, post.BoutLog[0].Recorded, "an earlier bout is not touched")

			got, err := eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			assert.Nil(t, got.SideB.On, "B2 lost the recorded bout and is off")
			assert.Empty(t, got.SideB.Remaining, "no fighters left on B")
			assert.True(t, got.SideB.LineupFound)
			assert.Equal(t, a[0].ID, onID(got.SideA), "the winner stays on")
			assert.Equal(t, memberIDs(a[1]), rosterIDs(got.SideA))

			history, err := store.LoadMatchHistory(comp.ID, matchID)
			require.NoError(t, err)
			require.NotEmpty(t, history)
			lastEntry := history[len(history)-1]
			assert.Equal(t, doorKachinukiRecord, lastEntry.Door)
			assert.Equal(t, []string{state.BoutGroup(2)}, lastEntry.Changed)

			// A second Record bout on the recorded bout writes nothing.
			again, post2, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			assert.False(t, again)
			assert.Nil(t, post2, "a replayed Record bout adds no stamp and no history line")
			history2, err := store.LoadMatchHistory(comp.ID, matchID)
			require.NoError(t, err)
			assert.Len(t, history2, len(history))
		})
	}
}

func TestKachinukiRoster_ARecordedDrawTakesBothOff(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-recorded-draw", 2,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 2,
		[]string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
	a, b := rt.a, rt.b
	// B1 beat A1, A2 beat B1, then A2 and B2 draw: both teams are out.
	saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
		rosterBout(1, a[0], b[0], &b[0], false),
		rosterBout(2, a[1], b[0], &a[1], false),
		rosterBout(3, a[1], b[1], nil, true),
	}, false)
	appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.False(t, appended, "both teams are out: nothing is appended")
	require.NotNil(t, post)
	assert.True(t, post.BoutLog[2].Recorded)

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.Nil(t, got.SideA.On, "a recorded draw takes A2 off")
	assert.Nil(t, got.SideB.On, "a recorded draw takes B2 off")
	assert.Empty(t, got.SideA.Remaining)
	assert.Empty(t, got.SideB.Remaining)
}

func TestKachinukiRoster_ARecordedDrawKeepsTheLastFighterOfAnExhaustedSide(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-taisho", 2,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 2,
		[]string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
	a, b := rt.a, rt.b
	// A1 beat B1, then A1 and B2 draw: B has nobody left, A has A2. The advance
	// keeps B2 on against A2 (advanceAfterHikiwake), and the line reads it so.
	saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
		rosterBout(1, a[0], b[0], &a[0], false),
		rosterBout(2, a[0], b[1], nil, true),
	}, false)
	appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, "Pool A-0")
	require.NoError(t, err)
	require.True(t, appended)
	require.Len(t, post.BoutLog, 3)
	assert.True(t, post.BoutLog[1].Recorded, "the drawn bout is recorded")
	assert.False(t, post.BoutLog[2].Recorded, "the appended pairing is the bout being fought")
	assert.Equal(t, b[1].ID, post.BoutLog[2].SideBMemberID, "B2 stays on")

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.Equal(t, a[1].ID, onID(got.SideA))
	assert.Equal(t, b[1].ID, onID(got.SideB), "B's last fighter stays on after the draw")
	assert.Empty(t, got.SideB.Remaining)
	assert.Empty(t, got.SideA.Remaining)
}

func TestKachinukiRoster_RemovingTheAppendedPairingUnrecordsTheBout(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := "pool"
		if bracket {
			name = "bracket"
		}
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-undo-"+name, 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 3,
				[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, true)
			a, b := rt.a, rt.b
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
				rosterBout(1, a[0], b[0], &a[0], false),
			}, bracket)
			appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			require.True(t, appended)
			require.True(t, post.BoutLog[0].Recorded)

			// Removing the appended pairing undoes the Record bout: bout 1 is
			// the bout being fought again, both its fighters on.
			updated, err := eng.RemoveTrailingKachinukiBout(comp.ID, matchID)
			require.NoError(t, err)
			require.Len(t, updated.SubResults, 1)
			assert.False(t, updated.SubResults[0].Recorded)
			got, err := eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			assert.Equal(t, a[0].ID, onID(got.SideA))
			assert.Equal(t, b[0].ID, onID(got.SideB), "B1 is on again")

			history, err := store.LoadMatchHistory(comp.ID, matchID)
			require.NoError(t, err)
			lastEntry := history[len(history)-1]
			assert.Equal(t, doorKachinukiRemove, lastEntry.Door)
			assert.ElementsMatch(t, []string{state.BoutGroup(2), state.BoutGroup(1)}, lastEntry.Changed)
		})
	}
}

func TestKachinukiRecorded_AWriteNeitherClearsNorSetsIt(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := "pool"
		if bracket {
			name = "bracket"
		}
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-recorded-carry-"+name, 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 3,
				[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, true)
			a, b := rt.a, rt.b
			first := rosterBout(1, a[0], b[0], &a[0], false)
			first.Recorded = true
			live := rosterBout(2, a[0], b[1], nil, false)
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{first, live}, bracket)

			// The running autosave: bout 1 echoed WITHOUT its mark, and bout 2
			// (a point) claiming a mark the server never gave it.
			sentFirst := first
			sentFirst.Recorded = false
			sentLive := live
			sentLive.IpponsA = []string{"M"}
			sentLive.Recorded = true
			require.NoError(t, eng.RecordMatchResult(comp.ID, matchID, &state.MatchResult{
				ID: matchID, SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID,
				Status: state.MatchStatusRunning, ModifiedAt: serverNowMs(),
				SubResults: []state.SubMatchResult{sentFirst, sentLive},
				Changed:    []string{state.BoutGroup(2)}, WriteDoor: DoorScore,
			}))

			subs := storedSubs(t, store, comp.ID, matchID, bracket)
			require.Len(t, subs, 2)
			assert.True(t, subs[0].Recorded, "a write that leaves the mark out keeps it")
			assert.False(t, subs[1].Recorded, "a client cannot mark a bout recorded")
			assert.Equal(t, []string{"M"}, subs[1].IpponsA, "the point itself landed")

			history, err := store.LoadMatchHistory(comp.ID, matchID)
			require.NoError(t, err)
			require.NotEmpty(t, history)
			assert.Equal(t, []string{state.BoutGroup(2)}, history[len(history)-1].Changed,
				"leaving the mark out is not a change of bout 1")
		})
	}
}

// branchName names a pool/bracket subtest.
func branchName(bracket bool) string {
	if bracket {
		return "bracket"
	}
	return "pool"
}

// storedGroupStamp is the stored match's stamp for group.
func storedGroupStamp(t *testing.T, store *state.Store, compID, matchID string, bracket bool, group string) int64 {
	t.Helper()
	if !bracket {
		pms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for i := range pms {
			if pms[i].ID == matchID {
				return pms[i].GroupStamp(group)
			}
		}
		t.Fatalf("pool match %s not found", matchID)
	}
	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bm := br.MatchByID(matchID)
	require.NotNil(t, bm)
	return bm.GroupStamp(group)
}

// A bout added by hand after a side had nobody left is not a pairing Record
// bout appended: removing it leaves the bout Record bout closed recorded, and
// the sheet back in its closed state (review round 3, finding 1).
func TestRemoveTrailingKachinukiBout_AManualBoutAfterExhaustionKeepsTheBoutRecorded(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := branchName(bracket)
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-manual-remove-"+name, 2,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 2,
				[]string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
			a, b := rt.a, rt.b
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
				rosterBout(1, a[0], b[0], &a[0], false),
				rosterBout(2, a[0], b[1], &a[0], false),
			}, bracket)
			appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			require.False(t, appended, "nobody left on B: nothing is appended")
			require.True(t, post.BoutLog[1].Recorded)

			// "+ Add next bout manually": the fighter pick autosaves the row.
			manual := state.SubMatchResult{Position: 3, SideA: a[0].Name, SideAMemberID: a[0].ID, SideB: b[1].Name, SideBMemberID: b[1].ID}
			require.NoError(t, eng.RecordMatchResult(comp.ID, matchID, &state.MatchResult{
				ID: matchID, SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID,
				Status: state.MatchStatusRunning, ModifiedAt: serverNowMs(),
				SubResults: append(append([]state.SubMatchResult(nil), post.BoutLog...), manual),
				Changed:    []string{state.BoutGroup(3)}, WriteDoor: DoorScore,
			}))
			require.Len(t, storedSubs(t, store, comp.ID, matchID, bracket), 3)

			updated, err := eng.RemoveTrailingKachinukiBout(comp.ID, matchID)
			require.NoError(t, err)
			require.Len(t, updated.SubResults, 2)
			assert.True(t, updated.SubResults[1].Recorded, "the removed bout was added by hand: bout 2 stays recorded")
			subs := storedSubs(t, store, comp.ID, matchID, bracket)
			require.Len(t, subs, 2)
			assert.True(t, subs[1].Recorded)

			got, err := eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			assert.Nil(t, got.SideB.On, "B2 is still off: the closed state stands")
			assert.Empty(t, got.SideB.Remaining)

			history, err := store.LoadMatchHistory(comp.ID, matchID)
			require.NoError(t, err)
			lastEntry := history[len(history)-1]
			assert.Equal(t, doorKachinukiRemove, lastEntry.Door)
			assert.Equal(t, []string{state.BoutGroup(3)}, lastEntry.Changed, "nothing was un-recorded")
		})
	}
}

// Record bout's mark takes no stamp, so a change to the closed bout stamped
// before Record bout and arriving after it (another device, a queued or
// debounced autosave) applies, and the bout stays recorded (review round 3,
// finding 3). Both shapes: at exhaustion (nothing appended) and with a
// pairing appended.
func TestKachinukiRecordBout_AnEarlierChangeToTheClosedBoutStillApplies(t *testing.T) {
	cases := []struct {
		name     string
		slug     string
		size     int
		closed   int // the position Record bout closes
		appended bool
	}{
		{name: "exhaustion", slug: "exh", size: 2, closed: 2, appended: false},
		{name: "pairing appended", slug: "app", size: 3, closed: 1, appended: true},
	}
	for _, tc := range cases {
		for _, bracket := range []bool{false, true} {
			name := tc.name + "/" + branchName(bracket)
			t.Run(name, func(t *testing.T) {
				eng, store, comp := setupKachinukiComp(t, "kachinuki-record-nostamp-"+tc.slug+"-"+branchName(bracket), tc.size,
					func(c *state.Competition) { c.Format = state.CompFormatMixed })
				aNames := []string{"A1", "A2", "A3"}[:tc.size]
				bNames := []string{"B1", "B2", "B3"}[:tc.size]
				rt := seedRosterTeams(t, store, comp.ID, tc.size, aNames, bNames, true, true)
				a, b := rt.a, rt.b
				bouts := []state.SubMatchResult{rosterBout(1, a[0], b[0], &a[0], false)}
				if tc.closed == 2 {
					bouts = append(bouts, rosterBout(2, a[0], b[1], &a[0], false))
				}
				t0 := serverNowMs() - 60_000
				matchID := saveRosterMatchAt(t, store, comp.ID, rt, bouts, bracket, t0)
				group := state.BoutGroup(tc.closed)

				appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
				require.NoError(t, err)
				require.Equal(t, tc.appended, appended)
				require.True(t, post.BoutLog[tc.closed-1].Recorded)
				assert.Equal(t, t0, storedGroupStamp(t, store, comp.ID, matchID, bracket, group),
					"the recorded mark does not move the closed bout's stamp")

				// A second point on the closed bout, made before Record bout.
				sent := append([]state.SubMatchResult(nil), post.BoutLog...)
				sent[tc.closed-1].IpponsA = []string{"M", "K"}
				sent[tc.closed-1].Recorded = false
				require.NoError(t, eng.RecordMatchResult(comp.ID, matchID, &state.MatchResult{
					ID: matchID, SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID,
					Status: state.MatchStatusRunning, ModifiedAt: t0 + 1_000,
					SubResults: sent,
					Changed:    []string{group}, WriteDoor: DoorScore,
				}))

				subs := storedSubs(t, store, comp.ID, matchID, bracket)
				assert.Equal(t, []string{"M", "K"}, subs[tc.closed-1].IpponsA, "the earlier change applied")
				assert.True(t, subs[tc.closed-1].Recorded, "and the bout stays recorded")
			})
		}
	}
}

// A knockout tie recorded when both teams had nobody left is fought on in
// encho on that bout: its fighters stay on in the roster read, and the write
// that starts the encho takes the recorded mark off (review round 3, finding
// 4). A write that does not raise the encho keeps it.
func TestKachinukiRecorded_EnchoOnARecordedTieClearsIt(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		name := branchName(bracket)
		t.Run(name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-recorded-encho-"+name, 2,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 2,
				[]string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
			a, b := rt.a, rt.b
			// B1 beat A1, A2 beat B1, then A2 and B2 draw: both teams are out.
			tie := rosterBout(3, a[1], b[1], nil, true)
			tie.IpponsA, tie.IpponsB = []string{"M"}, []string{"K"}
			matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{
				rosterBout(1, a[0], b[0], &b[0], false),
				rosterBout(2, a[1], b[0], &a[1], false),
				tie,
			}, bracket)
			appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			require.False(t, appended)
			require.True(t, post.BoutLog[2].Recorded)

			got, err := eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			if bracket {
				assert.Equal(t, a[1].ID, onID(got.SideA), "a knockout tie keeps A2 on: encho is the way on")
				assert.Equal(t, b[1].ID, onID(got.SideB), "and B2")
			} else {
				assert.Nil(t, got.SideA.On, "a pool draw at exhaustion takes both off")
				assert.Nil(t, got.SideB.On)
			}

			write := func(encho *state.EnchoMetadata) {
				sent := append([]state.SubMatchResult(nil), post.BoutLog...)
				sent[2].Decision = ""
				sent[2].Encho = encho
				require.NoError(t, eng.RecordMatchResult(comp.ID, matchID, &state.MatchResult{
					ID: matchID, SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID,
					Status: state.MatchStatusRunning, ModifiedAt: serverNowMs(),
					SubResults: sent,
					Changed:    []string{state.BoutGroup(3)}, WriteDoor: DoorScore,
				}))
			}
			// A write that changes the bout without raising its encho keeps it.
			write(nil)
			require.True(t, storedSubs(t, store, comp.ID, matchID, bracket)[2].Recorded)

			// The Encho tap: the bout is fought again.
			write(&state.EnchoMetadata{PeriodCount: 1})
			subs := storedSubs(t, store, comp.ID, matchID, bracket)
			assert.False(t, subs[2].Recorded, "starting encho on the recorded bout takes the mark off")
			assert.True(t, subs[2].Encho.On())

			got, err = eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			assert.Equal(t, a[1].ID, onID(got.SideA), "both fighters are on in encho")
			assert.Equal(t, b[1].ID, onID(got.SideB))
		})
	}
}

// storedSubs reads a match's bout log back from whichever store holds it.
func storedSubs(t *testing.T, store *state.Store, compID, matchID string, bracket bool) []state.SubMatchResult {
	t.Helper()
	if !bracket {
		pms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range pms {
			if m.ID == matchID {
				return m.SubResults
			}
		}
		t.Fatalf("pool match %s not found", matchID)
	}
	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bm := br.MatchByID(matchID)
	require.NotNil(t, bm)
	return bm.SubResults
}

// A side with no lineup in force reads no fighters at all: the bout log knows
// only who has fought, so a count from it would be false. Its fighter on is
// still named.
func TestKachinukiRoster_ASideWithNoLineupReadsNoFighters(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-no-lineup", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 3,
		[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, false)
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{rosterBout(1, rt.a[0], rt.b[0], nil, false)},
	}}))

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.True(t, got.SideA.LineupFound)
	assert.Equal(t, memberIDs(rt.a[1:]...), rosterIDs(got.SideA))
	assert.False(t, got.SideB.LineupFound)
	assert.Equal(t, rt.b[0].ID, onID(got.SideB))
	assert.NotNil(t, got.SideB.Remaining, "the wire carries [] rather than null")
	assert.Empty(t, got.SideB.Remaining)
}

// A lineup in force that fields nobody (the match's own empty lineup, which
// shows none) is no lineup for the read: never "last fighter" off an empty
// list.
func TestKachinukiRoster_AnEmptyLineupInForceIsNoLineup(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-empty-lineup", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 3,
		[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, true)
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{rosterBout(1, rt.a[0], rt.b[0], nil, false)},
	}}))
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: rt.bID, MatchID: "Pool A-0", Positions: map[domain.Position]string{},
	}, 3))

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.True(t, got.SideA.LineupFound)
	assert.False(t, got.SideB.LineupFound, "the empty own lineup fields nobody")
	assert.Empty(t, got.SideB.Remaining)
	assert.Equal(t, rt.b[0].ID, onID(got.SideB), "the fighter on is still named")
}

// No numbered bout yet: nobody is on, and the whole lineup is the queue.
func TestKachinukiRoster_NoBoutYet(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-no-bout", 2,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 2, []string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID, Status: state.MatchStatusRunning,
	}}))

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.Nil(t, got.SideA.On)
	assert.Nil(t, got.SideB.On)
	assert.Equal(t, memberIDs(rt.a...), rosterIDs(got.SideA))
	assert.Equal(t, memberIDs(rt.b...), rosterIDs(got.SideB))
}

func TestKachinukiRoster_Refusals(t *testing.T) {
	t.Run("not kachinuki", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "fixed", Kind: "team", TeamSize: 3}))
		_, err := eng.KachinukiRoster("fixed", "Pool A-0")
		assert.ErrorIs(t, err, ErrNotKachinuki)
	})
	t.Run("unknown match", func(t *testing.T) {
		eng, _, comp := setupKachinukiComp(t, "kachinuki-roster-unknown", 3)
		_, err := eng.KachinukiRoster(comp.ID, "Pool Z-9")
		assert.ErrorIs(t, err, ErrMatchNotFound)
	})
	t.Run("unknown competition", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		_, err := eng.KachinukiRoster("nope", "Pool A-0")
		var notFound *NotFoundError
		assert.ErrorAs(t, err, &notFound, "the handler answers it 404")
	})
	// The read answers an unreadable lineups.yaml with the error, never with
	// "no lineup": the advance alone degrades to the bout log there.
	t.Run("lineups that cannot be read", func(t *testing.T) {
		eng, store, dir := setupTestEngine(t)
		comp := &state.Competition{ID: "kachinuki-roster-unreadable", Kind: "team", TeamSize: 2,
			TeamMatchType: state.TeamMatchTypeKachinuki, Format: state.CompFormatMixed}
		require.NoError(t, store.SaveCompetition(comp))
		require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
			ID: "Pool A-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		}}))
		// A directory where the file should be: every read fails, even as root.
		require.NoError(t, os.Mkdir(filepath.Join(dir, "competitions", comp.ID, "lineups.yaml"), 0o755))
		_, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
		assert.Error(t, err)
		assert.NotErrorIs(t, err, ErrMatchNotFound)
	})
}

// The consistency pin: the advisory read and the advance build the queues
// through the same call and take retirements out through the same filter, so
// the fighter the read names as the losing side's NEXT is the one the advance
// appends once the fighter on loses, and after the append each side's fighter
// on is the appended pairing's. Run on a pool match and a bracket match, with
// lineups (the advisory names the queue) and without (the advance still pairs
// from the bout log, and the advisory reads no fighters).
func TestKachinukiRoster_NamesTheFighterTheAdvanceAppends(t *testing.T) {
	type place struct {
		name    string
		bracket bool
	}
	for _, p := range []place{{"pool", false}, {"bracket", true}} {
		t.Run(p.name+" with lineups", func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-pin-"+p.name, 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 3,
				[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, true)
			// Bout 2 live: A1 against B2, A1 losing it.
			bouts := []state.SubMatchResult{
				rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false),
				rosterBout(2, rt.a[0], rt.b[1], nil, false),
			}
			matchID := saveRosterMatch(t, store, comp.ID, rt, bouts, p.bracket)

			before, err := eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			require.NotEmpty(t, before.SideA.Remaining)
			nextA := before.SideA.Remaining[0].MemberID
			assert.Equal(t, rt.a[0].ID, onID(before.SideA))
			assert.Equal(t, rt.b[1].ID, onID(before.SideB))

			// B2 leads bout 2: the row carries a winner before Record bout, but
			// the bout is not recorded, so both fighters are still on and the
			// same fighter is next.
			bouts[1] = rosterBout(2, rt.a[0], rt.b[1], &rt.b[1], false)
			matchID = saveRosterMatch(t, store, comp.ID, rt, bouts, p.bracket)
			decided, err := eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			assert.Equal(t, rt.a[0].ID, onID(decided.SideA), "bout 2 is not recorded: A1 is still on")
			assert.Equal(t, rt.b[1].ID, onID(decided.SideB), "B2 is on")
			require.NotEmpty(t, decided.SideA.Remaining)
			assert.Equal(t, nextA, decided.SideA.Remaining[0].MemberID)
			changed, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			require.True(t, changed)
			assert.True(t, post.BoutLog[1].Recorded, "Record bout recorded bout 2 in the write that appended bout 3")
			appended := post.BoutLog[len(post.BoutLog)-1]
			assert.False(t, appended.Recorded)
			assert.Equal(t, nextA, appended.SideAMemberID, "the read named the fighter the advance sent in")
			assert.Equal(t, rt.a[1].ID, appended.SideAMemberID)

			after, err := eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			assert.Equal(t, appended.SideAMemberID, onID(after.SideA))
			assert.Equal(t, appended.SideBMemberID, onID(after.SideB), "the fighter who stayed on")
			assert.Equal(t, memberIDs(rt.a[2:]...), rosterIDs(after.SideA))
			assert.Equal(t, memberIDs(rt.b[2:]...), rosterIDs(after.SideB))
		})
		t.Run(p.name+" without lineups", func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-pin-bare-"+p.name, 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 3,
				[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, false, false)
			// The bout log's queue holds only the fighters it has seen: B1
			// beat A1, then A2 beat B2, so the advance pairs A2 (who stayed
			// on) with B1, the one B fighter seen and not retired.
			bouts := []state.SubMatchResult{
				rosterBout(1, rt.a[0], rt.b[0], &rt.b[0], false),
				rosterBout(2, rt.a[1], rt.b[1], &rt.a[1], false),
			}
			matchID := saveRosterMatch(t, store, comp.ID, rt, bouts, p.bracket)

			changed, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			require.True(t, changed, "the advance still pairs from the bout log")
			appended := post.BoutLog[len(post.BoutLog)-1]
			assert.Equal(t, rt.a[1].ID, appended.SideAMemberID)
			assert.Equal(t, rt.b[0].ID, appended.SideBMemberID)

			got, err := eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			assert.False(t, got.SideA.LineupFound)
			assert.False(t, got.SideB.LineupFound)
			assert.Empty(t, got.SideA.Remaining)
			assert.Empty(t, got.SideB.Remaining)
			assert.Equal(t, rt.a[1].ID, onID(got.SideA), "the appended pairing is on")
			assert.Equal(t, rt.b[0].ID, onID(got.SideB))
		})
	}
}

// saveRosterMatch stores one running kachinuki encounter between the two
// teams, as a pool match or as the final of a one-round bracket, and returns
// its id.
func saveRosterMatch(t *testing.T, store *state.Store, compID string, rt rosterTeams, bouts []state.SubMatchResult, bracket bool) string {
	t.Helper()
	return saveRosterMatchAt(t, store, compID, rt, bouts, bracket, 0)
}

// saveRosterMatchAt is saveRosterMatch with the match stamped at modifiedAt.
func saveRosterMatchAt(t *testing.T, store *state.Store, compID string, rt rosterTeams, bouts []state.SubMatchResult, bracket bool, modifiedAt int64) string {
	t.Helper()
	bouts = append([]state.SubMatchResult(nil), bouts...)
	if !bracket {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID,
			Status: state.MatchStatusRunning, SubResults: bouts, ModifiedAt: modifiedAt,
		}}))
		return "Pool A-0"
	}
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID: "r0-m0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID,
		Status: state.MatchStatusRunning, SubResults: bouts, ModifiedAt: modifiedAt,
	}}}}))
	return "r0-m0"
}
