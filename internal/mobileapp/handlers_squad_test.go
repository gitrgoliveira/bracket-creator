package mobileapp

import (
	"bytes"
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

// setupSquadTestRouter builds a router mirroring server.go's own wiring:
// all four squad routes on the admin group, gated by AuthMiddleware.
// setupSquadTestRouter returns the router, the store, and the participant id
// of a REAL team. The store refuses a member add for a team id no
// participant carries, so these tests need a genuine team rather than a
// placeholder string.
//
// The competition's TeamSize is 3 and the team carries no Metadata, so by
// the time this returns, the team already carries 3 SEEDED members
// (indices 1-3, blank names): the LoadParticipants call below runs
// upgradeSquadsFromMetadataLocked (bc-pnum). Any test that adds a member
// from here must account for those 3 pre-existing slots: a first Add gets
// index 4, not 1.
func setupSquadTestRouter(t *testing.T) (*gin.Engine, *state.Store, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir, err := os.MkdirTemp("", "squad-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Kind: "team", TeamSize: 3}))

	require.NoError(t, store.SaveParticipants("c1", []domain.Player{
		{Name: "Tora", Dojo: "Tora Dojo"},
	}))
	teams, err := store.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, teams, 1)
	require.NotEmpty(t, teams[0].ID)

	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterSquadHandlers(admin, store, store)
	return r, store, teams[0].ID
}

func squadJSONReq(method, path, password string, body any) *http.Request {
	var buf *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	} else {
		buf = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	req.Header.Set("Content-Type", "application/json")
	if password != "" {
		req.Header.Set("X-Tournament-Password", password)
	}
	return req
}

// POST mints an id and the next index (4, continuing past the 3 seeded
// slots), returns 201; GET /squads then reflects it.
func TestSquadHandlers_AddMember(t *testing.T) {
	r, _, teamID := setupSquadTestRouter(t)

	req := squadJSONReq(http.MethodPost, "/api/competitions/c1/teams/"+teamID+"/members", "secret", SquadMemberRequest{Name: "Alice"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var member domain.TeamMember
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &member))
	assert.NotEmpty(t, member.ID)
	assert.Equal(t, 4, member.Index, "the team already carries 3 seeded slots (TeamSize 3)")
	assert.Equal(t, "Alice", member.Name)

	req2 := squadJSONReq(http.MethodGet, "/api/competitions/c1/squads", "secret", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var got struct {
		Squads map[string][]domain.TeamMember `json:"squads"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &got))
	require.Len(t, got.Squads[teamID], 4, "3 seeded slots plus the reserve added above")
	assert.Equal(t, "Alice", got.Squads[teamID][3].Name)
}

// A blank name is refused with a 400 before it ever reaches the store.
func TestSquadHandlers_AddMemberBlankNameIs400(t *testing.T) {
	r, _, teamID := setupSquadTestRouter(t)
	req := squadJSONReq(http.MethodPost, "/api/competitions/c1/teams/"+teamID+"/members", "secret", SquadMemberRequest{Name: "   "})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// A duplicate member name within one team is a 409 (state.ErrDuplicateTeamMember,
// reusing the SAME status classifyRosterWriteError already gives that
// sentinel everywhere else it is returned).
func TestSquadHandlers_AddDuplicateMemberIs409(t *testing.T) {
	r, _, teamID := setupSquadTestRouter(t)
	req1 := squadJSONReq(http.MethodPost, "/api/competitions/c1/teams/"+teamID+"/members", "secret", SquadMemberRequest{Name: "Alice"})
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusCreated, w1.Code)

	req2 := squadJSONReq(http.MethodPost, "/api/competitions/c1/teams/"+teamID+"/members", "secret", SquadMemberRequest{Name: "alice"})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusConflict, w2.Code)
}

// PUT renames a member, keeping id/index, and returns 204.
func TestSquadHandlers_RenameMember(t *testing.T) {
	r, store, teamID := setupSquadTestRouter(t)
	member, err := store.AddTeamMember("c1", teamID, "Alice") // index 4: continues past the 3 seeded slots
	require.NoError(t, err)

	req := squadJSONReq(http.MethodPut, "/api/competitions/c1/teams/"+teamID+"/members/"+member.ID, "secret", SquadMemberRequest{Name: "Alicia"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	squads, err := store.LoadSquads("c1")
	require.NoError(t, err)
	require.Len(t, squads[teamID], 4, "3 seeded slots plus the renamed reserve")
	assert.Equal(t, "Alicia", squads[teamID][3].Name)
	assert.Equal(t, member.ID, squads[teamID][3].ID)
	assert.Equal(t, member.Index, squads[teamID][3].Index)
}

// PUT on an unknown member id is a 404 (state.ErrTeamMemberNotFound).
func TestSquadHandlers_RenameUnknownMemberIs404(t *testing.T) {
	r, _, teamID := setupSquadTestRouter(t)
	req := squadJSONReq(http.MethodPut, "/api/competitions/c1/teams/"+teamID+"/members/no-such-member", "secret", SquadMemberRequest{Name: "X"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// All four routes 404 on a competition id that names no competition,
// rather than a bare 500 from a write that could never land.
func TestSquadHandlers_UnknownCompetitionIs404(t *testing.T) {
	r, _, teamID := setupSquadTestRouter(t)

	t.Run("GET", func(t *testing.T) {
		req := squadJSONReq(http.MethodGet, "/api/competitions/no-such-comp/squads", "secret", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("POST", func(t *testing.T) {
		req := squadJSONReq(http.MethodPost, "/api/competitions/no-such-comp/teams/"+teamID+"/members", "secret", SquadMemberRequest{Name: "Alice"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("PUT", func(t *testing.T) {
		req := squadJSONReq(http.MethodPut, "/api/competitions/no-such-comp/teams/"+teamID+"/members/some-id", "secret", SquadMemberRequest{Name: "Alice"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("DELETE", func(t *testing.T) {
		req := squadJSONReq(http.MethodDelete, "/api/competitions/no-such-comp/teams/"+teamID+"/members/some-id", "secret", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// All four routes require the admin (main) password.
func TestSquadHandlers_RequireAuth(t *testing.T) {
	r, _, teamID := setupSquadTestRouter(t)

	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/competitions/c1/squads"},
		{http.MethodPost, "/api/competitions/c1/teams/" + teamID + "/members"},
		{http.MethodPut, "/api/competitions/c1/teams/" + teamID + "/members/some-id"},
		{http.MethodDelete, "/api/competitions/c1/teams/" + teamID + "/members/some-id"},
	}
	for _, rt := range routes {
		req := squadJSONReq(rt.method, rt.path, "", SquadMemberRequest{Name: "Alice"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s must require the main password", rt.method, rt.path)
	}
}

// DELETE clears the member's name, returns 204, and keeps id/index.
func TestSquadHandlers_ClearMember(t *testing.T) {
	r, store, teamID := setupSquadTestRouter(t)
	member, err := store.AddTeamMember("c1", teamID, "Alice") // index 4: continues past the 3 seeded slots
	require.NoError(t, err)

	req := squadJSONReq(http.MethodDelete, "/api/competitions/c1/teams/"+teamID+"/members/"+member.ID, "secret", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	squads, err := store.LoadSquads("c1")
	require.NoError(t, err)
	require.Len(t, squads[teamID], 4)
	assert.Equal(t, "", squads[teamID][3].Name, "clearing must blank the name")
	assert.Equal(t, member.ID, squads[teamID][3].ID, "clearing must keep the id")
	assert.Equal(t, member.Index, squads[teamID][3].Index, "clearing must keep the index")
}

// DELETE on an unknown member id is a 404 (state.ErrTeamMemberNotFound).
func TestSquadHandlers_ClearUnknownMemberIs404(t *testing.T) {
	r, _, teamID := setupSquadTestRouter(t)
	req := squadJSONReq(http.MethodDelete, "/api/competitions/c1/teams/"+teamID+"/members/no-such-member", "secret", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// DELETE is refused with 409 once the competition has started
// (state.ErrTeamMemberClearAfterStart), and nothing is written.
func TestSquadHandlers_ClearRefusedAfterStart(t *testing.T) {
	r, store, teamID := setupSquadTestRouter(t)
	member, err := store.AddTeamMember("c1", teamID, "Alice")
	require.NoError(t, err)

	comp, err := store.LoadCompetition("c1")
	require.NoError(t, err)
	comp.Status = state.CompStatusPools
	require.NoError(t, store.SaveCompetition(comp))

	req := squadJSONReq(http.MethodDelete, "/api/competitions/c1/teams/"+teamID+"/members/"+member.ID, "secret", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	squads, err := store.LoadSquads("c1")
	require.NoError(t, err)
	require.Len(t, squads[teamID], 4)
	assert.Equal(t, "Alice", squads[teamID][3].Name, "a refused clear must not have changed the stored name")
}

// POST for a team id no participant carries is a 404, not a silent write.
// Without an entry-removal operation the member would otherwise be
// permanent and unreachable.
func TestSquadHandlers_AddToUnknownTeamIs404(t *testing.T) {
	r, store, teamID := setupSquadTestRouter(t)

	req := squadJSONReq(http.MethodPost, "/api/competitions/c1/teams/not-a-real-team/members", "secret", SquadMemberRequest{Name: "Alice"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	squads, err := store.LoadSquads("c1")
	require.NoError(t, err)
	assert.Empty(t, squads["not-a-real-team"], "a refused add must not persist a squad for the bogus id")
	require.Len(t, squads, 1, "only the real team's pre-seeded squad must exist")
	assert.Len(t, squads[teamID], 3, "the refused add must not have touched the real team's seeded slots")
}
