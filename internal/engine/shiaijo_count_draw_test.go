package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDrawPipelineRejectsIllegalShiaijoCount sweeps the shiaijo-count rule at
// the engine's draw entry point, which is where a draw generated outside the
// HTTP layer would otherwise slip through. runDrawPipeline is shared by
// GenerateDraw and StartCompetition, so both are covered; the sweep asserts
// GenerateDraw and spot-checks StartCompetition below.
//
// The sweep runs 1..17, so it covers the counts the retired "1 or an even
// number" rule wrongly ACCEPTED (6, 10, 12, 14) as well as the odd ones.
//
// 32 players keeps every court count inside the other allocation guard
// (ValidateCourtCount caps a single pool at floor(N/2) courts), so a failure
// here is the shiaijo-count rule and nothing else.
func TestDrawPipelineRejectsIllegalShiaijoCount(t *testing.T) {
	players := make([]string, 32)
	for i := range players {
		players[i] = fmt.Sprintf("P%02d", i+1)
	}

	for _, format := range []string{state.CompFormatMixed, state.CompFormatPlayoffs} {
		for n := 1; n <= helper.MaxCourts; n++ {
			t.Run(fmt.Sprintf("%s/courts=%d", format, n), func(t *testing.T) {
				eng, store, _ := setupTestEngine(t)
				compID := fmt.Sprintf("shiaijo-%s-%d", format, n)
				createTestCompetition(t, store, compID, format, 4, func(c *state.Competition) {
					c.Courts = courtLabels(n)
				})
				saveTestParticipants(t, store, compID, players)

				err := eng.GenerateDraw(compID)
				if bctest.LegalShiaijoCount(n) {
					require.NoErrorf(t, err, "%d shiaijo must draw", n)
					return
				}
				require.Errorf(t, err, "%d shiaijo must be refused", n)
				assert.Contains(t, err.Error(), "shiaijo count must be a power of two")
				var vErr *ValidationError
				assert.True(t, errors.As(err, &vErr),
					"an illegal allocation is an operator input error (HTTP 400), not a 500")

				// Nothing was written: the gate runs before generation.
				comp, loadErr := store.LoadCompetition(compID)
				require.NoError(t, loadErr)
				assert.Equal(t, state.CompStatusSetup, comp.Status,
					"a refused draw must leave the competition in setup")
			})
		}
	}
}

// TestStartCompetitionRejectsIllegalShiaijoCount pins the one-click path
// (StartCompetition on a setup competition runs the same pipeline), so the
// rule cannot be bypassed by skipping the explicit Generate draw step.
func TestStartCompetitionRejectsIllegalShiaijoCount(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "start-three", state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = []string{"A", "B", "C"}
	})
	saveTestParticipants(t, store, "start-three", []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"})

	err := eng.StartCompetition("start-three")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use 2 or 4, or 1")
}

// TestStartCompetitionRejectsEvenNonPowerOfTwo is the specific regression for
// the rule change: 6 shiaijo passed the old rule. It must now be refused, and
// the suggestion must be 4 or 8 rather than the old rule's 5 or 7.
func TestStartCompetitionRejectsEvenNonPowerOfTwo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "start-six", state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = courtLabels(6)
	})
	saveTestParticipants(t, store, "start-six",
		[]string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"})

	err := eng.StartCompetition("start-six")
	require.Error(t, err, "6 shiaijo is even but not a power of two")
	assert.Contains(t, err.Error(), "use 4 or 8, or 1")
}

// TestDrawPipelineIgnoresShiaijoCountForNonBracketFormats pins the SCOPE of
// the rule. League and Swiss draw pools or rounds, never a bracket, so there
// are no blocks to merge and any allocation is legitimate. This is not
// incidental: SuggestedMaxCourts (the count the league court hint recommends)
// is floor(N/2)-1, which is rarely a power of two, so a format-blind rule
// would reject the app's own recommendation.
func TestDrawPipelineIgnoresShiaijoCountForNonBracketFormats(t *testing.T) {
	t.Run("league on 3 shiaijo draws", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		createTestCompetition(t, store, "league-three", state.CompFormatLeague, 6, func(c *state.Competition) {
			c.Courts = []string{"A", "B", "C"}
		})
		saveTestParticipants(t, store, "league-three", []string{"P1", "P2", "P3", "P4", "P5", "P6"})
		require.NoError(t, eng.GenerateDraw("league-three"))
	})

	t.Run("swiss on 3 shiaijo draws", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		createTestCompetition(t, store, "swiss-three", state.CompFormatSwiss, 4, func(c *state.Competition) {
			c.Courts = []string{"A", "B", "C"}
			c.SwissRounds = 3
		})
		saveTestParticipants(t, store, "swiss-three", []string{"P1", "P2", "P3", "P4", "P5", "P6"})
		require.NoError(t, eng.GenerateDraw("swiss-three"))
	})

	t.Run("suggested league court count need not be a power of two", func(t *testing.T) {
		// The concrete case the format scope protects: 8 players suggests 3.
		assert.Equal(t, 3, SuggestedMaxCourts(8))
		assert.Error(t, helper.ValidateShiaijoCount(SuggestedMaxCourts(8)),
			"the suggestion is illegal for a bracket, which is exactly why leagues are out of scope")
	})
}

// TestCompetitionDrawsBracket pins which formats the rule binds. The unset
// format matters: the draw pipeline's default branch builds a standalone
// playoffs bracket for it, so it must be in scope even though
// state.Competition.IsPlayoffEnabled answers false for the same value.
func TestCompetitionDrawsBracket(t *testing.T) {
	t.Parallel()

	tests := []struct {
		format string
		draws  bool
	}{
		{state.CompFormatMixed, true},
		{state.CompFormatPlayoffs, true},
		{"", true}, // pipeline default branch generates playoffs
		{state.CompFormatLeague, false},
		{state.CompFormatSwiss, false},
	}
	for _, tt := range tests {
		t.Run("format="+tt.format, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.draws, CompetitionDrawsBracket(tt.format))
		})
	}

	// The divergence from IsPlayoffEnabled is deliberate; pin it so a future
	// "why are there two predicates?" cleanup has to read the reason first.
	assert.False(t, (state.Competition{Format: ""}).IsPlayoffEnabled(),
		"IsPlayoffEnabled is a UI-affordance predicate and excludes the unset format")
	assert.True(t, CompetitionDrawsBracket(""),
		"the draw pipeline still builds a bracket for an unset format")
}

// TestDrawPipelineSkipsShiaijoCountWithoutCourts covers the competition that
// carries no explicit allocation at all: the generators read that as one
// court, so there is nothing to reject.
func TestDrawPipelineSkipsShiaijoCountWithoutCourts(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "no-courts", state.CompFormatPlayoffs, 4, func(c *state.Competition) {
		c.Courts = nil
	})
	saveTestParticipants(t, store, "no-courts", []string{"P1", "P2", "P3", "P4"})
	require.NoError(t, eng.GenerateDraw("no-courts"))
}
