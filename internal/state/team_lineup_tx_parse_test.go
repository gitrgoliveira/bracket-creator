package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// A transaction parses lineups.yaml at most once, however often it reads and
// writes it (stage 6 review 8, m4: a lineup save parsed it four to five times):
// not at all when the parsed-file cache holds the body the last save published,
// and never for a read after it staged lineups (its own memo answers). Every
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
		staged := lineupParses.Load()
		live, err = tx.LoadTeamLineups(comp)
		require.NoError(t, err)
		require.Equal(t, staged, lineupParses.Load(), "a read after the transaction staged lineups parses nothing")
		require.Equal(t, "Mori", live["t1-0"].Positions[p1])
		require.Equal(t, "Endo", live[teamLineupMatchKey("t1", "M1")].Positions[p2])
		require.NotContains(t, live, "t2-0")
		return nil
	}))
	require.Zero(t, lineupParses.Load()-before, "warm cache: no parse in the transaction")
	live, err := s.LoadTeamLineups(comp)
	require.NoError(t, err)
	require.Equal(t, "Mori", live["t1-0"].Positions[p1], "committed as staged")
	require.Equal(t, "Endo", live[teamLineupMatchKey("t1", "M1")].Positions[p2])
}

// With a cold cache a transaction parses lineups.yaml once, then reads and
// writes it with no further parse.
func TestStoreTx_ColdLineupsAreParsedOncePerTransaction(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const comp = "c"
	require.NoError(t, s.SaveCompetition(&Competition{ID: comp, Kind: "team", TeamSize: 3}))
	p1 := domain.PositionNumbered(1)
	require.NoError(t, s.SetTeamLineup(comp, domain.TeamLineup{TeamID: "t1", Positions: map[domain.Position]string{p1: "Sato"}}, 3))
	// Cold: drop the cached bodies (a fresh store is not cold: NewStore's
	// legacy sweep reads the competition's files).
	s.discardCompCacheBodies(comp)
	before := lineupParses.Load()
	require.NoError(t, s.WithTransaction(comp, func(tx StoreTx) error {
		for range 2 {
			all, err := tx.LoadTeamLineupsWithRemoved(comp)
			require.NoError(t, err)
			require.Equal(t, "Sato", all["t1-0"].Positions[p1])
		}
		require.Equal(t, int64(1), lineupParses.Load()-before, "cold cache: one parse")
		require.NoError(t, tx.SetTeamLineup(comp, domain.TeamLineup{TeamID: "t1", Positions: map[domain.Position]string{p1: "Mori"}}, 3))
		live, err := tx.LoadTeamLineups(comp)
		require.NoError(t, err)
		require.Equal(t, "Mori", live["t1-0"].Positions[p1])
		return nil
	}))
	require.Equal(t, int64(1), lineupParses.Load()-before, "one parse in the transaction")
}

// A transaction parses the pools.csv it staged once, however often it reads
// it back, and again only after it stages the file again; each read answers
// a copy.
func TestStoreTx_StagedPoolsAreParsedOncePerStagedWrite(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const comp = "c"
	require.NoError(t, s.SaveCompetition(&Competition{ID: comp}))
	pool := func(name string) []helper.Pool {
		return []helper.Pool{{PoolName: name, Players: []helper.Player{{Name: "Sato", Dojo: "D"}}}}
	}
	before := stagedPoolsParses.Load()
	require.NoError(t, s.WithTransaction(comp, func(tx StoreTx) error {
		require.NoError(t, tx.SavePools(comp, pool("Pool A")))
		for range 3 {
			got, err := tx.LoadPools(comp)
			require.NoError(t, err)
			require.Equal(t, "Pool A", got[0].PoolName)
			got[0].PoolName = "changed in the copy"
		}
		require.Equal(t, int64(1), stagedPoolsParses.Load()-before, "one parse for one staged write")
		require.NoError(t, tx.SavePools(comp, pool("Pool B")))
		got, err := tx.LoadPools(comp)
		require.NoError(t, err)
		require.Equal(t, "Pool B", got[0].PoolName, "a write staged again is read again")
		return nil
	}))
	require.Equal(t, int64(2), stagedPoolsParses.Load()-before)
}
