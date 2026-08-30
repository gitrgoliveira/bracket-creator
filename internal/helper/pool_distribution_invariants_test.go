package helper

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file was Phase 0 of the region-aware single-pass distribution plan
// on bc-dojo: the baseline the Phase 3 decision gate trusted, pinning what
// the OLD fill+repair pipeline guaranteed (so the rebuild could not
// silently regress it) and the identity contract that keeps the goldens and
// the estimator still. The sweeps were run as throwaway probes during the
// bc-dojo review; the sizes recorded here reproduce those measurements.
//
// Phase 4 swapped the region-aware distributor into production: every test
// below calls BuildPoolPhase, which now DELEGATES to
// BuildPoolPhaseTreeAware (pool_distribution_tree_aware.go), so this file's
// sweeps exercise the production path automatically rather than the old
// fill+repair pipeline they were written against. They still pass -- the
// tree-aware distributor keeps every invariant the old pipeline guaranteed,
// which is exactly what the Phase 3 gate (pool_distribution_gate_test.go)
// verified before the swap landed. What used to be, further down, a
// known-gap characterization of the one defect the rebuild existed to close
// is now a closed-gap regression pin instead (see
// TestPoolQualifiers_KnownGap_ClubWinnersCanMeetInRoundOne's own doc
// comment).

// buildClubRoster returns numPools*poolSize players: nClubs clubs of
// clubSize (grouped first, the order operators paste), unique-dojo fillers
// after, and the first nSeeds players seeded 1..nSeeds.
func buildClubRoster(numPools, poolSize, nClubs, clubSize, nSeeds int) []Player {
	n := numPools * poolSize
	r := make([]Player, 0, n)
	for c := 0; c < nClubs; c++ {
		for i := 1; i <= clubSize; i++ {
			r = append(r, Player{Name: fmt.Sprintf("C%d_%d", c, i), Dojo: fmt.Sprintf("Club%d", c)})
		}
	}
	for i := len(r) + 1; i <= n; i++ {
		r = append(r, Player{Name: fmt.Sprintf("O%d", i), Dojo: fmt.Sprintf("D%02d", i)})
	}
	for s := 0; s < nSeeds && s < len(r); s++ {
		r[s].Seed = s + 1
	}
	return r
}

func maxOfDojo(pools []Pool, dojo string) int {
	m := 0
	for _, p := range pools {
		if k := countDojoInPool(p, dojo); k > m {
			m = k
		}
	}
	return m
}

// TestPoolDistribution_Invariants sweeps 2048 multi-club rosters (the sweep
// that measured the fill+repair pipeline at 2048/2048 optimal) and asserts
// everything the rebuild must not lose:
//   - every club's worst per-pool concentration is ceil(clubSize/numPools),
//     the arithmetic optimum;
//   - pool sizes always equal the target sizes (nobody lost or duplicated);
//   - no two seeds share a pool.
func TestPoolDistribution_Invariants(t *testing.T) {
	total := 0
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			for courts := 1; courts <= 2; courts++ {
				for nClubs := 2; nClubs <= 4; nClubs++ {
					for clubSize := 2; clubSize <= numPools+2; clubSize++ {
						for nSeeds := 0; nSeeds <= 4 && nSeeds < numPools; nSeeds++ {
							if nClubs*clubSize > numPools*poolSize {
								continue
							}
							r := buildClubRoster(numPools, poolSize, nClubs, clubSize, nSeeds)
							pools, _, err := BuildPoolPhase(r, poolSize, false, courts)
							require.NoError(t, err)
							total++

							placed := 0
							for _, p := range pools {
								placed += len(p.Players)
								seeds := 0
								for _, pl := range p.Players {
									if pl.Seed > 0 {
										seeds++
									}
								}
								assert.LessOrEqual(t, seeds, 1, "two seeds share %s (pools=%d size=%d clubs=%dx%d seeds=%d)",
									p.PoolName, numPools, poolSize, nClubs, clubSize, nSeeds)
							}
							assert.Equal(t, numPools*poolSize, placed, "player lost or duplicated")

							optimum := (clubSize + numPools - 1) / numPools
							for c := 0; c < nClubs; c++ {
								dojo := fmt.Sprintf("Club%d", c)
								assert.LessOrEqualf(t, maxOfDojo(pools, dojo), optimum,
									"%s over-concentrated (pools=%d size=%d clubs=%dx%d seeds=%d)",
									dojo, numPools, poolSize, nClubs, clubSize, nSeeds)
							}
						}
					}
				}
			}
		}
	}
	require.GreaterOrEqual(t, total, 2000, "sweep shrank; the baseline is meaningless if it no longer covers the measured space")
}

// TestPoolDistribution_SeededClubSweep is the 210-config sweep with every
// seed drawn from ONE club plus unseeded club-mates: the shape where seed
// placement (fixed, EKC/D7 tree contract) and dojo spreading have to
// coexist. Two same-dojo seeds must never share a pool, and the club's
// spread must still reach the optimum around the immovable seeds.
func TestPoolDistribution_SeededClubSweep(t *testing.T) {
	total := 0
	for numPools := 3; numPools <= 7; numPools++ {
		for poolSize := 3; poolSize <= 5; poolSize++ {
			for nSeeds := 2; nSeeds <= 4 && nSeeds <= numPools; nSeeds++ {
				for clubExtra := 0; clubExtra <= 4; clubExtra++ {
					n := numPools * poolSize
					if nSeeds+clubExtra > n {
						continue
					}
					r := make([]Player, 0, n)
					for s := 1; s <= nSeeds; s++ {
						r = append(r, Player{Name: fmt.Sprintf("Seed%d", s), Dojo: "SeedClub", Seed: s})
					}
					for i := 1; i <= clubExtra; i++ {
						r = append(r, Player{Name: fmt.Sprintf("Mate%d", i), Dojo: "SeedClub"})
					}
					for i := len(r) + 1; i <= n; i++ {
						r = append(r, Player{Name: fmt.Sprintf("O%d", i), Dojo: fmt.Sprintf("D%02d", i)})
					}
					pools, _, err := BuildPoolPhase(r, poolSize, false, 2)
					require.NoError(t, err)
					total++

					clubSize := nSeeds + clubExtra
					optimum := (clubSize + numPools - 1) / numPools
					assert.LessOrEqualf(t, maxOfDojo(pools, "SeedClub"), optimum,
						"seeded club over-concentrated (pools=%d size=%d seeds=%d extra=%d)",
						numPools, poolSize, nSeeds, clubExtra)
					for _, p := range pools {
						seeds := 0
						for _, pl := range p.Players {
							if pl.Seed > 0 {
								seeds++
							}
						}
						assert.LessOrEqual(t, seeds, 1, "two same-dojo seeds share %s", p.PoolName)
					}
				}
			}
		}
	}
	require.GreaterOrEqual(t, total, 200, "sweep shrank below the measured 210-config space")
}

// TestPoolDistribution_UniqueDojoIdentity pins the identity contract the
// rebuild must keep BYTE-FOR-BYTE: a roster of all-unique dojos, in
// ascending dojo order, distributes as the exact round-robin deal (player i
// to pool i%numPools, in order). Three consumers depend on this staying
// true: draw_shapes.json's golden roster, estimateMixed's synthetic roster
// (pool SIZES must match the real draw), and every example workbook with a
// conflict-free roster. Ascending order matters: PoolSeeding sorts the
// unseeded by dojo name when counts tie, so only an already-sorted roster
// is order-preserved -- which is exactly how those three consumers build
// theirs.
func TestPoolDistribution_UniqueDojoIdentity(t *testing.T) {
	for _, numPools := range []int{3, 5, 8} {
		for _, poolSize := range []int{3, 4} {
			n := numPools * poolSize
			r := make([]Player, n)
			for i := range r {
				r[i] = Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo %03d", i+1)}
			}
			pools, _, err := BuildPoolPhase(r, poolSize, false, 2)
			require.NoError(t, err)

			// Undo ReorderPoolsForCourts' renaming by matching each player
			// back to the deal: player i must share a pool with every player
			// j where j%numPools == i%numPools, and with nobody else.
			poolOf := map[string]string{}
			for _, p := range pools {
				for _, pl := range p.Players {
					poolOf[pl.Name] = p.PoolName
				}
			}
			for i := 0; i < n; i++ {
				for j := i + 1; j < n; j++ {
					same := poolOf[r[i].Name] == poolOf[r[j].Name]
					want := i%numPools == j%numPools
					assert.Equal(t, want, same,
						"pools=%d size=%d: players %d and %d break the round-robin deal", numPools, poolSize, i, j)
				}
			}
		}
	}
}

// TestPoolQualifiers_KnownGap_ClubWinnersCanMeetInRoundOne is the CLOSED-GAP
// regression pin for the defect the region-aware rebuild (bc-dojo) exists
// to close. It used to pin the defect AS CURRENT BEHAVIOUR (the same
// known-limitation pattern the Swiss standings fix used): the pre-Phase-4
// pool distributor was blind to which part of the knockout tree each pool
// feeds, so in these two configurations two club-mates in DIFFERENT pools
// were drawn to meet in the FIRST knockout match even though the tree could
// hold them apart until round 2 (the brute-force ceiling for both shapes,
// see bruteForceMeetingCeiling in the gate test). Now that BuildPoolPhase
// delegates to the tree-aware distributor (bc-dojo Phase 4,
// pool_distribution_tree_aware.go), both shapes reach that ceiling exactly,
// and this test's job flips from documenting the gap to guarding against
// its return: a regression that makes either shape meet at round 1 again
// must fail loudly here.
//
// HISTORY, recorded because the first version of this test got it wrong:
// it originally pinned {4,3}, {4,4} and {5,4} as "the defect the rebuild
// exists to close". Exhaustive brute force during Phase 3 proved those three
// shapes are PIGEONHOLE-LIMITED -- the club occupies more than half the
// qualifying pools, so at one winner per pool some round-1 pair must be
// entirely the club's own no matter which pools are chosen. Their ceiling is
// 1 for ANY algorithm; they are pinned separately below as ceilings, not
// gaps.
func TestPoolQualifiers_KnownGap_ClubWinnersCanMeetInRoundOne(t *testing.T) {
	cases := []struct{ numPools, clubSize int }{
		{7, 3}, {7, 4},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("pools=%d club=%d", tc.numPools, tc.clubSize), func(t *testing.T) {
			earliest, ceiling := clubMeetingVsCeiling(t, tc.numPools, tc.clubSize)
			require.Equal(t, 2, ceiling, "fixture drifted: this shape's ceiling should be round 2")
			assert.Equal(t, ceiling, earliest,
				"CLOSED-GAP REGRESSION: the tree-aware distributor used to reach round %d here; a value below that means the region-aware rebuild regressed", ceiling)
		})
	}
}

// TestPoolQualifiers_PigeonholeCeilingIsRoundOne pins the three shapes the
// known-gap test ORIGINALLY blamed on the distributor, as what they really
// are: mathematical ceilings. The club spans more than half the qualifying
// pools, so a round-1 club pairing is forced for any algorithm; the honest
// assertion is that the ceiling itself is 1, and no rebuild may be judged
// against these shapes.
func TestPoolQualifiers_PigeonholeCeilingIsRoundOne(t *testing.T) {
	cases := []struct{ numPools, clubSize int }{
		{4, 3}, {4, 4}, {5, 4},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("pools=%d club=%d", tc.numPools, tc.clubSize), func(t *testing.T) {
			earliest, ceiling := clubMeetingVsCeiling(t, tc.numPools, tc.clubSize)
			assert.Equal(t, 1, ceiling, "the pigeonhole argument no longer holds for this shape; re-derive before trusting either assertion")
			assert.Equal(t, 1, earliest)
		})
	}
}

// clubMeetingVsCeiling runs the shipped pipeline for one club of clubSize
// over numPools pools of 4 at two shiaijo, one winner per pool, and returns
// the earliest knockout round two of the club's qualifying pools are drawn
// to meet, alongside the shape's brute-force ceiling.
//
// Calls BuildPoolPhaseTreeAware directly with poolWinners=1, not
// BuildPoolPhase: BuildPoolPhase (post bc-dojo Phase 4) delegates with a
// FIXED poolWinners=2 (its own documented default for a caller with no real
// qualifier count to hand), so calling it here while measuring against
// poolWinners=1 would score placement against one tree and the round count
// against another. A real poolWinners=1 competition reaches production
// through BuildPoolPhaseTreeAwareWithMode with its own real value, never
// through plain BuildPoolPhase, so this is also the more faithful stand-in
// for what this shape's actual draw would do.
func clubMeetingVsCeiling(t *testing.T, numPools, clubSize int) (earliest, ceiling int) {
	t.Helper()
	r := buildClubRoster(numPools, 4, 1, clubSize, 0)
	pools, drawCourts, err := BuildPoolPhaseTreeAware(r, 4, false, 2, 1)
	require.NoError(t, err)
	earliest = earliestClubMeetingRound(pools, 1, drawCourts, "Club0")
	_, sizes, err := poolTargetSizes(len(r), 4, false)
	require.NoError(t, err)
	span := clubSize
	if span > numPools {
		span = numPools
	}
	ceiling = bruteForceMeetingCeiling(sizes, 1, drawCourts, span)
	return earliest, ceiling
}

// TestPoolDistribution_RealRosters_MultiClub pins the multi-club behaviour of
// the tree-aware distributor on the two committed rosters, measured through
// the REAL knockout draw (earliestClubMeetingRound builds it and reads the
// tree) -- an earlier version of this pin scored post-reorder pools against
// the pre-reorder slot map and produced numbers wrong enough to briefly
// convict the repair loop of regressions it did not have. Measure through
// the draw or not at all.
//
// The large roster is the headline: the pre-swap pipeline sent TEN clubs
// into a round-1 club-mate meeting; the one-pass chooser alone cut that to
// one (Team Xi, dropped from round 5 to round 1 -- the case that justified
// improveClubMeetings); with the repair loop, ZERO clubs open the knockout
// against a club-mate and Xi recovers to round 4. Disabling the repair loop
// reddens both assertions (verified).
//
// The small roster is a floor, not a win: pool capacities 4/3/3 over three
// pools force clubs onto the round-1 pool pair whatever the assignment, and
// old and new both land at three such clubs (sum 7 both). The pin only
// guards the count against growing.
func TestPoolDistribution_RealRosters_MultiClub(t *testing.T) {
	cases := []struct {
		file            string
		poolMin, courts int
		maxRoundOne     int
		pins            map[string]int // dojo -> minimum earliest meeting round
	}{
		{"../../test-data/mock_data_small.csv", 3, 1, 3, nil},
		{"../../test-data/mock_data_large_zekken.csv", 3, 2, 0,
			map[string]int{"Team Xi": 2}},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			ps := loadDistributionRoster(t, c.file)
			pools, _, err := BuildPoolPhaseTreeAware(ps, c.poolMin, false, c.courts, 2)
			require.NoError(t, err)

			roundOne := 0
			seen := map[string]bool{}
			for _, p := range pools {
				for _, pl := range p.Players {
					if seen[pl.Dojo] {
						continue
					}
					seen[pl.Dojo] = true
					if m := earliestClubMeetingRound(pools, 2, c.courts, pl.Dojo); m < math.MaxInt/2 && m <= 1 {
						roundOne++
						t.Logf("round-1 club: %q", pl.Dojo)
					}
				}
			}
			assert.LessOrEqualf(t, roundOne, c.maxRoundOne,
				"more clubs open the knockout against a club-mate than the design allows on this roster")
			for dojo, minRound := range c.pins {
				got := earliestClubMeetingRound(pools, 2, c.courts, dojo)
				assert.GreaterOrEqualf(t, got, minRound,
					"%s must not open the knockout against a club-mate", dojo)
			}
		})
	}
}

// loadDistributionRoster reads a committed test-data roster: 2-column
// (Name, Dojo) or 3-column zekken (Name, Zekken, Dojo).
func loadDistributionRoster(t *testing.T, path string) []Player {
	t.Helper()
	fh, err := os.Open(path) // #nosec G304 -- test-only, fixed test-data path
	require.NoError(t, err)
	defer func() { _ = fh.Close() }()
	rd := csv.NewReader(fh)
	rd.FieldsPerRecord = -1
	recs, err := rd.ReadAll()
	require.NoError(t, err)
	var ps []Player
	for _, rec := range recs {
		if len(rec) < 2 {
			continue
		}
		dojo := strings.TrimSpace(rec[1])
		if len(rec) >= 3 {
			dojo = strings.TrimSpace(rec[2])
		}
		ps = append(ps, Player{Name: strings.TrimSpace(rec[0]), Dojo: dojo})
	}
	return ps
}
