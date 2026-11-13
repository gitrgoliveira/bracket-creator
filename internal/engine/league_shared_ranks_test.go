package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
)

// A league table reads a shared place for a tie nobody breaks (operator
// decisions N2a and N2b): a tie the operator accepted as shared ranks reads
// its best place (1, 1, 3), and a tie outside the band a tie is broken in
// reads its better place (4, 4). applyJointThirdRanks is the one place that
// gives a level group one rank; the rows keep their order, so the first row
// (the winner line) does not move.

// stamped marks rows [from, to] (0-based, inclusive) with the stamp the earlier
// passes of computeStandingsFrom leave: Tied for an open tie in the band,
// TieOutOfBand for a tie outside it.
func stamped(sorted []state.PlayerStanding, from, to int, tied, outOfBand bool) []state.PlayerStanding {
	for i := from; i <= to; i++ {
		sorted[i].Tied = tied
		sorted[i].TieOutOfBand = outOfBand
	}
	return sorted
}

func TestApplyJointThirdRanks_SharedPlaces(t *testing.T) {
	accepted := &state.Competition{Format: state.CompFormatLeague, Kind: "team", TeamSize: 2, LeagueTiebreakFinalized: true}
	open := &state.Competition{Format: state.CompFormatLeague, Kind: "team", TeamSize: 2}
	four := func(p0, p1, p2, p3 int) []state.PlayerStanding {
		return []state.PlayerStanding{
			rankedStanding("A", p0, 1), rankedStanding("B", p1, 2),
			rankedStanding("C", p2, 3), rankedStanding("D", p3, 4),
		}
	}

	t.Run("an accepted tie at 1st reads 1, 1, 3", func(t *testing.T) {
		sorted := stamped(four(100, 100, 50, 40), 0, 1, true, false)
		applyJointThirdRanks(accepted, sorted, nil)
		assert.Equal(t, []int{1, 1, 3, 4}, ranksOf(sorted))
		assert.Equal(t, "A", sorted[0].Player.Name, "the rows keep their order: the winner line reads the first row as before")
	})

	t.Run("an accepted three-way tie at 1st reads 1, 1, 1, 4", func(t *testing.T) {
		sorted := stamped(four(100, 100, 100, 40), 0, 2, true, false)
		applyJointThirdRanks(accepted, sorted, nil)
		assert.Equal(t, []int{1, 1, 1, 4}, ranksOf(sorted))
	})

	t.Run("an accepted tie at 2nd reads 1, 2, 2", func(t *testing.T) {
		sorted := stamped(four(100, 80, 80, 40), 1, 2, true, false)
		applyJointThirdRanks(accepted, sorted, nil)
		assert.Equal(t, []int{1, 2, 2, 4}, ranksOf(sorted))
	})

	t.Run("a tie still waiting for the operator keeps sequential ranks", func(t *testing.T) {
		sorted := stamped(four(100, 100, 50, 40), 0, 1, true, false)
		applyJointThirdRanks(open, sorted, nil)
		assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(sorted))
	})

	t.Run("accepted ranks do not share a group the tie-breaker settled", func(t *testing.T) {
		// A group a fought tie-breaker or lots ordered is not Tied: it keeps
		// the order it earned.
		sorted := four(100, 100, 50, 40)
		applyJointThirdRanks(accepted, sorted, nil)
		assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(sorted))
	})

	t.Run("a tie below the band reads 4, 4 with joint 3rd off", func(t *testing.T) {
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 90, 2), rankedStanding("C", 80, 3),
			rankedStanding("D", 50, 4), rankedStanding("E", 50, 5),
		}
		stamped(sorted, 3, 4, false, true)
		applyJointThirdRanks(open, sorted, nil)
		assert.Equal(t, []int{1, 2, 3, 4, 4}, ranksOf(sorted))
	})

	t.Run("an individual league's tie below the band shares too", func(t *testing.T) {
		individual := &state.Competition{Format: state.CompFormatLeague}
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 90, 2), rankedStanding("C", 80, 3),
			rankedStanding("D", 50, 4), rankedStanding("E", 50, 5), rankedStanding("F", 50, 6),
		}
		stamped(sorted, 3, 5, false, true)
		applyJointThirdRanks(individual, sorted, nil)
		assert.Equal(t, []int{1, 2, 3, 4, 4, 4}, ranksOf(sorted))
	})

	t.Run("a tie below the band holding lots keeps the operator's order", func(t *testing.T) {
		sorted := []state.PlayerStanding{
			rankedStanding("A", 100, 1), rankedStanding("B", 90, 2), rankedStanding("C", 80, 3),
			rankedStanding("D", 50, 4), rankedStanding("E", 50, 5),
		}
		stamped(sorted, 3, 4, false, true)
		sorted[3].IsOverridden, sorted[4].IsOverridden = true, true
		applyJointThirdRanks(open, sorted, nil)
		assert.Equal(t, []int{1, 2, 3, 4, 5}, ranksOf(sorted))
	})

	t.Run("a mixed competition's pool keeps sequential ranks", func(t *testing.T) {
		mixed := &state.Competition{Format: state.CompFormatMixed, LeagueTiebreakFinalized: true}
		sorted := stamped(four(100, 100, 50, 50), 0, 1, true, false)
		stamped(sorted, 2, 3, false, true)
		applyJointThirdRanks(mixed, sorted, nil)
		assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(sorted),
			"mixed pools feed knockout seeding and must keep sequential ranks")
	})
}

// An accepted shared place is shared only among the members the fought
// tie-breaker bouts left level: a team that beat the others keeps its place.
func TestApplyJointThirdRanks_AcceptedGroupSharesOnlyWithinLevelRuns(t *testing.T) {
	accepted := &state.Competition{Format: state.CompFormatLeague, Kind: "team", TeamSize: 2, LeagueTiebreakFinalized: true}
	four := func() []state.PlayerStanding {
		sorted := make([]state.PlayerStanding, 0, 4)
		for i, id := range []string{"a", "b", "c", "d"} {
			pts := 100
			if id == "d" {
				pts = 40
			}
			s := rankedStanding(id, pts, i+1)
			s.Player.ID = id
			sorted = append(sorted, s)
		}
		return stamped(sorted, 0, 2, true, false)
	}
	n := 0
	won := func(winner, loser string) state.MatchResult {
		n++
		return state.MatchResult{ID: fmt.Sprintf("Pool A-DH-%d", n), SideAID: winner, SideBID: loser,
			Status: state.MatchStatusCompleted, Winner: winner, WinnerID: winner}
	}

	t.Run("a team that beat both others keeps 1st, the two it beat share 2nd", func(t *testing.T) {
		sorted := four()
		applyJointThirdRanks(accepted, sorted, []state.MatchResult{won("a", "b"), won("a", "c")})
		assert.Equal(t, []int{1, 2, 2, 4}, ranksOf(sorted))
	})

	t.Run("a circle fought to the end leaves all three level", func(t *testing.T) {
		sorted := four()
		applyJointThirdRanks(accepted, sorted, []state.MatchResult{won("a", "b"), won("b", "c"), won("c", "a")})
		assert.Equal(t, []int{1, 1, 1, 4}, ranksOf(sorted))
	})

	t.Run("a bout nobody fought separates nobody", func(t *testing.T) {
		sorted := four()
		applyJointThirdRanks(accepted, sorted, nil)
		assert.Equal(t, []int{1, 1, 1, 4}, ranksOf(sorted))
	})

	t.Run("a team that beat nobody reads below the two level ahead of it", func(t *testing.T) {
		sorted := four()
		applyJointThirdRanks(accepted, sorted, []state.MatchResult{won("a", "b"), won("b", "c")})
		assert.Equal(t, []int{1, 1, 3, 4}, ranksOf(sorted))
	})
}

// TestCalculatePoolStandings_AcceptedTieSharesItsPlace drives the team league
// through the standings: the tie at 1st reads 1, 2 while it waits for the
// operator and 1, 1 once the shared ranks are accepted, with the rows in the
// same order.
func TestCalculatePoolStandings_AcceptedTieSharesItsPlace(t *testing.T) {
	const compID = "shared-ranks-top"
	eng, store := setupTeamLeagueComp(t, compID, "topTie")

	before, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	require.Len(t, before["Pool A"], 4)
	assert.Equal(t, []int{1, 2, 3, 4}, ranksOf(before["Pool A"]), "a tie still asked keeps sequential ranks")
	assert.True(t, before["Pool A"][0].Tied && before["Pool A"][1].Tied, "the tie at 1st is still to be broken")
	firstBefore := before["Pool A"][0].Player.ID

	comp := loadComp(t, store, compID)
	comp.LeagueTiebreakFinalized = true
	comp.Status = state.CompStatusComplete
	require.NoError(t, store.SaveCompetition(comp))

	after, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 1, 3, 4}, ranksOf(after["Pool A"]), "the accepted tie reads its best place")
	assert.True(t, after["Pool A"][0].Tied && after["Pool A"][1].Tied, "accepting does not settle the tie")
	assert.Equal(t, firstBefore, after["Pool A"][0].Player.ID, "the first row, which the winner line reads, does not move")
}

// TestCalculatePoolStandings_TieBelowBandSharesItsPlace drives an individual
// league of five through the standings: the 4th/5th tie lies outside the band
// a tie is broken in, so nothing is asked and both read 4.
func TestCalculatePoolStandings_TieBelowBandSharesItsPlace(t *testing.T) {
	const compID = "shared-ranks-below"
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Ind League", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	players := []helper.Player{
		{Name: "A", Dojo: "Dojo A"}, {Name: "B", Dojo: "Dojo B"}, {Name: "C", Dojo: "Dojo C"},
		{Name: "D", Dojo: "Dojo D"}, {Name: "E", Dojo: "Dojo E"},
	}
	winBy := func(id, a, b string) state.MatchResult {
		return state.MatchResult{ID: id, SideA: a, SideB: b, Winner: a,
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted, Court: "A"}
	}
	matches := []state.MatchResult{
		winBy("Pool A-0", "A", "B"), winBy("Pool A-1", "A", "C"), winBy("Pool A-2", "A", "D"), winBy("Pool A-3", "A", "E"),
		winBy("Pool A-4", "B", "C"), winBy("Pool A-5", "B", "D"), winBy("Pool A-6", "B", "E"),
		winBy("Pool A-7", "C", "D"), winBy("Pool A-8", "C", "E"),
		{ID: "Pool A-9", SideA: "D", SideB: "E", Decision: string(domain.DecisionHikiwake),
			Status: state.MatchStatusCompleted, Court: "A"},
	}
	bctest.StampIDs(players, matches)
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, matches))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	ranks := ranksByName(standings["Pool A"])
	assert.Equal(t, map[string]int{"A": 1, "B": 2, "C": 3, "D": 4, "E": 4}, ranks)

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "nothing is asked for a tie below the band")
}
