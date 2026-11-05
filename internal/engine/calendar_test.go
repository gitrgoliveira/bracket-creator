package engine

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalendar_BeforeDrawParityWithClashes(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A", "B"},
	}))

	emptyComp(t, store, "a", "Alpha", "01-07-2026", "09:00", []string{"A"})
	emptyComp(t, store, "b", "Bravo", "01-07-2026", "09:15", []string{"A"})

	clashes, err := eng.DetectClashesForCompetition("a")
	require.NoError(t, err)
	require.Len(t, clashes, 1)
	assert.Equal(t, "b", clashes[0].OtherCompID)
	assert.Equal(t, "09:15", clashes[0].OverlapStart)
	assert.Equal(t, "09:30", clashes[0].OverlapEnd)

	cal, err := eng.TournamentCalendar()
	require.NoError(t, err)
	require.Len(t, cal.Competitions, 2)

	var compA, compB *CalendarCompetition
	for i := range cal.Competitions {
		if cal.Competitions[i].ID == "a" {
			compA = &cal.Competitions[i]
		}
		if cal.Competitions[i].ID == "b" {
			compB = &cal.Competitions[i]
		}
	}
	require.NotNil(t, compA)
	require.NotNil(t, compB)
	assert.Equal(t, []string{"b"}, compA.ClashPartners)
	assert.Equal(t, []string{"a"}, compB.ClashPartners)
}

func TestCalendar_DrawnCompPushedByCourtFloorNoLongerClashes(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}))

	// comp1: drawn, runs 09:00 to 10:00 on Court A
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        "c1",
		Name:      "Early",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Courts:    []string{"A"},
		Status:    state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "m1", Court: "A", ScheduledAt: "09:00", Status: state.MatchStatusScheduled}, // 09:00 to 10:00
	}))

	// comp2: drawn, starts nominally at 09:00, but its slots were scheduled at 10:00
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        "c2",
		Name:      "Pushed",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Courts:    []string{"A"},
		Status:    state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SavePoolMatches("c2", []state.MatchResult{
		{ID: "m2", Court: "A", ScheduledAt: "10:00", Status: state.MatchStatusScheduled}, // 10:00 to 11:00
	}))

	clashes, err := eng.DetectClashesForCompetition("c1")
	require.NoError(t, err)
	assert.Empty(t, clashes, "pushed competition does not clash with earlier one")

	cal, err := eng.TournamentCalendar()
	require.NoError(t, err)
	for _, c := range cal.Competitions {
		assert.Empty(t, c.ClashPartners)
	}
}

func TestCalendar_SetupCompWithLeftoverSlotsPlacedByStartTime(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}))

	// Competition in setup with leftover slots from a failed draw at 15:00
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        "leftover",
		Name:      "Leftover",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Courts:    []string{"A"},
		Status:    state.CompStatusSetup,
	}))
	require.NoError(t, store.SavePoolMatches("leftover", []state.MatchResult{
		{ID: "old-slot", Court: "A", ScheduledAt: "15:00", Status: state.MatchStatusScheduled},
	}))

	tourn, _ := store.LoadTournament()
	comp, _ := store.LoadCompetition("leftover")
	place := eng.PlaceCompetition(comp, tourn)

	require.True(t, place.Placed)
	assert.Equal(t, "minimum", place.LengthSource)
	// Must be placed starting at 09:00 (540m), NOT at 15:00
	assert.Equal(t, 9*60, place.Windows["A"].Start)
}

func TestCalendar_NotPlacedReasons(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}))
	tourn, _ := store.LoadTournament()

	t.Run("no start time", func(t *testing.T) {
		comp := &state.Competition{
			ID:     "no-start",
			Date:   "01-07-2026",
			Status: state.CompStatusSetup,
		}
		place := eng.PlaceCompetition(comp, tourn)
		assert.False(t, place.Placed)
		assert.Equal(t, "no_start_time", place.NotPlacedReason)
	})

	t.Run("no date", func(t *testing.T) {
		comp := &state.Competition{
			ID:        "no-date",
			StartTime: "09:00",
			Status:    state.CompStatusSetup,
		}
		place := eng.PlaceCompetition(comp, tourn)
		assert.False(t, place.Placed)
		assert.Equal(t, "no_date", place.NotPlacedReason)
	})

	t.Run("date outside tournament", func(t *testing.T) {
		comp := &state.Competition{
			ID:        "wrong-date",
			Date:      "05-07-2026",
			StartTime: "09:00",
			Status:    state.CompStatusSetup,
		}
		place := eng.PlaceCompetition(comp, tourn)
		assert.False(t, place.Placed)
		assert.Equal(t, "date_outside_tournament", place.NotPlacedReason)
	})
}

func TestCalendar_DateOutsideTournamentAfterDayCountShrinks(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 2,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:        "day2",
		Date:      "02-07-2026",
		StartTime: "09:00",
		Status:    state.CompStatusSetup,
	}
	require.NoError(t, store.SaveCompetition(comp))

	// Day 2 exists when DurationDays is 2
	place := eng.PlaceCompetition(comp, tourn)
	assert.True(t, place.Placed)
	assert.Equal(t, 2, place.Day)

	// Shrink duration to 1 day
	tourn.DurationDays = 1
	require.NoError(t, store.SaveTournament(tourn))

	placeAfter := eng.PlaceCompetition(comp, tourn)
	assert.False(t, placeAfter.Placed)
	assert.Equal(t, "date_outside_tournament", placeAfter.NotPlacedReason)
}

func TestCalendar_PastMidnight(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:        "late",
		Date:      "01-07-2026",
		StartTime: "23:45",
		Courts:    []string{"A"},
		Status:    state.CompStatusSetup,
	}
	require.NoError(t, store.SaveCompetition(comp))

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.True(t, place.PastMidnight)
	// 23:45 (1425m) + 30m min footprint = 1455m (24:15)
	assert.Equal(t, 1425, place.Windows["A"].Start)
	assert.Equal(t, 1455, place.Windows["A"].End)
}

func TestCalendar_EstimateFailurePlacedAtMinimum(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	// Invalid format configuration causing estimate failure
	comp := &state.Competition{
		ID:        "bad-format",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Format:    "invalid_format",
		Courts:    []string{"A"},
		Status:    state.CompStatusSetup,
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, "bad-format", []string{"P1", "P2"})

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.Equal(t, "minimum", place.LengthSource)
	assert.Equal(t, "estimate_failed", place.LengthReason)
	assert.Equal(t, 9*60+MinClashFootprintMinutes, place.Windows["A"].End)
}

func TestCalendar_NoParticipantsAndNoExpectedCountAtMinimum(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:                   "empty",
		Date:                 "01-07-2026",
		StartTime:            "09:00",
		Courts:               []string{"A"},
		ExpectedParticipants: 0,
		Status:               state.CompStatusSetup,
	}
	require.NoError(t, store.SaveCompetition(comp))

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.Equal(t, "minimum", place.LengthSource)
	assert.Equal(t, "no_participants", place.LengthReason)
	assert.Equal(t, 9*60+MinClashFootprintMinutes, place.Windows["A"].End)
}

func TestPlaceCompetition_PastSetupEmptyRosterWithExpectedIsNoParticipantsMinimum(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:                   "past-setup-empty",
		Date:                 "01-07-2026",
		StartTime:            "09:00",
		Courts:               []string{"A"},
		ExpectedParticipants: 16,
		Status:               state.CompStatusDrawReady,
	}
	require.NoError(t, store.SaveCompetition(comp))

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.Equal(t, "minimum", place.LengthSource)
	assert.Equal(t, "no_participants", place.LengthReason)
	assert.Equal(t, 1, place.Day)
	assert.Equal(t, []string{"A"}, place.Courts)
	assert.Equal(t, 9*60, place.Windows["A"].Start)
	assert.Equal(t, 9*60+MinClashFootprintMinutes, place.Windows["A"].End)
	assert.Equal(t, MinClashFootprintMinutes, place.Minutes)
}

func TestCalendar_ExpectedParticipantsCountWithNoRoster(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:                           "expected-comp",
		Date:                         "01-07-2026",
		StartTime:                    "09:00",
		Format:                       state.CompFormatKnockout,
		Courts:                       []string{"A"},
		ExpectedParticipants:         24,
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 180,
		Status:                       state.CompStatusSetup,
	}
	require.NoError(t, store.SaveCompetition(comp))

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.Equal(t, "expected", place.LengthSource)
	assert.Greater(t, place.Windows["A"].End-place.Windows["A"].Start, MinClashFootprintMinutes)
}

func TestPlaceCompetitionInheritsTournamentCourts(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A", "B"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:                           "setup-comp",
		Date:                         "01-07-2026",
		StartTime:                    "09:00",
		Format:                       state.CompFormatKnockout,
		Courts:                       []string{},
		ExpectedParticipants:         24,
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 180,
		Status:                       state.CompStatusSetup,
	}
	require.NoError(t, store.SaveCompetition(comp))

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.Equal(t, []string{"A", "B"}, place.Courts)
	assert.Len(t, place.Windows, 2)
	assert.Contains(t, place.Windows, "A")
	assert.Contains(t, place.Windows, "B")
	assert.NotEmpty(t, place.ReplayRecipes["A"].Runs)
	assert.NotEmpty(t, place.ReplayRecipes["B"].Runs)
}

func TestCalendar_BreaksAndUnusedPauses(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 2,
		Courts:       []string{"A", "B"},
		Pauses: []state.Pause{
			{Name: "Lunch", Start: "12:00", Length: "1h", Day: 0},
			{Name: "Grading", Start: "15:00", Length: "30m", Day: 1},
			{Name: "Future Day", Start: "10:00", Length: "1h", Day: 5}, // Day 5 > 2
			{Name: "No Length", Start: "10:00", Length: "", Day: 1},
			{Name: "No Start", Start: "", Length: "30m", Day: 1},
		},
	}
	require.NoError(t, store.SaveTournament(tourn))

	cal, err := eng.TournamentCalendar()
	require.NoError(t, err)

	require.Len(t, cal.Days, 2)
	require.Len(t, cal.Days[0].Breaks, 2, "Day 1 has Lunch and Grading")
	assert.Equal(t, "Lunch", cal.Days[0].Breaks[0].Name)
	assert.Equal(t, "Grading", cal.Days[0].Breaks[1].Name)

	require.Len(t, cal.Days[1].Breaks, 1, "Day 2 has Lunch only")
	assert.Equal(t, "Lunch", cal.Days[1].Breaks[0].Name)

	unusedReasons := make(map[string]string)
	for _, u := range cal.UnusedPauses {
		unusedReasons[u.Name] = u.Reason
	}
	assert.Equal(t, "day_not_in_tournament", unusedReasons["Future Day"])
	assert.Equal(t, "no_length", unusedReasons["No Length"])
	assert.Equal(t, "no_start_time", unusedReasons["No Start"])
}

func TestCalendar_NoTournamentDatePlacedWithDayZero(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:   "No Date Tourn",
		Courts: []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:        "comp-nodate",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Courts:    []string{"A"},
		Status:    state.CompStatusSetup,
	}
	require.NoError(t, store.SaveCompetition(comp))

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.Equal(t, 0, place.Day)
}

func TestCalendar_KachinukiTeamBeforeDraw(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:                           "kachinuki-comp",
		Name:                         "Kachinuki Comp",
		Date:                         "01-07-2026",
		StartTime:                    "09:00",
		Kind:                         "team",
		TeamSize:                     5,
		TeamMatchType:                state.TeamMatchTypeKachinuki,
		Courts:                       []string{"A"},
		ExpectedParticipants:         8,
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 180,
		Status:                       state.CompStatusSetup,
	}
	require.NoError(t, store.SaveCompetition(comp))

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.NotEmpty(t, place.BestEnd)
	assert.NotEmpty(t, place.WorstEnd)
	assert.Less(t, place.BestEnd, place.WorstEnd)

	recipe, ok := place.ReplayRecipes["A"]
	require.True(t, ok)
	assert.NotEmpty(t, recipe.Runs)
	assert.NotEmpty(t, recipe.BestRuns)
	assert.NotEmpty(t, recipe.WorstRuns)
}

func TestCalendar_DrawnCompetitionRecipeSlotLengthsInStartOrder(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:                           "drawn-comp",
		Date:                         "01-07-2026",
		StartTime:                    "09:00",
		Courts:                       []string{"A"},
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 240,
		Status:                       state.CompStatusDrawReady,
	}
	require.NoError(t, store.SaveCompetition(comp))

	// Save matches in non-chronological order: 09:20 before 09:00
	require.NoError(t, store.SavePoolMatches("drawn-comp", []state.MatchResult{
		{ID: "m2", Court: "A", ScheduledAt: "09:20", Status: state.MatchStatusScheduled},
		{ID: "m1", Court: "A", ScheduledAt: "09:00", Status: state.MatchStatusScheduled},
	}))

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.Equal(t, "draw", place.LengthSource)

	recipe, ok := place.ReplayRecipes["A"]
	require.True(t, ok)
	require.Len(t, recipe.SlotLengths, 2)
	// Duration for pool match with 180s (3min) and 1.5x multiplier = 5m
	assert.Equal(t, []int{5, 5}, recipe.SlotLengths)
	assert.Equal(t, 9*60, place.Windows["A"].Start)
	assert.Equal(t, 9*60+25, place.Windows["A"].End)
	assert.Equal(t, 25, place.Minutes)
}

func TestCalendar_DrawnCompetitionHoldingUnlistedCourtShowsInLanes(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:        "extra-court-comp",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Courts:    []string{"A"}, // Only "A" listed
		Status:    state.CompStatusDrawReady,
	}
	require.NoError(t, store.SaveCompetition(comp))

	// Has match on court "B" (not listed in comp.Courts)
	require.NoError(t, store.SavePoolMatches("extra-court-comp", []state.MatchResult{
		{ID: "m1", Court: "B", ScheduledAt: "09:00", Status: state.MatchStatusScheduled},
	}))

	cal, err := eng.TournamentCalendar()
	require.NoError(t, err)
	assert.Contains(t, cal.Courts, "B", "unlisted court held by drawn competition must appear in lanes")
}

func TestCalendar_ClashPartnersUseWindowKeys(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A", "B"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	// Drawn competition with Courts ["A", "B"], but slots only on Court "B"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        "drawn-on-b",
		Name:      "Drawn on B",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Courts:    []string{"A", "B"},
		Status:    state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SavePoolMatches("drawn-on-b", []state.MatchResult{
		{ID: "mb1", Court: "B", ScheduledAt: "09:00", Status: state.MatchStatusScheduled},
	}))

	// Setup competition on Court "A" at the same time
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        "setup-on-a",
		Name:      "Setup on A",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Courts:    []string{"A"},
		Status:    state.CompStatusSetup,
	}))

	cal, err := eng.TournamentCalendar()
	require.NoError(t, err)

	var compDrawn, compSetup *CalendarCompetition
	for i := range cal.Competitions {
		if cal.Competitions[i].ID == "drawn-on-b" {
			compDrawn = &cal.Competitions[i]
		}
		if cal.Competitions[i].ID == "setup-on-a" {
			compSetup = &cal.Competitions[i]
		}
	}
	require.NotNil(t, compDrawn)
	require.NotNil(t, compSetup)
	assert.Empty(t, compDrawn.ClashPartners, "drawn comp with slots on B only should not clash with setup comp on A")
	assert.Empty(t, compSetup.ClashPartners, "setup comp on A should not clash with drawn comp with slots on B only")
}

func TestCalendar_PlaceCompetitionDoesNotMutateCallerCourts(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"X", "Y"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:        "drawn-mut",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Courts:    []string{"B", "A"},
		Status:    state.CompStatusDrawReady,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches("drawn-mut", []state.MatchResult{
		{ID: "m1", Court: "B", ScheduledAt: "09:00", Status: state.MatchStatusScheduled},
		{ID: "m2", Court: "C", ScheduledAt: "09:20", Status: state.MatchStatusScheduled},
	}))

	place := eng.PlaceCompetition(comp, tourn)
	require.True(t, place.Placed)
	assert.Equal(t, []string{"A", "B", "C"}, place.Courts)
	assert.Equal(t, []string{"B", "A"}, comp.Courts, "comp.Courts must not be mutated or reordered")
	assert.Equal(t, []string{"X", "Y"}, tourn.Courts, "tourn.Courts must not be mutated")
}

func TestCalendar_DrawnCompWithUnreadableWindowsNotPlaced(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	tourn := &state.Tournament{
		Name:         "Tourn",
		Date:         "01-07-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}
	require.NoError(t, store.SaveTournament(tourn))

	comp := &state.Competition{
		ID:        "unreadable-comp",
		Date:      "01-07-2026",
		StartTime: "09:00",
		Courts:    []string{"A"},
		Status:    state.CompStatusDrawReady,
	}
	require.NoError(t, store.SaveCompetition(comp))

	// Make pool-matches.csv unreadable by creating a directory where it belongs
	path := filepath.Join(dir, "competitions", "unreadable-comp", "pool-matches.csv")
	require.NoError(t, os.MkdirAll(path, 0o755))

	var logBuf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origOutput)

	place := eng.PlaceCompetition(comp, tourn)
	assert.False(t, place.Placed)
	assert.Equal(t, "draw_unreadable", place.NotPlacedReason)
	assert.Contains(t, logBuf.String(), "unreadable-comp")
}

func TestTournamentCalendar_EmptySlicesAreNeverNil(t *testing.T) {
	t.Run("store with tournament and no competitions", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name:         "Tourn",
			Date:         "01-07-2026",
			DurationDays: 1,
			Courts:       []string{"A"},
		}))

		cal, err := eng.TournamentCalendar()
		require.NoError(t, err)

		assert.NotNil(t, cal.Days)
		for _, d := range cal.Days {
			assert.NotNil(t, d.Breaks)
		}
		assert.NotNil(t, cal.Courts)
		assert.NotNil(t, cal.UnusedPauses)
		assert.NotNil(t, cal.Competitions)
		for _, c := range cal.Competitions {
			assert.NotNil(t, c.ClashPartners)
		}
	})

	t.Run("store with one competition that clashes with nothing", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name:         "Tourn",
			Date:         "01-07-2026",
			DurationDays: 1,
			Courts:       []string{"A"},
		}))

		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:                   "c1",
			Name:                 "Solo Comp",
			Date:                 "01-07-2026",
			StartTime:            "09:00",
			Courts:               []string{"A"},
			Format:               state.CompFormatKnockout,
			ExpectedParticipants: 8,
			Status:               state.CompStatusSetup,
		}))

		cal, err := eng.TournamentCalendar()
		require.NoError(t, err)

		assert.NotNil(t, cal.Days)
		for _, d := range cal.Days {
			assert.NotNil(t, d.Breaks)
		}
		assert.NotNil(t, cal.Courts)
		assert.NotNil(t, cal.UnusedPauses)
		assert.NotNil(t, cal.Competitions)
		require.Len(t, cal.Competitions, 1)
		for _, c := range cal.Competitions {
			assert.NotNil(t, c.ClashPartners)
			assert.Empty(t, c.ClashPartners)
		}
	})
}

func BenchmarkTournamentCalendar(b *testing.B) {
	dir := b.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(b, err)
	eng := New(store)

	tourn := &state.Tournament{
		Name:         "Benchmark Tournament",
		Date:         "01-07-2026",
		DurationDays: 3,
		Courts:       []string{"A", "B", "C", "D"},
		Pauses: []state.Pause{
			{Name: "Lunch", Start: "12:30", Length: "1h", Day: 0},
		},
	}
	require.NoError(b, store.SaveTournament(tourn))

	// Create 30 competitions with expensive formats (mixed and league with large rosters)
	for i := 0; i < 30; i++ {
		compID := fmt.Sprintf("bench-comp-%d", i)
		format := state.CompFormatMixed
		if i%2 == 1 {
			format = state.CompFormatLeague
		}
		comp := &state.Competition{
			ID:                           compID,
			Name:                         fmt.Sprintf("Competition %d", i),
			Date:                         "01-07-2026",
			StartTime:                    fmt.Sprintf("%02d:00", 9+(i%6)),
			Format:                       format,
			Courts:                       []string{"A", "B"},
			PoolSize:                     3,
			PoolWinners:                  1,
			PoolMatchDurationSeconds:     180,
			KnockoutMatchDurationSeconds: 240,
			Status:                       state.CompStatusSetup,
		}
		require.NoError(b, store.SaveCompetition(comp))
		players := make([]domain.Player, 128)
		for j := range players {
			players[j] = domain.Player{
				Name: fmt.Sprintf("Player %d-%d", i, j),
				Dojo: fmt.Sprintf("Dojo%c", rune('A'+j%5)),
			}
		}
		require.NoError(b, store.SaveParticipants(compID, players))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cal, err := eng.TournamentCalendar()
		if err != nil {
			b.Fatalf("TournamentCalendar failed: %v", err)
		}
		if len(cal.Competitions) != 30 {
			b.Fatalf("expected 30 competitions, got %d", len(cal.Competitions))
		}
	}
}
