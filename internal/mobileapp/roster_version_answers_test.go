package mobileapp

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The seeds a roster replace prunes are the ones stored when its locked step
// runs: a seed another desk set after the caller read them is kept (stage 6
// review 13, m7), where a list read before the lock written back over them
// lost it.
func TestReplaceRosterKeepingCheckIns_PrunesTheSeedsStoredWhenItRuns(t *testing.T) {
	_, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const compID = "comp-replace-prunes-current-seeds"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Prune", Status: state.CompStatusSetup}))
	alice := domain.Player{Name: "Alice Smith", Dojo: "Wakaba"}
	bob := domain.Player{Name: "Bob Jones", Dojo: "Tora"}
	carol := domain.Player{Name: "Carol Ito", Dojo: "Hana"}
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{alice, bob, carol}))
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: alice.Name, Dojo: alice.Dojo, SeedRank: 1},
		{Name: bob.Name, Dojo: bob.Dojo, SeedRank: 2},
	}))

	// The caller's read, taken before the lock.
	stale, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	require.Len(t, stale, 2)

	// Another desk seeds Carol after that read.
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: alice.Name, Dojo: alice.Dojo, SeedRank: 1},
		{Name: bob.Name, Dojo: bob.Dojo, SeedRank: 2},
		{Name: carol.Name, Dojo: carol.Dojo, SeedRank: 3},
	}))

	// Alice is taken off the list; her seed goes with her, Carol's stays.
	remaining := []domain.Player{bob, carol}
	seedsErr, err := store.ReplaceRosterKeepingCheckIns(compID, remaining,
		func(_ []domain.Player, current []domain.SeedAssignment) []domain.SeedAssignment {
			_, kept := seedsOrphanedByRosterReplace([]domain.Player{alice, bob, carol}, remaining, current)
			return kept
		})
	require.NoError(t, err)
	require.NoError(t, seedsErr)

	seeds, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	byName := map[string]int{}
	for _, s := range seeds {
		byName[s.Name] = s.SeedRank
	}
	assert.NotContains(t, byName, alice.Name, "the removed competitor's seed is dropped")
	assert.Equal(t, 2, byName[bob.Name])
	assert.Equal(t, 3, byName[carol.Name], "the seed set after the caller's read is kept")
}
