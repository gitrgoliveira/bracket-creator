package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BrandingDirName and SponsorsDirName are the folders, under the tournament
// data folder, that hold the uploaded logo and sponsor images. Named here so
// the startup write check and the upload handlers agree on them.
const (
	BrandingDirName = "branding"
	SponsorsDirName = "sponsors"
)

// ErrDataNotWritable: the server cannot write to a folder of its tournament
// data, so it does not start (operator decision 2026-10-04). Every save
// writes a temporary file beside its target and renames it into place, so a
// folder it cannot write would refuse every result saved there; found at
// startup, it is fixed before the event instead of failing mid-match.
var ErrDataNotWritable = errors.New("the server cannot write to its tournament data")

// writeProbePrefix names the scratch file the check creates and removes.
const writeProbePrefix = ".write-check-"

// checkDataWritable tries a write in every folder the server saves into:
// the data folder, competitions/, the transaction log, each competition's
// folder, its match history and its score event logs, and the branding and sponsors folders when
// they exist. A file's own mode does not matter: a save replaces it by
// renaming, which needs only its folder. Every folder that fails is named,
// once (its first failure). The probe goes through root, as every save does,
// so a folder that is a link out of the data folder fails here rather than
// at the first save.
func checkDataWritable(root *dataRoot) error {
	folder := root.folder
	compsDir := filepath.Join(folder, "competitions")
	dirs := []string{folder, compsDir, filepath.Join(folder, ".wal")}
	var failed []string
	named := make(map[string]bool)
	fail := func(dir string, err error) {
		if !named[dir] {
			named[dir] = true
			failed = append(failed, fmt.Sprintf("  %s: %v", dir, err))
		}
	}
	// whenFolder adds dir to the folders to probe when the root opens it as
	// a folder. An entry the root refuses (a link out of the data folder,
	// which every later read and write would refuse too) is named now; a
	// missing entry, or a file, is not a folder the server saves into.
	whenFolder := func(dir string) bool {
		info, err := root.stat(dir)
		switch {
		case err == nil && info.IsDir():
			dirs = append(dirs, dir)
			return true
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			fail(dir, pathErrCause(err))
		}
		return false
	}
	for _, name := range []string{BrandingDirName, SponsorsDirName} {
		whenFolder(filepath.Join(folder, name))
	}
	entries, err := root.readDir(compsDir)
	if err != nil {
		fail(compsDir, pathErrCause(err))
	}
	for _, e := range entries {
		if !e.IsDir() && e.Type()&fs.ModeSymlink == 0 {
			continue
		}
		compDir := filepath.Join(compsDir, e.Name())
		if !whenFolder(compDir) {
			continue
		}
		for _, sub := range []string{matchHistoryDir, scoreLogDir} {
			whenFolder(filepath.Join(compDir, sub))
		}
	}
	for _, d := range dirs {
		if err := probeWrite(root, d); err != nil {
			fail(d, err)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return fmt.Errorf("%w:\n%s\nResults could not be saved there. Give the user running the server write access to these folders (for example chmod u+w <folder>) and start again",
		ErrDataNotWritable, strings.Join(failed, "\n"))
}

// probeWrite creates a scratch file in dir and removes it.
func probeWrite(root *dataRoot, dir string) error {
	name := filepath.Join(dir, fmt.Sprintf("%s%d-%d", writeProbePrefix, os.Getpid(), time.Now().UnixNano()))
	f, err := root.openFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return pathErrCause(err)
	}
	if err := f.Close(); err != nil {
		return errors.Join(err, pathErrCause(root.remove(name)))
	}
	return pathErrCause(root.remove(name))
}

// pathErrCause drops the path an fs.PathError repeats, since the message
// already names the folder.
func pathErrCause(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
