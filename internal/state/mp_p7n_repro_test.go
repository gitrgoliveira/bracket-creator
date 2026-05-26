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

func TestMpP7nRepro_NonUUIDID_AutoDetect(t *testing.T) {
	// Same shape but without the HasIDs hint — the auto-detect path
	// from participants.go:111. uuidRE on a non-UUID first field
	// returns false, so hasIDs=false, every column becomes data, same
	// column-shift outcome.
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "asddasd"

	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Asddasd", WithZekkenName: false, HasParticipantIDs: false,
	}))
	players := []domain.Player{
		{ID: "asddasd-p1", Name: "Aaron Adams", Dojo: "Team Alpha"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	assert.Equal(t, "Aaron Adams", loaded[0].Name)
	assert.Equal(t, "Team Alpha", loaded[0].Dojo)
	assert.Empty(t, loaded[0].Metadata)
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
