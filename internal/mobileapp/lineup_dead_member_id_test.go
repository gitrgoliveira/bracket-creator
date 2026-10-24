package mobileapp

import (
	"net/http"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-evts stage 3 review 3, F2: a member add the server refuses leaves the id
// the device minted on a lineup save queued after it. A lineup never stores a
// member id the team does not hold: the organiser's save keeps the position by
// its name, with no id (an empty id resolves to nothing, bc-pnum), where it used
// to store the dead id. The anonymous self-run caller is still refused 400
// team_member_not_in_team (self_run_team_writes_test.go).
const deadMemberID = "0d0d0d0d-0d0d-4d0d-8d0d-0d0d0d0d0d0d"

func TestLineupSave_OrganiserKeepsANameWhoseMemberIDTheTeamDoesNotHold(t *testing.T) {
	cases := map[string]func(f teamWritesFixture) string{
		"a match's lineup": func(f teamWritesFixture) string { return f.lineupPath("PoolA-0") },
		"the starting lineup": func(f teamWritesFixture) string {
			return "/api/competitions/c1/teams/" + f.teamA + "/lineups/0"
		},
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			f := newTeamWritesFixture(t, false)
			body := LineupRequest{
				Positions: map[domain.Position]string{domain.PosSenpo: "Ren Abe", domain.PosJiho: "Sato"},
				MemberIDs: map[domain.Position]string{domain.PosSenpo: deadMemberID, domain.PosJiho: f.namedA},
			}
			w := f.send(http.MethodPut, path(f), "main-pw", body)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.NotContains(t, w.Body.String(), deadMemberID, "the answer carries no dead id")

			lineups, err := f.store.LoadTeamLineups("c1")
			require.NoError(t, err)
			var stored domain.TeamLineup
			var ok bool
			if name == "the starting lineup" {
				stored, ok = findRoundLineup(lineups, f.teamA, 0)
			} else {
				stored, ok = findMatchLineup(lineups, f.teamA, "PoolA-0")
			}
			require.True(t, ok)
			assert.Equal(t, "Ren Abe", stored.Positions[domain.PosSenpo], "the position keeps its name")
			assert.Empty(t, stored.MemberIDs[domain.PosSenpo], "and holds no id the team does not hold")
			assert.Equal(t, f.namedA, stored.MemberIDs[domain.PosJiho], "an id the team holds is kept")
		})
	}
}

func TestLineupSave_OrganiserPartialSaveDropsADeadIDOnTheChangedPosition(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	body := LineupRequest{
		Positions: map[domain.Position]string{domain.PosSenpo: "Ren Abe"},
		MemberIDs: map[domain.Position]string{domain.PosSenpo: deadMemberID},
		Changed:   &[]domain.Position{domain.PosSenpo},
	}
	w := f.send(http.MethodPut, f.lineupPath("PoolA-0"), "main-pw", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, ok := f.savedLineup(t, "PoolA-0")
	require.True(t, ok)
	assert.Equal(t, "Ren Abe", stored.Positions[domain.PosSenpo])
	assert.Empty(t, stored.MemberIDs[domain.PosSenpo])
}

func TestLineupSave_AnonymousStillRefusedForADeadID(t *testing.T) {
	f := newTeamWritesFixture(t, true)
	w := f.send(http.MethodPut, f.lineupPath("PoolA-0"), "", senpo("Ren Abe", deadMemberID))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), codeTeamMemberNotInTeam)
}
