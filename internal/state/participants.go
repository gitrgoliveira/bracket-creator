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

// ErrDuplicateName is returned by AddParticipant and UpdateParticipant when
// the supplied Player.Name collides with another participant in the same
// roster (excluding the participant being edited, for the update path). The
// comparison is case-insensitive (strings.EqualFold) to match the on-disk
// canonicalization applied by helper.CreatePlayers.
var ErrDuplicateName = errors.New("participant name already exists")

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

// LoadParticipantsOpts controls optional behavior in LoadParticipants.
type LoadParticipantsOpts struct {
	WithSeeds bool  // set false to skip the seeds.csv read (hot list paths)
	HasIDs    *bool // nil = auto-detect from first line; non-nil uses cached Competition.HasParticipantIDs
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
	// Use a virtual filename for cache to distinguish between with/without seeds.
	cacheKey := "participants.csv"
	if opts.WithSeeds {
		cacheKey = "participants_with_seeds.csv"
	}

	cache := s.getFileCache(compID, cacheKey)
	cache.mu.RLock()
	mtime := s.FileMtime(compID, "participants.csv")
	if opts.WithSeeds {
		mtime += s.FileMtime(compID, "seeds.csv")
	}

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

	// Detect new format: first field is a UUID. Use cached flag if provided.
	var hasIDs bool
	if opts.HasIDs != nil {
		hasIDs = *opts.HasIDs
	} else {
		hasIDs = len(records) > 0 && len(records[0]) > 0 && uuidRE(strings.TrimSpace(records[0][0]))
	}

	var ids []string
	var checkedInFlags []bool
	var playerRecords [][]string

	for _, record := range records {
		isCheckedIn := false

		// Determine data fields (everything after UUID if present).
		// Per-record UUID check: only strip the first field as an ID
		// when it actually matches the UUID pattern.
		dataStart := 0
		if hasIDs && len(record) > 0 && uuidRE(strings.TrimSpace(record[0])) {
			dataStart = 1
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

	foundIdx := -1
	for i := range players {
		if players[i].ID == pid {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		return nil, ErrParticipantNotFound
	}

	oldName := players[foundIdx].Name

	if err := transform(&players[foundIdx]); err != nil {
		return nil, err
	}
	// Canonicalize to match what CreatePlayers produces on load (Title-case),
	// so participants.csv and seeds.csv store the same form that will be read
	// back — otherwise a rename to "alice cooper" would be read as "Alice Cooper"
	// while seeds.csv still holds "alice cooper", breaking seed merging.
	players[foundIdx].Name = helper.TitleCaseName(players[foundIdx].Name)

	// Duplicate-name guard: when the transform renames the participant,
	// reject if any OTHER participant already has that name. Trim both
	// sides — LoadParticipants canonicalises via helper.CreatePlayers
	// (TrimSpace + cases.Title), so "Alice " collapses to "Alice" on
	// the next load and would reintroduce ambiguous name-keyed lookups.
	if players[foundIdx].Name != oldName {
		newTrimmed := strings.TrimSpace(players[foundIdx].Name)
		for i := range players {
			if i == foundIdx {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(players[i].Name), newTrimmed) {
				return nil, ErrDuplicateName
			}
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

// marshalParticipantsCSV serialises players into RFC 4180 CSV bytes.
// Shared by saveParticipantsNoLock and saveParticipantsLocked so the
// display-name and checked_in logic is defined once.
//
// withZekkenName MUST match the value the next LoadParticipants will use.
// The on-disk column layout differs between the two modes:
//   - non-zekken: [id, Name, Dojo, ...Metadata, Tag?, "checked_in"?]
//   - zekken:    [id, Name, DisplayName, Dojo, ...Metadata, Tag?, "checked_in"?]
//
// Pre-fix the function tried to be clever by writing the 2-column form (id,
// Name, Dojo) when DisplayName was empty or auto-derivable. That broke zekken
// reloads as soon as Tag or Metadata were present — e.g. the manual-tag
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
			// Tag/Metadata are present.
			dn := p.DisplayName
			if dn == "" {
				dn = helper.SanitizeName(p.Name)
			}
			record = []string{id, p.Name, dn, p.Dojo}
		} else {
			record = []string{id, p.Name, p.Dojo}
		}
		record = append(record, p.Metadata...)
		if p.Tag != "" {
			record = append(record, p.Tag)
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

	// Duplicate-name guard (per bead acceptance criteria): the admin
	// UI accepts the same name twice without warning otherwise, and
	// the rest of the roster identifies competitors by display name.
	// Trim both sides: LoadParticipants canonicalises via SanitizeName
	// (TrimSpace + Title), so a trailing-space variant like "Alice "
	// collapses to "Alice" on the next load — reject it up front.
	for _, existing := range players {
		if strings.EqualFold(strings.TrimSpace(existing.Name), strings.TrimSpace(p.Name)) {
			return nil, ErrDuplicateName
		}
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
	path := s.compPath(compID, "participants.csv")

	data, err := marshalParticipantsCSV(players, withZekkenName)
	if err != nil {
		return err
	}

	if err := s.atomicWrite(path, data, 0600); err != nil {
		return err
	}

	// Invalidate participant caches (with and without seeds) so the next Load
	// sees the fresh data without a disk re-read.
	for _, key := range []string{"participants.csv", "participants_with_seeds.csv"} {
		cache := s.getFileCache(compID, key)
		cache.mu.Lock()
		cache.data = nil
		cache.mtime = 0
		cache.mu.Unlock()
	}

	return nil
}
