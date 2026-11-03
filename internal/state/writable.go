package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// BrandingDirName, SponsorsDirName, BackupsDirName and RestoreDirName are the
// top-level folders under the tournament data folder for uploaded images, kept
// backup archives and restore staging.
const (
	BrandingDirName = "branding"
	SponsorsDirName = "sponsors"
	BackupsDirName  = "backups"
	RestoreDirName  = ".restore"
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
			failed = append(failed, fmt.Sprintf("  %s: %v; %s", dir, err, writeRemedy(err)))
		}
	}
	// The upload handlers write branding/ and sponsors/ by path, not through
	// the root, so each is probed only when the root opens it as a folder.
	for _, name := range []string{BrandingDirName, SponsorsDirName} {
		d := filepath.Join(folder, name)
		if info, err := root.stat(d); err == nil && info.IsDir() {
			dirs = append(dirs, d)
		}
	}
	// Backups and restore staging are written through the root: when an entry
	// already sits at backups/ or .restore/ (a folder, a file blocking its
	// creation, or a link out of the data folder), probe it so startup refuses
	// before a scheduled backup or restore fails.
	for _, name := range []string{BackupsDirName, RestoreDirName} {
		d := filepath.Join(folder, name)
		if _, err := root.stat(d); !errors.Is(err, fs.ErrNotExist) {
			dirs = append(dirs, d)
		}
	}
	entries, err := root.readDir(compsDir)
	if err != nil {
		fail(compsDir, pathErrCause(err))
	}
	for _, e := range entries {
		// The same competition folders ListCompetitions lists: a link under
		// competitions/ is not one.
		if !e.IsDir() {
			continue
		}
		compDir := filepath.Join(compsDir, e.Name())
		dirs = append(dirs, compDir)
		for _, sub := range []string{matchHistoryDir, scoreLogDir} {
			// Saved through the root: an entry it refuses (a link out of
			// the data folder) would refuse every save, so it is named; a
			// missing one, or a file, is not a folder the server saves into.
			d := filepath.Join(compDir, sub)
			info, err := root.stat(d)
			switch {
			case err == nil && info.IsDir():
				dirs = append(dirs, d)
			case err != nil && !errors.Is(err, fs.ErrNotExist):
				fail(d, pathErrCause(err))
			}
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
	return fmt.Errorf("%w:\n%s\nResults could not be saved there. Fix each folder as its line says and start again",
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

// writeRemedy is what the operator does about a folder the check could not
// write, judged from the error alone: replace a link out of the data folder
// with the folder itself (the store saves only inside the data folder), move
// away a file that sits where the folder should be, or, for every other
// failure, give the server write access to it.
func writeRemedy(err error) string {
	switch {
	case isPathEscape(err):
		return "it links out of the data folder (a link by an absolute path counts, wherever it points), which the server never saves through: put the folder itself there instead of the link"
	case errors.Is(err, syscall.ENOTDIR):
		return "a file is where this folder should be: move the file away, and the server makes the folder when it starts"
	}
	return "give the user running the server write access to it (for example chmod u+w <folder>)"
}

// pathEscapesText is the text of the error an os.Root gives for a name that
// leaves it, a link out of the data folder included. The os package keeps
// that error unexported and exports nothing errors.Is could match, so the
// text is the one test left; TestDataRoot_RefusesASymlinkOutOfTheDataFolder
// pins it against what the root really answers.
const pathEscapesText = "path escapes from parent"

// isPathEscape reports whether err, or an error it wraps, is the root's
// refusal of a name that leaves the data folder.
func isPathEscape(err error) bool {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if e.Error() == pathEscapesText {
			return true
		}
	}
	return false
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
