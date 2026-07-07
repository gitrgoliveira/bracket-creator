package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rankedStanding builds a standing carrying both a Points value (drives the
// Points-equality tie walk) and a pre-assigned sequential Rank, matching the
// slice CalculatePoolStandings hands to applyJointThirdRanks.
func rankedStanding(name string, points, rank int) state.PlayerStanding {
	return state.PlayerStanding{Player: domain.Player{Name: name}, Points: points, Rank: rank}
}

// ranksOf returns the Rank of each row in order, for compact assertions.
func ranksOf(sorted []state.PlayerStanding) []int {
	out := make([]int, len(sorted))
	for i, s := range sorted {
		out[i] = s.Rank
	}
	return out
}

// TestApplyJointThirdRanks covers the kendo joint-3rd convention: a genuine tie
// at 3rd place (or below) shares a rank when LeagueTwoThirdPlaces is on, so the
// standings table and podium show two joint 3rds instead of relabeling 4th.
func TestApplyJointThirdRanks(t *testing.T) {
	leagueOn := &state.Competition{Format: state.CompFormatLeague, LeagueTwoThirdPlaces: true}
	leagueOff := &state.Competition{Format: state.CompFormatLeague, LeagueTwoThirdPlaces: false}

	t.Run("kendo league on, genuine 3rd/4th tie → shared rank 3", func(t *testing.T) {
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 90, 2),
			rankedStanding("C", 50, 3), rankedStanding("D", 50, 4),
		}
		applyJointThirdRanks(leagueOn, sorted, false)
		assert.Equal(t, []int{1, 2, 3, 3}, ranksOf(sorted))
	})

	t.Run("setting off → ranks stay sequential (naginata single 3rd)", func(t *testing.T) {
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 90, 2),
			rankedStanding("C", 50, 3), rankedStanding("D", 50, 4),
		}
		applyJointThirdRanks(leagueOff, sorted, false)
		assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(sorted))
	})

	t.Run("three-way 3rd tie → shared rank 3 for all", func(t *testing.T) {
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 90, 2),
			rankedStanding("C", 50, 3), rankedStanding("D", 50, 4), rankedStanding("E", 50, 5),
		}
		applyJointThirdRanks(leagueOn, sorted, false)
		assert.Equal(t, []int{1, 2, 3, 3, 3}, ranksOf(sorted))
	})

	t.Run("no tie → ranks unchanged", func(t *testing.T) {
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 90, 2),
			rankedStanding("C", 80, 3), rankedStanding("D", 70, 4),
		}
		applyJointThirdRanks(leagueOn, sorted, false)
		assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(sorted))
	})

	t.Run("tie straddling 2nd/3rd → NOT shared (top-2 stays distinct)", func(t *testing.T) {
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 80, 2),
			rankedStanding("C", 80, 3), rankedStanding("D", 40, 4),
		}
		applyJointThirdRanks(leagueOn, sorted, false)
		assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(sorted),
			"a 2nd/3rd tie must be decided by a tie-breaker, never shared")
	})

	t.Run("1st/2nd tie → NOT shared", func(t *testing.T) {
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 100, 2),
			rankedStanding("C", 50, 3), rankedStanding("D", 40, 4),
		}
		applyJointThirdRanks(leagueOn, sorted, false)
		assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(sorted))
	})

	t.Run("pool has manual overrides → not collapsed (operator order wins)", func(t *testing.T) {
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 90, 2),
			rankedStanding("C", 50, 3), rankedStanding("D", 50, 4),
		}
		applyJointThirdRanks(leagueOn, sorted, true)
		assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(sorted))
	})

	t.Run("non-league (mixed) with setting on → not collapsed", func(t *testing.T) {
		mixed := &state.Competition{Format: state.CompFormatMixed, LeagueTwoThirdPlaces: true}
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 90, 2),
			rankedStanding("C", 50, 3), rankedStanding("D", 50, 4),
		}
		applyJointThirdRanks(mixed, sorted, false)
		assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(sorted),
			"mixed pools feed knockout seeding and must keep sequential ranks")
	})

	t.Run("nil comp / empty slice → no panic", func(t *testing.T) {
		applyJointThirdRanks(nil, nil, false)
		var empty []state.PlayerStanding
		applyJointThirdRanks(leagueOn, empty, false)
		assert.Empty(t, empty)
	})
}

// ranksByName maps a single pool's standings to name→rank for order-independent
// assertions.
func ranksByName(pool []state.PlayerStanding) map[string]int {
	out := make(map[string]int, len(pool))
	for _, s := range pool {
		out[s.Player.Name] = s.Rank
	}
	return out
}

// setupIndividualLeagueThirdTie builds a 4-player INDIVIDUAL league where A and
// B finish clear 1st/2nd and C/D are genuinely tied for 3rd (each: 0 wins, 2
// losses, 1 draw between themselves, symmetric points). Returns the engine +
// store so the test can toggle LeagueTwoThirdPlaces and recompute standings.
func setupIndividualLeagueThirdTie(t *testing.T, compID string, twoThird bool) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   compID,
		Name:                 "Ind League",
		Format:               state.CompFormatLeague,
		Status:               state.CompStatusPools,
		Courts:               []string{"A"},
		LeagueTwoThirdPlaces: twoThird,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "A"}, {Name: "B"}, {Name: "C"}, {Name: "D"},
		}},
	}))
	winBy := func(id, a, b, winner string) state.MatchResult {
		return state.MatchResult{ID: id, SideA: a, SideB: b, Winner: winner,
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted, Court: "A"}
	}
	matches := []state.MatchResult{
		winBy("Pool A-0", "A", "B", "A"),
		winBy("Pool A-1", "A", "C", "A"),
		winBy("Pool A-2", "A", "D", "A"),
		winBy("Pool A-3", "B", "C", "B"),
		winBy("Pool A-4", "B", "D", "B"),
		// C vs D: hikiwake (draw) → C and D stay tied for 3rd.
		{ID: "Pool A-5", SideA: "C", SideB: "D", Decision: string(domain.DecisionHikiwake),
			Status: state.MatchStatusCompleted, Court: "A"},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
	return eng, store
}

// scoreIndividualTB finds the injected ippon-shobu (TB) bout between the two
// named players and marks it complete with the given winner, mirroring the
// operator running the tie-breaker.
func scoreIndividualTB(t *testing.T, store *state.Store, compID, winner string) {
	t.Helper()
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	found := false
	for i := range all {
		if IsTiebreakerMatchID(all[i].ID) && all[i].Winner == "" {
			all[i].Status = state.MatchStatusCompleted
			all[i].Winner = winner
			all[i].IpponsA = []string{"M"}
			found = true
		}
	}
	require.True(t, found, "expected an unscored TB match to score")
	require.NoError(t, store.SavePoolMatches(compID, all))
}

// TestIndividualLeague_ThirdPlaceTie_TieBreak proves an individual league earns
// its podium places like a bracket: a 3rd-place tie is broken by an ippon-shobu
// bout (blocking completion) UNLESS the sanctioned kendo joint-3rd is enabled.
func TestIndividualLeague_ThirdPlaceTie_TieBreak(t *testing.T) {
	t.Run("setting off (naginata) → TB injected, blocks, then single 3rd", func(t *testing.T) {
		compID := "ind-league-break"
		eng, store := setupIndividualLeagueThirdTie(t, compID, false)

		// Regular matches complete with a 3rd/4th tie → a TB must be injected and
		// completion blocked (the league cannot finish with an unearned tie).
		outcome, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteTiebreakInjected, outcome, "3rd-place tie must hold a tie-breaker")

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		tbCount := 0
		for _, m := range matches {
			if IsTiebreakerMatchID(m.ID) {
				tbCount++
			}
		}
		assert.Equal(t, 1, tbCount, "exactly one C-vs-D ippon-shobu bout")

		// Still blocked while the TB is unscored.
		comp, _ := store.LoadCompetition(compID)
		assert.Equal(t, state.CompStatusPools, comp.Status)

		// Operator scores the tie-breaker (C beats D) → competition completes with
		// C 3rd and D 4th (distinct, a single 3rd place).
		scoreIndividualTB(t, store, compID, "C")
		outcome, err = eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteTransitioned, outcome)

		standings, err := eng.CalculatePoolStandings(compID)
		require.NoError(t, err)
		ranks := ranksByName(standings["Pool A"])
		assert.Equal(t, 3, ranks["C"], "TB winner takes the earned 3rd")
		assert.Equal(t, 4, ranks["D"], "TB loser is 4th, not a joint 3rd")
	})

	t.Run("setting on (kendo) → no TB, completes as joint 3rd", func(t *testing.T) {
		compID := "ind-league-joint"
		eng, store := setupIndividualLeagueThirdTie(t, compID, true)

		outcome, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteTransitioned, outcome, "joint-3rd is sanctioned, no tie-breaker")

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range matches {
			assert.False(t, IsTiebreakerMatchID(m.ID), "no TB bout when joint-3rd is enabled")
		}

		standings, err := eng.CalculatePoolStandings(compID)
		require.NoError(t, err)
		ranks := ranksByName(standings["Pool A"])
		assert.Equal(t, 3, ranks["C"])
		assert.Equal(t, 3, ranks["D"], "both share the sanctioned joint 3rd")
	})
}

// TestTieNeedsIndividualBreak covers the gate directly: leagues break band ties
// (minus the joint-3rd exemption); mixed pools break only advancement ties.
func TestTieNeedsIndividualBreak(t *testing.T) {
	grp := func(n int) []state.PlayerStanding { return make([]state.PlayerStanding, n) }

	t.Run("league breaks a 3rd/4th tie by default", func(t *testing.T) {
		comp := &state.Competition{Format: state.CompFormatLeague}
		assert.True(t, tieNeedsIndividualBreak(comp, []int{2, 3}, grp(2), 2))
	})
	t.Run("league exempts the joint-3rd tie when enabled", func(t *testing.T) {
		comp := &state.Competition{Format: state.CompFormatLeague, LeagueTwoThirdPlaces: true}
		assert.False(t, tieNeedsIndividualBreak(comp, []int{2, 3}, grp(2), 2))
	})
	t.Run("league still breaks a 1st/2nd tie even with joint-3rd on", func(t *testing.T) {
		comp := &state.Competition{Format: state.CompFormatLeague, LeagueTwoThirdPlaces: true}
		assert.True(t, tieNeedsIndividualBreak(comp, []int{0, 1}, grp(2), 2))
	})
	t.Run("league leaves a below-band 5th/6th tie unbroken", func(t *testing.T) {
		comp := &state.Competition{Format: state.CompFormatLeague} // effectiveTopN=3
		assert.False(t, tieNeedsIndividualBreak(comp, []int{4, 5}, grp(2), 2))
	})
	t.Run("mixed pool uses the advancement cut, not the band", func(t *testing.T) {
		comp := &state.Competition{Format: state.CompFormatMixed}
		// poolWinners=2: a 3rd/4th tie does not affect who advances.
		assert.False(t, tieNeedsIndividualBreak(comp, []int{2, 3}, grp(2), 2))
		assert.True(t, tieNeedsIndividualBreak(comp, []int{1, 2}, grp(2), 2))
	})
}

// TestCalculatePoolStandings_JointThirdWiring proves the joint-3rd convention is
// wired end-to-end through CalculatePoolStandings for an individual league, not
// just in the pure helper.
func TestCalculatePoolStandings_JointThirdWiring(t *testing.T) {
	t.Run("setting on → C and D share rank 3", func(t *testing.T) {
		eng, _ := setupIndividualLeagueThirdTie(t, "ind-league-on", true)
		standings, err := eng.CalculatePoolStandings("ind-league-on")
		require.NoError(t, err)
		ranks := ranksByName(standings["Pool A"])
		assert.Equal(t, 1, ranks["A"])
		assert.Equal(t, 2, ranks["B"])
		assert.Equal(t, 3, ranks["C"], "C shares 3rd")
		assert.Equal(t, 3, ranks["D"], "D shares 3rd (not relabeled 4th)")
	})

	t.Run("setting off → C and D keep distinct 3rd/4th (single 3rd)", func(t *testing.T) {
		eng, _ := setupIndividualLeagueThirdTie(t, "ind-league-off", false)
		standings, err := eng.CalculatePoolStandings("ind-league-off")
		require.NoError(t, err)
		ranks := ranksByName(standings["Pool A"])
		assert.Equal(t, 1, ranks["A"])
		assert.Equal(t, 2, ranks["B"])
		// C and D remain sequential 3 and 4 (order within the tie is unspecified).
		assert.ElementsMatch(t, []int{3, 4}, []int{ranks["C"], ranks["D"]})
	})
}
