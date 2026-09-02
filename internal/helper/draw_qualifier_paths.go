package helper

import "fmt"

// The tree-path seam for the region-aware pool distribution rebuild
// (bc-dojo Phase 1). See draw.go's package doc for the pool-to-knockout draw
// itself; this file answers one question for a caller that has not placed
// any player yet: "if I cut the pools into this many, this big, sending up
// this many qualifiers each, which knockout leaf slot does pool i's Nth
// qualifier land on?"
//
// The answer only needs pool COUNT, pool SIZE and poolWinners -- never a
// court index, and never who is actually in a pool. route() (draw.go) and
// its supporting arithmetic (subdivideCourts, assignHalvedBlocks,
// applyRegionDepthPreference, blockForRank) read only player COUNTS and
// pool/rank INDICES to decide which block a qualifier is routed to; nothing
// there reads a player's seed. So a SKELETON draw, built from placeholder
// pools that carry nobody, reproduces the exact same crossing pattern
// (R4a/b/c, D3, D5 in specs/007-ekc-draw/spec.md) the real per-competition
// draw will use once pools are filled -- which is what lets the region-aware
// distributor score a candidate pool before either the roster or the courts
// are decided.
//
// What a skeleton draw cannot reproduce exactly is which occupant a block's
// STRUCTURAL BYE favours once the real pools turn out to carry seeds:
// byePrecedenceLess (draw.go) reads poolSeedRank, which a placeholder pool
// never has, so a seeded pool's exact leaf slot can shift by one bye
// compared with this skeleton's all-unseeded one. That only ever perturbs
// the fine position INSIDE a block, never which block/region a qualifier is
// routed to, and it is an accepted approximation for the dojo-spread
// heuristic this feeds (the distributor's own seed placement is untouched
// and byte-identical to today's pipeline; this seam is only ever consulted
// for UNSEEDED players). See the Phase 3 scorecard for whether that
// approximation costs anything measurable.

// poolQualifierPaths builds a skeleton knockout draw from pool target SIZES
// alone and returns, for each pool (by its 0-based position in targetSizes,
// the same order BuildKnockoutDraw's `pools` argument is in), the knockout
// leaf slot of every qualifier that pool sends up: rank 1 (the pool winner)
// through min(poolWinners, that pool's own size).
//
// numCourts is passed through to BuildKnockoutDraw purely because the
// skeleton needs it to build the courts-first structure -- AssignPoolsToCourts
// still has to allocate pools to courts before route() can run. Nothing here
// reads a court index back out of the result, and no caller of this function
// may either: courts are assigned onto pools/regions strictly AFTER
// distribution, never as an input to it.
//
// Returns nil for no pools or poolWinners <= 0, matching BuildKnockoutDraw's
// own nil case.
func poolQualifierPaths(targetSizes []int, poolWinners, numCourts int) [][]int {
	if len(targetSizes) == 0 || poolWinners <= 0 {
		return nil
	}
	skeleton := buildQualifierSkeleton(targetSizes)
	draw := BuildKnockoutDraw(skeleton, poolWinners, numCourts)
	if draw == nil {
		return nil
	}
	return qualifierSlotsFromLeaves(draw, skeleton, func(int) int { return poolWinners })
}

// buildQualifierSkeleton builds the placeholder []Pool poolQualifierPaths and
// its two mode-specific siblings below all build a skeleton draw from: one
// pool per target size, named exactly as assignPlayersToPools would name it
// once cut, holding that many zero-value Players and nobody else. Shared so
// the three skeleton builders cannot drift on naming or sizing.
//
// Returns nil when any pool would exceed MaxPoolSize, which every caller
// already treats as "this shape is not supported" (their own documented nil
// return). poolTargetSizes refuses such a pool first, with an error naming the
// size, so this is the bound at the allocation rather than the one the
// operator ever sees.
func buildQualifierSkeleton(targetSizes []int) []Pool {
	skeleton := make([]Pool, len(targetSizes))
	for i, size := range targetSizes {
		if size < 0 {
			size = 0
		}
		// The skeleton holds one placeholder seat per pool seat, because the
		// draw builder reads pool LENGTHS (poolLoad, playersPerBlock) and the
		// fill-bracket mode writes a seed onto the first of them, so the
		// allocation cannot be replaced by a count. Bound it here, where it
		// happens: poolTargetSizes has already refused a pool this size and
		// told the operator why, so reaching this is a caller that skipped
		// that arithmetic, and refusing the SHAPE is what this function's
		// callers already expect a nil for.
		if size > MaxPoolSize {
			return nil
		}
		skeleton[i] = Pool{
			PoolName: poolPositionName(i),
			Players:  make([]Player, size),
		}
	}
	return skeleton
}

// qualifierSlotsFromLeaves extracts, for pool i, the knockout leaf slot of
// each qualifying rank in ranks(i) -- shared by poolQualifierPaths and its
// two mode-specific siblings so all three read a built draw's leaves the
// same way (skeleton pool name + GetOrdinal(rank), looked up in
// TreeToLeafArray's slot map).
func qualifierSlotsFromLeaves(draw *KnockoutDraw, skeleton []Pool, ranks func(poolIdx int) int) [][]int {
	leaves := TreeToLeafArray(draw.Root)
	slotOf := make(map[string]int, len(leaves))
	for slot, label := range leaves {
		if label != "" {
			slotOf[label] = slot
		}
	}
	out := make([][]int, len(skeleton))
	for i := range skeleton {
		winners := ranks(i)
		if winners > len(skeleton[i].Players) {
			winners = len(skeleton[i].Players)
		}
		for rank := 1; rank <= winners; rank++ {
			label := fmt.Sprintf("%s-%s", skeleton[i].PoolName, GetOrdinal(rank))
			if slot, ok := slotOf[label]; ok {
				out[i] = append(out[i], slot)
			}
		}
	}
	return out
}

// poolQualifierPathsPerPool is poolQualifierPaths' larger-pools counterpart
// (bc-dojo Phase 4, mode-aware pool distribution): builds the skeleton via
// BuildKnockoutDrawPerPool instead of the uniform builder, so a pool larger
// than the minimum sends its real EXTRA (crossed) qualifier in the skeleton
// too. Without this, the region-aware distributor would score every
// candidate placement against the STANDARD one-qualifier-per-pool tree even
// for a competition running state.ExtraQualifiersLargerPools, which is not
// the tree BuildKnockoutDrawPerPool actually builds for it.
//
// overrides is exactly what production's own extraQualifierOverrides
// (internal/engine/playoff_skeleton.go) builds from a competition's real
// pools -- pool index -> qualifier count, entries only for pools that differ
// from defaultWinners -- derived here PRE-placement from target SIZES by
// extraQualifierOverridesFromSizes instead, since the "is this pool
// oversized" test (state.Competition.QualifiersForPool) only ever reads a
// pool's participant COUNT, exactly what targetSizes already promises pool i
// will end up holding.
//
// Returns nil when BuildKnockoutDrawPerPool refuses this shape (out of
// larger-pools' scope, e.g. a court count with no same-half neighbour): the
// caller (treeAwareQualifierSlots) degrades to scoring every candidate as
// equally safe rather than guessing a placement no sheet corroborates --
// production's own draw-build step (buildPoolFedDraw) still refuses the
// SAME shape at the real knockout-build step regardless of how the pools
// were formed, so the operator is told either way.
func poolQualifierPathsPerPool(targetSizes []int, defaultWinners int, overrides map[int]int, numCourts int) [][]int {
	if len(targetSizes) == 0 || defaultWinners <= 0 {
		return nil
	}
	skeleton := buildQualifierSkeleton(targetSizes)
	draw := BuildKnockoutDrawPerPool(skeleton, defaultWinners, overrides, numCourts)
	if draw == nil {
		return nil
	}
	return qualifierSlotsFromLeaves(draw, skeleton, func(i int) int {
		return perPoolWinners(overrides, i, defaultWinners)
	})
}

// minSeedRankPerPool reduces seedPoolIdx (seed RANK -> pool index, as
// computed by placeSeedIndices) to POOL index -> that pool's BEST (lowest,
// i.e. minimum) surviving seed rank (bc-dojo-least-conflicted-pool FIX 2).
//
// Two seed ranks can legally share one pool -- gapped survivor ranks after
// no-shows, or more seed ranks than pools wrapping via idx%numPools in
// placeSeedIndices -- and poolQualifierPathsFillBracket only has ONE
// placeholder Players[0] slot per pool to record a seed rank onto. A plain
// `for rank, idx := range seedPoolIdx { skeleton[idx].Players[0].Seed =
// rank }` range-assign is then map-iteration-order dependent: whichever
// rank Go's map happens to visit LAST for that idx wins, which is
// nondeterministic run-to-run on identical input. That changes
// SelectFillBracketDrafts' seed-ordered draft set (it drafts seeded pools
// in seed order via poolSeedRank) and, through it, the whole unseeded pool
// distribution.
//
// Production's real draw-time draft always resolves a pool holding several
// surviving seeds to its BEST (lowest) rank -- poolSeedRank's own contract,
// reused unchanged elsewhere in this package -- so computing the minimum
// here first is both deterministic (a min-reduction is commutative: the
// result does not depend on which order the map is walked in) and
// consistent with what the real draw does.
func minSeedRankPerPool(seedPoolIdx map[int]int) map[int]int {
	out := make(map[int]int, len(seedPoolIdx))
	for rank, idx := range seedPoolIdx {
		if cur, ok := out[idx]; !ok || rank < cur {
			out[idx] = rank
		}
	}
	return out
}

// poolQualifierPathsFillBracket is poolQualifierPaths' fill-bracket
// counterpart (bc-dojo Phase 4): builds the skeleton via
// SelectFillBracketDraftIndices + BuildKnockoutDrawFillBracket, reusing
// production's OWN draft-selection pipeline rather than guessing which
// pools draft a 2nd.
//
// Selection needs to know pool SIZES (a candidate needs >= 2 members to have
// a 2nd place to draft) and which pools are SEEDED, by RANK (drafts are
// taken from seeded pools in seed order, oversized pools as the fallback) --
// both fixed pre-placement, once seed placement (placeSeedIndices) has run:
// a seed's pool is a function of its own rank/court alone, never of who else
// is unseeded. seedPoolIdx (seed RANK -> pool index, the same PRE-reorder
// index space targetSizes is in) is folded into placeholder Players so
// SelectFillBracketDrafts' own poolIsSeeded/poolSeedRank scan sees exactly
// the real seeding, without a single real competitor being placed.
//
// Returns nil when selection or placement refuses this shape (out of
// fill-bracket's scope for this roster/court count): production's own
// draw-build step reaches the identical refusal independently
// (buildPoolFedDraw), so the distributor degrading to "score every candidate
// as equally safe" here costs nothing a caller was not already going to be
// told about at draw time.
func poolQualifierPathsFillBracket(targetSizes []int, minSize int, seedPoolIdx map[int]int, numCourts int) [][]int {
	if len(targetSizes) == 0 {
		return nil
	}
	skeleton := buildQualifierSkeleton(targetSizes)
	// Record the MINIMUM seed rank per pool (bc-dojo-least-conflicted-pool
	// FIX 2), via minSeedRankPerPool -- see that function's own doc comment
	// for why a plain `skeleton[idx].Players[0].Seed = rank` range-assign is
	// wrong here. Only fill-bracket is affected: the standard skeleton
	// carries no seeds, and larger-pools' overrides map is consumed by
	// keyed lookup, never by a range-assign into a shared slot.
	for idx, rank := range minSeedRankPerPool(seedPoolIdx) {
		if idx < 0 || idx >= len(skeleton) || len(skeleton[idx].Players) == 0 {
			continue
		}
		skeleton[idx].Players[0].Seed = rank
	}

	draftIdx, err := SelectFillBracketDraftIndices(skeleton, minSize, numCourts)
	if err != nil {
		return nil
	}
	draw := BuildKnockoutDrawFillBracket(skeleton, draftIdx, numCourts)
	if draw == nil {
		return nil
	}
	drafted := make(map[int]bool, len(draftIdx))
	for _, pi := range draftIdx {
		drafted[pi] = true
	}
	return qualifierSlotsFromLeaves(draw, skeleton, func(i int) int {
		if drafted[i] {
			return 2
		}
		return 1
	})
}
