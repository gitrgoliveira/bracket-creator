package engine

// bc-kcdg: correcting a completed knockout match whose already-propagated
// winner fed a downstream match that has since recorded its own result used
// to repaint that downstream match's SideA/SideB while leaving its own
// Winner/score/status untouched -- a display/result divergence with no
// operator-visible warning. These tests pin the refusal-by-default guard
// (guardDownstreamKnockoutCorrection / DownstreamKnockoutPlayedError), the
// forced override + downstream reopen (forceReopenDownstreamChain), and the
// two operator-ruled exemptions: a bye-completed downstream slot never
// blocks, and a matchWriteRestore (K3 rollback) is never refused.
//
// Every test here fails if the fix in applyBracketResultIn /
// guardDownstreamKnockoutCorrection / OverrideBracketWinner is reverted --
// verified by hand by re-running the suite against the pre-fix code (the
// guard call removed, force ignored): the refusal/force/bronze/override
// cases all go red (the correction silently applies and repaints), while
// the winner-unchanged/bye/rollback cases stay green (they never depended on
// the guard).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reopenedIDs pulls the ids out of what a forced correction reopened, so an
// assertion reads as a list of matches rather than of structs. The NUMBER each
// carries is what the operator is shown; it is asserted where that matters
// (TestDownstreamKnockoutCorrection_ReopenedCarriesTheMatchNumber).
func reopenedIDs(rs []ReopenedMatch) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

// readCompFile reads a competition data file straight off disk, mirroring
// superseded_matrix_test.go's footprint assertion (dir comes from
// setupTestEngine's third return value).
func readCompFile(t *testing.T, dir, compID, name string) ([]byte, error) {
	t.Helper()
	return os.ReadFile(filepath.Join(dir, "competitions", compID, name))
}

// seedThreeRoundBracket builds a hand-played knockout with a genuine two-hop
// cascade of REAL results: m-r1-0 (round 0) feeds m-r2-0 (round 1), which
// feeds m-r3-0 (round 2, the final). All three are completed with their own
// recorded ippons, exactly the shape a live tournament reaches by playing
// every round rather than by bye auto-resolution.
func seedThreeRoundBracket(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg", Status: state.CompStatusKnockout,
	}))
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{ // round 0
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}},
			},
			{ // round 1
				{ID: "m-r2-0", SideA: "Alice", SideB: "Charlie", SideAID: "alice", SideBID: "charlie",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M", "M"}},
			},
			{ // round 2, the final
				{ID: "m-r3-0", SideA: "Alice", SideB: "Dave", SideAID: "alice", SideBID: "dave",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M", "M"}},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, b))
}

// correctR1ToBob is the forward, completed correction of m-r1-0 that flips
// its winner from Alice to Bob -- the write every test below submits.
func correctR1ToBob(reason string) *state.MatchResult {
	return &state.MatchResult{
		ID: "m-r1-0", SideA: "Alice", SideB: "Bob",
		Winner: "Bob", IpponsB: []string{"M"},
		Status: state.MatchStatusCompleted, CorrectionReason: reason,
	}
}

func TestDownstreamKnockoutCorrection_RefusedByDefault(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "kcdg-refuse"
	seedThreeRoundBracket(t, store, compID)

	before, err := readCompFile(t, dir, compID, "bracket.json")
	require.NoError(t, err)
	verBefore := store.FileVersion(compID, "bracket.json")

	var reopened []ReopenedMatch
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{Reopened: &reopened})
		return err
	})

	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, txErr, &dkErr)
	assert.Equal(t, "m-r1-0", dkErr.MatchID)
	assert.Equal(t, "m-r2-0", dkErr.BlockingMatchID, "the FIRST downstream match carrying its own result")
	assert.Equal(t, "Alice", dkErr.Displaced)
	assert.Empty(t, reopened)

	// A refusal leaves NO FOOTPRINT, mirroring superseded_matrix_test.go's
	// footprint assertion: neither the bytes nor the version counter move.
	after, err := readCompFile(t, dir, compID, "bracket.json")
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a refused correction must not rewrite the file")
	assert.Equal(t, verBefore, store.FileVersion(compID, "bracket.json"),
		"a refused correction must not bump the file version")
}

func TestDownstreamKnockoutCorrection_Force(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-force"
	seedThreeRoundBracket(t, store, compID)

	var reopened []ReopenedMatch
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed override"),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	})
	require.NoError(t, txErr)
	assert.Equal(t, []string{"m-r2-0"}, reopenedIDs(reopened),
		"ONE HOP: the next match only. The round beyond it is asked about in its own turn, "+
			"when the re-fought result propagates into it (operator ruling 2026-09-19).")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)

	// The corrected match itself applied normally.
	assert.Equal(t, "Bob", b.Rounds[0][0].Winner)

	// The repaint happened one hop down (bc-kcdg's whole point:
	// propagateBracketWinner's unconditional repaint runs exactly as it
	// always has). It stops there: m-r2-0 is reopened rather than re-decided,
	// so it has no NEW winner of its own to propagate into m-r3-0, which
	// therefore keeps its stale "Alice" pairing until the operator re-scores
	// m-r2-0 -- the normal write path then repaints m-r3-0 correctly, same as
	// any other re-score.
	assert.Equal(t, "Bob", b.Rounds[1][0].SideA)
	assert.Equal(t, "Alice", b.Rounds[2][0].SideA, "unresolved until m-r2-0 is re-scored")

	// Both downstream matches went back to the QUEUE, clean: no verdict, no
	// scoreline, no audit debt (requeueBracketMatch, shared with
	// RevertMatchToQueue).
	//
	// Not "reopened to running with ReopenPending set", which is what an
	// earlier revision did. Running says a match is on court being fought, and
	// nobody is fighting these; worse, the flag makes the next completion owe a
	// reason, and only the TEAM editor has a prompt for one, so completing a
	// re-fought individual match was rejected outright with "this match was
	// reopened; ending it again requires a reason" -- the operator could not
	// enter the very result the app had just sent them to fetch. Seen in the
	// browser, not theorised. CorrectionReason stays empty for the reason
	// RevertMatchToQueue documents: a requeued match carries no stale audit
	// metadata, and the operator's justification lives on the match they
	// actually corrected.
	next := b.Rounds[1][0]
	assert.Equal(t, state.MatchStatusScheduled, next.Status,
		"reopened IN PLACE (the queue is not touched) and waiting to be fought again, NOT claimed as in progress")
	assert.Empty(t, next.Winner, "its winner is cleared")
	assert.Empty(t, next.IpponsA, "its ippons are cleared")
	assert.Contains(t, next.CorrectionReason, "m-r1-0",
		"its audit note describes its OWN reopen and names the correction that caused it")
	assert.False(t, next.ReopenPending,
		"and it owes no further reason: only the team editor can collect one, so an "+
			"individual match reopened owing one could not be completed at all (400)")

	// The round BEYOND it is deliberately untouched by this confirmation.
	beyond := b.Rounds[2][0]
	assert.Equal(t, state.MatchStatusCompleted, beyond.Status, "the deeper round keeps its result for now")
	assert.Equal(t, "Alice", beyond.Winner)
}

func TestDownstreamKnockoutCorrection_WinnerUnchangedStillApplies(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-samewinner"
	seedThreeRoundBracket(t, store, compID)

	// A plain re-score that keeps Alice as the winner (just adds a second
	// ippon) must apply even though downstream carries real results: nothing
	// about the propagation chain would change.
	result := &state.MatchResult{
		ID: "m-r1-0", SideA: "Alice", SideB: "Bob",
		Winner: "Alice", IpponsA: []string{"M", "M"},
		Status: state.MatchStatusCompleted, CorrectionReason: "extra ippon noted",
	}
	var reopened []ReopenedMatch
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", result, ForceOptions{Reopened: &reopened})
		return err
	})
	require.NoError(t, txErr)
	assert.Empty(t, reopened)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, []string{"M", "M"}, b.Rounds[0][0].IpponsA)
	// Downstream untouched: still Alice, still completed with its own result.
	assert.Equal(t, "Alice", b.Rounds[1][0].Winner)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[1][0].Status)
}

func TestDownstreamKnockoutCorrection_ByeDoesNotBlock(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-bye"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg-bye", Status: state.CompStatusKnockout,
	}))
	// Round 1's SideB is a PERMANENT structural bye (no opponent ever): once
	// SideA is filled by m-r1-0's winner, propagateBracketWinner's own
	// empty-side arm auto-completes it. This is the final round, so nothing
	// is downstream of IT either -- the whole chain has no real result
	// anywhere, and the guard must let the correction through untouched.
	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}},
			},
			{
				{ID: "m-r2-0", SideA: "Alice", SideB: "", SideAID: "alice",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, b))

	var reopened []ReopenedMatch
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{Reopened: &reopened})
		return err
	})
	require.NoError(t, txErr, "a bye-completed downstream slot must never block a correction")
	assert.Empty(t, reopened, "a bye slot carries no result of its own, so nothing needs reopening")

	got, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", got.Rounds[0][0].Winner)
	// Re-propagation: the bye slot re-derives its winner from the new SideA.
	assert.Equal(t, "Bob", got.Rounds[1][0].SideA)
	assert.Equal(t, "Bob", got.Rounds[1][0].Winner)
	assert.Equal(t, state.MatchStatusCompleted, got.Rounds[1][0].Status)
}

func TestDownstreamKnockoutCorrection_RestoreBypassesGuard(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-restore"
	seedThreeRoundBracket(t, store, compID)

	// A K3-style snapshot replay: policy=matchWriteRestore, replaying a
	// DIFFERENT winner (Bob) than currently stored (Alice), through a chain
	// whose downstream already carries real results. Must apply
	// unconditionally, force or not, exactly like every other bracket-write
	// guard's restore exemption.
	snapshot := &state.MatchResult{
		ID: "m-r1-0", SideA: "Alice", SideB: "Bob",
		Winner: "Bob", IpponsB: []string{"M"},
		Status: state.MatchStatusCompleted,
	}
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.recordBracketMatchResult(tx, compID, "m-r1-0", snapshot, matchWriteRestore, false)
		return err
	})
	require.NoError(t, txErr, "matchWriteRestore must never be refused by the downstream guard")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", b.Rounds[0][0].Winner)
	// Restore does not force-reopen anything: the guard was never consulted.
	assert.Equal(t, "Bob", b.Rounds[1][0].SideA)
	assert.Equal(t, "Alice", b.Rounds[1][0].Winner, "restore's own repaint leaves the downstream verdict untouched, same as an unguarded correction would")
}

// seedBronzeBracket builds a played naginata-shaped knockout: two semifinals,
// a final, and a bronze match, all completed with their own results. A
// semifinal here feeds BOTH the final and the bronze match, which is the one
// shape where a single correction blocks on two matches at once.
func seedBronzeBracket(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg-bronze", Status: state.CompStatusKnockout,
	}))

	b := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{ // semifinals
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}},
				{ID: "m-r1-1", SideA: "Carol", SideB: "Dave", SideAID: "carol", SideBID: "dave",
					Winner: "Carol", WinnerID: "carol", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}},
			},
			{ // final
				{ID: "m-r2-0", SideA: "Alice", SideB: "Carol", SideAID: "alice", SideBID: "carol",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M", "M"}},
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: "m-bronze", SideA: "Bob", SideB: "Dave", SideAID: "bob", SideBID: "dave",
			Winner: "Bob", WinnerID: "bob", Status: state.MatchStatusCompleted,
			IpponsA: []string{"M"}, DisplayRound: -1,
		},
	}
	require.NoError(t, store.SaveBracket(compID, b))
}

func TestDownstreamKnockoutCorrection_Bronze(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-bronze"
	seedBronzeBracket(t, store, compID)

	// Correct the m-r1-0 semifinal to Bob: bronze has ALREADY been played
	// (Bob won it), and this correction's loser-to-bronze feed would try to
	// send Alice (the current loser) into bronze's SideA in place of Bob.
	t.Run("refused without force", func(t *testing.T) {
		var reopened []ReopenedMatch
		txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{Reopened: &reopened})
			return err
		})
		var dkErr *DownstreamKnockoutPlayedError
		require.ErrorAs(t, txErr, &dkErr)
		assert.Equal(t, "m-bronze", dkErr.BlockingMatchID)
		assert.Empty(t, reopened)
	})

	t.Run("force clears BOTH siblings the semifinal fed", func(t *testing.T) {
		var reopened []ReopenedMatch
		txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed"),
				ForceOptions{Force: true, Reopened: &reopened})
			return err
		})
		require.NoError(t, txErr)
		// Bronze AND the final, on the ONE confirmation that named them both.
		//
		// They cannot be split into a dialog each, which an earlier revision
		// tried: once the first confirmation applies, the winner no longer
		// changes, so repeating the correction raises nothing and the second
		// sibling would sit contradicting itself forever. Confirmed against the
		// running app before this test was written -- re-saving the same
		// correction returned 200 with the final still showing the old winner.
		assert.ElementsMatch(t, []string{"m-bronze", "m-r2-0"}, reopenedIDs(reopened))

		got, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, state.MatchStatusScheduled, got.ThirdPlaceMatch.Status)
		assert.Empty(t, got.ThirdPlaceMatch.Winner)
		assert.Equal(t, "Alice", got.ThirdPlaceMatch.SideA, "the semifinal's new loser (Alice) was repainted into bronze")
		assert.Equal(t, state.MatchStatusScheduled, got.Rounds[1][0].Status, "the final went with it")
		assert.Empty(t, got.Rounds[1][0].Winner)
	})

	t.Run("the refusal names both", func(t *testing.T) {
		// Re-seed: the subtest above consumed the played state.
		seedBronzeBracket(t, store, compID)
		txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{})
			return err
		})
		var dkErr *DownstreamKnockoutPlayedError
		require.ErrorAs(t, txErr, &dkErr)
		assert.ElementsMatch(t, []string{"m-bronze", "m-r2-0"}, reopenedIDs(dkErr.Blocking),
			"the operator must be told about everything the confirmation will clear")
		assert.Equal(t, "m-bronze", dkErr.BlockingMatchID, "single-value form stays the first")
	})
}

// TestDownstreamKnockoutCorrection_ForceWithUnchangedWinnerClearsNothing pins
// that force does not become a licence to clear. force deliberately SKIPS the
// guard, and an unconditional requeue there meant a confirmed write that
// stored the SAME winner still sent the next round back to the queue: nobody
// displaced, nothing to unwind, a played match cleared for nothing.
func TestDownstreamKnockoutCorrection_ForceWithUnchangedWinnerClearsNothing(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-force-same-winner"
	seedThreeRoundBracket(t, store, compID)

	var reopened []ReopenedMatch
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		// Same winner as stored (Alice), just a tidied scoreline.
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", &state.MatchResult{
			ID: "m-r1-0", SideA: "Alice", SideB: "Bob",
			Winner: "Alice", IpponsA: []string{"M", "K"},
			Status: state.MatchStatusCompleted, CorrectionReason: "scoreline typo",
		}, ForceOptions{Force: true, Reopened: &reopened})
		return err
	}))
	assert.Empty(t, reopened, "nothing was displaced, so nothing may be cleared")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[1][0].Status)
	assert.Equal(t, "Alice", b.Rounds[1][0].Winner)
}

func TestOverrideBracketWinner_DownstreamGuard(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-override"
	seedThreeRoundBracket(t, store, compID)

	t.Run("refused without force", func(t *testing.T) {
		var reopened []ReopenedMatch
		applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", "Bob", 0, ForceOptions{Reopened: &reopened})
		var dkErr *DownstreamKnockoutPlayedError
		require.ErrorAs(t, err, &dkErr)
		assert.False(t, applied)
		assert.Equal(t, "m-r2-0", dkErr.BlockingMatchID)
		assert.Empty(t, reopened)

		b, lerr := store.LoadBracket(compID)
		require.NoError(t, lerr)
		assert.Equal(t, "Alice", b.Rounds[0][0].Winner, "a refused override must not touch the bracket")
	})

	t.Run("force applies and reopens the chain", func(t *testing.T) {
		var reopened []ReopenedMatch
		applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", "Bob", 0, ForceOptions{Force: true, Reopened: &reopened})
		require.NoError(t, err)
		assert.True(t, applied)
		assert.Equal(t, []string{"m-r2-0"}, reopenedIDs(reopened), "one hop, same as the score-write door")

		b, lerr := store.LoadBracket(compID)
		require.NoError(t, lerr)
		assert.Equal(t, "Bob", b.Rounds[0][0].Winner)
		assert.Equal(t, "Bob", b.Rounds[1][0].SideA)
		assert.Equal(t, state.MatchStatusScheduled, b.Rounds[1][0].Status)
		assert.Equal(t, state.MatchStatusCompleted, b.Rounds[2][0].Status, "the round beyond waits its turn")
	})
}

// TestDownstreamKnockoutCorrection_OverriddenDownstreamBlocks pins the one
// result shape that carries NO scoreline at all: a downstream match decided
// by OverrideBracketWinner (the admin bracket panel's manual winner pick).
// That path sets Winner/WinnerID/Status=completed and IsOverridden, and
// nothing else -- no ippons, no decision, no hansoku, no ResultSource -- so a
// predicate keyed only on a recorded SCORELINE reads it as an untouched slot
// and lets the correction repaint it silently, which is exactly the bc-kcdg
// defect arriving through the manual-override door instead of the score one.
// IsOverridden is what separates it from a bye pass-through (which sets the
// same Winner/Status pair and must NOT block, operator ruling bc-kcdg #2).
func TestDownstreamKnockoutCorrection_OverriddenDownstreamBlocks(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-overridden-downstream"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg", Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}},
			},
			{
				// The manual-override shape: a verdict with no scoreline.
				{ID: "m-r2-0", SideA: "Alice", SideB: "Charlie", SideAID: "alice", SideBID: "charlie",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IsOverridden: true},
			},
		},
	}))

	var reopened []ReopenedMatch
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{Reopened: &reopened})
		return err
	})

	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, txErr, &dkErr, "an override-decided downstream match must block the correction")
	assert.Equal(t, "m-r2-0", dkErr.BlockingMatchID)
	assert.Empty(t, reopened)

	// And the forced path must requeue it, clearing the manual verdict.
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed"),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	}))
	assert.Equal(t, []string{"m-r2-0"}, reopenedIDs(reopened))
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusScheduled, b.Rounds[1][0].Status)
	assert.Empty(t, b.Rounds[1][0].Winner)
	assert.False(t, b.Rounds[1][0].IsOverridden, "the manual verdict must be cleared with the rest")
}

// TestDownstreamKnockoutCorrection_OneHopPerConfirmation pins the operator's
// ruling of 2026-09-19: a correction completes its own match and reopens THE
// NEXT one, if that one is closed. It does not unwind the rounds beyond it
// behind the same confirmation. Those are reached in their own turn, because
// the re-fought result propagates a round further and meets the same check.
//
// So the operator walks the bracket forward one decision at a time: correct,
// confirm, re-fight, enter, confirm again. Each dialog names exactly one match
// and clears exactly that match.
func TestDownstreamKnockoutCorrection_OneHopPerConfirmation(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-one-hop"
	seedThreeRoundBracket(t, store, compID)

	// Hop 1: force-correct m-r1-0. ONLY m-r2-0 is requeued; the final is not
	// touched yet and keeps the result it holds.
	var reopened []ReopenedMatch
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed override"),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	}))
	assert.Equal(t, []string{"m-r2-0"}, reopenedIDs(reopened), "one hop: the next match only")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusScheduled, b.Rounds[1][0].Status)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[2][0].Status, "the round beyond is left alone for now")
	assert.Equal(t, "Alice", b.Rounds[2][0].Winner, "and keeps its recorded result")

	// Hop 2: the operator re-fights m-r2-0 and enters the result. That write
	// would propagate into the still-completed final, so it is refused in its
	// own right -- naming the final, not the match just fought.
	refought := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "m-r2-0", SideA: "Bob", SideB: "Charlie",
			Winner: "Charlie", IpponsB: []string{"M"},
			Status: state.MatchStatusCompleted,
		}
	}
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r2-0", refought(), ForceOptions{})
		return err
	})
	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, txErr, &dkErr, "entering the re-fought result must ask about the NEXT round in its own turn")
	assert.Equal(t, "m-r3-0", dkErr.BlockingMatchID)

	// Hop 2 confirmed: the result lands and the final is requeued in turn.
	reopened = nil
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r2-0", refought(),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	}))
	assert.Equal(t, []string{"m-r3-0"}, reopenedIDs(reopened))

	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Charlie", b.Rounds[1][0].Winner)
	assert.Equal(t, state.MatchStatusScheduled, b.Rounds[2][0].Status)
	assert.Equal(t, "Charlie", b.Rounds[2][0].SideA, "repainted with the new winner")
}

// TestDownstreamKnockoutCorrection_StaleWriteReportsSupersededNotGuard pins
// bc-cse finding 2: a stale replayed write (an offline court reconnecting
// with a write timestamped before the currently-stored one) must be
// reported through the ordinary ErrMatchSuperseded / {"applied":false}
// contract, never through the downstream-correction guard's destructive
// "apply and reopen" 409 -- even when the stale payload would, if it were
// ever applied, also change the propagated winner and name a downstream
// match that carries its own result. Before the fix the guard ran BEFORE
// the LWW staleness check, so this exact write was refused with
// *DownstreamKnockoutPlayedError instead.
func TestDownstreamKnockoutCorrection_StaleWriteReportsSupersededNotGuard(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-stale-guard"
	seedThreeRoundBracket(t, store, compID)

	// Stamp m-r1-0 as though it had been scored with a server-relative write
	// time, mirroring a normally-scored match.
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	b.Rounds[0][0].ModifiedAt = 10_000
	require.NoError(t, store.SaveBracket(compID, b))

	// A stale replayed correction, timestamped BEFORE the stored write.
	stale := correctR1ToBob("fix")
	stale.ModifiedAt = 5_000

	var reopened []ReopenedMatch
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", stale, ForceOptions{Reopened: &reopened})
		return err
	})

	require.ErrorIs(t, txErr, ErrMatchSuperseded, "a stale write must be reported as superseded, not refused by the downstream-correction guard")
	var dkErr *DownstreamKnockoutPlayedError
	require.NotErrorAs(t, txErr, &dkErr, "the guard must never even run for a write the LWW check would drop")
	assert.Empty(t, reopened)

	got, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", got.Rounds[0][0].Winner, "a superseded write must not apply")
}

// TestDownstreamKnockoutCorrection_GuardResolvesWinnerFromStoredSidesWhenOmitted
// pins bc-cse finding 3: a correction payload that names the new winner by
// NAME only -- no SideA/SideB, no WinnerSide, no WinnerID -- must still be
// evaluated against the ACTUAL new winner, not a name/ippon-count guess made
// before the stored side names are backfilled onto the payload.
//
// IpponsA deliberately outscores IpponsB, the OPPOSITE of the named winner:
// before the fix, bracketWinnerChanged resolved WinnerID via
// resolveWinnerIDFromSides while result.SideA/SideB were still "" (the
// backfill hadn't run yet), so its name-fallback branches never matched and
// it fell through to the ippon-count guess -- resolving SideA's id (Alice,
// the CURRENT stored winner) instead of Bob's, making bracketWinnerChanged
// report "unchanged" and skip the guard entirely for a correction that DOES
// change the propagated winner.
func TestDownstreamKnockoutCorrection_GuardResolvesWinnerFromStoredSidesWhenOmitted(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-omitted-sides"
	seedThreeRoundBracket(t, store, compID)

	result := &state.MatchResult{
		ID:      "m-r1-0",
		Winner:  "Bob",
		IpponsA: []string{"M", "M"}, // outscores IpponsB; the OPPOSITE of Winner
		IpponsB: []string{"M"},
		Status:  state.MatchStatusCompleted, CorrectionReason: "fix",
	}

	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", result, ForceOptions{})
		return err
	})

	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, txErr, &dkErr, "the guard must detect the winner actually changed even when the payload omits side names")
	assert.Equal(t, "m-r2-0", dkErr.BlockingMatchID)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", b.Rounds[0][0].Winner, "a refused correction must not touch the bracket")
	assert.Equal(t, "alice", b.Rounds[0][0].WinnerID, "a refused correction must not corrupt the stored winner id either")
}

// TestRecordDecisionTxWithOptions_DecouplesForceFromBcKcdg pins bc-cse
// finding 5: RecordDecisionTx's T103 `force` (the kiken/fusenpai
// decision-lock override) and the bc-kcdg downstream-knockout-correction
// guard are different operator confirmations. RecordDecisionTxWithOptions
// must evaluate them independently -- a decision write with the T103 force
// flag set must still be refused by the bc-kcdg guard when kcdgOpts.Force is
// false -- and must surface the reopened match ids to the caller once
// kcdgOpts.Force authorizes the write, the same contract
// RecordMatchResultWithIneligibility(Tx) and OverrideBracketWinner already
// give theirs.
func TestRecordDecisionTxWithOptions_DecouplesForceFromBcKcdg(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-decision-decouple"
	seedThreeRoundBracket(t, store, compID)

	t.Run("T103 force does not imply bc-kcdg force", func(t *testing.T) {
		// decisionBy="aka" makes m-r1-0's SideA (Alice, the current winner)
		// the WITHDRAWER, so Bob (SideB) becomes the new winner by default --
		// exactly the winner change the bc-kcdg guard exists to catch. The
		// prior result carries no withdrawal decision, so hadPriorLoser is
		// false and T103's own lock check never even runs regardless of
		// `force`: passing force=true here exercises ONLY whether it leaks
		// into the unrelated bc-kcdg guard.
		txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, _, err := eng.RecordDecisionTxWithOptions(tx, compID, "m-r1-0", "kiken-voluntary", "aka", "reason",
				nil, true, ForceOptions{})
			return err
		})
		var dkErr *DownstreamKnockoutPlayedError
		require.ErrorAs(t, txErr, &dkErr, "the bc-kcdg guard must run regardless of the unrelated T103 force flag")
		assert.Equal(t, "m-r2-0", dkErr.BlockingMatchID)

		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, "Alice", b.Rounds[0][0].Winner, "a refused decision write must not touch the bracket")
	})

	t.Run("bc-kcdg force authorizes the write and surfaces Reopened", func(t *testing.T) {
		var reopened []ReopenedMatch
		txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, _, err := eng.RecordDecisionTxWithOptions(tx, compID, "m-r1-0", "kiken-voluntary", "aka", "reason",
				nil, false, ForceOptions{Force: true, Reopened: &reopened})
			return err
		})
		require.NoError(t, txErr)
		assert.Equal(t, []string{"m-r2-0"}, reopenedIDs(reopened),
			"the reopened downstream ids must reach the caller so it can broadcast match_updated for each")

		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, "Bob", b.Rounds[0][0].Winner)
	})
}

// TestDownstreamKnockoutCorrection_RunningDownstreamIsLeftAlone pins the
// operator's ruling of 2026-09-19: "you do not affect the state of the next
// match unless it was closed."
//
// A downstream match that is being FOUGHT is not closed, so the correction is
// neither refused nor allowed to clear it. Its status and its struck ippons
// are left exactly as they are, and the side name is repainted by ordinary
// propagation, as it has always been.
//
// The trade this ruling accepts, stated plainly because it is the one thing a
// reader will want to check: the bout in progress keeps the ippons already
// struck while the name above them changes. It is accepted because a running
// match holds no VERDICT yet, so nothing self-contradictory is recorded, and
// the alternative -- wiping a bout the shiaijo is in the middle of -- is the
// more destructive act.
func TestDownstreamKnockoutCorrection_RunningDownstreamIsLeftAlone(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-running-downstream"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg", Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}},
			},
			{
				// On court right now: no verdict, one ippon struck.
				{ID: "m-r2-0", SideA: "Alice", SideB: "Charlie", SideAID: "alice", SideBID: "charlie",
					Status: state.MatchStatusRunning, IpponsB: []string{"K"}},
			},
		},
	}))

	var reopened []ReopenedMatch
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"),
			ForceOptions{Reopened: &reopened})
		return err
	}), "a running downstream match must not refuse the correction")
	assert.Empty(t, reopened, "and must not be cleared by it")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", b.Rounds[0][0].Winner)
	assert.Equal(t, state.MatchStatusRunning, b.Rounds[1][0].Status, "still being fought")
	assert.Equal(t, []string{"K"}, b.Rounds[1][0].IpponsB, "its struck ippon survives untouched")
	assert.Equal(t, "Bob", b.Rounds[1][0].SideA, "the side is repainted by ordinary propagation")
}

// TestDownstreamKnockoutCorrection_DisplacedNamesTheStaleCompetitor pins what
// the operator is told on the SECOND hop of a correction walk. By then the
// match being re-scored has already been requeued by the first confirmation,
// so its stored winner is empty, and naming "the corrected match's winner"
// produced the generic "The competitor currently recorded as advancing" in the
// ordinary path. The name the operator needs is the one still sitting in the
// blocking match: the competitor the correction is about to knock out of it.
func TestDownstreamKnockoutCorrection_DisplacedNamesTheStaleCompetitor(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-displaced-name"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg", Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				// Already requeued by an earlier confirmation: no winner left.
				{ID: "m-r2-0", SideA: "Bob", SideB: "Charlie", SideAID: "bob", SideBID: "charlie",
					Status: state.MatchStatusScheduled},
			},
			{
				// Still holds the stale pairing from the old result.
				{ID: "m-r3-0", SideA: "Alice", SideB: "Dave", SideAID: "alice", SideBID: "dave",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M", "M"}},
			},
		},
	}))

	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r2-0", &state.MatchResult{
			ID: "m-r2-0", SideA: "Bob", SideB: "Charlie",
			Winner: "Charlie", IpponsB: []string{"M"},
			Status: state.MatchStatusCompleted,
		}, ForceOptions{})
		return err
	})

	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, txErr, &dkErr)
	assert.Equal(t, "m-r3-0", dkErr.BlockingMatchID)
	assert.Equal(t, "Alice", dkErr.Displaced,
		"the operator must be told WHO is being knocked out of the blocking match, "+
			"not a generic phrase produced by the requeued match's cleared winner")
}

// TestDownstreamKnockoutCorrection_DaihyosenSilentRescoreIsNotAWinnerChange
// pins the ORDER in which the guard reads the winner. A team knockout decided
// on the representative bout is re-scored verdict-silent by design: the team
// editor omits an untouched daihyosen row's ippon arrays, and the engine
// restores the stored verdict (preserveDaihyosenOutcome) before persisting.
//
// Reading the winner BEFORE that restore made the incoming result look
// winner-less, so an ordinary re-score of a rep-bout-decided match was refused
// as a "winner change" that would displace somebody. The operator was asked to
// clear the next round for a write that stores the very same winner.
func TestDownstreamKnockoutCorrection_DaihyosenSilentRescoreIsNotAWinnerChange(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-daihyosen-silent"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg", Status: state.CompStatusKnockout, TeamSize: 3,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "TeamA", SideB: "TeamB", SideAID: "ta", SideBID: "tb",
					Winner: "TeamA", WinnerID: "ta", Status: state.MatchStatusCompleted,
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "a1", SideB: "b1", Winner: "a1"},
						// The rep bout: position -1, decided by hantei.
						{Position: -1, SideA: "a2", SideB: "b2", Winner: "a2",
							Decision: "daihyosen", IpponsA: []string{domain.HanteiMark}},
					}},
			},
			{
				{ID: "m-r2-0", SideA: "TeamA", SideB: "TeamC", SideAID: "ta", SideBID: "tc",
					Winner: "TeamA", WinnerID: "ta", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M", "M"}},
			},
		},
	}))

	// A re-score that touches only the numbered bout and says NOTHING about
	// the rep bout's verdict: exactly what the team editor sends.
	var reopened []ReopenedMatch
	err := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", &state.MatchResult{
			ID: "m-r1-0", SideA: "TeamA", SideB: "TeamB",
			Status: state.MatchStatusCompleted, CorrectionReason: "bout 1 scoreline",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "a1", SideB: "b1", Winner: "a1", IpponsA: []string{"M"}},
				{Position: -1, SideA: "a2", SideB: "b2"},
			},
		}, ForceOptions{Reopened: &reopened})
		return e
	})
	require.NoError(t, err, "a re-score that keeps the stored rep-bout winner must not be refused")
	assert.Empty(t, reopened)

	b, lerr := store.LoadBracket(compID)
	require.NoError(t, lerr)
	assert.Equal(t, "TeamA", b.Rounds[0][0].Winner, "the restored verdict still wins the encounter")
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[1][0].Status, "and the next round is untouched")
}

// TestRecordDecisionTx_T103ForceDoesNotAuthorizeDownstreamClear pins that the
// two confirmations stay separate. T103's `force` answers "undo this kiken even
// though its loser has a later match"; the bc-kcdg override answers "clear the
// already-played next round". Feeding the first into the second meant an
// operator confirming a decision-lock override silently authorized a played
// match being sent back to the queue, with no dialog ever naming it.
func TestRecordDecisionTx_T103ForceDoesNotAuthorizeDownstreamClear(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-t103-separate"
	seedThreeRoundBracket(t, store, compID)

	err := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, _, e := eng.RecordDecisionTx(tx, compID, "m-r1-0", "kiken-voluntary", "aka", "withdrew",
			nil, true /* T103 force */)
		return e
	})
	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &dkErr,
		"T103's force must not stand in for the bc-kcdg confirmation: the refusal must still be raised")

	b, lerr := store.LoadBracket(compID)
	require.NoError(t, lerr)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[1][0].Status, "and nothing downstream may be cleared")
	assert.Equal(t, "Alice", b.Rounds[1][0].Winner)
}

// TestDownstreamKnockoutCorrection_ReopenedCarriesTheMatchNumber pins the
// identity the OPERATOR is shown. "m-r2-0" is an internal id that appears on no
// operator screen: the scores list, the bracket and the Excel tree all say
// "Match 3". A refusal or a notice naming the id sends the operator looking for
// something that is not in front of them (operator ruling 2026-09-19).
func TestDownstreamKnockoutCorrection_ReopenedCarriesTheMatchNumber(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-numbers"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg", Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}, MatchNumber: 1},
			},
			{
				{ID: "m-r2-0", SideA: "Alice", SideB: "Charlie", SideAID: "alice", SideBID: "charlie",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M", "M"}, MatchNumber: 3},
			},
		},
	}))

	// The refusal names the match by number, in the error text and in the
	// structured field the client renders from.
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{})
		return err
	})
	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, txErr, &dkErr)
	require.Len(t, dkErr.Blocking, 1)
	assert.Equal(t, 3, dkErr.Blocking[0].Number, "the operator's label, not the id")
	assert.Contains(t, dkErr.Error(), "Match 3")
	assert.NotContains(t, dkErr.Error(), "m-r2-0", "the internal id must not be shown")

	// And so does what the forced write reports back.
	var reopened []ReopenedMatch
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed"),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	}))
	require.Len(t, reopened, 1)
	assert.Equal(t, "m-r2-0", reopened[0].ID, "the id still travels, for addressing the match")
	assert.Equal(t, 3, reopened[0].Number)
	assert.Equal(t, "Match 3", MatchLabel(reopened[0]))

	// A match with no number (a bye placeholder, or a pre-numbering bracket)
	// falls back to the id rather than printing "Match 0".
	assert.Equal(t, "m-x", MatchLabel(ReopenedMatch{ID: "m-x"}))
}

// TestDownstreamKnockoutCorrection_ReopenedSiblingsDoNotHoldACourt pins the
// STATUS a downstream reopen leaves behind, which is not the one the kachinuki
// reopen leaves (running) but scheduled: the match was played earlier and has
// to be fought AGAIN, with nobody on the court yet.
//
// The difference is not cosmetic. A running match holds its court, and a
// semifinal reopens BOTH the final and the 3rd-place match, which a draw runs
// on the same court by default. With both left running, scoring either was
// refused with court_busy naming the other: neither could be completed, and
// the operator had no way out of a state their own confirmation had created.
// Found by scoring a reopened bronze through the browser, after the engine
// tests had passed.
func TestDownstreamKnockoutCorrection_ReopenedSiblingsDoNotHoldACourt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-bronze-court"
	seedBronzeBracket(t, store, compID)

	var reopened []ReopenedMatch
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed"),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	})
	require.NoError(t, txErr)
	require.ElementsMatch(t, []string{"m-bronze", "m-r2-0"}, reopenedIDs(reopened))

	got, err := store.LoadBracket(compID)
	require.NoError(t, err)
	for _, m := range []*state.BracketMatch{got.ThirdPlaceMatch, &got.Rounds[1][0]} {
		assert.Equal(t, state.MatchStatusScheduled, m.Status,
			"%s must be waiting to be fought, not holding the court", m.ID)
	}
	// The one thing the court lock counts: at most one running match. Two
	// siblings reopened together must never both be live.
	running := 0
	for _, round := range got.Rounds {
		for i := range round {
			if round[i].Status == state.MatchStatusRunning {
				running++
			}
		}
	}
	if got.ThirdPlaceMatch.Status == state.MatchStatusRunning {
		running++
	}
	assert.Zero(t, running, "a reopen starts nothing; the operator starts the next match themselves")
}
