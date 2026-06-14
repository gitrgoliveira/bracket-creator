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

	createTestCompetition(t, store, compID, "league", 2)

	// Seed participants with explicit UUIDs — state.LoadParticipants
	// only treats the first column as an ID when it parses as UUID v4.
	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	players := []domain.Player{
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
	assert.Truef(t, errors.Is(err, ErrIneligibleCompetitor), "want errors.Is == ErrIneligibleCompetitor, got %v", err)

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
		createTestCompetition(t, store, compID, "league", 3)

		aliceID := helper.NewUUID4()
		bobID := helper.NewUUID4()
		carolID := helper.NewUUID4()
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
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
		assert.Truef(t, errors.Is(err, ErrDecisionLocked), "want ErrDecisionLocked, got %v", err)
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

	// Verify that the failed match (Pool A-1) was rolled back (K3 partial-write fix).
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		if m.ID == "Pool A-1" {
			assert.Equal(t, state.MatchStatusScheduled, m.Status, "match should have rolled back to Scheduled after partial-write failure")
			assert.Empty(t, m.Decision, "match should have rolled back Decision after partial-write failure")
		}
	}
}

// TestRecordDecision_ConcurrentKikenRace verifies K2/CHK047: when two
// goroutines race to kiken the same player on different matches, the
// atomic check-and-set inside recordIneligibilityFromDecision serializes
// them on the per-comp lock — exactly one succeeds and the loser sees
// *AlreadyIneligibleError. This exercises the post-pre-check race
// window that the synchronous pre-check alone can't close.
func TestRecordDecision_ConcurrentKikenRace(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "race-kiken"
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

	// Fire both kiken calls in parallel. The atomic check inside
	// recordIneligibilityFromDecision must reject one of them.
	type result struct {
		err     error
		matchID string
	}
	results := make(chan result, 2)
	go func() {
		_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "race A", nil, false)
		results <- result{err: err, matchID: "Pool A-0"}
	}()
	go func() {
		_, _, err := eng.RecordDecision(compID, "Pool A-1", "kiken", "shiro", "race B", nil, false)
		results <- result{err: err, matchID: "Pool A-1"}
	}()

	var winners, losers []result
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
	require.ErrorAs(t, losers[0].err, &alreadyErr, "loser must be *AlreadyIneligibleError, got %T: %v", losers[0].err, losers[0].err)
	assert.Equal(t, aliceID, alreadyErr.PlayerID)
	assert.Equal(t, winners[0].matchID, alreadyErr.MatchID, "rejected operator should see the winning match ID")

	// Final store state must reflect the winning operator's record only.
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	st, ok := statuses[aliceID]
	require.True(t, ok)
	assert.False(t, st.Eligible)
	assert.Equal(t, winners[0].matchID, st.MatchID)
}

// TestRecordDecision_FusenshoSkipsConcurrentCheck verifies the fix for
// the over-broad guard: fusensho doesn't create ineligibility, so the
// concurrent-kiken check should not fire and surface a misleading
// "already_ineligible" 409. The StartMatch eligibility gate is the
// right rejector for "player ineligible from elsewhere" cases.
func TestRecordDecision_FusenshoSkipsConcurrentCheck(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "fusensho-bypass"
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

	// Mark Alice ineligible via a prior kiken in Pool A-0.
	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "injury", nil, false)
	require.NoError(t, err)

	// Now record fusensho on Alice in Pool A-1. This should NOT trip the
	// concurrent-kiken guard — fusensho doesn't write ineligibility.
	_, _, err = eng.RecordDecision(compID, "Pool A-1", "fusensho", "shiro", "default win", nil, false)
	assert.NoErrorf(t, err, "fusensho on an already-ineligible player must not trigger AlreadyIneligibleError; got %v", err)
}

// TestCheckEligibility_AllEligible verifies that CheckEligibility returns
// nil when no competitor-status records exist (default-eligible per FR-034).
func TestCheckEligibility_AllEligible(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "elig-all"
	createTestCompetition(t, store, compID, "league", 2)

	err := eng.CheckEligibility(compID, []string{"pid1", "pid2", ""})
	assert.NoError(t, err)
}

// TestCheckEligibility_OneIneligible verifies that CheckEligibility
// returns *IneligibleCompetitorError when one of the player IDs has
// Eligible: false. The empty-string player ID must be skipped.
func TestCheckEligibility_OneIneligible(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "elig-one"
	createTestCompetition(t, store, compID, "league", 2)

	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: "ineligible-pid",
		Eligible: false,
		Reason:   "kiken at match-1",
		MatchID:  "match-1",
	}))

	err := eng.CheckEligibility(compID, []string{"eligible-pid", "ineligible-pid"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIneligibleCompetitor))
	var ie *IneligibleCompetitorError
	require.ErrorAs(t, err, &ie)
	assert.Equal(t, "ineligible-pid", ie.PlayerID)
	assert.Equal(t, "kiken at match-1", ie.Reason)
}

// TestCheckEligibility_EmptyIDsSkipped verifies that empty-string IDs
// are silently skipped (no lookup, no error).
func TestCheckEligibility_EmptyIDsSkipped(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "elig-empty"
	createTestCompetition(t, store, compID, "league", 2)

	err := eng.CheckEligibility(compID, []string{"", ""})
	assert.NoError(t, err)
}

// TestRecordDecision_OnBracketMatch exercises the bracket-match paths of
// lookupMatchSides and lookupExistingResult. A kiken decision on a
// bracket match must succeed, write the competitor status, and set the
// bracket winner.
func TestRecordDecision_OnBracketMatch(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bracket-kiken"
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

	// Record kiken on the bracket match (Bob withdraws, decisionBy=shiro → Alice wins).
	result, status, err := eng.RecordDecision(compID, matchID, "kiken", "shiro", "injury", nil, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "Alice", result.Winner)
	require.NotNil(t, status)
	assert.Equal(t, bobID, status.PlayerID)
	assert.False(t, status.Eligible)
}

// TestStartMatch_BracketMatch_Eligible verifies that StartMatch on a
// bracket match returns nil when the participants are eligible (exercises
// lookupMatchSides via the bracket path).
func TestStartMatch_BracketMatch_Eligible(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bracket-start-match"
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
	matchID := bracket.Rounds[0][0].ID

	err = eng.StartMatch(compID, matchID)
	assert.NoError(t, err)
}

// TestIneligibleCompetitorError_Error verifies the error string format.
func TestIneligibleCompetitorError_Error(t *testing.T) {
	err := &IneligibleCompetitorError{PlayerID: "pid-123", Reason: "kiken at m1"}
	assert.Contains(t, err.Error(), "pid-123")
	assert.Contains(t, err.Error(), "kiken at m1")
}

// TestAlreadyIneligibleError_Error verifies the error string format.
func TestAlreadyIneligibleError_Error(t *testing.T) {
	err := &AlreadyIneligibleError{PlayerID: "pid-456", MatchID: "match-2", Reason: "prior kiken"}
	assert.Contains(t, err.Error(), "pid-456")
	assert.Contains(t, err.Error(), "match-2")
}

// TestLoserSideName exercises the loserSideName helper for various
// result shapes: winner set, winner not set but ippons asymmetric.
func TestLoserSideName(t *testing.T) {
	tests := []struct {
		name   string
		result state.MatchResult
		want   string
	}{
		{
			"winner is SideA → loser is SideB",
			state.MatchResult{SideA: "Alice", SideB: "Bob", Winner: "Alice"},
			"Bob",
		},
		{
			"winner is SideB → loser is SideA",
			state.MatchResult{SideA: "Alice", SideB: "Bob", Winner: "Bob"},
			"Alice",
		},
		{
			"no winner but SideA has ippons → SideB is loser",
			state.MatchResult{SideA: "Alice", SideB: "Bob", IpponsA: []string{"M"}, IpponsB: nil},
			"Bob",
		},
		{
			"no winner but SideB has ippons → SideA is loser",
			state.MatchResult{SideA: "Alice", SideB: "Bob", IpponsA: nil, IpponsB: []string{"M"}},
			"Alice",
		},
		{
			"both empty ippons and no winner → empty",
			state.MatchResult{SideA: "Alice", SideB: "Bob"},
			"",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, loserSideName(&tc.result))
		})
	}
}

// TestLookupPlayerID_EmptyName covers the early-return when name is "".
func TestLookupPlayerID_EmptyName(t *testing.T) {
	players := []domain.Player{{ID: "p1", Name: "Alice", Dojo: "A"}}
	assert.Equal(t, "", lookupPlayerID(players, ""), "empty name should return empty ID")
}

// TestCheckConcurrentIneligibility_EmptyLoser covers the loserName==""
// fast path in checkConcurrentIneligibility.
func TestCheckConcurrentIneligibility_EmptyLoser(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "conc-empty-loser"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	err := eng.checkConcurrentIneligibility(compID, "M1", "")
	assert.NoError(t, err, "empty loserName should return nil")
}

// TestCheckConcurrentIneligibility_PlayerNotInParticipants covers the
// playerID=="" path when the loser name is not registered as a participant.
func TestCheckConcurrentIneligibility_PlayerNotInParticipants(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "conc-unknown"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	// No participants saved → lookupPlayerID returns ""
	err := eng.checkConcurrentIneligibility(compID, "M1", "Ghost Player")
	assert.NoError(t, err, "unknown player should return nil without error")
}

// TestRestoreCompetitorEligibility_EmptyPriorLoser covers the priorLoser==""
// fast path in restoreCompetitorEligibility.
func TestRestoreCompetitorEligibility_EmptyPriorLoser(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "restore-empty"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	status, err := eng.restoreCompetitorEligibility(compID, "", "M1")
	assert.NoError(t, err)
	assert.Nil(t, status)
}

// TestRestoreCompetitorEligibility_PlayerNotInParticipants covers the
// playerID=="" path when the prior loser is not a registered participant.
func TestRestoreCompetitorEligibility_PlayerNotInParticipants(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "restore-unknown"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	// No participants → lookupPlayerID returns ""
	status, err := eng.restoreCompetitorEligibility(compID, "Ghost Player", "M1")
	assert.NoError(t, err)
	assert.Nil(t, status)
}

// TestResolveMatchParticipantIDs_UnknownMatch covers the lookupMatchSides
// error path in resolveMatchParticipantIDs.
func TestResolveMatchParticipantIDs_UnknownMatch(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "resolve-unknown"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
	_, err := eng.resolveMatchParticipantIDs(compID, "nonexistent-match")
	assert.Error(t, err)
}

// TestHasDownstreamMatchStarted_BracketMatch verifies that a started bracket
// match involving one of the named players is detected.
func TestHasDownstreamMatchStarted_BracketMatch(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "downstream-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "B1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
			},
		},
	}))

	started, err := eng.hasDownstreamMatchStarted(compID, []string{"Alice"}, "other-match")
	require.NoError(t, err)
	assert.True(t, started, "started bracket match involving Alice must be detected")
}

// TestHasDownstreamMatchStarted_EmptyPlayerNames covers the early-return
// when all player names are empty.
func TestHasDownstreamMatchStarted_EmptyPlayerNames(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "downstream-empty"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	started, err := eng.hasDownstreamMatchStarted(compID, []string{"", ""}, "M1")
	require.NoError(t, err)
	assert.False(t, started)
}

// TestCheckConcurrentIneligibility_AlreadyIneligible covers the
// AlreadyIneligibleError path: loserName is registered as a participant
// and already has an ineligibility status from a DIFFERENT match.
func TestCheckConcurrentIneligibility_AlreadyIneligible(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "conc-already"

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	playerID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: playerID, Name: "Alice", Dojo: "DojoA"},
	}))
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: playerID,
		Eligible: false,
		Reason:   "kiken",
		MatchID:  "M-prev",
	}))

	err := eng.checkConcurrentIneligibility(compID, "M-new", "Alice")
	require.Error(t, err)
	var alreadyErr *AlreadyIneligibleError
	require.ErrorAs(t, err, &alreadyErr)
	assert.Equal(t, playerID, alreadyErr.PlayerID)
	assert.Equal(t, "M-prev", alreadyErr.MatchID)
}

// TestCheckConcurrentIneligibility_SameMatchNotBlocked verifies that an
// ineligibility status recorded from the SAME match (the undo path) does
// not return an error (matchID == st.MatchID → skip).
func TestCheckConcurrentIneligibility_SameMatchNotBlocked(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "conc-same-match"

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	playerID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: playerID, Name: "Bob", Dojo: "DojoB"},
	}))
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: playerID,
		Eligible: false,
		Reason:   "kiken",
		MatchID:  "M-current", // same as what we're re-scoring
	}))

	err := eng.checkConcurrentIneligibility(compID, "M-current", "Bob")
	assert.NoError(t, err, "same-match ineligibility should not block re-scoring")
}

// TestCheckEligibilityExcludingMatch_ExcludedMatch covers the
// st.MatchID == excludeMatchID path: player is ineligible from the exact
// match being re-scored → not blocked (T103 undo path).
func TestCheckEligibilityExcludingMatch_ExcludedMatch(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "excl-match"

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	playerID := helper.NewUUID4()
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: playerID,
		Eligible: false,
		Reason:   "kiken",
		MatchID:  "M-same",
	}))

	err := eng.checkEligibilityExcludingMatch(compID, []string{playerID}, "M-same")
	assert.NoError(t, err, "ineligibility from excluded match must not block re-scoring")
}

// TestCheckEligibilityExcludingMatch_BlockedByDifferentMatch verifies
// that ineligibility from a DIFFERENT match still blocks.
func TestCheckEligibilityExcludingMatch_BlockedByDifferentMatch(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "excl-different"

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	playerID := helper.NewUUID4()
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: playerID,
		Eligible: false,
		Reason:   "kiken",
		MatchID:  "M-other",
	}))

	err := eng.checkEligibilityExcludingMatch(compID, []string{playerID}, "M-different")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIneligibleCompetitor))
}

// TestStartMatch_MatchNotFound verifies that StartMatch returns an error
// when the match is not found in pool matches or bracket.
func TestStartMatch_MatchNotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "start-not-found"

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

	err := eng.StartMatch(compID, "nonexistent-match-id")
	assert.Error(t, err)
}

// TestCheckEligibilityExcludingMatch_EmptyPlayerID covers the pid==""
// continue branch: an empty player ID must be silently skipped.
func TestCheckEligibilityExcludingMatch_EmptyPlayerID(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "excl-empty-pid"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	// Pass an empty string as one of the player IDs.
	err := eng.checkEligibilityExcludingMatch(compID, []string{"", ""}, "M1")
	assert.NoError(t, err, "empty player IDs must be skipped silently")
}

// TestRecordDecision_KikenReinstateable verifies that kiken-injury sets
// Reinstateable=true and kiken-voluntary does not.
func TestRecordDecision_KikenReinstateable(t *testing.T) {
	tests := []struct {
		name              string
		decision          string
		wantReinstateable bool
	}{
		{"kiken-injury sets reinstateable=true", "kiken-injury", true},
		{"kiken-voluntary leaves reinstateable=false", "kiken-voluntary", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "reinstateable-test-" + tc.decision
			createTestCompetition(t, store, compID, "league", 2)

			aliceID := helper.NewUUID4()
			require.NoError(t, store.SaveParticipants(compID, []domain.Player{
				{ID: aliceID, Name: "Alice", Dojo: "A"},
				{ID: helper.NewUUID4(), Name: "Bob", Dojo: "B"},
			}))
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
				{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
			}))

			_, status, err := eng.RecordDecision(compID, "Pool A-0", tc.decision, "aka", "reason", nil, false)
			require.NoError(t, err)
			require.NotNil(t, status)
			assert.False(t, status.Eligible)
			assert.Equal(t, tc.wantReinstateable, status.Reinstateable)

			statuses, err := store.LoadCompetitorStatus(compID)
			require.NoError(t, err)
			assert.Equal(t, tc.wantReinstateable, statuses[aliceID].Reinstateable)
		})
	}
}

// TestReinstateCompetitor verifies the ReinstateCompetitor engine method.
func TestReinstateCompetitor(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "reinstate-test"
	createTestCompetition(t, store, compID, "league", 2)

	playerID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: playerID, Name: "Alice", Dojo: "A"},
	}))

	t.Run("reinstate kiken-injury succeeds", func(t *testing.T) {
		require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID:      playerID,
			Eligible:      false,
			Reinstateable: true,
			Reason:        "kiken-injury at Pool A-0",
			MatchID:       "Pool A-0",
		}))

		status, err := eng.ReinstateCompetitor(compID, playerID)
		require.NoError(t, err)
		require.NotNil(t, status)
		assert.True(t, status.Eligible)
		assert.Equal(t, playerID, status.PlayerID)
		assert.Contains(t, status.Reason, "reinstated")
		assert.Contains(t, status.Reason, "kiken-injury at Pool A-0")
	})

	t.Run("reinstate kiken-voluntary rejected", func(t *testing.T) {
		require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID:      playerID,
			Eligible:      false,
			Reinstateable: false,
			Reason:        "kiken-voluntary at Pool A-0",
			MatchID:       "Pool A-0",
		}))

		_, err := eng.ReinstateCompetitor(compID, playerID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not reinstateable")
	})

	t.Run("reinstate already eligible rejected", func(t *testing.T) {
		require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID: playerID,
			Eligible: true,
			MatchID:  "Pool A-0",
		}))

		_, err := eng.ReinstateCompetitor(compID, playerID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not ineligible")
	})

	t.Run("reinstate empty playerID rejected", func(t *testing.T) {
		_, err := eng.ReinstateCompetitor(compID, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "playerID is required")
	})
}

// TestRollback_BracketSubResults_Cleared verifies the K3/CHK047 rollback
// path clears SubResults from a bracket match when the prior state had no
// sub-results. Regression: the nil-preserve branch would leave
// partially-written SubResults behind because the prior was nil and nil
// means "preserve". The fix normalizes nil → empty slice before replay.
func TestRollback_BracketSubResults_Cleared(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bracket-rollback-subs"

	createTestCompetition(t, store, compID, "playoffs", 3)

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	carolID := helper.NewUUID4()
	daveID := helper.NewUUID4()
	players := []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
		{ID: daveID, Name: "Dave", Dojo: "D"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	idByName := make(map[string]string, len(players))
	for _, p := range players {
		idByName[p.Name] = p.ID
	}

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	firstMatchID := bracket.Rounds[0][0].ID
	secondMatch := bracket.Rounds[0][1]
	secondMatchID := secondMatch.ID

	// Deterministically target whoever the generator placed as SideA of the
	// SECOND bracket match, and pre-mark that competitor ineligible from a
	// DIFFERENT match (the first match). This is what makes the test always
	// exercise the BRACKET rollback path (recordBracketMatchResultTx) — the
	// path the nil-preserve bug lives in — regardless of how the generator
	// seeds sides. (The earlier version fell back to a pool-match rollback
	// when Alice happened not to land in the second match, which silently
	// stopped proving the bracket behaviour.)
	targetName := secondMatch.SideA
	targetID := idByName[targetName]
	require.NotEmpty(t, targetID, "second bracket match SideA must map to a known participant")

	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: targetID,
		Eligible: false,
		Reason:   "kiken in an earlier match",
		MatchID:  firstMatchID,
	}))

	// Score the second bracket match with a kiken on the target (SideA →
	// decisionBy "aka" makes SideA the loser) plus SubResults and a hantei
	// flag. The engine writes the partial bracket result, then
	// recordIneligibilityFromDecisionTx detects the target is already
	// ineligible from firstMatchID and returns *AlreadyIneligibleError,
	// triggering the rollback.
	_, err = eng.RecordMatchResultWithIneligibility(compID, secondMatchID, &state.MatchResult{
		Winner:          secondMatch.SideB,
		Status:          state.MatchStatusCompleted,
		Decision:        "kiken",
		DecisionBy:      "aka",
		DecidedByHantei: state.HanteiPtr(true),
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: secondMatch.SideA, Winner: secondMatch.SideA},
			{Position: 2, SideA: secondMatch.SideA, IpponsB: []string{"M"}},
		},
	})
	require.Error(t, err, "must get AlreadyIneligibleError")
	var alreadyErr *AlreadyIneligibleError
	require.ErrorAs(t, err, &alreadyErr)

	// The bracket match must have been rolled back. Critically, both
	// nil-preserve fields written as part of the failed score attempt
	// must NOT persist — the prior had nil SubResults and (via HanteiPtr)
	// nil DecidedByHantei, and the rollback must normalize those to an
	// explicit empty slice / false to clear them.
	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	found := false
	for _, round := range bracket.Rounds {
		for _, bm := range round {
			if bm.ID == secondMatchID {
				found = true
				assert.Empty(t, bm.SubResults, "SubResults must be cleared by rollback when the prior had none")
				assert.False(t, bm.DecidedByHantei, "DecidedByHantei must be cleared by rollback when the prior was false")
			}
		}
	}
	require.True(t, found, "second match must exist in bracket after rollback")
}

// TestStartMatch_RejectsSimultaneousMatch verifies the simultaneity gate
// (Phase 2c): when a participant is already Running in another match within
// the same competition, StartMatch must return *IneligibleCompetitorError
// matching errors.Is(err, ErrIneligibleCompetitor) with a reason that
// mentions "already fighting".
func TestStartMatch_RejectsSimultaneousMatch(t *testing.T) {
	t.Run("pool match running blocks second pool match for same participant", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "simul-pool"
		createTestCompetition(t, store, compID, "league", 3)

		aliceID := helper.NewUUID4()
		bobID := helper.NewUUID4()
		charlieID := helper.NewUUID4()
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{ID: aliceID, Name: "Alice", Dojo: "A"},
			{ID: bobID, Name: "Bob", Dojo: "B"},
			{ID: charlieID, Name: "Charlie", Dojo: "C"},
		}))

		// A-0: Alice vs Bob (Running), A-2: Alice vs Charlie (Scheduled)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
			{ID: "A-1", SideA: "Bob", SideB: "Charlie", Status: state.MatchStatusScheduled},
			{ID: "A-2", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusScheduled},
		}))

		err := eng.StartMatch(compID, "A-2")
		require.Error(t, err)
		assert.Truef(t, errors.Is(err, ErrIneligibleCompetitor), "want ErrIneligibleCompetitor, got %v", err)

		var ineligErr *IneligibleCompetitorError
		require.ErrorAs(t, err, &ineligErr)
		assert.Contains(t, ineligErr.Reason, "already fighting")
	})

	t.Run("SideB participant running blocks second match", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "simul-sideb"
		createTestCompetition(t, store, compID, "league", 3)

		aliceID := helper.NewUUID4()
		bobID := helper.NewUUID4()
		charlieID := helper.NewUUID4()
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{ID: aliceID, Name: "Alice", Dojo: "A"},
			{ID: bobID, Name: "Bob", Dojo: "B"},
			{ID: charlieID, Name: "Charlie", Dojo: "C"},
		}))

		// A-0: Alice vs Bob (Running). A-1: Charlie vs Bob (Scheduled).
		// Bob (SideB of A-0) is running — starting A-1 should be blocked.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "B"},
			{ID: "A-1", SideA: "Charlie", SideB: "Bob", Status: state.MatchStatusScheduled},
		}))

		err := eng.StartMatch(compID, "A-1")
		require.Error(t, err)
		assert.Truef(t, errors.Is(err, ErrIneligibleCompetitor), "got %v", err)

		var ineligErr *IneligibleCompetitorError
		require.ErrorAs(t, err, &ineligErr)
		assert.Contains(t, ineligErr.Reason, "already fighting")
	})

	t.Run("completed match does not block new match for same participant", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "simul-completed"
		createTestCompetition(t, store, compID, "league", 3)

		aliceID := helper.NewUUID4()
		bobID := helper.NewUUID4()
		charlieID := helper.NewUUID4()
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{ID: aliceID, Name: "Alice", Dojo: "A"},
			{ID: bobID, Name: "Bob", Dojo: "B"},
			{ID: charlieID, Name: "Charlie", Dojo: "C"},
		}))

		// A-0: Alice vs Bob (Completed). A-2: Alice vs Charlie (Scheduled).
		// Completed match must NOT block Alice from starting A-2.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
			{ID: "A-2", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusScheduled},
		}))

		err := eng.StartMatch(compID, "A-2")
		assert.NoError(t, err, "completed match must not block the simultaneity check")
	})

	t.Run("scheduled match does not block new match for same participant", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "simul-scheduled"
		createTestCompetition(t, store, compID, "league", 3)

		aliceID := helper.NewUUID4()
		bobID := helper.NewUUID4()
		charlieID := helper.NewUUID4()
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{ID: aliceID, Name: "Alice", Dojo: "A"},
			{ID: bobID, Name: "Bob", Dojo: "B"},
			{ID: charlieID, Name: "Charlie", Dojo: "C"},
		}))

		// A-0: Alice vs Bob (Scheduled). A-2: Alice vs Charlie (Scheduled).
		// Another scheduled match must NOT block.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
			{ID: "A-2", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusScheduled},
		}))

		err := eng.StartMatch(compID, "A-2")
		assert.NoError(t, err, "scheduled match must not block the simultaneity check")
	})

	t.Run("bracket match running blocks pool match for same participant", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "simul-bracket"
		createTestCompetition(t, store, compID, "league", 3)

		aliceID := helper.NewUUID4()
		bobID := helper.NewUUID4()
		charlieID := helper.NewUUID4()
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{ID: aliceID, Name: "Alice", Dojo: "A"},
			{ID: bobID, Name: "Bob", Dojo: "B"},
			{ID: charlieID, Name: "Charlie", Dojo: "C"},
		}))

		// Alice is Running in a bracket match; trying to start a pool match
		// involving Alice should be blocked.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusScheduled},
		}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{ID: "B-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "C"},
				},
			},
		}))

		err := eng.StartMatch(compID, "P-0")
		require.Error(t, err)
		assert.Truef(t, errors.Is(err, ErrIneligibleCompetitor), "got %v", err)

		var ineligErr *IneligibleCompetitorError
		require.ErrorAs(t, err, &ineligErr)
		assert.Contains(t, ineligErr.Reason, "already fighting")
	})
}

// TestStartMatch_CourtExclusivity verifies mp-95mg: StartMatch and
// StartMatchTx must reject starting a match on a court that already
// has a running match, across ALL competitions sharing the tournament.
func TestStartMatch_CourtExclusivity(t *testing.T) {
	t.Run("court busy in same competition blocks new match", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "court-same-comp"
		createTestCompetition(t, store, compID, "league", 3)

		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "m1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
			{ID: "m2", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "A"},
		}))

		err := eng.StartMatch(compID, "m2")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrCourtBusy), "want ErrCourtBusy, got %v", err)

		var courtErr *CourtBusyError
		require.ErrorAs(t, err, &courtErr)
		assert.Equal(t, "A", courtErr.Court)
		assert.Equal(t, "m1", courtErr.MatchID)
	})

	t.Run("court busy in different competition blocks new match", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)

		// comp1 has a running match on court A.
		createTestCompetition(t, store, "comp1", "league", 3)
		require.NoError(t, store.SavePoolMatches("comp1", []state.MatchResult{
			{ID: "m1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
		}))

		// comp2 wants to start a match on court A.
		createTestCompetition(t, store, "comp2", "league", 3)
		require.NoError(t, store.SavePoolMatches("comp2", []state.MatchResult{
			{ID: "m2", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "A"},
		}))

		err := eng.StartMatch("comp2", "m2")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrCourtBusy), "want ErrCourtBusy, got %v", err)

		var courtErr *CourtBusyError
		require.ErrorAs(t, err, &courtErr)
		assert.Equal(t, "A", courtErr.Court)
		assert.Equal(t, "m1", courtErr.MatchID)
		assert.Equal(t, "comp1", courtErr.CompID)
	})

	t.Run("free court allows match to start", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "court-free"
		createTestCompetition(t, store, compID, "league", 3)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob"})

		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "m1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "B"},
			{ID: "m2", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A"},
		}))

		// m2 is on court A which is free — but Alice is also in m1.
		// Court check passes, but player simultaneity blocks it.
		err := eng.StartMatch(compID, "m2")
		// Error should be IneligibleCompetitor (player already fighting), not CourtBusy.
		assert.False(t, errors.Is(err, ErrCourtBusy), "court A is free; should not get CourtBusy")
	})

	t.Run("match with no court assigned is never blocked by court exclusivity", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "no-court"
		createTestCompetition(t, store, compID, "league", 3)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob"})

		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "m1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
			{ID: "m2", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled, Court: ""},
		}))

		err := eng.StartMatch(compID, "m2")
		// No court assigned → court check skipped; other checks may pass or fail
		// but ErrCourtBusy must not appear.
		assert.False(t, errors.Is(err, ErrCourtBusy), "unassigned-court match must not trigger ErrCourtBusy")
	})
}

func TestStartMatchTx_CourtExclusivity(t *testing.T) {
	t.Run("court busy in same competition blocks via tx path", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "tx-court-same"
		createTestCompetition(t, store, compID, "league", 3)
		// IDs must be hyphenated (PoolName-MatchIdx) so they survive the CSV
		// round-trip that LoadPoolMatchesLocked uses inside a transaction.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
			{ID: "P-1", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "A"},
		}))

		var engErr error
		txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
			engErr = eng.StartMatchTx(tx, compID, "P-1")
			return nil
		})
		require.NoError(t, txErr)
		require.Error(t, engErr)
		assert.True(t, errors.Is(engErr, ErrCourtBusy), "want ErrCourtBusy, got %v", engErr)

		var courtErr *CourtBusyError
		require.ErrorAs(t, engErr, &courtErr)
		assert.Equal(t, "A", courtErr.Court)
		assert.Equal(t, "P-0", courtErr.MatchID)
	})

	t.Run("court busy in different competition blocks via tx path", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)

		createTestCompetition(t, store, "comp1", "league", 3)
		require.NoError(t, store.SavePoolMatches("comp1", []state.MatchResult{
			{ID: "P-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
		}))

		createTestCompetition(t, store, "comp2", "league", 3)
		require.NoError(t, store.SavePoolMatches("comp2", []state.MatchResult{
			{ID: "P-0", SideA: "Charlie", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "A"},
		}))

		var engErr error
		txErr := store.WithTransaction("comp2", func(tx state.StoreTx) error {
			engErr = eng.StartMatchTx(tx, "comp2", "P-0")
			return nil
		})
		require.NoError(t, txErr)
		require.Error(t, engErr)
		assert.True(t, errors.Is(engErr, ErrCourtBusy), "want ErrCourtBusy, got %v", engErr)

		var courtErr *CourtBusyError
		require.ErrorAs(t, engErr, &courtErr)
		assert.Equal(t, "A", courtErr.Court)
		assert.Equal(t, "comp1", courtErr.CompID)
	})
}
