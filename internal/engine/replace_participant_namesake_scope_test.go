package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestReplaceParticipantInDraw_IDLessCrossPoolNamesake_OnlyTargetPoolRewritten
// is the bc-pnum review's repro. For id-less pool-matches.csv rows, the
// rename used to fall back to `rowName == oldName` with no dojo and no
// per-pool scope, so renaming one of two cross-dojo namesakes rewrote the
// OTHER namesake's pool-matches row too: pools.csv (name, dojo)-scoped by
// matchesParticipant correctly leaves the untouched pool alone, but
// pool-matches.csv (no per-side dojo at all) disagreed with it in the same
// write, and the untouched namesake's results then vanish from standings
// (lookupStandingsPlayer nil -> continue).
//
// Reachable in production exactly as described: a draw over an id-less
// legacy roster (runDrawPipeline never backfills ids) writes pools.csv and
// pool-matches.csv without ids, then a participant PUT backfills a UUID for
// the edited row (SaveParticipants mints one for any id-less row on write)
// and calls this cascade with the synthetic "name|dojo" pid
// (state.resolveParticipantIndex's own fallback for a legacy lookup).
func TestReplaceParticipantInDraw_IDLessCrossPoolNamesake_OnlyTargetPoolRewritten(t *testing.T) {
	eng, store, compID := setupLegacyIDLessMixedTwoPools(t)

	warnings, err := eng.ReplaceParticipantInDraw(compID, "Alice|DojoX", "Alice", "DojoX", "", "Alicia", "DojoX", "")
	require.NoError(t, err)
	assert.Empty(t, warnings, "no ambiguity within Pool A itself: Pool A's own namesake was already renamed")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var poolAMatch, poolBMatch *state.MatchResult
	for i := range matches {
		switch matches[i].ID {
		case "Pool A-0":
			poolAMatch = &matches[i]
		case "Pool B-0":
			poolBMatch = &matches[i]
		}
	}
	require.NotNil(t, poolAMatch, "Pool A-0 must exist")
	require.NotNil(t, poolBMatch, "Pool B-0 must exist")
	assert.Equal(t, "Alicia", poolAMatch.SideA, "Pool A's own match must be rewritten")
	assert.Equal(t, "Alice", poolBMatch.SideA, "Pool B's UNRELATED namesake match must be left untouched")

	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, poolsAfter, 2)
	assert.Equal(t, "Alicia", poolsAfter[0].Players[0].Name, "Pool A's own roster row is renamed")
	assert.Equal(t, "Alice", poolsAfter[1].Players[0].Name, "Pool B's namesake roster row is untouched (already correct before this fix)")
}

// TestReplaceParticipantInDraw_IDLessSamePoolNamesake_SkippedWithWarning is
// the review finding's same-pool variant: two id-less "Alice" rows IN THE SAME POOL
// cannot be told apart by a pool-matches row that carries only a name, so
// the rewrite must be skipped and warned about rather than guessed.
func TestReplaceParticipantInDraw_IDLessSamePoolNamesake_SkippedWithWarning(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "legacy-idless-same-pool"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Legacy Same Pool", Kind: "individual",
		Format: state.CompFormatMixed, Courts: []string{"A"},
		StartTime: "09:00", Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "DojoX"},
		{Name: "Alice", Dojo: "DojoY"},
		{Name: "Carol", Dojo: "DojoC"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice", Dojo: "DojoX"},
			{Name: "Alice", Dojo: "DojoY"},
			{Name: "Carol", Dojo: "DojoC"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled, Court: "A"},
	}))

	warnings, err := eng.ReplaceParticipantInDraw(compID, "Alice|DojoX", "Alice", "DojoX", "", "Alicia", "DojoX", "")
	require.NoError(t, err)
	require.NotEmpty(t, warnings, "an ambiguous same-pool namesake row must be warned about")
	joined := ""
	for _, w := range warnings {
		joined += w + "\n"
	}
	assert.Contains(t, joined, "ambiguous")
	assert.Contains(t, joined, "Alice")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "Alice", matches[0].SideA, "an ambiguous same-pool namesake row must be left unchanged, not guessed")

	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, poolsAfter, 1)
	assert.Equal(t, "Alicia", poolsAfter[0].Players[0].Name, "pools.csv itself is dojo-scoped and unaffected by this ambiguity: it still renames the exact (name, dojo) row")
	assert.Equal(t, "Alice", poolsAfter[0].Players[1].Name, "the other DojoY namesake's own row is untouched")
}

// setupLegacyIDLessMixedTwoPools builds a draw-ready MIXED competition with
// two pools whose pools.csv and pool-matches.csv both carry EMPTY id
// columns, mirroring a draw generated over a legacy id-less roster
// (runDrawPipeline never backfills ids). Pool A holds "Alice"/DojoX, Pool B
// holds a namesake "Alice"/DojoOther -- two DIFFERENT competitors, legal per
// CheckDuplicateEntriesByNameDojo.
func setupLegacyIDLessMixedTwoPools(t *testing.T) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	compID := "legacy-idless-two-pools"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Legacy ID-less", Kind: "individual",
		Format: state.CompFormatMixed, Courts: []string{"A"},
		StartTime: "09:00", Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "DojoX"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Alice", Dojo: "DojoOther"},
		{Name: "Carol", Dojo: "DojoC"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice", Dojo: "DojoX"},
			{Name: "Bob", Dojo: "DojoB"},
		}},
		{PoolName: "Pool B", Players: []helper.Player{
			{Name: "Alice", Dojo: "DojoOther"},
			{Name: "Carol", Dojo: "DojoC"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A"},
		{ID: "Pool B-0", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled, Court: "A"},
	}))
	return eng, store, compID
}
