package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Operator ruling: "all results must be recorded into storage" — a recorded
// daihyosen hantei must never be erased by a writer that did not address it.
// preserveSubHantei guards each forward SubResults replacement: the bracket
// writes call it via preserveDaihyosenOutcome, the pool writes via
// applyPoolWrite (see that function for why it is shared). These tests are the
// unit truth table plus an end-to-end run through BOTH pool paths; the shared
// merge makes that parity structural, but the end-to-end runs stay because they
// pin the behaviour the operator sees, not the arrangement of the code.
func TestPreserveSubHantei(t *testing.T) {
	dh := state.DaihyosenSubPosition
	storedSubs := func() []state.SubMatchResult {
		return []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M"}, Winner: "K1", Decision: "fought"},
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", IpponsA: []string{}, IpponsB: []string{},
				Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true)},
		}
	}

	t.Run("verdict-silent stale snapshot preserves the stored verdict", func(t *testing.T) {
		incoming := []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M", "K"}, Winner: "K1", Decision: "fought"},
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"}, // no verdict, no winner
		}
		preserveSubHantei(storedSubs(), incoming)
		require.True(t, incoming[1].HanteiDecided())
		assert.Equal(t, "Kyoto", incoming[1].Winner)
		// The unrelated bout correction is untouched.
		assert.Equal(t, []string{"M", "K"}, incoming[0].IpponsA)
	})

	t.Run("a row recording no ippons inherits the stored scoreline too", func(t *testing.T) {
		// A writer silent about the verdict is silent about the SCORELINE the
		// verdict rests on, so both travel. Without this the 1-1 hantei would
		// persist as 0-0, which moves the Ht to the other slot and drops (E).
		stored := []state.SubMatchResult{{
			Position: dh, SideA: "Kyoto", SideB: "Osaka",
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
			Encho:  &state.EnchoMetadata{PeriodCount: 1},
			Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true),
		}}
		incoming := []state.SubMatchResult{{Position: dh, SideA: "Kyoto", SideB: "Osaka"}}
		preserveSubHantei(stored, incoming)
		require.True(t, incoming[0].HanteiDecided())
		assert.Equal(t, []string{"M"}, incoming[0].IpponsA)
		assert.Equal(t, []string{"K"}, incoming[0].IpponsB)
		require.NotNil(t, incoming[0].Encho)
		assert.Equal(t, 1, incoming[0].Encho.PeriodCount)
		assert.Equal(t, "daihyosen", incoming[0].Decision)
		// Deep copy: mutating the restored row must not reach back into store state.
		incoming[0].IpponsA[0] = "K"
		assert.Equal(t, []string{"M"}, stored[0].IpponsA)
		incoming[0].Encho.PeriodCount = 9
		assert.Equal(t, 1, stored[0].Encho.PeriodCount)
	})

	t.Run("a row that records its own ippons keeps them", func(t *testing.T) {
		stored := []state.SubMatchResult{{
			Position: dh, SideA: "Kyoto", SideB: "Osaka",
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
			Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true),
		}}
		incoming := []state.SubMatchResult{{
			Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen",
			IpponsA: []string{"D"}, IpponsB: []string{"T"},
		}}
		preserveSubHantei(stored, incoming)
		require.True(t, incoming[0].HanteiDecided())
		assert.Equal(t, []string{"D"}, incoming[0].IpponsA, "the writer addressed the scoreline")
		assert.Equal(t, []string{"T"}, incoming[0].IpponsB)
	})

	t.Run("a decision incompatible with hantei blocks the preserve", func(t *testing.T) {
		// preserveSubHantei runs AFTER validateSubBout, so a row it mutates is
		// never re-validated. Stamping a verdict onto a withdrawal would persist
		// a bout that is both a kiken and a judges' decision, and SideMarksLR
		// would then emit Kiken and Ht together.
		for _, dec := range []string{"kiken-voluntary", "kiken-injury", "fusenpai", "hikiwake"} {
			incoming := []state.SubMatchResult{
				{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: dec},
			}
			preserveSubHantei(storedSubs(), incoming)
			assert.Nil(t, incoming[0].DecidedByHantei, "decision %q must not inherit a verdict", dec)
			assert.Equal(t, "", incoming[0].Winner, "decision %q must not inherit a winner", dec)
		}
	})

	t.Run("a dropped daihyosen row is NOT resurrected", func(t *testing.T) {
		// DELETE /daihyosen removes the row deliberately and writes through this
		// same path; re-appending would make an unscored rep bout undeletable.
		incoming := []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M"}, Winner: "K1", Decision: "fought"},
		}
		preserveSubHantei(storedSubs(), incoming)
		assert.Len(t, incoming, 1, "no row is appended")
	})

	t.Run("explicit false (withdrawal) is left untouched", func(t *testing.T) {
		// state.HanteiExplicit, not state.HanteiPtr: the latter is
		// nil-for-false (built for the omitempty projection), so it cannot
		// express the withdrawal this case is about.
		incoming := []state.SubMatchResult{
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen",
				DecidedByHantei: state.HanteiExplicit(false)},
		}
		preserveSubHantei(storedSubs(), incoming)
		require.NotNil(t, incoming[0].DecidedByHantei)
		assert.False(t, *incoming[0].DecidedByHantei)
		// Non-mutation check: the withdrawal names no winner, and the stored
		// "Kyoto" must NOT be pulled across to fill the gap.
		assert.Equal(t, "", incoming[0].Winner)
	})

	t.Run("a named winner on the incoming row stands", func(t *testing.T) {
		incoming := []state.SubMatchResult{
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen", Winner: "Osaka"},
		}
		preserveSubHantei(storedSubs(), incoming)
		assert.Nil(t, incoming[0].DecidedByHantei)
		assert.Equal(t, "Osaka", incoming[0].Winner)
	})

	t.Run("an untied incoming row cannot inherit the verdict", func(t *testing.T) {
		incoming := []state.SubMatchResult{
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen",
				IpponsA: []string{"M"}, IpponsB: []string{}},
		}
		preserveSubHantei(storedSubs(), incoming)
		assert.Nil(t, incoming[0].DecidedByHantei)
	})

	t.Run("no stored verdict: nothing to preserve", func(t *testing.T) {
		stored := []state.SubMatchResult{{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"}}
		incoming := []state.SubMatchResult{{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"}}
		preserveSubHantei(stored, incoming)
		assert.Nil(t, incoming[0].DecidedByHantei)
	})
}

// End-to-end through the pool write path: device B's stale snapshot (opened
// before the verdict was recorded) saves a correction and the stored verdict
// survives in storage.
func TestPoolWrite_StaleSnapshotKeepsHantei(t *testing.T) {
	// setupTestEngine (engine_test.go) already owns this preamble, with
	// t.Cleanup rather than defer; 380+ tests in this package use it.
	eng, store, _ := setupTestEngine(t)

	compID := "sh1"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "SH", Kind: "team", TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-1", SideA: "Kyoto", SideB: "Osaka", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M"}, Winner: "K1", Decision: "fought"},
			{Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka",
				Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true)},
		},
	}}))

	// Device B never saw the verdict: its snapshot's daihyosen row is
	// verdict-silent; it corrects bout 1 only.
	patch := &state.MatchResult{
		SideA: "Kyoto", SideB: "Osaka", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M", "K"}, Winner: "K1", Decision: "fought"},
			{Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"},
		},
	}
	require.NoError(t, eng.RecordMatchResult(compID, "P1-1", patch))

	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	var dhRow *state.SubMatchResult
	for i := range stored[0].SubResults {
		if stored[0].SubResults[i].Position == state.DaihyosenSubPosition {
			dhRow = &stored[0].SubResults[i]
		}
	}
	require.NotNil(t, dhRow)
	assert.True(t, dhRow.HanteiDecided(), "stored verdict must survive the stale write")
	assert.Equal(t, "Kyoto", dhRow.Winner)
	// The correction itself landed.
	assert.Equal(t, []string{"M", "K"}, stored[0].SubResults[0].IpponsA)
}

// F2: the verdict names a winner, so the ENCOUNTER must record one too.
// deriveDaihyosenWinner runs BEFORE the preserve in every writer, when the
// incoming row is still silent, so without the second pass the stored state
// contradicts itself and pool standings credit a draw.
func TestPreserveDaihyosenOutcome_DerivesMatchWinner(t *testing.T) {
	dh := state.DaihyosenSubPosition
	stored := []state.SubMatchResult{{
		Position: dh, SideA: "Kyoto", SideB: "Osaka",
		IpponsA: []string{"M"}, IpponsB: []string{"K"},
		Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true),
	}}
	result := &state.MatchResult{
		SideA: "Kyoto", SideB: "Osaka", Winner: "",
		SubResults: []state.SubMatchResult{{Position: dh, SideA: "Kyoto", SideB: "Osaka"}},
	}
	preserveDaihyosenOutcome(stored, result)
	require.True(t, result.SubResults[0].HanteiDecided())
	assert.Equal(t, "Kyoto", result.Winner, "the encounter must not read as a draw")

	t.Run("an explicit winner is never overridden", func(t *testing.T) {
		r := &state.MatchResult{
			SideA: "Kyoto", SideB: "Osaka", Winner: "Osaka",
			SubResults: []state.SubMatchResult{{Position: dh, SideA: "Kyoto", SideB: "Osaka"}},
		}
		preserveDaihyosenOutcome(stored, r)
		assert.Equal(t, "Osaka", r.Winner)
	})

	t.Run("nil result is a no-op", func(t *testing.T) {
		assert.NotPanics(t, func() { preserveDaihyosenOutcome(stored, nil) })
	})
}

// F9: IpponsA/IpponsB are POSITIONAL, so the scoreline may only travel onto a
// row naming the same sides in the same order.
func TestPreserveSubHantei_SideGuardsTheScoreline(t *testing.T) {
	dh := state.DaihyosenSubPosition
	stored := []state.SubMatchResult{{
		Position: dh, SideA: "Kyoto", SideB: "Osaka",
		IpponsA: []string{"M"}, IpponsB: []string{"K"},
		Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true),
	}}

	t.Run("swapped sides do not inherit a mirrored scoreline", func(t *testing.T) {
		incoming := []state.SubMatchResult{{Position: dh, SideA: "Osaka", SideB: "Kyoto"}}
		preserveSubHantei(stored, incoming)
		assert.Empty(t, incoming[0].IpponsA, "Kyoto's men must not be credited to Osaka")
		assert.Empty(t, incoming[0].IpponsB)
	})

	t.Run("an unnamed row inherits the names with the scoreline", func(t *testing.T) {
		incoming := []state.SubMatchResult{{Position: dh}}
		preserveSubHantei(stored, incoming)
		assert.Equal(t, "Kyoto", incoming[0].SideA)
		assert.Equal(t, []string{"M"}, incoming[0].IpponsA)
		assert.Equal(t, []string{"K"}, incoming[0].IpponsB)
	})
}

// Same scenario through the TX pool path, which is the one POST
// /competitions/:id/matches/:mid/score and the bulk-score endpoint actually
// take. Its non-tx twin already had the guard, so this pins the twin parity:
// pass poolWriteRestore instead of poolWriteForward at the withPoolMatchTx
// closure in scoring_tx.go and this goes red while the unit table stays green.
func TestPoolWriteTx_StaleSnapshotKeepsHantei(t *testing.T) {
	// setupTestEngine (engine_test.go) already owns this preamble, with
	// t.Cleanup rather than defer; 380+ tests in this package use it.
	eng, store, _ := setupTestEngine(t)

	compID := "shtx1"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "SHTX", Kind: "team", TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-1", SideA: "Kyoto", SideB: "Osaka", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M"}, Winner: "K1", Decision: "fought"},
			{Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka",
				IpponsA: []string{"M"}, IpponsB: []string{"K"},
				Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true)},
		},
	}}))

	patch := &state.MatchResult{
		SideA: "Kyoto", SideB: "Osaka", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M", "K"}, Winner: "K1", Decision: "fought"},
			{Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"},
		},
	}
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, terr := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "P1-1", patch)
		return terr
	}))

	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	var dhRow *state.SubMatchResult
	for i := range stored[0].SubResults {
		if stored[0].SubResults[i].Position == state.DaihyosenSubPosition {
			dhRow = &stored[0].SubResults[i]
		}
	}
	require.NotNil(t, dhRow)
	assert.True(t, dhRow.HanteiDecided(), "the /score tx path must not erase a recorded verdict")
	assert.Equal(t, "Kyoto", dhRow.Winner)
	// The scoreline the verdict rests on travels with it, so the bout does not
	// silently become 0-0.
	assert.Equal(t, []string{"M"}, dhRow.IpponsA)
	assert.Equal(t, []string{"K"}, dhRow.IpponsB)
	assert.Equal(t, []string{"M", "K"}, stored[0].SubResults[0].IpponsA)
}

// A bracket rollback must UNDO the write, not re-apply it. Every nil in a
// captured snapshot (SubResults, and the hantei flag at match and sub-bout
// level) is a PRESERVE trigger on the forward path, so replaying one forward
// leaves the partial write standing. matchWriteRestore is what inverts that
// reading; these cases pin each nil separately.
//
// This replaces a unit test of normalizePriorForRollback, the caller-side shim
// that used to pre-mangle the snapshot into "explicit clear" shape. Asserting
// on the write's OUTCOME rather than on the shim's mechanics is what makes the
// test survive the fields moving into the primitive.
func TestBracketRollbackDoesNotReapplyTheWrite(t *testing.T) {
	// The staged forward write: a daihyosen won on hantei.
	forward := func() *state.BracketMatch {
		return &state.BracketMatch{
			ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka",
			Winner: "Kyoto", Status: state.MatchStatusCompleted,
			DecidedByHantei: true,
			SubResults: []state.SubMatchResult{{
				Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka",
				Winner: "Kyoto", Decision: "daihyosen",
				DecidedByHantei: state.HanteiExplicit(true),
			}},
		}
	}

	// The snapshot as lookupExistingResult hands it back: an unscored match,
	// with all three fields collapsed to nil.
	snapshot := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka",
			Status: state.MatchStatusScheduled,
		}
	}

	t.Run("restore clears the sub-results the write added", func(t *testing.T) {
		bm := forward()
		applied, err := applyBracketMatchResult(bm, snapshot(), matchWriteRestore)
		require.NoError(t, err)
		require.True(t, applied)
		assert.Empty(t, bm.SubResults, "a nil snapshot means the match HAD no bouts")
	})

	t.Run("restore clears the hantei verdict at match level", func(t *testing.T) {
		bm := forward()
		_, err := applyBracketMatchResult(bm, snapshot(), matchWriteRestore)
		require.NoError(t, err)
		assert.False(t, bm.DecidedByHantei, "the verdict being rolled back must not survive")
	})

	t.Run("restore does not re-derive a winner from the rolled-back bout", func(t *testing.T) {
		bm := forward()
		_, err := applyBracketMatchResult(bm, snapshot(), matchWriteRestore)
		require.NoError(t, err)
		assert.Empty(t, bm.Winner)
		assert.Equal(t, state.MatchStatusScheduled, bm.Status)
	})

	t.Run("the SAME snapshot forward would restore the write instead", func(t *testing.T) {
		// The mirror image, stated as a test so the policy's necessity is not
		// prose-only: every assertion above inverts under matchWriteForward.
		bm := forward()
		_, err := applyBracketMatchResult(bm, snapshot(), matchWriteForward)
		require.NoError(t, err)
		assert.Len(t, bm.SubResults, 1, "forward reads nil as 'omitted, keep stored'")
		assert.True(t, bm.DecidedByHantei)
	})

	t.Run("restore applies an explicit verdict the snapshot really held", func(t *testing.T) {
		bm := &state.BracketMatch{ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka"}
		prior := snapshot()
		prior.Status = state.MatchStatusCompleted
		prior.Winner = "Kyoto"
		prior.DecidedByHantei = state.HanteiExplicit(true)
		prior.SubResults = []state.SubMatchResult{{
			Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka",
			Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiExplicit(true),
		}}
		_, err := applyBracketMatchResult(bm, prior, matchWriteRestore)
		require.NoError(t, err)
		assert.True(t, bm.DecidedByHantei, "restore replays what the snapshot captured")
		assert.Len(t, bm.SubResults, 1)
	})
}
