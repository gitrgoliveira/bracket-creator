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
