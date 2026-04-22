package helper

import (
	"fmt"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

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

// StandardSeeding reorders players into bracket positions such that seeded participants (Seed > 0)
// are spaced according to tournament standards (e.g., #1 and #2 on opposite halves).
// Unseeded players fill the remaining slots.
func StandardSeeding(players []Player) []Player {
	seeded := make([]Player, 0)
	unseeded := make([]Player, 0)

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

	// Handle displaced seeds (those whose rank position was out of range)
	// Place them in unoccupied slots furthest from already placed seeds to ensure distribution.
	// Tie-break: when multiple slots share the maximum minimum-distance, the highest index wins.
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
// in pools that are as far apart as possible.
//
// Distribution priority (see generatePoolPriority): both extremes first, then
// the inner midpoints, then a recursive midpoint of the remaining gaps. For
// example, with 4 pools the priority is [0, 3, 1, 2] and seeds 1..4 are
// assigned to pools 0, 3, 1, 2 in that order. Seeds beyond numPools wrap to
// the next "row" using the same priority.
//
// The returned slice is laid out so that calling CreatePools (which fills
// pools linearly) places each seed in the intended pool.
func PoolSeeding(players []Player, numPools int) []Player {
	if numPools <= 0 {
		return players
	}

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

	priority := generatePoolPriority(numPools)

	// We want to interleave players such that CreatePools (which fills linearly)
	// puts them in the correct pools.
	// Order: [Pool_0_Pos0, Pool_1_Pos0, ..., Pool_N-1_Pos0, Pool_0_Pos1, ...]
	result := make([]Player, len(players))
	occupied := make(map[int]bool)

	// Assign seeded players based on priority order. If the preferred slot is
	// taken (or out of range), step through the priority ring at the same
	// position-in-pool to keep pool distribution balanced before falling back
	// to a linear scan.
	for i, p := range seeded {
		poolRank := i % numPools
		posInPool := i / numPools

		placed := false
		for offset := 0; offset < numPools && !placed; offset++ {
			poolIdx := priority[(poolRank+offset)%numPools]
			targetIdx := posInPool*numPools + poolIdx
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

func generatePoolPriority(n int) []int {
	if n <= 0 {
		return []int{}
	}
	if n == 1 {
		return []int{0}
	}

	priority := []int{0, n - 1}
	if n > 2 {
		priority = append(priority, (n-1)/2, n/2)
	}

	// Deduplicate if n is small (e.g. n=3, (3-1)/2=1, 3/2=1)
	seen := make(map[int]bool)
	unique := make([]int, 0)
	for _, p := range priority {
		if !seen[p] {
			unique = append(unique, p)
			seen[p] = true
		}
	}
	priority = unique

	// Recursive splitting for remaining gaps
	for len(priority) < n {
		bestGap := -1
		bestStart := -1

		// Find largest gap between existing priority points
		sorted := make([]int, len(priority))
		copy(sorted, priority)
		sort.Ints(sorted)

		// Check gap between sorted points
		for i := 0; i < len(sorted)-1; i++ {
			gap := sorted[i+1] - sorted[i]
			if gap > bestGap {
				bestGap = gap
				bestStart = sorted[i]
			}
		}

		if bestGap > 1 {
			mid := bestStart + bestGap/2
			priority = append(priority, mid)
			seen[mid] = true
		} else {
			// No more gaps > 1, just fill remaining linearly
			for i := 0; i < n; i++ {
				if !seen[i] {
					priority = append(priority, i)
					seen[i] = true
				}
			}
		}
	}

	return priority
}

// ApplySeeds assigns seeds to the helper players, handling swaps if needed
// Returns an error if an assigned name could not be matched
func ApplySeeds(players []Player, assignments []domain.SeedAssignment) error {
	playerMap := make(map[string]*Player, len(players))
	for i := range players {
		playerMap[players[i].Name] = &players[i]
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

		p, ok := playerMap[a.Name]
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
