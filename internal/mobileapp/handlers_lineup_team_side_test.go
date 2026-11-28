package mobileapp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A match lineup save lands only for a team the match holds by id, for the
// organiser as for a participant (bc-drev FR6): a draw swap moves a team to
// another place, and a save made before it, a queued one included, must not
// land on a match the team no longer plays.
func TestLineupTeamSide_OrganiserSaveForATeamTheMatchDoesNotHold(t *testing.T) {
	r, store, _ := setupLineupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: "Team A", SideB: "Team B", SideAID: "teamA", SideBID: "teamB", Status: state.MatchStatusScheduled},
	}))
	require.NoError(t, store.SaveBracket("c1", &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "R1-0", SideA: "Team A", SideB: "Team B", SideAID: "teamA", SideBID: "teamB", Status: state.MatchStatusScheduled},
			{ID: "R1-1", SideA: "Team C", SideB: "", SideAID: "teamC", SideBID: "", Status: state.MatchStatusScheduled}},
		{{ID: "R2-0", SideA: "Winner of R1-0", SideB: "Team C", SideBID: "teamC", Status: state.MatchStatusScheduled}},
	}}))

	put := func(team, match string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut,
			"/api/competitions/c1/teams/"+team+"/match-lineups/"+match, bytes.NewReader(validPositionsBody()))
		req.Header.Set("X-Tournament-Password", "secret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("a pool match holding two other teams refuses it", func(t *testing.T) {
		w := put("teamC", "PoolA-0")
		requireRefusal(t, w, http.StatusNotFound, "team_not_in_match",
			"This team is not in this match. Check the score sheet and try again.")
		lineups, err := store.LoadTeamLineups("c1")
		require.NoError(t, err)
		_, found := findMatchLineup(lineups, "teamC", "PoolA-0")
		assert.False(t, found, "nothing is written")
	})

	t.Run("a knockout match holding two other teams refuses it", func(t *testing.T) {
		requireRefusal(t, put("teamC", "R1-0"), http.StatusNotFound, "team_not_in_match",
			"This team is not in this match. Check the score sheet and try again.")
	})

	t.Run("a team the match holds saves", func(t *testing.T) {
		require.Equal(t, http.StatusOK, put("teamA", "PoolA-0").Code)
		require.Equal(t, http.StatusOK, put("teamB", "R1-0").Code)
		require.Equal(t, http.StatusOK, put("teamC", "R2-0").Code)
	})

	t.Run("a side with no id yet cannot say who it holds, so the organiser saves", func(t *testing.T) {
		w := put("teamA", "R2-0")
		require.Equal(t, http.StatusOK, w.Code, "an unresolved feeder: %s", w.Body.String())
		w = put("teamB", "R1-1")
		require.Equal(t, http.StatusOK, w.Code, "a bye: %s", w.Body.String())
	})

	t.Run("a match the draw does not hold keeps the organiser's rule", func(t *testing.T) {
		require.Equal(t, http.StatusOK, put("teamC", "NoSuchMatch").Code)
	})
}

// matchHoldsTeam is the one rule both callers' saves are judged by.
func TestLineupTeamSide_MatchHoldsTeam(t *testing.T) {
	pairingOf := func(a, b string) domain.WinnerAttribution {
		return domain.WinnerAttribution{SideAID: a, SideBID: b, SideA: "Team " + a, SideB: "Team " + b}
	}
	both := pairingOf("a", "b")
	assert.True(t, matchHoldsTeam(both, "a", true))
	assert.True(t, matchHoldsTeam(both, "b", false))
	assert.False(t, matchHoldsTeam(both, "c", true), "a participant")
	assert.False(t, matchHoldsTeam(both, "c", false), "the organiser, since the sides are known")

	feeder := pairingOf("a", "")
	assert.False(t, matchHoldsTeam(feeder, "c", true), "a participant takes no lineup on a side with no id")
	assert.True(t, matchHoldsTeam(feeder, "c", false), "the organiser's save on an unresolved side is kept")
}
