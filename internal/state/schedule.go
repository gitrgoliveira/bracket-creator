package state

import (
	"encoding/csv"
	"os"
	"path/filepath"
)

type ScheduleEntry struct {
	MatchType   string `json:"matchType"` // pool | bracket
	MatchRef    string `json:"matchRef"`  // ID of the match
	Court       string `json:"court"`
	ScheduledAt string `json:"scheduledAt"`
	Status      string `json:"status"`
}

func (s *Store) LoadSchedule(compID string) ([]ScheduleEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := filepath.Clean(filepath.Join(s.folder, "competitions", compID, "schedule.csv"))
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ScheduleEntry{}, nil
		}
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	reader := csv.NewReader(f)
	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var schedule []ScheduleEntry
	for i, rec := range records {
		if i == 0 || len(rec) < 5 {
			continue // skip header
		}
		schedule = append(schedule, ScheduleEntry{
			MatchType:   rec[0],
			MatchRef:    rec[1],
			Court:       rec[2],
			ScheduledAt: rec[3],
			Status:      rec[4],
		})
	}

	return schedule, nil
}

func (s *Store) SaveSchedule(compID string, entries []ScheduleEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := filepath.Clean(filepath.Join(s.folder, "competitions", compID, "schedule.csv"))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	writer := csv.NewWriter(f)
	if err := writer.Write([]string{"MatchType", "MatchRef", "Court", "ScheduledAt", "Status"}); err != nil {
		return err
	}

	for _, e := range entries {
		if err := writer.Write([]string{
			e.MatchType,
			e.MatchRef,
			e.Court,
			e.ScheduledAt,
			e.Status,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
