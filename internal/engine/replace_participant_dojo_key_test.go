package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestReplaceParticipantInDraw_DojoConflictUsesNormalisedDojo pins that the
// post-replacement dojo-conflict warning compares dojos under the roster's
// identity normalisation, exactly as the draw does when it forms the pools:
// a replacement whose new dojo is "mumeishi" lands in a pool that already
// holds a "Mumeishi" competitor, and the operator must be warned. A raw
// string compare (the pre-fix code) counted the two spellings as different
// dojos and stayed silent.
func TestReplaceParticipantInDraw_DojoConflictUsesNormalisedDojo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "replace-dojo-key"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Dojo Key", Kind: "individual",
		Format: state.CompFormatLeague, Courts: []string{"A"},
		StartTime: "09:00", Status: state.CompStatusDrawReady,
	}))

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: aliceID, Name: "Alice", Dojo: "Mumeishi"},
			{ID: bobID, Name: "Bob", Dojo: "Seishin"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideAID: aliceID, SideB: "Bob", SideBID: bobID,
			Status: state.MatchStatusScheduled, Court: "A"},
	}))

	// Bob is replaced by Carol from "mumeishi": the same dojo as Alice under
	// the identity rule, spelled with a different case.
	warnings, err := eng.ReplaceParticipantInDraw(compID, bobID, "Bob", "Seishin", "", "Carol", "mumeishi", "")
	require.NoError(t, err)
	require.NotEmpty(t, warnings, "a case-different spelling of a pool-mate's dojo is the same dojo and must warn")
	assert.Contains(t, warnings[0], "dojo conflict")
	assert.Contains(t, warnings[0], "Pool A")
}
