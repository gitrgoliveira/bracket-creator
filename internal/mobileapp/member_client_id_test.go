package mobileapp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-evts stage 3 review 2, R1 (the operator's rule: "ALL goes to the outbox ...
// but never blocks the UI"): a team member add is an outbox entry, so the
// device mints the member's id and the lineup save that uses it can be queued
// behind the add without waiting for its answer. POST .../members takes an
// optional `id` (a lowercase UUID v4). A resend of an add that landed (same id,
// same name, same team) is answered with the member as stored and writes
// nothing; an id another member holds, or the same id with another name, is a
// 409 member_id_conflict. Every self-run rule holds with an id supplied.

const clientMemberID = "6f1c2a3b-4d5e-4f60-8a7b-9c0d1e2f3a4b"

func decodeMember(t *testing.T, body []byte) domain.TeamMember {
	t.Helper()
	var m domain.TeamMember
	require.NoError(t, json.Unmarshal(body, &m), string(body))
	return m
}

func (f teamWritesFixture) membersOf(t *testing.T, teamID string) []domain.TeamMember {
	t.Helper()
	squads, err := f.store.LoadSquads("c1")
	require.NoError(t, err)
	return squads[teamID]
}

func (f teamWritesFixture) teamB(t *testing.T) string {
	t.Helper()
	teams, err := f.store.LoadParticipants("c1", false)
	require.NoError(t, err)
	for _, p := range teams {
		if p.ID != f.teamA {
			return p.ID
		}
	}
	t.Fatal("no second team")
	return ""
}

func TestMemberPOST_ClientID(t *testing.T) {
	t.Run("the member is stored under the id the device minted", func(t *testing.T) {
		f := newTeamWritesFixture(t, false)
		before := len(f.membersOf(t, f.teamA))
		// The outbox's tags ride on every write; the member door reads none of them.
		w := f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{
			"id": clientMemberID, "name": "Ren Abe",
			"writeId": "w-1", "session": "s-1", "basis": map[string]any{"seen": 0},
		})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		m := decodeMember(t, w.Body.Bytes())
		assert.Equal(t, clientMemberID, m.ID)
		assert.Equal(t, "Ren Abe", m.Name)
		assert.Positive(t, m.ModifiedAt)
		assert.Len(t, f.membersOf(t, f.teamA), before+1)
	})

	t.Run("a resend of an add that landed is answered as stored and adds nothing", func(t *testing.T) {
		f := newTeamWritesFixture(t, false)
		first := f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"id": clientMemberID, "name": "Ren Abe"})
		require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
		count := len(f.membersOf(t, f.teamA))
		again := f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"id": clientMemberID, "name": "Ren Abe"})
		require.Equal(t, http.StatusOK, again.Code, again.Body.String())
		assert.Equal(t, decodeMember(t, first.Body.Bytes()), decodeMember(t, again.Body.Bytes()))
		assert.Len(t, f.membersOf(t, f.teamA), count)
	})

	t.Run("the same id with another name is a conflict", func(t *testing.T) {
		f := newTeamWritesFixture(t, false)
		require.Equal(t, http.StatusCreated, f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"id": clientMemberID, "name": "Ren Abe"}).Code)
		w := f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"id": clientMemberID, "name": "Mei Ito"})
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `"member_id_conflict"`)
	})

	t.Run("an id another member holds is a conflict, in this team or another", func(t *testing.T) {
		f := newTeamWritesFixture(t, false)
		w := f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"id": f.blankA, "name": "Ren Abe"})
		require.Equal(t, http.StatusConflict, w.Code, "the seeded blank member's id: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), `"member_id_conflict"`)
		assert.Empty(t, f.memberName(t, f.blankA), "the blank member is not renamed by an add")

		require.Equal(t, http.StatusCreated, f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"id": clientMemberID, "name": "Ren Abe"}).Code)
		other := "/api/competitions/c1/teams/" + f.teamB(t) + "/members"
		w = f.send(http.MethodPost, other, "main-pw", map[string]any{"id": clientMemberID, "name": "Ren Abe"})
		require.Equal(t, http.StatusConflict, w.Code, "another team's member: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), `"member_id_conflict"`)
	})

	t.Run("an id that is not a lowercase UUID v4 is refused", func(t *testing.T) {
		f := newTeamWritesFixture(t, false)
		for _, id := range []string{"not-a-uuid", strings.ToUpper(clientMemberID), "6f1c2a3b-4d5e-1f60-8a7b-9c0d1e2f3a4b", "../x"} {
			w := f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"id": id, "name": "Ren Abe"})
			assert.Equal(t, http.StatusBadRequest, w.Code, "id %q: %s", id, w.Body.String())
		}
	})

	t.Run("with no id the server mints one, as before", func(t *testing.T) {
		f := newTeamWritesFixture(t, false)
		w := f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"name": "Ren Abe"})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		assert.NotEmpty(t, decodeMember(t, w.Body.Bytes()).ID)
	})
}

// Every self-run rule of the member POST holds with a client id supplied.
func TestMemberPOST_ClientID_SelfRunRulesHold(t *testing.T) {
	id := func(i int) string {
		return clientMemberID[:34] + string(rune('a'+i/16)) + string("0123456789abcdef"[i%16])
	}

	t.Run("a name longer than a competitor's is refused", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		w := f.send(http.MethodPost, f.membersPath(), "", map[string]any{"id": clientMemberID, "name": strings.Repeat("a", MaxLenPlayerName+1)})
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})

	t.Run("a wrong password is refused", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		requireInvalidPassword(t, f.send(http.MethodPost, f.membersPath(), "stale-pw", map[string]any{"id": clientMemberID, "name": "Ren Abe"}))
	})

	t.Run("a participant adds up to the limit, and a resend at the limit is no add", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		limit := 2*5 + state.SquadReserveSlots
		last := ""
		for i := len(f.membersOf(t, f.teamA)); i < limit; i++ {
			last = id(i)
			w := f.send(http.MethodPost, f.membersPath(), "", map[string]any{"id": last, "name": "Member " + string(rune('A'+i))})
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		}
		w := f.send(http.MethodPost, f.membersPath(), "", map[string]any{"id": clientMemberID, "name": "One Too Many"})
		requireRefusal(t, w, http.StatusConflict, "team_member_limit",
			"This team already lists as many members as the score sheet can add. Ask the tournament organizer to add more.")
		resend := f.send(http.MethodPost, f.membersPath(), "", map[string]any{"id": last, "name": "Member " + string(rune('A'+limit-1))})
		assert.Equal(t, http.StatusOK, resend.Code, resend.Body.String())
		assert.Len(t, f.membersOf(t, f.teamA), limit)
	})

	t.Run("a finished competition's teams are the organiser's", func(t *testing.T) {
		f := newTeamWritesFixture(t, true)
		require.NoError(t, f.store.SaveCompetition(&state.Competition{ID: "c1", Name: "Teams", Kind: "team", TeamSize: 5, Status: state.CompStatusComplete}))
		requireRefusal(t, f.send(http.MethodPost, f.membersPath(), "", map[string]any{"id": clientMemberID, "name": "Ren Abe"}),
			http.StatusConflict, "competition_finished",
			"This competition has finished, so its teams can no longer be changed. Contact the tournament organizer to correct it.")
		w := f.send(http.MethodPost, f.membersPath(), "main-pw", map[string]any{"id": clientMemberID, "name": "Ren Abe"})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	})
}
