package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// courtCurrentPublic reads GET /api/viewer/court/A/current as an anonymous
// public caller and returns the decoded answer with its raw body.
func courtCurrentPublic(t *testing.T, r http.Handler) (courtCurrentResponse, string) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%q", w.Body.String())
	return resp, w.Body.String()
}

// hiddenNamesCourtFixture is an officiated tournament with one competition
// whose public names are hidden, on court A.
func hiddenNamesCourtFixture(t *testing.T, comp *state.Competition) (http.Handler, *state.Store) {
	t.Helper()
	r, store, _, _, tempDir := setupTestRouter(t)
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Cup", Password: "secret", Courts: []string{"A"}, Mode: state.TournamentModeOfficiated,
	}))
	secret, err := state.NewRevealSecret()
	require.NoError(t, err)
	comp.Courts = []string{"A"}
	comp.PublicNames = state.PublicNamesHidden
	comp.RevealSecret = secret
	require.NoError(t, store.SaveCompetition(comp))
	return r, store
}

// TestCourtCurrentBracketMatch_HiddenNamesMaskByStoredID pins the court
// overlay's knockout and 3rd-place sides to the id the row stores. A legacy
// row with no side id and a name two competitors share must read "?", not the
// number of whichever namesake the roster lists first; a stamped side reads
// as its own competitor's label. Neither shows the name.
func TestCourtCurrentBracketMatch_HiddenNamesMaskByStoredID(t *testing.T) {
	const cid = "ko-hidden"
	r, store := hiddenNamesCourtFixture(t, &state.Competition{
		ID: cid, Name: "Knockout", Kind: "individual", Format: state.CompFormatKnockout,
		Status: state.CompStatusKnockout, NumberPrefix: "K",
	})
	northID, southID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{
		{ID: southID, Name: "Sam", Dojo: "South"},
		{ID: northID, Name: "Sam", Dojo: "North"},
	}))

	cases := map[string]*state.Bracket{
		"round": {Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Sam", SideB: "Sam", SideBID: southID,
				Status: state.MatchStatusRunning, Court: "A"},
		}}},
		"3rd place": {ThirdPlaceMatch: &state.BracketMatch{
			ID: "m-bronze", SideA: "Sam", SideB: "Sam", SideBID: southID,
			Status: state.MatchStatusRunning, Court: "A",
		}},
	}
	for name, bracket := range cases {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, store.SaveBracket(cid, bracket))
			resp, body := courtCurrentPublic(t, r)
			require.NotNil(t, resp.SideA)
			require.NotNil(t, resp.SideB)
			assert.Equal(t, "?", resp.SideA.Name, "an id-less side fails closed instead of matching a namesake")
			assert.Equal(t, "?", resp.SideA.DisplayName)
			assert.Empty(t, resp.SideA.PlayerID, "an id-less side resolves no roster player")
			assert.NotEqual(t, "Sam", resp.SideB.Name, "a stamped side is masked too")
			assert.Equal(t, southID, resp.SideB.PlayerID)
			assert.Contains(t, body, `"namesHidden":true`)
		})
	}
}

// TestCourtCurrentRepBout_HiddenNamesMaskFighters pins the representative
// bout fighters of a running pool match: the court overlay shows a team
// member's label, never the name, and "?" for a fighter no member is named.
func TestCourtCurrentRepBout_HiddenNamesMaskFighters(t *testing.T) {
	const cid = "teams-hidden"
	r, store := hiddenNamesCourtFixture(t, &state.Competition{
		ID: cid, Name: "Teams", Kind: "team", TeamSize: 3, Format: state.CompFormatMixed,
		Status: state.CompStatusPools, NumberPrefix: "T",
	})
	tigers, lions := helper.NewUUID4(), helper.NewUUID4()
	teams := []domain.Player{
		{ID: tigers, Name: "Tigers", Dojo: "Dojo One"},
		{ID: lions, Name: "Lions", Dojo: "Dojo Two"},
	}
	require.NoError(t, store.SaveParticipants(cid, teams))
	// The draw's numbers come from pools.csv.
	teams[0].Number, teams[1].Number = "T1", "T2"
	require.NoError(t, store.SavePools(cid, []helper.Pool{{PoolName: "A", Players: teams}}))
	_, err := store.AddTeamMember(cid, tigers, "Ken Kato")
	require.NoError(t, err)
	require.NoError(t, store.SavePoolMatches(cid, []state.MatchResult{{
		ID: "PoolA-1", SideA: "Tigers", SideAID: tigers, SideB: "Lions", SideBID: lions,
		Status: state.MatchStatusRunning, Court: "A",
		RepPlayerA: "Ken Kato", RepPlayerB: "Nobody Here",
	}}))

	resp, body := courtCurrentPublic(t, r)
	assert.Equal(t, "Tigers", resp.SideA.Name, "a team's name stays")
	assert.Equal(t, "T1.1", resp.RepPlayerA, "a named member reads as their label")
	assert.Equal(t, "?", resp.RepPlayerB, "a fighter no member is named fails closed")
	assert.NotContains(t, body, "Ken Kato")
	assert.NotContains(t, body, "Nobody Here")
}
