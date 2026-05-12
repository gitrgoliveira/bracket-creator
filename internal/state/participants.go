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
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()

	// Use a virtual filename for cache to distinguish between with/without seeds.
	cacheKey := "participants.csv"
	if opts.WithSeeds {
		cacheKey = "participants_with_seeds.csv"
	}

	cache := s.getFileCache(compID, cacheKey)
	cache.mu.RLock()
	mtime := s.FileMtime(compID, "participants.csv")
	if opts.WithSeeds {
		mtime += s.FileMtime(compID, "seeds.csv")
	}

	if cache.data != nil && cache.mtime == mtime {
		p := cache.data.([]helper.Player)
		res := make([]helper.Player, len(p))
		copy(res, p)
		cache.mu.RUnlock()
		return res, nil
	}
	cache.mu.RUnlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Re-check after acquiring write lock
	if cache.data != nil && cache.mtime == mtime {
		p := cache.data.([]helper.Player)
		res := make([]helper.Player, len(p))
		copy(res, p)
		return res, nil
	}

	path := s.compPath(compID, "participants.csv")
	lines, err := helper.ReadEntriesFromFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cache.data = []helper.Player{}
			cache.mtime = mtime
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
	cache.data = players
	cache.mtime = mtime

	res := make([]helper.Player, len(players))
	copy(res, players)
	return res, nil
}

func (s *Store) SaveParticipants(compID string, players []helper.Player) error {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	path := s.compPath(compID, "participants.csv")

	var sb strings.Builder
	for _, p := range players {
		id := p.ID
		if id == "" {
			id = newParticipantID()
		}
		var row string
		if p.DisplayName != "" {
			row = fmt.Sprintf("%s, %s, %s", p.Name, p.DisplayName, p.Dojo)
		} else {
			row = fmt.Sprintf("%s, %s", p.Name, p.Dojo)
		}
		if p.Tag != "" {
			row += ", " + p.Tag
		}
		sb.WriteString(id + ", " + row + "\n")
	}

	if err := os.WriteFile(path, []byte(sb.String()), 0600); err != nil {
		return err
	}

	// Invalidate participant caches (with and without seeds) so the next Load
	// sees the fresh data without a disk re-read.
	for _, key := range []string{"participants.csv", "participants_with_seeds.csv"} {
		cache := s.getFileCache(compID, key)
		cache.mu.Lock()
		cache.data = nil
		cache.mtime = 0
		cache.mu.Unlock()
	}

	return nil
}
