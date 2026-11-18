package state

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// compRoots holds one os.Root per competition folder, opened from the data
// root on first use (Root.OpenRoot, so the OS still refuses a folder that links
// out of the data folder). dataRoot runs a call on a path strictly under
// competitions/<id>/ on that folder's root, which saves resolving
// competitions/ and <id> again on every call; this type owns the entries and
// the lock protocol that keeps them honest, and knows nothing of paths or
// operations. The saving that pays for the protocol is measured in
// .claude/rules/store.md.
//
// A held root is a descriptor on the folder, not its name: it follows the
// folder if the folder is renamed, and keeps an unlinked folder alive if it is
// removed. So every rename, removal or creation of a competition folder itself
// drops that folder's root, and holds its entry write-locked for the length of
// the syscall, so nothing reopens it mid-move (move). dataRoot makes that drop
// in rename, remove, removeAll, mkdir and mkdirAll, whatever called them:
// DeleteCompetition's removeAll, the restore journal's moves out and in
// (runRestoreJournal, which the live restore and startup recovery both run),
// and saveCompetitionChangedLocked's mkdirAll when it makes a folder removed by
// hand (one already there keeps its root). The removal of a restore token
// folder needs no drop: it lies under .restore/, which no held root points
// into, since a folder moved out is dropped as it moves. Two removeAll calls
// remove a folder INSIDE a competition folder (the score logs' events/ in
// score_log.go, the match history's history/ in match_history.go); the
// competition's root serves them and nothing is dropped. No store path renames
// or removes competitions/ or the data folder itself; one that did would have
// to drop every held root.
//
// A folder removed from OUTSIDE the app while the server runs answers ENOENT
// to a call on its held root; dataRoot then checks that the folder named
// competitions/<id> is still the one held (heldFolderGone), and if it is not,
// drops the root and runs the call once more on whatever is there now, so a
// folder put back by hand (a copy from a backup) is written again without a
// restart, and one still gone fails as before. A folder RENAMED from outside
// the app while the server runs is followed by its held root, which gets no
// ENOENT: the server goes on reading and writing the renamed folder until the
// root is next dropped or the server restarts (accepted; no supported workflow
// does it, since the CLI restore runs with the server stopped).
type compRoots struct {
	// data is the data folder's root, which the folder roots are opened from
	// and the held folders are compared against.
	data *os.Root
	// entries maps a competition id to its *compRoot. An entry is in the map
	// only while its root is open, or while the one goroutine that holds its
	// write lock opens or moves it; that goroutine stores a root or removes
	// the entry before it unlocks. So a reader that takes an entry's read
	// lock and finds no root knows the entry has left the map, and the map
	// never holds an entry for a folder that is not there (a public 404 for
	// a well-shaped id adds nothing).
	entries sync.Map
}

// compRoot is one competition folder's root. mu is a LEAF lock: it may be
// taken under the competition lock, s.mu, compRenameMu or a cache mutex, and
// nothing is taken while it is held (no callback runs under it). It is held
// for reading by every call made on root, for that call only, and for
// writing while root is opened, dropped, or its folder moved, so no call runs
// on a root being closed, and no root is opened on a folder halfway through a
// move.
type compRoot struct {
	mu   sync.RWMutex
	root *os.Root    // nil only while the entry's writer holds mu, or once it left the map
	held fs.FileInfo // the folder root holds, as it was when opened (heldFolderGone)
}

// compFolderOf answers the id when rel is a competition folder itself
// (competitions/<id>), the one shape whose rename, removal or creation must
// drop that folder's root.
func compFolderOf(rel string) (string, bool) {
	id, found := strings.CutPrefix(rel, compDirPrefix)
	if !found || id == "" || strings.ContainsRune(id, filepath.Separator) || ValidateCompetitionID(id) != nil {
		return "", false
	}
	return id, true
}

// compFolderRel is competition id's folder, relative to the data folder.
func compFolderRel(id string) string {
	return filepath.Join("competitions", id)
}

// close closes every competition folder root held.
func (c *compRoots) close() error {
	var errs []error
	c.entries.Range(func(k, v any) bool {
		e := v.(*compRoot)
		e.mu.Lock()
		if e.root != nil {
			errs = append(errs, e.root.Close())
			e.root = nil
		}
		c.entries.CompareAndDelete(k, e)
		e.mu.Unlock()
		return true
	})
	return errors.Join(errs...)
}

// lockEntry answers competition id's entry with its write lock held, making
// one (with no root) when the map holds none. The entry answered is the one
// in the map: an entry found there and locked only after it left the map is
// let go, and the look is made again. Release it with unlockEntry.
func (c *compRoots) lockEntry(id string) *compRoot {
	for {
		mine := &compRoot{}
		mine.mu.Lock()
		v, loaded := c.entries.LoadOrStore(id, mine)
		if !loaded {
			return mine
		}
		mine.mu.Unlock()
		e := v.(*compRoot)
		e.mu.Lock()
		if cur, ok := c.entries.Load(id); ok && cur == e {
			return e
		}
		e.mu.Unlock()
	}
}

// unlockEntry releases an entry lockEntry answered, first removing it from the
// map when it holds no root, so the map keeps only entries with an open root.
func (c *compRoots) unlockEntry(id string, e *compRoot) {
	if e.root == nil {
		c.entries.CompareAndDelete(id, e)
	}
	e.mu.Unlock()
}

// closeLocked closes the entry's root, if any. Caller holds e.mu for writing.
func (e *compRoot) closeLocked(id string) {
	if e.root == nil {
		return
	}
	if err := e.root.Close(); err != nil {
		// A directory descriptor that fails to close holds nothing the store
		// still needs; the next call opens a fresh root either way.
		slog.Debug("state: closing a competition folder root failed", "competition", id, "error", err)
	}
	e.root = nil
	e.held = nil
}

// acquire answers competition id's root with its entry's read lock held
// (release it with e.mu.RUnlock), opening the root first if none is held. It
// answers nil, holding nothing and storing nothing, when the folder cannot be
// opened as a root (it does not exist, is a file, or links out of the data
// folder): the caller then runs the call on the data root, which reports that
// exactly as before.
func (c *compRoots) acquire(id string) (*compRoot, *os.Root) {
	for {
		if v, ok := c.entries.Load(id); ok {
			e := v.(*compRoot)
			e.mu.RLock()
			if r := e.root; r != nil {
				return e, r
			}
			// The entry left the map while this waited (dropped, moved, or
			// its open failed): look again.
			e.mu.RUnlock()
			continue
		}
		e := c.lockEntry(id)
		if e.root == nil {
			if r, err := c.data.OpenRoot(compFolderRel(id)); err == nil {
				if held, err := r.Stat("."); err == nil {
					e.root, e.held = r, held
				} else if cerr := r.Close(); cerr != nil {
					slog.Debug("state: closing a competition folder root failed", "competition", id, "error", cerr)
				}
			}
		}
		opened := e.root != nil
		c.unlockEntry(id, e)
		if !opened {
			return nil, nil
		}
	}
}

// dropIf closes and forgets competition id's root if it is still r; a root
// opened since (by another call that saw the same fault) is kept.
func (c *compRoots) dropIf(id string, r *os.Root) {
	v, ok := c.entries.Load(id)
	if !ok {
		return
	}
	e := v.(*compRoot)
	e.mu.Lock()
	if e.root == r {
		e.closeLocked(id)
		c.entries.CompareAndDelete(id, e)
	}
	e.mu.Unlock()
}

// heldFolderGone reports whether the folder e holds is no longer the one named
// competitions/<id> (it was removed, or removed and another put in its place).
// Asked only after a call on e's root answered ENOENT, so a file that is simply
// missing costs one more stat. Caller holds e.mu for reading.
func (c *compRoots) heldFolderGone(id string, e *compRoot) bool {
	named, err := c.data.Stat(compFolderRel(id))
	return err != nil || !os.SameFile(named, e.held)
}

// move runs fn, a rename, removal or creation on the data root, with the root
// of every competition folder named among rels dropped and its entry
// write-locked until fn returns, so nothing opens that folder's root while it
// moves; the entries leave the map as they are released. rels that are no
// competition folder are left alone. Entries are locked in id order, the one
// order any call takes more than one in.
func (c *compRoots) move(fn func() error, rels ...string) error {
	var ids []string
	for _, rel := range rels {
		if id, ok := compFolderOf(rel); ok && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return fn()
	}
	slices.Sort(ids)
	held := make([]*compRoot, 0, len(ids))
	for _, id := range ids {
		e := c.lockEntry(id)
		e.closeLocked(id)
		held = append(held, e)
	}
	err := fn()
	for i, e := range held {
		c.unlockEntry(ids[i], e)
	}
	return err
}
