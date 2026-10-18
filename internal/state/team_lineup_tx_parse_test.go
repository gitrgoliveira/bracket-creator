package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// A transaction parses lineups.yaml once, however often it reads and writes it
// (stage 6 review 8, m4: a lineup save parsed it four to five times), and every
// read answers what the transaction staged, as a copy of its own.
func TestStoreTx_LineupsAreParsedOncePerTransaction(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const comp = "c"
	require.NoError(t, s.SaveCompetition(&Competition{ID: comp, Kind: "team", TeamSize: 3}))
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)
	require.NoError(t, s.SetTeamLineup(comp, domain.TeamLineup{TeamID: "t1", Positions: map[domain.Position]string{p1: "Sato"}}, 3))
	before := lineupParses.Load()
	require.NoError(t, s.WithTransaction(comp, func(tx StoreTx) error {
		all, err := tx.LoadTeamLineupsWithRemoved(comp)
		require.NoError(t, err)
		require.Equal(t, "Sato", all["t1-0"].Positions[p1])
		all["t1-0"].Positions[p1] = "changed in the copy"
		live, err := tx.LoadTeamLineups(comp)
		require.NoError(t, err)
		require.Equal(t, "Sato", live["t1-0"].Positions[p1], "a read answers a copy")
		require.NoError(t, tx.SetTeamLineup(comp, domain.TeamLineup{TeamID: "t1", Positions: map[domain.Position]string{p1: "Mori"}}, 3))
		require.NoError(t, tx.SetTeamLineups(comp, []domain.TeamLineup{
			{TeamID: "t2", Positions: map[domain.Position]string{p2: "Doi"}},
			{TeamID: "t1", MatchID: "M1", Positions: map[domain.Position]string{p2: "Endo"}},
		}, 3))
		require.NoError(t, tx.DeleteTeamLineup(comp, "t2", 0))
		live, err = tx.LoadTeamLineups(comp)
		require.NoError(t, err)
		require.Equal(t, "Mori", live["t1-0"].Positions[p1])
		require.Equal(t, "Endo", live[teamLineupMatchKey("t1", "M1")].Positions[p2])
		require.NotContains(t, live, "t2-0")
		return nil
	}))
	require.Equal(t, int64(1), lineupParses.Load()-before, "one parse in the transaction")
	live, err := s.LoadTeamLineups(comp)
	require.NoError(t, err)
	require.Equal(t, "Mori", live["t1-0"].Positions[p1], "committed as staged")
	require.Equal(t, "Endo", live[teamLineupMatchKey("t1", "M1")].Positions[p2])
}
