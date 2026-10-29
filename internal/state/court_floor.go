package state

import (
	"fmt"
	"log"
	"strconv"
	"strings"
)

// SlotMinutes computes the duration in minutes for a match in comp.
// isKnockout distinguishes knockout/bracket matches from pool matches.
type SlotMinutes func(comp *Competition, isKnockout bool) int

// CourtEnds calculates the planned end ("HH:MM") of compID's last slot on each
// court it holds matches on. It reads pool matches and bracket matches (including
// any 3rd-place match). A bracket bye (a completed match with an empty side)
// contributes its start time but zero duration, mirroring assignBracketMatchSlots.
// Returns an empty/nil map if the competition has no slots or is not drawn.
func (s *Store) CourtEnds(compID string, minutes SlotMinutes) (map[string]string, error) {
	comp, err := s.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, nil
	}

	poolMatches, err := s.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	bracket, err := s.LoadBracket(compID)
	if err != nil {
		return nil, err
	}

	latestEnd := make(map[string]int)
	observe := func(court, scheduledAt string, isKnockout, isBye bool) {
		court = strings.TrimSpace(court)
		if court == "" {
			return
		}
		endMins, ok := ParseClockMinutes(scheduledAt)
		if !ok {
			return
		}
		if !isBye && minutes != nil {
			endMins += minutes(comp, isKnockout)
		}
		if cur, exists := latestEnd[court]; !exists || endMins > cur {
			latestEnd[court] = endMins
		}
	}

	for i := range poolMatches {
		observe(poolMatches[i].Court, poolMatches[i].ScheduledAt, false, false)
	}
	for _, bm := range BracketMatchesInDrawOrder(bracket) {
		isBye := bm.Status == MatchStatusCompleted && (strings.TrimSpace(bm.SideA) == "" || strings.TrimSpace(bm.SideB) == "")
		observe(bm.Court, bm.ScheduledAt, true, isBye)
	}

	if len(latestEnd) == 0 {
		return nil, nil
	}

	res := make(map[string]string, len(latestEnd))
	for court, endMins := range latestEnd {
		res[court] = formatClockHHMM(endMins)
	}
	return res, nil
}

// CourtFloor determines the per-court earliest start time ("HH:MM") for comp
// on each of the specified courts.
//
// Scheduling rule:
// A competition's first slot on a court is no earlier than the planned end of the
// last slot on that court of every OTHER competition that:
//  1. is on the same day (trimmed, non-empty, equal Date; empty date floors nothing),
//  2. already holds slots on that court (pool and/or bracket matches with parseable ScheduledAt),
//  3. and whose StartTime is no later than this competition's (<=; no ID tie-break:
//     of two competitions with equal start time, the one drawn second goes after the first).
//
// A competition with no parseable StartTime of its own takes no floor; another with none is skipped.
// Courts nobody floors are absent from the returned map.
//
// Concurrency rule:
// CourtFloor scans other competitions using public Store loaders outside any transaction.
// It must NOT be called from inside WithTransaction to prevent circular-wait deadlocks
// across per-competition locks.
func (s *Store) CourtFloor(comp *Competition, courts []string, minutes SlotMinutes) (map[string]string, error) {
	if comp == nil || len(courts) == 0 {
		return nil, nil
	}
	compStartMins, ok := ParseClockMinutes(comp.StartTime)
	if !ok {
		return nil, nil
	}
	if strings.TrimSpace(comp.Date) == "" {
		return nil, nil
	}

	ids, err := s.ListCompetitions()
	if err != nil {
		return nil, err
	}

	courtSet := make(map[string]struct{}, len(courts))
	for _, c := range courts {
		if c != "" {
			courtSet[c] = struct{}{}
		}
	}
	if len(courtSet) == 0 {
		return nil, nil
	}

	floorMins := make(map[string]int)

	for _, id := range ids {
		if id == comp.ID {
			continue
		}
		other, err := s.LoadCompetition(id)
		if err != nil {
			log.Printf("state: court floor %s: load competition: %v", id, err)
			continue
		}
		if other == nil {
			continue
		}
		if !SameDate(comp.Date, other.Date) {
			continue
		}
		otherStartMins, ok := ParseClockMinutes(other.StartTime)
		if !ok || otherStartMins > compStartMins {
			continue
		}

		ends, err := s.CourtEnds(id, minutes)
		if err != nil {
			log.Printf("state: court floor %s: court ends: %v", id, err)
			continue
		}
		if len(ends) == 0 {
			continue
		}

		for court := range courtSet {
			endStr, ok := ends[court]
			if !ok {
				continue
			}
			endMins, ok := ParseClockMinutes(endStr)
			if !ok {
				continue
			}
			if cur, exists := floorMins[court]; !exists || endMins > cur {
				floorMins[court] = endMins
			}
		}
	}

	if len(floorMins) == 0 {
		return nil, nil
	}

	res := make(map[string]string, len(floorMins))
	for court, mins := range floorMins {
		res[court] = formatClockHHMM(mins)
	}
	return res, nil
}

// ParseClockMinutes parses a "HH:MM" clock string into minutes-since-midnight.
// ok is false for empty or malformed input, so callers skip an unplaceable
// slot rather than treating it as midnight.
func ParseClockMinutes(s string) (int, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, false
	}
	h, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	m, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func formatClockHHMM(mins int) string {
	if mins < 0 {
		mins = 0
	}
	if mins > 23*60+59 {
		mins = 23*60 + 59
	}
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// SameDate is true when both dates are non-empty and equal (after trimming).
// An empty date means the competition isn't placed on a day, so it shares none.
func SameDate(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a != "" && a == b
}
