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

// dataRoot is the one door through which the store opens, reads, writes,
// renames and removes its files. It holds an os.Root on the data folder, so
// the operating system, not a path check, keeps every one of those inside it:
// a name that climbs out with "..", or a symlink inside the folder that points
// outside it, is refused ("path escapes from parent"). A symlink AT the data
// folder itself is followed when the root is opened, so a data folder that is
// a link to another disk keeps working.
//
// Its methods take the paths the store already builds (compPath,
// matchHistoryPath, filepath.Join(folder, ...)), so those builders, the paths
// the store logs, and the tests that read a file by store.compPath stay as
// they were. Each method turns the path into a name relative to the folder,
// and puts the folder back in front of the name an error carries, so a
// message still names the file as the store built it.
//
// The root holds one open descriptor for the life of the Store. The Store has
// no Close: the app keeps one for the life of the process, NewStore closes the
// root when the store fails to open, and os.Root closes its descriptor when a
// Store a test dropped is collected.
//
// It also holds one os.Root per competition folder (comps), opened from the
// data root on first use (Root.OpenRoot, so the OS still refuses a folder that
// links out of the data folder). A call on a path strictly under
// competitions/<id>/ runs on that folder's root, which saves resolving
// competitions/ and <id> again on every call; a path that IS
// competitions/<id>, competitions/ itself, or anything else runs on the data
// root. A folder that cannot be opened as a root (it does not exist yet, is a
// file, links out) runs the call on the data root, which reports it exactly as
// before, and stores nothing.
//
// A held root is a descriptor on the folder, not its name: it follows the
// folder if the folder is renamed, and keeps an unlinked folder alive if it is
// removed. So every rename, removal or creation of a competition folder itself
// drops that folder's root, and holds its entry write-locked for the length of
// the syscall, so nothing reopens it mid-move (moveComps). The drop is made
// here, in rename, remove, removeAll, mkdir and mkdirAll, whatever called
// them: DeleteCompetition's removeAll, the restore journal's moves out and in
// (runRestoreJournal, which the live restore and startup recovery both run),
// and saveCompetitionChangedLocked's mkdirAll, which re-creates a folder
// removed by hand. The removal of a restore token folder needs no drop: it
// lies under .restore/, which no held root points into, since a folder moved
// out is dropped as it moves. Two removeAll calls remove a folder INSIDE a
// competition folder (the score logs' events/ in score_log.go, the match
// history's history/ in match_history.go); the competition's root serves them
// and nothing is dropped. No store path renames or removes competitions/ or
// the data folder itself; one that did would have to drop every held root.
//
// A folder removed from OUTSIDE the app while the server runs answers ENOENT
// to a call on its held root; the call then checks that the folder named
// competitions/<id> is still the one held (heldFolderGone), and if it is not,
// drops the root and runs once more on whatever is there now, so a folder put
// back by hand (a copy from a backup) is written again without a restart, and
// one still gone fails as before. A folder RENAMED from outside the app while
// the server runs is followed by its held root, which gets no ENOENT: the
// server goes on reading and writing the renamed folder until the root is
// next dropped or the server restarts (accepted; no supported workflow does
// it, since the CLI restore runs with the server stopped).
type dataRoot struct {
	root   *os.Root
	folder string
	// comps maps a competition id to its *compRoot. An entry is in the map
	// only while its root is open, or while the one goroutine that holds its
	// write lock opens or moves it; that goroutine stores a root or removes
	// the entry before it unlocks. So a reader that takes an entry's read
	// lock and finds no root knows the entry has left the map, and the map
	// never holds an entry for a folder that is not there (a public 404 for
	// a well-shaped id adds nothing).
	comps sync.Map
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

func openDataRoot(folder string) (*dataRoot, error) {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return nil, err
	}
	return &dataRoot{root: root, folder: folder}, nil
}

// close closes every competition folder root it holds, then the data root.
func (d *dataRoot) close() error {
	var errs []error
	d.comps.Range(func(k, v any) bool {
		e := v.(*compRoot)
		e.mu.Lock()
		if e.root != nil {
			errs = append(errs, e.root.Close())
			e.root = nil
		}
		d.comps.CompareAndDelete(k, e)
		e.mu.Unlock()
		return true
	})
	errs = append(errs, d.root.Close())
	return errors.Join(errs...)
}

// compDirPrefix is "competitions" plus a separator: the start of a name, made
// relative to the data folder, that lies in a competition folder.
var compDirPrefix = "competitions" + string(filepath.Separator)

// compOf splits a folder-relative name into the competition id it lies under
// and the rest of it: ("c1", "history/m.jsonl", true) for
// competitions/c1/history/m.jsonl. A name that is a competition folder itself,
// competitions/ itself, or anything outside it answers ok false, as does an
// id ValidateCompetitionID refuses.
func compOf(rel string) (id, rest string, ok bool) {
	after, found := strings.CutPrefix(rel, compDirPrefix)
	if !found {
		return "", "", false
	}
	id, rest, found = strings.Cut(after, string(filepath.Separator))
	if !found || rest == "" || ValidateCompetitionID(id) != nil {
		return "", "", false
	}
	return id, rest, true
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

// lockEntry answers competition id's entry with its write lock held, making
// one (with no root) when the map holds none. The entry answered is the one
// in the map: an entry found there and locked only after it left the map is
// let go, and the look is made again. Release it with unlockEntry.
func (d *dataRoot) lockEntry(id string) *compRoot {
	for {
		mine := &compRoot{}
		mine.mu.Lock()
		v, loaded := d.comps.LoadOrStore(id, mine)
		if !loaded {
			return mine
		}
		mine.mu.Unlock()
		e := v.(*compRoot)
		e.mu.Lock()
		if cur, ok := d.comps.Load(id); ok && cur == e {
			return e
		}
		e.mu.Unlock()
	}
}

// unlockEntry releases an entry lockEntry answered, first removing it from the
// map when it holds no root, so the map keeps only entries with an open root.
func (d *dataRoot) unlockEntry(id string, e *compRoot) {
	if e.root == nil {
		d.comps.CompareAndDelete(id, e)
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

// acquireComp answers competition id's root with its entry's read lock held
// (release it with e.mu.RUnlock), opening the root first if none is held. It
// answers nil, holding nothing and storing nothing, when the folder cannot be
// opened as a root (it does not exist, is a file, or links out of the data
// folder): the caller then runs the call on the data root, which reports that
// exactly as before.
func (d *dataRoot) acquireComp(id string) (*compRoot, *os.Root) {
	for {
		if v, ok := d.comps.Load(id); ok {
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
		e := d.lockEntry(id)
		if e.root == nil {
			if r, err := d.root.OpenRoot(compFolderRel(id)); err == nil {
				if held, err := r.Stat("."); err == nil {
					e.root, e.held = r, held
				} else if cerr := r.Close(); cerr != nil {
					slog.Debug("state: closing a competition folder root failed", "competition", id, "error", cerr)
				}
			}
		}
		opened := e.root != nil
		d.unlockEntry(id, e)
		if !opened {
			return nil, nil
		}
	}
}

// dropIf closes and forgets competition id's root if it is still r; a root
// opened since (by another call that saw the same fault) is kept.
func (d *dataRoot) dropIf(id string, r *os.Root) {
	v, ok := d.comps.Load(id)
	if !ok {
		return
	}
	e := v.(*compRoot)
	e.mu.Lock()
	if e.root == r {
		e.closeLocked(id)
		d.comps.CompareAndDelete(id, e)
	}
	e.mu.Unlock()
}

// heldFolderGone reports whether the folder r holds is no longer the one named
// competitions/<id> (it was removed, or removed and another put in its place).
// Asked only after a call on r answered ENOENT, so a file that is simply
// missing costs one more stat. Caller holds e.mu for reading.
func (d *dataRoot) heldFolderGone(id string, e *compRoot) bool {
	named, err := d.root.Stat(compFolderRel(id))
	return err != nil || !os.SameFile(named, e.held)
}

// on runs one call on the root a path under competition id lies on: that
// folder's root when it opens (onComp), the data root otherwise (onData). A
// call on the folder's root that answers ENOENT while the folder held is no
// longer the one named competitions/<id> drops the root and runs once more,
// never more than once. Errors name the path as the store built it.
func (d *dataRoot) on(id string, onComp func(r *os.Root) error, onData func() error) error {
	return d.onChecked(id, isNotExist, onComp, onData)
}

func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }

// onChecked is on, asking suspect of the call's error whether the held folder
// may be gone. removeAll asks after every call, since a removal answers no
// error for a name that is not there, so a removal in a folder removed by
// hand would otherwise succeed on the dead folder and leave the live one as
// it was.
func (d *dataRoot) onChecked(id string, suspect func(error) bool, onComp func(r *os.Root) error, onData func() error) error {
	if id != "" {
		base := filepath.Join(d.folder, compFolderRel(id))
		for try := 0; try < 2; try++ {
			e, r := d.acquireComp(id)
			if r == nil {
				break
			}
			err := onComp(r)
			stale := try == 0 && suspect(err) && d.heldFolderGone(id, e)
			e.mu.RUnlock()
			if !stale {
				return absUnder(base, err)
			}
			d.dropIf(id, r)
		}
	}
	return absUnder(d.folder, onData())
}

// at resolves path for a call by one name: the competition id it lies
// strictly under ("" for none), the name relative to that competition's
// folder, and the name relative to the data folder.
func (d *dataRoot) at(op, path string) (id, rest, rel string, err error) {
	rel, err = d.name(op, path)
	if err != nil {
		return "", "", "", err
	}
	id, rest, _ = compOf(rel)
	return id, rest, rel, nil
}

// moveComps runs fn, a rename, removal or creation on the data root, with the
// root of every competition folder named among rels dropped and its entry
// write-locked until fn returns, so nothing opens that folder's root while it
// moves; the entries leave the map as they are released. rels that are no
// competition folder are left alone. Entries are locked in id order, the one
// order any call takes more than one in.
func (d *dataRoot) moveComps(fn func() error, rels ...string) error {
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
		e := d.lockEntry(id)
		e.closeLocked(id)
		held = append(held, e)
	}
	err := fn()
	for i, e := range held {
		d.unlockEntry(ids[i], e)
	}
	return err
}

// name is path relative to the data folder.
func (d *dataRoot) name(op, path string) (string, error) {
	rel, err := filepath.Rel(d.folder, path)
	if err != nil {
		return "", &fs.PathError{Op: op, Path: path, Err: err}
	}
	return rel, nil
}

// rootOps names the operations an os.Root call reports with the
// root-relative name it was given, each mapped to the operation the same
// call by path reports, so a message reads as it did before the store had a
// root ("open /data/competitions/c1/config.md: ...", never "openat ...").
// RemoveAll's error is the one a removal by path usually stops on, the
// unlink. Any other error (a read or a directory listing failing on a file
// the root already opened) names the file root.Name() joined to the name,
// which is already the path the store built, and is left as it is.
var rootOps = map[string]string{
	"openat":    "open",
	"statat":    "stat",
	"mkdirat":   "mkdir",
	"removeat":  "remove",
	"renameat":  "rename",
	"RemoveAll": "unlinkat",
}

// abs puts the data folder back in front of the folder-relative names the
// data root's own errors carry.
func (d *dataRoot) abs(err error) error {
	return absUnder(d.folder, err)
}

// absUnder puts base, the folder of the root a call ran on, back in front of
// the root-relative names its errors carry, and the by-path operation name in
// place of the root's, on every such error in the chain (MkdirAll wraps the
// one that stopped it).
func absUnder(base string, err error) error {
	for e := err; e != nil; e = errors.Unwrap(e) {
		switch x := e.(type) {
		case *fs.PathError:
			if byPath, ok := rootOps[x.Op]; ok {
				x.Op = byPath
				x.Path = filepath.Join(base, x.Path)
			}
		case *os.LinkError:
			if byPath, ok := rootOps[x.Op]; ok {
				x.Op = byPath
				x.Old = filepath.Join(base, x.Old)
				x.New = filepath.Join(base, x.New)
			}
		}
	}
	return err
}

func (d *dataRoot) readFile(path string) ([]byte, error) {
	id, rest, rel, err := d.at("open", path)
	if err != nil {
		return nil, err
	}
	var data []byte
	err = d.on(id,
		func(r *os.Root) (err error) { data, err = r.ReadFile(rest); return err },
		func() (err error) { data, err = d.root.ReadFile(rel); return err })
	return data, err
}

// open opens path for reading. The *os.File answered is its own descriptor,
// valid after a held competition root is dropped, so no lock is kept for its
// life.
func (d *dataRoot) open(path string) (*os.File, error) {
	id, rest, rel, err := d.at("open", path)
	if err != nil {
		return nil, err
	}
	var f *os.File
	err = d.on(id,
		func(r *os.Root) (err error) { f, err = r.Open(rest); return err },
		func() (err error) { f, err = d.root.Open(rel); return err })
	return f, err
}

// openFile is open with flags and a mode; see open for the file's lifetime.
func (d *dataRoot) openFile(path string, flag int, perm fs.FileMode) (*os.File, error) {
	id, rest, rel, err := d.at("open", path)
	if err != nil {
		return nil, err
	}
	var f *os.File
	err = d.on(id,
		func(r *os.Root) (err error) { f, err = r.OpenFile(rest, flag, perm); return err },
		func() (err error) { f, err = d.root.OpenFile(rel, flag, perm); return err })
	return f, err
}

func (d *dataRoot) stat(path string) (fs.FileInfo, error) {
	id, rest, rel, err := d.at("stat", path)
	if err != nil {
		return nil, err
	}
	var info fs.FileInfo
	err = d.on(id,
		func(r *os.Root) (err error) { info, err = r.Stat(rest); return err },
		func() (err error) { info, err = d.root.Stat(rel); return err })
	return info, err
}

// readDir lists the folder at path, sorted by name, as os.ReadDir does.
func (d *dataRoot) readDir(path string) ([]fs.DirEntry, error) {
	id, rest, rel, err := d.at("readdir", path)
	if err != nil {
		return nil, err
	}
	var entries []fs.DirEntry
	err = d.on(id,
		func(r *os.Root) (err error) { entries, err = fs.ReadDir(r.FS(), filepath.ToSlash(rest)); return err },
		func() (err error) { entries, err = fs.ReadDir(d.root.FS(), filepath.ToSlash(rel)); return err })
	return entries, err
}

// change runs one call that creates or removes the thing at path: on its
// competition folder's root when path lies strictly under one, and on the data
// root otherwise, inside moveComps when path IS a competition folder, so the
// folder's held root is dropped before it is removed or made again.
func (d *dataRoot) change(op, path string, suspect func(error) bool, onComp func(r *os.Root, name string) error, onData func(name string) error) error {
	id, rest, rel, err := d.at(op, path)
	if err != nil {
		return err
	}
	return d.onChecked(id, suspect,
		func(r *os.Root) error { return onComp(r, rest) },
		func() error { return d.moveComps(func() error { return onData(rel) }, rel) })
}

func (d *dataRoot) mkdir(path string, perm fs.FileMode) error {
	return d.change("mkdir", path, isNotExist,
		func(r *os.Root, name string) error { return r.Mkdir(name, perm) },
		func(name string) error { return d.root.Mkdir(name, perm) })
}

// mkdirAll makes path and its missing parents. When path is a competition
// folder (saveCompetitionChangedLocked's, the one place that creates one), the
// folder's held root is dropped first, so a root held on a folder removed by
// hand never outlives the folder made in its place.
func (d *dataRoot) mkdirAll(path string, perm fs.FileMode) error {
	return d.change("mkdir", path, isNotExist,
		func(r *os.Root, name string) error { return r.MkdirAll(name, perm) },
		func(name string) error { return d.root.MkdirAll(name, perm) })
}

func (d *dataRoot) remove(path string) error {
	return d.change("remove", path, isNotExist,
		func(r *os.Root, name string) error { return r.Remove(name) },
		func(name string) error { return d.root.Remove(name) })
}

// removeAll removes path and what it holds. Removing a competition folder
// (DeleteCompetition) drops its held root under its entry's write lock first.
func (d *dataRoot) removeAll(path string) error {
	return d.change("remove", path, func(error) bool { return true },
		func(r *os.Root, name string) error { return r.RemoveAll(name) },
		func(name string) error { return d.root.RemoveAll(name) })
}

// rename resolves both paths once (two resolutions on one competition would
// take its entry's read lock twice, which deadlocks against a moveComps
// writer queued between them). Both ends in one competition folder (the temp
// and the target of an atomic write, a quarantine) run on that folder's root;
// anything else runs on the data root, inside moveComps when either end is a
// competition folder itself (the restore journal's moves out and in), so its
// held root is dropped for the length of the rename.
func (d *dataRoot) rename(oldPath, newPath string) error {
	oldRel, err := d.name("rename", oldPath)
	if err != nil {
		return err
	}
	newRel, err := d.name("rename", newPath)
	if err != nil {
		return err
	}
	oldID, oldRest, oldIn := compOf(oldRel)
	newID, newRest, newIn := compOf(newRel)
	id := ""
	if oldIn && newIn && oldID == newID {
		id = oldID
	}
	return d.on(id,
		func(r *os.Root) error { return r.Rename(oldRest, newRest) },
		func() error {
			return d.moveComps(func() error { return d.root.Rename(oldRel, newRel) }, oldRel, newRel)
		})
}

// walkDir walks the file tree rooted at path through the os.Root, calling fn
// for each file or directory in lexical order. Every path handed to fn has the
// data folder in front of it, matching filepath.WalkDir.
//
// It ALWAYS runs on the data root and holds no competition entry's lock, even
// for a tree inside a competition folder: its one caller
// (collectTreeFilesLocked, a backup) reads each file through readFile from
// inside fn, and a read lock held across the walk would make that a recursive
// read lock on the same entry, which deadlocks as soon as a moveComps writer
// queues between the two (a Go RWMutex blocks new readers behind a waiting
// writer).
func (d *dataRoot) walkDir(path string, fn fs.WalkDirFunc) error {
	name, err := d.name("open", path)
	if err != nil {
		return err
	}
	err = fs.WalkDir(d.root.FS(), filepath.ToSlash(name), func(p string, entry fs.DirEntry, walkErr error) error {
		full := d.folder
		if p != "." {
			full = filepath.Join(d.folder, filepath.FromSlash(p))
		}
		return fn(full, entry, d.abs(walkErr))
	})
	return d.abs(err)
}

// atomicWrite writes data to path through atomicWriteFile on this root.
func (d *dataRoot) atomicWrite(path string, data []byte, perm fs.FileMode) error {
	return atomicWriteFile(d, path, data, perm)
}

// writeSynced makes path's missing parent folders, then creates (or
// truncates) path, writes data, fsyncs it and closes it. The restore stages
// its files with it (restore.go): a staging folder is private to one restore,
// so nothing needs atomicWrite's temp-and-rename, only the fsync that makes
// the staged bytes durable before the journal names them.
func (d *dataRoot) writeSynced(path string, data []byte, perm fs.FileMode) error {
	if err := d.mkdirAll(filepath.Dir(filepath.Clean(path)), 0o700); err != nil {
		return err
	}
	f, err := d.openFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	return f.Close()
}

// exists reports whether path is present (as anything); an error other than
// "not there" is returned as it is.
func (d *dataRoot) exists(path string) (bool, error) {
	_, err := d.stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}
