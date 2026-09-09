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
// all three squad routes on the admin group, gated by AuthMiddleware.
func setupSquadTestRouter(t *testing.T) (*gin.Engine, *state.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir, err := os.MkdirTemp("", "squad-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Kind: "team", TeamSize: 3}))

	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterSquadHandlers(admin, store, store)
	return r, store
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

// POST mints an id and index (1), returns 201; GET /squads then reflects it.
func TestSquadHandlers_AddMember(t *testing.T) {
	r, _ := setupSquadTestRouter(t)

	req := squadJSONReq(http.MethodPost, "/api/competitions/c1/teams/team-1/members", "secret", SquadMemberRequest{Name: "Alice"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var member domain.TeamMember
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &member))
	assert.NotEmpty(t, member.ID)
	assert.Equal(t, 1, member.Index)
	assert.Equal(t, "Alice", member.Name)

	req2 := squadJSONReq(http.MethodGet, "/api/competitions/c1/squads", "secret", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var got struct {
		Squads map[string][]domain.TeamMember `json:"squads"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &got))
	require.Len(t, got.Squads["team-1"], 1)
	assert.Equal(t, "Alice", got.Squads["team-1"][0].Name)
}

// A blank name is refused with a 400 before it ever reaches the store.
func TestSquadHandlers_AddMemberBlankNameIs400(t *testing.T) {
	r, _ := setupSquadTestRouter(t)
	req := squadJSONReq(http.MethodPost, "/api/competitions/c1/teams/team-1/members", "secret", SquadMemberRequest{Name: "   "})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// A duplicate member name within one team is a 409 (state.ErrDuplicateTeamMember,
// reusing the SAME status classifyRosterWriteError already gives that
// sentinel everywhere else it is returned).
func TestSquadHandlers_AddDuplicateMemberIs409(t *testing.T) {
	r, _ := setupSquadTestRouter(t)
	req1 := squadJSONReq(http.MethodPost, "/api/competitions/c1/teams/team-1/members", "secret", SquadMemberRequest{Name: "Alice"})
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	require.Equal(t, http.StatusCreated, w1.Code)

	req2 := squadJSONReq(http.MethodPost, "/api/competitions/c1/teams/team-1/members", "secret", SquadMemberRequest{Name: "alice"})
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusConflict, w2.Code)
}

// PUT renames a member, keeping id/index, and returns 204.
func TestSquadHandlers_RenameMember(t *testing.T) {
	r, store := setupSquadTestRouter(t)
	member, err := store.AddTeamMember("c1", "team-1", "Alice")
	require.NoError(t, err)

	req := squadJSONReq(http.MethodPut, "/api/competitions/c1/teams/team-1/members/"+member.ID, "secret", SquadMemberRequest{Name: "Alicia"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	squads, err := store.LoadSquads("c1")
	require.NoError(t, err)
	require.Len(t, squads["team-1"], 1)
	assert.Equal(t, "Alicia", squads["team-1"][0].Name)
	assert.Equal(t, member.ID, squads["team-1"][0].ID)
	assert.Equal(t, member.Index, squads["team-1"][0].Index)
}

// PUT on an unknown member id is a 404 (state.ErrTeamMemberNotFound).
func TestSquadHandlers_RenameUnknownMemberIs404(t *testing.T) {
	r, _ := setupSquadTestRouter(t)
	req := squadJSONReq(http.MethodPut, "/api/competitions/c1/teams/team-1/members/no-such-member", "secret", SquadMemberRequest{Name: "X"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// All three routes 404 on a competition id that names no competition,
// rather than a bare 500 from a write that could never land.
func TestSquadHandlers_UnknownCompetitionIs404(t *testing.T) {
	r, _ := setupSquadTestRouter(t)

	t.Run("GET", func(t *testing.T) {
		req := squadJSONReq(http.MethodGet, "/api/competitions/no-such-comp/squads", "secret", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("POST", func(t *testing.T) {
		req := squadJSONReq(http.MethodPost, "/api/competitions/no-such-comp/teams/team-1/members", "secret", SquadMemberRequest{Name: "Alice"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("PUT", func(t *testing.T) {
		req := squadJSONReq(http.MethodPut, "/api/competitions/no-such-comp/teams/team-1/members/some-id", "secret", SquadMemberRequest{Name: "Alice"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// All three routes require the admin (main) password.
func TestSquadHandlers_RequireAuth(t *testing.T) {
	r, _ := setupSquadTestRouter(t)

	routes := []struct{ method, path string }{
		{http.MethodGet, "/api/competitions/c1/squads"},
		{http.MethodPost, "/api/competitions/c1/teams/team-1/members"},
		{http.MethodPut, "/api/competitions/c1/teams/team-1/members/some-id"},
	}
	for _, rt := range routes {
		req := squadJSONReq(rt.method, rt.path, "", SquadMemberRequest{Name: "Alice"})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s %s must require the main password", rt.method, rt.path)
	}
}
