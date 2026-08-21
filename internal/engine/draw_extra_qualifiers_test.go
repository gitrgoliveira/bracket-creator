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

// bc-qual LP-3c: engine wiring for the "larger-pools" ExtraQualifiers mode.
//
// buildPoolFedDraw (playoff_skeleton.go) is the shared boundary poolDraw
// (export re-derivation) and generatePoolPreviewBracket (bracket.go,
// generate-draw persist) both call. Two things must hold for every
// competition:
//
//   - ExtraQualifiersNone (the default) must be a byte-for-byte passthrough
//     to the pre-bc-qual uniform builder -- no existing competition's draw
//     may change shape because this code exists.
//   - ExtraQualifiersLargerPools must build via helper.BuildKnockoutDrawPerPool
//     and must NEVER silently fall back to the uniform builder when that
//     returns nil for an out-of-scope shape (bc-qual LP-3a review item (b)).

// uniformTestPools builds n pools of size players each, unique names/dojos
// (so helper.CreatePools' dojo-conflict avoidance never perturbs anything a
// hand-built []helper.Pool slice like this one bypasses anyway), named "Pool
// A".."Pool <n>" to match the real CreatePools/ReorderPoolsForCourts naming
// convention the draw-time labels are built from.
func uniformTestPools(n, players int) []helper.Pool {
	pools := make([]helper.Pool, n)
	for i := range pools {
		char := string(rune('A' + i%26))
		pools[i].PoolName = fmt.Sprintf("Pool %s", char)
		for j := 0; j < players; j++ {
			pools[i].Players = append(pools[i].Players, domain.Player{
				Name: fmt.Sprintf("Pool%d-Player%d", i, j),
				Dojo: fmt.Sprintf("Dojo%d-%d", i, j),
			})
		}
	}
	return pools
}

// TestBuildPoolFedDraw_StandardModeMatchesUniformBuilder pins the "byte
// identical to before" claim for the default mode: with ExtraQualifiers
// unset, buildPoolFedDraw must call helper.BuildKnockoutDraw with exactly
// the same arguments a pre-bc-qual caller would, and return exactly what it
// returns -- not a structurally-similar draw, the SAME one.
//
// Fault injection (manually verified, reverted after): temporarily making
// buildPoolFedDraw's standard-mode branch always call
// helper.BuildKnockoutDrawPerPool with a nil overrides map (instead of
// helper.BuildKnockoutDraw) turns this test red -- BuildKnockoutDrawPerPool
// lays out big blocks differently (uniformBigBlockSlots vs the old greedy
// layout at 9+ occupants), so the returned tree is no longer reflect.DeepEqual
// to helper.BuildKnockoutDraw's own output.
func TestBuildPoolFedDraw_StandardModeMatchesUniformBuilder(t *testing.T) {
	pools := uniformTestPools(4, 3)
	comp := &state.Competition{ExtraQualifiers: state.ExtraQualifiersNone, PoolWinners: 2}

	got, outOfScope, _ := buildPoolFedDraw(comp, pools, 2)
	require.False(t, outOfScope)
	require.NotNil(t, got)

	want := helper.BuildKnockoutDraw(pools, 2, 2)
	assert.Equal(t, want, got, "standard mode must be a pure passthrough to the uniform builder")
}

// TestBuildPoolFedDraw_LargerPools_SingleShiaijo_IsItsOwnDrawNotTheUniformOne
// still pins bc-qual LP-3a review item (b) -- larger-pools must never be
// served by the uniform builder -- but at the shape LP-3d changed.
//
// A single-shiaijo competition used to be refused outright: with no other
// court to cross to, BuildKnockoutDrawPerPool returned nil and this test
// asserted the refusal. It now BUILDS (the extra qualifier is seated in the
// opposite half of the only block there is), so the "never falls back"
// contract is asserted the direct way instead: the draw that comes back must
// contain the oversized pool's 2nd AND must differ from what the uniform
// builder produces for the same input. A silent fallback fails both halves,
// which is strictly more than the old nil-check could catch.
func TestBuildPoolFedDraw_LargerPools_SingleShiaijo_IsItsOwnDrawNotTheUniformOne(t *testing.T) {
	pools := uniformTestPools(3, 3)
	// Pool 0 is oversized (4 > PoolSize 3), so it sends an extra qualifier.
	pools[0].Players = append(pools[0].Players, domain.Player{Name: "Extra", Dojo: "DojoExtra"})

	comp := &state.Competition{
		ExtraQualifiers: state.ExtraQualifiersLargerPools,
		PoolWinners:     1,
		PoolSize:        3,
	}

	got, outOfScope, reason := buildPoolFedDraw(comp, pools, 1)
	require.False(t, outOfScope, "a single-shiaijo larger-pools draw is in scope since LP-3d")
	require.NotNil(t, got)
	assert.Empty(t, reason)

	assert.Contains(t, helper.TreeLeafLabels(got.Root), pools[0].PoolName+"-2nd",
		"the oversized pool's extra qualifier must be seated")

	uniform := helper.BuildKnockoutDraw(pools, 1, 1)
	require.NotNil(t, uniform, "sanity: the uniform builder also handles this shape")
	assert.NotEqual(t, uniform, got,
		"larger-pools must return its OWN draw; equality here would mean the uniform builder was substituted and the extra qualifier silently dropped")
}

// TestStartCompetition_LargerPools_RejectsPoolWinnersAtLeast2 exercises the
// state.ValidateExtraQualifiers boundary check wired into generatePools
// (pools.go) via the real generate-draw pipeline (StartCompetition ->
// runDrawPipeline -> generatePools). Draw support for larger-pools with two
// or more pool winners does not exist yet (bc-qual LP-3a review item (a)),
// so a competition configured that way must fail clean, before any pools or
// bracket are persisted, rather than reach the (unsupported) draw builder.
//
// Fault injection (manually verified, reverted after): deleting the
// state.ValidateExtraQualifiers call added to pools.go's mixed-format block
// turns this test red -- StartCompetition then succeeds (PoolWinners=2
// reaches BuildKnockoutDraw uniformly, since buildPoolFedDraw's larger-pools
// branch is only reached via generatePoolPreviewBracket, which runs AFTER
// generatePools; the pool phase itself has no other gate on PoolWinners vs
// ExtraQualifiers).
func TestStartCompetition_LargerPools_RejectsPoolWinnersAtLeast2(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "larger-pools-winners2"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 2
		c.ExtraQualifiers = state.ExtraQualifiersLargerPools
		c.Courts = []string{"A", "B"}
	})
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	err := eng.StartCompetition(compID)
	require.Error(t, err, "larger-pools with PoolWinners=2 must be rejected, not silently drawn")
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a ValidationError (-> HTTP 400)")
	assert.Contains(t, ve.Error(), "pool winners")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
}

// TestStartCompetition_LargerPools_SingleShiaijo_DrawsAndSeatsTheExtra is the
// end-to-end counterpart of the shape LP-3d opened up: a real single-shiaijo
// mixed competition, run through the actual generate-draw pipeline, must now
// START rather than fail, and must persist a bracket that holds the oversized
// pool's second qualifier.
//
// It used to assert the opposite (a ValidationError and no bracket), because
// a single shiaijo had no neighbouring court to cross to. Clubs run exactly
// this shape, so the operator ruling was to make it work; the separation the
// crossing used to buy is now a half separation inside the one block.
func TestStartCompetition_LargerPools_SingleShiaijo_DrawsAndSeatsTheExtra(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "larger-pools-single-court"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersLargerPools
		c.Courts = []string{"A"} // single shiaijo: the extra stays in this block
	})

	var players []domain.Player
	for i := 0; i < 10; i++ {
		players = append(players, domain.Player{
			Name: fmt.Sprintf("Player%02d", i),
			Dojo: fmt.Sprintf("Dojo%02d", i),
		})
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	require.NoError(t, eng.StartCompetition(compID),
		"a single-shiaijo larger-pools competition must start since LP-3d")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	require.NotEmpty(t, bracket.Rounds, "the knockout must be drawn, not dropped")

	var seated []string
	for _, round := range bracket.Rounds {
		for _, m := range round {
			seated = append(seated, m.PlaceholderA, m.PlaceholderB)
		}
	}
	var extras int
	for _, sname := range seated {
		if strings.HasSuffix(sname, "-2nd") {
			extras++
		}
	}
	assert.Positivef(t, extras, "the oversized pool's 2nd must be seated; got %v", seated)
}

// TestStartCompetition_LargerPools_CrossesOversizedPoolToNeighbourCourt is
// the end-to-end proof for bc-qual LP-3c wiring point 1: a real mixed
// competition drawn through the actual generate-draw pipeline
// (StartCompetition -> runDrawPipeline -> generatePools ->
// generatePoolPreviewBracket -> buildPoolFedDraw ->
// helper.BuildKnockoutDrawPerPool) must persist a preview bracket where the
// oversized pool's 2nd-place qualifier fights round 1 on the same-half
// neighbour court (ruling 3, bc-qual), never on its own pool's home court
// and never as a bye (ruling 3/4).
//
// Sizing: 97 participants, unique names/dojos, PoolSize=3 min mode, 4
// courts. helper.PoolCount gives 97/3 = 32 pools (floor); CreatePools fills
// all 32 to exactly 3 (96 players) then seats the 97th via forcePoolSize
// into pool index 0 ("Pool A"), producing EXACTLY one oversized (4-player)
// pool -- verified empirically below rather than assumed, since
// PoolSeeding's court-interleave reordering (upstream of CreatePools) makes
// hand-predicting composition fragile; what must hold is the INVARIANT (one
// oversized pool, crossed to its home court's same-half neighbour, fighting
// round 1), not a specific pool index. 32 pools over 4 courts is an exact,
// even 8-per-court split (helper.AssignPoolsToCourts has no remainder to
// distribute), which is also what puts the oversized pool's destination
// court at the "8 home + 1 crossed = 9 occupants" scale
// helper.BuildKnockoutDrawPerPool's block layout actually supports (see
// draw_perpool.go: a smaller block has no round-1 slot for the crossed
// occupant to fight in).
//
// Fault injection (manually verified, reverted after): temporarily reverting
// buildPoolFedDraw's larger-pools branch to call
// helper.BuildKnockoutDraw(pools, poolWinners, numCourts) unconditionally
// (i.e. ignoring ExtraQualifiers entirely) turns this test red: no
// "<pool>-2nd" placeholder appears anywhere in the persisted bracket at all,
// since the uniform builder only ever sends each pool's 1st.
func TestStartCompetition_LargerPools_CrossesOversizedPoolToNeighbourCourt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "larger-pools-crossing"

	courts := []string{"A", "B", "C", "D"}
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersLargerPools
		c.Courts = courts
	})

	var players []domain.Player
	for i := 0; i < 97; i++ {
		players = append(players, domain.Player{
			Name: fmt.Sprintf("Player%03d", i),
			Dojo: fmt.Sprintf("Dojo%03d", i), // unique: dojo-conflict avoidance never perturbs placement
		})
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	require.NoError(t, eng.StartCompetition(compID))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, pools, 32, "97 participants at PoolSize=3 min mode must yield 32 pools")

	oversizedIdx := -1
	for i, p := range pools {
		if len(p.Players) > 3 {
			require.Equal(t, -1, oversizedIdx, "expected exactly one oversized pool, found a second at index %d (%s)", i, p.PoolName)
			oversizedIdx = i
		}
	}
	require.NotEqual(t, -1, oversizedIdx, "expected exactly one oversized (4-player) pool")
	require.Len(t, pools[oversizedIdx].Players, 4)

	poolCourt, err := helper.AssignPoolsToCourts(len(pools), len(courts))
	require.NoError(t, err)
	homeCourtIdx := poolCourt[oversizedIdx]
	destCourtIdx := homeCourtIdx ^ 1 // crossNeighbourCourt's rule: same-half neighbour (B<->A, C<->D)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)

	crossedLabel := fmt.Sprintf("%s-2nd", pools[oversizedIdx].PoolName)
	var found *state.BracketMatch
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			m := &bracket.Rounds[ri][mi]
			if m.SideA == crossedLabel || m.SideB == crossedLabel {
				require.Nilf(t, found, "expected exactly one match to carry %q, found a second", crossedLabel)
				found = m
			}
		}
	}
	require.NotNilf(t, found, "expected the oversized pool's 2nd (%s) to appear in the preview bracket", crossedLabel)

	assert.NotEmpty(t, found.SideA, "the crossed qualifier must FIGHT round 1, never bye (ruling 3)")
	assert.NotEmpty(t, found.SideB, "the crossed qualifier must FIGHT round 1, never bye (ruling 3)")
	assert.Equal(t, courts[destCourtIdx], found.Court,
		"the extra qualifier must cross to the same-half neighbour court (B->A / C->D), not stay home")
	assert.NotEqual(t, courts[homeCourtIdx], found.Court,
		"the crossed qualifier must leave its own pool's home court")

	// Ruling 4: a pool sending an extra qualifier earns that qualifier no
	// priority for its OWN winner -- the pool's home 1st must still be found
	// on its own home court, not also relocated.
	homeLabel := fmt.Sprintf("%s-1st", pools[oversizedIdx].PoolName)
	var homeFound *state.BracketMatch
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			m := &bracket.Rounds[ri][mi]
			if m.SideA == homeLabel || m.SideB == homeLabel {
				homeFound = m
			}
		}
	}
	require.NotNilf(t, homeFound, "expected the oversized pool's 1st (%s) to appear in the preview bracket", homeLabel)
	assert.Equal(t, courts[homeCourtIdx], homeFound.Court, "the pool's own winner must stay on its home court")
}

// TestExportCompetitionXlsx_LargerPools_CrossedQualifierHasLiveFormula is a
// regression pin (bc-qual LP-3c review finding, not part of the original
// wiring plan): helper.PrintPoolMatches only registers a
// matchWinners["<pool>-<ordinal>"] Excel cell-reference entry for ranks
// 1..numWinners, and the two Excel export paths
// (Engine.ExportCompetitionXlsx here, internal/export.BuildResultsWorkbook)
// used to pass comp.EffectivePoolWinners() as that bound -- 1 for this
// competition's PoolWinners, since larger-pools requires PoolWinners==1. An
// oversized pool's crossed qualifier is always rank 2 (defaultWinners+1),
// which exceeded that bound, so it had NO matchWinners entry: the Tree
// sheet's writeTreeValue fell back to inert literal text ("Pool A-2nd", no
// formula), and the Elimination Matches sheet's printSingleEliminationMatch
// (which has no such fallback) emitted a BROKEN formula referencing an empty
// sheet/cell: the CONCATENATE's second argument evaluates to an empty sheet
// name and an empty cell reference, a formula Excel cannot evaluate. Both
// are silent -- neither errors, Excel just shows the wrong thing or a
// #NAME?/blank cell.
//
// state.Competition.MatchWinnerRanksNeeded (used by both export paths, and
// mirrored by the CLI in cmd/create-pools.go) fixes this: EffectivePoolWinners()+1
// under larger-pools, so every pool's rank-2 IFERROR/INDEX/MATCH cell is
// registered, even for pools that never actually use it (harmless: nothing
// looks up an unused entry).
func TestExportCompetitionXlsx_LargerPools_CrossedQualifierHasLiveFormula(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "larger-pools-export-formula"

	courts := []string{"A", "B", "C", "D"}
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersLargerPools
		c.Courts = courts
	})

	var players []domain.Player
	for i := 0; i < 97; i++ {
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

	foundCrossedFormula := false
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
				// A missing matchWinners entry formats as '%s'!%s with both
				// %s empty: the sheet-name quotes collapse together
				// immediately followed by "!" (''!).
				require.NotContainsf(t, formula, "''!", "broken CONCATENATE formula (empty sheet/cell ref) at %s!%s: %s", sheet, addr, formula)
				if strings.Contains(formula, "-2nd") && strings.Contains(formula, "'Pool Matches'!") {
					foundCrossedFormula = true
				}
			}
		}
	}
	assert.True(t, foundCrossedFormula, "expected a live CONCATENATE(\"Pool <X>-2nd \",'Pool Matches'!<cell>) formula for the oversized pool's crossed qualifier")
}
