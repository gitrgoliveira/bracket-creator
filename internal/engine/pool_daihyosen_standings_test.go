package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// These tests pin the behaviour the closing-ceremony / advancement rules
// depend on: when team pools finish tied on all 8 ranking criteria, a
// pool-stage daihyosen (rep bout) must decide BOTH who advances and in which
// position. The existing TestDHStandingsApplied only asserts the winner lands
// at position 0; these add the 2-way advancement case, the full 3-way order,
// and cross-group scoping (a DH result in one tied group must not reorder a
// different tied group in the same pool).

// teamPoolMatch builds one completed team pool match with two sub-bouts.
// When winner is "" the match is a draw (both sub-bouts hikiwake); otherwise
// the winner takes both sub-bouts (IV=2 for that side).
func teamPoolMatch(id, court, sideA, sideB, winner string) state.MatchResult {
	m := state.MatchResult{
		ID: id, SideA: sideA, SideB: sideB, Court: court,
		Status: state.MatchStatusCompleted, Winner: winner,
	}
	if winner == "" {
		m.Decision = string(domain.DecisionHikiwake)
		m.SubResults = []state.SubMatchResult{
			{Position: 1, SideA: sideA, SideB: sideB, Winner: "", Decision: string(domain.DecisionHikiwake)},
			{Position: 2, SideA: sideA, SideB: sideB, Winner: "", Decision: string(domain.DecisionHikiwake)},
		}
	} else {
		m.SubResults = []state.SubMatchResult{
			{Position: 1, SideA: sideA, SideB: sideB, Winner: winner},
			{Position: 2, SideA: sideA, SideB: sideB, Winner: winner},
		}
	}
	return m
}

// scoreInjectedDH marks every injected pool-DH match Completed and sets its
// Winner via pick(sideA, sideB). It then invalidates the standings cache so
// the next CalculatePoolStandings recomputes with the DH results applied.
func scoreInjectedDH(t *testing.T, eng *Engine, store *state.Store, compID string, pick func(sideA, sideB string) string) {
	t.Helper()
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	scored := 0
	for i := range all {
		if !IsPoolDaihyosenMatchID(all[i].ID) {
			continue
		}
		all[i].Status = state.MatchStatusCompleted
		all[i].Winner = pick(all[i].SideA, all[i].SideB)
		scored++
	}
	require.Positive(t, scored, "expected at least one injected DH match to score")
	require.NoError(t, store.SavePoolMatches(compID, all))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)
}

func poolOrder(standings []state.PlayerStanding) []string {
	names := make([]string, len(standings))
	for i, s := range standings {
		names[i] = s.Player.Name
	}
	return names
}

// setupTeamPool creates a single-pool team-league competition with the given
// teams and pre-scored regular matches, then returns the engine + store.
func setupTeamPool(t *testing.T, compID string, teams []string, matches []state.MatchResult) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Team Pool Advancement Test",
		Format: state.CompFormatLeague, Status: state.CompStatusPools,
		Courts: []string{"A"}, Kind: "team", TeamSize: 2,
	}))
	players := make([]helper.Player, len(teams))
	for i, n := range teams {
		players[i] = helper.Player{Name: n}
	}
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, matches))
	return eng, store
}

// TestDHStandingsApplied_TwoWayDecidesAdvancement is the canonical
// "who goes through" case: two teams drawn in pool play (tied on every
// criterion). The pool-stage daihyosen winner must occupy the advancing
// position, and the loser must drop below.
func TestDHStandingsApplied_TwoWayDecidesAdvancement(t *testing.T) {
	compID := "dh-2way"
	// Single match, drawn → TeamA and TeamB tied on all 8 criteria.
	eng, store := setupTeamPool(t, compID, []string{"TeamA", "TeamB"}, []state.MatchResult{
		teamPoolMatch("Pool A-0", "A", "TeamA", "TeamB", ""),
	})

	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 1, "one DH bout for a 2-team tie")

	// TeamB wins the daihyosen.
	scoreInjectedDH(t, eng, store, compID, func(_, _ string) string { return "TeamB" })

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	assert.Equal(t, []string{"TeamB", "TeamA"}, poolOrder(standings["Pool A"]),
		"DH winner TeamB must advance ahead of TeamA")
}

// TestDHStandingsApplied_ThreeWayFullOrder strengthens the existing
// TestDHStandingsApplied (which only checks position 0) by asserting the
// complete ordering produced by a 3-way DH round-robin.
func TestDHStandingsApplied_ThreeWayFullOrder(t *testing.T) {
	// Reuse the fully-tied 3-team fixture (Alpha == Beta == Gamma).
	eng, store := setupTeamPoolComp(t, "dh-3way-order", true)

	_, err := eng.InjectPoolDaihyosenMatches("dh-3way-order")
	require.NoError(t, err)

	// Alpha beats everyone; Beta beats Gamma → Alpha 2 wins, Beta 1, Gamma 0.
	scoreInjectedDH(t, eng, store, "dh-3way-order", func(sideA, sideB string) string {
		if sideA == "Alpha" || sideB == "Alpha" {
			return "Alpha"
		}
		return "Beta" // the remaining Beta vs Gamma bout
	})

	standings, err := eng.CalculatePoolStandings("dh-3way-order")
	require.NoError(t, err)
	assert.Equal(t, []string{"Alpha", "Beta", "Gamma"}, poolOrder(standings["Pool A"]),
		"DH win counts must order the whole tied group, not just the top")
}

// TestDHStandingsApplied_MultiGroupIsolation guards the per-group scoping of
// the DH secondary sort: two independent tied pairs in ONE pool, where each
// pair plays its own DH. A win in the lower group must not lift that team into
// the higher group, and win counts must not bleed across groups.
func TestDHStandingsApplied_MultiGroupIsolation(t *testing.T) {
	compID := "dh-multigroup"
	// 4-team round-robin (6 matches):
	//   Alpha & Beta each beat Gamma & Delta, and draw each other → top tier, Alpha==Beta.
	//   Gamma & Delta each lose to Alpha & Beta, and draw each other → bottom tier, Gamma==Delta.
	eng, store := setupTeamPool(t, compID,
		[]string{"Alpha", "Beta", "Gamma", "Delta"},
		[]state.MatchResult{
			teamPoolMatch("Pool A-0", "A", "Alpha", "Beta", ""), // top pair draw
			teamPoolMatch("Pool A-1", "A", "Alpha", "Gamma", "Alpha"),
			teamPoolMatch("Pool A-2", "A", "Alpha", "Delta", "Alpha"),
			teamPoolMatch("Pool A-3", "A", "Beta", "Gamma", "Beta"),
			teamPoolMatch("Pool A-4", "A", "Beta", "Delta", "Beta"),
			teamPoolMatch("Pool A-5", "A", "Gamma", "Delta", ""), // bottom pair draw
		})

	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 2, "one DH bout per tied pair (Alpha/Beta and Gamma/Delta)")

	// Beta wins the top-pair DH; Gamma wins the bottom-pair DH.
	scoreInjectedDH(t, eng, store, compID, func(sideA, sideB string) string {
		if sideA == "Gamma" || sideB == "Gamma" {
			return "Gamma" // Gamma vs Delta
		}
		return "Beta" // Alpha vs Beta
	})

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	// Gamma winning the LOWER-group DH must not push it above Alpha/Beta.
	assert.Equal(t, []string{"Beta", "Alpha", "Gamma", "Delta"}, poolOrder(standings["Pool A"]),
		"DH results must stay scoped to their own tied group")
}
