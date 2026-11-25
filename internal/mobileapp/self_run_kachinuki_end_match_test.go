package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestSelfRunKachinukiEndMatchFromThePublicSheet pins the walk's finding S5:
// a competitor on the public kachinuki sheet ends the encounter with End match,
// whose write carries the decision kachinuki-exhaustion. That decision is not a
// ruling (the server works the winner out from the bout log itself,
// deriveKachinukiWinner), so a participant may send it, and the result is
// recorded as self-reported like every other result entered on the public page.
func TestSelfRunKachinukiEndMatchFromThePublicSheet(t *testing.T) {
	const compID = "kachi-selfrun"
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := engine.New(store)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "SelfRun", Password: "secret", Courts: []string{"A"}, Mode: state.TournamentModeSelfRun,
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
		TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: helper.NewUUID4(), Name: "Ryu", Dojo: "DojoR"},
		{ID: helper.NewUUID4(), Name: "Tora", Dojo: "DojoT"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning, Court: "A",
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M", "K"}, Winner: "R-1", Decision: "fought"},
		},
	}}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterMatchHandlers(r.Group("/api"), eng, store, store, NewHub(), NewFileVerifier(store), store)

	body, err := json.Marshal(map[string]any{
		"sideA": "Ryu", "sideB": "Tora", "winner": "Ryu",
		"status": "completed", "decision": "kachinuki-exhaustion",
		"subResults": []map[string]any{kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought")},
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/P1-0/score", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	// The public page sends an EMPTY password, never omits it.
	req.Header.Set("X-Tournament-Password", "")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "End match from the public sheet finishes the encounter")
	assert.Equal(t, "Ryu", matches[0].Winner)
	assert.Equal(t, string(domain.DecisionKachinukiExhaustion), matches[0].Decision)
	assert.Equal(t, "self-reported", matches[0].ResultSource, "FR1: recorded as self-reported")
}
