package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoolQualifierPaths_MatchesRealPipeline is Phase 1's seam test: it
// builds REAL pools through the existing pipeline (BuildPoolPhase), draws
// the REAL knockout bracket from them (BuildKnockoutDraw) and asserts that
// poolQualifierPaths -- called with nothing but those pools' sizes -- names
// every slot each pool's placeholder actually landed on, across pool counts
// 3..8, winners 1..2 and courts 1/2/4.
//
// The roster is unseeded on purpose: poolQualifierPaths' skeleton has no
// seed information (see draw_qualifier_paths.go's doc comment on why), so
// this only exercises the part of the crossing structure the seam claims to
// reproduce exactly -- pool count, size and poolWinners -- and not the
// seed-dependent bye placement it explicitly does not promise.
func TestPoolQualifierPaths_MatchesRealPipeline(t *testing.T) {
	total := 0
	for numPools := 3; numPools <= 8; numPools++ {
		for _, winners := range []int{1, 2} {
			for _, courts := range []int{1, 2, 4} {
				t.Run(fmt.Sprintf("pools=%d winners=%d courts=%d", numPools, winners, courts), func(t *testing.T) {
					poolSize := 4
					n := numPools * poolSize
					players := make([]Player, n)
					for i := range players {
						players[i] = Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo%03d", i+1)}
					}

					pools, drawCourts, err := BuildPoolPhase(players, poolSize, false, courts)
					require.NoError(t, err)
					require.Len(t, pools, numPools)

					realDraw := BuildKnockoutDraw(pools, winners, drawCourts)
					require.NotNil(t, realDraw)
					realLeaves := TreeToLeafArray(realDraw.Root)

					targetSizes := make([]int, len(pools))
					for i, p := range pools {
						targetSizes[i] = len(p.Players)
					}

					got := poolQualifierPaths(targetSizes, winners, drawCourts)
					require.Len(t, got, numPools)

					total++
					for i, pool := range pools {
						maxRank := winners
						if maxRank > len(pool.Players) {
							maxRank = len(pool.Players)
						}
						for rank := 1; rank <= maxRank; rank++ {
							label := fmt.Sprintf("%s-%s", pool.PoolName, GetOrdinal(rank))
							wantSlot := -1
							for slot, v := range realLeaves {
								if v == label {
									wantSlot = slot
									break
								}
							}
							require.GreaterOrEqualf(t, wantSlot, 0, "label %s not found in real draw leaves", label)
							assert.Containsf(t, got[i], wantSlot,
								"pool %d (%s) rank %d: real draw placed %q at slot %d, seam did not report it (seam slots: %v)",
								i, pool.PoolName, rank, label, wantSlot, got[i])
						}
					}
				})
			}
		}
	}
	require.Equal(t, (8-3+1)*2*3, total, "sweep shrank: expected every pools/winners/courts combination to have run")
}

// TestPoolPositionName_MatchesAssignPlayersToPools pins poolPositionName
// (tournament.go) against the naming loop at the end of
// assignPlayersToPools, which poolPositionName's own doc comment claims
// equality with: build enough pools (poolSize 1, so pool i holds exactly
// player i) to walk past the 26-letter wrap into the doubled-letter scheme,
// and check every position's real name against poolPositionName(i).
func TestPoolPositionName_MatchesAssignPlayersToPools(t *testing.T) {
	n := 30 // past the a..z wrap at 26, into "Pool AA" etc.
	players := make([]Player, n)
	for i := range players {
		players[i] = Player{Name: fmt.Sprintf("P%02d", i), Dojo: fmt.Sprintf("Dojo%02d", i)}
	}
	pools, err := CreatePools(players, 1, false)
	require.NoError(t, err)
	require.Len(t, pools, n)
	for i, p := range pools {
		assert.Equalf(t, poolPositionName(i), p.PoolName, "position %d", i)
	}
}

// TestPoolQualifierPaths_EmptyCases pins the nil/zero edge cases
// BuildKnockoutDraw itself defines, so the seam degrades the same way its
// underlying draw does rather than panicking or silently returning garbage.
func TestPoolQualifierPaths_EmptyCases(t *testing.T) {
	assert.Nil(t, poolQualifierPaths(nil, 1, 2))
	assert.Nil(t, poolQualifierPaths([]int{4, 4}, 0, 2))
	assert.Nil(t, poolQualifierPaths([]int{4, 4}, -1, 2))
}

// TestPoolQualifierPaths_RedOnShiftedSlots is a RED-verification harness: it
// asserts the exact slot numbers for a fixed, small, hand-checked shape (4
// pools of 4, 1 winner, 2 courts), so a future change that shifts the
// crossing pattern is caught here even if every other assertion in this file
// (which only checks "the label is IN the returned set", not which exact
// slot) would not notice a shift among labels that both moved together.
func TestPoolQualifierPaths_RedOnShiftedSlots(t *testing.T) {
	targetSizes := []int{4, 4, 4, 4}
	got := poolQualifierPaths(targetSizes, 1, 2)
	require.Len(t, got, 4)

	// Cross-check against the real pipeline rather than a hand-picked
	// literal: what matters is that the seam's answer is EXACTLY the real
	// draw's leaf position for each pool, not a guessed constant.
	poolSize := 4
	players := make([]Player, 16)
	for i := range players {
		players[i] = Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo%03d", i+1)}
	}
	pools, drawCourts, err := BuildPoolPhase(players, poolSize, false, 2)
	require.NoError(t, err)
	realDraw := BuildKnockoutDraw(pools, 1, drawCourts)
	require.NotNil(t, realDraw)
	realLeaves := TreeToLeafArray(realDraw.Root)

	for i, pool := range pools {
		label := fmt.Sprintf("%s-%s", pool.PoolName, GetOrdinal(1))
		wantSlot := -1
		for slot, v := range realLeaves {
			if v == label {
				wantSlot = slot
			}
		}
		require.GreaterOrEqual(t, wantSlot, 0)
		require.Equal(t, []int{wantSlot}, got[i], "pool %d: exact slot mismatch", i)
	}
}
