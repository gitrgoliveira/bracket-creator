package state

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"
)

var errNoTournament = errors.New("state: no tournament")

// upgradeLegacyCeremonies migrates legacy ceremony blocks to pauses.
// It runs once at startup from NewStore, after sweepLegacyUpgrades, and a
// failure is logged and leaves the file as it was.
// v2.3.0, the one release that stored them, kept the ceremonies as lengths with
// no clock time; this one-time step turns them into pauses; the day's first
// competition keeps its opening; a closing ceremony needs a time the organiser
// gives later. A length that cannot be read becomes a pause with no length.
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
		if opening, ok := legacyBlockPause("Opening ceremony", desired.OpeningBlockLegacy); ok {
			if len(dayEarliest) == 0 {
				openingPauses = append(openingPauses, opening)
			} else {
				dayList := make([]int, 0, len(dayEarliest))
				for dKey := range dayEarliest {
					dayList = append(dayList, dKey)
				}
				slices.Sort(dayList)
				for _, n := range dayList {
					p := opening
					if desired.DurationDays > 1 {
						p.Day = n
					}
					m := dayEarliest[n]
					p.Start = fmt.Sprintf("%02d:%02d", m/60, m%60)
					openingPauses = append(openingPauses, p)
				}
			}
		}

		var closingPauses []Pause
		if closing, ok := legacyBlockPause("Closing ceremony", desired.ClosingBlockLegacy); ok {
			closingPauses = append(closingPauses, closing)
		}

		combined := make([]Pause, 0, len(openingPauses)+len(desired.Pauses)+len(closingPauses))
		combined = append(combined, openingPauses...)
		combined = append(combined, desired.Pauses...)
		combined = append(combined, closingPauses...)
		desired.Pauses = combined

		// Every key is carried now: a readable length into its pause, a value
		// time.ParseDuration refuses ("45 min") into a pause with no length
		// named with that text (legacyBlockPause; the lunch one is made by
		// the load, migrateLegacyTournament), and an empty, zero or negative
		// one, which v2.3 scheduled as nothing, into nothing. So all three go
		// in this write, and a later start has nothing left to upgrade.
		logUnreadBlock("opening_block", desired.OpeningBlockLegacy)
		logUnreadBlock("closing_block", desired.ClosingBlockLegacy)
		logUnreadBlock("lunch_block", desired.LunchBlockLegacy)
		desired.OpeningBlockLegacy = ""
		desired.ClosingBlockLegacy = ""
		desired.LunchBlockLegacy = ""

		return nil
	})

	if errors.Is(err, errNoTournament) {
		return nil
	}
	return err
}

// maxUnreadLengthNote caps the typed text an unreadable legacy length adds to
// its pause's name, so the name stays inside the 60 characters a tournament
// save accepts (v2.3 capped the value at 16, so only a hand edit is longer).
const maxUnreadLengthNote = 40

// legacyBlockPause is the pause a legacy ceremony or lunch value becomes, with
// no start or day yet: the one rule for the upgrade and the load's lunch.
// A length time.ParseDuration reads as above zero is the pause's length. A
// value it cannot read ("45 min") is what the organiser typed, which v2.3
// scheduled as nothing: it becomes a pause with no length, named with that
// text, so the setup page lists it as a pause to give a length, and the
// schedule skips it until it has one. Empty, zero or negative makes no pause,
// as v2.3 scheduled nothing for it.
func legacyBlockPause(name, value string) (Pause, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return Pause{}, false
	}
	if d, err := time.ParseDuration(value); err == nil {
		if d <= 0 {
			return Pause{}, false
		}
		return Pause{Name: name, Length: value}, true
	}
	if r := []rune(value); len(r) > maxUnreadLengthNote {
		value = string(r[:maxUnreadLengthNote])
	}
	return Pause{Name: name + " (" + value + ")"}, true
}

// logUnreadBlock names, at the upgrade, a legacy value that became a pause
// with no length, so the log says where that pause came from.
func logUnreadBlock(key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, err := time.ParseDuration(value); err == nil {
		return
	}
	log.Printf("state: tournament.md %s %q is not a length (such as 30m or 1h30m), so it became a pause with no length; give it one in the tournament settings", key, value)
}
