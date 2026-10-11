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

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0", 0)
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

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0", 0)
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
	// Shiro's (side B's) Yamada wins: Aka's Yamada retires, and only he does.
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{rosterBout(1, a[0], b[0], &b[0], false)},
	}}))

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0", 1)
	require.NoError(t, err)
	assert.Nil(t, got.SideA.On, "the recorded bout retired Aka's Yamada")
	assert.Equal(t, memberIDs(a[1]), rosterIDs(got.SideA), "Ito is next")
	assert.Equal(t, b[0].ID, onID(got.SideB), "Shiro's Yamada stays on")
	assert.Equal(t, memberIDs(b[1]), rosterIDs(got.SideB))
}

// A bout the sheet saw RECORDED that appended nothing (a side has nobody
// left) retires its loser: the fighter is no longer on. The same stored row
// read while the bout is live (a 1-0 lead already sets its winner) keeps both
// fighters on.
func TestKachinukiRoster_ARecordedBoutTakesItsLoserOff(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-recorded", 2,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 2,
		[]string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
	a, b := rt.a, rt.b
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			rosterBout(1, a[0], b[0], &a[0], false),
			rosterBout(2, a[0], b[1], &a[0], false),
		},
	}}))

	live, err := eng.KachinukiRoster(comp.ID, "Pool A-0", 1)
	require.NoError(t, err)
	assert.Equal(t, b[1].ID, onID(live.SideB), "bout 2 is live: B2 is still on")
	assert.Empty(t, live.SideB.Remaining)

	recorded, err := eng.KachinukiRoster(comp.ID, "Pool A-0", 2)
	require.NoError(t, err)
	assert.Nil(t, recorded.SideB.On, "bout 2 recorded: B2 lost and is off")
	assert.Empty(t, recorded.SideB.Remaining)
	assert.True(t, recorded.SideB.LineupFound)
	assert.Equal(t, a[0].ID, onID(recorded.SideA), "the winner stays on")
	assert.Equal(t, memberIDs(a[1]), rosterIDs(recorded.SideA))

	// The advance agrees: with nobody left on B it appends nothing.
	changed, _, err := eng.MaybeAdvanceKachinuki(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.False(t, changed)
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

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0", 0)
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

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0", 0)
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

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0", 0)
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
		_, err := eng.KachinukiRoster("fixed", "Pool A-0", 0)
		assert.ErrorIs(t, err, ErrNotKachinuki)
	})
	t.Run("unknown match", func(t *testing.T) {
		eng, _, comp := setupKachinukiComp(t, "kachinuki-roster-unknown", 3)
		_, err := eng.KachinukiRoster(comp.ID, "Pool Z-9", 0)
		assert.ErrorIs(t, err, ErrMatchNotFound)
	})
	t.Run("unknown competition", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		_, err := eng.KachinukiRoster("nope", "Pool A-0", 0)
		assert.Error(t, err)
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
		_, err := eng.KachinukiRoster(comp.ID, "Pool A-0", 0)
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

			before, err := eng.KachinukiRoster(comp.ID, matchID, 0)
			require.NoError(t, err)
			require.NotEmpty(t, before.SideA.Remaining)
			nextA := before.SideA.Remaining[0].MemberID
			assert.Equal(t, rt.a[0].ID, onID(before.SideA))
			assert.Equal(t, rt.b[1].ID, onID(before.SideB))

			// B2 wins bout 2 and the bout is recorded.
			bouts[1] = rosterBout(2, rt.a[0], rt.b[1], &rt.b[1], false)
			matchID = saveRosterMatch(t, store, comp.ID, rt, bouts, p.bracket)
			changed, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			require.True(t, changed)
			appended := post.BoutLog[len(post.BoutLog)-1]
			assert.Equal(t, nextA, appended.SideAMemberID, "the read named the fighter the advance sent in")
			assert.Equal(t, rt.a[1].ID, appended.SideAMemberID)

			after, err := eng.KachinukiRoster(comp.ID, matchID, 2)
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

			got, err := eng.KachinukiRoster(comp.ID, matchID, 2)
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
	bouts = append([]state.SubMatchResult(nil), bouts...)
	if !bracket {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID,
			Status: state.MatchStatusRunning, SubResults: bouts,
		}}))
		return "Pool A-0"
	}
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID: "r0-m0", SideA: "Ryu", SideAID: rt.aID, SideB: "Tora", SideBID: rt.bID,
		Status: state.MatchStatusRunning, SubResults: bouts,
	}}}}))
	return "r0-m0"
}
