package helper

import "fmt"

// Per-pool qualifier counts (bead bc-qual, phase LP-3a).
//
// BuildKnockoutDraw and BuildKnockoutDrawFromAssignment take ONE poolWinners
// for the whole competition (see draw.go), which cannot express the 34th EKC
// 2026 Ladies/Men Individual sheets: every pool sends its 1st, but a handful
// of OVERSIZED (4-person) pools also send a 2nd, and that 2nd does not follow
// R4's team-event partner-court crossing (A<->C, B<->D) -- it crosses to the
// SAME-HALF NEIGHBOUR court instead (operator ruling 2: B->A, C->D, witnessed
// on both 2026 sheets; the symmetric involution A->B, D->C is EXTRAPOLATED,
// no sheet shows a court A or D oversized pool). BuildKnockoutDrawPerPool is
// the entry point that expresses this without touching BuildKnockoutDraw's
// signature or any of its callers (cmd/, engine/): every existing call site
// keeps using the uniform builder untouched.
//
// Scope is deliberately narrow, matching what the two 2026 sheets evidence
// and nothing more (fixtures win; see draw_ekc_2026_individual_test.go):
//
//   - defaultWinners is 1 for both real sheets (R4d: "at one qualifier
//     nothing crosses" already holds for every pool's home 1st); a pool
//     mapped to defaultWinners+1 sends exactly ONE extra qualifier, which
//     crosses. A pool mapped to anything else (more than one extra) is out of
//     scope and fails the whole build rather than guessing a placement no
//     sheet corroborates.
//   - At most ONE crossed-in occupant lands on any one court. Two oversized
//     neighbours crossing into the same destination is unevidenced.
//   - The crossing map is the fixed 4-court adjacency (court^1: 0<->1,
//     2<->3), which is what "same-half neighbour" means at the only court
//     count either sheet uses. A different court count is out of scope.
//
// Where it IS in scope, the pipeline reuses as much of the uniform machinery
// as the shape allows rather than re-deriving it: pool-to-court allocation is
// still AssignPoolsToCourts, a court's home-only block (no crossed occupant,
// e.g. Men court B, whose oversized pool 22 keeps its own winner home while
// its 2nd leaves) still goes through buildBlock unchanged (operator ruling 4:
// a pool sending an extra qualifier earns that qualifier no priority for its
// OWN winner), and the block-to-root assembly (halves, D2, the semifinal and
// final wiring) still goes through drawPlan.combine -- at defaultWinners=1,
// planBlocks already returns one block per court with no subdivision (R4d),
// so block index and court index coincide and combine's ordinary half/quarter
// logic is exactly what both 2026 sheets print (semis on the inner courts,
// final on the first half's inner court). Only the two things per-pool
// changes -- which court a pool's extra qualifier lands on, and how a
// crossed-hosting court's OWN block is laid out -- are new code, in this
// file.

// BuildKnockoutDrawPerPool builds a court-first knockout draw where most
// pools send defaultWinners qualifiers but a handful send one extra, per
// overrides.
//
// overrides maps a pool's zero-based index (matching pools' own order, the
// same index poolCourt uses everywhere else in this package) to that pool's
// qualifier count. A pool absent from overrides, or mapped to <= 0, sends
// defaultWinners. Values other than defaultWinners+1 are out of scope (see
// the file-level comment) and fail the whole build.
//
// Returns nil when there is nothing to draw, or when the requested shape
// falls outside LP-3a's scope (fixture win: callers outside that scope should
// fall back to BuildKnockoutDraw rather than receive a guessed layout).
func BuildKnockoutDrawPerPool(pools []Pool, defaultWinners int, overrides map[int]int, numCourts int) *KnockoutDraw {
	if len(pools) == 0 || defaultWinners <= 0 {
		return nil
	}
	courts := EffectiveDrawCourts(len(pools), numCourts)
	poolCourt, err := AssignPoolsToCourts(len(pools), courts)
	if err != nil {
		return nil
	}
	return buildPerPoolDraw(pools, defaultWinners, overrides, poolCourt, courts)
}

// BuildKnockoutDrawPerPoolFromAssignment is BuildKnockoutDrawPerPool with the
// pool-to-court allocation supplied explicitly, mirroring
// BuildKnockoutDrawFromAssignment's reason for existing: AssignPoolsToCourts
// front-loads its remainder (e.g. 34 pools over 4 courts gives 9/9/8/8), but
// the 34th EKC Ladies Individual sheet runs a SYMMETRIC 9/8/8/9, so replaying
// that reference draw against the draw itself needs the real allocation fed
// in rather than derived.
func BuildKnockoutDrawPerPoolFromAssignment(pools []Pool, defaultWinners int, overrides map[int]int, poolCourt []int, numCourts int) *KnockoutDraw {
	if len(pools) == 0 || defaultWinners <= 0 || len(poolCourt) != len(pools) {
		return nil
	}
	return buildPerPoolDraw(pools, defaultWinners, overrides, poolCourt, clampCourts(numCourts))
}

// perPoolWinners resolves pool pi's qualifier count from overrides, falling
// back to defaultWinners exactly as the file-level comment states.
func perPoolWinners(overrides map[int]int, pi, defaultWinners int) int {
	if overrides != nil {
		if w, ok := overrides[pi]; ok && w > 0 {
			return w
		}
	}
	return defaultWinners
}

// crossNeighbourCourt is the LP-3a crossing map (operator ruling 2): a
// court's same-half neighbour under the fixed 4-court adjacency (0<->1,
// 2<->3). B->A and C->D are witnessed on both 2026 sheets; the involution
// (A->B, D->C) is its unwitnessed symmetric extrapolation, needed only so the
// map is total. Returns -1 when numCourts is not even (no neighbour to pair
// with), which the caller treats as out of scope.
func crossNeighbourCourt(court, numCourts int) int {
	if numCourts <= 0 || numCourts%2 != 0 {
		return -1
	}
	return court ^ 1
}

// buildPerPoolDraw is BuildKnockoutDrawPerPool with the pool-to-court
// allocation supplied explicitly, split out so it can be unit tested against
// a hand-fed allocation the way BuildKnockoutDrawFromAssignment is.
func buildPerPoolDraw(pools []Pool, defaultWinners int, overrides map[int]int, poolCourt []int, numCourts int) *KnockoutDraw {
	if numCourts < 1 || len(poolCourt) != len(pools) {
		return nil
	}

	// Every pool's home ranks (1..defaultWinners) go straight to its own
	// court (R4a/R4d); at most one further "extra" rank (defaultWinners+1)
	// is tracked per pool and routed below.
	homeByCourt := make([][]drawOccupant, numCourts)
	extraPool := make([]int, 0, 4) // pool indices sending the extra qualifier
	for pi, p := range pools {
		c := poolCourt[pi]
		if c < 0 || c >= numCourts {
			c = 0
		}
		winners := perPoolWinners(overrides, pi, defaultWinners)
		if winners > defaultWinners+1 {
			// More than one extra qualifier: no sheet evidences the further
			// crossing this would need (R4c/D3's rotation ladder is a
			// different rule for a different draw shape). Refuse rather than
			// guess.
			return nil
		}
		for rank := 1; rank <= defaultWinners; rank++ {
			homeByCourt[c] = append(homeByCourt[c], drawOccupant{
				label: fmt.Sprintf("%s-%s", p.PoolName, GetOrdinal(rank)),
				pool:  pi,
				rank:  rank,
			})
		}
		if winners == defaultWinners+1 {
			extraPool = append(extraPool, pi)
		}
	}

	crossedByCourt := make([]*drawOccupant, numCourts)
	for _, pi := range extraPool {
		home := poolCourt[pi]
		if home < 0 || home >= numCourts {
			home = 0
		}
		dest := crossNeighbourCourt(home, numCourts)
		if dest < 0 || dest >= numCourts {
			return nil // no same-half neighbour to cross to: out of scope
		}
		if crossedByCourt[dest] != nil {
			// Two oversized pools crossing into the same destination court:
			// unevidenced, and buildPerPoolCourtBlock only has room for one.
			return nil
		}
		o := drawOccupant{
			label: fmt.Sprintf("%s-%s", pools[pi].PoolName, GetOrdinal(defaultWinners+1)),
			pool:  pi,
			rank:  defaultWinners + 1,
		}
		crossedByCourt[dest] = &o
	}

	regions := make([]*Node, numCourts)
	for c := 0; c < numCourts; c++ {
		regions[c] = buildPerPoolCourtBlock(homeByCourt[c], crossedByCourt[c], pools, c, numCourts)
		if regions[c] == nil && (len(homeByCourt[c]) > 0 || crossedByCourt[c] != nil) {
			return nil // a non-empty court failed to build: fail loud, not partially
		}
	}

	// At defaultWinners, planBlocks(len(pools), defaultWinners, numCourts)
	// must return one block per court (R4d: nothing crosses at 1 qualifier,
	// so there is nothing for planBlocks to subdivide towards). That is what
	// makes block index and court index coincide, which is what lets
	// plan.combine assemble OUR per-court regions directly instead of
	// per-pool needing its own halves/quarters/semifinal logic.
	plan := newDrawPlan(pools, poolCourt, defaultWinners, numCourts)
	if plan.numBlocks != numCourts {
		return nil
	}
	root, courtRegions := plan.combine(regions)
	if root == nil {
		return nil
	}
	return &KnockoutDraw{
		Root:      root,
		Regions:   courtRegions,
		blocks:    regions,
		poolCourt: append([]int(nil), poolCourt...),
	}
}

// buildPerPoolCourtBlock builds one court's block: home occupants alone go
// through buildBlock completely unchanged (so a pool sending an extra
// qualifier elsewhere never disturbs another court's own layout -- Men court
// B, which hosts oversized pool 22's own winner but never its crossed-out
// 2nd, must stay on uniformBigBlockSlots exactly as LP-2 left it); a court
// with a crossed-in occupant goes through the LP-3a mixed-rank layout below.
func buildPerPoolCourtBlock(home []drawOccupant, crossed *drawOccupant, pools []Pool, court, numCourts int) *Node {
	if crossed == nil {
		return buildBlock(home, pools, false)
	}
	if len(home) == 0 {
		return nil // a lone crossed occupant with no home pools: unevidenced
	}
	slots := crossedBigBlockSlots(home, *crossed, crossedHalfIsTop(court, numCourts), pools)
	if slots == nil {
		return nil
	}
	return BuildSlotTree(slots)
}

// crossedHalfIsTop reports whether the INBOARD half of a crossed-hosting
// court's block -- the half nearer the draw's centre line, which is where
// rule 3 (bc-qual) seats the crossed occupant -- is the block's TOP internal
// half (uniformBigBlockSlots' own top/bottom split) rather than its bottom.
//
// Witnessed at the two ends of a 4-court draw only: court A (index 0, the
// FIRST court) is bottom-inboard, court D (the LAST court) is top-inboard,
// and all four 2026 observations plus both 2025 junior ones agree (file
// comment, draw_ekc_2026_individual_test.go). Generalised here by the same
// "nearer the centre" reading -- a court in the first half of the row points
// its inboard side towards the later courts (down/bottom), a court in the
// second half points it towards the earlier courts (up/top) -- which is an
// EXTRAPOLATION for any court besides the two ends of a 4-court draw; B and C
// never call this in the evidenced shape because they are crossing SOURCES,
// never destinations.
func crossedHalfIsTop(court, numCourts int) bool {
	return court >= numCourts/2
}

// crossedBigBlockSlots is uniformBigBlockSlots' LP-3a counterpart: it lays
// out a big block (per bigBlockHalfRoles' range) that holds one court's home
// 1sts PLUS exactly one occupant crossed in from a neighbouring court's
// oversized pool.
//
// Home occupants are sorted and split top/bottom exactly as
// uniformBigBlockSlots does -- sortBySeedThenPoolOrder, then floor(total/2)
// top / ceil(total/2) bottom, where total includes the crossed occupant, so
// a crossed-hosting block reaches the SAME top/bottom split a home-only block
// of the same total size would (Ladies court A: 9 home + 1 crossed = 10,
// split 5/5, identical to the 2025 sheet's pure 10-occupant court A). The
// crossed occupant is then spliced into whichever half crossedHalfIsTop
// names, via crossedHalfSlots; the other half is plain
// uniformBigBlockSlots-style bigBlockHalfSlots, untouched.
func crossedBigBlockSlots(homes []drawOccupant, crossed drawOccupant, crossedHalfTop bool, pools []Pool) []string {
	sorted := sortBySeedThenPoolOrder(homes, pools)
	total := len(sorted) + 1
	topCount, bottomCount := total/2, total-total/2

	var topHomeCount, bottomHomeCount int
	if crossedHalfTop {
		topHomeCount, bottomHomeCount = topCount-1, bottomCount
	} else {
		topHomeCount, bottomHomeCount = topCount, bottomCount-1
	}
	if topHomeCount < 0 || bottomHomeCount < 0 || topHomeCount+bottomHomeCount != len(sorted) {
		return nil
	}
	top, bottom := sorted[:topHomeCount], sorted[topHomeCount:]

	var topSlots, bottomSlots []string
	if crossedHalfTop {
		topSlots = crossedHalfSlots(top, crossed, true)
		bottomSlots = bigBlockHalfSlots(bottom, false)
	} else {
		topSlots = bigBlockHalfSlots(top, true)
		bottomSlots = crossedHalfSlots(bottom, crossed, false)
	}
	if topSlots == nil || bottomSlots == nil {
		return nil
	}
	return append(topSlots, bottomSlots...)
}

// crossedHalfSlots is bigBlockHalfSlots' LP-3a counterpart for a half that
// receives ONE crossed-in occupant alongside its home 1sts (rule 3, bc-qual:
// the crossed 2nd always takes a round-1 FIGHTING slot, never a bye, never a
// leaf-leaf riser).
//
// Built on bigBlockHalfRoles rather than a second hand-transcribed table,
// because every one of the four evidenced insertions reduces to the same
// move: take the round-1 MATCH pair nearest this half's own OUTER edge (index
// 0 for the top half, the last index for the bottom half -- exactly where
// bigBlockHalfRoles' own named byes already sit), and give the crossed
// occupant that pair's HIGHER-index (Aka) slot, the home occupant the lower.
// Verified against all four cells by hand first (h=5 top: Ladies court D,
// P28 v P19-2nd, sharing its half with P29's inboard bye; h=5 bottom: Ladies
// court A, P7 v P16-2nd; h=6 top: Men court D, P36 v P25-2nd, adjacent to
// P35's top-edge bye; h=7 bottom: Men court A, P11 v P22-2nd, adjacent to
// P12's bottom-edge bye) before being written as this loop.
//
// h=5 has only ONE round-1 match regardless of top/bottom, so both its cells
// are witnessed but never exercise the "which pair" choice at all (the file
// comment on draw_ekc_2026_individual_test.go notes the Ladies sheets "offer
// only one bout so they cannot discriminate" the rule). h=6 is witnessed only
// as a top half (Men court D) and h=7 only as a bottom half (Men court A);
// their mirror orientations (h=6 bottom, h=7 top) are consequently
// EXTRAPOLATED from bigBlockHalfRoles' own mirroring, not independently
// verified by any sheet.
func crossedHalfSlots(homes []drawOccupant, crossed drawOccupant, top bool) []string {
	h := len(homes) + 1
	roles := bigBlockHalfRoles(h, top)
	if roles == nil {
		return nil
	}

	var pairs [][2]int
	for i := 0; i+1 < len(roles); i += 2 {
		if roles[i] >= 0 && roles[i+1] >= 0 {
			pairs = append(pairs, [2]int{roles[i], roles[i+1]})
		}
	}
	if len(pairs) == 0 {
		return nil // no round-1 fighting slot in this half at all (e.g. h=4)
	}
	chosen := pairs[0]
	if !top {
		chosen = pairs[len(pairs)-1]
	}
	// The crossed occupant always takes the pair's HIGHER occupant-index (the
	// Aka/second slot, all four observed cases): only hi is needed downstream.
	hi := chosen[1]
	if chosen[0] > hi {
		hi = chosen[0]
	}

	labels := make([]string, h)
	next := 0
	for i := range labels {
		if i == hi {
			labels[i] = crossed.label
			continue
		}
		if next >= len(homes) {
			return nil
		}
		labels[i] = homes[next].label
		next++
	}

	out := make([]string, len(roles))
	for i, r := range roles {
		if r >= 0 {
			out[i] = labels[r]
		}
	}
	return out
}
