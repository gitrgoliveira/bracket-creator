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
// routed to, and it is an accepted approximation for the club-spread
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

	skeleton := make([]Pool, len(targetSizes))
	for i, size := range targetSizes {
		if size < 0 {
			size = 0
		}
		skeleton[i] = Pool{
			PoolName: poolPositionName(i),
			Players:  make([]Player, size),
		}
	}

	draw := BuildKnockoutDraw(skeleton, poolWinners, numCourts)
	if draw == nil {
		return nil
	}

	leaves := TreeToLeafArray(draw.Root)
	slotOf := make(map[string]int, len(leaves))
	for slot, label := range leaves {
		if label != "" {
			slotOf[label] = slot
		}
	}

	out := make([][]int, len(targetSizes))
	for i, size := range targetSizes {
		winners := poolWinners
		if winners > size {
			winners = size
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
