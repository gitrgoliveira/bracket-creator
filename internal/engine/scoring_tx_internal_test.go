package engine

// Tests for unexported scoring_tx.go helpers that are 0% or very low coverage:
// - recordBracketMatchResultTx (0%)
// - recordMatchResultTx (0%)
// - lookupExistingResultTx (bracket path + not-found)
// - lookupMatchSidesTx (bracket path + not-found)
// - maybeLockTeamLineupsForRoundTx (team comp branch)
// - checkConcurrentIneligibilityTx (already-ineligible path)
// - withPoolMatchTx (not-found branch)
// - restoreCompetitorEligibilityTx (empty priorLoser + happy path)

import (
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecordBracketMatchResultTx_HappyPath confirms that
// recordBracketMatchResultTx updates the target match in the bracket
// and propagates the winner to the next round when the match is
// completed.
func TestRecordBracketMatchResultTx_HappyPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rbmrt-ok"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "M1", SideA: "Alice", SideB: "Bob"}},
			{{ID: "M2"}},
		},
	}))

	result := &state.MatchResult{
		ID:      "M1",
		Winner:  "Alice",
		Status:  state.MatchStatusCompleted,
		IpponsA: []string{"M"},
	}
	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.recordBracketMatchResultTx(tx, compID, "M1", result)
		return nil
	})
	require.NoError(t, txErr)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", b.Rounds[0][0].Winner)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[0][0].Status)
	// Propagation: round 1 SideA should be Alice.
	assert.Equal(t, "Alice", b.Rounds[1][0].SideA)
}

// TestRecordBracketMatchResultTx_MatchNotFound exercises the
// "bracket match not found" error branch.
func TestRecordBracketMatchResultTx_MatchNotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rbmrt-notfound"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "M1", SideA: "X", SideB: "Y"}},
		},
	}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.recordBracketMatchResultTx(tx, compID, "GHOST", &state.MatchResult{Winner: "X"})
		return nil
	})
	require.Error(t, txErr)
	assert.Contains(t, txErr.Error(), "not found")
}

// TestRecordBracketMatchResultTx_NilBracket exercises the "bracket is
// nil" error branch (no bracket saved for the competition).
func TestRecordBracketMatchResultTx_NilBracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rbmrt-nilbracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	// No bracket saved → UpdateBracket closure receives nil.

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.recordBracketMatchResultTx(tx, compID, "M1", &state.MatchResult{Winner: "X"})
		return nil
	})
	require.Error(t, txErr)
	assert.Contains(t, txErr.Error(), "not found")
}

// TestRecordMatchResultTx_PoolPath confirms recordMatchResultTx can
// update a pool match (the common path).
func TestRecordMatchResultTx_PoolPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rmrt-pool"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
	}))

	result := &state.MatchResult{
		Winner: "Alice",
		Status: state.MatchStatusCompleted,
	}
	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.recordMatchResultTx(tx, compID, "Pool A-0", result)
		return nil
	})
	require.NoError(t, txErr)

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "Alice", matches[0].Winner)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status)
}

// TestRecordMatchResultTx_BracketFallback confirms recordMatchResultTx
// falls through to the bracket path when the match is not in the pool.
func TestRecordMatchResultTx_BracketFallback(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rmrt-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	// No pool matches, force the bracket fallback.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "B1", SideA: "Alice", SideB: "Bob"}},
			{{ID: "B2"}},
		},
	}))

	result := &state.MatchResult{Winner: "Bob", Status: state.MatchStatusCompleted}
	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.recordMatchResultTx(tx, compID, "B1", result)
		return nil
	})
	require.NoError(t, txErr)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", b.Rounds[0][0].Winner)
}

// TestRecordMatchResultTx_NotFoundAnywhere confirms recordMatchResultTx
// returns an error when the match ID does not exist in pool or bracket.
func TestRecordMatchResultTx_NotFoundAnywhere(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rmrt-nfound"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{{ID: "B1", SideA: "X", SideB: "Y"}}},
	}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.recordMatchResultTx(tx, compID, "GHOST", &state.MatchResult{Winner: "X"})
		return nil
	})
	require.Error(t, txErr)
}

// TestLookupExistingResultTx_BracketPath confirms the bracket fallback
// when the match ID exists only in the bracket store.
func TestLookupExistingResultTx_BracketPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "lertx-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "B1", SideA: "Alice", SideB: "Bob", Winner: "Alice",
				Status: state.MatchStatusCompleted, Decision: "fought"}},
		},
	}))

	var got *state.MatchResult
	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		got, txErr = eng.lookupExistingResultTx(tx, compID, "B1")
		return nil
	})
	require.NoError(t, txErr)
	require.NotNil(t, got)
	assert.Equal(t, "Alice", got.Winner)
	assert.Equal(t, "fought", got.Decision)
}

// TestLookupExistingResultTx_NotFound confirms the not-found error when
// the match ID does not exist in either store.
func TestLookupExistingResultTx_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "lertx-nf"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, txErr = eng.lookupExistingResultTx(tx, compID, "GHOST")
		return nil
	})
	require.Error(t, txErr)
	assert.Contains(t, txErr.Error(), "not found")
}

// TestLookupMatchSidesTx_BracketPath confirms the bracket fallback.
func TestLookupMatchSidesTx_BracketPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "lmstx-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "B1", SideA: "TeamRed", SideB: "TeamWhite"}},
		},
	}))

	var sideA, sideB string
	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		sideA, sideB, txErr = eng.lookupMatchSidesTx(tx, compID, "B1")
		return nil
	})
	require.NoError(t, txErr)
	assert.Equal(t, "TeamRed", sideA)
	assert.Equal(t, "TeamWhite", sideB)
}

// TestLookupMatchSidesTx_NotFound confirms the not-found error.
func TestLookupMatchSidesTx_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "lmstx-nf"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, _, txErr = eng.lookupMatchSidesTx(tx, compID, "GHOST")
		return nil
	})
	require.Error(t, txErr)
}

// TestMaybeLockTeamLineupsForRoundTx_TeamComp confirms the function
// calls LockTeamLineupsForRound for a running match in a team
// competition (TeamSize > 0).
func TestMaybeLockTeamLineupsForRoundTx_TeamComp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mlttr-team"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, TeamSize: 5,
	}))

	lineup := domain.TeamLineup{
		TeamID: "TeamA",
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "p1",
			domain.PosJiho:    "p2",
			domain.PosChuken:  "p3",
			domain.PosFukusho: "p4",
			domain.PosTaisho:  "p5",
		},
	}
	require.NoError(t, store.SetTeamLineup(compID, lineup, 5))

	result := &state.MatchResult{Status: state.MatchStatusRunning}
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		eng.maybeLockTeamLineupsForRoundTx(tx, compID, result)
		return nil
	})

	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	// The lineup for round 0 must be locked. Key format: "teamID-round".
	key := "TeamA-0"
	got, ok := lineups[key]
	require.True(t, ok)
	assert.NotNil(t, got.LockedAt, "lineup must be locked after maybeLockTeamLineupsForRoundTx")
}

// TestMaybeLockTeamLineupsForRoundTx_NilResult confirms the nil guard
// returns immediately without panicking.
func TestMaybeLockTeamLineupsForRoundTx_NilResult(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mlttr-nil"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, TeamSize: 5}))

	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		eng.maybeLockTeamLineupsForRoundTx(tx, compID, nil)
		return nil
	})
}

// TestMaybeLockTeamLineupsForRoundTx_NonTeamComp confirms no lock
// occurs when TeamSize == 0 (individual tournament).
func TestMaybeLockTeamLineupsForRoundTx_NonTeamComp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mlttr-indiv"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, TeamSize: 0}))

	result := &state.MatchResult{Status: state.MatchStatusCompleted}
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		eng.maybeLockTeamLineupsForRoundTx(tx, compID, result)
		return nil
	})
	// No lineups to check, just confirm no panic and no error.
}

// TestCheckConcurrentIneligibilityTx_AlreadyIneligible confirms that the
// T105 guard fires when the loser is already ineligible from a
// DIFFERENT match.
func TestCheckConcurrentIneligibilityTx_AlreadyIneligible(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "ccitx-inelig"

	aliceID := helper.NewUUID4()
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
	}))
	// Mark Alice as ineligible from match "Pool A-0".
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: aliceID, Eligible: false, MatchID: "Pool A-0", Reason: "kiken",
	}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		// "Alice" is the loser of a different match "Pool A-1".
		txErr = eng.checkConcurrentIneligibilityTx(tx, compID, "Pool A-1", "Alice")
		return nil
	})
	require.Error(t, txErr)
	var alreadyErrCCITX *AlreadyIneligibleError
	require.ErrorAs(t, txErr, &alreadyErrCCITX)
	assert.Equal(t, aliceID, alreadyErrCCITX.PlayerID)
}

// TestCheckConcurrentIneligibilityTx_SameMatchAllowed confirms that
// the guard is a no-op when the ineligibility record refers to the
// SAME match being processed (undo-path).
func TestCheckConcurrentIneligibilityTx_SameMatchAllowed(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "ccitx-sameok"

	aliceID := helper.NewUUID4()
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
	}))
	// Ineligibility from "Pool A-0" itself → same-match undo allowed.
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: aliceID, Eligible: false, MatchID: "Pool A-0", Reason: "kiken",
	}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.checkConcurrentIneligibilityTx(tx, compID, "Pool A-0", "Alice")
		return nil
	})
	require.NoError(t, txErr, "same-match ineligibility must be allowed (undo path)")
}

// TestCheckConcurrentIneligibilityTx_EmptyLoser confirms the empty-
// loser fast path returns nil without any store access.
func TestCheckConcurrentIneligibilityTx_EmptyLoser(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "ccitx-empty"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.checkConcurrentIneligibilityTx(tx, compID, "M1", "")
		return nil
	})
	require.NoError(t, txErr)
}

// TestWithPoolMatchTx_NotFound confirms the not-found path returns
// errMatchNotFound (caller falls through to bracket).
func TestWithPoolMatchTx_NotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "wpmtx-nf"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "X", SideB: "Y"},
	}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.withPoolMatchTx(tx, compID, "GHOST", func(_ *state.MatchResult) {})
		return nil
	})
	require.Error(t, txErr)
	assert.ErrorIs(t, txErr, errMatchNotFound)
}

// TestRestoreCompetitorEligibilityTx_EmptyPriorLoser confirms the
// empty-priorLoser fast path returns (nil, nil).
func TestRestoreCompetitorEligibilityTx_EmptyPriorLoser(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rcetx-empty"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	var (
		got   *domain.CompetitorStatus
		txErr error
	)
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		got, txErr = eng.restoreCompetitorEligibilityTx(tx, compID, "", "M1")
		return nil
	})
	require.NoError(t, txErr)
	assert.Nil(t, got)
}

// TestRestoreCompetitorEligibilityTx_HappyPath confirms the function
// writes an eligibility-restored status and returns it.
func TestRestoreCompetitorEligibilityTx_HappyPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rcetx-ok"

	aliceID := helper.NewUUID4()
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
	}))
	// Initially ineligible.
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: aliceID, Eligible: false, MatchID: "Pool A-0", Reason: "kiken",
	}))

	var (
		got   *domain.CompetitorStatus
		txErr error
	)
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		got, txErr = eng.restoreCompetitorEligibilityTx(tx, compID, "Alice", "Pool A-0")
		return nil
	})
	require.NoError(t, txErr)
	require.NotNil(t, got)
	assert.Equal(t, aliceID, got.PlayerID)
	assert.True(t, got.Eligible)

	// Verify the restored status landed on disk.
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.True(t, statuses[aliceID].Eligible)
}

// TestRecordMatchResultWithIneligibilityTx_BracketPath confirms the
// tx-aware variant updates a bracket match (not a pool match).
func TestRecordMatchResultWithIneligibilityTx_BracketPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rmwit-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "B1", SideA: "Alice", SideB: "Bob"}},
			{{ID: "B2"}},
		},
	}))

	result := &state.MatchResult{
		SideA:  "Alice",
		SideB:  "Bob",
		Winner: "Alice",
		Status: state.MatchStatusCompleted,
	}
	var (
		status *domain.CompetitorStatus
		txErr  error
	)
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		status, txErr = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "B1", result)
		return nil
	})
	require.NoError(t, txErr)
	assert.Nil(t, status, "no kiken/fusenpai → no status")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", b.Rounds[0][0].Winner)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[0][0].Status)
}

// TestHasDownstreamMatchStartedTx_PoolPath exercises the pool-match
// branch in hasDownstreamMatchStartedTx. Two cases: the Running match
// is excluded (returns false) and a Running match is NOT excluded
// (returns true).
func TestHasDownstreamMatchStartedTx_PoolPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "hdmstx-pool"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	// Use proper "PoolName-MatchIdx" ID format so the CSV round-trip
	// preserves the IDs.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled},
	}))

	t.Run("excluded running match → false", func(t *testing.T) {
		var (
			started bool
			txErr   error
		)
		_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
			// Exclude "Pool A-0" (Alice Running) → only P A-1 (Scheduled) left → false.
			started, txErr = eng.hasDownstreamMatchStartedTx(tx, compID, []string{"Alice"}, "Pool A-0")
			return nil
		})
		require.NoError(t, txErr)
		assert.False(t, started, "excluded running match must not count")
	})

	t.Run("non-excluded running match → true", func(t *testing.T) {
		var (
			started bool
			txErr   error
		)
		_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
			// Exclude "Pool A-1" (Scheduled) → "Pool A-0" (Running+Alice) detected.
			started, txErr = eng.hasDownstreamMatchStartedTx(tx, compID, []string{"Alice"}, "Pool A-1")
			return nil
		})
		require.NoError(t, txErr)
		assert.True(t, started, "running match involving Alice must be detected")
	})
}

// TestHasDownstreamMatchStartedTx_BracketPath exercises the bracket
// branch and asserts a running bracket match involving the player is
// detected.
func TestHasDownstreamMatchStartedTx_BracketPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "hdmstx-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "B1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
				{ID: "B2", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled},
			},
		},
	}))

	var (
		started bool
		txErr   error
	)
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		// Check from a hypothetical other match; B1 (Alice+Bob) is running.
		started, txErr = eng.hasDownstreamMatchStartedTx(tx, compID, []string{"Alice"}, "OTHER")
		return nil
	})
	require.NoError(t, txErr)
	assert.True(t, started, "B1 is running and involves Alice → should be detected")
}

// TestStartMatchTx_EmptyWantSet verifies that empty player names list
// returns false immediately.
func TestHasDownstreamMatchStartedTx_EmptyNames(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "hdmstx-empty"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	var started bool
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		var txErr error
		started, txErr = eng.hasDownstreamMatchStartedTx(tx, compID, []string{}, "M1")
		require.NoError(t, txErr)
		return nil
	})
	assert.False(t, started)
}

// TestRecordIneligibilityFromDecisionTx_NilResult confirms nil result
// is a no-op.
func TestRecordIneligibilityFromDecisionTx_NilResult(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "ridtx-nil"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		var status *domain.CompetitorStatus
		status, txErr = eng.recordIneligibilityFromDecisionTx(tx, compID, "M1", nil)
		assert.Nil(t, status)
		return nil
	})
	require.NoError(t, txErr)
}

// TestRecordIneligibilityFromDecisionTx_NonKiken confirms non-kiken/
// non-fusenpai decisions are a no-op.
func TestRecordIneligibilityFromDecisionTx_NonKiken(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "ridtx-nonkiken"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	result := &state.MatchResult{Decision: "fought", Winner: "Alice", SideA: "Alice", SideB: "Bob"}
	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		status, err := eng.recordIneligibilityFromDecisionTx(tx, compID, "M1", result)
		txErr = err
		assert.Nil(t, status)
		return nil
	})
	require.NoError(t, txErr)
}

// TestRecordIneligibilityFromDecisionTx_AlreadyIneligible confirms
// the AlreadyIneligibleError path (K3 rollback trigger).
func TestRecordIneligibilityFromDecisionTx_AlreadyIneligible(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "ridtx-alreadyinelig"

	aliceID := helper.NewUUID4()
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: helper.NewUUID4(), Name: "Bob", Dojo: "B"},
	}))
	// Alice is already ineligible from "Pool A-0", kiken on "Pool A-1" must fail.
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID:   aliceID,
		Eligible:   false,
		MatchID:    "Pool A-0",
		Reason:     "kiken at Pool A-0",
		RecordedAt: time.Now().UTC(),
	}))

	// Kiken on "Pool A-1" where Alice is the loser (decisionBy=aka → loser=SideA=Alice).
	result := &state.MatchResult{
		SideA:      "Alice",
		SideB:      "Bob",
		Winner:     "Bob",
		Decision:   string(domain.DecisionKiken),
		DecisionBy: "aka",
	}
	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, txErr = eng.recordIneligibilityFromDecisionTx(tx, compID, "Pool A-1", result)
		return nil
	})
	require.Error(t, txErr)
	var alreadyErrRIDTX *AlreadyIneligibleError
	require.ErrorAs(t, txErr, &alreadyErrRIDTX)
	assert.Equal(t, aliceID, alreadyErrRIDTX.PlayerID)
}

// TestRecordMatchResultWithIneligibilityTx_K3Rollback triggers the K3
// partial-write rollback path: Alice is already ineligible from
// "Pool A-0", so a kiken decision on "Pool A-1" (where Alice is the
// loser) must roll back the score-write via recordMatchResultTx and
// return AlreadyIneligibleError.
func TestRecordMatchResultWithIneligibilityTx_K3Rollback(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "k3-rollback"

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Bob"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
	}))
	// Alice is already ineligible from "Pool A-0".
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: aliceID, Eligible: false, MatchID: "Pool A-0",
		Reason: "kiken at Pool A-0", RecordedAt: time.Now().UTC(),
	}))

	// Kiken on "Pool A-1", Alice is SideA and loser (decisionBy=aka → winner=SideB=Bob).
	result := &state.MatchResult{
		SideA:      "Alice",
		SideB:      "Bob",
		Winner:     "Bob",
		Status:     state.MatchStatusCompleted,
		Decision:   string(domain.DecisionKiken),
		DecisionBy: "aka",
	}
	var (
		status *domain.CompetitorStatus
		txErr  error
	)
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		status, txErr = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-1", result)
		return nil
	})

	require.Error(t, txErr)
	var alreadyErrK3 *AlreadyIneligibleError
	require.ErrorAs(t, txErr, &alreadyErrK3, "K3: AlreadyIneligibleError must propagate")
	assert.Equal(t, aliceID, alreadyErrK3.PlayerID)
	assert.Nil(t, status)

	// K3 rollback: "Pool A-1" must be restored to Scheduled.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		if m.ID == "Pool A-1" {
			assert.Equal(t, state.MatchStatusScheduled, m.Status,
				"K3 rollback must revert Pool A-1 back to Scheduled; got %+v", m)
			assert.Empty(t, m.Winner, "K3 rollback must clear the winner")
			return
		}
	}
	t.Fatal("Pool A-1 not found after K3 rollback test")
}

// TestCheckConcurrentIneligibilityTx_PlayerNotInPool confirms that when
// the loser's name cannot be resolved to a playerID, the guard returns
// nil (best-effort, skip).
func TestCheckConcurrentIneligibilityTx_PlayerNotInPool(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "ccitx-notinpool"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: helper.NewUUID4(), Name: "Bob", Dojo: "B"},
	}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.checkConcurrentIneligibilityTx(tx, compID, "Pool A-0", "Unknown")
		return nil
	})
	require.NoError(t, txErr, "unknown player must not trigger an error (best-effort)")
}

// TestStartMatchTx_MatchNotFound confirms StartMatchTx returns an error
// when the matchID does not exist in pool or bracket.
func TestStartMatchTx_MatchNotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "smtx-nf"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		txErr = eng.StartMatchTx(tx, compID, "GHOST")
		return nil
	})
	require.Error(t, txErr)
}

// TestRecordDecisionTx_ValidationError confirms RecordDecisionTx returns
// a validation error when decisionBy is not "shiro" or "aka".
func TestRecordDecisionTx_ValidationError(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rdtx-valerr"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID}))

	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, _, txErr = eng.RecordDecisionTx(tx, compID, "Pool A-0", "kiken", "invalid-side", "", nil, false)
		return nil
	})
	require.Error(t, txErr)
	assert.Contains(t, txErr.Error(), "decisionBy")
}
