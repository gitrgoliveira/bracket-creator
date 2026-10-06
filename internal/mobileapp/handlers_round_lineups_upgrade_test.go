package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A competition recorded by v2.1.1 with a Lineups-page lineup for round 2
// (stored as round 1): once the data folder is loaded, the lineup-in-force read
// gives the team its starting lineup before its round 1 match and the moved
// lineup from that match on, naming the match it was moved onto.
func TestPublicLineupInForceGET_AfterTheRoundLineupsAreMoved(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	seed, err := state.NewStore(dir)
	require.NoError(t, err)
	const compID = "recorded-by-v211"
	tora, kuma, usagi, saru := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, seed.SaveCompetition(&state.Competition{
		ID: compID, Name: "Recorded By v2.1.1", Kind: "team", TeamSize: 3, Format: state.CompFormatKnockout, Status: state.CompStatusKnockout,
	}))
	require.NoError(t, seed.SaveParticipants(compID, []domain.Player{
		{ID: tora, Name: "Tora", Dojo: "A"}, {ID: kuma, Name: "Kuma", Dojo: "B"},
		{ID: usagi, Name: "Usagi", Dojo: "C"}, {ID: saru, Name: "Saru", Dojo: "D"},
	}))
	require.NoError(t, seed.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{ID: "r0-m0", SideA: "Tora", SideAID: tora, SideB: "Kuma", SideBID: kuma, Status: state.MatchStatusCompleted, Winner: "Tora", WinnerID: tora},
			{ID: "r0-m1", SideA: "Usagi", SideAID: usagi, SideB: "Saru", SideBID: saru, Status: state.MatchStatusCompleted, Winner: "Usagi", WinnerID: usagi},
		},
		{{ID: "r1-m0", SideA: "Tora", SideAID: tora, SideB: "Usagi", SideBID: usagi}},
	}}))
	// What v2.1.1 left in lineups.yaml, and no marker: it recorded none.
	stored, err := seed.LoadCompetition(compID)
	require.NoError(t, err)
	stored.RoundLineupsConverted = false
	require.NoError(t, seed.SaveCompetition(stored))
	lineup := func(round int, name string) domain.TeamLineup {
		return domain.TeamLineup{
			TeamID: tora, CompetitionID: compID, Round: round,
			Positions: map[domain.Position]string{domain.PositionNumbered(1): name},
		}
	}
	raw, err := yaml.Marshal(struct {
		Lineups []domain.TeamLineup `yaml:"lineups"`
	}{Lineups: []domain.TeamLineup{lineup(0, "Sato"), lineup(1, "Ito")}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", compID, "lineups.yaml"), raw, 0o600))

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	r := gin.New()
	RegisterPublicLineupHandlers(r.Group("/api"), store, store, engine.New(store))

	start := inForceBody(t, r, compID, tora, "r0-m0")
	assert.Equal(t, true, start["saved"])
	assert.Equal(t, map[string]any{"1": "Sato"}, start["positions"], "before round 2 the team fields its starting lineup")
	assert.Equal(t, float64(0), start["sourceRound"])
	assert.NotContains(t, start, "sourceMatchId")

	final := inForceBody(t, r, compID, tora, "r1-m0")
	assert.Equal(t, true, final["saved"])
	assert.Equal(t, map[string]any{"1": "Ito"}, final["positions"], "from its round 2 match the team fields the lineup saved for round 2")
	assert.Equal(t, "r1-m0", final["sourceMatchId"], "which is now the lineup of that match")
	assert.NotContains(t, final, "sourceRound")

	waiting := inForceBody(t, r, compID, usagi, "r1-m0")
	assert.Equal(t, false, waiting["saved"], "a team with no lineup has none in force")

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/"+compID+"/teams/"+tora+"/lineups/1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var roundOne map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roundOne))
	assert.Equal(t, false, roundOne["saved"], "the round lineup itself is gone: it was moved")
}
