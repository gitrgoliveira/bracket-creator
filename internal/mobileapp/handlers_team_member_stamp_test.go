package mobileapp

// handlers_team_member_stamp_test.go pins what the team member routes answer now
// that a member carries a server stamp (operator decision 2026-10-07). POST
// answers 201 with the member, stamped, and announces it like every other member
// write; PUT and DELETE answer 200 with the member, where they answered 204 with
// no body, so a client holding the answer holds the copy this write produced. GET
// team-members carries each member's stamp, 0 for one no write has touched.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stampFixture is an officiated team competition with one team, its member
// routes and a broadcaster that keeps what each event carried.
type stampFixture struct {
	r      *gin.Engine
	hub    *payloadBroadcaster
	teamID string
	base   string
}

func newStampFixture(t *testing.T) stampFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "member-stamp-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Kind: "team", TeamSize: 3}))
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{{Name: "Tora", Dojo: "Tora Dojo"}}))
	teams, err := store.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, teams, 1)

	hub := &payloadBroadcaster{}
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterSquadHandlers(admin, store, store, hub, store, NewFileVerifier(store))
	return stampFixture{r: r, hub: hub, teamID: teams[0].ID, base: "/api/competitions/c1/teams/" + teams[0].ID + "/members"}
}

func (f stampFixture) send(method, path string, body any) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, squadJSONReq(method, path, "secret", body))
	return w
}

func memberFrom(t *testing.T, w *httptest.ResponseRecorder, status int) domain.TeamMember {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	var m domain.TeamMember
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m), w.Body.String())
	return m
}

func TestTeamMemberAnswers_CarryTheMemberAndItsStamp(t *testing.T) {
	f := newStampFixture(t)

	added := memberFrom(t, f.send(http.MethodPost, f.base, map[string]any{"name": "Sato"}), http.StatusCreated)
	require.NotEmpty(t, added.ID)
	assert.Equal(t, "Sato", added.Name)
	assert.Positive(t, added.ModifiedAt, "a new member is stamped")
	assert.Equal(t, gin.H{"competitionId": "c1", "teamId": f.teamID}, f.hub.last(t), "an add is announced like every other write to a team's members")

	renamed := memberFrom(t, f.send(http.MethodPut, f.base+"/"+added.ID, map[string]any{"name": "Sato Kenji"}), http.StatusOK)
	assert.Equal(t, added.ID, renamed.ID)
	assert.Equal(t, added.Index, renamed.Index)
	assert.Equal(t, "Sato Kenji", renamed.Name)
	assert.Greater(t, renamed.ModifiedAt, added.ModifiedAt, "a rename stamps the member again, later")

	cleared := memberFrom(t, f.send(http.MethodDelete, f.base+"/"+added.ID, nil), http.StatusOK)
	assert.Equal(t, added.ID, cleared.ID)
	assert.Empty(t, cleared.Name)
	assert.Greater(t, cleared.ModifiedAt, renamed.ModifiedAt, "so does a clear")
}

func TestTeamMemberAnswers_TheListCarriesEachStamp(t *testing.T) {
	f := newStampFixture(t)
	added := memberFrom(t, f.send(http.MethodPost, f.base, map[string]any{"name": "Sato"}), http.StatusCreated)
	require.Positive(t, added.ModifiedAt)

	w := f.send(http.MethodGet, "/api/competitions/c1/team-members", nil)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		TeamMembers map[string][]map[string]any `json:"teamMembers"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	var stamped, untouched int
	for _, m := range body.TeamMembers[f.teamID] {
		stamp, present := m["modifiedAt"]
		require.True(t, present, "every member carries the field, whether or not a write has touched it: %v", m)
		if m["id"] == added.ID {
			assert.EqualValues(t, added.ModifiedAt, stamp)
			stamped++
		} else {
			assert.EqualValues(t, 0, stamp, "a seeded slot no write has touched carries 0")
			untouched++
		}
	}
	assert.Equal(t, 1, stamped)
	assert.Positive(t, untouched)
}

// A participant of a self-run tournament names a member who has no name from the
// public score sheet; the answer is the member as stamped there as well.
func TestTeamMemberAnswers_AnAnonymousNamingAnswersWithTheMember(t *testing.T) {
	f := newTeamWritesFixture(t, true)

	w := f.send(http.MethodPut, f.membersPath()+"/"+f.blankA, "", map[string]any{"name": "Ren Abe"})

	named := memberFrom(t, w, http.StatusOK)
	assert.Equal(t, f.blankA, named.ID)
	assert.Equal(t, "Ren Abe", named.Name)
	assert.Positive(t, named.ModifiedAt)

	w = f.send(http.MethodPost, f.membersPath(), "", map[string]any{"name": "Added Late"})
	assert.Positive(t, memberFrom(t, w, http.StatusCreated).ModifiedAt)
}
