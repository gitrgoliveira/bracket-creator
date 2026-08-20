package helper

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Small-field larger-pools shapes (bc-qual LP-3d).
//
// LP-3a built the "oversized send +1" crossing from the EKC individual
// sheets, whose courts hold eight to thirteen pools each, and refused
// everything it could not read off those sheets. The refusals were correct as
// scope discipline and wrong as a product: the operator ruling is that the
// mode must work for the fields clubs actually run, so this file pins the
// three shapes that used to be refused. Rule 3 is what constrains the
// extension -- a crossed 2nd takes a round-1 FIGHTING slot, never a bye --
// and every test here asserts it directly rather than trusting the layout.
//
// The evidenced sheet replays (draw_ekc_2026_individual_test.go,
// draw_ekc_senior_test.go) are the regression guard for the other direction:
// they must keep printing exactly what they printed before, which is why the
// new path is a FALLBACK reached only where the old one returned nil.

// perPoolSmallDraw runs the real pool phase and marks every oversized pool as
// sending one extra qualifier, exactly as the engine's
// extraQualifierOverrides does for a live competition. It returns the
// oversized pools it identified so no caller has to restate the rule --
// poolIsOversized (fill_bracket.go) is the package's owner of it.
func perPoolSmallDraw(t *testing.T, entrants, minSize, numCourts int) (pools []Pool, oversized []Pool, draw *KnockoutDraw) {
	t.Helper()
	pools, _, err := BuildPoolPhase(makeUniquePlayers(entrants), minSize, false, numCourts)
	require.NoError(t, err)
	overrides := map[int]int{}
	for i, p := range pools {
		if poolIsOversized(p, minSize) {
			overrides[i] = 2
			oversized = append(oversized, p)
		}
	}
	require.NotEmpty(t, oversized, "fixture must have at least one oversized pool")
	return pools, oversized, BuildKnockoutDrawPerPool(pools, 1, overrides, numCourts)
}

// assertEveryExtraFightsInRoundOne sweeps a whole draw for rule 3: every
// seated extra qualifier ("-2nd") must fight in round 1 of its own shiaijo's
// block.
func assertEveryExtraFightsInRoundOne(t *testing.T, draw *KnockoutDraw) {
	t.Helper()
	for _, region := range draw.Regions {
		rounds := regionRounds(region)
		for _, label := range TreeLeafLabels(region) {
			if strings.HasSuffix(label, "-2nd") {
				assertFightsInRoundOne(t, rounds, label)
			}
		}
	}
}

// assertFightsInRoundOne is rule 3 made executable: label must appear in a
// round-1 bout with a real opponent on the other side, never as a bye and
// never rising from a later round.
//
// rounds comes from regionRounds, whose bouts read "left v right" with "W"
// for a winner slot. Deliberately an INDEPENDENT oracle of production's
// crossedFightInRoundOne rather than a call to it -- a post-condition that
// validates itself pins nothing.
func assertFightsInRoundOne(t *testing.T, rounds [][]string, label string) {
	t.Helper()
	require.NotEmpty(t, rounds, "region has no rounds")
	for _, bout := range rounds[0] {
		left, right, ok := strings.Cut(bout, " v ")
		if !ok || (left != label && right != label) {
			continue
		}
		other := right
		if right == label {
			other = left
		}
		assert.NotEmptyf(t, other, "%s must have a real round-1 opponent", label)
		assert.NotEqualf(t, "W", other, "%s must fight in round 1, not a riser", label)
		return
	}
	t.Fatalf("rule 3: %s never appears in a round-1 bout; rounds=%v", label, rounds)
}

// TestPerPoolSingleShiaijoSeatsTheExtraQualifier is the shape a club event
// actually runs: one shiaijo, ten entrants, three pools of which one is
// oversized. There is no neighbouring court to cross to, so the extra
// qualifier stays in the only block there is -- seated in the OPPOSITE HALF
// from its own pool's winner, which is the same separation "Fit the knockout"
// applies at one shiaijo, and the reason the two can never meet before the
// final.
func TestPerPoolSingleShiaijoSeatsTheExtraQualifier(t *testing.T) {
	t.Parallel()
	pools, oversized, draw := perPoolSmallDraw(t, 10, 3, 1)
	require.NotNil(t, draw, "a single-shiaijo oversized field must produce a draw")
	require.Len(t, oversized, 1, "10 entrants at minimum 3 forms exactly one oversized pool")

	first, second := oversized[0].PoolName+"-1st", oversized[0].PoolName+"-2nd"

	all := TreeLeafLabels(draw.Root)
	assert.Contains(t, all, second, "the oversized pool's 2nd must be seated")
	assert.Len(t, all, len(pools)+1, "every pool's 1st plus the one extra qualifier")

	assertEveryExtraFightsInRoundOne(t, draw)

	left, right := TreeLeafLabels(draw.Root.Left), TreeLeafLabels(draw.Root.Right)
	assert.Truef(t,
		(slices.Contains(left, first) && slices.Contains(right, second)) ||
			(slices.Contains(right, first) && slices.Contains(left, second)),
		"the extra qualifier must sit in the opposite half from its own pool's winner; left=%v right=%v", left, right)
}

// TestPerPoolSmallReceivingCourtSeatsTheExtraQualifier covers the court that
// RECEIVES a crossing while holding only one or two pools of its own: the
// role tables LP-3a transcribed from the sheets start at three occupants in
// the crossed portion, so every field this size was refused even though rule
// 3 is satisfiable -- with one home pool the crossed 2nd simply fights it.
func TestPerPoolSmallReceivingCourtSeatsTheExtraQualifier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name             string
		entrants, courts int
	}{
		{"one pool per court", 13, 4},
		{"two pools per court", 25, 4},
		{"three pools, two courts", 10, 2},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pools, oversized, draw := perPoolSmallDraw(t, tc.entrants, 3, tc.courts)
			require.NotNilf(t, draw, "%d entrants on %d shiaijo must produce a draw", tc.entrants, tc.courts)

			seated := TreeLeafLabels(draw.Root)
			for _, p := range oversized {
				assert.Containsf(t, seated, p.PoolName+"-2nd", "%s is oversized and must send its 2nd", p.PoolName)
			}
			assert.Len(t, seated, len(pools)+len(oversized))
			assertEveryExtraFightsInRoundOne(t, draw)
		})
	}
}

// TestCrossedFightInRoundOneRejectsAByedExtra pins the rule-3 post-condition
// itself, directly.
//
// It has to be tested directly because no roster reaches it: byePrecedenceLess
// already ranks a home 1st ahead of a crossed 2nd for the bye, so every layout
// the builders currently produce satisfies rule 3 on its own and the check
// never fires end to end. Deleting the check therefore breaks no other test --
// verified by neutering it, whereupon the whole suite stays green. That makes
// it a safety net for a rule stated in prose across several hand-transcribed
// role tables, and an unpinned safety net is one a later refactor can silently
// remove, so its logic is pinned here instead of nowhere.
func TestCrossedFightInRoundOneRejectsAByedExtra(t *testing.T) {
	t.Parallel()

	crossed := []drawOccupant{{label: "Pool C-2nd", pool: 2, rank: 2}}

	// The extra qualifier fights a named opponent in round 1: rule 3 holds.
	ok := BuildSlotTree([]string{"Pool A-1st", "Pool B-1st", "Pool C-2nd", "Pool D-1st"})
	assert.True(t, crossedFightInRoundOne(ok, crossed),
		"a crossed 2nd paired against a named opponent in round 1 satisfies rule 3")

	// The same occupants, but the extra qualifier holds the BYE slot: the
	// layout rule 3 exists to forbid.
	byed := BuildSlotTree([]string{"Pool C-2nd", "", "Pool A-1st", "Pool B-1st"})
	assert.False(t, crossedFightInRoundOne(byed, crossed),
		"a byed crossed 2nd must be rejected: rule 3 gives it a fighting slot, never a bye")

	// And when it is not seated at all.
	absent := BuildSlotTree([]string{"Pool A-1st", "Pool B-1st"})
	assert.False(t, crossedFightInRoundOne(absent, crossed),
		"an unseated crossed 2nd cannot satisfy a rule about where it fights")
}

// TestPerPoolCrossingMapOnlyAnswersLegalShiaijoCounts guards the boundary of
// LP-3d's widening.
//
// A COMPETITION's shiaijo allocation is always a power of two (R9,
// ValidateShiaijoCount): a venue may have 3 or 5 shiaijo, but it gives one
// competition 1, 2 or 4 of them and never all 3. So the crossing map only has
// to answer for 1, 2, 4, 8 and 16, and at every one of those court^1 is a
// real court.
//
// This test exists because the first cut of LP-3d invented an answer for odd
// counts above one (the unpaired last court crossing downwards). That was
// dead code for a shape a competition cannot hold, and inventing a neighbour
// for an illegal allocation would draw it as though it were legal instead of
// refusing. An out-of-range index is the correct answer: buildPerPoolDraw
// treats it as out of scope.
func TestPerPoolCrossingMapOnlyAnswersLegalShiaijoCounts(t *testing.T) {
	t.Parallel()

	for _, courts := range validShiaijoCounts {
		for court := 0; court < courts; court++ {
			dest := crossNeighbourCourt(court, courts)
			require.GreaterOrEqualf(t, dest, 0, "%d shiaijo, court %d: every legal allocation has a destination", courts, court)
			require.Lessf(t, dest, courts, "%d shiaijo, court %d: the destination must be a real court", courts, court)
			if courts == 1 {
				assert.Equal(t, 0, dest, "a single shiaijo crosses into its own block, separated by half")
			} else {
				assert.Equal(t, court^1, dest, "%d shiaijo: the evidenced same-half neighbour", courts)
				assert.NotEqual(t, court, dest, "a multi-shiaijo crossing must leave its own court")
			}
		}
	}

	// An ILLEGAL allocation (3 is a legal venue but never one competition's
	// share) must not be answered with an invented neighbour: the index falls
	// outside the court range, which the builder refuses.
	assert.GreaterOrEqual(t, crossNeighbourCourt(2, 3), 3,
		"an out-of-range index is what makes buildPerPoolDraw decline an illegal allocation")
}

// TestPerPoolTwoOversizedPoolsSharingACourt pins the third refusal: both
// oversized pools live on the same shiaijo, so both extra qualifiers cross to
// the same destination. LP-3a allowed one crossed occupant per destination
// because one is all the sheets show; two is not ambiguous, only unwitnessed,
// and refusing it stranded every field of this shape.
func TestPerPoolTwoOversizedPoolsSharingACourt(t *testing.T) {
	t.Parallel()
	_, oversized, draw := perPoolSmallDraw(t, 11, 3, 2)
	require.NotNil(t, draw, "two oversized pools on one shiaijo must still produce a draw")

	seated := TreeLeafLabels(draw.Root)
	for _, p := range oversized {
		assert.Contains(t, seated, p.PoolName+"-2nd")
	}
	assertEveryExtraFightsInRoundOne(t, draw)
}
