package helper

import (
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
// extraQualifierOverrides does for a live competition.
func perPoolSmallDraw(t *testing.T, entrants, minSize, numCourts int) ([]Pool, *KnockoutDraw) {
	t.Helper()
	pools, _, err := BuildPoolPhase(makeUniquePlayers(entrants), minSize, false, numCourts)
	require.NoError(t, err)
	overrides := map[int]int{}
	for i, p := range pools {
		if len(p.Players) > minSize {
			overrides[i] = 2
		}
	}
	require.NotEmpty(t, overrides, "fixture must have at least one oversized pool")
	return pools, BuildKnockoutDrawPerPool(pools, 1, overrides, numCourts)
}

// leafLabels collects every occupant label under n, in tree order.
func leafLabels(n *Node) []string {
	if n == nil {
		return nil
	}
	if n.LeafNode {
		if n.LeafVal == "" {
			return nil
		}
		return []string{n.LeafVal}
	}
	return append(leafLabels(n.Left), leafLabels(n.Right)...)
}

// assertFightsInRoundOne is rule 3 made executable: label must appear in a
// round-1 bout with a real opponent on the other side, never as a bye and
// never rising from a later round.
func assertFightsInRoundOne(t *testing.T, region *Node, label string) {
	t.Helper()
	rounds := regionRounds(region)
	require.NotEmpty(t, rounds, "region has no rounds")
	for _, bout := range rounds[0] {
		sides := strings.SplitN(bout, " v ", 2)
		if len(sides) != 2 {
			continue
		}
		if sides[0] == label || sides[1] == label {
			other := sides[0]
			if sides[0] == label {
				other = sides[1]
			}
			assert.NotEmpty(t, other, "%s must have a real round-1 opponent", label)
			assert.NotEqual(t, "W", other, "%s must fight in round 1, not a riser", label)
			return
		}
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
	pools, draw := perPoolSmallDraw(t, 10, 3, 1)
	require.NotNil(t, draw, "a single-shiaijo oversized field must produce a draw")

	var oversized string
	for _, p := range pools {
		if len(p.Players) > 3 {
			oversized = p.PoolName
		}
	}
	first, second := oversized+"-1st", oversized+"-2nd"

	all := leafLabels(draw.Root)
	assert.Contains(t, all, second, "the oversized pool's 2nd must be seated")
	assert.Len(t, all, len(pools)+1, "every pool's 1st plus the one extra qualifier")

	assertFightsInRoundOne(t, draw.Root, second)

	left, right := leafLabels(draw.Root.Left), leafLabels(draw.Root.Right)
	assert.Truef(t,
		(sliceHas(left, first) && sliceHas(right, second)) ||
			(sliceHas(right, first) && sliceHas(left, second)),
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
			pools, draw := perPoolSmallDraw(t, tc.entrants, 3, tc.courts)
			require.NotNilf(t, draw, "%d entrants on %d shiaijo must produce a draw", tc.entrants, tc.courts)

			seated := leafLabels(draw.Root)
			oversized := 0
			for _, p := range pools {
				if len(p.Players) > 3 {
					oversized++
					assert.Containsf(t, seated, p.PoolName+"-2nd", "%s is oversized and must send its 2nd", p.PoolName)
				}
			}
			assert.Len(t, seated, len(pools)+oversized)
			for _, region := range draw.Regions {
				for _, l := range leafLabels(region) {
					if strings.HasSuffix(l, "-2nd") {
						assertFightsInRoundOne(t, region, l)
					}
				}
			}
		})
	}
}

// TestPerPoolTwoOversizedPoolsSharingACourt pins the third refusal: both
// oversized pools live on the same shiaijo, so both extra qualifiers cross to
// the same destination. LP-3a allowed one crossed occupant per destination
// because one is all the sheets show; two is not ambiguous, only unwitnessed,
// and refusing it stranded every field of this shape.
func TestPerPoolTwoOversizedPoolsSharingACourt(t *testing.T) {
	t.Parallel()
	pools, draw := perPoolSmallDraw(t, 11, 3, 2)
	require.NotNil(t, draw, "two oversized pools on one shiaijo must still produce a draw")

	seated := leafLabels(draw.Root)
	for _, p := range pools {
		if len(p.Players) > 3 {
			assert.Contains(t, seated, p.PoolName+"-2nd")
		}
	}
	for _, region := range draw.Regions {
		for _, l := range leafLabels(region) {
			if strings.HasSuffix(l, "-2nd") {
				assertFightsInRoundOne(t, region, l)
			}
		}
	}
}

func sliceHas(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
