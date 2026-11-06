package state

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// BackupKind identifies the reason a backup archive was written to the backups folder.
type BackupKind string

const (
	// BackupKindScheduled names a periodic full-tournament backup copy.
	BackupKindScheduled BackupKind = "scheduled"
	// BackupKindBeforeRestore names a safety copy written immediately before a restore.
	BackupKindBeforeRestore BackupKind = "before-restore"
)

// BackupStampLayout is the UTC timestamp layout used in backup archive filenames.
const BackupStampLayout = "20060102T150405Z"

// ErrInvalidBackupName is returned when a backup filename does not conform to the accepted pattern.
var ErrInvalidBackupName = errors.New("invalid backup file name")

// ErrBackupNotFound is returned when a requested backup archive does not exist as a regular file.
var ErrBackupNotFound = errors.New("backup file not found")

var backupNamePattern = regexp.MustCompile(`^\d{8}T\d{6}Z-(scheduled|before-restore-(tournament|competition))(-[2-9]|-[1-9]\d{1,3})?\.zip$`)

// BackupFile describes a backup zip file stored in the backups directory.
type BackupFile struct {
	Name   string
	Kind   BackupKind
	Scope  SnapshotScope
	Time   time.Time
	Size   int64
	Suffix int
}

// ValidBackupName reports whether name is an accepted backup filename: the
// exact pattern, with a real UTC timestamp. It is the one gate list, open and
// delete share, so a name that is hidden from the list cannot be opened either.
func ValidBackupName(name string) bool {
	_, err := ParseBackupName(name)
	return err == nil
}

// ParseBackupName validates name and extracts its kind, scope, UTC timestamp, and numeric suffix.
func ParseBackupName(name string) (BackupFile, error) {
	if !backupNamePattern.MatchString(name) {
		return BackupFile{}, fmt.Errorf("%w: %q", ErrInvalidBackupName, name)
	}
	sub := backupNamePattern.FindStringSubmatch(name)
	stamp := name[:len(BackupStampLayout)]
	tm, err := time.Parse(BackupStampLayout, stamp)
	if err != nil {
		return BackupFile{}, fmt.Errorf("%w: %q: %w", ErrInvalidBackupName, name, err)
	}

	var kind BackupKind
	var scope SnapshotScope
	if sub[1] == string(BackupKindScheduled) {
		kind = BackupKindScheduled
		scope = ScopeTournament
	} else {
		kind = BackupKindBeforeRestore
		scope = sub[2]
	}

	suffix := 1
	if sub[3] != "" {
		n, serr := strconv.Atoi(sub[3][1:])
		if serr != nil {
			return BackupFile{}, fmt.Errorf("%w: %q: %w", ErrInvalidBackupName, name, serr)
		}
		suffix = n
	}

	return BackupFile{
		Name:   name,
		Kind:   kind,
		Scope:  scope,
		Time:   tm.UTC(),
		Suffix: suffix,
	}, nil
}

// BackupFileName constructs a backup filename for kind, scope, UTC time at, and collision suffix n.
func BackupFileName(kind BackupKind, scope SnapshotScope, at time.Time, n int) (string, error) {
	if scope != ScopeTournament && scope != ScopeCompetition {
		return "", fmt.Errorf("%w: invalid scope %q", ErrInvalidBackupName, scope)
	}
	var middle string
	switch kind {
	case BackupKindScheduled:
		if scope != ScopeTournament {
			return "", fmt.Errorf("%w: scheduled backup requires tournament scope, got %q", ErrInvalidBackupName, scope)
		}
		middle = string(BackupKindScheduled)
	case BackupKindBeforeRestore:
		middle = string(BackupKindBeforeRestore) + "-" + scope
	default:
		return "", fmt.Errorf("%w: unknown backup kind %q", ErrInvalidBackupName, kind)
	}
	if n > 9999 {
		return "", fmt.Errorf("%w: suffix %d exceeds 9999", ErrInvalidBackupName, n)
	}

	stamp := at.UTC().Format(BackupStampLayout)
	var name string
	if n <= 1 {
		name = fmt.Sprintf("%s-%s.zip", stamp, middle)
	} else {
		name = fmt.Sprintf("%s-%s-%d.zip", stamp, middle, n)
	}
	if !ValidBackupName(name) {
		return "", fmt.Errorf("%w: %q", ErrInvalidBackupName, name)
	}
	return name, nil
}

// SaveBackupFile writes data to a new uniquely named file in backups/ and returns the chosen filename.
func (s *Store) SaveBackupFile(kind BackupKind, scope SnapshotScope, at time.Time, data []byte) (string, error) {
	if _, err := BackupFileName(kind, scope, at, 1); err != nil {
		return "", err
	}

	s.backupSaveMu.Lock()
	defer s.backupSaveMu.Unlock()

	backupsDir := filepath.Join(s.folder, BackupsDirName)
	if err := s.root.mkdirAll(backupsDir, 0o700); err != nil {
		return "", err
	}

	for n := 1; ; n++ {
		name, err := BackupFileName(kind, scope, at, n)
		if err != nil {
			return "", err
		}
		target := filepath.Join(backupsDir, name)
		_, statErr := s.root.stat(target)
		if statErr == nil {
			continue
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", statErr
		}
		if err := s.root.atomicWrite(target, data, 0o600); err != nil {
			return "", err
		}
		return name, nil
	}
}

// ListBackupFiles returns the valid regular backup files in backups/, sorted newest first.
func (s *Store) ListBackupFiles() ([]BackupFile, error) {
	backupsDir := filepath.Join(s.folder, BackupsDirName)
	entries, err := s.root.readDir(backupsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []BackupFile{}, nil
		}
		return nil, err
	}

	files := []BackupFile{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		bf, perr := ParseBackupName(entry.Name())
		if perr != nil {
			continue
		}
		info, ierr := entry.Info()
		if ierr != nil {
			if errors.Is(ierr, fs.ErrNotExist) {
				continue
			}
			return nil, ierr
		}
		if !info.Mode().IsRegular() {
			continue
		}
		bf.Size = info.Size()
		files = append(files, bf)
	}

	slices.SortFunc(files, func(a, b BackupFile) int {
		if !a.Time.Equal(b.Time) {
			if a.Time.After(b.Time) {
				return -1
			}
			return 1
		}
		if a.Suffix != b.Suffix {
			return cmp.Compare(b.Suffix, a.Suffix)
		}
		return cmp.Compare(a.Name, b.Name)
	})

	return files, nil
}

// OpenBackupFile opens a regular backup file in backups/ by name for reading.
func (s *Store) OpenBackupFile(name string) (*os.File, error) {
	if !ValidBackupName(name) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidBackupName, name)
	}
	path := filepath.Join(s.folder, BackupsDirName, name)
	f, err := s.root.open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %q: %w", ErrBackupNotFound, name, err)
		}
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if !info.Mode().IsRegular() {
		notRegErr := fmt.Errorf("%w: %q is not a regular file", ErrBackupNotFound, name)
		if cerr := f.Close(); cerr != nil {
			return nil, errors.Join(notRegErr, cerr)
		}
		return nil, notRegErr
	}
	return f, nil
}

// DeleteBackupFile removes a single regular backup file in backups/ by name.
func (s *Store) DeleteBackupFile(name string) error {
	if !ValidBackupName(name) {
		return fmt.Errorf("%w: %q", ErrInvalidBackupName, name)
	}
	path := filepath.Join(s.folder, BackupsDirName, name)
	info, err := s.root.stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %q: %w", ErrBackupNotFound, name, err)
		}
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %q is not a regular file", ErrBackupNotFound, name)
	}
	if err := s.root.remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%w: %q: %w", ErrBackupNotFound, name, err)
		}
		return err
	}
	return nil
}
