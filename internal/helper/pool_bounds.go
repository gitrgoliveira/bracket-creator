package helper

// PoolBoundsForSubtree returns the [start, end) slice into the pool list whose
// rosters a tree page may overlay: the whole pool block of the shiaijo that
// page belongs to. The pool list is laid out in contiguous per-court blocks
// (AssignPoolsToCourts, the same allocation PrintPoolMatches and the draw's
// regions use), so a page never offers pools from more than one court.
//
// numPools is the total number of pools, numCourts is the number of Shiaijo,
// numSubtrees is the total number of tree pages, and subtreeIdx is the
// zero-based index of the page being rendered.
//
// It used to SPLIT a court's block across that court's pages, giving page 1 the
// first half of the court's pools and page 2 the second. That split had nothing
// to back it: a page is a child subtree of the region, and which of the court's
// pools have a qualifier on which child is a property of the draw, not of the
// pool index. RenderTreePages narrows this range to the pools actually printed
// on the page, which is the only correspondence that exists. Two branches went
// with the split: "the last court absorbs the remainder", unreachable now that
// the page count is an exact multiple of the court count (R8), and the
// more-pages-than-pools clamp, which only existed because the split could
// produce an inverted range.
func PoolBoundsForSubtree(numPools, numCourts, numSubtrees, subtreeIdx int) (start, end int) {
	if numCourts < 1 || numSubtrees < 1 {
		return 0, 0
	}

	// --single-tree prints the whole draw on one page, which is the one case
	// where a page covers MORE than one court. It then offers every pool rather
	// than court A's alone (TreePageTitle names the whole range to match).
	if numSubtrees < numCourts {
		return 0, numPools
	}

	courtIdx := SubtreeCourtIndex(numSubtrees, numCourts, subtreeIdx)

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
	return courtStart, courtEnd
}

// PageRosterPools narrows a page's candidate pools (PoolBoundsForSubtree) to
// the ones that actually have a qualifier printed on that page, preserving pool
// order.
//
// With one page per shiaijo this is the identity: every home pool's winner is
// in its own court's region (R4a), so every candidate is present. It only bites
// on a court printed across 2 or 4 pages, where a page carries one child
// subtree of the region and therefore only some of the court's pools. Without
// it such a page overlays a roster for competitors who are not on it, which is
// the same defect - a page claiming pools its bracket does not contain - that
// the court-first draw exists to remove.
func PageRosterPools(pools []Pool, page *Node) []Pool {
	if page == nil || len(pools) == 0 {
		return nil
	}
	present := map[string]bool{}
	for _, l := range TreeLeafLabels(page) {
		if name, _ := splitPoolNameAndRank(l); name != "" {
			present[name] = true
		}
	}
	out := make([]Pool, 0, len(pools))
	for _, p := range pools {
		if present[p.PoolName] {
			out = append(out, p)
		}
	}
	return out
}
