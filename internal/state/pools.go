package state

import (
	"bytes"
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

	data, err := s.loadCached(compID, "pools.csv", parsePoolsFile)
	if err != nil {
		return nil, err
	}
	return s.copyPools(data.([]helper.Pool)), nil
}

func parsePoolsFile(path string) (any, error) {
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

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}

	// poolIdx maps pool name → index into pools so we can append players in-place
	// without a separate order slice or a final copy pass.
	poolIdx := make(map[string]int)
	pools := []helper.Pool{}

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
	return pools, nil
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

	// Build the CSV body in memory then write it atomically + durably
	// via atomicWriteFile. Pool CSVs are small (<1MB even for large
	// tournaments) so memory buffering is fine and gives us crash
	// safety the os.Create + streaming pattern lacked.
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
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
	if err := writer.Error(); err != nil {
		return err
	}

	if err := s.atomicWrite(path, buf.Bytes(), 0600); err != nil {
		return err
	}

	if pools == nil {
		pools = []helper.Pool{}
	}
	cache := s.getFileCache(compID, "pools.csv")
	cache.mu.Lock()
	cache.data = s.copyPools(pools)
	cache.mtime = s.FileMtime(compID, "pools.csv")
	cache.mu.Unlock()

	return nil
}

func (s *Store) LoadPoolMatches(compID string) ([]MatchResult, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}

	data, err := s.loadCached(compID, "pool-matches.csv", parsePoolMatchesFile)
	if err != nil {
		return nil, err
	}
	return s.copyMatchResults(data.([]MatchResult)), nil
}

// LoadPoolMatchesLocked loads pool matches WITHOUT acquiring the
// per-competition lock. Caller MUST already hold the write lock for
// this competition — typically from inside a transform passed to
// UpdatePoolMatchByID, UpdateBracket, or UpdateCompetitionChanged.
// Bypasses the cache deliberately: the cache mtime can lag a
// concurrent writer that the caller may be in the middle of making,
// and we want the most-recent on-disk state.
//
// Motivating use case: MaybeAutoCompletePools (engine/competition.go)
// re-checks "are all matches completed?" INSIDE its
// UpdateCompetitionChanged transform to close a TOCTOU window where
// the outer LoadPoolMatches snapshot can go stale. The transform
// holds the per-comp write lock, so the standard LoadPoolMatches
// would deadlock (sync.RWMutex non-recursive); this helper provides
// the lock-free read for that context.
func (s *Store) LoadPoolMatchesLocked(compID string) ([]MatchResult, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	path := s.compPath(compID, "pool-matches.csv")
	parsed, err := parsePoolMatchesFile(path)
	if err != nil {
		return nil, err
	}
	results, _ := parsed.([]MatchResult)
	return s.copyMatchResults(results), nil
}

func parsePoolMatchesFile(path string) (any, error) {
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

	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	return parsePoolMatchesRecords(records), nil
}

// parsePoolMatchesBytes parses pool-matches.csv from in-memory bytes.
// Used by tx-internal read-your-own-writes (the storeTx LoadPoolMatches
// peek at WAL-staged bytes). Empty input → empty slice, matching the
// "file does not exist" contract of parsePoolMatchesFile.
func parsePoolMatchesBytes(raw []byte) ([]MatchResult, error) {
	if len(raw) == 0 {
		return []MatchResult{}, nil
	}
	records, err := csv.NewReader(bytes.NewReader(raw)).ReadAll()
	if err != nil {
		return nil, err
	}
	return parsePoolMatchesRecords(records), nil
}

// parsePoolMatchesRecords turns a CSV record matrix into MatchResults.
// Extracted so the file-based and bytes-based parsers share the
// rec-shape→struct mapping verbatim (no drift between the two).
func parsePoolMatchesRecords(records [][]string) []MatchResult {
	results := []MatchResult{}
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
	return results
}

func (s *Store) SavePoolMatches(compID string, results []MatchResult) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}

	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	return s.savePoolMatchesLocked(compID, results, s.directWrite)
}

// savePoolMatchesLocked persists results to disk and refreshes the cache.
// Caller MUST hold the per-competition lock (s.getCompLock(compID)).
// Used by both SavePoolMatches (which takes the lock) and
// UpdatePoolMatchByID (which holds the lock across load + mutate + save).
//
// The write parameter routes the actual file write — directWrite for
// non-tx callers, a WAL-capturing writer for tx callers. See
// saveBracketLocked for the cache-refresh rationale (T211/T212).
func (s *Store) savePoolMatchesLocked(compID string, results []MatchResult, write writeFn) error {
	path := s.compPath(compID, "pool-matches.csv")

	// Build the CSV body in memory then write it atomically + durably
	// via atomicWriteFile. Pool-match CSVs stay well under 1MB even for
	// large tournaments (a few hundred matches × ~14 columns of short
	// fields), so memory buffering trades trivial RAM for crash safety
	// the previous os.Create + streaming pattern lacked.
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
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

if err := write(path, buf.Bytes(), 0600); err != nil {
		return err
	}

	cache := s.getFileCache(compID, "pool-matches.csv")
	cache.mu.Lock()
	cache.data = s.copyMatchResults(results)
	cache.mtime = s.FileMtime(compID, "pool-matches.csv")
	cache.mu.Unlock()

	return nil
}

// UpdatePoolMatchByID atomically loads the pool-matches CSV for compID,
// finds the match with matchID, calls mutate on it, and persists the
// updated slice. Returns (found, err): found is false when no match
// has that ID, allowing callers to fall through (e.g. to the bracket
// store for elimination-round matches).
//
// The entire load + find + mutate + save sequence runs under the
// per-competition lock so concurrent calls — even for different
// match IDs in the same competition — serialize correctly without
// losing each other's mutations.
//
// Without this primitive, the equivalent engine helper
// (engine.withPoolMatch) had a TOCTOU window: two operators scoring
// different matches on different courts could each LoadPoolMatches
// into separate copies, mutate their target match, and SavePoolMatches
// in sequence — the later save would overwrite the earlier save's
// mutation with stale data for the OTHER match. One operator's score
// would be silently lost during a live tournament.
func (s *Store) UpdatePoolMatchByID(compID, matchID string, mutate func(*MatchResult)) (bool, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return false, err
	}

	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	return s.updatePoolMatchByIDLocked(compID, matchID, mutate, s.directWrite)
}

// updatePoolMatchByIDLocked is the lock-free body of
// UpdatePoolMatchByID. Caller MUST already hold the per-comp write
// lock. Used by the tx-aware path so the same load + find + mutate +
// save sequence runs without re-acquiring the lock from inside a
// WithTransaction closure (T156, NFR-010). The write parameter
// selects direct-to-disk vs WAL-capturing semantics (T211/T212).
func (s *Store) updatePoolMatchByIDLocked(compID, matchID string, mutate func(*MatchResult), write writeFn) (bool, error) {
	// Load directly from disk under the lock. We deliberately bypass
	// the loadCached path here because the per-comp lock is what
	// coordinates with the save below; using the cache would risk
	// reading a stale snapshot if another writer released the lock
	// between cache populate and our acquire.
	path := s.compPath(compID, "pool-matches.csv")
	parsed, err := parsePoolMatchesFile(path)
	if err != nil {
		return false, err
	}
	results, _ := parsed.([]MatchResult)

	for i := range results {
		if results[i].ID == matchID {
			mutate(&results[i])
			return true, s.savePoolMatchesLocked(compID, results, write)
		}
	}
	return false, nil
}
