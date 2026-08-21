package helper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The 33rd EKC 2025 (Leiden) Junior Individual Male knockout draw, decoded
// from the official PDF page (jim2025draw-04.png), replayed against
// BuildKnockoutDrawPerPoolFromAssignment. This is the reference case for
// bead bc-qual phase LP-3b: buildSmallCrossedCourtBlock (draw_perpool.go),
// which extends the per-pool crossed-qualifier draw (LP-3a,
// draw_ekc_2026_individual_test.go) to destination blocks too small for
// crossedBigBlockSlots (total <= 8, vs. that function's total >= 9 domain).
//
// 18 pools, 1 qualifier each, 4 courts. Two pools are oversized (4-person)
// and send a 2nd qualifier that crosses to their same-half neighbour court
// (the LP-3a crossing map: B->A, C->D): pool 8 (home court B) crosses to
// court A, pool 11 (home court C) crosses to court D. That gives courts A
// and D six occupants apiece (5 home 1sts + 1 crossed 2nd) and leaves B and
// C as clean 4-occupant home-only blocks.
//
// AssignPoolsToCourts(18, 4) front-loads its remainder to 5/5/4/4 (courts
// A(1-5) B(6-10) C(11-14) D(15-18)), which does NOT match the sheet's own
// court blocks -- A(1-5) B(6-9) C(10-13) D(14-18), sizes 5/4/4/5 -- so, like
// the 34th EKC Ladies Individual reference case, the real allocation is fed
// in explicitly via BuildKnockoutDrawPerPoolFromAssignment rather than
// derived. (A prior draft of this comment claimed AssignPoolsToCourts
// already matched the sheet; it does not, confirmed by running it: this is
// what "verify against the image before coding" caught.)
//
// Every match transcribed here was read directly off jim2025draw-04.png,
// including aka/shiro (top-row/red vs bottom-row/white) slot order.
func TestEKCJuniorIndividualMalePerPool(t *testing.T) {
	const numPools = 18
	const oversizedPools = 2
	const sheetOccupants = numPools + oversizedPools // 20

	pools := ekcPools(numPools)
	var assignment []int
	for court, n := range []int{5, 4, 4, 5} {
		for i := 0; i < n; i++ {
			assignment = append(assignment, court)
		}
	}
	require.Len(t, assignment, numPools)
	overrides := map[int]int{7: 2, 10: 2} // pool 8 and pool 11 (0-based index)

	draw := BuildKnockoutDrawPerPoolFromAssignment(pools, 1, overrides, assignment, 4)
	require.NotNil(t, draw, "buildSmallCrossedCourtBlock must build courts A and D (total=6, both below crossedBigBlockSlots' total>=9 floor)")
	require.Len(t, draw.Regions, 4)

	// Court A: 5 home 1sts (pools 1-5) plus pool 8's crossed-in 2nd. Splits
	// 3 (home-only: P1 byes, P2 v P3) + 3 (2 home + 1 crossed: P4 byes,
	// P8-2nd v P5). This is the ONE cell that diverges from the sheet's own
	// aka/shiro order -- the sheet prints "P8 #2 v P5 #1" (crossed first);
	// production keeps the established "crossed always the later slot"
	// convention (matching court D below and all four 2026 cells), so this
	// bout comes out "Pool 5-1st v Pool 8-2nd" instead. See
	// buildSmallCrossedCourtBlock's doc comment.
	wantA := [][]string{
		{"Pool 2-1st v Pool 3-1st", "Pool 5-1st v Pool 8-2nd"},
		{"Pool 1-1st v W", "Pool 4-1st v W"},
		{"W v W"},
	}
	assert.Equal(t, wantA, regionRounds(draw.Regions[0]), "shiaijo A")

	// Court B: 4 home 1sts (pools 6-9), no crossed occupant -- a clean,
	// UNCHANGED buildBlock block (2 round-1 matches, no bye at all, so only
	// 2 local rounds where A/D need 3).
	wantB := [][]string{
		{"Pool 6-1st v Pool 7-1st", "Pool 8-1st v Pool 9-1st"},
		{"W v W"},
	}
	assert.Equal(t, wantB, regionRounds(draw.Regions[1]), "shiaijo B")

	// Court C: 4 home 1sts (pools 10-13), same shape as B.
	wantC := [][]string{
		{"Pool 10-1st v Pool 11-1st", "Pool 12-1st v Pool 13-1st"},
		{"W v W"},
	}
	assert.Equal(t, wantC, regionRounds(draw.Regions[2]), "shiaijo C")

	// Court D: 5 home 1sts (pools 14-18) plus pool 11's crossed-in 2nd.
	// Splits 3 (2 home + 1 crossed: P14 byes, P15 v P11-2nd) + 3 (home-only:
	// P16 byes, P17 v P18). This cell matches the sheet's own aka/shiro
	// order exactly (crossed already prints second, "P15 #1 v P11 #2").
	wantD := [][]string{
		{"Pool 15-1st v Pool 11-2nd", "Pool 17-1st v Pool 18-1st"},
		{"Pool 14-1st v W", "Pool 16-1st v W"},
		{"W v W"},
	}
	assert.Equal(t, wantD, regionRounds(draw.Regions[3]), "shiaijo D")

	// Arithmetic sanity check, independent of the builder: every occupant
	// named exactly once across the four regions, total region bouts +
	// (2 semis + 1 final) = sheetOccupants - 1 (F1..F19 on the sheet).
	regionTables := [][][]string{wantA, wantB, wantC, wantD}
	var named []string
	var regionBouts int
	for _, region := range regionTables {
		for _, round := range region {
			regionBouts += len(round)
			for _, bout := range round {
				sides := strings.SplitN(bout, " v ", 2)
				require.Len(t, sides, 2, "bout %q must be \"left v right\"", bout)
				for _, side := range sides {
					if side != "W" {
						named = append(named, side)
					}
				}
			}
		}
	}
	assert.Len(t, named, sheetOccupants, "every occupant is named exactly once across the four regions")
	assert.Equal(t, sheetOccupants-1, regionBouts+3,
		"19 total bouts: F1..F19, matching the sheet's own final match number")

	// The crossed 2nds land ONLY in their destination court's region and
	// nowhere else (R4-crossing at pool granularity, the same property
	// assertEKC2026Crossing checks for the 2026 sheets).
	for court, want := range map[int]string{0: poolLabel(8, "2nd"), 3: poolLabel(11, "2nd")} {
		for i, region := range regionTables {
			holds := strings.Contains(strings.Join(flatten(region), " "), want)
			if i == court {
				assert.True(t, holds, "%s must be seated in shiaijo %s", want, CourtLabel(i))
			} else {
				assert.False(t, holds, "%s must not be seated in shiaijo %s", want, CourtLabel(i))
			}
		}
	}

	// Closing: semi A-v-B and C-v-D, final on B -- the same tail every
	// 4-court draw in this package prints (F17-F19 on the sheet). Read off
	// the last two rows of the whole-draw round table rather than the
	// per-court tables above: courts A/D (3 local rounds) and B/C (2 local
	// rounds) combine at mismatched depths -- the same class of alignment
	// draw_ekc_2026_individual_test.go's file comment documents for its own
	// Ladies courts B/C -- and the semis/final sit unambiguously above ALL
	// of that regardless of how the earlier rounds align.
	rounds := courtsByRound(draw)
	require.True(t, len(rounds) >= 2, "must have at least a semis round and a final round")
	assert.Equal(t, []string{"B", "C"}, rounds[len(rounds)-2], "semis: A-v-B on shiaijo B, C-v-D on shiaijo C")
	assert.Equal(t, []string{"B"}, rounds[len(rounds)-1], "final on shiaijo B")
}

// flatten joins a regionRounds-shaped table into one slice of bout strings,
// for a simple substring membership check.
func flatten(rounds [][]string) []string {
	var out []string
	for _, round := range rounds {
		out = append(out, round...)
	}
	return out
}
