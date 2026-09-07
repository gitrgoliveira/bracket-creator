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
// marshalParticipantsCSV mints a UUID for every empty ID; which is exactly
// the migration that makes this shape hard to reach through the normal save
// path. Writing the file directly reproduces a roster that was never
// re-saved through the app, so loadParticipantsNoLock returns players with
// empty IDs.
func writeLegacyRoster(t *testing.T, store *Store, compID, csv string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: compID, Kind: "individual"}))
	dir := filepath.Join(store.folder, "competitions", compID)
	require.NoError(t, os.MkdirAll(dir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "participants.csv"), []byte(csv), 0600))
}

// TestCheckIn_LegacyUUIDlessRoster_CompositePidNoLongerResolves converts
// mp-8bjq's original pin (check-in resolves a legacy roster's ID-less rows
// via a composite "name|dojo" pid fallback). The operator ruling bc-pnum
// removed that fallback entirely: a participant is a record that carries an
// id field, so it is addressed BY ID ONLY. A composite "name|dojo" string is
// not an id -- it never resolves, on either the single or bulk check-in
// path, even for the exact legacy roster shape (no UUID column at all) the
// fallback used to exist for. The remedy is the same one
// ErrMissingParticipantIDsInDraw already documents for the draw pre-flight:
// save the roster once and ids are minted (marshalParticipantsCSV), then
// check-in addresses the row by that real id.
func TestCheckIn_LegacyUUIDlessRoster_CompositePidNoLongerResolves(t *testing.T) {
	t.Run("single check-in by name|dojo is not found, not misattributed", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)
		compID := "legacy-single"
		writeLegacyRoster(t, store, compID, "Alice,DojoA\nBob,DojoB\n")

		_, err = store.UpdateParticipant(compID, "Alice|DojoA", false, func(p *domain.Player) error {
			p.CheckedIn = true
			return nil
		})
		assert.ErrorIs(t, err, ErrParticipantNotFound,
			"a composite name|dojo pid is not an id and must not resolve, even against the exact legacy row it used to name")

		// Nothing was checked in: the refusal must not silently touch a
		// different row either.
		loaded, err := store.LoadParticipants(compID, false)
		require.NoError(t, err)
		for _, p := range loaded {
			assert.False(t, p.CheckedIn, "%s must remain unchecked", p.Name)
		}
	})

	t.Run("bulk check-in by name|dojo reports every entry NotFound", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)
		compID := "legacy-bulk"
		writeLegacyRoster(t, store, compID, "Alice,DojoA\nBob,DojoB\nCarol,DojoC\n")

		result, err := store.BulkCheckIn(compID, []string{"Alice|DojoA", "Bob|DojoB"})
		require.NoError(t, err)
		assert.Equal(t, 0, result.CheckedIn)
		assert.ElementsMatch(t, []string{"Alice|DojoA", "Bob|DojoB"}, result.NotFound)
	})

	t.Run("same-name-different-dojo pid still resolves to nothing, never the wrong row", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)
		compID := "legacy-collision"
		writeLegacyRoster(t, store, compID, "John Smith,Wakaba\nJohn Smith,Tora\n")

		_, err = store.UpdateParticipant(compID, "John Smith|Tora", false, func(p *domain.Player) error {
			p.CheckedIn = true
			return nil
		})
		assert.ErrorIs(t, err, ErrParticipantNotFound)

		loaded, err := store.LoadParticipants(compID, false)
		require.NoError(t, err)
		for _, p := range loaded {
			assert.False(t, p.CheckedIn, "%s at %s must remain unchecked", p.Name, p.Dojo)
		}
	})

	t.Run("unknown name|dojo still reports NotFound, not an error", func(t *testing.T) {
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

// TestResolveParticipantIndex unit-tests the resolver directly. ID-only
// (operator ruling bc-pnum): a stable UUID is the ONLY thing resolveParticipantIndex
// matches. This converts the pre-bc-pnum pin (which asserted a "legacy
// name|dojo" composite pid DID resolve an ID-less row, mp-8bjq) to assert
// the opposite: that composite shape never resolves anything, ID-less rows
// included -- there is no fallback left at all, not even for the exact
// legacy shape the fallback used to exist for.
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
		{"a name|dojo composite for an ID-less row no longer resolves", "Alice|DojoA", -1},
		{"a name|dojo composite for a same-name-different-dojo pair no longer resolves", "John Smith|Tora", -1},
		{"uuid row was never addressable by name and still isn't", "Uuidy|DojoU", -1},
		{"unknown", "Nobody|Nowhere", -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, resolveParticipantIndex(players, tc.pid))
		})
	}
}
