package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestReplaceParticipantInDraw_IDLessCrossPoolRow_NeverRenamed is the bc-pnum
// CONVERSION of the former TestReplaceParticipantInDraw_IDLessCrossPoolNamesake_OnlyTargetPoolRewritten.
// That test pinned a pre-bc-pnum fix: for id-less pool-matches.csv rows, the
// rename used to fall back to `rowName == oldName` with no dojo and no
// per-pool scope, so renaming one of two cross-dojo namesakes could rewrite
// the OTHER namesake's pool-matches row too; the fix scoped that fallback to
// the target's own pool.
//
// The bc-pnum operator ruling removed the fallback itself, for BOTH
// pools.csv and pool-matches.csv: matchesParticipant (replace_participant.go)
// is now `rowID != "" && pid != "" && rowID == pid`, so a row with no id at
// all is simply never matched, in either file, by this rename cascade --
// there is no (name, dojo) or per-pool scoping left to test, because there
// is no fallback path left to reach. This asserts the new, opposite
// behaviour: a synthetic "name|dojo" pid (the pre-bc-pnum legacy lookup key)
// matches no row anywhere, so NEITHER pool's roster nor pool-matches row is
// touched, and the generic "not found in draw artifacts" warning fires
// instead of the old empty-warnings happy path.
func TestReplaceParticipantInDraw_IDLessCrossPoolRow_NeverRenamed(t *testing.T) {
	eng, store, compID := setupLegacyIDLessMixedTwoPools(t)

	warnings, err := eng.ReplaceParticipantInDraw(compID, "Alice|DojoX", "Alice", "DojoX", "", "Alicia", "DojoX", "")
	require.NoError(t, err)
	require.Len(t, warnings, 1, "an id-less pid matches no row anywhere, so the generic not-found warning fires")
	assert.Contains(t, warnings[0], "not found in draw artifacts")

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
	assert.Equal(t, "Alice", poolAMatch.SideA, "an id-less row is never matched, so Pool A's own match is left exactly as it was")
	assert.Equal(t, "Alice", poolBMatch.SideA, "Pool B's unrelated namesake match is likewise untouched")

	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, poolsAfter, 2)
	assert.Equal(t, "Alice", poolsAfter[0].Players[0].Name, "pools.csv rows are id-only too (matchesParticipant): the id-less row is never matched")
	assert.Equal(t, "Alice", poolsAfter[1].Players[0].Name, "Pool B's namesake roster row is likewise untouched")
}

// TestReplaceParticipantInDraw_IDLessSamePoolRow_NeverRenamed is the bc-pnum
// CONVERSION of the former TestReplaceParticipantInDraw_IDLessSamePoolNamesake_SkippedWithWarning.
// That test pinned the same-pool variant of the review finding above: two
// id-less "Alice" rows in the SAME pool could not be told apart by a
// pool-matches row carrying only a name, so the fix skipped the rewrite and
// returned a specific "ambiguous" warning rather than guessing.
//
// Under the bc-pnum operator ruling there is no longer any name-based
// lookup for an id-less row to be ambiguous ABOUT: matchesParticipant never
// even reaches the point of comparing names, since the row's id is empty.
// The specific "ambiguous same-pool namesake" diagnosis this test pinned is
// therefore gone -- see TestReplaceParticipantInDraw_IDLessCrossPoolRow_NeverRenamed
// above for the identical reasoning applied to the cross-pool shape. This
// asserts the new, opposite behaviour: nothing is renamed anywhere (pools.csv
// included, which the pre-bc-pnum version DID rewrite via its dojo-scoped
// exact match) and the warning is the generic "not found in draw artifacts",
// not "ambiguous".
func TestReplaceParticipantInDraw_IDLessSamePoolRow_NeverRenamed(t *testing.T) {
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
	require.Len(t, warnings, 1, "an id-less pid matches no row anywhere, so the generic not-found warning fires")
	assert.Contains(t, warnings[0], "not found in draw artifacts")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "Alice", matches[0].SideA, "an id-less row is never matched, so it is left exactly as it was")

	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, poolsAfter, 1)
	assert.Equal(t, "Alice", poolsAfter[0].Players[0].Name, "pools.csv rows are id-only too (matchesParticipant): the id-less row is never matched, even the exact (name, dojo) one the pre-bc-pnum version rewrote")
	assert.Equal(t, "Alice", poolsAfter[0].Players[1].Name, "the other DojoY namesake's own row is likewise untouched")
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
