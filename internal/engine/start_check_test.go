package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startCheckRoster saves Alice, Bob, Charlie and Dave and returns their ids.
func startCheckRoster(t *testing.T, store *state.Store, compID string) (alice, bob, charlie, dave string) {
	t.Helper()
	alice, bob, charlie, dave = helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: alice, Name: "Alice", Dojo: "A"},
		{ID: bob, Name: "Bob", Dojo: "B"},
		{ID: charlie, Name: "Charlie", Dojo: "C"},
		{ID: dave, Name: "Dave", Dojo: "D"},
	}))
	return alice, bob, charlie, dave
}

// startMatchTxErr runs the real tx start gate for matchID and returns its
// answer, so a test can set it beside the check's.
func startMatchTxErr(t *testing.T, eng *Engine, store *state.Store, compID, matchID string) error {
	t.Helper()
	var engErr error
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		engErr = eng.StartMatchTx(tx, compID, matchID)
		return nil
	}))
	return engErr
}

// TestCheckStartAfterSendBack pins the start check (bc-stpf): the start gate
// judged as if the bout running on the pick's own court were sent back.
func TestCheckStartAfterSendBack(t *testing.T) {
	t.Run("a competitor the bout on the pick's own court shares is free once it goes back", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "sc-own-court"
		createTestCompetition(t, store, compID, "league", 4)
		alice, bob, charlie, _ := startCheckRoster(t, store, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Bob", SideAID: alice, SideBID: bob, Status: state.MatchStatusRunning, Court: "A"},
			{ID: "P-1", SideA: "Alice", SideB: "Charlie", SideAID: alice, SideBID: charlie, Status: state.MatchStatusScheduled, Court: "A"},
		}))

		assert.NoError(t, eng.CheckStartAfterSendBack(compID, "P-1"))

		// The real start, with the bout still running, is refused for the court.
		assert.ErrorIs(t, startMatchTxErr(t, eng, store, compID, "P-1"), ErrCourtBusy)
	})

	t.Run("a competitor fighting now on another court refuses with the simultaneity sentence", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "sc-other-court"
		createTestCompetition(t, store, compID, "league", 4)
		alice, bob, charlie, dave := startCheckRoster(t, store, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Charlie", SideB: "Dave", SideAID: charlie, SideBID: dave, Status: state.MatchStatusRunning, Court: "A"},
			{ID: "P-1", SideA: "Alice", SideB: "Bob", SideAID: alice, SideBID: bob, Status: state.MatchStatusRunning, Court: "B"},
			{ID: "P-2", SideA: "Alice", SideB: "Charlie", SideAID: alice, SideBID: charlie, Status: state.MatchStatusScheduled, Court: "A"},
		}))

		err := eng.CheckStartAfterSendBack(compID, "P-2")
		var ineligErr *IneligibleCompetitorError
		require.ErrorAs(t, err, &ineligErr)
		assert.True(t, ineligErr.Simultaneous)
		assert.Equal(t, alice, ineligErr.PlayerID)
		assert.Equal(t, "Alice is fighting now in P-1 on Shiaijo B. Finish that match first.", ineligErr.Reason)
	})

	t.Run("a knockout bout running on the pick's own court is skipped, one on another court is not", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "sc-bracket"
		createTestCompetition(t, store, compID, "league", 4)
		alice, bob, charlie, dave := startCheckRoster(t, store, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Charlie", SideAID: alice, SideBID: charlie, Status: state.MatchStatusScheduled, Court: "A"},
			{ID: "P-1", SideA: "Bob", SideB: "Dave", SideAID: bob, SideBID: dave, Status: state.MatchStatusScheduled, Court: "A"},
		}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{{
				{ID: "B-1", SideA: "Alice", SideB: "Dave", SideAID: alice, SideBID: dave, Status: state.MatchStatusRunning, Court: "A"},
			}},
			ThirdPlaceMatch: &state.BracketMatch{ID: "B-3", SideA: "Bob", SideB: "Charlie", SideAID: bob, SideBID: charlie, Status: state.MatchStatusRunning, Court: "C"},
		}))

		// Alice's knockout bout runs on court A, the pick's own: skipped.
		// Charlie's 3rd-place bout runs on court C: still refused.
		err := eng.CheckStartAfterSendBack(compID, "P-0")
		var ineligErr *IneligibleCompetitorError
		require.ErrorAs(t, err, &ineligErr)
		assert.True(t, ineligErr.Simultaneous)
		assert.Equal(t, charlie, ineligErr.PlayerID)

		// Bob and Dave: Dave's knockout bout is on court A (skipped), Bob's
		// 3rd-place bout on court C refuses.
		require.ErrorAs(t, eng.CheckStartAfterSendBack(compID, "P-1"), &ineligErr)
		assert.Equal(t, bob, ineligErr.PlayerID)
	})

	t.Run("a pick with no court skips nothing", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "sc-no-court"
		createTestCompetition(t, store, compID, "league", 4)
		alice, bob, charlie, _ := startCheckRoster(t, store, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Bob", SideAID: alice, SideBID: bob, Status: state.MatchStatusRunning},
			{ID: "P-1", SideA: "Alice", SideB: "Charlie", SideAID: alice, SideBID: charlie, Status: state.MatchStatusScheduled},
		}))

		var ineligErr *IneligibleCompetitorError
		require.ErrorAs(t, eng.CheckStartAfterSendBack(compID, "P-1"), &ineligErr)
		assert.True(t, ineligErr.Simultaneous)
	})

	t.Run("a barred side refuses", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "sc-barred"
		createTestCompetition(t, store, compID, "league", 4)
		alice, bob, charlie, dave := startCheckRoster(t, store, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Charlie", SideB: "Dave", SideAID: charlie, SideBID: dave, Status: state.MatchStatusRunning, Court: "A"},
			{ID: "P-1", SideA: "Alice", SideB: "Bob", SideAID: alice, SideBID: bob, Status: state.MatchStatusScheduled, Court: "A"},
		}))
		require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID:   bob,
			Eligible:   false,
			Reason:     "kiken at m_prev",
			MatchID:    "m_prev",
			RecordedAt: time.Now().UTC(),
		}))

		err := eng.CheckStartAfterSendBack(compID, "P-1")
		assert.ErrorIs(t, err, ErrIneligibleCompetitor)
		var ineligErr *IneligibleCompetitorError
		require.ErrorAs(t, err, &ineligErr)
		assert.False(t, ineligErr.Simultaneous)
		assert.Equal(t, bob, ineligErr.PlayerID)
	})

	t.Run("a running or finished pick refuses as not scheduled", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "sc-not-scheduled"
		createTestCompetition(t, store, compID, "league", 4)
		alice, bob, charlie, dave := startCheckRoster(t, store, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Bob", SideAID: alice, SideBID: bob, Status: state.MatchStatusRunning, Court: "A"},
			{ID: "P-1", SideA: "Charlie", SideB: "Dave", SideAID: charlie, SideBID: dave, Status: state.MatchStatusCompleted, Winner: "Charlie", WinnerID: charlie, Court: "A"},
		}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{{
				{ID: "B-1", SideA: "Alice", SideB: "Dave", SideAID: alice, SideBID: dave, Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: alice, Court: "B"},
			}},
		}))

		for matchID, want := range map[string]state.MatchStatus{
			"P-0": state.MatchStatusRunning,
			"P-1": state.MatchStatusCompleted,
			"B-1": state.MatchStatusCompleted,
		} {
			var notScheduled *MatchNotScheduledError
			require.ErrorAs(t, eng.CheckStartAfterSendBack(compID, matchID), &notScheduled, matchID)
			assert.Equal(t, matchID, notScheduled.MatchID)
			assert.Equal(t, want, notScheduled.Status)
			assert.Contains(t, notScheduled.Error(), "not scheduled")
		}
	})

	t.Run("an unknown match is not found", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "sc-unknown"
		createTestCompetition(t, store, compID, "league", 4)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A"},
		}))

		var notFound *NotFoundError
		require.ErrorAs(t, eng.CheckStartAfterSendBack(compID, "no-such"), &notFound)
	})

	t.Run("a scheduled knockout match as the pick", func(t *testing.T) {
		t.Run("a competitor shared with a bout running on the pick's own court is accepted", func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "sc-ko-pick-own-court"
			createTestCompetition(t, store, compID, "league", 4)
			alice, bob, charlie, dave := startCheckRoster(t, store, compID)
			require.NoError(t, store.SaveBracket(compID, &state.Bracket{
				Rounds: [][]state.BracketMatch{{
					{ID: "B-0", SideA: "Alice", SideB: "Charlie", SideAID: alice, SideBID: charlie, Status: state.MatchStatusRunning, Court: "A"},
					{ID: "B-1", SideA: "Alice", SideB: "Bob", SideAID: alice, SideBID: bob, Status: state.MatchStatusScheduled, Court: "A"},
				}},
				ThirdPlaceMatch: &state.BracketMatch{ID: "B-3", SideA: "Charlie", SideB: "Dave", SideAID: charlie, SideBID: dave, Status: state.MatchStatusScheduled, Court: "B"},
			}))

			assert.NoError(t, eng.CheckStartAfterSendBack(compID, "B-1"))
			assert.ErrorIs(t, startMatchTxErr(t, eng, store, compID, "B-1"), ErrCourtBusy)
		})

		t.Run("the third-place match refuses when a competitor fights on another court", func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "sc-ko-pick-third-place"
			createTestCompetition(t, store, compID, "league", 4)
			alice, bob, _, dave := startCheckRoster(t, store, compID)
			require.NoError(t, store.SaveBracket(compID, &state.Bracket{
				Rounds: [][]state.BracketMatch{{
					{ID: "B-1", SideA: "Bob", SideB: "Alice", SideAID: bob, SideBID: alice, Status: state.MatchStatusRunning, Court: "B"},
				}},
				ThirdPlaceMatch: &state.BracketMatch{ID: "B-3", SideA: "Bob", SideB: "Dave", SideAID: bob, SideBID: dave, Status: state.MatchStatusScheduled, Court: "C"},
			}))

			err := eng.CheckStartAfterSendBack(compID, "B-3")
			var ineligErr *IneligibleCompetitorError
			require.ErrorAs(t, err, &ineligErr)
			assert.True(t, ineligErr.Simultaneous)
			assert.Equal(t, bob, ineligErr.PlayerID)
		})

		t.Run("a barred side refuses", func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "sc-ko-pick-barred"
			createTestCompetition(t, store, compID, "league", 4)
			alice, bob, charlie, dave := startCheckRoster(t, store, compID)
			require.NoError(t, store.SaveBracket(compID, &state.Bracket{
				Rounds: [][]state.BracketMatch{{
					{ID: "B-1", SideA: "Alice", SideB: "Bob", SideAID: alice, SideBID: bob, Status: state.MatchStatusScheduled, Court: "A"},
				}},
				ThirdPlaceMatch: &state.BracketMatch{ID: "B-3", SideA: "Charlie", SideB: "Dave", SideAID: charlie, SideBID: dave, Status: state.MatchStatusScheduled, Court: "B"},
			}))
			require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
				PlayerID:   bob,
				Eligible:   false,
				Reason:     "kiken at m_prev",
				MatchID:    "m_prev",
				RecordedAt: time.Now().UTC(),
			}))

			err := eng.CheckStartAfterSendBack(compID, "B-1")
			assert.ErrorIs(t, err, ErrIneligibleCompetitor)
			var ineligErr *IneligibleCompetitorError
			require.ErrorAs(t, err, &ineligErr)
			assert.False(t, ineligErr.Simultaneous)
			assert.Equal(t, bob, ineligErr.PlayerID)
		})
	})
}

// TestStartMatchTx_SidesGateUnchanged pins that splitting the start gate
// (bc-stpf) left the real start as it was: the court check first, then a
// side fighting on ANY other court, the pick's own included.
func TestStartMatchTx_SidesGateUnchanged(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "sc-tx-unchanged"
	createTestCompetition(t, store, compID, "league", 4)
	alice, bob, charlie, _ := startCheckRoster(t, store, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "P-0", SideA: "Alice", SideB: "Bob", SideAID: alice, SideBID: bob, Status: state.MatchStatusRunning, Court: "A"},
		{ID: "P-1", SideA: "Alice", SideB: "Charlie", SideAID: alice, SideBID: charlie, Status: state.MatchStatusScheduled, Court: "B"},
	}))

	err := startMatchTxErr(t, eng, store, compID, "P-1")
	var ineligErr *IneligibleCompetitorError
	require.ErrorAs(t, err, &ineligErr)
	assert.True(t, ineligErr.Simultaneous)
	assert.False(t, errors.Is(err, ErrCourtBusy))
}
