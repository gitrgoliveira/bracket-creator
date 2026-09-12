package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// TestMatchesParticipant_NormalizedDojoComparison pinned the bc-pnum review's
// first half: an id-less pools.csv row's identity match used to fall back to
// a normalized (name, dojo) comparison. That fallback is removed by the
// bc-pnum operator ruling (matchesParticipant is now id-only: `rowID != ""
// && pid != "" && rowID == pid`, see replace_participant.go), so there is no
// dojo normalisation left in this function to test -- an id-less row simply
// never matches, regardless of name or dojo. Deleted rather than converted:
// a "does an id-less row ever match" test would just restate
// matchesParticipant's one line.

// TestReplaceParticipantInDraw_LegacyDojoOnlyEdit_NoSelfAmbiguityWarning is
// the bc-pnum review's second half. A legacy id-less roster's single "Alice"
// is edited on DOJO ALONE (name unchanged): SaveParticipants immediately
// mints a fresh id for any id-less row on write (marshalParticipantsCSV), so
// by the time the ambiguity scan re-reads participants.csv, Alice's own row
// now carries a real id unrelated to the synthetic "name|dojo" pid the
// caller resolved her by. An id-to-id exclusion check alone therefore fails
// to recognise her as herself, and a single-competitor roster's own edit
// wrongly reads as "ambiguous across dojos".
func TestReplaceParticipantInDraw_LegacyDojoOnlyEdit_NoSelfAmbiguityWarning(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "legacy-dojo-only-edit"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Legacy Dojo Only", Kind: "individual",
		Format: state.CompFormatPlayoffs, Courts: []string{"A"},
		StartTime: "09:00", Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "DojoX"},
		{Name: "Bob", Dojo: "DojoB"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{{ID: "m-r1-0", SideA: "Alice", SideB: "Bob"}}},
	}))

	// Same name, dojo DojoX -> DojoY: a dojo-only edit of the sole "Alice".
	warnings, err := eng.ReplaceParticipantInDraw(compID, "Alice|DojoX", "Alice", "DojoX", "", "Alice", "DojoY", "")
	require.NoError(t, err)
	for _, w := range warnings {
		assert.NotContains(t, w, "ambiguous", "a dojo-only edit of the sole \"Alice\" must not warn about itself: warnings=%v", warnings)
	}

	bracketAfter, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", bracketAfter.Rounds[0][0].SideA, "the bracket entry (same name) is untouched by a dojo-only edit")
}
