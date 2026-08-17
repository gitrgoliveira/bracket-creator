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
		// prose-only: the nil SubResults inverts under matchWriteForward.
		bm := forward()
		_, err := applyBracketMatchResult(bm, snapshot(), matchWriteForward)
		require.NoError(t, err)
		assert.Len(t, bm.SubResults, 1, "forward reads nil as 'omitted, keep stored'")
		// The match-level FLAG does not invert on this particular snapshot, and
		// that is a second rule rather than a hole in the first: the snapshot
		// reverts the match to scheduled with no winner, and hanteiStillHolds
		// refuses to carry a verdict onto a state the wire validator would
		// refuse. Forward used to keep it here, leaving `status: scheduled,
		// winner: "", decidedByHantei: true` — a match decided by judges that
		// nobody won and that has not been fought.
		assert.False(t, bm.DecidedByHantei,
			"a reversion to scheduled cannot carry the verdict, under either policy")
	})

	t.Run("forward DOES inherit the flag when the write could still carry it", func(t *testing.T) {
		// The nil-preserve on the flag, on a payload that keeps the conditions
		// the verdict rests on: still completed, still won by Kyoto, still tied.
		// This is the ordinary verdict-silent re-score (a second editor fixing
		// an unrelated field), and it is what the policy is for.
		bm := forward()
		silent := snapshot()
		silent.Status = state.MatchStatusCompleted
		silent.Winner = "Kyoto"
		silent.IpponsA, silent.IpponsB = []string{"M"}, []string{"K"}
		_, err := applyBracketMatchResult(bm, silent, matchWriteForward)
		require.NoError(t, err)
		assert.True(t, bm.DecidedByHantei, "forward reads nil as 'omitted, keep stored'")
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

// A verdict-silent forward write over a stored daihyosen verdict must reach
// bm.Winner on the BRACKET branch, exactly as it does on the pool one.
//
// applyBracketMatchResult assigns bm field by field (the pool twin overwrites
// the whole struct at the end), so the merge has to run before the winner is
// derived, validated and assigned. It used to run ~30 lines after all three:
// the restored verdict landed in bm.SubResults, but the winner deriving FROM it
// could no longer reach bm, and validateBracketCompletion had already rejected
// the write for having no winner at all.
func TestBracketForwardWrite_PreservedVerdictReachesTheWinner(t *testing.T) {
	stored := func() *state.BracketMatch {
		return &state.BracketMatch{
			ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka",
			Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{{
				Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka",
				Winner: "Kyoto", Decision: "daihyosen",
				DecidedByHantei: state.HanteiExplicit(true),
			}},
		}
	}

	// A second editor that mounted before the verdict existed: it re-sends the
	// rep bout saying nothing about hantei, and names no match winner.
	silent := func() *state.MatchResult {
		return &state.MatchResult{
			ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka",
			Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{{
				Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka",
				Decision: "daihyosen",
			}},
		}
	}

	t.Run("the completed write is accepted, not rejected as winner-less", func(t *testing.T) {
		bm := stored()
		applied, err := applyBracketMatchResult(bm, silent(), matchWriteForward)
		require.NoError(t, err, "the preserved rep bout supplies the winner the completion gate needs")
		require.True(t, applied)
	})

	t.Run("the preserved verdict decides the stored match winner", func(t *testing.T) {
		bm := stored()
		_, err := applyBracketMatchResult(bm, silent(), matchWriteForward)
		require.NoError(t, err)
		assert.Equal(t, "Kyoto", bm.Winner, "derived from the rep bout the merge restored")
		// The rep bout keeps its verdict; the match-level flag is a separate
		// field this payload says nothing about and the fixture never set.
		require.Len(t, bm.SubResults, 1)
		assert.True(t, bm.SubResults[0].HanteiDecided())
		assert.Equal(t, "Kyoto", bm.SubResults[0].Winner)
	})

	t.Run("the pool branch agrees on the same payload", func(t *testing.T) {
		// Pins the two branches together: this is the behaviour the bracket
		// half was missing, so a future divergence fails here too.
		poolStored := &state.MatchResult{
			ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka",
			SubResults: stored().SubResults,
		}
		result := silent()
		mismatch := applyPoolWrite(poolStored, result, matchWriteForward)
		require.False(t, mismatch)
		assert.Equal(t, "Kyoto", poolStored.Winner)
	})
}

// A rollback must put the match BACK, not blank it.
//
// bracketMatchAsResult is what captures the "prior" snapshot the K3 rollback
// replays. It used to omit the score entirely: a bracket match stores each
// side as one formatted string, MatchResult carries ippon arrays, and nothing
// decoded one into the other. So the snapshot arrived with nil ippons, and the
// restore wrote formatScore(nil, 0) over a real scoreline. Silent data loss on
// the one path whose whole job is to preserve state.
func TestBracketRollbackRestoresTheScore(t *testing.T) {
	// A match as it stands before the rejected write: 2-1 with an outstanding
	// hansoku against Osaka.
	scored := func() *state.BracketMatch {
		return &state.BracketMatch{
			ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka",
			Winner: "Kyoto", Status: state.MatchStatusCompleted,
			ScoreA: "MK", ScoreB: "D (H1)",
			ResultSource: "admin",
		}
	}

	t.Run("the snapshot carries the scoreline", func(t *testing.T) {
		snap := bracketMatchAsResult(scored())
		assert.Equal(t, []string{"M", "K"}, snap.IpponsA)
		assert.Equal(t, []string{"D"}, snap.IpponsB)
		assert.Equal(t, 0, snap.HansokuA)
		assert.Equal(t, 1, snap.HansokuB)
	})

	t.Run("replaying it restores the score rather than blanking it", func(t *testing.T) {
		prior := bracketMatchAsResult(scored())
		// The state after a rejected write: a different scoreline on disk.
		bm := scored()
		bm.ScoreA = "MKD"
		bm.ScoreB = ""
		applied, err := applyBracketMatchResult(bm, prior, matchWriteRestore)
		require.NoError(t, err)
		require.True(t, applied)
		assert.Equal(t, "MK", bm.ScoreA)
		assert.Equal(t, "D (H1)", bm.ScoreB)
	})

	t.Run("a genuinely unscored match still restores as unscored", func(t *testing.T) {
		// The nil-means-empty half of matchWriteRestore: a snapshot with no
		// score must CLEAR one the rejected write added, not preserve it.
		unscored := &state.BracketMatch{ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka",
			Status: state.MatchStatusScheduled}
		prior := bracketMatchAsResult(unscored)
		bm := scored()
		_, err := applyBracketMatchResult(bm, prior, matchWriteRestore)
		require.NoError(t, err)
		assert.Empty(t, bm.ScoreA)
		assert.Empty(t, bm.ScoreB)
	})

	// ModifiedAt is deliberately absent from the projection: carrying the
	// snapshot's older stamp would make the rollback lose the timestamp LWW
	// comparison against the stamp the rejected write just left, and be
	// dropped. Pinned so nobody "completes" the projection with it.
	t.Run("the rollback is not dropped by the LWW guard it passes through", func(t *testing.T) {
		// The snapshot is taken from a match stamped EARLIER than the rejected
		// write that followed it — the real ordering, and the one that makes
		// projecting ModifiedAt fatal: the restore would compare 1000 >= 9000000
		// and be discarded, leaving the rejected write on disk permanently.
		before := scored()
		before.ModifiedAt = 1_000
		prior := bracketMatchAsResult(before)
		require.Zero(t, prior.ModifiedAt, "the projection must leave the stamp off")

		bm := scored()
		bm.ModifiedAt = 9_000_000 // stamped by the write being rolled back
		applied, err := applyBracketMatchResult(bm, prior, matchWriteRestore)
		require.NoError(t, err)
		assert.True(t, applied, "a rollback must never lose to the write it undoes")
		assert.Equal(t, "MK", bm.ScoreA, "and it actually landed")
	})

	// The audit pair: restore is authoritative, so it both restores a note the
	// match held and clears one the rejected write added. applyPoolWrite has
	// always done this through its whole-struct overwrite.
	t.Run("the rejected write's correction note does not survive", func(t *testing.T) {
		bm := scored()
		bm.CorrectionReason = "typo in round 1"
		bm.ResultSource = "self-run"
		_, err := applyBracketMatchResult(bm, bracketMatchAsResult(scored()), matchWriteRestore)
		require.NoError(t, err)
		assert.Empty(t, bm.CorrectionReason, "the note belonged to the write being undone")
		assert.Equal(t, "admin", bm.ResultSource)
	})

	t.Run("a forward write still inherits an omitted note", func(t *testing.T) {
		bm := scored()
		bm.CorrectionReason = "kept"
		_, err := applyBracketMatchResult(bm, &state.MatchResult{
			ID: "m-r1-0", SideA: "Kyoto", SideB: "Osaka",
			Winner: "Kyoto", Status: state.MatchStatusCompleted,
		}, matchWriteForward)
		require.NoError(t, err)
		assert.Equal(t, "kept", bm.CorrectionReason, "forward omission means inherit")
	})
}

// The side guard abandons rather than mis-attributing. IpponsA/IpponsB are
// POSITIONAL and Winner is a NAME, so a row naming a different pair can take
// neither: the guard used to skip only the scoreline copy and fall through to
// the stamp, and the tie check could not catch that because the row it was
// left holding was still empty (0 == 0 passes vacuously).
func TestPreserveSubHanteiAbandonsOnASideMismatch(t *testing.T) {
	dh := state.DaihyosenSubPosition
	stored := []state.SubMatchResult{
		{Position: dh, SideA: "Kyoto", SideB: "Osaka", IpponsA: []string{"M"}, IpponsB: []string{"K"},
			Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true)},
	}

	t.Run("a row naming a different pair gets no verdict and no scoreline", func(t *testing.T) {
		incoming := []state.SubMatchResult{
			{Position: dh, SideA: "Nara", SideB: "Kobe", Decision: "daihyosen"},
		}
		preserveSubHantei(stored, incoming)
		assert.False(t, incoming[0].HanteiDecided(),
			"a verdict naming Kyoto cannot be stamped onto a Nara-vs-Kobe row")
		assert.Empty(t, incoming[0].Winner,
			"the stored winner names neither competitor on this row")
		assert.Empty(t, incoming[0].IpponsA, "positional ippons must not cross a renamed pair")
		assert.Empty(t, incoming[0].IpponsB)
	})

	t.Run("one drifted side is enough to abandon", func(t *testing.T) {
		incoming := []state.SubMatchResult{
			{Position: dh, SideA: "Kyoto", SideB: "Kobe", Decision: "daihyosen"},
		}
		preserveSubHantei(stored, incoming)
		assert.False(t, incoming[0].HanteiDecided())
		assert.Empty(t, incoming[0].Winner)
	})

	t.Run("matching sides still preserve, so the guard is not blanket", func(t *testing.T) {
		incoming := []state.SubMatchResult{
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"},
		}
		preserveSubHantei(stored, incoming)
		require.True(t, incoming[0].HanteiDecided())
		assert.Equal(t, "Kyoto", incoming[0].Winner)
	})
}

// Hansoku is part of the scoreline the verdict rests on, so it travels with the
// ippons. Restoring the letters but not the counts left prior's discharged "H"
// beside an incoming zero: the referee's outstanding foul silently vanished and
// the next foul on that side no longer discharged into an ippon.
func TestPreserveSubHanteiRestoresOutstandingHansoku(t *testing.T) {
	dh := state.DaihyosenSubPosition
	stored := []state.SubMatchResult{
		{Position: dh, SideA: "Kyoto", SideB: "Osaka",
			IpponsA: []string{"M"}, IpponsB: []string{"K"}, HansokuA: 0, HansokuB: 1,
			Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true)},
	}
	incoming := []state.SubMatchResult{
		{Position: dh, Decision: "daihyosen"}, // verdict-silent, scoreline-silent
	}
	preserveSubHantei(stored, incoming)
	require.True(t, incoming[0].HanteiDecided())
	assert.Equal(t, []string{"M"}, incoming[0].IpponsA)
	assert.Equal(t, 1, incoming[0].HansokuB,
		"the outstanding foul is part of the restored scoreline")
	assert.Equal(t, 0, incoming[0].HansokuA)
}
