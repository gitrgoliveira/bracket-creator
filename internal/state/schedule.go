package state

import (
	"bytes"
	"encoding/csv"
	"os"
)

type ScheduleEntry struct {
	MatchType   string `json:"matchType"` // pool | bracket | break
	MatchRef    string `json:"matchRef"`  // ID of the match (empty for breaks)
	Court       string `json:"court"`
	Date        string `json:"date"`        // YYYY-MM-DD — for multi-day tournaments
	ScheduledAt string `json:"scheduledAt"` // HH:MM
	Status      string `json:"status"`
	IsBreak     bool   `json:"isBreak,omitempty"`
	Label       string `json:"label,omitempty"` // display label for breaks
}

func (s *Store) LoadSchedule(compID string) ([]ScheduleEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.compPath(compID, "schedule.csv")
	f, err := os.Open(path) // #nosec G304 — path built by compPath which calls filepath.Clean
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
		e := ScheduleEntry{
			MatchType:   rec[0],
			MatchRef:    rec[1],
			Court:       rec[2],
			ScheduledAt: rec[3],
			Status:      rec[4],
		}
		if len(rec) > 5 {
			e.Date = rec[5]
		}
		if len(rec) > 6 {
			e.IsBreak = rec[6] == "true"
		}
		if len(rec) > 7 {
			e.Label = rec[7]
		}
		schedule = append(schedule, e)
	}

	return schedule, nil
}

func serializeSchedule(entries []ScheduleEntry) ([]byte, error) {
	isBreakStr := func(b bool) string {
		if b {
			return "true"
		}
		return ""
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"MatchType", "MatchRef", "Court", "ScheduledAt", "Status", "Date", "IsBreak", "Label"}); err != nil {
		return nil, err
	}
	for _, e := range entries {
		if err := w.Write([]string{
			e.MatchType, e.MatchRef, e.Court, e.ScheduledAt, e.Status,
			e.Date, isBreakStr(e.IsBreak), e.Label,
		}); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SaveScheduleChanged persists entries and reports whether the on-disk content
// actually changed. Use this instead of SaveSchedule when you need to gate a
// broadcast on a real mutation.
func (s *Store) SaveScheduleChanged(compID string, entries []ScheduleEntry) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.compPath(compID, "schedule.csv")
	newData, err := serializeSchedule(entries)
	if err != nil {
		return false, err
	}

	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, newData) { // #nosec G304
		return false, nil
	}

	return true, os.WriteFile(path, newData, 0600)
}

func (s *Store) SaveSchedule(compID string, entries []ScheduleEntry) error {
	_, err := s.SaveScheduleChanged(compID, entries)
	return err
}
