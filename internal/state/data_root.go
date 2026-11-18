package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
// It also holds one os.Root per competition folder (comps, comp_roots.go). A
// call on a path strictly under competitions/<id>/ runs on that folder's root;
// a path that IS competitions/<id>, competitions/ itself, or anything else runs
// on the data root. A folder that cannot be opened as a root (it does not exist
// yet, is a file, links out) runs the call on the data root, which reports it
// exactly as before, and stores nothing. A rename, removal or creation of a
// competition folder itself goes through comps.move, which drops that folder's
// held root for the length of the syscall.
type dataRoot struct {
	root   *os.Root
	folder string
	comps  compRoots
}

func openDataRoot(folder string) (*dataRoot, error) {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return nil, err
	}
	d := &dataRoot{root: root, folder: folder}
	d.comps.data = root
	return d, nil
}

// close closes every competition folder root it holds, then the data root.
func (d *dataRoot) close() error {
	return errors.Join(d.comps.close(), d.root.Close())
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
			e, r := d.comps.acquire(id)
			if r == nil {
				break
			}
			err := onComp(r)
			stale := try == 0 && suspect(err) && d.comps.heldFolderGone(id, e)
			e.mu.RUnlock()
			if !stale {
				return absUnder(base, err)
			}
			d.comps.dropIf(id, r)
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
// root otherwise, inside comps.move when path IS a competition folder, so the
// folder's held root is dropped before it is removed or made again.
func (d *dataRoot) change(op, path string, suspect func(error) bool, onComp func(r *os.Root, name string) error, onData func(name string) error) error {
	id, rest, rel, err := d.at(op, path)
	if err != nil {
		return err
	}
	return d.onChecked(id, suspect,
		func(r *os.Root) error { return onComp(r, rest) },
		func() error { return d.comps.move(func() error { return onData(rel) }, rel) })
}

func (d *dataRoot) mkdir(path string, perm fs.FileMode) error {
	return d.change("mkdir", path, isNotExist,
		func(r *os.Root, name string) error { return r.Mkdir(name, perm) },
		func(name string) error { return d.root.Mkdir(name, perm) })
}

// mkdirAll makes path and its missing parents. When path is a competition
// folder (saveCompetitionChangedLocked's, the one place that creates one), the
// folder's held root is dropped first unless the folder is there and is the one
// the root holds, so a root never outlives the folder it was opened on: not one
// removed by hand, and not one moved aside and replaced by another. A folder
// that is there and held keeps its root: every config.md save asks, and
// dropping the root each time would throw away the cache on a save that changes
// nothing. (Only this door asks: another call on a folder swapped by hand runs
// on the old one until it answers ENOENT, which the retry on the next call
// follows.)
func (d *dataRoot) mkdirAll(path string, perm fs.FileMode) error {
	if rel, err := d.name("mkdir", path); err == nil {
		if id, isComp := compFolderOf(rel); isComp {
			if fi, err := d.root.Stat(rel); err == nil && fi.IsDir() && !d.comps.heldOtherThan(id, fi) {
				return nil
			}
		}
	}
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
// take its entry's read lock twice, which deadlocks against a comps.move
// writer queued between them). Both ends in one competition folder (the temp
// and the target of an atomic write, a quarantine) run on that folder's root;
// anything else runs on the data root, inside comps.move when either end is a
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
			return d.comps.move(func() error { return d.root.Rename(oldRel, newRel) }, oldRel, newRel)
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
// read lock on the same entry, which deadlocks as soon as a comps.move writer
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
