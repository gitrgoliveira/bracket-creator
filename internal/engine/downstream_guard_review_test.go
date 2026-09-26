package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Four holes in bc-kcdg's guard, each found by review after the feature's own
// suite was green, each pinned here.

// TestDownstreamGuard_StatusOmittedWriteIsStillGuarded closes the widest one: a
// completing write that simply does not say so.
//
// applyBracketMatchResult reads an empty Status as Completed (older clients
// never sent the field), but the guard compared result.Status raw, so such a
// write completed the match, propagated its winner and repainted an
// already-played next round with no refusal at all. It is reachable from a real
// client: mobileapp.validateMatchResult gates its status check on
// `r.Status != ""`, so an omitted status is accepted.
func TestDownstreamGuard_StatusOmittedWriteIsStillGuarded(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-status-omitted"
	seedThreeRoundBracket(t, store, compID)

	result := correctR1ToBob("fix the semifinal")
	result.Status = "" // the whole point: the payload never says "completed"

	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", result, ForceOptions{})
		return err
	})

	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, txErr, &dkErr, "a write that completes the match must be guarded whether or not it says so")

	got, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", got.Rounds[1][0].SideA,
		"the refused write must not have repainted the played next round")
}

// TestDownstreamGuard_ReopenKeepsTheFight pins what a reopened match keeps
// and what it loses. The correction puts a different competitor in this
// match's slot and discards only the verdict: its bouts and an engi panel's
// flags stay (operator ruling 2026-09-26: scores are never cleared, the
// operator removes a wrong mark), exactly as a match still waiting in the
// queue keeps its points when the name in it changes.
func TestDownstreamGuard_ReopenKeepsTheFight(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-reopen-bouts"
	seedThreeRoundBracket(t, store, compID)

	// Give the played next round a bout log and an engi flag count, the two
	// things reopenBracketMatch deliberately leaves alone for kachinuki.
	require.NoError(t, store.UpdateBracket(compID, func(b *state.Bracket) error {
		b.Rounds[1][0].SubResults = []state.SubMatchResult{
			{Position: 1, SideA: "Alice", SideB: "Carol", Winner: "Alice", IpponsA: []string{"M"}},
		}
		b.Rounds[1][0].FlagsA = 3
		b.Rounds[1][0].FlagsB = 2
		return nil
	}))

	var reopened []ReopenedMatch
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed"),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	})
	require.NoError(t, txErr)
	require.Equal(t, []string{"m-r2-0"}, reopenedIDs(reopened))

	got, err := store.LoadBracket(compID)
	require.NoError(t, err)
	next := got.Rounds[1][0]
	assert.Equal(t, "Bob", next.SideA, "precondition: the slot was repainted")
	assert.Empty(t, next.Winner, "the verdict goes")
	require.Len(t, next.SubResults, 1, "the bouts stay")
	assert.Equal(t, []string{"M"}, next.SubResults[0].IpponsA)
	assert.Equal(t, 3, next.FlagsA, "and the engi flags")
	assert.Equal(t, 2, next.FlagsB)
}

// TestDownstreamGuard_RefusalNamesNoOneWhenTwoMatchesBlock pins the scope of
// the Displaced name.
//
// It describes ONE slot. A semifinal's two siblings hold different people (the
// final its winner, the bronze its loser), so stating one name for both is
// false of one of them: the dialog read "Ren Takada already played the
// 3rd-place match and Match 3" when Ren had played only the bronze.
func TestDownstreamGuard_RefusalNamesNoOneWhenTwoMatchesBlock(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-two-blockers-copy"
	seedBronzeBracket(t, store, compID)

	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{})
		return err
	})
	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, txErr, &dkErr)
	require.Len(t, dkErr.Blocking, 2, "precondition: a semifinal blocks on both the bronze and the final")

	msg := dkErr.Error()
	assert.NotContains(t, msg, dkErr.Displaced,
		"one competitor cannot be said to have played both matches")
	assert.Contains(t, msg, "have recorded their own results")

	// The single-blocker message still names them, where it is true.
	single := &DownstreamKnockoutPlayedError{
		MatchID:         "m-r1-0",
		Label:           "Match 1 (Semifinals)",
		BlockingMatchID: "m-r2-0",
		Blocking:        []ReopenedMatch{{ID: "m-r2-0", Number: 9}},
		Displaced:       "Alice",
	}
	assert.Contains(t, single.Error(), "Alice")
	assert.Contains(t, single.Error(), "Match 9")
}

// TestDownstreamKnockoutPlayedError_ErrorHasNoBareMatchIDFallback pins bc-cse
// item 14's removal of the bare-MatchID fallback: every production
// constructor of DownstreamKnockoutPlayedError sets Label
// (newDownstreamKnockoutPlayedError, answerRequalification), so Error()
// trusts Label as-is and no longer substitutes the internal MatchID when
// Label is empty. A raw id ("m-internal-42") reaching the operator would be
// exactly the leak MatchLabel/OperatorMatchLabel exist to prevent.
func TestDownstreamKnockoutPlayedError_ErrorHasNoBareMatchIDFallback(t *testing.T) {
	err := &DownstreamKnockoutPlayedError{
		MatchID:         "m-internal-42",
		BlockingMatchID: "m-r2-0",
		Blocking:        []ReopenedMatch{{ID: "m-r2-0", Number: 9}},
		Displaced:       "Alice",
	}
	msg := err.Error()
	assert.NotContains(t, msg, "m-internal-42",
		"an empty Label must not fall back to the raw MatchID")
}

// TestDownstreamGuard_EngiKnockoutIsGuardedToo closes the last write path.
//
// The engi dispatch seam returns before writeToPoolOrBracket, so the guard on
// that path never saw an engi write: an engi knockout correction repainted a
// played downstream match silently, the exact defect this bead exists to stop,
// on the one format nobody thought to check.
func TestDownstreamGuard_EngiKnockoutIsGuardedToo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-engi"
	comp := &state.Competition{
		ID: compID, Name: "Engi Knockout", Kind: "individual",
		Format: state.CompFormatKnockout, PoolSize: 3, PoolWinners: 2,
		Courts: []string{"A"}, StartTime: "09:00", Status: "setup", Engi: true,
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	sfIdx := len(b.Rounds) - 2
	sf0, sf1 := b.Rounds[sfIdx][0].ID, b.Rounds[sfIdx][1].ID
	finalID := b.Rounds[len(b.Rounds)-1][0].ID

	// Play both semifinals and then the final, so the final carries a result
	// of its own.
	_, err = eng.recordEngiMatchResult(store, compID, sf0, 3, 2, "")
	require.NoError(t, err)
	_, err = eng.recordEngiMatchResult(store, compID, sf1, 3, 2, "")
	require.NoError(t, err)
	_, err = eng.recordEngiMatchResult(store, compID, finalID, 3, 2, "")
	require.NoError(t, err)

	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	finalSideABefore := b.Rounds[len(b.Rounds)-1][0].SideA

	t.Run("refused by default", func(t *testing.T) {
		// Flip SF0 the other way: 2-3 sends the OTHER pair through.
		_, err := eng.recordEngiMatchResult(store, compID, sf0, 2, 3, "correction")
		var dkErr *DownstreamKnockoutPlayedError
		require.ErrorAs(t, err, &dkErr, "an engi knockout correction answers like every other write path")

		got, gErr := store.LoadBracket(compID)
		require.NoError(t, gErr)
		assert.Equal(t, finalSideABefore, got.Rounds[len(got.Rounds)-1][0].SideA,
			"the refused correction left the played final alone")
	})

	t.Run("force applies it and reopens the final", func(t *testing.T) {
		var reopened []ReopenedMatch
		_, err := eng.recordEngiMatchResult(store, compID, sf0, 2, 3, "confirmed",
			ForceOptions{Force: true, Reopened: &reopened})
		require.NoError(t, err)
		assert.Equal(t, []string{finalID}, reopenedIDs(reopened),
			"the caller is told what it reopened, so it can broadcast each one")

		got, gErr := store.LoadBracket(compID)
		require.NoError(t, gErr)
		final := got.Rounds[len(got.Rounds)-1][0]
		assert.NotEqual(t, finalSideABefore, final.SideA, "the correction propagated")
		assert.Equal(t, state.MatchStatusScheduled, final.Status, "and the final waits to be fought again")
		assert.Empty(t, final.Winner)
	})

	t.Run("through the dispatch seam, the door a client actually knocks on", func(t *testing.T) {
		// The subtests above call the engi recorder directly, so they say
		// nothing about the seam in RecordMatchResultWithIneligibilityTx that
		// routes an engi write to it -- and that seam is where ForceOptions
		// had to be threaded. Without this, dropping the options at the seam
		// leaves every test above green while no real client can either be
		// refused or confirm.
		// Put the final back to a played state so it blocks again.
		_, err := eng.recordEngiMatchResult(store, compID, finalID, 3, 2, "replay")
		require.NoError(t, err)

		// Flip SF0 back the other way, through the public entry point.
		refused := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, rErr := eng.RecordMatchResultWithIneligibilityTx(tx, compID, sf0,
				&state.MatchResult{ID: sf0, FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted, CorrectionReason: "back again"},
				ForceOptions{})
			return rErr
		})
		var dkErr *DownstreamKnockoutPlayedError
		require.ErrorAs(t, refused, &dkErr, "the seam must carry the refusal out to the caller")

		var reopened []ReopenedMatch
		confirmed := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, rErr := eng.RecordMatchResultWithIneligibilityTx(tx, compID, sf0,
				&state.MatchResult{ID: sf0, FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted, CorrectionReason: "confirmed"},
				ForceOptions{Force: true, Reopened: &reopened})
			return rErr
		})
		require.NoError(t, confirmed, "and must carry the confirmation in")
		assert.Equal(t, []string{finalID}, reopenedIDs(reopened),
			"the reopened list has to come back through the seam too, or nothing can broadcast it")
	})
}
