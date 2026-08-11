package engine

import (
	"fmt"
	"sort"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// distinctCourts returns the sorted set of non-empty shiaijo labels in the
// given list, so a test can compare "which shiaijo did this phase use" without
// caring about match order or how many matches each court got.
func distinctCourts(labels []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range labels {
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

// poolPhaseCourts reads the shiaijo the saved pool matches actually run on.
func poolPhaseCourts(t *testing.T, store *state.Store, compID string) []string {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches, "the draw should have produced pool matches")
	labels := make([]string, 0, len(matches))
	for _, m := range matches {
		labels = append(labels, m.Court)
	}
	return distinctCourts(labels)
}

// bracketCourts reads the shiaijo the knockout bracket's regions occupy. Only
// the first round is read: later rounds merge regions, so their court labels
// are a subset by construction.
func bracketCourts(t *testing.T, store *state.Store, compID string) []string {
	t.Helper()
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	require.NotEmpty(t, bracket.Rounds, "the draw should have produced a bracket")
	labels := make([]string, 0, len(bracket.Rounds[0]))
	for _, m := range bracket.Rounds[0] {
		labels = append(labels, m.Court)
	}
	return distinctCourts(labels)
}

// TestPoolPhaseAndBracketAgreeOnTheShiaijoCount pins the derived-allocation
// half of R9 on the engine path: when the pool count cannot carry the shiaijo
// the operator allocated, the count steps DOWN, and both phases have to step
// down together.
//
// The defect: helper.BuildKnockoutDraw clamps internally (a court with no home
// pool would own an empty region) while the pool scheduler was handed the raw
// allocation, so the two phases disagreed. 10 competitors at PoolSize 4 in max
// mode on 4 shiaijo gives 3 pools: the pool phase ran on A, B and C while the
// bracket had two regions, A and B. Three is not merely a disagreement, it is
// an allocation R9 forbids outright ("wherever an allocation is DERIVED rather
// than chosen, the DERIVED value is what R9 validates", and "any clamp that
// LOWERS a court count MUST land on a power of two").
//
// legalShiaijoCount states the rule independently of the production validator,
// so this cannot agree with a broken clamp by construction.
func TestPoolPhaseAndBracketAgreeOnTheShiaijoCount(t *testing.T) {
	cases := []struct {
		desc     string
		players  int
		poolSize int
		mode     string
		courts   int
		// wantCourts is the number of shiaijo the draw should end up on,
		// worked out by hand from the pool count rather than recomputed with
		// the production clamp.
		wantCourts int
	}{
		// The reported case: 3 pools cannot carry 4 shiaijo, and 3 is illegal,
		// so the draw runs on 2.
		{"3 pools on 4 shiaijo", 10, 4, "max", 4, 2},
		// 5 pools on 8: stepping down to the pool count would give an illegal
		// 5, so it lands on 4.
		{"5 pools on 8 shiaijo", 18, 4, "max", 8, 4},
		{"6 pools on 8 shiaijo", 22, 4, "max", 8, 4},
		{"7 pools on 8 shiaijo", 26, 4, "max", 8, 4},
		// 3 pools on 2 shiaijo needs no clamp at all: the allocation fits.
		{"3 pools on 2 shiaijo", 10, 4, "max", 2, 2},
		// The ordinary case, where nothing is derived.
		{"4 pools on 4 shiaijo", 16, 4, "min", 4, 4},
		{"8 pools on 8 shiaijo", 32, 4, "min", 8, 8},
		// A single shiaijo is explicitly legal and never clamped.
		{"5 pools on 1 shiaijo", 18, 4, "max", 1, 1},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			players := make([]string, tc.players)
			for i := range players {
				players[i] = fmt.Sprintf("P%02d", i+1)
			}
			const compID = "clamp"
			createTestCompetition(t, store, compID, state.CompFormatMixed, tc.poolSize, func(c *state.Competition) {
				c.PoolSizeMode = tc.mode
				c.Courts = courtLabels(tc.courts)
			})
			saveTestParticipants(t, store, compID, players)
			require.NoError(t, eng.GenerateDraw(compID))

			pools, err := store.LoadPools(compID)
			require.NoError(t, err)

			poolCourts := poolPhaseCourts(t, store, compID)
			bracket := bracketCourts(t, store, compID)

			assert.Lenf(t, poolCourts, tc.wantCourts,
				"%d pools on %d shiaijo should run the pool phase on %d: got %v",
				len(pools), tc.courts, tc.wantCourts, poolCourts)
			assert.Truef(t, legalShiaijoCount(len(poolCourts)),
				"the DERIVED pool-phase allocation must itself be a power of two, got %d (%v)",
				len(poolCourts), poolCourts)
			assert.Equalf(t, bracket, poolCourts,
				"the pool phase and the bracket must run on the SAME shiaijo: bracket %v, pools %v",
				bracket, poolCourts)
		})
	}
}

// TestLeagueKeepsEveryAllocatedShiaijo is the carve-out the clamp must not
// break. A league is one pool, so a pool-count clamp would collapse it to a
// single shiaijo; its matches are spread across the whole allocation by the
// single-pool branch instead, which reads the competition's own court list.
func TestLeagueKeepsEveryAllocatedShiaijo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	players := make([]string, 12)
	for i := range players {
		players[i] = fmt.Sprintf("P%02d", i+1)
	}
	const compID = "league-courts"
	createTestCompetition(t, store, compID, state.CompFormatLeague, 12, func(c *state.Competition) {
		c.Courts = []string{"A", "B", "C"}
	})
	saveTestParticipants(t, store, compID, players)
	require.NoError(t, eng.GenerateDraw(compID))

	assert.Equal(t, []string{"A", "B", "C"}, poolPhaseCourts(t, store, compID),
		"a league runs on every shiaijo it was allocated; the count rule does not bind it")
}
