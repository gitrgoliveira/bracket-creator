package state

import "sort"

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
