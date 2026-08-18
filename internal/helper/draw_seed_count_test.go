package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The seed count is the OPERATOR'S choice, any number from zero up (R1), and it
// MUST NOT change the shape of the draw (operator ruling, 2026-08-11).
//
// Everything else in this package tests the draw at exactly four seeds, or at
// none: the structural sweeps use makePools, whose pools hold no players at all,
// so poolSeedRank is MaxInt everywhere and R6's seed criterion never fires;
// draw_seed_bye_test.go drives the real pipeline but always sets ranks 1 to 4.
// Neither shape covers 1, 2, 3, or more than 4, and "four seeds" is not a
// configuration the tool may assume: a club event may seed nobody, and a large
// one may seed eight.
//
// The two properties below are what the ruling means in practice. Between them
// they say: seeds choose WHO stands in a slot, never WHICH slots exist.

// seededDrawPoolsN is seededDrawPools with the seed count as a parameter.
// numSeeds may be 0, and may exceed the pool count.
func seededDrawPoolsN(t *testing.T, numPools, numCourts, numSeeds int) []Pool {
	t.Helper()
	roster := drawGoldenRoster(numPools)
	require.LessOrEqualf(t, numSeeds, len(roster),
		"caller must skip a seed count the roster cannot supply (see rosterHolds)")
	for i := 1; i <= numSeeds; i++ {
		roster[i-1].Seed = i
	}
	seeded := PoolSeeding(roster, numPools, numCourts)
	pools, err := CreatePools(seeded, drawGoldenPoolSize, true)
	require.NoError(t, err)
	require.Len(t, pools, numPools)
	return ReorderPoolsForCourts(pools, numCourts)
}

// drawShapeSignature renders the STRUCTURE of a draw and nothing else: '#' for a
// slot holding a qualifier, '.' for an empty one. Two draws with the same
// signature have the same first-round pairings, the same byes, the same depth
// and the same path to the final; only the names differ.
func drawShapeSignature(root *Node) string {
	var b strings.Builder
	for _, l := range TreeToLeafArray(root) {
		if l == "" {
			b.WriteByte('.')
		} else {
			b.WriteByte('#')
		}
	}
	return b.String()
}

// rosterHolds reports whether the synthetic roster for numPools is large enough
// to carry numSeeds. The smallest fixture (2 pools) is 7 players, so the top of
// the sweep does not exist there; skipping is honest, where clamping would
// silently retitle an 8-seed case as a 7-seed one.
func rosterHolds(numPools, numSeeds int) bool {
	return numSeeds <= drawGoldenRosterSize(numPools)
}

// seedCounts spans the whole range the operator can set: none, fewer than the
// four D6 fully determines, exactly four, and more than four (including more
// than some of the swept pool counts, which exercises R2's surplus rule).
var seedCounts = []int{0, 1, 2, 3, 4, 5, 6, 8}

// TestSeedCountDoesNotChangeDrawShape is the ruling stated as a measurement.
//
// For each configuration the unseeded draw is the baseline, and every seeded
// draw of the same configuration must have an identical shape signature. The
// LABELS are expected to move -- that is R6 criterion 1 doing its job, handing a
// block's bye to a seeded pool's winner instead of to whatever pool order would
// have given it -- so only the structure is compared.
func TestSeedCountDoesNotChangeDrawShape(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for numPools := 2; numPools <= 10; numPools++ {
			for poolWinners := 1; poolWinners <= 3; poolWinners++ {
				name := fmt.Sprintf("%dpools_%dq_%dsj", numPools, poolWinners, numCourts)
				t.Run(name, func(t *testing.T) {
					base := BuildKnockoutDraw(
						seededDrawPoolsN(t, numPools, numCourts, 0), poolWinners, numCourts)
					require.NotNil(t, base)
					want := drawShapeSignature(base.Root)

					for _, numSeeds := range seedCounts[1:] {
						if !rosterHolds(numPools, numSeeds) {
							continue
						}
						pools := seededDrawPoolsN(t, numPools, numCourts, numSeeds)
						draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
						require.NotNilf(t, draw, "%d seeds must still produce a draw", numSeeds)
						assert.Equalf(t, want, drawShapeSignature(draw.Root),
							"%d seeds changed the shape of the draw; seeds may only decide who stands in a slot",
							numSeeds)
						assert.Equalf(t, base.NumCourts(), draw.NumCourts(),
							"%d seeds changed the shiaijo count of the draw", numSeeds)
					}
				})
			}
		}
	}
}

// TestStructuralRulesHoldAtEverySeedCount is the other half: the draw must be
// COMPLIANT at every seed count, not merely the same shape.
//
// Compliance is not implied by the shape test. Choosing the bye changes which
// occupant is removed from the round-1 layer before the rest are paired up
// (buildBlock), so a different bye means a different set of pairings, which is
// exactly what R5's quarter rule and D4's bye arithmetic are about. Both are
// therefore re-measured with seeds present rather than inferred from the
// unseeded sweeps.
func TestStructuralRulesHoldAtEverySeedCount(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		for numPools := 2; numPools <= 10; numPools++ {
			for poolWinners := 1; poolWinners <= 4; poolWinners++ {
				for _, numSeeds := range seedCounts {
					if !rosterHolds(numPools, numSeeds) {
						continue
					}
					name := fmt.Sprintf("%dpools_%dq_%dsj_%dseeds", numPools, poolWinners, numCourts, numSeeds)
					t.Run(name, func(t *testing.T) {
						pools := seededDrawPoolsN(t, numPools, numCourts, numSeeds)
						poolNames := make([]string, len(pools))
						for i, p := range pools {
							poolNames[i] = p.PoolName
						}
						draw := BuildKnockoutDraw(pools, poolWinners, numCourts)
						require.NotNil(t, draw)

						// R5: no two qualifiers of one pool in a quarter, which
						// the rule claims from 3 qualifiers up.
						if poolWinners >= 3 {
							assert.Empty(t, samePoolQuarterClashes(draw.Root, poolNames),
								"R5 must hold with seeds set, not only in the unseeded sweep")
						}

						// D4: the block's named-bye count follows its layout
						// mode (blockLayoutArithmetic), whoever R6 picked to
						// receive one. Vacancy blocks are pinned by
						// TestEKC2025MenTeamByes instead.
						for b, block := range drawByeUnits(draw) {
							leaves := TreeLeafLabels(block)
							q := len(leaves)
							if q <= 1 {
								continue
							}
							wantByes, _, skip := blockLayoutArithmetic(leaves)
							if skip {
								continue
							}
							slots := TreeToLeafArray(block)
							byes := 0
							for i := 0; i+1 < len(slots); i += 2 {
								if (slots[i] == "") != (slots[i+1] == "") {
									byes++
								}
							}
							assert.Equalf(t, wantByes, byes,
								"block %d: %d occupants must grant %d named byes at any seed count", b, q, wantByes)
						}
					})
				}
			}
		}
	}
}

// TestSeedWarningsScaleWithTheSeedCount pins the reporting end of R1: a
// competition may set any number of seeds, and the tool complains only when the
// configuration genuinely cannot hold them.
//
// Two seeds may never share a pool (R2), so the ONLY count that forces a
// complaint is more seeds than pools. Everything at or below the pool count must
// be silent, including zero.
func TestSeedWarningsScaleWithTheSeedCount(t *testing.T) {
	const numPools, numCourts = 6, 2
	for _, numSeeds := range seedCounts {
		t.Run(fmt.Sprintf("%d_seeds", numSeeds), func(t *testing.T) {
			pools := seededDrawPoolsN(t, numPools, numCourts, numSeeds)
			draw := BuildKnockoutDraw(pools, 2, numCourts)
			require.NotNil(t, draw)

			warnings := SeedPlacementWarnings(draw, pools)
			if numSeeds <= numPools {
				assert.Emptyf(t, warnings,
					"%d seeds fit in %d pools, so there is nothing to warn about", numSeeds, numPools)
				return
			}
			require.NotEmptyf(t, warnings,
				"%d seeds cannot fit in %d distinct pools and the operator must be told", numSeeds, numPools)
			assert.Contains(t, warnings[0], "ignored",
				"the warning has to say which ranks were dropped: %v", warnings)
		})
	}
}

// TestSeedIndexEqualsRankMinusOne pins that a seed set entering the draw
// CONTIGUOUS comes out of pool creation contiguous: every rank survives, exactly
// once, with no gap opened along the way.
//
// It used to carry more weight than that. PoolSeeding passed each seed's INDEX
// in the rank-sorted list to seedCourtOrder, whose rules are written in RANKS
// ("seed 1 -> A, seed 2 -> C"), and the two coincided only for a contiguous set.
// This test was meant to be the tripwire for that assumption, and it could not
// be: it builds its own seed sets, and every fixture in this package builds them
// contiguous. The gapped set arrives from OUTSIDE the package --
// engine.dropSeedAssignments drops a non-checked-in seed's assignment after the
// validating load -- so the tripwire never fired while ranks 3 and 4 were being
// placed in the wrong quarters.
//
// PoolSeeding now keys on p.Seed-1, so index == rank-1 is no longer load-bearing
// anywhere. The gapped case is pinned where it can actually be seen:
// TestPoolSeedingPlacesByRankNotByPosition (seed_test.go) at this level, and
// TestGenerateDraw_SeedGapFromCheckInKeepsD6Placement in the engine package,
// which drives the check-in drop that produces the gap.
func TestSeedIndexEqualsRankMinusOne(t *testing.T) {
	for _, numSeeds := range seedCounts {
		if numSeeds == 0 {
			continue
		}
		t.Run(fmt.Sprintf("%d_seeds", numSeeds), func(t *testing.T) {
			pools := seededDrawPoolsN(t, 8, 2, numSeeds)

			ranks := []int{}
			for _, p := range pools {
				for _, pl := range p.Players {
					if pl.Seed > 0 {
						ranks = append(ranks, pl.Seed)
					}
				}
			}
			require.Len(t, ranks, numSeeds, "every seed set must survive pool creation")

			seen := map[int]bool{}
			for _, r := range ranks {
				assert.Falsef(t, seen[r], "rank %d assigned twice", r)
				seen[r] = true
			}
			for r := 1; r <= numSeeds; r++ {
				assert.Truef(t, seen[r],
					"ranks must be contiguous from 1, so a seed's index is its rank minus one; rank %d is missing", r)
			}
		})
	}
}

// poolsOfSizes builds len(sizes) pools holding the given numbers of players.
// The names are synthetic; only the COUNT per pool matters here.
func poolsOfSizes(sizes []int) []Pool {
	pools := make([]Pool, len(sizes))
	for i, n := range sizes {
		pools[i] = Pool{PoolName: fmt.Sprintf("Pool %c", 'A'+i)}
		pools[i].Players = make([]Player, n)
		for j := range pools[i].Players {
			pools[i].Players[j] = Player{Name: fmt.Sprintf("p%d-%d", i, j)}
		}
	}
	return pools
}

// TestPoolSizesDoNotChangeDrawShape completes the statement of what the tree
// depends on, and it is shorter than it looks:
//
//	shape = f(pool count, qualifiers per pool, shiaijo count)
//
// Pool SIZES are not in it. They feed R6 criterion 2, the oversized-pool bye
// (D1's "whose qualifier plays more pool matches"), which chooses WHICH occupant
// receives a block's bye and never how many byes there are or where they fall.
// So they behave exactly as seeds do: they decide who stands in a slot, not
// which slots exist.
//
// This matters for an operator's mental model. The bracket can be drawn, printed
// and handed out as soon as the pools are formed, because nothing that happens
// after that -- not the pool sizes settling, not the seeds, not a single result
// -- can reshape it. Only the names change.
func TestPoolSizesDoNotChangeDrawShape(t *testing.T) {
	// Six pools throughout; only the sizes vary, from perfectly even to
	// pathologically lopsided.
	variants := [][]int{
		{3, 3, 3, 3, 3, 3},
		{4, 4, 4, 3, 3, 3},
		{7, 3, 3, 3, 3, 3},
		{9, 8, 7, 6, 5, 4},
		{2, 2, 2, 2, 2, 9},
	}
	for _, numCourts := range []int{1, 2, 4} {
		for poolWinners := 1; poolWinners <= 3; poolWinners++ {
			name := fmt.Sprintf("%dq_%dsj", poolWinners, numCourts)
			t.Run(name, func(t *testing.T) {
				base := BuildKnockoutDraw(poolsOfSizes(variants[0]), poolWinners, numCourts)
				require.NotNil(t, base)
				want := drawShapeSignature(base.Root)

				for _, sizes := range variants[1:] {
					draw := BuildKnockoutDraw(poolsOfSizes(sizes), poolWinners, numCourts)
					require.NotNil(t, draw)
					assert.Equalf(t, want, drawShapeSignature(draw.Root),
						"pool sizes %v changed the shape of the draw; sizes may only decide who receives a bye", sizes)
				}
			})
		}
	}
}
