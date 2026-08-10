package helper

import (
	"fmt"
	"math"
	"sort"
)

// The pool-to-knockout draw (specs/007-ekc-draw, bead bc-draw).
//
// The bracket is built COURT-FIRST: one subtree per shiaijo over that court's
// region occupants, then the region subtrees are combined into the full
// bracket. R3 ("each shiaijo's pools occupy exactly ONE contiguous region of
// the draw, and that region MUST be a subtree") therefore holds by
// construction, which is what lets an Excel tree page be a genuine subtree an
// operator can pick up per court (R8).
//
// The previous construction flattened every pool finisher into one list
// (GenerateFinals), halved it recursively (CreateBalancedTree) and then tried
// to repair placement with a two-node local swap (treeAdjustment /
// ApplyPoolAdjustments). All three are gone: placement is now by construction,
// so there is nothing left to repair. Do NOT reintroduce a fix-up pass -- the
// old one was provably unable to reach the cases it existed for (a 5-occupant
// region byes the wrong occupant under recursive halving) and it was not
// idempotent, so running it over an already-correct tree corrupted the draw.
//
// Rule references below (R1-R9, D1-D7) are to specs/007-ekc-draw/spec.md, which
// is the definition of record.

// KnockoutDraw is a built pool-to-knockout draw: the whole bracket plus the
// per-shiaijo regions it was assembled from.
//
// Regions is indexed by COURT: Regions[i] is the subtree belonging to shiaijo i
// (court label CourtLabel(i)), and every Regions[i] is a node inside Root, not
// a copy. Callers that paginate (RenderKnockoutPages) or derive a match's court
// (engine.buildBracketFromLeaves) read the regions rather than re-deriving them
// from leaf arithmetic, which is what makes the page-to-shiaijo mapping and the
// per-match court assignment exact instead of approximate.
//
// len(Regions) may be SMALLER than the court count the caller asked for: a
// court with no pools of its own would own an empty region, so the draw clamps
// to the pools available (see EffectiveDrawCourts). Treat len(Regions) as the
// authoritative shiaijo count for everything downstream.
type KnockoutDraw struct {
	Root    *Node
	Regions []*Node
}

// NumCourts is the number of shiaijo regions the draw actually has.
func (d *KnockoutDraw) NumCourts() int {
	if d == nil {
		return 0
	}
	return len(d.Regions)
}

// EffectiveDrawCourts clamps a requested shiaijo count to what the pool count
// can actually carry. A court with no home pools has no home 1st places and
// would own an empty region, so the draw never allocates more courts than
// pools. R9 (1 shiaijo or an EVEN number) is preserved by the clamp: stepping
// down to the pool count can produce an odd value, so it steps down once more.
// The CLI applies the same clamp to its --courts flag (cmd/create-pools.go).
func EffectiveDrawCourts(numPools, numCourts int) int {
	if numCourts < 1 {
		numCourts = 1
	}
	if numPools > 0 && numCourts > numPools {
		numCourts = numPools
		if numCourts > 1 && numCourts%2 == 1 {
			numCourts--
		}
	}
	return numCourts
}

// BuildKnockoutDraw builds the court-first knockout draw for a pool phase.
//
// pools are in draw order; poolWinners is the number of qualifiers each pool
// sends up (EffectivePoolWinners); numCourts is the competition's shiaijo
// allocation. Pools are allocated to courts by AssignPoolsToCourts, the same
// contiguous-block allocation the Pool Matches sheet and the pool schedule use,
// so a court's bracket region holds the pools that court actually ran (R3).
//
// Returns nil when there is nothing to draw (no pools, or poolWinners <= 0).
func BuildKnockoutDraw(pools []Pool, poolWinners, numCourts int) *KnockoutDraw {
	if len(pools) == 0 || poolWinners <= 0 {
		return nil
	}
	courts := EffectiveDrawCourts(len(pools), numCourts)
	assignment, err := AssignPoolsToCourts(len(pools), courts)
	if err != nil {
		return nil
	}
	return BuildKnockoutDrawFromAssignment(pools, poolWinners, assignment, courts)
}

// BuildKnockoutDrawFromAssignment is BuildKnockoutDraw with the pool-to-court
// allocation supplied explicitly (poolCourt[p] is pool p's zero-based court).
//
// Production always goes through BuildKnockoutDraw, which derives the
// allocation from AssignPoolsToCourts. This entry point exists so a reference
// draw whose allocation differs from ours can still be replayed against the
// DRAW itself: the 34th EKC Junior Individual Female sheet ran 7 pools as
// 2/1/2/2 across four courts, where AssignPoolsToCourts front-loads the
// remainder and yields 2/2/2/1.
func BuildKnockoutDrawFromAssignment(pools []Pool, poolWinners int, poolCourt []int, numCourts int) *KnockoutDraw {
	if len(pools) == 0 || poolWinners <= 0 || len(poolCourt) != len(pools) {
		return nil
	}
	if numCourts < 1 {
		numCourts = 1
	}

	plan := newDrawPlan(pools, poolCourt, numCourts)
	occupants := plan.route(pools, poolWinners)

	// One subtree per BLOCK. A block is a shiaijo, except on a single-shiaijo
	// competition, where R4(e) splits the court's pools into two half-blocks
	// that act as partner courts so the draw's shape is identical whether an
	// event runs on 1 court or several.
	blockRoots := make([]*Node, plan.numBlocks)
	for b := range blockRoots {
		blockRoots[b] = buildRegion(occupants[b], pools)
	}

	root, courtRegions := plan.combine(blockRoots)
	if root == nil {
		return nil
	}
	return &KnockoutDraw{Root: root, Regions: courtRegions}
}

// NewPlayoffDraw wraps an already-built elimination tree (a standalone playoffs
// bracket: StandardSeeding -> CreateBalancedTree, no pool phase) in the same
// region structure a pool-fed draw carries, by cutting it into numCourts
// genuine subtrees.
//
// R2-R7 do not apply to a playoffs bracket -- its placement is StandardSeeding's
// -- but R8 (pages per shiaijo) and the per-match court derivation do, and both
// read Regions. Cutting rather than rebuilding keeps the seeded placement
// untouched.
func NewPlayoffDraw(root *Node, numCourts int) *KnockoutDraw {
	if root == nil {
		return nil
	}
	if numCourts < 1 {
		numCourts = 1
	}
	return &KnockoutDraw{Root: root, Regions: splitIntoSubtrees(root, numCourts)}
}

// splitIntoSubtrees cuts a tree into at most n genuine subtrees covering every
// leaf, in left-to-right order: start from the root and repeatedly split the
// widest remaining piece. A tree with fewer than n splittable levels yields
// fewer than n pieces, which is honest -- there is no way to hand a court a
// subtree that does not exist.
func splitIntoSubtrees(root *Node, n int) []*Node {
	pieces := []*Node{root}
	for len(pieces) < n {
		widest, widestLeaves := -1, 1
		for i, p := range pieces {
			if p == nil || p.LeafNode || p.Left == nil || p.Right == nil {
				continue
			}
			if l := CountLeaves(p); l > widestLeaves {
				widest, widestLeaves = i, l
			}
		}
		if widest < 0 {
			break
		}
		p := pieces[widest]
		next := make([]*Node, 0, len(pieces)+1)
		next = append(next, pieces[:widest]...)
		next = append(next, p.Left, p.Right)
		next = append(next, pieces[widest+1:]...)
		pieces = next
	}
	return pieces
}

// TreeLeafLabels returns every real leaf label under node, left to right (which
// is top to bottom on a rendered page). Unlike TreeToLeafArray it does NOT pad
// to a power of two: it is the entrant list, not the bracket's slot array.
func TreeLeafLabels(node *Node) []string {
	if node == nil {
		return nil
	}
	if node.LeafNode {
		if node.LeafVal == "" {
			return nil
		}
		return []string{node.LeafVal}
	}
	return append(TreeLeafLabels(node.Left), TreeLeafLabels(node.Right)...)
}

// CountLeaves returns the number of real (non-empty) leaves under node.
func CountLeaves(node *Node) int {
	if node == nil {
		return 0
	}
	if node.LeafNode {
		if node.LeafVal == "" {
			return 0
		}
		return 1
	}
	return CountLeaves(node.Left) + CountLeaves(node.Right)
}

// ---------------------------------------------------------------------------
// Occupants
// ---------------------------------------------------------------------------

// drawOccupant is one qualifier placed in a region: the placeholder label the
// bracket carries ("Pool A-1st"), the pool it came from and its finishing rank.
// A rank-1 occupant is always a HOME occupant of its region (R4a); every other
// rank crossed in under R4b/R4c/D5.
type drawOccupant struct {
	label string
	pool  int
	rank  int
}

// poolLoad is R6 criterion 2's "oversized pool" metric (D1): how many pool
// matches that pool's qualifier plays.
//
// D1 defines it per pool format -- participant count under `full` (round
// robin), generated match count under `partial`. Both orderings are identical:
// a round robin of n generates n(n-1)/2 matches and a partial pool generates
// n-1, and both are strictly increasing in n, so ranking by either metric picks
// out the same oversized pools. Reading the generated count when it is present
// is what D1 asks for ("the count is the thing the rule is actually about"),
// and falling back to the participant count covers the callers that draw before
// the pool matches exist (the engine loads pools from pools.csv, which stores
// players only).
func poolLoad(p Pool) int {
	if len(p.Matches) > 0 {
		return len(p.Matches)
	}
	return len(p.Players)
}

// poolSeedRank is the best (lowest) operator-assigned seed rank in a pool, or
// math.MaxInt when the pool holds no seeded competitor. R6 criterion 1 ranks
// seeded pools' home 1sts first, in operator seed order.
func poolSeedRank(p Pool) int {
	best := math.MaxInt
	for _, pl := range p.Players {
		if pl.Seed > 0 && pl.Seed < best {
			best = pl.Seed
		}
	}
	return best
}

// byePrecedenceLess implements R6's precedence for a region's structural bye,
// as a total order over that region's occupants (lowest = first claim):
//
//  1. home 1st places of SEEDED pools, in operator seed order;
//  2. home 1st places of OVERSIZED pools, descending load (D1), ties by pool
//     order;
//  3. remaining home 1st places, in pool order;
//  4. crossed-in 2nd places by their own pool's precedence, then crossed-in
//     3rds, then any further rank (R7's degradation ladder).
//
// Criteria 2 and 3 collapse into one comparison: within the rank-1 class,
// unseeded pools sort by descending load and then pool order, which puts every
// oversized pool ahead of every minimum-load pool without needing to compute
// the competition minimum. Criterion 1 outranks it because seeding is
// competitive protection and the bye-for-a-bigger-pool rule is only fatigue
// compensation.
func byePrecedenceLess(a, b drawOccupant, pools []Pool) bool {
	ac, bc := byeRankClass(a.rank), byeRankClass(b.rank)
	if ac != bc {
		return ac < bc
	}
	as, bs := poolSeedRank(pools[a.pool]), poolSeedRank(pools[b.pool])
	if as != bs {
		return as < bs
	}
	al, bl := poolLoad(pools[a.pool]), poolLoad(pools[b.pool])
	if al != bl {
		return al > bl
	}
	return a.pool < b.pool
}

// byeRankClass maps a finishing rank onto its R6 class. Every home 1st shares
// class 0 (criteria 1-3 discriminate inside it); crossed-in ranks keep their
// own rank as the class so 2nds outrank 3rds outrank 4ths (R6-4 / R7).
func byeRankClass(rank int) int {
	if rank <= 1 {
		return 0
	}
	return rank
}

// ---------------------------------------------------------------------------
// Region construction (D4)
// ---------------------------------------------------------------------------

// buildRegion lays a shiaijo's occupants out inside its own subtree.
//
// The layout is GREEDY (D4): the round-1 layer holds floor(q/2) real matches
// and, when q is odd, exactly ONE named bye, which goes to the region's
// highest-precedence occupant under R6. Every other empty slot pairs with
// another empty slot into a phantom match that is dropped downstream and never
// printed. Deeper byes fall to whichever slot the phantom pairs leave and are
// taken by match WINNERS, not pools, so R6 does not allocate them.
//
// Concretely, q occupants fill a NextPow2(q) slot array as
//
//	odd  q: [bye, "", occ...]        even q: [occ...]
//
// and the tail is padded with empty slots. The single inserted gap after the
// bye occupant is what makes it the round-1 bye; everything after it pairs up
// consecutively. For the EKC Junior Individual Male court A (q=5) that is
// [P1, "", P2, P3, P4, P5, "", ""]: P1 byes, P2 v P3, P4 v P5, and the round-2
// bye falls to W(P4 v P5) rather than to P1 -- the ladder the sheet shows, and
// the one recursive halving provably cannot produce.
//
// Within the non-bye occupants the order interleaves the RANK groups, so a home
// 1st meets a crossed-in lower finisher in round 1 rather than another home 1st
// (EKC Junior Team Q1: P1#1 v P5#2, P2#1 v P6#2). Groups are split into two
// side blocks first, which is what keeps a pool's own qualifiers out of the
// same quarter when a region IS a half of the draw (see regionSideOfRank).
func buildRegion(occ []drawOccupant, pools []Pool) *Node {
	if len(occ) == 0 {
		return nil
	}
	byRank := map[int][]drawOccupant{}
	ranks := []int{}
	for _, o := range occ {
		if _, seen := byRank[o.rank]; !seen {
			ranks = append(ranks, o.rank)
		}
		byRank[o.rank] = append(byRank[o.rank], o)
	}
	sort.Ints(ranks)

	var bye *drawOccupant
	if len(occ)%2 == 1 {
		best := occ[0]
		for _, o := range occ[1:] {
			if byePrecedenceLess(o, best, pools) {
				best = o
			}
		}
		bye = &best
		// Remove the bye occupant from its rank group BEFORE the interleave, so
		// the remaining occupants pair up as if it had never been in the layer.
		// (EKC Junior Team Q2: with P3#1 taken out for the bye, the leftover
		// home 1st P4#1 heads the match against the crossed-in P7#2, exactly as
		// the sheet prints it.)
		g := byRank[best.rank]
		for i, o := range g {
			if o.label == best.label {
				byRank[best.rank] = append(append([]drawOccupant{}, g[:i]...), g[i+1:]...)
				break
			}
		}
	}

	order := []drawOccupant{}
	for side := 0; side < 2; side++ {
		groups := [][]drawOccupant{}
		for _, r := range ranks {
			if regionSideOfRank(r) != side {
				continue
			}
			if len(byRank[r]) > 0 {
				groups = append(groups, byRank[r])
			}
		}
		order = append(order, interleaveGroups(groups)...)
	}
	separateSamePoolPairs(order, bye)

	slots := make([]string, 0, NextPow2(len(occ)))
	if bye != nil {
		slots = append(slots, bye.label, "")
	}
	for _, o := range order {
		slots = append(slots, o.label)
	}
	for len(slots) < NextPow2(len(occ)) {
		slots = append(slots, "")
	}
	return buildSlotTree(slots)
}

// separateSamePoolPairs repairs the one thing the rank interleave cannot avoid
// on its own: a round-1 pairing between two qualifiers of the SAME pool.
//
// It arises only where a region receives two ranks from one source court and
// the layer is too short for the side split to keep them apart. The smallest
// case is a 2-pool competition at 3 qualifiers, where a region holds one home
// 1st plus the other pool's 2nd AND 3rd, and those two are the only possible
// pairing. R5 ("a pool's qualifiers MUST be separated maximally") outranks R6's
// bye precedence, which the spec states outright: precedence is a preference,
// R3/R4/R5 win.
//
// order (and bye, when there is one) are mutated in place. It first tries to
// trade the offending occupant with a member of another pairing, which costs
// nothing. Only when there is no other pairing to trade with does it fall back
// to swapping with the BYE occupant, which hands the bye to a lower finisher
// against R6's preference. A pairing it cannot break is left alone rather than
// shuffled pointlessly.
func separateSamePoolPairs(order []drawOccupant, bye *drawOccupant) {
	for i := 0; i+1 < len(order); i += 2 {
		if order[i].pool != order[i+1].pool {
			continue
		}
		swapped := false
		for j := 0; j+1 < len(order); j += 2 {
			if j == i {
				continue
			}
			// Trading order[i+1] with order[j+1] fixes this pairing only if it
			// does not create the same problem at j.
			if order[j].pool != order[i+1].pool && order[j+1].pool != order[i].pool {
				order[i+1], order[j+1] = order[j+1], order[i+1]
				swapped = true
				break
			}
		}
		if swapped || bye == nil || bye.pool == order[i].pool {
			continue
		}
		*bye, order[i+1] = order[i+1], *bye
	}
}

// regionSideOfRank splits finishing ranks into the two halves of a region so
// that ranks routed here FROM THE SAME COURT land in different quarters of the
// draw (R5 / D5).
//
// It only ever matters when a region is itself a half of the draw, i.e. on 1 or
// 2 shiaijo, because that is the only shape where a court sends two different
// ranks of the same pool into one region: with two blocks, ranks 1 and 4 arrive
// from the block's own pools and ranks 2 and 3 from its partner's (see
// drawPlan.route). Putting {1,2} on one side and {3,4} on the other therefore
// separates 1 from 4 and 2 from 3 while still pairing a 1st against a 2nd and a
// 3rd against a 4th in round 1. At 4 or more shiaijo each rank arrives from a
// different court, so the split changes the order of the layer but not a single
// round-1 pairing.
//
// The guarantee stops at 4 qualifiers per pool: a draw has four quarters, so
// from the 5th qualifier onward two of a pool's qualifiers must share one
// (D5, pigeonhole).
func regionSideOfRank(rank int) int {
	if rank < 1 {
		rank = 1
	}
	if (rank-1)%4 < 2 {
		return 0
	}
	return 1
}

// interleaveGroups round-robins over the rank groups: group[0][0], group[1][0],
// ..., group[0][1], ... Exhausted groups drop out. With a single group this is
// the identity, which is what makes a 1-qualifier region pair consecutive pools
// (EKC male: P2 v P3, P4 v P5).
func interleaveGroups(groups [][]drawOccupant) []drawOccupant {
	total := 0
	longest := 0
	for _, g := range groups {
		total += len(g)
		if len(g) > longest {
			longest = len(g)
		}
	}
	out := make([]drawOccupant, 0, total)
	for i := 0; i < longest; i++ {
		for _, g := range groups {
			if i < len(g) {
				out = append(out, g[i])
			}
		}
	}
	return out
}

// buildSlotTree turns a power-of-two slot array into a bracket subtree by
// recursive halving, COLLAPSING any half that is entirely empty.
//
// The collapse is what turns padding into structure: a side with no occupants
// contributes no node, so its sibling advances a round instead of playing a
// phantom match. TreeToLeafArray re-pads the result, so the slot array is
// recovered byte for byte on the way back out to the engine.
func buildSlotTree(slots []string) *Node {
	if len(slots) == 0 {
		return nil
	}
	if len(slots) == 1 {
		if slots[0] == "" {
			return nil
		}
		return &Node{LeafNode: true, LeafVal: slots[0], Val: 1}
	}
	mid := len(slots) / 2
	left := buildSlotTree(slots[:mid])
	right := buildSlotTree(slots[mid:])
	return joinNodes(left, right)
}

// joinNodes pairs two subtrees, collapsing to whichever side exists when the
// other is empty.
func joinNodes(left, right *Node) *Node {
	switch {
	case left == nil:
		return right
	case right == nil:
		return left
	}
	return &Node{Left: left, Right: right, Val: left.Val + right.Val}
}

// ---------------------------------------------------------------------------
// Court plan: crossing (R4), halves/quarters (R5, D3, D5) and region depth (D2)
// ---------------------------------------------------------------------------

// drawPlan is the fixed structure of a draw: how pools map onto blocks, how
// blocks pair up and which half and quarter each block sits in. Everything here
// is derived from the pool-to-court allocation alone, before a single qualifier
// is placed, which is what lets the crossing rules refer to quarters that do
// not exist yet.
type drawPlan struct {
	numCourts int
	numBlocks int
	// half is numBlocks/2: blocks [0, half) are the draw's first half and
	// blocks [half, numBlocks) its second, so partner blocks (b and b+half)
	// always sit in OPPOSITE halves. That is what makes R5's "a pool's 1st and
	// 2nd can only meet in the final" a guarantee rather than an attempt.
	half int
	// poolBlock[p] is pool p's block; blockCourt[b] is block b's shiaijo.
	poolBlock  []int
	blockCourt []int
	// quarterOf[b] is the quarter of the draw block b belongs to. With 4 or
	// more blocks a quarter spans whole blocks; with 2 the quarters live INSIDE
	// each region and regionSideOfRank separates them instead, so quarterOf
	// degrades to the block index.
	quarterOf []int
	// halfOrder[h] is the order the half's blocks are combined in (D2).
	halfOrder [2][]int
}

func newDrawPlan(pools []Pool, poolCourt []int, numCourts int) *drawPlan {
	p := &drawPlan{numCourts: numCourts}

	// R4(e): a single-shiaijo competition emulates the two-court structure by
	// splitting its pools into two half-blocks that act as partner courts, so
	// the draw's shape is identical whether an event runs on 1 court or
	// several. Both half-blocks still belong to the one shiaijo, so the
	// competition still prints as one region.
	if numCourts == 1 && len(pools) >= 2 {
		blocks, err := AssignPoolsToCourts(len(pools), 2)
		if err == nil {
			p.numBlocks = 2
			p.poolBlock = blocks
			p.blockCourt = []int{0, 0}
		}
	}
	if p.poolBlock == nil {
		p.numBlocks = numCourts
		p.poolBlock = poolCourt
		p.blockCourt = make([]int, numCourts)
		for i := range p.blockCourt {
			p.blockCourt[i] = i
		}
	}
	p.half = p.numBlocks / 2

	// D2: within a half the blocks are combined in COURT ORDER, which is what
	// the reference draws show. When a half's block count is not a power of two
	// the combination is uneven and the TRAILING slot is the shallow one (its
	// occupants reach the half-final a round early), so the block D2 prefers --
	// fewest pools, then fewest entrants, then earliest court -- is moved
	// there. This never fires on 1, 2 or 4 shiaijo (a half then holds 1 or 2
	// blocks, both powers of two); it is the 6/10/12/14-shiaijo case the spec
	// calls out, plus any half whose block count is odd.
	poolsPerBlock := make([]int, p.numBlocks)
	playersPerBlock := make([]int, p.numBlocks)
	for i := range pools {
		b := p.poolBlock[i]
		if b >= 0 && b < p.numBlocks {
			poolsPerBlock[b]++
			playersPerBlock[b] += len(pools[i].Players)
		}
	}
	for h := 0; h < 2; h++ {
		lo, hi := 0, p.numBlocks
		if p.numBlocks > 1 {
			if h == 0 {
				hi = p.half
			} else {
				lo = p.half
			}
		} else if h == 1 {
			lo, hi = 0, 0
		}
		order := make([]int, 0, hi-lo)
		for b := lo; b < hi; b++ {
			order = append(order, b)
		}
		p.halfOrder[h] = applyRegionDepthPreference(order, poolsPerBlock, playersPerBlock)
	}

	p.quarterOf = make([]int, p.numBlocks)
	for b := range p.quarterOf {
		p.quarterOf[b] = b
	}
	if p.numBlocks >= 4 {
		for h := 0; h < 2; h++ {
			order := p.halfOrder[h]
			slots := NextPow2(len(order))
			for pos, b := range order {
				q := h * 2
				if pos >= slots/2 {
					q++
				}
				p.quarterOf[b] = q
			}
		}
	}
	return p
}

// applyRegionDepthPreference implements D2. The greedy block combination pads
// at the END, so when the count is not a power of two the last position is the
// shallow one; the D2-preferred block is moved there and the rest keep court
// order. A power-of-two count has no shallow slot to allocate, so court order
// stands.
func applyRegionDepthPreference(order []int, poolsPerBlock, playersPerBlock []int) []int {
	if len(order) < 2 || NextPow2(len(order)) == len(order) {
		return order
	}
	best := 0
	for i := 1; i < len(order); i++ {
		a, b := order[i], order[best]
		switch {
		case poolsPerBlock[a] != poolsPerBlock[b]:
			if poolsPerBlock[a] < poolsPerBlock[b] {
				best = i
			}
		case playersPerBlock[a] != playersPerBlock[b]:
			if playersPerBlock[a] < playersPerBlock[b] {
				best = i
			}
		}
	}
	out := make([]int, 0, len(order))
	out = append(out, order[:best]...)
	out = append(out, order[best+1:]...)
	out = append(out, order[best])
	return out
}

// partnerBlock returns the block half the bracket away (R4). With an even block
// count this is an involution and partner blocks sit in opposite halves, which
// is the property R5 leans on. An odd count cannot pair up -- R9 rejects an odd
// shiaijo allocation greater than 1 at every write path -- so this only has to
// stay total, not meaningful, for legacy data.
func (p *drawPlan) partnerBlock(b int) int {
	if p.numBlocks < 2 {
		return 0
	}
	return (b + p.half) % p.numBlocks
}

func (p *drawPlan) halfOf(b int) int {
	if p.numBlocks < 2 || b < p.half {
		return 0
	}
	return 1
}

// route places every qualifier of every pool into a block, returning the
// occupant list per block in pool-then-rank order.
//
// R4a: the 1st place stays in its own court's region.
// R4b: the 2nd place crosses to the PARTNER court's region, which is half the
// bracket away, so a pool's two qualifiers can only meet in the final (R5).
// R4c/D3/D5: from the 3rd place on, qualifiers alternate halves in the pattern
// {1,4} in the 1st's half and {2,3} in the other, and inside the target half
// take the region that keeps them out of a quarter their pool already occupies.
// At exactly four shiaijo that reduces to the fixed rotation D5 tabulates
// (A: A,C,D,B -- and the 3rd-place column is D3's A->D, B->C, C->B, D->A
// involution).
//
// R4f, structure beats preference: nothing here reserves capacity, so a region
// short of home 1sts simply hosts more crossed-in qualifiers and two of them
// may meet in round 1 (the EKC Junior Team draw's Q4 does exactly that).
func (p *drawPlan) route(pools []Pool, poolWinners int) [][]drawOccupant {
	occupants := make([][]drawOccupant, p.numBlocks)
	// Per-pool tallies drive the quarter/region preferences below; the running
	// block occupancy is the balance tiebreak (D3 step 3).
	quarterUse := make([][]int, len(pools))
	blockUse := make([][]int, len(pools))
	for i := range pools {
		quarterUse[i] = make([]int, p.numBlocks)
		blockUse[i] = make([]int, p.numBlocks)
	}

	for pi := range pools {
		for rank := 1; rank <= poolWinners; rank++ {
			b := p.blockForRank(pi, rank, quarterUse[pi], blockUse[pi], occupants)
			occupants[b] = append(occupants[b], drawOccupant{
				label: fmt.Sprintf("%s-%s", pools[pi].PoolName, GetOrdinal(rank)),
				pool:  pi,
				rank:  rank,
			})
			quarterUse[pi][p.quarterOf[b]]++
			blockUse[pi][b]++
		}
	}
	return occupants
}

func (p *drawPlan) blockForRank(pi, rank int, quarterUse, blockUse []int, occupants [][]drawOccupant) int {
	home := p.poolBlock[pi]
	if home < 0 || home >= p.numBlocks {
		home = 0
	}
	if p.numBlocks < 2 {
		return 0
	}
	switch rank {
	case 1:
		return home
	case 2:
		return p.partnerBlock(home)
	}

	// Ranks 1 and 4 belong in the 1st place's half, ranks 2 and 3 in the other;
	// the pattern repeats every four ranks. Keeping the strongest qualifier the
	// only one of its pool in its own half for as long as the structure allows
	// is D3 step 2.
	target := p.halfOf(home)
	if m := (rank - 1) % 4; m == 1 || m == 2 {
		target = 1 - target
	}

	best := -1
	for _, b := range p.halfOrder[target] {
		if best < 0 || betterCrossTarget(b, best, p.quarterOf, quarterUse, blockUse, occupants) {
			best = b
		}
	}
	if best < 0 {
		// A half with no blocks at all (an odd block count degrades this way);
		// fall back to the pool's own region rather than dropping a qualifier.
		return home
	}
	return best
}

// betterCrossTarget reports whether block a is a better landing region than b
// for a crossing qualifier: first the quarter this pool has used least (R5's
// no-two-in-a-quarter), then the region it has used least, then the region with
// the fewest occupants so far (D3 step 3's balance), then the lower court order.
func betterCrossTarget(a, b int, quarterOf, quarterUse, blockUse []int, occupants [][]drawOccupant) bool {
	if qa, qb := quarterUse[quarterOf[a]], quarterUse[quarterOf[b]]; qa != qb {
		return qa < qb
	}
	if ba, bb := blockUse[a], blockUse[b]; ba != bb {
		return ba < bb
	}
	if oa, ob := len(occupants[a]), len(occupants[b]); oa != ob {
		return oa < ob
	}
	return a < b
}

// combine assembles the block subtrees into the full bracket and returns the
// root together with one region per SHIAIJO, indexed by court.
//
// Blocks are combined within a half first (so a half is a genuine subtree), and
// the two halves then meet at the root. On a single-shiaijo competition the two
// R4(e) half-blocks combine into the one region, which is the root.
func (p *drawPlan) combine(blockRoots []*Node) (*Node, []*Node) {
	halves := [2]*Node{}
	for h := 0; h < 2; h++ {
		nodes := make([]*Node, 0, len(p.halfOrder[h]))
		for _, b := range p.halfOrder[h] {
			nodes = append(nodes, blockRoots[b])
		}
		halves[h] = combineNodes(nodes)
	}
	root := joinNodes(halves[0], halves[1])
	if root == nil {
		return nil, nil
	}

	regions := make([]*Node, p.numCourts)
	if p.numCourts == 1 {
		regions[0] = root
		return root, regions
	}
	for b, n := range blockRoots {
		if c := p.blockCourt[b]; c >= 0 && c < p.numCourts {
			regions[c] = n
		}
	}
	return root, regions
}

// combineNodes joins region subtrees by the same greedy, collapse-on-empty
// halving buildSlotTree uses, padding at the END so the trailing block takes
// the shallow slot D2 allocates.
func combineNodes(nodes []*Node) *Node {
	if len(nodes) == 0 {
		return nil
	}
	if len(nodes) == 1 {
		return nodes[0]
	}
	padded := make([]*Node, NextPow2(len(nodes)))
	copy(padded, nodes)
	return combinePadded(padded)
}

func combinePadded(nodes []*Node) *Node {
	if len(nodes) == 1 {
		return nodes[0]
	}
	mid := len(nodes) / 2
	return joinNodes(combinePadded(nodes[:mid]), combinePadded(nodes[mid:]))
}

// ---------------------------------------------------------------------------
// Region spans: the bridge from regions to the pow2 leaf array
// ---------------------------------------------------------------------------

// RegionSpans returns, for each shiaijo region, the [start, end) slice of
// TreeToLeafArray(Root) that region occupies. Regions are contiguous and
// aligned by construction (R3), so a match's court is a lookup rather than an
// estimate: engine.buildBracketFromLeaves resolves each match's first-round
// slot through CourtForLeafSlot instead of dividing the slot count by the court
// count, which was only ever right when every court held the same number of
// pools.
//
// Indexed like Regions, i.e. by court. A court whose region is missing gets a
// zero-width span.
func (d *KnockoutDraw) RegionSpans() [][2]int {
	spans := make([][2]int, d.NumCourts())
	if d == nil || d.Root == nil {
		return spans
	}
	index := make(map[*Node]int, len(d.Regions))
	for i, r := range d.Regions {
		if r != nil {
			index[r] = i
		}
	}
	leafSpanWalk(d.Root, 0, index, spans)
	return spans
}

// leafSpanWalk mirrors TreeToLeafArray's padding exactly (each side is padded
// to NextPow2 of the wider side before concatenation) and records the offset of
// every region root it passes. Returns the padded width of the subtree.
func leafSpanWalk(n *Node, offset int, index map[*Node]int, out [][2]int) int {
	if n == nil {
		return 0
	}
	width := 1
	if !n.LeafNode {
		side := NextPow2(max(leafArrayWidth(n.Left), leafArrayWidth(n.Right)))
		width = 2 * side
	}
	if i, ok := index[n]; ok && i < len(out) {
		out[i] = [2]int{offset, offset + width}
	}
	if !n.LeafNode {
		side := width / 2
		leafSpanWalk(n.Left, offset, index, out)
		leafSpanWalk(n.Right, offset+side, index, out)
	}
	return width
}

// leafArrayWidth is len(TreeToLeafArray(n)) without building the slice.
func leafArrayWidth(n *Node) int {
	if n == nil {
		return 0
	}
	if n.LeafNode {
		return 1
	}
	return 2 * NextPow2(max(leafArrayWidth(n.Left), leafArrayWidth(n.Right)))
}

// CourtForLeafSlot returns the index of the region that owns leaf slot, given
// RegionSpans' output. A slot that falls in an alignment gap between regions is
// attributed to the last region that starts at or before it, and a slot before
// every region to region 0, so the answer is always a real court.
func CourtForLeafSlot(spans [][2]int, slot int) int {
	best, bestStart := 0, -1
	for i, s := range spans {
		if s[1] <= s[0] {
			continue
		}
		if slot >= s[0] && slot < s[1] {
			return i
		}
		if s[0] <= slot && s[0] > bestStart {
			best, bestStart = i, s[0]
		}
	}
	return best
}
