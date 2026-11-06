package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// writeFile creates or truncates path with perm, writes data, syncs the file
// to disk, and closes it.
func (d *dataRoot) writeFile(path string, data []byte, perm fs.FileMode) error {
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

// stageFile creates any missing parent directories under the root and writes
// and fsyncs data at path.
func (d *dataRoot) stageFile(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(filepath.Clean(path))
	if err := d.mkdirAll(dir, 0o700); err != nil {
		return err
	}
	return d.writeFile(path, data, perm)
}

// syncDir flushes the directory entry metadata of path to disk.
func (d *dataRoot) syncDir(path string) error {
	return syncDir(d, path)
}
