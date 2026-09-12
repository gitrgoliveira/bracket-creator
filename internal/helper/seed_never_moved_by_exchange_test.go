package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findPlayerPool returns the index of the pool containing the player named
// name, or -1 if not found. Test helper shared by the two pins below.
func findPlayerPool(pools []Pool, name string) int {
	for pi, p := range pools {
		for _, pl := range p.Players {
			if pl.Name == name {
				return pi
			}
		}
	}
	return -1
}

// TestImproveDojoMeetings_NeverMovesASeed pins the operator ruling (bc-pnum
// ruling 2c): "a seeded competitor is never moved by the exchange"
// (docs/user-guide/organisers/knockout-draw.md). improveDojoMeetings already
// guards both candidates of every swap it considers (`if a.Seed > 0 {
// continue }` / `if b.Seed > 0 { continue }`, pool_distribution_tree_aware.go),
// so this exercises that guard through the PUBLIC draw entry
// (BuildPoolPhaseTreeAware, which runs the descent then the exchange pass)
// on a roster engineered to tempt a swap involving the seed.
//
// The shape (3 pools of 4, 3 dojos of 4, seed 1 on the first member of the
// first dojo group) was found by probing buildMultiDojoRoster shapes with
// the guard temporarily disabled: with the guard removed, the exchange DOES
// relocate this seed (from pool 0 to pool 2) on this exact shape, proving it
// is a genuine temptation and not a vacuous scenario the guard was never
// going to be tested against.
//
// RED-VERIFIED (temporarily, while building this fix): commenting out both
// `if a.Seed > 0` / `if b.Seed > 0` guards in improveDojoMeetings reddens
// the assertion below (the seed's pool moves from 0 to 2); restored before
// landing.
func TestImproveDojoMeetings_NeverMovesASeed(t *testing.T) {
	const numPools, poolSize, nDojos, dojoGroupSize = 3, 4, 3, 4
	r := buildMultiDojoRoster(numPools, poolSize, nDojos, dojoGroupSize, 1)
	require.Equal(t, 1, r[0].Seed, "the roster's first member must be the seed this scenario was built around")

	descentAlone, _, err := buildPoolPhaseDojoTree(r, poolSize, false, 1, 1)
	require.NoError(t, err)
	production, _, err := BuildPoolPhaseTreeAware(r, poolSize, false, 1, 1)
	require.NoError(t, err)

	preExchangePool := findPlayerPool(descentAlone, r[0].Name)
	postExchangePool := findPlayerPool(production, r[0].Name)
	require.GreaterOrEqual(t, preExchangePool, 0, "seed must be placed by the descent")
	require.GreaterOrEqual(t, postExchangePool, 0, "seed must still be placed after the exchange pass")
	assert.Equal(t, preExchangePool, postExchangePool,
		"the winner-path exchange pass must never relocate a seeded competitor")
}

// TestDelayDojoMeetings_NeverMovesASeed pins the same ruling for the pool-
// draw's OTHER dojo-collision repair pass: delayDojoMeetings, called at the
// end of StandardSeeding to spread UNSEEDED competitors across the knockout
// bracket so two dojo-mates meet as late as the draw allows. Its own doc
// comment already states seeded slots are never moved "on EITHER side of a
// swap" (seed.go), enforced via the `movable` closure's `!occupied[i]`
// check, occupied being exactly the seeded-slot map StandardSeeding builds
// before calling it.
//
// The roster is pasted dojo-by-dojo (the function's own doc comment: "16
// competitors from four dojos of four gave EIGHT first-round matches, every
// one intra-dojo" -- exactly the shape this pass exists to repair), with the
// FIRST dojo group's first member seeded rank 1 -- landing it inside the
// very cluster the repair pass wants to break up, where an unconstrained
// repair would want to move it.
//
// RED-VERIFIED (temporarily, while building this fix): weakening `movable`
// to drop the `!occupied[i]` term (so a seeded slot is eligible) reddens the
// assertion below (the seed's slot moves off generateBracketOrder's own
// rank-1 slot); restored before landing.
func TestDelayDojoMeetings_NeverMovesASeed(t *testing.T) {
	players := make([]Player, 0, 16)
	for d := 0; d < 4; d++ {
		for i := 1; i <= 4; i++ {
			players = append(players, Player{Name: fmt.Sprintf("C%d_%d", d, i), Dojo: fmt.Sprintf("Dojo%d", d)})
		}
	}
	players[0].Seed = 1
	seedName := players[0].Name

	result := StandardSeeding(players)
	gotSlot := -1
	for i, p := range result {
		if p.Name == seedName {
			gotSlot = i
			break
		}
	}
	require.GreaterOrEqual(t, gotSlot, 0, "the seed must still be placed")

	// The seed's slot is entirely determined by generateBracketOrder for
	// rank 1: compute the SAME expected slot independently, so this pin
	// does not merely assert "whatever StandardSeeding did to itself" but
	// the specific slot delayDojoMeetings must leave untouched.
	power := 1
	for power < len(players) {
		power *= 2
	}
	order := generateBracketOrder(power)
	wantSlot := -1
	for slot, rank := range order {
		if slot >= len(players) {
			break
		}
		if rank == 1 {
			wantSlot = slot
			break
		}
	}
	require.GreaterOrEqual(t, wantSlot, 0, "rank 1 must have a slot within the roster's own length")
	assert.Equal(t, wantSlot, gotSlot,
		"delayDojoMeetings must never relocate a seeded competitor's slot")
}
