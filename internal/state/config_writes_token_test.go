package state

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ConfigWritesToken keys the viewer list and court feed flights: it must move
// on every config.md write of ANY competition, so a request after a draw is
// published or hidden never joins a build begun before it, and must stay put
// for the writes live scoring makes, so those builds are still shared.

func newConfigTokenStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	return store
}

func TestConfigWritesToken_MovesOnAConfigWriteToAnyCompetition(t *testing.T) {
	store := newConfigTokenStore(t)

	t0 := store.ConfigWritesToken()
	require.NoError(t, store.SaveCompetition(&Competition{ID: "men", Name: "Men"}))
	t1 := store.ConfigWritesToken()
	assert.Greater(t, t1, t0, "creating a competition writes its config.md")

	require.NoError(t, store.SaveCompetition(&Competition{ID: "women", Name: "Women"}))
	t2 := store.ConfigWritesToken()
	assert.Greater(t, t2, t1, "a second competition's config.md moves the same store-wide token")

	changed, err := store.UpdateCompetitionChanged("men", func(c *Competition) (*Competition, error) {
		c.Courts = []string{"A"}
		return c, nil
	})
	require.NoError(t, err)
	require.True(t, changed)
	t3 := store.ConfigWritesToken()
	assert.Greater(t, t3, t2, "an UpdateCompetitionChanged that writes moves it")

	changed, err = store.UpdateCompetitionChanged("men", func(c *Competition) (*Competition, error) {
		return c, nil
	})
	require.NoError(t, err)
	require.False(t, changed)
	assert.Equal(t, t3, store.ConfigWritesToken(), "a save that writes no bytes moves nothing")

	require.NoError(t, store.DeleteCompetition("women"))
	assert.Greater(t, store.ConfigWritesToken(), t3, "deleting a competition drops its config.md and moves the token")
}

func TestConfigWritesToken_MovesOnATransactionAbortOverConfig(t *testing.T) {
	store := newConfigTokenStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "men", Name: "Men"}))

	before := store.ConfigWritesToken()
	store.invalidateCachesForWALIntents("men", []wal.FileIntent{{Path: store.compPath("men", "config.md")}})
	assert.Greater(t, store.ConfigWritesToken(), before,
		"an aborted transaction rolls config.md back to content a build may have read past, so the token moves")

	before = store.ConfigWritesToken()
	store.invalidateCachesForWALIntents("men", []wal.FileIntent{{Path: store.compPath("men", "pool-matches.csv")}})
	assert.Equal(t, before, store.ConfigWritesToken(), "an abort over another file leaves it alone")
}

func TestConfigWritesToken_StaysForScoresCheckInsOverridesAndReads(t *testing.T) {
	store := newConfigTokenStore(t)
	const compID = "men"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Men"}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: "0b6f2f0e-4a1d-4c3b-9d2e-1f5a6b7c8d90", Name: "Ann", Dojo: "Dojo"},
		{ID: "6c1e9b4a-2f3d-4e5f-8a7b-9c0d1e2f3a4b", Name: "Bea", Dojo: "Dojo"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "Pool A-0", SideA: "Ann", SideB: "Bea", Status: MatchStatusScheduled},
	}))
	// The first load of a competition runs its one-time legacy checks, which
	// may repair config.md (here the has_participant_ids flag): a real write,
	// made once per process, so it is done before the baseline.
	_, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)

	before := store.ConfigWritesToken()

	found, err := store.UpdatePoolMatchByID(compID, "Pool A-0", func(m *MatchResult) error {
		m.Status = MatchStatusRunning
		m.IpponsA = []string{"M"}
		return nil
	})
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, before, store.ConfigWritesToken(), "a score write")

	_, applied, err := store.SetCheckIn(compID, "0b6f2f0e-4a1d-4c3b-9d2e-1f5a6b7c8d90", false, true, 1000)
	require.NoError(t, err)
	require.True(t, applied)
	assert.Equal(t, before, store.ConfigWritesToken(), "a check-in")

	require.NoError(t, store.SaveRankOverride(compID, "Pool A", "0b6f2f0e-4a1d-4c3b-9d2e-1f5a6b7c8d90", 1))
	assert.Equal(t, before, store.ConfigWritesToken(), "an overrides save")

	_, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	_, err = store.ListCompetitions()
	require.NoError(t, err)
	_, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)
	_, err = store.LoadParticipants(compID, false)
	require.NoError(t, err)
	_ = store.FileVersion(compID, "config.md")
	assert.Equal(t, before, store.ConfigWritesToken(), "reads")
}
