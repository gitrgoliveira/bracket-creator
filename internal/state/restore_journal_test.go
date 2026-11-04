package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errInjectedCrash = errors.New("injected crash")

// journalScenario builds the archive once: a tournament with a competition
// the folder also holds ("men", replaced), one it lacks ("fresh", created) and
// a branding/ folder the target folder lacks.
func journalScenarioArchive(t *testing.T) *backup.Archive {
	t.Helper()
	src, srcDir := newRestoreTestStore(t)
	restoreTestTournament(t, src, "archive-pw", "A")
	addRestoreComp(t, src, "men", "Men", "M", MatchStatusCompleted, "A")
	addRestoreComp(t, src, "fresh", "Fresh", "F", MatchStatusScheduled, "A")
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, BrandingDirName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, BrandingDirName, "logo.png"), []byte("LOGO"), 0o600))
	return archiveOf(t, src, ScopeTournament, "")
}

// journalScenarioFolder builds the folder the archive is restored over:
// "men" (replaced), "gone" (removed) and a sponsors/ folder the archive lacks.
func journalScenarioFolder(t *testing.T) (*Store, string) {
	t.Helper()
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "B")
	addRestoreComp(t, s, "gone", "Gone", "G", MatchStatusScheduled, "B")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, SponsorsDirName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, SponsorsDirName, "s1.png"), []byte("SPONSOR"), 0o600))
	return s, dir
}

var journalScenarioDecisions = map[string]RestoreDecision{
	"men":  {Action: RestoreReplace},
	"gone": {Action: RestoreRemove},
}

func TestRestoreJournal_ACrashAfterEachRenameConvergesOnTheNextStart(t *testing.T) {
	a := journalScenarioArchive(t)

	ref, refDir := journalScenarioFolder(t)
	var steps int
	afterRestoreStep = func(i int, _ restoreStep) error { steps = i + 1; return nil }
	_, err := ref.Restore(RestoreRequest{Archive: a, Decisions: journalScenarioDecisions})
	afterRestoreStep = nil
	require.NoError(t, err)
	want := folderFiles(t, refDir)
	require.Equal(t, 8, steps, "four moves out (gone, men, tournament.md, sponsors) and four in (fresh, men, tournament.md, branding)")
	assert.Equal(t, "LOGO", want["branding/logo.png"])
	assert.NotContains(t, want, "sponsors/s1.png")

	for k := 0; k < steps; k++ {
		s, dir := journalScenarioFolder(t)
		afterRestoreStep = func(i int, _ restoreStep) error {
			if i == k {
				return errInjectedCrash
			}
			return nil
		}
		_, err := s.Restore(RestoreRequest{Archive: a, Decisions: journalScenarioDecisions})
		afterRestoreStep = nil
		require.ErrorIs(t, err, errInjectedCrash, "step %d", k)

		recovered, err := NewStore(dir)
		require.NoError(t, err, "step %d", k)
		assert.Equal(t, want, folderFiles(t, dir), "a crash after step %d converges", k)
		ids, err := recovered.ListCompetitions()
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"fresh", "men"}, ids, "the roll-forward runs before the first competitions scan")
		assertNoStaging(t, dir)

		_, err = NewStore(dir)
		require.NoError(t, err)
		assert.Equal(t, want, folderFiles(t, dir), "running recovery again changes nothing (step %d)", k)
	}
}

func TestRestoreJournal_AStagingFolderWithoutAJournalIsRemoved(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	staged := filepath.Join(dir, backup.RestoreDirName, "deadbeef", restoreNewDir, "competitions", "other")
	require.NoError(t, os.MkdirAll(staged, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(staged, "config.md"), []byte("---\nid: other\n---\n"), 0o600))

	reopened, err := NewStore(dir)
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, backup.RestoreDirName, "deadbeef"))
	assert.True(t, os.IsNotExist(err))
	ids, err := reopened.ListCompetitions()
	require.NoError(t, err)
	assert.Equal(t, []string{"men"}, ids)
}

func TestRestoreJournal_PrefixesPendingSurvivesRecovery(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "K", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeCompetition, "men")
	afterRestoreStep = func(int, restoreStep) error { return errInjectedCrash }
	_, err := s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreCopy, CopyName: "Men B"}}})
	afterRestoreStep = nil
	require.ErrorIs(t, err, errInjectedCrash)

	reopened, err := NewStore(dir)
	require.NoError(t, err)
	pending, err := reopened.PrefixesPending()
	require.NoError(t, err)
	assert.Equal(t, []string{"men-b"}, pending)
	ids, err := reopened.ListCompetitions()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"men", "men-b"}, ids)
	assertNoStaging(t, dir)
}

func TestRestoreJournal_AJournalNamingAPathNoRestoreSwapsStopsStartup(t *testing.T) {
	_, dir := newRestoreTestStore(t)
	tokenDir := filepath.Join(dir, backup.RestoreDirName, "badc0ffee")
	require.NoError(t, os.MkdirAll(tokenDir, 0o700))
	raw, err := json.Marshal(restoreJournal{Format: restoreJournalV1, Steps: []restoreStep{{Kind: restoreMoveOut, Path: ".wal"}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(tokenDir, restoreJournalName), raw, 0o600))

	_, err = NewStore(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not one a restore swaps")
	_, statErr := os.Stat(filepath.Join(dir, ".wal"))
	assert.NoError(t, statErr, "nothing was renamed")
}

func TestRemovePrefixesPending(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	// Missing file: removing anything is fine.
	require.NoError(t, s.RemovePrefixesPending([]string{"a"}))

	// Write pending ids {a, b}
	require.NoError(t, s.writePrefixesPending([]string{"a", "b"}))
	pending, err := s.PrefixesPending()
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, pending)

	// Remove {a}, {b} remains
	require.NoError(t, s.RemovePrefixesPending([]string{"a"}))
	pending, err = s.PrefixesPending()
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, pending)

	// Remove {b} -> file gone
	require.NoError(t, s.RemovePrefixesPending([]string{"b"}))
	pending, err = s.PrefixesPending()
	require.NoError(t, err)
	assert.Empty(t, pending)
	_, err = os.Stat(filepath.Join(dir, backup.RestoreDirName, "prefixes-pending.json"))
	assert.True(t, os.IsNotExist(err))

	// Remove on missing file is fine.
	require.NoError(t, s.RemovePrefixesPending([]string{"b"}))
}

func TestRestore_RetriesTheJournalOnce(t *testing.T) {
	a := journalScenarioArchive(t)
	ref, refDir := journalScenarioFolder(t)
	_, err := ref.Restore(RestoreRequest{Archive: a, Decisions: journalScenarioDecisions})
	require.NoError(t, err)
	want := folderFiles(t, refDir)

	s, dir := journalScenarioFolder(t)
	var calls int
	afterRestoreStep = func(i int, _ restoreStep) error {
		calls++
		if calls == 1 {
			return errors.New("transient rename failure")
		}
		return nil
	}
	defer func() { afterRestoreStep = nil }()

	_, err = s.Restore(RestoreRequest{Archive: a, Decisions: journalScenarioDecisions})
	require.NoError(t, err)
	assert.Equal(t, want, folderFiles(t, dir))
	assertNoStaging(t, dir)
	assert.GreaterOrEqual(t, calls, 2)
}

func TestRestore_FailsWhenTheRetryFailsToo(t *testing.T) {
	a := journalScenarioArchive(t)
	s, _ := journalScenarioFolder(t)

	afterRestoreStep = func(i int, _ restoreStep) error {
		return errors.New("persistent rename failure")
	}
	defer func() { afterRestoreStep = nil }()

	_, err := s.Restore(RestoreRequest{Archive: a, Decisions: journalScenarioDecisions})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restore stopped part way")
}
