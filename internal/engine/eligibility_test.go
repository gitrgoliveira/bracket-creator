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

// TestStartMatchBlockedByIneligibleCompetitor verifies FR-035: when a
// participant has CompetitorStatus{Eligible: false} in the store,
// engine.StartMatch must return *IneligibleCompetitorError matching
// errors.Is(err, ErrIneligibleCompetitor) so the score handler can
// reply 409 with the player/reason.
func TestStartMatchBlockedByIneligibleCompetitor(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "elig-blocked"

	createTestCompetition(t, store, compID, "pools", 2)

	// Seed participants with explicit UUIDs — state.LoadParticipants
	// only treats the first column as an ID when it parses as UUID v4.
	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	players := []helper.Player{
		{ID: aliceID, Name: "Alice", Dojo: "DojoA"},
		{ID: bobID, Name: "Bob", Dojo: "DojoB"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	matches := []state.MatchResult{{
		ID:     "Pool A-0",
		SideA:  "Alice",
		SideB:  "Bob",
		Status: state.MatchStatusScheduled,
	}}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID:   aliceID,
		Eligible:   false,
		Reason:     "kiken at m_prev",
		MatchID:    "m_prev",
		RecordedAt: time.Now().UTC(),
	}))

	err := eng.StartMatch(compID, "Pool A-0")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIneligibleCompetitor), "want errors.Is == ErrIneligibleCompetitor, got %v", err)

	var ineligErr *IneligibleCompetitorError
	require.ErrorAs(t, err, &ineligErr)
	assert.Equal(t, aliceID, ineligErr.PlayerID)
	assert.Equal(t, "kiken at m_prev", ineligErr.Reason)
}

// TestRecordDecision_KikenUndo exercises the T103/CHK024 contract:
// once a kiken has been recorded, a follow-up POST /decision that
// overwrites it is allowed only if no subsequent match involving
// either side has started. The override is gated by an explicit
// `force` flag; on a successful undo the prior loser's
// CompetitorStatus is restored to Eligible=true.
func TestRecordDecision_KikenUndo(t *testing.T) {
	setup := func(t *testing.T) (*Engine, *state.Store, string, string, string) {
		t.Helper()
		eng, store, _ := setupTestEngine(t)
		compID := "undo-test"
		createTestCompetition(t, store, compID, "pools", 3)

		aliceID := helper.NewUUID4()
		bobID := helper.NewUUID4()
		carolID := helper.NewUUID4()
		require.NoError(t, store.SaveParticipants(compID, []helper.Player{
			{ID: aliceID, Name: "Alice", Dojo: "A"},
			{ID: bobID, Name: "Bob", Dojo: "B"},
			{ID: carolID, Name: "Carol", Dojo: "C"},
		}))
		// Pool with three matches so we have a "subsequent" match for
		// each participant to drive the downstream-check coverage.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
			{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled},
			{ID: "Pool A-2", SideA: "Bob", SideB: "Carol", Status: state.MatchStatusScheduled},
		}))
		// Record kiken on Alice at Pool A-0 first so the test starts
		// from the "prior decision" state.
		_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "knee injury", nil, false)
		require.NoError(t, err)
		return eng, store, compID, aliceID, bobID
	}

	t.Run("undo succeeds when no downstream match has started", func(t *testing.T) {
		eng, store, compID, aliceID, _ := setup(t)
		// Flip the kiken: Bob (shiro) withdrew instead. Same match,
		// different decisionBy.
		result, status, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "shiro", "scoring fix", nil, false)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, "Alice", result.Winner)
		// Status surfaced should be Alice → eligible again (the prior
		// kiken put her ineligible; the overwrite swaps it to Bob).
		require.NotNil(t, status, "expected restored-eligibility status")
		assert.Equal(t, aliceID, status.PlayerID)
		assert.True(t, status.Eligible)
		// Verify the persisted store matches.
		statuses, err := store.LoadCompetitorStatus(compID)
		require.NoError(t, err)
		assert.True(t, statuses[aliceID].Eligible)
	})

	t.Run("undo locked when a subsequent match has started for either side", func(t *testing.T) {
		eng, store, compID, _, _ := setup(t)
		// Mark Pool A-1 (Alice vs Carol) as running — that's a
		// subsequent match involving Alice.
		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for i := range matches {
			if matches[i].ID == "Pool A-1" {
				matches[i].Status = state.MatchStatusRunning
			}
		}
		require.NoError(t, store.SavePoolMatches(compID, matches))

		_, _, err = eng.RecordDecision(compID, "Pool A-0", "kiken", "shiro", "scoring fix", nil, false)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDecisionLocked), "want ErrDecisionLocked, got %v", err)
	})

	t.Run("force=true bypasses the decision lock", func(t *testing.T) {
		eng, store, compID, aliceID, _ := setup(t)
		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for i := range matches {
			if matches[i].ID == "Pool A-2" {
				matches[i].Status = state.MatchStatusCompleted
			}
		}
		require.NoError(t, store.SavePoolMatches(compID, matches))

		result, status, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "shiro", "scoring fix", nil, true)
		require.NoError(t, err)
		assert.Equal(t, "Alice", result.Winner)
		require.NotNil(t, status)
		assert.Equal(t, aliceID, status.PlayerID)
		assert.True(t, status.Eligible)
	})

	t.Run("idempotent overwrite (same decisionBy) does not restore eligibility", func(t *testing.T) {
		eng, store, compID, aliceID, _ := setup(t)
		// Re-record kiken with the SAME decisionBy. Prior loser ==
		// new loser, so eligibility should stay false.
		_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "knee injury (repeated)", nil, false)
		require.NoError(t, err)
		statuses, err := store.LoadCompetitorStatus(compID)
		require.NoError(t, err)
		st, ok := statuses[aliceID]
		require.True(t, ok)
		assert.False(t, st.Eligible, "same-side kiken should keep Alice ineligible")
	})

	t.Run("restored CompetitorStatus has no Reason and a fresh RecordedAt", func(t *testing.T) {
		eng, _, compID, _, _ := setup(t)
		_, status, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "shiro", "scoring fix", nil, false)
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.True(t, status.Eligible)
		assert.Empty(t, status.Reason, "eligible status should not carry a reason")
		assert.WithinDuration(t, time.Now().UTC(), status.RecordedAt, 5*time.Second)
	})
}

// TestRecordDecision_ConcurrentKiken verifies CHK047/T105: when two
// operators on different courts simultaneously try to kiken the same
// player, the second call returns *AlreadyIneligibleError (HTTP 409
// "already_ineligible") rather than overwriting the status silently.
func TestRecordDecision_ConcurrentKiken(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "concurrent-kiken"
	createTestCompetition(t, store, compID, "pools", 2)

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	carolID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []helper.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Carol", SideB: "Alice", Status: state.MatchStatusScheduled},
	}))

	// First operator records kiken on Alice in Pool A-0 (decisionBy=aka → Alice is loser).
	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "injury", nil, false)
	require.NoError(t, err)

	// Second operator attempts kiken on Alice (decisionBy=shiro → Alice is loser) in Pool A-1.
	// Alice is already ineligible from Pool A-0 — different match.
	_, _, err = eng.RecordDecision(compID, "Pool A-1", "kiken", "shiro", "concurrent", nil, false)
	require.Error(t, err)

	var alreadyErr *AlreadyIneligibleError
	require.ErrorAs(t, err, &alreadyErr, "expected AlreadyIneligibleError, got %T: %v", err, err)
	assert.Equal(t, aliceID, alreadyErr.PlayerID)
	assert.Equal(t, "Pool A-0", alreadyErr.MatchID)

	// Same match should NOT be blocked — that's the undo path (T103).
	_, _, err = eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "re-record same match", nil, false)
	assert.NoError(t, err, "re-recording the same match must not trigger AlreadyIneligibleError")
}
