package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A court count is bounded ABOVE as well as below, at every entry point that
// sizes an allocation from it.
//
// Flagged by CodeQL as "slice memory allocation with excessive size value".
// Nothing was reachable -- every caller validates first -- but the clamp itself
// only had a floor, so the bound lived entirely in the callers and an exported
// entry point could be handed any int. See clampCourts for why the ceiling is
// the labelling cap.
func TestCourtCountIsBoundedAboveNotJustBelow(t *testing.T) {
	t.Parallel()

	huge := 1 << 20

	t.Run("clampCourts bounds the range at both ends", func(t *testing.T) {
		assert.Equal(t, 1, clampCourts(0))
		assert.Equal(t, 1, clampCourts(-5))
		assert.Equal(t, 4, clampCourts(4), "a legal count is untouched")
		assert.Equal(t, MaxCourts, clampCourts(MaxCourts), "the cap itself is legal")
		assert.Equal(t, MaxCourts, clampCourts(huge))
	})

	t.Run("EffectiveDrawCourts caps before sizing the draw", func(t *testing.T) {
		// numPools is large enough that the step-down branch cannot mask the cap.
		assert.Equal(t, MaxCourts, EffectiveDrawCourts(huge, huge))
	})

	t.Run("one past the cap is a panic, not merely a big allocation", func(t *testing.T) {
		assert.Panics(t, func() { _ = CourtLabel(MaxCourts) },
			"if this stops panicking the cap can be raised, but not before")
	})

	t.Run("the exported draw entry points bound their own input", func(t *testing.T) {
		pools, err := CreatePools(drawGoldenRoster(2), drawGoldenPoolSize, true)
		require.NoError(t, err)
		require.NotEmpty(t, pools)
		assignment := make([]int, len(pools))

		// Pool-fed: reachable without BuildKnockoutDraw's clamp in front of it.
		draw := BuildKnockoutDrawFromAssignment(pools, 1, assignment, huge)
		require.NotNil(t, draw)
		assert.LessOrEqual(t, draw.NumCourts(), MaxCourts)

		// Playoffs: same bound, different entry.
		pd := NewPlayoffDraw(CreateBalancedTree([]string{"A", "B", "C", "D"}), huge)
		require.NotNil(t, pd)
		assert.LessOrEqual(t, len(pd.Regions), MaxCourts)
	})
}

// R9's power-of-two step-down applies only where a bracket gets merged. A
// league or Swiss competition has no bracket (ValidateCompetitionShiaijoCount
// exempts it), so stepping ITS pool phase down to a power of two idled shiaijo
// the operator had allocated: 3 pools on 4 shiaijo ran on 2.
func TestPoolCourtsStepDownIsScopedToBracketFormats(t *testing.T) {
	t.Parallel()

	cases := []struct{ pools, courts, bracket, noBracket int }{
		{pools: 3, courts: 4, bracket: 2, noBracket: 3},
		{pools: 5, courts: 8, bracket: 4, noBracket: 5},
		{pools: 6, courts: 8, bracket: 4, noBracket: 6},
		{pools: 7, courts: 8, bracket: 4, noBracket: 7},
		// Where the request already fits, both agree.
		{pools: 8, courts: 4, bracket: 4, noBracket: 4},
		{pools: 4, courts: 4, bracket: 4, noBracket: 4},
	}
	for _, c := range cases {
		assert.Equalf(t, c.bracket, EffectiveDrawCourts(c.pools, c.courts),
			"bracket: %d pools on %d shiaijo", c.pools, c.courts)
		assert.Equalf(t, c.noBracket, EffectivePoolCourts(c.pools, c.courts),
			"no bracket: %d pools on %d shiaijo must not lose a shiaijo to R9", c.pools, c.courts)
	}

	// The bound still applies to both.
	assert.Equal(t, MaxCourts, EffectivePoolCourts(1<<20, 1<<20))
	assert.Equal(t, 1, EffectivePoolCourts(0, 0))

	// And the allocation an operator actually sees: every shiaijo the league was
	// given a pool for gets one.
	assign, err := AssignPoolsToCourts(3, EffectivePoolCourts(3, 4))
	require.NoError(t, err)
	used := map[int]bool{}
	for _, c := range assign {
		used[c] = true
	}
	assert.Len(t, used, 3, "3 pools on 4 shiaijo must run on 3 of them, not 2")
}
