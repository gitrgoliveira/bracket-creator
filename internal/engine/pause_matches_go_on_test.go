package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pause whose "Matches stop for this pause" box is unticked moves no match:
// the slots, the stored estimate and the draft estimate all run through it.
func TestPauseMatchesGoOn_SlotsRunThroughIt(t *testing.T) {
	comp := &state.Competition{StartTime: "09:00", PoolMatchDurationSeconds: 180, Courts: []string{"A"}}
	tourn := &state.Tournament{
		ClockToElapsedMultiplier: 1.5,
		Pauses:                   []state.Pause{{Name: "Opening ceremony", Start: "09:00", Length: "30m", MatchesGoOn: true}},
	}
	matches := []state.MatchResult{{ID: "p1-0", Court: "A"}, {ID: "p1-1", Court: "A"}}
	matches, _ = assignPoolMatchSlotsFrom(matches, comp, tourn, nil)
	assert.Equal(t, "09:00", matches[0].ScheduledAt)
	assert.Equal(t, "09:05", matches[1].ScheduledAt)
}

func TestPauseMatchesGoOn_EstimateIgnoresIt(t *testing.T) {
	comp := newIndivComp([]string{"A"}, 3, 3, "11:30")
	none := &state.Tournament{ClockToElapsedMultiplier: 1.0, SlowestCourtBufferPct: 10}
	goOn := &state.Tournament{
		ClockToElapsedMultiplier: 1.0, SlowestCourtBufferPct: 10,
		Pauses: []state.Pause{{Name: "Lunch", Start: "12:00", Length: "1h", MatchesGoOn: true}},
	}
	assert.Equal(t, EstimateForCounts(12, 0, comp, none), EstimateForCounts(12, 0, comp, goOn))
}

func TestPauseMatchesGoOn_DraftEstimateLeavesItOutOfItsPauses(t *testing.T) {
	tourn := &state.Tournament{
		Name: "T", Date: "01-05-2026", DurationDays: 1, Courts: []string{"A"},
		Pauses: []state.Pause{
			{Name: "Opening ceremony", Start: "09:00", Length: "30m", MatchesGoOn: true},
			{Name: "Lunch", Start: "12:00", Length: "45m"},
		},
	}
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(tourn))
	comp := &state.Competition{Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"}, StartTime: "09:00", Date: "01-05-2026"}
	draft, err := eng.EstimateDraft(comp, tourn, 8)
	require.NoError(t, err)
	require.Len(t, draft.Timing.Breaks, 1)
	assert.Equal(t, "Lunch", draft.Timing.Breaks[0].Name)
}

// The calendar still draws the pause on its day, flagged so the page can say
// matches go on through it, and does not list it as a pause the schedule skips.
func TestPauseMatchesGoOn_CalendarDrawsItFlagged(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Date: "01-07-2026", DurationDays: 1, Courts: []string{"A"},
		Pauses: []state.Pause{
			{Name: "Opening ceremony", Start: "09:00", Length: "30m", MatchesGoOn: true},
			{Name: "Lunch", Start: "12:00", Length: "1h"},
		},
	}))
	cal, err := eng.TournamentCalendar()
	require.NoError(t, err)
	require.Len(t, cal.Days, 1)
	require.Len(t, cal.Days[0].Breaks, 2)
	assert.Equal(t, "Opening ceremony", cal.Days[0].Breaks[0].Name)
	assert.True(t, cal.Days[0].Breaks[0].MatchesGoOn)
	assert.False(t, cal.Days[0].Breaks[1].MatchesGoOn)
	assert.Empty(t, cal.UnusedPauses)
}

func TestPauseMatchesGoOn_RetimeRunsThroughIt(t *testing.T) {
	tourn := retimeTournament([]string{"A", "B"}, state.Pause{Name: "Lunch", Start: "12:00", Length: "1h", MatchesGoOn: true})
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, tourn)
	_, err := retimeInTx(t, eng, store, comp, movedTo(comp, comp.Date, "11:50"))
	require.NoError(t, err)
	inside := false
	for _, at := range storedTimes(t, store, comp) {
		m := minutesOf(t, at)
		if m >= 12*60 && m < 13*60 {
			inside = true
		}
	}
	assert.True(t, inside, "matches start during a pause they go on through")
}
