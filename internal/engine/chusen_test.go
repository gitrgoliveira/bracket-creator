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
