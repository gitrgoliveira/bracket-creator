package engine

// schedule_golden_test.go replays the frozen record in testdata/schedule_golden.json
// through pauses. The record is frozen: recorded from the engine before pauses replaced
// the ceremony lengths.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type goldenCase struct {
	Name string

	// Tournament timing inputs
	Date                     string // "DD-MM-YYYY"
	DurationDays             int
	TournamentCourts         []string
	LegacyLunch              string
	LegacyOpening            string
	LegacyClosing            string
	OpeningAt                string
	ClockToElapsedMultiplier float64
	SlowestCourtBufferPct    int

	// Competition shape
	Format                       string // mixed, league, knockout
	StartTime                    string // "HH:MM"
	CompDate                     string // "DD-MM-YYYY"
	Courts                       []string
	PoolMatchDurationSeconds     int
	KnockoutMatchDurationSeconds int
	PlayerCount                  int
	TeamSize                     int
	Bronze                       bool
	CourtFloor                   map[string]string // "HH:MM" per court

	// Expected rework shifts
	SlotShift     map[string]int
	EstimateShift int
	EndShift      int
}

func (gc goldenCase) build() (*state.Competition, *state.Tournament) {
	tournDate := gc.Date
	if tournDate == "" {
		tournDate = "01-05-2026"
	}
	days := gc.DurationDays
	if days <= 0 {
		days = 1
	}
	tournCourts := gc.TournamentCourts
	if len(tournCourts) == 0 {
		tournCourts = gc.Courts
	}
	mult := gc.ClockToElapsedMultiplier
	if mult == 0 {
		mult = 1.5
	}
	buf := gc.SlowestCourtBufferPct
	if buf == 0 {
		buf = 10
	}

	compDate := gc.CompDate
	if compDate == "" {
		compDate = tournDate
	}
	startTime := gc.StartTime
	if startTime == "" {
		startTime = "09:00"
	}

	openingAt := gc.OpeningAt
	if openingAt == "" {
		openingAt = gc.StartTime
		if openingAt == "" {
			openingAt = "09:00"
		}
	}

	tourn := &state.Tournament{
		Date:                     tournDate,
		DurationDays:             days,
		Courts:                   tournCourts,
		ClockToElapsedMultiplier: mult,
		SlowestCourtBufferPct:    buf,
	}

	var pauses []state.Pause
	if gc.LegacyOpening != "" {
		dayIdx := 0
		if days > 1 {
			for i, d := range tourn.Days() {
				if d == compDate {
					dayIdx = i + 1
					break
				}
			}
		}
		pauses = append(pauses, state.Pause{
			Name:   "Opening ceremony",
			Start:  openingAt,
			Length: gc.LegacyOpening,
			Day:    dayIdx,
		})
	}
	if gc.LegacyLunch != "" {
		pauses = append(pauses, state.Pause{
			Name:   "Lunch",
			Start:  "12:00",
			Length: gc.LegacyLunch,
			Day:    0,
		})
	}
	if gc.LegacyClosing != "" {
		pauses = append(pauses, state.Pause{
			Name:   "Closing ceremony",
			Length: gc.LegacyClosing,
			Day:    0,
		})
	}
	tourn.Pauses = pauses

	poolSec := gc.PoolMatchDurationSeconds
	if poolSec == 0 {
		poolSec = 180
	}
	koSec := gc.KnockoutMatchDurationSeconds
	if koSec == 0 {
		koSec = 240
	}

	kind := "individual"
	if gc.TeamSize > 0 {
		kind = "team"
	}

	twoThird := !gc.Bronze
	comp := &state.Competition{
		ID:                           "comp-" + strings.ToLower(strings.ReplaceAll(gc.Name, " ", "-")),
		Name:                         gc.Name,
		Kind:                         kind,
		Format:                       gc.Format,
		PoolFormat:                   "full",
		TeamSize:                     gc.TeamSize,
		PoolSize:                     3,
		PoolSizeMode:                 "min",
		PoolWinners:                  2,
		RoundRobin:                   true,
		Courts:                       gc.Courts,
		StartTime:                    startTime,
		Date:                         compDate,
		PoolMatchDurationSeconds:     poolSec,
		KnockoutMatchDurationSeconds: koSec,
		TwoThirdPlaces:               &twoThird,
	}

	return comp, tourn
}

type goldenRoot struct {
	Cases []goldenCaseOutput `json:"cases"`
}

type goldenCaseOutput struct {
	Name     string           `json:"name"`
	Slots    []goldenSlot     `json:"slots"`
	End      string           `json:"end"`
	Estimate ScheduleEstimate `json:"estimate"`
}

type goldenSlot struct {
	ID    string `json:"id"`
	Court string `json:"court"`
	At    string `json:"at"`
}

var goldenCases = []goldenCase{
	// 1. Knockout: no blocks, 1 shiaijo
	{
		Name:         "knockout-no-blocks-1court",
		Date:         "01-05-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
		Format:       state.CompFormatKnockout,
		StartTime:    "09:00",
		PlayerCount:  8,
		Bronze:       false,
	},
	// 2. Knockout: lunch block only ("1h"), 1 shiaijo
	{
		Name:         "knockout-lunch-only-1court",
		Date:         "01-05-2026",
		DurationDays: 1,
		LegacyLunch:  "1h",
		Courts:       []string{"A"},
		Format:       state.CompFormatKnockout,
		StartTime:    "11:40",
		PlayerCount:  8,
		Bronze:       false,
	},
	// 3. Knockout: opening block only ("30m"), 2 shiaijo
	{
		Name:          "knockout-opening-only-2courts",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		Courts:        []string{"A", "B"},
		Format:        state.CompFormatKnockout,
		StartTime:     "09:00",
		PlayerCount:   8,
		Bronze:        false,
	},
	// 4. Knockout: closing block only ("30m"), 2 shiaijo, bronze on
	{
		Name:          "knockout-closing-only-2courts-bronze-on",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyClosing: "30m",
		Courts:        []string{"A", "B"},
		Format:        state.CompFormatKnockout,
		StartTime:     "09:00",
		PlayerCount:   8,
		Bronze:        true,
	},
	// 5. Knockout: all three blocks, bronze on, 2 shiaijo
	{
		Name:          "knockout-all-blocks-bronze-on-2courts",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		LegacyLunch:   "1h",
		LegacyClosing: "30m",
		Courts:        []string{"A", "B"},
		Format:        state.CompFormatKnockout,
		StartTime:     "11:00",
		PlayerCount:   8,
		Bronze:        true,
	},
	// 6. Knockout: all three blocks, bronze off, 3 shiaijo
	{
		Name:          "knockout-all-blocks-bronze-off-3courts",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		LegacyLunch:   "1h",
		LegacyClosing: "30m",
		Courts:        []string{"A", "B", "C"},
		Format:        state.CompFormatKnockout,
		StartTime:     "11:00",
		PlayerCount:   8,
		Bronze:        false,
	},
	// 7. League: no blocks, 1 shiaijo
	{
		Name:         "league-no-blocks-1court",
		Date:         "01-05-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
		Format:       state.CompFormatLeague,
		StartTime:    "09:00",
		PlayerCount:  5,
	},
	// 8. League: lunch block only ("1h"), 2 shiaijo
	{
		Name:         "league-lunch-only-2courts",
		Date:         "01-05-2026",
		DurationDays: 1,
		LegacyLunch:  "1h",
		Courts:       []string{"A", "B"},
		Format:       state.CompFormatLeague,
		StartTime:    "11:40",
		PlayerCount:  6,
	},
	// 9. League: opening block only ("30m"), 1 shiaijo
	{
		Name:          "league-opening-only-1court",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		Courts:        []string{"A"},
		Format:        state.CompFormatLeague,
		StartTime:     "09:00",
		PlayerCount:   5,
	},
	// 10. League: closing block only ("30m"), 2 shiaijo
	{
		Name:          "league-closing-only-2courts",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyClosing: "30m",
		Courts:        []string{"A", "B"},
		Format:        state.CompFormatLeague,
		StartTime:     "09:00",
		PlayerCount:   6,
	},
	// 11. League: all three blocks, 3 shiaijo
	{
		Name:          "league-all-blocks-3courts",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		LegacyLunch:   "1h",
		LegacyClosing: "30m",
		Courts:        []string{"A", "B", "C"},
		Format:        state.CompFormatLeague,
		StartTime:     "11:00",
		PlayerCount:   6,
	},
	// 12. Mixed: no blocks, 1 shiaijo, bronze off
	{
		Name:         "mixed-no-blocks-1court-bronze-off",
		Date:         "01-05-2026",
		DurationDays: 1,
		Courts:       []string{"A"},
		Format:       state.CompFormatMixed,
		StartTime:    "09:00",
		PlayerCount:  6,
		Bronze:       false,
	},
	// 13. Mixed: lunch block only ("1h"), 2 shiaijo, bronze on
	{
		Name:         "mixed-lunch-only-2courts-bronze-on",
		Date:         "01-05-2026",
		DurationDays: 1,
		LegacyLunch:  "1h",
		Courts:       []string{"A", "B"},
		Format:       state.CompFormatMixed,
		StartTime:    "11:40",
		PlayerCount:  6,
		Bronze:       true,
	},
	// 14. Mixed: opening block only ("30m"), 2 shiaijo, bronze off
	{
		Name:          "mixed-opening-only-2courts-bronze-off",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		Courts:        []string{"A", "B"},
		Format:        state.CompFormatMixed,
		StartTime:     "09:00",
		PlayerCount:   6,
		Bronze:        false,
	},
	// 15. Mixed: closing block only ("30m"), 3 shiaijo, bronze on
	{
		Name:          "mixed-closing-only-3courts-bronze-on",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyClosing: "30m",
		Courts:        []string{"A", "B", "C"},
		Format:        state.CompFormatMixed,
		StartTime:     "09:00",
		PlayerCount:   6,
		Bronze:        true,
	},
	// 16. Mixed: all three blocks, 2 shiaijo, bronze on
	{
		Name:          "mixed-all-blocks-2courts-bronze-on",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		LegacyLunch:   "1h",
		LegacyClosing: "30m",
		Courts:        []string{"A", "B"},
		Format:        state.CompFormatMixed,
		StartTime:     "11:00",
		PlayerCount:   6,
		Bronze:        true,
	},
	// 17. Mixed: all three blocks, 3 shiaijo, bronze off
	{
		Name:          "mixed-all-blocks-3courts-bronze-off",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		LegacyLunch:   "1h",
		LegacyClosing: "30m",
		Courts:        []string{"A", "B", "C"},
		Format:        state.CompFormatMixed,
		StartTime:     "11:00",
		PlayerCount:   9,
		Bronze:        false,
	},
	// 18. Two-day tournament: competition on Day 2, 2 shiaijo
	{
		Name:          "twoday-day2-knockout-2courts",
		Date:          "01-05-2026",
		DurationDays:  2,
		CompDate:      "02-05-2026",
		LegacyOpening: "30m",
		LegacyLunch:   "1h",
		Courts:        []string{"A", "B"},
		Format:        state.CompFormatKnockout,
		StartTime:     "11:00",
		PlayerCount:   8,
		Bronze:        true,
	},
	// 19. Two-day tournament: competition on Day 1, 1 shiaijo
	{
		Name:         "twoday-day1-league-1court",
		Date:         "01-05-2026",
		DurationDays: 2,
		CompDate:     "01-05-2026",
		LegacyLunch:  "1h",
		Courts:       []string{"A"},
		Format:       state.CompFormatLeague,
		StartTime:    "11:40",
		PlayerCount:  5,
	},
	// 20. Same-day second competition: 14:15 start on SEPARATE shiaijo (no floor)
	{
		Name:             "sameday-separate-shiaijo-afternoon",
		Date:             "01-05-2026",
		DurationDays:     1,
		TournamentCourts: []string{"A", "B"},
		Courts:           []string{"B"},
		Format:           state.CompFormatKnockout,
		StartTime:        "14:15",
		PlayerCount:      4,
		Bronze:           false,
	},
	// 21. Same-day second competition: SHARED shiaijo with floor from first's per-court ends
	{
		Name:         "sameday-shared-shiaijo-with-floor",
		Date:         "01-05-2026",
		DurationDays: 1,
		Courts:       []string{"A", "B"},
		Format:       state.CompFormatKnockout,
		StartTime:    "09:00",
		CourtFloor:   map[string]string{"A": "13:30", "B": "14:00"},
		PlayerCount:  4,
		Bronze:       false,
	},
	// 22. Start time that lands inside lunch (12:10) - Knockout
	{
		Name:         "start-inside-lunch-knockout",
		Date:         "01-05-2026",
		DurationDays: 1,
		LegacyLunch:  "1h", // default 12:00-13:00
		Courts:       []string{"A"},
		Format:       state.CompFormatKnockout,
		StartTime:    "12:10",
		PlayerCount:  4,
		Bronze:       false,
	},
	// 23. Start time that lands inside lunch (12:10) - Mixed
	{
		Name:         "start-inside-lunch-mixed",
		Date:         "01-05-2026",
		DurationDays: 1,
		LegacyLunch:  "1h", // default 12:00-13:00
		Courts:       []string{"A", "B"},
		Format:       state.CompFormatMixed,
		StartTime:    "12:10",
		PlayerCount:  6,
		Bronze:       false,
	},
	// 24. Team competition: all blocks, 2 shiaijo
	{
		Name:          "team-mixed-all-blocks-2courts",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		LegacyLunch:   "1h",
		LegacyClosing: "30m",
		Courts:        []string{"A", "B"},
		Format:        state.CompFormatMixed,
		StartTime:     "11:00",
		PlayerCount:   6,
		TeamSize:      5,
		Bronze:        true,
	},
	// 25. Opening 30m and lunch 1h starting at 11:40 (opening lands inside lunch)
	{
		Name:          "opening-lands-inside-lunch-knockout",
		Date:          "01-05-2026",
		DurationDays:  1,
		LegacyOpening: "30m",
		LegacyLunch:   "1h",
		Courts:        []string{"A"},
		Format:        state.CompFormatKnockout,
		StartTime:     "11:40",
		PlayerCount:   4,
		Bronze:        false,
	},
	// 26. League case with lunch 1h on 1 court starting at 11:30
	{
		Name:         "league-lunch-only-1court-1130",
		Date:         "01-05-2026",
		DurationDays: 1,
		LegacyLunch:  "1h",
		Courts:       []string{"A"},
		Format:       state.CompFormatLeague,
		StartTime:    "11:30",
		PlayerCount:  5,
	},
	// 27. Two-day tournament: Day 1 first competition at 09:30, mixed, courts A and B
	{
		Name:             "ceremony-day1-first-0930",
		Date:             "01-05-2026",
		DurationDays:     2,
		CompDate:         "01-05-2026",
		TournamentCourts: []string{"A", "B", "C", "D"},
		LegacyOpening:    "30m",
		LegacyClosing:    "30m",
		OpeningAt:        "09:30",
		Courts:           []string{"A", "B"},
		Format:           state.CompFormatMixed,
		StartTime:        "09:30",
		PlayerCount:      6,
		Bronze:           false,
	},
	// 28. Two-day tournament: Day 1 later competition at 14:15 on separate courts C and D
	{
		Name:             "ceremony-day1-later-1415-separate",
		Date:             "01-05-2026",
		DurationDays:     2,
		CompDate:         "01-05-2026",
		TournamentCourts: []string{"A", "B", "C", "D"},
		LegacyOpening:    "30m",
		LegacyClosing:    "30m",
		OpeningAt:        "09:30",
		Courts:           []string{"C", "D"},
		Format:           state.CompFormatKnockout,
		StartTime:        "14:15",
		PlayerCount:      4,
		Bronze:           false,
		SlotShift:        map[string]int{"C": 30, "D": 30},
		EstimateShift:    30,
		EndShift:         30,
	},
	// 29. Two-day tournament: Day 1 later competition at 14:15 on shared courts A and B with floor
	{
		Name:             "ceremony-day1-later-1415-shared",
		Date:             "01-05-2026",
		DurationDays:     2,
		CompDate:         "01-05-2026",
		TournamentCourts: []string{"A", "B", "C", "D"},
		LegacyOpening:    "30m",
		LegacyClosing:    "30m",
		OpeningAt:        "09:30",
		Courts:           []string{"A", "B"},
		Format:           state.CompFormatKnockout,
		StartTime:        "14:15",
		CourtFloor:       map[string]string{"A": "14:40", "B": "14:30"},
		PlayerCount:      4,
		Bronze:           false,
		SlotShift:        map[string]int{"A": 5, "B": 15},
		EstimateShift:    30,
		EndShift:         5,
	},
	// 30. Two-day tournament: Day 2 first competition at 09:30, league, courts A and B
	{
		Name:             "ceremony-day2-first-0930",
		Date:             "01-05-2026",
		DurationDays:     2,
		CompDate:         "02-05-2026",
		TournamentCourts: []string{"A", "B", "C", "D"},
		LegacyOpening:    "30m",
		LegacyClosing:    "30m",
		OpeningAt:        "09:30",
		Courts:           []string{"A", "B"},
		Format:           state.CompFormatLeague,
		StartTime:        "09:30",
		PlayerCount:      6,
	},
	// 31. Two-day tournament: Day 1 inside opening at 09:45, knockout, courts C and D
	{
		Name:             "ceremony-day1-inside-opening-0945",
		Date:             "01-05-2026",
		DurationDays:     2,
		CompDate:         "01-05-2026",
		TournamentCourts: []string{"A", "B", "C", "D"},
		LegacyOpening:    "30m",
		LegacyClosing:    "30m",
		OpeningAt:        "09:30",
		Courts:           []string{"C", "D"},
		Format:           state.CompFormatKnockout,
		StartTime:        "09:45",
		PlayerCount:      4,
		Bronze:           false,
		SlotShift:        map[string]int{"C": 15, "D": 15},
		EstimateShift:    15,
		EndShift:         15,
	},
}

func buildPoolMatches(playerCount, poolSize int, courts []string) ([]state.MatchResult, int) {
	if poolSize <= 0 {
		poolSize = 3
	}
	numPools := playerCount / poolSize
	if numPools < 2 {
		numPools = 2
	}
	base := playerCount / numPools
	rem := playerCount % numPools
	sizes := make([]int, numPools)
	for i := range numPools {
		sizes[i] = base
		if i < rem {
			sizes[i]++
		}
	}

	var matches []state.MatchResult
	pIdx := 0
	for pi, sz := range sizes {
		court := courts[pi%len(courts)]
		poolName := fmt.Sprintf("Pool %c", 'A'+pi)
		mIdx := 0
		for i := 0; i < sz; i++ {
			for j := i + 1; j < sz; j++ {
				matches = append(matches, state.MatchResult{
					ID:      fmt.Sprintf("%s-%d", poolName, mIdx),
					SideA:   fmt.Sprintf("P%d", pIdx+i),
					SideB:   fmt.Sprintf("P%d", pIdx+j),
					SideAID: fmt.Sprintf("id-P%d", pIdx+i),
					SideBID: fmt.Sprintf("id-P%d", pIdx+j),
					Court:   court,
					Status:  state.MatchStatusScheduled,
				})
				mIdx++
			}
		}
		pIdx += sz
	}
	return matches, numPools
}

func buildKnockoutBracket(qualifierCount int, courts []string, bronze bool) *state.Bracket {
	pow2 := helper.NextPow2(qualifierCount)
	if pow2 < 2 {
		pow2 = 2
	}
	depth := 0
	for (1 << depth) < pow2 {
		depth++
	}

	rounds := make([][]state.BracketMatch, depth)
	matchNum := 1
	for r := 0; r < depth; r++ {
		numMatches := pow2 >> (r + 1)
		round := make([]state.BracketMatch, numMatches)
		for m := 0; m < numMatches; m++ {
			court := courts[(r+m)%len(courts)]
			if r == depth-1 {
				court = courts[0] // final on first court
			}
			round[m] = state.BracketMatch{
				ID:          fmt.Sprintf("m-r%d-%d", r+1, m),
				MatchNumber: matchNum,
				Court:       court,
				Status:      state.MatchStatusScheduled,
			}
			matchNum++
		}
		rounds[r] = round
	}

	bracket := &state.Bracket{
		Rounds: rounds,
	}

	if bronze && len(rounds) >= 2 {
		finalCourt := rounds[len(rounds)-1][0].Court
		bracket.ThirdPlaceMatch = &state.BracketMatch{
			ID:     state.BronzeMatchID,
			Court:  finalCourt,
			Status: state.MatchStatusScheduled,
		}
	}

	return bracket
}

func TestScheduleGolden(t *testing.T) {
	root := goldenRoot{
		Cases: make([]goldenCaseOutput, 0, len(goldenCases)),
	}

	for _, gc := range goldenCases {
		comp, tourn := gc.build()
		floorTimeMap := make(map[string]time.Time)
		for c, hhmm := range gc.CourtFloor {
			floorTimeMap[c] = parseClockHHMM(hhmm)
		}

		var slots []goldenSlot
		var maxEnd time.Time
		var poolCount, koCount int

		switch comp.Format {
		case state.CompFormatLeague:
			matches := buildLeagueMatches(gc.PlayerCount)
			ordered, slotIdxs := scheduleLeagueSlots(matches, comp.Courts)
			ordered, endCursor := assignLeagueSlotTimesFrom(ordered, slotIdxs, comp, tourn, floorTimeMap)
			maxEnd = endCursor
			poolCount = len(ordered)
			koCount = 0
			for _, m := range ordered {
				slots = append(slots, goldenSlot{
					ID:    m.ID,
					Court: m.Court,
					At:    m.ScheduledAt,
				})
			}

		case state.CompFormatKnockout:
			bracket := buildKnockoutBracket(gc.PlayerCount, comp.Courts, gc.Bronze)
			koEnd := assignBracketMatchSlots(bracket.Rounds, comp, tourn, floorTimeMap)
			if gc.Bronze && bracket.ThirdPlaceMatch != nil {
				scheduleBronze(bracket, comp, tourn)
			}
			maxEnd = koEnd
			if bracket.ThirdPlaceMatch != nil {
				lastRound := bracket.Rounds[len(bracket.Rounds)-1]
				if len(lastRound) > 0 {
					perMatch := perMatchElapsedMinutes(comp, tourn, true)
					finalEnd := parseClockHHMM(lastRound[0].ScheduledAt).Add(time.Duration(perMatch) * time.Minute)
					if finalEnd.After(maxEnd) {
						maxEnd = finalEnd
					}
				}
			}

			for _, round := range bracket.Rounds {
				for _, m := range round {
					if m.Status != state.MatchStatusCompleted || m.Winner == "" {
						koCount++
					}
					slots = append(slots, goldenSlot{
						ID:    m.ID,
						Court: m.Court,
						At:    m.ScheduledAt,
					})
				}
			}
			if bracket.ThirdPlaceMatch != nil {
				koCount++
				slots = append(slots, goldenSlot{
					ID:    bracket.ThirdPlaceMatch.ID,
					Court: bracket.ThirdPlaceMatch.Court,
					At:    bracket.ThirdPlaceMatch.ScheduledAt,
				})
			}

		case state.CompFormatMixed:
			poolMatches, numPools := buildPoolMatches(gc.PlayerCount, comp.PoolSize, comp.Courts)
			poolMatches, poolEnd := assignPoolMatchSlotsFrom(poolMatches, comp, tourn, floorTimeMap)
			for _, m := range poolMatches {
				slots = append(slots, goldenSlot{
					ID:    m.ID,
					Court: m.Court,
					At:    m.ScheduledAt,
				})
			}
			poolCount = len(poolMatches)

			qualifiers := numPools * comp.EffectivePoolWinners()
			if qualifiers < 2 {
				qualifiers = 2
			}
			bracket := buildKnockoutBracket(qualifiers, comp.Courts, gc.Bronze)
			koCourtStart := knockoutCourtStart(poolMatches, comp, tourn, floorTimeMap)
			koEnd := assignBracketMatchSlots(bracket.Rounds, comp, tourn, koCourtStart)
			if gc.Bronze && bracket.ThirdPlaceMatch != nil {
				scheduleBronze(bracket, comp, tourn)
			}
			pushKnockoutPastPools(bracket, comp, tourn, poolMatches)

			maxEnd = poolEnd
			if koEnd.After(maxEnd) {
				maxEnd = koEnd
			}
			if bracket.ThirdPlaceMatch != nil {
				lastRound := bracket.Rounds[len(bracket.Rounds)-1]
				if len(lastRound) > 0 {
					perMatch := perMatchElapsedMinutes(comp, tourn, true)
					finalEnd := parseClockHHMM(lastRound[0].ScheduledAt).Add(time.Duration(perMatch) * time.Minute)
					if finalEnd.After(maxEnd) {
						maxEnd = finalEnd
					}
				}
			}

			for _, round := range bracket.Rounds {
				for _, m := range round {
					if m.Status != state.MatchStatusCompleted || m.Winner == "" {
						koCount++
					}
					slots = append(slots, goldenSlot{
						ID:    m.ID,
						Court: m.Court,
						At:    m.ScheduledAt,
					})
				}
			}
			if bracket.ThirdPlaceMatch != nil {
				koCount++
				slots = append(slots, goldenSlot{
					ID:    bracket.ThirdPlaceMatch.ID,
					Court: bracket.ThirdPlaceMatch.Court,
					At:    bracket.ThirdPlaceMatch.ScheduledAt,
				})
			}
		}

		// Sort slots deterministically: court, then time, then id
		sort.Slice(slots, func(i, j int) bool {
			if slots[i].Court != slots[j].Court {
				return slots[i].Court < slots[j].Court
			}
			if slots[i].At != slots[j].At {
				return slots[i].At < slots[j].At
			}
			return slots[i].ID < slots[j].ID
		})

		est := EstimateForCounts(poolCount, koCount, comp, tourn)

		root.Cases = append(root.Cases, goldenCaseOutput{
			Name:     gc.Name,
			Slots:    slots,
			End:      maxEnd.Format(scheduleClockLayout),
			Estimate: est,
		})
	}

	goldenPath := filepath.Join("testdata", "schedule_golden.json")

	content, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		t.Fatalf("golden file %s does not exist", goldenPath)
	}
	require.NoError(t, err)

	var frozen goldenRoot
	require.NoError(t, json.Unmarshal(content, &frozen))

	frozenMap := make(map[string]goldenCaseOutput, len(frozen.Cases))
	for _, c := range frozen.Cases {
		frozenMap[c.Name] = c
	}

	caseMap := make(map[string]goldenCase, len(goldenCases))
	for _, gc := range goldenCases {
		caseMap[gc.Name] = gc
	}

	for _, actualCase := range root.Cases {
		t.Run(actualCase.Name, func(t *testing.T) {
			recCase, ok := frozenMap[actualCase.Name]
			if !ok {
				t.Fatalf("missing golden case %q in frozen record", actualCase.Name)
			}
			gc, ok := caseMap[actualCase.Name]
			if !ok {
				t.Fatalf("missing golden case configuration for %q", actualCase.Name)
			}

			// Expected changes of the pause rework:
			// (a) estimate.ceremonyMinutes becomes 0
			// (b) when LegacyClosing is set, totalDurationMinutes, bestCaseMinutes and worstCaseMinutes
			//     are each that many minutes LESS (perCourtMinutes unchanged)
			// (c) a case whose StartTime is later than OpeningAt carries SlotShift and EstimateShift:
			//     minutes earlier on every perCourtMinutes entry and, on top of (b), on total, best and worst.
			expectedEstimate := recCase.Estimate
			expectedEstimate.CeremonyMinutes = 0

			closingMin := 0
			if gc.LegacyClosing != "" {
				closingMin = ParseDurationMinutes(gc.LegacyClosing)
			}
			expectedEstimate.TotalDurationMinutes -= closingMin
			expectedEstimate.BestCaseMinutes -= closingMin
			expectedEstimate.WorstCaseMinutes -= closingMin

			if gc.EstimateShift > 0 {
				expectedEstimate.TotalDurationMinutes -= gc.EstimateShift
				expectedEstimate.BestCaseMinutes -= gc.EstimateShift
				expectedEstimate.WorstCaseMinutes -= gc.EstimateShift
				perCourt := make([]int, len(expectedEstimate.PerCourtMinutes))
				for i, m := range expectedEstimate.PerCourtMinutes {
					perCourt[i] = m - gc.EstimateShift
				}
				expectedEstimate.PerCourtMinutes = perCourt
			}

			// Expected slots: every slot on a shifted court is that many minutes earlier
			expectedSlots := make([]goldenSlot, len(recCase.Slots))
			for i, s := range recCase.Slots {
				expectedSlots[i] = s
				if shift, ok := gc.SlotShift[s.Court]; ok && shift > 0 {
					tAt := parseClockHHMM(s.At).Add(-time.Duration(shift) * time.Minute)
					expectedSlots[i].At = tAt.Format(scheduleClockLayout)
				}
			}

			// Expected end: recorded end minus EndShift
			expectedEnd := recCase.End
			if gc.EndShift > 0 {
				expectedEnd = parseClockHHMM(recCase.End).Add(-time.Duration(gc.EndShift) * time.Minute).Format(scheduleClockLayout)
			}

			if !assert.Equal(t, expectedEnd, actualCase.End, "case %s: end mismatch", actualCase.Name) {
				t.Errorf("case %s: field End: recorded=%s expected=%s actual=%s",
					actualCase.Name, recCase.End, expectedEnd, actualCase.End)
			}
			if len(gc.SlotShift) > 0 {
				assert.False(t, parseClockHHMM(actualCase.End).After(parseClockHHMM(recCase.End)),
					"case %s: shifted end %s must not be later than recorded %s",
					actualCase.Name, actualCase.End, recCase.End)
			}
			if !assert.Equal(t, expectedSlots, actualCase.Slots, "case %s: slots mismatch", actualCase.Name) {
				t.Errorf("case %s: field Slots: recorded=%+v expected=%+v actual=%+v",
					actualCase.Name, recCase.Slots, expectedSlots, actualCase.Slots)
			}
			if !assert.Equal(t, expectedEstimate, actualCase.Estimate, "case %s: estimate mismatch", actualCase.Name) {
				t.Errorf("case %s: field Estimate: recorded=%+v expected=%+v actual=%+v",
					actualCase.Name, recCase.Estimate, expectedEstimate, actualCase.Estimate)
			}
		})
	}
}
