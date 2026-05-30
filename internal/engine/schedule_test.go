package engine

import (
	"fmt"
	"math"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScheduleEstimatorClockMultiplier covers FR-055: a 3-minute on-clock
// match with the canonical 1.5x multiplier produces a 4.5-minute elapsed
// slot. Allow rounding to either neighbouring integer (4 or 5) — the
// estimator returns int-minutes for wire shape simplicity.
func TestScheduleEstimatorClockMultiplier(t *testing.T) {
	result := EstimateSchedule(EstimateInput{
		MatchDurationClockMinutes: 3,
		Multiplier:                1.5,
		NumMatches:                1,
		NumCourts:                 1,
	})
	assert.True(t, result.TotalDurationMinutes >= 4 && result.TotalDurationMinutes <= 5,
		"expected 4 or 5 minutes (4.5 rounded), got %d", result.TotalDurationMinutes)
	assert.Len(t, result.PerCourtMinutes, 1)
	assert.Equal(t, 0, result.CeremonyMinutes)
}

// TestScheduleEstimatorTeamMatchDuration covers FR-058: a 5-bout team
// match scales as bouts*clock*multiplier plus (bouts-1)*1 minute
// inter-bout transitions: 5*3*1.5 + 4 = 26.5 → 26 or 27.
func TestScheduleEstimatorTeamMatchDuration(t *testing.T) {
	result := EstimateSchedule(EstimateInput{
		MatchDurationClockMinutes: 3,
		Multiplier:                1.5,
		NumMatches:                1,
		NumCourts:                 1,
		TeamSize:                  5,
		BoutsPerTeamMatch:         5,
	})
	assert.True(t, result.TotalDurationMinutes >= 26 && result.TotalDurationMinutes <= 28,
		"expected 26-28 minutes for 5-bout team match, got %d", result.TotalDurationMinutes)
}

// TestScheduleEstimatorSlowestCourtBuffer covers FR-057: applying a
// 10–15% slowest-court buffer must increase the estimate vs. no buffer
// for the same inputs.
func TestScheduleEstimatorSlowestCourtBuffer(t *testing.T) {
	base := EstimateInput{
		MatchDurationClockMinutes: 3,
		Multiplier:                1.5,
		NumMatches:                20,
		NumCourts:                 2,
	}
	without := EstimateSchedule(base)

	withBuffer := base
	withBuffer.SlowestCourtBufferPct = 15
	bufferedResult := EstimateSchedule(withBuffer)

	assert.Greater(t, bufferedResult.TotalDurationMinutes, without.TotalDurationMinutes,
		"buffered estimate %d should exceed unbuffered %d",
		bufferedResult.TotalDurationMinutes, without.TotalDurationMinutes)
}

// TestExcelTimeEstimatorParity covers FR-059, SC-005: the Go calculator
// must agree with the Excel Time Estimator formulae within 5% for a
// canonical input. For 20 matches × 3min × 1.5× / 2 courts × 1.10
// buffer = 49.5 minutes, which rounds to 50.
func TestExcelTimeEstimatorParity(t *testing.T) {
	result := EstimateSchedule(EstimateInput{
		MatchDurationClockMinutes: 3,
		Multiplier:                1.5,
		NumMatches:                20,
		NumCourts:                 2,
		SlowestCourtBufferPct:     10,
	})

	const expected = 50
	delta := result.TotalDurationMinutes - expected
	if delta < 0 {
		delta = -delta
	}
	// 5% of 50 = 2.5; integer round-trip can land us at delta=3 in
	// adversarial rounding. Allow up to 3 minutes drift.
	assert.LessOrEqual(t, delta, 3,
		"estimate %d should be within ~5%% of Excel-derived %d (delta=%d)",
		result.TotalDurationMinutes, expected, delta)
}

// TestEstimateScheduleCeremonyAddsToTotal verifies CeremonyMinutes is
// passed through verbatim onto TotalDurationMinutes (the auto-scheduler
// integration in T150/T151 will eventually skip these slots; today the
// calculator just adds them to the total so callers can render the
// "all-in" estimate).
func TestEstimateScheduleCeremonyAddsToTotal(t *testing.T) {
	base := EstimateInput{
		MatchDurationClockMinutes: 3,
		Multiplier:                1.5,
		NumMatches:                10,
		NumCourts:                 1,
	}
	noCeremony := EstimateSchedule(base)

	withCeremony := base
	withCeremony.CeremonyMinutes = 60
	cer := EstimateSchedule(withCeremony)

	assert.Equal(t, 60, cer.CeremonyMinutes)
	assert.Equal(t, noCeremony.TotalDurationMinutes+60, cer.TotalDurationMinutes)
}

// TestEstimateScheduleCourtsClampedToOne defends against a 0-court input
// — we clamp rather than divide by zero.
func TestEstimateScheduleCourtsClampedToOne(t *testing.T) {
	result := EstimateSchedule(EstimateInput{
		MatchDurationClockMinutes: 3,
		Multiplier:                1.5,
		NumMatches:                4,
		NumCourts:                 0,
	})
	assert.Len(t, result.PerCourtMinutes, 1)
	assert.Greater(t, result.TotalDurationMinutes, 0)
}

// TestGenerateSchedule_MixedFormat verifies that GenerateSchedule produces
// "pool" type entries for a mixed (Pools + Knockout) competition.
func TestGenerateSchedule_MixedFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "gen-sched-pools"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Format: state.CompFormatMixed,
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "P1-0", Court: "A", Status: state.MatchStatusScheduled},
		{ID: "P1-1", Court: "B", Status: state.MatchStatusScheduled},
	}))

	err := eng.GenerateSchedule(compID)
	require.NoError(t, err)

	entries, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
	for _, e := range entries {
		assert.Equal(t, "pool", e.MatchType)
	}
}

// TestGenerateSchedule_BracketFormat verifies that GenerateSchedule produces
// "bracket" type entries for a bracket competition and defaults empty courts to "A".
func TestGenerateSchedule_BracketFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "gen-sched-bracket"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Format: state.CompFormatPlayoffs,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "B1", SideA: "Alice", SideB: "Bob", Court: "A"},
				{ID: "B2", SideA: "Charlie", SideB: "Dave"},
			},
		},
	}))

	err := eng.GenerateSchedule(compID)
	require.NoError(t, err)

	entries, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
	for _, e := range entries {
		assert.Equal(t, "bracket", e.MatchType)
	}
	// B2 has no court — should default to "A"
	var b2Entry state.ScheduleEntry
	for _, e := range entries {
		if e.MatchRef == "R1-MB2" {
			b2Entry = e
		}
	}
	assert.Equal(t, "A", b2Entry.Court, "empty court should default to 'A'")
}

// TestGenerateSchedule_SwissFormat verifies that GenerateSchedule produces
// "pool" type entries for a Swiss competition (matches live in pool-matches.csv,
// same as the Pools format).
func TestGenerateSchedule_SwissFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "gen-sched-swiss"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Format: state.CompFormatSwiss,
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Swiss-R1-0", Court: "A", Status: state.MatchStatusScheduled},
		{ID: "Swiss-R1-1", Court: "A", Status: state.MatchStatusScheduled},
	}))

	err := eng.GenerateSchedule(compID)
	require.NoError(t, err)

	entries, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "Swiss competition must produce one schedule entry per pool match")
	for _, e := range entries {
		assert.Equal(t, "pool", e.MatchType, "Swiss schedule entries must use 'pool' match type")
	}
}

// TestGenerateSchedule_CompNotFound verifies error when competition does not exist.
func TestGenerateSchedule_CompNotFound(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	err := eng.GenerateSchedule("no-such-comp")
	assert.Error(t, err)
}

// TestGenerateSchedule_NilBracket verifies no error and empty schedule when
// a bracket format competition has no bracket on disk yet.
func TestGenerateSchedule_NilBracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "gen-sched-nil-bracket"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Format: state.CompFormatPlayoffs,
	}))

	err := eng.GenerateSchedule(compID)
	require.NoError(t, err) // nil bracket → no entries, no error

	entries, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

// ---------------------------------------------------------------------------
// Tests for the Step 1 pure formula (perMatchElapsed)
// ---------------------------------------------------------------------------

// TestPerMatchElapsed_Individual verifies the individual-match branch:
// clockMin * multiplier (no bouts).
func TestPerMatchElapsed_Individual(t *testing.T) {
	tests := []struct {
		name       string
		clockMin   float64
		multiplier float64
		bouts      int
		want       float64
	}{
		{"3min 1.5x", 3, 1.5, 0, 4.5},
		{"5min 1.0x", 5, 1.0, 0, 5.0},
		{"4min 2.0x", 4, 2.0, 0, 8.0},
		{"zero clock", 0, 1.5, 0, 0.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := perMatchElapsed(tc.clockMin, tc.multiplier, tc.bouts)
			assert.InDelta(t, tc.want, got, 0.001)
		})
	}
}

// TestPerMatchElapsed_Team verifies the team-match branch:
// bouts*clockMin*multiplier + (bouts-1)*1.
func TestPerMatchElapsed_Team(t *testing.T) {
	tests := []struct {
		name       string
		clockMin   float64
		multiplier float64
		bouts      int
		want       float64
	}{
		// 5*3*1.5 + 4*1 = 22.5 + 4 = 26.5
		{"5 bouts 3min 1.5x", 3, 1.5, 5, 26.5},
		// 3*3*1.5 + 2*1 = 13.5 + 2 = 15.5
		{"3 bouts 3min 1.5x", 3, 1.5, 3, 15.5},
		// 1 bout: 1*5*2.0 + 0 = 10.0
		{"1 bout 5min 2.0x", 5, 2.0, 1, 10.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := perMatchElapsed(tc.clockMin, tc.multiplier, tc.bouts)
			assert.InDelta(t, tc.want, got, 0.001)
		})
	}
}

// ---------------------------------------------------------------------------
// Tests for EstimateForCounts (Step 2)
// ---------------------------------------------------------------------------

func newIndivComp(courts []string, poolDur, playoffDur int, startTime string) *state.Competition {
	return &state.Competition{
		Kind:                 "individual",
		Courts:               courts,
		PoolMatchDuration:    poolDur,
		PlayoffMatchDuration: playoffDur,
		StartTime:            startTime,
	}
}

func newTournament(multiplier float64, bufferPct int, opening, lunch, closing string) *state.Tournament {
	return &state.Tournament{
		ClockToElapsedMultiplier: multiplier,
		SlowestCourtBufferPct:    bufferPct,
		OpeningBlock:             opening,
		LunchBlock:               lunch,
		ClosingBlock:             closing,
	}
}

// TestEstimateForCounts_PerPhaseSplit verifies that pool and playoff matches
// each contribute their own per-phase duration to the total.
// SlowestCourtBufferPct=0 triggers the default (10%), which is applied by
// EstimateForCounts. Expected: 4*3 + 3*5 = 27 min * 1.10 = 29.7 → 30.
func TestEstimateForCounts_PerPhaseSplit(t *testing.T) {
	comp := newIndivComp([]string{"A"}, 3 /*pool clock*/, 5 /*playoff clock*/, "09:00")
	tourn := newTournament(1.0, 0, "", "", "")
	// pool: 4 matches * 3min = 12min; playoff: 3 matches * 5min = 15min = 27min
	// Default 10% buffer: 27 * 1.1 = 29.7 → 30.
	est := EstimateForCounts(4, 3, comp, tourn)
	assert.Equal(t, 30, est.TotalDurationMinutes)
	assert.Len(t, est.PerCourtMinutes, 1)
	assert.Equal(t, 30, est.PerCourtMinutes[0])
	// Verify playoff phase contributed more than pool phase by checking
	// a pool-only estimate is less than a playoff-only estimate of the same count.
	poolOnly := EstimateForCounts(4, 0, newIndivComp([]string{"A"}, 3, 5, "09:00"), newTournament(1.0, 0, "", "", ""))
	playoffOnly := EstimateForCounts(0, 4, newIndivComp([]string{"A"}, 3, 5, "09:00"), newTournament(1.0, 0, "", "", ""))
	assert.Less(t, poolOnly.TotalDurationMinutes, playoffOnly.TotalDurationMinutes,
		"playoff matches (5min clock) should produce a higher estimate than pool matches (3min clock) for equal count")
}

// TestEstimateForCounts_PoolThenPlayoffSequential pins the intentional
// pools-then-playoffs sequencing: a court runs its pool matches AND its playoff
// matches on the same advancing cursor, so the combined estimate is the SUM of
// the two phases — not the max() of them. This is the contract a post-draw
// estimate (mp-zoh) must reproduce by summing the two slot-assigner cursors
// rather than maxing them. See the EstimateForCounts doc comment.
func TestEstimateForCounts_PoolThenPlayoffSequential(t *testing.T) {
	comp := newIndivComp([]string{"A"}, 3 /*pool clock*/, 5 /*playoff clock*/, "09:00")
	tourn := newTournament(1.0, 10, "", "", "") // 1.0x, explicit 10% buffer

	// pool: 2*3=6 ; playoff: 2*5=10 ; sequential sum = 16 ; *1.10 = 17.6 → 18.
	combined := EstimateForCounts(2, 2, comp, tourn)
	poolOnly := EstimateForCounts(2, 0, newIndivComp([]string{"A"}, 3, 5, "09:00"), newTournament(1.0, 10, "", "", ""))
	playoffOnly := EstimateForCounts(0, 2, newIndivComp([]string{"A"}, 3, 5, "09:00"), newTournament(1.0, 10, "", "", ""))

	assert.Equal(t, 18, combined.TotalDurationMinutes)
	// The defining anti-max assertion: combined must exceed the larger single
	// phase. max() semantics would yield only playoffOnly (11).
	assert.Greater(t, combined.TotalDurationMinutes, playoffOnly.TotalDurationMinutes,
		"combined estimate must SUM the phases, not max them (combined=%d playoffOnly=%d)",
		combined.TotalDurationMinutes, playoffOnly.TotalDurationMinutes)
	assert.Equal(t, poolOnly.TotalDurationMinutes+playoffOnly.TotalDurationMinutes,
		combined.TotalDurationMinutes,
		"with no opening/lunch blocks the buffer is linear, so combined == poolOnly + playoffOnly")
}

// TestEstimateForCounts_TeamComp exercises the comp.Kind == "team" branch of
// perMatchElapsedMinutes through EstimateForCounts: a team match scales by bout
// count (= TeamSize) plus inter-bout transitions. All other EstimateForCounts
// tests use individual comps, so this guards the team code path.
func TestEstimateForCounts_TeamComp(t *testing.T) {
	team := &state.Competition{
		Kind:                 "team",
		TeamSize:             3,
		Courts:               []string{"A"},
		PoolMatchDuration:    2,
		PlayoffMatchDuration: 2,
		StartTime:            "09:00",
	}
	tourn := newTournament(1.5, 10, "", "", "")
	// Per team match (3 bouts): 3*2*1.5 + (3-1)*1 = 11. 2 matches on 1 court = 22.
	// 10% buffer (match time only): 22*1.1 = 24.2 → 24.
	est := EstimateForCounts(2, 0, team, tourn)
	assert.Equal(t, 24, est.TotalDurationMinutes)

	// Sanity: the team comp must exceed the individual equivalent (bouts scaling).
	indiv := EstimateForCounts(2, 0, newIndivComp([]string{"A"}, 2, 2, "09:00"), newTournament(1.5, 10, "", "", ""))
	assert.Greater(t, est.TotalDurationMinutes, indiv.TotalDurationMinutes,
		"team comp (%d) must scale by bouts above individual (%d)",
		est.TotalDurationMinutes, indiv.TotalDurationMinutes)
}

// TestEstimateForCounts_EvenDistribution verifies even distribution across courts.
// With SlowestCourtBufferPct=0 the default (10%) is applied: 2 matches *3min=6min
// per court * 1.1 = 6.6 → 7.
func TestEstimateForCounts_EvenDistribution(t *testing.T) {
	comp := newIndivComp([]string{"A", "B"}, 3, 3, "09:00")
	tourn := newTournament(1.0, 0, "", "", "")
	// 4 pool matches / 2 courts = 2 each * 3min = 6 min per court.
	// Default 10% buffer: 6 * 1.1 = 6.6 → 7.
	est := EstimateForCounts(4, 0, comp, tourn)
	assert.Equal(t, 7, est.TotalDurationMinutes)
	assert.Len(t, est.PerCourtMinutes, 2)
	// Both courts should have equal duration (balanced distribution).
	assert.Equal(t, est.PerCourtMinutes[0], est.PerCourtMinutes[1],
		"balanced fixture must produce equal per-court estimates")
}

// TestEstimateForCounts_LunchWindowStraddle verifies that when matches span the
// lunch window the total increases to accommodate the break.
// 11 matches from 11:30 at 3min/match: after 10 matches cursor=12:00, which is
// the lunch start → match 11's start is pushed to 13:00 → total > no-lunch total.
func TestEstimateForCounts_LunchWindowStraddle(t *testing.T) {
	// Use an explicit non-zero buffer (5%) so ApplyTournamentDefaults is a no-op
	// and the comparison between with/without-lunch is deterministic.
	comp := newIndivComp([]string{"A"}, 3, 3, "11:30")
	tourn := newTournament(1.0, 5, "", "1h", "") // 5% buffer, 1h lunch
	withLunch := EstimateForCounts(11, 0, comp, tourn)

	compNoLunch := newIndivComp([]string{"A"}, 3, 3, "11:30")
	tournNoLunch := newTournament(1.0, 5, "", "", "") // same but no lunch
	noLunch := EstimateForCounts(11, 0, compNoLunch, tournNoLunch)

	assert.Greater(t, withLunch.TotalDurationMinutes, noLunch.TotalDurationMinutes,
		"lunch window should increase total: withLunch=%d noLunch=%d",
		withLunch.TotalDurationMinutes, noLunch.TotalDurationMinutes)
}

// TestEstimateForCounts_NoLunchIfFinishesBefore verifies that when all matches
// complete before the lunch window starts, the total is unchanged.
func TestEstimateForCounts_NoLunchIfFinishesBefore(t *testing.T) {
	// 3 matches * 3min = 9min, starting at 09:00 → ends at 09:09, before lunch at 12:00.
	comp := newIndivComp([]string{"A"}, 3, 3, "09:00")
	tourn := newTournament(1.0, 0, "", "1h", "")
	with := EstimateForCounts(3, 0, comp, tourn)

	compNL := newIndivComp([]string{"A"}, 3, 3, "09:00")
	tournNL := newTournament(1.0, 0, "", "", "")
	without := EstimateForCounts(3, 0, compNL, tournNL)

	assert.Equal(t, without.TotalDurationMinutes, with.TotalDurationMinutes,
		"no matches fall in lunch window; totals should be equal: with=%d without=%d",
		with.TotalDurationMinutes, without.TotalDurationMinutes)
}

// TestEstimateForCounts_ClosingBlock verifies that CeremonyMinutes is populated
// from tournament.ClosingBlock and added to the total.
// With an explicit 5% buffer: 4*3=12min * 1.05 = 12.6 → 13, + 30 ceremony = 43.
func TestEstimateForCounts_ClosingBlock(t *testing.T) {
	comp := newIndivComp([]string{"A"}, 3, 3, "09:00")
	tourn := newTournament(1.0, 5, "", "", "30m") // 5% buffer, 30m closing block
	est := EstimateForCounts(4, 0, comp, tourn)

	assert.Equal(t, 30, est.CeremonyMinutes)
	// 4 * 3 = 12 match minutes * 1.05 = 12.6 → 13 + 30 ceremony = 43.
	assert.Equal(t, 43, est.TotalDurationMinutes)
}

// TestEstimateForCounts_BufferIncreasesTotal verifies that a larger
// SlowestCourtBufferPct produces a higher total than a smaller one.
// Both use explicit non-zero values so ApplyTournamentDefaults is a no-op.
func TestEstimateForCounts_BufferIncreasesTotal(t *testing.T) {
	comp := newIndivComp([]string{"A", "B"}, 3, 3, "09:00")
	tourn := newTournament(1.5, 5, "", "", "") // 5% buffer
	smallBuffer := EstimateForCounts(20, 0, comp, tourn)

	compB := newIndivComp([]string{"A", "B"}, 3, 3, "09:00")
	tournB := newTournament(1.5, 20, "", "", "") // 20% buffer
	largeBuffer := EstimateForCounts(20, 0, compB, tournB)

	assert.Greater(t, largeBuffer.TotalDurationMinutes, smallBuffer.TotalDurationMinutes,
		"20%% buffer should exceed 5%% buffer: large=%d small=%d",
		largeBuffer.TotalDurationMinutes, smallBuffer.TotalDurationMinutes)
}

// TestEstimateForCounts_BufferExcludesFixedBlocks verifies the slowest-court
// buffer is applied to MATCH time only — never to the fixed OpeningBlock offset.
// 1 court, 30m opening, 10 matches × (4min×1.5=6min) = 60 match-min, 10% buffer:
//
//	total = 30 (unbuffered opening) + round(60 × 1.10) = 30 + 66 = 96.
//
// A naive "buffer everything" would give round((30+60)×1.10) = 99, so 96 vs 99
// is the discriminating assertion (Copilot review #3326905303).
func TestEstimateForCounts_BufferExcludesFixedBlocks(t *testing.T) {
	comp := newIndivComp([]string{"A"}, 4, 4, "09:00")
	tourn := newTournament(1.5, 10, "30m", "", "") // 1.5x, 10% buffer, 30m opening
	est := EstimateForCounts(10, 0, comp, tourn)
	assert.Equal(t, 96, est.TotalDurationMinutes,
		"buffer must apply to match time only (expect 30+round(60*1.1)=96, not round(90*1.1)=99)")
}

// TestEstimateForCounts_NilComp verifies that a nil competition returns a zero estimate.
func TestEstimateForCounts_NilComp(t *testing.T) {
	est := EstimateForCounts(10, 5, nil, nil)
	assert.Equal(t, ScheduleEstimate{}, est)
}

// TestEstimateForCounts_NoCourts verifies that an empty courts slice defaults to
// a single court — matching the assigners (pools.go / bracket.go) and
// EstimateSchedule's NumCourts clamp — rather than returning a zero estimate
// (Copilot review #3328447507). The result must equal the explicit 1-court case.
func TestEstimateForCounts_NoCourts(t *testing.T) {
	empty := newIndivComp([]string{}, 3, 3, "09:00")
	oneCourt := newIndivComp([]string{"A"}, 3, 3, "09:00")
	estEmpty := EstimateForCounts(10, 5, empty, nil)
	estOne := EstimateForCounts(10, 5, oneCourt, nil)

	assert.Equal(t, estOne, estEmpty, "empty courts must behave as a single court")
	assert.Greater(t, estEmpty.TotalDurationMinutes, 0, "empty-courts estimate must not be zero")
	assert.Len(t, estEmpty.PerCourtMinutes, 1, "empty courts must yield exactly one per-court entry")
}

// TestEstimateForCounts_NegativeCountsClamped verifies negative match counts are
// clamped to 0 rather than producing negative/nonsensical durations
// (Copilot review #3326935837). Co-located with the other EstimateForCounts
// tests (#3328458142).
func TestEstimateForCounts_NegativeCountsClamped(t *testing.T) {
	comp := newIndivComp([]string{"A"}, 4, 4, "09:00")
	tourn := newTournament(1.5, 10, "", "", "")
	neg := EstimateForCounts(-5, -3, comp, tourn)
	zero := EstimateForCounts(0, 0, comp, tourn)
	assert.Equal(t, zero.TotalDurationMinutes, neg.TotalDurationMinutes,
		"negative counts must clamp to 0 (same as the empty estimate)")
	assert.GreaterOrEqual(t, neg.TotalDurationMinutes, 0, "duration must never be negative")
}

// TestEstimateForCounts_CourtsClampedToMax verifies an oversized Courts slice is
// clamped to MaxCourts (the A–Z cap), guarding the per-court allocations against
// a malformed/hostile Competition — same defensive bound as EstimateSchedule
// (Copilot review #3328458139).
func TestEstimateForCounts_CourtsClampedToMax(t *testing.T) {
	courts := make([]string, MaxCourts+50)
	for i := range courts {
		courts[i] = fmt.Sprintf("C%d", i)
	}
	comp := &state.Competition{Kind: "individual", Courts: courts, PoolMatchDuration: 3, StartTime: "09:00"}
	est := EstimateForCounts(100, 0, comp, newTournament(1.5, 10, "", "", ""))
	assert.Len(t, est.PerCourtMinutes, MaxCourts,
		"oversized Courts slice must clamp per-court entries to MaxCourts")
}

// ---------------------------------------------------------------------------
// Step 3: balanced fixture cross-path equality test
// ---------------------------------------------------------------------------

// TestEstimateForCountsVsSlotAssigner_Balanced asserts the cross-path
// relationship between EstimateForCounts (pre-draw) and assignPoolMatchSlots'
// end-cursor (post-draw) for a perfectly balanced fixture: they differ ONLY by
// the slowest-court buffer, which EstimateForCounts applies and the slot
// assigner does not. So EstimateForCounts.Total == round(slotDuration × (1 +
// buffer/100)).
//
// NOTE the buffer is set explicitly to 10 here: a zero buffer is unreachable
// because EstimateForCounts runs ApplyTournamentDefaults, which rewrites 0 → 10%
// (a true unbuffered EstimateForCounts cannot exist). The fixture uses
// integer-clean durations (4min × 1.5 = 6min/match) and a meaningful magnitude
// (10 matches/court = 60min) so the relationship is exercised, not coincidental.
func TestEstimateForCountsVsSlotAssigner_Balanced(t *testing.T) {
	const bufferPct = 10
	comp := &state.Competition{
		Kind:              "individual",
		Courts:            []string{"A", "B"},
		PoolMatchDuration: 4,
		StartTime:         "09:00",
	}
	tournament := &state.Tournament{
		ClockToElapsedMultiplier: 1.5,
		SlowestCourtBufferPct:    bufferPct, // explicit so ApplyTournamentDefaults is a no-op
	}

	// Balanced fixture: 20 pool matches, 10 per court.
	matches := make([]state.MatchResult, 0, 20)
	for i := range 10 {
		matches = append(matches,
			state.MatchResult{ID: fmt.Sprintf("A-%d", i), Court: "A"},
			state.MatchResult{ID: fmt.Sprintf("B-%d", i), Court: "B"})
	}
	_, maxCursor := assignPoolMatchSlots(matches, comp, tournament)

	dayStart := parseClockHHMM(comp.StartTime)
	slotDuration := maxCursor.Sub(dayStart).Minutes() // unbuffered (slot assigner ignores buffer)
	expected := int(math.Round(slotDuration * (1 + float64(bufferPct)/100)))

	est := EstimateForCounts(20, 0, comp, tournament)
	// EstimateForCounts == buffered slot duration. Allow 1 minute rounding tolerance.
	delta := est.TotalDurationMinutes - expected
	if delta < 0 {
		delta = -delta
	}
	assert.LessOrEqual(t, delta, 1,
		"EstimateForCounts (%d) should equal buffered slot-assigner duration (%d) for balanced fixture",
		est.TotalDurationMinutes, expected)
}
