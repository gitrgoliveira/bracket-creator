package helper

import (
	"fmt"
	"sort"
)

// The "fill-bracket" qualifier mode (bead bc-qual, phase LP-4: "Fit the
// knockout exactly"). Successor to LP-3a/LP-3c's ExtraQualifiersLargerPools,
// decoded from the 19th WKC 2024 (Milan) draw sheets rather than the EKC
// ones -- evidence recorded in bead bc-draw's Phase 0 comments dated
// 2026-08-19 (all four 19WKC events verified).
//
// Where larger-pools cuts pools EXACTLY as standard mode and lets an
// oversized pool send one extra qualifier (leaving the shortfall as byes
// unless the extra fills one), fill-bracket changes how pools are CUT in
// the first place: FillBracketPoolCount picks the pool count so that pool
// winners, plus a handful of DRAFTED 2nd places from the most senior
// oversized pools, exactly fill a power-of-two knockout bracket with ZERO
// byes. SelectFillBracketDrafts chooses which pools' 2nds are drafted;
// BuildKnockoutDrawFillBracket places them.
//
// Placement (rule 3, bc-qual): a drafted 2nd crosses to the OPPOSITE HALF
// of the draw from its own pool's winner -- team-style separation
// (drawPlan.partnerBlock's own R4b crossing rule), NOT larger-pools'
// same-half-neighbour rule (draw_perpool.go's crossNeighbourCourt). The
// 19WKC women's team sheet (45 entrants, the only witnessed fill-bracket
// instance) shows this at slots 5 and 12 of a flat 16-leaf bracket -- WKC's
// own outer-edge seed geometry, explicitly OUT OF SCOPE here (spec D6
// records it as future work). This package's court-region machinery is
// what stays: blocks per shiaijo, halves, semis on the middle court. The
// exact WITHIN-half slot a draft lands in beyond "opposite half, round-1
// fighting slot, never a bye" is this file's own EXTRAPOLATION, driven by
// the existing greedy block layout (buildBlock/interleaveByRank) rather
// than a replication of WKC's specific seating -- labelled on
// BuildKnockoutDrawFillBracket's own doc comment.
//
// SELECTION is CAPACITY-AWARE (second review rework): the placement side
// alone cannot rescue a shape where strict seed-then-pool-order drafting
// picks two pools that both happen to sit in the SAME half of the draw,
// leaving the opposite half's destination slots unfillable even though
// FillBracketPoolCount already promised the entrant count was fine.
// SelectFillBracketDrafts now takes the per-half draft CAPACITY
// (FillBracketDraftCapacity, computed from pool/draft COUNTS alone, before
// any pool is chosen) and skips a candidate whose destination half is
// already full rather than taking it and stranding the build -- see
// SelectFillBracketDrafts' own doc comment for the mechanism and the
// 19WKC sheet verification.

// FillBracketPoolCount implements the fill-bracket pool-formation
// objective: the LARGEST pool count P such that pools of minimum size
// minSize (a remainder of them one larger, size minSize+1 -- "oversized")
// use every entrant (minSize*P <= n <= (minSize+1)*P) AND the P pool
// winners, plus one drafted 2nd from each of D = NextPow2(P) - P oversized
// pools, exactly fill a power-of-two knockout bracket: D must not exceed
// the number of oversized pools available to draft from, n - minSize*P.
//
// Verified against every 19WKC 2024 event (all four; evidence in bead
// bc-draw's Phase 0 comments dated 2026-08-19):
//
//   - 60 entrants at minSize 3 -> P=16 (12 pools of 4, 4 of 3), D=0.
//   - 45 -> P=14 (11 of 3, 3 of 4), D=2. A same-size P=15 (all pools of
//     exactly 3) also uses every entrant, but has ZERO oversized pools to
//     draft a missing bracket slot from -- that absence is WHY 14 wins over
//     the naive floor(45/3)=15.
//   - 203 -> P=64 (53 of 3, 11 of 4), D=0.
//   - 242 -> P=64 (14 of 3, 50 of 4), D=0.
//
// Also checked against 11 entrants -> P=3 (one pool of 3, two of 4), D=1.
//
// Returns a clean, actionable error naming n and minSize when NO P in the
// legal range satisfies both constraints (e.g. minSize itself is invalid,
// or n is too small relative to minSize for any pool count to both use
// every entrant and supply enough oversized pools for the shortfall) --
// scope discipline: fail loudly rather than guess a shape no sheet
// evidences.
func FillBracketPoolCount(n, minSize int) (pools, drafts int, err error) {
	if minSize <= 0 {
		return 0, 0, fmt.Errorf("fill-bracket: minimum pool size must be at least 1, got %d", minSize)
	}
	if n < minSize {
		return 0, 0, fmt.Errorf("fill-bracket: %d entrant(s) is fewer than the minimum pool size %d, cannot form even one pool", n, minSize)
	}

	maxP := n / minSize
	minP := (n + minSize) / (minSize + 1) // ceil(n / (minSize+1))
	for p := maxP; p >= minP && p >= 1; p-- {
		remainder := n - minSize*p // number of oversized (minSize+1) pools at this P
		need := NextPow2(p) - p    // drafts required to fill the bracket
		if need <= remainder {
			return p, need, nil
		}
	}
	return 0, 0, fmt.Errorf("fill-bracket: no pool count fits %d entrants at minimum pool size %d into a power-of-two knockout bracket with the oversized pools available; adjust the entrant count or minimum pool size", n, minSize)
}

// fillBracketCourtLayout is the per-court/per-half target arithmetic shared
// by FillBracketDraftCapacity (feeding capacity-aware selection, run BEFORE
// any draft is chosen) and buildFillBracketDraw (placement, run AFTER) so
// the two cannot disagree: both derive it from the SAME inputs -- pool
// count, draft COUNT, shiaijo count -- never from which specific pools end
// up drafted.
type fillBracketCourtLayout struct {
	poolCourt []int // per-pool court index (real, or synthetic 0/1 for the 1-shiaijo emulation)
	numCourts int   // effective court count (2 for the 1-shiaijo case)
	plan      *drawPlan
	need      []int // per-court draft need (target - homeCount[c])
	poolHalf  []int // per-pool home half (0/1)

	capacityByHalf [2]int // per-half total draft need (sum of need[c] over that half's courts)
}

// computeFillBracketCourtLayout derives the layout for len(pools) pools plus
// `drafts` drafted 2nds (FillBracketPoolCount's own D) spread over numCourts
// shiaijo. numCourts==1 (after EffectiveDrawCourts clamping) is internally
// treated as two synthetic halves by index parity, exactly matching
// buildFillBracketSingleCourtDraw's own emulation.
//
// Returns ok=false when the per-court target T is not achievable -- see
// BuildKnockoutDrawFillBracket's doc comment for the derivation of why this
// should never happen for a power-of-two numCourts once
// FillBracketPoolCount has accepted the entrant count.
func computeFillBracketCourtLayout(pools []Pool, drafts, numCourts int) (*fillBracketCourtLayout, bool) {
	if len(pools) == 0 {
		return nil, false
	}
	courts := EffectiveDrawCourts(len(pools), numCourts)
	var poolCourt []int
	effCourts := courts
	if courts == 1 {
		// Mirrors buildFillBracketSingleCourtDraw's own synthetic split
		// exactly (index parity: pool i -> synthetic half i%2).
		poolCourt = make([]int, len(pools))
		for i := range pools {
			poolCourt[i] = i % 2
		}
		effCourts = 2
	} else {
		var err error
		poolCourt, err = AssignPoolsToCourts(len(pools), courts)
		if err != nil {
			return nil, false
		}
	}

	// plan supplies the block/half bookkeeping only (LP-3a precedent): at
	// one qualifier per pool nothing subdivides (R4d), so plan.numBlocks
	// must equal effCourts -- block index == court index -- or the
	// per-court arithmetic below does not correspond to plan.halfOf's
	// notion of "half".
	plan := newDrawPlan(pools, poolCourt, 1, effCourts)
	if plan.numBlocks != effCourts {
		return nil, false
	}

	homeCount := make([]int, effCourts)
	for _, c := range poolCourt {
		if c < 0 || c >= effCourts {
			c = 0
		}
		homeCount[c]++
	}
	maxHome := 0
	for _, c := range homeCount {
		if c > maxHome {
			maxHome = c
		}
	}

	// T: the single per-court target occupancy every court ends at exactly
	// (see BuildKnockoutDrawFillBracket's doc comment for the derivation).
	// Checked three ways rather than assumed: it must divide the total
	// evenly, it must be a power of two (buildBlock pads a non-power-of-two
	// occupant count with a real bye, which is exactly what T being a power
	// of two rules out), and it must be at least as large as every court's
	// own home-pool count (else that court would need a NEGATIVE number of
	// drafts, which is meaningless).
	total := len(pools) + drafts
	if total == 0 || total%effCourts != 0 {
		return nil, false
	}
	target := total / effCourts
	if target < maxHome || NextPow2(target) != target {
		return nil, false
	}

	need := make([]int, effCourts)
	var capacityByHalf [2]int
	for c := 0; c < effCourts; c++ {
		n := target - homeCount[c]
		if n < 0 {
			// Unreachable given target >= maxHome above; kept as an
			// explicit invariant check rather than trusted.
			return nil, false
		}
		need[c] = n
		capacityByHalf[plan.halfOf(c)] += n
	}

	poolHalf := make([]int, len(pools))
	for pi, c := range poolCourt {
		if c < 0 || c >= effCourts {
			c = 0
		}
		poolHalf[pi] = plan.halfOf(c)
	}

	return &fillBracketCourtLayout{
		poolCourt:      poolCourt,
		numCourts:      effCourts,
		plan:           plan,
		need:           need,
		poolHalf:       poolHalf,
		capacityByHalf: capacityByHalf,
	}, true
}

// FillBracketDraftCapacity exposes the per-pool home HALF and per-half
// draft CAPACITY (rule 3, bc-qual LP-4) that SelectFillBracketDrafts needs
// for capacity-aware selection -- computed from pool/draft COUNTS alone,
// before any specific pool is chosen, so a caller selects drafts and then
// builds the placement (BuildKnockoutDrawFillBracket) from the exact same
// arithmetic and the two cannot desync.
//
// drafts is the draft COUNT (FillBracketPoolCount's own D); numCourts is
// the shiaijo allocation the draw will run on (1 is emulated as two
// synthetic halves, exactly as BuildKnockoutDrawFillBracket's own
// single-shiaijo path). ok is false when this shape's per-court target is
// not achievable (see BuildKnockoutDrawFillBracket's doc comment) --
// treat that as fill-bracket being out of scope for this (pools, drafts,
// courts) triple, the same conclusion BuildKnockoutDrawFillBracket itself
// would reach.
func FillBracketDraftCapacity(pools []Pool, drafts, numCourts int) (poolHalf []int, capacityByHalf [2]int, ok bool) {
	layout, ok := computeFillBracketCourtLayout(pools, drafts, numCourts)
	if !ok {
		return nil, [2]int{}, false
	}
	return layout.poolHalf, layout.capacityByHalf, true
}

// SelectFillBracketDrafts returns the zero-based pool indices whose 2nd
// place is DRAFTED into the fill-bracket knockout (rule 2, bc-qual LP-4),
// CAPACITY-AWARE (second review rework): oversized pools (a pool with more
// than minSize members) are considered in seed-then-pool-order -- "chosen
// in seed-then-pool-order among oversized pools", unchanged from the first
// cut -- but a candidate is taken ONLY if the OPPOSITE half from its own
// home (rule 3) still has remaining draft capacity; a candidate whose
// destination half is already full is SKIPPED, not a hard failure, and the
// scan continues down the order. This is what closes the "invisible,
// data-dependent refusal" the placement-only rework left: strict
// seed-then-pool-order alone can (and empirically did, in roughly 14% of a
// swept range) pick two pools whose homes are BOTH in the same half when
// the draw has only one destination slot left in the opposite half -- a
// shape FillBracketPoolCount already promised was fine, refused only
// because selection had no way to route around it.
//
// Verified sheet-compatible: on the 19WKC women's team draw (bead bc-draw
// Phase 0, 2026-08-19) the three oversized pools are blocks 1, 9 and 16,
// with capacityByHalf = [1, 1] (one destination slot per half). In
// seed-then-pool order (1, 9, 16): block 1 (home half 1) takes half 2's
// only slot; block 9 (home half 1, the SAME side as block 1) finds half
// 2's slot already gone and is SKIPPED -- not because of seed order (it
// still comes before block 16 in the scan), but because capacity ran out;
// block 16 (home half 2) takes half 1's only remaining slot. The two pools
// selected -- 1 and 16 -- are exactly the two the sheet drafted, and the
// capacity-exhaustion skip of block 9 is exactly why the sheet drafts
// nothing from it. See TestSelectFillBracketDrafts_19WKCWomenTeam.
//
// poolHalf and capacityByHalf come from FillBracketDraftCapacity, computed
// over the SAME pool set before any pool is chosen; a caller that hands in
// mismatched values (e.g. from a different draft count or court
// allocation) gets undefined SELECTION, not a panic -- poolHalf is indexed
// defensively -- but the resulting draft list will not necessarily satisfy
// BuildKnockoutDrawFillBracket, which re-derives and re-checks its own
// capacity independently regardless.
//
// Returns an error (never a partial/short list) only when the scan ends
// having placed fewer than the total capacity
// (capacityByHalf[0]+capacityByHalf[1]) drafts -- i.e. not enough oversized
// pools exist, or too many of them share a half that has no remaining
// capacity to receive them. FillBracketPoolCount's own formation
// constraint rules the first case out for a pool set it produced; the
// second is the genuinely data-dependent residue
// TestFillBracketFormationAndBuilderAgree sweeps and reports -- the error
// message names it in those terms rather than "out of scope", since by
// this point the shape IS a legal formation, just one whose actual oversized
// pools cannot be routed to fill both halves.
func SelectFillBracketDrafts(pools []Pool, minSize int, poolHalf []int, capacityByHalf [2]int) ([]int, error) {
	total := capacityByHalf[0] + capacityByHalf[1]
	if total <= 0 {
		return nil, nil
	}

	type candidate struct {
		idx  int
		seed int
	}
	var oversized []candidate
	for i, p := range pools {
		if minSize > 0 && len(p.Players) > minSize {
			oversized = append(oversized, candidate{idx: i, seed: poolSeedRank(p)})
		}
	}

	sort.SliceStable(oversized, func(i, j int) bool {
		if oversized[i].seed != oversized[j].seed {
			return oversized[i].seed < oversized[j].seed
		}
		return oversized[i].idx < oversized[j].idx
	})

	remaining := capacityByHalf // [2]int is a value type: this copies, leaving the caller's untouched
	out := make([]int, 0, total)
	for _, c := range oversized {
		if len(out) >= total {
			break
		}
		home := 0
		if c.idx >= 0 && c.idx < len(poolHalf) {
			home = poolHalf[c.idx]
		}
		dest := 1 - home
		if remaining[dest] <= 0 {
			continue // this candidate's destination half is full: skip, keep scanning
		}
		remaining[dest]--
		out = append(out, c.idx)
	}

	if len(out) < total {
		return nil, fmt.Errorf("fill-bracket: the oversized pools' draw positions cannot supply both halves of the bracket (need %d drafted 2nd(s), only %d could be placed with capacity remaining in the opposite half; %d oversized pool(s) exist)", total, len(out), len(oversized))
	}
	return out, nil
}

// BuildKnockoutDrawFillBracket builds a court-first, ZERO-BYE knockout draw
// for the fill-bracket qualifier mode (bc-qual LP-4). Every pool sends its
// winner to its own court's region (R4a, unchanged from every other builder
// in this package); each pool in draftPoolIdx (selected by
// SelectFillBracketDrafts) ALSO sends its 2nd place, which crosses to a
// court in the OPPOSITE HALF of the draw from its own pool -- see the
// file-level comment for the evidence and the WKC-geometry scope line.
//
// The per-court TARGET occupancy (first rework, review finding): every
// court ends at the SAME occupancy T, computed as
// T = (len(pools) + len(draftPoolIdx)) / numCourts, not "whichever court
// happens to hold the most pools, topped up by at most one draft each". T
// is ALWAYS a power of two here, by construction: numCourts is a power of
// two (R9) and len(pools)+len(draftPoolIdx) is what FillBracketPoolCount
// computed as NextPow2(len(pools)) when draftPoolIdx came from its own D --
// and NextPow2 of X, divided by a power-of-two divisor that is <= X, is
// itself always a power of two. A court may receive ZERO, ONE, or MULTIPLE
// drafts to reach T (the single-shiaijo case takes every draft on its own
// "court"), and because every court ends at EXACTLY T -- a full, unpadded
// power-of-two block -- buildBlock's own NextPow2 padding is a no-op
// everywhere: no bespoke slot template is needed for the multi-draft case
// any more than for the single-draft one.
//
// Scope, and why anything outside it fails loudly (returns nil) rather than
// guessing: T must divide the total evenly, be a power of two, and be no
// smaller than any court's own home-pool count (checked, not just derived,
// so a caller handing in a T-breaking allocation or draft list gets nil
// rather than a guess); every court's shortfall (T minus its home count)
// must be exactly filled by drafts sourced from pools in the OPPOSITE half
// of the draw (rule 3) -- the per-HALF capacity (sum of shortfalls in that
// half) must match the number of drafts whose OWN pool sits in the other
// half exactly. This is re-derived and re-checked here independently of
// whatever selected draftPoolIdx, as belt-and-braces against a caller that
// bypasses SelectFillBracketDrafts (as several of this package's own tests
// deliberately do, to exercise this function in isolation); it should never
// actually fire on a draftPoolIdx that came from SelectFillBracketDrafts's
// capacity-aware selection (second review rework), which guarantees its
// output already satisfies this exactly, or returns its own error instead
// -- see TestFillBracketFormationAndBuilderAgree, which sweeps the common
// range through the full selection+placement pipeline and reports how
// often SelectFillBracketDrafts itself refuses (the residue: strict
// seed-then-pool order plus capacity-aware skipping still is not enough
// oversized pools placed usefully, a genuinely data-dependent shape).
//
// A single-shiaijo competition (numCourts clamps to 1) has no second COURT
// to be the "opposite half", so it is handled by
// buildFillBracketSingleCourtDraw below: R4(e)'s existing move (the
// uniform/larger-pools builders already emulate two partner courts with two
// internal half-blocks at one shiaijo) applied to fill-bracket's own
// target/need arithmetic.
func BuildKnockoutDrawFillBracket(pools []Pool, draftPoolIdx []int, numCourts int) *KnockoutDraw {
	if len(pools) == 0 {
		return nil
	}
	courts := EffectiveDrawCourts(len(pools), numCourts)
	if courts == 1 {
		return buildFillBracketSingleCourtDraw(pools, draftPoolIdx)
	}
	poolCourt, err := AssignPoolsToCourts(len(pools), courts)
	if err != nil {
		return nil
	}
	return buildFillBracketDraw(pools, draftPoolIdx, poolCourt, courts)
}

// buildFillBracketSingleCourtDraw is BuildKnockoutDrawFillBracket's R4(e)
// counterpart for a single shiaijo (rework, review finding): "opposite
// half" has no second COURT to mean at one shiaijo. Exactly as the
// uniform/larger-pools builders already emulate two partner courts with two
// internal half-blocks at one shiaijo, this splits the pool set into two
// SYNTHETIC halves by INDEX PARITY (pool i -> synthetic half i%2, the same
// deinterleave ReorderPoolsForCourts uses to spread oversized pools across
// real courts, and the same split computeFillBracketCourtLayout uses so
// SelectFillBracketDrafts' capacity-aware selection agrees with this
// function's own placement), builds them through the exact same
// buildFillBracketDraw machinery used for 2+ real shiaijo (treating the two
// synthetic halves as two synthetic "courts", so a drafted pool's winner
// and its own draft always land in different synthetic halves), and
// returns the combined tree as the one real shiaijo's single region.
//
// This is a genuine EXTRAPOLATION: no sheet witnesses a 1-shiaijo
// fill-bracket draw at all, and the parity split is this function's own
// choice among several that would equally satisfy "opposite half"
// (documented here, and on the split itself below, rather than left
// implicit, per the review's instruction). A contiguous front/back split
// was tried first and measured no better overall (see the split's own
// comment below for the specific finding).
func buildFillBracketSingleCourtDraw(pools []Pool, draftPoolIdx []int) *KnockoutDraw {
	if len(pools) == 0 {
		return nil
	}
	// Interleave by INDEX PARITY (pool i -> synthetic half i%2), the same
	// deinterleave ReorderPoolsForCourts already uses to spread oversized
	// pools across real courts (Phase 2a, bc-draw). A contiguous front/back
	// split was tried first; TestFillBracketFormationAndBuilderAgree measured
	// it at 122/867 half-capacity refusals overall (18/492 restricted to
	// D<=4) versus this parity split's 123/867 (18/492 restricted to D<=4)
	// under STRICT (non-capacity-aware) selection -- a WASH, not an
	// improvement, over the full swept range (the two choices rescue and
	// lose different specific n-values rather than one dominating). The
	// parity split is kept anyway because it is the PRINCIPLED choice, not
	// merely the one that happened to measure best on one run: it is the
	// one deinterleave already proven, by the multi-court cases that route
	// through the real ReorderPoolsForCourts, to spread the clustering
	// CreatePoolsForCount's remainder-spread produces -- consistent with the
	// rest of this package rather than a second, ad hoc rule. This
	// function's own choice matters LESS now that selection is
	// capacity-aware (second review rework): computeFillBracketCourtLayout
	// uses this exact same split for BOTH SelectFillBracketDrafts and this
	// function, so whichever split is chosen, the two agree with each other
	// by construction.
	synthetic := make([]int, len(pools))
	for i := range pools {
		synthetic[i] = i % 2
	}
	d := buildFillBracketDraw(pools, draftPoolIdx, synthetic, 2)
	if d == nil {
		return nil
	}
	return &KnockoutDraw{
		Root:    d.Root,
		Regions: []*Node{d.Root},
		blocks:  d.blocks,
		// Every pool is on the ONE real shiaijo (index 0); the synthetic
		// 0/1 split above is an internal construction detail, not a real
		// court assignment, and must not leak into what PoolCourt reports.
		poolCourt: make([]int, len(pools)),
	}
}

// buildFillBracketDraw is BuildKnockoutDrawFillBracket with the
// pool-to-court allocation supplied explicitly, split out for the same
// reason BuildKnockoutDrawFromAssignment and
// BuildKnockoutDrawPerPoolFromAssignment are: it can be exercised directly
// against a hand-fed allocation in tests. numCourts here may be a SYNTHETIC
// court count (buildFillBracketSingleCourtDraw calls it with 2 synthetic
// halves for one real shiaijo); it does not have to equal the real shiaijo
// count.
func buildFillBracketDraw(pools []Pool, draftPoolIdx []int, poolCourt []int, numCourts int) *KnockoutDraw {
	if numCourts < 1 || len(poolCourt) != len(pools) {
		return nil
	}

	// plan supplies the block/half bookkeeping only (LP-3a precedent): at
	// one qualifier per pool nothing subdivides (R4d), so plan.numBlocks
	// must equal numCourts -- block index == court index -- or the
	// per-court arithmetic below does not correspond to plan.halfOf's
	// notion of "half".
	plan := newDrawPlan(pools, poolCourt, 1, numCourts)
	if plan.numBlocks != numCourts {
		return nil
	}

	homeByCourt := make([][]drawOccupant, numCourts)
	homeCount := make([]int, numCourts)
	for pi, p := range pools {
		c := poolCourt[pi]
		if c < 0 || c >= numCourts {
			c = 0
		}
		homeByCourt[c] = append(homeByCourt[c], drawOccupant{
			label: fmt.Sprintf("%s-%s", p.PoolName, GetOrdinal(1)),
			pool:  pi,
			rank:  1,
		})
		homeCount[c]++
	}

	maxHome := 0
	for _, c := range homeCount {
		if c > maxHome {
			maxHome = c
		}
	}

	// T: see the exported function's doc comment for the derivation.
	total := len(pools) + len(draftPoolIdx)
	if total == 0 || total%numCourts != 0 {
		return nil
	}
	target := total / numCourts
	if target < maxHome || NextPow2(target) != target {
		return nil
	}

	// need[c]: how many drafts court c must receive to reach target. May be
	// zero, one, or (the single-shiaijo case) every draft there is.
	need := make([]int, numCourts)
	totalNeed := 0
	for c := 0; c < numCourts; c++ {
		n := target - homeCount[c]
		if n < 0 {
			// Unreachable given target >= maxHome above; kept as an
			// explicit invariant check rather than trusted.
			return nil
		}
		need[c] = n
		totalNeed += n
	}
	if totalNeed != len(draftPoolIdx) {
		// The supplied drafts do not exactly match this court layout's gap.
		return nil
	}

	// Bucket courts and drafts by half. capacityByHalf[h] is the TOTAL
	// number of draft slots courts in half h need to fill; draftByHalf[h]
	// is every draft whose OWN pool sits in half h. Rule 3 (opposite half):
	// half 0's drafts must exactly fill half 1's capacity, and vice versa.
	// A mismatch here should never actually happen for a draftPoolIdx that
	// came from SelectFillBracketDrafts's own capacity-aware selection (see
	// this function's doc comment); this check remains as belt-and-braces
	// for a hand-fed or otherwise bypassing caller.
	var capacityByHalf [2]int
	for c := 0; c < numCourts; c++ {
		capacityByHalf[plan.halfOf(c)] += need[c]
	}
	var draftByHalf [2][]int
	for _, pi := range draftPoolIdx {
		c := poolCourt[pi]
		if c < 0 || c >= numCourts {
			c = 0
		}
		h := plan.halfOf(c)
		draftByHalf[h] = append(draftByHalf[h], pi)
	}
	if len(draftByHalf[0]) != capacityByHalf[1] || len(draftByHalf[1]) != capacityByHalf[0] {
		return nil
	}

	// Deterministic distribution within each opposite-half group: draft
	// priority order (already seed-then-pool-order, from
	// SelectFillBracketDrafts) against destination COURT order, filling
	// each court's need[c] slots before moving to the next court. This is
	// the doc comment's flagged EXTRAPOLATION -- the single witnessed sheet
	// does not choose among multiple valid opposite-half destinations, or
	// among multiple drafts landing on the same court.
	crossedByCourt := make([][]drawOccupant, numCourts)
	fillHalfFrom := func(drafts []int, targetHalf int) {
		i := 0
		for c := 0; c < numCourts; c++ {
			if plan.halfOf(c) != targetHalf {
				continue
			}
			for k := 0; k < need[c]; k++ {
				pi := drafts[i]
				i++
				crossedByCourt[c] = append(crossedByCourt[c], drawOccupant{
					label: fmt.Sprintf("%s-%s", pools[pi].PoolName, GetOrdinal(2)),
					pool:  pi,
					rank:  2,
				})
			}
		}
	}
	fillHalfFrom(draftByHalf[0], 1)
	fillHalfFrom(draftByHalf[1], 0)

	regions := make([]*Node, numCourts)
	for c := 0; c < numCourts; c++ {
		occ := append(append([]drawOccupant{}, homeByCourt[c]...), crossedByCourt[c]...)
		regions[c] = buildBlock(occ, pools, plan.mirroredBlock(c))
		if regions[c] == nil {
			return nil
		}
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
