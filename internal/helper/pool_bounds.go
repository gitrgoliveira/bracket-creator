package helper

// PoolBoundsForSubtree returns the [start, end) slice into the pool list for
// the given subtree page. After ReorderPoolsForCourts the pool list is laid
// out in contiguous per-court blocks; this function respects those boundaries
// so that no tree page ever references pools from more than one court.
// The same AssignPoolsToCourts logic used by PrintPoolMatches drives both
// views, keeping the Pool Draw and Tree sheets consistent.
//
// numPools is the total number of pools, numCourts is the number of Shiaijo,
// numSubtrees is the total number of tree pages, and subtreeIdx is the
// zero-based index of the page being rendered.
func PoolBoundsForSubtree(numPools, numCourts, numSubtrees, subtreeIdx int) (start, end int) {
	if numCourts < 1 || numSubtrees < 1 {
		return 0, 0
	}
	pagesPerCourt := numSubtrees / numCourts
	if pagesPerCourt < 1 {
		pagesPerCourt = 1
	}
	courtIdx := SubtreeCourtIndex(numSubtrees, numCourts, subtreeIdx)
	pageWithinCourt := subtreeIdx % pagesPerCourt

	// Derive court block boundaries from the same assignment used by Pool Matches.
	assignments, _ := AssignPoolsToCourts(numPools, numCourts)
	courtStart, courtEnd := -1, 0
	for i, c := range assignments {
		if c == courtIdx {
			if courtStart < 0 {
				courtStart = i
			}
			courtEnd = i + 1
		}
	}
	if courtStart < 0 {
		return 0, 0
	}

	courtSize := courtEnd - courtStart
	poolsPerPage := (courtSize + pagesPerCourt - 1) / pagesPerCourt
	start = courtStart + pageWithinCourt*poolsPerPage
	end = min(start+poolsPerPage, courtEnd)
	return
}
