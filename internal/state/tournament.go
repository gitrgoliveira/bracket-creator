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
	s.tournamentMu.RLock()
	path := filepath.Clean(filepath.Join(s.folder, "tournament.md"))
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.tournamentMu.RUnlock()
			s.tournamentMu.Lock()
			defer s.tournamentMu.Unlock()
			s.cachedTourn = nil
			s.tournMtime = 0
			return nil, nil
		}
		s.tournamentMu.RUnlock()
		return nil, err
	}

	mtime := info.ModTime().UnixNano()
	if s.cachedTourn != nil && s.tournMtime == mtime {
		t := s.copyTournament(s.cachedTourn)
		s.tournamentMu.RUnlock()
		return t, nil
	}
	s.tournamentMu.RUnlock()

	s.tournamentMu.Lock()
	defer s.tournamentMu.Unlock()

	// Re-check after acquiring write lock
	info, err = os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cachedTourn = nil
			s.tournMtime = 0
			return nil, nil
		}
		return nil, err
	}
	mtime = info.ModTime().UnixNano()
	if s.cachedTourn != nil && s.tournMtime == mtime {
		return s.copyTournament(s.cachedTourn), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var t Tournament
	if err := parseFrontMatter(data, &t); err != nil {
		// If it's not a front-matter file, return a default tournament
		t = Tournament{
			Name:  "New Tournament",
			Date:  time.Now().Format("2006-01-02"),
			Venue: "Venue TBA",
		}
	}

	s.cachedTourn = &t
	s.tournMtime = mtime

	return s.copyTournament(s.cachedTourn), nil
}

func (s *Store) copyTournament(t *Tournament) *Tournament {
	if t == nil {
		return nil
	}
	cp := *t
	if t.Courts != nil {
		cp.Courts = make([]string, len(t.Courts))
		copy(cp.Courts, t.Courts)
	}
	return &cp
}

// SaveTournamentChanged persists t and reports whether the on-disk content
// actually changed. Use this instead of SaveTournament when you need to gate
// a broadcast on a real mutation.
func (s *Store) SaveTournamentChanged(t *Tournament) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tournamentMu.Lock()
	defer s.tournamentMu.Unlock()

	path := filepath.Clean(filepath.Join(s.folder, "tournament.md"))
	newData, err := writeFrontMatter(t)
	if err != nil {
		return false, err
	}

	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, newData) { // #nosec G304
		return false, nil
	}

	if err := os.WriteFile(path, newData, 0600); err != nil {
		return false, err
	}

	// Update cache
	s.cachedTourn = t
	info, _ := os.Stat(path)
	if info != nil {
		s.tournMtime = info.ModTime().UnixNano()
	}

	return true, nil
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
