package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertValidRounds checks structural invariants for any round-robin schedule.
// - Sum of matches across rounds == expectedTotalMatches
// - No player index repeated in any single round
// - All player indices in [0, n)
// - A < B for each pair
// - No duplicate pairs across all rounds
func assertValidRounds(t *testing.T, rounds [][]IntPair, n int, expectedTotalMatches int) {
	t.Helper()

	totalMatches := 0
	seenPairs := make(map[IntPair]bool)

	for ri, round := range rounds {
		seenInRound := make(map[int]bool)
		for _, p := range round {
			// Normalize so A < B
			if p.A > p.B {
				p.A, p.B = p.B, p.A
			}

			// Bounds check
			require.True(t, p.A >= 0 && p.A < n,
				"round %d: player index %d out of [0,%d)", ri, p.A, n)
			require.True(t, p.B >= 0 && p.B < n,
				"round %d: player index %d out of [0,%d)", ri, p.B, n)

			// A < B (not same player)
			require.True(t, p.A < p.B,
				"round %d: pair (%d,%d) must have A < B", ri, p.A, p.B)

			// No player twice in same round
			assert.False(t, seenInRound[p.A],
				"round %d: player %d appears more than once", ri, p.A)
			assert.False(t, seenInRound[p.B],
				"round %d: player %d appears more than once", ri, p.B)
			seenInRound[p.A] = true
			seenInRound[p.B] = true

			// No duplicate pairs overall
			key := IntPair{A: p.A, B: p.B}
			assert.False(t, seenPairs[key],
				"pair (%d,%d) appears more than once", p.A, p.B)
			seenPairs[key] = true

			totalMatches++
		}
	}

	assert.Equal(t, expectedTotalMatches, totalMatches,
		"total match count: want %d, got %d", expectedTotalMatches, totalMatches)
}

// assertAllPairsPresent verifies every (i,j) pair with i<j appears in rounds.
func assertAllPairsPresent(t *testing.T, rounds [][]IntPair, n int) {
	t.Helper()
	present := make(map[IntPair]bool)
	for _, round := range rounds {
		for _, p := range round {
			if p.A > p.B {
				p.A, p.B = p.B, p.A
			}
			present[IntPair{A: p.A, B: p.B}] = true
		}
	}
	for i := range n {
		for j := i + 1; j < n; j++ {
			assert.True(t, present[IntPair{A: i, B: j}],
				"pair (%d,%d) missing from schedule", i, j)
		}
	}
}

// --- CircleMethodRounds tests ---

func TestCircleMethodRounds_EvenN(t *testing.T) {
	// n=4: expect 3 rounds of 2 matches each; 6 total = 4*3/2
	rounds := CircleMethodRounds(4)
	require.Len(t, rounds, 3, "n=4 should produce 3 rounds")
	for i, r := range rounds {
		assert.Len(t, r, 2, "round %d should have 2 matches", i)
	}
	assertValidRounds(t, rounds, 4, 6)
	assertAllPairsPresent(t, rounds, 4)
}

func TestCircleMethodRounds_OddN(t *testing.T) {
	// n=5: expect 5 rounds of 2 matches each; 10 total = 5*4/2
	rounds := CircleMethodRounds(5)
	require.Len(t, rounds, 5, "n=5 should produce 5 rounds")
	for i, r := range rounds {
		assert.Len(t, r, 2, "round %d should have 2 matches (1 bye)", i)
	}
	assertValidRounds(t, rounds, 5, 10)
	assertAllPairsPresent(t, rounds, 5)
}

func TestCircleMethodRounds_Six(t *testing.T) {
	// n=6: 5 rounds of 3 matches; 15 total
	rounds := CircleMethodRounds(6)
	require.Len(t, rounds, 5, "n=6 should produce 5 rounds")
	for i, r := range rounds {
		assert.Len(t, r, 3, "round %d should have 3 matches", i)
	}
	assertValidRounds(t, rounds, 6, 15)
	assertAllPairsPresent(t, rounds, 6)
}

func TestCircleMethodRounds_Eight(t *testing.T) {
	// n=8: 7 rounds of 4 matches; 28 total
	rounds := CircleMethodRounds(8)
	require.Len(t, rounds, 7, "n=8 should produce 7 rounds")
	for i, r := range rounds {
		assert.Len(t, r, 4, "round %d should have 4 matches", i)
	}
	assertValidRounds(t, rounds, 8, 28)
	assertAllPairsPresent(t, rounds, 8)
}

func TestCircleMethodRounds_Three(t *testing.T) {
	// n=3: 3 rounds of 1 match; 3 total
	rounds := CircleMethodRounds(3)
	require.Len(t, rounds, 3, "n=3 should produce 3 rounds")
	for i, r := range rounds {
		assert.Len(t, r, 1, "round %d should have 1 match (1 bye)", i)
	}
	assertValidRounds(t, rounds, 3, 3)
	assertAllPairsPresent(t, rounds, 3)
}

func TestCircleMethodRounds_Two(t *testing.T) {
	// n=2: 1 round of 1 match; pair (0,1)
	rounds := CircleMethodRounds(2)
	require.Len(t, rounds, 1, "n=2 should produce 1 round")
	require.Len(t, rounds[0], 1, "round 0 should have 1 match")
	assert.Equal(t, IntPair{A: 0, B: 1}, rounds[0][0])
	assertValidRounds(t, rounds, 2, 1)
	assertAllPairsPresent(t, rounds, 2)
}

func TestCircleMethodRounds_EdgeCases(t *testing.T) {
	assert.Nil(t, CircleMethodRounds(0), "n=0 should return nil")
	assert.Empty(t, CircleMethodRounds(1), "n=1 should return nil or empty")
}

// Additional parametric test to cover a range of sizes.
func TestCircleMethodRounds_Parametric(t *testing.T) {
	for n := 2; n <= 12; n++ {
		n := n
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			rounds := CircleMethodRounds(n)
			expectedTotal := n * (n - 1) / 2
			assertValidRounds(t, rounds, n, expectedTotal)
			assertAllPairsPresent(t, rounds, n)
		})
	}
}

// --- PathGraphRounds tests ---

func TestPathGraphRounds_Four(t *testing.T) {
	// n=4: matches (0,1),(1,2),(2,3) — 3 total
	// Round 0 (even-indexed): (0,1),(2,3)
	// Round 1 (odd-indexed):  (1,2)
	rounds := PathGraphRounds(4)
	require.Len(t, rounds, 2, "n=4 should produce 2 rounds")
	assert.Equal(t, []IntPair{{A: 0, B: 1}, {A: 2, B: 3}}, rounds[0], "round 0 mismatch")
	assert.Equal(t, []IntPair{{A: 1, B: 2}}, rounds[1], "round 1 mismatch")
	assertValidRounds(t, rounds, 4, 3)
}

func TestPathGraphRounds_Six(t *testing.T) {
	// n=6: matches (0,1),(1,2),(2,3),(3,4),(4,5) — 5 total, 2 rounds
	// Round 0: (0,1),(2,3),(4,5) — even-indexed (0,2,4)
	// Round 1: (1,2),(3,4)      — odd-indexed  (1,3)
	rounds := PathGraphRounds(6)
	require.Len(t, rounds, 2, "n=6 should produce 2 rounds")
	assertValidRounds(t, rounds, 6, 5)
	// spot-check round 0
	assert.Equal(t, []IntPair{{A: 0, B: 1}, {A: 2, B: 3}, {A: 4, B: 5}}, rounds[0])
	assert.Equal(t, []IntPair{{A: 1, B: 2}, {A: 3, B: 4}}, rounds[1])
}

func TestPathGraphRounds_Three(t *testing.T) {
	// n=3: matches (0,1),(1,2)
	// Round 0: (0,1)
	// Round 1: (1,2)
	rounds := PathGraphRounds(3)
	require.Len(t, rounds, 2, "n=3 should produce 2 rounds")
	assert.Equal(t, []IntPair{{A: 0, B: 1}}, rounds[0])
	assert.Equal(t, []IntPair{{A: 1, B: 2}}, rounds[1])
	assertValidRounds(t, rounds, 3, 2)
}

func TestPathGraphRounds_EdgeCases(t *testing.T) {
	assert.Nil(t, PathGraphRounds(0), "n=0 should return nil")
	assert.Nil(t, PathGraphRounds(1), "n=1 should return nil")

	// n=2: single match (0,1), only round 0
	rounds := PathGraphRounds(2)
	require.Len(t, rounds, 1, "n=2 should produce 1 round")
	assert.Equal(t, []IntPair{{A: 0, B: 1}}, rounds[0])
}
