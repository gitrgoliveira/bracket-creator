package state

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func (s *Store) LoadBracket(compID string) (*Bracket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Clean(filepath.Join(s.folder, "competitions", compID, "bracket.json"))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Bracket{Rounds: [][]BracketMatch{}}, nil
		}
		return nil, err
	}

	var b Bracket
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}

	return &b, nil
}

func (s *Store) SaveBracket(compID string, b *Bracket) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Clean(filepath.Join(s.folder, "competitions", compID, "bracket.json"))
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}
