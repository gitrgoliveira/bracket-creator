package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// These tests pin the band-aware rule for the pool daihyosen: a supplementary
// bout is played only where the tie affects who advances (or their seed). The
// top `poolWinners` of each pool advance and 1st place gets a bracket bye, so a
// tie whose best position is within [1..poolWinners] is consequential, while a
// tie sitting entirely below the cut shares its rank with no bout
// (running_a_kendo_tournament.md:441 - a daihyosen is played "to determine
// their ranking", i.e. where the ranking matters).

// setupTeamPoolWinners is setupTeamPool with an explicit advancement cutoff so a
// tie can be placed above, across, or below the qualifying band.
func setupTeamPoolWinners(t *testing.T, compID string, teams []string, poolWinners int, matches []state.MatchResult) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Band-aware DH Test",
		Format: state.CompFormatLeague, Status: state.CompStatusPools,
		Courts: []string{"A"}, Kind: "team", TeamSize: 2,
		PoolWinners: poolWinners,
	}))
	players := make([]helper.Player, len(teams))
	for i, n := range teams {
		players[i] = helper.Player{Name: n}
	}
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, matches))
	return eng, store
}

// fourTeamOneTiedPair builds a 4-team round-robin (6 matches) where Alpha and
// Beta finish distinct at the top and Gamma & Delta tie for 3rd/4th (each loses
// to Alpha & Beta and draws the other).
func fourTeamOneTiedPair() []state.MatchResult {
	return []state.MatchResult{
		teamPoolMatch("Pool A-0", "A", "Alpha", "Beta", "Alpha"),
		teamPoolMatch("Pool A-1", "A", "Alpha", "Gamma", "Alpha"),
		teamPoolMatch("Pool A-2", "A", "Alpha", "Delta", "Alpha"),
		teamPoolMatch("Pool A-3", "A", "Beta", "Gamma", "Beta"),
		teamPoolMatch("Pool A-4", "A", "Beta", "Delta", "Beta"),
		teamPoolMatch("Pool A-5", "A", "Gamma", "Delta", ""), // 3rd/4th tie
	}
}

// TestInjectPoolDaihyosen_BelowCutIsNonConsequential: a tie entirely below the
// advancement cut (Gamma/Delta at 3rd/4th with top-2 advancing) injects NO
// daihyosen; the two teams simply share the rank.
func TestInjectPoolDaihyosen_BelowCutIsNonConsequential(t *testing.T) {
	compID := "dh-below-cut"
	eng, _ := setupTeamPoolWinners(t, compID, []string{"Alpha", "Beta", "Gamma", "Delta"}, 2, fourTeamOneTiedPair())

	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	assert.Empty(t, injected, "a tie below the top-2 cut must not trigger a daihyosen")
}

// TestInjectPoolDaihyosen_SameTieIsConsequentialWhenAllAdvance: the identical
// Gamma/Delta tie IS consequential when the cut is 4 (everyone advances, so the
// 3rd vs 4th bracket seed must be decided) - proving the difference is the band,
// not the standings.
func TestInjectPoolDaihyosen_SameTieIsConsequentialWhenAllAdvance(t *testing.T) {
	compID := "dh-all-advance"
	eng, _ := setupTeamPoolWinners(t, compID, []string{"Alpha", "Beta", "Gamma", "Delta"}, 4, fourTeamOneTiedPair())

	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 1, "with all teams advancing, the 3rd/4th seed tie needs one daihyosen")
	assert.True(t, IsPoolDaihyosenMatchID(injected[0].ID))
}

// TestInjectPoolDaihyosen_StraddlingCutInjectsOneBout: a two-way tie for 2nd/3rd
// (top-2 advance) straddles the cut - it decides who advances - so exactly one
// pairwise daihyosen is injected.
func TestInjectPoolDaihyosen_StraddlingCutInjectsOneBout(t *testing.T) {
	compID := "dh-straddle"
	// Alpha 1st (beats all); Beta & Gamma tie for 2nd/3rd (each beats Delta,
	// loses to Alpha, draws the other); Delta 4th (loses all).
	matches := []state.MatchResult{
		teamPoolMatch("Pool A-0", "A", "Alpha", "Beta", "Alpha"),
		teamPoolMatch("Pool A-1", "A", "Alpha", "Gamma", "Alpha"),
		teamPoolMatch("Pool A-2", "A", "Alpha", "Delta", "Alpha"),
		teamPoolMatch("Pool A-3", "A", "Beta", "Gamma", ""), // 2nd/3rd tie
		teamPoolMatch("Pool A-4", "A", "Beta", "Delta", "Beta"),
		teamPoolMatch("Pool A-5", "A", "Gamma", "Delta", "Gamma"),
	}
	eng, _ := setupTeamPoolWinners(t, compID, []string{"Alpha", "Beta", "Gamma", "Delta"}, 2, matches)

	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 1, "a 2nd/3rd tie straddling the cut needs exactly one daihyosen")
	got := map[string]bool{injected[0].SideA: true, injected[0].SideB: true}
	assert.True(t, got["Beta"] && got["Gamma"], "the daihyosen must be between the straddling teams (Beta vs Gamma)")
}
