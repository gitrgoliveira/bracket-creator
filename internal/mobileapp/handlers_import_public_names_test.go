package mobileapp

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An imported competition starts with the tournament's default public-names
// option, as one made through POST /competitions does: not a Swiss one, and
// not in a self-run tournament.
func TestImportCompetition_StartsWithTheTournamentDefaultPublicNames(t *testing.T) {
	newStore := func(t *testing.T, mode string) *state.Store {
		t.Helper()
		store, err := state.NewStore(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name: "T", Date: "11-06-2026", DurationDays: 1, Courts: []string{"A"},
			Mode: mode, DefaultPublicNames: state.PublicNamesPerson,
		}))
		return store
	}
	imported := func(t *testing.T, store *state.Store, entry ImportManifestComp) *state.Competition {
		t.Helper()
		res := importCompetition(store, engine.New(store), entry, map[string][]byte{})
		require.Emptyf(t, res.Error, "import should succeed: %s", res.Error)
		comp, err := store.LoadCompetition(entry.ID)
		require.NoError(t, err)
		require.NotNil(t, comp)
		return comp
	}

	t.Run("an officiated tournament's default is taken, with its reveal secret", func(t *testing.T) {
		comp := imported(t, newStore(t, state.TournamentModeOfficiated), ImportManifestComp{ID: "men", Name: "Men"})
		assert.Equal(t, state.PublicNamesPerson, comp.PublicNames)
		assert.Len(t, comp.RevealSecret, 64)
	})
	t.Run("a Swiss competition keeps showing names", func(t *testing.T) {
		comp := imported(t, newStore(t, state.TournamentModeOfficiated), ImportManifestComp{ID: "swiss", Name: "Swiss", Format: state.CompFormatSwiss, SwissRounds: 3})
		assert.Empty(t, comp.PublicNames)
	})
	t.Run("a self-run tournament's competition shows names", func(t *testing.T) {
		comp := imported(t, newStore(t, state.TournamentModeSelfRun), ImportManifestComp{ID: "men", Name: "Men"})
		assert.Empty(t, comp.PublicNames)
	})
}
