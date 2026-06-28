package state

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// ErrParticipantNotFound is returned by UpdateParticipant when the pid is not in the roster.
var ErrParticipantNotFound = errors.New("participant not found")

// ErrDuplicateName is returned by AddParticipant, UpdateParticipant, and the
// bulk write path when the supplied (Player.Name, Player.Dojo) pair collides
// with another participant in the same roster (excluding the participant being
// edited, for the update path). The comparison is on the normalized
// (name, dojo) key — the SAME name at a DIFFERENT dojo is allowed (two real
// people at different clubs), so the message names both fields.
var ErrDuplicateName = errors.New("a participant with the same name and dojo already exists")

// ErrCompetitionNotInSetup is returned by the setup-gated write paths
// (Store.AddParticipant and Store.ReplaceParticipant — both call
// requireSetupLocked) when the competition has already advanced past the
// setup phase. The status is re-checked under the per-competition lock so a
// concurrent POST /competitions/:id/start landing between the handler's
// outer check and the store call cannot leak a roster write into a started
// competition.
//
// Store.UpdateParticipant is intentionally NOT gated by this error — check-in
// toggles must keep working while the competition is running. Future setup-
// only write paths should call requireSetupLocked under the same lock as
// AddParticipant/ReplaceParticipant do.
var ErrCompetitionNotInSetup = errors.New("competition has already started")

// ErrReservedName is returned by the participant write paths when a name
// matches an internal bracket-placeholder pattern (e.g. "Pool A-1st",
// "Winner of r1-m3").  Such names would be silently misclassified as
// unresolved bracket slots, making knockout matches permanently unscoreable.
var ErrReservedName = errors.New("participant name collides with a reserved bracket-placeholder pattern")

// LoadParticipantsOpts controls optional behavior in LoadParticipants.
type LoadParticipantsOpts struct {
	WithSeeds bool  // set false to skip the seeds.csv read (hot list paths)
	HasIDs    *bool // nil = auto-detect from first line; non-nil uses cached Competition.HasParticipantIDs
}

// participantsCacheKey returns a virtual filename used as the cache key
// for a participants load. Splits by both WithSeeds and HasIDs parse
// mode so the three mutually-exclusive parses don't poison each other's
// cache entries (mp-p7n Copilot PR #185 round-6).
func participantsCacheKey(opts LoadParticipantsOpts) string {
	base := "participants"
	if opts.WithSeeds {
		base += "_with_seeds"
	}
	switch {
	case opts.HasIDs == nil:
		base += "_auto"
	case *opts.HasIDs:
		base += "_hint_true"
	default:
		base += "_hint_false"
	}
	return base + ".csv"
}

// allParticipantsCacheKeys enumerates every cache key that
// participantsCacheKey can produce. Used by saveParticipantsNoLock to
// invalidate all parse-mode variants in one pass on write.
func allParticipantsCacheKeys() []string {
	keys := make([]string, 0, 6)
	for _, withSeeds := range []bool{false, true} {
		trueP, falseP := true, false
		for _, hint := range []*bool{nil, &trueP, &falseP} {
			keys = append(keys, participantsCacheKey(LoadParticipantsOpts{
				WithSeeds: withSeeds,
				HasIDs:    hint,
			}))
		}
	}
	return keys
}

// LoadParticipants loads participants with seeds merged (default behavior).
func (s *Store) LoadParticipants(compID string, withZekkenName bool) ([]domain.Player, error) {
	return s.loadParticipants(compID, withZekkenName, LoadParticipantsOpts{WithSeeds: true})
}

// LoadParticipantsOpt loads participants with configurable options.
func (s *Store) LoadParticipantsOpt(compID string, withZekkenName bool, opts LoadParticipantsOpts) ([]domain.Player, error) {
	return s.loadParticipants(compID, withZekkenName, opts)
}

func (s *Store) loadParticipants(compID string, withZekkenName bool, opts LoadParticipantsOpts) ([]domain.Player, error) {
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()
	return s.loadParticipantsNoLock(compID, withZekkenName, opts)
}

func (s *Store) loadParticipantsNoLock(compID string, withZekkenName bool, opts LoadParticipantsOpts) ([]domain.Player, error) {
	// Use a virtual filename for cache to distinguish between with/without
	// seeds AND between the three possible parse modes
	// (HasIDs=&true / HasIDs=&false / nil → auto-detect).
	//
	// mp-p7n / Copilot PR #185 round-6: without the parse-mode suffix,
	// a no-hint auto-detect call that lands a "no-IDs" parse can poison
	// the same cache key that a later HasIDs=&true call reads — the
	// hinted call would return the cached shifted rows instead of
	// stripping column 0. Splitting the cache key by parse mode means
	// each mode's parse is cached independently. saveParticipantsNoLock
	// invalidates all variants below to keep them coherent on write.
	cacheKey := participantsCacheKey(opts)

	cache := s.getFileCache(compID, cacheKey)
	cache.mu.RLock()
	mtime := s.FileMtime(compID, "participants.csv")
	if opts.WithSeeds {
		mtime += s.FileMtime(compID, "seeds.csv")
	}
	// mp-p7n / Copilot PR #185 round-4: the load decision now depends
	// on Competition.HasParticipantIDs (see the trustHint branch below).
	// That flag lives in config.md, NOT participants.csv, so a flag flip
	// (e.g. the deferred HasParticipantIDs=true after the first roster
	// save) wouldn't bump participants.csv's mtime and would leave a
	// stale "no-IDs" parse in the cache. Fold config.md's mtime into
	// the cache key so any config write invalidates the cached players.
	mtime += s.FileMtime(compID, "config.md")

	if cache.data != nil && cache.mtime == mtime {
		p := cache.data.([]domain.Player)
		res := make([]domain.Player, len(p))
		copy(res, p)
		cache.mu.RUnlock()
		return res, nil
	}
	cache.mu.RUnlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	// Re-check after acquiring write lock
	if cache.data != nil && cache.mtime == mtime {
		p := cache.data.([]domain.Player)
		res := make([]domain.Player, len(p))
		copy(res, p)
		return res, nil
	}

	path := s.compPath(compID, "participants.csv")
	records, err := helper.ReadCSVFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cache.data = []domain.Player{}
			cache.mtime = mtime
			return []domain.Player{}, nil
		}
		return nil, err
	}

	// Detect ID column: when callers pass an explicit HasIDs hint we
	// trust it; otherwise consult Competition.HasParticipantIDs (set on
	// disk the first time participants are saved with IDs) and fall
	// back to a UUID-shape sniff on the first row only when no comp
	// record is available.
	//
	// mp-p7n: previously this path used only uuidRE on records[0][0],
	// which mis-classified non-UUID-shaped ids (e.g. the JS-side
	// `${compID}-p${N}` shape) as "no IDs" → column shift on load. The
	// HasParticipantIDs flag is the authoritative signal: when set,
	// every row has an id in column 0 regardless of its textual shape,
	// and the loader must strip it. Auto-detect against the file is
	// only used when we have no comp record (e.g. a stand-alone CSV
	// loaded outside the normal handler flow).
	// hasIDs: there is an id column in the file at all.
	// trustHint: strip column 0 from EVERY row unconditionally — set
	// only when the caller (or the comp's HasParticipantIDs flag)
	// asserts "every row has an id". Without this signal, the auto-
	// detect path falls back to a per-record UUID-shape check below
	// to preserve mixed-format legacy support (TestStore_ParticipantsCSV_MixedIDs:
	// a file with one UUID row and one bare-name row must load both).
	var hasIDs, trustHint bool
	switch {
	case opts.HasIDs != nil:
		hasIDs = *opts.HasIDs
		trustHint = *opts.HasIDs
	default:
		if comp, _ := s.loadCompetitionLocked(compID); comp != nil && comp.HasParticipantIDs {
			hasIDs = true
			trustHint = true
		} else {
			hasIDs = len(records) > 0 && len(records[0]) > 0 && uuidRE(strings.TrimSpace(records[0][0]))
			trustHint = false
		}
	}

	var ids []string
	var checkedInFlags []bool
	var playerRecords [][]string

	for _, record := range records {
		isCheckedIn := false

		// Determine data fields (everything after the id if present).
		//
		// mp-p7n: pre-fix, this branch gated on `uuidRE(record[0])` —
		// a per-record check that only stripped the first field when
		// it matched the canonical-UUID shape. That broke any roster
		// whose ids weren't UUID-shaped (e.g. the `${compID}-p${N}`
		// shape the JS-side mintParticipantIds was generating, or
		// arbitrary client-supplied ids): dataStart stayed 0, the
		// row was parsed as [Name, Dojo, Metadata] instead of
		// [id, Name, Dojo, Metadata], every field shifted one column
		// right, and the id got title-cased into Name on load
		// (producing the user-reported "Asddasd-P1, Aaron Adams,
		// Team Alpha" corruption).
		//
		// Fix:
		//   - `trustHint` (caller-supplied or comp.HasParticipantIDs=true):
		//     strip column 0 unconditionally — every row has an id of
		//     whatever shape. Preserves non-UUID ids (a client/import
		//     path may carry one) and keeps them joinable with other
		//     persisted state that references the player by id —
		//     CompetitorStatus.PlayerID, team-lineup PlayerIDs (Copilot
		//     PR #185 round-3: an alternative that regenerated non-UUID
		//     ids at save time would silently orphan all those references).
		//   - Auto-detected `hasIDs` (UUID-shape sniff on the first
		//     row): keep the per-record uuidRE check so mixed-format
		//     legacy files (some rows with UUID ids, some without)
		//     load both kinds correctly — TestStore_ParticipantsCSV_MixedIDs
		//     pins this contract.
		dataStart := 0
		if hasIDs && len(record) > 0 {
			if trustHint || uuidRE(strings.TrimSpace(record[0])) {
				dataStart = 1
			}
		}
		dataFields := record[dataStart:]

		// Skip records that are empty after UUID stripping (e.g. a
		// UUID-only row) — CreatePlayersFromRecords would skip these
		// too, and the metadata slices must stay aligned.
		allEmpty := true
		for _, f := range dataFields {
			if strings.TrimSpace(f) != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			continue
		}

		// Detect and strip checked_in marker from the last data field.
		// Minimum valid data row is "Name,Dojo" (2 parts), so checked_in
		// is only treated as a marker when at least 3 data parts are present.
		if len(dataFields) > 2 {
			last := strings.TrimSpace(dataFields[len(dataFields)-1])
			if strings.ToLower(last) == "checked_in" {
				isCheckedIn = true
				dataFields = dataFields[:len(dataFields)-1]
			}
		}

		if hasIDs {
			id := ""
			if dataStart > 0 {
				id = strings.TrimSpace(record[0])
			}
			ids = append(ids, id)
		}
		playerRecords = append(playerRecords, dataFields)
		checkedInFlags = append(checkedInFlags, isCheckedIn)
	}

	players, err := helper.CreatePlayersFromRecords(playerRecords, withZekkenName)
	if err != nil {
		return nil, err
	}

	for i := range players {
		if hasIDs && i < len(ids) {
			players[i].ID = ids[i]
		}
		if i < len(checkedInFlags) {
			players[i].CheckedIn = checkedInFlags[i]
		}
	}

	if opts.WithSeeds {
		// Merge seeds if they exist.
		seeds, _ := helper.ParseSeedsFile(s.compPath(compID, "seeds.csv"))
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
	}
	// helper.Player is a type alias for domain.Player (NFR-007); the
	// parser output can flow straight into the cache without conversion.
	cache.data = players
	cache.mtime = mtime

	res := make([]domain.Player, len(players))
	copy(res, players)
	return res, nil
}

// SaveParticipants persists the roster. It loads the competition under the
// per-comp lock to determine WithZekkenName so the on-disk CSV layout matches
// what the next LoadParticipants(_, comp.WithZekkenName) call will read —
// passing the wrong column count corrupts the file on the next round-trip.
func (s *Store) SaveParticipants(compID string, players []domain.Player) error {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	withZekken, err := s.withZekkenNameLocked(compID)
	if err != nil {
		return err
	}
	return s.saveParticipantsNoLock(compID, players, withZekken)
}

// withZekkenNameLocked returns the WithZekkenName flag for compID. Caller MUST
// hold the per-comp lock. Returns false when the competition record is missing
// (matches the default zero value SaveParticipants would otherwise observe).
func (s *Store) withZekkenNameLocked(compID string) (bool, error) {
	comp, err := s.loadCompetitionLocked(compID)
	if err != nil {
		return false, err
	}
	if comp == nil {
		return false, nil
	}
	return comp.WithZekkenName, nil
}

// participantPairKey is the (normalizedName, normalizedDojo) identity key used
// to resolve a participant when no stable UUID is available. It mirrors the
// Tier-1 dedup key (NormalizeParticipantName on both halves joined by "|"), so
// the pair is unique across a roster by construction — name alone is NOT unique
// (same name at a different dojo is allowed). Keep in sync with checkInKey in
// handlers_participants.go and checkinPid in web-mobile/js/data.jsx.
func participantPairKey(name, dojo string) string {
	return helper.NormalizeParticipantName(name) + "|" + helper.NormalizeParticipantName(dojo)
}

// pidPairKey derives the (normalizedName, normalizedDojo) key from a composite
// "name|dojo" pid sent by the client for legacy UUID-less rows. The raw pid is
// split on the FIRST "|" and each half normalized separately — splitting before
// normalizing trims whitespace around the delimiter, which normalizing the whole
// string would not. A pid with no "|" resolves as a name with an empty dojo.
func pidPairKey(pid string) string {
	name, dojo, _ := strings.Cut(pid, "|")
	return participantPairKey(name, dojo)
}

// resolveParticipantIndex finds the index of the participant addressed by pid.
// It matches a stable UUID first; failing that — legacy participants.csv files
// loaded without a UUID column have empty IDs (loadParticipantsNoLock mints
// none; the first write via saveParticipantsNoLock migrates those rows to UUIDs
// because marshalParticipantsCSV backfills empty IDs) — it falls back to
// matching the composite "name|dojo" key the client sends for ID-less rows. The
// fallback is restricted to ID-less rows (UUID rows are only addressable by
// their id) and the (name, dojo) pair is unique per roster, so resolution is
// unambiguous. Returns -1 when no participant matches.
func resolveParticipantIndex(players []domain.Player, pid string) int {
	if pid == "" {
		return -1
	}
	for i := range players {
		if players[i].ID != "" && players[i].ID == pid {
			return i
		}
	}
	want := pidPairKey(pid)
	for i := range players {
		if players[i].ID != "" {
			continue
		}
		if participantPairKey(players[i].Name, players[i].Dojo) == want {
			return i
		}
	}
	return -1
}

// BulkCheckInResult carries the outcome of a BulkCheckIn call.
type BulkCheckInResult struct {
	CheckedIn        int      `json:"checkedIn"`
	AlreadyCheckedIn int      `json:"alreadyCheckedIn"`
	NotFound         []string `json:"notFound"`
}

// BulkCheckIn atomically marks all participants in pids as checked-in under a
// single lock acquire, writing participants.csv exactly once. Only participants
// that were not already checked in count toward CheckedIn; the file is only
// written when at least one participant was actually toggled.
func (s *Store) BulkCheckIn(compID string, pids []string) (BulkCheckInResult, error) {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	withZekkenName, err := s.withZekkenNameLocked(compID)
	if err != nil {
		return BulkCheckInResult{}, err
	}

	players, err := s.loadParticipantsNoLock(compID, withZekkenName, LoadParticipantsOpts{WithSeeds: false})
	if err != nil {
		return BulkCheckInResult{}, err
	}

	// Two lookup maps so resolution matches resolveParticipantIndex: UUID rows
	// by their stable id, legacy UUID-less rows by their (name, dojo) pair key.
	byID := make(map[string]int, len(players))
	byKey := make(map[string]int, len(players))
	for i := range players {
		if players[i].ID != "" {
			byID[players[i].ID] = i
		} else {
			byKey[participantPairKey(players[i].Name, players[i].Dojo)] = i
		}
	}

	// Deduplicate by resolved participant index so that semantically equivalent
	// pids (same normalized name|dojo with different whitespace/case) don't count
	// the same participant twice. NotFound pids are deduped by raw string because
	// there is no index to compare against.
	seenIdx := make(map[int]struct{}, len(pids))
	seenNotFound := make(map[string]struct{})
	result := BulkCheckInResult{NotFound: []string{}}
	for _, pid := range pids {
		if pid == "" {
			continue
		}
		idx, ok := byID[pid]
		if !ok {
			idx, ok = byKey[pidPairKey(pid)]
		}
		if !ok {
			if _, dup := seenNotFound[pid]; !dup {
				result.NotFound = append(result.NotFound, pid)
				seenNotFound[pid] = struct{}{}
			}
			continue
		}
		if _, dup := seenIdx[idx]; dup {
			continue
		}
		seenIdx[idx] = struct{}{}
		if players[idx].CheckedIn {
			result.AlreadyCheckedIn++
		} else {
			players[idx].CheckedIn = true
			result.CheckedIn++
		}
	}

	if result.CheckedIn > 0 {
		if err := s.saveParticipantsNoLock(compID, players, withZekkenName); err != nil {
			return BulkCheckInResult{}, err
		}
	}

	return result, nil
}

// UpdateParticipant atomically loads the participant list, applies transform
// to the target player, and persists the result. Used to avoid TOCTOU races
// on concurrent check-ins.
func (s *Store) UpdateParticipant(compID string, pid string, withZekkenName bool, transform func(p *domain.Player) error) (*domain.Player, error) {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	return s.updateParticipantNoLock(compID, pid, withZekkenName, transform)
}

// updateParticipantNoLock contains the full mutate-load-rewrite body of
// UpdateParticipant. Caller MUST hold the per-comp lock. Factored out so
// ReplaceParticipant can wrap the same logic under a status-gated lock
// without forcing the lock-only check-in callers to pay for that gate.
func (s *Store) updateParticipantNoLock(compID string, pid string, withZekkenName bool, transform func(p *domain.Player) error) (*domain.Player, error) {
	// Seeds are not merged here — check-in mutations don't need seed data.
	players, err := s.loadParticipantsNoLock(compID, withZekkenName, LoadParticipantsOpts{WithSeeds: false})
	if err != nil {
		return nil, err
	}

	// Resolve by stable UUID, falling back to the composite "name|dojo" key for
	// legacy UUID-less rosters (see resolveParticipantIndex).
	foundIdx := resolveParticipantIndex(players, pid)
	if foundIdx == -1 {
		return nil, ErrParticipantNotFound
	}

	oldName := players[foundIdx].Name

	if err := transform(&players[foundIdx]); err != nil {
		return nil, err
	}

	// Reserved-name check before TitleCase, but ONLY when the name actually
	// changed.  Check-in transforms leave the name untouched; running the
	// guard unconditionally would break check-in for any participant whose
	// stored name happens to match the reserved pattern (however unlikely).
	// When the name DID change, check the raw pre-TitleCase value: TitleCase
	// alters ordinal suffixes ("3rd"→"3Rd") so the post-TitleCase form would
	// never match the reserved regex.
	// Detect rename by comparing raw values (before TrimSpace) so that a
	// stored name like "Alice" is never spuriously considered changed by a
	// check-in transform that doesn't touch Name at all.  TrimSpace is only
	// for the regex match, not for change detection.
	if players[foundIdx].Name != oldName {
		trimmedName := strings.TrimSpace(players[foundIdx].Name)
		if helper.IsReservedParticipantName(trimmedName) {
			return nil, fmt.Errorf("%w: %q", ErrReservedName, trimmedName)
		}
	}

	// Canonicalize to match what CreatePlayers produces on load (Title-case),
	// so participants.csv and seeds.csv store the same form that will be read
	// back — otherwise a rename to "alice cooper" would be read as "Alice Cooper"
	// while seeds.csv still holds "alice cooper", breaking seed merging.
	players[foundIdx].Name = helper.TitleCaseName(players[foundIdx].Name)

	// Duplicate guard: reject if any OTHER participant already has the same
	// (normalizedName, normalizedDojo) pair. This runs unconditionally — even
	// for check-in-only transforms that don't rename — which is harmless
	// because the participant being edited is skipped (i == foundIdx) and a
	// no-op edit can't collide with itself. Using both fields allows same-named
	// competitors from different clubs while rejecting diacritic/casing variants.
	newNormName := helper.NormalizeParticipantName(players[foundIdx].Name)
	newNormDojo := helper.NormalizeParticipantName(players[foundIdx].Dojo)
	for i := range players {
		if i == foundIdx {
			continue
		}
		if helper.NormalizeParticipantName(players[i].Name) == newNormName &&
			helper.NormalizeParticipantName(players[i].Dojo) == newNormDojo {
			return nil, ErrDuplicateName
		}
	}

	// Pre-validate and load seeds before touching participants.csv so
	// that a corrupt seeds file is caught before any disk write. If seeds
	// doesn't exist, seeds is nil and the rename step is skipped.
	var seeds []domain.SeedAssignment
	var seedsPath string
	if oldName != players[foundIdx].Name {
		seedsPath = s.compPath(compID, "seeds.csv")
		var loadErr error
		seeds, loadErr = helper.ParseSeedsFile(seedsPath)
		switch {
		case loadErr == nil:
			// seeds loaded; will rename below.
		case errors.Is(loadErr, os.ErrNotExist):
			seeds = nil // no seeds file — nothing to rename
		default:
			return nil, fmt.Errorf("load seeds for rename of %q: %w", oldName, loadErr)
		}
	}

	// Write seeds.csv BEFORE participants.csv so that a failure on the
	// participants write leaves a retryable state: seeds already carries the
	// new name, so a retry will see changed=false (oldName no longer in seeds)
	// and skip the rename, then successfully write participants. The reverse
	// order (participants first) is not retryable — oldName is gone from
	// participants so the seeds rename can never be applied again.
	if oldName != players[foundIdx].Name && seeds != nil {
		changed := false
		for i := range seeds {
			if seeds[i].Name == oldName {
				seeds[i].Name = players[foundIdx].Name
				changed = true
			}
		}
		if changed {
			// Use encoding/csv so names containing commas / quotes (e.g.
			// "Smith, John") are properly escaped — fmt.Fprintf("%d,%s\n")
			// would emit broken CSV that ParseSeedsFile then mis-splits,
			// silently dropping seeds. Mirrors Store.SaveSeeds.
			var sb strings.Builder
			w := csv.NewWriter(&sb)
			if werr := w.Write([]string{"Rank", "Name"}); werr != nil {
				return nil, fmt.Errorf("rename seed for %q: writing header: %w", oldName, werr)
			}
			for _, a := range seeds {
				if werr := w.Write([]string{strconv.Itoa(a.SeedRank), a.Name}); werr != nil {
					return nil, fmt.Errorf("rename seed for %q: writing record: %w", oldName, werr)
				}
			}
			w.Flush()
			if werr := w.Error(); werr != nil {
				return nil, fmt.Errorf("rename seed for %q: flushing: %w", oldName, werr)
			}
			if werr := s.atomicWrite(seedsPath, []byte(sb.String()), 0600); werr != nil {
				return nil, fmt.Errorf("rename seed for %q: %w", oldName, werr)
			}
		}
	}

	if err := s.saveParticipantsNoLock(compID, players, withZekkenName); err != nil {
		return nil, err
	}
	return &players[foundIdx], nil
}

// loadParticipantsLocked reads participants (with seeds merged) without
// acquiring a lock. Caller must hold the per-comp lock for compID. Used by
// the StoreTx transaction handle (transactions.go), which already holds the
// per-comp lock across its load/save sequence.
//
// Delegates to the canonical loadParticipantsNoLock (WithSeeds:true, no HasIDs
// hint → it consults Competition.HasParticipantIDs and keeps the per-record
// uuidRE fallback for mixed legacy files) so the parse logic lives in one
// place.
func (s *Store) loadParticipantsLocked(compID string, withZekkenName bool) ([]domain.Player, error) {
	return s.loadParticipantsNoLock(compID, withZekkenName, LoadParticipantsOpts{WithSeeds: true})
}

// marshalParticipantsCSV serialises players into RFC 4180 CSV bytes.
// Shared by saveParticipantsNoLock and the single-add endpoint so the
// display-name and checked_in logic is defined once.
//
// withZekkenName MUST match the value the next LoadParticipants will use.
// The on-disk column layout differs between the two modes:
//   - non-zekken: [id, Name, Dojo, ...Metadata, Source?, "checked_in"?]
//   - zekken:    [id, Name, DisplayName, Dojo, ...Metadata, Source?, "checked_in"?]
//
// Pre-fix the function tried to be clever by writing the 2-column form (id,
// Name, Dojo) when DisplayName was empty or auto-derivable. That broke zekken
// reloads as soon as Source or Metadata were present — e.g. the manual-source
// default added by the single-add endpoint produced [id, Name, Dojo, "manual"]
// for zekken comps, which CreatePlayersFromRecords(_, true) then read as
// Name=Name, DisplayName=Dojo, Dojo="manual" (corrupted). Branch on
// withZekkenName so each mode gets its canonical layout regardless of which
// optional trailing fields are present.
func marshalParticipantsCSV(players []domain.Player, withZekkenName bool) ([]byte, error) {
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	for _, p := range players {
		id := p.ID
		if id == "" {
			id = newParticipantID()
		}
		var record []string
		if withZekkenName {
			// Always include the DisplayName column so a subsequent
			// LoadParticipants(_, true) reads [Name, DisplayName, Dojo, ...]
			// — even when DisplayName equals SanitizeName(Name) and even when
			// Source/Metadata are present.
			dn := p.DisplayName
			if dn == "" {
				dn = helper.SanitizeName(p.Name)
			}
			record = []string{id, p.Name, dn, p.Dojo}
		} else {
			record = []string{id, p.Name, p.Dojo}
		}
		record = append(record, p.Metadata...)
		// Canonicalize (trim + lower-case) at the write chokepoint so participants.csv
		// is always normalized regardless of which handler built the Player — keeps
		// the loader from shifting a stray-cased value into Metadata and avoids
		// split filter buckets. Only persist a RECOGNIZED source: an unknown value
		// (set by some internal caller bypassing the API validator) would otherwise
		// be written as a column and reloaded as Metadata — a lossy round-trip — so
		// drop it instead.
		if src := helper.CanonicalRegistrationSource(p.Source); helper.IsRegistrationSource(src) {
			record = append(record, src)
		}
		if p.CheckedIn {
			record = append(record, "checked_in")
		}
		if err := w.Write(record); err != nil {
			return nil, fmt.Errorf("writing participant CSV record: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("flushing participant CSV: %w", err)
	}
	return []byte(sb.String()), nil
}

// requireSetupLocked returns ErrCompetitionNotInSetup when the on-disk
// competition status is past the setup phase. Caller MUST already hold the
// per-comp lock. Treats "missing competition" and "empty status" as setup
// (mirroring the handler-side check), so a fresh test fixture that doesn't
// explicitly set Status still passes.
func (s *Store) requireSetupLocked(compID string) error {
	comp, err := s.loadCompetitionLocked(compID)
	if err != nil {
		return err
	}
	if comp == nil {
		return nil // missing file: treat as fresh / setup (matches handler semantics)
	}
	if comp.Status != "" && comp.Status != CompStatusSetup {
		return ErrCompetitionNotInSetup
	}
	return nil
}

// AddParticipant atomically loads the participant list, mints a UUIDv4 ID,
// sets PoolPosition to the new tail index, appends the participant, and
// saves the file. Matches the ID format used everywhere else
// (see newParticipantID and saveParticipantsNoLock's ID-fill branch) so
// the format-sniffer in loadParticipantsNoLock keeps a single contract.
//
// Re-checks the competition status under the per-comp lock — a concurrent
// POST /competitions/:id/start landing between the handler's outer check and
// this lock would otherwise leak a roster mutation into a started competition.
func (s *Store) AddParticipant(compID string, p domain.Player, withZekkenName bool) (*domain.Player, error) {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	if err := s.requireSetupLocked(compID); err != nil {
		return nil, err
	}

	players, err := s.loadParticipantsNoLock(compID, withZekkenName, LoadParticipantsOpts{})
	if err != nil {
		return nil, err
	}

	// Duplicate-name guard: reject when (normalizedName, normalizedDojo)
	// matches an existing entry. Using both name and dojo means that two
	// real people at different clubs with the same name are allowed, while
	// diacritic / casing variants ("Müller/Wakaba" vs "muller/wakaba") are
	// correctly rejected.
	for _, existing := range players {
		if helper.NormalizeParticipantName(existing.Name) == helper.NormalizeParticipantName(strings.TrimSpace(p.Name)) &&
			helper.NormalizeParticipantName(existing.Dojo) == helper.NormalizeParticipantName(strings.TrimSpace(p.Dojo)) {
			return nil, ErrDuplicateName
		}
	}

	// Reserved-name check before TitleCase so the error fires on the raw input
	// ("Pool B-3rd") even though TitleCase would transform the ordinal suffix to
	// a non-matching form ("Pool B-3Rd"). The bulk SaveParticipants path skips
	// TitleCase, so saveParticipantsNoLock also carries this guard for that path.
	trimmedName := strings.TrimSpace(p.Name)
	if helper.IsReservedParticipantName(trimmedName) {
		return nil, fmt.Errorf("%w: %q", ErrReservedName, trimmedName)
	}

	p.Name = helper.TitleCaseName(p.Name)
	p.ID = newParticipantID()
	p.PoolPosition = int64(len(players))
	if p.DisplayName == "" {
		p.DisplayName = helper.SanitizeName(p.Name)
	}

	players = append(players, p)

	if err := s.saveParticipantsNoLock(compID, players, withZekkenName); err != nil {
		return nil, err
	}

	return &p, nil
}

// ReplaceParticipant is the setup-gated rename/replace path used by the
// PUT /competitions/:id/participants/:pid handler. It applies transform under
// the per-comp lock with the same status re-check as AddParticipant, so a
// concurrent start can't sneak in between the handler's outer 409 check and
// the file write. UpdateParticipant remains gateless because check-in toggles
// must keep working after the competition is running.
func (s *Store) ReplaceParticipant(compID string, pid string, withZekkenName bool, transform func(p *domain.Player) error) (*domain.Player, error) {
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	if err := s.requireSetupLocked(compID); err != nil {
		return nil, err
	}

	return s.updateParticipantNoLock(compID, pid, withZekkenName, transform)
}

func (s *Store) saveParticipantsNoLock(compID string, players []domain.Player, withZekkenName bool) error {
	// Tier-1 (perfect-duplicate) guard at the lowest write layer so EVERY
	// persistence path — the bulk PUT /competitions/:id roster import (the
	// SPA's primary flow), single add/edit, and any future caller — rejects
	// duplicate (normalizedName, normalizedDojo) pairs uniformly. Enforcing
	// only in the handlers would leave the guard bypassable, the same reason
	// the elevated-password gate is inline on the roster PUT path.
	entries := make([][2]string, len(players))
	for i, p := range players {
		entries[i] = [2]string{p.Name, p.Dojo}
	}
	if dupes := helper.CheckDuplicateEntriesByNameDojo(entries); len(dupes) > 0 {
		return fmt.Errorf("%w: %s", ErrDuplicateName, strings.Join(dupes, "; "))
	}

	// Reserved-name guard: names arriving via the bulk SaveParticipants path
	// may be raw (neither TitleCased nor trimmed), so trim before matching to
	// catch inputs like " Pool A-1st " that would otherwise slip through.
	// AddParticipant and updateParticipantNoLock apply their own pre-TitleCase
	// checks before reaching here; for those paths the guard is defence-in-depth.
	for _, p := range players {
		if trimmed := strings.TrimSpace(p.Name); helper.IsReservedParticipantName(trimmed) {
			return fmt.Errorf("%w: %q", ErrReservedName, trimmed)
		}
	}

	path := s.compPath(compID, "participants.csv")

	data, err := marshalParticipantsCSV(players, withZekkenName)
	if err != nil {
		return err
	}

	if err := s.atomicWrite(path, data, 0600); err != nil {
		return err
	}

	// Invalidate every parse-mode variant of the participant cache so a
	// subsequent Load (regardless of HasIDs hint / WithSeeds) re-parses
	// from the freshly-written file. See participantsCacheKey for the
	// matrix mp-p7n round-6 split into.
	s.invalidateParticipantCaches(compID)

	return nil
}

// invalidateParticipantCaches drops every parse-mode variant of the
// participant cache for compID. Called after a participants.csv write
// (saveParticipantsNoLock) AND after a competition config write
// (saveCompetitionChangedLocked) — the latter is load-bearing because
// the load decision depends on Competition.HasParticipantIDs, so a flag
// flip must force a re-parse even when participants.csv is untouched.
//
// mp-p7n / Copilot PR #185 round-9: this replaces sole reliance on
// config.md's mtime in the cache key. On a filesystem with coarse
// timestamp resolution, a save + flag-flip in quick succession can
// land the same summed mtime and leave the auto-detect cache serving
// the shifted parse. Explicit invalidation on the config write is
// deterministic regardless of timestamp granularity. The config.md
// mtime stays in the cache key as cheap cross-process defense.
//
// Each variant uses its own cache.mu; this is safe to call while the
// per-comp lock is held (different lock) and acquires no other lock.
func (s *Store) invalidateParticipantCaches(compID string) {
	for _, key := range allParticipantsCacheKeys() {
		cache := s.getFileCache(compID, key)
		cache.mu.Lock()
		cache.data = nil
		cache.mtime = 0
		cache.mu.Unlock()
	}
}
