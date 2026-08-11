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

						// D4: a block of q occupants grants exactly q mod 2
						// named byes, whoever R6 picked to receive one.
						for b, block := range drawByeUnits(draw) {
							q := len(TreeLeafLabels(block))
							if q <= 1 {
								continue
							}
							slots := TreeToLeafArray(block)
							byes := 0
							for i := 0; i+1 < len(slots); i += 2 {
								if (slots[i] == "") != (slots[i+1] == "") {
									byes++
								}
							}
							assert.Equalf(t, q%2, byes,
								"block %d: %d occupants must grant %d named byes at any seed count", b, q, q%2)
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

			warnings := SeedPlacementWarnings(draw, pools, numCourts)
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
