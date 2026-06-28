package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeLegacyRoster writes a genuine legacy (UUID-less) participants.csv to a
// fresh competition directory. SaveParticipants can't be used here because
// marshalParticipantsCSV mints a UUID for every empty ID — which is exactly the
// migration that masks the bug. Writing the file directly reproduces a roster
// that was never re-saved through the app, so loadParticipantsNoLock returns
// players with empty IDs (the pre-condition for the check-in resolution bug).
func writeLegacyRoster(t *testing.T, store *Store, compID, csv string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: compID, Kind: "individual"}))
	dir := filepath.Join(store.folder, "competitions", compID)
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "participants.csv"), []byte(csv), 0600))
}

// TestCheckIn_LegacyUUIDlessRoster pins mp-8bjq: check-in must resolve
// participants on a legacy roster whose participants.csv has no UUID column, by
// falling back to the composite "name|dojo" pid the client sends for ID-less
// rows. Without the fallback both the single and bulk paths 404 / NotFound.
func TestCheckIn_LegacyUUIDlessRoster(t *testing.T) {
	t.Run("single check-in resolves by name|dojo and persists", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)
		compID := "legacy-single"
		writeLegacyRoster(t, store, compID, "Alice,DojoA\nBob,DojoB\n")

		updated, err := store.UpdateParticipant(compID, "Alice|DojoA", false, func(p *domain.Player) error {
			p.CheckedIn = true
			return nil
		})
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, "Alice", updated.Name)
		assert.True(t, updated.CheckedIn)

		// Persisted: reload and confirm Alice is checked in, Bob is not.
		loaded, err := store.LoadParticipants(compID, false)
		require.NoError(t, err)
		byName := map[string]bool{}
		for _, p := range loaded {
			byName[p.Name] = p.CheckedIn
		}
		assert.True(t, byName["Alice"], "Alice must be checked in")
		assert.False(t, byName["Bob"], "Bob must remain unchecked")
	})

	t.Run("single check-in is whitespace-tolerant around the delimiter", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)
		compID := "legacy-ws"
		writeLegacyRoster(t, store, compID, "Alice,DojoA\n")

		// Client could send raw fields with surrounding spaces; the server
		// splits before normalizing, so " Alice | DojoA " still resolves.
		updated, err := store.UpdateParticipant(compID, " Alice | DojoA ", false, func(p *domain.Player) error {
			p.CheckedIn = true
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "Alice", updated.Name)
	})

	t.Run("bulk check-in resolves by name|dojo", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)
		compID := "legacy-bulk"
		writeLegacyRoster(t, store, compID, "Alice,DojoA\nBob,DojoB\nCarol,DojoC\n")

		result, err := store.BulkCheckIn(compID, []string{"Alice|DojoA", "Bob|DojoB"})
		require.NoError(t, err)
		assert.Equal(t, 2, result.CheckedIn)
		assert.Empty(t, result.NotFound)
	})

	t.Run("same name at different dojos resolves to the correct row", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)
		compID := "legacy-collision"
		writeLegacyRoster(t, store, compID, "John Smith,Wakaba\nJohn Smith,Tora\n")

		updated, err := store.UpdateParticipant(compID, "John Smith|Tora", false, func(p *domain.Player) error {
			p.CheckedIn = true
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "Tora", updated.Dojo, "must check in the Tora John Smith, not Wakaba")

		loaded, err := store.LoadParticipants(compID, false)
		require.NoError(t, err)
		for _, p := range loaded {
			if p.Dojo == "Wakaba" {
				assert.False(t, p.CheckedIn, "the Wakaba John Smith must remain unchecked")
			}
		}
	})

	t.Run("unknown name|dojo reports NotFound, not an error", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)
		compID := "legacy-unknown"
		writeLegacyRoster(t, store, compID, "Alice,DojoA\n")

		_, err = store.UpdateParticipant(compID, "Ghost|Nowhere", false, func(p *domain.Player) error {
			p.CheckedIn = true
			return nil
		})
		assert.ErrorIs(t, err, ErrParticipantNotFound)

		result, berr := store.BulkCheckIn(compID, []string{"Ghost|Nowhere"})
		require.NoError(t, berr)
		assert.Equal(t, 0, result.CheckedIn)
		assert.Equal(t, []string{"Ghost|Nowhere"}, result.NotFound)
	})
}

// TestResolveParticipantIndex unit-tests the resolver directly across the UUID
// and legacy fallback branches, including the rule that UUID rows are only
// addressable by their id (never by a name|dojo composite).
func TestResolveParticipantIndex(t *testing.T) {
	const uuid = "a1b2c3d4-0000-4000-8000-000000000001"
	players := []domain.Player{
		{ID: uuid, Name: "Uuidy", Dojo: "DojoU"},
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "John Smith", Dojo: "Wakaba"},
		{Name: "John Smith", Dojo: "Tora"},
	}

	tests := []struct {
		name string
		pid  string
		want int
	}{
		{"empty pid", "", -1},
		{"uuid match", uuid, 0},
		{"legacy name|dojo", "Alice|DojoA", 1},
		{"legacy normalizes case + diacritics", "alice|dojoa", 1},
		{"collision picks correct dojo", "John Smith|Tora", 3},
		{"uuid row not addressable by name", "Uuidy|DojoU", -1},
		{"unknown", "Nobody|Nowhere", -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveParticipantIndex(players, tc.pid))
		})
	}
}
