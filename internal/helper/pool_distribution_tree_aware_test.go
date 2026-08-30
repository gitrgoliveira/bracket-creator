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
				for clubExtra := 0; clubExtra <= 4; clubExtra++ {
					n := numPools * poolSize
					if nSeeds+clubExtra > n {
						continue
					}
					r := make([]Player, 0, n)
					for s := 1; s <= nSeeds; s++ {
						r = append(r, Player{Name: fmt.Sprintf("Seed%d", s), Dojo: "SeedClub", Seed: s})
					}
					for i := 1; i <= clubExtra; i++ {
						r = append(r, Player{Name: fmt.Sprintf("Mate%d", i), Dojo: "SeedClub"})
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
								numPools, poolSize, nSeeds, clubExtra, numCourts, poolWinners)
							for name, oldPool := range oldSeedPool {
								assert.Equalf(t, oldPool, newSeedPool[name],
									"pools=%d size=%d seeds=%d extra=%d courts=%d winners=%d: seed %s landed in %s (old) vs %s (new)",
									numPools, poolSize, nSeeds, clubExtra, numCourts, poolWinners, name, oldPool, newSeedPool[name])
							}
						}
					}
				}
			}
		}
	}
	require.GreaterOrEqual(t, total, 200, "sweep shrank below the measured 210-config space (x courts x winners)")
}

// TestSeedPlacementEquality_MultiClub broadens the pin with the Phase-0
// multi-club roster generator (several clubs at once, isMax variants, seeds
// beyond maxSeedRanks=4).
func TestSeedPlacementEquality_MultiClub(t *testing.T) {
	total := 0
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			for _, isMax := range []bool{false, true} {
				for nClubs := 2; nClubs <= 4; nClubs++ {
					for clubSize := 2; clubSize <= numPools+2; clubSize++ {
						for nSeeds := 1; nSeeds <= 4 && nSeeds < numPools; nSeeds++ {
							// nSeeds starts at 1: a seedless config has no
							// placement to compare, and the seedless sweep
							// burned most of this test's former 23-second
							// runtime comparing empty maps.
							if nClubs*clubSize > numPools*poolSize {
								continue
							}
							r := buildClubRoster(numPools, poolSize, nClubs, clubSize, nSeeds)
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
										"pools=%d size=%d isMax=%v clubs=%dx%d seeds=%d courts=%d: seed %s mismatch",
										numPools, poolSize, isMax, nClubs, clubSize, nSeeds, courts, name)
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
			r := buildClubRoster(numPools, 4, 1, nSeeds, nSeeds)
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
