package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Unit tests for EstimateScheduleForCompetition
// ---------------------------------------------------------------------------

// TestEstimateScheduleForCompetition_UnknownComp verifies that a missing
// competition returns *NotFoundError (HTTP 404 sentinel).
func TestEstimateScheduleForCompetition_UnknownComp(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	_, err := eng.EstimateScheduleForCompetition("no-such-comp")
	require.Error(t, err)
	var nfe *NotFoundError
	assert.ErrorAs(t, err, &nfe, "unknown compID must return NotFoundError")
}

// TestEstimateScheduleForCompetition_PlayoffsOnly verifies a playoffs-only
// competition: no pool matches, bracket over all players.
func TestEstimateScheduleForCompetition_PlayoffsOnly(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "est-playoffs"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   compID,
		Format:               state.CompFormatPlayoffs,
		Kind:                 "individual",
		Courts:               []string{"A"},
		StartTime:            "09:00",
		PoolMatchDuration:    3,
		PlayoffMatchDuration: 5,
		Status:               state.CompStatusSetup,
	}))
	// 8 players → NextPow2(8)=8, bracket matches = 8-1 = 7.
	saveTestParticipants(t, store, compID, []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"})

	est, err := eng.EstimateScheduleForCompetition(compID)
	require.NoError(t, err)
	// Must be non-zero (7 playoff matches × some duration > 0).
	assert.Greater(t, est.TotalDurationMinutes, 0)
	assert.Len(t, est.PerCourtMinutes, 1)

	// Cross-check: direct call with derived counts must match.
	comp, _ := store.LoadCompetition(compID)
	tourn, _ := store.LoadTournament()
	direct := EstimateForCounts(0, 7, comp, tourn)
	assert.Equal(t, direct.TotalDurationMinutes, est.TotalDurationMinutes,
		"EstimateScheduleForCompetition must agree with direct EstimateForCounts(0,7)")
}

// TestEstimateScheduleForCompetition_Mixed verifies a mixed (Pools + Knockout)
// competition: pool AND playoff matches contribute.
func TestEstimateScheduleForCompetition_Mixed(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "est-mixed"

	// 9 players, poolSize 3 min-mode, RR, 2 winners → 3 pools of 3,
	// pool matches = 3*C(3,2)=9, bracket = NextPow2(6)-1=7.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   compID,
		Format:               state.CompFormatMixed,
		Kind:                 "individual",
		PoolSize:             3,
		PoolSizeMode:         "min",
		PoolWinners:          2,
		RoundRobin:           true,
		Courts:               []string{"A"},
		StartTime:            "09:00",
		PoolMatchDuration:    3,
		PlayoffMatchDuration: 5,
		Status:               state.CompStatusSetup,
	}))
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank", "Grace", "Hank", "Ivy",
	})

	est, err := eng.EstimateScheduleForCompetition(compID)
	require.NoError(t, err)
	assert.Greater(t, est.TotalDurationMinutes, 0)

	// Cross-check: direct call with expected counts must match.
	comp, _ := store.LoadCompetition(compID)
	tourn, _ := store.LoadTournament()
	direct := EstimateForCounts(9, 7, comp, tourn)
	assert.Equal(t, direct.TotalDurationMinutes, est.TotalDurationMinutes,
		"EstimateScheduleForCompetition must agree with direct EstimateForCounts(9,7)")
}

// TestEstimateScheduleForCompetition_League verifies a league-format
// competition: all participants in one pool, no playoffs.
func TestEstimateScheduleForCompetition_League(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "est-league"

	// 5 players → C(5,2)=10 pool matches, 0 playoff.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                compID,
		Format:            state.CompFormatLeague,
		Kind:              "individual",
		Courts:            []string{"A"},
		StartTime:         "09:00",
		PoolMatchDuration: 3,
		Status:            state.CompStatusSetup,
	}))
	saveTestParticipants(t, store, compID, []string{"A", "B", "C", "D", "E"})

	est, err := eng.EstimateScheduleForCompetition(compID)
	require.NoError(t, err)
	assert.Greater(t, est.TotalDurationMinutes, 0)

	comp, _ := store.LoadCompetition(compID)
	tourn, _ := store.LoadTournament()
	direct := EstimateForCounts(10, 0, comp, tourn)
	assert.Equal(t, direct.TotalDurationMinutes, est.TotalDurationMinutes,
		"EstimateScheduleForCompetition must agree with direct EstimateForCounts(10,0)")
}

// TestEstimateScheduleForCompetition_Swiss verifies a Swiss-format competition:
// swissRounds * ceil(n/2) pool matches, no playoffs.
func TestEstimateScheduleForCompetition_Swiss(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "est-swiss"

	// 8 players, 3 rounds → 3 * 4 = 12 pool matches.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                compID,
		Format:            state.CompFormatSwiss,
		Kind:              "individual",
		SwissRounds:       3,
		Courts:            []string{"A"},
		StartTime:         "09:00",
		PoolMatchDuration: 3,
		Status:            state.CompStatusSetup,
	}))
	saveTestParticipants(t, store, compID, []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"})

	est, err := eng.EstimateScheduleForCompetition(compID)
	require.NoError(t, err)
	assert.Greater(t, est.TotalDurationMinutes, 0)

	comp, _ := store.LoadCompetition(compID)
	tourn, _ := store.LoadTournament()
	direct := EstimateForCounts(12, 0, comp, tourn)
	assert.Equal(t, direct.TotalDurationMinutes, est.TotalDurationMinutes,
		"EstimateScheduleForCompetition must agree with direct EstimateForCounts(12,0)")
}

// TestEstimateScheduleForCompetition_ZeroParticipants verifies that a
// competition with no participants returns a zero estimate (not an error).
func TestEstimateScheduleForCompetition_ZeroParticipants(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "est-empty"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Format: state.CompFormatMixed,
		Courts: []string{"A"},
		Status: state.CompStatusSetup,
	}))
	// Deliberately no participants saved.

	est, err := eng.EstimateScheduleForCompetition(compID)
	require.NoError(t, err)
	// Zero participants → zero matches → opening-block-only or zero duration.
	// The important contract is: no error returned.
	assert.GreaterOrEqual(t, est.TotalDurationMinutes, 0)
}

// TestEstimateScheduleForCompetition_SourceLinked_PoolsExist verifies the
// source-linked playoffs path: the finalist count is derived from the source's
// pool count × poolWinners when the roster is empty.
func TestEstimateScheduleForCompetition_SourceLinked_PoolsExist(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "est-source-comp"
	playoffsID := "est-linked-playoffs"

	// Set up source competition with pools already generated.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           srcID,
		Format:       state.CompFormatMixed,
		Kind:         "individual",
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       state.CompStatusPools,
	}))
	saveTestParticipants(t, store, srcID, []string{"A", "B", "C", "D", "E", "F"})

	// Persist 2 pools (mimicking what generatePools produces).
	fakePools := []helper.Pool{
		{PoolName: "Pool 1", Players: []domain.Player{{Name: "A"}, {Name: "B"}, {Name: "C"}}},
		{PoolName: "Pool 2", Players: []domain.Player{{Name: "D"}, {Name: "E"}, {Name: "F"}}},
	}
	require.NoError(t, store.SavePools(srcID, fakePools))

	// Set up a source-linked playoffs competition with an empty roster.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   playoffsID,
		Format:               state.CompFormatPlayoffs,
		Kind:                 "individual",
		SourceCompID:         srcID,
		PoolWinners:          2,
		Courts:               []string{"A"},
		StartTime:            "09:00",
		PlayoffMatchDuration: 5,
		Status:               state.CompStatusSetup,
	}))
	// No participants saved (empty roster — the draw hasn't resolved them yet).

	est, err := eng.EstimateScheduleForCompetition(playoffsID)
	require.NoError(t, err)
	// 2 pools × 2 winners = 4 finalists → NextPow2(4)=4, bracket=3 matches.
	assert.Greater(t, est.TotalDurationMinutes, 0,
		"source-linked playoffs with 4 finalists must produce a non-zero estimate")

	// Cross-check against direct EstimateForCounts with 3 bracket matches.
	comp, _ := store.LoadCompetition(playoffsID)
	tourn, _ := store.LoadTournament()
	direct := EstimateForCounts(0, 3, comp, tourn)
	assert.Equal(t, direct.TotalDurationMinutes, est.TotalDurationMinutes,
		"source-linked playoff estimate must agree with direct EstimateForCounts(0,3)")
}

// TestEstimateScheduleForCompetition_SourceLinked_NoPools verifies the fallback
// when the source competition's draw has not been generated yet: the playoffs
// estimate returns a zero ScheduleEstimate without error.
func TestEstimateScheduleForCompetition_SourceLinked_NoPools(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "est-source-no-pools"
	playoffsID := "est-linked-no-pools"

	// Source exists but has no pools.csv yet.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     srcID,
		Format: state.CompFormatMixed,
		Status: state.CompStatusSetup,
	}))

	// Linked playoffs competition.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           playoffsID,
		Format:       state.CompFormatPlayoffs,
		Kind:         "individual",
		SourceCompID: srcID,
		PoolWinners:  2,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       state.CompStatusSetup,
	}))
	// No participants saved.

	est, err := eng.EstimateScheduleForCompetition(playoffsID)
	require.NoError(t, err, "missing source pools must return (zero, nil), not an error")
	// Zero matches → TotalDurationMinutes is whatever EstimateForCounts returns
	// for 0+0 matches (opening block or zero). The critical contract is: no error.
	assert.GreaterOrEqual(t, est.TotalDurationMinutes, 0)
}

// ---------------------------------------------------------------------------
// Integration cross-check: bracket count vs real draw pipeline
// ---------------------------------------------------------------------------

// TestEstimateMatchCounts_vs_RealPlayoffsDraw validates that
// helper.EstimateMatchCounts' bracket count equals the actual number of
// BracketMatch slots generated by generatePlayoffs (the real draw engine).
//
// This is the Phase 2 "residual risk" guard for the claim in estimate.go:
//
//	bracketMatchCount = NextPow2(players) - 1
//
// The test runs the REAL draw pipeline via eng.StartCompetition, counts the
// total BracketMatches across all rounds, and asserts it equals the estimator's
// count. If the formula were wrong, this test would fail.
func TestEstimateMatchCounts_vs_RealPlayoffsDraw(t *testing.T) {
	tests := []struct {
		name        string
		compID      string // alphanumeric/hyphen only — no spaces or parens
		playerCount int
		wantBracket int // NextPow2(playerCount) - 1
	}{
		{"4 players (power-of-2, no byes)", "playoffs-4p", 4, 3},
		{"5 players (needs byes)", "playoffs-5p", 5, 7},
		{"8 players (power-of-2, no byes)", "playoffs-8p", 8, 7},
		{"12 players (needs byes, pads to 16)", "playoffs-12p", 12, 15},
		{"16 players (power-of-2)", "playoffs-16p", 16, 15},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := tc.compID

			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID:        compID,
				Format:    state.CompFormatPlayoffs,
				Kind:      "individual",
				Courts:    []string{"A"},
				StartTime: "09:00",
				Status:    state.CompStatusSetup,
			}))

			names := make([]string, tc.playerCount)
			for i := range names {
				names[i] = "Player" + string(rune('A'+i%26))
			}
			saveTestParticipants(t, store, compID, names)

			require.NoError(t, eng.StartCompetition(compID))

			bracket, err := store.LoadBracket(compID)
			require.NoError(t, err)
			require.NotNil(t, bracket)

			// Count all bracket match slots across all rounds.
			realMatchCount := 0
			for _, round := range bracket.Rounds {
				realMatchCount += len(round)
			}

			// Verify the estimator formula matches the real draw.
			assert.Equal(t, tc.wantBracket, realMatchCount,
				"real bracket match count for %d players should be %d",
				tc.playerCount, tc.wantBracket)

			// Verify EstimateMatchCounts agrees.
			poolCount, playoffCount, err := helper.EstimateMatchCounts(helper.EstimateMatchCountsInput{
				Format:      state.CompFormatPlayoffs,
				PlayerCount: tc.playerCount,
			})
			require.NoError(t, err)
			assert.Equal(t, 0, poolCount, "playoffs-only must have 0 pool matches")
			assert.Equal(t, realMatchCount, playoffCount,
				"EstimateMatchCounts bracket count must match real draw for %d players", tc.playerCount)
		})
	}
}

// TestEstimateMatchCounts_vs_RealMixedDraw validates that
// helper.EstimateMatchCounts' pool AND bracket counts match the real draw
// pipeline for a mixed (Pools + Knockout) competition with full round-robin.
//
// This is the full mixed-format residual-risk guard: both the pool count
// formula and the bracket count formula are exercised against real code.
func TestEstimateMatchCounts_vs_RealMixedDraw(t *testing.T) {
	tests := []struct {
		name         string
		compID       string // alphanumeric/hyphen only — no spaces or parens
		playerCount  int
		poolSize     int
		poolSizeMode string
		poolWinners  int
		roundRobin   bool
		wantPools    int // expected pool match count from estimator
		wantBracket  int // expected bracket match count from estimator
	}{
		{
			// 6 players, poolSize 3 min-mode, RR, 2 winners.
			// 2 pools of 3 → pool matches: 2*C(3,2)=6; bracket: NextPow2(4)-1=3.
			name: "6p size3 min rr winners2", compID: "mixed-6p-s3",
			playerCount: 6, poolSize: 3, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 6, wantBracket: 3,
		},
		{
			// 9 players, poolSize 3 min-mode, RR, 2 winners.
			// 3 pools of 3 → pool matches: 3*C(3,2)=9; bracket: NextPow2(6)-1=7.
			name: "9p size3 min rr winners2", compID: "mixed-9p-s3",
			playerCount: 9, poolSize: 3, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 9, wantBracket: 7,
		},
		{
			// 12 players, poolSize 4 min-mode, RR, 2 winners.
			// 3 pools of 4 → pool matches: 3*C(4,2)=18; bracket: NextPow2(6)-1=7.
			name: "12p size4 min rr winners2", compID: "mixed-12p-s4",
			playerCount: 12, poolSize: 4, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 18, wantBracket: 7,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := tc.compID

			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID:           compID,
				Format:       state.CompFormatMixed,
				Kind:         "individual",
				PoolSize:     tc.poolSize,
				PoolSizeMode: tc.poolSizeMode,
				PoolWinners:  tc.poolWinners,
				RoundRobin:   tc.roundRobin,
				Courts:       []string{"A"},
				StartTime:    "09:00",
				Status:       state.CompStatusSetup,
			}))

			names := make([]string, tc.playerCount)
			for i := range names {
				names[i] = "Player" + string(rune('A'+i%26))
			}
			saveTestParticipants(t, store, compID, names)

			require.NoError(t, eng.StartCompetition(compID))

			// Count real pool matches.
			poolMatches, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			realPoolCount := len(poolMatches)

			// For a mixed competition after StartCompetition, the status is
			// CompStatusPools — no bracket yet. Count via the estimator only;
			// the integration test for pool matches was already done in Phase 1's
			// TestEstimateMatchCounts_CrossCheck_MatchesCreatePools. Here we verify
			// the estimator agrees with the post-start pool-matches.csv length.
			poolCount, playoffCount, err := helper.EstimateMatchCounts(helper.EstimateMatchCountsInput{
				Format:       state.CompFormatMixed,
				PlayerCount:  tc.playerCount,
				PoolSize:     tc.poolSize,
				PoolSizeMode: tc.poolSizeMode,
				PoolWinners:  tc.poolWinners,
				RoundRobin:   tc.roundRobin,
			})
			require.NoError(t, err)

			assert.Equal(t, realPoolCount, poolCount,
				"estimator pool count must match pool-matches.csv length for %s", tc.name)
			assert.Equal(t, tc.wantPools, poolCount,
				"pool count should be %d for %s", tc.wantPools, tc.name)
			assert.Equal(t, tc.wantBracket, playoffCount,
				"bracket count should be %d for %s", tc.wantBracket, tc.name)

			// Now we need to complete all pool matches and start the playoffs
			// to validate the bracket count against the real bracket.
			// Mark all pool matches complete so we can generate the bracket.
			for i := range poolMatches {
				poolMatches[i].Status = state.MatchStatusCompleted
				poolMatches[i].Winner = poolMatches[i].SideA
			}
			require.NoError(t, store.SavePoolMatches(compID, poolMatches))

			// Validate the bracket count by running a standalone playoffs draw
			// with exactly the expected finalist count (numPools × poolWinners).
			var numPools int
			if tc.poolSizeMode == "max" {
				numPools = (tc.playerCount + tc.poolSize - 1) / tc.poolSize
			} else {
				numPools = tc.playerCount / tc.poolSize
			}
			finalists := numPools * tc.poolWinners

			eng2, store2, _ := setupTestEngine(t)
			bracketCompID := "bracket-" + tc.compID
			require.NoError(t, store2.SaveCompetition(&state.Competition{
				ID:        bracketCompID,
				Format:    state.CompFormatPlayoffs,
				Kind:      "individual",
				Courts:    []string{"A"},
				StartTime: "09:00",
				Status:    state.CompStatusSetup,
			}))
			finalistNames := make([]string, finalists)
			for i := range finalistNames {
				finalistNames[i] = "Finalist" + string(rune('A'+i%26))
			}
			saveTestParticipants(t, store2, bracketCompID, finalistNames)
			require.NoError(t, eng2.StartCompetition(bracketCompID))

			bracket, err := store2.LoadBracket(bracketCompID)
			require.NoError(t, err)
			require.NotNil(t, bracket)

			realBracketCount := 0
			for _, round := range bracket.Rounds {
				realBracketCount += len(round)
			}

			assert.Equal(t, tc.wantBracket, realBracketCount,
				"real bracket match count for %d finalists should be %d (case %s)",
				finalists, tc.wantBracket, tc.name)
			assert.Equal(t, playoffCount, realBracketCount,
				"EstimateMatchCounts bracket count must match real draw for %d finalists",
				finalists)
		})
	}
}
