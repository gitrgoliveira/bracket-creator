package engine

// Tests for the mp-825 wiring in the score path: RecordMatchResult (and
// its tx twin) must lock the MATCH-scoped lineups for the encounter that
// just went live, while leaving other matches' lineups editable. The
// state-level lock primitive is covered in internal/state; these tests
// guard the engine wiring so a regression that drops the
// LockTeamLineupForMatch call (leaving only the legacy round sweep) would
// fail.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fiveLineupForMatch(teamID, matchID string) domain.TeamLineup {
	return domain.TeamLineup{
		TeamID:  teamID,
		MatchID: matchID,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1", domain.PosJiho: "p2", domain.PosChuken: "p3",
			domain.PosFukusho: "p4", domain.PosTaisho: "p5",
		},
	}
}

func matchLineupLocked(t *testing.T, store *state.Store, compID, teamID, matchID string) bool {
	t.Helper()
	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	for _, l := range lineups {
		if l.MatchID == matchID && l.TeamID == teamID {
			return l.LockedAt != nil
		}
	}
	t.Fatalf("no match-scoped lineup for team=%s match=%s", teamID, matchID)
	return false
}

// TestRecordMatchResult_LocksMatchScopedLineup is the non-tx wiring test:
// recording PoolA-0 as completed must freeze the PoolA-0 lineup but leave
// the PoolA-1 lineup editable.
func TestRecordMatchResult_LocksMatchScopedLineup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "rmr-match-lock"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-0", SideA: "teamA", SideB: "teamB", Status: state.MatchStatusScheduled},
		{ID: "PoolA-1", SideA: "teamA", SideB: "teamC", Status: state.MatchStatusScheduled},
	}))
	require.NoError(t, store.SetTeamLineup(compID, fiveLineupForMatch("teamA", "PoolA-0"), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveLineupForMatch("teamA", "PoolA-1"), 5))

	require.NoError(t, eng.RecordMatchResult(compID, "PoolA-0", &state.MatchResult{
		Winner: "teamA",
		Status: state.MatchStatusCompleted,
	}))

	assert.True(t, matchLineupLocked(t, store, compID, "teamA", "PoolA-0"),
		"the completed match's lineup must be locked")
	assert.False(t, matchLineupLocked(t, store, compID, "teamA", "PoolA-1"),
		"a different match's lineup must remain editable")
}

// TestMaybeLockTeamLineupsForRoundTx_LocksMatchScopedLineup is the
// tx-path twin: the helper invoked from RecordMatchResult...Tx must lock
// the target match's lineup while leaving another match's lineup open.
func TestMaybeLockTeamLineupsForRoundTx_LocksMatchScopedLineup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "rmrtx-match-lock"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, TeamSize: 5}))
	require.NoError(t, store.SetTeamLineup(compID, fiveLineupForMatch("teamA", "PoolA-0"), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveLineupForMatch("teamA", "PoolA-1"), 5))

	err := store.WithTransaction(compID, func(tx state.StoreTx) error {
		eng.maybeLockTeamLineupsForRoundTx(tx, compID, &state.MatchResult{
			ID:     "PoolA-0",
			Status: state.MatchStatusRunning,
		})
		return nil
	})
	require.NoError(t, err)

	assert.True(t, matchLineupLocked(t, store, compID, "teamA", "PoolA-0"),
		"target match lineup must be locked via the tx path")
	assert.False(t, matchLineupLocked(t, store, compID, "teamA", "PoolA-1"),
		"other match lineup must remain editable")
}
