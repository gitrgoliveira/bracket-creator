package state

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

func (s *Store) LoadParticipants(compID string, withZekkenName bool) ([]helper.Player, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Join(s.folder, "competitions", compID, "participants.csv")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return []helper.Player{}, nil
	}

	lines, err := helper.ReadEntriesFromFile(path)
	if err != nil {
		return nil, err
	}

	return helper.CreatePlayers(lines, withZekkenName)
}

func (s *Store) SaveParticipants(compID string, players []helper.Player) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Clean(filepath.Join(s.folder, "competitions", compID, "participants.csv"))
	var sb strings.Builder
	for _, p := range players {
		if p.DisplayName != "" && p.DisplayName != p.Name {
			fmt.Fprintf(&sb, "%s, %s, %s\n", p.Name, p.DisplayName, p.Dojo)
		} else {
			fmt.Fprintf(&sb, "%s, %s\n", p.Name, p.Dojo)
		}
	}

	return os.WriteFile(path, []byte(sb.String()), 0600)
}
