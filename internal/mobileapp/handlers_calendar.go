// TournamentCalendar over 30 competitions on the most expensive formats (mixed and
// league with large rosters) measured 25 ms per call (BenchmarkTournamentCalendar
// in internal/engine/calendar_test.go, go test -bench, 2026-11-04). Check-in burst:
// every check-in fires participants_updated; two open calendar pages at a desk doing
// 20 check-ins a minute ask for up to 40 calendars a minute after the page 1 s
// debounce, which is about 40 x 25 ms = 1 s of CPU per minute (under 2 percent of
// one core), so it is small and needs no cache.

package mobileapp

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

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
		if _, ok := state.ParseClockMinutes(rawStartTime); !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "startTime must be HH:MM"})
			return
		}

		// A config with no status is one not yet drawn, as the estimate reads it.
		notDrawn := comp.Status == state.CompStatusSetup || comp.Status == ""
		targetToPlace := comp
		if notDrawn {
			// Only a competition not yet drawn is placed on the courts asked for;
			// a drawn one keeps the shiaijo of its draw, so it needs none.
			chosenCourts, ok := calendarPreviewCourts(c, tourn)
			if !ok {
				return
			}
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
			if err != nil {
				if refusal := retimeRefusal(err); refusal != nil {
					writeTxResponse(c, refusal)
					return
				}
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

// calendarPreviewCourts reads and checks the `courts` query of a preview for a
// competition not yet drawn: shiaijo labels, each held by the tournament and
// none repeated. It answers 400 and returns false when they do not hold.
func calendarPreviewCourts(c *gin.Context, tourn *state.Tournament) ([]string, bool) {
	rawCourts := c.Query("courts")
	if strings.TrimSpace(rawCourts) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "courts is required"})
		return nil, false
	}
	held := make(map[string]bool, len(tourn.Courts))
	for _, tc := range tourn.Courts {
		held[strings.TrimSpace(tc)] = true
	}
	parts := strings.Split(rawCourts, ",")
	chosen := make([]string, 0, len(parts))
	seen := make(map[string]bool, len(parts))
	for _, p := range parts {
		label := strings.TrimSpace(p)
		switch {
		case label == "":
			c.JSON(http.StatusBadRequest, gin.H{"error": "court label cannot be empty"})
			return nil, false
		case seen[label]:
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("duplicate court label %q", label)})
			return nil, false
		}
		seen[label] = true
		chosen = append(chosen, label)
	}
	for _, label := range chosen {
		if !held[label] {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("court label %q is not held by the tournament", label)})
			return nil, false
		}
	}
	return chosen, true
}
