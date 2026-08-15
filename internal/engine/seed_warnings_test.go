package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Engine.SeedWarnings is reached in production only through the mobile app's
// draw-warnings endpoint, so its own package had no test for it and every
// statement in it was uncovered. The contract worth pinning here is the one D7
// insists on: this is a WARNING channel and it never fails a caller. Each of
// the four early returns below is a state an operator can actually be in, and
// all four must come back as "nothing to say" rather than as an error or a
// panic.
func TestSeedWarningsReturnsNilRatherThanFailing(t *testing.T) {
	t.Run("competition does not exist", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		assert.Nil(t, eng.SeedWarnings("no-such-competition"))
	})

	t.Run("competition exists but has no pools yet", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "warnings-no-pools"
		createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
		assert.Nil(t, eng.SeedWarnings(compID),
			"the draw has not been generated, so there is nothing to report on")
	})

	t.Run("pools drawn with no seeds at all", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "warnings-no-seeds"
		createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
		saveTestParticipants(t, store, compID,
			[]string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})
		require.NoError(t, eng.GenerateDraw(compID))

		assert.Empty(t, eng.SeedWarnings(compID),
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

			assert.Empty(t, eng.SeedWarnings(compID),
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

	warnings := eng.SeedWarnings(compID)
	assert.NotEmpty(t, warnings,
		"4 seeds over 3 pools cannot all be placed, and the operator has to be told")
	for _, w := range warnings {
		assert.NotEmpty(t, w, "an empty warning string tells the operator nothing")
	}
}

// TestDrawnCourtCount pins the source these warnings take their shiaijo count
// from: the shiaijo the drawn pools' matches actually run on, not comp.Courts.
//
// comp.Courts stays editable while a competition runs (a shiaijo can be dropped
// once its bouts are all fought), and these warnings describe where a seed's
// qualifier LANDED. Reading the live field would let a late edit rewrite the
// account of a draw that was already made.
//
// The end-to-end effect is narrow -- most warnings are about two seeds sharing a
// POOL, which no court count can change, and the one shiaijo-spread warning is
// itself gated on there being at least as many shiaijo as seeds -- so this pins
// the mechanism directly rather than through a shape that happens to expose it.
func TestDrawnCourtCount(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "drawn-court-count"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.PoolSizeMode = "max"
		c.PoolWinners = 2
		c.Courts = courtLabels(4)
	})

	assert.Zero(t, eng.drawnCourtCount(compID),
		"before the draw there are no pool matches, so there is no drawn allocation to report")

	require.NoError(t, store.SaveParticipants(compID, playoffsParityRoster(16)))
	require.NoError(t, eng.StartCompetition(compID))

	assert.Equal(t, 4, eng.drawnCourtCount(compID),
		"the pools were drawn across four shiaijo")

	// Shrinking the stored allocation must not change the answer: the matches on
	// disk still run where they were drawn.
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Courts = courtLabels(2)
	require.NoError(t, store.SaveCompetition(comp))

	assert.Equal(t, 4, eng.drawnCourtCount(compID),
		"editing the court field must not rewrite what the draw was built on")
}
