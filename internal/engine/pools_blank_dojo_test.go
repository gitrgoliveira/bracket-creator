package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateDraw_RefusesBlankDojoRoster pins the engine-side half of FIX 1
// (bc-dojo-least-conflicted-pool): a competition whose participants.csv holds
// a blank-dojo row must have its draw refused as a *engine.ValidationError
// (-> HTTP 400 at the generate-draw handler), naming the offending player,
// rather than reaching helper.BuildPoolPhaseTreeAwareWithMode and silently
// corrupting the tree-aware distributor's capacity accounting.
//
// The roster is written DIRECTLY to participants.csv, bypassing
// state.SaveParticipants' own write-time blank-dojo guard
// (state.ErrBlankDojo, internal/state/participants.go): that guard protects
// every NEW save, but state.LoadParticipants is deliberately still willing to
// LOAD a roster that predates the guard, or was hand-edited on disk, so an
// operator can see and repair it (see state.ErrBlankDojo's own doc comment).
// The draw pipeline only ever READS participants.csv before drawing, so it
// never crosses that write-time floor and must supply its own refusal.
func TestGenerateDraw_RefusesBlankDojoRoster(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "blank-dojo-roster"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = []string{"A"}
	})

	// Legacy/hand-edited, UUID-less, non-zekken (2-column "Name,Dojo") CSV:
	// a trailing empty second field parses to Dojo == "" (helper.CreatePlayersFromRecords'
	// non-zekken branch takes line[1] verbatim, with no blank check).
	csvPath := filepath.Join(dir, "competitions", compID, "participants.csv")
	require.NoError(t, os.MkdirAll(filepath.Dir(csvPath), 0700))
	csv := "Alice,DojoA\n" +
		"NoDojoHere,\n" +
		"Carol,DojoC\n" +
		"Dave,DojoA\n" +
		"Bob,DojoB\n" +
		"Erin,DojoB\n" +
		"Frank,DojoC\n" +
		"Grace,DojoA\n"
	require.NoError(t, os.WriteFile(csvPath, []byte(csv), 0600))

	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a *engine.ValidationError (-> HTTP 400 at POST /competitions/:id/generate-draw)")
	assert.Contains(t, ve.Error(), "NoDojoHere", "the error must name the offending player so the operator knows which row to repair")

	comp, lerr := store.LoadCompetition(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
	pools, perr := store.LoadPools(compID)
	require.NoError(t, perr)
	assert.Empty(t, pools, "nothing may be persisted for a refused draw")
}

// writeBlankDojoRosterCSV writes the same legacy/hand-edited blank-dojo
// roster TestGenerateDraw_RefusesBlankDojoRoster uses, directly to
// participants.csv for compID (bypassing state.SaveParticipants' own
// write-time guard -- see that test's own doc comment for why the draw
// pipeline must supply its own refusal instead of relying on it).
func writeBlankDojoRosterCSV(t *testing.T, dir, compID string) {
	t.Helper()
	csvPath := filepath.Join(dir, "competitions", compID, "participants.csv")
	require.NoError(t, os.MkdirAll(filepath.Dir(csvPath), 0700))
	csv := "Alice,DojoA\n" +
		"NoDojoHere,\n" +
		"Carol,DojoC\n" +
		"Dave,DojoA\n" +
		"Bob,DojoB\n" +
		"Erin,DojoB\n" +
		"Frank,DojoC\n" +
		"Grace,DojoA\n"
	require.NoError(t, os.WriteFile(csvPath, []byte(csv), 0600))
}

// TestGenerateDraw_RefusesBlankDojoRoster_Playoffs and
// TestGenerateDraw_RefusesBlankDojoRoster_Swiss pin bc-drwx item 8: the
// blank-dojo refusal used to live ONLY inside the pool distributor
// (BuildPoolPhaseTreeAware*, reached by generatePools for mixed/league), so
// a standalone playoffs or Swiss competition over the exact same
// legacy/hand-edited blank-dojo roster TestGenerateDraw_RefusesBlankDojoRoster
// exercises for mixed drew SILENTLY instead of refusing -- neither
// generatePlayoffs (helper.StandardSeeding has no dojo opinion) nor
// GenerateSwissRound goes anywhere near the distributor. runDrawPipeline's
// own pre-flight (helper.ValidateNoBlankDojo, called once ahead of the
// format switch) now covers every format.
func TestGenerateDraw_RefusesBlankDojoRoster_Playoffs(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "blank-dojo-roster-playoffs"

	createTestCompetition(t, store, compID, state.CompFormatPlayoffs, 0, func(c *state.Competition) {
		c.Courts = []string{"A"}
	})
	writeBlankDojoRosterCSV(t, dir, compID)

	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a *engine.ValidationError (-> HTTP 400 at POST /competitions/:id/generate-draw)")
	assert.Contains(t, ve.Error(), "NoDojoHere", "the error must name the offending player so the operator knows which row to repair")

	comp, lerr := store.LoadCompetition(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
	bracket, berr := store.LoadBracket(compID)
	require.NoError(t, berr)
	assert.Empty(t, bracket.Rounds, "nothing may be persisted for a refused draw")
}

func TestGenerateDraw_RefusesBlankDojoRoster_Swiss(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "blank-dojo-roster-swiss"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        "Blank Dojo Swiss",
		Kind:        "individual",
		Format:      state.CompFormatSwiss,
		SwissRounds: 3,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		Status:      state.CompStatusSetup,
	}))
	writeBlankDojoRosterCSV(t, dir, compID)

	err := eng.GenerateDraw(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a *engine.ValidationError (-> HTTP 400 at POST /competitions/:id/generate-draw)")
	assert.Contains(t, ve.Error(), "NoDojoHere", "the error must name the offending player so the operator knows which row to repair")

	comp, lerr := store.LoadCompetition(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
	assert.Equal(t, 0, comp.SwissCurrentRound, "a refused draw must not advance the Swiss round")
	matches, merr := store.LoadPoolMatches(compID)
	require.NoError(t, merr)
	assert.Empty(t, matches, "nothing may be persisted for a refused draw")
}

// TestGenerateDraw_RefusesOneColumnLegacyRoster pins bc-drwx item 7: a
// participants.csv with NO dojo column at all (every row is a bare name,
// with no comma) must be refused at the SAME draw pre-flight as an explicit
// blank dojo ("Name,"), not silently accepted as one giant dojo named "NA".
//
// helper.CreatePlayersFromRecords' tolerant (requireDojo=false) non-zekken
// branch -- the one state.LoadParticipants uses -- used to default a MISSING
// dojo column to the literal string "NA" while leaving an EXPLICIT blank
// column ("Name,") as "". That asymmetry meant a legacy, one-column roster
// (the shape a very old export, or a hand-typed name-only list, produces)
// sailed straight past ValidateNoBlankDojo -- "NA" is non-blank -- and drew
// as one giant "NA" dojo, spreading every competitor as if they shared a
// single real dojo, while the exact same missing-dojo intent spelled with a
// trailing comma was correctly refused. The fix makes a missing column
// yield "" too, so both spellings of "no dojo here" are caught the same way
// by the same pre-flight, and the operator sees one consistent refusal
// naming every affected row instead of a silently-merged dojo.
//
// Written DIRECTLY to participants.csv, bypassing state.SaveParticipants'
// own write-time guard, for the same reason writeBlankDojoRosterCSV is (see
// TestGenerateDraw_RefusesBlankDojoRoster's own doc comment): the write
// floor cannot protect a file that predates it or was hand-edited, so the
// read-then-draw pipeline must catch this on its own.
func TestGenerateDraw_RefusesOneColumnLegacyRoster(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "one-column-legacy-roster"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = []string{"A"}
	})

	// Legacy, UUID-less, non-zekken, ONE-COLUMN CSV: no comma anywhere, so
	// every row takes CreatePlayersFromRecords' len(line) < 2 branch.
	csvPath := filepath.Join(dir, "competitions", compID, "participants.csv")
	require.NoError(t, os.MkdirAll(filepath.Dir(csvPath), 0700))
	csv := "Alice\n" +
		"Bob\n" +
		"Carol\n" +
		"Dave\n" +
		"Erin\n" +
		"Frank\n" +
		"Grace\n" +
		"Heidi\n"
	require.NoError(t, os.WriteFile(csvPath, []byte(csv), 0600))

	err := eng.GenerateDraw(compID)
	require.Error(t, err, "a one-column roster must be refused, not drawn as one giant \"NA\" dojo")
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a *engine.ValidationError (-> HTTP 400 at POST /competitions/:id/generate-draw)")
	assert.Contains(t, ve.Error(), "Alice", "the error must name the offending players so the operator knows which rows to repair")
	assert.NotContains(t, ve.Error(), "NA", "the missing column must not be papered over with a fake dojo string")

	comp, lerr := store.LoadCompetition(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
	pools, perr := store.LoadPools(compID)
	require.NoError(t, perr)
	assert.Empty(t, pools, "nothing may be persisted for a refused draw")
}
