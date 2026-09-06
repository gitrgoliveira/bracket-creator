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
	// The default-win ippon slots carry the FIK maru "○" marker, not a
	// waza letter, no technique was struck (mp-lybf). Bob (shiro/SideB)
	// is the survivor, so the fill lands on IpponsB.
	assert.Equal(t, []string{"○", "○"}, matches[0].IpponsB)
	assert.Empty(t, matches[0].IpponsA)

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
		t.Fatal("RecordDecisionTx deadlocked, concurrent kiken under WithTransaction did not return within 5s")
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
// undo path works correctly under the tx-aware variant, both the
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

	// Now undo, flip decisionBy so Bob is the new loser.
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

// TestRecordDecisionTx_RenamedLoser_RescoreDoesNotRestoreStillWithdrawnPlayer
// is PR #416 finding 1's repro. A bracket match carries no per-side ids
// (BracketMatch persists names only), so the FIRST kiken resolves the loser
// by a name-based roster scan -- correctly, at the time. If the roster is
// then edited so the withdrawn player's display name no longer matches the
// bracket row's stored SideA/SideB (a rename that never touches an
// already-generated bracket), re-recording the SAME decision (same
// decisionBy, hence the same intended loser) can no longer resolve a NEW
// ineligibility by name: recordIneligibilityFromDecision returns (nil, nil),
// exactly the "did not resolve/write a loser" case, not "this decision no
// longer makes anyone ineligible".
//
// Before the fix, RecordDecisionTx's restore loop treated status==nil as
// proof every MatchID==matchID/Eligible==false entry was stale and restored
// it -- flipping the STILL-withdrawn player back to Eligible:true even
// though the operator never rescinded anything. The fix gates the restore on
// the new decision actually having settled a loser when it is itself a
// withdrawal.
func TestRecordDecisionTx_RenamedLoser_RescoreDoesNotRestoreStillWithdrawnPlayer(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "renamed-loser-bracket"
	createTestCompetition(t, store, compID, "playoffs", 3)

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotEmpty(t, bracket.Rounds)
	matchID := bracket.Rounds[0][0].ID

	// First kiken: decisionBy=shiro -> Bob (SideB) withdraws, resolved by
	// name (bracket matches carry no ids at all).
	_, status, err := eng.RecordDecision(compID, matchID, "kiken-voluntary", "shiro", "injury", nil, false)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, bobID, status.PlayerID)
	assert.False(t, status.Eligible, "precondition: Bob must be ineligible after the first kiken")

	// Roster edit: Bob is renamed. The bracket row's own SideB still reads
	// "Bob" (a rename never rewrites an already-generated bracket).
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob-Renamed", Dojo: "B"},
	}))

	// Re-record the SAME decision (same decisionBy, same intended loser).
	// The name-based roster scan can no longer find "Bob" at all, so this
	// write resolves no CompetitorStatus -- it neither confirms nor changes
	// anyone's eligibility.
	_, status2, err := eng.RecordDecision(compID, matchID, "kiken-voluntary", "shiro", "injury", nil, false)
	require.NoError(t, err)
	assert.Nil(t, status2, "the write did not resolve/write a loser after the rename, so it must not report a restore")

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.Contains(t, statuses, bobID)
	assert.False(t, statuses[bobID].Eligible, "Bob is STILL withdrawn; a re-score the engine could not attribute must never silently restore him")
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

	// Now try the undo via the tx variant, should hit ErrDecisionLocked.
	var engErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, _, engErr = eng.RecordDecisionTx(tx, compID, "Pool A-0", "kiken", "shiro", "undo", nil, false)
		return nil
	})
	require.Error(t, engErr)
	assert.Truef(t, errors.Is(engErr, ErrDecisionLocked), "expected ErrDecisionLocked, got %v", engErr)
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

// TestRecordMatchResultWithIneligibilityTx_PreservesSideIDs guards the
// regression where scoring through the /score (transaction) path wiped the
// SideAID/SideBID stamped at generation: the whole-struct overwrite in the Tx
// write closure dropped them because score requests carry names only. It also
// checks WinnerID resolves from the WinnerSide hint, the only way to tell apart
// a winner when both sides share a name.
func TestRecordMatchResultWithIneligibilityTx_PreservesSideIDs(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tx-ids"
	createTestCompetition(t, store, compID, "league", 2)

	aID := helper.NewUUID4()
	bID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aID, Name: "Tanaka Kenji", Dojo: "Tokyo"},
		{ID: bID, Name: "Tanaka Kenji", Dojo: "Osaka"}, // same name, different id
	}))
	// Match with ids stamped at generation (sides stored by name).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka Kenji", SideB: "Tanaka Kenji",
			SideAID: aID, SideBID: bID, Status: state.MatchStatusScheduled},
	}))

	// Score via the Tx path with NAMES only (no ids), exactly what /score sends.
	result := &state.MatchResult{
		ID: "Pool A-0", SideA: "Tanaka Kenji", SideB: "Tanaka Kenji",
		Winner: "Tanaka Kenji", WinnerSide: "A", IpponsA: []string{"M"},
		Status: state.MatchStatusCompleted,
	}
	var engErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, engErr = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", result)
		return nil
	})
	require.NoError(t, engErr)

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, aID, matches[0].SideAID, "Tx score must preserve SideAID")
	assert.Equal(t, bID, matches[0].SideBID, "Tx score must preserve SideBID")
	assert.Equal(t, aID, matches[0].WinnerID, "WinnerSide=A must resolve WinnerID even when both sides share a name")
}

// TestRecordMatchResultWithIneligibilityTx_PreservesCorrectionReason guards the
// pool/bracket twin asymmetry: the bracket write is set-if-non-empty, so a
// stored CorrectionReason survives a write carrying none, while the pool write
// is a whole-struct overwrite that used to BLANK it. That matters because the
// kachinuki reopen path persists the operator's mandatory audit justification
// in that field, and the first "Record bout" after a reopen is a plain pool
// write with no reason of its own.
func TestRecordMatchResultWithIneligibilityTx_PreservesCorrectionReason(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tx-correction-reason"
	createTestCompetition(t, store, compID, "league", 2)

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: helper.NewUUID4(), Name: "Alice", Dojo: "A"},
		{ID: helper.NewUUID4(), Name: "Bob", Dojo: "B"},
	}))

	score := func(t *testing.T, result *state.MatchResult) state.MatchResult {
		t.Helper()
		var engErr error
		_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
			_, engErr = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", result)
			return nil
		})
		require.NoError(t, engErr)
		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, matches, 1)
		return matches[0]
	}

	t.Run("stored reason survives a write carrying none", func(t *testing.T) {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			Status:           state.MatchStatusRunning,
			CorrectionReason: "reopen: taisho bout must be re-fought",
		}}))

		got := score(t, &state.MatchResult{
			ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			IpponsA: []string{"M"}, Status: state.MatchStatusRunning,
		})
		assert.Equal(t, "reopen: taisho bout must be re-fought", got.CorrectionReason,
			"a bout write with no reason must not erase the reopen audit justification")
	})

	t.Run("an explicit reason still overwrites the stored one", func(t *testing.T) {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			Status:           state.MatchStatusCompleted,
			CorrectionReason: "reopen: taisho bout must be re-fought",
		}}))

		got := score(t, &state.MatchResult{
			ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			Winner: "Alice", IpponsA: []string{"M"},
			Status:           state.MatchStatusCompleted,
			CorrectionReason: "scorer error: wrong waza",
		})
		assert.Equal(t, "scorer error: wrong waza", got.CorrectionReason,
			"preservation must not shadow a reason the operator supplied")
	})
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
// tx.UpdatePoolMatchByID inside a WithTransaction does NOT deadlock,
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
			found, err := tx.UpdatePoolMatchByID(compID, "Pool A-0", func(r *state.MatchResult) error {
				r.Status = state.MatchStatusRunning
				return nil
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
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1", Dojo: "Dojo A1"}, {Name: "A2", Dojo: "Dojo A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1", Dojo: "Dojo B1"}, {Name: "B2", Dojo: "Dojo B2"}}},
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
		{Name: "A1", Dojo: "Dojo A1"}, {Name: "A2", Dojo: "Dojo A2"}, {Name: "B1", Dojo: "Dojo B1"}, {Name: "B2", Dojo: "Dojo B2"},
	}))

	// Save the initial scheduled pool matches.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: state.MatchStatusScheduled},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Status: state.MatchStatusScheduled},
	}))

	// Build the preview bracket from the pools.
	draw := helper.BuildKnockoutDraw(pools, 1, 1)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	bracket, err := eng.buildBracketFromDraw(comp, draw)
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

	// RE-SCORE Pool A, A1 still wins, just with a different ippons count.
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

	// Do NOT score the knockout leaf, it stays scheduled.

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

// TestPoolRescore_NamesakeFinisherSwap_KnockoutStarted_Rejected is the
// regression guard for the finding that oldTopN/newSet/displaced in
// RecordMatchResultWithIneligibilityTx were keyed by bare Player.Name: a
// re-score that swaps WHICH of two same-named, different-dojo competitors
// (CheckDuplicateEntriesByNameDojo explicitly allows this) holds a
// qualifying rank changed the IDENTITY at that rank without changing the
// bare name occupying it, so the name-keyed `displaced` computation stayed
// empty and hasStartedKnockoutMatchTx was never even consulted -- a silent
// identity swap under a started bracket slot.
//
// Fixture: Pool A has three competitors -- P1 (always wins, rank 1) and two
// "Alice"s from different dojos. AliceX beats AliceY initially (AliceX ranks
// 2nd, qualifying with poolWinners=2; AliceY ranks 3rd, non-qualifying). A
// knockout match is hand-crafted as already RUNNING for the qualifying
// "Alice" (there is no way to tell from the bracket's bare-name side which
// dojo actually qualified, which is itself the point: the guard's downstream
// bracket lookup is name-only and deliberately over-broad). Re-scoring the
// AliceX-vs-AliceY pool match to flip the winner promotes AliceY to 2nd and
// drops AliceX out of the top-N: same bare name at rank 2, different
// competitor. The identity-keyed guard must detect this as a displacement and
// reject the re-score; the pre-fix bare-name-keyed version saw "Alice" still
// present at rank 2 and let it through.
func TestPoolRescore_NamesakeFinisherSwap_KnockoutStarted_Rejected(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "namesake-guard-test"

	p1 := domain.Player{ID: "id-p1", Name: "P1", Dojo: "Dojo P1"}
	aliceX := domain.Player{ID: "id-alice-x", Name: "Alice", Dojo: "Dojo X"}
	aliceY := domain.Player{ID: "id-alice-y", Name: "Alice", Dojo: "Dojo Y"}

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          compID,
		Name:        compID,
		Kind:        "individual",
		Format:      state.CompFormatMixed,
		Status:      state.CompStatusPools,
		Courts:      []string{"A"},
		StartTime:   "09:00",
		PoolWinners: 2,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{p1, aliceX, aliceY}},
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{p1, aliceX, aliceY}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "P1", SideB: "Alice", SideAID: p1.ID, SideBID: aliceX.ID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "P1", SideB: "Alice", SideAID: p1.ID, SideBID: aliceY.ID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-2", SideA: "Alice", SideB: "Alice", SideAID: aliceX.ID, SideBID: aliceY.ID, Status: state.MatchStatusScheduled},
	}))

	scoreNamesakeMatchTx(t, eng, store, compID, "Pool A-0", "P1", "Alice", p1.ID, aliceX.ID, p1.ID)
	scoreNamesakeMatchTx(t, eng, store, compID, "Pool A-1", "P1", "Alice", p1.ID, aliceY.ID, p1.ID)
	scoreNamesakeMatchTx(t, eng, store, compID, "Pool A-2", "Alice", "Alice", aliceX.ID, aliceY.ID, aliceX.ID)

	// Sanity: AliceX (rank 2) qualifies with poolWinners=2, AliceY (rank 3) does not.
	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	require.Len(t, standings["Pool A"], 3)
	require.Equal(t, aliceX.ID, standings["Pool A"][1].Player.ID, "AliceX must be rank 2 before the re-score")

	// Hand-craft a started knockout match for the qualifying "Alice" (bracket
	// sides are bare names only; there is no dojo to tell the two apart).
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "r1-m0", SideA: "Alice", SideB: "P1", Status: state.MatchStatusRunning}},
		},
	}))

	// Re-score Pool A-2, flipping the winner: AliceY now beats AliceX.
	var rescore error
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, rescore = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-2", &state.MatchResult{
			SideA:    "Alice",
			SideB:    "Alice",
			SideAID:  aliceX.ID,
			SideBID:  aliceY.ID,
			Winner:   "Alice",
			WinnerID: aliceY.ID,
			IpponsB:  []string{"M"},
			Status:   state.MatchStatusCompleted,
		})
		return nil
	})
	require.NoError(t, txErr)
	require.Error(t, rescore, "swapping which namesake holds the qualifying rank must be rejected while the knockout leaf is started")
	assert.ErrorIs(t, rescore, ErrDownstreamKnockoutScored)
	var dkErr *DownstreamKnockoutScoredError
	require.ErrorAs(t, rescore, &dkErr)
	assert.Equal(t, "Pool A", dkErr.Pool)

	// Verify the pool match was rolled back: AliceX must still be the recorded winner.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var poolA2 *state.MatchResult
	for i := range matches {
		if matches[i].ID == "Pool A-2" {
			poolA2 = &matches[i]
			break
		}
	}
	require.NotNil(t, poolA2)
	assert.Equal(t, aliceX.ID, poolA2.WinnerID, "pool match result must have been rolled back to AliceX as winner")
}

// scoreNamesakeMatchTx is a test helper mirroring scorePoolMatchTx but for
// fixtures where SideA/SideB can be the SAME display name (namesakes from
// different dojos): it stamps SideAID/SideBID/WinnerID explicitly rather than
// inferring the winner from the (ambiguous) bare Winner name alone.
func scoreNamesakeMatchTx(t *testing.T, eng *Engine, store *state.Store, compID, matchID, sideA, sideB, sideAID, sideBID, winnerID string) {
	t.Helper()
	result := &state.MatchResult{
		SideA:    sideA,
		SideB:    sideB,
		SideAID:  sideAID,
		SideBID:  sideBID,
		Winner:   sideA,
		WinnerID: winnerID,
		IpponsA:  []string{"M"},
		Status:   state.MatchStatusCompleted,
	}
	if winnerID == sideBID {
		result.Winner = sideB
		result.IpponsA = nil
		result.IpponsB = []string{"M"}
	}
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, result)
		return err
	})
	require.NoError(t, txErr)
}

// TestPoolRescore_NonMixedComp_GuardIsNoOp verifies the guard is skipped
// entirely for standalone (non-mixed) competitions.
func TestPoolRescore_NonMixedComp_GuardIsNoOp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-guard-test"
	createTestCompetition(t, store, compID, "league", 2)

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "Dojo Alice"}, {Name: "Bob", Dojo: "Dojo Bob"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice",
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	// Re-score to flip winner, should succeed with no guard.
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
// pool result preserved, never silently committed. (Copilot review, PR #246.)
func TestPoolRescore_CorruptBracket_FailsClosed(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "guard-corrupt"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1", Dojo: "Dojo A1"}, {Name: "A2", Dojo: "Dojo A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1", Dojo: "Dojo B1"}, {Name: "B2", Dojo: "Dojo B2"}}},
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00", PoolWinners: 1,
	}))
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1", Dojo: "Dojo A1"}, {Name: "A2", Dojo: "Dojo A2"}, {Name: "B1", Dojo: "Dojo B1"}, {Name: "B2", Dojo: "Dojo B2"},
	}))
	// Both pools already decided: A1 1st in Pool A, B1 1st in Pool B.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Winner: "B1", IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	// Build + save a valid bracket, then corrupt it on disk. The tx read path
	// (loadBracketLocked) parses the file directly (no cache), so the corrupt
	// bytes surface as a parse error inside the guard.
	draw := helper.BuildKnockoutDraw(pools, 1, 1)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	bracket, err := eng.buildBracketFromDraw(comp, draw)
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
// returns the displaced name actually sitting in the started match, not just
// the first input name, so the 409 payload's Finisher stays consistent with
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
	// engage for a bracket match, the re-score is allowed.
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

// TestCourtOccupied covers the PURE court-occupancy scan extracted for E4
// (mp-gmcg review): given already-loaded pool matches + bracket, find the
// RUNNING match on a court other than skipMatchID, searching pool → rounds →
// bronze. The loading wrapper (courtFreeInCompTxWith) and its id-only entry
// point (checkCourtExclusivityTx) are covered end-to-end by the reopen
// court-busy tests.
func TestCourtOccupied(t *testing.T) {
	running := func(id, court string) state.MatchResult {
		return state.MatchResult{ID: id, Court: court, Status: state.MatchStatusRunning}
	}
	brk := func(id, court string, st state.MatchStatus) state.BracketMatch {
		return state.BracketMatch{ID: id, Court: court, Status: st}
	}

	t.Run("finds a running pool match on the court", func(t *testing.T) {
		pool := []state.MatchResult{running("P1-0", "A"), running("P1-1", "B")}
		occ := courtOccupied(pool, nil, "A", "other")
		require.NotNil(t, occ)
		assert.Equal(t, "P1-0", occ.MatchID)
	})

	t.Run("skips the target match (skipMatchID)", func(t *testing.T) {
		pool := []state.MatchResult{running("P1-0", "A")}
		assert.Nil(t, courtOccupied(pool, nil, "A", "P1-0"),
			"the match being (re)opened is itself on the court and must be skipped")
	})

	t.Run("ignores non-running matches", func(t *testing.T) {
		pool := []state.MatchResult{
			{ID: "done", Court: "A", Status: state.MatchStatusCompleted},
			{ID: "sched", Court: "A", Status: state.MatchStatusScheduled},
		}
		assert.Nil(t, courtOccupied(pool, nil, "A", "x"))
	})

	t.Run("ignores a running match on a DIFFERENT court", func(t *testing.T) {
		pool := []state.MatchResult{running("P1-0", "B")}
		assert.Nil(t, courtOccupied(pool, nil, "A", "x"))
	})

	t.Run("finds a running bracket ROUND match", func(t *testing.T) {
		b := &state.Bracket{Rounds: [][]state.BracketMatch{
			{brk("R1-0", "A", state.MatchStatusScheduled)},
			{brk("R2-0", "A", state.MatchStatusRunning)},
		}}
		occ := courtOccupied(nil, b, "A", "x")
		require.NotNil(t, occ)
		assert.Equal(t, "R2-0", occ.MatchID)
	})

	t.Run("finds the running BRONZE sibling a rounds-only scan would miss", func(t *testing.T) {
		b := &state.Bracket{
			Rounds:          [][]state.BracketMatch{{brk("R1-0", "A", state.MatchStatusScheduled)}},
			ThirdPlaceMatch: &state.BracketMatch{ID: "BRONZE", Court: "A", Status: state.MatchStatusRunning},
		}
		occ := courtOccupied(nil, b, "A", "x")
		require.NotNil(t, occ)
		assert.Equal(t, "BRONZE", occ.MatchID)
	})

	t.Run("skips the bronze when it IS the target", func(t *testing.T) {
		b := &state.Bracket{ThirdPlaceMatch: &state.BracketMatch{ID: "BRONZE", Court: "A", Status: state.MatchStatusRunning}}
		assert.Nil(t, courtOccupied(nil, b, "A", "BRONZE"))
	})

	t.Run("pool is scanned before the bracket (same court, both running)", func(t *testing.T) {
		pool := []state.MatchResult{running("P1-0", "A")}
		b := &state.Bracket{Rounds: [][]state.BracketMatch{{brk("R1-0", "A", state.MatchStatusRunning)}}}
		occ := courtOccupied(pool, b, "A", "x")
		require.NotNil(t, occ)
		assert.Equal(t, "P1-0", occ.MatchID, "pool home is searched first")
	})

	t.Run("nothing loaded, nothing occupied", func(t *testing.T) {
		assert.Nil(t, courtOccupied(nil, nil, "A", "x"))
		assert.Nil(t, courtOccupied([]state.MatchResult{}, &state.Bracket{}, "A", "x"))
	})

	t.Run("does not stamp CompID (the caller carries it)", func(t *testing.T) {
		occ := courtOccupied([]state.MatchResult{running("P1-0", "A")}, nil, "A", "x")
		require.NotNil(t, occ)
		assert.Empty(t, occ.CompID, "the pure scan has no compID; wrappers stamp it")
	})
}
