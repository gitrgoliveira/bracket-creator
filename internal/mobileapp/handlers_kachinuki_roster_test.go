package mobileapp

// GET /api/competitions/:id/matches/:mid/kachinuki-roster (bc-kfnl): a
// kachinuki encounter's fighters not yet retired, for the advisory line the
// team score sheet shows above the live bout. A public read: the self-run
// public score sheet mounts the same team editor.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedKachinukiRosterComp stores a kachinuki competition "k1" with two teams,
// a starting lineup for team A only, and one running pool encounter whose
// first bout A1 won.
func seedKachinukiRosterComp(t *testing.T, store *state.Store) (a1, a2 domain.TeamMember) {
	t.Helper()
	aID, bID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "k1", Name: "Kachinuki", Kind: "team", TeamSize: 2,
		TeamMatchType: state.TeamMatchTypeKachinuki, Format: state.CompFormatMixed,
	}))
	require.NoError(t, store.SaveParticipants("k1", []domain.Player{
		{ID: aID, Name: "Ryu", Dojo: "DojoR"},
		{ID: bID, Name: "Tora", Dojo: "DojoT"},
	}))
	var err error
	a1, err = store.AddTeamMember("k1", aID, "A1")
	require.NoError(t, err)
	a2, err = store.AddTeamMember("k1", aID, "A2")
	require.NoError(t, err)
	require.NoError(t, store.SetTeamLineup("k1", domain.TeamLineup{
		TeamID: aID, Round: 0,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): "A1", domain.PositionNumbered(2): "A2"},
		MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): a1.ID, domain.PositionNumbered(2): a2.ID},
	}, 2))
	require.NoError(t, store.SavePoolMatches("k1", []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "A1", SideAMemberID: a1.ID, SideB: "B1", Winner: "A1", WinnerMemberID: a1.ID, Decision: "fought"},
			{Position: 2, SideA: "A1", SideAMemberID: a1.ID, SideB: "B2"},
		},
	}}))
	return a1, a2
}

func getRoster(r http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestKachinukiRosterGET(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	a1, a2 := seedKachinukiRosterComp(t, store)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "fixed", Kind: "team", TeamSize: 3}))

	t.Run("200 with each side's queue", func(t *testing.T) {
		w := getRoster(r, "/api/competitions/k1/matches/Pool%20A-0/kachinuki-roster")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body struct {
			SideA struct {
				LineupFound bool `json:"lineupFound"`
				Remaining   []struct {
					Name     string `json:"name"`
					MemberID string `json:"memberId"`
				} `json:"remaining"`
			} `json:"sideA"`
			SideB struct {
				LineupFound bool              `json:"lineupFound"`
				Remaining   []json.RawMessage `json:"remaining"`
			} `json:"sideB"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.True(t, body.SideA.LineupFound)
		require.Len(t, body.SideA.Remaining, 2)
		assert.Equal(t, a1.ID, body.SideA.Remaining[0].MemberID, "A1 won and stays on")
		assert.Equal(t, "A2", body.SideA.Remaining[1].Name)
		assert.Equal(t, a2.ID, body.SideA.Remaining[1].MemberID)
		assert.False(t, body.SideB.LineupFound, "team B has no lineup")
		assert.Empty(t, body.SideB.Remaining)
		assert.Contains(t, w.Body.String(), `"remaining":[]`, "an empty queue is [] on the wire, never null")
	})
	t.Run("404 for an unknown match", func(t *testing.T) {
		w := getRoster(r, "/api/competitions/k1/matches/nope/kachinuki-roster")
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})
	t.Run("404 for an unknown competition", func(t *testing.T) {
		w := getRoster(r, "/api/competitions/missing/matches/Pool%20A-0/kachinuki-roster")
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})
	t.Run("400 for a competition that is not kachinuki", func(t *testing.T) {
		w := getRoster(r, "/api/competitions/fixed/matches/Pool%20A-0/kachinuki-roster")
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})
	t.Run("400 for an invalid competition id", func(t *testing.T) {
		w := getRoster(r, "/api/competitions/bad%21id/matches/m/kachinuki-roster")
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})
}

// A read the engine cannot make is the server's fault: a 500.
func TestKachinukiRosterGET_ReadFailureIs500(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	r := gin.New()
	RegisterPublicLineupHandlers(r.Group("/api"), store, store, failingLineupEngine{})

	w := getRoster(r, "/api/competitions/c1/matches/m1/kachinuki-roster")
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

// The read is on the PUBLIC side in a self-run tournament: the public score
// sheet mounts the team editor that shows the advisory line, and sends an
// EMPTY password. It is a read that reveals nothing the public lineup reads do
// not, so it is not in isSelfRunMainGatedConfigRoute.
func TestSelfRun_KachinukiRosterIsPublic(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store, "admin-pw")
	seedKachinukiRosterComp(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/k1/matches/Pool%20A-0/kachinuki-roster", nil)
	req.Header.Set("X-Tournament-Password", "")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	assert.False(t, isSelfRunMainGatedConfigRoute(http.MethodGet, "/api/competitions/:id/matches/:mid/kachinuki-roster"),
		"the roster read stays public in self-run")
}
