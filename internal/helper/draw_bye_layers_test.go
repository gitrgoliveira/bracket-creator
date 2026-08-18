package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Bye placement is two independent rules composed (spec R6(a)):
//
//	layer 1 -- occupant counts, plus D2 for the shallow region -- decides WHICH
//	           blocks carry a bye. It is blind to seeding.
//	layer 2 -- R6's precedence list -- decides WHO takes a bye that layer 1 has
//	           already produced. It never creates one.
//
// These cases pin the composition, using the 34th EKC reference shapes as the
// fixtures so a failure names a real draw rather than an invented one. They
// exist because "the top seeds get the byes" is an OUTCOME of the two layers on
// most fields, not a rule in its own right, and reading it as one leads to
// changing R2 seed placement or D1 pool sizing to chase byes -- both wrong.

// byesPerCourt reports each region's round-1 named byes, keyed by shiaijo
// letter, so a case can state the whole draw's bye picture in one literal.
func byesPerCourt(draw *KnockoutDraw) map[string][]string {
	out := map[string][]string{}
	for i, r := range draw.Regions {
		_, byes := regionRound1(r)
		out[CourtLabel(i)] = byes
	}
	return out
}

// TestByePlacementLayer1IgnoresSeeding pins the half of R6(a) that no amount of
// seeding can move: which blocks carry a bye, and how many.
//
// The fixture is the Male shape (18 pools, 1 qualifier, 4 shiaijo -> 5/5/4/4).
// Courts A and B hold an odd occupant count and must each carry exactly one
// bye; C and D are even and must carry none. Every seeding below -- none, seeds
// only in the FULL regions, seeds only in the short ones, seeds everywhere --
// must produce that same shape.
//
// The C/D rows are the load-bearing ones: they are what makes "resize pools or
// move seeds so a seed lands on a bye" a wrong fix rather than a tuning knob.
func TestByePlacementLayer1IgnoresSeeding(t *testing.T) {
	cases := []struct {
		name  string
		seeds []int
	}{
		{"no seeds at all", nil},
		{"one seed in a full region (C)", []int{11}},
		{"two seeds, both in full regions (C, D)", []int{11, 15}},
		{"one seed in a short region (A)", []int{3}},
		{"seeds in a short and a full region (A, C)", []int{3, 11}},
		{"a seed on the LAST pool of each short region", []int{5, 10}},
		{"one seed per shiaijo", []int{2, 7, 12, 16}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			draw := BuildKnockoutDraw(ekcPools(18, tc.seeds...), 1, 4)
			byes := byesPerCourt(draw)
			assert.Len(t, byes["A"], 1, "court A holds 5 occupants and must carry exactly one bye")
			assert.Len(t, byes["B"], 1, "court B holds 5 occupants and must carry exactly one bye")
			assert.Empty(t, byes["C"], "seeding must not conjure a bye in a full region")
			assert.Empty(t, byes["D"], "seeding must not conjure a bye in a full region")
		})
	}
}

// TestByePlacementLayer2AllocatesOnlyWithinAByeBearingBlock is the companion:
// layer 2 DOES respond to seeding, but only inside a block layer 1 has already
// given a bye. Same Male shape, so the two tests differ in exactly one respect.
//
// Read the last three rows together: a seed on pool 11 or 15 changes nothing at
// all, because those pools live in the full regions C and D. That is what "when
// a seeded pool's winner does not bye, layer 1 left that block full" means, and
// it is the case I twice mistook for a bug.
func TestByePlacementLayer2AllocatesOnlyWithinAByeBearingBlock(t *testing.T) {
	cases := []struct {
		name  string
		seeds []int
		wantA string
		wantB string
	}{
		// No seeds: criterion 3, pool order, which is the reference sheet.
		{"unseeded, byes go by pool order", nil, "Pool 1-1st", "Pool 6-1st"},

		// A seed inside a bye-bearing block takes that block's bye off the
		// first pool. This is criterion 1 actually doing something, which no
		// reference draw shows (R6(b)).
		{"seed on A's third pool", []int{3}, "Pool 3-1st", "Pool 6-1st"},
		{"seed on A's last pool", []int{5}, "Pool 5-1st", "Pool 6-1st"},
		{"seed on B's last pool", []int{10}, "Pool 1-1st", "Pool 10-1st"},
		{"a seed in each short region", []int{5, 10}, "Pool 5-1st", "Pool 10-1st"},

		// A seed inside a FULL block gets nothing, and does not disturb the
		// blocks that do have byes.
		{"seed on C's pools only", []int{11, 13}, "Pool 1-1st", "Pool 6-1st"},
		{"seed on D's pools only", []int{15, 18}, "Pool 1-1st", "Pool 6-1st"},
		{"seeds in both a short and a full region", []int{3, 11}, "Pool 3-1st", "Pool 6-1st"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			byes := byesPerCourt(BuildKnockoutDraw(ekcPools(18, tc.seeds...), 1, 4))
			assert.Equal(t, []string{tc.wantA}, byes["A"])
			assert.Equal(t, []string{tc.wantB}, byes["B"])
			assert.Empty(t, byes["C"])
			assert.Empty(t, byes["D"])
		})
	}
}

// TestByePrecedenceSeedBeatsPoolOrder is the ONLY witness in the suite for R6
// criterion 1, and it has to be synthetic: R6(b) records that all three
// reference draws seed their blocks' first pool, where criteria 1 and 3 agree.
//
// Putting the seed on a non-first pool separates them. Without criterion 1 the
// bye would go to Pool 1-1st / Pool 6-1st by pool order, so this case fails if
// criterion 1 is dropped -- which the reference tests would not catch.
func TestByePrecedenceSeedBeatsPoolOrder(t *testing.T) {
	byes := byesPerCourt(BuildKnockoutDraw(ekcPools(18, 4, 9), 1, 4))

	assert.Equal(t, []string{"Pool 4-1st"}, byes["A"],
		"the seeded pool's winner outranks the block's first pool (R6-1 over R6-3)")
	assert.Equal(t, []string{"Pool 9-1st"}, byes["B"],
		"the seeded pool's winner outranks the block's first pool (R6-1 over R6-3)")

	// Guard the guard: the same shape unseeded gives the pool-order answer, so
	// the assertions above are pinning criterion 1 and not a fixture accident.
	unseeded := byesPerCourt(BuildKnockoutDraw(ekcPools(18), 1, 4))
	assert.Equal(t, []string{"Pool 1-1st"}, unseeded["A"])
	assert.Equal(t, []string{"Pool 6-1st"}, unseeded["B"])
}

// TestEKCReferenceByesSurviveSeedStripping is the evidence behind R6(b): it
// asserts the reference draws are NOT witnesses for criterion 1.
//
// Each sheet is rebuilt with every seed removed. If any bye moved, the sheet
// would be discriminating seeds from pool order and R6(b) would be wrong. None
// does -- so the reference suite constrains layer 1 and rank class only, and
// criterion 1 needs TestByePrecedenceSeedBeatsPoolOrder to be covered at all.
//
// This test is deliberately phrased as "unchanged" rather than transcribing the
// byes again: its subject is the DIFFERENCE seeds make, which is none.
func TestEKCReferenceByesSurviveSeedStripping(t *testing.T) {
	femaleAllocation := []int{0, 0, 1, 2, 2, 3, 3} // A(1,2) B(3) C(4,5) D(6,7)

	cases := []struct {
		name          string
		seeded        *KnockoutDraw
		stripped      *KnockoutDraw
		seededPools   []int
		firstInBlocks string
	}{
		{
			name:          "Junior Individual Female",
			seeded:        BuildKnockoutDrawFromAssignment(ekcPools(7, 1, 3, 4), 1, femaleAllocation, 4),
			stripped:      BuildKnockoutDrawFromAssignment(ekcPools(7), 1, femaleAllocation, 4),
			seededPools:   []int{1, 3, 4},
			firstInBlocks: "pools 1, 3 and 4 each open their block (A, B, C)",
		},
		{
			name:          "Junior Team",
			seeded:        BuildKnockoutDraw(ekcPools(7, 3, 7), 2, 4),
			stripped:      BuildKnockoutDraw(ekcPools(7), 2, 4),
			seededPools:   []int{3, 7},
			firstInBlocks: "pools 3 and 7 each open their block (B, D)",
		},
		{
			// The Male sheet records no seeds for us to strip, so it is
			// included the other way round: adding seeds on the pools that
			// already bye must also change nothing.
			name:          "Junior Individual Male",
			seeded:        BuildKnockoutDraw(ekcPools(18, 1, 6), 1, 4),
			stripped:      BuildKnockoutDraw(ekcPools(18), 1, 4),
			seededPools:   []int{1, 6},
			firstInBlocks: "pools 1 and 6 each open their block (A, B)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NotNil(t, tc.seeded)
			require.NotNil(t, tc.stripped)
			assert.Equal(t, byesPerCourt(tc.seeded), byesPerCourt(tc.stripped),
				"seeds %v change no bye on this sheet: %s, so criteria 1 and 3 agree and the draw cannot witness criterion 1 (R6(b))",
				tc.seededPools, tc.firstInBlocks)
		})
	}
}

// TestByePrecedenceRankClassOutranksSeeding pins the last claim in R6(a): a
// home 1st byes ahead of a crossed-in 2nd even when the 2nd came from a seeded
// pool and the 1st did not. Criterion 4 sits below criteria 1-3 wholesale, so
// rank class dominates seeding.
//
// The Team shape puts court D's block at {P7#1, P3#2, P4#2}. Seeding pool 3 and
// NOT pool 7 makes P3#2 the only seeded occupant there; the bye must still be
// P7#1, the block's lone home 1st.
func TestByePrecedenceRankClassOutranksSeeding(t *testing.T) {
	draw := BuildKnockoutDraw(ekcPools(7, 3), 2, 4)
	byes := byesPerCourt(draw)

	// Confirm the premise before trusting the assertion: court D really does
	// hold a seeded pool's 2nd alongside an unseeded home 1st.
	occupants := TreeLeafLabels(draw.Regions[3])
	require.Contains(t, occupants, "Pool 7-1st", "court D's home 1st")
	require.Contains(t, occupants, "Pool 3-2nd", "the seeded pool's crossed-in 2nd")

	assert.Equal(t, []string{"Pool 7-1st"}, byes["D"],
		"an unseeded home 1st outranks a seeded pool's crossed-in 2nd (R6-3 over R6-4)")
}
