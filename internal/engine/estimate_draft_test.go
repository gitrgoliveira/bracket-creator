package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEstimateDraft_ParityWithStoredPath checks that EstimateDraft matches
// EstimateScheduleForCompetition across formats, kinds, team match types,
// participant counts, and pause configurations.
func TestEstimateDraft_ParityWithStoredPath(t *testing.T) {
	formats := []string{
		state.CompFormatKnockout,
		state.CompFormatMixed,
		state.CompFormatLeague,
		state.CompFormatSwiss,
	}

	type kindCase struct {
		label         string
		kind          string
		teamSize      int
		teamMatchType state.TeamMatchType
	}

	kindCases := []kindCase{
		{
			label:         "individual",
			kind:          "individual",
			teamSize:      0,
			teamMatchType: "",
		},
		{
			label:         "team-fixed",
			kind:          "team",
			teamSize:      3,
			teamMatchType: state.TeamMatchTypeFixed,
		},
		{
			label:         "team-kachinuki",
			kind:          "team",
			teamSize:      3,
			teamMatchType: state.TeamMatchTypeKachinuki,
		},
	}

	counts := []int{8, 13}

	type tournCase struct {
		label  string
		pauses []state.Pause
	}

	tournCases := []tournCase{
		{
			label:  "no-pauses",
			pauses: nil,
		},
		{
			label: "two-pauses",
			pauses: []state.Pause{
				{Name: "Lunch", Start: "12:00", Length: "45m", Day: 0},
				{Name: "Briefing", Start: "08:30", Length: "30m", Day: 1},
			},
		},
	}

	for _, tCase := range tournCases {
		eng, store, _ := setupTestEngine(t)
		tourn := &state.Tournament{
			Name:         "Tourn-" + tCase.label,
			Date:         "01-05-2026",
			DurationDays: 2,
			Courts:       []string{"A"},
			Pauses:       tCase.pauses,
		}
		require.NoError(t, store.SaveTournament(tourn))

		compIdx := 0
		for _, format := range formats {
			for _, kc := range kindCases {
				for _, n := range counts {
					tcName := fmt.Sprintf("%s/%s/%s/n=%d", tCase.label, format, kc.label, n)
					t.Run(tcName, func(t *testing.T) {
						compIdx++
						compID := fmt.Sprintf("c-%s-%d", tCase.label, compIdx)
						comp := &state.Competition{
							ID:                           compID,
							Format:                       format,
							Kind:                         kc.kind,
							TeamSize:                     kc.teamSize,
							TeamMatchType:                kc.teamMatchType,
							PoolSize:                     3,
							PoolSizeMode:                 "min",
							PoolWinners:                  2,
							RoundRobin:                   true,
							SwissRounds:                  3,
							Courts:                       []string{"A"},
							StartTime:                    "09:00",
							Date:                         "01-05-2026",
							Status:                       state.CompStatusSetup,
							ExpectedParticipants:         0,
							CheckInEnabled:               false,
							PoolMatchDurationSeconds:     180,
							KnockoutMatchDurationSeconds: 300,
						}
						require.NoError(t, store.SaveCompetition(comp))

						names := make([]string, n)
						for i := 0; i < n; i++ {
							names[i] = fmt.Sprintf("Entry-%d-%d", compIdx, i+1)
						}
						saveTestParticipants(t, store, compID, names)

						storedEst, storedErr := eng.EstimateScheduleForCompetition(compID)
						compCopy := *comp
						draftEst, draftErr := eng.EstimateDraft(&compCopy, n)

						if storedErr != nil {
							var ve *ValidationError
							if errors.As(storedErr, &ve) {
								assert.ErrorAs(t, draftErr, &ve, "draft must return validation error for %s", tcName)
								return
							}
							require.NoError(t, storedErr)
						}
						require.NoError(t, draftErr)

						storedEst.ParticipantsSource = ""
						assert.Equal(t, n, storedEst.Participants)
						assert.Equal(t, n, draftEst.Participants)
						assert.Equal(t, storedEst, draftEst.ScheduleEstimate)
					})
				}
			}
		}
	}
}

// TestEstimateDraft_PoolAndKnockoutMatches verifies PoolMatches and KnockoutMatches
// agree with helper.EstimateMatchCounts for knockout and mixed formats.
func TestEstimateDraft_PoolAndKnockoutMatches(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	// Knockout 8 (0 and 7)
	compKO := &state.Competition{
		Format:    state.CompFormatKnockout,
		Kind:      "individual",
		Courts:    []string{"A"},
		StartTime: "09:00",
	}
	draftKO, err := eng.EstimateDraft(compKO, 8)
	require.NoError(t, err)

	expectedPoolKO, expectedKnockoutKO, err := helper.EstimateMatchCounts(helper.EstimateMatchCountsInput{
		Format:      state.CompFormatKnockout,
		PlayerCount: 8,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, draftKO.PoolMatches)
	assert.Equal(t, 7, draftKO.KnockoutMatches)
	assert.Equal(t, expectedPoolKO, draftKO.PoolMatches)
	assert.Equal(t, expectedKnockoutKO, draftKO.KnockoutMatches)

	// Mixed case: 9 players, poolSize 3 min-mode, RR, 2 winners
	compMixed := &state.Competition{
		Format:       state.CompFormatMixed,
		Kind:         "individual",
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
	}
	draftMixed, err := eng.EstimateDraft(compMixed, 9)
	require.NoError(t, err)

	expectedPoolMixed, expectedKnockoutMixed, err := helper.EstimateMatchCounts(helper.EstimateMatchCountsInput{
		Format:       state.CompFormatMixed,
		PlayerCount:  9,
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, expectedPoolMixed, draftMixed.PoolMatches)
	assert.Equal(t, expectedKnockoutMixed, draftMixed.KnockoutMatches)
}

// TestEstimateDraft_Timing verifies multiplier and allowance percentage reporting.
func TestEstimateDraft_Timing(t *testing.T) {
	comp := &state.Competition{
		Format:    state.CompFormatKnockout,
		Kind:      "individual",
		Courts:    []string{"A"},
		StartTime: "09:00",
	}

	// Tournament with ClockToElapsedMultiplier 2 and SlowestCourtBufferPct 20
	eng1, store1, _ := setupTestEngine(t)
	require.NoError(t, store1.SaveTournament(&state.Tournament{
		Name:                     "T-CustomTiming",
		Date:                     "01-05-2026",
		DurationDays:             1,
		Courts:                   []string{"A"},
		ClockToElapsedMultiplier: 2.0,
		SlowestCourtBufferPct:    20,
	}))
	draft1, err := eng1.EstimateDraft(comp, 8)
	require.NoError(t, err)
	assert.Equal(t, 2.0, draft1.Timing.Multiplier)
	assert.Equal(t, 20, draft1.Timing.AllowancePct)

	// Tournament with neither set (nil tournament in store)
	eng2, _, _ := setupTestEngine(t)
	draft2, err := eng2.EstimateDraft(comp, 8)
	require.NoError(t, err)
	assert.Equal(t, 1.5, draft2.Timing.Multiplier)
	assert.Equal(t, 10, draft2.Timing.AllowancePct)

	// Tournament saved with zero values
	eng3, store3, _ := setupTestEngine(t)
	require.NoError(t, store3.SaveTournament(&state.Tournament{
		Name:         "T-DefaultTiming",
		Date:         "01-05-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}))
	draft3, err := eng3.EstimateDraft(comp, 8)
	require.NoError(t, err)
	assert.Equal(t, 1.5, draft3.Timing.Multiplier)
	assert.Equal(t, 10, draft3.Timing.AllowancePct)
}

// TestEstimateDraft_Breaks verifies break filtering and sorting across days.
func TestEstimateDraft_Breaks(t *testing.T) {
	tourn := &state.Tournament{
		Name:         "Tourn2Days",
		Date:         "01-05-2026",
		DurationDays: 2,
		Courts:       []string{"A"},
		Pauses: []state.Pause{
			{Name: "Lunch", Start: "12:00", Length: "45m", Day: 0},
			{Name: "Opening Ceremony", Start: "09:00", Length: "30m", Day: 1},
			{Name: "Grading", Start: "10:00", Length: "1h", Day: 2},
			{Name: "Malformed Start", Start: "noon", Length: "30m", Day: 1},
			{Name: "Zero Length", Start: "14:00", Length: "0m", Day: 1},
			{Name: "Empty Length", Start: "15:00", Length: "", Day: 2},
		},
	}

	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(tourn))

	// Comp on Day 1: Date == "01-05-2026"
	compDay1 := &state.Competition{
		Format:    state.CompFormatKnockout,
		Kind:      "individual",
		Courts:    []string{"A"},
		StartTime: "09:00",
		Date:      "01-05-2026",
	}
	draft1, err := eng.EstimateDraft(compDay1, 8)
	require.NoError(t, err)
	require.Len(t, draft1.Timing.Breaks, 2)
	assert.Equal(t, "Opening Ceremony", draft1.Timing.Breaks[0].Name)
	assert.Equal(t, "09:00", draft1.Timing.Breaks[0].Start)
	assert.Equal(t, "09:30", draft1.Timing.Breaks[0].End)
	assert.Equal(t, "Lunch", draft1.Timing.Breaks[1].Name)
	assert.Equal(t, "12:00", draft1.Timing.Breaks[1].Start)
	assert.Equal(t, "12:45", draft1.Timing.Breaks[1].End)

	// Comp on Day 2: Date == "02-05-2026"
	compDay2 := &state.Competition{
		Format:    state.CompFormatKnockout,
		Kind:      "individual",
		Courts:    []string{"A"},
		StartTime: "09:00",
		Date:      "02-05-2026",
	}
	draft2, err := eng.EstimateDraft(compDay2, 8)
	require.NoError(t, err)
	require.Len(t, draft2.Timing.Breaks, 2)
	assert.Equal(t, "Grading", draft2.Timing.Breaks[0].Name)
	assert.Equal(t, "10:00", draft2.Timing.Breaks[0].Start)
	assert.Equal(t, "11:00", draft2.Timing.Breaks[0].End)
	assert.Equal(t, "Lunch", draft2.Timing.Breaks[1].Name)
	assert.Equal(t, "12:00", draft2.Timing.Breaks[1].Start)
	assert.Equal(t, "12:45", draft2.Timing.Breaks[1].End)

	// With no pauses
	engNoPauses, storeNoPauses, _ := setupTestEngine(t)
	require.NoError(t, storeNoPauses.SaveTournament(&state.Tournament{
		Name:         "TournNoPauses",
		Date:         "01-05-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
	}))
	draftNoPauses, err := engNoPauses.EstimateDraft(compDay1, 8)
	require.NoError(t, err)
	assert.NotNil(t, draftNoPauses.Timing.Breaks)
	assert.Empty(t, draftNoPauses.Timing.Breaks)

	raw, err := json.Marshal(draftNoPauses)
	require.NoError(t, err)
	jsonStr := string(raw)
	assert.Contains(t, jsonStr, `"breaks":[]`)
	assert.Contains(t, jsonStr, `"totalDurationMinutes"`)
	assert.Contains(t, jsonStr, `"poolMatches"`)
	assert.Contains(t, jsonStr, `"knockoutMatches"`)
	assert.Contains(t, jsonStr, `"timing"`)
}

// TestEstimateDraft_Bounds verifies validation on entry counts and nil competition.
func TestEstimateDraft_Bounds(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	comp := &state.Competition{
		Format:    state.CompFormatKnockout,
		Kind:      "individual",
		Courts:    []string{"A"},
		StartTime: "09:00",
	}

	var ve *ValidationError

	// participants 0
	_, err := eng.EstimateDraft(comp, 0)
	require.Error(t, err)
	assert.ErrorAs(t, err, &ve, "participants 0 must return ValidationError")

	// participants -1
	_, err = eng.EstimateDraft(comp, -1)
	require.Error(t, err)
	assert.ErrorAs(t, err, &ve, "participants -1 must return ValidationError")

	// participants MaxEstimateParticipants + 1
	_, err = eng.EstimateDraft(comp, MaxEstimateParticipants+1)
	require.Error(t, err)
	assert.ErrorAs(t, err, &ve, "participants > MaxEstimateParticipants must return ValidationError")
	assert.Contains(t, err.Error(), "1024")

	// MaxEstimateParticipants succeeds
	draftMax, err := eng.EstimateDraft(comp, MaxEstimateParticipants)
	require.NoError(t, err)
	assert.Equal(t, MaxEstimateParticipants, draftMax.Participants)

	// comp nil is an error
	_, err = eng.EstimateDraft(nil, 8)
	require.Error(t, err)
	assert.ErrorAs(t, err, &ve, "nil comp must return ValidationError")
}

// TestEstimateDraft_DoesNotTouchTheStore verifies that EstimateDraft is purely in-memory
// and does not persist the competition.
func TestEstimateDraft_DoesNotTouchTheStore(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	comp := &state.Competition{
		ID:        "draft-unstored-comp",
		Format:    state.CompFormatKnockout,
		Kind:      "individual",
		Courts:    []string{"A"},
		StartTime: "09:00",
	}

	_, err := eng.EstimateDraft(comp, 8)
	require.NoError(t, err)

	loaded, err := store.LoadCompetition("draft-unstored-comp")
	require.NoError(t, err)
	assert.Nil(t, loaded, "competition must not be persisted by EstimateDraft")
}
