package engine

// round_lineups_match_only_test.go pins, through the engine's own reads, what a
// team that v2.1.1 had a lineup entered for a match for, and none for a round,
// fields once the data folder is loaded (operator decision 2026-10-07: "Show what
// v2.1.1 showed"). v2.1.1 showed such a team its own lineup at that match, its
// starting lineup anywhere else, or nothing where it had none. A team now carries
// the lineup of its previous match, so each match it is seated in is given, as its
// own, what v2.1.1 showed there; for a team with no lineup at all that is an EMPTY
// lineup of its own, which the rule honours as the match's own, so the match shows
// none rather than what an earlier one does. The teams found are recorded once, so
// a lineup saved by this release afterwards carries as it should.

import (
	"slices"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const matchOnlyComp = "match-only-legacy"

// matchOnlyRecording is a four-team pool drawn and not yet played, whose data
// folder is as v2.1.1 left it: Alice has a lineup entered for Pool A-0 and none
// for a round; Bob has a starting lineup and a lineup entered for Pool A-1;
// Charlie and Dave have none.
type matchOnlyRecording struct {
	dir    string
	ids    map[string]string
	pool   []state.MatchResult
	legacy []domain.TeamLineup
}

func newMatchOnlyRecording(t *testing.T) matchOnlyRecording {
	t.Helper()
	_, seed, dir := setupTestEngine(t)
	createTestCompetition(t, seed, matchOnlyComp, "league", 4, func(c *state.Competition) {
		c.Kind, c.TeamSize, c.Status = "team", 3, state.CompStatusDrawReady
	})
	names := []string{"Alice", "Bob", "Charlie", "Dave"}
	saveTestParticipants(t, seed, matchOnlyComp, names)
	roster, err := seed.LoadParticipants(matchOnlyComp, false)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, p := range roster {
		ids[p.Name] = p.ID
	}
	bout := func(id string, round int, a, b string) state.MatchResult {
		return state.MatchResult{ID: id, SideA: a, SideAID: ids[a], SideB: b, SideBID: ids[b], Round: round}
	}
	pool := []state.MatchResult{
		bout("Pool A-0", 1, "Alice", "Bob"),
		bout("Pool A-1", 0, "Bob", "Charlie"),
		bout("Pool A-2", 1, "Charlie", "Dave"),
		bout("Pool A-3", 0, "Dave", "Alice"),
		bout("Pool A-4", 2, "Alice", "Charlie"),
		bout("Pool A-5", 2, "Bob", "Dave"),
	}
	require.NoError(t, seed.SavePoolMatches(matchOnlyComp, pool))
	lineup := func(team, matchID, tag string) domain.TeamLineup {
		return domain.TeamLineup{
			TeamID: ids[team], CompetitionID: matchOnlyComp, MatchID: matchID,
			Positions: map[domain.Position]string{domain.PositionNumbered(1): tag},
		}
	}
	legacy := []domain.TeamLineup{
		lineup("Alice", "Pool A-0", "Alice-entered"),
		lineup("Bob", "", "Bob-start"),
		lineup("Bob", "Pool A-1", "Bob-entered"),
	}
	writeLegacyLineups(t, seed, dir, matchOnlyComp, legacy)
	return matchOnlyRecording{dir: dir, ids: ids, pool: pool, legacy: legacy}
}

// open loads the data folder, which runs the load repair, and the engine over it.
func (r matchOnlyRecording) open(t *testing.T) (*state.Store, *Engine) {
	t.Helper()
	store, err := state.NewStore(r.dir)
	require.NoError(t, err)
	return store, New(store)
}

func (r matchOnlyRecording) legacyTeams() []string {
	return slices.Sorted(slices.Values([]string{r.ids["Alice"], r.ids["Bob"]}))
}

func inForceOf(t *testing.T, eng *Engine, team, matchID string) InForceLineup {
	t.Helper()
	got, err := eng.LineupInForce(matchOnlyComp, team, matchID)
	require.NoError(t, err)
	return got
}

// Every match shows what v2.1.1 showed, and a match v2.1.1 showed nothing at
// holds an empty lineup of its own: the answer is saved, names the match itself as
// its source, and holds no position, where carrying would have shown an earlier
// match's.
func TestRoundLineups_AMatchOnlyLegacyTeamFieldsWhatV211Showed(t *testing.T) {
	rec := newMatchOnlyRecording(t)
	store, eng := rec.open(t)

	for _, m := range rec.pool {
		for _, side := range []string{m.SideAID, m.SideBID} {
			got := inForceOf(t, eng, side, m.ID)
			assert.Equal(t, v211Reading(rec.legacy, side, m.ID, m.Round), legacyLineupTag(got.Lineup), "%s at %s", side, m.ID)
		}
	}
	for _, matchID := range []string{"Pool A-3", "Pool A-4"} {
		got := inForceOf(t, eng, rec.ids["Alice"], matchID)
		assert.True(t, got.Found, "v2.1.1 showed Alice nothing at %s, and the match holds a lineup of its own that says so", matchID)
		assert.Equal(t, matchID, got.Source.MatchID, "it is the match's own lineup, not one carried from Pool A-0")
		assert.Empty(t, got.Lineup.Positions)
	}
	assert.Equal(t, "Alice-entered", legacyLineupTag(inForceOf(t, eng, rec.ids["Alice"], "Pool A-0").Lineup), "the lineup entered is left as it is")
	for _, matchID := range []string{"Pool A-0", "Pool A-5"} {
		got := inForceOf(t, eng, rec.ids["Bob"], matchID)
		assert.Equal(t, "Bob-start", legacyLineupTag(got.Lineup), "Bob's starting lineup is what v2.1.1 showed at %s", matchID)
		assert.Equal(t, matchID, got.Source.MatchID)
	}
	for _, team := range []string{"Charlie", "Dave"} {
		assert.False(t, inForceOf(t, eng, rec.ids[team], "Pool A-2").Found, "%s had no lineup at all", team)
	}

	comp, err := store.LoadCompetition(matchOnlyComp)
	require.NoError(t, err)
	assert.Equal(t, rec.legacyTeams(), comp.RoundLineupsLegacy)
	assert.False(t, comp.RoundLineupsConverted, "the two legacy teams wait: a correction can still seat one in a match")
}

// A lineup saved by this release after the first pass is the operator's own, and
// carries to the team's later matches, through a write of the draw and a restart
// alike: the record of the legacy teams is what keeps it from being found as one.
func TestRoundLineups_ALineupSavedAfterTheFirstSettlementCarries(t *testing.T) {
	rec := newMatchOnlyRecording(t)
	store, eng := rec.open(t)
	charlie := rec.ids["Charlie"]
	require.False(t, inForceOf(t, eng, charlie, "Pool A-2").Found, "precondition: Charlie has no lineup")

	require.NoError(t, store.SetTeamLineup(matchOnlyComp, domain.TeamLineup{
		TeamID: charlie, CompetitionID: matchOnlyComp, MatchID: "Pool A-1",
		Positions: map[domain.Position]string{domain.PositionNumbered(1): "Charlie-new"},
	}, 3))

	carries := func(t *testing.T, eng *Engine) {
		t.Helper()
		for _, matchID := range []string{"Pool A-2", "Pool A-4"} {
			got := inForceOf(t, eng, charlie, matchID)
			assert.Equal(t, "Charlie-new", legacyLineupTag(got.Lineup), "Charlie at %s", matchID)
			assert.Equal(t, "Pool A-1", got.Source.MatchID, "carried from the match it was saved for")
		}
	}
	carries(t, eng)

	found, err := store.UpdatePoolMatchByID(matchOnlyComp, "Pool A-3", func(m *state.MatchResult) error {
		m.Status = state.MatchStatusRunning
		return nil
	})
	require.NoError(t, err)
	require.True(t, found)
	carries(t, eng)

	restarted, restartedEng := rec.open(t)
	carries(t, restartedEng)
	comp, err := restarted.LoadCompetition(matchOnlyComp)
	require.NoError(t, err)
	assert.Equal(t, rec.legacyTeams(), comp.RoundLineupsLegacy, "the legacy teams are the ones the first pass found")
}

// A discard takes the match lineups with the draw, and a team that was legacy only
// for them has nothing left to be shown: with none left the competition is
// marked, so the next draw is not pinned, and its matches carry from the
// starting lineup as they should.
func TestRoundLineups_ADiscardEndsTheConversionOfMatchOnlyLegacyTeams(t *testing.T) {
	rec := newMatchOnlyRecording(t)
	store, eng := rec.open(t)
	comp, err := store.LoadCompetition(matchOnlyComp)
	require.NoError(t, err)
	require.Equal(t, rec.legacyTeams(), comp.RoundLineupsLegacy, "precondition")

	require.NoError(t, eng.DiscardDraw(matchOnlyComp))

	comp, err = store.LoadCompetition(matchOnlyComp)
	require.NoError(t, err)
	assert.Empty(t, comp.RoundLineupsLegacy)
	assert.Empty(t, comp.RoundLineupsGiven)
	assert.True(t, comp.RoundLineupsConverted, "no legacy team remains")
	lineups, err := store.LoadTeamLineups(matchOnlyComp)
	require.NoError(t, err)
	for _, l := range lineups {
		assert.Empty(t, l.MatchID, "the lineups of the discarded draw's matches are gone")
	}

	// The next draw reuses the match ids, and carries: nothing is pinned.
	require.NoError(t, store.SavePoolMatches(matchOnlyComp, rec.pool))
	bob := inForceOf(t, eng, rec.ids["Bob"], "Pool A-0")
	assert.Equal(t, "Bob-start", legacyLineupTag(bob.Lineup))
	assert.Empty(t, bob.Source.MatchID, "Bob's starting lineup is carried, not copied onto the match")
	assert.False(t, inForceOf(t, eng, rec.ids["Alice"], "Pool A-3").Found, "Alice has no lineup")
}
