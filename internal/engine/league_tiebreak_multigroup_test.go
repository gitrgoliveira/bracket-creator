package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// teamIDsByName resolves competitor names to their participant ids by reading
// them back off the saved pools, rather than assuming a particular id scheme:
// GenerateLeagueTiebreakMatches now selects the tied group by id only
// (operator ruling bc-pnum; teamNames is accepted but never used to select),
// so every caller here must hand it real ids.
func teamIDsByName(t *testing.T, store *state.Store, compID string, names []string) []string {
	t.Helper()
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	byName := make(map[string]string)
	for _, p := range pools {
		for _, pl := range p.Players {
			byName[pl.Name] = pl.ID
		}
	}
	ids := make([]string, len(names))
	for i, n := range names {
		id, ok := byName[n]
		require.True(t, ok, "no player named %q in saved pools for %s", n, compID)
		require.NotEmpty(t, id, "player %q has no id in saved pools for %s", n, compID)
		ids[i] = id
	}
	return ids
}

// setupTwoTiedGroupLeague builds a 4-team team-league with TWO separate
// consequential tied groups: {Alpha,Beta} tied for 1st–2nd and {Gamma,Delta}
// tied for 3rd–4th. Alpha/Beta each beat both of Gamma/Delta and draw each
// other; Gamma/Delta lose to both of Alpha/Beta and draw each other. The two
// groups have different Points (top vs bottom), so detectPoolTies returns two
// distinct groups.
func setupTwoTiedGroupLeague(t *testing.T, compID string) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Two-Tie League",
		Format:   state.CompFormatLeague,
		Status:   state.CompStatusPools,
		Courts:   []string{"A"},
		TeamSize: 2,
	}))
	players := []helper.Player{
		{Name: "Alpha", Dojo: "Dojo Alpha"}, {Name: "Beta", Dojo: "Dojo Beta"}, {Name: "Gamma", Dojo: "Dojo Gamma"}, {Name: "Delta", Dojo: "Dojo Delta"},
	}

	draw := string(domain.DecisionHikiwake)
	win := func(id, a, b, winner string) state.MatchResult {
		m := state.MatchResult{ID: id, SideA: a, SideB: b, Status: state.MatchStatusCompleted, Court: "A"}
		if winner == "" {
			m.Decision = draw
			m.SubResults = []state.SubMatchResult{
				{Position: 1, SideA: a, SideB: b, Decision: draw},
				{Position: 2, SideA: a, SideB: b, Decision: draw},
			}
		} else {
			m.Winner = winner
			m.SubResults = []state.SubMatchResult{
				{Position: 1, SideA: a, SideB: b, Winner: winner},
				{Position: 2, SideA: a, SideB: b, Winner: winner},
			}
		}
		return m
	}
	matches := []state.MatchResult{
		win("Pool A-0", "Alpha", "Beta", ""),       // top group draw
		win("Pool A-1", "Alpha", "Gamma", "Alpha"), // top beats bottom
		win("Pool A-2", "Alpha", "Delta", "Alpha"),
		win("Pool A-3", "Beta", "Gamma", "Beta"),
		win("Pool A-4", "Beta", "Delta", "Beta"),
		win("Pool A-5", "Gamma", "Delta", ""), // bottom group draw
	}
	bctest.StampIDs(players, matches)
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, matches))
	return eng, store
}

// scoreGroupDH generates a tie-breaker for the named group and marks every
// resulting DH match completed with the given winner, mirroring the operator
// running + scoring a tie-breaker.
func scoreGroupDH(t *testing.T, eng *Engine, store *state.Store, compID string, teams []string, winner string) {
	t.Helper()
	// Selection is id-only now (operator ruling bc-pnum): teams (names) is
	// accepted but never used to select, so resolve real ids off the saved
	// pools first.
	teamIDs := teamIDsByName(t, store, compID, teams)
	injected, err := eng.GenerateLeagueTiebreakMatches(compID, teams, teamIDs)
	require.NoError(t, err)
	require.NotEmpty(t, injected, "expected DH matches to be generated for %v", teams)

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range all {
		if IsPoolDaihyosenMatchID(all[i].ID) && all[i].Winner == "" {
			set := map[string]bool{all[i].SideA: true, all[i].SideB: true}
			if set[teams[0]] && set[teams[1]] {
				all[i].Status = state.MatchStatusCompleted
				all[i].Winner = winner
				// WinnerID too: resolveWinnerSide resolves the winner by id
				// only (operator ruling bc-pnum), so a name-only Winner would
				// read as an unresolved DH.
				switch winner {
				case all[i].SideA:
					all[i].WinnerID = all[i].SideAID
				case all[i].SideB:
					all[i].WinnerID = all[i].SideBID
				}
			}
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))
}

// TestMaybeAutoCompletePools_MultipleConsequentialGroups is the regression test
// for the tri-review critical finding: with two separate consequential tied
// groups, resolving ONE group's tie-breaker must NOT let the competition complete
// while the OTHER group is still unresolved. Before the per-group gate fix, the
// coarse `!hasCompleteDH` guard let the first completed DH flip the competition
// to complete with the second tie unresolved.
func TestMaybeAutoCompletePools_MultipleConsequentialGroups(t *testing.T) {
	compID := "two-tie-league"
	eng, store := setupTwoTiedGroupLeague(t, compID)

	// Both groups consequential (top-3 default band: 1–2 and 3–4).
	cands, err := eng.LeagueTiebreakCandidates(compID)
	require.NoError(t, err)
	require.Len(t, cands, 2, "expected two consequential tied groups")

	// Nothing actioned yet → block.
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteAwaitingLeagueTiebreak, outcome)

	// Resolve ONLY the top group.
	scoreGroupDH(t, eng, store, compID, []string{"Alpha", "Beta"}, "Alpha")

	// Must STILL block, the 3rd–4th tie is unresolved. (Pre-fix: this returned
	// AutoCompleteTransitioned, completing with an unresolved consequential tie.)
	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteAwaitingLeagueTiebreak, outcome,
		"competition must not complete while a second consequential tie is unresolved")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "must not have transitioned")

	// Resolve the bottom group too.
	scoreGroupDH(t, eng, store, compID, []string{"Gamma", "Delta"}, "Gamma")

	// Both resolved → completes.
	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome)

	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status)
}

// TestMaybeAutoCompletePools_SingleGroupNoWedge guards the failure mode of the
// naive fix (always calling LeagueTiebreakCandidates without the per-group DH
// check): a single tied group, once its tie-breaker is scored, must complete and
// not wedge in AwaitingLeagueTiebreak (DH results don't break the Points tie, so
// the group keeps appearing in LeagueTiebreakCandidates).
func TestMaybeAutoCompletePools_SingleGroupNoWedge(t *testing.T) {
	compID := "single-tie-league"
	eng, store := setupTeamPoolComp(t, compID, true) // 3-way all-draw tie

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteAwaitingLeagueTiebreak, outcome)

	// Operator runs the full 3-way round-robin tie-breaker with a clear order
	// (Alpha > Beta > Gamma, Alpha > Gamma): no cycle. Selection is id-only
	// (operator ruling bc-pnum), so resolve real ids off the saved pools.
	teamIDs := teamIDsByName(t, store, compID, []string{"Alpha", "Beta", "Gamma"})
	injected, err := eng.GenerateLeagueTiebreakMatches(compID, []string{"Alpha", "Beta", "Gamma"}, teamIDs)
	require.NoError(t, err)
	require.Len(t, injected, 3)
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	winners := map[string]string{}     // pairKey -> winner
	pick := func(a, b string) string { // deterministic: Alpha>Beta>Gamma
		order := map[string]int{"Alpha": 3, "Beta": 2, "Gamma": 1}
		if order[a] > order[b] {
			return a
		}
		return b
	}
	for i := range all {
		if IsPoolDaihyosenMatchID(all[i].ID) {
			w := pick(all[i].SideA, all[i].SideB)
			all[i].Status = state.MatchStatusCompleted
			all[i].Winner = w
			// WinnerID too: resolveWinnerSide resolves the winner by id only
			// (operator ruling bc-pnum).
			switch w {
			case all[i].SideA:
				all[i].WinnerID = all[i].SideAID
			case all[i].SideB:
				all[i].WinnerID = all[i].SideBID
			}
			winners[all[i].ID] = w
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, all))

	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "resolved single group must complete, not wedge")
}

// TestMaybeAutoCompletePools_TwoGroups_StandingsOrderReflectsDHWinners is the
// league-lifecycle counterpart to the completion-gating tests above: it pins the
// FINAL RANK ORDER after both groups' tie-breakers are scored through the real
// operator path (GenerateLeagueTiebreakMatches). The existing league tests only
// assert the AutoComplete* outcome; none verify that the DH winner actually ends
// up ranked above the loser, which is the "who advances and in which position"
// property. It also guards cross-group isolation: the bottom group's DH winner
// must not be lifted above the top group (both DH results are excluded from the
// Points totals, so ordering comes solely from the per-group DH secondary sort).
func TestMaybeAutoCompletePools_TwoGroups_StandingsOrderReflectsDHWinners(t *testing.T) {
	compID := "two-tie-order"
	eng, store := setupTwoTiedGroupLeague(t, compID)

	// Within each tied group, the DH winner is the team that finished the raw
	// standings lower (Beta below Alpha, Delta below Gamma), so a correct
	// secondary sort must visibly reorder them.
	scoreGroupDH(t, eng, store, compID, []string{"Alpha", "Beta"}, "Beta")
	scoreGroupDH(t, eng, store, compID, []string{"Gamma", "Delta"}, "Gamma")

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTransitioned, outcome, "both groups resolved must complete")

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	assert.Equal(t, []string{"Beta", "Alpha", "Gamma", "Delta"}, poolOrder(standings["Pool A"]),
		"DH winners must advance within their own group; bottom-group win must not cross into the top group")
}
