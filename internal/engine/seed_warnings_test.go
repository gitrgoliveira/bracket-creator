package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// seedWarningsByID is the id-taking shape SeedWarningsFor deliberately does not
// have: its one production caller already holds the record when it asks, so an
// exported wrapper would be reachable only from here. Keeping the convenience in
// the test package means a reader of the engine's API cannot mistake it for a
// supported second entry point.
//
// LoadCompetition answers a missing competition with (nil, nil), so the
// no-such-competition case below still travels the real path -- through
// SeedWarningsFor's own nil guard -- rather than being short-circuited here.
func seedWarningsByID(t *testing.T, eng *Engine, id string) []string {
	t.Helper()
	comp, err := eng.store.LoadCompetition(id)
	require.NoError(t, err)
	return eng.SeedWarningsFor(comp)
}

// Engine.SeedWarningsFor is reached in production only through the mobile app's
// draw-warnings endpoint, so its own package had no test for it and every
// statement in it was uncovered. The contract worth pinning here is the one D7
// insists on: this is a WARNING channel and it never fails a caller. Each of
// the four early returns below is a state an operator can actually be in, and
// all four must come back as "nothing to say" rather than as an error or a
// panic.
func TestSeedWarningsReturnsNilRatherThanFailing(t *testing.T) {
	t.Run("competition does not exist", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		assert.Nil(t, seedWarningsByID(t, eng, "no-such-competition"))
	})

	t.Run("competition exists but has no pools yet", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "warnings-no-pools"
		createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
		assert.Nil(t, seedWarningsByID(t, eng, compID),
			"the draw has not been generated, so there is nothing to report on")
	})

	t.Run("pools drawn with no seeds at all", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "warnings-no-seeds"
		createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
		saveTestParticipants(t, store, compID,
			[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})
		require.NoError(t, eng.GenerateDraw(compID))

		assert.Empty(t, seedWarningsByID(t, eng, compID),
			"an unseeded competition is a normal configuration and MUST be warning-free")
	})
}

// A format with no bracket has nothing to warn about. Every warning this
// channel can produce describes where a seed's qualifier sits in a KNOCKOUT
// bracket, so a league or a Swiss must be silent: the admin console renders
// these under "the draw could not honour every rule", and a league organiser
// who seeded two competitors was being shown "Seed 2 ignored ... The draw used
// seed 1" about a draw that never gets built.
func TestSeedWarningsAreSilentForFormatsWithoutABracket(t *testing.T) {
	for _, format := range []string{state.CompFormatLeague, state.CompFormatSwiss} {
		t.Run(format, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "warnings-" + format

			createTestCompetition(t, store, compID, format, 4)
			saveTestParticipants(t, store, compID,
				[]string{"Alice", "Bob", "Charlie", "Dave"})
			// Two seeds in one pool is the loudest thing this channel says, and
			// it is exactly the sentence that made no sense for a league.
			require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
				{Name: "Alice", SeedRank: 1},
				{Name: "Bob", SeedRank: 2},
			}))
			require.NoError(t, eng.GenerateDraw(compID))

			assert.Empty(t, seedWarningsByID(t, eng, compID),
				"a %s has no bracket, so there is no seed placement to report on", format)
		})
	}
}

// The other half of the contract: when the configuration genuinely cannot honour
// a seed, the operator is TOLD, and the draw still happens.
//
// Four seeds over three pools is the case R2 names outright: two seeds may never
// share a pool, so the fourth rank has nowhere to go and is ignored. D7 requires
// that to degrade with a warning rather than refuse, because a live event has no
// time for a hard failure and the operator can always move a seed by hand.
func TestSeedWarningsReportsSurplusSeedRanks(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "warnings-surplus-seeds"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID,
		[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank", "Grace", "Heidi", "Ivan"})
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1},
		{Name: "Bob", SeedRank: 2},
		{Name: "Charlie", SeedRank: 3},
		{Name: "Dave", SeedRank: 4},
	}))

	require.NoError(t, eng.GenerateDraw(compID),
		"a seeding constraint that cannot be met must never refuse the draw (D7)")

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, pools, 3, "the fixture must produce fewer pools than seeds for this to bite")

	warnings := seedWarningsByID(t, eng, compID)
	assert.NotEmpty(t, warnings,
		"4 seeds over 3 pools cannot all be placed, and the operator has to be told")
	for _, w := range warnings {
		assert.NotEmpty(t, w, "an empty warning string tells the operator nothing")
	}
}
