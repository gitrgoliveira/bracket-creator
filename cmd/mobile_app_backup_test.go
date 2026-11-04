package cmd

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testPasswordRegex = regexp.MustCompile(`(?m)^password:.*\n`)

// testDataFiles reads all tournament data files, ignoring store internal directories and password.
func testDataFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := make(map[string]string)
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		slash := filepath.ToSlash(rel)
		if slash == "." {
			return nil
		}
		if backup.IsExcludedPath(slash) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || backup.IsTempFileName(d.Name()) {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		if slash == "tournament.md" {
			b = testPasswordRegex.ReplaceAll(b, nil)
		}
		out[slash] = string(b)
		return nil
	})
	require.NoError(t, err)
	return out
}

func executeMobileApp(args ...string) (string, error) {
	cmd := newMobileAppCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// buildTestPopulatedFolder creates a small populated tournament with one competition, a draw, and one recorded match.
func buildTestPopulatedFolder(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)

	const originalPassword = "initial-pass"
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Kendo Taikai",
		Date:         "04-11-2026",
		Venue:        "Budokan",
		Courts:       []string{"A"},
		Password:     originalPassword,
		DurationDays: 1,
		Mode:         state.TournamentModeOfficiated,
	}))

	compID := "open-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           compID,
		Name:         "Open Championship",
		Kind:         "individual",
		Format:       "league",
		PoolSize:     4,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
		NumberPrefix: "O",
	}))

	players := []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice Smith", Dojo: "North Dojo"},
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob Jones", Dojo: "South Dojo"},
		{ID: "33333333-3333-4333-8333-333333333333", Name: "Charlie Brown", Dojo: "East Dojo"},
		{ID: "44444444-4444-4444-8444-444444444444", Name: "David White", Dojo: "West Dojo"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	eng := engine.New(store)
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	first := matches[0]
	require.NoError(t, eng.RecordMatchResult(compID, first.ID, &state.MatchResult{
		ID:         first.ID,
		SideA:      first.SideA,
		SideB:      first.SideB,
		Winner:     first.SideA,
		WinnerSide: "A",
		Status:     state.MatchStatusCompleted,
		IpponsA:    []string{"M"},
	}))

	return dir, originalPassword
}

// (1) Round trip: backup a populated folder to a zip, restore it with --password pw into a brand-new empty folder,
// then verify the restored competition files equal the source and that the restored tournament.md password is "pw".
func TestMobileAppBackupRestore_RoundTrip(t *testing.T) {
	srcDir, _ := buildTestPopulatedFolder(t)
	zipPath := filepath.Join(t.TempDir(), "backup.zip")

	_, err := executeMobileApp("backup", "--folder", srcDir, "--out", zipPath)
	require.NoError(t, err)
	require.FileExists(t, zipPath)

	targetDir := t.TempDir()
	const newPassword = "restored-pass"
	out, err := executeMobileApp("restore", "--folder", targetDir, "--password", newPassword, zipPath)
	require.NoError(t, err, "output: %s", out)

	srcFiles := testDataFiles(t, srcDir)
	restoredFiles := testDataFiles(t, targetDir)

	for k, srcContent := range srcFiles {
		restoredContent, ok := restoredFiles[k]
		assert.True(t, ok, "missing restored file %s", k)
		assert.Equal(t, srcContent, restoredContent, "file content mismatch for %s", k)
	}

	reopenedStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	tourn, err := reopenedStore.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)
	assert.Equal(t, newPassword, tourn.Password)
}

// (2) A clash (competition id already in the target folder) is refused without --replace or --copy, nothing changed;
// and succeeds with --replace; and with --copy ID=NAME creates the copy.
func TestMobileAppBackupRestore_ClashHandling(t *testing.T) {
	srcDir, _ := buildTestPopulatedFolder(t)
	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	_, err := executeMobileApp("backup", "--folder", srcDir, "--out", zipPath)
	require.NoError(t, err)

	// Target folder already has comp1
	targetDir := t.TempDir()
	targetStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	require.NoError(t, targetStore.SaveTournament(&state.Tournament{
		Name:     "Existing Taikai",
		Password: "pass",
		Courts:   []string{"A"},
	}))
	require.NoError(t, targetStore.SaveCompetition(&state.Competition{
		ID:           "open-comp",
		Name:         "Existing Open",
		Courts:       []string{"A"},
		NumberPrefix: "E",
	}))

	beforeFiles := testDataFiles(t, targetDir)

	// Refused without choice
	out, err := executeMobileApp("restore", "--folder", targetDir, zipPath)
	require.Error(t, err)
	assert.Contains(t, out+err.Error(), "open-comp")

	afterRefusalFiles := testDataFiles(t, targetDir)
	assert.Equal(t, beforeFiles, afterRefusalFiles, "refusal must change nothing in target folder")

	// Succeeds with --copy ID=NAME
	out, err = executeMobileApp("restore", "--folder", targetDir, "--copy", "open-comp=Copied Open", zipPath)
	require.NoError(t, err, "output: %s", out)

	reopenedStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	comps, err := reopenedStore.ListCompetitions()
	require.NoError(t, err)
	assert.Contains(t, comps, "open-comp")
	assert.Contains(t, comps, "copied-open")

	// Succeeds with --replace
	out, err = executeMobileApp("restore", "--folder", targetDir, "--replace", "open-comp", "--keep", "copied-open", zipPath)
	require.NoError(t, err, "output: %s", out)

	replacedStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	comp, err := replacedStore.LoadCompetition("open-comp")
	require.NoError(t, err)
	assert.Equal(t, "Open Championship", comp.Name)
}

// (3) A competition in the folder but not in a tournament archive is refused without --keep/--remove, listing it,
// and works with each flag.
func TestMobileAppBackupRestore_MissingInArchiveChoice(t *testing.T) {
	srcDir, _ := buildTestPopulatedFolder(t)
	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	_, err := executeMobileApp("backup", "--folder", srcDir, "--out", zipPath)
	require.NoError(t, err)

	// Target folder has extra-comp not in backup
	targetDir := t.TempDir()
	targetStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	require.NoError(t, targetStore.SaveTournament(&state.Tournament{
		Name:     "Target Taikai",
		Password: "pass",
		Courts:   []string{"A"},
	}))
	require.NoError(t, targetStore.SaveCompetition(&state.Competition{
		ID:           "extra-comp",
		Name:         "Extra Local Competition",
		Courts:       []string{"A"},
		NumberPrefix: "X",
	}))

	// Refused without --keep or --remove, listing it
	out, err := executeMobileApp("restore", "--folder", targetDir, zipPath)
	require.Error(t, err)
	assert.Contains(t, out+err.Error(), "extra-comp")

	// Works with --keep
	out, err = executeMobileApp("restore", "--folder", targetDir, "--keep", "extra-comp", zipPath)
	require.NoError(t, err, "output: %s", out)
	reopenedStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	comps, err := reopenedStore.ListCompetitions()
	require.NoError(t, err)
	assert.Contains(t, comps, "extra-comp")
	assert.Contains(t, comps, "open-comp")

	// Works with --remove (and replacing open-comp)
	out, err = executeMobileApp("restore", "--folder", targetDir, "--replace", "open-comp", "--remove", "extra-comp", zipPath)
	require.NoError(t, err, "output: %s", out)
	reopenedStore, err = state.NewStore(targetDir)
	require.NoError(t, err)
	comps, err = reopenedStore.ListCompetitions()
	require.NoError(t, err)
	assert.NotContains(t, comps, "extra-comp")
	assert.Contains(t, comps, "open-comp")
}

// (4) A fresh folder (no tournament.md) is refused without --password.
func TestMobileAppBackupRestore_FreshFolderRequiresPassword(t *testing.T) {
	srcDir, _ := buildTestPopulatedFolder(t)
	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	_, err := executeMobileApp("backup", "--folder", srcDir, "--out", zipPath)
	require.NoError(t, err)

	targetDir := t.TempDir()
	out, err := executeMobileApp("restore", "--folder", targetDir, zipPath)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(out+err.Error()), "password")
}

// (5) Backup of a folder with no tournament.md is refused with "no tournament in this folder" and the folder is
// still empty afterwards (and the --out file does not exist).
func TestMobileAppBackup_EmptyFolderRefused(t *testing.T) {
	emptyDir := t.TempDir()
	outZip := filepath.Join(t.TempDir(), "out.zip")

	out, err := executeMobileApp("backup", "--folder", emptyDir, "--out", outZip)
	require.Error(t, err)
	assert.Contains(t, out+err.Error(), "no tournament in this folder")

	entries, rerr := os.ReadDir(emptyDir)
	require.NoError(t, rerr)
	assert.Empty(t, entries, "folder must remain completely empty")
	assert.NoFileExists(t, outZip, "output zip must not be created")
}

// (6) A bad --copy (no "=", empty id, empty name) is refused.
func TestMobileAppBackupRestore_BadCopyFlagRefused(t *testing.T) {
	srcDir, _ := buildTestPopulatedFolder(t)
	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	_, err := executeMobileApp("backup", "--folder", srcDir, "--out", zipPath)
	require.NoError(t, err)

	targetDir := t.TempDir()

	// No "="
	_, err = executeMobileApp("restore", "--folder", targetDir, "--copy", "open-comp", zipPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open-comp")

	// Empty id
	_, err = executeMobileApp("restore", "--folder", targetDir, "--copy", "=NewName", zipPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "=NewName")

	// Empty name
	_, err = executeMobileApp("restore", "--folder", targetDir, "--copy", "open-comp=", zipPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open-comp=")
}

// (7) Restoring a competition-scope archive into a folder keeps the folder password and needs no --password.
func TestMobileAppBackupRestore_CompetitionScopeKeepsPassword(t *testing.T) {
	srcDir, _ := buildTestPopulatedFolder(t)
	compZip := filepath.Join(t.TempDir(), "comp.zip")

	_, err := executeMobileApp("backup", "--folder", srcDir, "--competition", "open-comp", "--out", compZip)
	require.NoError(t, err)
	require.FileExists(t, compZip)

	targetDir := t.TempDir()
	targetStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	const targetPassword = "keep-this-password"
	require.NoError(t, targetStore.SaveTournament(&state.Tournament{
		Name:     "Target Taikai",
		Password: targetPassword,
		Courts:   []string{"A"},
	}))

	out, err := executeMobileApp("restore", "--folder", targetDir, compZip)
	require.NoError(t, err, "output: %s", out)

	reopenedStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	tourn, err := reopenedStore.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, targetPassword, tourn.Password)

	comps, err := reopenedStore.ListCompetitions()
	require.NoError(t, err)
	assert.Contains(t, comps, "open-comp")
}

// (8) --undo of a restore that created a competition removes it (restore a tournament archive that creates a
// competition with the safety copy, then restore the before-restore zip from <folder>/backups/ with --undo,
// and the created competition is gone); --undo on a manual backup file is refused.
func TestMobileAppBackupRestore_Undo(t *testing.T) {
	srcDir, _ := buildTestPopulatedFolder(t)
	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	_, err := executeMobileApp("backup", "--folder", srcDir, "--out", zipPath)
	require.NoError(t, err)

	targetDir := t.TempDir()
	targetStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	require.NoError(t, targetStore.SaveTournament(&state.Tournament{
		Name:     "Target Taikai",
		Password: "pass",
		Courts:   []string{"A"},
	}))

	// --undo on a manual backup file is refused
	out, err := executeMobileApp("restore", "--folder", targetDir, "--undo", zipPath)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(out+err.Error()), "undo")

	// Restore that creates open-comp
	out, err = executeMobileApp("restore", "--folder", targetDir, zipPath)
	require.NoError(t, err, "output: %s", out)

	reopenedStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	comps, err := reopenedStore.ListCompetitions()
	require.NoError(t, err)
	require.Contains(t, comps, "open-comp")

	// Safety copy must exist in <targetDir>/backups/
	backups, err := reopenedStore.ListBackupFiles()
	require.NoError(t, err)
	require.NotEmpty(t, backups)
	var safetyFile string
	for _, b := range backups {
		if b.Kind == state.BackupKindBeforeRestore {
			safetyFile = filepath.Join(targetDir, "backups", b.Name)
			break
		}
	}
	require.NotEmpty(t, safetyFile, "must find a before-restore safety copy")

	// Execute --undo on the safety copy
	out, err = executeMobileApp("restore", "--folder", targetDir, "--undo", safetyFile)
	require.NoError(t, err, "output: %s", out)

	reopenedStore, err = state.NewStore(targetDir)
	require.NoError(t, err)
	comps, err = reopenedStore.ListCompetitions()
	require.NoError(t, err)
	assert.NotContains(t, comps, "open-comp", "created competition should be removed by undo")
}

// (9) A restore writes a before-restore safety copy file in <folder>/backups/ (match the name pattern
// state.ParseBackupName understands).
func TestMobileAppBackupRestore_SafetyCopyWritten(t *testing.T) {
	srcDir, _ := buildTestPopulatedFolder(t)
	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	_, err := executeMobileApp("backup", "--folder", srcDir, "--out", zipPath)
	require.NoError(t, err)

	targetDir := t.TempDir()
	targetStore, err := state.NewStore(targetDir)
	require.NoError(t, err)
	require.NoError(t, targetStore.SaveTournament(&state.Tournament{
		Name:     "Target Taikai",
		Password: "pass",
		Courts:   []string{"A"},
	}))

	_, err = executeMobileApp("restore", "--folder", targetDir, zipPath)
	require.NoError(t, err)

	backupsDir := filepath.Join(targetDir, "backups")
	entries, err := os.ReadDir(backupsDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	found := false
	for _, entry := range entries {
		bf, perr := state.ParseBackupName(entry.Name())
		if perr == nil && bf.Kind == state.BackupKindBeforeRestore {
			found = true
			break
		}
	}
	assert.True(t, found, "a before-restore safety copy satisfying state.ParseBackupName must be written")
}

// Check subcommands are registered and help mentions stopping the server.
func TestMobileAppBackupRestore_RegistrationAndHelp(t *testing.T) {
	cmd := newMobileAppCmd()
	backupCmd, _, err := cmd.Find([]string{"backup"})
	require.NoError(t, err)
	require.NotNil(t, backupCmd)
	assert.Contains(t, strings.ToLower(backupCmd.Long), "stop the server")

	restoreCmd, _, err := cmd.Find([]string{"restore"})
	require.NoError(t, err)
	require.NotNil(t, restoreCmd)
	assert.Contains(t, strings.ToLower(restoreCmd.Long), "stop the server")
}

// A folder a previous run created with the default tournament (tournament.md
// present, no password) still needs --password for a tournament restore.
func TestMobileAppRestore_DefaultTournamentFolderNeedsPassword(t *testing.T) {
	srcDir, _ := buildTestPopulatedFolder(t)
	zipPath := filepath.Join(t.TempDir(), "backup.zip")
	_, err := executeMobileApp("backup", "--folder", srcDir, "--out", zipPath)
	require.NoError(t, err)

	targetDir := t.TempDir()
	_, err = state.NewStore(targetDir)
	require.NoError(t, err)
	before := testDataFiles(t, targetDir)

	out, err := executeMobileApp("restore", "--folder", targetDir, zipPath)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(out+err.Error()), "password")
	assert.Equal(t, before, testDataFiles(t, targetDir), "a refusal must change nothing")

	_, err = executeMobileApp("restore", "--folder", targetDir, "--password", "pw", zipPath)
	require.NoError(t, err)
}
