package state

import (
	"encoding/json"
	"os"
)

func (s *Store) LoadBracket(compID string) (*Bracket, error) {
	data, err := s.loadCached(compID, "bracket.json", parseBracketFile)
	if err != nil {
		return nil, err
	}
	return s.copyBracket(data.(*Bracket)), nil
}

func parseBracketFile(path string) (any, error) {
	raw, err := os.ReadFile(path) // #nosec G304 — path built by compPath which calls filepath.Clean
	if err != nil {
		if os.IsNotExist(err) {
			return &Bracket{Rounds: [][]BracketMatch{}}, nil
		}
		return nil, err
	}
	var b Bracket
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) copyBracket(b *Bracket) *Bracket {
	if b == nil {
		return nil
	}
	res := &Bracket{
		Rounds: make([][]BracketMatch, len(b.Rounds)),
	}
	for i, round := range b.Rounds {
		res.Rounds[i] = make([]BracketMatch, len(round))
		copy(res.Rounds[i], round)
	}
	return res
}

func (s *Store) SaveBracket(compID string, b *Bracket) error {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	path := s.compPath(compID, "bracket.json")
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}

	cache := s.getFileCache(compID, "bracket.json")
	cache.mu.Lock()
	cache.data = s.copyBracket(b)
	cache.mtime = s.FileMtime(compID, "bracket.json")
	cache.mu.Unlock()

	return nil
}
