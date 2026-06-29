package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteCompetitionFile covers the allowlist validation and the idempotent
// delete path (delete existing file and re-delete a non-existent file).
func TestDeleteCompetitionFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "del-file-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Del"}))

	t.Run("invalid comp id", func(t *testing.T) {
		err := store.DeleteCompetitionFile("../etc", "bracket.json")
		assert.Error(t, err)
	})

	t.Run("empty filename rejected", func(t *testing.T) {
		err := store.DeleteCompetitionFile(compID, "")
		assert.Error(t, err)
	})

	t.Run("path traversal rejected", func(t *testing.T) {
		err := store.DeleteCompetitionFile(compID, "../tournament.md")
		assert.Error(t, err)
	})

	t.Run("filename not in allowlist", func(t *testing.T) {
		err := store.DeleteCompetitionFile(compID, "participants.csv")
		assert.Error(t, err)
	})

	t.Run("idempotent delete of non-existent file", func(t *testing.T) {
		// bracket.json does not exist yet; should succeed (nil) per idempotent spec.
		err := store.DeleteCompetitionFile(compID, "bracket.json")
		assert.NoError(t, err)
	})

	t.Run("delete existing allowed file", func(t *testing.T) {
		// Create the file in the competition directory then delete it.
		compDir := filepath.Join(dir, "competitions", compID)
		require.NoError(t, os.MkdirAll(compDir, 0o700))
		fpath := filepath.Join(compDir, "pools.csv")
		require.NoError(t, os.WriteFile(fpath, []byte("data"), 0o600))

		err := store.DeleteCompetitionFile(compID, "pools.csv")
		assert.NoError(t, err)
		_, statErr := os.Stat(fpath)
		assert.True(t, os.IsNotExist(statErr), "file should be gone after deletion")
	})
}

// TestBulkCheckIn covers the happy path, deduplication, already-checked-in
// counting, and unknown-pid reporting.
func TestBulkCheckIn(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "bulk-ci-comp"
	require.NoError(t, store.SaveCompetition(&Competition{
		ID:   compID,
		Name: "Bulk Check-in",
		Kind: "individual",
	}))

	// IDs must be UUID v4 so the CSV parser recognises them as the UUID
	// column and round-trips them correctly; short IDs like "p1" are parsed
	// as the legacy (no-UUID) format, leaving players[i].ID empty and
	// causing BulkCheckIn to miss every lookup.
	const (
		pid1 = "a1b2c3d4-0000-4000-8000-000000000001"
		pid2 = "a1b2c3d4-0000-4000-8000-000000000002"
		pid3 = "a1b2c3d4-0000-4000-8000-000000000003"
	)
	players := []domain.Player{
		{ID: pid1, Name: "Alice", Dojo: "DojoA"},
		{ID: pid2, Name: "Bob", Dojo: "DojoB"},
		{ID: pid3, Name: "Carol", Dojo: "DojoC"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	t.Run("check in two players", func(t *testing.T) {
		result, err := store.BulkCheckIn(compID, []string{pid1, pid2})
		require.NoError(t, err)
		assert.Equal(t, 2, result.CheckedIn)
		assert.Equal(t, 0, result.AlreadyCheckedIn)
		assert.Empty(t, result.NotFound)
	})

	t.Run("already checked-in counted separately", func(t *testing.T) {
		result, err := store.BulkCheckIn(compID, []string{pid1, pid3})
		require.NoError(t, err)
		assert.Equal(t, 1, result.CheckedIn, "pid3 is new")
		assert.Equal(t, 1, result.AlreadyCheckedIn, "pid1 was already checked in")
		assert.Empty(t, result.NotFound)
	})

	t.Run("unknown pids reported", func(t *testing.T) {
		unknown := "ffffffff-0000-4000-8000-000000000099"
		result, err := store.BulkCheckIn(compID, []string{unknown, pid2})
		require.NoError(t, err)
		assert.Equal(t, 0, result.CheckedIn)
		assert.Equal(t, 1, result.AlreadyCheckedIn)
		assert.Equal(t, []string{unknown}, result.NotFound)
	})

	t.Run("duplicate pids in request deduplicated", func(t *testing.T) {
		result, err := store.BulkCheckIn(compID, []string{pid1, pid1, pid1})
		require.NoError(t, err)
		assert.Equal(t, 0, result.CheckedIn)
		assert.Equal(t, 1, result.AlreadyCheckedIn, "deduplicated to one entry")
	})

	t.Run("empty pids list is a no-op", func(t *testing.T) {
		result, err := store.BulkCheckIn(compID, []string{})
		require.NoError(t, err)
		assert.Equal(t, 0, result.CheckedIn)
		assert.Equal(t, 0, result.AlreadyCheckedIn)
	})
}
