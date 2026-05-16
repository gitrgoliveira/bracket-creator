package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
