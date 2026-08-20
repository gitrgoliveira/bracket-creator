package helper

import (
	"fmt"
	"math"
	"sort"
)

// The "fill-bracket" qualifier mode (bead bc-qual, phase LP-4: "Fit the
// knockout exactly"). Successor to LP-3a/LP-3c's ExtraQualifiersLargerPools,
// decoded from the WKC combination tables rather than the EKC sheets --
// originally the 19WKC 2024 (Milan) events, then re-derived 2026-08-19 the
// moment the 17WKC 2018 (Incheon) tables were added: the 17WKC Women's Team
// sheet is the one event whose drafts DISCRIMINATE between candidate rules,
// and it overturned the first cut's "oversized pools supply the drafts"
// reading (draw_wkc_test.go holds the sheet-by-sheet evidence).
//
// Where larger-pools cuts pools EXACTLY as standard mode and lets an
// oversized pool send one extra qualifier (leaving the shortfall as byes
// unless the extra fills one), fill-bracket changes how pools are CUT in
// the first place: FillBracketPoolCount picks the pool count so that pool
// winners, plus a handful of DRAFTED 2nd places, exactly fill a power-of-two
// knockout bracket with ZERO byes. SelectFillBracketDrafts chooses which
// pools' 2nds are drafted -- SEEDED pools in seed order, exactly as both WKC
// sheets footnote and fill their draft slots, with oversized pools as this
// package's fallback for a roster without enough seeds;
// BuildKnockoutDrawFillBracket places them.
//
// Placement (rule 3, bc-qual): a drafted 2nd crosses to the OPPOSITE HALF
// of the draw from its own pool's winner -- team-style separation
// (drawPlan.partnerBlock's own R4b crossing rule), NOT larger-pools'
// same-half-neighbour rule (draw_perpool.go's crossNeighbourCourt). Six
// independent observations across the two championships show it (2 on the
// 19WKC women's team sheet at slots 5 and 12 of a flat 16-leaf bracket, 4 on
// the 17WKC one at slots 4, 5, 12 and 13) -- WKC's own outer-edge seed
// geometry stays explicitly OUT OF SCOPE (spec D6 records it as future
// work). This package's court-region machinery is what stays: blocks per
// shiaijo, halves, semis on the middle court. The exact WITHIN-half slot a
// draft lands in beyond "opposite half, round-1 fighting slot, never a bye"
// is this file's own EXTRAPOLATION, driven by the existing greedy block
// layout (buildBlock/interleaveByRank) rather than a replication of WKC's
// specific seating -- labelled on BuildKnockoutDrawFillBracket's own doc
// comment.
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
// objective, which is WKC's own (decoded 2026-08-19 from the 19WKC 2024 and
// 17WKC 2018 combination tables -- all six events that filled a bracket, four
// team and two individual, reproduce under this rule):
//
//  1. P must use every entrant in pools of size minSize or minSize+1
//     ("oversized"): ceil(n/(minSize+1)) <= P <= floor(n/minSize).
//  2. The bracket is the SMALLEST power of two any such P can reach,
//     B = NextPow2(ceil(n/(minSize+1))) -- never a bracket a round bigger
//     than the field forces.
//  3. Within that bracket, P is the LARGEST count whose draft requirement
//     D = B - P is EVEN, falling back to an odd-D count only when no even-D
//     count is supplied (rule 4 below).
//
// The evenness preference is not curve-fitting: both sheets that drafted at
// all placed their draft slots mirror-symmetrically, one per half (19WKC
// Women's Team, D=2) or two per half (17WKC Women's Team, D=4), so each half
// of the draw sends exactly as many 2nds as it receives. An odd D cannot do
// that. It stays a PREFERENCE rather than a hard rule so that a range with
// only one legal P (e.g. 11 entrants at minSize 3 -> P=3, D=1) still forms
// rather than refusing a field the builder handles fine -- the asymmetric
// D=1 shape is this package's own extension, no sheet witnesses it.
//
// The six verified events, all at minSize 3:
//
//   - 60  (19WKC Men's Team)         -> P=16, D=0
//   - 49  (17WKC Men's Team)         -> P=16, D=0
//   - 45  (19WKC Women's Team)       -> P=14, D=2. P=15 also uses every
//     entrant, but D=1 is odd; 14 is the largest even-D count, and 14 is
//     what the sheet cut.
//   - 38  (17WKC Women's Team)       -> P=12, D=4.
//   - 203 (19WKC Women's Individual) -> P=64, D=0
//   - 242 (19WKC Men's Individual)   -> P=64, D=0
//
// The rule above says nothing about WHERE the D drafted 2nds come from --
// that is SelectFillBracketDrafts' job (seeded pools first, then oversized).
// But formation cannot IGNORE supply either: the largest even-D count can
// demand more drafts than any roster could deliver (a field just past a
// bracket boundary, say 70 teams -> 22 pools wanting 10 drafts, with 4
// oversized pools and however many seeds), and returning a count whose
// selection is doomed just moves the refusal somewhere less actionable. The
// WKC sheets never face this -- their entrant counts sit close enough to
// their brackets that <=4 seeds always cover the shortfall -- so what
// formation does beyond them is this package's own extension:
//
//  4. seededPools is the number of seed ranks the roster carries (each lands
//     in its own pool, R2). A candidate P is SUPPLIED when D fits the
//     guaranteed candidate count max(min(seededPools, P), n - minSize*P) --
//     the max, not the sum, because a seeded pool can also be the oversized
//     one and must not be counted twice. Preference: the largest SUPPLIED
//     even-D count, then the largest SUPPLIED odd-D count, then a clean
//     error naming seeding as the remedy.
//
// The 45-team sheet pins that preference ORDER, not just the arithmetic:
// P=15 (D=1, odd) is supplied there too -- three oversized pools at P=15
// would be zero, but four seeds cover one draft -- yet the sheet cut 14, so
// even-D must outrank largest-P. An unseeded roster degrades gracefully:
// supply is then oversized pools alone, which slides P down toward the
// fatter-pool shapes the first cut of this function produced (e.g. 38
// unseeded -> 10 pools, 6 drafts from 8 oversized), instead of refusing.
//
// (The previous cut of this function required D <= oversized outright,
// because drafts then came only from oversized pools. That constraint made
// 38 entrants cut 11 pools where the 17WKC sheet cut 12, and fell with the
// oversized-only draft rule itself.)
//
// Returns a clean, actionable error naming n and minSize when no P exists at
// all (minSize invalid, n too small for even one pool, no P uses every
// entrant, or no supplied P) -- scope discipline: fail loudly rather than
// guess a shape no sheet evidences.
func FillBracketPoolCount(n, minSize, seededPools int) (pools, drafts int, err error) {
	if minSize <= 0 {
		return 0, 0, fmt.Errorf("fill-bracket: minimum pool size must be at least 1, got %d", minSize)
	}
	if n < minSize {
		return 0, 0, fmt.Errorf("fill-bracket: %d entrant(s) is fewer than the minimum pool size %d, cannot form even one pool", n, minSize)
	}

	maxP := n / minSize
	minP := (n + minSize) / (minSize + 1) // ceil(n / (minSize+1))
	if minP > maxP {
		return 0, 0, fmt.Errorf("fill-bracket: no pool count fits %d entrants at minimum pool size %d into pools of %d or %d that use every entrant; adjust the entrant count or minimum pool size", n, minSize, minSize, minSize+1)
	}

	// NextPow2 is nondecreasing in P, so the smallest reachable bracket is
	// minP's, and every P at or below that bracket's size reaches it.
	bracket := NextPow2(minP)
	top := maxP
	if bracket < top {
		top = bracket
	}

	// Rule 4's guaranteed draft-candidate count at pool count p: seeded
	// pools (each seed rank lands in its own pool, capped at p) or oversized
	// pools, whichever is MORE -- the worst case is that every seeded pool
	// is also an oversized one.
	supplied := func(p int) bool {
		seeds := seededPools
		if seeds > p {
			seeds = p
		}
		supply := n - minSize*p // oversized pool count at this p
		if seeds > supply {
			supply = seeds
		}
		return bracket-p <= supply
	}

	// Largest supplied even-D count first, then largest supplied odd-D.
	for _, wantOdd := range []int{0, 1} {
		for p := top; p >= minP; p-- {
			if (bracket-p)%2 == wantOdd && supplied(p) {
				return p, bracket - p, nil
			}
		}
	}
	return 0, 0, fmt.Errorf("fill-bracket: no pool count fits %d entrants at minimum pool size %d: every cut needs more drafted 2nds than the roster's %d seeded pool(s) and its oversized pools can supply; seed more pools, or adjust the entrant count or minimum pool size", n, minSize, seededPools)
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
// place is DRAFTED into the fill-bracket knockout (rule 2, bc-qual LP-4).
//
// Candidates are the SEEDED pools, in seed order, then the OVERSIZED pools
// (more than minSize members) in pool order; a pool that is both is one
// candidate, ranked as seeded. Seeded-first is WKC's own rule, and the only
// reading both championships' sheets support:
//
//   - 19WKC Women's Team needs 2 drafts and takes them from blocks 1 and 16
//     -- the two NAMED seeds (Japan, Korea). Block 9 is oversized too and
//     sends nothing: it is seed 3 or 4, and only two were needed. Seed order
//     explains that; size cannot.
//   - 17WKC Women's Team needs 4 and takes them from blocks 1, 16, 8 and 9
//     -- exactly the four blocks its own footnote seeds ("Seed 1a: Japan,
//     16a: Korea / Seed 8a,9a: USA & Brasil by Draw"), and blocks 8 and 9
//     hold THREE teams each, so two of the four drafted blocks are not
//     oversized at all. This is the sheet that killed the first cut's
//     oversized-only rule, which could never reach it and refused the whole
//     shape.
//
// The oversized tail is this package's own fallback, no sheet witnesses it:
// a roster with fewer seeds than drafts would otherwise refuse outright,
// when an oversized pool has a spare competitor to give. Unseeded rosters
// therefore behave exactly as the first cut did (every candidate is
// unseeded-oversized, taken in pool order).
//
// Selection is CAPACITY-AWARE (second review rework): a candidate is taken
// ONLY if the OPPOSITE half from its own home (rule 3) still has remaining
// draft capacity; a candidate whose destination half is already full is
// SKIPPED, not a hard failure, and the scan continues down the order. This
// is what closes the "invisible, data-dependent refusal" the placement-only
// rework left: strict priority order alone can pick two pools whose homes
// are BOTH in the same half when the draw has only one destination slot
// left in the opposite half -- a shape FillBracketPoolCount already promised
// was fine, refused only because selection had no way to route around it.
//
// poolHalf and capacityByHalf come from FillBracketDraftCapacity, computed
// over the SAME pool set before any pool is chosen; a caller that hands in
// mismatched values (e.g. from a different draft count or court
// allocation) gets undefined SELECTION, not a panic -- poolHalf is indexed
// defensively -- but the resulting draft list will not necessarily satisfy
// BuildKnockoutDrawFillBracket, which re-derives and re-checks its own
// capacity independently regardless.
//
// Returns an error (never a partial/short list) when the scan ends having
// placed fewer than the total capacity (capacityByHalf[0]+capacityByHalf[1])
// drafts: not enough seeded-or-oversized pools exist, or too many of them
// share a half that has no remaining capacity to receive them. Unlike the
// first cut, FillBracketPoolCount does NOT rule the first case out -- its
// formation no longer depends on draft supply, because supply now turns on
// seeding, which formation cannot see. The error therefore names seeding as
// the operator's remedy: seeding more pools fixes the supply without
// re-cutting anything.
func SelectFillBracketDrafts(pools []Pool, minSize int, poolHalf []int, capacityByHalf [2]int) ([]int, error) {
	total := capacityByHalf[0] + capacityByHalf[1]
	if total <= 0 {
		return nil, nil
	}

	type candidate struct {
		idx    int
		seed   int
		seeded bool
	}
	var cands []candidate
	seededCount, oversizedCount := 0, 0
	for i, p := range pools {
		if len(p.Players) < 2 {
			continue // no 2nd place to draft
		}
		seeded := poolSeedRank(p) != math.MaxInt
		over := minSize > 0 && len(p.Players) > minSize
		if seeded {
			seededCount++
		}
		if over {
			oversizedCount++
		}
		if seeded || over {
			cands = append(cands, candidate{idx: i, seed: poolSeedRank(p), seeded: seeded})
		}
	}

	// SEEDED pools first, in seed order, then oversized pools in pool order
	// (the seed key is math.MaxInt for every unseeded pool, so within the
	// unseeded tail it decides nothing and the idx tiebreak is the order).
	// A pool that is both seeded and oversized is one candidate, ranked as
	// seeded.
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].seeded != cands[j].seeded {
			return cands[i].seeded
		}
		if cands[i].seed != cands[j].seed {
			return cands[i].seed < cands[j].seed
		}
		return cands[i].idx < cands[j].idx
	})

	remaining := capacityByHalf // [2]int is a value type: this copies, leaving the caller's untouched
	out := make([]int, 0, total)
	for _, c := range cands {
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
		return nil, fmt.Errorf("fill-bracket: the draw needs %d drafted 2nd(s) and only %d could be supplied (drafts come from seeded pools in seed order, then oversized pools, each landing in the opposite half of the bracket from its own pool; %d seeded and %d oversized pool(s) exist); seed more pools, or adjust the entrant count or minimum pool size", total, len(out), seededCount, oversizedCount)
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
