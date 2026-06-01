package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEstimateMatchCounts_PoolMatchCountPerPoolSize pins the match count
// produced by each pool-format variant for various pool sizes.
//
// This test documents the exact semantics of each sub-path so that a
// future change to CreatePoolMatches / CreatePoolRoundRobinMatches /
// CreatePartialPoolMatches is caught here before it silently drifts from
// the estimator.
func TestEstimateMatchCounts_PoolMatchCountPerPoolSize(t *testing.T) {
	tests := []struct {
		name        string
		poolSize    int
		roundRobin  bool
		poolFormat  string // "" | "full" | "partial"
		wantPerPool int
	}{
		// -- Full round-robin (poolFormat="" or "full", roundRobin=true) --
		// C(n,2) = n*(n-1)/2
		{"rr size 2", 2, true, "", 1},
		{"rr size 3", 3, true, "", 3},
		{"rr size 4", 4, true, "", 6},
		{"rr size 5", 5, true, "", 10},
		{"rr size 6", 6, true, "", 15},
		// full format explicit
		{"rr full size 4", 4, true, "full", 6},

		// -- Non-RR (poolFormat="" or "full", roundRobin=false) --
		// CreatePoolMatches: size 2→1, 3→3, 4→4, N≥5→N
		{"non-rr size 2", 2, false, "", 1},
		{"non-rr size 3", 3, false, "", 3},
		{"non-rr size 4", 4, false, "", 4},
		{"non-rr size 5", 5, false, "", 5},
		{"non-rr size 6", 6, false, "", 6},
		{"non-rr size 7", 7, false, "", 7},

		// -- Partial round-robin (poolFormat="partial") --
		// Adjacent-pair: N-1 matches per pool
		{"partial size 2", 2, false, "partial", 1},
		{"partial size 3", 3, false, "partial", 2},
		{"partial size 4", 4, false, "partial", 3},
		{"partial size 5", 5, false, "partial", 4},
		{"partial size 6", 6, false, "partial", 5},
		// roundRobin flag ignored when poolFormat is partial
		{"partial size 4 rr-ignored", 4, true, "partial", 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := poolMatchesPerPool(tc.poolSize, tc.roundRobin, tc.poolFormat)
			assert.Equal(t, tc.wantPerPool, got,
				"poolMatchesPerPool(%d, rr=%v, fmt=%q)", tc.poolSize, tc.roundRobin, tc.poolFormat)
		})
	}
}

// TestEstimateMatchCounts_BracketMatchCount pins the playoff bracket match count
// for various roster sizes. Finding 3 fix: bracketMatchCount returns the count
// of matches that actually CONSUME COURT TIME (i.e., Status != Completed at
// generation time), not the total slot count NextPow2(players)-1.
//
// Auto-resolved bye matches (where one or both sides are empty) are pre-marked
// Completed in generatePlayoffs (bracket.go:102-111) and the court cursor is NOT
// advanced for them in assignBracketMatchSlots (scheduler_slots.go:286-291).
//
// The correct count = NextPow2(N)-1 - completedAtGeneration(byes), where the
// completed count is computed analytically via completedAtGeneration/chainComplete
// (mirrors bracket.go's auto-resolve + propagateBracketWinner chain logic).
//
// Note: this is NOT simply players-1. For example N=6: real=6>N-1=5, because
// two byes paired as "both-empty" produce a ghost upstream that consumes a slot.
// Ground truth from the real draw (engine.generatePlayoffs + counting non-Completed):
//
//	N=3: real=2, N=5: real=4, N=6: real=6, N=7: real=6
//	N=9: real=8, N=10: real=11, N=12: real=12, N=16: real=15
func TestEstimateMatchCounts_BracketMatchCount(t *testing.T) {
	tests := []struct {
		players int
		want    int // real court-time matches (from real draw, verified empirically)
	}{
		{0, 0},
		{1, 0},
		{2, 1},   // pow2=2, byes=0: real=1
		{3, 2},   // pow2=4, byes=1: completed=1, real=3-1=2
		{4, 3},   // pow2=4, byes=0: real=3
		{5, 4},   // pow2=8, byes=3: completed=3, real=7-3=4
		{6, 6},   // pow2=8, byes=2: completed=1 (1 both-empty pair), real=7-1=6
		{7, 6},   // pow2=8, byes=1: completed=1, real=7-1=6
		{8, 7},   // pow2=8, byes=0: real=7
		{9, 8},   // pow2=16, byes=7: completed=7, real=15-7=8
		{10, 11}, // pow2=16, byes=6: completed=4, real=15-4=11
		{11, 11}, // pow2=16, byes=5: completed=4, real=15-4=11
		{12, 12}, // pow2=16, byes=4: completed=3, real=15-3=12
		{13, 12}, // pow2=16, byes=3: completed=3, real=15-3=12
		{14, 14}, // pow2=16, byes=2: completed=1, real=15-1=14
		{15, 14}, // pow2=16, byes=1: completed=1, real=15-1=14
		{16, 15}, // pow2=16, byes=0: real=15
		{17, 16}, // pow2=32, byes=15: completed=15, real=31-15=16
		{24, 24}, // pow2=32, byes=8: completed=7, real=31-7=24
		{32, 31}, // pow2=32, byes=0: real=31
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			got := bracketMatchCount(tc.players)
			assert.Equal(t, tc.want, got, "bracketMatchCount(%d)", tc.players)
		})
	}
}

// TestEstimateMatchCounts_Mixed_IndividualFullRR is a table-driven end-to-end
// test for the mixed (Pools + Knockout) format with full round-robin pools.
//
// Reference formulas:
//
//	numPools    = ceil or floor of playerCount/poolSize (driven by poolSizeMode)
//	poolMatches = sum over each pool of C(poolN, 2) (full RR)
//	playoffs    = NextPow2(numPools * poolWinners) - 1
func TestEstimateMatchCounts_Mixed_IndividualFullRR(t *testing.T) {
	tests := []struct {
		name         string
		playerCount  int
		poolSize     int
		poolSizeMode string // "max" | "min" (or "")
		poolWinners  int
		roundRobin   bool
		wantPools    int
		wantPool     int // total pool matches
		wantPlayoff  int // total bracket matches
	}{
		{
			// 12 players, poolSize 4, min-mode → 3 pools of 4
			// pool matches: 3 * C(4,2) = 3*6 = 18
			// finalists: 6; bracketMatchCount(6)=6 (pow2=8, byes=2, completed=1, real=6)
			name:        "12p size4 min rr winners2",
			playerCount: 12, poolSize: 4, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 3, wantPool: 18, wantPlayoff: 6,
		},
		{
			// 13 players, poolSize 4, max-mode → ceil(13/4)=4 pools
			// sizes: base=13/4=3 rem=1 → [4,3,3,3]
			// pool matches: C(4,2) + 3*C(3,2) = 6 + 9 = 15
			// finalists: 8; bracketMatchCount(8)=7 (pow2=8, no byes: real=7)
			name:        "13p size4 max rr winners2",
			playerCount: 13, poolSize: 4, poolSizeMode: "max", poolWinners: 2, roundRobin: true,
			wantPools: 4, wantPool: 15, wantPlayoff: 7,
		},
		{
			// 9 players, poolSize 3, min-mode → 3 pools of 3
			// pool matches: 3 * C(3,2) = 3*3 = 9
			// finalists: 6; bracketMatchCount(6)=6 (pow2=8, byes=2, completed=1, real=6)
			name:        "9p size3 min rr winners2",
			playerCount: 9, poolSize: 3, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 3, wantPool: 9, wantPlayoff: 6,
		},
		{
			// 12 players, poolSize 4, min-mode, 1 winner per pool → 3 pools of 4
			// finalists: 3; bracketMatchCount(3)=2 (pow2=4, byes=1, completed=1, real=2)
			name:        "12p size4 min rr winners1",
			playerCount: 12, poolSize: 4, poolSizeMode: "min", poolWinners: 1, roundRobin: true,
			wantPools: 3, wantPool: 18, wantPlayoff: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := EstimateMatchCountsInput{
				Format:       "mixed",
				PlayerCount:  tc.playerCount,
				PoolSize:     tc.poolSize,
				PoolSizeMode: tc.poolSizeMode,
				PoolWinners:  tc.poolWinners,
				RoundRobin:   tc.roundRobin,
				PoolFormat:   "",
			}
			poolCount, playoffCount, err := EstimateMatchCounts(in)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPool, poolCount, "pool matches")
			assert.Equal(t, tc.wantPlayoff, playoffCount, "playoff matches")
		})
	}
}

// TestEstimateMatchCounts_Mixed_NonRR covers the mixed format with
// non-round-robin pool matches (CreatePoolMatches path).
func TestEstimateMatchCounts_Mixed_NonRR(t *testing.T) {
	tests := []struct {
		name        string
		playerCount int
		poolSize    int
		poolSizeMd  string
		poolWinners int
		wantPools   int
		wantPool    int
		wantPlayoff int
	}{
		{
			// 12p size4 min → 3 pools of 4 → 4 matches each (non-RR size 4)
			// finalists: 6; bracketMatchCount(6)=6 (pow2=8, byes=2, completed=1, real=6)
			name:        "12p size4 min non-rr winners2",
			playerCount: 12, poolSize: 4, poolSizeMd: "min", poolWinners: 2,
			wantPools: 3, wantPool: 12, wantPlayoff: 6,
		},
		{
			// 15p size5 min → 3 pools of 5 → 5 matches each (non-RR size 5)
			// finalists: 6; bracketMatchCount(6)=6 (pow2=8, byes=2, completed=1, real=6)
			name:        "15p size5 min non-rr winners2",
			playerCount: 15, poolSize: 5, poolSizeMd: "min", poolWinners: 2,
			wantPools: 3, wantPool: 15, wantPlayoff: 6,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := EstimateMatchCountsInput{
				Format:       "mixed",
				PlayerCount:  tc.playerCount,
				PoolSize:     tc.poolSize,
				PoolSizeMode: tc.poolSizeMd,
				PoolWinners:  tc.poolWinners,
				RoundRobin:   false,
				PoolFormat:   "",
			}
			poolCount, playoffCount, err := EstimateMatchCounts(in)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPool, poolCount, "pool matches")
			assert.Equal(t, tc.wantPlayoff, playoffCount, "playoff matches")
		})
	}
}

// TestEstimateMatchCounts_Mixed_PartialRR covers the partial pool format.
func TestEstimateMatchCounts_Mixed_PartialRR(t *testing.T) {
	tests := []struct {
		name        string
		playerCount int
		poolSize    int
		poolSizeMd  string
		poolWinners int
		wantPool    int
		wantPlayoff int
	}{
		{
			// 12p size4 min → 3 pools of 4 → N-1=3 matches each → 9
			// finalists: 6; bracketMatchCount(6)=6 (pow2=8, byes=2, completed=1, real=6)
			name:        "12p size4 partial",
			playerCount: 12, poolSize: 4, poolSizeMd: "min", poolWinners: 2,
			wantPool: 9, wantPlayoff: 6,
		},
		{
			// 9p size3 min → 3 pools of 3 → N-1=2 matches each → 6
			// finalists: 6; bracketMatchCount(6)=6 (pow2=8, byes=2, completed=1, real=6)
			name:        "9p size3 partial",
			playerCount: 9, poolSize: 3, poolSizeMd: "min", poolWinners: 2,
			wantPool: 6, wantPlayoff: 6,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := EstimateMatchCountsInput{
				Format:       "mixed",
				PlayerCount:  tc.playerCount,
				PoolSize:     tc.poolSize,
				PoolSizeMode: tc.poolSizeMd,
				PoolWinners:  tc.poolWinners,
				PoolFormat:   "partial",
			}
			poolCount, playoffCount, err := EstimateMatchCounts(in)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPool, poolCount, "pool matches")
			assert.Equal(t, tc.wantPlayoff, playoffCount, "playoff matches")
		})
	}
}

// TestEstimateMatchCounts_PlayoffsOnly verifies that a playoffs-only competition
// has zero pool matches and the correct non-bye bracket match count (Finding 3:
// only real court-time-consuming matches, not total slots including auto-resolved
// byes). The correct count = NextPow2(N)-1 - completedAtGeneration(byes).
func TestEstimateMatchCounts_PlayoffsOnly(t *testing.T) {
	tests := []struct {
		playerCount int
		wantPlayoff int // real court-time matches (NOT NextPow2-1 for non-pow2 N)
	}{
		{4, 3},   // pow2=4, no byes: 3 real
		{8, 7},   // pow2=8, no byes: 7 real
		{16, 15}, // pow2=16, no byes: 15 real
		{6, 6},   // pow2=8, byes=2: completed=1 (both-empty), real=6 (NOT 5=N-1!)
		{12, 12}, // pow2=16, byes=4: completed=3, real=12 (NOT 11=N-1!)
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			in := EstimateMatchCountsInput{
				Format:      "playoffs",
				PlayerCount: tc.playerCount,
			}
			poolCount, playoffCount, err := EstimateMatchCounts(in)
			require.NoError(t, err)
			assert.Equal(t, 0, poolCount, "playoffs-only must have 0 pool matches")
			assert.Equal(t, tc.wantPlayoff, playoffCount)
		})
	}
}

// TestEstimateMatchCounts_League verifies that a league competition
// produces a single pool of all players with full round-robin, and zero
// playoff matches.
func TestEstimateMatchCounts_League(t *testing.T) {
	tests := []struct {
		playerCount int
		wantPool    int
	}{
		// C(n,2)
		{4, 6},
		{6, 15},
		{8, 28},
		{10, 45},
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			in := EstimateMatchCountsInput{
				Format:      "league",
				PlayerCount: tc.playerCount,
			}
			poolCount, playoffCount, err := EstimateMatchCounts(in)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPool, poolCount, "league pool matches (C(n,2))")
			assert.Equal(t, 0, playoffCount, "league has no playoff bracket")
		})
	}
}

// TestEstimateMatchCounts_Swiss verifies the Swiss-format match count:
// SwissRounds * ceil(playerCount/2) matches total.
//
// The bye match (when playerCount is odd) is included in the count —
// the engine persists it in pool-matches.csv just like any other match
// (see buildSwissMatches in swiss.go) so the scheduler must allocate a
// slot for it.
func TestEstimateMatchCounts_Swiss(t *testing.T) {
	tests := []struct {
		name        string
		playerCount int
		swissRounds int
		wantPool    int // = swissRounds * ceil(playerCount/2)
	}{
		// Even field: 8 players, 3 rounds → 3 * 4 = 12
		{"8p 3r even", 8, 3, 12},
		// Odd field: 7 players, 3 rounds → 3 * 4 = 12 (3 real + 1 bye per round)
		{"7p 3r odd", 7, 3, 12},
		// 10 players, 4 rounds → 4 * 5 = 20
		{"10p 4r even", 10, 4, 20},
		// 5 players, 4 rounds → 4 * 3 = 12 (2 real + 1 bye per round)
		{"5p 4r odd", 5, 4, 12},
		// 0 rounds configured → 0 matches (no estimate possible)
		{"0 rounds", 8, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := EstimateMatchCountsInput{
				Format:      "swiss",
				PlayerCount: tc.playerCount,
				SwissRounds: tc.swissRounds,
			}
			poolCount, playoffCount, err := EstimateMatchCounts(in)
			require.NoError(t, err)
			assert.Equal(t, tc.wantPool, poolCount, "swiss total matches")
			assert.Equal(t, 0, playoffCount, "swiss has no playoff bracket")
		})
	}
}

// TestEstimateMatchCounts_ZeroPlayers verifies that a zero player count
// produces zero matches without an error (the engine would reject it earlier,
// but the estimator should not panic or corrupt its math).
func TestEstimateMatchCounts_ZeroPlayers(t *testing.T) {
	for _, format := range []string{"mixed", "playoffs", "league", "swiss"} {
		t.Run(format, func(t *testing.T) {
			in := EstimateMatchCountsInput{
				Format:      format,
				PlayerCount: 0,
				PoolSize:    4,
				PoolWinners: 2,
				SwissRounds: 3,
			}
			poolCount, playoffCount, err := EstimateMatchCounts(in)
			require.NoError(t, err)
			assert.Equal(t, 0, poolCount)
			assert.Equal(t, 0, playoffCount)
		})
	}
}

// TestEstimateMatchCounts_UnknownFormat verifies that an unrecognised
// format string returns an error without panicking.
func TestEstimateMatchCounts_UnknownFormat(t *testing.T) {
	in := EstimateMatchCountsInput{
		Format:      "bogus",
		PlayerCount: 10,
	}
	_, _, err := EstimateMatchCounts(in)
	require.Error(t, err)
}

// TestEstimateMatchCounts_PoolSizeZero_Mixed verifies that a zero pool size
// returns an error (division by zero guard) for the mixed format.
func TestEstimateMatchCounts_PoolSizeZero_Mixed(t *testing.T) {
	in := EstimateMatchCountsInput{
		Format:      "mixed",
		PlayerCount: 12,
		PoolSize:    0,
		PoolWinners: 2,
	}
	_, _, err := EstimateMatchCounts(in)
	require.Error(t, err)
}

// TestEstimateMatchCounts_PlayerCountLessThanPoolSize_MinMode verifies that
// a player count below the pool size in min mode returns an error (Finding 4).
// This mirrors CreatePools' error at tournament.go:222: when floor(N/poolSize)
// rounds to zero there can be no pools. Max mode always produces ≥1 pool (ceil
// division), so it is unaffected.
func TestEstimateMatchCounts_PlayerCountLessThanPoolSize_MinMode(t *testing.T) {
	in := EstimateMatchCountsInput{
		Format:       "mixed",
		PlayerCount:  3, // fewer than poolSize
		PoolSize:     5, // min mode → numPools = floor(3/5) = 0 → error
		PoolSizeMode: "min",
		PoolWinners:  2,
	}
	_, _, err := EstimateMatchCounts(in)
	require.Error(t, err, "player count < pool size in min mode must return an error")
}

// TestEstimateMatchCounts_CrossCheck_MatchesCreatePools confirms that the
// pool-count math in EstimateMatchCounts agrees with CreatePools for a
// concrete example. We call CreatePools directly and compare the pool count
// and total match count against the estimator, so any divergence between the
// two code paths is caught.
//
// This is the central risk guard from the bead's plan: no formula duplication,
// no drift from the real draw.
//
// Finding 4: min-mode overflow cases are explicitly included to guard against
// the even-distribution bug (estimateMixed was using base/rem split, but
// CreatePools in min mode uses targetSizes[i]=poolSize with forcePoolSize
// distributing overflow from both ends inward). For these cases the pool sizes
// produced by CreatePools differ from an even split, making the C(n,2) sum
// different.
func TestEstimateMatchCounts_CrossCheck_MatchesCreatePools(t *testing.T) {
	tests := []struct {
		name         string
		playerCount  int
		poolSize     int
		poolSizeMode string
		roundRobin   bool
		poolWinners  int
	}{
		// Even-distribution cases (min mode, no overflow): already passing.
		{"12p size4 min rr", 12, 4, "min", true, 2},
		{"12p size4 max rr", 12, 4, "max", true, 2},
		{"9p size3 min rr", 9, 3, "min", true, 2},
		{"10p size3 min rr", 10, 3, "min", true, 2},
		{"10p size3 max rr", 10, 3, "max", true, 2},
		{"15p size5 min non-rr", 15, 5, "min", false, 2},
		{"16p size4 min rr", 16, 4, "min", true, 2},
		{"13p size4 max rr", 13, 4, "max", true, 2},

		// Finding 4: min-mode overflow cases where forcePoolSize produces pool
		// sizes differing by >1 from the even base/rem split.
		//
		// This happens when overflow = (N mod poolSize) > numPools = N/poolSize,
		// i.e., when forcePoolSize exhausts its targetSize+1 cap for all pools
		// and then falls back to "return 0", piling all remaining overflow into
		// pool[0] rather than distributing evenly.
		//
		// Key example: N=14, poolSize=5 (min):
		//   numPools=2, targetSizes=[5,5], overflow=4.
		//   forcePoolSize: pool[0]→6, pool[1]→6 (2 of 4 used), then falls back:
		//   pool[0]→7, pool[0]→8. Final sizes: [8, 6].
		//   RR: C(8,2)+C(6,2) = 28+15 = 43.
		//   Even split (base=7, rem=0): [7,7] → C(7,2)*2 = 42. DIFFERENT!
		{"14p size5 min rr (overflow>pools)", 14, 5, "min", true, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// --- Real draw path ---
			// Use distinct names and distinct dojos so discoverPool never
			// hits a conflict — ensuring the pool sizes are determined purely
			// by the target-size/forcePoolSize logic, not by dojo avoidance.
			players := make([]Player, tc.playerCount)
			for i := range players {
				players[i] = Player{
					Name: fmt.Sprintf("p%d", i),
					Dojo: fmt.Sprintf("Dojo%d", i), // unique dojo per player
				}
			}
			isMax := tc.poolSizeMode == "max"
			realPools, err := CreatePools(players, tc.poolSize, isMax)
			require.NoError(t, err)

			if tc.roundRobin {
				CreatePoolRoundRobinMatches(realPools)
			} else {
				CreatePoolMatches(realPools)
			}
			realPoolCount := len(realPools)
			realMatchCount := 0
			for _, p := range realPools {
				realMatchCount += len(p.Matches)
			}

			// --- Estimator path ---
			in := EstimateMatchCountsInput{
				Format:       "mixed",
				PlayerCount:  tc.playerCount,
				PoolSize:     tc.poolSize,
				PoolSizeMode: tc.poolSizeMode,
				PoolWinners:  tc.poolWinners,
				RoundRobin:   tc.roundRobin,
				PoolFormat:   "",
			}
			estPool, _, err2 := EstimateMatchCounts(in)
			require.NoError(t, err2)

			assert.Equal(t, realMatchCount, estPool,
				"pool match count mismatch: CreatePools=%d estimator=%d (pools=%d)", realMatchCount, estPool, realPoolCount)
		})
	}
}
