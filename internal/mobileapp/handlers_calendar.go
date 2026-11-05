// TournamentCalendar over 30 competitions on the most expensive formats (mixed and
// league with large rosters) measured 25 ms per call (BenchmarkTournamentCalendar
// in internal/engine/calendar_test.go, go test -bench, 2026-11-04). Check-in burst:
// every check-in fires participants_updated; two open calendar pages at a desk doing
// 20 check-ins a minute ask for up to 40 calendars a minute after the page 1 s
// debounce, which is about 40 x 25 ms = 1 s of CPU per minute (under 2 percent of
// one core), so it is small and needs no cache.

package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// CalendarOverlap describes an overlapping schedule window on a court with another competition.
type CalendarOverlap struct {
	CompetitionID string `json:"competitionId"`
	Name          string `json:"name"`
	Court         string `json:"court"`
	Start         int    `json:"start"`
	End           int    `json:"end"`
}

type calendarPreviewResponse struct {
	CompetitionID string                  `json:"competitionId"`
	Status        state.CompetitionStatus `json:"status"`
	// Previewed is true when the placement was computed from the requested
	// day/startTime/courts (competition status is setup), and false when the
	// competition is already drawn so the placement (day, windows) and
	// overlaps reflect where the competition sits now rather than the
	// requested values; Retime carries what moving it would do instead.
	Previewed bool                        `json:"previewed"`
	Placement engine.CompetitionPlacement `json:"placement"`
	Overlaps  []CalendarOverlap           `json:"overlaps"`
	// Retime is the dry run of moving a drawn (draw-ready) competition to the
	// requested day and start: per shiaijo the old and new first start and last
	// end, the number of matches no longer holding a fresh time, and whether any
	// time changes. Absent for a competition that is not draw ready.
	Retime *engine.RetimeOutcome `json:"retime,omitempty"`
}

// RegisterCalendarHandlers registers the tournament schedule calendar endpoints.
func RegisterCalendarHandlers(r gin.IRoutes, store CalendarStore, eng CalendarPlanner) {
	r.GET("/schedule/calendar", func(c *gin.Context) {
		res, err := eng.TournamentCalendar()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build the calendar"})
			return
		}

		c.JSON(http.StatusOK, res)
	})

	r.GET("/schedule/calendar/preview", func(c *gin.Context) {
		compID := strings.TrimSpace(c.Query("competitionId"))
		if compID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "competitionId is required"})
			return
		}

		comp, err := store.LoadCompetition(compID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load competition"})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Competition not found"})
			return
		}

		tourn, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load tournament config"})
			return
		}
		if tourn == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tournament is not configured"})
			return
		}

		rawDate := strings.TrimSpace(c.Query("date"))
		if rawDate == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "date is required"})
			return
		}
		days := tourn.Days()
		if len(days) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "The tournament has no days yet. Set its date first."})
			return
		}

		// Days() spells a day DD-MM-YYYY, the same spelling a competition stores in
		// its date, so the request must name one of them exactly.
		matchedDay := ""
		for _, d := range days {
			if d == rawDate {
				matchedDay = d
				break
			}
		}
		if matchedDay == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("date must be one of the tournament days, written DD-MM-YYYY (%s to %s)", days[0], days[len(days)-1])})
			return
		}

		rawStartTime := strings.TrimSpace(c.Query("startTime"))
		if rawStartTime == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "startTime is required"})
			return
		}
		if _, err := time.Parse("15:04", rawStartTime); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "startTime must be HH:MM"})
			return
		}

		rawCourts := c.Query("courts")
		if strings.TrimSpace(rawCourts) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "courts is required"})
			return
		}
		parts := strings.Split(rawCourts, ",")
		chosenCourts := make([]string, 0, len(parts))
		seenCourts := make(map[string]bool, len(parts))
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "court label cannot be empty"})
				return
			}
			if seenCourts[trimmed] {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("duplicate court label %q", trimmed)})
				return
			}
			seenCourts[trimmed] = true
			chosenCourts = append(chosenCourts, trimmed)
		}

		tournCourtMap := make(map[string]bool, len(tourn.Courts))
		for _, tc := range tourn.Courts {
			tournCourtMap[strings.TrimSpace(tc)] = true
		}
		for _, cc := range chosenCourts {
			if !tournCourtMap[cc] {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("court label %q is not held by the tournament", cc)})
				return
			}
		}

		// A config with no status is one not yet drawn, as the estimate reads it.
		notDrawn := comp.Status == state.CompStatusSetup || comp.Status == ""
		targetToPlace := comp
		if notDrawn {
			clone := *comp
			clone.Date = matchedDay
			clone.StartTime = rawStartTime
			clone.Courts = append([]string(nil), chosenCourts...)
			targetToPlace = &clone
		}

		placement := eng.PlaceCompetition(targetToPlace, tourn)

		cal, err := eng.TournamentCalendar()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to build the calendar"})
			return
		}

		overlapsList := make([]CalendarOverlap, 0)
		for _, other := range cal.Competitions {
			if other.ID == comp.ID {
				continue
			}
			for _, court := range engine.ClashingCourts(targetToPlace.Date, placement, other.Date, other.Placement) {
				otherWin := other.Placement.Windows[court]
				overlapsList = append(overlapsList, CalendarOverlap{
					CompetitionID: other.ID,
					Name:          other.Name,
					Court:         court,
					Start:         otherWin.Start,
					End:           otherWin.End,
				})
			}
		}

		sort.SliceStable(overlapsList, func(i, j int) bool {
			if overlapsList[i].Name != overlapsList[j].Name {
				return overlapsList[i].Name < overlapsList[j].Name
			}
			if overlapsList[i].CompetitionID != overlapsList[j].CompetitionID {
				return overlapsList[i].CompetitionID < overlapsList[j].CompetitionID
			}
			return overlapsList[i].Court < overlapsList[j].Court
		})

		// A drawn competition's windows come from the stored draw; moving it
		// moves its matches, so the answer carries that dry run (nothing written).
		var retime *engine.RetimeOutcome
		if comp.Status == state.CompStatusDrawReady {
			moved := *comp
			moved.Date = matchedDay
			moved.StartTime = rawStartTime
			out, err := eng.RetimePreview(comp, &moved)
			switch {
			case errors.Is(err, engine.ErrRetimeUnavailable):
				c.JSON(http.StatusConflict, gin.H{"code": "retime_unavailable", "error": "This competition's match order could not be read back, so its matches were not moved. Nothing was changed."})
				return
			case errors.Is(err, state.ErrCourtFloorPastMidnight):
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
				return
			case err != nil:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to work out the new match times"})
				return
			}
			retime = &out
		}

		c.JSON(http.StatusOK, calendarPreviewResponse{
			CompetitionID: comp.ID,
			Status:        comp.Status,
			Previewed:     notDrawn,
			Placement:     placement,
			Overlaps:      overlapsList,
			Retime:        retime,
		})
	})
}
