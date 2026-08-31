package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReorderPositionsMatchesReorderPoolsForCourts pins reorderPositions
// against the real ReorderPoolsForCourts: build numPools distinguishable
// synthetic pools (each holding one uniquely-named player, since
// ReorderPoolsForCourts overwrites PoolName itself as part of reordering and
// so cannot be used as the identity marker here), reorder them for real, and
// check that pool i's content ended up at reorderPositions(...)[i]. This is
// the seam a bug slipped through during Phase 2 development (see
// treeAwareQualifierSlots' doc comment) -- pre-reorder and post-reorder pool
// order are different index spaces, and this test is what would have caught
// scoring against the wrong one.
func TestReorderPositionsMatchesReorderPoolsForCourts(t *testing.T) {
	for numPools := 1; numPools <= 10; numPools++ {
		for numCourts := 1; numCourts <= 8; numCourts++ {
			t.Run(fmt.Sprintf("pools=%d courts=%d", numPools, numCourts), func(t *testing.T) {
				pools := make([]Pool, numPools)
				for i := range pools {
					pools[i] = Pool{Players: []Player{{Name: fmt.Sprintf("marker-%d", i)}}}
				}
				reordered := ReorderPoolsForCourts(pools, numCourts)
				post := reorderPositions(numPools, numCourts)
				require.Len(t, post, numPools)
				require.Len(t, reordered, numPools)
				for preIdx, postIdx := range post {
					require.GreaterOrEqualf(t, postIdx, 0, "pool %d", preIdx)
					require.Lessf(t, postIdx, numPools, "pool %d", preIdx)
					require.Len(t, reordered[postIdx].Players, 1)
					assert.Equalf(t, fmt.Sprintf("marker-%d", preIdx), reordered[postIdx].Players[0].Name,
						"pre-reorder pool %d: expected it at post-reorder position %d", preIdx, postIdx)
				}
			})
		}
	}
}

// seedPoolByName maps every seeded player's Name to the PoolName it ended up
// in, for a finished []Pool. Used to compare seed placement between the old
// and new pipelines regardless of unseeded ordering differences.
func seedPoolByName(pools []Pool) map[string]string {
	out := map[string]string{}
	for _, p := range pools {
		for _, pl := range p.Players {
			if pl.Seed > 0 {
				out[pl.Name] = p.PoolName
			}
		}
	}
	return out
}

// TestSeedPlacementEquality_OldVsTreeAware is Phase 2's mandated pin: every
// seed must land in the SAME named pool as the PRE-SWAP pipeline put it,
// because placeSeedIndices is an extraction of PoolSeeding's own seed
// arithmetic -- any drift means the extraction broke something, not that the
// new path made a different but valid choice.
//
// The old side is referencePoolSeedingPipeline (the gate test's faithful
// reconstruction of PoolSeeding -> CreatePools -> ReorderPoolsForCourts).
// It was BuildPoolPhase when this pin was written, which was right up until
// the Phase 4 swap made BuildPoolPhase DELEGATE to the tree-aware path --
// from that moment the pin compared the new path against itself and held for
// any seed arithmetic whatsoever. A self-comparison that can never fail is
// not a pin.
func TestSeedPlacementEquality_OldVsTreeAware(t *testing.T) {
	total := 0
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			for nSeeds := 2; nSeeds <= 4 && nSeeds <= numPools; nSeeds++ {
				for dojoExtra := 0; dojoExtra <= 4; dojoExtra++ {
					n := numPools * poolSize
					if nSeeds+dojoExtra > n {
						continue
					}
					r := make([]Player, 0, n)
					for s := 1; s <= nSeeds; s++ {
						r = append(r, Player{Name: fmt.Sprintf("Seed%d", s), Dojo: "SeedDojo", Seed: s})
					}
					for i := 1; i <= dojoExtra; i++ {
						r = append(r, Player{Name: fmt.Sprintf("Mate%d", i), Dojo: "SeedDojo"})
					}
					for i := len(r) + 1; i <= n; i++ {
						r = append(r, Player{Name: fmt.Sprintf("O%d", i), Dojo: fmt.Sprintf("D%02d", i)})
					}

					for _, numCourts := range []int{1, 2, 4} {
						for _, poolWinners := range []int{1, 2} {
							oldPools, _, err := referencePoolSeedingPipeline(r, poolSize, false, numCourts)
							require.NoError(t, err)
							newPools, _, err := BuildPoolPhaseTreeAware(r, poolSize, false, numCourts, poolWinners)
							require.NoError(t, err)
							total++

							oldSeedPool := seedPoolByName(oldPools)
							newSeedPool := seedPoolByName(newPools)
							require.Equal(t, len(oldSeedPool), len(newSeedPool),
								"pools=%d size=%d seeds=%d extra=%d courts=%d winners=%d: seed count differs",
								numPools, poolSize, nSeeds, dojoExtra, numCourts, poolWinners)
							for name, oldPool := range oldSeedPool {
								assert.Equalf(t, oldPool, newSeedPool[name],
									"pools=%d size=%d seeds=%d extra=%d courts=%d winners=%d: seed %s landed in %s (old) vs %s (new)",
									numPools, poolSize, nSeeds, dojoExtra, numCourts, poolWinners, name, oldPool, newSeedPool[name])
							}
						}
					}
				}
			}
		}
	}
	require.GreaterOrEqual(t, total, 200, "sweep shrank below the measured 210-config space (x courts x winners)")
}

// TestSeedPlacementEquality_MultiDojo broadens the pin with the Phase-0
// multi-dojo roster generator (several dojos at once, isMax variants, seeds
// beyond maxSeedRanks=4).
func TestSeedPlacementEquality_MultiDojo(t *testing.T) {
	total := 0
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			for _, isMax := range []bool{false, true} {
				for nDojos := 2; nDojos <= 4; nDojos++ {
					for dojoGroupSize := 2; dojoGroupSize <= numPools+2; dojoGroupSize++ {
						for nSeeds := 1; nSeeds <= 4 && nSeeds < numPools; nSeeds++ {
							// nSeeds starts at 1: a seedless config has no
							// placement to compare, and the seedless sweep
							// burned most of this test's former 23-second
							// runtime comparing empty maps.
							if nDojos*dojoGroupSize > numPools*poolSize {
								continue
							}
							r := buildMultiDojoRoster(numPools, poolSize, nDojos, dojoGroupSize, nSeeds)
							for _, courts := range []int{1, 2} {
								oldPools, _, err := referencePoolSeedingPipeline(r, poolSize, isMax, courts)
								require.NoError(t, err)
								newPools, _, err := BuildPoolPhaseTreeAware(r, poolSize, isMax, courts, 1)
								require.NoError(t, err)
								total++

								oldSeedPool := seedPoolByName(oldPools)
								newSeedPool := seedPoolByName(newPools)
								for name, oldPool := range oldSeedPool {
									assert.Equalf(t, oldPool, newSeedPool[name],
										"pools=%d size=%d isMax=%v dojos=%dx%d seeds=%d courts=%d: seed %s mismatch",
										numPools, poolSize, isMax, nDojos, dojoGroupSize, nSeeds, courts, name)
								}
							}
						}
					}
				}
			}
		}
	}
	require.GreaterOrEqual(t, total, 2000, "sweep shrank; the pin is meaningless if it no longer covers the measured space")
}

// TestBuildPoolPhaseTreeAware_SizesMatchOldPath pins that the new path's
// pool SIZES equal the old path's on every config -- a hard requirement
// independent of who ends up in which pool.
func TestBuildPoolPhaseTreeAware_SizesMatchOldPath(t *testing.T) {
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			for _, isMax := range []bool{false, true} {
				for _, courts := range []int{1, 2, 4} {
					n := numPools * poolSize
					r := make([]Player, n)
					for i := range r {
						r[i] = Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo%03d", i+1)}
					}
					oldPools, oldCourts, err := BuildPoolPhase(r, poolSize, isMax, courts)
					require.NoError(t, err)
					newPools, newCourts, err := BuildPoolPhaseTreeAware(r, poolSize, isMax, courts, 1)
					require.NoError(t, err)

					require.Equal(t, oldCourts, newCourts, "pools=%d size=%d isMax=%v courts=%d: drawCourts differs", numPools, poolSize, isMax, courts)
					require.Len(t, newPools, len(oldPools))
					oldSizes := make([]int, len(oldPools))
					newSizes := make([]int, len(newPools))
					for i, p := range oldPools {
						oldSizes[i] = len(p.Players)
					}
					for i, p := range newPools {
						newSizes[i] = len(p.Players)
					}
					assert.Equal(t, oldSizes, newSizes, "pools=%d size=%d isMax=%v courts=%d: pool sizes differ", numPools, poolSize, isMax, courts)

					total := 0
					for _, p := range newPools {
						total += len(p.Players)
					}
					assert.Equal(t, n, total, "player lost or duplicated in the new path")
				}
			}
		}
	}
}

// TestBuildPoolPhaseTreeAware_NoTwoSeedsShareAPool is a basic sanity check
// independent of the scorecard: the new path must never put two seeds in one
// pool, exactly like the old path.
func TestBuildPoolPhaseTreeAware_NoTwoSeedsShareAPool(t *testing.T) {
	for numPools := 3; numPools <= 7; numPools++ {
		for nSeeds := 2; nSeeds <= 4 && nSeeds <= numPools; nSeeds++ {
			r := buildMultiDojoRoster(numPools, 4, 1, nSeeds, nSeeds)
			pools, _, err := BuildPoolPhaseTreeAware(r, 4, false, 2, 1)
			require.NoError(t, err)
			for _, p := range pools {
				seeds := 0
				for _, pl := range p.Players {
					if pl.Seed > 0 {
						seeds++
					}
				}
				assert.LessOrEqual(t, seeds, 1, "two seeds share %s (pools=%d seeds=%d)", p.PoolName, numPools, nSeeds)
			}
		}
	}
}

// TestBuildPoolPhaseTreeAwareWithMode_RefusesBlankDojo pins FIX 1
// (bc-dojo-least-conflicted-pool): a roster containing a blank-dojo player
// must be refused outright by the shared pre-flight
// (buildPoolPhaseTreeAwareCore), reached by all three tree-aware entry
// points, rather than silently corrupting the descent's capacity accounting
// (recordDojoOccupancy is guarded on `p.Dojo != ""`, so a blank-dojo player
// consumes a real pool seat without the tree ever seeing it) or
// improveDojoMeetings' spread/meeting objective (which would otherwise count
// Dojo=="" as a phantom dojo). The error must name the offending player and
// no pools may be returned.
func TestBuildPoolPhaseTreeAwareWithMode_RefusesBlankDojo(t *testing.T) {
	players := []Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "NoDojo", Dojo: ""},
		{Name: "Carol", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoA"},
		{Name: "Erin", Dojo: "DojoB"},
		{Name: "Frank", Dojo: "DojoC"},
		{Name: "Grace", Dojo: "DojoA"},
	}

	pools, drawCourts, err := BuildPoolPhaseTreeAwareWithMode(players, 4, false, 1, 2, "", 0)
	require.Error(t, err, "a blank-dojo roster must be refused, not silently drawn")
	assert.ErrorIs(t, err, ErrBlankDojo)
	assert.Contains(t, err.Error(), "NoDojo", "the error must name the offending player so the operator knows which row to repair")
	assert.Nil(t, pools)
	assert.Zero(t, drawCourts)

	// Every tree-aware entry point funnels through the same pre-flight.
	pools2, _, err2 := BuildPoolPhaseTreeAware(players, 4, false, 1, 2)
	require.Error(t, err2)
	assert.ErrorIs(t, err2, ErrBlankDojo)
	assert.Nil(t, pools2)

	pools3, _, err3 := BuildPoolPhaseFillBracketTreeAware(players, 4, 1)
	require.Error(t, err3)
	assert.ErrorIs(t, err3, ErrBlankDojo)
	assert.Nil(t, pools3)
}

// TestBuildPoolPhaseTreeAwareWithMode_RefusesBlankDojo_MultipleNames checks
// that every offending player is named, not just the first found, so an
// operator with several bad rows can fix them all from one error rather
// than re-running the draw once per row.
func TestBuildPoolPhaseTreeAwareWithMode_RefusesBlankDojo_MultipleNames(t *testing.T) {
	players := []Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "NoDojoOne", Dojo: ""},
		{Name: "NoDojoTwo", Dojo: ""},
		{Name: "Carol", Dojo: "DojoC"},
	}
	_, _, err := BuildPoolPhaseTreeAwareWithMode(players, 2, false, 1, 2, "", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "NoDojoOne")
	assert.Contains(t, err.Error(), "NoDojoTwo")
}
