package helper

import (
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
// for various roster sizes. Single-elimination bracket of NextPow2(n) leaves
// has NextPow2(n)-1 matches total (including auto-resolved byes).
func TestEstimateMatchCounts_BracketMatchCount(t *testing.T) {
	tests := []struct {
		players int
		want    int
	}{
		{1, 0},  // 1 player: no matches (NextPow2(1)=1, 1-1=0)
		{2, 1},  // NextPow2(2)=2, 2-1=1
		{3, 3},  // NextPow2(3)=4, 4-1=3
		{4, 3},  // NextPow2(4)=4, 4-1=3
		{5, 7},  // NextPow2(5)=8, 8-1=7
		{8, 7},  // NextPow2(8)=8, 8-1=7
		{9, 15}, // NextPow2(9)=16, 16-1=15
		{16, 15},
		{17, 31}, // NextPow2(17)=32, 32-1=31
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
			// bracket: NextPow2(3*2=6) - 1 = 8-1 = 7
			name:        "12p size4 min rr winners2",
			playerCount: 12, poolSize: 4, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 3, wantPool: 18, wantPlayoff: 7,
		},
		{
			// 13 players, poolSize 4, max-mode → ceil(13/4)=4 pools
			// sizes: 4,4,4,1 (well: 13%4=1 remainder goes to first 1 pool: 4,3,3,3? Actually:
			//   base=13/4=3 rem=13%4=1 → first 1 pool gets 4, rest get 3 → [4,3,3,3])
			// pool matches: C(4,2) + 3*C(3,2) = 6 + 3*3 = 15
			// bracket: NextPow2(4*2=8)-1 = 7
			name:        "13p size4 max rr winners2",
			playerCount: 13, poolSize: 4, poolSizeMode: "max", poolWinners: 2, roundRobin: true,
			wantPools: 4, wantPool: 15, wantPlayoff: 7,
		},
		{
			// 9 players, poolSize 3, min-mode → 3 pools of 3
			// pool matches: 3 * C(3,2) = 3*3 = 9
			// bracket: NextPow2(3*2=6)-1 = 8-1 = 7
			name:        "9p size3 min rr winners2",
			playerCount: 9, poolSize: 3, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 3, wantPool: 9, wantPlayoff: 7,
		},
		{
			// 12 players, poolSize 4, min-mode, 1 winner per pool → 3 pools of 4
			// bracket: NextPow2(3*1=3)-1 = 4-1 = 3
			name:        "12p size4 min rr winners1",
			playerCount: 12, poolSize: 4, poolSizeMode: "min", poolWinners: 1, roundRobin: true,
			wantPools: 3, wantPool: 18, wantPlayoff: 3,
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
			// playoff: NextPow2(3*2=6)-1=7
			name:        "12p size4 min non-rr winners2",
			playerCount: 12, poolSize: 4, poolSizeMd: "min", poolWinners: 2,
			wantPools: 3, wantPool: 12, wantPlayoff: 7,
		},
		{
			// 15p size5 min → 3 pools of 5 → 5 matches each (non-RR size 5)
			// playoff: NextPow2(3*2=6)-1=7
			name:        "15p size5 min non-rr winners2",
			playerCount: 15, poolSize: 5, poolSizeMd: "min", poolWinners: 2,
			wantPools: 3, wantPool: 15, wantPlayoff: 7,
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
			// playoff: NextPow2(6)-1=7
			name:        "12p size4 partial",
			playerCount: 12, poolSize: 4, poolSizeMd: "min", poolWinners: 2,
			wantPool: 9, wantPlayoff: 7,
		},
		{
			// 9p size3 min → 3 pools of 3 → N-1=2 matches each → 6
			// playoff: NextPow2(6)-1=7
			name:        "9p size3 partial",
			playerCount: 9, poolSize: 3, poolSizeMd: "min", poolWinners: 2,
			wantPool: 6, wantPlayoff: 7,
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
// has zero pool matches and NextPow2(N)-1 bracket matches.
func TestEstimateMatchCounts_PlayoffsOnly(t *testing.T) {
	tests := []struct {
		playerCount int
		wantPlayoff int
	}{
		{4, 3},
		{8, 7},
		{16, 15},
		{6, 7},   // NextPow2(6)=8 → 7
		{12, 15}, // NextPow2(12)=16 → 15
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

// TestEstimateMatchCounts_CrossCheck_MatchesCreatePools confirms that the
// pool-count math in EstimateMatchCounts agrees with CreatePools for a
// concrete example. We call CreatePools directly and compare the pool count
// and total match count against the estimator, so any divergence between the
// two code paths is caught.
//
// This is the central risk guard from the bead's plan: no formula duplication,
// no drift from the real draw.
func TestEstimateMatchCounts_CrossCheck_MatchesCreatePools(t *testing.T) {
	tests := []struct {
		name         string
		playerCount  int
		poolSize     int
		poolSizeMode string
		roundRobin   bool
		poolWinners  int
	}{
		{"12p size4 min rr", 12, 4, "min", true, 2},
		{"12p size4 max rr", 12, 4, "max", true, 2},
		{"9p size3 min rr", 9, 3, "min", true, 2},
		{"10p size3 min rr", 10, 3, "min", true, 2},
		{"10p size3 max rr", 10, 3, "max", true, 2},
		{"15p size5 min non-rr", 15, 5, "min", false, 2},
		{"16p size4 min rr", 16, 4, "min", true, 2},
		{"13p size4 max rr", 13, 4, "max", true, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// --- Real draw path ---
			players := make([]Player, tc.playerCount)
			for i := range players {
				players[i] = Player{Name: "p" + string(rune('A'+i%26)), Dojo: "D"}
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
