package state

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func (s *Store) LoadTournament() (*Tournament, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Clean(filepath.Join(s.folder, "tournament.md"))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Not found
		}
		return nil, err
	}

	var t Tournament
	if err := parseFrontMatter(data, &t); err != nil {
		// If it's not a front-matter file, return a default tournament
		return &Tournament{
			Name:  "New Tournament",
			Date:  time.Now().Format("2006-01-02"),
			Venue: "Venue TBA",
		}, nil
	}

	return &t, nil
}

// SaveTournamentChanged persists t and reports whether the on-disk content
// actually changed. Use this instead of SaveTournament when you need to gate
// a broadcast on a real mutation.
func (s *Store) SaveTournamentChanged(t *Tournament) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Clean(filepath.Join(s.folder, "tournament.md"))
	newData, err := writeFrontMatter(t)
	if err != nil {
		return false, err
	}

	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, newData) { // #nosec G304
		return false, nil
	}

	return true, os.WriteFile(path, newData, 0600)
}

func (s *Store) SaveTournament(t *Tournament) error {
	_, err := s.SaveTournamentChanged(t)
	return err
}

func parseFrontMatter(data []byte, v interface{}) error {
	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return fmt.Errorf("missing front matter delimiter")
	}

	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return fmt.Errorf("invalid front matter format")
	}

	return yaml.Unmarshal([]byte(parts[1]), v)
}

func writeFrontMatter(v interface{}) ([]byte, error) {
	y, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf("---\n%s---\n", string(y))), nil
}
