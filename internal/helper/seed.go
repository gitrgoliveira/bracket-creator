package helper

import (
	"fmt"

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

	for i := 0; i < len(seeded); i++ {
		for j := i + 1; j < len(seeded); j++ {
			if seeded[j].Seed < seeded[i].Seed {
				seeded[i], seeded[j] = seeded[j], seeded[i]
			}
		}
	}

	power := 1
	for power < len(players) {
		power *= 2
	}

	order := generateBracketOrder(power)

	result := make([]Player, len(players))
	placed := 0

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
			placed++
			delete(seedMap, rank) // Remove placed seeded player to avoid duplication
		}
	}

	unIdx := 0
	for i := 0; i < len(players); i++ {
		if !occupied[i] {
			if unIdx < len(unseeded) {
				result[i] = unseeded[unIdx]
				unIdx++
			} else {
				for _, p := range seeded {
					if _, ok := seedMap[p.Seed]; ok {
						result[i] = p
						delete(seedMap, p.Seed)
						break
					}
				}
			}
		}
	}
	return result
}

// ApplySeeds assigns seeds to the helper players, handling swaps if needed
// Returns an error if an assigned name could not be matched
func ApplySeeds(players []Player, assignments []domain.SeedAssignment) error {
	playerMap := make(map[string]*Player)
	for i := range players {
		playerMap[players[i].Name] = &players[i]
	}

	for _, a := range assignments {
		if p, ok := playerMap[a.Name]; ok {
			var existingPlayer *Player
			for i := range players {
				if players[i].Seed == a.SeedRank && players[i].Name != p.Name {
					existingPlayer = &players[i]
					break
				}
			}

			if existingPlayer != nil {
				existingPlayer.Seed = p.Seed
			}
			p.Seed = a.SeedRank
		} else {
			return fmt.Errorf("seeded participant not found in main list: %s", a.Name)
		}
	}
	return nil
}
