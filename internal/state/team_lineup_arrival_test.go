package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// One competition's lineups.yaml holding a stamp far in the future (a clock
// that was stepped back since, a hand-edited file) raises the arrival stamps
// of THAT competition only: another competition's arrival stamps stay at the
// server's time, so an unstamped change there (an older build's, a removal
// sent with no stamp) never outranks the device changes made after it
// (stage 6 review 9, G4).
func TestLineupArrivalStamp_AFutureStampInOneCompetitionLeavesTheOthersAtNow(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	const ahead, other = "rv9-ahead", "rv9-other"
	p1 := domain.PositionNumbered(1)
	future := time.Now().UnixMilli() + 24*3600_000
	for _, c := range []string{ahead, other} {
		require.NoError(t, store.SaveCompetition(&Competition{ID: c}))
	}
	require.NoError(t, store.SetTeamLineup(ahead, domain.TeamLineup{TeamID: "ta", MatchID: "P1-0",
		Positions: map[domain.Position]string{p1: "Mori"}, Stamps: map[domain.Position]int64{p1: future},
		CopiedAt: future, Own: []domain.Position{p1}}, 3))
	_, err := store.LoadTeamLineupsWithRemoved(ahead)
	require.NoError(t, err)
	require.NoError(t, store.SetTeamLineup(other, domain.TeamLineup{TeamID: "tb", MatchID: "P1-0",
		Positions: map[domain.Position]string{p1: "Sato"}, Stamps: map[domain.Position]int64{p1: 1000},
		CopiedAt: 1000, Own: []domain.Position{p1}}, 3))
	// A removal sent with no stamp is stamped as it lands.
	removed, err := store.RemoveTeamLineupAt(other, "tb", "P1-0", 0, 0)
	require.NoError(t, err)
	require.True(t, removed)
	all, err := store.LoadTeamLineupsWithRemoved(other)
	require.NoError(t, err)
	found := false
	for _, l := range all {
		if l.TeamID == "tb" && l.MatchID == "P1-0" && l.IsRemoved() {
			found = true
			require.Less(t, l.RemovedAt, time.Now().UnixMilli()+60_000,
				"the other competition's arrival stamp stays near the server's time")
		}
	}
	require.True(t, found)
	// The competition holding the future stamp still never stamps at or
	// before it (m5).
	removed, err = store.RemoveTeamLineupAt(ahead, "ta", "P1-0", 0, 0)
	require.NoError(t, err)
	require.True(t, removed)
	all, err = store.LoadTeamLineupsWithRemoved(ahead)
	require.NoError(t, err)
	for _, l := range all {
		if l.TeamID == "ta" && l.MatchID == "P1-0" && l.IsRemoved() {
			require.Greater(t, l.RemovedAt, future)
		}
	}
}
