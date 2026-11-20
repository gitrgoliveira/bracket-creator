package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeTeamMembersUnreadable puts a directory where the competition's
// team-members.yaml should be, so every read of it fails (even as root).
func makeTeamMembersUnreadable(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	path := filepath.Join(store.GetFolder(), "competitions", compID, "team-members.yaml")
	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, os.Mkdir(path, 0o755))
}

// numberedTeams saves a kachinuki competition's two teams, Ryu (side A) and
// Tora (side B), whose members the store seeds by number (team size plus two
// reserves, none named), as every read after the save finds them.
func numberedTeams(t *testing.T, store *state.Store, compID string) (aID, bID string) {
	t.Helper()
	aID, bID = helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aID, Name: "Ryu", Dojo: "DojoR"},
		{ID: bID, Name: "Tora", Dojo: "DojoT"},
	}))
	// The members are seeded on the competition's next load (the save's own
	// seeding pass reads the roster as it was before the save).
	_, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	return aID, bID
}

// numberBout is a numbered bout row between two members picked by number (no
// names), with the winner recorded as the client records a nameless winner:
// its team's name and the member id.
func numberBout(pos int, aMember, bMember, winnerTeam, winnerMember string) state.SubMatchResult {
	return state.SubMatchResult{
		Position: pos, SideAMemberID: aMember, SideBMemberID: bMember,
		Winner: winnerTeam, WinnerMemberID: winnerMember, Decision: "fought",
	}
}

// A match that has started with no lineup of its own never follows the lineup
// the team carries (K1 (c)); it fields its members by number over an EMPTY
// lineup (Q4 (b)). The same match before it starts fields the carried lineup.
func TestKachinukiFielded_AStartedMatchWithNothingOfItsOwnFieldsByNumber(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-fielded-started", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	aID, bID := numberedTeams(t, store, comp.ID)
	a := teamMemberIDs(t, store, comp.ID, aID, 3)
	b := teamMemberIDs(t, store, comp.ID, bID, 3)
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: aID, Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1", domain.PositionNumbered(2): "R-2", domain.PositionNumbered(3): "R-3",
		},
	}, 3))

	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusScheduled,
	}}))
	q := storedKachinukiQueues(t, eng, comp.ID, "Pool A-0")
	assert.Equal(t, []string{"R-1", "R-2", "R-3"}, queueNames(q.queueA), "before the start, Ryu fields the lineup it carries")
	assert.Equal(t, b, queueMemberIDs(q.queueB), "Tora has no lineup: members 1 to 3 by number")

	// Started, with a bout fought by number: A's member 1 loses to B's member 1.
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{numberBout(1, a[0], b[0], "Tora", b[0])},
	}}))
	q = storedKachinukiQueues(t, eng, comp.ID, "Pool A-0")
	assert.Equal(t, a, queueMemberIDs(q.queueA), "started with nothing of its own: number order, not the carried lineup")
	assert.True(t, q.queueA.fromLineup(), "a side fielded by number reads as fielding members")

	changed, post, err := eng.MaybeAdvanceKachinuki(comp.ID, "Pool A-0")
	require.NoError(t, err)
	require.True(t, changed, "Record bout appends the next member by number")
	require.Len(t, post.BoutLog, 2)
	assert.Equal(t, a[1], post.BoutLog[1].SideAMemberID, "Ryu's member 2 comes in")
	assert.Equal(t, b[0], post.BoutLog[1].SideBMemberID, "Tora's member 1 stays on")
}

// A saved lineup's empty places are filled by member number (Q2): a lineup
// naming only member 3 at senpo fields member 3, then members 1, 2, 4 and 5,
// so the queue is the whole team, not one fighter.
func TestKachinukiFielded_AShortLineupQueuesItsEmptyPlacesByNumber(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-fielded-short", 5,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	aID, bID := numberedTeams(t, store, comp.ID)
	a := teamMemberIDs(t, store, comp.ID, aID, 5)
	b := teamMemberIDs(t, store, comp.ID, bID, 5)
	sano, err := store.RenameTeamMember(comp.ID, aID, a[2], "Sano")
	require.NoError(t, err)
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: aID, MatchID: "Pool A-0",
		Positions: map[domain.Position]string{domain.PosSenpo: "Sano"},
		MemberIDs: map[domain.Position]string{domain.PosSenpo: sano.ID},
	}, 5))
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{numberBout(1, a[2], b[0], "Tora", b[0])},
	}}))

	q := storedKachinukiQueues(t, eng, comp.ID, "Pool A-0")
	assert.Equal(t, []string{a[2], a[0], a[1], a[3], a[4]}, queueMemberIDs(q.queueA))

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.True(t, got.SideA.LineupFound)
	assert.Equal(t, []string{a[0], a[1], a[3], a[4]}, rosterIDs(got.SideA), "Sano is on; four are left")
	assert.Empty(t, got.Warning)
}

// The members are read lazily: a lineup that names every place reads nothing,
// and one reader reads the file once however many sides need it.
func TestKachinukiFielded_TheMembersAreReadOnlyWhenAPlaceIsEmpty(t *testing.T) {
	full := domain.TeamLineup{TeamID: "a", MatchID: "M", Positions: map[domain.Position]string{
		domain.PositionNumbered(1): "A1", domain.PositionNumbered(2): "A2",
	}}
	rule := kachinukiLineups{lineupRule: ruleOver(map[string]domain.TeamLineup{"a": full}, nil, nil)}
	loads := 0
	load := func(string) (map[string][]domain.TeamMember, error) {
		loads++
		return map[string][]domain.TeamMember{
			"b": {{ID: "b1", Index: 1}, {ID: "b2", Index: 2}},
		}, nil
	}
	m := &state.MatchResult{ID: "M", Status: state.MatchStatusRunning}

	members := readKachinukiMembers(load, "c", "test")
	got := kachinukiFielded(rule, members, 2, m, "a")
	assert.Equal(t, 0, loads, "a lineup naming every place reads no members")
	assert.Equal(t, full.Positions, got.Positions)

	b := kachinukiFielded(rule, members, 2, m, "b")
	_ = kachinukiFielded(rule, members, 2, m, "b")
	assert.Equal(t, 1, loads, "read once per reader")
	assert.Equal(t, map[domain.Position]string{domain.PositionNumbered(1): "b1", domain.PositionNumbered(2): "b2"}, b.MemberIDs)

	assert.Empty(t, kachinukiFielded(rule, members, 2, m, "").OrderedMembers(2), "a side with no id fields nobody")
	assert.Empty(t, members.warning())
}

// team-members.yaml that cannot be read: the advance and the roster read carry
// on without number order (Q5 (a)), each logs an ERROR naming the file, and the
// roster read answers the warning the sheet shows.
func TestKachinukiFielded_UnreadableMembersLogAndFillNothing(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-fielded-members-unread", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	aID, bID := numberedTeams(t, store, comp.ID)
	a := teamMemberIDs(t, store, comp.ID, aID, 3)
	b := teamMemberIDs(t, store, comp.ID, bID, 3)
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			numberBout(1, a[0], b[0], "Ryu", a[0]),
			numberBout(2, a[0], b[1], "Tora", b[1]),
		},
	}}))
	makeTeamMembersUnreadable(t, store, comp.ID)

	var got KachinukiRoster
	var err error
	logs := captureLog(t, func() { got, err = eng.KachinukiRoster(comp.ID, "Pool A-0") })
	require.NoError(t, err, "an unreadable team-members.yaml does not fail the read")
	assert.Equal(t, kachinukiMembersUnreadWarning, got.Warning)
	assert.False(t, got.SideA.LineupFound, "no lineup and no members: the side fields nobody")
	assert.False(t, got.SideB.LineupFound)
	assert.Equal(t, a[0], onID(got.SideA), "the fighters on are still named")
	assert.Equal(t, b[1], onID(got.SideB))
	assert.Contains(t, logs, "engine: ERROR: engine.KachinukiRoster compId="+comp.ID+": team-members.yaml load error:")
	assert.Contains(t, logs, "fighters are not seated by member number")

	// Ryu fields nobody, so its queue is empty: A's member 1 lost bout 2 and
	// Record bout appends nothing, and fails nothing.
	var changed bool
	logs = captureLog(t, func() { changed, _, err = eng.MaybeAdvanceKachinuki(comp.ID, "Pool A-0") })
	require.NoError(t, err, "an unreadable team-members.yaml does not fail the advance")
	assert.False(t, changed)
	assert.Contains(t, logs, "engine: ERROR: engine.MaybeAdvanceKachinuki compId="+comp.ID+": team-members.yaml load error:")
}

// A read that never needed the members (every lineup place named) reports no
// warning even when the file is broken: nothing was left unseated.
func TestKachinukiFielded_AFullLineupNeedsNoMembersAndWarnsOfNothing(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-fielded-full-unread", 2,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	rt := seedRosterTeams(t, store, comp.ID, 2, []string{"A1", "A2"}, []string{"B1", "B2"}, true, true)
	matchID := saveRosterMatch(t, store, comp.ID, rt, []state.SubMatchResult{rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false)}, false)
	makeTeamMembersUnreadable(t, store, comp.ID)

	got, err := eng.KachinukiRoster(comp.ID, matchID)
	require.NoError(t, err)
	assert.Empty(t, got.Warning)
	assert.Equal(t, memberIDs(rt.b[1]), rosterIDs(got.SideB))
}

// lineups.yaml that cannot be read: a lineup may exist that was not read, so
// nothing is filled by number over it. Each side fields nobody, so its queue is
// empty: Record bout marks the bout recorded and appends nothing, and the
// members are not even read.
func TestKachinukiFielded_UnreadableLineupsFillNothing(t *testing.T) {
	m := &state.MatchResult{ID: "M", Status: state.MatchStatusScheduled}
	loads := 0
	members := readKachinukiMembers(func(string) (map[string][]domain.TeamMember, error) {
		loads++
		return map[string][]domain.TeamMember{"a": {{ID: "a1", Index: 1}}}, nil
	}, "c", "test")
	got := kachinukiFielded(kachinukiLineups{lineupRule: &lineupRule{}, unread: true}, members, 1, m, "a")
	assert.Empty(t, got.OrderedMembers(1))
	assert.Equal(t, 0, loads)

	eng, store, comp := setupKachinukiComp(t, "kachinuki-fielded-lineups-unread", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	aID, bID := numberedTeams(t, store, comp.ID)
	a := teamMemberIDs(t, store, comp.ID, aID, 3)
	b := teamMemberIDs(t, store, comp.ID, bID, 3)
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{numberBout(1, a[0], b[0], "Ryu", a[0])},
	}}))
	require.NoError(t, os.WriteFile(
		filepath.Join(store.GetFolder(), "competitions", comp.ID, "lineups.yaml"),
		[]byte("lineups: [this is: not: valid yaml"), 0o600))

	q := storedKachinukiQueues(t, eng, comp.ID, "Pool A-0")
	assert.False(t, q.queueA.fromLineup(), "no number order over lineups that could not be read")
	assert.False(t, q.queueB.fromLineup())
	assert.Empty(t, q.queueA.fighters, "a side that fields nobody has an empty queue")
	assert.Empty(t, q.queueB.fighters)

	changed, post, err := eng.MaybeAdvanceKachinuki(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.False(t, changed, "an empty queue appends nothing")
	require.NotNil(t, post, "Record bout still writes: it marks the bout recorded")
	require.Len(t, post.BoutLog, 1)
	assert.True(t, post.BoutLog[0].Recorded)
}

// A side that fields nobody has an EMPTY queue (operator decision K2): the bout
// log is never a queue. Record bout marks the bout it closes recorded and
// appends nothing, a second tap writes nothing, and the roster read lists
// nobody remaining and never calls the side exhausted. The fixture fields
// nobody the way production can: no lineup saved and team-members.yaml
// unreadable.
func TestKachinukiAdvance_ASideThatFieldsNobodyRecordsTheBoutAndAppendsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		bout func(a, b string) state.SubMatchResult
	}{
		{"a win", func(a, b string) state.SubMatchResult { return numberBout(1, a, b, "Tora", b) }},
		{"a draw", func(a, b string) state.SubMatchResult {
			return state.SubMatchResult{Position: 1, SideAMemberID: a, SideBMemberID: b, Decision: string(domain.DecisionHikiwake)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-fields-nobody", 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			aID, bID := numberedTeams(t, store, comp.ID)
			a := teamMemberIDs(t, store, comp.ID, aID, 3)
			b := teamMemberIDs(t, store, comp.ID, bID, 3)
			require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
				ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{tc.bout(a[0], b[0])},
			}}))
			makeTeamMembersUnreadable(t, store, comp.ID)

			q := storedKachinukiQueues(t, eng, comp.ID, "Pool A-0")
			assert.Empty(t, q.queueA.fighters, "Ryu fields nobody: an empty queue, not the bout log's fighters")
			assert.Empty(t, q.queueB.fighters, "Tora fields nobody: an empty queue, not the bout log's fighters")

			changed, post, err := eng.MaybeAdvanceKachinuki(comp.ID, "Pool A-0")
			require.NoError(t, err)
			assert.False(t, changed, "nothing is appended")
			require.NotNil(t, post, "the bout is marked recorded")
			require.Len(t, post.BoutLog, 1, "the log holds only the bout it closed")
			assert.True(t, post.BoutLog[0].Recorded)

			history, err := store.LoadMatchHistory(comp.ID, "Pool A-0")
			require.NoError(t, err)
			require.Len(t, history, 1)
			assert.Equal(t, doorKachinukiRecord, history[0].Door)

			changed, post, err = eng.MaybeAdvanceKachinuki(comp.ID, "Pool A-0")
			require.NoError(t, err)
			assert.False(t, changed)
			assert.Nil(t, post, "a second tap writes nothing")
			history, err = store.LoadMatchHistory(comp.ID, "Pool A-0")
			require.NoError(t, err)
			assert.Len(t, history, 1, "and adds no history line")

			got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
			require.NoError(t, err)
			for _, side := range []KachinukiRosterSide{got.SideA, got.SideB} {
				assert.False(t, side.LineupFound)
				assert.Empty(t, side.Remaining, "nobody remaining is listed")
				assert.False(t, side.Exhausted, "a side that fields nobody is never called exhausted")
			}
			assert.False(t, got.NextPairing, "a second tap would append nothing")
		})
	}
}

// A lineups.yaml that cannot be read is logged as an ERROR naming the file and
// the caller, the same shape as the team-members line (plan section 3), and the
// rule answers no lineup marked unread.
func TestReadKachinukiLineups_AnUnreadableFileIsAnErrorNamingIt(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-lineups-unread-log", 3)
	require.NoError(t, os.WriteFile(
		filepath.Join(store.GetFolder(), "competitions", comp.ID, "lineups.yaml"),
		[]byte("lineups: [this is: not: valid yaml"), 0o600))
	var rule kachinukiLineups
	logs := captureLog(t, func() {
		rule = eng.readKachinukiLineups("caller-z", comp.ID, false, nil, nil)
	})
	assert.True(t, rule.lineupsUnread())
	assert.False(t, rule.inForce("a", "M").Found, "no lineup is resolved")
	assert.Equal(t, 1, strings.Count(logs, "engine: ERROR: caller-z compId="+comp.ID+": lineups.yaml load error:"), logs)
	assert.Contains(t, logs, "; resolving no lineup, so fighters are not seated by member number")
}

// kachinukiMembers on a load error reports unread and answers no members, and
// logs once however often it is asked.
func TestKachinukiMembers_ALoadErrorIsLoggedOnce(t *testing.T) {
	loads := 0
	members := readKachinukiMembers(func(string) (map[string][]domain.TeamMember, error) {
		loads++
		return nil, errors.New("disk on fire")
	}, "comp-x", "caller-y")
	var okA, okB bool
	logs := captureLog(t, func() {
		_, okA = members.forTeam("a")
		_, okB = members.forTeam("b")
	})
	assert.False(t, okA)
	assert.False(t, okB)
	assert.Equal(t, 1, loads)
	assert.Equal(t, kachinukiMembersUnreadWarning, members.warning())
	assert.Equal(t, 1, strings.Count(logs, "engine: ERROR: caller-y compId=comp-x: team-members.yaml load error: disk on fire; fighters are not seated by member number"))

	var none *kachinukiMembers
	_, ok := none.forTeam("a")
	assert.False(t, ok, "a nil reader knows no members")
	assert.Empty(t, none.warning())
}

// The two sentences the roster read warns with, word for word: the sheet shows
// them as toasts and the API documents them, and both end telling the operator
// what to do (plan section 3, operator decisions Q5 (a) and L1).
func TestKachinukiWarnings_TheTwoSentences(t *testing.T) {
	assert.Equal(t, "Team members could not be read, so fighters are not seated by member number. Add each bout's pairing by hand.",
		kachinukiMembersUnreadWarning)
	assert.Equal(t, "The lineups could not be read, so who fights next is not worked out. Add each bout's pairing by hand.",
		kachinukiLineupsUnreadWarning)
}

// Number order seats members 1 to team size (Q1 (a)), worked out at every read:
// a competition whose team size was raised after its members were seeded (3 to
// 5) fields members 4 and 5 by number, the two reserves the seeding made at
// size 3, so a team with no lineup fields members 1 to 5. The member ids are
// taken before the raise, so the test proves the reserves are fielded, not
// members the raise seeded.
func TestKachinukiFielded_ARaisedTeamSizeFieldsTheReservesByNumber(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-fielded-size-raised", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	aID, bID := numberedTeams(t, store, comp.ID)
	a := teamMemberIDs(t, store, comp.ID, aID, 5)
	b := teamMemberIDs(t, store, comp.ID, bID, 5)
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{numberBout(1, a[0], b[0], "Ryu", a[0])},
	}}))

	q := storedKachinukiQueues(t, eng, comp.ID, "Pool A-0")
	assert.Equal(t, a[:3], queueMemberIDs(q.queueA), "at size 3, members 1 to 3; 4 and 5 are reserves")
	assert.Equal(t, b[:3], queueMemberIDs(q.queueB))

	comp.TeamSize = 5
	require.NoError(t, store.SaveCompetition(comp))

	q = storedKachinukiQueues(t, eng, comp.ID, "Pool A-0")
	assert.Equal(t, a, queueMemberIDs(q.queueA), "at size 5, members 1 to 5 by number")
	assert.Equal(t, b, queueMemberIDs(q.queueB))

	got, err := eng.KachinukiRoster(comp.ID, "Pool A-0")
	require.NoError(t, err)
	assert.Equal(t, a[1:], rosterIDs(got.SideA), "the read lists the old reserves behind the fighter on")
	assert.Equal(t, b[1:], rosterIDs(got.SideB))
	assert.Empty(t, got.Warning)
}

// A lineup saved full at size 3 has the place a raise to 4 adds filled by
// member 4, a reserve at size 3. (A raise to 5 renames every place to the FIK
// names, so a lineup saved under numbered places is no lineup there; that is
// the position names, not the fill.)
func TestKachinukiFielded_ARaisedTeamSizeFillsTheNewPlaceOfAFullLineup(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-fielded-size-raised-lineup", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	aID, bID := numberedTeams(t, store, comp.ID)
	a := teamMemberIDs(t, store, comp.ID, aID, 4)
	b := teamMemberIDs(t, store, comp.ID, bID, 4)
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: aID, MatchID: "Pool A-0",
		MemberIDs: map[domain.Position]string{
			domain.PositionNumbered(1): a[2], domain.PositionNumbered(2): a[0], domain.PositionNumbered(3): a[1],
		},
	}, 3))
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{numberBout(1, a[2], b[0], "Ryu", a[2])},
	}}))
	q := storedKachinukiQueues(t, eng, comp.ID, "Pool A-0")
	require.Equal(t, []string{a[2], a[0], a[1]}, queueMemberIDs(q.queueA), "at size 3 the full lineup is the queue")

	comp.TeamSize = 4
	require.NoError(t, store.SaveCompetition(comp))

	q = storedKachinukiQueues(t, eng, comp.ID, "Pool A-0")
	assert.Equal(t, []string{a[2], a[0], a[1], a[3]}, queueMemberIDs(q.queueA), "the new place 4 takes member 4")
	assert.Equal(t, b, queueMemberIDs(q.queueB), "no lineup: members 1 to 4")
}
