package domain

import (
	"errors"
	"fmt"
)

// SeedAssignment represents the mapping of a previous winner to a seed position.
type SeedAssignment struct {
	Name     string `json:"name"`
	Dojo     string `json:"dojo,omitempty"`
	SeedRank int    `json:"seedRank"`
}

// SeedKey is the composite key that identifies a seeded competitor: names are
// not unique within a competition (only same name AND same dojo is rejected),
// so a seed is matched to its participant by the (name, dojo) pair. Exported
// because every producer, matcher and merger of seed assignments must compose
// the pair the same way -- AssignSeeds here, helper.ApplySeeds, and the
// seeds.csv-onto-roster merge in state.loadParticipants all key on it.
//
// Matchers that consult it also share one fallback for legacy rows: an
// assignment with NO dojo matches by bare name, but only when that name is
// unique in the roster. RosterIndex below is the ONE implementation of that
// fallback; every matcher builds one over its roster and calls Lookup rather
// than re-deriving the rule.
func SeedKey(name, dojo string) string {
	return name + "|" + dojo
}

// RosterIndex resolves a seed row's (name, dojo) key against a roster of
// players, implementing the ONE shared fallback described on SeedKey above:
// an exact (name, dojo) match first, and -- ONLY when the row carries no
// dojo -- a bare-name match, but only when that name is unique in the
// roster. An ambiguous bare name (or an exact-key miss with a non-empty
// dojo) resolves to false rather than guessing.
//
// This was previously reimplemented independently in four places (this
// package's AssignSeeds, helper.ApplySeeds, the seeds.csv-onto-roster merge
// in state.loadParticipants, and the legacy dojo-backfill in
// state.upgradeSeedDojosLocked), which is exactly the kind of drift SeedKey's
// doc comment warned about without anything actually shared. All four now
// build one RosterIndex over their roster and call Lookup.
//
// Build once per roster with NewRosterIndex; the returned pointers alias the
// slice passed in, so mutating through them (as AssignSeeds and ApplySeeds
// do) mutates the caller's slice directly.
type RosterIndex struct {
	byKey  map[string]*Player
	byName map[string]*Player // only names unique in the roster
}

// NewRosterIndex builds a RosterIndex over players. players must not be
// reallocated (e.g. via append past its length) while the index is in use;
// the index holds pointers into its backing array.
func NewRosterIndex(players []Player) *RosterIndex {
	nameCount := make(map[string]int, len(players))
	for i := range players {
		nameCount[players[i].Name]++
	}
	idx := &RosterIndex{
		byKey:  make(map[string]*Player, len(players)),
		byName: make(map[string]*Player, len(players)),
	}
	for i := range players {
		idx.byKey[SeedKey(players[i].Name, players[i].Dojo)] = &players[i]
		if nameCount[players[i].Name] == 1 {
			idx.byName[players[i].Name] = &players[i]
		}
	}
	return idx
}

// Lookup resolves (name, dojo) to a roster player using the shared fallback
// documented on RosterIndex: exact key first, then -- only when dojo=="" --
// the unique-bare-name fallback.
func (idx *RosterIndex) Lookup(name, dojo string) (*Player, bool) {
	if p, ok := idx.byKey[SeedKey(name, dojo)]; ok {
		return p, true
	}
	if dojo == "" {
		if p, ok := idx.byName[name]; ok {
			return p, true
		}
	}
	return nil, false
}

// ErrInvalidSeedAssignments marks every rejection below as a complaint about
// the OPERATOR'S INPUT rather than a failure of the tool.
//
// Callers need the distinction because a seed list arrives from two places that
// fail differently: a --seeds CSV the operator wrote by hand, and seeds.csv,
// which the app writes from its own seeding panel. Reading either can also fail
// for real I/O reasons, and the two must not be reported alike. Without a
// sentinel the mobile app answered a mistyped seed rank with HTTP 500, which
// tells an operator mid-event that the tool is broken when the fix is one
// number in a form.
var ErrInvalidSeedAssignments = errors.New("invalid seed assignments")

// Validate checks if the seed assignment is valid.
func (s *SeedAssignment) Validate() error {
	if s.SeedRank <= 0 {
		return fmt.Errorf("%w: seed rank must be greater than 0", ErrInvalidSeedAssignments)
	}
	if s.Name == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalidSeedAssignments)
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
			return fmt.Errorf("%w: duplicate seed rank detected", ErrInvalidSeedAssignments)
		}
		seen[a.SeedRank] = true
		if a.SeedRank > maxRank {
			maxRank = a.SeedRank
		}
	}

	if len(seen) > 0 && len(seen) != maxRank {
		return fmt.Errorf("%w: seed ranks must be sequential without gaps", ErrInvalidSeedAssignments)
	}

	return nil
}

// AssignSeeds applies valid seed assignments to a list of players
// It swaps seeds if a collision occurs. Returns error if a seeded participant is not found.
func AssignSeeds(players []Player, assignments []SeedAssignment) error {
	roster := NewRosterIndex(players)

	// Build a seed→player reverse index for O(1) collision detection.
	// Only non-zero seeds are tracked.
	seedToPlayer := make(map[int]*Player, len(players))
	for i := range players {
		if players[i].Seed > 0 {
			seedToPlayer[players[i].Seed] = &players[i]
		}
	}

	for _, a := range assignments {
		p, ok := roster.Lookup(a.Name, a.Dojo)
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

		// Assign the new rank to p
		p.Seed = a.SeedRank
		if a.SeedRank > 0 {
			seedToPlayer[a.SeedRank] = p
		}
	}
	return nil
}
