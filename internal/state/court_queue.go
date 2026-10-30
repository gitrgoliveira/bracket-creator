package state

import (
	"math"
	"sort"
	"strings"
)

// The COURT QUEUE (bc-pubq): the ONE owner of the order a shiaijo plays its
// matches in and of every viewer payload's queuePosition. The JS mirror
// (web-mobile/js/court_queue.jsx) and this file are both pinned by one golden
// table, testdata/court_queue.json; change the rule here and in the table,
// never at a call site.
//
// For every court, over every match of every competition on that court:
// 1. Matches running on the court come first (two can be running after a forced start),
//    ordered by the same keys as rule 2.
// 2. Then every waiting (status scheduled) match, ordered by scheduledAt (HH:MM string;
//    a match without a time sorts after the timed ones via QueueSortTime); equal times
//    by the competition's start time parsed as minutes since midnight (Competition.StartTime,
//    blank or unparseable sorts last via QueueStartRank), then competition id (string compare),
//    then draw index (int).
// 3. Completed matches are not in the queue.
// 4. A waiting match with a barred side (IneligibleSides != nil, stamped at request time
//    by mobileapp.annotateEligibility before the queue runs) stays in the ordered queue
//    but takes no position (0).
// 5. queuePosition = 1-based rank among the waiting, non-barred matches of that court
//    queue. Running, barred and completed matches get 0 (omitted on the wire by omitempty).
// 6. Courts are keyed by (competition date, court): competitions on different
//    Competition.Date values never share a queue, even with the same court letter (a
//    multi-day tournament reuses letters). Dates are compared after strings.TrimSpace;
//    two blank dates are the same day. A queue must still order every match, so undated
//    matches share one queue, whereas the floor and clash warning need a real day to compare.
//    The court string is used as stored.

// CourtQueueEntry is one match as the court queue reads it.
type CourtQueueEntry struct {
	CompetitionID        string      `json:"competitionId"`
	CompetitionStartTime string      `json:"competitionStartTime"`
	Date                 string      `json:"date"`
	Court                string      `json:"court"`
	MatchID              string      `json:"matchId"`
	Status               MatchStatus `json:"status"`
	ScheduledAt          string      `json:"scheduledAt"`
	DrawIndex            int         `json:"drawIndex"`
	Barred               bool        `json:"barred"`
}

// CourtKey names one court queue.
type CourtKey struct{ Date, Court string }

// QueueStartRank returns the competition start time as minutes since midnight,
// or math.MaxInt when blank or unparseable so untimed competitions sort last.
func QueueStartRank(startTime string) int {
	if mins, ok := ParseClockMinutes(startTime); ok {
		return mins
	}
	return math.MaxInt
}

// SameQueueDay reports whether two competition dates share court queues: equal
// after trimming, two blank dates included. It is the one owner of rule 6 above;
// state.SameDate differs (two blank dates are NOT a day there) and must not stand
// in for it.
func SameQueueDay(a, b string) bool {
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

// CourtQueues returns each court's queue as indexes into entries, in queue order
// (running first, then waiting; completed left out; barred kept in place).
func CourtQueues(entries []CourtQueueEntry) map[CourtKey][]int {
	byCourt := make(map[CourtKey][]int)
	for i, e := range entries {
		if e.Status != MatchStatusRunning && e.Status != MatchStatusScheduled {
			continue
		}
		// The key's date is trimmed, which is what SameQueueDay compares.
		key := CourtKey{
			Date:  strings.TrimSpace(e.Date),
			Court: e.Court,
		}
		byCourt[key] = append(byCourt[key], i)
	}

	for _, idxs := range byCourt {
		sort.SliceStable(idxs, func(i, j int) bool {
			ei, ej := entries[idxs[i]], entries[idxs[j]]
			ri, rj := ei.Status == MatchStatusRunning, ej.Status == MatchStatusRunning
			if ri != rj {
				return ri
			}
			ti, tj := QueueSortTime(ei.ScheduledAt), QueueSortTime(ej.ScheduledAt)
			if ti != tj {
				return ti < tj
			}
			ci, cj := QueueStartRank(ei.CompetitionStartTime), QueueStartRank(ej.CompetitionStartTime)
			if ci != cj {
				return ci < cj
			}
			if ei.CompetitionID != ej.CompetitionID {
				return ei.CompetitionID < ej.CompetitionID
			}
			if ei.DrawIndex != ej.DrawIndex {
				return ei.DrawIndex < ej.DrawIndex
			}
			return idxs[i] < idxs[j]
		})
	}

	return byCourt
}

// CourtQueuePositions returns, per entry (same index), its queuePosition (0 when none).
func CourtQueuePositions(entries []CourtQueueEntry) []int {
	positions := make([]int, len(entries))
	if len(entries) == 0 {
		return positions
	}
	queues := CourtQueues(entries)
	for _, queue := range queues {
		counter := 0
		for _, idx := range queue {
			e := entries[idx]
			if e.Status == MatchStatusScheduled && !e.Barred {
				counter++
				positions[idx] = counter
			}
		}
	}
	return positions
}

// BracketMatchesInDrawOrder returns pointers to all bracket matches in draw-index
// order: round by round (round 0 first), position order within each round,
// with ThirdPlaceMatch (the bronze, when present) placed immediately before the
// final round's matches.
func BracketMatchesInDrawOrder(bracket *Bracket) []*BracketMatch {
	if bracket == nil {
		return nil
	}
	numMatches := 0
	for _, round := range bracket.Rounds {
		numMatches += len(round)
	}
	if bracket.ThirdPlaceMatch != nil {
		numMatches++
	}
	if numMatches == 0 {
		return nil
	}

	result := make([]*BracketMatch, 0, numMatches)
	if len(bracket.Rounds) == 0 {
		if bracket.ThirdPlaceMatch != nil {
			result = append(result, bracket.ThirdPlaceMatch)
		}
		return result
	}

	finalRound := len(bracket.Rounds) - 1
	for r := 0; r < finalRound; r++ {
		for i := range bracket.Rounds[r] {
			result = append(result, &bracket.Rounds[r][i])
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		result = append(result, bracket.ThirdPlaceMatch)
	}
	for i := range bracket.Rounds[finalRound] {
		result = append(result, &bracket.Rounds[finalRound][i])
	}
	return result
}

// CourtQueueEntriesOf builds the entries of one competition from its pool
// matches and bracket (nil allowed), with the draw index described above.
// Barred is Status == scheduled && IneligibleSides != nil.
func CourtQueueEntriesOf(comp *Competition, poolMatches []MatchResult, bracket *Bracket) []CourtQueueEntry {
	// A competition still in setup holds no draw: matches a failed draw
	// attempt left behind take no place, as the JS courtQueueEntriesOf says.
	if comp != nil && comp.Status == CompStatusSetup {
		return nil
	}
	bracketMatches := BracketMatchesInDrawOrder(bracket)
	total := len(poolMatches) + len(bracketMatches)
	if total == 0 {
		return nil
	}

	base := CourtQueueEntry{}
	if comp != nil {
		base.CompetitionID = comp.ID
		base.CompetitionStartTime = comp.StartTime
		base.Date = comp.Date
	}

	entries := make([]CourtQueueEntry, 0, total)
	add := func(court, id string, status MatchStatus, scheduledAt string, barred bool) {
		e := base
		e.Court, e.MatchID, e.Status, e.ScheduledAt = court, id, status, scheduledAt
		e.DrawIndex = len(entries)
		e.Barred = status == MatchStatusScheduled && barred
		entries = append(entries, e)
	}

	for _, m := range poolMatches {
		add(m.Court, m.ID, m.Status, m.ScheduledAt, m.IneligibleSides != nil)
	}
	for _, bm := range bracketMatches {
		add(bm.Court, bm.ID, bm.Status, bm.ScheduledAt, bm.IneligibleSides != nil)
	}

	return entries
}
