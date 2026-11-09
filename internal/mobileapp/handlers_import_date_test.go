package mobileapp

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-imdt: a manifest row with no date used to import as a success, yet the
// competition could never be drawn ("Cannot start: invalid date in Settings
// tab"). The importer now gives it the tournament's Day 1, the rule POST
// /competitions applies (defaultCompetitionDate), and refuses a row whose date
// still cannot be resolved.
func TestImportCompetition_DateDefaulting(t *testing.T) {
	newStore := func(t *testing.T, tournDate string) *state.Store {
		t.Helper()
		store, err := state.NewStore(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name: "T", Date: tournDate, DurationDays: 2, Courts: []string{"A"},
		}))
		return store
	}

	t.Run("a row with no date takes the tournament date", func(t *testing.T) {
		store := newStore(t, "11-06-2026")
		res := importCompetition(store, engine.New(store), ImportManifestComp{ID: "no-date", Name: "No Date"}, map[string][]byte{})
		require.Emptyf(t, res.Error, "import should succeed: %s", res.Error)
		comp, err := store.LoadCompetition("no-date")
		require.NoError(t, err)
		assert.Equal(t, "11-06-2026", comp.Date)
	})

	t.Run("an explicit date is left unchanged", func(t *testing.T) {
		store := newStore(t, "11-06-2026")
		res := importCompetition(store, engine.New(store), ImportManifestComp{ID: "day-two", Name: "Day Two", Date: "12-06-2026"}, map[string][]byte{})
		require.Emptyf(t, res.Error, "import should succeed: %s", res.Error)
		comp, err := store.LoadCompetition("day-two")
		require.NoError(t, err)
		assert.Equal(t, "12-06-2026", comp.Date)
	})

	t.Run("a row with no date under a tournament with no date is refused", func(t *testing.T) {
		store := newStore(t, "")
		res := importCompetition(store, engine.New(store), ImportManifestComp{ID: "stuck", Name: "Stuck"}, map[string][]byte{})
		assert.Contains(t, res.Error, "date:")
		assert.Contains(t, res.Error, "tournament has no date")
		stored, _ := store.LoadCompetition("stuck")
		assert.Nil(t, stored, "a refused row must not be persisted")
	})

	t.Run("a dated row under a tournament with no date still imports", func(t *testing.T) {
		store := newStore(t, "")
		res := importCompetition(store, engine.New(store), ImportManifestComp{ID: "dated", Name: "Dated", Date: "11-06-2026"}, map[string][]byte{})
		require.Emptyf(t, res.Error, "import should succeed: %s", res.Error)
	})
}

// The import door refuses the pool-winners bound the create and settings doors
// refuse (state.Competition.ValidatePoolWinners), so a manifest cannot persist
// a value the estimate and the calendar would build a draw too large from.
func TestImportCompetition_PoolWinnersBound(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	seedImportTournamentDate(t, store)

	res := importCompetition(store, engine.New(store), ImportManifestComp{
		ID: "pw-import", Name: "PW Import", Format: state.CompFormatMixed,
		PoolSize: 3, PoolSizeMode: "max", PoolWinners: 300,
	}, map[string][]byte{})
	assert.Equal(t, "poolWinners must be at most 3 for pools of 3 (got 300)", res.Error)
	stored, _ := store.LoadCompetition("pw-import")
	assert.Nil(t, stored, "a refused row must not be persisted")

	res = importCompetition(store, engine.New(store), ImportManifestComp{
		ID: "pw-import-ok", Name: "PW Import OK", Format: state.CompFormatMixed,
		PoolSize: 3, PoolSizeMode: "max", PoolWinners: 2,
	}, map[string][]byte{})
	require.Emptyf(t, res.Error, "import should succeed: %s", res.Error)
}

// seedImportTournamentDate gives a test's tournament a date, which a manifest
// row that states none takes (bc-imdt). The date is the one the importer tests
// that state a date use, so those rows stay inside the tournament's day range.
func seedImportTournamentDate(t testing.TB, store *state.Store) {
	t.Helper()
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Date: "12-05-2026", Courts: []string{"A", "B", "C", "D"}}))
}
