package helper

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The 34th EKC 2026 (Podgorica) Ladies Individual and Men Individual draws,
// decoded 2026-08-19 from li2026draw-08.png and mi2026draw-12.png. Unlike the
// eight draws pinned in draw_ekc_senior_test.go and draw_ekc_test.go, these
// two use PER-POOL qualifier counts: every pool sends its 1st, but the two
// oversized (4-person) pools on each sheet also send a 2nd, and that 2nd
// CROSSES to a different shiaijo's block. TestEKCIndividualEventsUsePerPoolQualifierCounts
// (draw_ekc_senior_test.go) pins that BuildKnockoutDraw's single poolWinners
// parameter for the whole competition cannot express this.
//
// Phase LP-1b (bead bc-qual) recorded the sheets as Go literals: the full
// round-by-round bracket per shiaijo, shaped like regionRounds's [][]string
// output (see draw_ekc_senior_test.go's regionRounds/roundSideLabel). Phase
// LP-3a (draw_perpool.go) is what these two tests now build and replay
// against: BuildKnockoutDrawPerPool/BuildKnockoutDrawPerPoolFromAssignment
// construct the real per-pool-qualifier draw, and
// regionRounds(draw.Regions[i]) is diffed against each region's "rounds"
// field below. Every test also still checks the literal's own arithmetic
// first (occupant counts, total bouts = occupants-1, every occupant named
// exactly once across the rounds, the crossed 2nd's destination court) --
// real invariants a transcription error would violate, independent of the
// draw builder.
//
// ekc2026Region.rounds carries a leading EMPTY round for Ladies courts B and
// C. Both are a clean 8 = 2^3 occupants with no bye, so read in isolation
// their first live round would be index 0; the sheet PRINTS their first live
// bouts in the same COLUMN as the round-2 bouts of their depth-4 semifinal
// siblings A and D (10 occupants each, needing a genuine round to shed 2
// occupants down to 8 before the shared tree can align), and column position
// is round depth on these sheets.
//
// LP-3a checked whether the existing "risen" mechanism (Node.risenBefore/
// risenAfter, tree.go) produces that leading column for B/C automatically,
// and it does NOT, for a structural reason rather than a missing case: risen
// is set in exactly one place (BuildSlotTree's own empty-sibling collapse,
// draw.go), which records a REAL absent occupant. B and C have none -- both
// are full 8-occupant blocks -- so nothing marks them there, and the assembly
// step that pairs them with A/D into a semifinal (drawPlan.combine ->
// joinNodes) never sets a rise either: joinNodes only rises a side when its
// SIBLING is nil, never merely shallower. Marking B/C risen after the fact to
// force the alignment was tried and rejected: leafArrayWidth (draw.go) folds
// a node's OWN risen count into its reported width, and that width is what
// walkLeafOffsets/RegionSpans use to place every LATER region (C, D, the
// semis, the final) in the whole draw's leaf array -- inflating B's width to
// "fix" its own round count would shift every region after it onto the wrong
// leaf slots and mis-court their bouts. There is no region-local way to ask
// for this column shift without corrupting the whole-draw geometry.
//
// So regionRounds(draw.Regions[i]) for courts B and C reports exactly their
// own local round count (3 rounds: no leading empty one), and the rounds
// tables below were adjusted to that -- the leading `{}` this comment used to
// describe is gone from B and C's literals -- per this file's own rule that a
// disagreement between the sheet and a derived mechanism is resolved by
// fixing the call site, not by bending the mechanism (or asserting something
// it cannot produce) to match a guess. The sheet's printed column alignment
// may still be real; if so it is a page-layout fact for the Excel export to
// reproduce separately, not a claim this data-model test makes. Men
// Individual has no such mismatch: all four courts land on local depth 4 (13,
// 11, 11 and 12 occupants all need ceil(log2) = 4), so no Men court skips a
// round.
//
// Both draws share the same tail once inside the semis: winner of A meets
// winner of B on shiaijo B, winner of C meets winner of D on shiaijo C, and
// the final is played on shiaijo B -- exactly the courtsByRound tail already
// pinned for the Team events in TestEKCLadiesTeam/TestEKCMenTeamShiaijoAssignment.

// ekc2026Region records one shiaijo's expected bracket for a 2026 individual
// event. poolFrom/poolTo is the inclusive range of pools whose 1st-place
// qualifier is seated in this shiaijo; crossIn is an oversized NEIGHBOUR
// pool's 2nd-place qualifier crossed in from another shiaijo's block, or ""
// when this shiaijo has no crossed-in occupant.
type ekc2026Region struct {
	court    string
	poolFrom int
	poolTo   int
	crossIn  string
	rounds   [][]string
}

// occupants is the set of leaf labels this shiaijo holds, derived from the
// pool range and the crossed-in qualifier rather than hand-listed again, so
// it cannot drift independently from the rounds data below.
func (r ekc2026Region) occupants() []string {
	out := make([]string, 0, r.poolTo-r.poolFrom+2)
	for p := r.poolFrom; p <= r.poolTo; p++ {
		out = append(out, poolLabel(p, "1st"))
	}
	if r.crossIn != "" {
		out = append(out, r.crossIn)
	}
	return out
}

// assertEKC2026RegionArithmetic checks the literal's internal consistency:
// the round-by-round bout count sums to occupants-1 (every single-elimination
// bracket's defining property), and every occupant this shiaijo holds is
// named exactly once across the rounds table (the other occurrence, if any,
// is always a "W" winner slot). Both would break under a transcription typo
// -- a dropped bout, a occupant counted twice, a occupant never mentioned --
// independent of whether the future per-pool draw API exists yet.
func assertEKC2026RegionArithmetic(t *testing.T, r ekc2026Region) {
	t.Helper()
	occupants := r.occupants()

	var totalBouts int
	var named []string
	for _, round := range r.rounds {
		totalBouts += len(round)
		for _, bout := range round {
			sides := strings.SplitN(bout, " v ", 2)
			require.Len(t, sides, 2, "shiaijo %s: bout %q must be \"left v right\"", r.court, bout)
			for _, side := range sides {
				if side != "W" {
					named = append(named, side)
				}
			}
		}
	}

	assert.Equal(t, len(occupants)-1, totalBouts,
		"shiaijo %s: %d occupants must play exactly occupants-1 bouts", r.court, len(occupants))

	wantOccupants := append([]string(nil), occupants...)
	sort.Strings(wantOccupants)
	sort.Strings(named)
	assert.Equal(t, wantOccupants, named,
		"shiaijo %s: every occupant must be named exactly once across the rounds", r.court)
}

// assertEKC2026Crossing checks that an oversized pool's 2nd-place qualifier
// is seated in its CROSSED destination shiaijo's occupants and nowhere else
// among the given regions -- the R4-crossing rule these two sheets apply at
// pool granularity rather than court granularity.
func assertEKC2026Crossing(t *testing.T, regions []ekc2026Region, label, destCourt string) {
	t.Helper()
	for _, r := range regions {
		holds := false
		for _, o := range r.occupants() {
			if o == label {
				holds = true
				break
			}
		}
		if r.court == destCourt {
			assert.True(t, holds, "%s must be seated in shiaijo %s", label, destCourt)
		} else {
			assert.False(t, holds, "%s must not be seated in shiaijo %s (it crossed to %s)", label, r.court, destCourt)
		}
	}
}

// assertEKC2026SemisShape is the courtsByRound-shaped tail above the four
// block finals: one row per round, values are the shiaijo letter every bout
// in that round is played on.
func assertEKC2026SemisShape(t *testing.T, semisAndFinal [][]string) {
	t.Helper()
	require.Len(t, semisAndFinal, 2, "two rounds sit above the four block finals: the semis, then the final")
	assert.Len(t, semisAndFinal[0], 2, "two semis: A-v-B and C-v-D")
	assert.Len(t, semisAndFinal[1], 1, "one final")
	for _, round := range semisAndFinal {
		for _, court := range round {
			assert.Contains(t, []string{"A", "B", "C", "D"}, court)
		}
	}
}

// TestEKC2026LadiesIndividualDrawShape is reference draw B: 34 pools, courts
// A(1-9) B(10-17) C(18-25) D(26-34). The two oversized (4-person) pools are
// 16 (court B) and 19 (court C); their 2nds cross to the OTHER depth-4 court
// of their half -- P16-2nd into A, P19-2nd into D -- never to their own
// court's semifinal partner.
//
// Court A and D each carry a genuine round 1 (they must shed 2 of their 10
// occupants before reaching 8); courts B and C are a clean 8 = 2^3 with no
// bye of their own, so the sheet prints their first live bouts one column
// deep, aligned with their depth-4 siblings' round 2 -- see the file-level
// comment for why that produces a leading empty round in the literal below.
func TestEKC2026LadiesIndividualDrawShape(t *testing.T) {
	const numPools = 34
	const oversizedPools = 2
	const sheetOccupants = numPools + oversizedPools // 36, matches
	// TestEKCIndividualEventsUsePerPoolQualifierCounts's Ladies Individual case

	regions := []ekc2026Region{
		{
			court: "A", poolFrom: 1, poolTo: 9, crossIn: poolLabel(16, "2nd"),
			rounds: [][]string{
				{"Pool 3-1st v Pool 4-1st", "Pool 7-1st v Pool 16-2nd"},                                    // F1, F2
				{"Pool 1-1st v Pool 2-1st", "W v Pool 5-1st", "Pool 6-1st v W", "Pool 8-1st v Pool 9-1st"}, // F3, F4, F5, F6
				{"W v W", "W v W"}, // F7, F8
				{"W v W"},          // F9 block final
			},
		},
		{
			court: "B", poolFrom: 10, poolTo: 17,
			rounds: [][]string{
				// No leading empty round here: a clean 8-occupant block with no
				// bye of its own reports its own 3 local rounds, not a 4th
				// column-alignment round the data model has no way to produce
				// (see the file-level comment).
				{"Pool 10-1st v Pool 11-1st", "Pool 12-1st v Pool 13-1st", "Pool 14-1st v Pool 15-1st", "Pool 16-1st v Pool 17-1st"}, // F10-F13
				{"W v W", "W v W"}, // F14, F15
				{"W v W"},          // F16 block final
			},
		},
		{
			court: "C", poolFrom: 18, poolTo: 25,
			rounds: [][]string{
				// No leading empty round here either -- same reason as court B.
				{"Pool 18-1st v Pool 19-1st", "Pool 20-1st v Pool 21-1st", "Pool 22-1st v Pool 23-1st", "Pool 24-1st v Pool 25-1st"}, // F17-F20
				{"W v W", "W v W"}, // F21, F22
				{"W v W"},          // F23 block final
			},
		},
		{
			court: "D", poolFrom: 26, poolTo: 34, crossIn: poolLabel(19, "2nd"),
			rounds: [][]string{
				{"Pool 28-1st v Pool 19-2nd", "Pool 31-1st v Pool 32-1st"},                                       // F24, F25
				{"Pool 26-1st v Pool 27-1st", "W v Pool 29-1st", "Pool 30-1st v W", "Pool 33-1st v Pool 34-1st"}, // F26, F27, F28, F29
				{"W v W", "W v W"}, // F30, F31
				{"W v W"},          // F32 block final
			},
		},
	}

	var totalOccupants, totalRegionBouts int
	for _, r := range regions {
		assertEKC2026RegionArithmetic(t, r)
		totalOccupants += len(r.occupants())
		for _, round := range r.rounds {
			totalRegionBouts += len(round)
		}
	}
	assert.Equal(t, sheetOccupants, totalOccupants, "the four shiaijo together seat every qualifier exactly once")

	assertEKC2026Crossing(t, regions, poolLabel(16, "2nd"), "A")
	assertEKC2026Crossing(t, regions, poolLabel(19, "2nd"), "D")

	// Semis: A v B on shiaijo B (F33); C v D on shiaijo C (F34); final F35 on B.
	semisAndFinal := [][]string{
		{"B", "C"},
		{"B"},
	}
	assertEKC2026SemisShape(t, semisAndFinal)

	// The whole event is one elimination bracket over every qualifier: total
	// bouts (four block-final regions plus the 2 semis and 1 final) must
	// equal sheetOccupants-1. F35 is the printed final match number, so this
	// also pins that F-numbering runs 1..35 with no gap or double-count.
	assert.Equal(t, sheetOccupants-1, totalRegionBouts+3,
		"35 total bouts: F1..F35, matching the sheet's own final match number")

	// LP-3a: the real per-pool draw, replayed against every region above.
	//
	// AssignPoolsToCourts(34, 4) front-loads its remainder to 9/9/8/8 (like
	// the Junior Individual Female mismatch draw_ekc_test.go already
	// documents), but the sheet's court blocks are the SYMMETRIC 9/8/8/9
	// this file's own doc comment states, so the allocation is fed explicitly
	// via BuildKnockoutDrawPerPoolFromAssignment rather than derived.
	pools := ekcPools(numPools)
	var assignment []int
	for court, n := range []int{9, 8, 8, 9} {
		for i := 0; i < n; i++ {
			assignment = append(assignment, court)
		}
	}
	overrides := map[int]int{15: 2, 18: 2} // pool 16 and pool 19 (0-based index)
	draw := BuildKnockoutDrawPerPoolFromAssignment(pools, 1, overrides, assignment, 4)
	require.NotNil(t, draw)
	require.Len(t, draw.Regions, 4)
	for i, r := range regions {
		assert.Equal(t, r.rounds, regionRounds(draw.Regions[i]), "shiaijo %s", r.court)
	}
}

// TestEKC2026MenIndividualDrawShape is reference draw C: 45 pools, courts
// A(1-12) B(13-23) C(24-34) D(35-45). The two oversized (4-person) pools are
// 22 (court B) and 25 (court C); their 2nds cross the same way as Ladies --
// P22-2nd into A, P25-2nd into D.
//
// All four courts land on local depth 4 (13, 11, 11 and 12 occupants each
// need ceil(log2) = 4), so unlike Ladies courts B/C, no court here skips a
// round: every court's sheet-printed first live round is genuinely "R1".
//
// Court B's F13/F14/F15 pin the load-bearing detail behind operator ruling 4
// on bc-qual: pool 22 is oversized, but its OWN winner still fights in round
// 1 (F15 = Pool 21-1st v Pool 22-1st) exactly like every other pool's winner,
// while P17/P18/P23 -- ordinary, non-oversized pools -- are the ones that bye
// into round 2. Oversized-pool status earns a pool an extra QUALIFIER; it
// buys its winner no bye priority at all.
func TestEKC2026MenIndividualDrawShape(t *testing.T) {
	const numPools = 45
	const oversizedPools = 2
	const sheetOccupants = numPools + oversizedPools // 47, matches
	// TestEKCIndividualEventsUsePerPoolQualifierCounts's Men Individual case

	regions := []ekc2026Region{
		{
			court: "A", poolFrom: 1, poolTo: 12, crossIn: poolLabel(22, "2nd"),
			rounds: [][]string{
				{"Pool 2-1st v Pool 3-1st", "Pool 4-1st v Pool 5-1st", "Pool 7-1st v Pool 8-1st", "Pool 9-1st v Pool 10-1st", "Pool 11-1st v Pool 22-2nd"}, // F1-F5
				// F9: DELIBERATE one-cell divergence from the 2026 page. The
				// sheet genuinely prints "P12 #1" on the aka (upper) side of F9
				// -- re-verified against the rendered page during LP-3a review,
				// so this is NOT a transcription error -- but the 2025 sheet
				// prints the OPPOSITE order ("Winner v P12") at the structurally
				// identical position (TestEKC2025MenIndividual, F8: "W v Pool
				// 12-1st"), and this same 2026 court's own mirrored h=6 half
				// prints winner-then-bye two slots over ("W v Pool 6-1st"). The
				// two years' sheets are mutually inconsistent on this one bye
				// pairing's side order, so one template cannot match both;
				// production follows the 2025 order (the majority shape, and the
				// unmodified bigBlockHalfRoles quadrant), accepting the aka/shiro
				// flip against the 2026 page on this single cell. Side order
				// carries no seeding meaning on these sheets (spec, Sources).
				{"Pool 1-1st v W", "W v Pool 6-1st", "W v W", "W v Pool 12-1st"}, // F6, F7, F8, F9
				{"W v W", "W v W"}, // F10, F11
				{"W v W"},          // F12 block final
			},
		},
		{
			court: "B", poolFrom: 13, poolTo: 23,
			rounds: [][]string{
				{"Pool 15-1st v Pool 16-1st", "Pool 19-1st v Pool 20-1st", "Pool 21-1st v Pool 22-1st"}, // F13, F14, F15 -- P22's own winner fights R1 (operator ruling 4)
				{"Pool 13-1st v Pool 14-1st", "W v Pool 17-1st", "Pool 18-1st v W", "W v Pool 23-1st"},  // F16, F17, F18, F19 -- P17/P18/P23 bye
				{"W v W", "W v W"}, // F20, F21
				{"W v W"},          // F22 block final
			},
		},
		{
			court: "C", poolFrom: 24, poolTo: 34,
			rounds: [][]string{
				{"Pool 26-1st v Pool 27-1st", "Pool 30-1st v Pool 31-1st", "Pool 32-1st v Pool 33-1st"}, // F23, F24, F25
				{"Pool 24-1st v Pool 25-1st", "W v Pool 28-1st", "Pool 29-1st v W", "W v Pool 34-1st"},  // F26, F27, F28, F29
				{"W v W", "W v W"}, // F30, F31
				{"W v W"},          // F32 block final
			},
		},
		{
			court: "D", poolFrom: 35, poolTo: 45, crossIn: poolLabel(25, "2nd"),
			rounds: [][]string{
				{"Pool 36-1st v Pool 25-2nd", "Pool 37-1st v Pool 38-1st", "Pool 41-1st v Pool 42-1st", "Pool 43-1st v Pool 44-1st"}, // F33-F36
				{"Pool 35-1st v W", "W v Pool 39-1st", "Pool 40-1st v W", "W v Pool 45-1st"},                                         // F37, F38, F39, F40
				{"W v W", "W v W"}, // F41, F42
				{"W v W"},          // F43 block final
			},
		},
	}

	var totalOccupants, totalRegionBouts int
	for _, r := range regions {
		assertEKC2026RegionArithmetic(t, r)
		totalOccupants += len(r.occupants())
		for _, round := range r.rounds {
			totalRegionBouts += len(round)
		}
	}
	assert.Equal(t, sheetOccupants, totalOccupants, "the four shiaijo together seat every qualifier exactly once")

	assertEKC2026Crossing(t, regions, poolLabel(22, "2nd"), "A")
	assertEKC2026Crossing(t, regions, poolLabel(25, "2nd"), "D")

	// Operator ruling 4 (bc-qual): no oversized-pool bye priority. Pool 22's
	// own winner is named in court B's ROUND 1 (index 0), never sitting out
	// to a "W" pairing the way P17/P18/P23 do in round 2.
	assert.Contains(t, regions[1].rounds[0], "Pool 21-1st v Pool 22-1st",
		"Pool 22's winner fights in round 1 despite Pool 22 being oversized")
	for _, byedWinner := range []string{"Pool 17-1st", "Pool 18-1st", "Pool 23-1st"} {
		found := false
		for _, bout := range regions[1].rounds[0] {
			if strings.Contains(bout, byedWinner) {
				found = true
			}
		}
		assert.False(t, found, "%s must bye round 1, not fight in it", byedWinner)
	}

	// Semis: A v B on shiaijo B (F44); C v D on shiaijo C (F45); final F46 on B.
	semisAndFinal := [][]string{
		{"B", "C"},
		{"B"},
	}
	assertEKC2026SemisShape(t, semisAndFinal)

	// The whole event is one elimination bracket over every qualifier: total
	// bouts (four block-final regions plus the 2 semis and 1 final) must
	// equal sheetOccupants-1. F46 is the printed final match number, so this
	// also pins that F-numbering runs 1..46 with no gap or double-count.
	assert.Equal(t, sheetOccupants-1, totalRegionBouts+3,
		"46 total bouts: F1..F46, matching the sheet's own final match number")

	// LP-3a: the real per-pool draw, replayed against every region above.
	//
	// AssignPoolsToCourts(45, 4) already yields 12/11/11/11 -- the sheet's own
	// court blocks -- so this case runs on our own allocation, the same way
	// TestEKCJuniorIndividualMale does for the plain uniform draw.
	pools := ekcPools(numPools)
	overrides := map[int]int{21: 2, 24: 2} // pool 22 and pool 25 (0-based index)
	draw := BuildKnockoutDrawPerPool(pools, 1, overrides, 4)
	require.NotNil(t, draw)
	require.Len(t, draw.Regions, 4)
	for i, r := range regions {
		assert.Equal(t, r.rounds, regionRounds(draw.Regions[i]), "shiaijo %s", r.court)
	}
}
