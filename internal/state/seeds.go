package state

import (
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// LoadSeeds and SaveSeeds use the PER-COMPETITION lock (not the store-wide
// `s.mu`) so they serialize against other per-comp readers/writers — in
// particular against the StartCompetition transform held by
// UpdateCompetitionChanged. Pre-fix, SaveSeeds took `s.mu.Lock()`
// (store-wide) and the StartCompetition transform took the per-comp lock,
// so the seeds drift check inside the transform (via FileMtime) had a
// race window: a concurrent SaveSeeds could land AFTER the mtime check
// but BEFORE the status commit, leaving status=Pools on disk with
// seeds.csv reflecting roster the engine never read.
//
// Switching to per-comp locking ALSO improves scalability — concurrent
// seed saves for DIFFERENT comps no longer block each other on the
// global store mutex. Same locking strategy participants.csv and
// pools.csv already use.
func (s *Store) LoadSeeds(compID string) ([]domain.SeedAssignment, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()

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
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	path := s.compPath(compID, "seeds.csv")

	// Sort by rank for readability
	sort.Slice(assignments, func(i, j int) bool {
		return assignments[i].SeedRank < assignments[j].SeedRank
	})

	var sb strings.Builder
	w := csv.NewWriter(&sb)
	if err := w.Write([]string{"Rank", "Name"}); err != nil {
		return fmt.Errorf("writing seeds CSV header: %w", err)
	}
	for _, a := range assignments {
		if err := w.Write([]string{strconv.Itoa(a.SeedRank), a.Name}); err != nil {
			return fmt.Errorf("writing seeds CSV record: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("flushing seeds CSV: %w", err)
	}

	return s.atomicWrite(path, []byte(sb.String()), 0600)
}
