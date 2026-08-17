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
