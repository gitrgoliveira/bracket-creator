package helper

import (
	"fmt"
	"slices"
)

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
// map is total.
//
// LP-3d adds the ONE court count the sheets never use but clubs constantly
// run: a single shiaijo. There is no other court to cross to, so the extra
// qualifier stays in the only block there is and the separation becomes a
// HALF one -- the crossed 2nd is seated in the opposite half from its own
// pool's winner, which is the same INVARIANT "Fit the knockout" holds for a
// drafted 2nd at one shiaijo (BuildKnockoutDrawFillBracket), though by its
// own split policy rather than this one; see buildCrossedBlockHalved. Rule
// 3's purpose (the two qualifiers of one pool must not meet before they have
// to) is what survives; "neighbouring court" is the multi-court expression of
// it, not the rule itself.
//
// Nothing else needs adding, because a COMPETITION's shiaijo allocation is
// always a power of two -- 1, 2, 4, 8 or 16, enforced by
// ValidateShiaijoCount (R9) at every entry point and clamped by
// EffectiveDrawCourts. A venue may well have 3 or 5 shiaijo, but it gives a
// single competition 1, 2 or 4 of them and never all 3, so court^1 is always
// a real court here and an odd count above one is not a shape this map has
// to answer for. Passing one anyway returns an out-of-range index, which the
// caller refuses rather than papering over with an invented neighbour.
func crossNeighbourCourt(court, numCourts int) int {
	if numCourts <= 0 || court < 0 || court >= numCourts {
		return -1
	}
	if numCourts == 1 {
		return 0 // self: the separation is by half, see buildCrossedBlockHalved
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

	// A court may receive MORE than one crossed occupant (LP-3d): with few
	// courts, several oversized pools share a home court and therefore share
	// a destination. The sheets only ever show one, so the evidenced layout
	// path below still takes exactly one; the general fallback handles the
	// rest rather than refusing the field outright.
	crossedByCourt := make([][]drawOccupant, numCourts)
	for _, pi := range extraPool {
		home := poolCourt[pi]
		if home < 0 || home >= numCourts {
			home = 0
		}
		dest := crossNeighbourCourt(home, numCourts)
		if dest < 0 || dest >= numCourts {
			return nil
		}
		crossedByCourt[dest] = append(crossedByCourt[dest], drawOccupant{
			label: fmt.Sprintf("%s-%s", pools[pi].PoolName, GetOrdinal(defaultWinners+1)),
			pool:  pi,
			rank:  defaultWinners + 1,
		})
	}

	regions := make([]*Node, numCourts)
	for c := 0; c < numCourts; c++ {
		regions[c] = buildPerPoolCourtBlock(homeByCourt[c], crossedByCourt[c], pools, c, numCourts)
		if regions[c] == nil && (len(homeByCourt[c]) > 0 || len(crossedByCourt[c]) > 0) {
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
// with a crossed-in occupant goes through the LP-3a mixed-rank layout below,
// falling back to LP-3b's small-block layout when the destination is too
// small for that (crossedBigBlockSlots' own domain is total >= 9).
func buildPerPoolCourtBlock(home []drawOccupant, crossed []drawOccupant, pools []Pool, court, numCourts int) *Node {
	if len(crossed) == 0 {
		return buildBlock(home, pools, false)
	}
	// Ordered candidate layouts, most-evidenced first, each returning nil for
	// a shape it does not cover. Whichever answers first is CHECKED against
	// rule 3 before it is returned, so the rule holds for every crossed
	// seating this package produces -- including the role tables transcribed
	// from the sheets, whose rule-3 compliance used to be asserted in prose
	// only. Returning nil (no candidate satisfied it) is the documented
	// contract for an out-of-scope shape: fail loudly rather than seat an
	// extra qualifier the rules say cannot be seated that way.
	for _, candidate := range crossedBlockLayouts(home, crossed, pools, court, numCourts) {
		if block := candidate(); block != nil && crossedFightInRoundOne(block, crossed) {
			return block
		}
	}
	return nil
}

// blockLayout lays a set of occupants out as one block. It is what makes the
// general crossed paths below share their SHAPE (flat, or halved for a
// self-crossing block) while differing only in how each resulting group of
// occupants is turned into a tree.
type blockLayout func(occ []drawOccupant, pools []Pool) *Node

// crossedBlockLayouts is the ordered chain buildPerPoolCourtBlock walks for a
// block that seats at least one crossed occupant.
//
//  1. The EVIDENCED sheet layouts, unchanged: one crossed occupant, at least
//     one home pool, and a block big enough for the role tables. Every sheet
//     replay still prints exactly what it printed before LP-3d.
//  2. The package's ORDINARY block layout (buildBlock), which is what a
//     home-only block of the same size would get -- the R6(c) template and
//     uniformBigBlockSlots included. LP-3d originally jumped straight to
//     layOutBlock here, which quietly denied club-sized crossed blocks the
//     evidenced templates their home-only siblings receive; whether a
//     template's own bye placement is acceptable for a crossed occupant is
//     now decided by rule 3 rather than by never asking.
//  3. The same shape laid out under rule 3 directly: the crossed occupants
//     are excluded from bye selection, so a block whose ordinary layout would
//     have byed one still gets seated instead of refused.
func crossedBlockLayouts(home, crossed []drawOccupant, pools []Pool, court, numCourts int) []func() *Node {
	ordinary := func(occ []drawOccupant, pools []Pool) *Node { return buildBlock(occ, pools, false) }
	ruleThree := func(occ []drawOccupant, pools []Pool) *Node {
		return layOutBlock(occ, pools, func(o drawOccupant) bool { return !occupantIsCrossed(o, crossed) })
	}
	return []func() *Node{
		func() *Node { return buildEvidencedCrossedBlock(home, crossed, pools, court, numCourts) },
		func() *Node { return buildGeneralCrossedBlock(home, crossed, pools, ordinary) },
		func() *Node { return buildGeneralCrossedBlock(home, crossed, pools, ruleThree) },
	}
}

// buildEvidencedCrossedBlock is the pair of layouts transcribed from the EKC
// sheets (LP-3a), which between them cover one crossed occupant seated among
// enough home 1sts to fill their role tables. Returns nil for anything else.
func buildEvidencedCrossedBlock(home, crossed []drawOccupant, pools []Pool, court, numCourts int) *Node {
	if len(crossed) != 1 || len(home) == 0 {
		return nil
	}
	top := crossedHalfIsTop(court, numCourts)
	if slots := crossedBigBlockSlots(home, crossed[0], top, pools); slots != nil {
		return BuildSlotTree(slots)
	}
	return buildSmallCrossedCourtBlock(home, crossed[0], top, pools)
}

// buildGeneralCrossedBlock lays out the crossed-hosting blocks the sheet
// layouts do not cover: a destination court holding only one or two pools of
// its own, a court receiving MORE than one crossed occupant, and the
// single-shiaijo draw where the crossed 2nd has no other court to go to and
// stays in its own block.
//
// It invents no layout of its own -- lay does the work -- and only decides
// the block's SHAPE:
//
//   - When this block also holds a crossed occupant's OWN pool winner (the
//     single-shiaijo case, where the pool's 1st and 2nd are necessarily in
//     the same block), the two must be seated in opposite halves so they can
//     only meet in the final. That is buildCrossedBlockHalved.
//   - Otherwise every crossed occupant came from another court and its own
//     winner is elsewhere, so the block lays out flat.
//
// It takes no court index because crossedHalfIsTop's inboard-half convention
// does not reach here: that is a detail read off the big sheet blocks, which
// buildEvidencedCrossedBlock still owns. The blocks this function lays out
// are smaller than anything a sheet shows, so there is no inboard observation
// to honour -- only rule 3, which its caller checks outright.
func buildGeneralCrossedBlock(home, crossed []drawOccupant, pools []Pool, lay blockLayout) *Node {
	if len(home)+len(crossed) < 2 {
		return nil // a lone occupant has nobody to fight
	}
	if blockHoldsOwnPoolWinner(home, crossed) {
		return buildCrossedBlockHalved(home, crossed, pools, lay)
	}
	return lay(slices.Concat(home, crossed), pools)
}

// buildCrossedBlockHalved seats a block whose crossed occupants share it with
// their own pool's winner -- the single-shiaijo draw, where there is no other
// court to cross to. The pool's two qualifiers go in opposite halves, which is
// the separation "crossing" buys on a multi-court draw.
//
// (BuildKnockoutDrawFillBracket reaches the same INVARIANT for a drafted 2nd
// at one shiaijo, but by a different route: it splits the POOL set by index
// parity and runs its ordinary two-court pipeline. The invariant is shared;
// the split policy is not, and neither is evidenced over the other.)
//
// Home winners are snaked across the halves in precedence order (strongest to
// the top, next to the bottom, and so on) so the two halves stay balanced and
// the strongest pools are spread rather than stacked. Each crossed occupant
// then goes to the half that does NOT hold its own winner; one that has no
// winner here (a genuine cross-court arrival sharing a block with a
// self-crossing one) goes to the lighter half. Querying the halves as they
// grow is safe because a pool sends at most one extra qualifier, so a crossed
// occupant already placed can never match a later one's pool.
func buildCrossedBlockHalved(home, crossed []drawOccupant, pools []Pool, lay blockLayout) *Node {
	var top, bottom []drawOccupant
	for i, o := range sortBySeedThenPoolOrder(home, pools) {
		if i%2 == 0 {
			top = append(top, o)
		} else {
			bottom = append(bottom, o)
		}
	}
	for _, c := range crossed {
		switch {
		case holdsPool(top, c.pool):
			bottom = append(bottom, c)
		case holdsPool(bottom, c.pool):
			top = append(top, c)
		case len(top) <= len(bottom):
			top = append(top, c)
		default:
			bottom = append(bottom, c)
		}
	}

	topNode, bottomNode := lay(top, pools), lay(bottom, pools)
	if topNode == nil || bottomNode == nil {
		return nil
	}
	return joinNodes(topNode, bottomNode)
}

// crossedFightInRoundOne is rule 3 as a post-condition: every crossed
// occupant must appear in a round-1 bout facing a named opponent. A byed
// crossed occupant never appears in round 1 at all, and one paired into a
// riser faces a winner slot rather than a name, so both fail here.
func crossedFightInRoundOne(block *Node, crossed []drawOccupant) bool {
	rounds := BuildEliminationMatchRounds(block)
	if len(rounds) == 0 {
		return false
	}
	fights := map[string]bool{}
	for _, m := range rounds[0] {
		left, right := blockLeafLabel(m.Left), blockLeafLabel(m.Right)
		if left == "" || right == "" {
			continue // a bye or a riser: neither is a round-1 fight
		}
		fights[left], fights[right] = true, true
	}
	for _, c := range crossed {
		if !fights[c.label] {
			return false
		}
	}
	return true
}

func blockLeafLabel(n *Node) string {
	if n == nil || !n.LeafNode {
		return ""
	}
	return n.LeafVal
}

func occupantIsCrossed(o drawOccupant, crossed []drawOccupant) bool {
	return slices.ContainsFunc(crossed, func(c drawOccupant) bool { return c.label == o.label })
}

func blockHoldsOwnPoolWinner(home, crossed []drawOccupant) bool {
	return slices.ContainsFunc(crossed, func(c drawOccupant) bool { return holdsPool(home, c.pool) })
}

func holdsPool(occ []drawOccupant, pool int) bool {
	return slices.ContainsFunc(occ, func(o drawOccupant) bool { return o.pool == pool })
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
//
// The placement rule itself (pairs[0]-for-top/pairs[last]-for-bottom, crossed
// takes the higher slot) is factored out to spliceCrossedIntoRoles so LP-3b's
// crossedPortionSlots (below) shares it rather than re-deriving it at the
// smaller, quarter-scale role table.
func crossedHalfSlots(homes []drawOccupant, crossed drawOccupant, top bool) []string {
	return spliceCrossedIntoRoles(bigBlockHalfRoles(len(homes)+1, top), homes, crossed, top)
}

// spliceCrossedIntoRoles seats crossed into whichever role table it is
// handed -- bigBlockHalfRoles (crossedHalfSlots, 9-16 occupant blocks) or
// smallCrossedPortionRoles (crossedPortionSlots, LP-3b, <=8 occupant blocks)
// -- by the SAME rule either scale uses: find the round-1 MATCH pairs (both
// slots real), take the one nearest this half/portion's own OUTER edge
// (pairs[0] for top, pairs[len-1] for bottom -- exactly where the role
// table's own named byes already sit at the finer scale too), and give
// crossed that pair's HIGHER occupant-index slot. See crossedHalfSlots'
// comment for where this was verified.
func spliceCrossedIntoRoles(roles []int, homes []drawOccupant, crossed drawOccupant, top bool) []string {
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
		return nil // no round-1 fighting slot in this half/portion at all (e.g. h=4)
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

	labels := make([]string, len(homes)+1)
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

// ---------------------------------------------------------------------------
// Small crossed-hosting blocks (bead bc-qual, phase LP-3b)
// ---------------------------------------------------------------------------
//
// crossedBigBlockSlots (above) needs bigBlockHalfRoles' h in [4,8] on BOTH
// the crossed-hosting half AND its sibling at once -- it splits a block into
// two halves of up to 8 occupants each, so it only has room to operate from
// total = 9 up. A destination court whose TOTAL (home 1sts plus the one
// crossed-in qualifier) is 8 or fewer therefore returned nil and the whole
// larger-pools draw refused to build, even though the 33rd EKC 2025 Junior
// Individual Male sheet (jim2025draw-04.png) witnesses exactly this shape:
// 18 pools, courts A(1-5) B(6-9) C(10-13) D(14-18), oversized pools 8
// (court B) and 11 (court C) whose 2nds cross B->A and C->D, giving 6-total
// destination blocks (5 home + 1 crossed) on both A and D.
//
// buildSmallCrossedCourtBlock is buildPerPoolCourtBlock's fallback for this
// size range: it splits the total into a QUARTER-sized top portion
// (floor(total/2)) and bottom portion (ceil(total/2)) -- one level finer
// than crossedBigBlockSlots' half split, since a total this small fits in a
// SINGLE 8-slot region rather than two -- and builds each portion
// independently before joining them. The portion WITHOUT the crossed
// occupant is unchanged buildBlock (it already handles any home-only count
// correctly; the pure-home court blocks B and C on the same sheet already
// went through it unmodified). The portion WITH the crossed occupant goes
// through crossedPortionSlots, this size range's counterpart of
// crossedHalfSlots, built on smallCrossedPortionRoles rather than
// bigBlockHalfRoles.
//
// Evidenced ONLY at total=6: court A splits 3 (home-only, byes P1) + 3 (2
// home + 1 crossed, byes P4), court D splits 3 (2 home + 1 crossed, byes
// P14) + 3 (home-only, byes P16). Both give the crossed-hosting portion
// exactly 3 occupants -- smallCrossedPortionRoles' floor. Nothing smaller
// is evidenced, and nothing smaller COULD satisfy rule 3 (bc-qual): seating
// the crossed occupant in a fighting slot while a DIFFERENT, home occupant
// takes that portion's bye needs a bye-home, a fighting-home and the
// crossed occupant all at once -- three real occupants minimum. A
// 2-occupant portion (1 home + 1 crossed) has no home left over for the
// bye once the crossed occupant's fighting partner is assigned, so it would
// have to either bye the crossed occupant outright (rule 3 forbids this) or,
// by the same "half-full quadrant" shape bigBlockHalfRoles' own h=4 case
// uses, pair it into a LEAF-LEAF riser that skips round 1 entirely (rule 3
// forbids this too). A 1-occupant portion (the crossed occupant alone, no
// home at all) has no fighting partner full stop. Both are refused via
// smallCrossedPortionRoles rather than guessing at an unevidenced shape.
// m=4 (a fully-packed portion, no bye slot to misassign) is the one
// extension past direct evidence: nothing remains ambiguous once a portion
// is completely full of real occupants, so it reuses spliceCrossedIntoRoles'
// own pairs[0]-for-top/pairs[last]-for-bottom/higher-slot convention rather
// than leaving a structurally trivial case unbuilt.
//
// The floor in TOTAL terms (this function's whole domain is total <= 8; an
// out-of-range total is refused up front) is asymmetric: total >= 5 when the
// crossed occupant lands in the BOTTOM portion, which always gets the
// larger ceil(total/2) share; total >= 6 when it lands in the smaller,
// floor(total/2) TOP share. crossedHalfIsTop (shared with
// crossedBigBlockSlots) names which. Both evidenced courts sit at the
// tighter bound with an even 3/3 split, so the asymmetry itself falls
// straight out of the size arithmetic rather than being independently
// chosen or tested.
//
// ONE cell diverges from the sheet on aka/shiro order, the same class of
// divergence draw_ekc_2026_individual_test.go's F9 comment already accepts:
// court A's crossed-hosting quarter prints "P8-2nd v P5-1st" (crossed
// FIRST) on the sheet, but court D's structurally identical quarter prints
// "P15-1st v P11-2nd" (crossed SECOND -- spliceCrossedIntoRoles' own
// "always the higher slot" rule). The two cells of this ONE sheet disagree
// with each other, so a single rule cannot match both; production keeps the
// established, majority convention (crossed always the higher/later slot,
// matching all four 2026 cells plus this sheet's own court D) and accepts
// the one-cell flip against court A. Side order carries no seeding meaning
// on these sheets.
func buildSmallCrossedCourtBlock(homes []drawOccupant, crossed drawOccupant, crossedTop bool, pools []Pool) *Node {
	sorted := sortBySeedThenPoolOrder(homes, pools)
	total := len(sorted) + 1
	if total > 8 {
		return nil // crossedBigBlockSlots' domain, not this function's
	}
	topCount, bottomCount := total/2, total-total/2

	var topHomeCount, bottomHomeCount int
	if crossedTop {
		topHomeCount, bottomHomeCount = topCount-1, bottomCount
	} else {
		topHomeCount, bottomHomeCount = topCount, bottomCount-1
	}
	if topHomeCount < 0 || bottomHomeCount < 0 || topHomeCount+bottomHomeCount != len(sorted) {
		return nil
	}
	top, bottom := sorted[:topHomeCount], sorted[topHomeCount:]

	var topNode, bottomNode *Node
	if crossedTop {
		slots := crossedPortionSlots(top, crossed, true)
		if slots == nil {
			return nil
		}
		topNode = BuildSlotTree(slots)
		bottomNode = buildBlock(bottom, pools, false)
	} else {
		topNode = buildBlock(top, pools, false)
		slots := crossedPortionSlots(bottom, crossed, false)
		if slots == nil {
			return nil
		}
		bottomNode = BuildSlotTree(slots)
	}
	return joinNodes(topNode, bottomNode)
}

// crossedPortionSlots is crossedHalfSlots' LP-3b counterpart at quarter
// scale: it seats one crossed occupant among a QUARTER's home 1sts (up to 4
// occupants, not up to 8) via smallCrossedPortionRoles and
// spliceCrossedIntoRoles.
func crossedPortionSlots(homes []drawOccupant, crossed drawOccupant, top bool) []string {
	return spliceCrossedIntoRoles(smallCrossedPortionRoles(len(homes)+1), homes, crossed, top)
}

// smallCrossedPortionRoles is bigBlockHalfRoles' LP-3b counterpart, scaled
// down from an 8-slot half to a 4-slot QUARTER: the per-occupant-count (m)
// shape of one portion of a buildSmallCrossedCourtBlock block, as a 4-slot
// array of occupant indices (-1 for an empty slot), occupants numbered
// 0..m-1 in sortBySeedThenPoolOrder order.
//
//   - m=3: one named bye plus one round-1 match -- the ONLY evidenced shape
//     (both courts of the 33rd EKC 2025 Junior Individual Male sheet), and
//     the SAME array regardless of top/bottom: both evidenced cells (court
//     A's bottom-hosted quarter, court D's top-hosted quarter) place the bye
//     first and the match second, so there is nothing to mirror -- unlike
//     bigBlockHalfRoles' h=5, no leaf-leaf component exists at this size for
//     an orientation flip to act on.
//   - m=4: two round-1 matches, no bye at all (trivial; not exercised by the
//     sheet, whose only destination-block total is 6, but a fully-packed
//     quarter has no bye to misplace and needs no table entry beyond this).
//
// m=1 and m=2 return nil: see buildSmallCrossedCourtBlock's doc comment for
// why neither can seat a crossed occupant without breaking rule 3.
func smallCrossedPortionRoles(m int) []int {
	switch m {
	case 3:
		return []int{0, -1, 1, 2}
	case 4:
		return []int{0, 1, 2, 3}
	}
	return nil
}
