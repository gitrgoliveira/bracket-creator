package state

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func newParticipantID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant bits
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

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

	// Detect new format: first field is a UUID.
	hasIDs := len(lines) > 0 && uuidRE.MatchString(strings.TrimSpace(strings.SplitN(lines[0], ",", 2)[0]))

	var ids []string
	var plainLines []string
	if hasIDs {
		for _, line := range lines {
			idx := strings.IndexByte(line, ',')
			if idx < 0 {
				plainLines = append(plainLines, line)
				ids = append(ids, "")
				continue
			}
			ids = append(ids, strings.TrimSpace(line[:idx]))
			plainLines = append(plainLines, line[idx+1:])
		}
	} else {
		plainLines = lines
	}

	players, err := helper.CreatePlayers(plainLines, withZekkenName)
	if err != nil {
		return nil, err
	}

	if hasIDs {
		for i := range players {
			if i < len(ids) {
				players[i].ID = ids[i]
			}
		}
	}

	// Merge seeds if they exist.
	seeds, _ := helper.ParseSeedsFile(filepath.Join(s.folder, "competitions", compID, "seeds.csv"))
	if len(seeds) > 0 {
		seedMap := make(map[string]int)
		for _, sd := range seeds {
			seedMap[sd.Name] = sd.SeedRank
		}
		for i := range players {
			if seed, ok := seedMap[players[i].Name]; ok {
				players[i].Seed = seed
			}
		}
	}

	return players, nil
}

func (s *Store) SaveParticipants(compID string, players []helper.Player) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Clean(filepath.Join(s.folder, "competitions", compID, "participants.csv"))
	var sb strings.Builder
	for _, p := range players {
		id := p.ID
		if id == "" {
			id = newParticipantID()
		}
		var row string
		if p.DisplayName != "" && p.DisplayName != p.Name {
			row = fmt.Sprintf("%s, %s, %s", p.Name, p.DisplayName, p.Dojo)
		} else {
			row = fmt.Sprintf("%s, %s", p.Name, p.Dojo)
		}
		if p.Tag != "" {
			row += ", " + p.Tag
		}
		sb.WriteString(id + ", " + row + "\n")
	}

	return os.WriteFile(path, []byte(sb.String()), 0600)
}
