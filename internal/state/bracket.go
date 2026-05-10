package state

import (
	"encoding/json"
	"os"
)

func (s *Store) LoadBracket(compID string) (*Bracket, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.compPath(compID, "bracket.json")
	data, err := os.ReadFile(path) // #nosec G304 — path built by compPath which calls filepath.Clean
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

	path := s.compPath(compID, "bracket.json")
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}
