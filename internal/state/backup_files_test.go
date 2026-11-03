package state

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackupFiles_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	at := time.Date(2026, 11, 3, 12, 34, 56, 789000000, time.UTC)
	schedBytes := []byte("PK-scheduled-archive-payload")
	compBytes := []byte("PK-before-restore-competition-payload-longer")

	schedName, err := store.SaveBackupFile(BackupKindScheduled, ScopeTournament, at, schedBytes)
	require.NoError(t, err)
	assert.Equal(t, "20261103T123456Z-scheduled.zip", schedName)

	compName, err := store.SaveBackupFile(BackupKindBeforeRestore, ScopeCompetition, at, compBytes)
	require.NoError(t, err)
	assert.Equal(t, "20261103T123456Z-before-restore-competition.zip", compName)

	fSched, err := store.OpenBackupFile(schedName)
	require.NoError(t, err)
	gotSched, err := io.ReadAll(fSched)
	require.NoError(t, err)
	require.NoError(t, fSched.Close())
	assert.Equal(t, schedBytes, gotSched)

	fComp, err := store.OpenBackupFile(compName)
	require.NoError(t, err)
	gotComp, err := io.ReadAll(fComp)
	require.NoError(t, err)
	require.NoError(t, fComp.Close())
	assert.Equal(t, compBytes, gotComp)

	list, err := store.ListBackupFiles()
	require.NoError(t, err)
	wantTime := at.UTC().Truncate(time.Second)
	assert.Equal(t, []BackupFile{
		{
			Name:   compName,
			Kind:   BackupKindBeforeRestore,
			Scope:  ScopeCompetition,
			Time:   wantTime,
			Size:   int64(len(compBytes)),
			Suffix: 1,
		},
		{
			Name:   schedName,
			Kind:   BackupKindScheduled,
			Scope:  ScopeTournament,
			Time:   wantTime,
			Size:   int64(len(schedBytes)),
			Suffix: 1,
		},
	}, list)

	entries, err := os.ReadDir(filepath.Join(dir, BackupsDirName))
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, strings.Contains(entry.Name(), ".tmp-"), "unexpected leftover temp file %s", entry.Name())
	}
}

func TestBackupFiles_SameSecondClashAndConcurrentSaves(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	firstBytes := []byte("first-save-bytes")
	secondBytes := []byte("second-save-bytes")
	thirdBytes := []byte("third-save-bytes")

	n1, err := store.SaveBackupFile(BackupKindScheduled, ScopeTournament, at, firstBytes)
	require.NoError(t, err)
	assert.Equal(t, "20261103T120000Z-scheduled.zip", n1)

	n2, err := store.SaveBackupFile(BackupKindScheduled, ScopeTournament, at, secondBytes)
	require.NoError(t, err)
	assert.Equal(t, "20261103T120000Z-scheduled-2.zip", n2)

	n3, err := store.SaveBackupFile(BackupKindScheduled, ScopeTournament, at, thirdBytes)
	require.NoError(t, err)
	assert.Equal(t, "20261103T120000Z-scheduled-3.zip", n3)

	f1, err := store.OpenBackupFile(n1)
	require.NoError(t, err)
	gotFirst, err := io.ReadAll(f1)
	require.NoError(t, err)
	require.NoError(t, f1.Close())
	assert.Equal(t, firstBytes, gotFirst)

	// Concurrent saves in the same second must all succeed with distinct names.
	concurrentAt := time.Date(2026, 11, 3, 13, 0, 0, 0, time.UTC)
	const workers = 8
	names := make([]string, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(idx int) {
			defer wg.Done()
			names[idx], errs[idx] = store.SaveBackupFile(
				BackupKindBeforeRestore,
				ScopeTournament,
				concurrentAt,
				[]byte("concurrent-payload"),
			)
		}(i)
	}
	wg.Wait()

	seen := make(map[string]bool, workers)
	for i := 0; i < workers; i++ {
		require.NoError(t, errs[i])
		assert.False(t, seen[names[i]], "duplicate name %s", names[i])
		seen[names[i]] = true
	}
	assert.Len(t, seen, workers)
}

func TestBackupFiles_ListOrder(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	backupsDir := filepath.Join(dir, BackupsDirName)
	require.NoError(t, os.MkdirAll(backupsDir, 0o700))

	fixtureNames := []string{
		"20261103T120000Z-scheduled.zip",
		"20261103T120000Z-scheduled-2.zip",
		"20261103T120000Z-scheduled-9.zip",
		"20261103T120000Z-scheduled-10.zip",
		"20261103T120001Z-before-restore-tournament.zip",
		"20261102T235959Z-scheduled.zip",
	}
	for _, name := range fixtureNames {
		require.NoError(t, os.WriteFile(filepath.Join(backupsDir, name), []byte("zip-bytes"), 0o600))
	}

	list, err := store.ListBackupFiles()
	require.NoError(t, err)

	gotNames := make([]string, len(list))
	for i, bf := range list {
		gotNames[i] = bf.Name
	}

	assert.Equal(t, []string{
		"20261103T120001Z-before-restore-tournament.zip",
		"20261103T120000Z-scheduled-10.zip",
		"20261103T120000Z-scheduled-9.zip",
		"20261103T120000Z-scheduled-2.zip",
		"20261103T120000Z-scheduled.zip",
		"20261102T235959Z-scheduled.zip",
	}, gotNames)
}

func TestBackupFiles_ListIgnoresNonPatternAndNonRegularEntries(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	backupsDir := filepath.Join(dir, BackupsDirName)
	require.NoError(t, os.MkdirAll(backupsDir, 0o700))

	require.NoError(t, os.WriteFile(filepath.Join(backupsDir, "notes.txt"), []byte("note"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(backupsDir, "20261103T120000Z-scheduled.ZIP"), []byte("upper"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(backupsDir, "20261103T120000Z-scheduled.zip.tmp-1-2"), []byte("tmp"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(backupsDir, "20261103T120000Z-before-restore-tournament.zip"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(backupsDir, "20261399T000000Z-scheduled.zip"), []byte("bad-date"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(backupsDir, "20261103T120000Z-scheduled.zip"), []byte("valid"), 0o600))

	list, err := store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "20261103T120000Z-scheduled.zip", list[0].Name)
}

func TestBackupFiles_MissingBackupsDirListsEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	list, err := store.ListBackupFiles()
	require.NoError(t, err)
	require.NotNil(t, list)
	assert.Empty(t, list)
}

func TestBackupFiles_NameValidationAndRefusal(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveTournament(&Tournament{
		Name:   "Protected Tournament",
		Date:   "03-11-2026",
		Venue:  "Budokan",
		Courts: []string{"A"},
	}))
	tournPath := filepath.Join(dir, "tournament.md")
	beforeBytes, err := os.ReadFile(tournPath)
	require.NoError(t, err)

	invalidNames := []struct {
		label string
		name  string
	}{
		{"dot-dot", ".."},
		{"parent traversal", "../tournament.md"},
		{"forward slash", "a/b.zip"},
		{"trailing slash", "20261103T120000Z-scheduled.zip/"},
		{"backslash", `20261103T120000Z\scheduled.zip`},
		{"nul byte", "20261103T120000Z-scheduled.zip\x00"},
		{"trailing space", "20261103T120000Z-scheduled.zip "},
		{"upper-case extension", "20261103T120000Z-scheduled.ZIP"},
		{"unknown kind", "20261103T120000Z-unknown.zip"},
		{"unknown scope", "20261103T120000Z-before-restore-team.zip"},
		{"suffix -1", "20261103T120000Z-scheduled-1.zip"},
		{"suffix -0", "20261103T120000Z-scheduled-0.zip"},
		{"suffix -01", "20261103T120000Z-scheduled-01.zip"},
		{"suffix -10000", "20261103T120000Z-scheduled-10000.zip"},
		{"trailing newline", "20261103T120000Z-scheduled.zip\n"},
		{"leading newline", "\n20261103T120000Z-scheduled.zip"},
		{"empty", ""},
	}

	for _, tc := range invalidNames {
		t.Run("invalid_"+tc.label, func(t *testing.T) {
			assert.False(t, ValidBackupName(tc.name))

			_, perr := ParseBackupName(tc.name)
			assert.ErrorIs(t, perr, ErrInvalidBackupName)

			f, oerr := store.OpenBackupFile(tc.name)
			assert.Nil(t, f)
			assert.ErrorIs(t, oerr, ErrInvalidBackupName)

			derr := store.DeleteBackupFile(tc.name)
			assert.ErrorIs(t, derr, ErrInvalidBackupName)
		})
	}

	// ParseBackupName also refuses an impossible calendar date even when the shape matches.
	_, perr := ParseBackupName("20261399T000000Z-scheduled.zip")
	assert.ErrorIs(t, perr, ErrInvalidBackupName)

	// Verify tournament.md outside backups/ was untouched by any refused DeleteBackupFile call.
	afterBytes, err := os.ReadFile(tournPath)
	require.NoError(t, err)
	assert.Equal(t, beforeBytes, afterBytes)

	validNames := []struct {
		name   string
		kind   BackupKind
		scope  SnapshotScope
		suffix int
	}{
		{"20261103T120000Z-scheduled.zip", BackupKindScheduled, ScopeTournament, 1},
		{"20261103T120000Z-scheduled-2.zip", BackupKindScheduled, ScopeTournament, 2},
		{"20261103T120000Z-scheduled-9.zip", BackupKindScheduled, ScopeTournament, 9},
		{"20261103T120000Z-scheduled-10.zip", BackupKindScheduled, ScopeTournament, 10},
		{"20261103T120000Z-scheduled-9999.zip", BackupKindScheduled, ScopeTournament, 9999},
		{"20261103T120000Z-before-restore-tournament.zip", BackupKindBeforeRestore, ScopeTournament, 1},
		{"20261103T120000Z-before-restore-competition-3.zip", BackupKindBeforeRestore, ScopeCompetition, 3},
	}

	for _, tc := range validNames {
		t.Run("valid_"+tc.name, func(t *testing.T) {
			assert.True(t, ValidBackupName(tc.name))
			bf, err := ParseBackupName(tc.name)
			require.NoError(t, err)
			assert.Equal(t, tc.name, bf.Name)
			assert.Equal(t, tc.kind, bf.Kind)
			assert.Equal(t, tc.scope, bf.Scope)
			assert.Equal(t, tc.suffix, bf.Suffix)
			assert.Equal(t, time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC), bf.Time)
			assert.Equal(t, time.UTC, bf.Time.Location())
			assert.Zero(t, bf.Size)
		})
	}
}

func TestBackupFiles_OpenAndDeleteMissingOrDirectory(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	missing := "20261103T120000Z-scheduled.zip"
	f, err := store.OpenBackupFile(missing)
	assert.Nil(t, f)
	assert.ErrorIs(t, err, ErrBackupNotFound)
	assert.ErrorIs(t, store.DeleteBackupFile(missing), ErrBackupNotFound)

	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	n1, err := store.SaveBackupFile(BackupKindScheduled, ScopeTournament, at, []byte("keep-1"))
	require.NoError(t, err)
	n2, err := store.SaveBackupFile(BackupKindScheduled, ScopeTournament, at, []byte("delete-2"))
	require.NoError(t, err)

	require.NoError(t, store.DeleteBackupFile(n2))
	assert.ErrorIs(t, store.DeleteBackupFile(n2), ErrBackupNotFound)

	list, err := store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, n1, list[0].Name)

	// A directory named like a valid zip is refused with ErrBackupNotFound and not removed.
	dirAsZip := "20261103T120001Z-scheduled.zip"
	dirPath := filepath.Join(dir, BackupsDirName, dirAsZip)
	require.NoError(t, os.MkdirAll(dirPath, 0o700))

	fDir, err := store.OpenBackupFile(dirAsZip)
	assert.Nil(t, fDir)
	assert.ErrorIs(t, err, ErrBackupNotFound)
	assert.ErrorIs(t, store.DeleteBackupFile(dirAsZip), ErrBackupNotFound)

	info, err := os.Stat(dirPath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestBackupFileName_Table(t *testing.T) {
	jst := time.FixedZone("JST", 9*3600)
	atJST := time.Date(2026, 11, 3, 21, 0, 0, 0, jst) // 12:00:00Z

	tests := []struct {
		label   string
		kind    BackupKind
		scope   SnapshotScope
		at      time.Time
		n       int
		want    string
		wantErr bool
	}{
		{
			label: "scheduled n=0 unsuffixed and converted to UTC",
			kind:  BackupKindScheduled,
			scope: ScopeTournament,
			at:    atJST,
			n:     0,
			want:  "20261103T120000Z-scheduled.zip",
		},
		{
			label: "scheduled n=1 unsuffixed",
			kind:  BackupKindScheduled,
			scope: ScopeTournament,
			at:    atJST.UTC(),
			n:     1,
			want:  "20261103T120000Z-scheduled.zip",
		},
		{
			label: "scheduled n=2 suffixed",
			kind:  BackupKindScheduled,
			scope: ScopeTournament,
			at:    atJST.UTC(),
			n:     2,
			want:  "20261103T120000Z-scheduled-2.zip",
		},
		{
			label: "before-restore tournament n=9999",
			kind:  BackupKindBeforeRestore,
			scope: ScopeTournament,
			at:    atJST.UTC(),
			n:     9999,
			want:  "20261103T120000Z-before-restore-tournament-9999.zip",
		},
		{
			label: "before-restore competition n=1",
			kind:  BackupKindBeforeRestore,
			scope: ScopeCompetition,
			at:    atJST.UTC(),
			n:     1,
			want:  "20261103T120000Z-before-restore-competition.zip",
		},
		{
			label:   "scheduled with competition scope refused",
			kind:    BackupKindScheduled,
			scope:   ScopeCompetition,
			at:      atJST.UTC(),
			n:       1,
			wantErr: true,
		},
		{
			label:   "unknown kind refused",
			kind:    BackupKind("manual"),
			scope:   ScopeTournament,
			at:      atJST.UTC(),
			n:       1,
			wantErr: true,
		},
		{
			label:   "unknown scope refused",
			kind:    BackupKindBeforeRestore,
			scope:   "division",
			at:      atJST.UTC(),
			n:       1,
			wantErr: true,
		},
		{
			label:   "n=10000 refused",
			kind:    BackupKindScheduled,
			scope:   ScopeTournament,
			at:      atJST.UTC(),
			n:       10000,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.label, func(t *testing.T) {
			got, err := BackupFileName(tc.kind, tc.scope, tc.at, tc.n)
			if tc.wantErr {
				assert.Empty(t, got)
				assert.True(t, errors.Is(err, ErrInvalidBackupName), "expected ErrInvalidBackupName, got %v", err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestBackupFiles_SymlinkPointingOutsideDataFolderRefusedAndUnlisted(t *testing.T) {
	dir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.zip")
	require.NoError(t, os.WriteFile(outsideFile, []byte("outside-secret"), 0o600))

	store, err := NewStore(dir)
	require.NoError(t, err)

	backupsDir := filepath.Join(dir, BackupsDirName)
	require.NoError(t, os.MkdirAll(backupsDir, 0o700))

	linkName := "20261103T120000Z-scheduled.zip"
	linkPath := filepath.Join(backupsDir, linkName)
	if err := os.Symlink(outsideFile, linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	f, err := store.OpenBackupFile(linkName)
	if f != nil {
		_ = f.Close()
	}
	require.Error(t, err)

	list, err := store.ListBackupFiles()
	require.NoError(t, err)
	assert.Empty(t, list)
}
