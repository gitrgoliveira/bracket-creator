package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImproveDojoMeetings_AllQualifierNeverVetoesWinnerPathImprovement pins
// bc-drwx item 7: the all-qualifier (tier d) "never earlier" terms used to
// be ANDed into the exchange's accept guard as a HARD PRECONDITION alongside
// the winner-path guard, so tier (d) could VETO a swap tiers (a)-(c)
// preferred outright. Repro (operator-observed): a swap moving one dojo's
// winner-path meeting from round 1 to a later round was refused because a
// second, unrelated dojo's all-qualifier crossing moved earlier -- even
// though nothing about the winner-path tiers objected to the first dojo's
// improvement.
//
// Hand-built pools/qualifierSlots (bypassing the real tree builder, for
// exact control over every round number): 4 pools, poolWinners=2.
//   - pool0 winner=0, crossed=50; pool1 winner=1, crossed=101;
//     pool2 winner=16, crossed=200; pool3 winner=1000, crossed=100.
//   - DojoX has a SEEDED anchor in pool0 (excluded from the exchange
//     entirely) and one movable member in pool1: winner-path meeting
//     dojoMeetRound(0,1)=1 (round 1) before, dojoMeetRound(0,16)=5 after
//     moving to pool2 -- the swap the exchange SHOULD take, since it turns
//     one of the round-1 dojos in the objective's own round-ones count to
//     zero.
//   - DojoY has one movable member in pool2 (the swap's other half) and a
//     SEEDED anchor in pool3. Winner-path meeting is unaffected by the
//     swap (pool2/pool3 -> pool1/pool3, both round 10, verified below).
//     ALL-QUALIFIER meeting (worst case over winner+crossed slots) is
//     round 7 before (pool2/pool3) and round 1 after (pool1/pool3, driven
//     by crossed(pool1)=101 landing adjacent to crossed(pool3)=100) --
//     strictly WORSE, exactly the "never earlier" violation tier (d) used
//     to hard-veto on.
//   - Every OTHER unseeded player is seeded out of candidacy (only the two
//     movable players above are unseeded), so this is the ONLY swap the
//     exchange loop can ever consider: a deterministic, unambiguous pin.
func TestImproveDojoMeetings_AllQualifierNeverVetoesWinnerPathImprovement(t *testing.T) {
	qualifierSlots := [][]int{
		{0, 50},     // pool0
		{1, 101},    // pool1
		{16, 200},   // pool2
		{1000, 100}, // pool3
	}
	// Sanity: pin the exact round numbers this test's whole construction
	// depends on, so a change to dojoMeetRound's arithmetic fails HERE
	// with a clear message rather than producing a confusing false
	// pass/fail below.
	require.Equal(t, 1, dojoMeetRound(0, 1), "sanity: DojoX's before winner-path meeting")
	require.Equal(t, 5, dojoMeetRound(0, 16), "sanity: DojoX's after winner-path meeting")

	buildPools := func() []Pool {
		return []Pool{
			{PoolName: "Pool A", Players: []Player{{Name: "X0", Dojo: "DojoX", Seed: 1}}},
			{PoolName: "Pool B", Players: []Player{{Name: "MovableX", Dojo: "DojoX"}}},
			{PoolName: "Pool C", Players: []Player{{Name: "MovableY", Dojo: "DojoY"}}},
			{PoolName: "Pool D", Players: []Player{{Name: "Y3", Dojo: "DojoY", Seed: 2}}},
		}
	}

	pools := buildPools()
	keys := make(dojoKeyCache)
	ids := newDojoIDCache(keys, 0)
	for i := range pools {
		for _, pl := range pools[i].Players {
			ids.of(pl.Dojo)
		}
	}
	improveDojoMeetings(pools, qualifierSlots, ids)

	assert.Equal(t, "DojoX", pools[2].Players[0].Dojo,
		"Pool C must now hold the DojoX player: tiers (a)-(c) strictly prefer this swap "+
			"(it turns DojoX's round-1 winner-path meeting into round 5, dropping the objective's "+
			"round-ones count from 1 to 0) and tier (d) must never veto it")
	assert.Equal(t, "DojoY", pools[1].Players[0].Dojo,
		"Pool B must now hold the DojoY player (the swap's other half)")
}
