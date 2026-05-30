package state

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func (s *Store) LoadTournament() (*Tournament, error) {
	s.tournamentMu.RLock()
	path := filepath.Clean(filepath.Join(s.folder, "tournament.md"))
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.tournamentMu.RUnlock()
			s.tournamentMu.Lock()
			defer s.tournamentMu.Unlock()
			s.cachedTourn = nil
			s.tournMtime = 0
			return nil, nil
		}
		s.tournamentMu.RUnlock()
		return nil, err
	}

	mtime := info.ModTime().UnixNano()
	if s.cachedTourn != nil && s.tournMtime == mtime {
		t := s.copyTournament(s.cachedTourn)
		s.tournamentMu.RUnlock()
		return t, nil
	}
	s.tournamentMu.RUnlock()

	s.tournamentMu.Lock()
	defer s.tournamentMu.Unlock()

	// Re-check after acquiring write lock
	info, err = os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cachedTourn = nil
			s.tournMtime = 0
			return nil, nil
		}
		return nil, err
	}
	mtime = info.ModTime().UnixNano()
	if s.cachedTourn != nil && s.tournMtime == mtime {
		return s.copyTournament(s.cachedTourn), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var t Tournament
	if err := parseFrontMatter(data, &t); err != nil {
		// If it's not a front-matter file, return a default tournament
		t = Tournament{
			Name:         "New Tournament",
			Date:         time.Now().Format("02-01-2006"),
			Venue:        "Venue TBA",
			DurationDays: 1,
		}
	} else if t.DurationDays == 0 {
		// Migrate existing single-day tournament.md files that predate the
		// DurationDays field: omitempty writes nothing for 1, so on load the
		// zero value must be treated as 1.
		t.DurationDays = 1
	}

	s.cachedTourn = &t
	s.tournMtime = mtime

	return s.copyTournament(s.cachedTourn), nil
}

func (s *Store) copyTournament(t *Tournament) *Tournament {
	if t == nil {
		return nil
	}
	cp := *t
	if t.Courts != nil {
		cp.Courts = make([]string, len(t.Courts))
		copy(cp.Courts, t.Courts)
	}
	return &cp
}

// SaveTournamentChanged persists t and reports whether the on-disk content
// actually changed. Use this instead of SaveTournament when you need to gate
// a broadcast on a real mutation.
func (s *Store) SaveTournamentChanged(t *Tournament) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tournamentMu.Lock()
	defer s.tournamentMu.Unlock()

	path := filepath.Clean(filepath.Join(s.folder, "tournament.md"))
	newData, err := writeFrontMatter(t)
	if err != nil {
		return false, err
	}

	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, newData) { // #nosec G304
		return false, nil
	}

	if err := s.atomicWrite(path, newData, 0600); err != nil {
		return false, err
	}

	// Update cache
	s.cachedTourn = t
	info, _ := os.Stat(path)
	if info != nil {
		s.tournMtime = info.ModTime().UnixNano()
	}

	return true, nil
}

func (s *Store) SaveTournament(t *Tournament) error {
	_, err := s.SaveTournamentChanged(t)
	return err
}

// UpdateTournamentChanged atomically loads the current tournament under
// the store's write lock, calls transform(current, desired) which may
// modify desired in place (e.g. to preserve fields from current), and
// — if transform returns nil — persists desired. Returns (changed, err)
// like SaveTournamentChanged.
//
// This is the race-free primitive for "load the existing record, copy
// some fields forward, save the result." Without it, a handler that
// calls LoadTournament + SaveTournamentChanged sequentially has a
// TOCTOU window between the two calls during which a concurrent
// writer can land changes that the load-modify-save then clobbers.
//
// Specifically motivated by the PUT /api/tournament password-preserve
// semantics: when the incoming body sends Password == "", the handler
// must copy the stored Password into the desired record. Two
// concurrent PUTs — one with empty Password (intent: keep), one with
// a new password (intent: change) — could race in the old code so
// that the empty-Password PUT's late save clobbers the
// change-Password PUT's earlier save. With this method, the load +
// transform + save sequence is serialized under the store's lock.
//
// `current` is nil ONLY when the tournament.md file does not exist
// yet (first-ever save). When the file exists but parses cleanly,
// `current` holds the parsed record. When the file exists but the
// front matter is corrupt, the load below falls back to a default
// Tournament record (matching LoadTournament's behavior) and that
// default is passed to `transform` — `current` is still non-nil in
// that case. transform must handle both cases. If transform returns
// a non-nil error, no write happens and the error is returned to
// the caller as-is (callers can use errors.Is to discriminate
// validation vs. I/O).
//
// IMPORTANT: transform runs while this method holds both s.mu and
// s.tournamentMu (non-recursive locks). It MUST NOT call back into
// any Store method that acquires either lock — that includes
// LoadTournament, SaveTournament, SaveTournamentChanged, and a
// recursive UpdateTournamentChanged. Deadlock would result. The
// transform should only mutate `desired` (possibly by copying fields
// from `current`) and return; for any cross-resource coordination
// (e.g. updating a competition during a tournament update), perform
// the load BEFORE calling this method and pass the resulting data
// in via a closure over local variables.
func (s *Store) UpdateTournamentChanged(desired *Tournament, transform func(current *Tournament, desired *Tournament) error) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tournamentMu.Lock()
	defer s.tournamentMu.Unlock()

	path := filepath.Clean(filepath.Join(s.folder, "tournament.md"))

	// Load current under the lock. Mirror LoadTournament's parse
	// fallback so the transform sees the same "default record" view
	// that the rest of the system gets — except return nil when the
	// file doesn't exist (so callers can distinguish "first save" from
	// "subsequent save"; LoadTournament also returns nil in that case).
	var current *Tournament
	if data, rerr := os.ReadFile(path); rerr == nil { // #nosec G304
		var t Tournament
		if perr := parseFrontMatter(data, &t); perr == nil {
			if t.DurationDays == 0 {
				// Migrate: omitempty means an existing file with no
				// duration_days field deserialises to 0 — treat as 1.
				t.DurationDays = 1
			}
			current = &t
		} else {
			// Parse failure: fall back to the same default record
			// LoadTournament constructs, so transform's view is
			// consistent.
			current = &Tournament{
				Name:         "New Tournament",
				Date:         time.Now().Format("02-01-2006"),
				Venue:        "Venue TBA",
				DurationDays: 1,
			}
		}
	} else if !os.IsNotExist(rerr) {
		return false, rerr
	}

	if err := transform(current, desired); err != nil {
		return false, err
	}

	newData, err := writeFrontMatter(desired)
	if err != nil {
		return false, err
	}

	if existing, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(existing, newData) { // #nosec G304
		return false, nil
	}

	if err := s.atomicWrite(path, newData, 0600); err != nil {
		return false, err
	}

	s.cachedTourn = desired
	if info, serr := os.Stat(path); serr == nil && info != nil {
		s.tournMtime = info.ModTime().UnixNano()
	}

	return true, nil
}

func parseFrontMatter(data []byte, v interface{}) error {
	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return fmt.Errorf("missing front matter delimiter")
	}

	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return fmt.Errorf("invalid front matter format")
	}

	return yaml.Unmarshal([]byte(parts[1]), v)
}

func writeFrontMatter(v interface{}) ([]byte, error) {
	y, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf("---\n%s---\n", string(y))), nil
}
