package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seededDraw runs the REAL create-pools pipeline for numPools pools of four on
// numCourts shiaijo, with the first numSeeds entrants seeded 1..numSeeds, and
// returns the pools and the draw built from them. Nothing here re-implements
// placement: PoolSeeding decides which pool each seed lands in and
// BuildKnockoutDraw decides where in the bracket its winner sits, exactly as
// cmd/create-pools does.
func seededDraw(t *testing.T, numPools, numSeeds, numCourts int) ([]Pool, *KnockoutDraw) {
	t.Helper()
	players := make([]Player, 0, numPools*4)
	for i := 0; i < numPools*4; i++ {
		p := Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo %03d", i+1)}
		if i < numSeeds {
			p.Seed = i + 1
		}
		players = append(players, p)
	}
	counted := PoolCount(len(players), 4, false)
	require.Equal(t, numPools, counted)

	players = PoolSeeding(players, counted, numCourts)
	pools, err := CreatePools(players, 4, false)
	require.NoError(t, err)
	pools = ReorderPoolsForCourts(pools, numCourts)

	courts := EffectiveDrawCourts(len(pools), numCourts)
	draw := BuildKnockoutDraw(pools, 2, courts)
	require.NotNil(t, draw)
	return pools, draw
}

// TestSeedPlacementWarningsSurplusRanks is R2's last bullet: more seeds than
// pools is NOT an error. The surplus ranks are ignored, off the bottom (D7
// protects the top seed most), and the operator is told which.
func TestSeedPlacementWarningsSurplusRanks(t *testing.T) {
	pools, draw := seededDraw(t, 3, 4, 2)

	warnings := SeedPlacementWarnings(draw, pools, 2)
	require.NotEmpty(t, warnings, "4 seeds over 3 pools must warn")
	assert.Contains(t, warnings[0], "Seed 4 ignored")
	assert.Contains(t, warnings[0], "two seeds must never share a pool")
	assert.Contains(t, warnings[0], "3 pools for 4 seeds")
	assert.Contains(t, warnings[0], "The draw used seeds 1, 2 and 3.")
	for _, w := range warnings {
		assert.NotContains(t, strings.ToLower(w), "error")
		assert.NotContains(t, strings.ToLower(w), "cannot draw")
	}
}

// TestSeedPlacementWarningsRelaxedQuarter is D7's ladder in action: a
// constraint that cannot be honoured gives way and the operator is told, and it
// is never an error.
//
// The worked example is 4 seeds and 5 pools on ONE shiaijo. PoolSeeding spreads
// seeds over SHIAIJO, so with only one to spread over it puts seed 4 in the
// pool next to seed 1's, and both land in the draw's first block; the draw then
// cannot give them separate halves or separate quarters and says so. The same
// competition on TWO shiaijo now satisfies both constraints and warns about
// nothing -- see TestSeedPlacementWarningsSilentWhenSatisfiable -- because the
// pool set is subdivided into four blocks whatever the shiaijo count, so four
// distinct quarters exist to place four seeds in.
func TestSeedPlacementWarningsRelaxedQuarter(t *testing.T) {
	pools, draw := seededDraw(t, 5, 4, 1)

	warnings := SeedPlacementWarnings(draw, pools, 1)
	require.Len(t, warnings, 2, "every seed has its own pool, so only halves and quarters give way: %v", warnings)
	assert.Contains(t, warnings[0], "could not be split into halves")
	assert.Contains(t, warnings[1], "own quarter of the draw")
	assert.Contains(t, warnings[1], "seeds 1 and 4")
	assert.Contains(t, warnings[1], "The draw was made anyway.")
}

// TestSeedPlacementWarningsSilentWhenSatisfiable pins the other half of the
// rule: a configuration the seeding rules CAN satisfy warns about nothing, and
// neither does a competition with no seeds at all ("Zero seeds MUST be a
// normal, warning-free configuration", D6).
func TestSeedPlacementWarningsSilentWhenSatisfiable(t *testing.T) {
	cases := []struct {
		name                          string
		numPools, numSeeds, numCourts int
	}{
		{"4 seeds on 4 shiaijo", 8, 4, 4},
		{"4 seeds on 2 shiaijo", 8, 4, 2},
		// D7's old worked example. Two shiaijo now carry four blocks, so all
		// four seeds get a quarter each and nothing gives way.
		{"4 seeds on 2 shiaijo over 5 pools", 5, 4, 2},
		{"2 seeds on 2 shiaijo", 6, 2, 2},
		{"1 seed", 6, 1, 2},
		{"no seeds", 6, 0, 2},
		{"no seeds on one shiaijo", 5, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pools, draw := seededDraw(t, tc.numPools, tc.numSeeds, tc.numCourts)
			assert.Empty(t, SeedPlacementWarnings(draw, pools, tc.numCourts))
		})
	}
}

// TestSeedPlacementWarningsNilInputs covers the callers that ask before there
// is anything to answer with: no draw, no pools. Neither is an error and
// neither warns.
func TestSeedPlacementWarningsNilInputs(t *testing.T) {
	pools, draw := seededDraw(t, 4, 2, 2)
	assert.Nil(t, SeedPlacementWarnings(nil, pools, 2))
	assert.Nil(t, SeedPlacementWarnings(&KnockoutDraw{}, pools, 2))
	assert.Nil(t, SeedPlacementWarnings(draw, nil, 2))
}

// TestSeedPlacementWarningsReportsSharedShiaijoOnlyWhenAvoidable is the
// noise guard on D7's third constraint. Two seeded pools per shiaijo is the
// CORRECT outcome on two shiaijo and four seeds (D6 says so outright), so it
// must not be reported as a relaxation; only a shiaijo count that could have
// given every seed its own is worth a warning.
func TestSeedPlacementWarningsReportsSharedShiaijoOnlyWhenAvoidable(t *testing.T) {
	pools, draw := seededDraw(t, 8, 4, 2)
	for _, w := range SeedPlacementWarnings(draw, pools, 2) {
		assert.NotContains(t, w, "own shiaijo",
			"4 seeds on 2 shiaijo share shiaijo by design, not by relaxation")
	}
}

func TestRankList(t *testing.T) {
	assert.Equal(t, "none", RankList(nil))
	assert.Equal(t, "1", RankList([]int{1}))
	assert.Equal(t, "1 and 2", RankList([]int{1, 2}))
	assert.Equal(t, "1, 2 and 3", RankList([]int{1, 2, 3}))
}
