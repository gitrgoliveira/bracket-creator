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
// guard, pinning the ID branch of the tiebreaker. computeStandingsFrom
// builds a map[string]*PlayerStanding and ranges it into a slice, then sorts
// by a single packed Points score. Two players who tie on every ranking
// criterion (equal Points, no supplementary bout) compare equal in that
// sort, so their relative order used to be left to Go's randomized map
// iteration -- different on every call. For a live tournament that means two
// tied qualifiers seed into the knockout in a run-dependent order. The fix
// adds a total-order (ID, then Name) tiebreaker; per operator ruling,
// ordering tied qualifiers by id rather than by name is INTENDED (bc-pnum).
//
// Pool players carry EXPLICIT, STABLE ids here on purpose. A prior version
// of this fixture saved pools with NO ids at all, to exercise the Name leg
// of the tiebreaker -- but the bc-pnum load-time repair
// (state.upgradePoolParticipantIDsLocked) stamps a FRESH, RANDOM uuid onto
// any pools.csv row saved without one, copied from the roster's matching
// (name, dojo) entry, which participants.csv also minted randomly on save.
// That repair ran on the FIRST computeStandings call in the loop below (via
// LoadPools), so every one of the 50 iterations still saw the SAME
// (repaired) ids and therefore ranked consistently with EACH OTHER -- the
// inner require.Equalf loop stayed green -- but the FINAL assertion against
// a fixed expected order flipped on whichever run's random ids happened to
// sort the other way, roughly half the time. Giving the ids here up front
// means the repair finds nothing to do (every row already has one) and the
// asserted order is these ids' own, fixed, order -- not a coin flip.
// TestPoolStandings_TiedPlayersDeterministicOrder_NoIDs below is the sibling
// that pins the OTHER branch: a roster that genuinely carries no id at all.
//
// The pool players are inserted [Zeta, Alpha] but idAlpha < idZeta, so a
// passing run proves the tiebreaker actually reordered rather than merely
// preserving insertion order.
func TestPoolStandings_TiedPlayersDeterministicOrder(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "xemw-tie"

	const (
		idAlpha = "aaaaaaaa-0000-4000-8000-000000000001"
		idZeta  = "bbbbbbbb-0000-4000-8000-000000000002"
	)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00", PoolWinners: 2,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: idZeta, Name: "Zeta", Dojo: "Dojo Zeta"},
			{ID: idAlpha, Name: "Alpha", Dojo: "Dojo Alpha"},
		}},
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: idZeta, Name: "Zeta", Dojo: "Dojo Zeta"}, {ID: idAlpha, Name: "Alpha", Dojo: "Dojo Alpha"},
	}))
	// A draw with no ippons: both finish W:0 L:0 D:1, P:0-0, so they tie on every
	// criterion and no bout separates them.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Zeta", SideAID: idZeta, SideB: "Alpha", SideBID: idAlpha, Status: state.MatchStatusCompleted,
			Winner: "", Decision: string(domain.DecisionHikiwake)},
	}))

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
		"with explicit stable ids (idAlpha < idZeta), a genuine tie must resolve deterministically by ID")
}

// TestPoolStandings_TiedPlayersDeterministicOrder_NoIDs pins the NAME branch
// of the SAME mp-xemw tiebreaker (see the sibling test above for the ID
// branch and for why this fixture must skip participants.csv entirely): a
// roster that genuinely carries no id anywhere on disk leaves the ID
// comparison a "" == "" no-op, so the sort falls through to Name.
//
// No participants.csv is saved for this competition, so the bc-pnum
// load-time repair (state.upgradePoolParticipantIDsLocked) has no roster to
// resolve pools.csv against and the rows stay id-less -- unlike the sibling
// test, which gives every row an explicit id up front so the repair finds
// nothing to do either, just for a different reason. No pool match is saved
// either: an id-less SideA/SideB can never resolve to a *PlayerStanding
// (registerStandingsPlayer, engi.go, never indexes an id-less player under
// the "" key, so lookupStandingsPlayer always misses it), so a saved match
// here would be silently skipped by computeStandingsFrom's own
// `sA == nil || sB == nil` guard rather than exercising anything -- the two
// players already tie at their zero-value stats without one.
//
// The pool players are inserted [Zeta, Alpha] but the deterministic order is
// [Alpha, Zeta] (by Name), so a passing run proves the tiebreaker actually
// reordered rather than merely preserving insertion order.
func TestPoolStandings_TiedPlayersDeterministicOrder_NoIDs(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "xemw-tie-noids"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00", PoolWinners: 2,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Zeta", Dojo: "Dojo Zeta"}, {Name: "Alpha", Dojo: "Dojo Alpha"}}},
	}))

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
		"with no ids anywhere on disk, a genuine tie must resolve deterministically by Name")
}
