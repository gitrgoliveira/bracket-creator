package state

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"
)

var validIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

type Store struct {
	folder string
	mu     sync.RWMutex

	// compMu maps competition ID -> *sync.RWMutex for fine-grained locking.
	compMu sync.Map

	// cache for tournament configuration to avoid redundant disk I/O.
	tournamentMu sync.RWMutex
	cachedTourn  *Tournament
	tournMtime   int64

	// cache for competition-level files
	compCache sync.Map // map[string]*compCache
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
		folder: folder,
	}

	if err := s.init(); err != nil {
		return nil, err
	}

	return s, nil
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

	return nil
}

func (s *Store) getCompLock(id string) *sync.RWMutex {
	actual, _ := s.compMu.LoadOrStore(id, &sync.RWMutex{})
	return actual.(*sync.RWMutex)
}

func (s *Store) GetFolder() string {
	return s.folder
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
