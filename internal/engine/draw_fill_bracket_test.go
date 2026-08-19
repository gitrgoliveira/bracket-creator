package engine

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// bc-qual LP-4: engine wiring for the "fill-bracket" ExtraQualifiers mode.
//
// buildPoolFedDraw (playoff_skeleton.go) gains a fill-bracket branch beside
// larger-pools' (bc-qual LP-3c): it resolves the draft selection via
// fillBracketDraftIndices and calls helper.BuildKnockoutDrawFillBracket. The
// same two invariants larger-pools already pins apply here:
//
//   - ExtraQualifiersNone (untouched by this file) remains a byte-for-byte
//     passthrough to the uniform builder.
//   - ExtraQualifiersFillBracket must NEVER silently fall back to the
//     uniform builder when its own builder returns nil / errors for an
//     out-of-scope shape (same discipline as bc-qual LP-3a review item (b)).

// TestBuildPoolFedDraw_FillBracket_ZeroDrafts_MatchesDirectBuilder pins the
// zero-draft case: 16 pools is already a power of two (NextPow2(16)-16=0),
// so buildPoolFedDraw's fill-bracket branch must call
// helper.BuildKnockoutDrawFillBracket with a nil/empty draft list and
// return exactly what it returns.
//
// Fault injection (manually verified, reverted after): changing
// buildPoolFedDraw's fill-bracket branch to call helper.BuildKnockoutDraw
// instead of helper.BuildKnockoutDrawFillBracket turns this test red for a
// pool count that is NOT itself a power of two (the two builders only
// coincide when there is nothing to draft AND nothing to lay out
// differently); reproduced here by comparing against the SAME builder
// (BuildKnockoutDrawFillBracket) called directly rather than
// BuildKnockoutDraw, so a regression to the wrong builder is caught even at
// zero drafts, where the two happen to produce structurally similar (but
// not code-path-identical) trees.
func TestBuildPoolFedDraw_FillBracket_ZeroDrafts_MatchesDirectBuilder(t *testing.T) {
	pools := uniformTestPools(16, 3) // already a power of two: nothing to draft
	comp := &state.Competition{ExtraQualifiers: state.ExtraQualifiersFillBracket, PoolWinners: 1, PoolSize: 3}

	got, outOfScope, reason := buildPoolFedDraw(comp, pools, 4)
	require.False(t, outOfScope)
	require.Empty(t, reason)
	require.NotNil(t, got)

	want := helper.BuildKnockoutDrawFillBracket(pools, nil, 4)
	assert.Equal(t, want, got, "fill-bracket mode must call helper.BuildKnockoutDrawFillBracket, not a different builder")
}

// TestBuildPoolFedDraw_FillBracket_OutOfScope_NeverFallsBackToUniform pins
// the LP-3a review item (b) discipline extended to fill-bracket: 3 pools,
// all exactly at the minimum size (none oversized), needs
// NextPow2(3)-3 = 1 drafted 2nd to fill a 4-leaf bracket, but there are ZERO
// oversized pools to draft from -- helper.SelectFillBracketDrafts correctly
// errors, and buildPoolFedDraw must report that as outOfScope=true and MUST
// NOT substitute the uniform builder's output.
//
// Fault injection (manually verified, reverted after): changing
// buildPoolFedDraw's fill-bracket branch to
// `if d := helper.BuildKnockoutDrawFillBracket(pools, nil, numCourts); d !=
// nil { return d, false }; return helper.BuildKnockoutDraw(pools,
// poolWinners, numCourts), false` (the silent-fallback shape this test
// exists to forbid) turns this test red: got is no longer nil and
// outOfScope is false.
func TestBuildPoolFedDraw_FillBracket_OutOfScope_NeverFallsBackToUniform(t *testing.T) {
	pools := uniformTestPools(3, 3) // no oversized pool anywhere
	comp := &state.Competition{ExtraQualifiers: state.ExtraQualifiersFillBracket, PoolWinners: 1, PoolSize: 3}

	got, outOfScope, reason := buildPoolFedDraw(comp, pools, 2)
	assert.True(t, outOfScope, "a draft with no oversized pool to source it from is out of scope")
	assert.Nil(t, got, "an out-of-scope shape must return no draw, never the uniform builder's output")
	// Third review: the specific cause is threaded up, in operator terms,
	// not just a bare "out of scope" boolean.
	assert.Contains(t, reason, "oversized pool(s) exist")

	uniform := helper.BuildKnockoutDraw(pools, 1, 2)
	require.NotNil(t, uniform, "sanity: the uniform builder does handle this shape")
}

// TestStartCompetition_FillBracket_RejectsPoolWinnersAtLeast2 mirrors
// TestStartCompetition_LargerPools_RejectsPoolWinnersAtLeast2: fill-bracket
// shares larger-pools' poolWinners==1 gate in state.ValidateExtraQualifiers
// (bc-qual LP-4 extends the fill-bracket case from reject-always to the
// same conditions), so a competition configured with PoolWinners=2 must
// fail clean, before any pools or bracket are persisted.
//
// Fault injection (manually verified, reverted after): reverting
// ValidateExtraQualifiers' fill-bracket case to unconditional rejection (the
// pre-LP-4 state) turns this test's error message assertion red (it would
// say "is not yet supported" rather than mention pool winners) but the
// overall pass/fail stays red-safe either way; the sibling
// TestStartCompetition_FillBracket_FormsPoolsAndZeroByeBracket test is what
// actually pins that fill-bracket now succeeds when this gate is satisfied.
func TestStartCompetition_FillBracket_RejectsPoolWinnersAtLeast2(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "fill-bracket-winners2"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 2
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		c.Courts = []string{"A", "B"}
	})
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	err := eng.StartCompetition(compID)
	require.Error(t, err, "fill-bracket with PoolWinners=2 must be rejected, not silently drawn")
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a ValidationError (-> HTTP 400)")
	assert.Contains(t, ve.Error(), "pool winners")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
}

// TestStartCompetition_FillBracket_FormsPoolsAndZeroByeBracket is the
// engine-level end-to-end proof for bc-qual LP-4: a real mixed competition
// drawn through the actual generate-draw pipeline (StartCompetition ->
// runDrawPipeline -> generatePools [helper.BuildPoolPhaseFillBracket] ->
// generatePoolPreviewBracket -> buildPoolFedDraw ->
// helper.BuildKnockoutDrawFillBracket) must persist:
//
//   - pools.csv with the FORMATION-computed pool count (14, not the naive
//     floor(45/3)=15 -- see helper.FillBracketPoolCount's doc comment, the
//     19WKC Women's Team shape), 3 of them oversized (45 = 11*3 + 3*4);
//   - a bracket with ZERO byes: a 16-leaf, 4-round tree where every
//     round-1 match has both sides filled, including the D=2 drafted 2nds,
//     each fighting round 1 (never a bye, rule 3).
//
// 45 participants, unique dojos, PoolSize=3 min mode, 4 courts mirrors the
// helper-level draw test (fill_bracket_test.go) at the same sizing, so the
// two are cross-checked against the same 19WKC-derived arithmetic from two
// different entry points (direct helper call vs. the full engine pipeline).
//
// Fault injection (manually verified, reverted after): temporarily reverting
// buildPoolFedDraw's fill-bracket branch to call
// helper.BuildKnockoutDraw(pools, poolWinners, numCourts) unconditionally
// (ignoring ExtraQualifiers) turns this test red: no "-2nd" label appears
// anywhere in round 1, and 14 pools do not fill a 16-leaf bracket at all (2
// leaves would come back empty/byed), which the "zero byes" assertion below
// catches directly.
func TestStartCompetition_FillBracket_FormsPoolsAndZeroByeBracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "fill-bracket-45"

	courts := []string{"A", "B", "C", "D"}
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		c.Courts = courts
	})

	var players []domain.Player
	for i := 0; i < 45; i++ {
		players = append(players, domain.Player{
			Name: fmt.Sprintf("Player%03d", i),
			Dojo: fmt.Sprintf("Dojo%03d", i), // unique: dojo-conflict avoidance never perturbs placement
		})
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	require.NoError(t, eng.StartCompetition(compID))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, pools, 14, "45 participants at minimum PoolSize=3 fill-bracket must form 14 pools (FillBracketPoolCount), not floor(45/3)=15")

	oversized := 0
	for _, p := range pools {
		if len(p.Players) > 3 {
			oversized++
		}
	}
	assert.Equal(t, 3, oversized, "45 = 11*3 + 3*4: exactly 3 oversized (4-player) pools")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	require.Len(t, bracket.Rounds, 4, "a 16-leaf bracket has 4 rounds")
	require.Len(t, bracket.Rounds[0], 8, "a 16-leaf bracket has 8 round-1 matches")

	draftedCount := 0
	for i, m := range bracket.Rounds[0] {
		assert.NotEmptyf(t, m.SideA, "round-1 match %d must not bye: fill-bracket guarantees zero byes", i)
		assert.NotEmptyf(t, m.SideB, "round-1 match %d must not bye: fill-bracket guarantees zero byes", i)
		if strings.HasSuffix(m.SideA, "-2nd") {
			draftedCount++
		}
		if strings.HasSuffix(m.SideB, "-2nd") {
			draftedCount++
		}
	}
	assert.Equal(t, 2, draftedCount, "D = NextPow2(14)-14 = 2 drafted 2nds, each fighting round 1")
}

// TestExportCompetitionXlsx_FillBracket_DraftedQualifierHasLiveFormula
// mirrors TestExportCompetitionXlsx_LargerPools_CrossedQualifierHasLiveFormula:
// state.Competition.MatchWinnerRanksNeeded's +1 (bc-qual LP-4 extension)
// must reach BOTH Excel export paths for fill-bracket exactly as it does
// for larger-pools, so a drafted 2nd's Tree/Elimination-sheet cell is a LIVE
// CONCATENATE formula referencing the pool's actual result, never inert
// literal text or a broken formula referencing an empty sheet/cell.
func TestExportCompetitionXlsx_FillBracket_DraftedQualifierHasLiveFormula(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "fill-bracket-export-formula"

	courts := []string{"A", "B", "C", "D"}
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		c.Courts = courts
	})

	var players []domain.Player
	for i := 0; i < 45; i++ {
		players = append(players, domain.Player{
			Name: fmt.Sprintf("Player%03d", i),
			Dojo: fmt.Sprintf("Dojo%03d", i),
		})
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	foundDraftedFormula := false
	for _, sheet := range f.GetSheetList() {
		if !strings.HasPrefix(sheet, "Tree") && sheet != "Elimination Matches" {
			continue
		}
		rows, err := f.GetRows(sheet)
		require.NoError(t, err)
		for r := range rows {
			for c := 0; c < 40; c++ {
				addr, _ := excelize.CoordinatesToCellName(c+1, r+1)
				formula, ferr := f.GetCellFormula(sheet, addr)
				require.NoError(t, ferr)
				if formula == "" {
					continue
				}
				require.NotContainsf(t, formula, "''!", "broken CONCATENATE formula (empty sheet/cell ref) at %s!%s: %s", sheet, addr, formula)
				if strings.Contains(formula, "-2nd") && strings.Contains(formula, "'Pool Matches'!") {
					foundDraftedFormula = true
				}
			}
		}
	}
	assert.True(t, foundDraftedFormula, "expected a live CONCATENATE(\"Pool <X>-2nd \",'Pool Matches'!<cell>) formula for a drafted 2nd qualifier")
}

// TestStartCompetition_FillBracket_HalfCapacityRefusal_ObservedPipelineState
// answers a review question about bc-qual LP-4's rework, reported rather
// than redesigned per the review's own instruction ("do not redesign the
// pipeline; just report"): a half-capacity refusal (rule 3's opposite-half
// pairing cannot be satisfied for this specific pool/court shape -- see
// TestFillBracketFormationAndBuilderAgree, internal/helper, which sweeps
// and counts these) is discovered INSIDE generatePoolPreviewBracket, which
// runs AFTER generatePools has already called SavePools/SavePoolMatches
// (competition.go's runDrawPipeline, unchanged by this rework: "Generate
// Pools, Bracket, or Swiss round-1 ... write to other files ... via their
// own per-comp lock acquisitions, so they run OUTSIDE the
// UpdateCompetitionChanged transform below"). This test observes, rather
// than assumes, the resulting on-disk state for a real StartCompetition
// call: pools.csv/pool-matches.csv ARE persisted (5 pools, matching
// FillBracketPoolCount's own P) even though the overall call fails,
// bracket.json is NOT written, and the competition's Status is NEVER
// advanced past CompStatusSetup (the atomic status commit, further down in
// runDrawPipeline, is never reached).
//
// 18 entrants at minimum pool size 3, 4 shiaijo is one of the capacity-aware
// rework's own residual pairs (third review: making SelectFillBracketDrafts
// capacity-aware dropped the swept range's refusal count from 123/867 to
// 21/867, ALL 21 now at courts=4 only -- see
// TestFillBracketFormationAndBuilderAgree's pinned list, internal/helper,
// and the identical n/courts pair in the CLI-level
// TestCreatePools_ExtraQualifiers_FillBracket_OutOfScope, cmd/).
//
// This is a PRE-EXISTING pipeline characteristic (runDrawPipeline's own doc
// comment already states "A failure mid-pipeline leaves partial state on
// disk"), not something bc-qual LP-4 introduces: the identical shape exists
// for larger-pools' own out-of-scope path
// (TestStartCompetition_LargerPools_OutOfScopeSingleCourt_ReturnsValidationError
// above only checked the bracket side of this, not pools.csv/Status; this
// test checks all three explicitly). A retry (calling StartCompetition
// again with nothing else changed) re-runs the whole pipeline from Setup
// and deterministically regenerates the same 5 pools, self-healing the
// pools.csv/pool-matches.csv content -- but this test does not exercise a
// retry, since the review asked for a report of the OBSERVED behaviour,
// not a redesign or a fix.
func TestStartCompetition_FillBracket_HalfCapacityRefusal_ObservedPipelineState(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "fill-bracket-half-capacity-refusal"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		c.Courts = []string{"A", "B", "C", "D"} // 18 entrants at min 3, 4 shiaijo: P=5, D=3, a half-capacity refusal shape
	})

	var players []domain.Player
	for i := 0; i < 18; i++ {
		players = append(players, domain.Player{
			Name: fmt.Sprintf("Player%03d", i),
			Dojo: fmt.Sprintf("Dojo%03d", i), // unique: dojo-conflict avoidance never perturbs placement
		})
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	err := eng.StartCompetition(compID)
	require.Error(t, err, "a half-capacity refusal must fail the overall call")
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a ValidationError (-> HTTP 400)")
	// Third review: the operator sees the SPECIFIC cause, not a generic
	// "out of scope" -- SelectFillBracketDrafts' own error is threaded
	// through buildPoolFedDraw/generatePoolPreviewBracket unmodified.
	assert.Contains(t, err.Error(), "cannot supply both halves of the bracket")

	// Observed state 1: pools.csv/pool-matches.csv ARE on disk, formed by
	// generatePools before generatePoolPreviewBracket ever ran.
	pools, perr := store.LoadPools(compID)
	require.NoError(t, perr)
	assert.Lenf(t, pools, 5, "generatePools persisted pools despite the overall call failing later (FillBracketPoolCount's own P for 18 entrants at min 3)")
	poolMatches, merr := store.LoadPoolMatches(compID)
	require.NoError(t, merr)
	assert.NotEmpty(t, poolMatches, "pool-matches.csv was also persisted before the failure")

	// Observed state 2: bracket.json was NOT written (generatePoolPreviewBracket
	// returned its error before calling SaveBracket).
	bracket, berr := store.LoadBracket(compID)
	require.NoError(t, berr)
	if bracket != nil {
		assert.Empty(t, bracket.Rounds, "no bracket rounds must be persisted for a rejected fill-bracket draw")
		assert.Nil(t, bracket.ThirdPlaceMatch)
	}

	// Observed state 3: the competition's Status never advanced past Setup
	// (the atomic CompStatusDrawReady commit, further down runDrawPipeline,
	// is never reached because generatePoolPreviewBracket's error returns
	// out of the whole pipeline first).
	comp, cerr := store.LoadCompetition(compID)
	require.NoError(t, cerr)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "Status is left at Setup even though pools.csv already reflects a formed draw")
}
