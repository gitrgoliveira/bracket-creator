package helper

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// Seed-placement warnings (R2, D6 and D7 of specs/007-ekc-draw/spec.md).
//
// R2 and D7 both require the operator to be TOLD when a seeding constraint
// could not be honoured, and both are explicit that it MUST NOT be an error:
// "the surplus seed ranks are ignored with a warning", "the operator sees a
// warning describing what was relaxed", "a seeding rule that can refuse to draw
// is worse than a seeding rule that degrades predictably". A live event has no
// time for a hard failure, and the operator can always move a seed by hand.
//
// The warnings are read off the BUILT DRAW rather than instrumented into the
// placement, so they describe what actually happened rather than what the
// placement intended. That also makes them recomputable at any time from the
// pools and the court count, with nothing to persist and nothing to keep in
// sync.

// maxSeedRanks is how many seed ranks D6 places. Ranks beyond the 4th have no
// half/quarter rule to relax (seedCourtOrder falls back to a round robin over
// courts), so they are only ever reported for the shared-pool case.
const maxSeedRanks = 4

// SeedPlacementWarnings reports every D6 seed-placement constraint the draw
// could not honour, in D7's precedence order: halves, then quarters, then
// shiaijo. The shared-pool case (R2's last bullet) comes first because it
// changes which seeds were placed at all.
//
// Each string is one plain-language sentence for an operator, safe to print on
// a terminal or render in the admin console. Returns nil when there is nothing
// to report, which includes the no-seeds case: a competition without seeds is
// a normal configuration and MUST be warning-free (D6).
//
// The shiaijo check reads the draw's OWN region count
// (KnockoutDraw.NumCourts), which is the post-EffectiveDrawCourts count the
// pools were really allocated over. The competition's requested count is
// deliberately not an input: warning about shiaijo the draw does not have would
// be a false alarm.
func SeedPlacementWarnings(draw *KnockoutDraw, pools []Pool) []string {
	if draw == nil || draw.Root == nil || len(pools) == 0 {
		return nil
	}
	seeds := seedPools(pools)
	if len(seeds) == 0 {
		return nil
	}

	var warnings []string
	placed, ignored := splitSharedPoolSeeds(seeds)
	if len(ignored) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"Seed%s %s ignored: two seeds must never share a pool, and this competition has %d pool%s for %d seed%s. The draw used seed%s %s.",
			Plural(len(ignored)), RankList(ranksOf(ignored)),
			len(pools), Plural(len(pools)), len(seeds), Plural(len(seeds)),
			Plural(len(placed)), RankList(ranksOf(placed))))
	}
	if len(placed) > maxSeedRanks {
		placed = placed[:maxSeedRanks]
	}

	// A seed's position in the draw is its pool's WINNER, which is the
	// qualifier D6 spreads: "seeded pools MUST be distinct, and their
	// qualifiers MUST be spread as widely as the configuration allows".
	paths := drawLeafPaths(draw.Root)
	located := make([]seedPlacement, 0, len(placed))
	for _, s := range placed {
		path, ok := paths[fmt.Sprintf("%s-%s", pools[s.pool].PoolName, GetOrdinal(1))]
		if !ok {
			continue
		}
		located = append(located, seedPlacement{rank: s.rank, pool: s.pool, path: path})
	}
	if len(located) < 2 {
		return warnings
	}

	// D7 constraint 1: seeds 1 and 3 in one half, 2 and 4 in the other, so the
	// semifinals are 1 v 3 and 2 v 4 when the seeds hold.
	if pairs := seedPairsBreaking(located, func(a, b seedPlacement) bool {
		wantSame := (a.rank-b.rank)%2 == 0
		return sameBranch(a.path, b.path, 1) != wantSame
	}); len(pairs) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"Seeds could not be split into halves as 1 and 3 against 2 and 4 (%s). The draw was made anyway.",
			strings.Join(pairs, ", ")))
	}

	// D7 constraint 2: a distinct quarter each.
	if pairs := seedPairsBreaking(located, func(a, b seedPlacement) bool {
		return sameBranch(a.path, b.path, 2)
	}); len(pairs) > 0 {
		warnings = append(warnings, fmt.Sprintf(
			"Not every seed could be given its own quarter of the draw (%s). The draw was made anyway.",
			strings.Join(pairs, ", ")))
	}

	// D7 constraint 3: distinct shiaijo. Only a relaxation when there are
	// enough shiaijo to go round; two seeds per shiaijo is the CORRECT outcome
	// on two shiaijo and four seeds (D6), not something that gave way.
	// The allocation the draw was ASSEMBLED from, not one re-derived from the
	// pool count: a draw built through BuildKnockoutDrawFromAssignment may not
	// match what AssignPoolsToCourts would have chosen, and warning about a
	// shiaijo clash the draw does not have is worse than staying quiet.
	courts := draw.PoolCourt(len(pools))
	if courts != nil && draw.NumCourts() >= len(located) {
		if pairs := seedPairsBreaking(located, func(a, b seedPlacement) bool {
			return courts[a.pool] == courts[b.pool]
		}); len(pairs) > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"Not every seed could be given its own shiaijo (%s). The draw was made anyway.",
				strings.Join(pairs, ", ")))
		}
	}

	return warnings
}

// seedPlacement is one seed's position in the built draw: the path from the
// root to its pool winner's leaf, which is what halves and quarters are read
// from.
type seedPlacement struct {
	rank int
	pool int
	path []int
}

type seedPool struct {
	rank int
	pool int
}

// AnySeeded reports whether these pools carry any operator-assigned seed.
//
// SeedPlacementWarnings returns nil for an unseeded competition, but only once
// its caller has built a whole draw to hand it, and unseeded is the common case
// in the app; callers use this to skip that work. It is seedPools' own emptiness
// test rather than a second scan, so the two cannot drift about what counts as
// seeded. The allocation that buys is one empty slice on the quiet path, against
// a draw construction saved.
func AnySeeded(pools []Pool) bool {
	return len(seedPools(pools)) > 0
}

// seedPools lists every operator-assigned seed with the pool it landed in,
// lowest rank first. A pool with several seeded competitors reports each of
// them; splitSharedPoolSeeds is what decides which one counts.
func seedPools(pools []Pool) []seedPool {
	out := []seedPool{}
	for pi, p := range pools {
		for _, pl := range p.Players {
			if pl.Seed > 0 {
				out = append(out, seedPool{rank: pl.Seed, pool: pi})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].rank != out[j].rank {
			return out[i].rank < out[j].rank
		}
		return out[i].pool < out[j].pool
	})
	return out
}

// splitSharedPoolSeeds applies R2's last bullet: two seeds never share a pool,
// so when several land in one pool only the BEST rank counts and the rest are
// ignored. Seeding protects the top seed most (D7), which is why the surplus is
// taken off the bottom.
func splitSharedPoolSeeds(seeds []seedPool) (placed, ignored []seedPool) {
	taken := map[int]bool{}
	for _, s := range seeds {
		if taken[s.pool] {
			ignored = append(ignored, s)
			continue
		}
		taken[s.pool] = true
		placed = append(placed, s)
	}
	return placed, ignored
}

// drawLeafPaths maps every non-empty leaf label to its path from the root, 0
// for Left and 1 for Right. A shared prefix of length n means the two leaves
// sit in the same subtree n levels down: 1 is a half of the draw, 2 a quarter.
func drawLeafPaths(root *Node) map[string][]int {
	out := map[string][]int{}
	var walk func(n *Node, path []int)
	walk = func(n *Node, path []int) {
		if n == nil {
			return
		}
		if n.LeafNode {
			if n.LeafVal != "" {
				out[n.LeafVal] = append([]int{}, path...)
			}
			return
		}
		walk(n.Left, append(path, 0))
		walk(n.Right, append(path, 1))
	}
	walk(root, nil)
	return out
}

// sameBranch reports whether two leaves share the subtree depth levels below
// the root. A leaf shallower than depth is alone in its branch (an empty
// sibling collapsed), so it shares that branch with nobody.
func sameBranch(a, b []int, depth int) bool {
	if len(a) < depth || len(b) < depth {
		return false
	}
	for i := 0; i < depth; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// seedPairsBreaking returns "seeds 2 and 4"-style descriptions of every seed
// pair the predicate flags, in rank order.
func seedPairsBreaking(placed []seedPlacement, broken func(a, b seedPlacement) bool) []string {
	out := []string{}
	for i := range placed {
		for j := i + 1; j < len(placed); j++ {
			if broken(placed[i], placed[j]) {
				out = append(out, fmt.Sprintf("seeds %d and %d", placed[i].rank, placed[j].rank))
			}
		}
	}
	return out
}

func ranksOf(seeds []seedPool) []int {
	out := make([]int, 0, len(seeds))
	for _, s := range seeds {
		out = append(out, s.rank)
	}
	return out
}

// RankList renders seed ranks as "3", "3 and 4" or "3, 4 and 5". Exported so
// the engine's draw-refusal message reads identically to these warnings; two
// hand-rolled list formatters would drift and an operator would see the same
// set of ranks written two ways.
func RankList(ranks []int) string {
	parts := make([]string, 0, len(ranks))
	for _, r := range ranks {
		parts = append(parts, fmt.Sprintf("%d", r))
	}
	switch len(parts) {
	case 0:
		return "none"
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// Plural is the "s" suffix for a count, exported alongside RankList for the
// same reason.
func Plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// SeedGapDiagnosis names the seed ranks the operator still has to type, for a
// set whose ranks are not contiguous from 1.
//
// This is the DIAGNOSIS only, with no remedy attached, because the remedy
// depends on where the refusal happened: the draw says "then generate the draw
// again", the seeds endpoint says "send the complete seeding", and the admin
// console says which tab to go to. Every one of them states the fault in these
// words, so an operator who meets it twice does not read two accounts of it.
//
// The set is worth diagnosing rather than merely rejecting because a hole in it
// is the tool's OWN normal intermediate state: the seeding panel saves each
// rank the moment it is typed, so an operator who enters seed 4 before seeds 1
// to 3 has {4} on disk and has done nothing wrong yet. "seed ranks must be
// sequential without gaps" restates the rule at them; this names the numbers.
//
// Mirrored in JS by seedGapDiagnosis (web-mobile/js/admin_helpers.jsx), which
// blocks the draw controls before the request is made. Both are pinned to the
// shared golden table in testdata/seed_gap_messages.json.
//
// Returns "" when the ranks are contiguous, when there are no seeds at all, or
// when the fault is anything OTHER than a gap (a duplicate rank, a rank of 0).
// Those the validator already describes precisely, and a caller must pass its
// description through rather than mislabel it as a gap.
func SeedGapDiagnosis(assignments []domain.SeedAssignment) string {
	present := make(map[int]bool, len(assignments))
	highest := 0
	for _, a := range assignments {
		if a.SeedRank <= 0 {
			return ""
		}
		if present[a.SeedRank] {
			return ""
		}
		present[a.SeedRank] = true
		if a.SeedRank > highest {
			highest = a.SeedRank
		}
	}
	missing := []int{}
	for r := 1; r < highest; r++ {
		if !present[r] {
			missing = append(missing, r)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"Seeding is incomplete: seed rank%s %s %s not been set, but rank %d has.",
		Plural(len(missing)), RankList(missing), haveOrHas(len(missing)), highest)
}

func haveOrHas(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}
