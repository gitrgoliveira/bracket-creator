package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The three decoded 34th EKC 2026 (Podgorica) draw sheets, replayed against the
// court-first draw. They are the acceptance test for specs/007-ekc-draw: R3
// (court blocks), R4 (crossing), R5 (separation), R6 (byes) and D4 (region
// shape) are all pinned here by real reference data rather than by prose.
//
// Each case states, per shiaijo region, the round-1 pairings and the named
// round-1 bye exactly as the sheet prints them.

// ekcPool builds a named pool with size players. Sizes matter to R6 criterion 2
// (oversized pools), so every reference pool is given the SAME size: the sheets
// record no pool sizes, and equal sizes make criterion 2 contribute nothing, so
// the byes below are decided by criterion 1 (seeds) and criterion 3 (pool
// order) alone -- which is exactly what the sheets can corroborate.
func ekcPool(name string, size int, seed int) Pool {
	p := Pool{PoolName: name}
	for i := 0; i < size; i++ {
		pl := Player{Name: fmt.Sprintf("%s p%d", name, i+1), Dojo: fmt.Sprintf("%s d%d", name, i+1)}
		if i == 0 && seed > 0 {
			pl.Seed = seed
		}
		p.Players = append(p.Players, pl)
	}
	return p
}

// ekcPools builds numPools pools named "Pool 1".."Pool N", with the 1-based
// pool numbers in seededPools carrying seed ranks 1, 2, 3... in the order given.
func ekcPools(numPools int, seededPools ...int) []Pool {
	seedOf := map[int]int{}
	for i, p := range seededPools {
		seedOf[p] = i + 1
	}
	pools := make([]Pool, numPools)
	for i := range pools {
		pools[i] = ekcPool(fmt.Sprintf("Pool %d", i+1), 4, seedOf[i+1])
	}
	return pools
}

// courtsByRound reads the shiaijo the draw assigns to every bout, round by
// round, in the same order BuildEliminationMatchRounds emits them. The 34th EKC
// sheets print a shiaijo letter on every bout, so a reference draw pins the
// court assignment as strictly as it pins the pairings.
func courtsByRound(draw *KnockoutDraw) [][]string {
	nc := draw.NodeCourts()
	var out [][]string
	for _, round := range BuildEliminationMatchRounds(draw.Root) {
		row := make([]string, 0, len(round))
		for _, m := range round {
			row = append(row, CourtLabel(nc[m]))
		}
		out = append(out, row)
	}
	return out
}

// regionRound1 returns a region's round-1 layer exactly as D4 defines it: the
// slot array paired (2i, 2i+1), with both-real pairs reported as matches, a
// one-real pair as a NAMED BYE, and both-empty pairs (phantoms) dropped
// entirely -- they are never printed or displayed.
func regionRound1(region *Node) (matches []string, byes []string) {
	slots := TreeToLeafArray(region)
	matches, byes = []string{}, []string{}
	for i := 0; i+1 < len(slots); i += 2 {
		a, b := slots[i], slots[i+1]
		switch {
		case a != "" && b != "":
			matches = append(matches, a+" v "+b)
		case a != "":
			byes = append(byes, a)
		case b != "":
			byes = append(byes, b)
		}
	}
	if len(slots) == 1 && slots[0] != "" {
		// A one-occupant region has no round-1 layer at all: its occupant byes
		// straight into the region's parent (EKC Female court B).
		byes = append(byes, slots[0])
	}
	return matches, byes
}

// halfLeaves returns the labels in each half of the whole draw, which is what
// R5's "opposite halves" claim is checked against.
func halfLeaves(root *Node) (first, second []string) {
	require := TreeToLeafArray(root)
	mid := len(require) / 2
	for _, l := range require[:mid] {
		if l != "" {
			first = append(first, l)
		}
	}
	for _, l := range require[mid:] {
		if l != "" {
			second = append(second, l)
		}
	}
	return first, second
}

type ekcRegion struct {
	court   string
	matches []string
	byes    []string
}

func assertEKCRegions(t *testing.T, draw *KnockoutDraw, want []ekcRegion) {
	t.Helper()
	require.NotNil(t, draw)
	require.Len(t, draw.Regions, len(want), "one bracket region per shiaijo (R3)")
	for i, w := range want {
		t.Run(w.court, func(t *testing.T) {
			matches, byes := regionRound1(draw.Regions[i])
			assert.Equal(t, w.matches, matches, "round-1 matches on shiaijo %s", w.court)
			assert.Equal(t, w.byes, byes, "round-1 byes on shiaijo %s", w.court)
		})
	}
}

// TestEKCJuniorIndividualMale is reference draw A: 18 pools, 1 qualifier per
// pool, four shiaijo A(1-5) B(6-10) C(11-14) D(15-18).
//
// It is the sharpest case in the spec. Courts A and B hold 5 pools each, and a
// 5-occupant region is where the two candidate region constructions disagree:
// the sheet prints ONE named bye and TWO round-1 matches (P1 byes, P2 v P3,
// P4 v P5), with the round-2 bye falling to W(P4 v P5) rather than to P1.
// Recursive halving would have put the bye on occupant 3, and
// pad-to-NextPow2-and-spread would have produced three named byes and one
// match. Only the greedy layout (D4) reproduces the sheet.
//
// It is also the case that refutes any blanket "round-1 opponents must come
// from different courts" rule: at 1 qualifier nothing crosses (R4d) and P2 v P3
// are both court A's pools.
func TestEKCJuniorIndividualMale(t *testing.T) {
	pools := ekcPools(18)

	// AssignPoolsToCourts(18, 4) already yields 5/5/4/4, so this case runs on
	// our own allocation rather than a hand-fed one.
	assignment, err := AssignPoolsToCourts(18, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3}, assignment,
		"the sheet's court blocks A(1-5) B(6-10) C(11-14) D(15-18)")

	draw := BuildKnockoutDraw(pools, 1, 4)
	assertEKCRegions(t, draw, []ekcRegion{
		{court: "A", matches: []string{"Pool 2-1st v Pool 3-1st", "Pool 4-1st v Pool 5-1st"}, byes: []string{"Pool 1-1st"}},
		{court: "B", matches: []string{"Pool 7-1st v Pool 8-1st", "Pool 9-1st v Pool 10-1st"}, byes: []string{"Pool 6-1st"}},
		{court: "C", matches: []string{"Pool 11-1st v Pool 12-1st", "Pool 13-1st v Pool 14-1st"}, byes: []string{}},
		{court: "D", matches: []string{"Pool 15-1st v Pool 16-1st", "Pool 17-1st v Pool 18-1st"}, byes: []string{}},
	})

	// The worked ladder the spec calls out: court A is 5 -> 3 -> 2 -> 1, four
	// matches in three rounds, and the round-2 bye belongs to the winner of
	// P4 v P5, NOT to P1.
	regionA := draw.Regions[0]
	require.NotNil(t, regionA)
	assert.Equal(t, 4, len(BuildEliminationMatchRounds(regionA)[0])+len(BuildEliminationMatchRounds(regionA)[1])+len(BuildEliminationMatchRounds(regionA)[2]),
		"a 5-occupant region plays four matches")
	assert.Equal(t, 4, CalculateDepth(regionA), "5 -> 3 -> 2 -> 1 is three rounds of matches")
	// P1's second match is the round-2 bout, so P1 is NOT the round-2 bye; the
	// bye sits on the branch holding P4 and P5.
	assert.Equal(t, []string{"Pool 4-1st", "Pool 5-1st"}, TreeLeafLabels(regionA.Right),
		"W(P4 v P5) takes the round-2 bye straight into the region final")

	// R6: a region with no structural bye grants none whatever its precedence.
	for _, i := range []int{2, 3} {
		_, byes := regionRound1(draw.Regions[i])
		assert.Empty(t, byes, "a 4-occupant region has no bye to give")
	}

	// The sheet's COLUMNS, not just its pairings: F1 and F2 both sit in the
	// round-1 column, so P4 v P5 is fought in round 1 even though its winner
	// then byes round 2. The phantom collapse used to lift that bout into
	// round 2 on every surface that reads rounds (Node.risen records the lift
	// and TraverseRounds counts it back); the vacancy blocks of the 2025 Men
	// Team sheet are the shape that must NOT be pulled forward the same way,
	// and TestEKC2025MenTeamByes pins them.
	assert.Equal(t, [][]string{
		{"Pool 2-1st v Pool 3-1st", "Pool 4-1st v Pool 5-1st"}, // F1, F2
		{"Pool 1-1st v W"}, // F3
		{"W v W"},          // F4
	}, regionRounds(regionA), "the round each court-A bout is fought in, as the sheet prints it")
}

// TestEKCJuniorIndividualFemale is reference draw B: 7 pools, 1 qualifier,
// courts A(1,2) B(3) C(4,5) D(6,7), seeds on pools 1, 3 and 4.
//
// INPUT DIFFERENCE, deliberate: AssignPoolsToCourts(7, 4) front-loads the
// remainder and gives 2/2/2/1, where the sheet used 2/1/2/2. The allocation is
// fed explicitly so this case tests the DRAW rather than the allocation. It is
// the only reference with unequal pools per court, and it is what corroborates
// D2: the court with the fewest pools (B, with one) reaches the half-final
// having played nothing while court A's occupant plays F1 first.
func TestEKCJuniorIndividualFemale(t *testing.T) {
	pools := ekcPools(7, 1, 3, 4)
	sheetAllocation := []int{0, 0, 1, 2, 2, 3, 3} // A(1,2) B(3) C(4,5) D(6,7)

	draw := BuildKnockoutDrawFromAssignment(pools, 1, sheetAllocation, 4)

	// The sheet's shiaijo: F1 on A, F2 on C, F3 on D, then F4 on B, F5 on C and
	// the final F6 on B.
	assert.Equal(t, [][]string{
		{"A", "C", "D"},
		{"B", "C"},
		{"B"},
	}, courtsByRound(draw), "the shiaijo printed on every bout of the Junior Individual Female sheet")

	assertEKCRegions(t, draw, []ekcRegion{
		{court: "A", matches: []string{"Pool 1-1st v Pool 2-1st"}, byes: []string{}}, // F1
		{court: "B", matches: []string{}, byes: []string{"Pool 3-1st"}},              // byes to F4
		{court: "C", matches: []string{"Pool 4-1st v Pool 5-1st"}, byes: []string{}}, // F2
		{court: "D", matches: []string{"Pool 6-1st v Pool 7-1st"}, byes: []string{}}, // F3
	})

	// F4 = W(F1) v P3 and F5 = W(F2) v W(F3): the halves are {A,B} and {C,D},
	// and half 1 holds three entrants against half 2's four.
	first, second := halfLeaves(draw.Root)
	assert.Equal(t, []string{"Pool 1-1st", "Pool 2-1st", "Pool 3-1st"}, first)
	assert.Equal(t, []string{"Pool 4-1st", "Pool 5-1st", "Pool 6-1st", "Pool 7-1st"}, second)

	// D2 / R6-1 together: court B's region is a bare leaf (the shallow region
	// went to the court with the fewest pools) and its occupant is a seeded
	// pool's winner.
	require.True(t, draw.Regions[1].LeafNode, "the one-pool court's region is a single leaf")
	assert.Equal(t, "Pool 3-1st", draw.Regions[1].LeafVal)

	// R2: the three seeded pools sit on three different shiaijo (A, B, C).
	seededCourts := map[int]bool{}
	for i, r := range draw.Regions {
		for _, l := range TreeLeafLabels(r) {
			switch l {
			case "Pool 1-1st", "Pool 3-1st", "Pool 4-1st":
				seededCourts[i] = true
			}
		}
	}
	assert.Equal(t, map[int]bool{0: true, 1: true, 2: true}, seededCourts,
		"one seeded pool per shiaijo, on distinct courts (R2)")
}

// TestEKCJuniorTeam is reference draw C: 7 pools, 2 qualifiers, courts A(1,2)
// B(3,4) C(5,6) D(7). This IS AssignPoolsToCourts(7, 4), so the whole case runs
// on our own allocation.
//
// It pins R4 crossing at 2 qualifiers (every 1st stays home, every 2nd crosses
// to the partner court, partners A-C and B-D), R5 (a pool's 1st and 2nd in
// opposite halves), R4f (Q4 is short of home 1sts and hosts P3#2 v P4#2, two
// court-B seconds meeting each other) and R6-1 (both byes go to a seeded pool's
// home 1st).
func TestEKCJuniorTeam(t *testing.T) {
	// The sheet's byes went to P3#1 and P7#1, "both seeded pools' winners".
	pools := ekcPools(7, 3, 7)

	assignment, err := AssignPoolsToCourts(7, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 0, 1, 1, 2, 2, 3}, assignment,
		"the sheet's court blocks A(1,2) B(3,4) C(5,6) D(7)")

	draw := BuildKnockoutDraw(pools, 2, 4)

	// The sheet's shiaijo, bout by bout: F1-F3 on A, F4-F5 on B, F6-F8 on C,
	// F9-F10 on D, then F11 on B, F12 on C and the final F13 on B. The closing
	// bouts run on the MIDDLE shiaijo, which is what CourtForSpan encodes; the
	// leftmost-region answer this replaced would have put F11 and F13 on A.
	assert.Equal(t, [][]string{
		{"A", "A", "B", "C", "C", "D"},
		{"A", "B", "C", "D"},
		{"B", "C"},
		{"B"},
	}, courtsByRound(draw), "the shiaijo printed on every bout of the Junior Team sheet")

	assertEKCRegions(t, draw, []ekcRegion{
		{court: "A", matches: []string{"Pool 1-1st v Pool 5-2nd", "Pool 2-1st v Pool 6-2nd"}, byes: []string{}},
		{court: "B", matches: []string{"Pool 4-1st v Pool 7-2nd"}, byes: []string{"Pool 3-1st"}},
		{court: "C", matches: []string{"Pool 5-1st v Pool 1-2nd", "Pool 6-1st v Pool 2-2nd"}, byes: []string{}},
		{court: "D", matches: []string{"Pool 3-2nd v Pool 4-2nd"}, byes: []string{"Pool 7-1st"}},
	})

	// R5, verified on P1 exactly as the spec asks: P1#1 in half 1 (Q1), P1#2 in
	// half 2 (Q3), so they can only meet in the final.
	first, second := halfLeaves(draw.Root)
	assert.Contains(t, first, "Pool 1-1st")
	assert.Contains(t, second, "Pool 1-2nd")
	for _, p := range []int{1, 2, 3, 4, 5, 6, 7} {
		a := fmt.Sprintf("Pool %d-1st", p)
		b := fmt.Sprintf("Pool %d-2nd", p)
		assert.NotEqual(t, holdsLabel(first, a), holdsLabel(first, b),
			"pool %d's two qualifiers must sit in opposite halves (R5)", p)
	}

	// R4b as a rule rather than as four transcribed rows: every 2nd place is in
	// its partner court's region (A<->C, B<->D).
	partner := map[int]int{0: 2, 1: 3, 2: 0, 3: 1}
	poolCourt := assignment
	for pi := range pools {
		want := partner[poolCourt[pi]]
		label := fmt.Sprintf("Pool %d-2nd", pi+1)
		assert.Contains(t, TreeLeafLabels(draw.Regions[want]), label,
			"pool %d's 2nd crosses to the partner shiaijo", pi+1)
		assert.Contains(t, TreeLeafLabels(draw.Regions[poolCourt[pi]]), fmt.Sprintf("Pool %d-1st", pi+1),
			"pool %d's 1st stays on its own shiaijo", pi+1)
	}
}

// TestEKCDrawsAreSubtreesPerShiaijo pins R3 across all three references: every
// region is a genuine subtree of the whole bracket (not just a contiguous span
// of leaves), the regions partition the entrants, and no leaf appears twice.
func TestEKCDrawsAreSubtreesPerShiaijo(t *testing.T) {
	cases := map[string]*KnockoutDraw{
		"male":   BuildKnockoutDraw(ekcPools(18), 1, 4),
		"female": BuildKnockoutDrawFromAssignment(ekcPools(7, 1, 3, 4), 1, []int{0, 0, 1, 2, 2, 3, 3}, 4),
		"team":   BuildKnockoutDraw(ekcPools(7, 3, 7), 2, 4),
	}
	for name, draw := range cases {
		t.Run(name, func(t *testing.T) {
			require.NotNil(t, draw)
			all := TreeLeafLabels(draw.Root)
			seen := map[string]int{}
			for _, r := range draw.Regions {
				require.True(t, containsNode(draw.Root, r), "a region must be a node of the bracket")
				for _, l := range TreeLeafLabels(r) {
					seen[l]++
				}
			}
			assert.Len(t, seen, len(all), "the regions partition the entrants")
			for l, n := range seen {
				assert.Equal(t, 1, n, "%s appears in exactly one region", l)
			}
			for _, l := range all {
				assert.Contains(t, seen, l)
				assert.True(t, strings.HasPrefix(l, "Pool "))
			}
		})
	}
}

// containsNode reports whether want is a node inside root (pointer identity).
func containsNode(root, want *Node) bool {
	if root == nil || want == nil {
		return false
	}
	if root == want {
		return true
	}
	return containsNode(root.Left, want) || containsNode(root.Right, want)
}

// TestSeedsLandInD6HalvesAndQuarters is R2 stated as D6, the normative form:
// seeds 1 and 3 fall in one HALF of the draw and seeds 2 and 4 in the other,
// each of the four in a distinct QUARTER and, subject to that, on distinct
// courts and in distinct pools.
//
// It runs the whole operator-visible chain - PoolSeeding, CreatePools,
// ReorderPoolsForCourts, BuildKnockoutDraw - because no single step owns the
// rule: PoolSeeding decides which court a seeded pool runs on, and the draw
// decides which half and quarter that court's region is. Before this, seeds
// went round-robin over courts (1 -> A, 2 -> B, 3 -> C, 4 -> D), which put
// seeds 1 and 2 in the SAME half of a four-shiaijo draw and let the top two
// seeds meet in a semifinal instead of the final.
//
// The 1 v 3 / 2 v 4 grouping is deliberate and differs from the conventional
// bracket's 1 v 4 / 2 v 3; anyone comparing against a standard seeding table
// will see it immediately.
func TestSeedsLandInD6HalvesAndQuarters(t *testing.T) {
	for _, courts := range []int{1, 2, 4} {
		for _, numPools := range []int{4, 8} {
			t.Run(fmt.Sprintf("%d_pools_%d_courts", numPools, courts), func(t *testing.T) {
				players := make([]Player, numPools*4)
				for i := range players {
					players[i] = Player{Name: fmt.Sprintf("p%03d", i), Dojo: fmt.Sprintf("dojo%03d", i)}
				}
				for s := 1; s <= 4; s++ {
					players[s-1].Seed = s
				}

				ordered := PoolSeeding(players, numPools, courts)
				pools, err := CreatePools(ordered, 4, false)
				require.NoError(t, err)
				pools = ReorderPoolsForCourts(pools, courts)
				draw := BuildKnockoutDraw(pools, 2, courts)
				require.NotNil(t, draw)

				seededPool := map[int]string{}
				for _, p := range pools {
					for _, pl := range p.Players {
						if pl.Seed > 0 {
							require.Empty(t, seededPool[pl.Seed], "seed %d appears twice", pl.Seed)
							seededPool[pl.Seed] = p.PoolName
						}
					}
				}
				require.Len(t, seededPool, 4, "all four seeds must be in a pool")
				assert.Len(t, map[string]bool{
					seededPool[1]: true, seededPool[2]: true,
					seededPool[3]: true, seededPool[4]: true,
				}, 4, "seeded pools must be distinct (D7 constraint 4, never droppable)")

				// A seed's position in the draw is its pool WINNER's slot.
				leaves := TreeToLeafArray(draw.Root)
				half, quarter := map[int]int{}, map[int]int{}
				for seed, poolName := range seededPool {
					idx := -1
					for i, l := range leaves {
						if l == poolName+"-1st" {
							idx = i
						}
					}
					require.GreaterOrEqual(t, idx, 0, "seed %d's pool winner must be in the draw", seed)
					half[seed] = idx / (len(leaves) / 2)
					quarter[seed] = idx / (len(leaves) / 4)
				}

				assert.Equal(t, half[1], half[3], "seeds 1 and 3 share a half")
				assert.Equal(t, half[2], half[4], "seeds 2 and 4 share a half")
				assert.NotEqual(t, half[1], half[2], "seeds 1 and 2 are in opposite halves")
				assert.Len(t, map[int]bool{
					quarter[1]: true, quarter[2]: true, quarter[3]: true, quarter[4]: true,
				}, 4, "each seed is in its own quarter")
			})
		}
	}
}

// poolLabel names a pool's qualifier the way the draw labels its leaves.
func poolLabel(pool int, rank string) string {
	return fmt.Sprintf("Pool %d-%s", pool, rank)
}

// holdsLabel reports whether xs holds want. The reference cases compare half
// membership, where the useful assertion is "in exactly one of the two halves"
// rather than a position, so a plain membership test is what they need.
func holdsLabel(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
