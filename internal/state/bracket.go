package state

import (
	"encoding/json"
	"os"
)

func (s *Store) LoadBracket(compID string) (*Bracket, error) {
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()

	cache := s.getFileCache(compID, "bracket.json")
	cache.mu.RLock()
	mtime := s.FileMtime(compID, "bracket.json")
	if cache.data != nil && cache.mtime == mtime {
		res := s.copyBracket(cache.data.(*Bracket))
		cache.mu.RUnlock()
		return res, nil
	}
	cache.mu.RUnlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Re-check after acquiring write lock
	if cache.data != nil && cache.mtime == mtime {
		return s.copyBracket(cache.data.(*Bracket)), nil
	}

	path := s.compPath(compID, "bracket.json")
	data, err := os.ReadFile(path) // #nosec G304 — path built by compPath which calls filepath.Clean
	if err != nil {
		if os.IsNotExist(err) {
			b := &Bracket{Rounds: [][]BracketMatch{}}
			cache.data = b
			cache.mtime = mtime
			return b, nil
		}
		return nil, err
	}

	var b Bracket
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}

	cache.data = &b
	cache.mtime = mtime

	return s.copyBracket(&b), nil
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
