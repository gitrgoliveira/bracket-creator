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
// per-comp READ lock (loadCached takes it, exactly as the public Load*
// methods do). The caller MUST NOT already hold the write lock for any
// competition that this method scans, that would deadlock the
// non-reentrant RWMutex. On the StartMatchTx path the caller holds
// compID_X's write lock, so skipCompID must be set to compID_X; that
// competition is checked by the caller via StoreTx instead.
//
// The scan reads the CACHED values without deep-copying them (the no-copy
// technique MatchStatusByID established, mp-gmcg review R9): it needs three
// strings per match — Status, Court, ID — and returns only two of them by
// value, so nothing interior escapes and the cached tree is never mutated.
// This matters because the caller runs it inside the tournament-global
// courtCheckMu (CheckCrossCompCourtBusy), once per competition in the
// tournament, on every finalizing score write: copyMatchResults/copyBracket
// would clone every bout log in the tournament to read a status field, while
// holding the mutex that serializes score writes across all courts.
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
	data, err := s.loadCached(compID, "pool-matches.csv", parsePoolMatchesFile)
	if err != nil {
		return nil, err
	}
	matches, _ := data.([]MatchResult)
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
	data, err := s.loadCached(compID, "bracket.json", parseBracketFile)
	if err != nil {
		return nil, err
	}
	bracket, _ := data.(*Bracket)
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
