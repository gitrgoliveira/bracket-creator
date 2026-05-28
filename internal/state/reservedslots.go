package state

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// loadParticipantsLocked reads participants (with seeds merged) without
// acquiring a lock. Caller must hold the per-comp lock for compID.
//
// mp-p7n / Copilot PR #185 round-9 follow-up: this used to be a
// hand-rolled copy of loadParticipantsNoLock's parse loop, but the copy
// only had the auto-detect (uuidRE-on-row-0) path — it never consulted
// Competition.HasParticipantIDs. That meant a roster with non-UUID ids
// (the JS-side `${compID}-p${N}` shape) loaded here would column-shift
// (id→Name, Name→Dojo, Dojo→Metadata) and, because AddReservedSlot /
// RemoveReservedSlot load→modify→save the whole roster, the shift would
// be PERSISTED. Delegating to the canonical loadParticipantsNoLock
// (WithSeeds:true, no HasIDs hint → it consults HasParticipantIDs and
// keeps the per-record uuidRE fallback for mixed legacy files) fixes
// the divergence in one place. Both functions share the same
// "caller-holds-the-per-comp-lock" contract, so this is a safe
// substitution; the only added behaviour is caching, which is keyed by
// parse mode + config.md mtime and invalidated on every write.
func (s *Store) loadParticipantsLocked(compID string, withZekkenName bool) ([]domain.Player, error) {
	return s.loadParticipantsNoLock(compID, withZekkenName, LoadParticipantsOpts{WithSeeds: true})
}

// saveParticipantsLocked writes participants without acquiring a lock and
// invalidates the participant caches. Caller must hold the per-comp write lock
// for compID.
//
// mp-p7n / Copilot PR #185 round-9 follow-up: delegates to
// saveParticipantsNoLock so the marshal + write + cache-invalidation
// logic lives in exactly one place. The pre-delegation copy invalidated
// only 2 of the 6 parse-mode cache-key variants (the same stale-key bug
// fixed in saveParticipantsNoLock at round-6), which could leave a
// hinted-mode cache entry stale after a reserved-slot add/remove.
func (s *Store) saveParticipantsLocked(compID string, players []domain.Player, withZekkenName bool) error {
	return s.saveParticipantsNoLock(compID, players, withZekkenName)
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
	if err := s.saveParticipantsLocked(compID, players, withZekkenName); err != nil {
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
	return s.saveParticipantsLocked(compID, filtered, withZekkenName)
}
