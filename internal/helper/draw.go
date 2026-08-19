package helper

import (
	"fmt"
	"math"
	"sort"
)

// The pool-to-knockout draw (specs/007-ekc-draw, bead bc-draw).
//
// The bracket is built BLOCK-FIRST: one subtree per block over that block's
// occupants, then the block subtrees are combined into halves and the halves
// into the full bracket. A block is a shiaijo's region at four or more shiaijo
// and one of its subdivisions below that (planBlocks), so R3 ("each shiaijo's
// pools occupy exactly ONE contiguous region of the draw, and that region MUST
// be a subtree") holds by construction at every shiaijo count, which is what
// lets an Excel tree page be a genuine subtree an operator can pick up per
// court (R8).
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
// A second fix-up pass was tried and removed for the same reason:
// splitRegionQuarters dealt an already-built region's occupants into two blocks
// after the fact, under a constraint search, and left 22 of 462 swept
// pool-instances with two qualifiers of one pool in one quarter. The cause was
// upstream -- below four shiaijo the plan had only two blocks, so the quarter
// boundary fell INSIDE a block and route could not aim at it. Subdividing the
// pool set (planBlocks) puts the quarters where route can see them. Fix the
// structure, never the output.
//
// Rule references below (R1-R9, D1-D8) are to specs/007-ekc-draw/spec.md, which
// is the definition of record.

// KnockoutDraw is a built pool-to-knockout draw: the whole bracket plus the
// per-shiaijo regions it was assembled from.
//
// Regions is indexed by COURT: Regions[i] is the subtree belonging to shiaijo i
// (court label CourtLabel(i)), and every Regions[i] is a node inside Root, not
// a copy. Callers that paginate (RenderKnockoutPages) or derive a match's court
// (engine.buildBracketFromDraw) read the regions rather than re-deriving them
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

	// blocks is the partition Regions are assembled from, in block order. It
	// equals Regions wherever planBlocks does not subdivide (every count from
	// four shiaijo up, and below that whenever there are too few qualifiers to
	// cut finer); where it does, each region spans two or four of these. It is
	// D4's unit -- a block's layout (greedy, or the R6(c) template) and its
	// named byes belong to the block, not to a printable region -- so the bye
	// arithmetic is checked against this rather than against Regions.
	//
	// This is a TEST SEAM, and the only one on this type: nothing in
	// production reads it, because paging and court derivation are both
	// region-level. It is kept because D4's bye arithmetic is defined per
	// block, so draw_seed_bye_test.go can only check the rule against this
	// partition -- recomputing it in the test would mean duplicating
	// planBlocks and would stop testing what the draw actually built.
	blocks []*Node

	// poolCourt[p] is the zero-based shiaijo pool p was allocated to -- the
	// allocation this draw was actually ASSEMBLED from, kept rather than
	// recomputed. BuildKnockoutDrawFromAssignment exists precisely because a
	// real allocation can differ from AssignPoolsToCourts (the 34th EKC Junior
	// Female sheet ran 7 pools as 2/1/2/2 where the derived answer is 2/2/2/1),
	// so anything asking "which shiaijo is this pool on?" has to read what the
	// draw used rather than re-derive an answer that may not match it.
	//
	// nil for a draw with no pool phase (NewPlayoffDraw).
	poolCourt []int
}

// PoolCourt returns the zero-based shiaijo each pool was drawn onto, indexed by
// pool. Nil when the draw has no pool phase, or when the stored allocation does
// not cover the pools handed in: callers skip the per-shiaijo question rather
// than guess at it.
func (d *KnockoutDraw) PoolCourt(numPools int) []int {
	if d == nil || numPools <= 0 || len(d.poolCourt) != numPools {
		return nil
	}
	return d.poolCourt
}

// NumCourts is the number of shiaijo regions the draw actually has.
//
// This, not the requested court count, is the band count for the Elimination
// Matches sheet. That sheet cannot self-clamp the way the pool sheets do,
// because it takes no pools. On the pool-fed path NumCourts equals the count
// those sheets clamp to; on the PURE PLAYOFFS path (pools empty, so
// EffectiveDrawCourts returns the raw count) NewPlayoffDraw -> splitIntoSubtrees
// can honestly yield FEWER regions than numCourts when the tree has too few
// splittable levels. Using it makes the elimination banding equal the tree-page
// count in BOTH formats, which is why both export paths pass it.
func (d *KnockoutDraw) NumCourts() int {
	if d == nil {
		return 0
	}
	return len(d.Regions)
}

// EffectiveDrawCourts clamps a requested shiaijo count to what the pool count
// can actually carry. A court with no home pools has no home 1st places and
// would own an empty region, so the draw never allocates more courts than
// pools. The CLI applies the same clamp to its --courts flag
// (cmd/create-pools.go).
//
// R9 (1, 2, 4, 8 or 16 shiaijo) is preserved BY the clamp, not merely checked
// after it: stepping down to the pool count can land anywhere, so the step-down
// goes to the largest legal count that fits (LargestShiaijoCountAtMost). It is
// not enough to step down to an EVEN count -- 8 shiaijo clamped onto 7 pools
// would give 6, which R9 rejects because 3 regions in a half cannot merge
// pairwise. 7 pools therefore carry 4 shiaijo.
//
// Only the clamping branch normalises. A requested count that already fits the
// pools is returned untouched, because that value is the caller's allocation
// and has its own validator (ValidateShiaijoCount) at every write path;
// silently "fixing" it here would hide an invalid allocation rather than refuse
// it, and would give an operator a draw on a different number of shiaijo from
// the one they assigned.
//
// The [1, MaxCourts] bound (clampCourts) is the one exception to "returned
// untouched", and it is a bound rather than a normalisation: every slice the
// draw builds below is len numCourts or numBlocks, so an unchecked count is an
// allocation nothing sized.
//
// The step-down is R9's, and applying it to every format is correct rather than
// merely harmless: the only bracketless format that reaches a pool phase is
// league, which runDrawPipeline pins to a SINGLE pool, and at one pool the
// step-down and a plain min() give the same answer at every court count. A
// multi-pool bracketless competition -- the one shape where they would differ --
// cannot be created.
func EffectiveDrawCourts(numPools, numCourts int) int {
	numCourts = clampCourts(numCourts)
	if numPools > 0 && numCourts > numPools {
		numCourts = LargestShiaijoCountAtMost(numPools)
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
	// Not EffectiveDrawCourts: that also steps a count down to the pool count,
	// which would desync from the poolCourt assignment the caller has already
	// built for numCourts.
	numCourts = clampCourts(numCourts)

	plan := newDrawPlan(pools, poolCourt, poolWinners, numCourts)
	occupants := plan.route(pools, poolWinners)

	// One subtree per BLOCK. A block is usually a shiaijo's region; on 1 or 2
	// shiaijo with enough qualifiers to fill them, planBlocks cuts the pool set
	// into half-blocks that act as partner courts, so the block tree -- and with
	// it the draw's shape -- is as far as possible the same object whatever the
	// shiaijo count (R4(e); D8 records where that stops being true).
	blockRoots := make([]*Node, plan.numBlocks)
	for b := range blockRoots {
		blockRoots[b] = buildBlock(occupants[b], pools, plan.mirroredBlock(b))
	}

	root, courtRegions := plan.combine(blockRoots)
	if root == nil {
		return nil
	}
	return &KnockoutDraw{
		Root:      root,
		Regions:   courtRegions,
		blocks:    blockRoots,
		poolCourt: append([]int(nil), poolCourt...),
	}
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
	// Normalize through the slot codec so every playoffs consumer sees ONE
	// geometry. CreateBalancedTree gives a ragged roster a riseless tree whose
	// shallow pairs classify a round late; the skeleton export rebuilds from
	// the bracket's slots and gets the risen tree, which fights those pairs in
	// round 1 as the reference sheets print (Node.risen). Without this, the
	// CLI's printed rounds and the app export's disagree about the same draw.
	// BuildSlotTree(SlotArray(x)) is idempotent on slot-built trees.
	root = BuildSlotTree(SlotArray(root))
	if root == nil {
		return nil
	}
	numCourts = clampCourts(numCourts)
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

// drawOccupant is one qualifier placed in a block: the placeholder label the
// bracket carries ("Pool A-1st"), the pool it came from and its finishing rank.
// A rank-1 occupant is always a HOME occupant of its block (R4a); every other
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

// byePrecedenceLess implements R6's precedence for a block's structural bye,
// as a total order over that block's occupants (lowest = first claim):
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
// Block construction (D4)
// ---------------------------------------------------------------------------

// buildBlock lays one block's occupants out inside its own subtree.
//
// The layout is GREEDY (D4): the round-1 layer holds floor(q/2) real matches
// and, when q is odd, exactly ONE named bye, which goes to the block's
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
// The BLOCK is D4's unit, not the printable region. The two coincide at every
// shiaijo count but one, which is why D4 was first written in terms of a
// region; on a single shiaijo the region spans two half-blocks and each carries
// its own greedy layer, so a 1-court draw has the two named byes a 2-court draw
// of the same pools has rather than one. R5's separation needs nothing here:
// route already sends a pool's 1st and 2nd to different blocks, so a block
// holds at most one of them, and from the 3rd on the blocks run out before the
// quarters do (planBlocks).
//
// Within a block the order interleaves the RANK groups, so a home 1st meets a
// crossed-in lower finisher in round 1 rather than another home 1st (EKC Junior
// Team Q1: P1#1 v P5#2, P2#1 v P6#2).
//
// mirrored is the block's position in its half: false for the half's first
// (outer-top) block, true for its second. It matters only to the R6(c)
// template, which lays the second block out as the first's vertical mirror --
// the shape both Men Team sheets print on shiaijo B and D.
func buildBlock(occ []drawOccupant, pools []Pool, mirrored bool) *Node {
	if len(occ) == 0 {
		return nil
	}
	if slots := templateSlots(occ, pools, mirrored); slots != nil {
		return BuildSlotTree(slots)
	}
	if slots := uniformBigBlockSlots(occ, pools); slots != nil {
		return BuildSlotTree(slots)
	}
	if len(occ) == 1 {
		// A lone occupant IS its block: emit the leaf directly. Building the
		// old [occupant, ""] pair instead would mark a rise, and the rise
		// would read as a real empty slot downstream -- SlotArray doubled
		// every single-pool region of a 4-court draw and shifted each bout
		// one shiaijo over.
		return &Node{LeafNode: true, LeafVal: occ[0].label, Val: 1}
	}
	width := NextPow2(len(occ))
	slots := make([]string, 0, width)
	rest := append([]drawOccupant{}, occ...)

	var bye *drawOccupant
	if len(rest)%2 == 1 {
		best := 0
		for i := 1; i < len(rest); i++ {
			if byePrecedenceLess(rest[i], rest[best], pools) {
				best = i
			}
		}
		b := rest[best]
		bye = &b
		// Remove the bye occupant BEFORE the interleave, so the remaining
		// occupants pair up as if it had never been in the layer. (EKC Junior
		// Team Q2: with P3#1 taken out for the bye, the leftover home 1st P4#1
		// heads the match against the crossed-in P7#2, exactly as the sheet
		// prints it.)
		rest = append(rest[:best:best], rest[best+1:]...)
	}

	order := interleaveByRank(rest)
	separateSamePoolPairs(order, bye, pools)

	if bye != nil {
		slots = append(slots, bye.label, "")
	}
	for _, o := range order {
		slots = append(slots, o.label)
	}
	for len(slots) < width {
		slots = append(slots, "")
	}
	return BuildSlotTree(slots)
}

// templateSlots is R6(c)'s block layout, decoded from all eight Men Team
// blocks of the 33rd (2025) and 34th (2026) EKC draw sheets. A qualifying
// block is TWO sub-blocks, each headed by a home 1st who byes into the
// sub-block final:
//
//	first block of a half:   [h1 BYE | c1 v c2]   [h2 BYE | h3 v c3]
//	second block of a half:  [h1 BYE | c1 v h2]   [h3 BYE | c2 v c3]
//
// h = home 1sts strongest-first (byePrecedenceLess), c = crossed qualifiers
// likewise. The second block is the first's vertical mirror, aka/shiro
// included: its mixed pair lists the crossed qualifier first. Note what the
// mirror does to the heads: position-order fill puts h1 and h3 on the second
// block's heads, so shiaijo B byes P4 and P6 while P5 plays -- exactly the
// sheet, and not a precedence anomaly.
//
// A missing occupant (the 5-occupant blocks of the 2025 sheet) vacates a
// PLAYING slot -- head slots are always filled -- and its would-be opponent
// byes, so such a block prints THREE named byes and ONE round-1 match. A
// crossed bye created this way goes to the WEAKEST crossed: both 2025 vacancy
// byes passed over a seeded pool's 2nd (P11#2 over Spain's P10#2, P6#2 over
// France's P4#2), so this is the sheets' rule, not an inversion slip. The swap
// is positional: the weakest trades slots with whoever held the bye slot,
// which is also what prints 2025 shiaijo D's remaining pair as P5#2 v P4#2.
//
// Scope is the EVIDENCED shapes only: 5 or 6 occupants, 2-3 home 1sts, 2-3
// crossed. Anything else (1-qualifier blocks, which have no crossed by
// definition; rank mixes like 2 homes + 4 crossed at 3 qualifiers; blocks of
// 7+) returns nil HERE -- no sheet constrains those shapes for a MIXED-rank
// block. A same-size block with no crossed at all (every occupant a home
// 1st) falls through instead to uniformBigBlockSlots for 9-16 occupants
// (LP-2), or the greedy layout otherwise; the Junior Individual Male sheets
// pin greedy for the 1-qualifier case.
func templateSlots(occ []drawOccupant, pools []Pool, mirrored bool) []string {
	if len(occ) < 5 || len(occ) > 6 {
		return nil
	}
	var homes, crossed []drawOccupant
	for _, o := range occ {
		if o.rank <= 1 {
			homes = append(homes, o)
		} else {
			crossed = append(crossed, o)
		}
	}
	if len(homes) < 2 || len(homes) > 3 || len(crossed) < 2 || len(crossed) > 3 {
		return nil
	}
	sort.SliceStable(homes, func(i, j int) bool { return byePrecedenceLess(homes[i], homes[j], pools) })
	sort.SliceStable(crossed, func(i, j int) bool { return byePrecedenceLess(crossed[i], crossed[j], pools) })

	// Role positions. Slots 1 and 5 are the head gaps; 0 and 4 the heads.
	hSlots, cSlots := []int{0, 4, 6}, []int{2, 3, 7}
	if mirrored {
		hSlots, cSlots = []int{0, 3, 4}, []int{2, 6, 7}
	}
	slots := make([]string, 8)
	fillRankSlots(slots, homes, hSlots)
	fillRankSlots(slots, crossed, cSlots)

	// A crossed slot whose pair slot is empty is a vacancy-created bye; the
	// weakest crossed claims it (trading slots with the incumbent).
	for _, i := range cSlots {
		if slots[i] == "" || slots[i^1] != "" {
			continue
		}
		weakest := i
		for _, j := range cSlots {
			if slots[j] != "" && slots[j^1] != "" && byePrecedenceLess(occAt(crossed, slots[weakest]), occAt(crossed, slots[j]), pools) {
				weakest = j
			}
		}
		slots[i], slots[weakest] = slots[weakest], slots[i]
	}
	return slots
}

// fillRankSlots places one rank group, strongest first, into its role slots in
// position order. A short group gives up its PLAYING slots from the tail --
// head slots (whose pair is a permanent gap) are always filled, because an
// unheaded sub-block would bye nobody where the sheet byes a home 1st.
func fillRankSlots(slots []string, occ []drawOccupant, positions []int) {
	drop := len(positions) - len(occ)
	keep := make([]int, 0, len(positions))
	for i := len(positions) - 1; i >= 0; i-- {
		p := positions[i]
		if drop > 0 && p != 0 && p != 4 {
			drop--
			continue
		}
		keep = append(keep, p)
	}
	for i, j := 0, len(keep)-1; i < j; i, j = i+1, j-1 {
		keep[i], keep[j] = keep[j], keep[i]
	}
	for i, o := range occ {
		if i < len(keep) {
			slots[keep[i]] = o.label
		}
	}
}

// occAt maps a slot label back to its occupant so the weakest-crossed swap can
// compare precedence; the labels in a block are unique by construction.
func occAt(occ []drawOccupant, label string) drawOccupant {
	for _, o := range occ {
		if o.label == label {
			return o
		}
	}
	return drawOccupant{}
}

// uniformBigBlockSlots is the LP-2 extension of R6(c) to blocks of 9-16
// occupants, decoded from the 33rd EKC 2025 Ladies and Men Individual sheets
// (draw_ekc_2025_individual_test.go): 4-court events whose per-court block
// sizes (10/10/9/9 and 12/12/12/11) are the next size up from the 5-6
// occupant Men Team blocks templateSlots already covers.
//
// Scope is UNIFORM qualifiers only (every occupant a home 1st, poolWinners
// effectively 1): a block containing any crossed-in occupant is out of scope
// and returns nil here, leaving the greedy fallback untouched. A block with
// exactly one crossed-in occupant (an oversized neighbour pool's 2nd, R4
// crossing at per-pool qualifier counts) is crossedBigBlockSlots' job instead
// (draw_perpool.go, bead bc-qual phase LP-3a); this function does not call
// it and is unchanged by its existence, which is what keeps every uniform
// sweep case on the code path this comment and draw_ekc_2025_individual_test.go
// already pin. Blocks of 8 or fewer are also left alone: they fall
// through to the existing template (5-6) or greedy (<=4, 7, 8) paths, and 7
// and 8 do not need a new template at all -- see the note below.
//
// The shape: split q occupants into a TOP half of floor(q/2) and a BOTTOM
// half of ceil(q/2) (the smaller half on top -- sheet-verified at q=9 (4+5)
// and q=11 (5+6); halves are equal at 10 and 12), then lay each half out in
// an 8-slot quadrant pair per bigBlockHalfRoles. This only has slot room to
// operate at q in [9,16] (NextPow2(q)=16, so each half gets its own 8-slot
// half rather than sharing a single 4-slot one): at q = 7 or 8 the total
// width is only 8, so each half gets 4 slots, and the existing GREEDY
// fallback already produces exactly this shape by construction (worked
// through by hand for q=7: bye=highest-precedence occupant, remaining 6
// interleaved -- the width-8 slot array splits into a 3-occupant left
// quarter [bye,"",o1,o2] and a 4-occupant right quarter [o3,o4,o5,o6], the
// same floor(7/2)=3 top / ceil(7/2)=4 bottom split this rule states, and at
// width 4 there is no room for a leaf-leaf riser pair to begin with). So 7
// and 8 are deliberately left on the greedy path rather than routed through
// a second, unverified width-4 template.
func uniformBigBlockSlots(occ []drawOccupant, pools []Pool) []string {
	if len(occ) < 9 || len(occ) > 16 {
		return nil
	}
	for _, o := range occ {
		if o.rank != 1 {
			// Crossed-in qualifier: mixed-rank big blocks are
			// crossedBigBlockSlots' job (draw_perpool.go, LP-3a).
			return nil
		}
	}
	// Deliberately NOT byePrecedenceLess: its load term is R6 criterion 2
	// (oversized pools ahead of pool order), and the 2026 Men Individual
	// sheet contradicts exactly that in a uniform big block -- court B lays
	// pools 13-23 consecutively and the oversized pool 22's winner FIGHTS
	// round 1 while ordinary pools 17/18/23 bye (operator ruling 4 on
	// bc-qual; pinned in draw_ekc_2026_individual_test.go). Big blocks lay
	// consecutively, seeds first (criterion 1 stands; on every decoded sheet
	// the seeded pool is its court's first pool, so this equals plain pool
	// order there).
	sorted := sortBySeedThenPoolOrder(occ, pools)

	top, bottom := sorted[:len(sorted)/2], sorted[len(sorted)/2:]
	slots := make([]string, 0, 16)
	slots = append(slots, bigBlockHalfSlots(top, true)...)
	slots = append(slots, bigBlockHalfSlots(bottom, false)...)
	return slots
}

// sortBySeedThenPoolOrder is the "lay consecutively, seeds first" ordering
// uniformBigBlockSlots uses (operator ruling 4, bc-qual: no criterion-2 load
// priority in a big block), factored out so LP-3a's crossedBigBlockSlots
// (draw_perpool.go) sorts a crossed-hosting block's HOME occupants the exact
// same way rather than restating the comparator.
func sortBySeedThenPoolOrder(occ []drawOccupant, pools []Pool) []drawOccupant {
	sorted := append([]drawOccupant{}, occ...)
	sort.SliceStable(sorted, func(i, j int) bool {
		as, bs := poolSeedRank(pools[sorted[i].pool]), poolSeedRank(pools[sorted[j].pool])
		if as != bs {
			return as < bs
		}
		return sorted[i].pool < sorted[j].pool
	})
	return sorted
}

// bigBlockHalfSlots fills one half's 8-slot quadrant pair from its occupants
// (already in byePrecedenceLess order) via bigBlockHalfRoles.
func bigBlockHalfSlots(half []drawOccupant, top bool) []string {
	roles := bigBlockHalfRoles(len(half), top)
	out := make([]string, len(roles))
	for i, r := range roles {
		if r >= 0 {
			out[i] = half[r].label
		}
	}
	return out
}

// bigBlockHalfRoles is the per-occupant-count (h) shape of one half of a
// uniformBigBlockSlots block, as an 8-slot array of occupant indices (-1 for
// an empty slot), occupants numbered 0..h-1 in byePrecedenceLess order.
//
// Each half is two 4-slot quadrants. A "leaf-leaf" quadrant [x,"",y,""] has
// two occupants who both skip round 1 (their round-1 partner slot is empty)
// and meet EACH OTHER in round 2 -- the round-1-column never shows them. A
// "bye+match" quadrant has one real round-1 match plus one occupant who
// skips round 1 and meets that match's WINNER in round 2. Riser (leaf-leaf)
// quadrants sit at the block's OUTER edge, named byes sit INBOARD (nearer
// the boundary between the top and bottom halves) -- top and bottom are
// therefore mirrors of each other, reflected around that boundary, except at
// h=6 (byes at both quadrant edges, its own mirror) and h=4/h=8 (no bye at
// all, nothing to mirror).
//
//   - h=4: two leaf-leaf pairs, no round-1 match at all (Ladies 2025 courts C
//     and D top sub-blocks, pools 21-24 / 30-33).
//   - h=5: outer quadrant is the leaf-leaf pair, inner quadrant is the
//     match+bye (top half: pools 1-5 and 11-15 of the same sheet; bottom
//     half, mirrored: pools 6-10 and 16-20).
//   - h=6: named bye, match, match, named bye -- byes at both edges, the same
//     array for top or bottom (Men 2025 courts A-C, both sub-blocks each).
//   - h=7: sheet-observed only as a BOTTOM half (Men 2025 court D does not
//     reach this size, so no sheet shows it either; this is carried over
//     from the phase's decoded rule, not independently re-verified here).
//     The TOP-half mirror is a further EXTRAPOLATION with no sheet evidence
//     at all -- flag it as such wherever it is relied on.
//   - h=8: four round-1 matches, no empties at all (trivial; not exercised by
//     either 2025 sheet, whose largest block is 12).
func bigBlockHalfRoles(h int, top bool) []int {
	switch h {
	case 4:
		return []int{0, -1, 1, -1, 2, -1, 3, -1}
	case 5:
		if top {
			return []int{0, -1, 1, -1, 2, 3, 4, -1}
		}
		return []int{0, -1, 1, 2, 3, -1, 4, -1}
	case 6:
		return []int{0, -1, 1, 2, 3, 4, 5, -1}
	case 7:
		if top {
			return []int{0, -1, 1, 2, 3, 4, 5, 6}
		}
		return []int{0, 1, 2, 3, 4, 5, 6, -1}
	case 8:
		return []int{0, 1, 2, 3, 4, 5, 6, 7}
	}
	return nil
}

// interleaveByRank groups a block's occupants by finishing rank and round-robins
// over the groups, lowest rank first.
func interleaveByRank(occ []drawOccupant) []drawOccupant {
	byRank := map[int][]drawOccupant{}
	ranks := []int{}
	for _, o := range occ {
		if _, seen := byRank[o.rank]; !seen {
			ranks = append(ranks, o.rank)
		}
		byRank[o.rank] = append(byRank[o.rank], o)
	}
	sort.Ints(ranks)
	groups := make([][]drawOccupant, 0, len(ranks))
	for _, r := range ranks {
		groups = append(groups, byRank[r])
	}
	return interleaveGroups(groups)
}

// separateSamePoolPairs repairs the one thing the rank interleave cannot avoid
// on its own: a round-1 pairing between two qualifiers of the SAME pool.
//
// It arises only where a block receives two ranks of one pool, which route
// avoids up to 4 qualifiers per pool but cannot beyond that (a draw has four
// blocks, so a 5th qualifier must double up). The 3-pool, 4-qualifier draw is
// the other case: it has only two blocks to route over, so every pool sends two
// qualifiers into each. R5 ("a pool's qualifiers MUST be separated maximally")
// outranks R6's bye precedence, which the spec states outright: precedence is a
// preference, R3/R4/R5 win.
//
// order (and bye, when there is one) are mutated in place. It first tries to
// trade the offending occupant with a member of another pairing, which costs
// nothing. Only when there is no other pairing to trade with does it fall back
// to swapping with the BYE occupant, which hands the bye to a lower finisher
// against R6's preference. A pairing it cannot break is left alone rather than
// shuffled pointlessly.
//
// The fallback promotes the BETTER of the two clashing occupants (R6 order),
// not whichever happens to sit second. Both choices break the pairing equally
// well, so taking the better one is free, and taking the worse one is the
// inversion it looks like: 2 pools at 3 qualifiers gives a block of
// {A-1st, B-2nd, B-3rd}, where swapping the tail byes B's THIRD place while a
// pool winner plays. Promoting B-2nd instead costs A-1st the same bye but keeps
// the rest of R6's order intact. This is the only configuration in the swept
// range where the fallback fires at all.
func separateSamePoolPairs(order []drawOccupant, bye *drawOccupant, pools []Pool) {
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
		promote := i + 1
		if byePrecedenceLess(order[i], order[i+1], pools) {
			promote = i
		}
		*bye, order[promote] = order[promote], *bye
	}
}

// interleaveGroups round-robins over the rank groups: group[0][0], group[1][0],
// ..., group[0][1], ... Exhausted groups drop out. With a single group this is
// the identity, which is what makes a 1-qualifier block pair consecutive pools
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

// BuildSlotTree turns a power-of-two slot array into a bracket subtree by
// recursive halving, COLLAPSING any half that is entirely empty.
//
// The collapse is what turns padding into structure: a side with no occupants
// contributes no node, so its sibling advances a round instead of playing a
// phantom match. TreeToLeafArray re-pads the result, so the slot array is
// recovered byte for byte on the way back out to the engine.
//
// This is the ONLY correct way to rebuild a tree from a pow2 leaf array, and it
// is why CreateBalancedTree must not be used for one. CreateBalancedTree gives
// every slot a node, so an all-empty half becomes a phantom match that
// PrintLeafNodes draws and AssignMatchNumbers numbers -- a bye's empty slots
// printed as a bout, and every later number shifted off the bracket's own
// (bc-cse: a 5-entrant playoffs sheet printed 7 junctions for a 4-bout draw,
// and the results workbook, which matches score blocks BY printed number, then
// wrote each result into the wrong block). CreateBalancedTree stays correct for
// its own input, an entrant list with no bye slots in it: with no empty slot to
// collapse the two builders produce the identical tree.
//
// Exported for the standalone-playoffs export path (engine.EliminationDraw),
// which rebuilds its tree from the frozen bracket's pow2 first round; the
// pool-fed draw reaches it through buildBlock.
func BuildSlotTree(slots []string) *Node {
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
	left := BuildSlotTree(slots[:mid])
	right := BuildSlotTree(slots[mid:])
	// A SLOT-level collapse marks the survivor as RISEN so round
	// classification can put it back at the level it was built at (see
	// Node.risen): without the mark, a phantom-risen match schedules a round
	// late on every surface that reads rounds -- the Excel columns, the match
	// numbers and the app's bracket -- where the sheet fights it in round 1.
	// The mark belongs HERE and not in joinNodes: combine's assembly joins
	// (a lone block against an empty half, say) collapse structure, not empty
	// slots, and marking those lifted a 2-entrant draw's only match clean out
	// of every round.
	switch {
	case left == nil:
		if right != nil {
			right.risenBefore++
		}
		return right
	case right == nil:
		left.risenAfter++
		return left
	}
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
	// quarterOf[b] is the quarter of the draw block b belongs to, and it is what
	// route aims at for R5's "no two qualifiers of one pool in the same
	// quarter". The subdivision in planBlocks reaches four blocks whenever there
	// are enough qualifiers to fill them, so a quarter then spans whole blocks
	// and the rule is something route can act on. Where it does not -- too few
	// occupants to cut that fine, or a legacy odd shiaijo count -- quarterOf
	// degrades to the block index and the preference reads as "a different
	// block", which at two blocks is R4's opposite halves and still delivers
	// R5's guarantee at 2 qualifiers.
	quarterOf []int
	// halfOrder[h] is the order the half's blocks are combined in (D2).
	halfOrder [2][]int
}

// mirroredBlock reports whether block b sits in the SECOND (inner-bottom)
// position of its half, which is the position both Men Team sheets print as
// the first block's vertical mirror (R6(c)). Position is read from halfOrder,
// so a D2 reorder moves a block's orientation with its printed position. The
// four-court reference sheets pin exactly two blocks per half; longer halves
// (subdivided low-court draws, 8+ shiaijo) mirror their second half by the
// same outside-in reading, extrapolated.
func (p *drawPlan) mirroredBlock(b int) bool {
	for h := range p.halfOrder {
		for i, bb := range p.halfOrder[h] {
			if bb == b {
				return i >= (len(p.halfOrder[h])+1)/2
			}
		}
	}
	return false
}

// planBlocks is how many BLOCKS the pool set is cut into.
//
// A block is the unit R4 crosses between and D4 lays out. At four or more
// shiaijo a block IS a shiaijo's region and the draw's four quarters are whole
// blocks, which is why quarter separation is free there. Below four shiaijo the
// same structure is reached by subdividing the POOL SET -- R4(e)'s "half-blocks
// that act as partner courts" -- so the block tree, and with it every crossing
// and every quarter, is the same object whatever the shiaijo count, which is
// R4(e)'s "identical whether an event runs on 1 court or several".
//
// ONE limit stops the subdivision, and it is about OCCUPANTS, not pools.
//
// A block does not own pools; it holds QUALIFIERS, and from 2 qualifiers up
// there are more of those than there are pools. A block with no pool of its own
// is not a defect -- R4(f) blesses it outright and the EKC Junior Team draw
// prints it (Q4: P3#2 v P4#2). What the subdivision must not do is manufacture
// a bye that R6 cannot CHOOSE: a block left with a single occupant byes that
// occupant no matter what precedence says, and cutting finer is our decision,
// not the operator's. So the split stops while a block would still average
// fewer than two occupants (numPools*poolWinners < 2*blocks after doubling).
//
// Both failures that motivated it are real and were measured:
//
//   - 5 pools, 1 qualifier, 1 shiaijo. Four blocks hold A1+B1 / C1 / D1 / E1,
//     so C1 byes purely for being alone -- while pool A holds seed 1. Two
//     blocks hold A1+B1+C1 / D1+E1, and R6 gives the odd block's bye to A1.
//     This is the shape bc-draw names in its verification requirements.
//   - 3 pools, 2 qualifiers. Four blocks isolate pool B's winner in one and its
//     runner-up in another, byeing BOTH while two other pool winners play. Two
//     blocks bye A1 and C1: two different pools' winners, by precedence.
//
// The limit is NOT the pool count, which is what it was first written as. At 3
// pools and 4 qualifiers there are 12 occupants for 4 blocks, three each, and
// capping by pools there bought nothing but the one R5 residual the spec used
// to carve out.
func planBlocks(numPools, poolWinners, numCourts int) int {
	if numCourts > 2 {
		return numCourts
	}
	blocks := numCourts
	if poolWinners < 2 {
		// R4(d): at ONE qualifier per pool nothing crosses. There is no 2nd for
		// a partner block to receive, and R5 has nothing to separate, so a
		// subdivision buys nothing and can only take R6's choice of bye away.
		// Both EKC individual draws are this shape and each court's region is a
		// single block: court A ran 5 pools and P1 -- the first, and a seeded
		// one -- byed, which is R6 choosing from all five.
		return blocks
	}
	if blocks < 2 {
		// R4(e): a lone shiaijo has no partner court, so it is cut in two or
		// its pools' 1sts and 2nds have nowhere to cross to and could meet in
		// round 1 -- the whole of what R5 exists to prevent.
		blocks = 2
	}
	for blocks < 4 && numPools*poolWinners >= 4*blocks {
		blocks *= 2
	}
	return blocks
}

// subdivideCourts cuts each shiaijo's pools into per sub-blocks by REPEATED
// HALVING and returns pool -> block. Blocks are numbered court-major, so block
// b belongs to shiaijo b/per, a court's blocks are consecutive, and (for the
// counts that subdivide at all) they sit inside one half of the draw.
//
// Halving rather than dividing by per in one go is what keeps the block tree
// NESTED. AssignPoolsToCourts front-loads its remainder, so a direct 4-way
// split of 10 pools gives 3/3/2/2 while halving gives 3/2/3/2, and only the
// latter has the same two HALVES as the 2-way split those same 10 pools get on
// two shiaijo. Nesting is what makes the shiaijo count select a LEVEL of one
// fixed tree instead of drawing a different bracket.
//
// A pool whose court index is out of range is left in block 0, which is the
// same clamp blockForRank applies to a home block it cannot resolve.
func subdivideCourts(poolCourt []int, numCourts, per int) []int {
	out := make([]int, len(poolCourt))
	for c := 0; c < numCourts; c++ {
		idx := []int{}
		for pi, pc := range poolCourt {
			if pc == c {
				idx = append(idx, pi)
			}
		}
		assignHalvedBlocks(idx, out, c*per, per)
	}
	return out
}

// assignHalvedBlocks writes block numbers [base, base+span) over idx by
// repeated halving, the first half of the pools taking the first half of the
// block range. An empty half is legitimate: a block with no pools of its own
// simply has no home 1st and hosts crossed-in qualifiers (R4f), and a block
// with nothing at all collapses in joinNodes.
func assignHalvedBlocks(idx, out []int, base, span int) {
	if span <= 1 {
		for _, pi := range idx {
			out[pi] = base
		}
		return
	}
	mid := (len(idx) + 1) / 2
	assignHalvedBlocks(idx[:mid], out, base, span/2)
	assignHalvedBlocks(idx[mid:], out, base+span/2, span/2)
}

func newDrawPlan(pools []Pool, poolCourt []int, poolWinners, numCourts int) *drawPlan {
	p := &drawPlan{numCourts: numCourts}

	p.numBlocks = planBlocks(len(pools), poolWinners, numCourts)
	per := 1
	if numCourts > 0 {
		per = p.numBlocks / numCourts
	}
	p.poolBlock = subdivideCourts(poolCourt, numCourts, per)
	p.blockCourt = make([]int, p.numBlocks)
	for b := range p.blockCourt {
		p.blockCourt[b] = b / per
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
// R4a: the 1st place stays in its own block.
// R4b: the 2nd place crosses to the PARTNER block, which is half the bracket
// away, so a pool's two qualifiers can only meet in the final (R5).
// R4c/D3/D5: from the 3rd place on, qualifiers alternate halves in the pattern
// {1,4} in the 1st's half and {2,3} in the other, and inside the target half
// take the region that keeps them out of a quarter their pool already occupies.
// At exactly four shiaijo that reduces to the fixed rotation D5 tabulates
// (A: A,C,D,B -- and the 3rd-place column is D3's A->D, B->C, C->B, D->A
// involution).
//
// R4f, structure beats preference: nothing here reserves capacity, so a block
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
		// fall back to the pool's own block rather than dropping a qualifier.
		return home
	}
	return best
}

// betterCrossTarget reports whether block a is a better landing block than b
// for a crossing qualifier: first the quarter this pool has used least (R5's
// no-two-in-a-quarter), then the block it has used least, then the block with
// the fewest occupants so far (D3 step 3's balance), then the lower block order.
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
// the two halves then meet at the root. A region is the node at the LEVEL of
// the block tree the shiaijo count selects, and every one of them is a node
// inside the returned root rather than a copy, which is what R3 and the
// page-to-shiaijo mapping rest on.
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
	switch p.numCourts {
	case 1:
		// Every block belongs to the one shiaijo, so its region is the draw.
		regions[0] = root
	case 2:
		// A shiaijo's blocks are exactly one half's blocks, so the half node IS
		// the region.
		regions[0], regions[1] = halves[0], halves[1]
	default:
		for b, n := range blockRoots {
			if c := p.blockCourt[b]; c >= 0 && c < p.numCourts {
				regions[c] = n
			}
		}
	}
	return root, regions
}

// combineNodes joins region subtrees by the same greedy, collapse-on-empty
// halving BuildSlotTree uses, padding at the END so the trailing block takes
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
// estimate: engine.buildBracketFromDraw resolves each match's first-round
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
	// The BAND offset, not the content one: a region's span has to cover
	// every slot the region occupies, rise slots included, or the slots a
	// leading collapse left empty would be attributed to the NEXT court.
	walkLeafOffsets(d.Root, 0, func(node *Node, bandAt, _, width int) {
		if i, ok := index[node]; ok && i < len(spans) {
			spans[i] = [2]int{bandAt, bandAt + width}
		}
	})
	return spans
}

// walkLeafOffsets visits every node with its offset and width in
// TreeToLeafArray(root) -- the ONE traversal that turns the tree back into leaf
// positions. The padding rule is not restated here, nor in leafArrayWidth: both
// defer to leafPadTarget (tree.go), so a change to TreeToLeafArray's geometry
// has one place to land.
//
// Both readers of that geometry go through it. They must agree exactly: one
// (RegionSpans) decides which slots a shiaijo's region owns, the other
// (NodeCourts) decides which shiaijo a bout prints under, and a disagreement
// puts the operator console and the printed running order on different courts
// with nothing to catch it. Written once for the same reason leafPadTarget is.
// The two offsets are reported SEPARATELY because the two readers want
// different ones, and conflating them is how the risen-before geometry went
// wrong: a node owns the whole BAND [bandOffset, bandOffset+width), rises
// included, but its entrants sit in the CONTENT sub-range, which for a
// leading collapse starts partway in. RegionSpans tiles the leaf array and so
// needs the band (content-only spans would leave the rise slots owned by
// nobody); SlotRoundMatches locates a bout's first-round window and so needs
// the content. Passing the content offset with the band WIDTH, as this used
// to, gave RegionSpans a span running past the node's own slots, and so
// handed the first slots of the NEXT region to this court.
func walkLeafOffsets(n *Node, offset int, visit func(node *Node, bandOffset, contentOffset, width int)) {
	if n == nil {
		return
	}
	width := leafArrayWidth(n)
	// Rises occupy real empty slots: the node's content sits at the top of
	// its widened band when the collapsed sibling trailed it, at the bottom
	// when it led, so offsets below stay slot-true (SlotArray is the same
	// reading; before- and after-rises never mix on one node in practice).
	content := width >> (n.risenAfter + n.risenBefore)
	contentOffset := offset
	level := width
	for i := 0; i < n.risenBefore; i++ {
		level /= 2
		contentOffset += level
	}
	visit(n, offset, contentOffset, width)
	if n.LeafNode {
		return
	}
	side := content / 2
	walkLeafOffsets(n.Left, contentOffset, visit)
	walkLeafOffsets(n.Right, contentOffset+side, visit)
}

// leafArrayWidth is len(TreeToLeafArray(n)) without building the slice. It
// measures with leafPadTarget, the same rule TreeToLeafArray builds with, so
// the two cannot disagree about where a region starts.
// SlotRoundMatch locates one bout of a draw in pow2-bracket terms: the slot
// offset and entrant width of its first-round window, plus the round the
// risen-aware walk (BuildEliminationMatchRounds) fights it in, 0-based from
// the first round. The engine uses this to stamp DisplayRound on its pow2
// bracket matches: neither the pow2 row (which tail-pads an assembly-level
// late bout into round-1 adjacency) nor the feeder graph (which defers a
// phantom-risen pair the sheets fight in round 1) can tell those two shapes
// apart on their own -- the risen tree is the one place the distinction
// lives, so its walk is the one source of a bout's round.
type SlotRoundMatch struct {
	Offset       int
	EntrantWidth int
	Round        int
}

// SlotRoundMatches maps every bout of the draw tree through walkLeafOffsets'
// slot geometry. EntrantWidth is the bout's content width (its slot width
// with the rises stripped), i.e. the width of the pow2 round row the bout's
// entrants sit in.
func SlotRoundMatches(root *Node) []SlotRoundMatch {
	type geo struct{ offset, content int }
	geos := map[*Node]geo{}
	// The CONTENT offset: a bout's first-round window is where its entrants
	// actually sit, which for a leading collapse starts partway into the band.
	walkLeafOffsets(root, 0, func(n *Node, _, contentAt, width int) {
		geos[n] = geo{contentAt, width >> (n.risenAfter + n.risenBefore)}
	})
	var out []SlotRoundMatch
	for roundIdx, round := range BuildEliminationMatchRounds(root) {
		for _, m := range round {
			if g, ok := geos[m]; ok {
				out = append(out, SlotRoundMatch{Offset: g.offset, EntrantWidth: g.content, Round: roundIdx})
			}
		}
	}
	return out
}

// slotDepth is the tree depth in SLOT levels: the depth CalculateDepth would
// report had no empty sibling ever been collapsed. The two differ exactly on
// trees whose top carries rises -- a split page holding one risen block, say --
// where physical depth under-counts and would drop the block's bout from every
// round (its virtual level exceeds the physical target range). leafArrayWidth
// is always a power of two, so this is log2(width)+1 computed by bit length.
func slotDepth(n *Node) int {
	if n == nil {
		return 0
	}
	w := leafArrayWidth(n)
	d := 0
	for w > 0 {
		d++
		w >>= 1
	}
	return d
}

func leafArrayWidth(n *Node) int {
	if n == nil {
		return 0
	}
	w := 1
	if !n.LeafNode {
		w = 2 * leafPadTarget(leafArrayWidth(n.Left), leafArrayWidth(n.Right))
	}
	// A rise doubles the node's slot footprint per level: the empty sibling's
	// slots are real positions (SlotArray emits them), and the spans/court
	// arithmetic must measure the same array the engine bracket is built from.
	return w << (n.risenAfter + n.risenBefore)
}

// NodeCourts maps every node of the draw's tree to the index of the shiaijo
// that hosts it, via CourtForSpan: the region owning the node for anything
// inside one region, and the centre-most court it spans for the half-finals and
// the final.
//
// This is the SAME question engine.buildBracketFromDraw answers for a stored
// bracket match, asked the same way, and the two must agree: one is what the
// operator's screen says, the other is what the printed handout says. They are
// both "which region owns this bout's first slot", so neither may derive it by
// dividing a match count by a court count. That division is only right when
// every region holds the same number of pools, and the court-first draw
// deliberately allows unequal regions -- 4 pools over 4 shiaijo gives four
// single-qualifier regions whose two first-round bouts belong to regions 0 and
// 2, not 0 and 1.
//
// Returns nil for an empty draw, which callers read as "one band, court 0".
func (d *KnockoutDraw) NodeCourts() map[*Node]int {
	if d == nil || d.Root == nil {
		return nil
	}
	spans := d.RegionSpans()
	out := make(map[*Node]int)
	// The BAND, matching RegionSpans above: the two are compared against each
	// other, so they must measure the same thing.
	walkLeafOffsets(d.Root, 0, func(node *Node, bandAt, _, width int) {
		out[node] = CourtForSpan(spans, bandAt, width)
	})
	return out
}

// CourtForSpan returns the shiaijo that should host a bout covering leaf slots
// [at, at+width).
//
// A bout whose span lies inside ONE region takes that region's court: R3 says a
// shiaijo's pools occupy one contiguous subtree, so every bout below the region
// root belongs to the court that runs it.
//
// A bout ABOVE the regions -- the half-finals and the final -- belongs to no
// single court, and takes the CENTRE-MOST of the courts it spans, ties to the
// lower index. That is where a hall puts its closing bouts: on four shiaijo the
// two semi-finals run on B and C and the final on B, not on A because A happens
// to be leftmost. The centre is measured across the whole allocation, not
// across the bout's own span, which is what makes the first half's inner court
// (B) rather than its outer one (A) the answer.
//
// It generalises to every legal count: 2 shiaijo put the final on A, 8 put the
// semi-finals on D and E with the final on D, and 1 puts everything on A. The
// operator can still move any bout afterwards; this is only the default.
func CourtForSpan(spans [][2]int, at, width int) int {
	if width <= 0 {
		return CourtForLeafSlot(spans, at)
	}
	end := at + width
	var covered []int
	for i, sp := range spans {
		if sp[1] <= sp[0] {
			continue
		}
		if sp[0] < end && at < sp[1] {
			covered = append(covered, i)
		}
	}
	if len(covered) < 2 {
		return CourtForLeafSlot(spans, at)
	}
	// |2i - (n-1)| is the distance to the centre of the allocation, doubled to
	// stay in integers. The first minimum wins, which is the lower-index tie.
	best, bestDist := covered[0], 0
	for k, i := range covered {
		d := 2*i - (len(spans) - 1)
		if d < 0 {
			d = -d
		}
		if k == 0 || d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
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
