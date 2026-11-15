package state

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
	"gopkg.in/yaml.v3"
)

// SnapshotScope names the portion of the data folder a snapshot captures.
// Defined as a string alias so callers can pass either the exported constants
// or the literal scope strings ("tournament", "competition").
type SnapshotScope = string

const (
	ScopeTournament  SnapshotScope = "tournament"
	ScopeCompetition SnapshotScope = "competition"
)

// ErrCompetitionNotFound is returned when a competition-scoped operation names
// a competition folder that does not exist on disk.
var ErrCompetitionNotFound = errors.New("competition not found")

// ErrPendingWALUnsettled is returned when a committed transaction log for a
// competition cannot be applied before a snapshot or restore.
var ErrPendingWALUnsettled = errors.New("pending transaction log could not be settled")

// PendingWALError reports the competition whose committed transaction log
// could not be settled, wrapping ErrPendingWALUnsettled and the underlying
// failure.
type PendingWALError struct {
	CompetitionID string
	// Log is the id of the log that could not land (its file in .wal is
	// <Log>.json), so the operator can find it; empty when none is known.
	Log string
	Err error
}

func (e *PendingWALError) Error() string {
	if e.Log != "" {
		return fmt.Sprintf("%s for competition %q (transaction log %s.json): %v", ErrPendingWALUnsettled.Error(), e.CompetitionID, e.Log, e.Err)
	}
	return fmt.Sprintf("%s for competition %q: %v", ErrPendingWALUnsettled.Error(), e.CompetitionID, e.Err)
}

func (e *PendingWALError) Unwrap() []error {
	return []error{ErrPendingWALUnsettled, e.Err}
}

// Snapshot holds an in-memory copy of the files read from the data folder,
// keyed by forward-slash path relative to the data folder, along with each
// captured competition's display name.
type Snapshot struct {
	Scope          SnapshotScope
	TournamentName string
	Files          map[string][]byte
	Competitions   map[string]string
}

// CheckFrontMatter reports whether b parses as store front matter (the YAML
// header format shared by tournament.md and config.md).
func CheckFrontMatter(b []byte) error {
	var fields map[string]any
	if err := parseFrontMatter(b, &fields); err != nil {
		return err
	}
	if len(fields) == 0 {
		return errors.New("empty front matter")
	}
	return nil
}

// Snapshot reads an in-memory copy of either the whole tournament
// (ScopeTournament) or a single competition (ScopeCompetition). Tournament-level
// files are read together under their locks, and each competition is read under
// its own lock one at a time, so a ScopeTournament archive is not a single
// point-in-time snapshot across competitions and a competition created during
// the pass may be missed.
//
// For each competition, one at a time, it acquires that competition's read
// lock (re-checking getCompLock in case DeleteCompetition removed the mutex
// while waiting) and then s.mu's read lock. If a committed WAL naming that
// competition is present, it upgrades to the competition's write lock and s.mu's
// write lock, settles the WAL via settleCompWALsLocked, and reads the
// competition's files under those same locks without releasing in between.
//
// Tournament-level files (tournament.md, branding/, sponsors/) are read under
// s.mu.RLock then s.tournamentMu.RLock. The password field is removed from the
// captured tournament.md so no archive ever carries it.
func (s *Store) Snapshot(scope SnapshotScope, compID string) (Snapshot, error) {
	switch scope {
	case ScopeCompetition:
		if err := ValidateCompetitionID(compID); err != nil {
			return Snapshot{}, fmt.Errorf("invalid competition ID: %w", err)
		}
		return s.snapshotOneCompetition(compID)
	case ScopeTournament:
		s.mu.RLock()
		s.tournamentMu.RLock()
		snap, err := s.snapshotLocked(ScopeTournament, nil, false)
		var ids []string
		if err == nil {
			ids, err = s.listCompetitionsLocked()
		}
		s.tournamentMu.RUnlock()
		s.mu.RUnlock()
		if err != nil {
			return Snapshot{}, err
		}

		for _, id := range ids {
			if ValidateCompetitionID(id) != nil {
				continue
			}
			compSnap, cerr := s.snapshotOneCompetition(id)
			if cerr != nil {
				if errors.Is(cerr, ErrCompetitionNotFound) {
					continue
				}
				return Snapshot{}, cerr
			}
			for k, v := range compSnap.Files {
				snap.Files[k] = v
			}
			for k, v := range compSnap.Competitions {
				snap.Competitions[k] = v
			}
		}
		return snap, nil
	default:
		return Snapshot{}, fmt.Errorf("invalid snapshot scope %q", scope)
	}
}

// snapshotOneCompetition locks compID (and s.mu), settles any committed WAL
// for compID, and reads its files via snapshotLocked before releasing the
// locks.
func (s *Store) snapshotOneCompetition(compID string) (Snapshot, error) {
	mu := s.rlockComp(compID)
	s.mu.RLock()

	hasPending, err := s.hasPendingCompWALLocked(compID)
	if err != nil {
		s.mu.RUnlock()
		mu.RUnlock()
		return Snapshot{}, err
	}
	if !hasPending {
		snap, serr := s.snapshotLocked(ScopeCompetition, []string{compID}, false)
		s.mu.RUnlock()
		mu.RUnlock()
		return snap, serr
	}

	s.mu.RUnlock()
	mu.RUnlock()

	wmu := s.lockComp(compID)
	defer wmu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.settleCompWALsLocked(compID); err != nil {
		return Snapshot{}, err
	}
	return s.snapshotLocked(ScopeCompetition, []string{compID}, false)
}

// rlockComp acquires the read lock for id and verifies that getCompLock(id)
// still returns the same mutex (DeleteCompetition deletes the entry from
// compMu before unlocking, leaving waiters on an orphaned mutex).
func (s *Store) rlockComp(id string) *sync.RWMutex {
	for {
		mu := s.getCompLock(id)
		mu.RLock()
		if s.getCompLock(id) == mu {
			return mu
		}
		mu.RUnlock()
	}
}

// lockComp acquires the write lock for id and verifies that getCompLock(id)
// still returns the same mutex.
func (s *Store) lockComp(id string) *sync.RWMutex {
	for {
		mu := s.getCompLock(id)
		mu.Lock()
		if s.getCompLock(id) == mu {
			return mu
		}
		mu.Unlock()
	}
}

// listCompetitionsLocked lists the directory names under competitions/.
// Caller must hold at least s.mu.RLock.
func (s *Store) listCompetitionsLocked() ([]string, error) {
	entries, err := s.root.readDir(filepath.Join(s.folder, "competitions"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
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

// hasPendingCompWALLocked reports whether any committed WAL in s.walDir has an
// intent targeting compID's directory. Caller must hold at least the
// competition read lock and s.mu.RLock.
func (s *Store) hasPendingCompWALLocked(compID string) (bool, error) {
	pending, err := s.pendingCompWALsLocked(compID)
	if err != nil {
		return false, err
	}
	return len(pending) > 0, nil
}

// pendingCompWALsLocked scans s.walDir and returns the committed WALs that
// have at least one intent targeting compID's directory, leaving out any
// another writer overtook.
func (s *Store) pendingCompWALsLocked(compID string) ([]*wal.WAL, error) {
	all, err := wal.Scan(s.walDir, s.directWriteWAL, s.directRemoveWAL, s.directReadWAL)
	if err != nil {
		return nil, err
	}
	compDir := s.compPath(compID)
	var matched []*wal.WAL
	for _, w := range all {
		// An overtaken log is never replayed (wal.WAL.Stale): settling it
		// would put older content back, and the next start sets it aside.
		// A damaged log names no step (wal.WAL.Invalid): only startup sets
		// it aside.
		if w.Stale() != "" || w.Invalid() || !walTouchesComp(w, compDir) {
			continue
		}
		// A log of this competition Scan could not check refuses the
		// settle; one of another competition does not concern it (startup
		// still refuses on it).
		if uerr := w.Unchecked(); uerr != nil {
			return nil, uerr
		}
		matched = append(matched, w)
	}
	return matched, nil
}

func walTouchesComp(w *wal.WAL, compDir string) bool {
	cleanDir := filepath.Clean(compDir)
	prefix := cleanDir + string(filepath.Separator)
	for _, in := range w.Intents() {
		cleanPath := storePath(in.Path)
		if cleanPath == cleanDir || strings.HasPrefix(cleanPath, prefix) {
			return true
		}
	}
	return false
}

// settleCompWALsLocked scans s.walDir for committed WALs targeting compID and
// settles each with w.Apply() followed by w.Done(). Caller MUST hold compID's
// write lock and s.mu.Lock. If Apply or Done fails, it returns a
// *PendingWALError wrapping ErrPendingWALUnsettled and naming compID.
func (s *Store) settleCompWALsLocked(compID string) error {
	pending, err := s.pendingCompWALsLocked(compID)
	if err != nil {
		return err
	}
	for _, w := range pending {
		if err := w.Apply(); err != nil {
			return &PendingWALError{CompetitionID: compID, Log: w.ID(), Err: err}
		}
		s.invalidateCachesForWALIntents(compID, w.Intents())
		if err := w.Done(); err != nil {
			return &PendingWALError{CompetitionID: compID, Log: w.ID(), Err: err}
		}
	}
	// Every log for compID has landed, whichever door settled it (a write,
	// a read, a snapshot or a restore), so the write path stops asking.
	s.keptWAL.Delete(compID)
	return nil
}

// snapshotLocked reads the requested scope and competition IDs into a Snapshot
// without acquiring any lock. Caller MUST hold the required locks:
//   - for ScopeTournament: s.mu and s.tournamentMu (at least read locks)
//   - for each id in ids: that competition's lock and s.mu (at least read locks)
//
// When tolerateCorruptConfig is true, a competition whose config.md fails to
// parse as front matter is retained in snap.Competitions under its ID with its
// raw files captured, rather than aborting the snapshot.
func (s *Store) snapshotLocked(scope SnapshotScope, ids []string, tolerateCorruptConfig bool) (Snapshot, error) {
	if scope != ScopeTournament && scope != ScopeCompetition {
		return Snapshot{}, fmt.Errorf("invalid snapshot scope %q", scope)
	}
	snap := Snapshot{
		Scope:        scope,
		Files:        make(map[string][]byte),
		Competitions: make(map[string]string),
	}
	if scope == ScopeTournament {
		if err := s.snapshotTournamentFilesLocked(&snap); err != nil {
			return Snapshot{}, err
		}
	}
	for _, id := range ids {
		exists, err := s.snapshotCompetitionLocked(id, &snap, tolerateCorruptConfig)
		if err != nil {
			return Snapshot{}, err
		}
		if !exists && scope == ScopeCompetition {
			return Snapshot{}, fmt.Errorf("%w: %q (%w)", ErrCompetitionNotFound, id, fs.ErrNotExist)
		}
	}
	return snap, nil
}

func (s *Store) snapshotTournamentFilesLocked(snap *Snapshot) error {
	tournPath := filepath.Clean(filepath.Join(s.folder, "tournament.md"))
	raw, err := s.root.readFile(tournPath)
	switch {
	case err == nil:
		var t Tournament
		if perr := parseFrontMatter(raw, &t); perr != nil {
			return perr
		}
		snap.TournamentName = t.Name
		stripped, werr := serializeTournamentWithoutPassword(&t)
		if werr != nil {
			return werr
		}
		snap.Files["tournament.md"] = stripped
	case errors.Is(err, fs.ErrNotExist):
		// When tournament.md has never been written, archive a placeholder
		// tournament without a date so the captured bytes and archive
		// fingerprint do not change across days while the file is absent.
		t := Tournament{
			Name:  "New Tournament",
			Venue: "Venue TBA",
		}
		ApplyTournamentDefaults(&t)
		snap.TournamentName = t.Name
		stripped, werr := serializeTournamentWithoutPassword(&t)
		if werr != nil {
			return werr
		}
		snap.Files["tournament.md"] = stripped
	default:
		return err
	}

	for _, dirName := range []string{BrandingDirName, SponsorsDirName} {
		dirPath := filepath.Join(s.folder, dirName)
		info, serr := s.root.stat(dirPath)
		if serr != nil {
			if errors.Is(serr, fs.ErrNotExist) {
				continue
			}
			return serr
		}
		if !info.IsDir() {
			continue
		}
		if err := s.collectTreeFilesLocked(dirPath, snap.Files); err != nil {
			return err
		}
	}
	return nil
}

// serializeTournamentWithoutPassword serialises t as front matter with its
// Password cleared and the top-level password key removed.
func serializeTournamentWithoutPassword(t *Tournament) ([]byte, error) {
	cp := *t
	cp.Password = ""
	var n yaml.Node
	if err := n.Encode(&cp); err != nil {
		return nil, err
	}
	top := &n
	if top.Kind == yaml.DocumentNode && len(top.Content) > 0 {
		top = top.Content[0]
	}
	if top.Kind == yaml.MappingNode {
		filtered := make([]*yaml.Node, 0, len(top.Content))
		for i := 0; i+1 < len(top.Content); i += 2 {
			if top.Content[i].Value == "password" {
				continue
			}
			filtered = append(filtered, top.Content[i], top.Content[i+1])
		}
		top.Content = filtered
	}
	return writeFrontMatter(&n)
}

func (s *Store) snapshotCompetitionLocked(id string, snap *Snapshot, tolerateCorruptConfig bool) (bool, error) {
	compDir := s.compPath(id)
	info, err := s.root.stat(compDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, nil
	}
	cfgInfo, err := s.root.stat(s.compPath(id, "config.md"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if !cfgInfo.Mode().IsRegular() {
		return false, nil
	}
	if err := s.collectTreeFilesLocked(compDir, snap.Files); err != nil {
		return false, err
	}
	compName := id
	cfgKey := filepath.ToSlash(filepath.Join("competitions", id, "config.md"))
	if cfgBytes, ok := snap.Files[cfgKey]; ok {
		var c Competition
		if err := parseFrontMatter(cfgBytes, &c); err != nil {
			if !tolerateCorruptConfig {
				return false, fmt.Errorf("competition %q: config.md is not valid front matter: %w", id, err)
			}
		} else if c.Name != "" {
			compName = c.Name
		}
	}
	snap.Competitions[id] = compName
	return true, nil
}

func (s *Store) collectTreeFilesLocked(rootDir string, dst map[string][]byte) error {
	cleanRoot := filepath.Clean(rootDir)
	return s.root.walkDir(cleanRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		rel, err := filepath.Rel(s.folder, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if path != cleanRoot && backup.IsExcludedPath(rel) {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if backup.IsExcludedPath(rel) || isSkippedTempName(name) {
			return nil
		}
		data, err := s.root.readFile(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// The file may have vanished concurrently (e.g. uncoordinated branding/sponsors upload or delete).
				return nil
			}
			return err
		}
		dst[rel] = data
		return nil
	})
}

func isSkippedTempName(name string) bool {
	return backup.IsTempFileName(name)
}
