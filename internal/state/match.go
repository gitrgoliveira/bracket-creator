package state

import (
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// CourtOccupancy carries the first running match found on a given court.
type CourtOccupancy struct {
	CompID  string
	MatchID string
}

// RunningMatchOnCourt scans every competition for a match with the given
// court that is currently in MatchStatusRunning. It returns the first
// occupant found, or nil if the court is free. An empty court string is
// never considered busy (unassigned matches don't block anything).
//
// The scan reads the CACHED values without deep-copying them (cachedPoolMatches
// / cachedBracket, mp-gmcg review R9): it needs three strings per match —
// Status, Court, ID — and returns only two of them by value, so nothing
// interior escapes and the cached tree is never mutated. copyMatchResults/
// copyBracket would clone every bout log in the tournament to read one status
// field, and this runs once per competition on every finalizing score write.
//
// Lock discipline: each competition's data is loaded under its own per-comp
// READ lock (loadCached takes it). The caller MUST NOT already hold the write
// lock for any competition this scans (the RWMutex is non-reentrant); on the
// StartMatchTx path skipCompID is the caller's own write-locked competition,
// which it checks via StoreTx instead.
func (s *Store) RunningMatchOnCourt(court, skipCompID string) (*CourtOccupancy, error) {
	if court == "" {
		return nil, nil
	}
	ids, err := s.ListCompetitions()
	if err != nil {
		return nil, err
	}
	for _, compID := range ids {
		if compID == skipCompID {
			continue
		}
		occ, err := runningOnCourtInPoolMatches(s, compID, court)
		if err != nil {
			return nil, err
		}
		if occ != nil {
			return occ, nil
		}
		occ, err = runningOnCourtInBracket(s, compID, court)
		if err != nil {
			return nil, err
		}
		if occ != nil {
			return occ, nil
		}
	}
	return nil, nil
}

// runningOnCourtInPoolMatches scans compID's cached pool matches (no deep
// copy, see RunningMatchOnCourt) for a running match on court.
func runningOnCourtInPoolMatches(s *Store, compID, court string) (*CourtOccupancy, error) {
	matches, err := s.cachedPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	for i := range matches {
		if matches[i].Status == MatchStatusRunning && matches[i].Court == court {
			return &CourtOccupancy{CompID: compID, MatchID: matches[i].ID}, nil
		}
	}
	return nil, nil
}

// runningOnCourtInBracket is runningOnCourtInPoolMatches over the bracket:
// rounds first, then the bronze (3rd-place) SIBLING of Rounds, which a
// rounds-only loop never reaches.
func runningOnCourtInBracket(s *Store, compID, court string) (*CourtOccupancy, error) {
	bracket, err := s.cachedBracket(compID)
	if err != nil {
		return nil, err
	}
	if bracket == nil {
		return nil, nil
	}
	for _, round := range bracket.Rounds {
		for i := range round {
			if round[i].Status == MatchStatusRunning && round[i].Court == court {
				return &CourtOccupancy{CompID: compID, MatchID: round[i].ID}, nil
			}
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil && bm.Status == MatchStatusRunning && bm.Court == court {
		return &CourtOccupancy{CompID: compID, MatchID: bm.ID}, nil
	}
	return nil, nil
}

// QueueSortTime is the time a court queue sorts a match by: its ScheduledAt,
// with an untimed match reading "99:99" so it sorts after every timed one.
func QueueSortTime(scheduledAt string) string {
	if scheduledAt == "" {
		return "99:99"
	}
	return scheduledAt
}

// CanonicalQueueTime parses s as minutes since midnight, accepting only the
// canonical two-digit "HH:MM" form, the one a string comparison orders
// correctly ("9:45" sorts after "10:00").
func CanonicalQueueTime(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' {
		return 0, false
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// CourtQueueBounds returns the earliest and latest scheduled times (ScheduledAt)
// among the waiting matches the given court can play, across all competitions:
// timed in canonical HH:MM (a legacy "9:45" is skipped, since a string
// comparison cannot order it against "10:00"), and, for a knockout match, with
// both competitors known. A "Winner of ..." or "Pool A-1st" side, or a bronze
// match's empty one, is not a queue entry yet (the court console lists such a
// match apart), so a match moved onto the court is never placed behind the
// later round it feeds.
//
// A court whose only such waiting matches carry no time at all answers
// QueueSortTime("") ("99:99") for both bounds, the time its queue sorts them
// by, so a waiting match moved there sinks with them (lastInQueueTime,
// internal/engine). Where any of them is timed, the untimed ones are not a
// bound. Both bounds are empty when no waiting match counted.
//
// A pool bout's own knockout is skipped as well: it follows that competition's
// pool phase on every court (pushKnockoutPastPools, internal/engine), so a pool
// bout placed after it would push it later again on every move.
//
// Lock discipline: cached reads acquire each competition's read lock.
// Because the RWMutex is not reentrant, the caller must NOT hold any
// competition's lock, and so must call this method outside any transaction.
//
// Snapshot: this reads an instantaneous snapshot of tournament state across
// competitions.
//
// Unlike RunningMatchOnCourt, it does not skip a whole competition. When
// moving or rescheduling a match, the other scheduled matches of its
// competition still belong to the court queue, so it skips the match whose
// competition ID matches skipCompID and match ID matches skipMatchID, and,
// when that match is one of the competition's pool bouts, the competition's
// knockout (above); every other match of the competition still counts.
func (s *Store) CourtQueueBounds(court, skipCompID, skipMatchID string) (earliest, latest string, err error) {
	if court == "" {
		return "", "", nil
	}
	ids, err := s.ListCompetitions()
	if err != nil {
		return "", "", err
	}
	acc := queueBoundsAcc{
		court:       court,
		skipCompID:  skipCompID,
		skipMatchID: skipMatchID,
	}
	for _, compID := range ids {
		heldSkipped, err := acc.scanPoolMatches(s, compID)
		if err != nil {
			return "", "", err
		}
		if heldSkipped {
			continue
		}
		if err := acc.scanBracket(s, compID); err != nil {
			return "", "", err
		}
	}
	if acc.latest == "" && acc.untimed {
		return QueueSortTime(""), QueueSortTime(""), nil
	}
	return acc.earliest, acc.latest, nil
}

type queueBoundsAcc struct {
	court       string
	skipCompID  string
	skipMatchID string
	earliest    string
	latest      string
	// untimed: the court holds a waiting match with no ScheduledAt.
	untimed bool
}

func (a *queueBoundsAcc) observe(compID, matchID, court string, status MatchStatus, scheduledAt string) {
	if court != a.court || status != MatchStatusScheduled {
		return
	}
	if compID == a.skipCompID && matchID == a.skipMatchID {
		return
	}
	if scheduledAt == "" {
		a.untimed = true
		return
	}
	if _, ok := CanonicalQueueTime(scheduledAt); !ok {
		return
	}
	if a.earliest == "" || scheduledAt < a.earliest {
		a.earliest = scheduledAt
	}
	if a.latest == "" || scheduledAt > a.latest {
		a.latest = scheduledAt
	}
}

// scanPoolMatches observes compID's pool matches and reports whether they hold
// the skipped match.
func (a *queueBoundsAcc) scanPoolMatches(s *Store, compID string) (heldSkipped bool, err error) {
	matches, err := s.cachedPoolMatches(compID)
	if err != nil {
		return false, err
	}
	for i := range matches {
		heldSkipped = heldSkipped || (compID == a.skipCompID && matches[i].ID == a.skipMatchID)
		a.observe(compID, matches[i].ID, matches[i].Court, matches[i].Status, matches[i].ScheduledAt)
	}
	return heldSkipped, nil
}

func (a *queueBoundsAcc) scanBracket(s *Store, compID string) error {
	bracket, err := s.cachedBracket(compID)
	if err != nil {
		return err
	}
	if bracket == nil {
		return nil
	}
	observe := func(bm *BracketMatch) {
		if bm.hasBothCompetitors() {
			a.observe(compID, bm.ID, bm.Court, bm.Status, bm.ScheduledAt)
		}
	}
	for _, round := range bracket.Rounds {
		for i := range round {
			observe(&round[i])
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil {
		observe(bm)
	}
	return nil
}

// hasBothCompetitors reports whether both sides are resolved competitors: a
// side with an id is one, and so is a named side a repair has not reached yet,
// unless the name is a "Winner of ..." or "Pool A-1st" placeholder (the court
// console's hasBothSides, admin_helpers.jsx).
func (m *BracketMatch) hasBothCompetitors() bool {
	resolved := func(name, id string) bool {
		return id != "" || (name != "" && !helper.IsWinnerOfPlaceholder(name) && !helper.IsPoolFinalistPlaceholder(name))
	}
	return resolved(m.SideA, m.SideAID) && resolved(m.SideB, m.SideBID)
}

// DeriveQueuePositions assigns a 1-indexed queue position to each
// scheduled match per court. Live (running) and completed matches
// receive 0.
//
// Ordering: within each court, positions are assigned in
// (status priority, scheduledAt, original index) order, the same
// basis used by ScheduleViewer (viewer.jsx) and the client-side SSE
// recompute (_orderByCourtKey in patch.jsx), so "Next up / N before
// yours" labels are consistent between server responses and the
// post-SSE client view.
//
// FR-025, R3: positions are recomputed at serve time and on every SSE
// match-state change so viewers see the queue shrink as matches finish.
// The function is pure and side-effect-free; the caller is responsible
// for assigning the returned positions onto the MatchResult slice.
func DeriveQueuePositions(matches []MatchResult) []int {
	positions := make([]int, len(matches))
	if len(matches) == 0 {
		return positions
	}

	type entry struct {
		idx int
		m   MatchResult
	}
	byCourt := make(map[string][]entry)
	for i, m := range matches {
		byCourt[m.Court] = append(byCourt[m.Court], entry{idx: i, m: m})
	}

	statusOrder := func(s MatchStatus) int {
		switch s {
		case MatchStatusRunning:
			return 0
		case MatchStatusScheduled:
			return 1
		default:
			return 2
		}
	}

	for _, entries := range byCourt {
		sort.SliceStable(entries, func(i, j int) bool {
			oi, oj := statusOrder(entries[i].m.Status), statusOrder(entries[j].m.Status)
			if oi != oj {
				return oi < oj
			}
			ai, aj := QueueSortTime(entries[i].m.ScheduledAt), QueueSortTime(entries[j].m.ScheduledAt)
			if ai != aj {
				return ai < aj
			}
			return entries[i].idx < entries[j].idx
		})
		counter := 0
		for _, e := range entries {
			// bc-cse: a scheduled match whose resolved side ids are
			// currently barred (IneligibleSides, stamped by the caller
			// BEFORE this runs -- see mobileapp.annotateEligibility)
			// is not queued: it cannot be started until the operator
			// reinstates the barred side or records the default win, so
			// "Next up" and on-deck alerts must skip it rather than
			// present a match nobody can actually start next.
			if e.m.Status == MatchStatusScheduled && e.m.IneligibleSides == nil {
				counter++
				positions[e.idx] = counter
			}
		}
	}
	return positions
}
