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

// A competition recorded by v2.1.1 with a Lineups-page lineup for round 1 and
// one for round 2 (stored as rounds 0 and 1): once the data folder is loaded, the
// lineup-in-force read gives the team, at each match it is seated in, the lineup
// v2.1.1 showed there, naming the match it now belongs to. The round lineup
// itself is kept until the competition is completed.
func TestPublicLineupInForceGET_AfterTheRoundLineupsAreConverted(t *testing.T) {
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

	first := inForceBody(t, r, compID, tora, "r0-m0")
	assert.Equal(t, true, first["saved"])
	assert.Equal(t, map[string]any{"1": "Sato"}, first["positions"], "at its first match the team fields what v2.1.1 showed there: the lineup saved for round 1")
	assert.Equal(t, "r0-m0", first["sourceMatchId"], "which is now the lineup of that match")
	assert.NotContains(t, first, "sourceRound")

	final := inForceBody(t, r, compID, tora, "r1-m0")
	assert.Equal(t, true, final["saved"])
	assert.Equal(t, map[string]any{"1": "Ito"}, final["positions"], "from its round 2 match the team fields the lineup saved for round 2")
	assert.Equal(t, "r1-m0", final["sourceMatchId"], "which is now the lineup of that match")
	assert.NotContains(t, final, "sourceRound")

	start := inForceBody(t, r, compID, tora, "a-match-the-draw-does-not-hold")
	assert.Equal(t, true, start["saved"])
	assert.Equal(t, map[string]any{"1": "Sato"}, start["positions"], "a match the draw does not hold gets the starting lineup")
	assert.Equal(t, float64(0), start["sourceRound"])
	assert.NotContains(t, start, "sourceMatchId")

	waiting := inForceBody(t, r, compID, usagi, "r1-m0")
	assert.Equal(t, false, waiting["saved"], "a team with no lineup has none in force")

	roundOneSaved := func(r *gin.Engine) any {
		req := httptest.NewRequest(http.MethodGet, "/api/competitions/"+compID+"/teams/"+tora+"/lineups/1", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var roundOne map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &roundOne))
		return roundOne["saved"]
	}
	assert.Equal(t, true, roundOneSaved(r), "while the competition is on the round lineup is kept: a correction can still seat a team")

	// Completed in a write the draw's hooks do not see: the next load retires it.
	_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
		c.Status = state.CompStatusComplete
		return c, nil
	})
	require.NoError(t, err)
	restarted, err := state.NewStore(dir)
	require.NoError(t, err)
	r = gin.New()
	RegisterPublicLineupHandlers(r.Group("/api"), restarted, restarted, engine.New(restarted))

	assert.Equal(t, false, roundOneSaved(r), "the round lineup itself is gone: each match it was shown at holds its own lineup")
	again := inForceBody(t, r, compID, tora, "r1-m0")
	assert.Equal(t, map[string]any{"1": "Ito"}, again["positions"])
	assert.Equal(t, "r1-m0", again["sourceMatchId"])
}

// A team that v2.1.1 had a lineup entered for a match for, and none for a round,
// was shown nothing at its other matches (operator decision 2026-10-07: "Show what
// v2.1.1 showed"). Once the data folder is loaded each of those matches holds an
// EMPTY lineup of its own, and both lineup reads answer it as a saved one: saved
// true, the match itself as the source, and an empty positions object (never a
// missing one), where an unsaved lineup answers saved false. A team with no
// lineup at all has none in force.
func TestPublicLineupReads_AMatchOnlyLegacyTeamIsShownNothingElsewhere(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	seed, err := state.NewStore(dir)
	require.NoError(t, err)
	const compID = "match-only-by-v211"
	tora, kuma, usagi, saru := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, seed.SaveCompetition(&state.Competition{
		ID: compID, Name: "Match Only By v2.1.1", Kind: "team", TeamSize: 3, Format: state.CompFormatKnockout, Status: state.CompStatusKnockout,
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
	stored, err := seed.LoadCompetition(compID)
	require.NoError(t, err)
	stored.RoundLineupsConverted = false
	require.NoError(t, seed.SaveCompetition(stored))
	raw, err := yaml.Marshal(struct {
		Lineups []domain.TeamLineup `yaml:"lineups"`
	}{Lineups: []domain.TeamLineup{{
		TeamID: tora, CompetitionID: compID, MatchID: "r0-m0",
		Positions: map[domain.Position]string{domain.PositionNumbered(1): "Sato"},
	}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", compID, "lineups.yaml"), raw, 0o600))

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	r := gin.New()
	RegisterPublicLineupHandlers(r.Group("/api"), store, store, engine.New(store))

	entered := inForceBody(t, r, compID, tora, "r0-m0")
	assert.Equal(t, true, entered["saved"])
	assert.Equal(t, map[string]any{"1": "Sato"}, entered["positions"], "the lineup entered is left as it is")
	assert.Equal(t, "r0-m0", entered["sourceMatchId"])

	final := inForceBody(t, r, compID, tora, "r1-m0")
	assert.Equal(t, true, final["saved"], "an empty lineup of its own is a saved one")
	assert.Equal(t, "r1-m0", final["sourceMatchId"], "it belongs to the match itself, not to the match it was entered for")
	assert.NotContains(t, final, "sourceRound")
	assert.Equal(t, map[string]any{}, final["positions"], "an empty object, not a missing field")
	assert.Equal(t, "r1-m0", final["matchId"])
	assert.Equal(t, tora, final["teamId"])

	// The match-lineup read of the same match: the same stored lineup.
	req := httptest.NewRequest(http.MethodGet, "/api/competitions/"+compID+"/teams/"+tora+"/match-lineups/r1-m0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var own map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &own))
	assert.Equal(t, true, own["saved"])
	assert.Equal(t, map[string]any{}, own["positions"])

	nothing := inForceBody(t, r, compID, kuma, "r0-m0")
	assert.Equal(t, false, nothing["saved"], "a team with no lineup at all has none in force")
}
