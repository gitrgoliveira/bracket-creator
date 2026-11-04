package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
type dataRoot struct {
	root   *os.Root
	folder string
}

func openDataRoot(folder string) (*dataRoot, error) {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return nil, err
	}
	return &dataRoot{root: root, folder: folder}, nil
}

func (d *dataRoot) close() error {
	return d.root.Close()
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
// folder-relative name it was given, each mapped to the operation the same
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
// root's own errors carry, and the by-path operation name in place of the
// root's, on every such error in the chain (MkdirAll wraps the one that
// stopped it).
func (d *dataRoot) abs(err error) error {
	for e := err; e != nil; e = errors.Unwrap(e) {
		switch x := e.(type) {
		case *fs.PathError:
			if byPath, ok := rootOps[x.Op]; ok {
				x.Op = byPath
				x.Path = filepath.Join(d.folder, x.Path)
			}
		case *os.LinkError:
			if byPath, ok := rootOps[x.Op]; ok {
				x.Op = byPath
				x.Old = filepath.Join(d.folder, x.Old)
				x.New = filepath.Join(d.folder, x.New)
			}
		}
	}
	return err
}

func (d *dataRoot) readFile(path string) ([]byte, error) {
	name, err := d.name("open", path)
	if err != nil {
		return nil, err
	}
	data, err := d.root.ReadFile(name)
	return data, d.abs(err)
}

func (d *dataRoot) open(path string) (*os.File, error) {
	name, err := d.name("open", path)
	if err != nil {
		return nil, err
	}
	f, err := d.root.Open(name)
	return f, d.abs(err)
}

func (d *dataRoot) openFile(path string, flag int, perm fs.FileMode) (*os.File, error) {
	name, err := d.name("open", path)
	if err != nil {
		return nil, err
	}
	f, err := d.root.OpenFile(name, flag, perm)
	return f, d.abs(err)
}

func (d *dataRoot) stat(path string) (fs.FileInfo, error) {
	name, err := d.name("stat", path)
	if err != nil {
		return nil, err
	}
	info, err := d.root.Stat(name)
	return info, d.abs(err)
}

// readDir lists the folder at path, sorted by name, as os.ReadDir does.
func (d *dataRoot) readDir(path string) ([]fs.DirEntry, error) {
	name, err := d.name("readdir", path)
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(d.root.FS(), filepath.ToSlash(name))
	return entries, d.abs(err)
}

func (d *dataRoot) mkdir(path string, perm fs.FileMode) error {
	name, err := d.name("mkdir", path)
	if err != nil {
		return err
	}
	return d.abs(d.root.Mkdir(name, perm))
}

func (d *dataRoot) mkdirAll(path string, perm fs.FileMode) error {
	name, err := d.name("mkdir", path)
	if err != nil {
		return err
	}
	return d.abs(d.root.MkdirAll(name, perm))
}

func (d *dataRoot) remove(path string) error {
	name, err := d.name("remove", path)
	if err != nil {
		return err
	}
	return d.abs(d.root.Remove(name))
}

func (d *dataRoot) removeAll(path string) error {
	name, err := d.name("remove", path)
	if err != nil {
		return err
	}
	return d.abs(d.root.RemoveAll(name))
}

func (d *dataRoot) rename(oldPath, newPath string) error {
	oldName, err := d.name("rename", oldPath)
	if err != nil {
		return err
	}
	newName, err := d.name("rename", newPath)
	if err != nil {
		return err
	}
	return d.abs(d.root.Rename(oldName, newName))
}

// walkDir walks the file tree rooted at path through the os.Root, calling fn
// for each file or directory in lexical order. Every path handed to fn has the
// data folder in front of it, matching filepath.WalkDir.
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
