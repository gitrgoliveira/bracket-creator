package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCourtsEqual exercises every branch of the nil/empty/equal/unequal
// logic. Both nil and empty slice must compare as equal ("no courts"
// from the config's point of view).
func TestCourtsEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []string{}, []string{}, true},
		{"nil vs empty", nil, []string{}, true},
		{"empty vs nil", []string{}, nil, true},
		{"equal single", []string{"A"}, []string{"A"}, true},
		{"equal multiple", []string{"A", "B", "C"}, []string{"A", "B", "C"}, true},
		{"different lengths", []string{"A", "B"}, []string{"A"}, false},
		{"different values", []string{"A", "B"}, []string{"A", "C"}, false},
		{"one nil one single", nil, []string{"A"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, courtsEqual(tc.a, tc.b))
		})
	}
}

// TestMaybeAutoCompletePools_AllComplete verifies that calling
// MaybeAutoCompletePools on a pools competition whose every match is
// completed transitions Status to "completed" and returns true.
func TestMaybeAutoCompletePools_AllComplete(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-all"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Auto Complete Test",
		Format: state.CompFormatPools,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	matches := []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "P1-1", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusCompleted, Winner: "Charlie"},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	changed, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.True(t, changed, "all matches completed should trigger status transition")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status)
}

// TestMaybeAutoCompletePools_OnePending verifies that a single
// scheduled/running match prevents the auto-complete transition.
func TestMaybeAutoCompletePools_OnePending(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-pending"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Pending Test",
		Format: state.CompFormatPools,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	matches := []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "P1-1", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	changed, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.False(t, changed, "a pending match must prevent auto-complete")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status, "status should remain Pools")
}

// TestMaybeAutoCompletePools_NonPoolsFormat verifies no-op for
// competitions that are not in the Pools format.
func TestMaybeAutoCompletePools_NonPoolsFormat(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-playoffs"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Playoffs Test",
		Format: state.CompFormatPlayoffs,
		Status: state.CompStatusPlayoffs,
		Courts: []string{"A"},
	}))
	// No pool matches on disk — all pool match loads return empty.

	changed, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	// No matches at all → outer fast-path sees "all complete" (vacuously true),
	// but the inner transform guard rejects CompFormatPlayoffs → no transition.
	assert.False(t, changed, "non-pools format must not trigger auto-complete")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPlayoffs, comp.Status)
}

// TestMaybeAutoCompletePools_AlreadyComplete verifies idempotency:
// calling the function on an already-completed competition must return
// false (no change), not error.
func TestMaybeAutoCompletePools_AlreadyComplete(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-idempotent"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Already Done",
		Format: state.CompFormatPools,
		Status: state.CompStatusComplete,
		Courts: []string{"A"},
	}))

	matches := []state.MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	changed, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.False(t, changed, "already-completed competition must return changed=false")
}
