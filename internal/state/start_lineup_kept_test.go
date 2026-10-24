package state

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lineup a kachinuki match started with (a pin, StartedAt set) is
// announced once the transaction that wrote it commits, after its lock is
// released, and never for one that aborts or for a lineup an operator saved.
func TestOnStartLineupKept_AnnouncedOnceCommitted(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()
	const c = "kept"
	require.NoError(t, s.SaveCompetition(&Competition{ID: c, Kind: "team", TeamSize: 3}))
	var got []string
	s.OnStartLineupKept(func(compID, teamID, matchID string) {
		// The lock is released: a store read here does not wait on it.
		_, err := s.LoadTeamLineups(compID)
		require.NoError(t, err)
		got = append(got, compID+"/"+teamID+"/"+matchID)
	})
	pin := domain.TeamLineup{TeamID: "ta", MatchID: "m1", Positions: map[domain.Position]string{"1": "Aoki"}, StartedAt: 100}
	own := domain.TeamLineup{TeamID: "tb", MatchID: "m1", Positions: map[domain.Position]string{"1": "Fujii"}}

	err := s.WithTransaction(c, func(tx StoreTx) error {
		require.NoError(t, tx.SetTeamLineup(c, pin, 3))
		return errors.New("abort")
	})
	require.Error(t, err)
	assert.Empty(t, got, "an aborted transaction announces nothing")

	require.NoError(t, s.WithTransaction(c, func(tx StoreTx) error {
		require.NoError(t, tx.SetTeamLineup(c, own, 3))
		require.NoError(t, tx.SetTeamLineup(c, pin, 3))
		assert.Empty(t, got, "nothing is announced before the commit")
		return nil
	}))
	assert.Equal(t, []string{c + "/ta/m1"}, got, "the pin alone, once committed")

	lineups, err := s.LoadTeamLineups(c)
	require.NoError(t, err)
	var kept domain.TeamLineup
	for _, l := range lineups {
		if l.TeamID == "ta" {
			kept = l
		}
	}
	assert.True(t, kept.IsStartPin(), "the pin round-trips through lineups.yaml")
	assert.Equal(t, int64(100), kept.StartedAt)
}
