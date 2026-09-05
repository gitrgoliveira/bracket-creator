package helper

import (
	"fmt"
	"math/bits"
	"sort"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// generateBracketOrder returns the slot indices for a standard tournament
// bracket of size n (must be a power of 2).  The recursion ensures that:
//   - seeds 1 and n are placed in opposite halves of the draw, so they can
//     only meet in the final;
//   - seeds 2 and (n-1) are placed in opposite halves within their halves,
//     so they can only meet in the semis;
//   - and so on recursively.
//
// Example (n=4): returns [1, 4, 2, 3], meaning slot 0 gets seed 1,
// slot 1 gets seed 4, slot 2 gets seed 2, slot 3 gets seed 3.
func generateBracketOrder(n int) []int {
	if n <= 1 {
		return []int{1}
	}
	half := generateBracketOrder(n / 2)
	res := make([]int, n)
	for i, val := range half {
		res[i*2] = val
		res[i*2+1] = n - val + 1
	}
	return res
}

// partitionSeeded splits players into a seeded group (Seed > 0, stable-sorted by
// Seed ascending) and an unseeded group (Seed == 0, in original input order).
// Shared by StandardSeeding and StandardSeedingFull.
func partitionSeeded(players []Player) (seeded, unseeded []Player) {
	for _, p := range players {
		if p.Seed > 0 {
			seeded = append(seeded, p)
		} else {
			unseeded = append(unseeded, p)
		}
	}
	sort.SliceStable(seeded, func(i, j int) bool {
		return seeded[i].Seed < seeded[j].Seed
	})
	return seeded, unseeded
}

// StandardSeedingFull places players into a FULL power-of-two bracket and returns
// a slice of length NextPow2(len(players)), or nil for an empty input. Each
// player is positioned by its seeding RANK via generateBracketOrder, and the
// surplus high-rank slots are left as zero-value Players (empty Name), i.e. byes.
//
// Rank assignment matches StandardSeeding (and the Excel draw): a seeded player
// (Seed > 0) claims its Seed NUMBER as its rank, so an operator who assigns
// non-contiguous seeds (e.g. {1, 2, 5}) gets the #5 seed at the rank-5 bracket
// position, not the third-from-top. Genuine unseeded players fill the remaining
// ranks 1..N in input order; any seed whose number is out of range or collides
// is then treated as unseeded too (appended after the genuine unseeded players,
// so a displaced seed's exact slot is unspecified; these are degenerate inputs).
//
// Unlike StandardSeeding (which returns a dense len(players) slice; a caller
// that needs the real bracket geometry runs it through CreateBalancedTree and
// TreeToLeafArray, whose recursive per-side padding does NOT put every bye at
// the tail of the leaf array -- see dojoMeetRound's own doc comment), the
// byes here are interleaved at their standard positions: a bracket of N
// players in a 2^k draw gives the top
// 2^k, N seeds a first-round bye, and because every bye rank pairs with a
// distinct low (top-seed) rank in round 1, the draw never contains an
// empty-vs-empty match. Used by the live-playoffs leaf builder so the knockout
// tree matches conventional seeding instead of clustering all byes at the bottom.
func StandardSeedingFull(players []Player) []Player {
	if len(players) == 0 {
		return nil
	}

	seeded, unseeded := partitionSeeded(players)
	n := len(players)

	// rankToPlayer maps a 1-based seeding rank to its player. Seeded players claim
	// their Seed number; out-of-range (Seed > n) or colliding seeds fall back to
	// the unseeded pool. Unseeded then fill the remaining ranks 1..n in order.
	rankToPlayer := make(map[int]Player, n)
	for _, p := range seeded {
		_, taken := rankToPlayer[p.Seed]
		if p.Seed >= 1 && p.Seed <= n && !taken {
			rankToPlayer[p.Seed] = p
		} else {
			unseeded = append(unseeded, p) // displaced seed → treat as unseeded
		}
	}
	ui := 0
	for rank := 1; rank <= n && ui < len(unseeded); rank++ {
		if _, taken := rankToPlayer[rank]; !taken {
			rankToPlayer[rank] = unseeded[ui]
			ui++
		}
	}

	power := NextPow2(n)
	order := generateBracketOrder(power) // order[slot] = seeding rank at that slot

	result := make([]Player, power)
	for slot, rank := range order {
		if p, ok := rankToPlayer[rank]; ok {
			result[slot] = p
		}
		// rank not assigned (rank > n) → leave zero-value Player (a bye)
	}
	return result
}

// StandardSeeding reorders players into bracket positions such that seeded participants (Seed > 0)
// are spaced according to tournament standards (e.g., #1 and #2 on opposite halves).
// Unseeded players fill the remaining slots.
func StandardSeeding(players []Player) []Player {
	seeded, unseeded := partitionSeeded(players)

	power := 1
	for power < len(players) {
		power *= 2
	}

	order := generateBracketOrder(power)

	result := make([]Player, len(players))

	// Map seed rank to Player
	seedMap := make(map[int]*Player)
	for i := range seeded {
		seedMap[seeded[i].Seed] = &seeded[i]
	}

	occupied := make(map[int]bool)
	for i, rank := range order {
		if i >= len(players) {
			continue
		}
		if p, ok := seedMap[rank]; ok {
			result[i] = *p
			occupied[i] = true
			delete(seedMap, rank) // Remove placed seeded player to avoid duplication
		}
	}

	// Handle displaced seeds: seeds whose natural bracket position (determined
	// by generateBracketOrder) exceeds len(players) because the bracket size
	// is rounded up to the nearest power of 2 but there are fewer actual
	// participants.  Each displaced seed is placed in the unoccupied slot that
	// maximises the nearest-neighbour distance to all already-placed seeds.
	// Tie-break: when multiple empty slots share the same maximum distance,
	// the highest-index slot wins, which keeps the bracket visually consistent
	// when several seeds are displaced.
	if len(seedMap) > 0 {
		// Collect remaining seeds in rank order (seeded is already sorted by Seed).
		remainingSeeds := make([]Player, 0, len(seedMap))
		for _, p := range seeded {
			if _, ok := seedMap[p.Seed]; ok {
				remainingSeeds = append(remainingSeeds, p)
			}
		}

		// Maintain a sorted slice of occupied indices to compute nearest distance in O(log n).
		occupiedIdx := make([]int, 0, len(occupied))
		for i := range occupied {
			occupiedIdx = append(occupiedIdx, i)
		}
		sort.Ints(occupiedIdx)

		nearestDist := func(i int) int {
			if len(occupiedIdx) == 0 {
				return len(players)
			}
			pos := sort.SearchInts(occupiedIdx, i)
			best := len(players)
			if pos < len(occupiedIdx) {
				if d := occupiedIdx[pos] - i; d < best {
					best = d
				}
			}
			if pos > 0 {
				if d := i - occupiedIdx[pos-1]; d < best {
					best = d
				}
			}
			return best
		}

		for _, p := range remainingSeeds {
			bestSlot := -1
			maxDist := -1

			for i := 0; i < len(players); i++ {
				if occupied[i] {
					continue
				}
				d := nearestDist(i)
				if d > maxDist {
					maxDist = d
					bestSlot = i
				} else if d == maxDist {
					bestSlot = i
				}
			}

			if bestSlot != -1 {
				result[bestSlot] = p
				occupied[bestSlot] = true
				insertPos := sort.SearchInts(occupiedIdx, bestSlot)
				occupiedIdx = append(occupiedIdx, 0)
				copy(occupiedIdx[insertPos+1:], occupiedIdx[insertPos:])
				occupiedIdx[insertPos] = bestSlot
				delete(seedMap, p.Seed)
			}
		}
	}

	unIdx := 0
	for i := 0; i < len(players); i++ {
		if !occupied[i] {
			if unIdx < len(unseeded) {
				result[i] = unseeded[unIdx]
				unIdx++
			}
		}
	}
	delayDojoMeetings(result, occupied)
	return result
}

// delayDojoMeetings swaps UNSEEDED competitors so that two members of one dojo
// meet as LATE in the knockout as the draw allows, and in particular never in
// the first round.
//
// Why it is needed: unseeded competitors fill bracket slots in roster order and
// CreateBalancedTree pairs adjacent slots, so a roster entered dojo by dojo --
// which is how operators paste one -- opens with dojo-mate against dojo-mate.
// Measured before this existed: 16 competitors from four dojos of four gave
// EIGHT first-round matches, every one intra-dojo.
//
// Not first is only half the rule. Two competitors from one dojo placed in the
// same half still meet in the semi-final when the bracket could have held them
// apart until the final, so this maximises the round in which each dojo's
// members first meet rather than merely pushing them out of round one.
//
// It is a repair pass rather than a smarter fill: reordering the unseeded list
// up front would reorder EVERY draw, including the ones with no dojo collision
// at all, changing published brackets for no benefit. This only moves
// competitors when doing so strictly improves the meeting rounds, so a roster
// with nothing to fix is returned byte-identical.
//
// Seeded slots are never moved, on EITHER side of a swap: their placement is
// the seeding contract, and a dojo is not a reason to break it. Two seeds from
// one dojo therefore keep whatever pairing their ranks produced.
func delayDojoMeetings(result []Player, occupied map[int]bool) {
	// slots translates every DENSE index this function works in into the
	// real, padded tree-slot space dojoMeetRound's XOR arithmetic requires
	// (bc-drwx item 1; see denseSlotMap's and dojoMeetRound's own doc
	// comments). Built once: the mapping is a property of len(result) alone
	// and does not change as players are swapped between dense positions.
	slots := denseSlotMap(len(result))

	// keys[i] is dojoKey(result[i].Dojo) -- built ONCE here rather than
	// recomputed inside every comparison (bc-drwx review fix: the original
	// item-3 fix called dojoKey/NormalizeParticipantName fresh inside
	// sortedSameDojoPairs, bestRelocation and dojoSumMeetRounds' pairScore,
	// which measured 25x-200x slower than origin/main -- NormalizeParticipantName
	// does real work (NFD decompose, strip combining marks, re-NFC, lowercase,
	// whitespace collapse), and those functions called it on the SAME O(N)
	// dojo strings over and over inside O(N^2)-O(N^3) loops. keys is kept in
	// lockstep with result: whenever result[i]/result[j] are swapped (the
	// accepted swap below, and dojoSwapGain's own temporary swap-and-revert),
	// keys[i]/keys[j] are swapped too, so it is always exactly
	// dojoKey(result[i].Dojo) without ever calling dojoKey again.
	keys := make([]string, len(result))
	for i := range result {
		keys[i] = dojoKey(result[i].Dojo)
	}

	movable := func(i int) bool {
		return !occupied[i] && result[i].Name != "" && result[i].Dojo != ""
	}

	// Early-out (bc-drwx item 2): a swap only ever happens between two
	// movable slots of DIFFERENT dojos (dojoSwapGain's own candidate filter,
	// below), so with fewer than two distinct dojos among the movable
	// (unseeded) players NO swap can EVER exist, whatever the roster size --
	// the whole climb below is provably a no-op before it examines a single
	// pair. A single-dojo roster (or one the CLI's legacy no-dojo-column
	// parser defaulted every blank dojo to "NA") used to pay for that
	// discovery the slow way: see this function's own "Performance note"
	// below for the O(N^4) it used to cost. This check is O(N).
	movableDojos := map[string]bool{}
	for i := range result {
		if movable(i) {
			movableDojos[keys[i]] = true
			if len(movableDojos) >= 2 {
				break
			}
		}
	}
	if len(movableDojos) < 2 {
		return
	}

	// A hill climb on the total of every same-dojo pair's meeting round: the
	// higher the total, the later dojo-mates meet. A first-round pairing
	// scores the minimum, so removing one is always the largest single gain
	// available, which is why "never first" falls out of maximising this
	// rather than needing a rule of its own.
	//
	// Performance note (bc-drwx items 2 and 3, COMBINED). Item 2: the
	// ORIGINAL shape of this climb re-scanned every same-dojo pair (O(N^2))
	// on every outer iteration to find the CURRENT worst one, excluding one
	// PAIR at a time when it turned out unfixable. That is fine when swaps
	// land often, but a roster with few or no cross-dojo swap PARTNERS --
	// the early-out above's exact target, plus shapes like two large dojos
	// and nothing else -- can have O(N^2) same-dojo pairs ALL turn out
	// unfixable before the climb gives up, each one re-paying that O(N^2)
	// rescan: O(N^4) overall.
	//
	// A prior version of this note measured item 2's O(N^2)->O(N^2 log N)
	// selection rewrite alone, on a build that predated item 3's dojoKey/
	// NormalizeParticipantName spelling-insensitive dojo matching. That
	// build never shipped: once item 3 landed, dojoKey was called fresh
	// inside this very selection loop (sortedSameDojoPairs/bestRelocation/
	// dojoSumMeetRounds' pairScore), which cost 25x-200x on its own until
	// this session's own review fix hoisted it into `keys` above. The
	// numbers below are the two fixes TOGETHER, measured on this machine,
	// origin/main (neither fix) vs. this file as committed (both fixes),
	// single run each, BenchmarkStandardSeeding_*:
	//   single-dojo,    64 entrants: (no origin/main equivalent -- this
	//     benchmark was added alongside the fix) -> 61.5us
	//   single-dojo,   128 entrants: (no origin/main equivalent) -> 128.7us
	//   single-dojo,   256 entrants: (no origin/main equivalent) -> 254.5us
	//     (all three confirm the early-out above is an O(N) no-op
	//     regardless of N, which is the one property this shape pins)
	//   2 dojos of 128 (256 entrants): 12.24s    -> 889ms    (~13.8x)
	//   2 dojos of 96 + 64 singletons: 6.13s     -> 2.30s    (~2.7x -- the
	//     one shape that does not reach the sub-1s target: many singleton
	//     partners mean many small accepted swaps, so many generations each
	//     re-pay the O(N^2 log N) sort; still no longer O(N^4))
	//   16 dojos of 16 (256 entrants): 1.39s     -> 1.00s    (~1.4x)
	//   32 dojos of 8  (256 entrants): 1.02s     -> 907ms    (~1.1x)
	//   16 dojos of 8  (128 entrants): 133ms     -> 113ms    (~1.2x)
	//   32 dojos of 4  (128 entrants): 111ms     -> 102ms    (~1.1x)
	//   16 dojos of 4   (64 entrants): 14.0ms    -> 13.6ms   (roughly
	//     unchanged)
	//   32 dojos of 2   (64 entrants): 11.1ms    -> 11.0ms   (roughly
	//     unchanged: few enough pairs that the rescan was already cheap)
	//
	// This rewrite reduces the SELECTION cost from O(N^2) per stuck pair to
	// one O(N^2 log N) sort per GENERATION (the span between accepted
	// swaps), without changing which pair the climb examines or in what
	// order: pairs are sorted by meeting round once, using a STABLE sort
	// over a list built in (i ascending, j ascending) order, so ties at the
	// same round keep the exact scan-discovery order the original nested
	// loop's strict '<' comparison gave them. The list is then walked once,
	// worst round first; a pair is "excluded" simply by not being revisited
	// this generation, which is equivalent to the original's explicit
	// pairKey exclusion set because bestRelocation's answer for a slot never
	// changes within a generation (dojoSwapGain is a pure function of the
	// slot and the current `result`, unaffected by which pair asked) -- the
	// per-slot memo below (slotBest) is exactly what made that reuse safe
	// before this rewrite too, this only removes the now-redundant rescan
	// that used to sit around it. The first pair with a beneficial
	// relocation ends the generation immediately (swap, re-sort, go again),
	// matching the original's "accept the first improving swap found while
	// scanning worst-first" behaviour exactly.
	for iter := 0; iter < len(result)*len(result); iter++ {
		pairs := sortedSameDojoPairs(result, keys, slots)
		if len(pairs) == 0 {
			return // no selectable dojo pair remains: nothing left to delay
		}

		// slotBest memoizes bestRelocation's result (the best (gain, y)
		// found scanning every candidate partner for one slot) for the life
		// of THIS generation -- see the doc comment above for why that
		// reuse is safe. Rebuilt fresh every generation, exactly as before.
		type slotCandidate struct{ gain, y int }
		slotBest := map[int]slotCandidate{}
		bestRelocation := func(x int) (gain, y int) {
			if c, ok := slotBest[x]; ok {
				return c.gain, c.y
			}
			gain, y = 0, -1
			if movable(x) {
				for cand := range result {
					if cand == x || !movable(cand) || keys[cand] == keys[x] {
						continue
					}
					if g := dojoSwapGain(result, keys, slots, x, cand); g > gain {
						gain, y = g, cand
					}
				}
			}
			slotBest[x] = slotCandidate{gain, y}
			return gain, y
		}

		swapped := false
		for _, p := range pairs {
			// Try relocating either member of this pair. Accept the swap
			// that improves the overall total by the most; ties keep the
			// earlier candidate so the result does not depend on scan
			// order. bestRelocation(p.i) is evaluated (and, on a cache
			// hit, answered) strictly before bestRelocation(p.j), and only
			// a STRICT '>' replaces the running best, so p.j tying p.i's
			// gain never displaces it -- the exact order and tie-break the
			// pre-rewrite double loop produced.
			bestGain, bestX, bestY := 0, -1, -1
			for _, x := range []int{p.i, p.j} {
				if gain, y := bestRelocation(x); gain > bestGain {
					bestGain, bestX, bestY = gain, x, y
				}
			}
			if bestGain <= 0 {
				// This pair is stuck: move on to the next-worst pair in
				// sorted order, rather than abandoning every other dojo's
				// meeting untouched.
				continue
			}
			result[bestX], result[bestY] = result[bestY], result[bestX]
			keys[bestX], keys[bestY] = keys[bestY], keys[bestX]
			swapped = true
			break
		}
		if !swapped {
			return // every same-dojo pair in this generation is stuck
		}
		// The landscape changed for every pair, stuck or not: the next
		// generation's sortedSameDojoPairs call (top of the next outer
		// iteration) gives it a fresh look, exactly as the original's reset
		// `excluded`/`slotBest` did.
	}
}

// dojoMeetPair is one same-dojo pair of DENSE indices i<j, together with the
// real-tree meeting round (bc-drwx item 1) they are currently drawn to meet
// in -- sortedSameDojoPairs' own output, and delayDojoMeetings' unit of work
// for one generation of its hill climb.
type dojoMeetPair struct{ i, j, round int }

// sortedSameDojoPairs lists every unseeded, non-blank, same-dojo pair of
// DENSE indices in `result`, worst (earliest) meeting round first. Pairs are
// appended in (i ascending, j ascending) order -- the same order the
// pre-rewrite nested loop scanned them in -- and then STABLY sorted by
// round, so pairs tied at the same round keep that exact relative order:
// this is what makes delayDojoMeetings' single sorted pass reproduce the
// original repeated-rescan's worst-first, first-found-on-a-tie selection
// exactly, just without repaying the O(N^2) scan once per stuck pair (see
// delayDojoMeetings' own "Performance note").
func sortedSameDojoPairs(result []Player, keys []string, slots []int) []dojoMeetPair {
	var pairs []dojoMeetPair
	for i := range result {
		if result[i].Name == "" || result[i].Dojo == "" {
			continue
		}
		for j := i + 1; j < len(result); j++ {
			if result[j].Name == "" || keys[i] != keys[j] {
				continue
			}
			pairs = append(pairs, dojoMeetPair{i, j, dojoMeetRound(slots[i], slots[j])})
		}
	}
	sort.SliceStable(pairs, func(a, b int) bool { return pairs[a].round < pairs[b].round })
	return pairs
}

// dojoMeetRound returns the bracket round in which slots i and j would meet,
// counting the first round as 1. Two slots meet in the smallest sub-branch
// holding both, and CreateBalancedTree builds those sub-branches by repeatedly
// halving, so the round is the position of the highest bit in which the two
// slot numbers differ: adjacent slots (differing only in bit 0) meet in round
// 1, slots in opposite halves meet in the last round.
//
// i and j MUST already be real, padded TREE SLOT numbers -- the space
// TreeToLeafArray/SlotArray produce over a CreateBalancedTree, where every
// junction's two sides have already been padded to a common power of two
// before being concatenated (see TreeToLeafArray's own doc comment). A bare
// StandardSeeding DENSE index (0..len(players)-1, no padding at all) is NOT
// that space for a non-power-of-two player count: CreateBalancedTree's
// recursion splits the leaf list in half at every level rather than padding
// byes onto the tail, so dense-index adjacency does not correspond to real
// tree-leaf adjacency once any level of the recursion is uneven. Calling this
// directly on dense indices silently scores pairs that are not real matches
// and misses real round-1 pairs (bc-drwx item 1) -- delayDojoMeetings uses
// denseSlotMap to translate before ever reaching this function.
func dojoMeetRound(i, j int) int {
	// Slot numbers are indexes into the draw, so the XOR is non-negative and
	// the conversion below cannot wrap. Checked rather than asserted, both to
	// say so to a reader and because gosec cannot see it (G115).
	d := i ^ j
	if d <= 0 {
		return 0
	}
	return bits.Len(uint(d))
}

// denseSlotMap maps a StandardSeeding DENSE index (0..n-1, no padding) to the
// real, padded knockout leaf slot that entrant lands on in the tree every
// production consumer actually builds from that same dense array
// (cmd/create-playoffs.go, internal/engine/bracket.go,
// internal/engine/playoff_skeleton.go all run
// CreateBalancedTree(namesInDenseOrder)). It builds a tree over placeholder
// labels ("0".."n-1") the identical way -- CreateBalancedTree, then
// TreeToLeafArray to reproduce that tree's real, per-level-padded slot
// geometry -- and reads back which slot each dense index landed on, which is
// the one mapping dojoMeetRound needs to XOR on the tree's actual geometry
// rather than the dense index space it used to be handed directly (see
// dojoMeetRound's own doc comment). Computed ONCE per delayDojoMeetings call
// (the mapping never changes across a swap: swapping the PLAYERS at two dense
// positions does not change which slot either position maps to), never
// per-pair.
func denseSlotMap(n int) []int {
	if n <= 0 {
		return nil
	}
	labels := make([]string, n)
	for i := range labels {
		labels[i] = strconv.Itoa(i)
	}
	leaves := TreeToLeafArray(CreateBalancedTree(labels))
	slots := make([]int, n)
	for slot, label := range leaves {
		if label == "" {
			continue // bye slot: no dense index maps here
		}
		idx, err := strconv.Atoi(label)
		if err != nil {
			continue // unreachable: labels are always "0".."n-1"
		}
		slots[idx] = slot
	}
	return slots
}

// dojoSumMeetRounds totals the meeting round of every same-dojo pair that
// includes slot x or slot y (each such pair counted exactly once, including
// the {x, y} pair itself when both are members of the same dojo). Only
// slots x and y can change when those two are swapped, so dojoSwapGain --
// its sole caller, always with x != y -- never needs the sum over every
// OTHER pair in the draw: those are unaffected by the swap and would cancel
// out of the before/after delta anyway. Walking only the pairs that touch
// {x, y} turns this from an O(N^2) whole-draw scan into O(N) per call, an
// ~100x cut on THIS function alone at N=256 (measured), which is what made
// the O(N) candidates * O(N) dojoSwapGain = O(N^2) candidate-confirmation
// scan in delayDojoMeetings affordable per call. That confirmation scan
// remained delayDojoMeetings' own dominant cost afterwards (it recurs once
// per outer iteration, and wave-1's stuck-pair continuation can run many
// iterations per accepted swap) until the wave-2 slotBest memo cached it
// per slot for the life of a generation. See delayDojoMeetings' own
// "Performance note" for the current measured cost breakdown and
// end-to-end numbers -- kept in that ONE place rather than restated here,
// since a previous version of this note tried to restate it and drifted
// into misattributing the dominant cost to the wrong sub-loop.
//
// x, y and every index this walks are DENSE indices into result; slots is
// denseSlotMap(len(result)), translating each pair to real tree-slot space
// before it reaches dojoMeetRound (bc-drwx item 1).
func dojoSumMeetRounds(result []Player, keys []string, slots []int, x, y int) int {
	pairScore := func(i, j int) int {
		if result[i].Name == "" || result[j].Name == "" || result[i].Dojo == "" {
			return 0
		}
		if keys[i] != keys[j] {
			return 0
		}
		return dojoMeetRound(slots[i], slots[j])
	}
	sum := 0
	for j := range result {
		if j == x {
			continue
		}
		sum += pairScore(x, j)
	}
	if y == x {
		return sum
	}
	// The {x, y} pair itself was already scored above (j == y in the loop
	// over x); skip both here so it is never counted twice.
	for j := range result {
		if j == y || j == x {
			continue
		}
		sum += pairScore(y, j)
	}
	return sum
}

// dojoSwapGain reports how much later same-dojo competitors would meet if the
// occupants of slots x and y traded places. Positive means an improvement.
// x, y are DENSE indices; slots is denseSlotMap(len(result)) (see
// dojoSumMeetRounds' own doc comment).
func dojoSwapGain(result []Player, keys []string, slots []int, x, y int) int {
	before := dojoSumMeetRounds(result, keys, slots, x, y)
	result[x], result[y] = result[y], result[x]
	keys[x], keys[y] = keys[y], keys[x]
	after := dojoSumMeetRounds(result, keys, slots, x, y)
	result[x], result[y] = result[y], result[x]
	keys[x], keys[y] = keys[y], keys[x]
	return after - before
}

// PoolSeeding, its sortUnseededByDojoCluster helper and its
// placeSeedsForPools helper were removed as dead code (bc-drwx item 11): no
// production caller has reached them since bc-dojo Phase 4 made
// BuildPoolPhase delegate to the tree-aware distributor
// (buildPoolPhaseTreeAwareCore, pool_distribution_tree_aware.go) instead of
// PoolSeeding -> CreatePools -> ReorderPoolsForCourts. placeSeedIndices
// below is the one piece of that trio that IS still live -- it is what the
// tree-aware distributor's own seed placement (buildPoolPhaseTreeAwareCore)
// calls -- and was kept exactly as it was; the many pre-existing tests that
// used to call PoolSeeding directly now call referencePoolSeeding
// (pool_distribution_gate_test.go), a test-only reconstruction of
// PoolSeeding's exact former body built from placeSeedIndices, so they keep
// pinning the same properties under their new name.

// placeSeedIndices computes, for each seeded player in `seeded` (already
// sorted by Seed rank ascending, as partitionSeeded returns it), the index it
// occupies in PoolSeeding's permuted roster: the same court-aware
// seedPoolRank/seedCourtOrder/generatePoolPriority arithmetic PoolSeeding has
// always used, returned as a slice PARALLEL to `seeded` rather than folded
// into a sparse array, so a caller does not have to recover seed order from
// map iteration (which Go leaves unspecified).
//
// It is stateful in the same way PoolSeeding's own loop is: a later seed's
// placement avoids whatever index an earlier seed already claimed
// (`occupied`), so processing order matters and must stay `seeded`'s order.
//
// numCourts must already be clamped by the caller (clampCourts); the live
// caller, buildPoolPhaseTreeAwareCore (pool_distribution_tree_aware.go),
// clamps once before calling this. totalLen is the FULL roster length (every
// player, not just the seeded ones) -- a seed's target index is computed
// against that whole slot space.
func placeSeedIndices(seeded []Player, numPools, numCourts, totalLen int) []int {
	// -1 means "never placed" (only reachable when there are more seeds than
	// roster slots, i.e. totalLen has no room left at all): placeSeedsForPools
	// must skip those rather than default them to index 0, which would
	// silently overwrite whatever legitimately occupies slot 0.
	indices := make([]int, len(seeded))
	for i := range indices {
		indices[i] = -1
	}
	if numPools <= 0 || numCourts <= 0 {
		return indices
	}

	// Determine how many pools are assigned to each court.
	courtPoolCounts := make([]int, numCourts)
	for i := 0; i < numPools; i++ {
		courtPoolCounts[i%numCourts]++
	}

	// Generate priority for each court.
	courtPriorities := make([][]int, numCourts)
	for c := 0; c < numCourts; c++ {
		courtPriorities[c] = generatePoolPriority(courtPoolCounts[c])
	}

	occupied := make(map[int]bool, len(seeded))

	// poolSeedDojos tracks, per pool, the (normalized) dojos of every seed
	// already placed there -- bc-drwx item 4. A WRAPPED seed (rankIdx >=
	// numPools, i.e. beyond D6's own half/quarter structure) has no further
	// halves/quarters to relax and seedPoolRank's out-of-range fallback
	// (`rankIdx % numPools`) gives it the EXACT SAME remainder as the
	// unwrapped rank it wraps onto (rank 5 and rank 1 both land on
	// remainder 0 at numPools=4), so without this it silently doubles up on
	// whichever pool that coincidence points at -- even when the doubled-up
	// seed is a DOJO-MATE of the seed already there and a different,
	// equally valid pool was available. See dojoNode/pass loop below.
	poolSeedDojos := make([]map[string]bool, numPools)
	for i := range poolSeedDojos {
		poolSeedDojos[i] = map[string]bool{}
	}

	for si, p := range seeded {
		// si is p's POSITION in `seeded` (this function's own output index);
		// rankIdx is p's RANK minus one, which is what the placement
		// arithmetic below actually keys on. The two coincide for a
		// contiguous seed set 1..N but not for a gapped one (see
		// seedCourtOrder's doc comment), so they must never be conflated.
		rankIdx := p.Seed - 1
		// global pool rank (0 to numPools-1). Pool rank r lands on court
		// r%numCourts (the deinterleave ReorderPoolsForCourts applies), so
		// targeting a rank whose court is seedCourtOrder's is what puts the
		// seed in D6's half and quarter.
		poolRank := seedPoolRank(rankIdx, numPools, numCourts)
		posInPool := rankIdx / numPools // which slot within the pool

		// candidateGlobalPool is the natural arithmetic's own answer for a
		// given offset (0..numPools-1) -- unchanged from before this fix,
		// just factored out so both the new wrapped-seed passes below and
		// the original single pass can share it.
		candidateGlobalPool := func(offset int) int {
			currentRank := (poolRank + offset) % numPools
			courtIdx := currentRank % numCourts
			posInCourt := currentRank / numCourts
			if courtPoolCounts[courtIdx] > 0 {
				localPoolIdx := courtPriorities[courtIdx][posInCourt%courtPoolCounts[courtIdx]]
				return localPoolIdx*numCourts + courtIdx
			}
			// Fallback if a court has 0 pools (shouldn't happen if numCourts <= numPools)
			return currentRank
		}

		tryPlace := func(globalPoolIdx int) bool {
			targetIdx := posInPool*numPools + globalPoolIdx
			if targetIdx < totalLen && !occupied[targetIdx] {
				indices[si] = targetIdx
				occupied[targetIdx] = true
				if globalPoolIdx >= 0 && globalPoolIdx < numPools {
					poolSeedDojos[globalPoolIdx][dojoKey(p.Dojo)] = true
				}
				return true
			}
			return false
		}

		placed := false
		// Wrapped-seed preference passes (posInPool > 0 means rankIdx >=
		// numPools, i.e. this seed is beyond the first full round and would
		// otherwise land wherever the modulo fallback happens to point).
		// Never taken for posInPool == 0 (every seed within the first
		// numPools ranks), so this is a no-op for every roster the
		// pre-existing seed-equality pins already covered.
		if posInPool > 0 && p.Dojo != "" {
			// Pass 0: the first candidate pool (scanned in the SAME
			// rank-priority order the natural arithmetic already defines)
			// that holds no seed at all.
			for offset := 0; offset < numPools && !placed; offset++ {
				gp := candidateGlobalPool(offset)
				if gp < 0 || gp >= numPools || len(poolSeedDojos[gp]) > 0 {
					continue
				}
				placed = tryPlace(gp)
			}
			// Pass 1: no seed-free pool existed (unavoidable once
			// nSeeds > numPools -- every pool already holds exactly one).
			// The first candidate whose existing seed(s) are not a
			// dojo-mate of this one.
			for offset := 0; offset < numPools && !placed; offset++ {
				gp := candidateGlobalPool(offset)
				if gp < 0 || gp >= numPools || poolSeedDojos[gp][dojoKey(p.Dojo)] {
					continue
				}
				placed = tryPlace(gp)
			}
		}
		// The original, unconditional pass: every seed within the first
		// numPools ranks reaches this immediately (posInPool == 0, the two
		// passes above never ran); a wrapped seed only reaches it when
		// BOTH preference passes above found nothing (every pool has a
		// seed AND every one of them is this seed's dojo-mate), the one
		// case genuinely as constrained as this function's pre-fix
		// behaviour always was.
		for offset := 0; offset < numPools && !placed; offset++ {
			placed = tryPlace(candidateGlobalPool(offset))
		}
		if !placed {
			// Last resort: take the first available slot.
			for j := 0; j < totalLen; j++ {
				if !occupied[j] {
					indices[si] = j
					occupied[j] = true
					if numPools > 0 {
						poolSeedDojos[j%numPools][dojoKey(p.Dojo)] = true
					}
					break
				}
			}
		}
	}

	return indices
}

// generatePoolPriority returns an ordering of pool indices (0..n-1) designed
// to spread top seeds across courts as evenly as possible.  The algorithm
// is a recursive bisection:
//
//  1. Start with the two extremes: index 0 and index n-1, so seeds 1 and 2
//     land at opposite ends of the draw.
//  2. Add the two midpoints (⌊(n-1)/2⌋ and ⌈(n-1)/2⌉), placing seeds 3 and 4
//     in the centre of each half.
//  3. Repeatedly find the largest gap between already-placed indices and insert
//     the midpoint of that gap until all n pool indices are assigned.
//  4. When no gap larger than 1 remains, fill any unassigned indices linearly.
//
// Example (n=4): [0, 3, 1, 2]
// Example (n=6): [0, 5, 2, 3, 1, 4]
func generatePoolPriority(n int) []int {
	if n <= 0 {
		return []int{}
	}
	if n == 1 {
		return []int{0}
	}

	priority := make([]int, 0, n)
	seen := make(map[int]bool, n)
	sorted := make([]int, 0, n)

	addPoint := func(v int) {
		if seen[v] {
			return
		}
		seen[v] = true
		priority = append(priority, v)
		pos := sort.SearchInts(sorted, v)
		sorted = append(sorted, 0)
		copy(sorted[pos+1:], sorted[pos:])
		sorted[pos] = v
	}

	addPoint(0)
	addPoint(n - 1)
	if n > 2 {
		addPoint((n - 1) / 2)
		addPoint(n / 2)
	}

	for len(priority) < n {
		bestGap := -1
		bestStart := -1
		for i := 0; i < len(sorted)-1; i++ {
			gap := sorted[i+1] - sorted[i]
			if gap > bestGap {
				bestGap = gap
				bestStart = sorted[i]
			}
		}

		if bestGap > 1 {
			addPoint(bestStart + bestGap/2)
		} else {
			for i := 0; i < n; i++ {
				addPoint(i)
			}
		}
	}

	return priority
}

// seedCourtOrder returns the court seed rank i+1 (0-based i) belongs on, per
// D6: seeds 1 and 3 fall in one HALF of the draw and seeds 2 and 4 in the
// other, each of the four in a distinct QUARTER.
//
// Courts [0, k) are the draw's first half and [k, 2k) its second (k =
// numCourts/2), and within a half the first k/2 courts are one quarter and the
// rest the other, which is exactly how the draw combines regions. Seeds
// alternate halves by parity; within each half the TOP seed of the pair takes
// the INNER quarter (the one adjacent to the draw's centre) and the lower seed
// the outer:
//
//	4 courts: seed 1 -> B, seed 2 -> C, seed 3 -> A, seed 4 -> D
//	2 courts: seeds 1 and 3 -> A, seeds 2 and 4 -> B (two seeded pools per court)
//	1 court:  every seed on the one court; the quarters are inside its region
//
// The inner-quarter order is the EKF's, decoded by rank-matching seeded pools
// against the previous edition's results across six draws and two years (spec
// D6's evidence table): the reigning champion's pool sits on court B and the
// runner-up's on C in every 4-court EKC team draw of 2025 and 2026. The two
// mappings differ in nothing functional -- same halves, same quarters, same
// semifinal pairings -- so the sheets are the only authority there is, and
// they say inner. (The WKC's linear bracket seeds its outer edges instead,
// blocks 1/16/8/9; that geometry belongs to a bracket without court regions
// and is recorded in the spec, not implemented here.)
//
// This deliberately differs from the conventional bracket, which groups seed 4
// with 1 and 3 with 2 and gives semifinals 1 v 4 and 2 v 3. The operator chose
// 1 with 3 and 2 with 4, so the semifinals are 1 v 3 and 2 v 4 when the seeds
// hold. It used to be a plain round robin over courts (seed 1 -> A, 2 -> B,
// 3 -> C, 4 -> D), which put seeds 1 and 2 in the SAME half of a 4-court draw
// and let them meet in a semifinal rather than the final.
//
// Ranks beyond the 4th, and any rank the court count cannot separate, fall back
// to the round robin: there is no further structure to spread them over. The
// operator may set ANY number of seeds, zero included (R1); this is a function
// of one seed's position and never reads the total.
//
// i is the seed's RANK minus one, and every "seed n" above reads it as rank
// n+1. Callers MUST pass p.Seed-1; the seed's INDEX in the rank-sorted list is
// NOT a substitute, because the set reaching the draw is not always contiguous.
//
// domain.ValidateAssignments does reject a gap, but it only guarantees that for
// the operator's INPUT. A gapped set still reaches placement: after that
// validating load, engine.dropSeedAssignments (internal/engine/competition.go)
// removes the assignments of seeded competitors who did not check in, and the
// survivors keep their RAW ranks -- {1, 2, 3, 4} minus a rank-2 no-show is
// {1, 3, 4}. Nothing renumbers or re-validates in between.
//
// Keying on the index there is not a cosmetic slip. Rank 3 would take index 1,
// land in rank 2's quarter, and the draw would no longer be the one those seeds
// describe; helper.SeedPlacementWarnings reads the RANK, so it would then report
// the halves it could not honour as if the operator's configuration, rather than
// the check-in drop, had caused it. Placement and warnings must read one space.
func seedCourtOrder(i, numCourts int) int {
	if numCourts < 2 || i >= 4 {
		return i % numCourts
	}
	k := numCourts / 2
	// Step of one QUARTER inside a half. With a single court per half the two
	// quarters live inside that court's own region, so there is nowhere to step
	// to and seeds 1 and 3 share the court (D6's two-court case; quarter is 0
	// there, which is also why the inner/outer flip below only manifests at
	// four courts and up -- exactly where the sheets are).
	quarter := k / 2
	step := i / 2
	if i%2 == 0 {
		// First half: the top seed of the pair takes the INNER quarter, the
		// last quarter of the half; its partner seed takes the outer.
		step = 1 - step
	}
	court := (i%2)*k + step*quarter
	if court >= numCourts {
		court = numCourts - 1
	}
	return court
}

// seedPoolRank maps a seed to the pool rank whose court is seedCourtOrder's,
// keeping seeds that share a court on different pools of it. Falls back to the
// plain round robin when the derived rank is out of range (fewer pools than
// courts), and the placement loop's own offset search then resolves any
// collision.
//
// i is the seed's RANK minus one, as in seedCourtOrder: a gapped set is
// possible, so a sparse high rank can fall out of range here and take the round
// robin instead. That degrades predictably (D7) and is why the fallback and the
// caller's offset search both stay.
func seedPoolRank(i, numPools, numCourts int) int {
	if numCourts < 2 {
		return i % numPools
	}
	rank := (i/numCourts)*numCourts + seedCourtOrder(i, numCourts)
	if rank >= numPools {
		return i % numPools
	}
	return rank
}

// ApplySeeds assigns seeds to the helper players, handling swaps if needed
// Returns an error if an assigned name could not be matched
func ApplySeeds(players []Player, assignments []domain.SeedAssignment) error {
	c := cases.Title(language.Und, cases.NoLower)

	// NewRosterIndex is the ONE shared implementation of the exact-key /
	// unique-bare-name fallback (see domain.SeedKey and domain.RosterIndex);
	// ApplySeeds title-cases the assignment's name before querying it so a
	// hand-typed "alice cooper" still matches the roster's canonical
	// "Alice Cooper", but the index itself is queried the same way every
	// other matcher in the codebase does.
	roster := domain.NewRosterIndex(players)

	// Build a seed→player reverse index for O(1) collision detection.
	// Only non-zero seeds are tracked.
	seedToPlayer := make(map[int]*Player, len(players))
	for i := range players {
		if players[i].Seed > 0 {
			seedToPlayer[players[i].Seed] = &players[i]
		}
	}

	seenSeeds := make(map[int]string)
	for _, a := range assignments {
		if a.SeedRank > 0 {
			if name, seen := seenSeeds[a.SeedRank]; seen {
				return fmt.Errorf("duplicate seed rank %d assigned to both %s and %s", a.SeedRank, name, a.Name)
			}
			seenSeeds[a.SeedRank] = a.Name
		}

		titleName := c.String(a.Name)
		p, ok := roster.Lookup(titleName, a.Dojo)
		if !ok {
			return fmt.Errorf("seeded participant not found in main list: %s", a.Name)
		}

		oldRank := p.Seed

		// O(1): find whoever currently holds the target rank (excluding p itself)
		var existingPlayer *Player
		if a.SeedRank > 0 {
			if ep := seedToPlayer[a.SeedRank]; ep != nil && ep != p {
				existingPlayer = ep
			}
		}

		// Perform swap and keep the reverse index consistent
		if existingPlayer != nil {
			// existingPlayer surrenders a.SeedRank and takes p's old rank
			delete(seedToPlayer, a.SeedRank)
			existingPlayer.Seed = oldRank
			if oldRank > 0 {
				seedToPlayer[oldRank] = existingPlayer
			}
		} else if oldRank > 0 {
			// No collision: vacate p's current slot
			delete(seedToPlayer, oldRank)
		}

		p.Seed = a.SeedRank
		if a.SeedRank > 0 {
			seedToPlayer[a.SeedRank] = p
		}
	}
	return nil
}
