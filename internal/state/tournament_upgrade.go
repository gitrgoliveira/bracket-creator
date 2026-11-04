package state

import (
	"errors"
	"fmt"
	"slices"
	"time"
)

var errNoTournament = errors.New("state: no tournament")

// parseClockMinutes converts "HH:MM" or "H:MM" to minutes since midnight.
// If unparseable, it returns 24*60 (end of day).
func parseClockMinutes(s string) int {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 24 * 60
	}
	return t.Hour()*60 + t.Minute()
}

// upgradeLegacyCeremonies migrates legacy ceremony blocks to pauses.
// It runs once at startup from NewStore, after sweepLegacyUpgrades, and a
// failure is logged and leaves the file as it was.
// v2.2.0 and v2.3.0 stored the ceremonies as lengths with no clock time; this
// one-time step turns them into pauses; the day's first competition keeps its
// opening; a closing ceremony needs a time the organiser gives later.
func (s *Store) upgradeLegacyCeremonies() error {
	t, err := s.LoadTournament()
	if err != nil {
		return err
	}
	if t == nil || (t.OpeningBlockLegacy == "" && t.ClosingBlockLegacy == "") {
		return nil
	}

	compIDs, err := s.ListCompetitions()
	if err != nil {
		return err
	}

	days := t.Days()
	dayIndex := func(compDate string) (int, bool) {
		if compDate == "" {
			return 1, true
		}
		for i, d := range days {
			if d == compDate {
				return i + 1, true
			}
		}
		return 0, false
	}

	dayEarliest := make(map[int]string)
	for _, id := range compIDs {
		comp, err := s.LoadCompetition(id)
		if err != nil {
			return fmt.Errorf("reading competition %s: %w", id, err)
		}
		if comp == nil {
			continue
		}
		day, ok := dayIndex(comp.Date)
		if !ok {
			continue
		}
		startTime := comp.StartTime
		if startTime == "" {
			startTime = "09:00"
		}
		prev, exists := dayEarliest[day]
		if !exists || parseClockMinutes(startTime) < parseClockMinutes(prev) {
			dayEarliest[day] = startTime
		}
	}

	_, err = s.UpdateTournamentChanged(&Tournament{}, func(current *Tournament, desired *Tournament) error {
		if current == nil {
			return errNoTournament
		}
		cp := s.copyTournament(current)
		*desired = *cp

		var openingPauses []Pause
		if d, perr := time.ParseDuration(desired.OpeningBlockLegacy); perr == nil && d > 0 {
			if len(dayEarliest) == 0 {
				openingPauses = append(openingPauses, Pause{
					Name:   "Opening ceremony",
					Length: desired.OpeningBlockLegacy,
				})
			} else {
				dayList := make([]int, 0, len(dayEarliest))
				for dKey := range dayEarliest {
					dayList = append(dayList, dKey)
				}
				slices.Sort(dayList)
				for _, n := range dayList {
					dayVal := n
					if desired.DurationDays <= 1 {
						dayVal = 0
					}
					openingPauses = append(openingPauses, Pause{
						Name:   "Opening ceremony",
						Start:  dayEarliest[n],
						Length: desired.OpeningBlockLegacy,
						Day:    dayVal,
					})
				}
			}
		}

		var closingPauses []Pause
		if d, perr := time.ParseDuration(desired.ClosingBlockLegacy); perr == nil && d > 0 {
			closingPauses = append(closingPauses, Pause{
				Name:   "Closing ceremony",
				Length: desired.ClosingBlockLegacy,
				Day:    0,
			})
		}

		combined := make([]Pause, 0, len(openingPauses)+len(desired.Pauses)+len(closingPauses))
		combined = append(combined, openingPauses...)
		combined = append(combined, desired.Pauses...)
		combined = append(combined, closingPauses...)
		desired.Pauses = combined

		desired.OpeningBlockLegacy = ""
		desired.ClosingBlockLegacy = ""

		return nil
	})

	if errors.Is(err, errNoTournament) {
		return nil
	}
	return err
}
