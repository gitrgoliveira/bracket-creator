package engine

// A pool tie-break bout saved as a draw settles nothing: the pool keeps its
// tie and its knockout places stay empty. The operator is pointed at the bout
// (admin_pools.jsx) and fights it again by REOPENING it, the same door a
// withdrawal recorded by mistake uses, so these pin that the reopen gate lets
// exactly that bout through and still refuses a plain drawn pool match.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedDrawnSupplementary(t *testing.T, id string) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	const compID = "rd-mixed"
	createTestCompetition(t, store, compID, "mixed", 3)
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: id, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusCompleted, Decision: "hikiwake", Court: "A",
	}}))
	return eng, store, compID
}

func TestReopenDrawnSupplementaryBout(t *testing.T) {
	for _, id := range []string{"Pool A-TB-0", "Pool A-DH-0"} {
		t.Run(id, func(t *testing.T) {
			eng, store, compID := seedDrawnSupplementary(t, id)
			status, err := eng.ReopenMatch(compID, id, "")
			require.NoError(t, err)
			assert.Nil(t, status, "a draw barred nobody, so nothing is restored")

			matches, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			require.Len(t, matches, 1)
			assert.Equal(t, state.MatchStatusRunning, matches[0].Status, "fought again on its court")
			assert.Equal(t, "", matches[0].Decision, "the draw is gone")
			assert.Equal(t, "", matches[0].Winner)
		})
	}
}

// The gate stays narrow: a drawn REGULAR pool match, and a tie-break bout that
// has a winner, are corrected in the score editor, not reopened.
func TestReopenDrawnSupplementaryBout_OtherMatchesAreStillRefused(t *testing.T) {
	t.Run("a drawn regular pool match", func(t *testing.T) {
		eng, _, compID := seedDrawnSupplementary(t, "Pool A-0")
		_, err := eng.ReopenMatch(compID, "Pool A-0", "")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr)
	})
	t.Run("a tie-break bout with a winner", func(t *testing.T) {
		eng, store, compID := seedDrawnSupplementary(t, "Pool A-TB-0")
		ms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		ms[0].Decision, ms[0].Winner, ms[0].WinnerID = "fought", wrTeamA, wrTeamAID
		require.NoError(t, store.SavePoolMatches(compID, ms))
		_, err = eng.ReopenMatch(compID, "Pool A-TB-0", "")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr)
	})
}

// A bout the both-sides-barred door closed as a draw looks like any drawn
// supplementary bout, but its sides are withdrawn elsewhere: reopening it to
// RUNNING would leave a bout nobody can start, so it goes back to SCHEDULED,
// where the barred-match notice is shown again. One side barred is the same
// (a draw names no withdrawing side), and a bar recorded BY the bout itself
// never counts.
func TestReopenDrawnSupplementaryBout_BarredSidesGoToScheduled(t *testing.T) {
	bar := func(t *testing.T, store *state.Store, compID, playerID, matchID string) {
		t.Helper()
		require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID: playerID, Eligible: false, Reason: "kiken", MatchID: matchID,
		}))
	}
	cases := []struct {
		name   string
		bars   map[string]string // player id -> the match that barred them
		expect state.MatchStatus
	}{
		{"both barred elsewhere", map[string]string{wrTeamAID: "other-1", wrTeamBID: "other-2"}, state.MatchStatusScheduled},
		{"one barred elsewhere", map[string]string{wrTeamAID: "other-1"}, state.MatchStatusScheduled},
		{"barred by the bout itself", map[string]string{wrTeamAID: "Pool A-TB-0"}, state.MatchStatusRunning},
	}
	for _, id := range []string{"Pool A-TB-0", "Pool A-DH-0"} {
		for _, tc := range cases {
			t.Run(id+"/"+tc.name, func(t *testing.T) {
				eng, store, compID := seedDrawnSupplementary(t, id)
				for player, matchID := range tc.bars {
					if matchID == "Pool A-TB-0" {
						matchID = id
					}
					bar(t, store, compID, player, matchID)
				}
				_, err := eng.ReopenMatch(compID, id, "")
				require.NoError(t, err)

				matches, err := store.LoadPoolMatches(compID)
				require.NoError(t, err)
				require.Len(t, matches, 1)
				assert.Equal(t, tc.expect, matches[0].Status)
				assert.Equal(t, "", matches[0].Decision, "the draw is gone")
			})
		}
	}
}
