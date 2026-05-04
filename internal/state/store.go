package state

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var validIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

type Store struct {
	folder string
	mu     sync.RWMutex
}

func NewStore(folder string) (*Store, error) {
	s := &Store{
		folder: folder,
	}

	if err := s.init(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) init() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure folder exists
	if err := os.MkdirAll(s.folder, 0700); err != nil {
		return err
	}

	// Ensure competitions folder exists
	if err := os.MkdirAll(filepath.Join(s.folder, "competitions"), 0700); err != nil {
		return err
	}

	// Ensure tournament.md exists
	tournamentPath := filepath.Join(s.folder, "tournament.md")
	if _, err := os.Stat(tournamentPath); os.IsNotExist(err) {
		t := &Tournament{
			Name: "New Tournament",
		}
		if err := s.saveTournamentNoLock(t); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) GetFolder() string {
	return s.folder
}

func ValidateCompetitionID(id string) error {
	if id == "" {
		return fmt.Errorf("competition ID cannot be empty")
	}
	if len(id) > 64 {
		return fmt.Errorf("competition ID too long (max 64 characters)")
	}
	if !validIDPattern.MatchString(id) {
		return fmt.Errorf("competition ID contains invalid characters (allowed: alphanumeric, hyphens, underscores)")
	}
	return nil
}
