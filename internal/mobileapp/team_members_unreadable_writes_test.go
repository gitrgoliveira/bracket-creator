package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A member write over a team-members.yaml that cannot be read is a definite
// refusal, 409 team_members_unreadable, never the 500 the outbox retries for as
// long as the page is open (a member add stuck on a 500 held every lineup save
// naming the member behind it). The organiser and an anonymous self-run
// participant alike; the add, the rename and the removal alike.
func TestTeamMemberWrites_MembersUnreadableIsRefusedNotRetried(t *testing.T) {
	for _, selfRun := range []bool{false, true} {
		w := newNoLineupWorld(t, selfRun)
		w.corruptCompetitionFile("team-members.yaml")
		members := "/api/competitions/" + w.comp + "/teams/" + w.teamA + "/members"

		check := func(what string, method, path string, body any) {
			t.Helper()
			rec := w.send(method, path, body)
			require.Equal(t, http.StatusConflict, rec.Code, "%s (selfRun=%v): %s", what, selfRun, rec.Body.String())
			var got map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			assert.Equal(t, "team_members_unreadable", got["error"], what)
			assert.Equal(t, teamMembersUnreadableAddMessage, got["message"], what)
		}
		check("add", http.MethodPost, members, map[string]any{"name": "Maeda", "id": helper.NewUUID4()})
		if !selfRun {
			// The organiser's rename reads the same file (a cleared name is
			// refused before it, the competition having started).
			check("rename", http.MethodPut, members+"/"+w.a[1], map[string]any{"name": "Maeda"})
			// The removal reads it only while the competition can still start.
			comp, err := w.store.LoadCompetition(w.comp)
			require.NoError(t, err)
			comp.Status = state.CompStatusSetup
			require.NoError(t, w.store.SaveCompetition(comp))
			check("remove", http.MethodDelete, members+"/"+w.a[1], nil)
		}
	}
}

// The read of a competition's team members over a team-members.yaml that cannot
// be read answers 409 with the member writes' code, never the 500 of a server
// fault, so the lineup screens can say which file the organiser needs to check.
func TestTeamMembersRead_MembersUnreadableIsNamed(t *testing.T) {
	w := newNoLineupWorld(t, false)
	w.corruptCompetitionFile("team-members.yaml")

	rec := w.send(http.MethodGet, "/api/competitions/"+w.comp+"/team-members", nil)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	var got map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Equal(t, "team_members_unreadable", got["error"])
	assert.Equal(t, "The team members could not be read. The organiser needs to check team-members.yaml in the tournament data.", got["message"])
}

// teamMembersUnreadableAddMessage is the member write's sentence, spelled out
// here so a change to what the operator reads is a change to this test.
const teamMembersUnreadableAddMessage = "The team members could not be read, so the team member was not saved. The organiser needs to check team-members.yaml in the tournament data."

// With team-members.yaml unreadable the sheet places a name typed on a
// kachinuki match's bout 1 by name alone (no member is added): the match lineup
// save naming position 1 with no member id lands, and the server seats that
// name on bout 1.
func TestMatchLineupByName_MembersUnreadableSeatsBoutOne(t *testing.T) {
	w := newNoLineupWorld(t, false)
	w.start()
	w.corruptCompetitionFile("team-members.yaml")

	changed := []string{"1"}
	rec := w.send(http.MethodPut, "/api/competitions/"+w.comp+"/teams/"+w.teamA+"/match-lineups/"+url.PathEscape(w.match),
		map[string]any{"positions": map[string]string{"1": "Maeda"}, "changed": changed})
	require.Equal(t, http.StatusOK, rec.Code, "a name alone needs no team member: %s", rec.Body.String())

	// The first write that stores bout 1 (a point tapped) seats it.
	w.pointOnBoutOne()
	bout1 := state.SubResultAt(w.stored().SubResults, 1)
	assert.Equal(t, "Maeda", bout1.SideA, "bout 1 seats the typed name: %+v", w.stored().SubResults)
	assert.Empty(t, bout1.SideAMemberID)
}
