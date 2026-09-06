package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateDraw_RefusesMissingIDsRoster pins bc-pnum ruling 1c: a
// competition whose participants.csv predates the id-minting write path (see
// internal/state/participants.go marshalParticipantsCSV) must have its draw
// refused as a *engine.ValidationError (-> HTTP 400 at generate-draw/start),
// naming the offending player, rather than drawing and stamping blank ids
// into pools.csv/bracket.json.
//
// Written DIRECTLY to participants.csv, bypassing state.SaveParticipants'
// own write-time mint, exactly like writeBlankDojoRosterCSV in
// pools_blank_dojo_test.go: a legacy or hand-edited file predates that mint
// and the draw pipeline only ever READS the file before drawing, so it must
// supply its own refusal rather than relying on the write floor.
func writeMissingIDsRosterCSV(t *testing.T, dir, compID string) {
	t.Helper()
	csvPath := filepath.Join(dir, "competitions", compID, "participants.csv")
	require.NoError(t, os.MkdirAll(filepath.Dir(csvPath), 0700))
	// Legacy, id-less, non-zekken (2-column "Name,Dojo") CSV: no leading
	// UUID column.
	csv := "Alice,DojoA\n" +
		"NoIDHere,DojoB\n" +
		"Carol,DojoC\n" +
		"Dave,DojoA\n" +
		"Bob,DojoB\n" +
		"Erin,DojoB\n" +
		"Frank,DojoC\n" +
		"Grace,DojoA\n"
	require.NoError(t, os.WriteFile(csvPath, []byte(csv), 0600))
}

func TestGenerateDraw_RefusesMissingIDsRoster(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "missing-ids-roster"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = []string{"A"}
	})
	writeMissingIDsRosterCSV(t, dir, compID)

	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a *engine.ValidationError (-> HTTP 400 at POST /competitions/:id/generate-draw)")
	assert.Contains(t, ve.Error(), "no id on file", "the error must name the remedy, not just fail silently")

	comp, lerr := store.LoadCompetition(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
	pools, perr := store.LoadPools(compID)
	require.NoError(t, perr)
	assert.Empty(t, pools, "nothing may be persisted for a refused draw")
}

func TestGenerateDraw_RefusesMissingIDsRoster_Playoffs(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "missing-ids-roster-playoffs"

	createTestCompetition(t, store, compID, state.CompFormatPlayoffs, 0, func(c *state.Competition) {
		c.Courts = []string{"A"}
	})
	writeMissingIDsRosterCSV(t, dir, compID)

	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a *engine.ValidationError (-> HTTP 400 at POST /competitions/:id/generate-draw)")

	comp, lerr := store.LoadCompetition(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
	bracket, berr := store.LoadBracket(compID)
	require.NoError(t, berr)
	assert.Empty(t, bracket.Rounds, "nothing may be persisted for a refused draw")
}

func TestGenerateDraw_RefusesMissingIDsRoster_Swiss(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "missing-ids-roster-swiss"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Missing IDs Swiss",
		Kind:        "individual",
		Format:      state.CompFormatSwiss,
		SwissRounds: 3,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		Status:      state.CompStatusSetup,
	}))
	writeMissingIDsRosterCSV(t, dir, compID)

	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a *engine.ValidationError (-> HTTP 400 at POST /competitions/:id/generate-draw)")

	comp, lerr := store.LoadCompetition(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
	assert.Equal(t, 0, comp.SwissCurrentRound, "a refused draw must not advance the Swiss round")
}

// TestGenerateDraw_StampedRosterDrawsNormally is the control: a roster whose
// every row already has an id (via a real SaveParticipants) must draw as
// normal, unaffected by the new pre-flight.
func TestGenerateDraw_StampedRosterDrawsNormally(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "stamped-roster"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = []string{"A"}
	})
	roster := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Carol", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoA"},
		{Name: "Erin", Dojo: "DojoB"},
		{Name: "Frank", Dojo: "DojoC"},
		{Name: "Grace", Dojo: "DojoA"},
		{Name: "Heidi", Dojo: "DojoB"},
	}
	require.NoError(t, store.SaveParticipants(compID, roster)) // mints an id for every row

	require.NoError(t, eng.GenerateDraw(compID))
	pools, perr := store.LoadPools(compID)
	require.NoError(t, perr)
	assert.NotEmpty(t, pools)
}
