package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures ---------------------------------------------------------------

// drawSeededComp draws a pools + knockout (or knockout-only) competition of n
// competitors, one per dojo, the first `seeds` of them seeded 1, 2, ... in
// roster order, and returns its pools (nil for a knockout-only draw).
func drawSeededComp(t *testing.T, eng *Engine, store *state.Store, compID, format string, n, seeds int, opts ...func(*state.Competition)) []helper.Pool {
	t.Helper()
	createTestCompetition(t, store, compID, format, 3, opts...)
	players := make([]domain.Player, n)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%02d", i+1), Dojo: fmt.Sprintf("Dojo %02d", i+1)}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	var assignments []domain.SeedAssignment
	for r := 1; r <= seeds; r++ {
		assignments = append(assignments, domain.SeedAssignment{Name: players[r-1].Name, Dojo: players[r-1].Dojo, SeedRank: r})
	}
	require.NoError(t, store.SaveSeeds(compID, assignments))
	require.NoError(t, eng.GenerateDraw(compID))
	if format == state.CompFormatKnockout {
		return nil
	}
	return loadSwapPools(t, store, compID)
}

// seededRow finds the pools.csv row of seed rank in pools.
func seededRow(t *testing.T, pools []helper.Pool, rank int) (pool, row int) {
	t.Helper()
	for i, p := range pools {
		for j, pl := range p.Players {
			if pl.Seed == rank {
				return i, j
			}
		}
	}
	require.Failf(t, "no seed", "seed %d is in no pool", rank)
	return 0, 0
}

// unseededRow is the last unseeded row of pools[pool].
func unseededRow(t *testing.T, pools []helper.Pool, pool int) int {
	t.Helper()
	for j := len(pools[pool].Players) - 1; j >= 0; j-- {
		if pools[pool].Players[j].Seed == 0 {
			return j
		}
	}
	require.Failf(t, "no unseeded row", "pool %s holds seeds only", pools[pool].PoolName)
	return 0
}

// requireSeedRefused asserts that the swap of a and b, and its dry run, are
// both refused SwapSeeded, the sentence naming every seed of the two, and
// that nothing in the draw changed.
func requireSeedRefused(t *testing.T, eng *Engine, dir, compID string, a, b SwapSide, seeds ...string) {
	t.Helper()
	before := drawFiles(t, dir, compID)
	for _, dry := range []bool{true, false} {
		_, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b, DryRun: dry})
		requireSwapRefused(t, err, SwapSeeded)
		for _, s := range seeds {
			assert.Contains(t, err.Error(), s)
		}
		assert.Contains(t, err.Error(), "seeds keep their places in the draw")
	}
	assert.Equal(t, before, drawFiles(t, dir, compID), "a refused swap saves nothing")
}

// --- seeds keep their places (operator ruling 2026-11-27) -------------------

// In a pools + knockout draw a seed is refused as either side of a swap, into
// another pool or inside their own, and two seeds are named together.
func TestSwapDrawPlaces_SeedIsRefusedInAPoolsDraw(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "swap-seeded"
	pools := drawSeededComp(t, eng, store, compID, state.CompFormatMixed, 15, 2)
	require.GreaterOrEqual(t, len(pools), 3)
	sp, sr := seededRow(t, pools, 1)
	p2, r2 := seededRow(t, pools, 2)
	require.NotEqual(t, sp, p2)
	op := (sp + 1) % len(pools)
	if op == p2 {
		op = (op + 1) % len(pools)
	}
	seed := poolSwapSide(pools, sp, sr)
	seed2 := poolSwapSide(pools, p2, r2)
	other := poolSwapSide(pools, op, unseededRow(t, pools, op))
	mate := poolSwapSide(pools, sp, unseededRow(t, pools, sp))
	name := pools[sp].Players[sr].Name

	requireSeedRefused(t, eng, dir, compID, seed, other, name+" is seed 1")
	requireSeedRefused(t, eng, dir, compID, other, seed, name+" is seed 1")
	requireSeedRefused(t, eng, dir, compID, seed, mate, name+" is seed 1")
	requireSeedRefused(t, eng, dir, compID, seed, seed2, name+" is seed 1 and "+pools[p2].Players[r2].Name+" is seed 2")

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: mate, B: other})
	require.NoError(t, err)
	assert.True(t, got.Applied, "two unseeded competitors still swap")
}

// In a knockout-only draw the seed is read from the roster, and refused the
// same way.
func TestSwapDrawPlaces_SeedIsRefusedInAKnockoutDraw(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "swap-seeded-ko"
	drawSeededComp(t, eng, store, compID, state.CompFormatKnockout, 8, 1)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	roster, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	var seeded domain.Player
	for _, p := range roster {
		if p.Seed == 1 {
			seeded = p
		}
	}
	require.NotEmpty(t, seeded.ID)
	atSeed, ok := LeafSlotOf(b, seeded.ID)
	require.True(t, ok)
	var others []SwapSide
	for _, id := range b.DrawOrder {
		if id == seeded.ID {
			continue
		}
		place, ok := LeafSlotOf(b, id)
		require.True(t, ok)
		others = append(others, SwapSide{ID: id, Place: place})
	}
	require.GreaterOrEqual(t, len(others), 2)
	seed := SwapSide{ID: seeded.ID, Place: atSeed}

	requireSeedRefused(t, eng, dir, compID, seed, others[0], seeded.Name+" is seed 1")
	requireSeedRefused(t, eng, dir, compID, others[0], seed, seeded.Name+" is seed 1")

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: others[0], B: others[1]})
	require.NoError(t, err)
	assert.True(t, got.Applied, "two unseeded competitors still swap")
}
