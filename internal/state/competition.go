package state

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

func (s *Store) ListCompetitions() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(filepath.Join(s.folder, "competitions"))
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	return ids, nil
}

func (s *Store) LoadCompetition(id string) (*Competition, error) {
	if err := ValidateCompetitionID(id); err != nil {
		return nil, fmt.Errorf("invalid competition ID: %w", err)
	}

	data, err := s.loadCached(id, "config.md", parseCompetitionFile)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return nil, nil
	}
	return s.copyCompetition(data.(*Competition)), nil
}

func parseCompetitionFile(path string) (any, error) {
	raw, err := os.ReadFile(path) // #nosec G304 — path built by compPath which calls filepath.Clean
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var c Competition
	if err := parseFrontMatter(raw, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) copyCompetition(c *Competition) *Competition {
	if c == nil {
		return nil
	}
	cp := *c
	if c.Courts != nil {
		cp.Courts = make([]string, len(c.Courts))
		copy(cp.Courts, c.Courts)
	}
	if c.Players != nil {
		cp.Players = make([]helper.Player, len(c.Players))
		copy(cp.Players, c.Players)
	}
	return &cp
}

// SaveCompetitionChanged persists c and reports whether the on-disk content
// actually changed. Use this instead of SaveCompetition when you need to gate
// a broadcast on a real mutation.
func (s *Store) SaveCompetitionChanged(c *Competition) (bool, error) {
	if err := ValidateCompetitionID(c.ID); err != nil {
		return false, fmt.Errorf("invalid competition ID: %w", err)
	}

	mu := s.getCompLock(c.ID)
	mu.Lock()
	defer mu.Unlock()

	return s.saveCompetitionChangedLocked(c)
}

func (s *Store) SaveCompetition(c *Competition) error {
	_, err := s.SaveCompetitionChanged(c)
	return err
}

// saveCompetitionChangedLocked persists c to disk and refreshes the
// cache. Caller MUST hold the per-competition lock
// (s.getCompLock(c.ID)). Used by both SaveCompetitionChanged (which
// takes the lock) and UpdateCompetitionChanged (which holds the lock
// across load + transform + save).
func (s *Store) saveCompetitionChangedLocked(c *Competition) (bool, error) {
	if err := os.MkdirAll(s.compPath(c.ID), 0700); err != nil {
		return false, err
	}

	path := s.compPath(c.ID, "config.md")
	newData, err := writeFrontMatter(c)
	if err != nil {
		return false, err
	}

	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, newData) { // #nosec G304
		return false, nil
	}

	if err := os.WriteFile(path, newData, 0600); err != nil {
		return false, err
	}

	cache := s.getFileCache(c.ID, "config.md")
	cache.mu.Lock()
	cache.data = s.copyCompetition(c)
	cache.mtime = s.FileMtime(c.ID, "config.md")
	cache.mu.Unlock()

	return true, nil
}

// UpdateCompetitionChanged atomically loads the competition with id,
// calls transform with the loaded record (which may be nil if no file
// exists yet), and — if transform returns a non-nil *Competition —
// persists it. Returns (changed, err) where changed reports whether
// the on-disk content actually differs from the prior version (same
// semantics as SaveCompetitionChanged). The entire load + transform
// + save sequence runs under the per-competition lock so concurrent
// calls serialize correctly without losing each other's mutations.
//
// transform's return value selects what to save:
//   - return current (after mutating in place), nil → save the mutated
//     record
//   - return a NEW *Competition, nil → save that (use this for the
//     PUT-create case where current is nil and the body becomes the
//     new on-disk state)
//   - return nil, nil → skip the save (no-op, used when the
//     precondition the caller wanted to commit on is no longer true)
//   - return _, err → propagate the error unchanged; no save
//
// Pre-atomic-primitive, handlers and engine code called LoadCompetition
// + SaveCompetitionChanged sequentially with no shared lock between
// the two calls. A concurrent writer could change the on-disk state
// between Load and Save; the late save would clobber that change with
// stale data. Specific failure modes this fix closes:
//   - POST /invalidate vs. concurrent score-save's MaybeAutoCompletePools:
//     admin's "invalid" status overwritten back to "complete" (or
//     vice versa).
//   - PUT /competitions/:id uniqueness-check race: two new competitions
//     with the same name both pass the check and both land.
//   - StartCompetition vs. concurrent edit of the same competition.
//
// IMPORTANT: transform runs while this method holds the per-competition
// lock. It MUST NOT call any other Store method that acquires the same
// lock (SaveCompetition, SaveCompetitionChanged, SaveParticipants,
// SavePools, SaveBracket, SavePoolMatches, recursive
// UpdateCompetitionChanged / UpdateBracket / UpdatePoolMatchByID, etc.) —
// `sync.Mutex` is non-recursive and would deadlock. For cross-file
// operations (e.g. participants save), perform them AFTER
// UpdateCompetitionChanged returns.
func (s *Store) UpdateCompetitionChanged(id string, transform func(current *Competition) (*Competition, error)) (bool, error) {
	if err := ValidateCompetitionID(id); err != nil {
		return false, fmt.Errorf("invalid competition ID: %w", err)
	}

	mu := s.getCompLock(id)
	mu.Lock()
	defer mu.Unlock()

	// Load directly under the lock (bypass the cached path — the
	// per-comp lock is what coordinates with the save below).
	path := s.compPath(id, "config.md")
	parsed, err := parseCompetitionFile(path)
	if err != nil {
		return false, err
	}
	current, _ := parsed.(*Competition)

	desired, err := transform(current)
	if err != nil {
		return false, err
	}
	if desired == nil {
		// Transform signaled no-op (either because there's nothing to
		// save or because the precondition fell through). Skip save.
		return false, nil
	}
	// Defensive: ensure ID on the persisted record matches the path
	// we're locking on. Caller may have constructed a new record
	// without setting ID.
	desired.ID = id
	return s.saveCompetitionChangedLocked(desired)
}

func (s *Store) DeleteCompetition(id string) error {
	if err := ValidateCompetitionID(id); err != nil {
		return fmt.Errorf("invalid competition ID: %w", err)
	}

	mu := s.getCompLock(id)
	mu.Lock()
	defer mu.Unlock()

	if err := os.RemoveAll(s.compPath(id)); err != nil {
		return err
	}
	s.compCache.Delete(id)
	s.compMu.Delete(id)
	return nil
}
