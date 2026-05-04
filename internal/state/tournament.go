package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		return nil, err
	}

	return &t, nil
}

func (s *Store) SaveTournament(t *Tournament) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveTournamentNoLock(t)
}

func (s *Store) saveTournamentNoLock(t *Tournament) error {
	path := filepath.Clean(filepath.Join(s.folder, "tournament.md"))
	data, err := writeFrontMatter(t)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
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
