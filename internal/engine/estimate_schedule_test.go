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
	// pool matches = 3*C(3,2)=9, bracket = bracketMatchCount(6)=5
	// (pow2=8, byes=2 distributed to top seeds — mp-sess — so real=N-1=5).
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
	// 3 pools × 2 winners = 6 finalists; bracketMatchCount(6) = 5
	// (pow2=8, byes=2 distributed to top seeds — mp-sess — so real=N-1=5).
	comp, _ := store.LoadCompetition(compID)
	tourn, _ := store.LoadTournament()
	direct := EstimateForCounts(9, 5, comp, tourn)
	assert.Equal(t, direct.TotalDurationMinutes, est.TotalDurationMinutes,
		"EstimateScheduleForCompetition must agree with direct EstimateForCounts(9,5)")
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
// Finding 1: check-in filter applied in estimateParticipantCount
// ---------------------------------------------------------------------------

// TestEstimateParticipantCount_CheckInFilter verifies that when CheckInEnabled is
// true and at least one player is checked in, estimateParticipantCount returns
// only the checked-in count — mirroring filterCheckedIn (competition.go:387).
//
// Opt-in semantics: if nobody is checked in, the full roster is returned.
//
// Player counts chosen to be powers-of-2 so bracketMatchCount is the same
// regardless of Finding 3's fix (players-1 == NextPow2(players)-1 for
// power-of-2 inputs), keeping this test independent of that finding.
func TestEstimateParticipantCount_CheckInFilter(t *testing.T) {
	t.Run("some checked in: estimate uses filtered count", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "est-checkin-some"

		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:                   compID,
			Format:               state.CompFormatPlayoffs,
			Kind:                 "individual",
			Courts:               []string{"A"},
			StartTime:            "09:00",
			PlayoffMatchDuration: 5,
			Status:               state.CompStatusSetup,
			CheckInEnabled:       true,
		}))
		// 8 registered, only 4 checked in.
		// 4 → bracketMatchCount(4) = 3 (power-of-2, same under any formula).
		// 8 → bracketMatchCount(8) = 7 (power-of-2, same under any formula).
		// The buggy (unfixed) code uses 8 players → 7 bouts; the fix uses 4 → 3.
		saveParticipantsWithCheckIn(t, store, compID,
			[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank", "Grace", "Hank"},
			map[string]bool{"Alice": true, "Bob": true, "Charlie": true, "Dave": true},
		)

		est, err := eng.EstimateScheduleForCompetition(compID)
		require.NoError(t, err)

		// Cross-check: estimate must equal direct call with 4 players (3 bouts).
		comp, _ := store.LoadCompetition(compID)
		tourn, _ := store.LoadTournament()
		direct4 := EstimateForCounts(0, 3, comp, tourn)
		direct8 := EstimateForCounts(0, 7, comp, tourn)
		assert.Equal(t, direct4.TotalDurationMinutes, est.TotalDurationMinutes,
			"with 4 checked-in players the estimate must use filtered count (4, 3 bouts), not full roster (8, 7 bouts)")
		assert.NotEqual(t, direct8.TotalDurationMinutes, est.TotalDurationMinutes,
			"estimate must NOT match the unfiltered 8-player count")
	})

	t.Run("nobody checked in: full roster used (opt-in fallback)", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "est-checkin-none"

		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:                   compID,
			Format:               state.CompFormatPlayoffs,
			Kind:                 "individual",
			Courts:               []string{"A"},
			StartTime:            "09:00",
			PlayoffMatchDuration: 5,
			Status:               state.CompStatusSetup,
			CheckInEnabled:       true,
		}))
		// 8 players, none checked in → opt-in fallback returns all 8.
		// bracketMatchCount(8)=7 under any formula (power-of-2).
		saveParticipantsWithCheckIn(t, store, compID,
			[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank", "Grace", "Hank"},
			map[string]bool{},
		)

		est, err := eng.EstimateScheduleForCompetition(compID)
		require.NoError(t, err)

		// 8 players playoffs: bracketMatchCount(8) = 7 (power-of-2, any formula).
		comp, _ := store.LoadCompetition(compID)
		tourn, _ := store.LoadTournament()
		direct := EstimateForCounts(0, 7, comp, tourn)
		assert.Equal(t, direct.TotalDurationMinutes, est.TotalDurationMinutes,
			"with nobody checked in, full roster of 8 must be used")
	})

	t.Run("check-in disabled: full roster always used", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "est-checkin-disabled"

		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:                   compID,
			Format:               state.CompFormatPlayoffs,
			Kind:                 "individual",
			Courts:               []string{"A"},
			StartTime:            "09:00",
			PlayoffMatchDuration: 5,
			Status:               state.CompStatusSetup,
			CheckInEnabled:       false, // disabled
		}))
		// 4 checked in of 8, but CheckInEnabled=false → all 8 used.
		saveParticipantsWithCheckIn(t, store, compID,
			[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank", "Grace", "Hank"},
			map[string]bool{"Alice": true, "Bob": true, "Charlie": true, "Dave": true},
		)

		est, err := eng.EstimateScheduleForCompetition(compID)
		require.NoError(t, err)

		// bracketMatchCount(8) = 7 (power-of-2, any formula).
		comp, _ := store.LoadCompetition(compID)
		tourn, _ := store.LoadTournament()
		direct := EstimateForCounts(0, 7, comp, tourn)
		assert.Equal(t, direct.TotalDurationMinutes, est.TotalDurationMinutes,
			"with CheckInEnabled=false, all 8 participants must be used")
	})
}

// ---------------------------------------------------------------------------
// Finding 2: source PoolWinners used for source-linked finalist count
// ---------------------------------------------------------------------------

// TestEstimateFinalistCount_UsesSourcePoolWinners verifies that the finalist
// count is derived from the SOURCE competition's PoolWinners, not from the
// playoffs competition's PoolWinners. Mirrors ResolveQualifiedPools (engine/knockout.go).
//
// Finalist counts chosen to be powers-of-2 so bracketMatchCount is stable
// regardless of Finding 3's fix, keeping this test independent.
func TestEstimateFinalistCount_UsesSourcePoolWinners(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	srcID := "est-src-pw4"
	playoffsID := "est-playoffs-pw2"

	// Source has PoolWinners=4, 2 pools → 2×4=8 finalists (power-of-2: 7 bouts).
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          srcID,
		Format:      state.CompFormatMixed,
		Kind:        "individual",
		PoolWinners: 4, // SOURCE says 4 winners per pool
		Status:      state.CompStatusPools,
	}))
	fakePools := []helper.Pool{
		{PoolName: "Pool 1", Players: []domain.Player{{Name: "A"}, {Name: "B"}}},
		{PoolName: "Pool 2", Players: []domain.Player{{Name: "C"}, {Name: "D"}}},
	}
	require.NoError(t, store.SavePools(srcID, fakePools))

	// Playoffs comp has PoolWinners=2 (→ 2×2=4 finalists, 3 bouts) — must NOT be used.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   playoffsID,
		Format:               state.CompFormatPlayoffs,
		Kind:                 "individual",
		SourceCompID:         srcID,
		PoolWinners:          2, // PLAYOFF says 2 — must be ignored
		Courts:               []string{"A"},
		StartTime:            "09:00",
		PlayoffMatchDuration: 5,
		Status:               state.CompStatusSetup,
	}))
	// No participants — source-linked path.

	est, err := eng.EstimateScheduleForCompetition(playoffsID)
	require.NoError(t, err)

	// Expected (fixed): 2 pools × 4 winners (SOURCE) = 8 finalists → 7 bouts.
	// Buggy code uses 2 pools × 2 winners (PLAYOFF) = 4 finalists → 3 bouts.
	comp, _ := store.LoadCompetition(playoffsID)
	tourn, _ := store.LoadTournament()
	direct8 := EstimateForCounts(0, 7, comp, tourn) // 8 finalists (source PoolWinners=4)
	direct4 := EstimateForCounts(0, 3, comp, tourn) // 4 finalists (playoff PoolWinners=2 — BUG)
	assert.Equal(t, direct8.TotalDurationMinutes, est.TotalDurationMinutes,
		"finalist count must use source comp's PoolWinners=4 (8 finalists, 7 bouts), "+
			"not playoff comp's PoolWinners=2 (4 finalists, 3 bouts)")
	assert.NotEqual(t, direct4.TotalDurationMinutes, est.TotalDurationMinutes,
		"estimate must NOT use the playoff comp's PoolWinners=2 (that would be the bug)")
}

// ---------------------------------------------------------------------------
// Integration cross-check: bracket count vs real draw pipeline
// ---------------------------------------------------------------------------

// TestEstimateMatchCounts_vs_RealPlayoffsDraw validates that
// helper.EstimateMatchCounts' bracket count equals the number of REAL (non-bye)
// bracket matches generated by generatePlayoffs (the real draw engine).
//
// bracketMatchCount returns the count of court-time-consuming bracket matches
// (those NOT auto-marked Completed at generation time), not NextPow2(players)-1
// (total slot count). Since mp-5ng7 the draw uses StandardSeeding +
// CreateBalancedTree + TreeToLeafArray, clustering structural byes where the
// tree is asymmetric. This produces "" vs "" double-bye slots and later-round
// "" vs "Winner of…" latent byes — all auto-completed at generation time so
// they do not consume court time. The real count remains the N-1 identity.
//
// The test runs the REAL draw pipeline via eng.StartCompetition, counts the
// non-auto-resolved matches (Status != Completed at generation time), and
// asserts it equals the estimator's count.
func TestEstimateMatchCounts_vs_RealPlayoffsDraw(t *testing.T) {
	tests := []struct {
		name        string
		compID      string // alphanumeric/hyphen only — no spaces or parens
		playerCount int
		wantBracket int // real court-time matches = N-1 (distributed byes)
	}{
		// Power-of-2: no byes → all N-1 slots are real.
		{"4 players (power-of-2, no byes)", "playoffs-4p", 4, 3},
		{"8 players (power-of-2, no byes)", "playoffs-8p", 8, 7},
		{"16 players (power-of-2)", "playoffs-16p", 16, 15},
		// Non-power-of-2: byes are distributed to top seeds, never paired, so the
		// court-time count is N-1.
		// N=5: pow2=8, byes=3 distributed → real=N-1=4.
		{"5 players (byes to top seeds, 4 real)", "playoffs-5p", 5, 4},
		// N=12: pow2=16, byes=4 distributed → real=N-1=11.
		{"12 players (needs byes, pads to 16)", "playoffs-12p", 12, 11},
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

			// Count only non-auto-resolved (real court-time-consuming) matches.
			// Auto-resolved byes have Status==Completed at generation time
			// (bracket.go:102-111) and do NOT advance the court cursor
			// (scheduler_slots.go:286-291). The estimator must only count real bouts.
			realPlayableCount := 0
			for _, round := range bracket.Rounds {
				for _, m := range round {
					if m.Status != state.MatchStatusCompleted {
						realPlayableCount++
					}
				}
			}

			// Verify the estimator formula matches the real draw.
			assert.Equal(t, tc.wantBracket, realPlayableCount,
				"real non-bye bracket match count for %d players should be %d",
				tc.playerCount, tc.wantBracket)

			// Verify EstimateMatchCounts agrees with real playable count.
			poolCount, playoffCount, err := helper.EstimateMatchCounts(helper.EstimateMatchCountsInput{
				Format:      state.CompFormatPlayoffs,
				PlayerCount: tc.playerCount,
			})
			require.NoError(t, err)
			assert.Equal(t, 0, poolCount, "playoffs-only must have 0 pool matches")
			assert.Equal(t, realPlayableCount, playoffCount,
				"EstimateMatchCounts bracket count must match real non-bye match count for %d players", tc.playerCount)
		})
	}
}

// TestEstimateMatchCounts_vs_RealMixedDraw validates that
// helper.EstimateMatchCounts' pool AND bracket counts match the real draw
// pipeline for a mixed (Pools + Knockout) competition with full round-robin.
//
// This is the full mixed-format residual-risk guard: both the pool count
// formula and the bracket count formula are exercised against real code.
//
// wantBracket is the count of court-time-consuming bracket matches (Status !=
// Completed at generation time). For power-of-2 finalist counts this equals
// players-1 (no byes); for non-power-of-2 counts with paired byes it differs.
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
		wantBracket  int // court-time-consuming bracket matches (excl. auto-resolved byes)
	}{
		{
			// 6 players, poolSize 3 min-mode, RR, 2 winners.
			// 2 pools of 3 → pool matches: 2*C(3,2)=6; finalists=4, bracketMatchCount(4)=3.
			name: "6p size3 min rr winners2", compID: "mixed-6p-s3",
			playerCount: 6, poolSize: 3, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 6, wantBracket: 3,
		},
		{
			// 9 players, poolSize 3 min-mode, RR, 2 winners.
			// 3 pools of 3 → pool matches: 3*C(3,2)=9; finalists=6, bracketMatchCount(6)=5.
			// (pow2=8, byes=2 distributed to top seeds — mp-sess — so real=N-1=5)
			name: "9p size3 min rr winners2", compID: "mixed-9p-s3",
			playerCount: 9, poolSize: 3, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 9, wantBracket: 5,
		},
		{
			// 12 players, poolSize 4 min-mode, RR, 2 winners.
			// 3 pools of 4 → pool matches: 3*C(4,2)=18; finalists=6, bracketMatchCount(6)=5.
			name: "12p size4 min rr winners2", compID: "mixed-12p-s4",
			playerCount: 12, poolSize: 4, poolSizeMode: "min", poolWinners: 2, roundRobin: true,
			wantPools: 18, wantBracket: 5,
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

			// Complete all pool matches and start the playoffs to validate the
			// bracket count against the real bracket.
			for i := range poolMatches {
				poolMatches[i].Status = state.MatchStatusCompleted
				poolMatches[i].Winner = poolMatches[i].SideA
			}
			require.NoError(t, store.SavePoolMatches(compID, poolMatches))

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

			// Count only non-auto-resolved (real) matches — same as
			// TestEstimateMatchCounts_vs_RealPlayoffsDraw (Finding 3).
			realPlayableCount := 0
			for _, round := range bracket.Rounds {
				for _, m := range round {
					if m.Status != state.MatchStatusCompleted {
						realPlayableCount++
					}
				}
			}

			assert.Equal(t, tc.wantBracket, realPlayableCount,
				"real non-bye bracket match count for %d finalists should be %d (case %s)",
				finalists, tc.wantBracket, tc.name)
			assert.Equal(t, playoffCount, realPlayableCount,
				"EstimateMatchCounts bracket count must match real non-bye match count for %d finalists",
				finalists)
		})
	}
}
