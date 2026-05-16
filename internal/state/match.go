package state

// DeriveQueuePositions assigns a 1-indexed queue position to each
// scheduled match per court. Live (running) and completed matches
// receive 0. The slice ordering of the input is treated as the queue
// order — typically callers sort by scheduledAt or insertion order
// before calling this.
//
// FR-025, R3: positions are recomputed at serve time and on every SSE
// match-state change so viewers see the queue shrink as matches finish.
// The function is pure and side-effect-free; the caller is responsible
// for assigning the returned positions onto a separate MatchResult slice
// if the wire payload needs them.
func DeriveQueuePositions(matches []MatchResult) []int {
	positions := make([]int, len(matches))
	counters := make(map[string]int)
	for i, m := range matches {
		if m.Status == MatchStatusScheduled {
			counters[m.Court]++
			positions[i] = counters[m.Court]
		}
	}
	return positions
}
