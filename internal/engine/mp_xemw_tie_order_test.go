package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoolStandings_TiedPlayersDeterministicOrder is the mp-xemw regression
// guard. computeStandingsFrom builds a map[string]*PlayerStanding and ranges it
// into a slice, then sorts by a single packed Points score. Two players who tie
// on every ranking criterion (equal Points, no supplementary bout) compare equal
// in that sort, so their relative order used to be left to Go's randomized map
// iteration -- different on every call. For a live tournament that means two tied
// qualifiers seed into the knockout in a run-dependent order. The fix adds a
// total-order (ID, then Name) tiebreaker.
//
// The pool players are inserted [Zeta, Alpha] but the deterministic order is
// [Alpha, Zeta] (by Name, since these players carry no ID), so a passing run
// proves the tiebreaker actually reordered rather than merely preserving
// insertion order. computeStandings is the UNCACHED core: CalculatePoolStandings
// would cache the first result and hide the nondeterminism.
func TestPoolStandings_TiedPlayersDeterministicOrder(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "xemw-tie"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00", PoolWinners: 2,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Zeta"}, {Name: "Alpha"}}},
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Zeta"}, {Name: "Alpha"},
	}))
	// A draw with no ippons: both finish W:0 L:0 D:1, P:0-0, so they tie on every
	// criterion and no bout separates them.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Zeta", SideB: "Alpha", Status: state.MatchStatusCompleted,
			Winner: "", Decision: string(domain.DecisionHikiwake)},
	}))

	// Map iteration is re-randomized on every computeStandings call, so pre-fix
	// roughly half of these runs flip the pair; 50 runs makes an all-identical
	// result astronomically unlikely without a total order.
	const runs = 50
	var firstOrder []string
	for i := 0; i < runs; i++ {
		standings, err := eng.computeStandings(compID)
		require.NoError(t, err)
		poolA := standings["Pool A"]
		require.Len(t, poolA, 2)
		order := []string{poolA[0].Player.Name, poolA[1].Player.Name}
		if i == 0 {
			firstOrder = order
			continue
		}
		require.Equalf(t, firstOrder, order,
			"tied players must rank identically on every computeStandings call (run %d)", i)
	}
	assert.Equal(t, []string{"Alpha", "Zeta"}, firstOrder,
		"with no IDs, a genuine tie must resolve deterministically by Name")
}
