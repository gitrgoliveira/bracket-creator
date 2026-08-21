package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// An incomplete seeding MUST stay visible to the operator.
//
// The seeding panel persists each rank the moment it is typed, so an operator
// working down their list who enters seed 4 first leaves seeds.csv holding rank
// 4 alone. That is not a corrupt file and not a hand-edit: it is the normal
// intermediate state of the tool's own UI.
//
// It used to disappear. The participants read validated, got "seed ranks must be
// sequential without gaps", discarded the result, and reported zero seeded
// players. The console's own warning ("seed gap detected: rank 1, 2, 3 are
// missing") could not fire, because from its point of view there were no seeds
// at all -- and the next edit wrote that empty view back over the file, losing
// what had been entered.
func seedStoreWithGappedFile(t *testing.T) (*state.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, s.SaveCompetition(&state.Competition{
		ID: "c1", Name: "C1", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2,
	}))
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"}, {Name: "Dave"},
	}))
	// Exactly what SaveSeeds writes when only rank 4 has been entered.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "c1", "seeds.csv"),
		[]byte("Rank,Name\n4,Dave\n"), 0o600))
	return s, dir
}

func TestIncompleteSeedingStaysVisibleToTheOperator(t *testing.T) {
	s, _ := seedStoreWithGappedFile(t)

	players, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)

	seeded := map[string]int{}
	for _, p := range players {
		if p.Seed > 0 {
			seeded[p.Name] = p.Seed
		}
	}
	assert.Equal(t, map[string]int{"Dave": 4}, seeded,
		"the rank the operator typed must come back, or the console cannot warn them it is incomplete")
}

func TestLoadSeedsRawShowsWhatLoadSeedsRefuses(t *testing.T) {
	s, _ := seedStoreWithGappedFile(t)

	// The SHOW path answers.
	raw, err := s.LoadSeedsRaw("c1")
	require.NoError(t, err, "GET /seeds must not answer an incomplete seeding with a 500")
	assert.Equal(t, []domain.SeedAssignment{{Name: "Dave", SeedRank: 4}}, raw)

	// The USE path still refuses, and says why.
	_, err = s.LoadSeeds("c1")
	require.Error(t, err, "an incomplete seeding must never be handed to something that draws with it")
	assert.ErrorIs(t, err, domain.ErrInvalidSeedAssignments)
	assert.Contains(t, err.Error(), "sequential without gaps")
}

// Renaming a competitor must work while the seeding is half-entered, and must
// carry the rename into seeds.csv. Refusing would strand seeds.csv naming
// somebody who no longer exists under that name.
func TestRenameWorksWhileSeedingIsIncomplete(t *testing.T) {
	s, dir := seedStoreWithGappedFile(t)

	players, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)
	var daveID string
	for _, p := range players {
		if p.Name == "Dave" {
			daveID = p.ID
		}
	}
	require.NotEmpty(t, daveID, "fixture must give Dave an id to update by")

	_, err = s.UpdateParticipant("c1", daveID, false, func(p *domain.Player) error {
		p.Name = "David"
		return nil
	})
	require.NoError(t, err, "a rename must not be blocked by an unfinished seeding")

	got, err := os.ReadFile(filepath.Join(dir, "competitions", "c1", "seeds.csv"))
	require.NoError(t, err)
	assert.Contains(t, string(got), "David",
		"the rename must reach seeds.csv, or it names a competitor who no longer exists")
}
