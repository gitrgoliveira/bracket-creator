package state_test

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveSeeds_RefusesNonEmptyWithoutRoster pins the operator ruling (bc-sdid):
// a competitor list must exist to define seeds. SaveSeeds is the one door
// every seeds.csv write goes through, so the refusal lives here rather than
// in any one caller.
func TestSaveSeeds_RefusesNonEmptyWithoutRoster(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	err = s.SaveSeeds("c1", []domain.SeedAssignment{{Name: "Alice", SeedRank: 1}})
	require.Error(t, err)
	assert.ErrorIs(t, err, state.ErrSeedsWithoutRoster)
	assert.True(t, errors.Is(err, state.ErrSeedsWithoutRoster), "handlers classify on this sentinel")

	seeds, err := s.LoadSeedsRaw("c1")
	require.NoError(t, err)
	assert.Empty(t, seeds, "a refused seeding must not reach seeds.csv")
}

// TestSaveSeeds_AllowsEmptyWithoutRoster: clearing a seeding must never
// require a roster to exist -- there is nothing to attach and nothing the
// operator could usefully be refused.
func TestSaveSeeds_AllowsEmptyWithoutRoster(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	require.NoError(t, s.SaveSeeds("c1", []domain.SeedAssignment{}))

	seeds, err := s.LoadSeedsRaw("c1")
	require.NoError(t, err)
	assert.Empty(t, seeds)
}

// TestSaveSeeds_StampsParticipantID: SaveSeeds resolves each assignment
// against the roster it already loaded to run the refusal check above, and
// stamps the resolved participant's id, so every caller that writes through
// SaveSeeds gets the id column filled in for free.
func TestSaveSeeds_StampsParticipantID(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	added, err := s.AddParticipant("c1", domain.Player{Name: "Alice", Dojo: "Wakaba"}, false)
	require.NoError(t, err)
	require.NotEmpty(t, added.ID)

	// The caller supplies no id at all -- exactly what every real writer
	// (the seeding panel, extractSeeds, import) does today.
	require.NoError(t, s.SaveSeeds("c1", []domain.SeedAssignment{
		{Name: "Alice", Dojo: "Wakaba", SeedRank: 1},
	}))

	seeds, err := s.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, added.ID, seeds[0].ID, "SaveSeeds must stamp the id it resolved the row to")
}

// TestSeedMergeResolvesByIDEvenWhenNameIsStale is the id-first integration
// test for the seeds.csv-onto-roster merge (state.loadParticipants): a seed
// row's stored Name/Dojo can drift out of date (the row is not rewritten on
// every rename -- see updateParticipantNoLock's own doc comment), but once
// the row carries an id, that id is authoritative and must win over whoever
// currently happens to hold the stale name.
func TestSeedMergeResolvesByIDEvenWhenNameIsStale(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))

	alice, err := s.AddParticipant("c1", domain.Player{Name: "Alice Original", Dojo: "Old Dojo"}, false)
	require.NoError(t, err)

	// Hand-write a seed row carrying Alice's real id but her STALE identity
	// (as if she had been renamed without going through the seeds.csv
	// rewrite -- e.g. a hand-edited file, or a build that predates it).
	require.NoError(t, s.SaveSeeds("c1", []domain.SeedAssignment{
		{ID: alice.ID, Name: "Alice Original", Dojo: "Old Dojo", SeedRank: 1},
	}))

	// Rename Alice on the roster WITHOUT going through UpdateParticipant's
	// seeds.csv rewrite, to simulate a row the rewrite has not (yet) reached,
	// and to free up the (name, dojo) pair the seed row still carries.
	loaded, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)
	for i := range loaded {
		if loaded[i].ID == alice.ID {
			loaded[i].Name = "Alice Renamed"
			loaded[i].Dojo = "New Dojo"
		}
	}
	require.NoError(t, s.SaveParticipants("c1", loaded))

	// A SECOND, unrelated participant now takes the exact (name, dojo) pair
	// the seed row still carries. A name/dojo fallback would misattribute
	// the seed to THIS participant instead.
	impersonator, err := s.AddParticipant("c1", domain.Player{Name: "Alice Original", Dojo: "Old Dojo"}, false)
	require.NoError(t, err)

	players, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)
	seedOf := map[string]int{}
	for _, p := range players {
		seedOf[p.ID] = p.Seed
	}
	assert.Equal(t, 1, seedOf[alice.ID], "the id must resolve the seed to Alice under her NEW identity")
	assert.Equal(t, 0, seedOf[impersonator.ID], "the impersonator holding the stale name/dojo must NOT inherit the seed")
}
