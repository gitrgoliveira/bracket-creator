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

	path := filepath.Join(s.folder, "competitions", compID, "overrides.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &Overrides{
			PoolRanks: make(map[string]map[string]int),
			Winners:   make(map[string]string),
		}, nil
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
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

func (s *Store) SaveOverrides(compID string, o *Overrides) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *Store) SaveRankOverride(compID, poolID, playerName string, rank int) error {
	o, err := s.LoadOverrides(compID)
	if err != nil {
		return err
	}

	if o.PoolRanks[poolID] == nil {
		o.PoolRanks[poolID] = make(map[string]int)
	}
	o.PoolRanks[poolID][playerName] = rank

	return s.SaveOverrides(compID, o)
}

func (s *Store) SaveWinnerOverride(compID, matchID, winnerName string) error {
	o, err := s.LoadOverrides(compID)
	if err != nil {
		return err
	}

	o.Winners[matchID] = winnerName

	return s.SaveOverrides(compID, o)
}

func (s *Store) ResetOverrides(compID string) error {
	o := &Overrides{
		PoolRanks: make(map[string]map[string]int),
		Winners:   make(map[string]string),
	}
	return s.SaveOverrides(compID, o)
}
