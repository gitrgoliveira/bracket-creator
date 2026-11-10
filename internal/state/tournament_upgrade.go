package state

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"time"
)

var errNoTournament = errors.New("state: no tournament")

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
	if t == nil || (t.OpeningBlockLegacy == "" && t.ClosingBlockLegacy == "" && t.LunchBlockLegacy == "") {
		return nil
	}

	compIDs, err := s.ListCompetitions()
	if err != nil {
		return err
	}

	days := t.Days()
	dayEarliest := make(map[int]int)
	for _, id := range compIDs {
		comp, err := s.LoadCompetition(id)
		if err != nil {
			return fmt.Errorf("reading competition %s: %w", id, err)
		}
		if comp == nil {
			continue
		}
		// A competition dated outside the tournament's days (0) has no opening.
		day := DayIndex(days, comp.Date)
		if day == 0 {
			continue
		}
		startTime := comp.StartTime
		if startTime == "" {
			startTime = "09:00"
		}
		startMins, ok := ParseClockMinutes(startTime)
		if !ok {
			continue
		}
		prev, exists := dayEarliest[day]
		if !exists || startMins < prev {
			dayEarliest[day] = startMins
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
					m := dayEarliest[n]
					openingPauses = append(openingPauses, Pause{
						Name:   "Opening ceremony",
						Start:  fmt.Sprintf("%02d:%02d", m/60, m%60),
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

		// A key is dropped only once its value is carried: a length moved into
		// a pause, or one that release scheduled as nothing (empty, zero or
		// negative). A value time.ParseDuration refuses ("45 min") was also
		// scheduled as nothing by v2.2 and v2.3, which read it the same way,
		// but it is what the organiser typed: it stays on disk, untouched,
		// and is named in the log, never deleted.
		desired.OpeningBlockLegacy = keepUnreadBlock("opening_block", desired.OpeningBlockLegacy)
		desired.ClosingBlockLegacy = keepUnreadBlock("closing_block", desired.ClosingBlockLegacy)
		desired.LunchBlockLegacy = keepUnreadBlock("lunch_block", desired.LunchBlockLegacy)

		return nil
	})

	if errors.Is(err, errNoTournament) {
		return nil
	}
	return err
}

// keepUnreadBlock answers what a legacy ceremony key keeps after the upgrade:
// nothing when time.ParseDuration reads its value (the upgrade has carried it),
// the value itself when it does not, logged so the organiser can re-enter it
// as a pause.
func keepUnreadBlock(key, value string) string {
	if value == "" {
		return ""
	}
	if _, err := time.ParseDuration(value); err == nil {
		return ""
	}
	log.Printf("state: tournament.md %s %q is not a length (such as 30m or 1h30m), so no pause was made from it; it is kept in tournament.md: add it as a pause in the tournament settings", key, value)
	return value
}
