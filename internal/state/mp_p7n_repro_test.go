package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mp-p7n: failing repro of "phantom leading column appears in textarea
// after Apply".
//
// User-reported flow:
//   - Competition has withZekkenName=false, numberPrefix empty,
//     hasParticipantIDs=true.
//   - User clicks Apply with clean 2-col text "Aaron Adams, Team Alpha".
//   - After Apply the textarea regenerates as
//     "Asddasd-P1, Aaron Adams, Team Alpha" — name and dojo shifted one
//     column right, with the participant id leaking into Name (and
//     getting title-cased: "asddasd-p1" → "Asddasd-P1").
//
// Root cause hypothesis: web-mobile/js/admin_participants.jsx::mintParticipantIds
// generates participant IDs in the shape `${compID}-p${N}` for new
// players (and the sample-roster generator data.jsx::makePlayer does the
// same). Those values are NOT UUIDv4. participants.csv is saved with the
// non-UUID id as the first column, e.g.
//
//	asddasd-p1,Aaron Adams,Team Alpha
//
// On the next LoadParticipantsOpt(_, withZekkenName=false), the per-record
// check at participants.go:125 only strips the first field as an ID when
// `uuidRE(record[0])` returns true. A non-UUID id leaves dataStart=0, so
// the row gets parsed as `[Name="asddasd-p1", Dojo="Aaron Adams",
// Metadata=["Team Alpha"]]`. CreatePlayersFromRecords title-cases Name →
// "Asddasd-P1". The corrupted player flows back to the JS layer and the
// textarea (which serialises `${p.name}, ${p.dojo}[, ${p.danGrade}]`)
// renders the bug shape from the screenshot.
//
// These tests will fail until the save path normalises non-UUID ids
// (regenerating them to UUIDv4 before the CSV write) or the load path
// is hardened to skip non-UUID first columns when hasIDs is hinted true.

func TestMpP7nRepro_NonUUIDIDCausesColumnShift(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "asddasd"

	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Asddasd", WithZekkenName: false, HasParticipantIDs: true,
	}))

	// Players carry mintParticipantIds-shaped IDs ("${compID}-p${N}"),
	// NOT UUIDv4. This is what the JS layer hands the server when the
	// frontend mints new participant rows (admin_participants.jsx:127-128).
	players := []domain.Player{
		{ID: "asddasd-p1", Name: "Aaron Adams", Dojo: "Team Alpha"},
		{ID: "asddasd-p2", Name: "Albus Blake", Dojo: "Team Delta"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	// Confirm what the CSV writer actually persisted.
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "participants.csv"))
	require.NoError(t, err)
	t.Logf("participants.csv =\n%s", string(raw))

	// Now load using the same hint the live handler uses
	// (handlers_viewer.go:147 passes HasIDs=&true when
	// HasParticipantIDs=true).
	trueP := true
	loaded, err := store.LoadParticipantsOpt(compID, false, LoadParticipantsOpts{
		WithSeeds: false,
		HasIDs:    &trueP,
	})
	require.NoError(t, err)
	require.Len(t, loaded, 2)

	// THE BUG: pre-fix these assertions fail. Name becomes "Asddasd-P1"
	// (the title-cased id), Dojo becomes "Aaron Adams" (the original
	// name), and Metadata gets "Team Alpha" (the original dojo).
	assert.Equal(t, "Aaron Adams", loaded[0].Name,
		"Name must be the saved 'Aaron Adams', not a title-cased participant id")
	assert.Equal(t, "Team Alpha", loaded[0].Dojo,
		"Dojo must be 'Team Alpha', not 'Aaron Adams' (shifted from the Name column)")
	assert.Empty(t, loaded[0].Metadata,
		"Metadata must be empty — pre-bug it contains ['Team Alpha'] (shifted from the Dojo column)")
}

func TestMpP7nRepro_NonUUIDID_PreservesOriginalID(t *testing.T) {
	// mp-p7n: the loader now trusts the HasIDs hint and strips column
	// 0 regardless of UUID shape. Together with the no-regeneration
	// save path, that means a client-supplied non-UUID id round-trips
	// intact — important for joining with other persisted state that
	// references the player by id (CompetitorStatus.PlayerID,
	// ReservedSlot.ParticipantID, team lineup PlayerIDs). Copilot
	// PR #185 round-3 finding: regenerating ids would silently orphan
	// those references.
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "asddasd"

	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Asddasd", WithZekkenName: false, HasParticipantIDs: true,
	}))
	players := []domain.Player{
		{ID: "asddasd-p1", Name: "Aaron Adams", Dojo: "Team Alpha"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	trueP := true
	loaded, err := store.LoadParticipantsOpt(compID, false, LoadParticipantsOpts{
		WithSeeds: false, HasIDs: &trueP,
	})
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	// Name / Dojo align correctly (no column shift) AND the original
	// non-UUID id is preserved (no regeneration on save).
	assert.Equal(t, "Aaron Adams", loaded[0].Name)
	assert.Equal(t, "Team Alpha", loaded[0].Dojo)
	assert.Empty(t, loaded[0].Metadata)
	assert.Equal(t, "asddasd-p1", loaded[0].ID,
		"original id must survive the round-trip — regenerating it would orphan competitor_status / reserved-slot references")
}

// mp-p7n / Copilot PR #185 round-4 + round-9: closes the cache-
// poisoning race between a roster save (non-UUID ids on disk) and the
// deferred HasParticipantIDs=true flip.
//
// Pre-fix sequence:
//  1. SaveParticipants writes `${compID}-pN` rows; participants.csv mtime updates.
//  2. Reader A loads BEFORE HasParticipantIDs is set; falls back to
//     auto-detect, uuidRE-on-row-0 fails on the non-UUID first column,
//     hasIDs=false, the row is parsed as data (column shift), cached.
//  3. The deferred HasParticipantIDs=true flip lands in config.md, but
//     participants.csv mtime is unchanged → cache still serves the
//     shifted players.
//
// Fix: saveCompetitionChangedLocked explicitly invalidates the
// participant cache variants on every config write (round-9). This is
// deterministic regardless of filesystem timestamp granularity — note
// this test does NOT sleep between the roster save and the flag flip,
// so a coarse-mtime filesystem that left the summed cache mtime
// unchanged would still pass (pre-round-9, this test needed a 20ms
// sleep to force a distinct config.md mtime; that crutch is gone).
func TestMpP7nRepro_CacheInvalidatedOnHasParticipantIDsFlip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "race"

	// HasParticipantIDs=false initially (simulating the pre-flip window).
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Race", WithZekkenName: false, HasParticipantIDs: false,
	}))
	players := []domain.Player{
		{ID: "race-p1", Name: "Aaron Adams", Dojo: "Team Alpha"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	// First load: HasParticipantIDs=false, non-UUID first column,
	// falls into the auto-detect path → uuidRE-on-row-0 fails → no
	// strip → column shift. The corrupted view gets cached.
	loadedPre, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedPre, 1)
	assert.Equal(t, "Race-P1", loadedPre[0].Name,
		"pre-flip load takes the auto-detect path and mis-loads (column shift)")

	// Flip the flag — simulating the deferred HasParticipantIDs=true
	// that lands after the first roster save succeeds. NO sleep: the
	// round-9 explicit cache invalidation must work even when this
	// config write shares participants.csv's coarse mtime.
	current, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	current.HasParticipantIDs = true
	require.NoError(t, store.SaveCompetition(current))

	// Post-flip load: the participant cache MUST have been invalidated
	// by the config write. The loader now takes the trustHint branch
	// and correctly strips column 0.
	loadedPost, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedPost, 1)
	assert.Equal(t, "Aaron Adams", loadedPost[0].Name,
		"post-flip load must reflect the new flag, not the cached pre-flip parse")
	assert.Equal(t, "Team Alpha", loadedPost[0].Dojo)
	assert.Equal(t, "race-p1", loadedPost[0].ID,
		"original non-UUID id must be preserved (no regeneration)")
}

// mp-p7n / Copilot PR #185 round-6: the participant cache key must
// split by HasIDs parse mode. Without the split, a no-hint
// auto-detect load that lands a "no-IDs" parse can poison the same
// cache entry that a later HasIDs=&true call reads — the hinted call
// would return the cached shifted rows instead of stripping column 0.
func TestMpP7nRepro_CacheKeySplitsByParseMode(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "cache-mode"

	// HasParticipantIDs=false on disk — auto-detect path will fall back
	// to uuidRE-on-row-0, which fails for the non-UUID id below and
	// returns the column-shifted view.
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Cache Mode", WithZekkenName: false, HasParticipantIDs: false,
	}))
	players := []domain.Player{
		{ID: "cache-mode-p1", Name: "Aaron Adams", Dojo: "Team Alpha"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	// First: no-hint load lands the shifted parse and caches it.
	loadedAuto, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedAuto, 1)
	assert.Equal(t, "Cache-Mode-P1", loadedAuto[0].Name,
		"no-hint load takes the auto-detect path and mis-loads (column shift)")

	// Second: hinted load against the SAME on-disk file (no mtime
	// change) MUST not hit the auto-detect cache entry. Pre-fix this
	// would have returned the same shifted parse from the shared cache;
	// post-fix the parse-mode-keyed cache forces a fresh parse with
	// the hint, which correctly strips column 0.
	trueP := true
	loadedHint, err := store.LoadParticipantsOpt(compID, false, LoadParticipantsOpts{
		WithSeeds: true, HasIDs: &trueP,
	})
	require.NoError(t, err)
	require.Len(t, loadedHint, 1)
	assert.Equal(t, "Aaron Adams", loadedHint[0].Name,
		"hinted load must NOT serve the auto-detect cache entry — distinct cache key per parse mode")
	assert.Equal(t, "Team Alpha", loadedHint[0].Dojo)
	assert.Equal(t, "cache-mode-p1", loadedHint[0].ID)
}

func TestMpP7nRepro_UUIDIDIsFine(t *testing.T) {
	// Sanity: with proper UUIDv4 IDs, the round-trip is clean. This
	// test should PASS both pre-fix and post-fix — it isolates the bug
	// to the non-UUID id case.
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "uuid-ok"

	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "UUID OK", WithZekkenName: false, HasParticipantIDs: true,
	}))
	players := []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Aaron Adams", Dojo: "Team Alpha"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	trueP := true
	loaded, err := store.LoadParticipantsOpt(compID, false, LoadParticipantsOpts{
		WithSeeds: false, HasIDs: &trueP,
	})
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Aaron Adams", loaded[0].Name)
	assert.Equal(t, "Team Alpha", loaded[0].Dojo)
}
