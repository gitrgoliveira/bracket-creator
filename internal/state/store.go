package state

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
)

var validIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

type Store struct {
	folder string
	mu     sync.RWMutex

	// walDir is "<folder>/.wal" — where WithTransaction parks its
	// intent log files (T210/T211/T212). A committed-but-not-applied
	// WAL here is replayed by init() on every NewStore call so a
	// crash between the WAL commit and the target writes can't
	// silently lose the transaction.
	walDir string

	// compMu maps competition ID -> *sync.RWMutex for fine-grained locking.
	compMu sync.Map

	// compRenameMu serializes "uniqueness-check + save" sequences across
	// all competitions. Required because per-comp locks alone can't fix
	// the cross-comp AB-BA race: two concurrent renames of different
	// competitions to the same new name each acquire their own comp's
	// write lock, then need to read OTHER comps to do the uniqueness
	// check — which would deadlock pairwise. This coarser mutex covers
	// only the check+save window, not all comp writes, so per-comp
	// operations (score saves, schedule edits, override-rank, etc.)
	// remain concurrent across competitions.
	compRenameMu sync.Mutex

	// cache for tournament configuration to avoid redundant disk I/O.
	tournamentMu sync.RWMutex
	cachedTourn  *Tournament
	tournMtime   int64

	// cache for competition-level files
	compCache sync.Map // map[string]*compCache

	announcementStore *AnnouncementStore
}

type compCache struct {
	files sync.Map // map[string]*fileCache
}

type fileCache struct {
	mu    sync.RWMutex
	data  any
	mtime int64
}

func NewStore(folder string) (*Store, error) {
	s := &Store{
		folder:            folder,
		walDir:            filepath.Join(folder, ".wal"),
		announcementStore: NewAnnouncementStore(),
	}

	if err := s.init(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) AnnouncementStore() *AnnouncementStore {
	return s.announcementStore
}

func (s *Store) init() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure folder exists
	if err := os.MkdirAll(s.folder, 0700); err != nil {
		return err
	}

	// Ensure competitions folder exists
	if err := os.MkdirAll(filepath.Join(s.folder, "competitions"), 0700); err != nil {
		return err
	}

	// Ensure WAL directory exists. WithTransaction stages its intent
	// log files under here; creating the dir up-front means the
	// first transaction doesn't have to worry about it.
	if err := os.MkdirAll(s.walDir, 0o750); err != nil {
		return err
	}

	// Replay any committed-but-not-finished transactions left over
	// from a previous process. The WAL package's Scan returns one
	// *wal.WAL per persisted intent file in id-sorted order
	// (uniqueWALID's prefix is unix-nanos, so this is commit-time
	// order). For each, run Apply (idempotent atomic-writes — safe
	// to re-run on a WAL that partially applied before the crash)
	// then Done.
	//
	// Errors are logged and skipped, not fatal: a corrupt WAL
	// shouldn't block startup if the rest are healthy. The
	// alternative (refuse to start) would brick a tournament over
	// a single bad intent file.
	pending, err := wal.Scan(s.walDir, s.directWriteWAL)
	if err != nil {
		// Scan only errors on broken ReadDir; treat as "no WAL to
		// replay" but log so the operator notices.
		slog.Warn("state: WAL scan failed at startup; skipping replay",
			"dir", s.walDir, "err", err)
		return nil
	}
	for _, w := range pending {
		slog.Info("state: replaying WAL transaction",
			"wal", w.ID(), "intents", len(w.Intents()))
		if err := w.Apply(); err != nil {
			slog.Error("state: WAL Apply failed during replay; keeping WAL for next start",
				"wal", w.ID(), "err", err)
			continue
		}
		if err := w.Done(); err != nil {
			slog.Warn("state: WAL Done failed after successful replay; will retry on next start",
				"wal", w.ID(), "err", err)
		}
	}

	return nil
}

func (s *Store) getCompLock(id string) *sync.RWMutex {
	actual, _ := s.compMu.LoadOrStore(id, &sync.RWMutex{})
	return actual.(*sync.RWMutex)
}

func (s *Store) GetFolder() string {
	return s.folder
}

// WithCompetitionRenameLock runs fn while holding the store's
// rename-coordination mutex (s.compRenameMu). Use it to wrap a
// competition uniqueness-check + save sequence so two concurrent
// renames of DIFFERENT competitions to the SAME new name can't both
// pass the check (each seeing the other still has its old name) and
// both land — leaving two competitions with the same effective Name.
//
// The mutex is finer-grained than s.mu (which covers all store state)
// and coarser than getCompLock(id) (which covers a single competition).
// It only serializes "name-uniqueness" operations against each other;
// per-competition score saves, schedule edits, override-rank, etc.
// remain fully concurrent across competitions.
//
// Lock ordering note: fn typically calls LoadCompetition on OTHER
// competitions (via checkUniqueCompName) and SaveCompetitionChanged on
// THIS competition. Both of those take per-comp locks internally —
// safe because s.compRenameMu is a different mutex from any per-comp
// lock, and the per-comp locks are acquired one at a time in fn.
// No AB-BA deadlock is possible because s.compRenameMu serializes the
// outer operation.
//
// IMPORTANT: s.compRenameMu is a sync.Mutex (non-recursive). fn MUST
// NOT recursively call WithCompetitionRenameLock — that would deadlock
// on the second acquire. Same advisory as the Update*Changed family:
// transforms / closures running under a store mutex should perform
// only the load + check + save work for the resource they're locking,
// not invoke other lock-acquiring Store methods for the SAME lock.
// Calls into methods that acquire OTHER locks (per-comp via
// SaveCompetitionChanged / LoadCompetition for different IDs) are
// fine — those locks are independent.
func (s *Store) WithCompetitionRenameLock(fn func() error) error {
	s.compRenameMu.Lock()
	defer s.compRenameMu.Unlock()
	return fn()
}

func (s *Store) getFileCache(compID, filename string) *fileCache {
	c, _ := s.compCache.LoadOrStore(compID, &compCache{})
	cc := c.(*compCache)
	f, _ := cc.files.LoadOrStore(filename, &fileCache{})
	return f.(*fileCache)
}

// loadCached returns the cached value for (compID, filename). On a cache hit
// (file mtime matches cached mtime) the cached pointer is returned directly;
// callers are responsible for deep-copying before exposing it. On a miss, the
// per-comp read lock and the file-cache write lock are held while parse runs.
// The mtime stored in the cache is the value read under the file-cache write
// lock immediately before parse, so a concurrent external writer that mutates
// the file after we've read it will produce a different FileMtime on the next
// call and force a re-parse (rather than returning stale content). The per-comp
// lock prevents in-process writers from racing with parse.
//
// parse receives the on-disk path and must return the value to cache (an empty
// container for "file does not exist", or nil when that's the intended sentinel
// — see LoadCompetition; nil values do not cache-hit and will be re-parsed on
// each call).
func (s *Store) loadCached(compID, filename string, parse func(path string) (any, error)) (any, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()

	cache := s.getFileCache(compID, filename)

	cache.mu.RLock()
	mtime := s.FileMtime(compID, filename)
	if cache.data != nil && cache.mtime == mtime {
		data := cache.data
		cache.mu.RUnlock()
		return data, nil
	}
	cache.mu.RUnlock()

	cache.mu.Lock()
	defer cache.mu.Unlock()
	mtime = s.FileMtime(compID, filename)
	if cache.data != nil && cache.mtime == mtime {
		return cache.data, nil
	}

	data, err := parse(s.compPath(compID, filename))
	if err != nil {
		return nil, err
	}
	cache.data = data
	cache.mtime = mtime
	return data, nil
}

// compPath builds and cleans the path to a file inside a competition directory.
func (s *Store) compPath(compID string, parts ...string) string {
	segments := append([]string{s.folder, "competitions", compID}, parts...)
	return filepath.Clean(filepath.Join(segments...))
}

// FileMtime returns the UnixNano mtime of a file inside a competition directory.
// Returns 0 if the file does not exist or stat fails.
func (s *Store) FileMtime(compID, filename string) int64 {
	info, err := os.Stat(s.compPath(compID, filename))
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

func ValidateCompetitionID(id string) error {
	if id == "" {
		return fmt.Errorf("competition ID cannot be empty")
	}
	if len(id) > 64 {
		return fmt.Errorf("competition ID too long (max 64 characters)")
	}
	if !validIDPattern.MatchString(id) {
		return fmt.Errorf("competition ID contains invalid characters (allowed: alphanumeric, hyphens, underscores)")
	}
	return nil
}
