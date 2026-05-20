package state

import (
	"encoding/csv"
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
	records, err := helper.ReadCSVFile(path)
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
		hasIDs = len(records) > 0 && len(records[0]) > 0 && uuidRE(strings.TrimSpace(records[0][0]))
	}

	var ids []string
	var checkedInFlags []bool
	var playerRecords [][]string

	for _, record := range records {
		isCheckedIn := false

		// Determine data fields (everything after UUID if present).
		// Per-record UUID check: only strip the first field as an ID
		// when it actually matches the UUID pattern.
		dataStart := 0
		if hasIDs && len(record) > 0 && uuidRE(strings.TrimSpace(record[0])) {
			dataStart = 1
		}
		dataFields := record[dataStart:]

		// Skip records that are empty after UUID stripping (e.g. a
		// UUID-only row) — CreatePlayersFromRecords would skip these
		// too, and the metadata slices must stay aligned.
		allEmpty := true
		for _, f := range dataFields {
			if strings.TrimSpace(f) != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			continue
		}

		// Detect and strip checked_in marker from the last data field.
		// Minimum valid data row is "Name,Dojo" (2 parts), so checked_in
		// is only treated as a marker when at least 3 data parts are present.
		if len(dataFields) > 2 {
			last := strings.TrimSpace(dataFields[len(dataFields)-1])
			if strings.ToLower(last) == "checked_in" {
				isCheckedIn = true
				dataFields = dataFields[:len(dataFields)-1]
			}
		}

		if hasIDs {
			id := ""
			if dataStart > 0 {
				id = strings.TrimSpace(record[0])
			}
			ids = append(ids, id)
		}
		playerRecords = append(playerRecords, dataFields)
		checkedInFlags = append(checkedInFlags, isCheckedIn)
	}

	players, err := helper.CreatePlayersFromRecords(playerRecords, withZekkenName)
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

	oldName := players[foundIdx].Name
	oldDojo := players[foundIdx].Dojo

	if err := transform(&players[foundIdx]); err != nil {
		return nil, err
	}

	if err := s.saveParticipantsNoLock(compID, players); err != nil {
		return nil, err
	}

	// Update seeds.csv if name changes (under same lock to avoid deadlocks)
	if oldName != players[foundIdx].Name {
		seedsPath := s.compPath(compID, "seeds.csv")
		seeds, err := helper.ParseSeedsFile(seedsPath)
		if err == nil {
			changed := false
			for i := range seeds {
				if seeds[i].Name == oldName {
					seeds[i].Name = players[foundIdx].Name
					if seeds[i].Dojo != "" && seeds[i].Dojo == oldDojo {
						seeds[i].Dojo = players[foundIdx].Dojo
					}
					changed = true
				}
			}
			if changed {
				var sb strings.Builder
				sb.WriteString("Rank,Name\n")
				for _, a := range seeds {
					fmt.Fprintf(&sb, "%d,%s\n", a.SeedRank, a.Name)
				}
				_ = s.atomicWrite(seedsPath, []byte(sb.String()), 0600)
			}
		}
	}

	return &players[foundIdx], nil
}

// marshalParticipantsCSV serialises players into RFC 4180 CSV bytes.
// Shared by saveParticipantsNoLock and saveParticipantsLocked so the
// display-name and checked_in logic is defined once.
func marshalParticipantsCSV(players []domain.Player) ([]byte, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	for _, p := range players {
		id := p.ID
		if id == "" {
			id = newParticipantID()
		}
		// Only write the 3-column form when DisplayName carries information
		// beyond what helper.SanitizeName would derive from Name on load.
		// Writing the auto-derived form would corrupt non-zekken loads:
		// LoadParticipants(_, withZekkenName=false) reads column 2 as Dojo
		// and pushes the real Dojo into Metadata.
		var record []string
		if p.DisplayName != "" && p.DisplayName != helper.SanitizeName(p.Name) {
			record = []string{id, p.Name, p.DisplayName, p.Dojo}
		} else {
			record = []string{id, p.Name, p.Dojo}
		}
		if p.Tag != "" {
			record = append(record, p.Tag)
		}
		if p.CheckedIn {
			record = append(record, "checked_in")
		}
		if err := w.Write(record); err != nil {
			return nil, fmt.Errorf("writing participant CSV record: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flushing participant CSV: %w", err)
	}
	return []byte(sb.String()), nil
}

// AddParticipant atomically loads the participant list, assigns a sequential ID compID-pX,
// sets PoolPosition, appends the participant, and saves the file.
func (s *Store) AddParticipant(compID string, p domain.Player, withZekkenName bool) (*domain.Player, error) {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	players, err := s.loadParticipantsNoLock(compID, withZekkenName, LoadParticipantsOpts{})
	if err != nil {
		return nil, err
	}

	usedIds := make(map[string]bool)
	for _, pl := range players {
		if pl.ID != "" {
			usedIds[pl.ID] = true
		}
	}

	nextSlot := 1
	for {
		id := fmt.Sprintf("%s-p%d", compID, nextSlot)
		if !usedIds[id] {
			p.ID = id
			break
		}
		nextSlot++
	}

	p.PoolPosition = int64(len(players))
	if p.DisplayName == "" {
		p.DisplayName = helper.SanitizeName(p.Name)
	}

	players = append(players, p)

	if err := s.saveParticipantsNoLock(compID, players); err != nil {
		return nil, err
	}

	return &p, nil
}

func (s *Store) saveParticipantsNoLock(compID string, players []domain.Player) error {
	path := s.compPath(compID, "participants.csv")

	data, err := marshalParticipantsCSV(players)
	if err != nil {
		return err
	}

	if err := s.atomicWrite(path, data, 0600); err != nil {
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
