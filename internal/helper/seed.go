package helper

import (
	"fmt"
	"sort"

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
// Unlike StandardSeeding (which returns a dense len(players) slice and leaves the
// caller to pad byes at the end of the leaf array), the byes here are interleaved
// at their standard positions: a bracket of N players in a 2^k draw gives the top
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
	return result
}

// PoolSeeding reorders players for pool distribution so that top seeds land
// in pools that are appropriately spread across the given number of courts.
//
// It assigns each seed to a court by seedCourtOrder (D6) and uses a per-court
// priority to ensure correct bracket placement (e.g., top and bottom of the
// court's bracket) after the pools are deinterleaved by ReorderPoolsForCourts.
//
// Placement is keyed on each player's RANK, never on its position among the
// seeded players, and the set handed in does NOT have to be contiguous: seeds
// {1, 3, 4} place rank 3 in rank 3's quarter, leaving rank 2's empty. That is
// the same promise StandardSeedingFull makes, and it is what engine.SeedWarnings
// reports against, so a caller that renumbers a gapped set before calling would
// silently move seeds. engine.dropSeedAssignments produces exactly such a set
// when a seeded competitor does not check in.
//
// numCourts must be the count the DRAW will run on (helper.EffectiveDrawCourts),
// not the operator's raw allocation: it is the modulus the spread is computed
// against, and the pool deinterleave and pool-to-shiaijo allocation have to
// agree with it.
func PoolSeeding(players []Player, numPools int, numCourts int) []Player {
	if numPools <= 0 {
		return players
	}
	// Both ends, through the one owner: numCourts is the spread modulus below.
	numCourts = clampCourts(numCourts)

	seeded := make([]Player, 0)
	unseeded := make([]Player, 0)

	for _, p := range players {
		if p.Seed > 0 {
			seeded = append(seeded, p)
		} else {
			unseeded = append(unseeded, p)
		}
	}

	// Sort seeded players by their Seed rank
	sort.SliceStable(seeded, func(i, j int) bool {
		return seeded[i].Seed < seeded[j].Seed
	})

	// Cluster unseeded players by dojo (largest groups first) so that players
	// from the same dojo occupy consecutive result slots. Consecutive slots map
	// to distinct start-pool indices mod numPools, preventing forceSameDojo
	// fallback from landing same-dojo players in the same pool.
	dojoCount := make(map[string]int)
	for _, p := range unseeded {
		dojoCount[p.Dojo]++
	}
	sort.SliceStable(unseeded, func(i, j int) bool {
		ci, cj := dojoCount[unseeded[i].Dojo], dojoCount[unseeded[j].Dojo]
		if ci != cj {
			return ci > cj
		}
		if unseeded[i].Dojo != unseeded[j].Dojo {
			return unseeded[i].Dojo < unseeded[j].Dojo
		}
		return false
	})

	// Determine how many pools are assigned to each court
	courtPoolCounts := make([]int, numCourts)
	for i := 0; i < numPools; i++ {
		courtPoolCounts[i%numCourts]++
	}

	// Generate priority for each court
	courtPriorities := make([][]int, numCourts)
	for c := 0; c < numCourts; c++ {
		courtPriorities[c] = generatePoolPriority(courtPoolCounts[c])
	}

	// We want to interleave players such that CreatePools (which fills linearly)
	// puts them in the correct pools.
	result := make([]Player, len(players))
	occupied := make(map[int]bool)

	// Assign seeded players based on court-aware priority order.
	for _, p := range seeded {
		// si is the seed's RANK minus one, NOT its position in the sorted
		// list. The two coincide for a contiguous set 1..N, but the set that
		// reaches here can be GAPPED: engine.dropSeedAssignments removes the
		// assignments of seeds who did not check in, after the validating load
		// has already run, and the survivors keep their raw ranks (e.g.
		// {1, 3, 4}). Reading the position would then place rank 3 in rank 2's
		// quarter -- and helper.SeedPlacementWarnings, which reads the RANK,
		// would report the resulting spread as a configuration the operator
		// chose. Both derived quantities below read the rank space, so the two
		// stay in the same space as the warnings.
		si := p.Seed - 1
		// global pool rank (0 to numPools-1). Pool rank r lands on court
		// r%numCourts (the deinterleave ReorderPoolsForCourts applies), so
		// targeting a rank whose court is seedCourtOrder's is what puts the
		// seed in D6's half and quarter.
		poolRank := seedPoolRank(si, numPools, numCourts)
		posInPool := si / numPools // which slot within the pool

		placed := false
		for offset := 0; offset < numPools && !placed; offset++ {
			// calculate the court and local pool index for (poolRank+offset)
			currentRank := (poolRank + offset) % numPools
			courtIdx := currentRank % numCourts
			posInCourt := currentRank / numCourts

			var globalPoolIdx int
			if courtPoolCounts[courtIdx] > 0 {
				localPoolIdx := courtPriorities[courtIdx][posInCourt%courtPoolCounts[courtIdx]]
				globalPoolIdx = localPoolIdx*numCourts + courtIdx
			} else {
				// Fallback if a court has 0 pools (shouldn't happen if numCourts <= numPools)
				globalPoolIdx = currentRank
			}

			targetIdx := posInPool*numPools + globalPoolIdx
			if targetIdx < len(players) && !occupied[targetIdx] {
				result[targetIdx] = p
				occupied[targetIdx] = true
				placed = true
			}
		}
		if !placed {
			// Last resort: take the first available slot.
			for j := 0; j < len(players); j++ {
				if !occupied[j] {
					result[j] = p
					occupied[j] = true
					break
				}
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

	return result
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

	playerMap := make(map[string]*Player, len(players))
	nameCount := make(map[string]int, len(players))
	for i := range players {
		playerMap[domain.SeedKey(players[i].Name, players[i].Dojo)] = &players[i]
		nameCount[players[i].Name]++
	}

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
		p, ok := playerMap[domain.SeedKey(titleName, a.Dojo)]
		if !ok && a.Dojo == "" && nameCount[titleName] == 1 {
			for i := range players {
				if players[i].Name == titleName {
					p = &players[i]
					ok = true
					break
				}
			}
		}
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
