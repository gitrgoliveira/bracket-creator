package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// D6's rank-to-court order, pinned at both levels: the unit tables below state
// the mapping for every supported court count, and the pipeline test after
// them states its operational content -- the top seeds fight where the closing
// bouts run. The 4-court order is DECODED from the EKF sheets by rank-matching
// seeded pools against the previous edition's results (spec D6's evidence
// table: the champion's pool on court B and the runner-up's on C in all four
// team draws of 2025 and 2026); 8 and 16 courts extrapolate the same
// inner-quarter reading, and 1 and 2 courts are untouched by it.

// TestSeedCourtOrderMatchesTheDecodedSheets writes the expected court for each
// seed rank out as literals, per court count, so the expectation cannot drift
// with the code under test.
func TestSeedCourtOrderMatchesTheDecodedSheets(t *testing.T) {
	cases := []struct {
		numCourts int
		courts    []int // indexed by rank-1
		note      string
	}{
		{1, []int{0, 0, 0, 0}, "one court holds every seed; quarters live inside its region"},
		{2, []int{0, 1, 0, 1}, "seeds 1 and 3 share court A, 2 and 4 share B (operator decision 2026-08-09; the inner-quarter order is invisible here)"},
		{4, []int{1, 2, 0, 3}, "the decoded EKF order: 1 -> B, 2 -> C, 3 -> A, 4 -> D"},
		{8, []int{2, 4, 0, 6}, "extrapolated inner-quarter reading: 1 -> C, 2 -> E, 3 -> A, 4 -> G"},
		{16, []int{4, 8, 0, 12}, "extrapolated inner-quarter reading: 1 -> E, 2 -> I, 3 -> A, 4 -> M"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d courts", tc.numCourts), func(t *testing.T) {
			for i, want := range tc.courts {
				assert.Equalf(t, want, seedCourtOrder(i, tc.numCourts),
					"seed %d on %d courts: %s", i+1, tc.numCourts, tc.note)
			}
			// Ranks past the 4th have no structure left to spread over and
			// take the round robin.
			for i := 4; i < 8; i++ {
				assert.Equalf(t, i%tc.numCourts, seedCourtOrder(i, tc.numCourts),
					"seed %d falls back to the round robin", i+1)
			}
		})
	}
}

// TestTopSeedsFightWhereTheClosingBoutsRun is the operational content of the
// inner-quarter order, asserted through the production pipeline (PoolSeeding
// -> CreatePools -> ReorderPoolsForCourts -> BuildKnockoutDraw) at the 34th
// EKC Men Team shape: the final runs on seed 1's shiaijo and the two
// semifinals run on seed 1's and seed 2's, so the expected finalists fight
// their whole campaign -- pools, regional rounds, semifinal, final -- without
// changing courts until the final itself brings seed 2 over.
//
// This is a COMPOSITION of two independently pinned rules (seedCourtOrder and
// CourtForSpan's middle-court closing bouts), asserted as a whole so a change
// to either that broke the alignment fails here by name.
func TestTopSeedsFightWhereTheClosingBoutsRun(t *testing.T) {
	const numPools, numCourts, numSeeds = 12, 4, 4

	pools := seededDrawPoolsN(t, numPools, numCourts, numSeeds)
	assignment, err := AssignPoolsToCourts(numPools, numCourts)
	require.NoError(t, err)

	courtOfSeed := map[int]string{}
	for i, p := range pools {
		if r := poolSeedRank(p); r <= numSeeds {
			courtOfSeed[r] = CourtLabel(assignment[i])
		}
	}
	require.Len(t, courtOfSeed, numSeeds, "every seed rank must land in a pool")

	assert.Equal(t, map[int]string{1: "B", 2: "C", 3: "A", 4: "D"}, courtOfSeed,
		"the decoded EKF rank-to-court order (spec D6 evidence table)")

	draw := BuildKnockoutDraw(pools, 2, numCourts)
	require.NotNil(t, draw)
	rounds := courtsByRound(draw)
	require.GreaterOrEqual(t, len(rounds), 2)
	final := rounds[len(rounds)-1]
	semis := rounds[len(rounds)-2]
	require.Len(t, final, 1)
	require.Len(t, semis, 2)

	assert.Equal(t, courtOfSeed[1], final[0],
		"the final runs on seed 1's shiaijo: the champion's campaign never changes courts")
	assert.ElementsMatch(t, []string{courtOfSeed[1], courtOfSeed[2]}, semis,
		"each semifinal runs on a top-two seed's shiaijo")
}
