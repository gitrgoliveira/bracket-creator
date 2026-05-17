package state

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

func (s *Store) LoadReservedSlots(compID string) ([]ReservedSlot, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	data, err := s.loadCached(compID, "reserved-slots.json", parseReservedSlotsFile)
	if err != nil {
		return nil, err
	}
	return s.copyReservedSlots(data.([]ReservedSlot)), nil
}

func parseReservedSlotsFile(path string) (any, error) {
	data, err := os.ReadFile(path) // #nosec G304 — path built by compPath which calls filepath.Clean
	if err != nil {
		if os.IsNotExist(err) {
			return []ReservedSlot{}, nil
		}
		return nil, err
	}
	var slots []ReservedSlot
	if err := json.Unmarshal(data, &slots); err != nil {
		return nil, err
	}
	if slots == nil {
		slots = []ReservedSlot{}
	}
	return slots, nil
}

func (s *Store) copyReservedSlots(slots []ReservedSlot) []ReservedSlot {
	if slots == nil {
		return nil
	}
	res := make([]ReservedSlot, len(slots))
	copy(res, slots)
	return res
}

func (s *Store) SaveReservedSlots(compID string, slots []ReservedSlot) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	return s.saveReservedSlotsLocked(compID, slots)
}

// loadReservedSlotsLocked reads reserved slots without acquiring a lock.
// Caller must hold the per-comp lock for compID.
func (s *Store) loadReservedSlotsLocked(compID string) ([]ReservedSlot, error) {
	data, err := parseReservedSlotsFile(s.compPath(compID, "reserved-slots.json"))
	if err != nil {
		return nil, err
	}
	return data.([]ReservedSlot), nil
}

// saveReservedSlotsLocked writes reserved slots without acquiring a lock and
// warms the file cache. Caller must hold the per-comp write lock for compID.
func (s *Store) saveReservedSlotsLocked(compID string, slots []ReservedSlot) error {
	path := s.compPath(compID, "reserved-slots.json")
	data, err := json.MarshalIndent(slots, "", "  ")
	if err != nil {
		return err
	}
	if err := s.atomicWrite(path, data, 0600); err != nil {
		return err
	}
	if slots == nil {
		slots = []ReservedSlot{}
	}
	cache := s.getFileCache(compID, "reserved-slots.json")
	cache.mu.Lock()
	cache.data = s.copyReservedSlots(slots)
	cache.mtime = s.FileMtime(compID, "reserved-slots.json")
	cache.mu.Unlock()
	return nil
}

// loadParticipantsLocked reads participants without acquiring a lock.
// Caller must hold the per-comp lock for compID. Mirrors LoadParticipants.
func (s *Store) loadParticipantsLocked(compID string, withZekkenName bool) ([]domain.Player, error) {
	path := s.compPath(compID, "participants.csv")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return []domain.Player{}, nil
	}

	lines, err := helper.ReadEntriesFromFile(path)
	if err != nil {
		return nil, err
	}

	hasIDs := len(lines) > 0 && uuidRE(strings.TrimSpace(strings.SplitN(lines[0], ",", 2)[0]))

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

	seeds, _ := helper.ParseSeedsFile(s.compPath(compID, "seeds.csv"))
	if len(seeds) > 0 {
		seedMap := make(map[string]int, len(seeds))
		for _, sd := range seeds {
			seedMap[sd.Name] = sd.SeedRank
		}
		for i := range players {
			if seed, ok := seedMap[players[i].Name]; ok {
				players[i].Seed = seed
			}
		}
	}

	// helper.Player is a type alias for domain.Player (NFR-007); the
	// parser output is already []domain.Player.
	return players, nil
}

// saveParticipantsLocked writes participants without acquiring a lock and
// invalidates the participant caches. Caller must hold the per-comp write lock
// for compID. Mirrors SaveParticipants.
func (s *Store) saveParticipantsLocked(compID string, players []domain.Player) error {
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
	if err := s.atomicWrite(path, []byte(sb.String()), 0600); err != nil {
		return err
	}
	for _, key := range []string{"participants.csv", "participants_with_seeds.csv"} {
		cache := s.getFileCache(compID, key)
		cache.mu.Lock()
		cache.data = nil
		cache.mtime = 0
		cache.mu.Unlock()
	}
	return nil
}

// AddReservedSlot creates a placeholder participant and a reserved-slot entry
// linking it to sourceCompID at the given rank.  It returns the new slot.
func (s *Store) AddReservedSlot(compID string, sourceCompID string, sourceRank int, withZekkenName bool) (*ReservedSlot, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	slots, err := s.loadReservedSlotsLocked(compID)
	if err != nil {
		return nil, err
	}
	for _, sl := range slots {
		if sl.SourceCompID == sourceCompID && sl.SourceRank == sourceRank {
			return &sl, nil
		}
	}

	slotID := newParticipantID()
	partID := newParticipantID()

	placeholder := domain.Player{
		ID:          partID,
		Name:        fmt.Sprintf("Reserved: %s rank %d", sourceCompID, sourceRank),
		DisplayName: fmt.Sprintf("Rsv %s #%d", sourceCompID, sourceRank),
		Dojo:        "TBD",
		Tag:         "reserved",
	}

	players, err := s.loadParticipantsLocked(compID, withZekkenName)
	if err != nil {
		return nil, err
	}
	players = append(players, placeholder)
	if err := s.saveParticipantsLocked(compID, players); err != nil {
		return nil, err
	}

	slot := ReservedSlot{
		ID:            slotID,
		ParticipantID: partID,
		SourceCompID:  sourceCompID,
		SourceRank:    sourceRank,
	}
	slots = append(slots, slot)
	if err := s.saveReservedSlotsLocked(compID, slots); err != nil {
		return nil, err
	}

	return &slot, nil
}

// RemoveReservedSlot deletes a slot and its placeholder participant.
func (s *Store) RemoveReservedSlot(compID string, slotID string, withZekkenName bool) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	slots, err := s.loadReservedSlotsLocked(compID)
	if err != nil {
		return err
	}

	var partID string
	var remaining []ReservedSlot
	for _, sl := range slots {
		if sl.ID == slotID {
			partID = sl.ParticipantID
		} else {
			remaining = append(remaining, sl)
		}
	}
	if partID == "" {
		return fmt.Errorf("reserved slot %s not found", slotID)
	}

	if err := s.saveReservedSlotsLocked(compID, remaining); err != nil {
		return err
	}

	players, err := s.loadParticipantsLocked(compID, withZekkenName)
	if err != nil {
		return err
	}
	filtered := make([]domain.Player, 0, len(players))
	for _, p := range players {
		if p.ID != partID {
			filtered = append(filtered, p)
		}
	}
	return s.saveParticipantsLocked(compID, filtered)
}
