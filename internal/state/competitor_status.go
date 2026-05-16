package state

import (
	"os"
	"sort"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"gopkg.in/yaml.v3"
)

// competitorStatusFile is the on-disk YAML shape for
// tournament-data/competitions/<id>/competitor-status.yaml.
//
// Wire-stable: persisted as a sorted slice so the file content is
// deterministic across runs.
type competitorStatusFile struct {
	Statuses []domain.CompetitorStatus `yaml:"statuses"`
}

const competitorStatusFilename = "competitor-status.yaml"

// LoadCompetitorStatus returns the per-player status map for compID.
// A missing file is treated as "all eligible" per FR-034 / NFR-025.
func (s *Store) LoadCompetitorStatus(compID string) (map[string]domain.CompetitorStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadCompetitorStatusLocked(compID)
}

func (s *Store) loadCompetitorStatusLocked(compID string) (map[string]domain.CompetitorStatus, error) {
	path := s.compPath(compID, competitorStatusFilename)
	data, err := os.ReadFile(path) // #nosec G304 — compPath cleans the path.
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]domain.CompetitorStatus{}, nil
		}
		return nil, err
	}
	var file competitorStatusFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	out := make(map[string]domain.CompetitorStatus, len(file.Statuses))
	for _, st := range file.Statuses {
		out[st.PlayerID] = st
	}
	return out, nil
}

func (s *Store) saveCompetitorStatusLocked(compID string, statuses map[string]domain.CompetitorStatus) error {
	if err := os.MkdirAll(s.compPath(compID), 0700); err != nil {
		return err
	}
	keys := make([]string, 0, len(statuses))
	for k := range statuses {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	file := competitorStatusFile{Statuses: make([]domain.CompetitorStatus, 0, len(keys))}
	for _, k := range keys {
		file.Statuses = append(file.Statuses, statuses[k])
	}
	data, err := yaml.Marshal(&file)
	if err != nil {
		return err
	}
	return os.WriteFile(s.compPath(compID, competitorStatusFilename), data, 0600)
}

// SetCompetitorStatus persists a status entry, replacing any prior
// entry for the same PlayerID. RecordedAt defaults to time.Now().UTC()
// when the caller leaves it zero.
func (s *Store) SetCompetitorStatus(compID string, status domain.CompetitorStatus) error {
	if err := status.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.loadCompetitorStatusLocked(compID)
	if err != nil {
		return err
	}
	if status.RecordedAt.IsZero() {
		status.RecordedAt = time.Now().UTC()
	}
	current[status.PlayerID] = status
	return s.saveCompetitorStatusLocked(compID, current)
}
