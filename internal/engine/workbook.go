package engine

import (
	"fmt"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RenderCompetitionWorkbook renders the sheet pipeline shared by both
// workbook exports of one competition (mp-yuy8): Engine.ExportCompetitionXlsx
// (internal/engine/export.go, the blank-template export) and
// export.BuildResultsWorkbook (internal/export/builder.go, the results-
// archive export). Both used to hand-copy the same sheet sequence; a sheet
// added to one and not the other has already shipped as a real bug (mp-8b1b
// finding R8, the Kachinuki Detail sheet). This is now the single place that
// sequence is spelled out.
//
// Order (mandatory, matches TestExportPipelineSheetParity in
// internal/export/pipeline_parity_test.go):
//
//  1. Data sheet (helper.AddPoolDataToSheet)
//  2. Pool Draw sheet (helper.AddPoolsToSheet)
//  3. Pool Matches sheet (helper.PrintPoolMatches)
//  4. Knockout: Tree pages + Elimination Matches, when the competition has a
//     playoff phase AND a derivable draw (helper.RenderKnockoutPages ->
//     helper.PrintEliminationWithBronze), else ErrBracketDrawMismatch when
//     the persisted bracket already carries a third-place bout that this
//     call's draw cannot re-derive -- see the hasBronze comment below for why
//     that state is refused rather than partially rendered.
//  5. Delete the "Tree" template sheet (f.DeleteSheet)
//  6. Names to Print sheet, one per shiaijo (helper.CreateNamesWithPoolToPrint)
//  7. Kachinuki Detail sheet (helper.WriteKachinukiDetailSheet)
//
// Every caller-specific extra rides OUTSIDE this function, called by the
// caller before or after: the blank-template export's Tags sheet (needs the
// tournament's publicURL, which this function deliberately does not take --
// see the parameter doc below), and the results export's score/standings/
// bracket-name literal overlays. The overlays write onto the Pool Matches and
// Elimination Matches sheets, and it is safe for them to run AFTER this
// function returns rather than interleaved where they used to sit: nothing
// this function does past PrintPoolMatches (step 3) and
// PrintEliminationWithBronze (step 4) touches either sheet again. Steps 5-7 write to the Tree, Names to Print and
// Kachinuki Detail sheets, none of which any overlay reads or writes.
//
// Parameters are explicit values, not a *state.Store: every strict/best-
// effort load already happened in the caller (mp-yuy8 criterion 6) and must
// stay there, so the two callers cannot silently diverge on how they resolve
// tournament/pools/bracket. Two parameters are themselves the caller's own
// derivation that this function does not repeat:
//
//   - draw is the caller's own EliminationDraw(...) result. EliminationDraw
//     itself needs a *state.Store (for the pure-playoffs participant-seeding
//     fallback, PlayoffFinalsFromParticipants), so it cannot move into this
//     store-free function; both callers already compute it before their call
//     here (they need numCourts, courts derived from comp, for it too).
//   - kachinukiMatches is the caller's own bout-log read: the two callers use
//     different Engine methods over different inputs (collectKachinukiMatches
//     takes an id+comp pair scoped for the blank-template path;
//     KachinukiDetailMatches takes only the id), so the DATA differs even
//     though the RENDERING (step 7) does not.
//
// courts is the caller's own CompetitionCourts(comp, tourn) result rather
// than a tournament parameter this function would resolve itself, because
// both callers already need courts (and numCourts, len(courts)) to compute
// draw before calling this function -- recomputing it here would just be a
// second call to the same pure function over the same inputs.
//
// Returns poolsByCourt, the one artifact from PrintPoolMatches a caller's own
// extras need: the results export's overlayPoolScores/overlayPoolStandings
// take it directly to map a court's "N-th pool" back to a pool index. Nothing
// else PrintPoolMatches or AddPoolDataToSheet returns (poolCoords,
// playerCoords, matchWinners) is read again once this function's own steps
// that consume them have run, so returning them would be dead weight at
// every call site.
func RenderCompetitionWorkbook(
	f *excelize.File,
	comp *state.Competition,
	pools []helper.Pool,
	bracket *state.Bracket,
	courts []string,
	courtOfPool map[string]string,
	draw *helper.KnockoutDraw,
	kachinukiMatches []helper.KachinukiMatchDetail,
) ([][]int, error) {
	// 1. Data sheet (Player Name, Dojo, Display Name).
	poolCoords, playerCoords := helper.AddPoolDataToSheet(f, pools, comp.EffectiveWithZekkenName(), comp.Name)

	// 2. Pool Draw sheet (reactive formula references to data sheet).
	if err := helper.AddPoolsToSheet(f, pools, poolCoords, playerCoords); err != nil {
		return nil, err
	}

	// 3. Pool Matches sheet. numCourts is the operator's allocation;
	//    PrintPoolMatches bands the sheet on the shiaijo count the pool phase
	//    actually runs on, clamping it itself. MatchWinnerRanksNeeded (not
	//    EffectivePoolWinners() directly): under bc-qual larger-pools, an
	//    oversized pool's crossed 2nd needs a matchWinners["<pool>-2nd"] entry
	//    too, or the Tree/Elimination sheets print it as inert literal text
	//    instead of a live link to the pool's actual result.
	matchWinners, poolsByCourt := helper.PrintPoolMatches(
		f, pools, comp.TeamSize, comp.MatchWinnerRanksNeeded(), courts, courtOfPool,
		comp.Mirror, poolCoords, playerCoords, comp.Engi,
	)

	// hasBronze: a third-place bout exists only for a competition that cannot
	// award a JOINT third place. Kendo's knockout gives both beaten
	// semi-finalists an equal 3rd and plays no bronze match at all; naginata
	// decides a single 3rd, so it needs one (docs/user-guide/organisers/
	// naginata.md). comp.Naginata is how that rule is currently encoded for a
	// knockout -- note the league path expresses the same question with its own
	// explicit field, LeagueTwoThirdPlaces, so "can this competition award a
	// joint third?" has two unrelated spellings in the tree today.
	//
	// bracket.ThirdPlaceMatch is only ever written when comp.Naginata was true
	// at generation time (buildBracketFromDraw, gated on
	// helper.NeedsBronzeBlock -- see bracket.go), and Naginata is
	// locked once the competition starts (PUT /api/competitions/:id rejects
	// a change while started; a bracket only exists once started). So a
	// non-nil ThirdPlaceMatch here always implies Naginata, and testing it
	// directly is equivalent to (comp.Naginata || isPurePlayoffs(comp,
	// pools)) && bracket != nil && bracket.ThirdPlaceMatch != nil, the
	// formula the blank-template export used pre-extraction -- the extra
	// disjunct was redundant against the writer (mp-yuy8 criterion 5). This
	// is also exactly the condition the results export already used
	// unconditionally for its includeBronze flag below, so using it here
	// keeps both callers' PrintEliminationWithBronze call identical to what
	// each already computed.
	hasBronze := bracket != nil && bracket.ThirdPlaceMatch != nil

	// 4. Knockout: Tree pages + Elimination Matches, in the one mandatory
	//    order RenderKnockoutPages enforces, for a competition with a
	//    playoff phase and a derivable draw. Band each bout by the shiaijo it
	//    is CURRENTLY on, read off the stored bracket, falling back to the
	//    draw's regions where there is none -- the operator reassigns
	//    matches between courts while the competition runs, and these sheets
	//    are what their shiaijo runs off.
	if draw != nil && comp.IsPlayoffEnabled() {
		plan := LiveCourtPlan(draw, courts, bracket)
		eliminationMatchRounds, _, err := helper.RenderKnockoutPages(f, plan, false, pools, poolCoords, playerCoords, matchWinners)
		if err != nil {
			return nil, fmt.Errorf("render workbook: %w", err)
		}
		helper.PrintEliminationWithBronze(f, matchWinners, eliminationMatchRounds, comp.TeamSize,
			plan, comp.Mirror, comp.Engi, hasBronze)
	} else if hasBronze {
		// The stored bracket already carries a third-place bout, but this
		// call's draw came back empty: the bracket and the competition's
		// current settings disagree. Reachable through a real write path,
		// not just a hand-edited bracket.json -- comp.ExtraQualifiers
		// carries no `started` guard in PUT /api/competitions/:id (unlike
		// its Naginata/Engi/Format/Kind/TeamMatchType siblings, which all
		// reject a change once the competition has started), so an operator
		// can flip it after the bracket -- bronze block included -- was
		// already built. EliminationDraw re-derives the draw from the
		// CURRENT pools and comp.ExtraQualifiers at export time (its own
		// doc comment: "equals the persisted bracket only while [pools,
		// poolWinners, courts] are unchanged since the draw"), and
		// buildPoolFedDraw's larger-pools/fill-bracket builders can mark a
		// shape "out of scope" and degrade to nil for it -- at which point
		// EliminationDraw returns nil even though bracket.ThirdPlaceMatch is
		// still on disk from the original draw.
		//
		// Rendering only the bronze block here would produce an Elimination
		// Matches sheet with a lone 3rd-place block and NO other knockout
		// content -- a silently-partial workbook the operator has no way to
		// tell is partial. Refuse instead: the operator must discard and
		// regenerate the draw, or restore the settings the bracket was
		// built with.
		return nil, ErrBracketDrawMismatch
	}
	// The bare "Tree" sheet is a layout scaffold, never output. Delete it
	// whether it was copied into pages above or left unused (a format with no
	// knockout), so no blank tree page ever reaches the workbook or the
	// printed booklet.
	if err := f.DeleteSheet(helper.SheetTree); err != nil {
		return nil, fmt.Errorf("render workbook: delete tree template sheet: %w", err)
	}

	// 5. Names to Print sheet, one per shiaijo. Clamps the allocation to the
	//    pool phase's own shiaijo count internally, as step 3 does.
	helper.CreateNamesWithPoolToPrint(f, pools, comp.EffectiveWithZekkenName(), courts, courtOfPool, playerCoords)

	// 6. Kachinuki Detail sheet (T195-T203, CHK037). Opt-in: only emitted
	//    when the competition runs the kachinuki team-match format AND has
	//    at least one match with bout data. The renderer is a no-op for
	//    empty input, so this is safe even when the format is fixed.
	if err := helper.WriteKachinukiDetailSheet(f, kachinukiMatches); err != nil {
		return nil, err
	}

	return poolsByCourt, nil
}
