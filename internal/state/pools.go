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

	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()

	cache := s.getFileCache(compID, "pools.csv")
	cache.mu.RLock()
	mtime := s.FileMtime(compID, "pools.csv")
	if cache.data != nil && cache.mtime == mtime {
		res := s.copyPools(cache.data.([]helper.Pool))
		cache.mu.RUnlock()
		return res, nil
	}
	cache.mu.RUnlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Re-check after acquiring write lock
	if cache.data != nil && cache.mtime == mtime {
		return s.copyPools(cache.data.([]helper.Pool)), nil
	}

	path := s.compPath(compID, "pools.csv")

	// #nosec G304
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			cache.data = []helper.Pool{}
			cache.mtime = mtime
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

	// poolIdx maps pool name → index into pools so we can append players in-place
	// without a separate order slice or a final copy pass.
	poolIdx := make(map[string]int)
	var pools []helper.Pool

	for _, rec := range records {
		if len(rec) < 2 {
			continue
		}
		poolName := rec[0]
		playerName := rec[1]

		idx, ok := poolIdx[poolName]
		if !ok {
			idx = len(pools)
			poolIdx[poolName] = idx
			pools = append(pools, helper.Pool{PoolName: poolName})
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
		pools[idx].Players = append(pools[idx].Players, player)
	}

	cache.data = pools
	cache.mtime = mtime

	return s.copyPools(pools), nil
}

func (s *Store) copyPools(pools []helper.Pool) []helper.Pool {
	if pools == nil {
		return nil
	}
	res := make([]helper.Pool, len(pools))
	for i, p := range pools {
		res[i] = p
		if p.Players != nil {
			res[i].Players = make([]helper.Player, len(p.Players))
			copy(res[i].Players, p.Players)
		}
	}
	return res
}

func (s *Store) copyMatchResults(results []MatchResult) []MatchResult {
	if results == nil {
		return nil
	}
	res := make([]MatchResult, len(results))
	for i, r := range results {
		res[i] = r
		if r.IpponsA != nil {
			res[i].IpponsA = make([]string, len(r.IpponsA))
			copy(res[i].IpponsA, r.IpponsA)
		}
		if r.IpponsB != nil {
			res[i].IpponsB = make([]string, len(r.IpponsB))
			copy(res[i].IpponsB, r.IpponsB)
		}
		if r.SubResults != nil {
			res[i].SubResults = make([]SubMatchResult, len(r.SubResults))
			for j, sr := range r.SubResults {
				res[i].SubResults[j] = sr
				if sr.IpponsA != nil {
					res[i].SubResults[j].IpponsA = make([]string, len(sr.IpponsA))
					copy(res[i].SubResults[j].IpponsA, sr.IpponsA)
				}
				if sr.IpponsB != nil {
					res[i].SubResults[j].IpponsB = make([]string, len(sr.IpponsB))
					copy(res[i].SubResults[j].IpponsB, sr.IpponsB)
				}
			}
		}
	}
	return res
}

func (s *Store) SavePools(compID string, pools []helper.Pool) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}

	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

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

	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()

	cache := s.getFileCache(compID, "pool-matches.csv")
	cache.mu.RLock()
	mtime := s.FileMtime(compID, "pool-matches.csv")
	if cache.data != nil && cache.mtime == mtime {
		res := s.copyMatchResults(cache.data.([]MatchResult))
		cache.mu.RUnlock()
		return res, nil
	}
	cache.mu.RUnlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Re-check after acquiring write lock
	if cache.data != nil && cache.mtime == mtime {
		return s.copyMatchResults(cache.data.([]MatchResult)), nil
	}

	path := s.compPath(compID, "pool-matches.csv")
	// #nosec G304
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			cache.data = []MatchResult{}
			cache.mtime = mtime
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

	cache.data = results
	cache.mtime = mtime

	return s.copyMatchResults(results), nil
}

func (s *Store) SavePoolMatches(compID string, results []MatchResult) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}

	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

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
	if err := writer.Error(); err != nil {
		return err
	}

	cache := s.getFileCache(compID, "pool-matches.csv")
	cache.mu.Lock()
	cache.data = s.copyMatchResults(results)
	cache.mtime = s.FileMtime(compID, "pool-matches.csv")
	cache.mu.Unlock()

	return nil
}
