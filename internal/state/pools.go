package state

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"log/slog"
	"os"
	"sort"
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
		// Default to 1-based append order so that a corrupt/missing col 2 never
		// leaves PoolPosition at the zero value (which would misplace the player
		// at the front of the stable sort and corrupt Excel pool-draw labels).
		// col 2 overrides this only when it is present, parseable, AND non-negative.
		// savePoolsLocked writes it as strconv.Itoa(i), 0-indexed, so add 1 to
		// align with the 1-based convention used by Excel pool/draw exporters.
		// (Other producer paths may use different conventions; this +1 applies only
		// to the CSV round-trip used by LoadPools.)
		player.PoolPosition = int64(len(pools[idx].Players) + 1) // 1-based default
		if len(rec) > 2 && rec[2] != "" {
			if pos, err2 := strconv.ParseInt(rec[2], 10, 64); err2 == nil && pos >= 0 {
				player.PoolPosition = pos + 1 // convert 0-indexed CSV value → 1-indexed
			}
			// negative or non-integer values leave the 1-based append-order default intact
		}
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
		// Participant UUID (appended after the legacy 7-column layout).
		// Absent in pre-change files → empty id; the league matrix then
		// falls back to name-based cell matching.
		if len(rec) > 7 {
			player.ID = rec[7]
		}
		pools[idx].Players = append(pools[idx].Players, player)
	}

	// Sort each pool's Players by their stored draw position so that the ordering
	// is authoritative from the persisted field, not from CSV row order. This
	// guarantees correct draw order even if rows were written out-of-order or
	// the file was manually edited. Legacy files without col 2 receive sequential
	// 1-based append-order defaults above. Ties (e.g. from a manually edited file
	// with duplicate positions) are resolved by insertion order via SliceStable.
	for i := range pools {
		sort.SliceStable(pools[i].Players, func(a, b int) bool {
			return pools[i].Players[a].PoolPosition < pools[i].Players[b].PoolPosition
		})
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
		res[i].SubResults = cloneSubResults(r.SubResults)
		// Deep-copy the pointer fields so a caller mutating a returned
		// result through *Encho / *DecidedByHantei cannot corrupt cached
		// state. Mirrors copyBracket, which already clones its Encho pointer.
		res[i].Encho = r.Encho.Clone()
		if r.DecidedByHantei != nil {
			v := *r.DecidedByHantei
			res[i].DecidedByHantei = &v
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

	return s.savePoolsLocked(compID, pools)
}

// loadPoolsLocked reads pools.csv directly from disk without acquiring the
// per-competition lock. Caller MUST already hold the per-comp write lock.
// typically from inside a WithTransaction closure.
func (s *Store) loadPoolsLocked(compID string) ([]helper.Pool, error) {
	path := s.compPath(compID, "pools.csv")
	parsed, err := parsePoolsFile(path)
	if err != nil {
		return nil, err
	}
	pools, _ := parsed.([]helper.Pool)
	return s.copyPools(pools), nil
}

// savePoolsLocked writes pools.csv without acquiring the per-competition lock.
// Caller MUST already hold the per-comp write lock, typically from inside a
// WithTransaction closure. Writes directly to disk (not WAL-staged) but is
// crash-safe via atomicWrite.
func (s *Store) savePoolsLocked(compID string, pools []helper.Pool) error {
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
			if err := writer.Write([]string{p.PoolName, player.Name, strconv.Itoa(i), player.DisplayName, player.Dojo, seedStr, player.Number, player.ID}); err != nil {
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

	matches, err := s.cachedPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	return s.copyMatchResults(matches), nil
}

// LoadPoolMatchesLocked loads pool matches WITHOUT acquiring the
// per-competition lock. Caller MUST already hold the write lock for
// this competition, typically from inside a transform passed to
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

// splitIppons parses a "|"-joined ippon field into a slice, mapping an
// EMPTY field to an empty slice. strings.Split("", "|") returns [""] (a
// one-element slice holding the empty string), which len() then counts as a
// phantom ippon, inflating points-won/lost in standings and corrupting
// individual pool tie detection (two players who actually tied read as
// differing by a phantom point). An empty field maps to a zero-length slice
// across every consumer; we return a non-nil empty slice (not nil) so the
// JSON projection stays a stable array ([]), not null, the viewer endpoints
// serialize IpponsA/IpponsB and an array field should never flip to null.
func splitIppons(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "|")
}

// poolMatchColumn describes ONE column of pool-matches.csv: the header name,
// how a match renders into the cell (put) and how a cell reads back (take).
//
// poolMatchColumns below is THE definition of the file format. The header, the
// row writer (savePoolMatchesLocked) and the reader (parsePoolMatchesRecords)
// all derive from this single ordered list, so the three can no longer
// disagree; before this table each kept its own hand-maintained copy, and
// nothing failed when a new MatchResult field missed one of them, which is
// exactly how DecisionBy, DecisionReason, Encho and ModifiedAt were silently
// lost on restart.
//
// Two rules, both learned the hard way:
//
//   - TABLE ORDER IS THE ON-DISK CONTRACT, never Go struct order. Files
//     written by older builds must keep loading and files written now must
//     load in older builds up to their column count, so a new column is
//     APPENDED here, never inserted, and TestPoolMatchesGoldenBytes pins the
//     result. Reordering MatchResult's fields must never move a column.
//   - "Should this field be persisted?" is a judgement, which is why this is
//     an explicit list rather than reflection: a reflective codec would have
//     happily persisted QueuePosition (derived from the schedule, a second
//     source of truth that can disagree) and Rev/RevSession/WinnerSide.
//     TestPoolMatchRoundTripIsComplete forces that judgement into the open
//     for every new MatchResult field.
//
// put runs against the row as it should be PERSISTED, so every put is a pure
// field projection against the MatchResult as stored — no codec wraps around
// it. The judges'-decision mark is a real entry in the winner's IpponsA/
// IpponsB slice (domain.HanteiMark), so the IpponsA/IpponsB columns persist
// it exactly like any other ippon letter; nothing here needs to know it is
// special. takes run in column order against a zero MatchResult seeded with
// Round: -1 (the absent-column default), so MatchIdx's take may build on the
// ID PoolName's take just set; a short row simply stops early, which is how
// files written before a column existed load with that field at its
// documented default. The one thing that DOES run outside the column loop is
// legacy-flag folding: parsePoolMatchesRecords calls NormalizeLegacyHantei
// after the loop to fold an old decidedByHantei boolean (nested in the
// SubResults JSON cell) into the mark for pre-ruling files; that is a
// read-side upgrade, not a codec the table's own columns need to know about.
type poolMatchColumn struct {
	name string
	put  func(r *MatchResult) string
	take func(m *MatchResult, cell string)
}

// poolMatchIDParts splits a pool match ID ("Pool A-3", "Pool B-DH-1") at the
// FIRST dash into the PoolName and MatchIdx columns. An ID without a dash
// stores an empty MatchIdx.
//
// strings.Cut rather than SplitN: it returns substrings of the original string
// instead of allocating a slice, and the PoolName and MatchIdx puts each call
// this once per row.
func poolMatchIDParts(id string) (pool, idx string) {
	pool, idx, _ = strings.Cut(id, "-")
	return pool, idx
}

// strCol builds the plain pass-through column: put reads the field, take
// writes the cell back into it verbatim. Both directions go through ONE
// accessor, so a put that reads SideAID next to a take that writes SideBID --
// a two-character copy-paste slip that reads as correct -- cannot be written.
func strCol(name string, field func(m *MatchResult) *string) poolMatchColumn {
	return poolMatchColumn{
		name: name,
		put:  func(r *MatchResult) string { return *field(r) },
		take: func(m *MatchResult, cell string) { *field(m) = cell },
	}
}

// intCol is strCol for a plain numeric column: an unparseable cell leaves the
// field at its seeded zero.
func intCol(name string, field func(m *MatchResult) *int) poolMatchColumn {
	return poolMatchColumn{
		name: name,
		put:  func(r *MatchResult) string { return strconv.Itoa(*field(r)) },
		take: func(m *MatchResult, cell string) {
			if v, err := strconv.Atoi(cell); err == nil {
				*field(m) = v
			}
		},
	}
}

// clampedIntCol is intCol restricted to positive values: non-numeric,
// zero and negative cells all read as the seeded 0.
func clampedIntCol(name string, field func(m *MatchResult) *int) poolMatchColumn {
	return poolMatchColumn{
		name: name,
		put:  func(r *MatchResult) string { return strconv.Itoa(*field(r)) },
		take: func(m *MatchResult, cell string) {
			if v, err := strconv.Atoi(cell); err == nil && v > 0 {
				*field(m) = v
			}
		},
	}
}

var poolMatchColumns = []poolMatchColumn{
	{name: "PoolName",
		put:  func(r *MatchResult) string { p, _ := poolMatchIDParts(r.ID); return p },
		take: func(m *MatchResult, cell string) { m.ID = cell }},
	{name: "MatchIdx",
		put:  func(r *MatchResult) string { _, idx := poolMatchIDParts(r.ID); return idx },
		take: func(m *MatchResult, cell string) { m.ID += "-" + cell }},
	strCol("SideA", func(m *MatchResult) *string { return &m.SideA }),
	strCol("SideB", func(m *MatchResult) *string { return &m.SideB }),
	strCol("Winner", func(m *MatchResult) *string { return &m.Winner }),
	// The ippon cells hold the PERSISTED slices: waza letters joined with "|",
	// plus the judges'-decision mark in the winner's cell when the match was
	// decided by hantei. The mark (domain.HanteiMark) is a real entry in that
	// slice — the mark IS the record — so it round-trips through this column
	// like any other letter, with no wrapping codec on either side.
	{name: "IpponsA",
		put:  func(r *MatchResult) string { return strings.Join(r.IpponsA, "|") },
		take: func(m *MatchResult, cell string) { m.IpponsA = splitIppons(cell) }},
	{name: "IpponsB",
		put:  func(r *MatchResult) string { return strings.Join(r.IpponsB, "|") },
		take: func(m *MatchResult, cell string) { m.IpponsB = splitIppons(cell) }},
	intCol("HansokuA", func(m *MatchResult) *int { return &m.HansokuA }),
	intCol("HansokuB", func(m *MatchResult) *int { return &m.HansokuB }),
	strCol("Decision", func(m *MatchResult) *string { return &m.Decision }),
	{name: "Status",
		put:  func(r *MatchResult) string { return string(r.Status) },
		take: func(m *MatchResult, cell string) { m.Status = MatchStatus(cell) }},
	strCol("Court", func(m *MatchResult) *string { return &m.Court }),
	// A team encounter's sub-bouts nest as a JSON document inside this one
	// cell, keeping the file to one row per match. Marshalling the whole
	// struct means every SubMatchResult field is persisted the moment it is
	// declared (the property bracket.json has always had); the trade, recorded
	// in docs/architecture/data-model.md, is that the richest data in a team
	// competition is not readable as columns.
	{name: "SubResults",
		put: func(r *MatchResult) string {
			if len(r.SubResults) == 0 {
				return ""
			}
			b, err := json.Marshal(r.SubResults)
			if err != nil {
				// The column type is string, not (string, error): every other
				// put in this table is infallible (strconv formatting never
				// fails), so there is no error path to propagate this into.
				// Log it so a marshal failure on live team-match data is at
				// least visible instead of silently writing an empty cell.
				slog.Error("state: pool match SubResults marshal failed; writing empty cell",
					"matchID", r.ID, "error", err)
				return ""
			}
			return string(b)
		},
		take: func(m *MatchResult, cell string) {
			if cell == "" {
				return
			}
			if err := json.Unmarshal([]byte(cell), &m.SubResults); err != nil {
				// Same infallible-signature constraint as put above: a
				// corrupt cell must not fail the whole row read (the rest of
				// the match is still good), but it must not load in silence
				// either, or a hand-edited/truncated cell reads back as an
				// empty team encounter with no trace of the data loss.
				slog.Error("state: pool match SubResults cell corrupt; loading as empty",
					"matchID", m.ID, "error", err)
			}
		}},
	strCol("ScheduledAt", func(m *MatchResult) *string { return &m.ScheduledAt }),
	strCol("ResultSource", func(m *MatchResult) *string { return &m.ResultSource }),
	// Round reads -1 for "unknown" in every failure shape -- an empty or
	// unparseable cell, or a row too short to hold the column at all -- by
	// leaving the reader's seed value (Round: -1) alone, like every other
	// column with an absent-default.
	intCol("Round", func(m *MatchResult) *int { return &m.Round }),
	// Participant-id columns. Absent in files written before they existed;
	// ids stay empty and consumers fall back to name matching.
	strCol("SideAID", func(m *MatchResult) *string { return &m.SideAID }),
	strCol("SideBID", func(m *MatchResult) *string { return &m.SideBID }),
	strCol("WinnerID", func(m *MatchResult) *string { return &m.WinnerID }),
	strCol("CorrectionReason", func(m *MatchResult) *string { return &m.CorrectionReason }),
	// Rep-player columns (mp-62vr): the individual fighters each team fields
	// for a pool/league daihyosen/tiebreaker rep bout.
	strCol("RepPlayerA", func(m *MatchResult) *string { return &m.RepPlayerA }),
	strCol("RepPlayerB", func(m *MatchResult) *string { return &m.RepPlayerB }),
	// Engi referee flag counts. A non-numeric value is treated as 0 and a
	// negative value is clamped to 0: flags are validated non-negative at the
	// HTTP boundary, so a corrupted / hand-edited file must not load negative
	// counts that would break engi standings/rendering.
	clampedIntCol("FlagsA", func(m *MatchResult) *int { return &m.FlagsA }),
	clampedIntCol("FlagsB", func(m *MatchResult) *int { return &m.FlagsB }),
	// The "reopened without an audit reason yet" flag (mp-gmcg). A non-boolean
	// value reads as false: a hand-edited CSV must not be able to wedge a
	// match behind a justification it can never satisfy.
	{name: "ReopenPending",
		put: func(r *MatchResult) string { return strconv.FormatBool(r.ReopenPending) },
		take: func(m *MatchResult, cell string) {
			if v, err := strconv.ParseBool(cell); err == nil {
				m.ReopenPending = v
			}
		}},
	// Decision-audit columns: WHO recorded a kiken/fusenpai and WHY. These
	// were held only in memory once, so the audit trail behind a withdrawal
	// survived until the next restart and no further, while the bracket kept
	// both all along (bracket.json marshals every exported field).
	strCol("DecisionBy", func(m *MatchResult) *string { return &m.DecisionBy }),
	strCol("DecisionReason", func(m *MatchResult) *string { return &m.DecisionReason }),
	// Encho (overtime) collapses to its period count, gated on Encho.On() in
	// BOTH directions: only a positive count is written and only a positive
	// count rebuilds the block, which is lossless because On() requires
	// non-nil AND positive, so a nil block and a degenerate {PeriodCount: 0}
	// are already indistinguishable to every consumer. A negative value is
	// rejected at the HTTP boundary and reads here as absent, so a hand-edited
	// file cannot load one.
	{name: "Encho",
		put: func(r *MatchResult) string {
			if r.Encho.On() {
				return strconv.Itoa(r.Encho.PeriodCount)
			}
			return "0"
		},
		take: func(m *MatchResult, cell string) {
			if v, err := strconv.Atoi(cell); err == nil && v > 0 {
				m.Encho = &EnchoMetadata{PeriodCount: v}
			}
		}},
	// The server-relative stamp the timestamp last-write-wins guard compares.
	// It has to be PERSISTED for that guard to survive a restart. Unparseable
	// or negative reads as 0, which domain.ApplyByTimestamp treats as
	// unstamped and therefore always-applies: a corrupt cell degrades to
	// arrival order, never to silently dropping an operator's write.
	{name: "ModifiedAt",
		put: func(r *MatchResult) string { return strconv.FormatInt(r.ModifiedAt, 10) },
		take: func(m *MatchResult, cell string) {
			if v, err := strconv.ParseInt(cell, 10, 64); err == nil && v > 0 {
				m.ModifiedAt = v
			}
		}},
}

// poolMatchHeader derives the CSV header row from the table.
func poolMatchHeader() []string {
	names := make([]string, len(poolMatchColumns))
	for i, c := range poolMatchColumns {
		names[i] = c.name
	}
	return names
}

// poolMatchesLegacyCoreColumns is the width of the original pre-append layout;
// a row narrower than this is malformed and skipped rather than half-read.
const poolMatchesLegacyCoreColumns = 12

// parsePoolMatchesRecords turns a CSV record matrix into MatchResults.
// Extracted so the file-based and bytes-based parsers share the
// rec-shape→struct mapping verbatim (no drift between the two). Each cell is
// read by its poolMatchColumns take, so the reader cannot disagree with the
// writer about what a column holds.
func parsePoolMatchesRecords(records [][]string) []MatchResult {
	results := []MatchResult{}
	for i, rec := range records {
		if i == 0 && len(rec) > 0 && rec[0] == poolMatchColumns[0].name {
			continue // skip header
		}
		if len(rec) < poolMatchesLegacyCoreColumns {
			continue
		}

		// Round: -1 is the absent-column default (see poolMatchColumns).
		m := MatchResult{Round: -1}
		for c, col := range poolMatchColumns {
			if c >= len(rec) {
				break
			}
			col.take(&m, rec[c])
		}

		// The judges'-decision mark loads as the plain ippon entry it is (the
		// IpponsA/IpponsB take above already read it, verbatim, into the
		// slice). NormalizeLegacyHantei's only remaining job here is folding
		// LEGACY decidedByHantei flags nested in the SubResults JSON cell
		// (pre-ruling files) into that same mark form; see legacy_hantei.go.
		m.NormalizeLegacyHantei()

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
// The write parameter routes the actual file write, directWrite for
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
	if err := writer.Write(poolMatchHeader()); err != nil {
		return err
	}

	for _, r := range results {
		row := make([]string, len(poolMatchColumns))
		for c, col := range poolMatchColumns {
			row[c] = col.put(&r)
		}
		if err := writer.Write(row); err != nil {
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

	// Bump last: this is the single chokepoint every pool-matches writer funnels
	// through (SavePoolMatches, UpdatePoolMatchByID, and both storeTx variants),
	// so downstream caches keyed on FileVersion invalidate without each of those
	// call sites having to remember to do it.
	s.bumpFileVersion(compID, "pool-matches.csv")

	return nil
}

// UpdatePoolMatchByID atomically loads the pool-matches CSV for compID,
// finds the match with matchID, calls mutate on it, and persists the
// updated slice. Returns (found, err): found is false when no match
// has that ID, allowing callers to fall through (e.g. to the bracket
// store for elimination-round matches).
//
// The entire load + find + mutate + save sequence runs under the
// per-competition lock so concurrent calls, even for different
// match IDs in the same competition, serialize correctly without
// losing each other's mutations.
//
// Without this primitive, the equivalent engine helper
// (engine.withPoolMatch) had a TOCTOU window: two operators scoring
// different matches on different courts could each LoadPoolMatches
// into separate copies, mutate their target match, and SavePoolMatches
// in sequence, the later save would overwrite the earlier save's
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
