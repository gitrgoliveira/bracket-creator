package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A court count is bounded ABOVE as well as below, at every entry point that
// sizes an allocation from it.
//
// Flagged by CodeQL as "slice memory allocation with excessive size value" on
// nine sites in draw.go and one in excel.go: each is make([]T, numCourts) or
// make([]T, numBlocks), and numBlocks is numCourts for any count above 2. Every
// caller in this repo validates first (ValidateCourts caps at MaxCourts,
// validateCourtLabels caps the app side), so nothing was reachable -- but the
// clamps themselves only had a floor, so the bound lived entirely in callers
// and an exported entry point could be handed any int.
//
// The cap is not arbitrary: CourtLabel indexes a 26-character string, so a
// count past MaxCourts is an out-of-range PANIC on the way to the allocation.
// That makes "how many courts" a range, not a minimum.
func TestCourtCountIsBoundedAboveNotJustBelow(t *testing.T) {
	t.Parallel()

	huge := 1 << 20

	t.Run("clampCourts caps at the A-Z label cap", func(t *testing.T) {
		assert.Equal(t, 1, clampCourts(0), "floor")
		assert.Equal(t, 1, clampCourts(-5), "floor")
		assert.Equal(t, 4, clampCourts(4), "a legal count is untouched")
		assert.Equal(t, MaxCourts, clampCourts(MaxCourts), "the cap itself is legal")
		assert.Equal(t, MaxCourts, clampCourts(huge), "cap")
	})

	t.Run("EffectiveDrawCourts caps before sizing the draw", func(t *testing.T) {
		// numPools is large enough that the step-down branch cannot mask the cap.
		assert.Equal(t, MaxCourts, EffectiveDrawCourts(huge, huge))
		assert.Equal(t, 4, EffectiveDrawCourts(8, 4), "a legal count is untouched")
	})

	t.Run("every court name the cap admits is labellable", func(t *testing.T) {
		// The reason for the cap: one past it panics rather than over-allocating.
		for i := range clampCourts(huge) {
			assert.NotEmpty(t, CourtLabel(i))
		}
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
		assert.LessOrEqual(t, len(draw.Regions), MaxCourts)

		// Playoffs: same bound, different entry.
		tree := CreateBalancedTree([]string{"A", "B", "C", "D"})
		pd := NewPlayoffDraw(tree, huge)
		require.NotNil(t, pd)
		assert.LessOrEqual(t, len(pd.Regions), MaxCourts)
	})
}
