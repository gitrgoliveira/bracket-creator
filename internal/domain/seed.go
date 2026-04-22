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

	for _, a := range assignments {
		p, ok := playerMap[a.Name]
		if !ok {
			return errors.New("seeded participant not found in main list: " + a.Name)
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

		// Assign the new rank to p
		p.Seed = a.SeedRank
		if a.SeedRank > 0 {
			seedToPlayer[a.SeedRank] = p
		}
	}
	return nil
}
