package helper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// seededDraw runs the REAL create-pools pipeline for numPools pools of four on
// numCourts shiaijo, with the first numSeeds entrants seeded 1..numSeeds, and
// returns the pools and the draw built from them. Nothing here re-implements
// placement: PoolSeeding decides which pool each seed lands in and
// BuildKnockoutDraw decides where in the bracket its winner sits, exactly as
// cmd/create-pools does.
func seededDraw(t *testing.T, numPools, numSeeds, numCourts int) ([]Pool, *KnockoutDraw) {
	t.Helper()
	players := make([]Player, 0, numPools*4)
	for i := 0; i < numPools*4; i++ {
		p := Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo %03d", i+1)}
		if i < numSeeds {
			p.Seed = i + 1
		}
		players = append(players, p)
	}
	counted := PoolCount(len(players), 4, false)
	require.Equal(t, numPools, counted)

	players = referencePoolSeeding(players, counted, numCourts)
	pools, err := CreatePools(players, 4, false)
	require.NoError(t, err)
	pools = ReorderPoolsForCourts(pools, numCourts)

	courts := EffectiveDrawCourts(len(pools), numCourts)
	draw := BuildKnockoutDraw(pools, 2, courts)
	require.NotNil(t, draw)
	return pools, draw
}

// TestSeedPlacementWarningsSurplusRanks is R2's last bullet: more seeds than
// pools is NOT an error. The surplus ranks are ignored, off the bottom (D7
// protects the top seed most), and the operator is told which.
func TestSeedPlacementWarningsSurplusRanks(t *testing.T) {
	pools, draw := seededDraw(t, 3, 4, 2)

	warnings := SeedPlacementWarnings(draw, pools)
	require.NotEmpty(t, warnings, "4 seeds over 3 pools must warn")
	assert.Contains(t, warnings[0], "Seed 4 ignored")
	assert.Contains(t, warnings[0], "two seeds must never share a pool")
	assert.Contains(t, warnings[0], "3 pools for 4 seeds")
	assert.Contains(t, warnings[0], "The draw used seeds 1, 2 and 3.")
	for _, w := range warnings {
		assert.NotContains(t, strings.ToLower(w), "error")
		assert.NotContains(t, strings.ToLower(w), "cannot draw")
	}
}

// TestSeedPlacementWarningsRelaxedQuarter is D7's ladder in action: a
// constraint that cannot be honoured gives way and the operator is told, and it
// is never an error.
//
// The worked example is 4 seeds and 5 pools on ONE shiaijo. PoolSeeding spreads
// seeds over SHIAIJO, so with only one to spread over it puts seed 4 in the
// pool next to seed 1's, and both land in the draw's first block; the draw then
// cannot give them separate halves or separate quarters and says so. The same
// competition on TWO shiaijo now satisfies both constraints and warns about
// nothing -- see TestSeedPlacementWarningsSilentWhenSatisfiable -- because the
// pool set is subdivided into four blocks whatever the shiaijo count, so four
// distinct quarters exist to place four seeds in.
func TestSeedPlacementWarningsRelaxedQuarter(t *testing.T) {
	pools, draw := seededDraw(t, 5, 4, 1)

	warnings := SeedPlacementWarnings(draw, pools)
	require.Len(t, warnings, 2, "every seed has its own pool, so only halves and quarters give way: %v", warnings)
	assert.Contains(t, warnings[0], "could not be split into halves")
	assert.Contains(t, warnings[1], "own quarter of the draw")
	assert.Contains(t, warnings[1], "seeds 1 and 4")
	assert.Contains(t, warnings[1], "The draw was made anyway.")
}

// TestSeedPlacementWarningsSilentWhenSatisfiable pins the other half of the
// rule: a configuration the seeding rules CAN satisfy warns about nothing, and
// neither does a competition with no seeds at all ("Zero seeds MUST be a
// normal, warning-free configuration", D6).
func TestSeedPlacementWarningsSilentWhenSatisfiable(t *testing.T) {
	cases := []struct {
		name                          string
		numPools, numSeeds, numCourts int
	}{
		{"4 seeds on 4 shiaijo", 8, 4, 4},
		{"4 seeds on 2 shiaijo", 8, 4, 2},
		// D7's old worked example. Two shiaijo now carry four blocks, so all
		// four seeds get a quarter each and nothing gives way.
		{"4 seeds on 2 shiaijo over 5 pools", 5, 4, 2},
		{"2 seeds on 2 shiaijo", 6, 2, 2},
		{"1 seed", 6, 1, 2},
		{"no seeds", 6, 0, 2},
		{"no seeds on one shiaijo", 5, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pools, draw := seededDraw(t, tc.numPools, tc.numSeeds, tc.numCourts)
			assert.Empty(t, SeedPlacementWarnings(draw, pools))
		})
	}
}

// TestAnySeededAgreesWithSeedPlacementWarnings pins the invariant that
// engine.SeedWarnings' early return rests on: every warning here is about a
// seed, so a pool set AnySeeded rejects can never produce one, and skipping the
// draw build for it cannot swallow anything the operator needed. If a warning
// is ever added that fires without a seed, this is the test that catches it.
func TestAnySeededAgreesWithSeedPlacementWarnings(t *testing.T) {
	cases := []struct {
		name                          string
		numPools, numSeeds, numCourts int
		wantSeeded                    bool
	}{
		{"no seeds", 6, 0, 2, false},
		{"no seeds on one shiaijo", 5, 0, 1, false},
		{"one seed", 6, 1, 2, true},
		// The loudest configuration there is: more seeds than pools, which
		// warns about surplus ranks AND about spread it could not honour.
		{"four seeds over three pools on one shiaijo", 3, 4, 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pools, draw := seededDraw(t, tc.numPools, tc.numSeeds, tc.numCourts)
			require.Equal(t, tc.wantSeeded, AnySeeded(pools))
			if !tc.wantSeeded {
				assert.Empty(t, SeedPlacementWarnings(draw, pools),
					"an unseeded pool set must have nothing to say, or the early return would hide it")
			}
		})
	}
}

// TestSeedPlacementWarningsNilInputs covers the callers that ask before there
// is anything to answer with: no draw, no pools. Neither is an error and
// neither warns.
func TestSeedPlacementWarningsNilInputs(t *testing.T) {
	pools, draw := seededDraw(t, 4, 2, 2)
	assert.Nil(t, SeedPlacementWarnings(nil, pools))
	assert.Nil(t, SeedPlacementWarnings(&KnockoutDraw{}, pools))
	assert.Nil(t, SeedPlacementWarnings(draw, nil))
}

// TestSeedPlacementWarningsReportsSharedShiaijoOnlyWhenAvoidable is the
// noise guard on D7's third constraint. Two seeded pools per shiaijo is the
// CORRECT outcome on two shiaijo and four seeds (D6 says so outright), so it
// must not be reported as a relaxation; only a shiaijo count that could have
// given every seed its own is worth a warning.
func TestSeedPlacementWarningsReportsSharedShiaijoOnlyWhenAvoidable(t *testing.T) {
	pools, draw := seededDraw(t, 8, 4, 2)
	for _, w := range SeedPlacementWarnings(draw, pools) {
		assert.NotContains(t, w, "own shiaijo",
			"4 seeds on 2 shiaijo share shiaijo by design, not by relaxation")
	}
}

// gappedSeededDraw is seededDraw's sibling for a NON-contiguous seed set:
// each rank in ranks (any positive ints, need not be 1..len(ranks)) is
// assigned to one of the first len(ranks) players, in order. Distinct dojos
// throughout, same as seededDraw.
func gappedSeededDraw(t *testing.T, numPools int, ranks []int, numCourts int) ([]Pool, *KnockoutDraw) {
	t.Helper()
	players := make([]Player, 0, numPools*4)
	for i := 0; i < numPools*4; i++ {
		p := Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("Dojo %03d", i+1)}
		if i < len(ranks) {
			p.Seed = ranks[i]
		}
		players = append(players, p)
	}
	counted := PoolCount(len(players), 4, false)
	require.Equal(t, numPools, counted)

	players = referencePoolSeeding(players, counted, numCourts)
	pools, err := CreatePools(players, 4, false)
	require.NoError(t, err)
	pools = ReorderPoolsForCourts(pools, numCourts)

	courts := EffectiveDrawCourts(len(pools), numCourts)
	draw := BuildKnockoutDraw(pools, 2, courts)
	require.NotNil(t, draw)
	return pools, draw
}

// TestSeedPlacementWarningsWrappedSeedInSeedFreePoolIsSilent pins bc-pnum
// ruling 2b. Ranks {1, 2, 3, 5} over 4 pools, four distinct dojos (rank 4 is
// never assigned, so seed 5 WRAPS): 2a's fix lands wrapped seed 5 in the
// one genuinely seed-free pool (no dojo-mate conflict either, since every
// dojo here is distinct), so it never shares a pool with another seed and
// never trips the "ignored" warning.
//
// Before the 2b fix this still produced a SPURIOUS "Seeds could not be
// split into halves" warning: seedPools sorts placed seeds by rank
// ascending, so with only 4 seeds placed at all, rank 5 was the 4th
// ELEMENT of that slice, and the old COUNT-based truncation
// (`placed[:maxSeedRanks]`) kept it even though, at rank 5, it has no
// genuine half/quarter home in D6's structure to begin with -- that
// structure is only ever reported via the shared-pool "ignored" branch,
// which does not apply here since nothing shares a pool.
func TestSeedPlacementWarningsWrappedSeedInSeedFreePoolIsSilent(t *testing.T) {
	for _, numCourts := range []int{1, 2, 4} {
		t.Run(fmt.Sprintf("numCourts=%d", numCourts), func(t *testing.T) {
			pools, draw := gappedSeededDraw(t, 4, []int{1, 2, 3, 5}, numCourts)
			warnings := SeedPlacementWarnings(draw, pools)
			assert.Empty(t, warnings,
				"a wrapped seed placed in a genuinely seed-free pool must not trip the halves/quarters/shiaijo checks: %v", warnings)
		})
	}
}

// TestSeedPlacementWarningsSharedPoolStillWarns is the companion control: a
// wrapped seed that genuinely CANNOT avoid sharing a pool (every pool
// already holds a dojo-mate) still produces the "ignored" warning -- 2b only
// removes the spurious halves/quarters warning for the seed-free case, it
// does not silence the shared-pool case the rule exists to report.
func TestSeedPlacementWarningsSharedPoolStillWarns(t *testing.T) {
	// 2 pools, ranks {1, 2, 3}: rank 3 wraps and, with only 2 pools and both
	// already seeded, must share one of them.
	pools, draw := gappedSeededDraw(t, 2, []int{1, 2, 3}, 1)
	warnings := SeedPlacementWarnings(draw, pools)
	require.NotEmpty(t, warnings, "seed 3 cannot avoid sharing a pool with 1 or 2 pools available")
	assert.Contains(t, warnings[0], "ignored")
	assert.Contains(t, warnings[0], "two seeds must never share a pool")
}

func TestRankList(t *testing.T) {
	assert.Equal(t, "none", RankList(nil))
	assert.Equal(t, "1", RankList([]int{1}))
	assert.Equal(t, "1 and 2", RankList([]int{1, 2}))
	assert.Equal(t, "1, 2 and 3", RankList([]int{1, 2, 3}))
}

// Go half of the shared Go/JS golden table for the incomplete-seeding message:
// see the `_comment` in testdata/seed_gap_messages.json for why the table is
// shared. JS half: web-mobile/js/__tests__/seed_gap.test.jsx.
type seedGapCase struct {
	Why       string `json:"why"`
	Ranks     []int  `json:"ranks"`
	Diagnosis string `json:"diagnosis"`
}

func loadSeedGapGolden(t *testing.T) []seedGapCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "seed_gap_messages.json"))
	require.NoError(t, err)
	var table struct {
		Cases []seedGapCase `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	// Load-bearing: ranging over an empty table produces zero assertions and
	// no red, so a degraded file needs its own failure.
	require.NotEmpty(t, table.Cases,
		"testdata/seed_gap_messages.json parsed to zero cases: the mirror would assert nothing")
	return table.Cases
}

func TestSeedGapDiagnosis_GoldenTable(t *testing.T) {
	for _, tc := range loadSeedGapGolden(t) {
		t.Run(tc.Why, func(t *testing.T) {
			assignments := make([]domain.SeedAssignment, 0, len(tc.Ranks))
			for i, r := range tc.Ranks {
				assignments = append(assignments,
					domain.SeedAssignment{Name: fmt.Sprintf("P%d", i+1), SeedRank: r})
			}
			assert.Equal(t, tc.Diagnosis, SeedGapDiagnosis(assignments), tc.Why)
		})
	}
}

// Every case the golden table calls a gap must be one the validator actually
// refuses, and every case it calls clean must be one the validator accepts.
// Without this the diagnosis could describe a seeding nothing ever rejects (a
// warning for a state that draws fine) or stay silent on one that is refused
// with no explanation.
func TestSeedGapGoldenTableAgreesWithTheValidator(t *testing.T) {
	for _, tc := range loadSeedGapGolden(t) {
		t.Run(tc.Why, func(t *testing.T) {
			assignments := make([]domain.SeedAssignment, 0, len(tc.Ranks))
			for i, r := range tc.Ranks {
				assignments = append(assignments,
					domain.SeedAssignment{Name: fmt.Sprintf("P%d", i+1), SeedRank: r})
			}
			err := domain.ValidateAssignments(assignments)
			if tc.Diagnosis != "" {
				require.Error(t, err, "a diagnosed gap must be a seeding the validator refuses")
				assert.ErrorIs(t, err, domain.ErrInvalidSeedAssignments)
				return
			}
			// The silent cases split: contiguous ranks are accepted outright,
			// while duplicates and non-positive ranks are refused by a rule
			// that is NOT a gap and whose own words must reach the operator.
			if err != nil {
				assert.ErrorIs(t, err, domain.ErrInvalidSeedAssignments)
				assert.NotContains(t, err.Error(), "sequential without gaps",
					"a case the diagnosis stays silent on must not be refused AS a gap: %v", err)
			}
		})
	}
}

// TestSeedPlacementWarningsUsesTheDrawsOwnAllocation pins that the per-shiaijo
// check reads the allocation the draw was ASSEMBLED from, not one re-derived
// from the pool count.
//
// BuildKnockoutDrawFromAssignment exists because a real allocation can differ
// from AssignPoolsToCourts: the 34th EKC Junior Female sheet ran 7 pools as
// 2/1/2/2 where the derived answer is 2/2/2/1. Here 4 pools on 2 shiaijo derive
// to [0 0 1 1], and the draw is built from [0 1 0 1] instead. Pools A and C
// hold the two seeds, so they SHARE a shiaijo under the allocation actually
// used and sit on different ones under the derived allocation. Re-deriving
// therefore stays silent about a clash the draw really has.
func TestSeedPlacementWarningsUsesTheDrawsOwnAllocation(t *testing.T) {
	pools := make([]Pool, 4)
	for pi := range pools {
		pools[pi].PoolName = fmt.Sprintf("Pool %c", 'A'+pi)
		for i := 0; i < 4; i++ {
			pl := Player{Name: fmt.Sprintf("P%d-%d", pi, i), Dojo: fmt.Sprintf("Dojo %d-%d", pi, i)}
			// Seeds in pools A and C: adjacent courts under the derived
			// allocation, the SAME court under the one supplied below.
			if i == 0 && (pi == 0 || pi == 2) {
				pl.Seed = pi/2 + 1
			}
			pools[pi].Players = append(pools[pi].Players, pl)
		}
	}

	derived, err := AssignPoolsToCourts(len(pools), 2)
	require.NoError(t, err)
	require.Equal(t, []int{0, 0, 1, 1}, derived,
		"fixture assumes the derived allocation separates pools A and C")

	used := []int{0, 1, 0, 1}
	draw := BuildKnockoutDrawFromAssignment(pools, 2, used, 2)
	require.NotNil(t, draw)
	require.Equal(t, used, draw.PoolCourt(len(pools)),
		"the draw must report the allocation it was built from")

	warnings := SeedPlacementWarnings(draw, pools)
	joined := strings.Join(warnings, " | ")
	assert.Contains(t, joined, "own shiaijo",
		"seeds 1 and 2 share shiaijo A under the allocation the draw used, so the relaxation must be reported: %s", joined)
}
