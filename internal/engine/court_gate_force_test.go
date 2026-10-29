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

// bc-adna: the court gates judge only a write that moves a match into
// running, and the organiser's forceCourt skips both of them while the sides
// gate still runs.

// TestCheckCrossCompCourtBusyOnStart_JudgesOnlyAMoveIntoRunning pins the
// cross-competition gate on the stored status it reads before the write's
// transaction.
func TestCheckCrossCompCourtBusyOnStart_JudgesOnlyAMoveIntoRunning(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "comp1", "league", 3)
	require.NoError(t, store.SavePoolMatches("comp1", []state.MatchResult{
		{ID: "P-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
	}))
	createTestCompetition(t, store, "comp2", "league", 3)
	require.NoError(t, store.SavePoolMatches("comp2", []state.MatchResult{
		{ID: "P-0", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "A"},
		{ID: "P-1", SideA: "Erin", SideB: "Frank", Status: state.MatchStatusRunning, Court: "A"},
	}))

	t.Run("a start beside another competition's running bout is court_busy", func(t *testing.T) {
		err := eng.CheckCrossCompCourtBusyOnStart("comp2", "P-0", state.MatchStatusRunning)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrCourtBusy), "want ErrCourtBusy, got %v", err)
		var busy *CourtBusyError
		require.ErrorAs(t, err, &busy)
		assert.Equal(t, "A", busy.Court)
		assert.Equal(t, "comp1", busy.CompID)
		assert.Equal(t, "P-0", busy.MatchID)
	})

	t.Run("a write to a match already running is not judged", func(t *testing.T) {
		assert.NoError(t, eng.CheckCrossCompCourtBusyOnStart("comp2", "P-1", state.MatchStatusRunning))
	})

	t.Run("a finish is not judged", func(t *testing.T) {
		assert.NoError(t, eng.CheckCrossCompCourtBusyOnStart("comp2", "P-1", state.MatchStatusCompleted))
		assert.NoError(t, eng.CheckCrossCompCourtBusyOnStart("comp2", "P-0", state.MatchStatusCompleted))
	})

	t.Run("a write that leaves the match scheduled is not judged", func(t *testing.T) {
		assert.NoError(t, eng.CheckCrossCompCourtBusyOnStart("comp2", "P-0", state.MatchStatusScheduled))
	})

	t.Run("an unknown match is still not found on a start", func(t *testing.T) {
		err := eng.CheckCrossCompCourtBusyOnStart("comp2", "P-9", state.MatchStatusRunning)
		var nf *NotFoundError
		require.ErrorAs(t, err, &nf)
	})
}

// TestStartMatchTx_SkipCourtGate pins the in-competition half: skipping the
// court leaves the sides gate in place.
func TestStartMatchTx_SkipCourtGate(t *testing.T) {
	t.Run("skipped, a start beside a running bout of the same competition passes", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "skip-court-same"
		createTestCompetition(t, store, compID, "league", 3)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
			{ID: "P-1", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "A"},
		}))

		var judged, skipped error
		require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
			judged = eng.StartMatchTx(tx, compID, "P-1")
			skipped = eng.StartMatchTx(tx, compID, "P-1", true)
			return nil
		}))
		assert.ErrorIs(t, judged, ErrCourtBusy, "without the skip the court is judged as before")
		assert.NoError(t, skipped)
	})

	t.Run("skipped, a barred side is still refused", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "skip-court-barred"
		createTestCompetition(t, store, compID, "league", 3)
		aliceID := helper.NewUUID4()
		bobID := helper.NewUUID4()
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{ID: aliceID, Name: "Alice", Dojo: "DojoA"},
			{ID: bobID, Name: "Bob", Dojo: "DojoB"},
		}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Carol", SideB: "Dan", Status: state.MatchStatusRunning, Court: "A"},
			{ID: "P-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A"},
		}))
		require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID:   aliceID,
			Eligible:   false,
			Reason:     "kiken at m_prev",
			MatchID:    "m_prev",
			RecordedAt: time.Now().UTC(),
		}))

		var engErr error
		require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
			engErr = eng.StartMatchTx(tx, compID, "P-1", true)
			return nil
		}))
		require.Error(t, engErr)
		assert.True(t, errors.Is(engErr, ErrIneligibleCompetitor), "want ErrIneligibleCompetitor, got %v", engErr)
		assert.False(t, errors.Is(engErr, ErrCourtBusy), "the court was skipped")
	})
}

// TestReopenMatch_ForceCourt pins "Reopen anyway": a reopen into running on a
// busy court lands when forced and is court_busy otherwise, for both gates.
func TestReopenMatch_ForceCourt(t *testing.T) {
	t.Run("same competition", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-force-same", 3)
		require.NoError(t, store.SavePoolMatches("reopen-force-same", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
			runningOnCourt("P1-1", "A"),
		}))

		require.ErrorIs(t, reopenErr(eng, "reopen-force-same", "P1-0", "need more bouts"), ErrCourtBusy)

		_, err := eng.ReopenMatch("reopen-force-same", "P1-0", "need more bouts", ForceOptions{ForceCourt: true})
		require.NoError(t, err)
		matches, lerr := store.LoadPoolMatches("reopen-force-same")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusRunning, matches[0].Status, "the forced reopen lands")
		assert.Equal(t, state.MatchStatusRunning, matches[1].Status, "the live bout keeps running beside it")
	})

	t.Run("another competition", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-force-cross", 3)
		require.NoError(t, store.SavePoolMatches("reopen-force-cross", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
		}))
		createTestCompetition(t, store, "other", "league", 3)
		require.NoError(t, store.SavePoolMatches("other", []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
		}))

		require.ErrorIs(t, reopenErr(eng, "reopen-force-cross", "P1-0", "need more bouts"), ErrCourtBusy)

		_, err := eng.ReopenMatch("reopen-force-cross", "P1-0", "need more bouts", ForceOptions{ForceCourt: true})
		require.NoError(t, err)
		matches, lerr := store.LoadPoolMatches("reopen-force-cross")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusRunning, matches[0].Status, "the forced reopen lands")
	})
}
