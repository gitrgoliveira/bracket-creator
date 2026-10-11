package engine

import (
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

// A win retires the loser, a hikiwake retires both, and a pairing not fought
// yet retires nobody: what is left is each lineup's fighters who have not
// retired, in lineup order, the fighter on the court included.
func TestKachinukiRoster_RemainingFollowsTheBoutLog(t *testing.T) {
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
	assert.Equal(t, memberIDs(a[1:]...), rosterIDs(got.SideA), "A1 drew and retired; A2 is on")
	assert.Equal(t, memberIDs(b[2:]...), rosterIDs(got.SideB), "B1 lost, B2 drew; B3 is on")
	assert.Equal(t, "A2", got.SideA.Remaining[0].Name)
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

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.Equal(t, memberIDs(a[1]), rosterIDs(got.SideA), "Aka's Yamada lost")
	assert.Equal(t, memberIDs(b...), rosterIDs(got.SideB), "Shiro's Yamada stays on")
}

// A side with no lineup in force reads no fighters at all: the bout log knows
// only who has fought, so a count from it would be false.
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
	assert.Equal(t, memberIDs(rt.a...), rosterIDs(got.SideA))
	assert.False(t, got.SideB.LineupFound)
	assert.NotNil(t, got.SideB.Remaining, "the wire carries [] rather than null")
	assert.Empty(t, got.SideB.Remaining)
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
		assert.ErrorIs(t, err, ErrTeamMatchNotFound)
	})
	t.Run("unknown competition", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		_, err := eng.KachinukiRoster("nope", "Pool A-0")
		assert.Error(t, err)
	})
}

// The consistency pin: the advisory read and the advance take the remaining
// list from the same call, so after the advance appends the next pairing,
// each side's fighter in it is the head of that side's advisory queue. Run on
// a pool match and a bracket match, with lineups (the advisory names the
// queue) and without (the advance still pairs from the bout log, and the
// advisory reads no fighters).
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
			bouts := []state.SubMatchResult{
				rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false),
				rosterBout(2, rt.a[0], rt.b[1], &rt.b[1], false),
			}
			matchID := saveRosterMatch(t, store, comp.ID, rt, bouts, p.bracket)

			changed, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			require.True(t, changed)
			appended := post.BoutLog[len(post.BoutLog)-1]

			got, err := eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			require.NotEmpty(t, got.SideA.Remaining)
			require.NotEmpty(t, got.SideB.Remaining)
			assert.Equal(t, appended.SideAMemberID, got.SideA.Remaining[0].MemberID, "the fighter the advance sent in")
			assert.Equal(t, appended.SideBMemberID, got.SideB.Remaining[0].MemberID, "the fighter who stayed on")
			assert.Equal(t, rt.a[1].ID, appended.SideAMemberID)
			assert.Equal(t, memberIDs(rt.a[1:]...), rosterIDs(got.SideA))
			assert.Equal(t, memberIDs(rt.b[1:]...), rosterIDs(got.SideB))
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
		})
	}
}

// saveRosterMatch stores one running kachinuki encounter between the two
// teams, as a pool match or as the final of a one-round bracket, and returns
// its id.
func saveRosterMatch(t *testing.T, store *state.Store, compID string, rt rosterTeams, bouts []state.SubMatchResult, bracket bool) string {
	t.Helper()
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
