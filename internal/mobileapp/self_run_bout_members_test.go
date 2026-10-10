package mobileapp

// A participant's fighters are their team's, and a bout's winner is one of its
// two fighters. The member judge (landedMembersRefusal) refuses a score write
// that LANDS, on a numbered bout row, a side member id the side's team does not
// hold (400 team_member_not_in_team), or a winnerMemberId that is not one of the
// row's two side ids (400 winner_member_not_in_bout), and the refused write
// leaves the match and its history untouched. Landed means the stored row now
// holds it and did not before, at the same position: an id the row already held
// is never judged, so a row that a correction left with another team's fighter,
// or a stale winner id, does not refuse every later write. The organiser is not
// judged here (the lineup's always-editable rule), and a members file that
// cannot be read refuses the write with a terminal 409 only when a row actually
// needs it.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fighterNotOnTeamBody = "The fighter chosen is not on this team. Pick again from the list."
	winnerNotInBoutBody  = "The winner recorded is not one of the two fighters in this bout."
)

// boutIDs are the member ids a numbered row carries; an empty one is left off
// the row, as the editor leaves it off.
type boutIDs struct{ a, b, winner string }

// bouts sends the team sheet with the member ids it names on its numbered rows.
func (f *repBoutFixture) bouts(password string, at int64, ids map[int]boutIDs) *httptest.ResponseRecorder {
	sheet := scoreSheet(state.MatchStatusRunning, "", at, nil)
	rows := sheet["subResults"].([]any)
	for pos, id := range ids {
		row := rows[pos-1].(map[string]any)
		for key, v := range map[string]string{"sideAMemberId": id.a, "sideBMemberId": id.b, "winnerMemberId": id.winner} {
			if v != "" {
				row[key] = v
			}
		}
	}
	return f.send(http.MethodPut, repBoutMatchPath+"/score", password, sheet)
}

// teamMembers are the two teams' members, seeded.
func (f *repBoutFixture) teamMembers(t *testing.T) (a, b []domain.TeamMember) {
	t.Helper()
	f.store.EnsureLegacyUpgraded("c1")
	squads, err := f.store.LoadSquads("c1")
	require.NoError(t, err)
	a, b = squads[repBoutTeamAID], squads[repBoutTeamBID]
	require.GreaterOrEqual(t, len(a), 2, "team A is seeded with members")
	require.GreaterOrEqual(t, len(b), 2, "team B is seeded with members")
	return a, b
}

func (f *repBoutFixture) storedBout(t *testing.T, position int) state.SubMatchResult {
	t.Helper()
	for _, sub := range storedB1(t, f.store, "c1").SubResults {
		if sub.Position == position {
			return sub
		}
	}
	t.Fatalf("B1 has no bout %d", position)
	return state.SubMatchResult{}
}

func (f *repBoutFixture) corruptTeamMembers(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile(
		filepath.Join(f.store.GetFolder(), "competitions", "c1", "team-members.yaml"),
		[]byte("not: [valid yaml"), 0o600))
}

func TestSelfRun_NumberedRowFighterMustBeOnTheirTeam(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, b := f.teamMembers(t)

	for name, ids := range map[string]boutIDs{
		"side A fielding the other team's member": {a: b[0].ID},
		"side B fielding the other team's member": {b: a[0].ID},
		"an id no team holds":                     {a: "00000000-0000-4000-8000-000000000000"},
		"one good fighter and one foreign one":    {a: a[0].ID, b: a[1].ID},
	} {
		t.Run(name, func(t *testing.T) {
			w := f.bouts("", f.now+100, map[int]boutIDs{1: ids})
			requireRefusal(t, w, http.StatusBadRequest, "team_member_not_in_team", fighterNotOnTeamBody)
			bout := f.storedBout(t, 1)
			assert.Empty(t, bout.SideAMemberID, "a refused write writes nothing")
			assert.Empty(t, bout.SideBMemberID)
		})
	}
}

func TestSelfRun_NumberedRowFighterOfOwnTeamPasses(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, b := f.teamMembers(t)

	w := f.bouts("", f.now+100, map[int]boutIDs{1: {a: a[0].ID, b: b[0].ID, winner: a[0].ID}, 2: {a: a[1].ID, b: b[1].ID, winner: b[1].ID}})
	require.Equal(t, http.StatusOK, w.Code, "each side's own member, and a winner who is one of the two: %s", w.Body.String())
	assert.Equal(t, a[0].ID, f.storedBout(t, 1).SideAMemberID)
	assert.Equal(t, b[0].ID, f.storedBout(t, 1).SideBMemberID)
	assert.Equal(t, b[1].ID, f.storedBout(t, 2).SideBMemberID)
}

// An id the stored row already holds is not what the write introduces, so it is
// not judged, and the members file is not even read (it is corrupt here).
func TestSelfRun_NumberedRowInheritedFighterIsNotJudged(t *testing.T) {
	f := newRepBoutFixture(t, true)
	_, b := f.teamMembers(t)
	f.setB1(t, func(bm *state.BracketMatch) {
		bm.SubResults[0].SideAMemberID = b[0].ID // a correction seated another team on side A
	})
	f.corruptTeamMembers(t)

	w := f.bouts("", f.now+100, map[int]boutIDs{1: {a: b[0].ID}})
	require.Equal(t, http.StatusOK, w.Code, "the echo of the stored id is kept: %s", w.Body.String())
	assert.Equal(t, b[0].ID, f.storedBout(t, 1).SideAMemberID)
}

func TestSelfRun_WinnerMemberMustBeAFighterOfTheBout(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, b := f.teamMembers(t)

	for name, ids := range map[string]boutIDs{
		"a member who is not in the bout":          {a: a[0].ID, b: b[0].ID, winner: a[1].ID},
		"an id no team holds":                      {a: a[0].ID, b: b[0].ID, winner: "00000000-0000-4000-8000-000000000000"},
		"a winner on a row that names no fighters": {winner: a[0].ID},
	} {
		t.Run(name, func(t *testing.T) {
			w := f.bouts("", f.now+100, map[int]boutIDs{1: ids})
			requireRefusal(t, w, http.StatusBadRequest, "winner_member_not_in_bout", winnerNotInBoutBody)
			assert.Empty(t, f.storedBout(t, 1).WinnerMemberID, "a refused write writes nothing")
		})
	}
}

// A winner id the stored row already holds is not judged, whoever it names: a
// legacy row, a correction that seated another team and a renamed member leave
// such ids behind, and refusing them would refuse every later write.
func TestSelfRun_InheritedWinnerMemberIsNotJudged(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, _ := f.teamMembers(t)
	f.setB1(t, func(bm *state.BracketMatch) {
		bm.SubResults[0].WinnerMemberID = a[1].ID // names neither of the row's (empty) sides
	})
	f.corruptTeamMembers(t)

	w := f.bouts("", f.now+100, map[int]boutIDs{1: {winner: a[1].ID}})
	require.Equal(t, http.StatusOK, w.Code, "the stale winner id is echoed, not introduced: %s", w.Body.String())
}

// A row silent about a side keeps the stored row's id there (the kachinuki
// merge's inherit), so a winner who is the stored fighter of that side is one
// of the bout's two fighters and passes.
func TestSelfRun_WinnerMemberMayBeTheStoredFighterOfASilentSide(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, _ := f.teamMembers(t)
	f.setB1(t, func(bm *state.BracketMatch) {
		bm.SubResults[0].SideAMemberID = a[0].ID
	})

	w := f.bouts("", f.now+100, map[int]boutIDs{1: {winner: a[0].ID}})
	require.Equal(t, http.StatusOK, w.Code, "the row names no side, the stored row's side A is the winner: %s", w.Body.String())
}

// The organiser's write is not judged on numbered rows (as the lineup PUT is
// always editable for the organiser).
func TestOrganiser_NumberedRowsAreNotJudged(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, b := f.teamMembers(t)

	w := f.bouts(organiserPassword, f.now+100, map[int]boutIDs{1: {a: b[0].ID, b: a[0].ID, winner: a[1].ID}})
	require.Equal(t, http.StatusOK, w.Code, "the organiser may seat any member: %s", w.Body.String())
	assert.Equal(t, b[0].ID, f.storedBout(t, 1).SideAMemberID)
}

// A write whose ids all equal the stored row's reads no members at all.
func TestSelfRun_AnEchoedIdReadsNoTeamMembers(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, _ := f.teamMembers(t)
	w := f.bouts("", f.now+100, map[int]boutIDs{1: {a: a[0].ID}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	f.corruptTeamMembers(t)

	w = f.bouts("", f.now+200, map[int]boutIDs{1: {a: a[0].ID}})
	require.Equal(t, http.StatusOK, w.Code, "nothing is introduced, so nothing is read: %s", w.Body.String())
}

// A fighter that needs the members file while it cannot be read is refused
// terminally, for a numbered row as for a representative pick.
func TestSelfRun_UnreadableTeamMembersRefusesANumberedRowTerminally(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, _ := f.teamMembers(t)
	f.corruptTeamMembers(t)
	before := f.storedState(t)

	requireTeamMembersUnreadable(t, f.bouts("", f.now+100, map[int]boutIDs{1: {a: a[0].ID}}))
	assert.Equal(t, before, f.storedState(t), "a refused write writes nothing, not even its history line")
}

// The participant's lineup PUT is refused the same way when the members file
// cannot be read (it was a 500 the offline queue retried forever).
func TestSelfRun_UnreadableTeamMembersRefusesTheLineupTerminally(t *testing.T) {
	f := newTeamWritesFixture(t, true)
	require.NoError(t, os.WriteFile(
		filepath.Join(f.store.GetFolder(), "competitions", "c1", "team-members.yaml"),
		[]byte("not: [valid yaml"), 0o600))

	w := f.send(http.MethodPut, f.lineupPath("PoolA-0"), "", senpo("Mei Ito", f.blankA))
	requireTeamMembersUnreadable(t, w)
	_, ok := f.savedLineup(t, "PoolA-0")
	assert.False(t, ok, "a refused lineup writes nothing")
}

// A numbered row is judged on what LANDS, as a representative pick is
// (landedMembersRefusal): a row whose stored counterpart is NEWER is held and
// kept in the match's history, so the fighter it names is never written, and
// refusing the write for it would refuse the whole write, terminally, for a
// fighter that was not written, losing the groups of the write that apply. Here
// the stored bout 1 names fighter Y, stamped T; the write names another team's
// member on it under T-1, beside bouts 2 and 3 and the scoreline, which apply.
func TestSelfRun_AHeldNumberedRowIsNotJudged(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, b := f.teamMembers(t)
	f.setB1(t, func(bm *state.BracketMatch) {
		bm.SubResults[0].SideAMemberID = a[0].ID
		bm.ModifiedAt = f.now
		bm.GroupStamps = map[string]int64{
			state.BoutGroup(1):  f.now,
			state.GroupPoints:   f.now - 10_000,
			state.BoutGroup(2):  f.now - 10_000,
			state.BoutGroup(3):  f.now - 10_000,
			state.GroupResult:   f.now - 10_000,
			state.GroupRepPickA: f.now - 10_000,
		}
	})

	w := f.bouts("", f.now-1, map[int]boutIDs{1: {a: b[0].ID}})
	require.Equal(t, http.StatusOK, w.Code, "the held row is kept in the history, not refused: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), `"heldGroups":["bout:1"]`)
	assert.Equal(t, a[0].ID, f.storedBout(t, 1).SideAMemberID, "the newer stored fighter stands")
}

// The pin beside it: the same write stamped AFTER the stored row applies the row,
// so its fighter is judged and refused.
func TestSelfRun_ANewerNumberedRowIsStillJudged(t *testing.T) {
	f := newRepBoutFixture(t, true)
	a, b := f.teamMembers(t)
	f.setB1(t, func(bm *state.BracketMatch) {
		bm.SubResults[0].SideAMemberID = a[0].ID
		bm.ModifiedAt = f.now
		bm.GroupStamps = map[string]int64{state.BoutGroup(1): f.now}
	})

	w := f.bouts("", f.now+1, map[int]boutIDs{1: {a: b[0].ID}})
	requireRefusal(t, w, http.StatusBadRequest, "team_member_not_in_team", fighterNotOnTeamBody)
	assert.Equal(t, a[0].ID, f.storedBout(t, 1).SideAMemberID, "a refused write writes nothing")
}

// A row the write does not name in `changed` is not written either, so its
// fighter is not judged.
func TestSelfRun_ANumberedRowTheWriteDoesNotNameIsNotJudged(t *testing.T) {
	f := newRepBoutFixture(t, true)
	_, b := f.teamMembers(t)

	sheet := scoreSheet(state.MatchStatusRunning, "", f.now+100, nil)
	sheet["subResults"].([]any)[0].(map[string]any)["sideAMemberId"] = b[0].ID
	sheet["changed"] = []string{state.GroupPoints}
	w := f.send(http.MethodPut, repBoutMatchPath+"/score", "", sheet)
	require.Equal(t, http.StatusOK, w.Code, "bout 1 is not among the groups the write changes: %s", w.Body.String())
	assert.Empty(t, f.storedBout(t, 1).SideAMemberID, "and it is not written")
}

// A fighter is judged on what the write LANDS, not on what its payload says it
// changes. A kachinuki write is rewritten by the engine before the merge (the
// bout log is merged by position and the rows' member ids are filled in), so a
// row the payload does not name can still land: here `changed` names the match's
// points alone, and the engine writes bout 1 with the foreign fighter anyway.
// The write is refused and leaves the match and its history untouched.
func TestSelfRun_AKachinukiRowTheEngineRewritesIsJudgedOnWhatLands(t *testing.T) {
	f := newRepBoutFixture(t, true)
	require.NoError(t, f.store.SaveCompetition(&state.Competition{
		ID: "c1", Name: "Teams", Kind: "team", Format: state.CompFormatKnockout,
		TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
	}))
	_, b := f.teamMembers(t)
	before := f.storedState(t)

	sheet := scoreSheet(state.MatchStatusRunning, "", f.now+100, nil)
	row := sheet["subResults"].([]any)[0].(map[string]any)
	row["sideA"], row["sideB"], row["winner"] = "Alice", "Bob", "Alice"
	row["sideAMemberId"] = b[0].ID
	sheet["changed"] = []string{state.GroupPoints}
	w := f.send(http.MethodPut, repBoutMatchPath+"/score", "", sheet)

	requireRefusal(t, w, http.StatusBadRequest, "team_member_not_in_team", fighterNotOnTeamBody)
	assert.Equal(t, before, f.storedState(t), "a refused write leaves the match and its history as they were")
}

// The pin beside it for the other home of a match: a pool match is read back
// from the staged pool-matches.csv, not bracket.json, and is judged the same way.
func TestSelfRun_AKachinukiPoolRowTheEngineRewritesIsJudgedOnWhatLands(t *testing.T) {
	f := newRepBoutFixture(t, true)
	require.NoError(t, f.store.SaveCompetition(&state.Competition{
		ID: "c1", Name: "Teams", Kind: "team", Format: state.CompFormatLeague,
		TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
	}))
	f.setB1(t, func(bm *state.BracketMatch) { bm.Status = state.MatchStatusScheduled }) // a team fights one match at a time
	require.NoError(t, f.store.SavePoolMatches("c1", []state.MatchResult{{
		ID: "Pool A-0", SideA: "TeamA", SideB: "TeamB", SideAID: repBoutTeamAID, SideBID: repBoutTeamBID,
		Status: state.MatchStatusRunning, ModifiedAt: f.now - 60_000,
		SubResults: []state.SubMatchResult{{Position: 1, IpponsA: []string{"M"}, Winner: "TeamA"}},
	}}))
	_, b := f.teamMembers(t)
	path := "/api/competitions/c1/matches/Pool%20A-0/score"
	poolRows := func() []state.SubMatchResult {
		matches, err := f.store.LoadPoolMatches("c1")
		require.NoError(t, err)
		return matches[0].SubResults
	}
	before := poolRows()

	sheet := scoreSheet(state.MatchStatusRunning, "", f.now+100, nil)
	row := sheet["subResults"].([]any)[0].(map[string]any)
	row["sideA"], row["sideB"], row["winner"] = "Alice", "Bob", "Alice"
	row["sideAMemberId"] = b[0].ID
	sheet["changed"] = []string{state.GroupPoints}
	w := f.send(http.MethodPut, path, "", sheet)

	requireRefusal(t, w, http.StatusBadRequest, "team_member_not_in_team", fighterNotOnTeamBody)
	assert.Equal(t, before, poolRows(), "a refused write leaves the pool match as it was")
}

// matchState is what a refused write must leave as it was: the bracket file's
// bytes and the match's history.
type matchState struct {
	bracket string
	history []state.MatchHistoryEntry
}

func (f *repBoutFixture) storedState(t *testing.T) matchState {
	t.Helper()
	bracket, err := os.ReadFile(filepath.Join(f.store.GetFolder(), "competitions", "c1", "bracket.json")) // #nosec G304 -- a test fixture path
	require.NoError(t, err)
	history, err := f.store.LoadMatchHistory("c1", "B1")
	require.NoError(t, err)
	return matchState{bracket: string(bracket), history: history}
}
