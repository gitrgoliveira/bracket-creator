package state

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

func (s *Store) LoadSeeds(compID string) ([]domain.SeedAssignment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.compPath(compID, "seeds.csv")
	result, err := helper.ParseSeedsFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []domain.SeedAssignment{}, nil
		}
		return nil, err
	}
	return result, nil
}

func (s *Store) SaveSeeds(compID string, assignments []domain.SeedAssignment) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.compPath(compID, "seeds.csv")

	// Sort by rank for readability
	sort.Slice(assignments, func(i, j int) bool {
		return assignments[i].SeedRank < assignments[j].SeedRank
	})

	var sb strings.Builder
	sb.WriteString("Rank,Name\n")
	for _, a := range assignments {
		fmt.Fprintf(&sb, "%d,%s\n", a.SeedRank, a.Name)
	}

	return os.WriteFile(path, []byte(sb.String()), 0600)
}
