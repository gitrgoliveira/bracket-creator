package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"path/filepath"
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
)

// The restore journal. A restore stages everything it will put in place under
// <data>/.restore/<token>/new/, then writes .restore/<token>/journal.json
// listing each rename of the swap, then runs the renames. A crash after the
// journal is durable is rolled forward by Store.init (recoverRestoresLocked);
// a crash before it leaves a token folder with no journal, which init removes.
//
// Every step is one rename of one of two kinds, each judged by what exists so
// that running the list again changes nothing:
//
//   - move out (live -> .restore/<token>/old/<path>): done when old/<path>
//     exists. The live path may by then hold the restored content, so the live
//     side is never looked at.
//   - move in (.restore/<token>/new/<path> -> live): done when new/<path> is
//     absent.
//
// The journal lists a move out only for a live path that exists when it is
// written, and a move in only for a path the archive holds, so a branding/ the
// folder lacks gets only a move in and one the archive lacks only a move out
// (it goes with the token folder).

const (
	restoreJournalName  = "journal.json"
	restoreNewDir       = "new"
	restoreOldDir       = "old"
	prefixesPendingName = "prefixes-pending.json"
	restoreJournalV1    = 1
)

type restoreStepKind string

const (
	restoreMoveOut restoreStepKind = "out"
	restoreMoveIn  restoreStepKind = "in"
)

// restoreStep is one rename. Path is the live path relative to the data
// folder, with forward slashes ("competitions/<id>", "tournament.md",
// "branding", "sponsors").
type restoreStep struct {
	Kind restoreStepKind `json:"kind"`
	Path string          `json:"path"`
}

type restoreJournal struct {
	Format int           `json:"format"`
	Steps  []restoreStep `json:"steps"`
}

// afterRestoreStep, when set, runs after each journal step the swap or the
// recovery completes; an error it returns stops the run there, as a crash
// would. Only the crash-injection tests set it.
var afterRestoreStep func(i int, step restoreStep) error

func (s *Store) restoreRootDir() string {
	return filepath.Join(s.folder, backup.RestoreDirName)
}

func (s *Store) restoreTokenDir(token string) string {
	return filepath.Join(s.restoreRootDir(), token)
}

// validRestoreStepPath accepts only the live paths a restore ever swaps, so a
// hand-edited or damaged journal cannot rename anything else in the folder.
func validRestoreStepPath(p string) bool {
	switch p {
	case "tournament.md", BrandingDirName, SponsorsDirName:
		return true
	}
	dir, id := path.Split(p)
	return dir == "competitions/" && ValidateCompetitionID(id) == nil
}

func (j restoreJournal) validate() error {
	if j.Format != restoreJournalV1 {
		return fmt.Errorf("restore journal: format %d, this build reads %d", j.Format, restoreJournalV1)
	}
	for _, st := range j.Steps {
		if st.Kind != restoreMoveOut && st.Kind != restoreMoveIn {
			return fmt.Errorf("restore journal: unknown step kind %q", st.Kind)
		}
		if !validRestoreStepPath(st.Path) {
			return fmt.Errorf("restore journal: step path %q is not one a restore swaps", st.Path)
		}
	}
	return nil
}

// writeRestoreJournal writes the journal atomically and fsyncs the token
// folder, so the journal is durable before the first live rename.
func (s *Store) writeRestoreJournal(tokenDir string, j restoreJournal) error {
	data, err := json.Marshal(j)
	if err != nil {
		return err
	}
	if err := s.root.atomicWrite(filepath.Join(tokenDir, restoreJournalName), data, 0o600); err != nil {
		return err
	}
	return syncDir(s.root, tokenDir)
}

// runRestoreJournal runs every step in order, skipping the ones already done,
// then fsyncs every folder a rename touched. Caller holds the locks the
// restore holds, or is Store.init under its s.mu.
func (s *Store) runRestoreJournal(tokenDir string, j restoreJournal) error {
	touched := map[string]struct{}{}
	for i, st := range j.Steps {
		live := filepath.Join(s.folder, filepath.FromSlash(st.Path))
		switch st.Kind {
		case restoreMoveOut:
			old := filepath.Join(tokenDir, restoreOldDir, filepath.FromSlash(st.Path))
			done, err := s.root.exists(old)
			if err != nil {
				return err
			}
			if !done {
				if err := s.root.mkdirAll(filepath.Dir(old), 0o700); err != nil {
					return err
				}
				if err := s.root.rename(live, old); err != nil {
					return fmt.Errorf("restore: move out %s: %w", st.Path, err)
				}
			}
			touched[filepath.Dir(live)] = struct{}{}
			touched[filepath.Dir(old)] = struct{}{}
		case restoreMoveIn:
			staged := filepath.Join(tokenDir, restoreNewDir, filepath.FromSlash(st.Path))
			pending, err := s.root.exists(staged)
			if err != nil {
				return err
			}
			if pending {
				if err := s.root.rename(staged, live); err != nil {
					return fmt.Errorf("restore: move in %s: %w", st.Path, err)
				}
			}
			touched[filepath.Dir(live)] = struct{}{}
			touched[filepath.Dir(staged)] = struct{}{}
		}
		if afterRestoreStep != nil {
			if err := afterRestoreStep(i, st); err != nil {
				return err
			}
		}
	}
	dirs := make([]string, 0, len(touched))
	for d := range touched {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)
	for _, d := range dirs {
		if err := syncDir(s.root, d); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// finishRestoreToken removes the journal (after runRestoreJournal's folder
// fsyncs), fsyncs the token folder and the data folder, and only then removes
// the token folder with what it still holds (the moved-out originals).
func (s *Store) finishRestoreToken(tokenDir string) error {
	if err := s.root.remove(filepath.Join(tokenDir, restoreJournalName)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := syncDir(s.root, tokenDir); err != nil {
		return err
	}
	if err := syncDir(s.root, s.folder); err != nil {
		return err
	}
	if err := s.root.removeAll(tokenDir); err != nil {
		return err
	}
	return syncDir(s.root, s.restoreRootDir())
}

// recoverRestoresLocked rolls forward every restore a crash interrupted after
// its journal was written, and removes every staging folder left without one
// (the restore died while checking or staging, before it touched a live
// path). prefixes-pending.json is left for the caller, which can reach the
// engine. Runs at the end of Store.init, under its s.mu, so it uses s.root
// primitives only, never a locking Store method.
func (s *Store) recoverRestoresLocked() error {
	entries, err := s.root.readDir(s.restoreRootDir())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		tokenDir := filepath.Join(s.restoreRootDir(), e.Name())
		raw, err := s.root.readFile(filepath.Join(tokenDir, restoreJournalName))
		if errors.Is(err, fs.ErrNotExist) {
			slog.Info("state: removing a restore staging folder left without a journal", "dir", tokenDir)
			if err := s.root.removeAll(tokenDir); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		var j restoreJournal
		if err := json.Unmarshal(raw, &j); err != nil {
			return fmt.Errorf("restore journal %s: %w", tokenDir, err)
		}
		if err := j.validate(); err != nil {
			return fmt.Errorf("%s: %w", tokenDir, err)
		}
		slog.Info("state: rolling forward an interrupted restore", "dir", tokenDir, "steps", len(j.Steps))
		if err := s.runRestoreJournal(tokenDir, j); err != nil {
			return err
		}
		if err := s.finishRestoreToken(tokenDir); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) prefixesPendingPath() string {
	return filepath.Join(s.restoreRootDir(), prefixesPendingName)
}

type prefixesPendingFile struct {
	IDs []string `json:"ids"`
}

func (s *Store) readPrefixesPending() ([]string, error) {
	raw, err := s.root.readFile(s.prefixesPendingPath())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var f prefixesPendingFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", s.prefixesPendingPath(), err)
	}
	return f.IDs, nil
}

// savePrefixesPending replaces prefixes-pending.json with ids (atomically,
// then the folder fsynced). The caller holds s.mu.
func (s *Store) savePrefixesPending(ids []string) error {
	data, err := json.Marshal(prefixesPendingFile{IDs: ids})
	if err != nil {
		return err
	}
	if err := s.root.mkdirAll(s.restoreRootDir(), 0o700); err != nil {
		return err
	}
	if err := s.root.atomicWrite(s.prefixesPendingPath(), data, 0o600); err != nil {
		return err
	}
	return syncDir(s.root, s.restoreRootDir())
}

// writePrefixesPending adds ids to prefixes-pending.json (atomically, then the
// folder fsynced), keeping any ids an earlier restore left there unresolved.
func (s *Store) writePrefixesPending(ids []string) error {
	have, err := s.readPrefixesPending()
	if err != nil {
		return err
	}
	all := append(slices.Clone(have), ids...)
	slices.Sort(all)
	all = slices.Compact(all)
	return s.savePrefixesPending(all)
}

// PrefixesPending lists the competitions a restore put in place whose number
// prefix collides with one the restore left in place, and that still wait for
// a new prefix (the caller's step 9). Empty when there are none.
func (s *Store) PrefixesPending() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.readPrefixesPending()
}

// RemovePrefixesPending drops the given ids from prefixes-pending.json once the
// caller has resolved them. If no ids remain, the file is removed; otherwise it
// is rewritten atomically. A missing file is not an error.
func (s *Store) RemovePrefixesPending(ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	have, err := s.readPrefixesPending()
	if err != nil {
		return err
	}
	if len(have) == 0 {
		return nil
	}
	toDrop := make(map[string]bool, len(ids))
	for _, id := range ids {
		toDrop[id] = true
	}
	var keep []string
	for _, id := range have {
		if !toDrop[id] {
			keep = append(keep, id)
		}
	}
	if len(keep) == 0 {
		if err := s.root.remove(s.prefixesPendingPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return syncDir(s.root, s.restoreRootDir())
	}
	return s.savePrefixesPending(keep)
}
