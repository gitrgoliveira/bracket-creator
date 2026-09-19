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

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	var reopened []string
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

	var reopened []string
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed override"),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	})
	require.NoError(t, txErr)
	assert.ElementsMatch(t, []string{"m-r2-0", "m-r3-0"}, reopened,
		"both the immediate downstream match AND the deeper round it fed must be reopened")

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

	// Both downstream matches were reopened: verdict cleared, status running,
	// but the bout log (none here, so just the ippons) is what CorrectionReason
	// documents; the key assertion is the verdict fields, per reopenBracketMatch.
	for _, m := range []state.BracketMatch{b.Rounds[1][0], b.Rounds[2][0]} {
		assert.Equal(t, state.MatchStatusRunning, m.Status, "match %s must be reopened to running", m.ID)
		assert.Empty(t, m.Winner, "match %s must have its winner cleared", m.ID)
		assert.Empty(t, m.IpponsA, "match %s must have its ippons cleared", m.ID)
		assert.NotEmpty(t, m.CorrectionReason, "a reopened match must carry an audit reason")
	}
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
	var reopened []string
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

	var reopened []string
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

func TestDownstreamKnockoutCorrection_Bronze(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-bronze"
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

	// Correct the m-r1-0 semifinal to Bob: bronze has ALREADY been played
	// (Bob won it), and this correction's loser-to-bronze feed would try to
	// send Alice (the current loser) into bronze's SideA in place of Bob.
	t.Run("refused without force", func(t *testing.T) {
		var reopened []string
		txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{Reopened: &reopened})
			return err
		})
		var dkErr *DownstreamKnockoutPlayedError
		require.ErrorAs(t, txErr, &dkErr)
		assert.Equal(t, "m-bronze", dkErr.BlockingMatchID)
		assert.Empty(t, reopened)
	})

	t.Run("force applies and reopens bronze", func(t *testing.T) {
		var reopened []string
		txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed"),
				ForceOptions{Force: true, Reopened: &reopened})
			return err
		})
		require.NoError(t, txErr)
		assert.Contains(t, reopened, "m-bronze")

		got, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, state.MatchStatusRunning, got.ThirdPlaceMatch.Status)
		assert.Empty(t, got.ThirdPlaceMatch.Winner)
		assert.Equal(t, "Alice", got.ThirdPlaceMatch.SideA, "the semifinal's new loser (Alice) was repainted into bronze")
	})
}

func TestOverrideBracketWinner_DownstreamGuard(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-override"
	seedThreeRoundBracket(t, store, compID)

	t.Run("refused without force", func(t *testing.T) {
		var reopened []string
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
		var reopened []string
		applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", "Bob", 0, ForceOptions{Force: true, Reopened: &reopened})
		require.NoError(t, err)
		assert.True(t, applied)
		assert.ElementsMatch(t, []string{"m-r2-0", "m-r3-0"}, reopened)

		b, lerr := store.LoadBracket(compID)
		require.NoError(t, lerr)
		assert.Equal(t, "Bob", b.Rounds[0][0].Winner)
		assert.Equal(t, "Bob", b.Rounds[1][0].SideA)
		assert.Equal(t, state.MatchStatusRunning, b.Rounds[1][0].Status)
		assert.Equal(t, state.MatchStatusRunning, b.Rounds[2][0].Status)
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

	var reopened []string
	txErr := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{Reopened: &reopened})
		return err
	})

	var dkErr *DownstreamKnockoutPlayedError
	require.ErrorAs(t, txErr, &dkErr, "an override-decided downstream match must block the correction")
	assert.Equal(t, "m-r2-0", dkErr.BlockingMatchID)
	assert.Empty(t, reopened)

	// And the forced path must reopen it, clearing the manual verdict.
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("confirmed"),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	}))
	assert.Equal(t, []string{"m-r2-0"}, reopened)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusRunning, b.Rounds[1][0].Status)
	assert.Empty(t, b.Rounds[1][0].Winner)
	assert.False(t, b.Rounds[1][0].IsOverridden, "the manual verdict must be cleared with the rest")
}
