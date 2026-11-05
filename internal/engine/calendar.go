package engine

import (
	"log"
	"sort"
	"strings"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ReplayRecipe provides the court replay data needed to simulate match scheduling.
// Before the draw, Runs, BestRuns, WorstRuns, BufferPct and FloorMinutes describe the rounds.
// After the draw, SlotLengths contains each match slot's duration in minutes in chronological order.
type ReplayRecipe struct {
	Runs         []ReplayRun `json:"runs,omitempty"`
	BestRuns     []ReplayRun `json:"bestRuns,omitempty"`
	WorstRuns    []ReplayRun `json:"worstRuns,omitempty"`
	BufferPct    int         `json:"bufferPct,omitempty"`
	FloorMinutes int         `json:"floorMinutes,omitempty"`
	SlotLengths  []int       `json:"slotLengths,omitempty"`
}

// CompetitionPlacement describes how a competition is placed on the tournament schedule.
type CompetitionPlacement struct {
	Placed          bool                         `json:"placed"`
	NotPlacedReason string                       `json:"notPlacedReason,omitempty"`
	Day             int                          `json:"day,omitempty"` // 1-based day index in Tournament.Days(), or 0 if tournament has no date
	Courts          []string                     `json:"courts,omitempty"`
	Windows         map[string]state.CourtWindow `json:"windows,omitempty"`
	LengthSource    string                       `json:"lengthSource,omitempty"` // "estimate", "expected", "draw", "minimum"
	LengthReason    string                       `json:"lengthReason,omitempty"` // e.g. "no_participants", "estimate_failed"
	BestEnd         string                       `json:"bestEnd,omitempty"`      // kachinuki best end
	WorstEnd        string                       `json:"worstEnd,omitempty"`     // kachinuki worst end
	ReplayRecipes   map[string]ReplayRecipe      `json:"replayRecipes,omitempty"`
	PastMidnight    bool                         `json:"pastMidnight,omitempty"`
	Minutes         int                          `json:"minutes,omitempty"`
}

// TakesPartInClashes reports whether the placement is compared for schedule clashes: a placed competition, or one whose date falls outside the tournament (it still holds its courts on that date).
func (p CompetitionPlacement) TakesPartInClashes() bool {
	return p.Placed || p.NotPlacedReason == "date_outside_tournament"
}

// CalendarBreak describes a scheduled pause window during a tournament day.
type CalendarBreak struct {
	Name      string `json:"name"`
	Start     string `json:"start"`
	End       string `json:"end"`
	StartMins int    `json:"startMins"`
	EndMins   int    `json:"endMins"`
}

// CalendarDay describes a tournament day and its scheduled pauses.
type CalendarDay struct {
	Day    int             `json:"day"`
	Date   string          `json:"date"`
	Breaks []CalendarBreak `json:"breaks"`
}

// UnusedPause describes a configured pause that is dropped from the schedule.
type UnusedPause struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// CalendarCompetition describes a single competition on the tournament calendar.
type CalendarCompetition struct {
	ID            string                  `json:"id"`
	Name          string                  `json:"name"`
	Kind          string                  `json:"kind"`
	Status        state.CompetitionStatus `json:"status"`
	ConfigVersion string                  `json:"configVersion,omitempty"`
	Placement     CompetitionPlacement    `json:"placement"`
	ClashPartners []string                `json:"clashPartners"`
}

// TournamentCalendarResult represents the complete tournament calendar response.
type TournamentCalendarResult struct {
	Days         []CalendarDay         `json:"days"`
	Courts       []string              `json:"courts"`
	UnusedPauses []UnusedPause         `json:"unusedPauses"`
	Competitions []CalendarCompetition `json:"competitions"`
}

// pastMidnight reports whether endMins is after midnight (greater than 24*60).
// An end at exactly 24:00 finishes at midnight, not after it (CourtFloor's >=
// is about a floor, a START at midnight, which is a different question).
func pastMidnight(endMins int) bool {
	return endMins > 24*60
}

// Overlaps reports whether two half-open windows [Start, End) overlap.
// Touching windows (e.g. a.End == b.Start) do not overlap.
func Overlaps(a, b state.CourtWindow) bool {
	if a.Start >= a.End || b.Start >= b.End {
		return false
	}
	return a.Start < b.End && b.Start < a.End
}

func minimumPlacement(dayIdx int, courts []string, startMins int, reason string, bufferPct int) CompetitionPlacement {
	footprint := MinClashFootprintMinutes
	endMins := startMins + footprint
	windows := make(map[string]state.CourtWindow, len(courts))
	recipes := make(map[string]ReplayRecipe, len(courts))
	for _, c := range courts {
		windows[c] = state.CourtWindow{Start: startMins, End: endMins}
		recipes[c] = ReplayRecipe{
			BufferPct:    bufferPct,
			FloorMinutes: MinClashFootprintMinutes,
		}
	}
	return CompetitionPlacement{
		Placed:        true,
		Day:           dayIdx,
		Courts:        courts,
		Windows:       windows,
		LengthSource:  "minimum",
		LengthReason:  reason,
		ReplayRecipes: recipes,
		PastMidnight:  pastMidnight(endMins),
		Minutes:       footprint,
	}
}

// PlaceCompetition computes the schedule placement for comp within tournament.
func (e *Engine) PlaceCompetition(comp *state.Competition, tournament *state.Tournament) CompetitionPlacement {
	if comp == nil {
		return CompetitionPlacement{Placed: false, NotPlacedReason: "no_start_time"}
	}

	startMins, ok := state.ParseClockMinutes(comp.StartTime)
	if !ok {
		return CompetitionPlacement{Placed: false, NotPlacedReason: "no_start_time"}
	}

	trimmedDate := strings.TrimSpace(comp.Date)
	if trimmedDate == "" {
		return CompetitionPlacement{Placed: false, NotPlacedReason: "no_date"}
	}

	var days []string
	if tournament != nil {
		days = tournament.Days()
	}
	dayIdx := 0
	notPlacedReason := ""
	if len(days) > 0 {
		for i, d := range days {
			if d == trimmedDate {
				dayIdx = i + 1
				break
			}
		}
		if dayIdx == 0 {
			notPlacedReason = "date_outside_tournament"
		}
	} else {
		// When tournament.Days() is empty (the tournament has no date yet),
		// the competition is still placed with Day 0 so the clash check keeps
		// today's results for a tournament with no date.
		dayIdx = 0
	}

	outsideDays := func(p CompetitionPlacement) CompetitionPlacement {
		if notPlacedReason != "" {
			p.Placed = false
			p.NotPlacedReason = notPlacedReason
		}
		return p
	}

	courts := append([]string(nil), InheritedDrawCourts(comp.Courts, tournament)...)

	// Drawn rule: exactly CourtFloor's: comp.Status != state.CompStatusSetup
	// AND CourtWindows returned at least one court.
	if comp.Status != state.CompStatusSetup {
		drawWindows, err := e.store.CourtWindows(comp.ID, slotMinutesFor(tournament))
		if err != nil {
			log.Printf("engine: place competition %s: court windows: %v", comp.ID, err)
			return CompetitionPlacement{Placed: false, NotPlacedReason: "draw_unreadable"}
		}
		if len(drawWindows) > 0 {
			courtSet := make(map[string]bool, len(courts))
			for _, c := range courts {
				courtSet[c] = true
			}
			for c := range drawWindows {
				if !courtSet[c] {
					courts = append(courts, c)
					courtSet[c] = true
				}
			}
			sortCourtLabels(courts)

			recipes := make(map[string]ReplayRecipe, len(drawWindows))
			pastMid := false
			maxSpan := 0
			for c, w := range drawWindows {
				recipes[c] = ReplayRecipe{SlotLengths: w.SlotLengths}
				if pastMidnight(w.End) {
					pastMid = true
				}
				if span := w.End - w.Start; span > maxSpan {
					maxSpan = span
				}
			}
			return outsideDays(CompetitionPlacement{
				Placed:        true,
				Day:           dayIdx,
				Courts:        courts,
				Windows:       drawWindows,
				LengthSource:  "draw",
				ReplayRecipes: recipes,
				PastMidnight:  pastMid,
				Minutes:       maxSpan,
			})
		}
	}

	// Before the draw:
	normComp, normTourn := normalizedEstimateInputs(comp, tournament)
	scenarios := estimateScenarios(normComp, normTourn)
	n := estimateCourtCount(comp, tournament)

	playerCount, source, seedRanks, countErr := e.estimateParticipantCount(comp, tournament)
	if countErr != nil || playerCount == 0 {
		reason := "no_participants"
		if countErr != nil {
			reason = "estimate_failed"
		}
		return outsideDays(minimumPlacement(dayIdx, courts, startMins, reason, normTourn.SlowestCourtBufferPct))
	}

	est, poolCount, knockoutCount, estErr := estimateForPlayers(comp, tournament, playerCount, seedRanks, false)
	if estErr != nil {
		return outsideDays(minimumPlacement(dayIdx, courts, startMins, "estimate_failed", normTourn.SlowestCourtBufferPct))
	}

	footprint := est.TotalDurationMinutes
	if footprint < MinClashFootprintMinutes {
		footprint = MinClashFootprintMinutes
	}
	endMins := startMins + footprint
	windows := make(map[string]state.CourtWindow, len(courts))
	for _, c := range courts {
		windows[c] = state.CourtWindow{Start: startMins, End: endMins}
	}

	lengthSource := "estimate"
	if source == ParticipantsSourceExpected {
		lengthSource = "expected"
	}

	var bestEnd, worstEnd string
	if normComp.IsKachinuki() {
		bestEnd = state.FormatClockHHMM(startMins + est.BestCaseMinutes)
		worstEnd = state.FormatClockHHMM(startMins + est.WorstCaseMinutes)
	}

	recipes := make(map[string]ReplayRecipe, len(courts))
	for ci, c := range courts {
		if ci >= n {
			recipes[c] = ReplayRecipe{
				BufferPct:    normTourn.SlowestCourtBufferPct,
				FloorMinutes: MinClashFootprintMinutes,
			}
			continue
		}
		buildRuns := func(sc schedScenario) []ReplayRun {
			var runs []ReplayRun
			pCount := courtShare(poolCount, n, ci)
			if pCount > 0 {
				runs = append(runs, ReplayRun{Count: pCount, Minutes: sc.poolPerMatch})
			}
			koCount := courtShare(knockoutCount, n, ci)
			if koCount > 0 {
				runs = append(runs, ReplayRun{Count: koCount, Minutes: sc.knockoutPerMatch})
			}
			return runs
		}

		if normComp.IsKachinuki() {
			recipes[c] = ReplayRecipe{
				Runs:         buildRuns(scenarios[1]), // avg
				BestRuns:     buildRuns(scenarios[0]), // best
				WorstRuns:    buildRuns(scenarios[2]), // worst
				BufferPct:    normTourn.SlowestCourtBufferPct,
				FloorMinutes: MinClashFootprintMinutes,
			}
		} else {
			recipes[c] = ReplayRecipe{
				Runs:         buildRuns(scenarios[0]),
				BufferPct:    normTourn.SlowestCourtBufferPct,
				FloorMinutes: MinClashFootprintMinutes,
			}
		}
	}

	return outsideDays(CompetitionPlacement{
		Placed:        true,
		Day:           dayIdx,
		Courts:        courts,
		Windows:       windows,
		LengthSource:  lengthSource,
		BestEnd:       bestEnd,
		WorstEnd:      worstEnd,
		ReplayRecipes: recipes,
		PastMidnight:  pastMidnight(endMins),
		Minutes:       footprint,
	})
}

// calendarDays builds the CalendarDay slice and UnusedPause slice for tournament t.
// dayBreaks remains the single owner of pause usability.
func calendarDays(t *state.Tournament) ([]CalendarDay, []UnusedPause) {
	if t == nil {
		return []CalendarDay{}, []UnusedPause{}
	}
	dates := t.Days()
	if dates == nil {
		dates = []string{}
	}
	days := make([]CalendarDay, len(dates))
	for i, d := range dates {
		days[i] = CalendarDay{
			Day:    i + 1,
			Date:   d,
			Breaks: []CalendarBreak{},
		}
	}

	refMidnight := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
	unused := []UnusedPause{}

	for _, p := range t.Pauses {
		pUsed := false

		for di, d := range dates {
			windows := dayBreaksFor(t, p, d)
			for _, w := range windows {
				pUsed = true
				startMins := int(w.start.Sub(refMidnight).Minutes())
				endMins := int(w.end.Sub(refMidnight).Minutes())
				days[di].Breaks = append(days[di].Breaks, CalendarBreak{
					Name:      p.Name,
					Start:     state.FormatClockHHMM(startMins),
					End:       state.FormatClockHHMM(endMins),
					StartMins: startMins,
					EndMins:   endMins,
				})
			}
		}

		if !pUsed {
			reason := unusedPauseReason(p, t)
			unused = append(unused, UnusedPause{Name: p.Name, Reason: reason})
		}
	}

	for i := range days {
		if days[i].Breaks == nil {
			days[i].Breaks = []CalendarBreak{}
		}
		sort.SliceStable(days[i].Breaks, func(a, b int) bool {
			return days[i].Breaks[a].StartMins < days[i].Breaks[b].StartMins
		})
	}

	if days == nil {
		days = []CalendarDay{}
	}
	if unused == nil {
		unused = []UnusedPause{}
	}

	return days, unused
}

// unusedPauseReason explains why a configured pause was not used by the schedule.
func unusedPauseReason(p state.Pause, t *state.Tournament) string {
	if t == nil || len(t.Days()) == 0 {
		return "no_tournament_date"
	}
	if strings.TrimSpace(p.Start) == "" {
		return "no_start_time"
	}
	durDays := t.DurationDays
	if durDays < 1 {
		durDays = 1
	}
	if p.Day < 0 || p.Day > durDays {
		return "day_not_in_tournament"
	}
	if ParseDurationMinutes(p.Length) <= 0 {
		return "no_length"
	}
	return "unreadable_start_time"
}

// TournamentCalendar produces the complete schedule calendar for the active tournament.
func (e *Engine) TournamentCalendar() (*TournamentCalendarResult, error) {
	tourn, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}

	days, unused := calendarDays(tourn)

	ids, err := e.store.ListCompetitions()
	if err != nil {
		return nil, err
	}

	comps := make([]*state.Competition, 0, len(ids))
	for _, id := range ids {
		c, err := e.store.LoadCompetition(id)
		if err != nil {
			log.Printf("engine: tournament calendar: load competition %s: %v", id, err)
			continue
		}
		if c == nil {
			log.Printf("engine: tournament calendar: load competition %s: not found", id)
			continue
		}
		comps = append(comps, c)
	}

	placements := make([]CompetitionPlacement, len(comps))
	for i, comp := range comps {
		placements[i] = e.PlaceCompetition(comp, tourn)
	}

	courtSeen := make(map[string]bool)
	if tourn != nil {
		for _, c := range tourn.Courts {
			c = strings.TrimSpace(c)
			if c != "" {
				courtSeen[c] = true
			}
		}
	}
	for _, p := range placements {
		if !p.Placed {
			continue
		}
		for _, c := range p.Courts {
			c = strings.TrimSpace(c)
			if c != "" {
				courtSeen[c] = true
			}
		}
		for c := range p.Windows {
			c = strings.TrimSpace(c)
			if c != "" {
				courtSeen[c] = true
			}
		}
	}
	lanes := make([]string, 0, len(courtSeen))
	for c := range courtSeen {
		lanes = append(lanes, c)
	}
	sortCourtLabels(lanes)
	if lanes == nil {
		lanes = []string{}
	}

	clashMap := make(map[string][]string, len(comps))
	for i := 0; i < len(comps); i++ {
		for j := i + 1; j < len(comps); j++ {
			pI := placements[i]
			pJ := placements[j]
			if !pI.TakesPartInClashes() || !pJ.TakesPartInClashes() {
				continue
			}
			if !state.SameDate(comps[i].Date, comps[j].Date) {
				continue
			}
			hasClash := false
			for court, wI := range pI.Windows {
				if wJ, ok := pJ.Windows[court]; ok {
					if Overlaps(wI, wJ) {
						hasClash = true
						break
					}
				}
			}
			if hasClash {
				clashMap[comps[i].ID] = append(clashMap[comps[i].ID], comps[j].ID)
				clashMap[comps[j].ID] = append(clashMap[comps[j].ID], comps[i].ID)
			}
		}
	}

	calComps := make([]CalendarCompetition, len(comps))
	for i, comp := range comps {
		partners := clashMap[comp.ID]
		if partners == nil {
			partners = []string{}
		} else {
			sort.Strings(partners)
		}
		calComps[i] = CalendarCompetition{
			ID:            comp.ID,
			Name:          comp.Name,
			Kind:          comp.Kind,
			Status:        comp.Status,
			ConfigVersion: state.ConfigVersion(comp),
			Placement:     placements[i],
			ClashPartners: partners,
		}
	}

	if calComps == nil {
		calComps = []CalendarCompetition{}
	}

	return &TournamentCalendarResult{
		Days:         days,
		Courts:       lanes,
		UnusedPauses: unused,
		Competitions: calComps,
	}, nil
}

func sortCourtLabels(courts []string) {
	sort.SliceStable(courts, func(i, j int) bool {
		a, b := courts[i], courts[j]
		if len(a) != len(b) {
			return len(a) < len(b)
		}
		return a < b
	})
}
