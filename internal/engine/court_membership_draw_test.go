package engine

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Orphaned-shiaijo gate at the engine's draw entry point (bc-draw R9 UAT
// gap 3). Reducing the tournament's court count used to leave every
// competition's own allocation untouched, so a competition assigned A–D kept
// D after the venue shrank to A–C, and the draw happily placed matches on a
// shiaijo with no operator view (/admin/shiaijo/:court is built from the
// tournament's list) — invisible matches for the whole event.
//
// The tournament-side refusal (handlers_tournament.go) stops that being
// created through the UI. This gate is what covers records ALREADY in that
// state: saved before the guard existed, imported, or hand-edited. Same
// placement as the pairing gate in runDrawPipeline, so it catches GenerateDraw
// and StartCompetition alike.

func saveVenue(t *testing.T, store *state.Store, courts ...string) {
	t.Helper()
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Venue Cup", Courts: courts}))
}

func TestDrawPipelineRejectsShiaijoTheTournamentLacks(t *testing.T) {
	roster := []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"}

	// Every format, not just the bracket-drawing ones: a league match on a
	// court no operator can open is just as invisible as a knockout one.
	for _, format := range []string{
		state.CompFormatMixed,
		state.CompFormatPlayoffs,
		state.CompFormatLeague,
		state.CompFormatSwiss,
	} {
		t.Run(format, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			saveVenue(t, store, "A", "B", "C")
			createTestCompetition(t, store, "orphan", format, 4, func(c *state.Competition) {
				// A pairable count (4) so a failure here can only be the
				// membership rule, never the pairing rule.
				c.Courts = []string{"A", "B", "C", "D"}
				c.SwissRounds = 3
			})
			saveTestParticipants(t, store, "orphan", roster)

			err := eng.GenerateDraw("orphan")
			require.Error(t, err, "a draw onto a shiaijo the tournament lacks must be refused")
			assert.Contains(t, err.Error(), "shiaijo D is not part of this tournament")
			assert.Contains(t, err.Error(), "the tournament has A, B, C")

			var vErr *ValidationError
			assert.True(t, errors.As(err, &vErr),
				"an orphaned allocation is an operator input error (HTTP 400), not a 500")

			comp, loadErr := store.LoadCompetition("orphan")
			require.NoError(t, loadErr)
			assert.Equal(t, state.CompStatusSetup, comp.Status,
				"a refused draw must leave the competition in setup")
		})
	}
}

func TestStartCompetitionRejectsShiaijoTheTournamentLacks(t *testing.T) {
	// The one-click path runs the same pipeline, so the rule cannot be
	// bypassed by skipping the explicit Generate draw step.
	eng, store, _ := setupTestEngine(t)
	saveVenue(t, store, "A", "B", "C")
	createTestCompetition(t, store, "start-orphan", state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = []string{"A", "B", "C", "D"}
	})
	saveTestParticipants(t, store, "start-orphan", []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"})

	err := eng.StartCompetition("start-orphan")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shiaijo D is not part of this tournament")
}

func TestDrawPipelineAcceptsASubsetOfTheTournamentsShiaijo(t *testing.T) {
	// The normal case must stay unaffected: a competition allocated some of
	// the venue's courts draws exactly as before.
	eng, store, _ := setupTestEngine(t)
	saveVenue(t, store, "A", "B", "C", "D")
	createTestCompetition(t, store, "subset", state.CompFormatPlayoffs, 4, func(c *state.Competition) {
		c.Courts = []string{"A", "B"}
	})
	saveTestParticipants(t, store, "subset", []string{"P1", "P2", "P3", "P4"})
	require.NoError(t, eng.GenerateDraw("subset"))
}

func TestDrawPipelineSkipsMembershipWithoutATournament(t *testing.T) {
	// No tournament.md yet (bootstrap, and the shape most engine tests run
	// in): "unknown court list", not "the venue has no courts". Refusing here
	// would block every draw in that window.
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "no-tourn", state.CompFormatPlayoffs, 4, func(c *state.Competition) {
		c.Courts = []string{"A", "B"}
	})
	saveTestParticipants(t, store, "no-tourn", []string{"P1", "P2", "P3", "P4"})
	require.NoError(t, eng.GenerateDraw("no-tourn"))
}
