package state

import (
	"fmt"
	"os"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// LoadParticipantsOpts controls optional behavior in LoadParticipants.
type LoadParticipantsOpts struct {
	WithSeeds bool  // set false to skip the seeds.csv read (hot list paths)
	HasIDs    *bool // nil = auto-detect from first line; non-nil uses cached Competition.HasParticipantIDs
}

// LoadParticipants loads participants with seeds merged (default behavior).
func (s *Store) LoadParticipants(compID string, withZekkenName bool) ([]helper.Player, error) {
	return s.loadParticipants(compID, withZekkenName, LoadParticipantsOpts{WithSeeds: true})
}

// LoadParticipantsOpt loads participants with configurable options.
func (s *Store) LoadParticipantsOpt(compID string, withZekkenName bool, opts LoadParticipantsOpts) ([]helper.Player, error) {
	return s.loadParticipants(compID, withZekkenName, opts)
}

func (s *Store) loadParticipants(compID string, withZekkenName bool, opts LoadParticipantsOpts) ([]helper.Player, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.compPath(compID, "participants.csv")
	lines, err := helper.ReadEntriesFromFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []helper.Player{}, nil
		}
		return nil, err
	}

	// Detect new format: first field is a UUID. Use cached flag if provided.
	var hasIDs bool
	if opts.HasIDs != nil {
		hasIDs = *opts.HasIDs
	} else {
		hasIDs = len(lines) > 0 && uuidRE(strings.TrimSpace(strings.SplitN(lines[0], ",", 2)[0]))
	}

	var ids []string
	var plainLines []string
	if hasIDs {
		for _, line := range lines {
			id, rest, ok := strings.Cut(line, ",")
			if !ok {
				plainLines = append(plainLines, line)
				ids = append(ids, "")
				continue
			}
			ids = append(ids, strings.TrimSpace(id))
			plainLines = append(plainLines, rest)
		}
	} else {
		plainLines = lines
	}

	players, err := helper.CreatePlayers(plainLines, withZekkenName)
	if err != nil {
		return nil, err
	}

	if hasIDs {
		for i := range players {
			if i < len(ids) {
				players[i].ID = ids[i]
			}
		}
	}

	if opts.WithSeeds {
		// Merge seeds if they exist.
		seeds, _ := helper.ParseSeedsFile(s.compPath(compID, "seeds.csv"))
		if len(seeds) > 0 {
			seedMap := make(map[string]int)
			for _, sd := range seeds {
				seedMap[sd.Name] = sd.SeedRank
			}
			for i := range players {
				if seed, ok := seedMap[players[i].Name]; ok {
					players[i].Seed = seed
				}
			}
		}
	}

	return players, nil
}

func (s *Store) SaveParticipants(compID string, players []helper.Player) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.compPath(compID, "participants.csv")
	var sb strings.Builder
	for _, p := range players {
		id := p.ID
		if id == "" {
			id = newParticipantID()
		}
		var row string
		if p.DisplayName != "" && p.DisplayName != p.Name {
			row = fmt.Sprintf("%s, %s, %s", p.Name, p.DisplayName, p.Dojo)
		} else {
			row = fmt.Sprintf("%s, %s", p.Name, p.Dojo)
		}
		if p.Tag != "" {
			row += ", " + p.Tag
		}
		sb.WriteString(id + ", " + row + "\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0600)
}
