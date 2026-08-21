package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bout above the regions belongs to no single shiaijo, and takes the
// CENTRE-MOST court it spans rather than the leftmost.
//
// The leftmost answer was not a ruling, it was the arithmetic of
// CourtForLeafSlot falling out: every bout took the region owning its first
// leaf, so the final landed on A whatever the allocation. A hall runs its
// closing bouts on the middle shiaijo, where the crowd gathers. The centre is
// measured across the WHOLE allocation, not the bout's own span, which is what
// makes the first half's INNER court the answer (B, not A, on four shiaijo).
func TestCourtForSpanPutsTheClosingBoutsInTheMiddle(t *testing.T) {
	t.Parallel()

	// One region per court, four slots each: the shape a real 4-shiaijo draw has.
	spans := func(n, per int) [][2]int {
		out := make([][2]int, n)
		for i := range out {
			out[i] = [2]int{i * per, (i + 1) * per}
		}
		return out
	}

	t.Run("four shiaijo: semi-finals on B and C, final on B", func(t *testing.T) {
		sp := spans(4, 4)
		assert.Equal(t, 1, CourtForSpan(sp, 0, 8), "semi-final over A+B runs on B")
		assert.Equal(t, 2, CourtForSpan(sp, 8, 8), "semi-final over C+D runs on C")
		assert.Equal(t, 1, CourtForSpan(sp, 0, 16), "the final ties B against C and takes the lower")
	})

	t.Run("two shiaijo: the final stays on A", func(t *testing.T) {
		sp := spans(2, 4)
		assert.Equal(t, 0, CourtForSpan(sp, 0, 8), "A and B are equidistant, so the lower wins")
	})

	t.Run("eight shiaijo: semi-finals on D and E, final on D", func(t *testing.T) {
		sp := spans(8, 4)
		assert.Equal(t, 3, CourtForSpan(sp, 0, 16))
		assert.Equal(t, 4, CourtForSpan(sp, 16, 16))
		assert.Equal(t, 3, CourtForSpan(sp, 0, 32))
	})

	t.Run("a bout inside one region keeps that region's court", func(t *testing.T) {
		sp := spans(4, 4)
		for court := range 4 {
			at := court * 4
			assert.Equalf(t, court, CourtForSpan(sp, at, 1), "leaf on shiaijo %d", court)
			assert.Equalf(t, court, CourtForSpan(sp, at, 2), "round-1 bout on shiaijo %d", court)
			assert.Equalf(t, court, CourtForSpan(sp, at, 4), "region root on shiaijo %d", court)
		}
	})

	t.Run("one shiaijo puts everything on A", func(t *testing.T) {
		sp := spans(1, 8)
		assert.Equal(t, 0, CourtForSpan(sp, 0, 8))
		assert.Equal(t, 0, CourtForSpan(sp, 0, 1))
	})

	t.Run("a zero width falls back to the single-slot answer", func(t *testing.T) {
		sp := spans(4, 4)
		assert.Equal(t, CourtForLeafSlot(sp, 9), CourtForSpan(sp, 9, 0))
	})
}

// The draw's own tree must agree with the rule, at every legal shiaijo count:
// the root is the final and belongs on the centre-most court.
func TestNodeCourtsPutsTheFinalInTheMiddle(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ courts, wantFinal int }{
		{courts: 1, wantFinal: 0},
		{courts: 2, wantFinal: 0},
		{courts: 4, wantFinal: 1},
		{courts: 8, wantFinal: 3},
	} {
		t.Run(fmt.Sprintf("%d shiaijo", tc.courts), func(t *testing.T) {
			pools, drawCourts, err := BuildPoolPhase(drawGoldenRoster(tc.courts*2), drawGoldenPoolSize, true, tc.courts)
			require.NoError(t, err)
			draw := BuildKnockoutDraw(pools, 2, drawCourts)
			require.NotNil(t, draw)
			courts := draw.NodeCourts()
			require.NotNil(t, courts)
			assert.Equal(t, tc.wantFinal, courts[draw.Root],
				"the final runs on the centre-most shiaijo of %d", tc.courts)
		})
	}
}
