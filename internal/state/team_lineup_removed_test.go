package state

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/require"
)

// A lineup taken away is kept as a record a carry can read, and is no lineup
// in force: every read of the lineups in force leaves it out.
func TestTeamLineupRemoval_KeepsARecordOnlyTheCarryReads(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const comp = "rm"
	require.NoError(t, s.SaveCompetition(&Competition{ID: comp, Kind: "team", TeamSize: 3}))
	p1 := domain.PositionNumbered(1)
	pos := map[domain.Position]string{p1: "Sato"}
	require.NoError(t, s.SetTeamLineup(comp, domain.TeamLineup{TeamID: "t", Positions: pos}, 3))
	require.NoError(t, s.SetTeamLineup(comp, domain.TeamLineup{TeamID: "t", MatchID: "M1", Positions: pos, CopiedAt: 5, Own: []domain.Position{p1}}, 3))
	require.NoError(t, s.SetTeamLineup(comp, domain.TeamLineup{TeamID: "t", MatchID: "M2", Positions: pos, StartedAt: 7}, 3))
	require.NoError(t, s.SetTeamLineup(comp, domain.TeamLineup{TeamID: "t", Round: 2, Positions: pos}, 3))

	require.NoError(t, s.DeleteTeamLineupForMatch(comp, "t", "M1"))
	require.NoError(t, s.DeleteTeamLineupForMatch(comp, "t", "M2"))
	require.NoError(t, s.DeleteTeamLineup(comp, "t", 2))
	require.NoError(t, s.WithTransaction(comp, func(tx StoreTx) error { return tx.DeleteTeamLineup(comp, "t", 0) }))
	require.NoError(t, s.DeleteTeamLineupForMatch(comp, "t", "nothing"))

	live, err := s.LoadTeamLineups(comp)
	require.NoError(t, err)
	require.Empty(t, live, "nothing is in force")
	require.NoError(t, s.ReadTeamLineups(comp, func(l map[string]domain.TeamLineup) { require.Empty(t, l) }))
	require.NoError(t, s.WithTransaction(comp, func(tx StoreTx) error {
		l, err := tx.LoadTeamLineups(comp)
		require.Empty(t, l)
		return err
	}))
	all, err := s.LoadTeamLineupsWithRemoved(comp)
	require.NoError(t, err)
	kept := map[string]bool{}
	for _, l := range all {
		require.True(t, l.IsRemoved())
		require.Positive(t, l.RemovedAt)
		kept[l.MatchID] = true
	}
	// The match's own lineup and the starting lineup are kept; a pin (never
	// carried) and a later round's lineup (never read) are not.
	require.Equal(t, map[string]bool{"M1": true, "": true}, kept)
	require.Len(t, all, 2)

	// Saved again, the lineup is its own beside the record (several lineups
	// in one write; one the domain refuses sets none).
	require.Error(t, s.SetTeamLineups(comp, []domain.TeamLineup{{TeamID: "t", MatchID: "M1", Positions: pos}, {TeamID: "t", MatchID: "M3", Positions: map[domain.Position]string{domain.PositionNumbered(9): "x"}}}, 3))
	require.NoError(t, s.SetTeamLineups(comp, nil, 3))
	require.NoError(t, s.SetTeamLineups(comp, []domain.TeamLineup{{TeamID: "t", MatchID: "M1", Positions: pos}}, 3))
	live, err = s.LoadTeamLineups(comp)
	require.NoError(t, err)
	require.Len(t, live, 1)
	all, err = s.LoadTeamLineupsWithRemoved(comp)
	require.NoError(t, err)
	require.Len(t, all, 3)

	// A new draw drops the match's lineup and its record; the starting
	// lineup's record stays.
	require.NoError(t, s.ClearDrawLineups(comp))
	all, err = s.LoadTeamLineupsWithRemoved(comp)
	require.NoError(t, err)
	require.Len(t, all, 1)
	for _, l := range all {
		require.Empty(t, l.MatchID)
		require.True(t, l.IsRemoved())
	}
}
