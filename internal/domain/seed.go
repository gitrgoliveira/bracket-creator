package domain

import (
	"errors"
)

// SeedAssignment represents the mapping of a previous winner to a seed position.
type SeedAssignment struct {
	Name     string
	SeedRank int
}

// Validate checks if the seed assignment is valid.
func (s *SeedAssignment) Validate() error {
	if s.SeedRank <= 0 {
		return errors.New("seed rank must be greater than 0")
	}
	if s.Name == "" {
		return errors.New("name cannot be empty")
	}
	return nil
}

// ValidateAssignments checks a list for duplicate seed ranks, valid properties, and gapless sequences.
func ValidateAssignments(assignments []SeedAssignment) error {
	seen := make(map[int]bool)
	maxRank := 0

	for _, a := range assignments {
		if err := a.Validate(); err != nil {
			return err
		}
		if seen[a.SeedRank] {
			return errors.New("duplicate seed rank detected")
		}
		seen[a.SeedRank] = true
		if a.SeedRank > maxRank {
			maxRank = a.SeedRank
		}
	}

	if len(seen) > 0 && len(seen) != maxRank {
		return errors.New("seed ranks must be sequential without gaps")
	}

	return nil
}

// AssignSeeds applies valid seed assignments to a list of players
// It swaps seeds if a collision occurs. Returns error if a seeded participant is not found.
func AssignSeeds(players []Player, assignments []SeedAssignment) error {
	// Build map for quick lookup by name
	playerMap := make(map[string]*Player)
	for i := range players {
		playerMap[players[i].Name] = &players[i]
	}

	for _, a := range assignments {
		if p, ok := playerMap[a.Name]; ok {
			// Check if seed rank is already taken by another player
			var existingPlayer *Player
			for i := range players {
				if players[i].Seed == a.SeedRank && players[i].Name != p.Name {
					existingPlayer = &players[i]
					break
				}
			}

			// If taken, swap their seeds
			if existingPlayer != nil {
				existingPlayer.Seed = p.Seed
			}

			// Assign new seed rank
			p.Seed = a.SeedRank
		} else {
			return errors.New("seeded participant not found in main list: " + a.Name)
		}
	}
	return nil
}
