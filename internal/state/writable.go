package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
// folder and its match history, and the branding and sponsors folders when
// they exist. A file's own mode does not matter: a save replaces it by
// renaming, which needs only its folder. Every folder that fails is named,
// once (its first failure).
func checkDataWritable(folder string) error {
	compsDir := filepath.Join(folder, "competitions")
	dirs := []string{folder, compsDir, filepath.Join(folder, ".wal")}
	for _, name := range []string{BrandingDirName, SponsorsDirName} {
		if isDir(filepath.Join(folder, name)) {
			dirs = append(dirs, filepath.Join(folder, name))
		}
	}
	var failed []string
	named := make(map[string]bool)
	fail := func(dir string, err error) {
		if !named[dir] {
			named[dir] = true
			failed = append(failed, fmt.Sprintf("  %s: %v", dir, err))
		}
	}
	entries, err := os.ReadDir(compsDir)
	if err != nil {
		fail(compsDir, pathErrCause(err))
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		compDir := filepath.Join(compsDir, e.Name())
		dirs = append(dirs, compDir)
		if history := filepath.Join(compDir, matchHistoryDir); isDir(history) {
			dirs = append(dirs, history)
		}
	}
	for _, d := range dirs {
		if err := probeWrite(d); err != nil {
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
func probeWrite(dir string) error {
	f, err := os.CreateTemp(dir, writeProbePrefix+"*")
	if err != nil {
		return pathErrCause(err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		return errors.Join(err, pathErrCause(os.Remove(name)))
	}
	return pathErrCause(os.Remove(name))
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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
