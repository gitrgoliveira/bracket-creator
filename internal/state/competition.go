package state

import (
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

	path := filepath.Clean(filepath.Join(s.folder, "competitions", id, "config.md"))
	data, err := os.ReadFile(path)
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

func (s *Store) SaveCompetition(c *Competition) error {
	if err := ValidateCompetitionID(c.ID); err != nil {
		return fmt.Errorf("invalid competition ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Clean(filepath.Join(s.folder, "competitions", c.ID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := filepath.Clean(filepath.Join(dir, "config.md"))
	data, err := writeFrontMatter(c)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func (s *Store) DeleteCompetition(id string) error {
	if err := ValidateCompetitionID(id); err != nil {
		return fmt.Errorf("invalid competition ID: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.folder, "competitions", id)
	return os.RemoveAll(dir)
}
