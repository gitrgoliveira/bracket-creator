package engine

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type calendarRuleCasesFile struct {
	About        string                 `json:"about"`
	DayBreaks    []dayBreaksTestCase    `json:"dayBreaks"`
	ReplayCourt  []replayCourtTestCase  `json:"replayCourt"`
	PastMidnight []pastMidnightTestCase `json:"pastMidnight"`
	Overlaps     []overlapsTestCase     `json:"overlaps"`
}

type dayBreaksTestCase struct {
	Name     string        `json:"name"`
	Days     []string      `json:"days"`
	Date     string        `json:"date"`
	Pauses   []state.Pause `json:"pauses"`
	Expected []struct {
		Name  string `json:"name"`
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"expected"`
}

type replayCourtTestCase struct {
	Name   string      `json:"name"`
	Start  string      `json:"start"`
	Runs   []ReplayRun `json:"runs"`
	Breaks []struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"breaks"`
	BufferPct               int    `json:"bufferPct"`
	ExpectedDurationMinutes int    `json:"expectedDurationMinutes"`
	ExpectedEnd             string `json:"expectedEnd"`
}

type pastMidnightTestCase struct {
	Name     string `json:"name"`
	EndMins  int    `json:"endMins"`
	Expected bool   `json:"expected"`
}

type overlapsTestCase struct {
	Name     string            `json:"name"`
	A        state.CourtWindow `json:"a"`
	B        state.CourtWindow `json:"b"`
	Expected bool              `json:"expected"`
}

func loadCalendarRuleCases(t *testing.T) calendarRuleCasesFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "calendar_rule_cases.json"))
	require.NoError(t, err)
	var cf calendarRuleCasesFile
	require.NoError(t, json.Unmarshal(data, &cf))
	return cf
}

func TestCalendarRules_DayBreaks(t *testing.T) {
	cf := loadCalendarRuleCases(t)
	refMidnight := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range cf.DayBreaks {
		t.Run(tc.Name, func(t *testing.T) {
			firstDate := ""
			if len(tc.Days) > 0 {
				firstDate = tc.Days[0]
			}
			tourn := state.Tournament{
				Date:         firstDate,
				DurationDays: len(tc.Days),
				Pauses:       tc.Pauses,
			}
			windows := dayBreaks(&tourn, tc.Date)
			require.Len(t, windows, len(tc.Expected))
			for i, exp := range tc.Expected {
				startMins := int(windows[i].start.Sub(refMidnight).Minutes())
				endMins := int(windows[i].end.Sub(refMidnight).Minutes())
				assert.Equal(t, exp.Start, state.FormatClockHHMM(startMins))
				assert.Equal(t, exp.End, state.FormatClockHHMM(endMins))
			}
		})
	}
}

func TestCalendarRules_ReplayCourt(t *testing.T) {
	cf := loadCalendarRuleCases(t)
	baseDate := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range cf.ReplayCourt {
		t.Run(tc.Name, func(t *testing.T) {
			startMins, ok := state.ParseClockMinutes(tc.Start)
			require.True(t, ok)

			startTime := baseDate.Add(time.Duration(startMins) * time.Minute)

			var breaks []breakWindow
			for _, b := range tc.Breaks {
				bStartMins, ok1 := state.ParseClockMinutes(b.Start)
				bEndMins, ok2 := state.ParseClockMinutes(b.End)
				require.True(t, ok1)
				require.True(t, ok2)
				breaks = append(breaks, breakWindow{
					start: baseDate.Add(time.Duration(bStartMins) * time.Minute),
					end:   baseDate.Add(time.Duration(bEndMins) * time.Minute),
				})
			}

			end, duration := replayCourt(startTime, tc.Runs, breaks, tc.BufferPct)
			durInt := int(math.Round(duration))
			assert.Equal(t, tc.ExpectedDurationMinutes, durInt)

			endMins := int(math.Round(end.Sub(baseDate).Minutes()))
			actualEnd := state.FormatClockHHMM(endMins)
			assert.Equal(t, tc.ExpectedEnd, actualEnd)
		})
	}
}

func TestCalendarRules_PastMidnight(t *testing.T) {
	cf := loadCalendarRuleCases(t)
	for _, tc := range cf.PastMidnight {
		t.Run(tc.Name, func(t *testing.T) {
			actual := pastMidnight(tc.EndMins)
			assert.Equal(t, tc.Expected, actual)
		})
	}
}

func TestCalendarRules_Overlaps(t *testing.T) {
	cf := loadCalendarRuleCases(t)
	for _, tc := range cf.Overlaps {
		t.Run(tc.Name, func(t *testing.T) {
			actual := overlaps(tc.A, tc.B)
			assert.Equal(t, tc.Expected, actual)
		})
	}
}
