package state

import "sort"

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
// Lock discipline: each competition's data is loaded under its own
// per-comp READ lock (via the public Load* methods). The caller MUST NOT
// already hold the write lock for any competition that this method
// scans — that would deadlock the non-reentrant RWMutex. On the
// StartMatchTx path the caller holds compID_X's write lock, so
// skipCompID must be set to compID_X; that competition is checked by
// the caller via StoreTx instead.
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
		if occ := runningOnCourtInPoolMatches(s, compID, court); occ != nil {
			return occ, nil
		}
		if occ, err := runningOnCourtInBracket(s, compID, court); err == nil && occ != nil {
			return occ, nil
		}
	}
	return nil, nil
}

func runningOnCourtInPoolMatches(s *Store, compID, court string) *CourtOccupancy {
	matches, err := s.LoadPoolMatches(compID)
	if err != nil {
		return nil
	}
	for _, m := range matches {
		if m.Status == MatchStatusRunning && m.Court == court {
			return &CourtOccupancy{CompID: compID, MatchID: m.ID}
		}
	}
	return nil
}

func runningOnCourtInBracket(s *Store, compID, court string) (*CourtOccupancy, error) {
	bracket, err := s.LoadBracket(compID)
	if err != nil || bracket == nil {
		return nil, err
	}
	for _, round := range bracket.Rounds {
		for _, bm := range round {
			if bm.Status == MatchStatusRunning && bm.Court == court {
				return &CourtOccupancy{CompID: compID, MatchID: bm.ID}, nil
			}
		}
	}
	return nil, nil
}

// DeriveQueuePositions assigns a 1-indexed queue position to each
// scheduled match per court. Live (running) and completed matches
// receive 0.
//
// Ordering: within each court, positions are assigned in
// (status priority, scheduledAt, original index) order — the same
// basis used by ScheduleViewer (viewer.jsx) and the client-side SSE
// recompute (_orderByCourtKey in patch.jsx) — so "Next up / N before
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
			ai := entries[i].m.ScheduledAt
			if ai == "" {
				ai = "99:99"
			}
			aj := entries[j].m.ScheduledAt
			if aj == "" {
				aj = "99:99"
			}
			if ai != aj {
				return ai < aj
			}
			return entries[i].idx < entries[j].idx
		})
		counter := 0
		for _, e := range entries {
			if e.m.Status == MatchStatusScheduled {
				counter++
				positions[e.idx] = counter
			}
		}
	}
	return positions
}
