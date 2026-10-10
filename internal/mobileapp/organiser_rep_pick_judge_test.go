package mobileapp

// A representative pick is judged for every caller (PR #463, round 13, gap H).
//
// The pick is a bare member id on a side whose team a correction elsewhere can
// change under an open editor (the re-seat), and every surface resolves it later
// through the side's team, so an id that team does not hold shows as nobody.
// The organiser's write is therefore judged exactly as a participant's is
// (400 team_member_not_in_team), scoped the same way: only a side the merge will
// apply, only an id the stored row does not already hold, only against a side
// that carries a team id. The organiser's rights are untouched: they can pick
// again at once, on a finished match too (only team membership is read).
//
// The fixture is repBoutFixture (self_run_daihyosen_test.go); every request here
// carries the main password, in a self-run tournament and in an officiated one.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// organiserPassword is the main password (handlers_lineup_changed_test.go).
const (
	// unheldMemberID is a well-formed id no team holds.
	unheldMemberID = "99999999-9999-4999-8999-999999999999"
	notOnTeamBody  = "The representative chosen is not on this team. Pick again from the list."
)

var organiserModes = []struct {
	name    string
	selfRun bool
}{
	{"self-run tournament, organiser password", true},
	{"officiated tournament", false},
}

// organiserPickFixture is a repBoutFixture whose representative bout the
// organiser added, with each team's members read once.
type organiserPickFixture struct {
	*repBoutFixture
	membersA, membersB []string
}

func newOrganiserPickFixture(t *testing.T, selfRun bool) *organiserPickFixture {
	t.Helper()
	f := newRepBoutFixture(t, selfRun)
	w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", organiserPassword, map[string]any{"modifiedAt": f.now})
	require.Equal(t, http.StatusOK, w.Code, "the organiser adds the representative bout: %s", w.Body.String())
	squads, err := f.store.LoadSquads("c1")
	require.NoError(t, err)
	ids := func(teamID string) []string {
		var out []string
		for _, m := range squads[teamID] {
			out = append(out, m.ID)
		}
		return out
	}
	p := &organiserPickFixture{repBoutFixture: f, membersA: ids(repBoutTeamAID), membersB: ids(repBoutTeamBID)}
	require.GreaterOrEqual(t, len(p.membersA), 2, "team A is seeded with members")
	require.GreaterOrEqual(t, len(p.membersB), 2, "team B is seeded with members")
	return p
}

// sheetWith is the team sheet with the representative row naming a on side A
// and b on side B ("" names no one), stamped at, naming the groups in changed
// (nil names none, as an older client sends it).
func sheetWith(status state.MatchStatus, winner string, at int64, changed []string, row map[string]any, a, b string) map[string]any {
	row["sideAMemberId"], row["sideBMemberId"] = a, b
	sheet := scoreSheet(status, winner, at, row)
	if changed != nil {
		sheet["changed"] = changed
	}
	return sheet
}

// pick sends a running sheet from the organiser.
func (p *organiserPickFixture) pick(at int64, changed []string, a, b string) *httptest.ResponseRecorder {
	row := repBoutRow([]string{}, []string{}, "")
	return p.send(http.MethodPut, repBoutMatchPath+"/score", organiserPassword,
		sheetWith(state.MatchStatusRunning, "", at, changed, row, a, b))
}

func requireNotOnTeam(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	requireRefusal(t, w, http.StatusBadRequest, "team_member_not_in_team", notOnTeamBody)
}

// An organiser's pick of a member the side's team does not hold is refused like
// a participant's, and nothing is written: not the id, not the pick's stamp.
func TestOrganiser_RepresentativeMembersMustBeOnTheirTeam(t *testing.T) {
	for _, mode := range organiserModes {
		t.Run(mode.name, func(t *testing.T) {
			cases := []struct {
				name    string
				changed []string
				a, b    func(p *organiserPickFixture) string
			}{
				{"another team's member on side A", []string{state.GroupRepPickA},
					func(p *organiserPickFixture) string { return p.membersB[0] }, nil},
				{"a member no team holds on side A", []string{state.GroupRepPickA},
					func(*organiserPickFixture) string { return unheldMemberID }, nil},
				{"another team's member on side B", []string{state.GroupRepPickB}, nil,
					func(p *organiserPickFixture) string { return p.membersA[0] }},
				{"an older client that names no groups", nil,
					func(p *organiserPickFixture) string { return p.membersB[0] }, nil},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					p := newOrganiserPickFixture(t, mode.selfRun)
					var a, b string
					if tc.a != nil {
						a = tc.a(p)
					}
					if tc.b != nil {
						b = tc.b(p)
					}
					before := storedB1(t, p.store, "c1")

					w := p.pick(p.now+100, tc.changed, a, b)
					requireNotOnTeam(t, w)
					after := storedB1(t, p.store, "c1")
					assert.Equal(t, *p.storedRepBout(t), before.SubResults[state.DaihyosenSubIndex(before.SubResults)],
						"a refused pick writes nothing")
					assert.Equal(t, before.GroupStamp(state.GroupRepPickA), after.GroupStamp(state.GroupRepPickA))
					assert.Equal(t, before.GroupStamp(state.GroupRepPickB), after.GroupStamp(state.GroupRepPickB))
					assert.Empty(t, p.storedRepBout(t).SideAMemberID)
					assert.Empty(t, p.storedRepBout(t).SideBMemberID)

					// The organiser's rights are untouched: their own team's
					// members are accepted at once.
					w = p.pick(p.now+200, tc.changed, p.membersA[0], p.membersB[0])
					require.Equal(t, http.StatusOK, w.Code, "each side's own member is accepted: %s", w.Body.String())
					stored := p.storedRepBout(t)
					if tc.changed == nil || tc.changed[0] == state.GroupRepPickA {
						assert.Equal(t, p.membersA[0], stored.SideAMemberID)
					}
					if tc.changed == nil || tc.changed[0] == state.GroupRepPickB {
						assert.Equal(t, p.membersB[0], stored.SideBMemberID)
					}
				})
			}
		})
	}
}

// The merge holds a pick stamped before the stored pick's, and a write whose
// every group is held is answered superseded: the judge must not refuse what the
// merge was going to keep in the history anyway.
func TestOrganiser_AStalePickTheMergeHoldsIsNotRefused(t *testing.T) {
	for _, mode := range organiserModes {
		t.Run(mode.name, func(t *testing.T) {
			p := newOrganiserPickFixture(t, mode.selfRun)
			w := p.pick(p.now, nil, p.membersA[0], "")
			require.Equal(t, http.StatusOK, w.Code, "a valid pick: %s", w.Body.String())
			before := *p.storedRepBout(t)
			require.Equal(t, p.membersA[0], before.SideAMemberID)

			w = p.pick(p.now-100, nil, p.membersB[0], "")
			require.Equal(t, http.StatusOK, w.Code, "a write stamped before the stored pick is held, not refused: %s", w.Body.String())
			assert.Contains(t, w.Body.String(), `"superseded"`)
			assert.Equal(t, before, *p.storedRepBout(t), "the held pick is not written")

			requireNotOnTeam(t, p.pick(p.now, nil, p.membersB[0], ""))
			requireNotOnTeam(t, p.pick(p.now+100, nil, p.membersB[0], ""))
			assert.Equal(t, before, *p.storedRepBout(t), "a refused pick writes nothing")
		})
	}
}

// A write answers for what it introduces, not for what it inherited: a stored
// id the side's team does not hold (a re-seat left it) is echoed back by an
// editor that has not seen the re-seat, and that is not a pick.
func TestOrganiser_AnInheritedPickIsNotJudged(t *testing.T) {
	for _, mode := range organiserModes {
		t.Run(mode.name, func(t *testing.T) {
			p := newOrganiserPickFixture(t, mode.selfRun)
			p.setB1(t, func(bm *state.BracketMatch) {
				bm.SubResults[state.DaihyosenSubIndex(bm.SubResults)].SideAMemberID = p.membersB[0]
			})

			w := p.pick(p.now+200, nil, p.membersB[0], "")
			require.Equal(t, http.StatusOK, w.Code, "the echo of the stored pick is kept: %s", w.Body.String())
			assert.Equal(t, p.membersB[0], p.storedRepBout(t).SideAMemberID)

			w = p.pick(p.now+250, nil, p.membersB[0], p.membersB[1])
			require.Equal(t, http.StatusOK, w.Code, "an echo beside a valid new pick: %s", w.Body.String())
			assert.Equal(t, p.membersB[0], p.storedRepBout(t).SideAMemberID)
			assert.Equal(t, p.membersB[1], p.storedRepBout(t).SideBMemberID)

			requireNotOnTeam(t, p.pick(p.now+300, nil, p.membersB[1], p.membersB[1]))
			assert.Equal(t, p.membersB[0], p.storedRepBout(t).SideAMemberID, "a refused pick writes nothing")
		})
	}
}

// Only a side the write names is judged, as for a participant: an echo of an id
// the side's team no longer holds beside the named side is not refused.
func TestOrganiser_ASideTheWriteDoesNotNameIsNotJudged(t *testing.T) {
	p := newOrganiserPickFixture(t, true)
	w := p.pick(p.now+300, []string{state.GroupRepPickA}, p.membersA[0], p.membersA[1])
	require.Equal(t, http.StatusOK, w.Code, "side B is not named, so it is not judged: %s", w.Body.String())
	assert.Equal(t, p.membersA[0], p.storedRepBout(t).SideAMemberID, "the named side applies")
	assert.Empty(t, p.storedRepBout(t).SideBMemberID, "and the side it did not name is not written")

	requireNotOnTeam(t, p.pick(p.now+400, []string{state.GroupRepPickA, state.GroupRepPickB}, p.membersA[0], p.membersA[1]))
}

// A pick on a side with no team id has nothing to be judged against.
func TestOrganiser_APickOnAnIdlessSideIsNotJudged(t *testing.T) {
	p := newOrganiserPickFixture(t, true)
	p.setB1(t, func(bm *state.BracketMatch) { bm.SideAID = "" })

	w := p.pick(p.now+200, nil, p.membersB[0], "")
	require.Equal(t, http.StatusOK, w.Code, "the pick on an id-less side is accepted: %s", w.Body.String())
	assert.Equal(t, p.membersB[0], p.storedRepBout(t).SideAMemberID)
}

// A pick on a finished match is judged like any other, and an invalid one is
// refused for the pick, never for the finish (409 result_finalized is a
// participant's refusal; the organiser corrects a finished match).
func TestOrganiser_PickOnAFinishedMatchIsJudgedButNotRefusedForTheFinish(t *testing.T) {
	for _, mode := range organiserModes {
		t.Run(mode.name, func(t *testing.T) {
			p := newOrganiserPickFixture(t, mode.selfRun)
			finish := sheetWith(state.MatchStatusCompleted, "TeamA", p.now+100, nil,
				repBoutRow([]string{"M"}, []string{}, "TeamA"), "", "")
			w := p.send(http.MethodPut, repBoutMatchPath+"/score", organiserPassword, finish)
			require.Equal(t, http.StatusOK, w.Code, "the organiser finishes the match: %s", w.Body.String())
			require.Equal(t, state.MatchStatusCompleted, storedB1(t, p.store, "c1").Status)

			correct := func(at int64, a, b string) *httptest.ResponseRecorder {
				sheet := sheetWith(state.MatchStatusCompleted, "TeamA", at, []string{state.GroupRepPickA, state.GroupRepPickB},
					repBoutRow([]string{"M"}, []string{}, "TeamA"), a, b)
				sheet["correctionReason"] = "the wrong fighter was picked"
				return p.send(http.MethodPut, repBoutMatchPath+"/score", organiserPassword, sheet)
			}

			w = correct(p.now+200, p.membersB[0], "")
			requireNotOnTeam(t, w)
			assert.Empty(t, p.storedRepBout(t).SideAMemberID, "a refused pick writes nothing")

			w = correct(p.now+300, p.membersA[0], p.membersB[0])
			require.Equal(t, http.StatusOK, w.Code, "the organiser corrects a finished match: %s", w.Body.String())
			assert.Equal(t, state.MatchStatusCompleted, storedB1(t, p.store, "c1").Status)
			assert.Equal(t, p.membersA[0], p.storedRepBout(t).SideAMemberID)
			assert.Equal(t, p.membersB[0], p.storedRepBout(t).SideBMemberID)
		})
	}
}

// A start keeps the stored score whatever it sends (startOnly), so the row it
// carries is not a pick and is not judged.
func TestOrganiser_StartOnlyIsNotJudged(t *testing.T) {
	p := newOrganiserPickFixture(t, true)
	sheet := sheetWith(state.MatchStatusRunning, "", p.now+100, nil,
		repBoutRow([]string{}, []string{}, ""), p.membersB[0], "")
	sheet["startOnly"] = true

	w := p.send(http.MethodPut, repBoutMatchPath+"/score", organiserPassword, sheet)
	require.Equal(t, http.StatusOK, w.Code, "a start is not judged on the row it carries: %s", w.Body.String())
	assert.Empty(t, p.storedRepBout(t).SideAMemberID, "and the stored score is kept")
}

// A write whose representative row names no member reads no team members: with
// the file unreadable it is still answered, while a pick fails closed.
func TestOrganiser_AWriteThatPicksNobodyReadsNoTeamMembers(t *testing.T) {
	p := newOrganiserPickFixture(t, true)
	require.NoError(t, os.WriteFile(
		filepath.Join(p.store.GetFolder(), "competitions", "c1", "team-members.yaml"),
		[]byte("not: [valid yaml"), 0o600))

	w := p.pick(p.now+100, nil, "", "")
	require.Equal(t, http.StatusOK, w.Code, "no pick, nothing to judge: %s", w.Body.String())

	w = p.pick(p.now+200, nil, p.membersA[0], "")
	assert.Equal(t, http.StatusInternalServerError, w.Code, "a pick that cannot be judged fails closed: %s", w.Body.String())
	assert.Empty(t, p.storedRepBout(t).SideAMemberID)
}

// The organiser's write can create the representative row (a participant's
// never does), and every pick on a row it creates is introduced: a member no
// team holds is refused there too, and no row is created.
func TestOrganiser_APickOnARowTheWriteCreatesIsJudged(t *testing.T) {
	f := newRepBoutFixture(t, true)
	sheet := sheetWith(state.MatchStatusRunning, "", f.now+100, nil,
		repBoutRow([]string{}, []string{}, ""), unheldMemberID, "")

	w := f.send(http.MethodPut, repBoutMatchPath+"/score", organiserPassword, sheet)
	requireNotOnTeam(t, w)
	assert.False(t, carriesDaihyosenRow(storedB1(t, f.store, "c1").SubResults), "a refused write creates nothing")
}
