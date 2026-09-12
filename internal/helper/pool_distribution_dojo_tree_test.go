package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file exercises the dojo-tree descent
// (assignUnseededByDojoTree and friends, bottom of
// pool_distribution_tree_aware.go, bc-dojo-least-conflicted-pool) IN
// ISOLATION, without the winner-path pairwise-exchange pass
// (improveDojoMeetings) that runs immediately after it in production
// (buildPoolPhaseTreeAwareCore's own step 3 then step 4). Isolating the
// descent this way is what lets TestDojoTreeDescent_PoolWinnersTwoRegression
// pin the exchange pass's OWN contribution (see that test's doc comment)
// separately from the descent's.

// buildPoolPhaseDojoTree runs the DESCENT ALONE, standalone: the same
// pool-count/target-size/seed-placement steps buildPoolPhaseTreeAwareCore
// uses, but stopping after assignUnseededByDojoTree -- no
// improveDojoMeetings call, so no winner-path exchange repair. Production
// (BuildPoolPhaseTreeAware) is this SAME sequence plus that repair pass.
func buildPoolPhaseDojoTree(players []Player, poolSize int, isMax bool, numCourts int, poolWinners int) ([]Pool, int, error) {
	numPools, baseTargetSizes, err := poolTargetSizes(len(players), poolSize, isMax)
	if err != nil {
		return nil, 0, err
	}
	drawCourts := EffectiveDrawCourts(numPools, numCourts)

	pools := make([]Pool, numPools)
	for i := range pools {
		pools[i].PoolName = poolPositionName(i)
	}
	targetSizes := realTargetSizes(baseTargetSizes, len(players))
	seeded, unseeded := partitionSeeded(players)

	seedIdx := placeSeedIndices(seeded, numPools, clampCourts(drawCourts), len(players))
	for si, idx := range seedIdx {
		if idx < 0 {
			continue
		}
		p := seeded[si]
		poolIdx := idx % numPools
		p.PoolPosition = int64(len(pools[poolIdx].Players) + 1)
		pools[poolIdx].Players = append(pools[poolIdx].Players, p)
	}

	qualifierSlots := treeAwareQualifierSlots(targetSizes, poolWinners, drawCourts, qualifierMode{ExtraQualifiers: QualifierModeStandard})
	ids, _ := newDojoIDCacheFor(players)
	if err := assignUnseededByDojoTree(pools, targetSizes, unseeded, qualifierSlots, ids); err != nil {
		return nil, 0, err
	}
	return ReorderPoolsForCourts(pools, drawCourts), drawCourts, nil
}

// TestDojoTreeDescent_PerPoolOptimum_PoolWinnersOne re-runs the gate's own
// 2048-config multi-dojo sweep (pool_distribution_gate_test.go's
// TestTreeAwareGateScorecard) through the DESCENT ALONE (no exchange pass),
// at poolWinners=1 -- the value every one of those configs actually uses.
// This invariant holds through the descent alone (no exchange needed), so
// it stays green here.
func TestDojoTreeDescent_PerPoolOptimum_PoolWinnersOne(t *testing.T) {
	total := 0
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			for courts := 1; courts <= 2; courts++ {
				for nDojos := 2; nDojos <= 4; nDojos++ {
					for dojoGroupSize := 2; dojoGroupSize <= 2*numPools; dojoGroupSize++ {
						for nSeeds := 0; nSeeds <= 4 && nSeeds < numPools; nSeeds++ {
							if nDojos*dojoGroupSize > numPools*poolSize {
								continue
							}
							r := buildMultiDojoRoster(numPools, poolSize, nDojos, dojoGroupSize, nSeeds)
							pools, _, err := buildPoolPhaseDojoTree(r, poolSize, false, courts, 1)
							require.NoError(t, err)
							total++

							placed := 0
							for _, p := range pools {
								placed += len(p.Players)
							}
							assert.Equal(t, numPools*poolSize, placed, "player lost or duplicated")

							optimum := (dojoGroupSize + numPools - 1) / numPools
							for c := 0; c < nDojos; c++ {
								dojo := fmt.Sprintf("Dojo%d", c)
								assert.LessOrEqualf(t, maxOfDojo(pools, dojo), optimum,
									"%s over-concentrated (pools=%d size=%d dojos=%dx%d seeds=%d)",
									dojo, numPools, poolSize, nDojos, dojoGroupSize, nSeeds)
							}
						}
					}
				}
			}
		}
	}
	require.GreaterOrEqual(t, total, 2000, "sweep shrank; the pin is meaningless if it no longer covers the measured space")
}

// TestDojoTreeDescent_UniqueDojoIdentity pins that the descent's bypass
// path (pickDojoTreeAwarePool falling through to leastConflictedPool
// whenever a dojo has nobody placed anywhere yet) reproduces the SAME
// round-robin deal TestPoolDistribution_UniqueDojoIdentity pins for
// production: an all-unique-dojo roster hits that bypass on every single
// placement, so the deal comes from leastConflictedPool's own
// index-ordered scan, never from the tree, and the exchange pass is a
// no-op on such a roster by construction (no dojo ever spans >=2 pools),
// so descent-alone and full production agree here too.
func TestDojoTreeDescent_UniqueDojoIdentity(t *testing.T) {
	for _, numPools := range []int{3, 5, 8} {
		for _, poolSize := range []int{3, 4} {
			n := numPools * poolSize
			r := make([]Player, n)
			for i := range r {
				r[i] = Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo %03d", i+1)}
			}
			pools, _, err := buildPoolPhaseDojoTree(r, poolSize, false, 2, 1)
			require.NoError(t, err)

			poolOf := map[string]string{}
			for _, p := range pools {
				for _, pl := range p.Players {
					poolOf[pl.Name] = p.PoolName
				}
			}
			for i := 0; i < n; i++ {
				for j := i + 1; j < n; j++ {
					same := poolOf[r[i].Name] == poolOf[r[j].Name]
					want := i%numPools == j%numPools
					assert.Equal(t, want, same,
						"pools=%d size=%d: players %d and %d break the round-robin deal", numPools, poolSize, i, j)
				}
			}
		}
	}
}

// TestDojoTreeDescent_PoolWinnersTwoRegression is bc-dojo-least-conflicted-pool's
// pinned example of the ONE gap the descent alone could not close, and of
// the winner-path pairwise exchange (improveDojoMeetings, wired into
// production as buildPoolPhaseTreeAwareCore's step 4) CLOSING it: the exact
// shape that motivated the OLD pipeline's three-way rotation (5 pools of
// 3, four 3-member dojos, poolWinners 2). Both the descent alone and full
// production reach WINNER-PATH round-1 count 0 here (round-1 count was
// never the issue for this shape under the operator's winner-path metric
// -- see this file's own top-of-file doc comment and
// pool_distribution_invariants_test.go's TestPoolDistribution_RealRosters_MultiDojo
// for the same ruling applied elsewhere). The gap is on the lexicographic
// tie-break the multi-dojo sweep's decision rule also checks (round-1
// count, THEN sum of finite winner-path rounds): descent alone leaves one
// dojo meeting its own qualifiers one round earlier than a further
// exchange could still buy it (sum 11); production's exchange pass finds
// that exchange and reaches sum 12, matching the OLD all-qualifier-scored
// pipeline's own result on this shape (its own historical value, no
// longer reproducible directly since that pipeline was deleted alongside
// this productionisation). This is one of the 12-of-1596 configs in the
// required sweep with the identical pattern (round-1 tied at 0, descent
// alone one worse on sum), all at poolWinners=2, all closed by the
// exchange, zero regressions introduced by it (TestVERIFYFullSweep-shaped
// sweep run during development; not committed as a file since it
// duplicates TestTreeAwareGateScorecard's own end-to-end coverage).
//
// RED-VERIFIED (temporarily, while building this): commenting out the
// improveDojoMeetings call in buildPoolPhaseTreeAwareCore collapses
// production's result to the descent-alone one (sum 11), reddening the
// productionSum assertion below; restored before landing.
func TestDojoTreeDescent_PoolWinnersTwoRegression(t *testing.T) {
	r := buildMultiDojoRoster(5, 3, 4, 3, 0)

	descentAlone, descentCourts, err := buildPoolPhaseDojoTree(r, 3, false, 1, 2)
	require.NoError(t, err)
	production, productionCourts, err := BuildPoolPhaseTreeAware(r, 3, false, 1, 2)
	require.NoError(t, err)

	winnerPathStats := func(pools []Pool, courts int) (roundOnes, sum int) {
		seen := map[string]bool{}
		for _, p := range pools {
			for _, pl := range p.Players {
				if seen[pl.Dojo] {
					continue
				}
				seen[pl.Dojo] = true
				if m := earliestDojoWinnerMeetingRound(pools, 2, courts, pl.Dojo); m != mathMaxIntSentinel {
					sum += m
					if m <= 1 {
						roundOnes++
					}
				}
			}
		}
		return roundOnes, sum
	}

	descentRoundOnes, descentSum := winnerPathStats(descentAlone, descentCourts)
	productionRoundOnes, productionSum := winnerPathStats(production, productionCourts)

	assert.Equal(t, 0, descentRoundOnes, "descent alone should reach WINNER-PATH round-1 count 0 on this shape")
	assert.Equal(t, 11, descentSum, "descent alone's winner-path sum drifted on this shape; re-derive before trusting the exchange comparison below")
	assert.Equal(t, 0, productionRoundOnes, "production should ALSO reach WINNER-PATH round-1 count 0 on this shape")
	assert.Equal(t, 12, productionSum, "PINNED: the winner-path exchange pass must close the descent-alone gap on this shape (sum 12, matching the old all-qualifier-scored pipeline's own result) -- a value of 11 here means the exchange stopped searching once round-1 count hit zero instead of continuing to improve the sum, which is exactly the regression this test exists to catch")
	assert.Greater(t, productionSum, descentSum, "production must strictly improve on the descent alone here -- this is the exchange pass's OWN measured contribution")
}

// TestDojoTreeDescent_RoomPoolsFunnel is the RED/GREEN pin for the
// roomPools tie-break (dojoNode's own doc comment): a per-pool-count
// NORMALISED comparison alone (dojoCount/poolCount, capacity/poolCount)
// still funnels a dojo's members into one lone pool once every pool in a
// branch already holds exactly one of that dojo, because a single roomy
// pool can keep outscoring a tied pair on capacity alone. Uses the exact
// roster (10-of-24, interleaved) and shape (poolSize 4, 2 courts, the
// production default poolWinners=2 BuildPoolPhase uses) that pinned this in
// TestPoolSeeding_DojoSpreadFallback (seed_test.go) for production
// end-to-end; this test is that same pin for the descent stage in
// isolation. Commenting out the roomPools cases in chooseDojoTreePool
// (falling through to the capacity-only comparison) reddens this test --
// verified while building the fix, not asserted here since a real
// regression test cannot also ship the bug it guards against.
func TestDojoTreeDescent_RoomPoolsFunnel(t *testing.T) {
	players := buildOversubscribedDojoRoster(24, 10, "Tora Dojo")

	pools, _, err := buildPoolPhaseDojoTree(players, 4, false, 2, defaultPoolWinners)
	require.NoError(t, err)
	require.Len(t, pools, 6)

	toraCounts := make([]int, len(pools))
	keys := make(dojoKeyCache)
	for i, pool := range pools {
		toraCounts[i] = countDojoInPool(pool, "Tora Dojo", keys)
	}
	maxCount := 0
	for _, c := range toraCounts {
		if c > maxCount {
			maxCount = c
		}
	}
	assert.LessOrEqual(t, maxCount, 2, "Tora Dojo should never exceed 2 players in any pool, got per-pool counts %v", toraCounts)
}

// TestDojoTreeDescent_DojoOptimumGuard is the RED/GREEN pin for the
// dojoOptimum hard-cap guard (assignUnseededByDojoTree's own doc comment):
// without it, a dojo already at its per-pool optimum in every branch the
// tree descent considers "safe" can still be handed a further member
// whenever the descent's relative comparisons all tie, because nothing in
// the descent itself knows the dojo's ABSOLUTE ceiling. Uses a LESS extreme
// oversubscription than "dojo/deep-oversubscription" (which has zero slack
// anywhere and reaches its violation only at the roster's literal last
// remaining seat, unavoidable for any forward-only pass -- see this file's
// own doc comment) so the guard has an actual alternative pool to redirect
// to and the assertion is a real pass, not a coincidence: bead-scenario's
// own shape (24 entrants, 10 from one dojo, pool size 4, 2 courts).
func TestDojoTreeDescent_DojoOptimumGuard(t *testing.T) {
	roster := drawGoldenDojoRoster(24, 10, drawGoldenDojoName)

	pools, _, err := buildPoolPhaseDojoTree(roster, drawGoldenPoolSize, false, 2, drawGoldenDojoPoolWinners)
	require.NoError(t, err)

	_, maxCount, _ := computeDojoOversubscriptionStats(pools, drawGoldenDojoName)
	optimum := (10 + len(pools) - 1) / len(pools)
	assert.LessOrEqual(t, maxCount, optimum, "dojoOptimum guard should keep every pool at or under ceil(10/%d)=%d", len(pools), optimum)
}

// TestProduction_DeepOversubscription_SpreadTierClosesLastSeat is the
// pinned regression for improveDojoMeetings' tier (a) (cap excess, this
// file's own top-of-file... see pool_distribution_tree_aware.go's
// improveDojoMeetings doc comment for the tier's full rationale): the
// "dojo/deep-oversubscription" shape (draw_shapes_golden_test.go, 24
// entrants, 12 from one dojo -- EXACTLY half the roster, zero slack
// anywhere) is the one case the descent's own OWN forward-only cap guard
// (assignUnseededByDojoTree's dojoOptimum/poolUnderDojoCap) cannot reach:
// by the time the roster's LAST unseeded member is placed, only one seat
// remains in the ENTIRE roster, and no amount of look-ahead during
// PLACEMENT can route around that. The exchange pass, running AFTER every
// player already has a seat, can: it trades a member of the over-cap pool
// for a member of a different dojo sitting in an at-cap-minus-one pool,
// which strictly reduces total excess with no forward-only guard's
// blindness to contend with. Operator ruling: "provably irreducible" was
// wrong for the shipped (descent-alone) placement ORDER specifically, not
// for this problem in general -- the OLD (pre-descent) fill+repair
// pipeline reached [2,2,2,2,2,2] on this exact shape, and production
// (descent + this tier) now does too.
func TestProduction_DeepOversubscription_SpreadTierClosesLastSeat(t *testing.T) {
	roster := drawGoldenDojoRoster(24, 12, drawGoldenDojoName)

	pools, _, err := BuildPoolPhase(roster, drawGoldenPoolSize, false, 2)
	require.NoError(t, err)

	_, maxCount, _ := computeDojoOversubscriptionStats(pools, drawGoldenDojoName)
	assert.Equal(t, 2, maxCount, "PINNED: production must reach MaxSameDojoCount 2 on the deep-oversubscription shape (the exchange pass's tier (a) closing the descent's one unreachable corner) -- a value of 3 here means tier (a) regressed")
}
