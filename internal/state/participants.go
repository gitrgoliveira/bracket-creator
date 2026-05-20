package state

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// ErrParticipantNotFound is returned by UpdateParticipant when the pid is not in the roster.
var ErrParticipantNotFound = errors.New("participant not found")

// LoadParticipantsOpts controls optional behavior in LoadParticipants.
type LoadParticipantsOpts struct {
	WithSeeds bool  // set false to skip the seeds.csv read (hot list paths)
	HasIDs    *bool // nil = auto-detect from first line; non-nil uses cached Competition.HasParticipantIDs
}

// LoadParticipants loads participants with seeds merged (default behavior).
func (s *Store) LoadParticipants(compID string, withZekkenName bool) ([]domain.Player, error) {
	return s.loadParticipants(compID, withZekkenName, LoadParticipantsOpts{WithSeeds: true})
}

// LoadParticipantsOpt loads participants with configurable options.
func (s *Store) LoadParticipantsOpt(compID string, withZekkenName bool, opts LoadParticipantsOpts) ([]domain.Player, error) {
	return s.loadParticipants(compID, withZekkenName, opts)
}

func (s *Store) loadParticipants(compID string, withZekkenName bool, opts LoadParticipantsOpts) ([]domain.Player, error) {
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()
	return s.loadParticipantsNoLock(compID, withZekkenName, opts)
}

func (s *Store) loadParticipantsNoLock(compID string, withZekkenName bool, opts LoadParticipantsOpts) ([]domain.Player, error) {
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
		p := cache.data.([]domain.Player)
		res := make([]domain.Player, len(p))
		copy(res, p)
		cache.mu.RUnlock()
		return res, nil
	}
	cache.mu.RUnlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Re-check after acquiring write lock
	if cache.data != nil && cache.mtime == mtime {
		p := cache.data.([]domain.Player)
		res := make([]domain.Player, len(p))
		copy(res, p)
		return res, nil
	}

	path := s.compPath(compID, "participants.csv")
	lines, err := helper.ReadEntriesFromFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cache.data = []domain.Player{}
			cache.mtime = mtime
			return []domain.Player{}, nil
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
	var checkedInFlags []bool
	var plainLines []string

	for _, line := range lines {
		isCheckedIn := false

		// Robust column-based detection: strip the UUID prefix first so
		// the threshold is applied to the data columns only (per Copilot
		// review). After stripping the ID the minimum valid data row is
		// "Name,Dojo" (2 parts), so checked_in is only treated as a
		// marker when at least 3 data parts are present (Name, Dojo,
		// checked_in).
		//
		// Known limitation: a dojo literally named "checked_in" in a
		// zekken competition that also has a distinct DisplayName column
		// produces "Name, DisplayName, checked_in" (3 data parts) after
		// UUID strip, which the threshold cannot distinguish from the
		// legitimate "Name, Dojo, checked_in" row. Resolving this
		// ambiguity without a format version or column header is not
		// possible; in practice no real dojo uses this name.
		line = strings.TrimSpace(line)

		// Strip UUID prefix for threshold calculation only (idLine is
		// what remains after the ID field).
		idLine := line
		if hasIDs {
			if _, rest, ok := strings.Cut(line, ","); ok {
				idLine = strings.TrimSpace(rest)
			}
		}
		dataParts := strings.Split(idLine, ",")
		if len(dataParts) > 2 {
			last := strings.TrimSpace(dataParts[len(dataParts)-1])
			if strings.ToLower(last) == "checked_in" {
				isCheckedIn = true
				// Strip from the full original line (preserves UUID prefix).
				if li := strings.LastIndex(line, ","); li >= 0 {
					line = strings.TrimRight(line[:li], " ")
				}
			}
		}

		if hasIDs {
			id, rest, ok := strings.Cut(line, ",")
			if !ok {
				plainLines = append(plainLines, line)
				ids = append(ids, "")
				checkedInFlags = append(checkedInFlags, isCheckedIn)
				continue
			}
			ids = append(ids, strings.TrimSpace(id))
			plainLines = append(plainLines, rest)
			checkedInFlags = append(checkedInFlags, isCheckedIn)
		} else {
			plainLines = append(plainLines, line)
			checkedInFlags = append(checkedInFlags, isCheckedIn)
		}
	}

	players, err := helper.CreatePlayers(plainLines, withZekkenName)
	if err != nil {
		return nil, err
	}

	for i := range players {
		if hasIDs && i < len(ids) {
			players[i].ID = ids[i]
		}
		if i < len(checkedInFlags) {
			players[i].CheckedIn = checkedInFlags[i]
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
	// helper.Player is a type alias for domain.Player (NFR-007); the
	// parser output can flow straight into the cache without conversion.
	cache.data = players
	cache.mtime = mtime

	res := make([]domain.Player, len(players))
	copy(res, players)
	return res, nil
}

func (s *Store) SaveParticipants(compID string, players []domain.Player) error {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	return s.saveParticipantsNoLock(compID, players)
}

// UpdateParticipant atomically loads the participant list, applies transform
// to the target player, and persists the result. Used to avoid TOCTOU races
// on concurrent check-ins.
func (s *Store) UpdateParticipant(compID string, pid string, withZekkenName bool, transform func(p *domain.Player) error) (*domain.Player, error) {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	// Seeds are not merged here — check-in mutations don't need seed data.
	players, err := s.loadParticipantsNoLock(compID, withZekkenName, LoadParticipantsOpts{WithSeeds: false})
	if err != nil {
		return nil, err
	}

	foundIdx := -1
	for i := range players {
		if players[i].ID == pid {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		return nil, ErrParticipantNotFound
	}

	if err := transform(&players[foundIdx]); err != nil {
		return nil, err
	}

	if err := s.saveParticipantsNoLock(compID, players); err != nil {
		return nil, err
	}

	return &players[foundIdx], nil
}

func (s *Store) saveParticipantsNoLock(compID string, players []domain.Player) error {
	path := s.compPath(compID, "participants.csv")

	var sb strings.Builder
	for _, p := range players {
		id := p.ID
		if id == "" {
			id = newParticipantID()
		}
		// Only write the 3-column form when DisplayName carries information
		// beyond what helper.SanitizeName would derive from Name on load.
		// Writing the auto-derived form would corrupt non-zekken loads:
		// LoadParticipants(_, withZekkenName=false) reads column 2 as Dojo
		// and pushes the real Dojo into Metadata. See the round-trip
		// regression test in participants_test.go.
		var row string
		if p.DisplayName != "" && p.DisplayName != helper.SanitizeName(p.Name) {
			row = fmt.Sprintf("%s, %s, %s", p.Name, p.DisplayName, p.Dojo)
		} else {
			row = fmt.Sprintf("%s, %s", p.Name, p.Dojo)
		}
		if p.Tag != "" {
			row += ", " + p.Tag
		}
		if p.CheckedIn {
			row += ", checked_in"
		}
		sb.WriteString(id + ", " + row + "\n")
	}

	if err := s.atomicWrite(path, []byte(sb.String()), 0600); err != nil {
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
