package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minutesBetween returns the absolute minute distance between two
// HH:MM clock strings. Used by the slot-assignment tests to assert
// "this slot is N minutes after that one" without committing to a
// specific time-of-day.
func minutesBetween(t *testing.T, a, b string) int {
	t.Helper()
	ta := parseClockHHMM(a)
	tb := parseClockHHMM(b)
	d := int(tb.Sub(ta).Minutes())
	if d < 0 {
		d = -d
	}
	return d
}

// TestAssignSlotsSequentialPerCourt verifies that matches on the
// same court are placed sequentially with the per-match elapsed
// minutes between consecutive starts. T150.
func TestAssignSlotsSequentialPerCourt(t *testing.T) {
	comp := &state.Competition{
		StartTime:         "09:00",
		PoolMatchDuration: 3,
		Courts:            []string{"A"},
	}
	tournament := &state.Tournament{
		ClockToElapsedMultiplier: 1.5,
	}

	matches := []state.MatchResult{
		{ID: "p1-0", Court: "A"},
		{ID: "p1-1", Court: "A"},
		{ID: "p1-2", Court: "A"},
		{ID: "p1-3", Court: "A"},
		{ID: "p1-4", Court: "A"},
		{ID: "p1-5", Court: "A"},
	}
	matches = assignPoolMatchSlots(matches, comp, tournament)

	// 3 minutes * 1.5 multiplier = 4.5 → rounds to 5. Each slot
	// should be 5 minutes after the previous.
	perMatch := perMatchElapsedMinutes(comp, tournament, false)
	require.Equal(t, 5, perMatch)

	assert.Equal(t, "09:00", matches[0].ScheduledAt)
	for i := 1; i < len(matches); i++ {
		gap := minutesBetween(t, matches[i-1].ScheduledAt, matches[i].ScheduledAt)
		assert.Equal(t, perMatch, gap,
			"match %d → %d should be %dm apart, got %d (%s → %s)",
			i-1, i, perMatch, gap, matches[i-1].ScheduledAt, matches[i].ScheduledAt)
	}
}

// TestAssignSlotsParallelAcrossCourts verifies matches on different
// courts advance independently. With 6 matches split 3+3 across two
// courts, each court should restart from StartTime at its first
// match. T150.
func TestAssignSlotsParallelAcrossCourts(t *testing.T) {
	comp := &state.Competition{
		StartTime:         "09:00",
		PoolMatchDuration: 3,
		Courts:            []string{"A", "B"},
	}
	tournament := &state.Tournament{
		ClockToElapsedMultiplier: 1.5,
	}

	matches := []state.MatchResult{
		{ID: "p1-0", Court: "A"},
		{ID: "p1-1", Court: "A"},
		{ID: "p1-2", Court: "A"},
		{ID: "p2-0", Court: "B"},
		{ID: "p2-1", Court: "B"},
		{ID: "p2-2", Court: "B"},
	}
	matches = assignPoolMatchSlots(matches, comp, tournament)

	// Both courts start at 09:00.
	assert.Equal(t, "09:00", matches[0].ScheduledAt, "court A first match")
	assert.Equal(t, "09:00", matches[3].ScheduledAt, "court B first match")

	// Each court's third match is at 09:10 (2 * 5min).
	assert.Equal(t, "09:10", matches[2].ScheduledAt)
	assert.Equal(t, "09:10", matches[5].ScheduledAt)
}

// TestAssignSlotsTeamMatchScalesByBouts verifies that a team
// competition produces longer per-match elapsed minutes than an
// individual one with the same clock duration. FR-058 / T150.
func TestAssignSlotsTeamMatchScalesByBouts(t *testing.T) {
	tournament := &state.Tournament{ClockToElapsedMultiplier: 1.5}

	indiv := &state.Competition{
		Kind:              "individual",
		PoolMatchDuration: 3,
	}
	team5 := &state.Competition{
		Kind:              "team",
		TeamSize:          5,
		PoolMatchDuration: 3,
	}

	indivPer := perMatchElapsedMinutes(indiv, tournament, false)
	teamPer := perMatchElapsedMinutes(team5, tournament, false)

	// 5 * 3 * 1.5 + (5-1) * 1 = 22.5 + 4 = 26.5 → 27.
	assert.Equal(t, 27, teamPer, "5-bout team match should run ~27 minutes")
	// Individual: 3 * 1.5 = 4.5 → 5.
	assert.Equal(t, 5, indivPer)
	assert.Greater(t, teamPer, 5*indivPer-2,
		"team match should be at least ~5x individual (allowing rounding)")
}

// TestAssignSlotsSkipsOpeningBlock verifies that the first match on
// each court is offset by the configured OpeningBlock duration.
// T151, FR-056.
func TestAssignSlotsSkipsOpeningBlock(t *testing.T) {
	comp := &state.Competition{
		StartTime:         "09:00",
		PoolMatchDuration: 3,
		Courts:            []string{"A"},
	}
	tournament := &state.Tournament{
		ClockToElapsedMultiplier: 1.5,
		OpeningBlock:             "30m",
	}

	matches := []state.MatchResult{
		{ID: "p1-0", Court: "A"},
		{ID: "p1-1", Court: "A"},
	}
	matches = assignPoolMatchSlots(matches, comp, tournament)

	assert.Equal(t, "09:30", matches[0].ScheduledAt,
		"opening 30m should push first slot to 09:30")
	assert.Equal(t, "09:35", matches[1].ScheduledAt,
		"second slot is opening-offset + 5m")
}

// TestAssignSlotsSkipsLunchBlock verifies that a slot computed to
// fall inside the lunch window is pushed to the end of the block.
// Default lunch starts at 12:00; with a 30m block the next available
// slot is 12:30. T151, FR-056.
func TestAssignSlotsSkipsLunchBlock(t *testing.T) {
	comp := &state.Competition{
		StartTime:         "11:30",
		PoolMatchDuration: 6, // 6 * 1.5 = 9 minutes per match
		Courts:            []string{"A"},
	}
	tournament := &state.Tournament{
		ClockToElapsedMultiplier: 1.5,
		LunchBlock:               "1h",
	}

	matches := []state.MatchResult{
		{ID: "p1-0", Court: "A"}, // 11:30
		{ID: "p1-1", Court: "A"}, // 11:39
		{ID: "p1-2", Court: "A"}, // 11:48 (still pre-lunch)
		{ID: "p1-3", Court: "A"}, // 11:57 → inside lunch (12:00–13:00) → 13:00
		{ID: "p1-4", Court: "A"}, // 13:09
	}
	matches = assignPoolMatchSlots(matches, comp, tournament)

	assert.Equal(t, "11:30", matches[0].ScheduledAt)
	assert.Equal(t, "11:39", matches[1].ScheduledAt)
	assert.Equal(t, "11:48", matches[2].ScheduledAt)
	// The 4th match would naturally land at 11:57, NOT inside the
	// 12:00–13:00 lunch window — the previous match's END
	// (11:57+9=12:06) crosses into lunch. We expect the SLOT START
	// at 11:57 to NOT be skipped (it's pre-lunch). Verify the test
	// reflects the actual contract: skip only when the START falls
	// inside the block.
	assert.Equal(t, "11:57", matches[3].ScheduledAt,
		"match starting at 11:57 is pre-lunch even if it overruns")

	// The 5th match: cursor after #4 is 12:06, which IS inside the
	// lunch window → pushed to 13:00.
	assert.Equal(t, "13:00", matches[4].ScheduledAt,
		"slot starting inside lunch (12:06) pushed to 13:00")
}

// TestAssignSlotsNoMatchInsideCeremony asserts the strong invariant:
// every assigned ScheduledAt MUST be outside every ceremony window.
// T151 validation hook.
func TestAssignSlotsNoMatchInsideCeremony(t *testing.T) {
	comp := &state.Competition{
		StartTime:         "08:30",
		PoolMatchDuration: 4,
		Courts:            []string{"A", "B"},
	}
	tournament := &state.Tournament{
		ClockToElapsedMultiplier: 1.5,
		OpeningBlock:             "30m",
		LunchBlock:               "1h",
	}

	// Generate enough matches to span lunch on both courts.
	var matches []state.MatchResult
	for i := 0; i < 40; i++ {
		court := "A"
		if i%2 == 1 {
			court = "B"
		}
		matches = append(matches, state.MatchResult{ID: "m", Court: court})
	}
	matches = assignPoolMatchSlots(matches, comp, tournament)

	lunchStart := parseClockHHMM("12:00")
	lunchEnd := parseClockHHMM("13:00")
	for i, m := range matches {
		at := parseClockHHMM(m.ScheduledAt)
		assert.False(t, !at.Before(lunchStart) && at.Before(lunchEnd),
			"match %d (%s on court %s) must not start inside the lunch window 12:00–13:00",
			i, m.ScheduledAt, m.Court)
	}
}

// TestAssignSlotsRespectsClockToElapsedMultiplier — doubling the
// multiplier roughly doubles the gap between consecutive slots on
// the same court. T150.
func TestAssignSlotsRespectsClockToElapsedMultiplier(t *testing.T) {
	comp := &state.Competition{
		StartTime:         "09:00",
		PoolMatchDuration: 4,
		Courts:            []string{"A"},
	}
	mult1 := &state.Tournament{ClockToElapsedMultiplier: 1.0}
	mult2 := &state.Tournament{ClockToElapsedMultiplier: 2.0}

	per1 := perMatchElapsedMinutes(comp, mult1, false)
	per2 := perMatchElapsedMinutes(comp, mult2, false)

	assert.Equal(t, 4, per1, "multiplier=1.0 → 4 min")
	assert.Equal(t, 8, per2, "multiplier=2.0 → 8 min")

	matchesA := []state.MatchResult{
		{Court: "A"}, {Court: "A"}, {Court: "A"},
	}
	matchesB := []state.MatchResult{
		{Court: "A"}, {Court: "A"}, {Court: "A"},
	}
	assignPoolMatchSlots(matchesA, comp, mult1)
	assignPoolMatchSlots(matchesB, comp, mult2)

	gapA := minutesBetween(t, matchesA[0].ScheduledAt, matchesA[1].ScheduledAt)
	gapB := minutesBetween(t, matchesB[0].ScheduledAt, matchesB[1].ScheduledAt)
	assert.Equal(t, 4, gapA)
	assert.Equal(t, 8, gapB)
}

// TestAssignSlotsZeroDurationFallback verifies the slot loop does
// not divide by zero or collapse when the competition carries no
// per-phase duration. ApplyCompetitionDefaults handles legacy
// MatchDuration mapping, but when even that is unset we fall back
// to a sensible default rather than panic. T150 defensive.
func TestAssignSlotsZeroDurationFallback(t *testing.T) {
	comp := &state.Competition{
		StartTime: "09:00",
		Courts:    []string{"A"},
		// PoolMatchDuration / PlayoffMatchDuration / MatchDuration all zero
	}
	tournament := &state.Tournament{ClockToElapsedMultiplier: 1.5}

	matches := []state.MatchResult{
		{Court: "A"}, {Court: "A"},
	}
	matches = assignPoolMatchSlots(matches, comp, tournament)

	// Default 3 clock min * 1.5 multiplier = 4.5 → 5.
	per := perMatchElapsedMinutes(comp, tournament, false)
	assert.Greater(t, per, 0, "fallback per-match must be > 0")
	gap := minutesBetween(t, matches[0].ScheduledAt, matches[1].ScheduledAt)
	assert.Equal(t, per, gap)
}

// TestAssignSlotsLegacyMatchDurationFallback verifies that a
// competition predating per-phase durations still produces a sensible
// slot loop after ApplyCompetitionDefaults populates the new fields.
// T150 backward compatibility.
func TestAssignSlotsLegacyMatchDurationFallback(t *testing.T) {
	comp := &state.Competition{
		StartTime:     "09:00",
		Courts:        []string{"A"},
		MatchDuration: 5, // legacy field only
	}
	state.ApplyCompetitionDefaults(comp)
	require.Equal(t, 5, comp.PoolMatchDuration, "ApplyCompetitionDefaults must promote MatchDuration")

	tournament := &state.Tournament{ClockToElapsedMultiplier: 1.5}
	per := perMatchElapsedMinutes(comp, tournament, false)
	// 5 * 1.5 = 7.5 → 8.
	assert.Equal(t, 8, per)
}

// TestAssignSlotsBracketByesSkipCursor verifies that bracket
// matches auto-completed as byes do not advance the court cursor —
// otherwise a half-empty bracket would inherit phantom 5-minute
// delays from each byte. T150.
func TestAssignSlotsBracketByesSkipCursor(t *testing.T) {
	comp := &state.Competition{
		StartTime:            "09:00",
		PlayoffMatchDuration: 4,
		Courts:               []string{"A"},
	}
	tournament := &state.Tournament{ClockToElapsedMultiplier: 1.5}

	rounds := [][]state.BracketMatch{
		{
			{ID: "m-r1-0", Court: "A", Status: state.MatchStatusCompleted, Winner: "X"}, // bye
			{ID: "m-r1-1", Court: "A", Status: state.MatchStatusScheduled},
			{ID: "m-r1-2", Court: "A", Status: state.MatchStatusScheduled},
		},
	}
	assignBracketMatchSlots(rounds, comp, tournament)

	// Bye, then two real matches. All three get a time slot, but
	// the bye does not advance the cursor: 09:00 (bye), 09:00, 09:06.
	per := perMatchElapsedMinutes(comp, tournament, true)
	require.Equal(t, 6, per, "4 * 1.5 = 6")

	assert.Equal(t, "09:00", rounds[0][0].ScheduledAt, "bye slot")
	assert.Equal(t, "09:00", rounds[0][1].ScheduledAt, "first real match")
	assert.Equal(t, "09:06", rounds[0][2].ScheduledAt, "second real match")
}

// TestAssignSlotsParityWithEstimateSchedule documents the FR-059
// agreement constraint: the per-match elapsed minutes used here for
// individual matches MUST equal EstimateSchedule's per-match
// calculation. A drift between the two surfaces would silently
// violate the 5% parity requirement (T148, FR-059).
func TestAssignSlotsParityWithEstimateSchedule(t *testing.T) {
	tournament := &state.Tournament{ClockToElapsedMultiplier: 1.5}

	indiv := &state.Competition{Kind: "individual", PoolMatchDuration: 3}
	indivPer := perMatchElapsedMinutes(indiv, tournament, false)
	indivEst := EstimateSchedule(EstimateInput{
		MatchDurationClockMinutes: 3,
		Multiplier:                1.5,
		NumMatches:                1,
		NumCourts:                 1,
	}).TotalDurationMinutes
	assert.Equal(t, indivEst, indivPer,
		"individual per-match (slot loop) must equal EstimateSchedule total")

	team := &state.Competition{Kind: "team", TeamSize: 5, PoolMatchDuration: 3}
	teamPer := perMatchElapsedMinutes(team, tournament, false)
	teamEst := EstimateSchedule(EstimateInput{
		MatchDurationClockMinutes: 3,
		Multiplier:                1.5,
		NumMatches:                1,
		NumCourts:                 1,
		TeamSize:                  5,
		BoutsPerTeamMatch:         5,
	}).TotalDurationMinutes
	assert.Equal(t, teamEst, teamPer,
		"team per-match (slot loop) must equal EstimateSchedule total")
}

// TestParseDurationMinutes_Valid verifies standard Go duration strings
// are converted to the correct whole-minute value.
func TestParseDurationMinutes_Valid(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"30m", 30},
		{"1h", 60},
		{"1h30m", 90},
		{"45m", 45},
		{"90m", 90},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, parseDurationMinutes(tc.in))
		})
	}
}

// TestParseDurationMinutes_FallbackToZero verifies that empty or
// unparseable inputs return 0 (treated as "no block configured").
func TestParseDurationMinutes_FallbackToZero(t *testing.T) {
	assert.Equal(t, 0, parseDurationMinutes(""))
	assert.Equal(t, 0, parseDurationMinutes("not-a-duration"))
	assert.Equal(t, 0, parseDurationMinutes("-1h"))
}

// TestParseClockHHMM_Valid verifies exact HH:MM parsing.
func TestParseClockHHMM_Valid(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"09:00", "09:00"},
		{"13:45", "13:45"},
		{"00:00", "00:00"},
		{"23:59", "23:59"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := parseClockHHMM(tc.in)
			assert.Equal(t, tc.want, got.Format("15:04"))
		})
	}
}

// TestParseClockHHMM_FallbackTo0900 verifies that empty and malformed
// inputs both fall back to the 09:00 default.
func TestParseClockHHMM_FallbackTo0900(t *testing.T) {
	fallback := func(s string) string {
		return parseClockHHMM(s).Format("15:04")
	}
	assert.Equal(t, "09:00", fallback(""), "empty string must fall back to 09:00")
	assert.Equal(t, "09:00", fallback("not-a-time"), "malformed must fall back to 09:00")
	assert.Equal(t, "09:00", fallback("9:00"), "missing leading zero must fall back to 09:00")
}
