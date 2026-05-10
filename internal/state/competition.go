package state

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

func (s *Store) ListCompetitions() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(filepath.Join(s.folder, "competitions"))
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	return ids, nil
}

func (s *Store) LoadCompetition(id string) (*Competition, error) {
	if err := ValidateCompetitionID(id); err != nil {
		return nil, fmt.Errorf("invalid competition ID: %w", err)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.compPath(id, "config.md")
	data, err := os.ReadFile(path) // #nosec G304 — path built by compPath which calls filepath.Clean
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var c Competition
	if err := parseFrontMatter(data, &c); err != nil {
		return nil, err
	}

	return &c, nil
}

// SaveCompetitionChanged persists c and reports whether the on-disk content
// actually changed. Use this instead of SaveCompetition when you need to gate
// a broadcast on a real mutation.
func (s *Store) SaveCompetitionChanged(c *Competition) (bool, error) {
	if err := ValidateCompetitionID(c.ID); err != nil {
		return false, fmt.Errorf("invalid competition ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(s.compPath(c.ID), 0700); err != nil {
		return false, err
	}

	path := s.compPath(c.ID, "config.md")
	newData, err := writeFrontMatter(c)
	if err != nil {
		return false, err
	}

	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, newData) { // #nosec G304
		return false, nil
	}

	return true, os.WriteFile(path, newData, 0600)
}

func (s *Store) SaveCompetition(c *Competition) error {
	_, err := s.SaveCompetitionChanged(c)
	return err
}

func (s *Store) DeleteCompetition(id string) error {
	if err := ValidateCompetitionID(id); err != nil {
		return fmt.Errorf("invalid competition ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return os.RemoveAll(s.compPath(id))
}
