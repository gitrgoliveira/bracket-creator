package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestChusenCandidates_CycleNeedsChusen: three teams tied on every criterion play
// a daihyosen round-robin that ends in a perfect cycle (Alpha>Beta, Beta>Gamma,
// Gamma>Alpha, one win each). The order is undetermined, so the group surfaces as
// a chusen (drawing-lots) candidate for the operator to resolve.
func TestChusenCandidates_CycleNeedsChusen(t *testing.T) {
	compID := "chusen-cycle"
	eng, store := setupTeamPoolComp(t, compID, true) // 3 teams fully tied
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)

	scoreInjectedDH(t, eng, store, compID, func(sideA, sideB string) string {
		switch {
		case sideA == "Alpha" && sideB == "Beta":
			return "Alpha"
		case sideA == "Alpha" && sideB == "Gamma":
			return "Gamma" // Gamma > Alpha
		case sideA == "Beta" && sideB == "Gamma":
			return "Beta"
		}
		return sideA
	})

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
	require.Len(t, cands, 1, "an unresolved 3-way daihyosen cycle needs a chusen")
	assert.Equal(t, "Pool A", cands[0].PoolName)
	assert.Len(t, cands[0].Teams, 3)
	assert.Equal(t, 1, cands[0].MinPosition)
}

// TestChusenCandidates_StrictOrderNeedsNoChusen: the same tied group, but the
// daihyosen produces a strict 2/1/0 win order (Alpha beats all, Beta beats
// Gamma), so no chusen is needed.
func TestChusenCandidates_StrictOrderNeedsNoChusen(t *testing.T) {
	compID := "chusen-resolved"
	eng, store := setupTeamPoolComp(t, compID, true)
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)

	scoreInjectedDH(t, eng, store, compID, func(sideA, sideB string) string {
		if sideA == "Alpha" || sideB == "Alpha" {
			return "Alpha"
		}
		return "Beta"
	})

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
	assert.Empty(t, cands, "a strictly-ordered daihyosen needs no chusen")
}

// TestChusenCandidates_ResolvedByOverride: once the operator records the drawn
// order (a per-pool rank override for every tied member), the cycle group no
// longer needs a chusen.
func TestChusenCandidates_ResolvedByOverride(t *testing.T) {
	compID := "chusen-override"
	eng, store := setupTeamPoolComp(t, compID, true)
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	scoreInjectedDH(t, eng, store, compID, func(sideA, sideB string) string {
		switch {
		case sideA == "Alpha" && sideB == "Beta":
			return "Alpha"
		case sideA == "Alpha" && sideB == "Gamma":
			return "Gamma"
		case sideA == "Beta" && sideB == "Gamma":
			return "Beta"
		}
		return sideA
	})
	require.NoError(t, store.SaveOverrides(compID, &state.Overrides{
		PoolRanks: map[string]map[string]int{
			"Pool A": {"Alpha": 1, "Beta": 2, "Gamma": 3},
		},
	}))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
	assert.Empty(t, cands, "a chusen recorded as a full rank override clears the candidate")
}

// TestChusenCandidates_NonTeamHasNone: individual (non-team) competitions never
// surface chusen candidates (chusen here resolves team-pool daihyosen cycles).
func TestChusenCandidates_NonTeamHasNone(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "chusen-indiv", Name: "Individual", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"}, TeamSize: 0,
	}))
	cands, err := eng.ChusenCandidates("chusen-indiv")
	require.NoError(t, err)
	assert.Empty(t, cands)
}

// TestChusenCandidates_PartialRoundNotPremature: with only ONE of a 3-team
// group's three daihyosen bouts scored, the partial win counts (1/0/0) contain a
// spurious duplicate (the two zeros). Chusen must NOT surface until the whole
// pairwise round is complete.
func TestChusenCandidates_PartialRoundNotPremature(t *testing.T) {
	compID := "chusen-partial"
	eng, store := setupTeamPoolComp(t, compID, true) // 3 teams fully tied
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)

	// Complete exactly one of the three injected daihyosen bouts.
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	completedOne := false
	for i := range all {
		if IsPoolDaihyosenMatchID(all[i].ID) && !completedOne {
			all[i].Status = state.MatchStatusCompleted
			all[i].Winner = all[i].SideA
			completedOne = true
		}
	}
	require.True(t, completedOne, "expected at least one injected DH bout")
	require.NoError(t, store.SavePoolMatches(compID, all))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
	assert.Empty(t, cands, "chusen must not surface mid-round (only 1 of 3 DH bouts scored)")
}

// TestChusenCandidates_AllDrawnNeedsChusen: a full daihyosen round completed as
// all hikiwake (every bout Winner="") leaves every team on 0 wins, so the order
// is undetermined and must surface as needing chusen (otherwise the competition
// can never advance).
func TestChusenCandidates_AllDrawnNeedsChusen(t *testing.T) {
	compID := "chusen-alldrawn"
	eng, store := setupTeamPoolComp(t, compID, true) // 3 teams fully tied
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	scored := 0
	for i := range all {
		if IsPoolDaihyosenMatchID(all[i].ID) {
			all[i].Status = state.MatchStatusCompleted
			all[i].Winner = "" // hikiwake
			scored++
		}
	}
	require.Equal(t, 3, scored, "expected the 3-team round-robin of DH bouts")
	require.NoError(t, store.SavePoolMatches(compID, all))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
	require.Len(t, cands, 1, "an all-drawn daihyosen round leaves the order undetermined -> chusen")
}

// TestChusenCandidates_PartialTieWithoutCycleNeedsChusen: a 4-team tied group
// whose daihyosen round has NO win/loss cycle anywhere (Alpha beats everyone
// and loses to nobody), yet two teams still finish level on daihyosen wins
// (Beta and Gamma draw each other and each beat Delta once: 1 win apiece).
// groupNeedsChusen fires on any duplicate win count, not only a true cycle -
// this pins that a plain partial tie also surfaces the chusen panel.
func TestChusenCandidates_PartialTieWithoutCycleNeedsChusen(t *testing.T) {
	compID := "chusen-partial-tie"
	teams := []string{"Alpha", "Beta", "Gamma", "Delta"}
	// Fully tied 4-team pool (every match drawn) so all four share one tied
	// group in standings.
	matches := []state.MatchResult{
		teamPoolMatch("Pool A-0", "A", "Alpha", "Beta", ""),
		teamPoolMatch("Pool A-1", "A", "Alpha", "Gamma", ""),
		teamPoolMatch("Pool A-2", "A", "Alpha", "Delta", ""),
		teamPoolMatch("Pool A-3", "A", "Beta", "Gamma", ""),
		teamPoolMatch("Pool A-4", "A", "Beta", "Delta", ""),
		teamPoolMatch("Pool A-5", "A", "Gamma", "Delta", ""),
	}
	eng, store := setupTeamPool(t, compID, teams, matches)

	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 6, "expected the full 4-team DH round-robin")

	winner := func(a, b string) string {
		pair := map[string]bool{a: true, b: true}
		switch {
		case pair["Alpha"]:
			return "Alpha" // beats every opponent, loses to nobody: no cycle involves Alpha
		case pair["Beta"] && pair["Gamma"]:
			return "" // draw: the source of the Beta/Gamma win-count tie
		case pair["Beta"] && pair["Delta"]:
			return "Beta"
		case pair["Gamma"] && pair["Delta"]:
			return "Gamma"
		}
		t.Fatalf("unexpected DH pair %s vs %s", a, b)
		return ""
	}
	scoreInjectedDH(t, eng, store, compID, winner)
	// Final daihyosen wins: Alpha=3, Beta=1, Gamma=1, Delta=0 - a duplicate at
	// 1 with no cyclic relationship anywhere in the results.

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
	require.Len(t, cands, 1, "Beta and Gamma tie on daihyosen wins with no cycle present -> still needs chusen")
	assert.Len(t, cands[0].Teams, 4)
}
