package state

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	path := filepath.Join(s.folder, "competitions", compID, "overrides.json")
	data, err := os.ReadFile(filepath.Clean(path))
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
	dir := filepath.Join(s.folder, "competitions", compID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "overrides.json")
	data, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Clean(path), data, 0600)
}

// modifyOverrides loads, mutates, and saves overrides under a single write lock,
// eliminating the Load(RLock) → mutate → Save(Lock) lost-update window.
func (s *Store) modifyOverrides(compID string, fn func(*Overrides)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, err := s.loadOverridesLocked(compID)
	if err != nil {
		return err
	}
	fn(o)
	return s.saveOverridesLocked(compID, o)
}

func (s *Store) SaveRankOverride(compID, poolID, playerName string, rank int) error {
	return s.modifyOverrides(compID, func(o *Overrides) {
		if o.PoolRanks[poolID] == nil {
			o.PoolRanks[poolID] = make(map[string]int)
		}
		o.PoolRanks[poolID][playerName] = rank
	})
}

func (s *Store) SaveWinnerOverride(compID, matchID, winnerName string) error {
	return s.modifyOverrides(compID, func(o *Overrides) {
		o.Winners[matchID] = winnerName
	})
}

func (s *Store) ResetOverrides(compID string) error {
	o := &Overrides{
		PoolRanks: make(map[string]map[string]int),
		Winners:   make(map[string]string),
	}
	return s.SaveOverrides(compID, o)
}
