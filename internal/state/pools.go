package state

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

func (s *Store) LoadPools(compID string) ([]helper.Pool, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.compPath(compID, "pools.csv")
	// #nosec G304
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []helper.Pool{}, nil
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

	poolMap := make(map[string]*helper.Pool)
	var poolOrder []string

	for _, rec := range records {
		if len(rec) < 2 {
			continue
		}
		poolName := rec[0]
		playerName := rec[1]

		p, ok := poolMap[poolName]
		if !ok {
			p = &helper.Pool{PoolName: poolName}
			poolMap[poolName] = p
			poolOrder = append(poolOrder, poolName)
		}

		player := helper.Player{Name: playerName}
		if len(rec) > 3 {
			player.DisplayName = rec[3]
		}
		if len(rec) > 4 {
			player.Dojo = rec[4]
		}
		if len(rec) > 5 && rec[5] != "" {
			seed, _ := strconv.Atoi(rec[5])
			player.Seed = seed
		}
		if len(rec) > 6 {
			player.Number = rec[6]
		}
		p.Players = append(p.Players, player)
	}

	var pools []helper.Pool
	for _, name := range poolOrder {
		pools = append(pools, *poolMap[name])
	}

	return pools, nil
}

func (s *Store) SavePools(compID string, pools []helper.Pool) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.compPath(compID, "pools.csv")
	// #nosec G304
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	writer := csv.NewWriter(f)
	for _, p := range pools {
		for i, player := range p.Players {
			seedStr := ""
			if player.Seed > 0 {
				seedStr = strconv.Itoa(player.Seed)
			}
			if err := writer.Write([]string{p.PoolName, player.Name, strconv.Itoa(i), player.DisplayName, player.Dojo, seedStr, player.Number}); err != nil {
				return err
			}
		}
	}
	writer.Flush()
	return writer.Error()
}

func (s *Store) LoadPoolMatches(compID string) ([]MatchResult, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.compPath(compID, "pool-matches.csv")
	// #nosec G304
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []MatchResult{}, nil
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

	var results []MatchResult
	for i, rec := range records {
		if i == 0 && len(rec) > 0 && rec[0] == "PoolName" {
			continue // skip header
		}
		if len(rec) < 12 {
			continue
		}

		hansokuA, _ := strconv.Atoi(rec[7])
		hansokuB, _ := strconv.Atoi(rec[8])

		m := MatchResult{
			ID:       rec[0] + "-" + rec[1], // PoolName-MatchIdx
			SideA:    rec[2],
			SideB:    rec[3],
			Winner:   rec[4],
			IpponsA:  strings.Split(rec[5], "|"),
			IpponsB:  strings.Split(rec[6], "|"),
			HansokuA: hansokuA,
			HansokuB: hansokuB,
			Decision: rec[9],
			Status:   MatchStatus(rec[10]),
			Court:    rec[11],
		}

		if len(rec) > 12 && rec[12] != "" {
			_ = json.Unmarshal([]byte(rec[12]), &m.SubResults)
		}
		if len(rec) > 13 {
			m.ScheduledAt = rec[13]
		}

		results = append(results, m)
	}

	return results, nil
}

func (s *Store) SavePoolMatches(compID string, results []MatchResult) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.compPath(compID, "pool-matches.csv")
	// #nosec G304
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	writer := csv.NewWriter(f)
	if err := writer.Write([]string{"PoolName", "MatchIdx", "SideA", "SideB", "Winner", "IpponsA", "IpponsB", "HansokuA", "HansokuB", "Decision", "Status", "Court", "SubResults", "ScheduledAt"}); err != nil {
		return err
	}

	for _, r := range results {
		parts := strings.SplitN(r.ID, "-", 2)
		poolName := parts[0]
		matchIdx := ""
		if len(parts) > 1 {
			matchIdx = parts[1]
		}

		subJSON := ""
		if len(r.SubResults) > 0 {
			b, _ := json.Marshal(r.SubResults)
			subJSON = string(b)
		}

		if err := writer.Write([]string{
			poolName,
			matchIdx,
			r.SideA,
			r.SideB,
			r.Winner,
			strings.Join(r.IpponsA, "|"),
			strings.Join(r.IpponsB, "|"),
			strconv.Itoa(r.HansokuA),
			strconv.Itoa(r.HansokuB),
			r.Decision,
			string(r.Status),
			r.Court,
			subJSON,
			r.ScheduledAt,
		}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
