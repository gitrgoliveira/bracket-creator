package state_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The seeds.csv-onto-roster merge in loadParticipants must key the way the
// matchers match: on domain.SeedKey (name, dojo), with the shared bare-name
// fallback only for a legacy no-dojo row naming a UNIQUE participant. Keyed on
// the name alone -- as it was before the Dojo column existed -- a seed for
// "Yuki Tanaka" of Seibukan attached to BOTH Yuki Tanakas, so the console
// displayed a seeding that domain.AssignSeeds would refuse to draw.
func TestSeedMergeResolvesSameNamePlayersByDojo(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{
		ID: "c1", Name: "C1", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2,
	}))
	// Two same-name players in different dojos: legal (only same name AND
	// same dojo is rejected) and the case the Dojo column exists for.
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{Name: "Yuki Tanaka", Dojo: "Seibukan"},
		{Name: "Yuki Tanaka", Dojo: "Tobukan"},
		{Name: "Rin Sato", Dojo: "Seibukan"},
	}))
	require.NoError(t, s.SaveSeeds("c1", []domain.SeedAssignment{
		{Name: "Yuki Tanaka", Dojo: "Tobukan", SeedRank: 1},
		{Name: "Rin Sato", SeedRank: 2}, // legacy shape: no dojo, unique name
	}))

	// Fresh store so the read comes from disk, not the participants cache.
	fresh, err := state.NewStore(dir)
	require.NoError(t, err)
	players, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, players, 3)

	seedOf := map[string]int{}
	for _, p := range players {
		seedOf[domain.SeedKey(p.Name, p.Dojo)] = p.Seed
	}
	assert.Equal(t, 0, seedOf[domain.SeedKey("Yuki Tanaka", "Seibukan")],
		"the seed names the Tobukan Tanaka; attaching it to the namesake is the pre-Dojo bug")
	assert.Equal(t, 1, seedOf[domain.SeedKey("Yuki Tanaka", "Tobukan")])
	assert.Equal(t, 2, seedOf[domain.SeedKey("Rin Sato", "Seibukan")],
		"a legacy no-dojo row naming a unique participant must keep attaching by name")
}
