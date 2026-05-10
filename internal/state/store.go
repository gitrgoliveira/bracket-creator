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

	return nil
}

func (s *Store) GetFolder() string {
	return s.folder
}

// compPath builds and cleans the path to a file inside a competition directory.
func (s *Store) compPath(compID string, parts ...string) string {
	segments := append([]string{s.folder, "competitions", compID}, parts...)
	return filepath.Clean(filepath.Join(segments...))
}

// FileMtime returns the UnixNano mtime of a file inside a competition directory.
// Returns 0 if the file does not exist or stat fails.
func (s *Store) FileMtime(compID, filename string) int64 {
	info, err := os.Stat(s.compPath(compID, filename))
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
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
