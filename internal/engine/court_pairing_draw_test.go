package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDrawPipelineRejectsUnpairableShiaijo sweeps the shiaijo-count rule at
// the engine's draw entry point, which is where a draw generated outside the
// HTTP layer would otherwise slip through. runDrawPipeline is shared by
// GenerateDraw and StartCompetition, so both are covered; the sweep asserts
// GenerateDraw and spot-checks StartCompetition below.
//
// 32 players keeps every court count inside the other allocation guard
// (ValidateCourtCount caps a single pool at floor(N/2) courts), so a failure
// here is the pairing rule and nothing else.
func TestDrawPipelineRejectsUnpairableShiaijo(t *testing.T) {
	players := make([]string, 32)
	for i := range players {
		players[i] = fmt.Sprintf("P%02d", i+1)
	}

	for _, format := range []string{state.CompFormatMixed, state.CompFormatPlayoffs} {
		for n := 1; n <= 8; n++ {
			valid := n == 1 || n%2 == 0
			t.Run(fmt.Sprintf("%s/courts=%d", format, n), func(t *testing.T) {
				eng, store, _ := setupTestEngine(t)
				compID := fmt.Sprintf("pairing-%s-%d", format, n)
				createTestCompetition(t, store, compID, format, 4, func(c *state.Competition) {
					c.Courts = courtLabels(n)
				})
				saveTestParticipants(t, store, compID, players)

				err := eng.GenerateDraw(compID)
				if valid {
					require.NoErrorf(t, err, "%d shiaijo must draw", n)
					return
				}
				require.Errorf(t, err, "%d shiaijo must be refused", n)
				assert.Contains(t, err.Error(), "courts must be 1 or an even number")
				assert.Contains(t, err.Error(), fmt.Sprintf("use %d or %d, or 1", n-1, n+1))
				var vErr *ValidationError
				assert.True(t, errors.As(err, &vErr),
					"an unpairable allocation is an operator input error (HTTP 400), not a 500")

				// Nothing was written: the gate runs before generation.
				comp, loadErr := store.LoadCompetition(compID)
				require.NoError(t, loadErr)
				assert.Equal(t, state.CompStatusSetup, comp.Status,
					"a refused draw must leave the competition in setup")
			})
		}
	}
}

// TestStartCompetitionRejectsUnpairableShiaijo pins the one-click path
// (StartCompetition on a setup competition runs the same pipeline), so the
// rule cannot be bypassed by skipping the explicit Generate draw step.
func TestStartCompetitionRejectsUnpairableShiaijo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "start-odd", state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = []string{"A", "B", "C"}
	})
	saveTestParticipants(t, store, "start-odd", []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"})

	err := eng.StartCompetition("start-odd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use 2 or 4, or 1")
}

// TestDrawPipelineIgnoresPairingForNonBracketFormats pins the SCOPE of the
// rule. League and Swiss draw pools or rounds, never a bracket, so there are
// no court regions to pair and an odd allocation is legitimate. This is not
// incidental: SuggestedMaxCourts (the count the league court hint recommends)
// is floor(N/2)-1, which is odd for half of all rosters, so a format-blind
// rule would reject the app's own recommendation.
func TestDrawPipelineIgnoresPairingForNonBracketFormats(t *testing.T) {
	t.Run("league on 3 shiaijo draws", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		createTestCompetition(t, store, "league-odd", state.CompFormatLeague, 6, func(c *state.Competition) {
			c.Courts = []string{"A", "B", "C"}
		})
		saveTestParticipants(t, store, "league-odd", []string{"P1", "P2", "P3", "P4", "P5", "P6"})
		require.NoError(t, eng.GenerateDraw("league-odd"))
	})

	t.Run("swiss on 3 shiaijo draws", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		createTestCompetition(t, store, "swiss-odd", state.CompFormatSwiss, 4, func(c *state.Competition) {
			c.Courts = []string{"A", "B", "C"}
			c.SwissRounds = 3
		})
		saveTestParticipants(t, store, "swiss-odd", []string{"P1", "P2", "P3", "P4", "P5", "P6"})
		require.NoError(t, eng.GenerateDraw("swiss-odd"))
	})

	t.Run("suggested league court count may be odd", func(t *testing.T) {
		// The concrete case the format scope protects: 8 players suggests 3.
		assert.Equal(t, 3, SuggestedMaxCourts(8))
		assert.Error(t, ValidateCourtPairing(SuggestedMaxCourts(8)),
			"the suggestion is unpairable, which is exactly why leagues are out of scope")
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

// TestDrawPipelineSkipsPairingWithoutCourts covers the competition that
// carries no explicit allocation at all: the generators read that as one
// court, so there is nothing to reject.
func TestDrawPipelineSkipsPairingWithoutCourts(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "no-courts", state.CompFormatPlayoffs, 4, func(c *state.Competition) {
		c.Courts = nil
	})
	saveTestParticipants(t, store, "no-courts", []string{"P1", "P2", "P3", "P4"})
	require.NoError(t, eng.GenerateDraw("no-courts"))
}
