package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A competition recorded by v2.1.1 keeps each team's lineup for a later round
// until the team is seated at that round, and the engine's own write that
// advances a winner then moves it: the lineup in force for the final is the one
// saved for round 2 (stored round 1), for the finalists, and the lineups of the
// teams knocked out are dropped once the final's sides are decided.
func TestRoundLineups_MovedWhenTheEngineAdvancesAWinner(t *testing.T) {
	_, seed, dir := setupTestEngine(t)
	const compID = "round-lineups-engine"
	createTestCompetition(t, seed, compID, "knockout", 3, func(c *state.Competition) {
		c.Kind, c.TeamSize = "team", 3
	})
	saveTestParticipants(t, seed, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	roster, err := seed.LoadParticipants(compID, false)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, p := range roster {
		ids[p.Name] = p.ID
	}
	// The data folder as v2.1.1 left it: no marker, and a starting lineup and a
	// lineup for the second round (stored round 1) for every team, as its
	// Lineups page wrote them.
	stored, err := seed.LoadCompetition(compID)
	require.NoError(t, err)
	stored.RoundLineupsConverted = false
	require.NoError(t, seed.SaveCompetition(stored))
	var legacy []domain.TeamLineup
	for _, name := range []string{"Alice", "Bob", "Charlie", "Dave"} {
		for round, tag := range []string{"start", "second"} {
			legacy = append(legacy, domain.TeamLineup{
				TeamID: ids[name], CompetitionID: compID, Round: round,
				Positions: map[domain.Position]string{domain.PositionNumbered(1): name + "-" + tag},
			})
		}
	}
	raw, err := yaml.Marshal(struct {
		Lineups []domain.TeamLineup `yaml:"lineups"`
	}{Lineups: legacy})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", compID, "lineups.yaml"), raw, 0o600))

	store, err := state.NewStore(dir) // the upgrade: the load repair runs
	require.NoError(t, err)
	eng := New(store)
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	semi0, semi1, final := bracket.Rounds[0][0], bracket.Rounds[0][1], bracket.Rounds[1][0]
	inForce := func(team, matchID string) string {
		got, err := eng.LineupInForce(compID, ids[team], matchID)
		require.NoError(t, err)
		require.True(t, got.Found, "%s at %s", team, matchID)
		return got.Lineup.Positions[domain.PositionNumbered(1)]
	}
	marker := func() bool {
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		return comp.RoundLineupsConverted
	}
	require.False(t, marker(), "precondition: the lineups for the second round wait for their teams to reach it")
	winnerA, loserA := semi0.SideA, semi0.SideB
	winnerB, loserB := semi1.SideA, semi1.SideB

	_, err = eng.OverrideBracketWinner(compID, semi0.ID, winnerA, 0)
	require.NoError(t, err)
	assert.Equal(t, winnerA+"-second", inForce(winnerA, final.ID), "the winner of the first semifinal has its second round lineup in the final")
	assert.Equal(t, winnerA+"-start", inForce(winnerA, semi0.ID), "and the starting lineup before it")
	assert.False(t, marker(), "the other semifinal is still to be played")

	_, err = eng.OverrideBracketWinner(compID, semi1.ID, winnerB, 0)
	require.NoError(t, err)
	assert.Equal(t, winnerB+"-second", inForce(winnerB, final.ID))
	assert.True(t, marker(), "the final's sides are decided, so nothing waits any more")
	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	for _, l := range lineups {
		assert.True(t, l.MatchID != "" || l.Round == 0, "no lineup for a later round is left: %+v", l)
	}
	for _, loser := range []string{loserA, loserB} {
		assert.Equal(t, loser+"-start", inForce(loser, final.ID), "a team that did not reach the final keeps its starting lineup")
	}
}
