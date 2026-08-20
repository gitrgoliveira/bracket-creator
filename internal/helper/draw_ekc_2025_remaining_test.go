package helper

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The three 33rd EKC 2025 (Leiden) events the suite did not yet replay:
// Junior Team, Ladies Team and Junior Individual Female. With
// draw_ekc_2025_individual_test.go (Ladies + Men Individual),
// draw_ekc_2025_junior_perpool_test.go (Junior Individual Male) and
// TestEKC2025MenTeamByes (Men Team), all seven 2025 events are now pinned,
// matching the coverage draw_ekc_test.go and draw_ekc_senior_test.go already
// give the seven 2026 events.
//
// Decoded from the official PDF's draw pages (pages 6, 10 and 2), which carry
// no text layer and were read as rendered images at 200 dpi, bout by bout,
// including the shiaijo letter printed on every bout.
//
// The three land in three different places against this package, which is why
// they are worth having together:
//
//   - Junior Team reproduces EXACTLY, pairings, sides and shiaijo.
//   - Ladies Team agrees on everything that decides WHERE a competitor fights
//     (pool-to-shiaijo allocation, which 2nd crosses to which shiaijo, the
//     occupants of every block) and disagrees only on how two 5-occupant
//     blocks arrange those occupants internally.
//   - Junior Individual Female crosses its one oversized pool's 2nd to a
//     shiaijo this package's individual crossing rule does not send it to, and
//     the resulting shape is one BuildKnockoutDrawPerPool declines outright.
//
// The last two are recorded, not accommodated. A one-sheet divergence is not
// evidence enough to change a rule that four other sheets corroborate, and
// the operator ruling on this specific deviation was to keep the crossing rule
// uniform. What these tests buy is that the divergence is stated, reproducible
// and attached to its evidence, instead of being rediscovered from scratch.

// occupantsOf lists every pool-qualifier label a region holds, sorted, so a
// block's MEMBERSHIP can be asserted independently of how the block arranges
// its members internally. "W" (a winner placeholder) is not an occupant.
func occupantsOf(region *Node) []string {
	var out []string
	for _, slot := range TreeToLeafArray(region) {
		if slot != "" {
			out = append(out, slot)
		}
	}
	sort.Strings(out)
	return out
}

// TestEKC2025JuniorTeam replays the 33rd EKC 2025 Junior Team draw in full:
// 6 pools, 2 qualifiers each, 4 shiaijo.
//
// AssignPoolsToCourts(6, 4) gives A{1,2} B{3,4} C{5} D{6}, which is the
// sheet's own allocation, and every 2nd crosses on the TEAM partner map
// (A<->C, B<->D): pools 1 and 2 send their 2nds to C, pools 3 and 4 to D,
// pool 5 to A and pool 6 to B.
//
// Reproduces bout for bout, side for side, and shiaijo for shiaijo, including
// the semifinals on B and C and the final on B. This is the second independent
// instance of the same team-crossing rule TestEKCJuniorTeam pins on the 2026
// sheet, a year apart and at a different pool count.
func TestEKC2025JuniorTeam(t *testing.T) {
	t.Parallel()

	assignment, err := AssignPoolsToCourts(6, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 0, 1, 1, 2, 3}, assignment,
		"the sheet's shiaijo blocks A(1,2) B(3,4) C(5) D(6)")

	draw := BuildKnockoutDraw(ekcPools(6), 2, 4)
	require.NotNil(t, draw)
	require.Len(t, draw.Regions, 4)

	assert.Equal(t, [][]string{
		{"A", "B", "C", "D"}, // F1, F3, F5, F7
		{"A", "B", "C", "D"}, // F2, F4, F6, F8
		{"B", "C"},           // F9, F10
		{"B"},                // F11
	}, courtsByRound(draw), "the shiaijo printed on every bout of the Junior Team sheet")

	assert.Equal(t, [][]string{
		{"Pool 2-1st v Pool 5-2nd"}, // F1
		{"Pool 1-1st v W"},          // F2
	}, regionRounds(draw.Regions[0]), "shiaijo A")

	assert.Equal(t, [][]string{
		{"Pool 4-1st v Pool 6-2nd"}, // F3
		{"Pool 3-1st v W"},          // F4
	}, regionRounds(draw.Regions[1]), "shiaijo B")

	assert.Equal(t, [][]string{
		{"Pool 1-2nd v Pool 2-2nd"}, // F5
		{"Pool 5-1st v W"},          // F6
	}, regionRounds(draw.Regions[2]), "shiaijo C")

	assert.Equal(t, [][]string{
		{"Pool 3-2nd v Pool 4-2nd"}, // F7
		{"Pool 6-1st v W"},          // F8
	}, regionRounds(draw.Regions[3]), "shiaijo D")
}

// TestEKC2025LadiesTeamAllocationAndCrossing replays the 33rd EKC 2025 Ladies
// Team draw: 9 pools, 2 qualifiers each, 4 shiaijo, blocks A{1,2} B{3,4}
// C{5,6} D{7,8,9}.
//
// Two things about this sheet are worth pinning and are pinned here.
//
// First, the allocation is BACK-loaded: the extra pool goes to D, not to A.
// AssignPoolsToCourts front-loads its remainder and would give A{1,2,3}
// B{4,5} C{6,7} D{8,9}, so the sheet is replayed through
// BuildKnockoutDrawFromAssignment with its own allocation, the same reason
// BuildKnockoutDrawPerPoolFromAssignment exists for the 34th EKC Ladies
// Individual sheet.
//
// Second, given that allocation the crossing is exactly this package's team
// map: pools 1,2 (A) send their 2nds to C, pools 3,4 (B) to D, pools 5,6 (C)
// to A, pools 7,8,9 (D) to B. That is what decides which shiaijo a competitor
// fights on, and it is asserted here as the membership of each block.
//
// What this test deliberately does NOT assert is the internal arrangement of
// shiaijo B and D. See TestEKC2025LadiesTeamFiveOccupantBlocksDiverge.
func TestEKC2025LadiesTeamAllocationAndCrossing(t *testing.T) {
	t.Parallel()

	// The sheet's own allocation, back-loaded, as read off the shiaijo letters
	// printed beside each pool winner: P1,P2 on A; P3,P4 on B; P5,P6 on C;
	// P7,P8,P9 on D.
	sheetAssignment := []int{0, 0, 1, 1, 2, 2, 3, 3, 3}
	frontLoaded, err := AssignPoolsToCourts(9, 4)
	require.NoError(t, err)
	require.NotEqual(t, sheetAssignment, frontLoaded,
		"this sheet is back-loaded; if AssignPoolsToCourts ever matches it, replay it directly instead")

	draw := BuildKnockoutDrawFromAssignment(ekcPools(9), 2, sheetAssignment, 4)
	require.NotNil(t, draw)
	require.Len(t, draw.Regions, 4)

	// Block membership, i.e. who fights on which shiaijo. Read straight off
	// the sheet: A holds P1#1, P5#2, P2#1, P6#2; B holds P7#2, P8#2, P4#1,
	// P9#2, P3#1; C holds P5#1, P1#2, P6#1, P2#2; D holds P3#2, P4#2, P8#1,
	// P9#1, P7#1.
	assert.Equal(t, []string{"Pool 1-1st", "Pool 2-1st", "Pool 5-2nd", "Pool 6-2nd"},
		occupantsOf(draw.Regions[0]), "shiaijo A: its two home pools plus the 2nds crossed in from C")
	assert.Equal(t, []string{"Pool 3-1st", "Pool 4-1st", "Pool 7-2nd", "Pool 8-2nd", "Pool 9-2nd"},
		occupantsOf(draw.Regions[1]), "shiaijo B: its two home pools plus all three 2nds crossed in from D")
	assert.Equal(t, []string{"Pool 1-2nd", "Pool 2-2nd", "Pool 5-1st", "Pool 6-1st"},
		occupantsOf(draw.Regions[2]), "shiaijo C: its two home pools plus the 2nds crossed in from A")
	assert.Equal(t, []string{"Pool 3-2nd", "Pool 4-2nd", "Pool 7-1st", "Pool 8-1st", "Pool 9-1st"},
		occupantsOf(draw.Regions[3]), "shiaijo D: its three home pools plus the 2nds crossed in from B")

	// The two 4-occupant blocks reproduce exactly, bout for bout and side for
	// side: A is F1/F2/F3 and C is F8/F9/F10 on the sheet.
	assert.Equal(t, [][]string{
		{"Pool 1-1st v Pool 5-2nd", "Pool 2-1st v Pool 6-2nd"}, // F1, F2
		{"W v W"}, // F3
	}, regionRounds(draw.Regions[0]), "shiaijo A")
	assert.Equal(t, [][]string{
		{"Pool 5-1st v Pool 1-2nd", "Pool 6-1st v Pool 2-2nd"}, // F8, F9
		{"W v W"}, // F10
	}, regionRounds(draw.Regions[2]), "shiaijo C")

	// And the shiaijo every bout prints under, including the semifinals on B
	// and C and the final on B.
	assert.Equal(t, [][]string{
		{"B", "D"},                               // F4, F11: the two pre-round bouts
		{"A", "A", "B", "B", "C", "C", "D", "D"}, // F1,F2,F5,F6,F8,F9,F12,F13
		{"A", "B", "C", "D"},                     // F3, F7, F10, F14: the block finals
		{"B", "C"},                               // F15, F16
		{"B"},                                    // F17
	}, courtsByRound(draw), "the shiaijo printed on every bout of the Ladies Team sheet")
}

// TestEKC2025LadiesTeamFiveOccupantBlocksDiverge records the one thing the
// Ladies Team sheet and this package disagree about: how a 5-occupant block
// arranges occupants it did not all draw itself.
//
// Both blocks hold the same five occupants either way (asserted in the test
// above); the question is only which two meet in the pre-round and which home
// pool takes the winner.
//
//	shiaijo B  sheet: F4 = P7#2 v P8#2, then F5 = P4#1 v P9#2 and F6 = P3#1 v W
//	           here:  pre-round P8-2nd v P7-2nd, then P3-1st v P9-2nd and P4-1st v W
//	shiaijo D  sheet: F11 = P3#2 v P4#2, then F12 = P8#1 v P9#1 and F13 = P7#1 v W
//	           here:  pre-round P3-2nd v P8-1st, then P8... (see the assertion)
//
// On B the two agree about WHICH pair fights the pre-round (the two crossed
// 2nds from pools 7 and 8) and differ only in side order and in which of the
// two home pools takes the pre-round winner. On D they differ about the pair
// itself: the sheet puts the two crossed 2nds together, this package pairs a
// crossed 2nd with a home 1st.
//
// The sheet's rule would read "a block's pre-round is fought by its crossed
// occupants". One sheet is not enough to adopt that, and the 2025 Men Team
// sheet's 5-occupant blocks (TestEKC2025MenTeamByes) are already pinned to the
// current template, so this stays recorded rather than acted on. It is the
// evidence to weigh if a second sheet ever shows the same thing.
func TestEKC2025LadiesTeamFiveOccupantBlocksDiverge(t *testing.T) {
	t.Parallel()

	draw := BuildKnockoutDrawFromAssignment(ekcPools(9), 2, []int{0, 0, 1, 1, 2, 2, 3, 3, 3}, 4)
	require.NotNil(t, draw)

	// What this package builds today.
	assert.Equal(t, [][]string{
		{"Pool 8-2nd v Pool 7-2nd"},
		{"Pool 3-1st v Pool 9-2nd", "Pool 4-1st v W"},
		{"W v W"},
	}, regionRounds(draw.Regions[1]), "shiaijo B, as built here")

	assert.Equal(t, [][]string{
		{"Pool 3-2nd v Pool 8-1st"},
		{"Pool 7-1st v W", "Pool 9-1st v Pool 4-2nd"},
		{"W v W"},
	}, regionRounds(draw.Regions[3]), "shiaijo D, as built here")

	// What the sheet prints, stated so the divergence is legible without
	// reopening the PDF. If a change ever makes these the built values, the
	// assertions above fail and this comment is the reason why that is good
	// news rather than a regression.
	sheetB := [][]string{
		{"Pool 7-2nd v Pool 8-2nd"},
		{"Pool 4-1st v Pool 9-2nd", "Pool 3-1st v W"},
		{"W v W"},
	}
	sheetD := [][]string{
		{"Pool 3-2nd v Pool 4-2nd"},
		{"Pool 8-1st v Pool 9-1st", "Pool 7-1st v W"},
		{"W v W"},
	}
	assert.NotEqual(t, sheetB, regionRounds(draw.Regions[1]),
		"shiaijo B now matches the sheet; make it an equality assertion and delete this case")
	assert.NotEqual(t, sheetD, regionRounds(draw.Regions[3]),
		"shiaijo D now matches the sheet; make it an equality assertion and delete this case")

	// Whichever arrangement is used, the pre-round must be fought by occupants
	// the block actually holds, and every occupant must appear exactly once.
	// That invariant holds on both, and is the reason the divergence is a
	// question of style rather than of correctness.
	for _, region := range []*Node{draw.Regions[1], draw.Regions[3]} {
		seen := map[string]int{}
		for _, slot := range TreeToLeafArray(region) {
			if slot != "" {
				seen[slot]++
			}
		}
		require.Len(t, seen, 5, "a 5-occupant block holds five distinct occupants")
		for label, n := range seen {
			assert.Equalf(t, 1, n, "%s appears %d times in one block", label, n)
		}
	}
}

// TestEKC2025JuniorIndividualFemaleCrossesToTheFarShiaijo records the
// small-draw deviation on the 33rd EKC 2025 Junior Individual Female sheet.
//
// The event is 16 competitors in 5 pools: pool 3 holds four and the rest three,
// on 4 shiaijo, A{1,2} B{3} C{4} D{5}. Six qualify, so pool 3 sends its 2nd as
// well: this is exactly the ExtraQualifiersLargerPools shape, and exactly the
// worked example the operator documentation uses.
//
// The sheet's bouts, read off the rendered page:
//
//	F1 (A)  P1#1 v P2#1
//	F2 (D)  P3#2 v P5#1
//	F3 (B)  Winner F1 v P3#1
//	F4 (C)  Winner F2 v P4#1
//	F5 (B)  Winner F3 v Winner F4
//
// Pool 3 is on shiaijo B, and its extra qualifier is drawn onto shiaijo D.
// This package's INDIVIDUAL crossing rule sends an oversized pool's extra to
// its same-half neighbour (court^1), which for B is A; D is B's partner under
// the TEAM map. So this individual event crossed on the team map, and it is
// the only sheet in the suite that does: the 2026 Ladies and Men Individual
// sheets cross B->A and C->D across six observations between them.
//
// Keeping the crossing rule uniform is an operator ruling. This test exists to
// hold the evidence, not to argue with it.
//
// The consequence used to be a refusal: with the extra sent to A rather than
// D, court A held two home pools plus one crossed occupant, which was below
// the floor of the role tables LP-3a transcribed from the bigger sheets, so
// BuildKnockoutDrawPerPool declined. That refusal was STRUCTURAL, not a
// deference to this sheet -- the builder had no layout for a crossed-hosting
// block that small, and declining was the documented contract for a shape it
// could not lay out (never fall back to the uniform builder, which would drop
// the crossing silently).
//
// LP-3d gives that block a layout (buildGeneralCrossedBlock), so the shape
// now BUILDS, and the latent disagreement with this sheet becomes visible
// rather than hidden behind a refusal: the app seats pool 3's extra on A,
// where the uniform rule sends it, and this one sheet seats it on D. That is
// what the operator ruling chose, so the test asserts both halves of it --
// the sheet's destination is still recorded here, and the draw the app builds
// is still the uniform one.
func TestEKC2025JuniorIndividualFemaleCrossesToTheFarShiaijo(t *testing.T) {
	t.Parallel()

	assignment, err := AssignPoolsToCourts(5, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 0, 1, 2, 3}, assignment,
		"the sheet's shiaijo blocks A(1,2) B(3) C(4) D(5)")

	// Pool 3 is the oversized one: four competitors where the others hold
	// three, so it alone sends a second qualifier.
	pools := ekcPools(5)
	pools[2].Players = append(pools[2].Players, Player{Name: "Pool 3 p5", Dojo: "Pool 3 d5"})
	sizes := make([]int, len(pools))
	for i, p := range pools {
		sizes[i] = len(p.Players)
	}
	require.Equal(t, []int{4, 4, 5, 4, 4}, sizes,
		"the fixture's pool 3 is the oversized one; ekcPool builds equal-sized pools and this adds the extra competitor")

	// Where the sheet sends pool 3's extra qualifier, and where this package
	// would send it. B is shiaijo index 1.
	const shiaijoB = 1
	assert.Equal(t, 0, crossNeighbourCourt(shiaijoB, 4),
		"this package's individual rule crosses B to A, its same-half neighbour")
	const sheetDestination = 3 // shiaijo D
	assert.NotEqual(t, sheetDestination, crossNeighbourCourt(shiaijoB, 4),
		"the sheet crosses B to D, the TEAM partner; recorded, not adopted")

	draw := BuildKnockoutDrawPerPool(pools, 1, map[int]int{2: 2}, 4)
	require.NotNil(t, draw,
		"LP-3d gives this small crossed-hosting block a layout, so the shape builds instead of being declined")

	// The crossing that gets built is the uniform one (B->A), NOT the sheet's
	// (B->D). Asserting the seated block makes the divergence explicit
	// instead of leaving it implied by the rule alone.
	extra := pools[2].PoolName + "-2nd"
	assert.Containsf(t, blockOccupantLabels(draw.Regions[0]), extra,
		"the uniform rule seats pool 3's extra on shiaijo A")
	assert.NotContainsf(t, blockOccupantLabels(draw.Regions[sheetDestination]), extra,
		"the sheet seats it on shiaijo D; recorded, not adopted")

	// Rule 3 still holds for the shape the sheet does not cover: the extra
	// qualifier fights in round 1 rather than taking a bye.
	assertFightsInRoundOne(t, draw.Regions[0], extra)
}

// blockOccupantLabels lists the occupant labels seated in one shiaijo's block.
func blockOccupantLabels(region *Node) []string {
	return leafLabels(region)
}
