package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mp-n6ke follow-up: DeleteCompetition used to call compCache.Delete(id), which
// dropped the whole per-competition fileCache map and reset every version
// counter to 0. Competition IDs are deterministic slugs of the name, so deleting
// a competition and recreating it under the same name reuses the ID and would
// hand the new competition the same starting tokens the old one had, letting a
// downstream cache keyed on (compID, mtime, version) serve the dead
// competition's data.
//
// These tests pin the two halves of the replacement: counters keep climbing
// across a delete, and the cached bodies are still dropped.

func TestMpN6keFileVersionKeepsClimbingAcrossCompetitionDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "recycled"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Recycled"}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: MatchStatusScheduled},
	}))

	beforeDelete := store.FileVersion(compID, "pool-matches.csv")
	require.Positive(t, beforeDelete, "a write must have moved the counter")

	require.NoError(t, store.DeleteCompetition(compID))

	afterDelete := store.FileVersion(compID, "pool-matches.csv")
	assert.Greater(t, afterDelete, beforeDelete,
		"deleting a competition must advance the counter, not reset it: the ID is a name slug and can be recreated")

	// Recreate under the same ID, exactly what an operator retyping the same
	// competition name produces. The counter must not fall back to where the
	// deleted competition started.
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Recycled"}))
	assert.GreaterOrEqual(t, store.FileVersion(compID, "pool-matches.csv"), afterDelete,
		"a recreated competition must not inherit the deleted one's starting tokens")
}

// TestMpN6keDeleteStillDropsCachedBodies guards the other half: keeping the
// fileCache struct alive for its counter must not keep the parsed body alive
// too. loadCached validates on mtime alone, and mtime is quantized to ~1ms, so a
// recreated competition writing the same filename inside one tick would present
// an identical UnixNano and hit the dead competition's body.
func TestMpN6keDeleteStillDropsCachedBodies(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "recycled-body"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Recycled Body"}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "Pool A-0", SideA: "OLD-A", SideB: "OLD-B", Status: MatchStatusScheduled},
	}))
	// Read once so the body is definitely cached, not just written through.
	warm, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, warm, 1)
	require.Equal(t, "OLD-A", warm[0].SideA)

	require.NoError(t, store.DeleteCompetition(compID))

	// No sleep before recreating: the point is that this must hold even when the
	// recreate lands in the same clock tick as the delete.
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Recycled Body"}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "Pool A-0", SideA: "NEW-A", SideB: "NEW-B", Status: MatchStatusScheduled},
	}))

	got, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "NEW-A", got[0].SideA,
		"a recreated competition must not read the deleted competition's cached body")
}
