package engine

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecordDecisionTx_BasicEquivalence pins that the tx-aware
// RecordDecisionTx produces the same on-disk outcome as the
// non-tx RecordDecision for a vanilla kiken write.
func TestRecordDecisionTx_BasicEquivalence(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tx-basic"
	createTestCompetition(t, store, compID, "pools", 2)

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
	}))

	var (
		result *state.MatchResult
		status *domain.CompetitorStatus
		engErr error
	)
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		result, status, engErr = eng.RecordDecisionTx(tx, compID, "Pool A-0", "kiken", "aka", "knee injury", nil, false)
		return nil
	})
	require.NoError(t, txErr)
	require.NoError(t, engErr)
	require.NotNil(t, result)
	require.Equal(t, "Bob", result.Winner)
	require.NotNil(t, status)
	assert.Equal(t, aliceID, status.PlayerID)
	assert.False(t, status.Eligible)

	// Verify the match landed on disk.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "Bob", matches[0].Winner)
	assert.Equal(t, "kiken", matches[0].Decision)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status)

	// Verify ineligibility landed on disk.
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	st, ok := statuses[aliceID]
	require.True(t, ok)
	assert.False(t, st.Eligible)
}

// TestRecordDecisionTx_ConcurrentDoesNotDeadlock asserts the tx-aware
// path serializes on the per-comp lock without deadlocking. This is
// the load-bearing T156 invariant: the migration must not hang under
// concurrent kiken writes.
func TestRecordDecisionTx_ConcurrentDoesNotDeadlock(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tx-deadlock"
	createTestCompetition(t, store, compID, "pools", 2)

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	carolID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Carol", SideB: "Alice", Status: state.MatchStatusScheduled},
	}))

	type res struct {
		err     error
		matchID string
	}
	results := make(chan res, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		var engErr error
		_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
			_, _, engErr = eng.RecordDecisionTx(tx, compID, "Pool A-0", "kiken", "aka", "race A", nil, false)
			return nil
		})
		results <- res{err: engErr, matchID: "Pool A-0"}
	}()
	go func() {
		defer wg.Done()
		var engErr error
		_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
			_, _, engErr = eng.RecordDecisionTx(tx, compID, "Pool A-1", "kiken", "shiro", "race B", nil, false)
			return nil
		})
		results <- res{err: engErr, matchID: "Pool A-1"}
	}()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RecordDecisionTx deadlocked — concurrent kiken under WithTransaction did not return within 5s")
	}

	var winners, losers []res
	for range 2 {
		r := <-results
		if r.err == nil {
			winners = append(winners, r)
		} else {
			losers = append(losers, r)
		}
	}
	require.Len(t, winners, 1, "exactly one concurrent kiken should succeed; got winners=%+v losers=%+v", winners, losers)
	require.Len(t, losers, 1, "exactly one concurrent kiken should be rejected; got winners=%+v losers=%+v", winners, losers)

	var alreadyErr *AlreadyIneligibleError
	require.ErrorAs(t, losers[0].err, &alreadyErr, "loser must be *AlreadyIneligibleError")
	assert.Equal(t, aliceID, alreadyErr.PlayerID)
	assert.Equal(t, winners[0].matchID, alreadyErr.MatchID)

	// Final ineligibility record should reflect the winner.
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	st, ok := statuses[aliceID]
	require.True(t, ok)
	assert.False(t, st.Eligible)
	assert.Equal(t, winners[0].matchID, st.MatchID)

	// K3 rollback: the loser's match should have rolled back to Scheduled
	// because the partial write was reverted within the same tx.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		if m.ID == losers[0].matchID {
			assert.Equal(t, state.MatchStatusScheduled, m.Status,
				"K3 rollback inside tx must revert the losing operator's match write; got %+v", m)
			assert.Empty(t, m.Decision, "K3 rollback should clear Decision; got %+v", m)
		}
	}
}

// TestRecordDecisionTx_KikenUndoSucceeds verifies that the T103 kiken-
// undo path works correctly under the tx-aware variant — both the
// downstream-match lock check and the prior-loser eligibility restore
// dispatch through the supplied StoreTx without re-acquiring the lock.
func TestRecordDecisionTx_KikenUndoSucceeds(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tx-undo"
	createTestCompetition(t, store, compID, "pools", 3)

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	carolID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled},
	}))

	// Record kiken (Alice loser) via the tx variant.
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, _, err := eng.RecordDecisionTx(tx, compID, "Pool A-0", "kiken", "aka", "knee injury", nil, false)
		return err
	})

	// Now undo — flip decisionBy so Bob is the new loser.
	var (
		result *state.MatchResult
		status *domain.CompetitorStatus
		engErr error
	)
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		result, status, engErr = eng.RecordDecisionTx(tx, compID, "Pool A-0", "kiken", "shiro", "scoring fix", nil, false)
		return nil
	})
	require.NoError(t, engErr)
	require.NotNil(t, result)
	assert.Equal(t, "Alice", result.Winner)
	require.NotNil(t, status, "expected restored-eligibility status for Alice")
	assert.Equal(t, aliceID, status.PlayerID)
	assert.True(t, status.Eligible)

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.True(t, statuses[aliceID].Eligible)
}

// TestRecordDecisionTx_DownstreamLockReturnsErr asserts the T103
// decision-lock check fires correctly in the tx-aware path.
func TestRecordDecisionTx_DownstreamLockReturnsErr(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tx-lock"
	createTestCompetition(t, store, compID, "pools", 3)

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	carolID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusRunning},
	}))

	// Pre-record a kiken so the next call is the "undo" path.
	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "first", nil, false)
	require.NoError(t, err)

	// Now try the undo via the tx variant — should hit ErrDecisionLocked.
	var engErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, _, engErr = eng.RecordDecisionTx(tx, compID, "Pool A-0", "kiken", "shiro", "undo", nil, false)
		return nil
	})
	require.Error(t, engErr)
	assert.True(t, errors.Is(engErr, ErrDecisionLocked), "expected ErrDecisionLocked, got %v", engErr)
}

// TestRecordMatchResultWithIneligibilityTx_Basic verifies the
// tx-aware score-write produces the same on-disk outcome as the
// non-tx variant.
func TestRecordMatchResultWithIneligibilityTx_Basic(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tx-score"
	createTestCompetition(t, store, compID, "pools", 2)

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
	}))

	result := &state.MatchResult{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
		Winner: "Alice", IpponsA: []string{"M"},
		Status: state.MatchStatusCompleted,
	}
	var (
		status *domain.CompetitorStatus
		engErr error
	)
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		status, engErr = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", result)
		return nil
	})
	require.NoError(t, engErr)
	assert.Nil(t, status, "no kiken/fusenpai → no status")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "Alice", matches[0].Winner)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status)
}

// TestStartMatchTx_BlocksIneligibleParticipant verifies the FR-035
// pre-flight gate. After Alice is recorded as kiken'd on Pool A-0
// (her status: ineligible, matchID=Pool A-0), StartMatchTx for
// Pool A-1 (Alice vs Carol) MUST return *IneligibleCompetitorError so
// the score handler can return 409. UAT-discovered gap (review v3),
// FR-035.
func TestStartMatchTx_BlocksIneligibleParticipant(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "fr035-block"
	createTestCompetition(t, store, compID, "pools", 2)

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	carolID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled},
	}))
	// Kiken Alice from Pool A-0.
	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "knee", nil, false)
	require.NoError(t, err)

	// StartMatchTx for Pool A-1 (different match, Alice still a
	// participant) → ineligible error.
	var startErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		startErr = eng.StartMatchTx(tx, compID, "Pool A-1")
		return nil
	})
	require.Error(t, startErr)
	var ineligErr *IneligibleCompetitorError
	require.ErrorAs(t, startErr, &ineligErr)
	assert.Equal(t, aliceID, ineligErr.PlayerID)
	assert.Contains(t, ineligErr.Reason, "kiken")

	// StartMatchTx for Pool A-0 (the SOURCE match itself) → allowed,
	// so the undo path works.
	var srcErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		srcErr = eng.StartMatchTx(tx, compID, "Pool A-0")
		return nil
	})
	assert.NoError(t, srcErr, "the match that recorded the ineligibility must be re-scoreable")
}

// TestStoreTxUpdatePoolMatchByIDLockFree pins that calling
// tx.UpdatePoolMatchByID inside a WithTransaction does NOT deadlock —
// proves the lock-free dispatch on the storeTx side is wired up.
func TestStoreTxUpdatePoolMatchByIDLockFree(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tx-pool-update"
	createTestCompetition(t, store, compID, "pools", 2)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "X", SideB: "Y", Status: state.MatchStatusScheduled},
	}))
	_ = eng // used as fixture only; this test exercises the tx API directly

	done := make(chan struct{})
	go func() {
		_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
			found, err := tx.UpdatePoolMatchByID(compID, "Pool A-0", func(r *state.MatchResult) {
				r.Status = state.MatchStatusRunning
			})
			require.NoError(t, err)
			require.True(t, found)
			return nil
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("tx.UpdatePoolMatchByID deadlocked inside WithTransaction")
	}

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusRunning, matches[0].Status)
}
