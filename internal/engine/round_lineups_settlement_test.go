package engine

import (
	"fmt"
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
// until no match can seat a team, and the engine's own write that advances a
// winner gives the team its lineup for the match it is seated in: the lineup in
// force for the final is the one saved for round 2 (stored round 1), for the
// finalists, and the round lineups are removed once the final's sides are
// decided.
func TestRoundLineups_GivenWhenTheEngineAdvancesAWinner(t *testing.T) {
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
	assert.False(t, marker(), "the final's sides are decided, but the competition is not completed: a correction can still seat another team")
	roundLineups := func(st *state.Store) (held int) {
		lineups, err := st.LoadTeamLineups(compID)
		require.NoError(t, err)
		for _, l := range lineups {
			if l.MatchID == "" && l.Round >= 1 {
				held++
			}
		}
		return held
	}
	assert.Equal(t, 4, roundLineups(store), "every team still holds its lineup for the second round")
	for _, loser := range []string{loserA, loserB} {
		assert.Equal(t, loser+"-start", inForce(loser, final.ID), "a team that did not reach the final keeps its starting lineup")
	}

	// The second semifinal's result was recorded the wrong way round, and is
	// corrected before the final is played: the other team is seated in the final,
	// with the lineup v2.1.1 showed there.
	_, err = eng.OverrideBracketWinner(compID, semi1.ID, loserB, 0)
	require.NoError(t, err)
	assert.Equal(t, loserB+"-second", inForce(loserB, final.ID), "the team the correction seats has its second round lineup in the final")
	assert.Equal(t, loserB+"-start", inForce(loserB, semi1.ID))

	// The competition is completed in a write the draw's hooks do not see; the
	// next load retires the round lineups, and what v2.1.1 showed stays.
	_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
		c.Status = state.CompStatusComplete
		return c, nil
	})
	require.NoError(t, err)
	restarted, err := state.NewStore(dir)
	require.NoError(t, err)
	eng = New(restarted)
	comp, err := restarted.LoadCompetition(compID)
	require.NoError(t, err)
	assert.True(t, comp.RoundLineupsConverted)
	assert.Zero(t, roundLineups(restarted), "no lineup for a later round is left")
	assert.Equal(t, winnerA+"-second", inForce(winnerA, final.ID))
	assert.Equal(t, loserB+"-second", inForce(loserB, final.ID))
}

// legacyLineupTag is what a lineup saved by v2.1.1's Lineups page is told apart
// by: the name in its first position.
func legacyLineupTag(l domain.TeamLineup) string { return l.Positions[domain.PositionNumbered(1)] }

// v211Reading is the lineup v2.1.1 showed a team at a match read at matchRound,
// from the lineups its Lineups page had saved: the team's own lineup for the
// match; else the round lineup with the highest round at or below the match's;
// else the highest round there was. A lineup entered for another match was never
// carried. This is v2.1.1's state.FindBestLineup, restated so the code under
// test is not its own oracle.
func v211Reading(lineups []domain.TeamLineup, team, matchID string, matchRound int) string {
	for _, l := range lineups {
		if l.TeamID == team && l.MatchID == matchID {
			return legacyLineupTag(l)
		}
	}
	bestRound, highestRound := -1, -1
	var best, highest string
	for _, l := range lineups {
		if l.TeamID != team || l.MatchID != "" {
			continue
		}
		if l.Round <= matchRound && l.Round > bestRound {
			bestRound, best = l.Round, legacyLineupTag(l)
		}
		if l.Round > highestRound {
			highestRound, highest = l.Round, legacyLineupTag(l)
		}
	}
	if bestRound >= 0 {
		return best
	}
	return highest
}

// writeLegacyLineups leaves compID's data folder as v2.1.1 did: lineups.yaml as
// its Lineups page wrote it, and no marker (it recorded none). Opening the folder
// again with state.NewStore then runs the load repair.
func writeLegacyLineups(t *testing.T, store *state.Store, dir, compID string, lineups []domain.TeamLineup) {
	t.Helper()
	stored, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	stored.RoundLineupsConverted = false
	require.NoError(t, store.SaveCompetition(stored))
	raw, err := yaml.Marshal(struct {
		Lineups []domain.TeamLineup `yaml:"lineups"`
	}{Lineups: lineups})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", compID, "lineups.yaml"), raw, 0o600))
}

// TestRoundLineups_EveryMatchOfARealDrawShowsWhatV211Showed draws a team
// competition, gives every team a lineup for every round its pool plays (as
// v2.1.1's Lineups page could), runs the load repair, and compares the lineup in
// force at every match each team is seated in with what v2.1.1 showed there.
// The pools of a real draw number their bouts in playing order, and a team's
// stored rounds in that order do not rise, so a lineup given to "the team's first
// match at that round or later" would show another round's lineup at the matches
// between.
func TestRoundLineups_EveryMatchOfARealDrawShowsWhatV211Showed(t *testing.T) {
	for _, tc := range []struct {
		format   string
		teams    int
		poolSize int
		courts   []string
	}{
		{state.CompFormatLeague, 5, 5, []string{"A"}},
		{state.CompFormatLeague, 6, 6, []string{"A", "B"}},
		{state.CompFormatMixed, 8, 4, []string{"A", "B"}},
	} {
		t.Run(fmt.Sprintf("%s of %d", tc.format, tc.teams), func(t *testing.T) {
			_, seed, dir := setupTestEngine(t)
			const compID = "round-lineups-real-draw"
			createTestCompetition(t, seed, compID, tc.format, tc.poolSize, func(c *state.Competition) {
				c.Kind, c.TeamSize, c.Courts = "team", 3, tc.courts
			})
			names := make([]string, tc.teams)
			for i := range names {
				names[i] = fmt.Sprintf("Team%02d", i)
			}
			saveTestParticipants(t, seed, compID, names)
			require.NoError(t, New(seed).StartCompetition(compID))
			roster, err := seed.LoadParticipants(compID, false)
			require.NoError(t, err)
			ids := map[string]string{}
			for _, p := range roster {
				ids[p.Name] = p.ID
			}
			pool, err := seed.LoadPoolMatches(compID)
			require.NoError(t, err)
			lastRound := 0
			for _, m := range pool {
				lastRound = max(lastRound, m.Round)
			}
			require.Positive(t, lastRound, "precondition: the pool has rounds to read lineups by")

			var legacy []domain.TeamLineup
			for _, name := range names {
				for round := 0; round <= lastRound; round++ {
					legacy = append(legacy, domain.TeamLineup{
						TeamID: ids[name], CompetitionID: compID, Round: round,
						Positions: map[domain.Position]string{domain.PositionNumbered(1): fmt.Sprintf("%s-round%d", name, round)},
					})
				}
			}
			writeLegacyLineups(t, seed, dir, compID, legacy)

			store, err := state.NewStore(dir) // the upgrade: the load repair runs
			require.NoError(t, err)
			everyMatchShowsWhatV211Showed := func(st *state.Store) {
				t.Helper()
				eng := New(st)
				for _, m := range pool {
					for _, side := range []string{m.SideAID, m.SideBID} {
						want := v211Reading(legacy, side, m.ID, max(m.Round, 0))
						got, err := eng.LineupInForce(compID, side, m.ID)
						require.NoError(t, err)
						assert.Equal(t, want, legacyLineupTag(got.Lineup), "the lineup in force at %s (round %d) for %s", m.ID, m.Round, side)
					}
				}
			}
			roundLineupsHeld := func(st *state.Store) (held int, marked bool) {
				t.Helper()
				stored, err := st.LoadTeamLineups(compID)
				require.NoError(t, err)
				for _, l := range stored {
					if l.MatchID == "" && l.Round >= 1 {
						held++
					}
				}
				comp, err := st.LoadCompetition(compID)
				require.NoError(t, err)
				return held, comp.RoundLineupsConverted
			}

			everyMatchShowsWhatV211Showed(store)
			held, marked := roundLineupsHeld(store)
			assert.Equal(t, tc.teams*lastRound, held, "every round lineup stays until the competition is completed")
			assert.False(t, marked)

			// The competition is completed in a write the draw's hooks do not see:
			// the next load retires the round lineups, and every match still shows
			// what v2.1.1 showed.
			_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
				c.Status = state.CompStatusComplete
				return c, nil
			})
			require.NoError(t, err)
			restarted, err := state.NewStore(dir)
			require.NoError(t, err)
			everyMatchShowsWhatV211Showed(restarted)
			held, marked = roundLineupsHeld(restarted)
			assert.Zero(t, held, "every match holds its lineup, so the round lineups are gone")
			assert.True(t, marked)
		})
	}
}

// TestRoundLineups_ARetriedDrawIsGivenItsOwnLineups: a draw that fails after it
// wrote its matches (here the fill-bracket half-capacity refusal, which
// generatePoolPreviewBracket raises once generatePools has saved the pools) leaves
// the competition in Setup, with those matches on disk. The write that saved them
// gave every legacy team a lineup at each match it was seated in and recorded the
// pairs. The operator seeds the field, as the refusal advises, and retries: the new
// draw reuses the match ids with other pairings, so what the failed attempt
// settled belongs to a draw that no longer exists. The retried draw is settled
// from nothing, and each team shows what v2.1.1 showed at the matches of the draw
// that stands.
func TestRoundLineups_ARetriedDrawIsGivenItsOwnLineups(t *testing.T) {
	_, seed, dir := setupTestEngine(t)
	const compID = "round-lineups-retried-draw"
	createTestCompetition(t, seed, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Kind, c.TeamSize = "team", 3
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		c.Courts = []string{"A", "B", "C", "D"}
	})
	names := make([]string, 18)
	players := make([]domain.Player, len(names))
	for i := range names {
		names[i] = fmt.Sprintf("Team%02d", i)
		players[i] = domain.Player{Name: names[i], Dojo: fmt.Sprintf("Dojo%02d", i)}
	}
	require.NoError(t, seed.SaveParticipants(compID, players))
	roster, err := seed.LoadParticipants(compID, false)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, p := range roster {
		ids[p.Name] = p.ID
	}
	// Every team has a lineup for each round its pool could play, as v2.1.1's
	// Lineups page could save them, so the lineup shown at a match depends on the
	// round the match was read at.
	var legacy []domain.TeamLineup
	for _, name := range names {
		for round := 0; round <= 3; round++ {
			legacy = append(legacy, domain.TeamLineup{
				TeamID: ids[name], CompetitionID: compID, Round: round,
				Positions: map[domain.Position]string{domain.PositionNumbered(1): fmt.Sprintf("%s-round%d", name, round)},
			})
		}
	}
	writeLegacyLineups(t, seed, dir, compID, legacy)
	store, err := state.NewStore(dir) // the upgrade: the load repair runs
	require.NoError(t, err)
	eng := New(store)

	err = eng.StartCompetition(compID)
	var refusal *ValidationError
	require.ErrorAs(t, err, &refusal, "precondition: the unseeded draw is refused after its pools were saved")
	failed, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, failed, "precondition: the failed attempt left its matches on disk")
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusSetup, comp.Status)
	require.NotEmpty(t, comp.RoundLineupsGiven, "precondition: the failed attempt's matches were settled, and the pairs recorded")
	// What the failed attempt gave a team at a match is marked, so a team seated at
	// the same match id by the retried draw shows whether it was given its lineup
	// again or kept the failed attempt's.
	failedLineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	for _, l := range failedLineups {
		if l.MatchID != "" {
			l.Positions = map[domain.Position]string{domain.PositionNumbered(1): "from-the-failed-draw"}
			require.NoError(t, store.SetTeamLineup(compID, l, 3))
		}
	}

	var seeds []domain.SeedAssignment
	for i := 0; i < 4; i++ {
		seeds = append(seeds, domain.SeedAssignment{Name: names[i], SeedRank: i + 1})
	}
	require.NoError(t, store.SaveSeeds(compID, seeds))
	require.NoError(t, eng.StartCompetition(compID), "the seeded draw succeeds")

	pool, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	seated := map[string]bool{}
	for _, m := range pool {
		seated[m.SideAID+"|"+m.ID] = true
		seated[m.SideBID+"|"+m.ID] = true
	}
	stored, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	for _, l := range stored {
		if l.MatchID != "" {
			assert.Truef(t, seated[l.TeamID+"|"+l.MatchID], "team %s holds a lineup for %s, where the draw that stands does not seat it", l.TeamID, l.MatchID)
		}
	}
	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	for team, matchIDs := range comp.RoundLineupsGiven {
		for _, id := range matchIDs {
			assert.Truef(t, seated[team+"|"+id], "the record lists team %s at %s, where the draw that stands does not seat it", team, id)
		}
	}
	for _, m := range pool {
		for _, side := range []string{m.SideAID, m.SideBID} {
			got, err := eng.LineupInForce(compID, side, m.ID)
			require.NoError(t, err)
			assert.Equal(t, v211Reading(legacy, side, m.ID, max(m.Round, 0)), legacyLineupTag(got.Lineup),
				"the lineup in force at %s (round %d) for %s", m.ID, m.Round, side)
		}
	}
}

// TestRoundLineups_EveryFormatsDrawStartsFromNoLineupsOfAnEarlierDraw: whichever
// generator the pipeline runs (pools, a knockout, a Swiss round), a draw from
// Setup first clears the match lineups and the record of settled pairs that an
// attempt that failed after its writes left behind, and keeps the round lineups.
func TestRoundLineups_EveryFormatsDrawStartsFromNoLineupsOfAnEarlierDraw(t *testing.T) {
	for _, format := range []string{state.CompFormatMixed, state.CompFormatLeague, state.CompFormatKnockout, state.CompFormatSwiss} {
		t.Run(format, func(t *testing.T) {
			_, seed, dir := setupTestEngine(t)
			const compID = "round-lineups-earlier-draw"
			createTestCompetition(t, seed, compID, format, 3, func(c *state.Competition) {
				c.Kind, c.TeamSize = "team", 3
			})
			names := []string{"Tora", "Usagi", "Kuma", "Saru", "Inu", "Neko", "Tori", "Uma"}
			saveTestParticipants(t, seed, compID, names)
			roster, err := seed.LoadParticipants(compID, false)
			require.NoError(t, err)
			ids := map[string]string{}
			for _, p := range roster {
				ids[p.Name] = p.ID
			}
			lineup := func(team, matchID string, round int) domain.TeamLineup {
				return domain.TeamLineup{
					TeamID: ids[team], CompetitionID: compID, Round: round, MatchID: matchID,
					Positions: map[domain.Position]string{domain.PositionNumbered(1): team},
				}
			}
			// Every team has a lineup for the second round (stored 1), so the
			// competition has legacy teams and the record is kept; one of them
			// also has a lineup for a match of an earlier draw, and that draw's
			// pair is recorded.
			var legacy []domain.TeamLineup
			for _, name := range names {
				legacy = append(legacy, lineup(name, "", 1))
			}
			legacy = append(legacy, lineup("Tora", "earlier-draw-match", 0))
			writeLegacyLineups(t, seed, dir, compID, legacy)
			_, err = seed.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
				c.RoundLineupsGiven = map[string][]string{ids["Tora"]: {"earlier-draw-match"}}
				return c, nil
			})
			require.NoError(t, err)
			store, err := state.NewStore(dir)
			require.NoError(t, err)

			require.NoError(t, New(store).StartCompetition(compID))

			lineups, err := store.LoadTeamLineups(compID)
			require.NoError(t, err)
			for _, l := range lineups {
				assert.NotEqual(t, "earlier-draw-match", l.MatchID, "the earlier draw's lineup is gone")
			}
			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			assert.NotContains(t, comp.RoundLineupsGiven[ids["Tora"]], "earlier-draw-match", "and so is its pair in the record")
			held := 0
			for _, l := range lineups {
				if l.MatchID == "" && l.Round >= 1 {
					held++
				}
			}
			assert.Equal(t, len(names), held, "the round lineups stay")
			assert.False(t, comp.RoundLineupsConverted, "the legacy teams still wait for the competition to be completed")
		})
	}
}

// TestRoundLineups_ASemiFinalOnlyLineupDoesNotHideTheRoundLineupAtTheFinal: a
// team has a lineup for the second round (stored 1) and one entered for its
// semi-final alone. v2.1.1 showed the semi-final lineup at the semi-final only,
// and the round lineup at the semi-final's round and after it, so again at the
// final: it must not be given up because the semi-final has a lineup of its own.
func TestRoundLineups_ASemiFinalOnlyLineupDoesNotHideTheRoundLineupAtTheFinal(t *testing.T) {
	_, seed, dir := setupTestEngine(t)
	const compID = "round-lineups-semi-final"
	createTestCompetition(t, seed, compID, "knockout", 3, func(c *state.Competition) {
		c.Kind, c.TeamSize = "team", 3
	})
	saveTestParticipants(t, seed, compID, []string{"Tora", "Usagi", "Kuma", "Saru", "Inu", "Neko", "Tori", "Uma"})
	eng := New(seed)
	require.NoError(t, eng.StartCompetition(compID))
	bracket, err := seed.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, bracket.Rounds, 3)
	for _, m := range bracket.Rounds[0] {
		_, err := eng.OverrideBracketWinner(compID, m.ID, m.SideA, 0)
		require.NoError(t, err)
	}
	bracket, err = seed.LoadBracket(compID)
	require.NoError(t, err)
	semi, otherSemi := bracket.Rounds[1][0], bracket.Rounds[1][1]
	team := semi.SideAID
	require.NotEmpty(t, team)
	_, err = eng.OverrideBracketWinner(compID, semi.ID, semi.SideA, 0)
	require.NoError(t, err)
	bracket, err = seed.LoadBracket(compID)
	require.NoError(t, err)
	final := bracket.Rounds[2][0]
	require.True(t, final.SideAID == team || final.SideBID == team, "precondition: the team is seated in the final")
	var quarterFinal string
	for _, m := range bracket.Rounds[0] {
		if m.SideAID == team || m.SideBID == team {
			quarterFinal = m.ID
		}
	}
	require.NotEmpty(t, quarterFinal)

	legacy := []domain.TeamLineup{
		{TeamID: team, CompetitionID: compID, Round: 0, Positions: map[domain.Position]string{domain.PositionNumbered(1): "start"}},
		{TeamID: team, CompetitionID: compID, Round: 1, Positions: map[domain.Position]string{domain.PositionNumbered(1): "round2-onwards"}},
		{TeamID: team, CompetitionID: compID, MatchID: semi.ID, Positions: map[domain.Position]string{domain.PositionNumbered(1): "semi-final-only"}},
	}
	writeLegacyLineups(t, seed, dir, compID, legacy)
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng = New(store)
	inForce := func(matchID string) string {
		got, err := eng.LineupInForce(compID, team, matchID)
		require.NoError(t, err)
		require.True(t, got.Found, matchID)
		return legacyLineupTag(got.Lineup)
	}
	roundLineupKept := func() bool {
		lineups, err := store.LoadTeamLineups(compID)
		require.NoError(t, err)
		for _, l := range lineups {
			if l.TeamID == team && l.MatchID == "" && l.Round == 1 {
				return true
			}
		}
		return false
	}

	assert.Equal(t, "start", inForce(quarterFinal))
	assert.Equal(t, "semi-final-only", inForce(semi.ID))
	assert.Equal(t, "round2-onwards", inForce(final.ID), "v2.1.1 showed the round lineup again at the final")
	assert.True(t, roundLineupKept(), "the competition is on, so the round lineup is kept")

	_, err = eng.OverrideBracketWinner(compID, otherSemi.ID, otherSemi.SideA, 0)
	require.NoError(t, err)

	assert.Equal(t, "round2-onwards", inForce(final.ID))
	assert.Equal(t, "semi-final-only", inForce(semi.ID))
	assert.True(t, roundLineupKept(), "the final has both its sides, but the competition is not completed")
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.False(t, comp.RoundLineupsConverted)

	// Completed in a write the draw's hooks do not see: the next load retires it,
	// and the final still shows the round lineup.
	_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
		c.Status = state.CompStatusComplete
		return c, nil
	})
	require.NoError(t, err)
	store, err = state.NewStore(dir)
	require.NoError(t, err)
	eng = New(store)
	assert.Equal(t, "round2-onwards", inForce(final.ID))
	assert.Equal(t, "semi-final-only", inForce(semi.ID))
	assert.False(t, roundLineupKept(), "the round lineup has been given to every match it was shown at")
	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.True(t, comp.RoundLineupsConverted)
}

// TestRoundLineups_AnEditToAnEarlierMatchDoesNotChangeALaterOne: after the
// upgrade every match a team is seated in holds a lineup of its own, so a lineup
// the operator then enters for an earlier match changes that match and nothing
// after it. (A match without a lineup of its own would carry the edit.)
func TestRoundLineups_AnEditToAnEarlierMatchDoesNotChangeALaterOne(t *testing.T) {
	_, seed, dir := setupTestEngine(t)
	const compID = "round-lineups-edit"
	createTestCompetition(t, seed, compID, "league", 4, func(c *state.Competition) {
		c.Kind, c.TeamSize = "team", 3
	})
	names := []string{"Alice", "Bob", "Charlie", "Dave"}
	saveTestParticipants(t, seed, compID, names)
	roster, err := seed.LoadParticipants(compID, false)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, p := range roster {
		ids[p.Name] = p.ID
	}
	// A pool of four numbered as a real draw numbers it: Alice plays rounds 1, 0, 2.
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
	require.NoError(t, seed.SavePoolMatches(compID, pool))
	var legacy []domain.TeamLineup
	for _, name := range names {
		for round := 0; round <= 2; round++ {
			legacy = append(legacy, domain.TeamLineup{
				TeamID: ids[name], CompetitionID: compID, Round: round,
				Positions: map[domain.Position]string{domain.PositionNumbered(1): fmt.Sprintf("%s-round%d", name, round)},
			})
		}
	}
	writeLegacyLineups(t, seed, dir, compID, legacy)
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)
	inForce := func(matchID string) string {
		got, err := eng.LineupInForce(compID, ids["Alice"], matchID)
		require.NoError(t, err)
		require.True(t, got.Found, matchID)
		return legacyLineupTag(got.Lineup)
	}
	assert.Equal(t, "Alice-round0", inForce("Pool A-3"), "v2.1.1 showed the starting lineup at Alice's second bout")

	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: ids["Alice"], MatchID: "Pool A-0",
		Positions: map[domain.Position]string{domain.PositionNumbered(1): "edited"},
	}, 3))

	assert.Equal(t, "edited", inForce("Pool A-0"), "the match that was edited")
	assert.Equal(t, "Alice-round0", inForce("Pool A-3"), "a later match keeps what v2.1.1 showed there")
	assert.Equal(t, "Alice-round2", inForce("Pool A-4"))
}

// TestRoundLineups_AfterDiscardDrawTheNextDrawIsGivenItsReadingsAgain: discarding
// a draw takes the lineups saved for its matches with it, and the pairs the
// conversion had settled: the next draw reuses the match ids, and its matches
// must be given their lineups again, not left as the operator's removals.
func TestRoundLineups_AfterDiscardDrawTheNextDrawIsGivenItsReadingsAgain(t *testing.T) {
	_, seed, dir := setupTestEngine(t)
	const compID = "round-lineups-discard"
	createTestCompetition(t, seed, compID, "league", 4, func(c *state.Competition) {
		c.Kind, c.TeamSize, c.Status = "team", 3, state.CompStatusDrawReady
	})
	names := []string{"Alice", "Bob", "Charlie", "Dave"}
	saveTestParticipants(t, seed, compID, names)
	roster, err := seed.LoadParticipants(compID, false)
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
	require.NoError(t, seed.SavePoolMatches(compID, pool))
	var legacy []domain.TeamLineup
	for _, name := range names {
		for round := 0; round <= 2; round++ {
			legacy = append(legacy, domain.TeamLineup{
				TeamID: ids[name], CompetitionID: compID, Round: round,
				Positions: map[domain.Position]string{domain.PositionNumbered(1): fmt.Sprintf("%s-round%d", name, round)},
			})
		}
	}
	writeLegacyLineups(t, seed, dir, compID, legacy)
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)
	everyMatchShowsWhatV211Showed := func() {
		t.Helper()
		for _, m := range pool {
			for _, side := range []string{m.SideAID, m.SideBID} {
				got, err := eng.LineupInForce(compID, side, m.ID)
				require.NoError(t, err)
				assert.Equal(t, v211Reading(legacy, side, m.ID, m.Round), legacyLineupTag(got.Lineup), "%s at %s", side, m.ID)
			}
		}
	}
	everyMatchShowsWhatV211Showed()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotEmpty(t, comp.RoundLineupsGiven, "precondition: the pairs of the draw are recorded as settled")

	require.NoError(t, eng.DiscardDraw(compID))

	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Empty(t, comp.RoundLineupsGiven, "the pairs of the discarded draw are not kept")
	assert.False(t, comp.RoundLineupsConverted)

	// The next draw reuses the match ids (here with the same pairings).
	require.NoError(t, store.SavePoolMatches(compID, pool))
	everyMatchShowsWhatV211Showed()
}
