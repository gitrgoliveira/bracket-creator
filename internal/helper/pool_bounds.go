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

	// Compute the first page index assigned to this court and derive the
	// page's position within that court's block. When numSubtrees is not
	// divisible by numCourts, SubtreeCourtIndex clamps the overflow pages onto
	// the last court, giving that court more pages than pagesPerCourt. Using
	// subtreeIdx % pagesPerCourt would map all overflow pages to position 0,
	// causing them to render the same pool slice. Instead we compute the
	// position relative to the actual first page of this court's block.
	firstPage := courtIdx * pagesPerCourt
	pageWithinCourt := subtreeIdx - firstPage
	// The last court absorbs all remaining pages beyond the even distribution.
	pagesInCourt := pagesPerCourt
	if courtIdx == numCourts-1 {
		pagesInCourt = numSubtrees - firstPage
	}

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
	poolsPerPage := (courtSize + pagesInCourt - 1) / pagesInCourt
	start = courtStart + pageWithinCourt*poolsPerPage
	end = min(start+poolsPerPage, courtEnd)
	return
}
