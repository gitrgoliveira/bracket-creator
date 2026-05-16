package state

import (
	"bytes"
	"encoding/json"
	"os"
)

type Overrides struct {
	PoolRanks map[string]map[string]int `json:"poolRanks"` // PoolID -> PlayerName -> Rank
	Winners   map[string]string         `json:"winners"`   // MatchID -> WinnerName
}

func (s *Store) LoadOverrides(compID string) (*Overrides, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadOverridesLocked(compID)
}

func (s *Store) SaveOverrides(compID string, o *Overrides) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveOverridesLocked(compID, o)
}

// loadOverridesLocked reads overrides without acquiring the mutex.
// Caller must hold at least s.mu.RLock.
func (s *Store) loadOverridesLocked(compID string) (*Overrides, error) {
	data, err := os.ReadFile(s.compPath(compID, "overrides.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return &Overrides{
				PoolRanks: make(map[string]map[string]int),
				Winners:   make(map[string]string),
			}, nil
		}
		return nil, err
	}
	var o Overrides
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, err
	}
	if o.PoolRanks == nil {
		o.PoolRanks = make(map[string]map[string]int)
	}
	if o.Winners == nil {
		o.Winners = make(map[string]string)
	}
	return &o, nil
}

// saveOverridesLocked writes overrides without acquiring the mutex.
// Caller must hold s.mu.Lock.
func (s *Store) saveOverridesLocked(compID string, o *Overrides) error {
	if err := os.MkdirAll(s.compPath(compID), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(s.compPath(compID, "overrides.json"), data, 0600)
}

// modifyOverridesChanged loads, mutates, and saves overrides under a single
// write lock, reporting whether the marshalled content changed.
func (s *Store) modifyOverridesChanged(compID string, fn func(*Overrides)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.loadOverridesLocked(compID)
	if err != nil {
		return false, err
	}
	// Snapshot before mutation for comparison (compact marshal; indent is cosmetic).
	before, err := json.Marshal(o)
	if err != nil {
		return false, err
	}
	fn(o)
	after, err := json.Marshal(o)
	if err != nil {
		return false, err
	}
	if bytes.Equal(before, after) {
		return false, nil
	}
	return true, s.saveOverridesLocked(compID, o)
}

// modifyOverrides loads, mutates, and saves overrides under a single write lock,
// eliminating the Load(RLock) → mutate → Save(Lock) lost-update window.
func (s *Store) modifyOverrides(compID string, fn func(*Overrides)) error {
	_, err := s.modifyOverridesChanged(compID, fn)
	return err
}

// SaveRankOverrideChanged saves the rank override and reports whether the
// overrides file actually changed. Use this to gate broadcasts.
func (s *Store) SaveRankOverrideChanged(compID, poolID, playerName string, rank int) (bool, error) {
	return s.modifyOverridesChanged(compID, func(o *Overrides) {
		if o.PoolRanks[poolID] == nil {
			o.PoolRanks[poolID] = make(map[string]int)
		}
		o.PoolRanks[poolID][playerName] = rank
	})
}

func (s *Store) SaveRankOverride(compID, poolID, playerName string, rank int) error {
	_, err := s.SaveRankOverrideChanged(compID, poolID, playerName, rank)
	return err
}

func (s *Store) SaveWinnerOverride(compID, matchID, winnerName string) error {
	return s.modifyOverrides(compID, func(o *Overrides) {
		o.Winners[matchID] = winnerName
	})
}

// ResetOverridesChanged clears all overrides and reports whether the file changed
// (false when overrides were already empty).
func (s *Store) ResetOverridesChanged(compID string) (bool, error) {
	return s.modifyOverridesChanged(compID, func(o *Overrides) {
		o.PoolRanks = make(map[string]map[string]int)
		o.Winners = make(map[string]string)
	})
}

func (s *Store) ResetOverrides(compID string) error {
	_, err := s.ResetOverridesChanged(compID)
	return err
}
