package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The eight events of the two most recent World Kendo Championships, decoded
// from the FIK "combination table" sheets:
//
//	19WKC Milan 2024   Women's Individual 203, Men's Individual 242,
//	                   Women's Team 45, Men's Team 60
//	17WKC Incheon 2018 Women's Individual 171, Men's Individual 205,
//	                   Women's Team 38, Men's Team 49
//
// (There is no 18WKC to decode: Paris 2021 was cancelled, the only cancelled
// edition in the championship's history.)
//
// Every sheet has the same shape: numbered BLOCKS holding 2 to 4 competitors
// or teams, a block that instead reads "2nd of Block M" (a DRAFT slot fed by
// another block's runner-up), and the final-tournament match numbers down the
// right. A block is this codebase's pool; a draft slot is what
// ExtraQualifiersFillBracket calls a drafted 2nd.
//
// These are reference data, not aspiration. Where the sheets and this package
// agree, the test says so and pins it. Where they DISAGREE, the test says that
// too, in the same detail -- a divergence recorded is a decision someone can
// revisit, while a divergence quietly omitted is one nobody knows exists.
// The 17WKC Women's Team sheet did exactly that job once already: it was
// first recorded here as a divergence (drafts from seeded blocks the
// oversized-only rule could not reach), the operator ruled the WKC method
// canonical for "Fit the knockout", and the rule changed to match -- so its
// test below is now an agreement test, and the formation/selection rules it
// exercises (FillBracketPoolCount rules 1-4, seeded-first drafting) were
// re-derived from these sheets rather than merely checked against them.
//
// Seeding is uniform across all eight sheets and stated in each one's own
// footnote: blocks 1 and 16 carry the two named seeds, blocks 8 and 9 the two
// drawn ones ("Seed 1a: Japan, 16a: Korea / Seed 8a,9a: ... by Draw"). That
// matters: it is what decides which blocks send a drafted 2nd.

// wkcBlocks builds n blocks of minSize competitors, enlarging the blocks named
// in oversized and seeding the blocks named in seedByBlock. Indices are
// 0-based POOL indices, not the sheets' 1-based block numbers, and each test
// states the mapping it used, because a sheet's block numbering includes the
// draft slots and this package's pool indices do not.
func wkcBlocks(n, minSize int, oversized []int, seedByBlock map[int]int) []Pool {
	over := map[int]bool{}
	for _, i := range oversized {
		over[i] = true
	}
	pools := make([]Pool, n)
	for i := range pools {
		pools[i].PoolName = fmt.Sprintf("Pool %s", poolLetters(i))
		size := minSize
		if over[i] {
			size = minSize + 1
		}
		for j := 0; j < size; j++ {
			p := Player{
				Name: fmt.Sprintf("B%02d-%d", i, j),
				Dojo: fmt.Sprintf("Nation%02d-%d", i, j),
			}
			if j == 0 {
				if s, ok := seedByBlock[i]; ok {
					p.Seed = s
				}
			}
			pools[i].Players = append(pools[i].Players, p)
		}
	}
	return pools
}

// poolLetters mirrors the A..Z, AA.. naming CreatePools gives pools, so a
// fixture's labels match the ones the draw builder emits.
func poolLetters(i int) string {
	if i < 26 {
		return string(rune('A' + i))
	}
	return string(rune('A'+i/26-1)) + string(rune('A'+i%26))
}

// countWKCPlayers is the entrant total a fixture actually holds, so every case
// below can prove it really is the sheet's field size before asserting
// anything about the draw built from it.
func countWKCPlayers(pools []Pool) int {
	n := 0
	for _, p := range pools {
		n += len(p.Players)
	}
	return n
}

// TestWKCTeamFormationAgainstTheSheets runs the four TEAM events through
// FillBracketPoolCount, the formation objective ExtraQualifiersFillBracket
// uses, and compares it with the block layout each sheet actually printed.
// seededPools is 4 throughout: every sheet footnotes exactly four seeded
// blocks (1, 16, 8 and 9), and 38's cut depends on them.
//
// All four agree exactly. The 38-team row is the one that did NOT under the
// original oversized-only supply rule (11 pools where the sheet cut 12) and
// is the reason FillBracketPoolCount's rule 4 exists; see that function's
// doc comment for the derivation.
func TestWKCTeamFormationAgainstTheSheets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		entrants    int
		minSize     int
		sheetPools  int
		sheetDrafts int
		wantPools   int
		wantDrafts  int
	}{
		{
			name:     "19WKC Men's Team: 60 teams, 16 blocks, no draft slots",
			entrants: 60, minSize: 3,
			sheetPools: 16, sheetDrafts: 0,
			wantPools: 16, wantDrafts: 0,
		},
		{
			name:     "17WKC Men's Team: 49 teams, 16 blocks, no draft slots",
			entrants: 49, minSize: 3,
			sheetPools: 16, sheetDrafts: 0,
			wantPools: 16, wantDrafts: 0,
		},
		{
			name:     "19WKC Women's Team: 45 teams, 14 blocks plus 2 draft slots",
			entrants: 45, minSize: 3,
			sheetPools: 14, sheetDrafts: 2,
			wantPools: 14, wantDrafts: 2,
		},
		{
			name:     "17WKC Women's Team: 38 teams, 12 blocks plus 4 draft slots",
			entrants: 38, minSize: 3,
			sheetPools: 12, sheetDrafts: 4,
			wantPools: 12, wantDrafts: 4,
			// The row that forced rule 4: P=12's four drafts exceed its two
			// oversized blocks, and the sheet takes the other two from
			// seeded blocks 8 and 9 (three teams each). Under the original
			// oversized-only supply rule this package cut 11 pools here.
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pools, drafts, err := FillBracketPoolCount(tc.entrants, tc.minSize, 4)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPools, pools, "pool count")
			assert.Equal(t, tc.wantDrafts, drafts, "draft count")

			// Whatever the objective chose, the two numbers must fill a
			// power-of-two bracket exactly -- that IS the objective.
			assert.Equal(t, NextPow2(pools), pools+drafts,
				"winners plus drafted 2nds must exactly fill the bracket, with no byes")

			assert.Equal(t, tc.sheetPools, pools, "matches the sheet's block count")
			assert.Equal(t, tc.sheetDrafts, drafts, "matches the sheet's draft-slot count")
		})
	}
}

// TestWKC19WomenTeamDraftSelectionMatchesTheSheet replays the 19WKC Women's
// Team draw against SelectFillBracketDrafts.
//
// The sheet prints 16 blocks: 14 hold teams and blocks 5 and 12 are draft
// slots reading "2nd of Block 16" and "2nd of Block 1". Dropping the two draft
// slots leaves pool indices 0..13 for blocks 1,2,3,4,6,7,8,9,10,11,13,14,15,16
// -- so block 1 is pool 0, block 8 is pool 6, block 9 is pool 7 and block 16
// is pool 13. The 4-team blocks are 1, 9 and 16 (pools 0, 7, 13) and the seeds
// are Japan on block 1, Korea on block 16, then Canada and Australia by draw
// on blocks 9 and 8.
//
// Modelled as ONE venue-wide bracket (numCourts 1) because that is what the
// sheet is: a WKC combination table has no shiaijo column, and the whole 16
// blocks feed a single final tournament.
//
// The result is exact: the two blocks this package drafts are blocks 1 and 16,
// the two the sheet drafts. Block 9 -- oversized AND seeded -- sends nothing
// on both, and the reason is seed order: it is seed 3 or 4, and only two
// drafts were needed. (An earlier reading credited a capacity skip; the
// 17WKC sheet, where the drafted blocks are not even all oversized, settled
// that seed order is the sheet's actual mechanism.)
//
// Note what supplies that agreement. Seeding does. An unseeded fixture of the
// same shape drafts by pool order and picks blocks 1 and 9 instead, so a test
// that omitted the sheet's seeds would agree with it only by accident.
func TestWKC19WomenTeamDraftSelectionMatchesTheSheet(t *testing.T) {
	t.Parallel()

	const (
		block1  = 0
		block8  = 6
		block9  = 7
		block16 = 13
	)
	pools := wkcBlocks(14, 3,
		[]int{block1, block9, block16},
		map[int]int{block1: 1, block16: 2, block8: 3, block9: 4})
	require.Equal(t, 45, countWKCPlayers(pools), "sanity: this fixture is the sheet's 45-team field")

	poolHalf, capacityByHalf, ok := FillBracketDraftCapacity(pools, 2, 1)
	require.True(t, ok)

	drafted, err := SelectFillBracketDrafts(pools, 3, poolHalf, capacityByHalf)
	require.NoError(t, err)
	assert.Equal(t, []int{block1, block16}, drafted,
		"the sheet's draft slots read \"2nd of Block 1\" and \"2nd of Block 16\"; block 9 is oversized too and sends nothing")
}

// TestWKC17WomenTeamDraftSelectionMatchesTheSheet replays the 17WKC Women's
// Team draw, the event that decided the drafted-2nd rule.
//
// The sheet, verbatim: 16 blocks, of which 4, 5, 12 and 13 are draft slots
// reading "2nd of Block 9", "2nd of Block 16", "2nd of Block 1" and "2nd of
// Block 8". The footnote reads "Seed 1a: Japan, 16a: Korea / Seed 8a,9a:
// USA & Brasil by Draw". The four blocks that send a drafted 2nd are
// therefore exactly the four SEEDED blocks -- and blocks 8 and 9 hold three
// teams each, so two of the four are not oversized at all. This is what
// killed the original oversized-only candidate rule, which refused the whole
// shape (2 oversized pools, 4 slots to fill); when the sheet was first
// decoded, this test recorded that refusal as a divergence, and the operator
// ruling that "Fit the knockout" should implement WKC's own method turned it
// into the agreement test it now is.
//
// Dropping the two draft slots leaves pool indices 0..11 for blocks
// 1,2,3,6,7,8,9,10,11,14,15,16 -- so block 1 is pool 0, block 8 pool 5,
// block 9 pool 6 and block 16 pool 11. Selection order is seed order with
// the capacity check never binding (two slots per half, seeds alternating
// halves), so the drafted set AND its order are the seeds': 1, 16, 8, 9.
func TestWKC17WomenTeamDraftSelectionMatchesTheSheet(t *testing.T) {
	t.Parallel()

	// 12 real blocks: 1,2,3,6,7,8,9,10,11,14,15,16 -> pool indices 0..11.
	const (
		block1  = 0
		block8  = 5
		block9  = 6
		block16 = 11
	)
	pools := wkcBlocks(12, 3,
		[]int{block1, block16}, // only blocks 1 and 16 hold four teams
		map[int]int{block1: 1, block16: 2, block8: 3, block9: 4})
	require.Equal(t, 38, countWKCPlayers(pools), "sanity: this fixture is the sheet's 38-team field")

	poolHalf, capacityByHalf, ok := FillBracketDraftCapacity(pools, 4, 1)
	require.True(t, ok)

	drafted, err := SelectFillBracketDrafts(pools, 3, poolHalf, capacityByHalf)
	require.NoError(t, err,
		"the sheet's four drafts are supplied by its four seeded blocks; only two are oversized")
	assert.Equal(t, []int{block1, block16, block8, block9}, drafted,
		"the drafted blocks are the four seeded ones, in seed order, exactly as the sheet's draft slots and footnote state")

	// And the full draw those drafts feed: zero byes, opposite-half crossing
	// (TestWKCDraftedSecondsCrossToTheOppositeHalf asserts the crossing on
	// this same fixture).
	draw := BuildKnockoutDrawFillBracket(pools, drafted, 1)
	require.NotNil(t, draw)
	leaves := TreeToLeafArray(draw.Root)
	require.Len(t, leaves, 16)
	for i, l := range leaves {
		assert.NotEmptyf(t, l, "slot %d is a bye on a sheet with none", i)
	}
}

// TestWKCDraftedSecondsCrossToTheOppositeHalf is the rule the sheets confirm
// most strongly, on six independent observations across two championships:
// a drafted 2nd never sits in the same half of the bracket as its own block's
// winner.
//
//	19WKC Women's Team  slot  5 (half 1) <- 2nd of block 16 (half 2)
//	                    slot 12 (half 2) <- 2nd of block  1 (half 1)
//	17WKC Women's Team  slot  4 (half 1) <- 2nd of block  9 (half 2)
//	                    slot  5 (half 1) <- 2nd of block 16 (half 2)
//	                    slot 12 (half 2) <- 2nd of block  1 (half 1)
//	                    slot 13 (half 2) <- 2nd of block  8 (half 1)
//
// Both draws are built here and checked for that property, plus the zero byes
// the objective exists to produce.
//
// What is NOT asserted, deliberately: the exact slot index each draft lands
// on. The sheets keep their blocks contiguous (1-4, then 6-8, in the first
// half) while this package interleaves pools across halves, so the two
// disagree on ordering while agreeing on every structural claim above. Pinning
// the sheets' literal ordering would assert a convention this package does not
// have and never claimed to.
func TestWKCDraftedSecondsCrossToTheOppositeHalf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		pools    []Pool
		drafts   []int
		entrants int
	}{
		{
			name:     "19WKC Women's Team, 45 teams, 2 drafted 2nds",
			pools:    wkcBlocks(14, 3, []int{0, 7, 13}, map[int]int{0: 1, 13: 2, 6: 3, 7: 4}),
			drafts:   []int{0, 13},
			entrants: 45,
		},
		{
			name:     "17WKC Women's Team, 38 teams, 4 drafted 2nds",
			pools:    wkcBlocks(12, 3, []int{0, 11}, map[int]int{0: 1, 11: 2, 5: 3, 6: 4}),
			drafts:   []int{0, 5, 6, 11},
			entrants: 38,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.entrants, countWKCPlayers(tc.pools), "sanity: the sheet's field size")

			draw := BuildKnockoutDrawFillBracket(tc.pools, tc.drafts, 1)
			require.NotNil(t, draw)

			leaves := TreeToLeafArray(draw.Root)
			require.Equal(t, len(tc.pools)+len(tc.drafts), len(leaves),
				"winners plus drafted 2nds fill the bracket exactly")
			for i, l := range leaves {
				assert.NotEmptyf(t, l, "slot %d is a bye; this objective exists to produce none", i)
			}

			half := len(leaves) / 2
			sideOf := func(label string) int {
				for i, l := range leaves {
					if l == label {
						if i < half {
							return 0
						}
						return 1
					}
				}
				t.Fatalf("label %q is not in the draw: %v", label, leaves)
				return -1
			}

			for _, d := range tc.drafts {
				name := tc.pools[d].PoolName
				first := sideOf(name + "-1st")
				second := sideOf(name + "-2nd")
				assert.NotEqualf(t, first, second,
					"%s sends both its 1st and its 2nd to half %d; every drafted 2nd on both sheets crosses",
					name, first)
			}

			// And no pool sends more than the one winner plus, if drafted, one
			// runner-up: a sheet block never appears three times.
			counts := map[string]int{}
			for _, l := range leaves {
				counts[strings.SplitN(l, "-", 2)[0]]++
			}
			for _, p := range tc.pools {
				want := 1
				for _, d := range tc.drafts {
					if tc.pools[d].PoolName == p.PoolName {
						want = 2
					}
				}
				assert.Equalf(t, want, counts[p.PoolName], "%s occupies %d slots, want %d", p.PoolName, counts[p.PoolName], want)
			}
		})
	}
}

// TestWKCIndividualFormationAgainstTheSheets covers the four INDIVIDUAL
// events. They split cleanly by championship, and the split is the point.
//
// The 19WKC pair fill their brackets exactly, with the block count this
// package's objective picks: 203 entrants over 64 blocks and 242 over 64, both
// with no draft slot printed on the sheet and none needed.
//
// The 17WKC pair do NOT. 171 entrants are cut into 58 blocks and 205 into 70 --
// neither a power of two, so both draws carry byes -- and both sheets contain
// blocks of TWO competitors (three of them at 171, five at 205), which is
// below the minimum either of this package's formation paths will cut to. So
// the 17WKC individual events are not a fill-bracket draw at all, and are
// recorded here as the evidence that the objective is a CHOICE a federation
// makes per event rather than a universal rule.
func TestWKCIndividualFormationAgainstTheSheets(t *testing.T) {
	t.Parallel()

	t.Run("19WKC individuals fill the bracket exactly, as this package would cut them", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name     string
			entrants int
		}{
			{"Women's Individual, 203 entrants", 203},
			{"Men's Individual, 242 entrants", 242},
		} {
			pools, drafts, err := FillBracketPoolCount(tc.entrants, 3, 4)
			require.NoErrorf(t, err, tc.name)
			assert.Equalf(t, 64, pools, "%s: the sheet prints 64 blocks", tc.name)
			assert.Equalf(t, 0, drafts, "%s: the sheet prints no draft slot", tc.name)
		}
	})

	t.Run("17WKC individuals were not cut to fill the bracket", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			name           string
			entrants       int
			sheetBlocks    int
			sheetTwoBlocks int // blocks of only two competitors
		}{
			{"Women's Individual, 171 entrants", 171, 58, 3},
			{"Men's Individual, 205 entrants", 205, 70, 5},
		} {
			// The sheet's own block layout: two-competitor blocks plus
			// three-competitor blocks, accounting for every entrant.
			three := tc.sheetBlocks - tc.sheetTwoBlocks
			require.Equalf(t, tc.entrants, 2*tc.sheetTwoBlocks+3*three,
				"%s: the decoded block sizes must account for the whole field", tc.name)

			assert.NotEqualf(t, NextPow2(tc.sheetBlocks), tc.sheetBlocks,
				"%s: the sheet's block count is not a power of two, so its draw carries byes", tc.name)

			// Neither formation path this package offers produces that layout:
			// fill-bracket lands on a different block count, and minimum-size
			// cutting never emits a block below the minimum.
			fillPools, _, err := FillBracketPoolCount(tc.entrants, 3, 4)
			require.NoErrorf(t, err, tc.name)
			assert.NotEqualf(t, tc.sheetBlocks, fillPools,
				"%s: recorded as a divergence; if fill-bracket now reproduces the sheet, this case is stale", tc.name)
			assert.NotEqualf(t, tc.sheetBlocks, PoolCount(tc.entrants, 3, false),
				"%s: minimum-players-per-pool cutting does not reach the sheet's block count either", tc.name)
		}
	})
}
