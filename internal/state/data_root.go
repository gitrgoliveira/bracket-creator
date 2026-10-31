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

// rootOps maps the operation an os.Root error names to the one the same call
// by path names, so a message reads as it did before the store had a root
// ("open /data/competitions/c1/config.md: ...", never "openat ..."). RemoveAll
// says "unlinkat" by path too, so it needs no entry.
var rootOps = map[string]string{
	"openat":   "open",
	"statat":   "stat",
	"mkdirat":  "mkdir",
	"removeat": "remove",
	"renameat": "rename",
}

// rootOp is the by-path name of an os.Root error's operation.
func rootOp(op string) string {
	if byPath, ok := rootOps[op]; ok {
		return byPath
	}
	return op
}

// abs puts the data folder back in front of the folder-relative names an
// os.Root error carries, and the by-path operation name in place of the
// root's, on every such error in the chain (MkdirAll wraps the one that
// stopped it).
func (d *dataRoot) abs(err error) error {
	for e := err; e != nil; e = errors.Unwrap(e) {
		switch x := e.(type) {
		case *fs.PathError:
			x.Op = rootOp(x.Op)
			x.Path = filepath.Join(d.folder, x.Path)
		case *os.LinkError:
			x.Op = rootOp(x.Op)
			x.Old = filepath.Join(d.folder, x.Old)
			x.New = filepath.Join(d.folder, x.New)
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
