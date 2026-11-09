package state

import (
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"strings"
)

// ErrCourtFloorPastMidnight reports a shared court another competition of the
// same day is planned to hold until midnight or later, so no slot of that day
// is left on it: the floor would need a time scheduledAt (a bare "HH:MM")
// cannot hold. The draw is refused rather than given a wrapped or clamped time.
// The error is a *CourtFloorPastMidnightError naming the court and competition.
var ErrCourtFloorPastMidnight = errors.New("shared court planned until after midnight")

// CourtFloorPastMidnightError is ErrCourtFloorPastMidnight with the court and
// the competition whose plan holds it past midnight.
type CourtFloorPastMidnightError struct {
	Court           string
	CompetitionID   string
	CompetitionName string
}

// Error is the operator's sentence: what holds the court and what to do.
func (e *CourtFloorPastMidnightError) Error() string {
	name := e.CompetitionName
	if strings.TrimSpace(name) == "" {
		name = e.CompetitionID
	}
	return fmt.Sprintf("Shiaijo %s is planned until after midnight by %s. Move this competition to another shiaijo or day, or re-time %s with Auto-schedule.", e.Court, name, name)
}

// Is lets errors.Is(err, ErrCourtFloorPastMidnight) match.
func (e *CourtFloorPastMidnightError) Is(target error) bool {
	return target == ErrCourtFloorPastMidnight
}

// SlotMinutes computes the duration in minutes for a match in comp.
// isKnockout distinguishes knockout/bracket matches from pool matches.
type SlotMinutes func(comp *Competition, isKnockout bool) int

// CourtWindow holds the first slot start and last slot end on a court, in
// minutes since midnight (unwrapped, so an end past midnight reads >= 1440),
// and the match slot lengths on that court in chronological order.
type CourtWindow struct {
	Start       int   `json:"start"`
	End         int   `json:"end"`
	SlotLengths []int `json:"-"`
}

// CourtWindows calculates the planned window [first slot start, last slot end]
// in minutes since midnight for compID on each court it holds matches on.
// It reads pool matches and bracket matches (including any 3rd-place match) in
// ONE walk. A bracket bye (a completed match with an empty side) contributes its
// start time but zero duration, mirroring assignBracketMatchSlots.
// An end past midnight is not wrapped: it reads >= 1440. Returns an empty/nil map
// if the competition has no slots.
func (s *Store) CourtWindows(compID string, minutes SlotMinutes) (map[string]CourtWindow, error) {
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

	type slotRecord struct {
		startMins int
		length    int
	}
	courtSlots := make(map[string][]slotRecord)
	minStart := make(map[string]int)
	maxEnd := make(map[string]int)
	observe := func(court, scheduledAt string, isKnockout, isBye bool) {
		court = strings.TrimSpace(court)
		if court == "" {
			return
		}
		startMins, ok := ParseClockMinutes(scheduledAt)
		if !ok {
			return
		}
		length := 0
		if !isBye && minutes != nil {
			length = minutes(comp, isKnockout)
		}
		endMins := startMins + length
		if cur, exists := minStart[court]; !exists || startMins < cur {
			minStart[court] = startMins
		}
		if cur, exists := maxEnd[court]; !exists || endMins > cur {
			maxEnd[court] = endMins
		}
		courtSlots[court] = append(courtSlots[court], slotRecord{startMins: startMins, length: length})
	}

	for i := range poolMatches {
		observe(poolMatches[i].Court, poolMatches[i].ScheduledAt, false, false)
	}
	for _, bm := range BracketMatchesInDrawOrder(bracket) {
		isBye := bm.Status == MatchStatusCompleted && (strings.TrimSpace(bm.SideA) == "" || strings.TrimSpace(bm.SideB) == "")
		observe(bm.Court, bm.ScheduledAt, true, isBye)
	}

	if len(minStart) == 0 {
		return nil, nil
	}
	windows := make(map[string]CourtWindow, len(minStart))
	for court := range minStart {
		slots := courtSlots[court]
		slices.SortStableFunc(slots, func(a, b slotRecord) int {
			return a.startMins - b.startMins
		})
		lengths := make([]int, len(slots))
		for i, s := range slots {
			lengths[i] = s.length
		}
		windows[court] = CourtWindow{
			Start:       minStart[court],
			End:         maxEnd[court],
			SlotLengths: lengths,
		}
	}
	return windows, nil
}

// CourtEnds calculates the planned end ("HH:MM") of compID's last slot on each
// court it holds matches on. It is a view over CourtWindows.
// An end past midnight is not wrapped: it reads "24:10" (no clock time), so a
// reader can tell it from a real one. Returns an empty/nil map if the
// competition has no slots or is not drawn.
func (s *Store) CourtEnds(compID string, minutes SlotMinutes) (map[string]string, error) {
	windows, err := s.CourtWindows(compID, minutes)
	if err != nil || len(windows) == 0 {
		return nil, err
	}
	res := make(map[string]string, len(windows))
	for court, w := range windows {
		res[court] = FormatClockHHMM(w.End)
	}
	return res, nil
}

// courtEndMinutes is CourtEnds in minutes since midnight, unclamped: a plan
// that runs past midnight reads 24*60 or more, which no "HH:MM" clock holds.
// It is a view over CourtWindows.
func (s *Store) courtEndMinutes(compID string, minutes SlotMinutes) (map[string]int, error) {
	windows, err := s.CourtWindows(compID, minutes)
	if err != nil || len(windows) == 0 {
		return nil, err
	}
	res := make(map[string]int, len(windows))
	for court, w := range windows {
		res[court] = w.End
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
// Courts nobody floors are absent from the returned map. A floor at or past
// midnight is a *CourtFloorPastMidnightError (ErrCourtFloorPastMidnight).
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
	floorBy := make(map[string]*Competition)

	for _, id := range ids {
		if id == comp.ID {
			continue
		}
		other, err := s.LoadCompetition(id)
		if err != nil {
			log.Printf("state: court floor %s: load competition: %v", id, err)
			continue
		}
		// A competition still in setup holds no draw; matches left by a failed
		// draw attempt do not count, as the court queue leaves them out too.
		if other == nil || other.Status == CompStatusSetup {
			continue
		}
		if !SameDate(comp.Date, other.Date) {
			continue
		}
		otherStartMins, ok := ParseClockMinutes(other.StartTime)
		if !ok || otherStartMins > compStartMins {
			continue
		}

		ends, err := s.courtEndMinutes(id, minutes)
		if err != nil {
			log.Printf("state: court floor %s: court ends: %v", id, err)
			continue
		}

		for court := range courtSet {
			endMins, ok := ends[court]
			if !ok {
				continue
			}
			if cur, exists := floorMins[court]; !exists || endMins > cur {
				floorMins[court] = endMins
				floorBy[court] = other
			}
		}
	}

	if len(floorMins) == 0 {
		return nil, nil
	}
	// A floor at or past midnight has no "HH:MM" the schedule could hold
	// (scheduledAt is a bare clock time): refuse, never a wrapped or clamped
	// time that would put this competition on top of the other one.
	for _, court := range slices.Sorted(maps.Keys(floorMins)) {
		if floorMins[court] >= 24*60 {
			by := floorBy[court]
			return nil, &CourtFloorPastMidnightError{Court: court, CompetitionID: by.ID, CompetitionName: by.Name}
		}
	}

	res := make(map[string]string, len(floorMins))
	for court, mins := range floorMins {
		res[court] = FormatClockHHMM(mins)
	}
	return res, nil
}

// ParseClockMinutes parses a "HH:MM" clock string into minutes-since-midnight:
// outer whitespace trimmed, a 1 or 2 digit hour, a colon and 2 minute digits,
// digits only (no sign, no inner space). The JS mirror is queueStartRank
// (court_queue.jsx), pinned with it by testdata/court_queue.json. ok is false
// for empty or malformed input, so callers skip an unplaceable slot rather
// than treating it as midnight.
func ParseClockMinutes(s string) (int, bool) {
	s = strings.TrimSpace(s)
	colon := strings.IndexByte(s, ':')
	if colon < 1 || colon > 2 || len(s) != colon+3 {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if i != colon && (s[i] < '0' || s[i] > '9') {
			return 0, false
		}
	}
	h := 0
	for i := 0; i < colon; i++ {
		h = h*10 + int(s[i]-'0')
	}
	m := int(s[colon+1]-'0')*10 + int(s[colon+2]-'0')
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// FormatClockHHMM renders minutes-since-midnight as "HH:MM". It does NOT wrap at 24h:
// an end past midnight reads "24:10", never a clamped 23:59 that would mean an
// earlier time than the plan holds.
func FormatClockHHMM(mins int) string {
	if mins < 0 {
		mins = 0
	}
	return fmt.Sprintf("%02d:%02d", mins/60, mins%60)
}

// SameDate is true when both dates are non-empty and equal (after trimming).
// An empty date means the competition isn't placed on a day, so it shares none.
func SameDate(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a != "" && a == b
}
