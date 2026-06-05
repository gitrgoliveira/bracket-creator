package engine

import (
	"errors"
	"os"
	"path/filepath"
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
	createTestCompetition(t, store, compID, "league", 2)

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
	createTestCompetition(t, store, compID, "league", 2)

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
	createTestCompetition(t, store, compID, "league", 3)

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
	createTestCompetition(t, store, compID, "league", 3)

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
	createTestCompetition(t, store, compID, "league", 2)

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
	createTestCompetition(t, store, compID, "league", 2)

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
	createTestCompetition(t, store, compID, "league", 2)
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

// TestRecordMatchResultWithIneligibilityTx_HansokuAutoAward verifies
// that the tx-aware scoring path also applies the FIK Article 20
// hansoku→ippon auto-award.
func TestRecordMatchResultWithIneligibilityTx_HansokuAutoAward(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tx-hansoku"
	createTestCompetition(t, store, compID, "league", 2)

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
	}))

	result := &state.MatchResult{
		Winner:   "Alice",
		HansokuA: 2,
		IpponsA:  []string{"M"},
		Status:   state.MatchStatusCompleted,
	}
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", result)
		return err
	})
	require.NoError(t, txErr)

	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, []string{"H"}, stored[0].IpponsB)
	assert.Equal(t, []string{"M"}, stored[0].IpponsA)
}

// --- mp-e2k1: pool re-score guard against scored downstream knockout --------

// saveMixedCompForGuardTest sets up a minimal mixed competition with two pools
// (poolWinners=1), saves the scheduled pool matches and the preview knockout
// bracket. Returns the engine, store, and compID. Tests that need the round-0
// knockout match read it from the saved bracket via store.LoadBracket →
// Rounds[0][0].ID.
func saveMixedCompForGuardTest(t *testing.T, teamSize int) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	compID := "guard-test"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}}},
	}
	// Build competition.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        compID,
		Kind:        "individual",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		PoolWinners: 1,
		TeamSize:    teamSize,
	}))
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"}, {Name: "B2"},
	}))

	// Save the initial scheduled pool matches.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: state.MatchStatusScheduled},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Status: state.MatchStatusScheduled},
	}))

	// Build the preview bracket from the pools.
	finals := helper.GenerateFinals(pools, 1)
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	leaves := helper.TreeToLeafArray(tree)
	comp, _ := store.LoadCompetition(compID)
	bracket, err := eng.buildBracketFromLeaves(comp, leaves)
	require.NoError(t, err)
	bracket.Preview = true
	require.NoError(t, store.SaveBracket(compID, bracket))

	return eng, store, compID
}

// scorePoolMatchTx is a test helper that writes a pool match result inside a tx.
func scorePoolMatchTx(t *testing.T, eng *Engine, store *state.Store, compID, matchID, sideA, sideB, winner string) {
	t.Helper()
	result := &state.MatchResult{
		SideA:   sideA,
		SideB:   sideB,
		Winner:  winner,
		IpponsA: []string{"M"},
		Status:  state.MatchStatusCompleted,
	}
	if winner == sideB {
		result.IpponsA = nil
		result.IpponsB = []string{"M"}
	}
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, result)
		return err
	})
	require.NoError(t, txErr)
}

// TestPoolRescore_NoFinisherChange_Allowed verifies that re-scoring a pool match
// does NOT trigger the guard when the top-N finisher identity is unchanged.
// (e.g. A1 still wins after re-scoring with different ippons)
func TestPoolRescore_NoFinisherChange_Allowed(t *testing.T) {
	eng, store, compID := saveMixedCompForGuardTest(t, 0)

	// Score Pool A: A1 wins. Score Pool B: B1 wins.
	scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
	scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")

	// Resolve the bracket so A1 and B1 land in the knockout leaf.
	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	// Score the knockout match (A1 vs B1) → A1 wins.
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, b.Rounds, 1)
	require.Len(t, b.Rounds[0], 1)
	knockoutMatchID := b.Rounds[0][0].ID

	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, knockoutMatchID, &state.MatchResult{
			SideA:   "A1",
			SideB:   "B1",
			Winner:  "A1",
			IpponsA: []string{"M"},
			Status:  state.MatchStatusCompleted,
		})
		return err
	})
	require.NoError(t, txErr)

	// RE-SCORE Pool A — A1 still wins, just with a different ippons count.
	// The finisher set is unchanged, so the guard must NOT fire.
	var rescore error
	txErr = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, rescore = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", &state.MatchResult{
			SideA:   "A1",
			SideB:   "A2",
			Winner:  "A1",
			IpponsA: []string{"M", "M"},
			Status:  state.MatchStatusCompleted,
		})
		return nil
	})
	require.NoError(t, txErr)
	assert.NoError(t, rescore, "re-score with same finisher must be allowed even after knockout match is scored")
}

// TestPoolRescore_FinisherFlip_LeafScheduled_Allowed verifies that re-scoring
// a pool match to flip the 1st-place finisher while the knockout leaf is still
// scheduled succeeds and does NOT trigger the guard.
func TestPoolRescore_FinisherFlip_LeafScheduled_Allowed(t *testing.T) {
	eng, store, compID := saveMixedCompForGuardTest(t, 0)

	// Score Pool A: A1 wins. Score Pool B: B1 wins.
	scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
	scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")

	// Resolve the bracket so the knockout leaf has real names.
	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	// Do NOT score the knockout leaf — it stays scheduled.

	// RE-SCORE Pool A to flip finisher (A2 now wins).
	var rescore error
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, rescore = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", &state.MatchResult{
			SideA:   "A1",
			SideB:   "A2",
			Winner:  "A2",
			IpponsB: []string{"M"},
			Status:  state.MatchStatusCompleted,
		})
		return nil
	})
	require.NoError(t, txErr)
	assert.NoError(t, rescore, "finisher flip on unscored knockout leaf must be allowed")
}

// TestPoolRescore_FinisherFlip_KnockoutCompleted_Rejected verifies that re-scoring
// a pool match to flip a finisher whose knockout leaf is completed returns
// DownstreamKnockoutScoredError (wrapping ErrDownstreamKnockoutScored) and rolls
// back the pool-match result to the prior state.
func TestPoolRescore_FinisherFlip_KnockoutCompleted_Rejected(t *testing.T) {
	eng, store, compID := saveMixedCompForGuardTest(t, 0)

	// Score Pool A (A1 wins) and Pool B (B1 wins).
	scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
	scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")

	// Resolve bracket.
	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	// Score the knockout match (A1 vs B1) → A1 wins.
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, b.Rounds[0], 1)
	knockoutMatchID := b.Rounds[0][0].ID

	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, knockoutMatchID, &state.MatchResult{
			SideA:   "A1",
			SideB:   "B1",
			Winner:  "A1",
			IpponsA: []string{"M"},
			Status:  state.MatchStatusCompleted,
		})
		return err
	})
	require.NoError(t, txErr)

	// Attempt to re-score Pool A flipping the finisher (A2 beats A1).
	var rescore error
	txErr = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, rescore = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", &state.MatchResult{
			SideA:   "A1",
			SideB:   "A2",
			Winner:  "A2",
			IpponsB: []string{"M"},
			Status:  state.MatchStatusCompleted,
		})
		return nil
	})
	require.NoError(t, txErr)
	require.Error(t, rescore, "flipping finisher of a scored knockout must be rejected")
	assert.ErrorIs(t, rescore, ErrDownstreamKnockoutScored)

	var dkErr *DownstreamKnockoutScoredError
	require.ErrorAs(t, rescore, &dkErr)
	assert.Equal(t, "Pool A", dkErr.Pool)
	assert.Equal(t, "A1", dkErr.Finisher)
	assert.Equal(t, knockoutMatchID, dkErr.MatchID)

	// Verify the pool match was rolled back to prior state (A1 wins).
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var poolA0 *state.MatchResult
	for i := range matches {
		if matches[i].ID == "Pool A-0" {
			poolA0 = &matches[i]
			break
		}
	}
	require.NotNil(t, poolA0)
	assert.Equal(t, "A1", poolA0.Winner, "pool match result must have been rolled back to prior (A1 wins)")

	// Verify the bracket was NOT modified (A1 is still the knockout match winner).
	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "A1", b.Rounds[0][0].Winner, "knockout match winner must remain unchanged after rejected pool re-score")
}

// TestPoolRescore_FinisherFlip_KnockoutRunning_Rejected verifies the guard
// also fires when the downstream bracket match is RUNNING (not yet completed).
func TestPoolRescore_FinisherFlip_KnockoutRunning_Rejected(t *testing.T) {
	eng, store, compID := saveMixedCompForGuardTest(t, 0)

	scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
	scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	// Mark the knockout match as RUNNING (not completed).
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	knockoutMatchID := b.Rounds[0][0].ID

	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, knockoutMatchID, &state.MatchResult{
			SideA:  "A1",
			SideB:  "B1",
			Status: state.MatchStatusRunning,
		})
		return err
	})
	require.NoError(t, txErr)

	// Attempt re-score Pool A flipping the finisher.
	var rescore error
	txErr = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, rescore = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", &state.MatchResult{
			SideA:   "A1",
			SideB:   "A2",
			Winner:  "A2",
			IpponsB: []string{"M"},
			Status:  state.MatchStatusCompleted,
		})
		return nil
	})
	require.NoError(t, txErr)
	require.Error(t, rescore)
	assert.ErrorIs(t, rescore, ErrDownstreamKnockoutScored, "running knockout leaf must also block a finisher flip")
}

// TestPoolRescore_NonMixedComp_GuardIsNoOp verifies the guard is skipped
// entirely for standalone (non-mixed) competitions.
func TestPoolRescore_NonMixedComp_GuardIsNoOp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-guard-test"
	createTestCompetition(t, store, compID, "league", 2)

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice"}, {Name: "Bob"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	// Re-score to flip winner — should succeed with no guard.
	var rescore error
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, rescore = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", &state.MatchResult{
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Bob",
			IpponsB: []string{"M"},
			Status:  state.MatchStatusCompleted,
		})
		return nil
	})
	require.NoError(t, txErr)
	assert.NoError(t, rescore, "non-mixed comp must never trigger the downstream knockout guard")
}

// TestPoolRescore_TeamMixed_GuardFires verifies that the guard applies equally
// to team-format mixed competitions (TeamSize > 0).
func TestPoolRescore_TeamMixed_GuardFires(t *testing.T) {
	eng, store, compID := saveMixedCompForGuardTest(t, 3 /* TeamSize */)

	// Score Pool A and Pool B.
	scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
	scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")

	// Resolve the bracket.
	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	// Score the knockout leaf.
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	knockoutMatchID := b.Rounds[0][0].ID

	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, knockoutMatchID, &state.MatchResult{
			SideA:   "A1",
			SideB:   "B1",
			Winner:  "A1",
			IpponsA: []string{"M"},
			Status:  state.MatchStatusCompleted,
		})
		return err
	})
	require.NoError(t, txErr)

	// Attempt to flip Pool A finisher.
	var rescore error
	txErr = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, rescore = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", &state.MatchResult{
			SideA:   "A1",
			SideB:   "A2",
			Winner:  "A2",
			IpponsB: []string{"M"},
			Status:  state.MatchStatusCompleted,
		})
		return nil
	})
	require.NoError(t, txErr)
	require.Error(t, rescore, "team mixed comp must also be protected by the downstream knockout guard")
	assert.ErrorIs(t, rescore, ErrDownstreamKnockoutScored)
}

// TestPoolRescore_CorruptBracket_FailsClosed verifies the guard does NOT fail
// open when the bracket can't be read. A displacing re-score whose verification
// hits a corrupt bracket.json must be rejected (error surfaced) and the prior
// pool result preserved — never silently committed. (Copilot review, PR #246.)
func TestPoolRescore_CorruptBracket_FailsClosed(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "guard-corrupt"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}}},
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00", PoolWinners: 1,
	}))
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"}, {Name: "B2"},
	}))
	// Both pools already decided: A1 1st in Pool A, B1 1st in Pool B.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Winner: "B1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	// Build + save a valid bracket, then corrupt it on disk. The tx read path
	// (loadBracketLocked) parses the file directly (no cache), so the corrupt
	// bytes surface as a parse error inside the guard.
	finals := helper.GenerateFinals(pools, 1)
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	comp, _ := store.LoadCompetition(compID)
	bracket, err := eng.buildBracketFromLeaves(comp, helper.TreeToLeafArray(tree))
	require.NoError(t, err)
	require.NoError(t, store.SaveBracket(compID, bracket))
	bracketPath := filepath.Join(dir, "competitions", compID, "bracket.json")
	require.NoError(t, os.WriteFile(bracketPath, []byte("{ this is not valid json"), 0o600))

	// Re-score Pool A-0 to flip the finisher (A1 → A2). This displaces A1, so the
	// guard tries to read the (now corrupt) bracket. It must fail closed.
	var rescore error
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, rescore = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", &state.MatchResult{
			SideA: "A1", SideB: "A2", Winner: "A2", IpponsB: []string{"M"}, Status: state.MatchStatusCompleted,
		})
		return nil // mirror the handler: surface the engine error out-of-band
	})
	require.NoError(t, txErr)
	require.Error(t, rescore, "a corrupt bracket must make the guard fail closed, not silently allow the re-score")
	assert.NotErrorIs(t, rescore, ErrDownstreamKnockoutScored, "this is a read fault, not a clean downstream-scored rejection")

	// The corrupting re-score must NOT have persisted: Pool A-0 still A1-wins.
	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var poolA0 *state.MatchResult
	for i := range stored {
		if stored[i].ID == "Pool A-0" {
			poolA0 = &stored[i]
		}
	}
	require.NotNil(t, poolA0)
	assert.Equal(t, "A1", poolA0.Winner, "prior pool result must be preserved after a fail-closed rejection")
}

// TestHasStartedKnockoutMatchTx_ReportsMatchedFinisher verifies the helper
// returns the displaced name actually sitting in the started match — not just
// the first input name — so the 409 payload's Finisher stays consistent with
// MatchID when more than one finisher is displaced (poolWinners > 1).
// (Copilot review, PR #246.)
func TestHasStartedKnockoutMatchTx_ReportsMatchedFinisher(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "matched-finisher"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00", PoolWinners: 2,
	}))
	// A1's leaf is still scheduled; A2's leaf is running. Scanning for both must
	// return A2 (the one in the started match), regardless of slice order.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "r0-m0", SideA: "A1", SideB: "X1", Status: state.MatchStatusScheduled},
			{ID: "r0-m1", SideA: "X2", SideB: "A2", Status: state.MatchStatusRunning},
		}},
	}))

	run := func(names []string) (string, string) {
		var gotName, gotID string
		require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
			var err error
			gotName, gotID, err = eng.hasStartedKnockoutMatchTx(tx, compID, names)
			return err
		}))
		return gotName, gotID
	}

	name, id := run([]string{"A1", "A2"})
	assert.Equal(t, "A2", name, "must report the finisher in the started match, not displaced[0]")
	assert.Equal(t, "r0-m1", id)

	// Order-independence: A2 first must give the same result.
	name, id = run([]string{"A2", "A1"})
	assert.Equal(t, "A2", name)
	assert.Equal(t, "r0-m1", id)

	// Only a scheduled-leaf finisher → no started match found.
	name, id = run([]string{"A1"})
	assert.Empty(t, name)
	assert.Empty(t, id)
}

// TestKnockoutRescore_NotGatedAsPoolMatch verifies the guard does not mistake a
// knockout (bracket) match for a pool match. Bracket IDs ("m-rN-i") parse as a
// pool via poolNameFromMatchID's trailing-"-digits" rule, so without the
// IsPoolMatchID gate a KO re-score would run the pool-standings guard. Re-scoring
// a KO match must succeed and never raise DownstreamKnockoutScoredError.
// (Copilot review, PR #246.)
func TestKnockoutRescore_NotGatedAsPoolMatch(t *testing.T) {
	eng, store, compID := saveMixedCompForGuardTest(t, 0)

	scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
	scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")
	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	knockoutMatchID := b.Rounds[0][0].ID
	require.False(t, IsPoolMatchID(knockoutMatchID), "precondition: KO match ID must not be a pool ID")

	// Score the knockout match (A1 beats B1).
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, knockoutMatchID, &state.MatchResult{
			SideA: "A1", SideB: "B1", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted,
		})
		return err
	}))

	// RE-SCORE the same knockout match (flip to B1). The pool guard must not
	// engage for a bracket match — the re-score is allowed.
	var rescore error
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, rescore = eng.RecordMatchResultWithIneligibilityTx(tx, compID, knockoutMatchID, &state.MatchResult{
			SideA: "A1", SideB: "B1", Winner: "B1", IpponsB: []string{"M"}, Status: state.MatchStatusCompleted,
		})
		return nil
	}))
	assert.NoError(t, rescore, "knockout re-score must not be gated by the pool re-score guard")
	assert.NotErrorIs(t, rescore, ErrDownstreamKnockoutScored)
}
