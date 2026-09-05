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
//  1. Data sheet (helper.AddDataToSheetForExport, the ONE writer -- see the
//     namesToPrintPlayers parameter doc below)
//  2. Pool Draw sheet (helper.AddPoolsToSheet)
//  3. Pool Matches sheet (helper.PrintPoolMatches)
//  4. Knockout: Tree pages + Elimination Matches, when the competition has a
//     playoff phase AND a derivable draw (helper.RenderKnockoutPages ->
//     helper.PrintEliminationWithBronze), else ErrBracketDrawMismatch when
//     the persisted bracket already carries knockout content (see
//     bracketHasKnockoutContent) that this call's draw cannot re-derive --
//     see that predicate's comment below for why that state is refused
//     rather than partially rendered.
//  5. Delete the "Tree" template sheet (f.DeleteSheet)
//  6. Names to Print sheet, one per shiaijo (helper.CreateNamesWithPoolToPrint,
//     or helper.CreateNamesToPrint over namesToPrintPlayers -- same branch as
//     step 1)
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
//
// namesToPrintPlayers is bc-pnum A8/[review]'s single guarded branch for a
// playoffs-only competition (never has a pools.csv, so nothing above would
// otherwise populate the Data / Names-to-Print sheets at all): non-nil only
// for the blank-template export's numbered roster
// (Engine.NumberedParticipantsFor), nil for every other caller and every
// other format. Before this parameter existed, the blank-template export
// called helper.AddPoolDataToSheet here (over the empty pools slice, writing
// only headers) and THEN called helper.AddPlayerDataToSheet a second time,
// itself, after this function returned -- two writers of the same sheet,
// which is why "Data added to spreadsheet" used to print twice for the one
// playoffs-only shape that needed the second writer at all. Steps 1 and 6
// below are now the ONE place that decides which writer runs, so the sheet
// is written exactly once regardless of caller.
func RenderCompetitionWorkbook(
	f *excelize.File,
	comp *state.Competition,
	pools []helper.Pool,
	bracket *state.Bracket,
	courts []string,
	courtOfPool map[string]string,
	draw *helper.KnockoutDraw,
	kachinukiMatches []helper.KachinukiMatchDetail,
	namesToPrintPlayers []helper.Player,
) ([][]int, error) {
	// 1. Data sheet (Player Name, Dojo, Display Name). AddDataToSheetForExport
	//    is the ONE writer of this sheet: it picks AddPlayerDataToSheet over
	//    the numbered roster when namesToPrintPlayers is non-empty (the
	//    playoffs-only shape with no pools.csv), else AddPoolDataToSheet as
	//    before. AddPoolDataToSheet is never ALSO called in the
	//    namesToPrintPlayers branch, which is what used to make "Data added
	//    to spreadsheet" print twice for that one shape (see the doc comment
	//    above).
	poolCoords, playerCoords := helper.AddDataToSheetForExport(f, pools, namesToPrintPlayers, comp.EffectiveWithZekkenName(), comp.Name)

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
	// award a JOINT third place. Kendo's knockout convention gives both beaten
	// semi-finalists an equal 3rd and plays no bronze match at all; a
	// competition that requires a single 3rd needs one instead. Before bc-3rdp
	// this was encoded as comp.Naginata alone; state.Competition.
	// RequiresSingleThirdPlace / EffectiveTwoThirdPlaces is now the ONE named
	// predicate for "can this competition award a joint third?", answering it
	// for every format (Naginata is one input among several it resolves, not
	// the whole rule).
	//
	// bracket.ThirdPlaceMatch is only ever written when comp.
	// RequiresSingleThirdPlace() was true at generation time (buildBracketFromDraw,
	// gated on helper.NeedsBronzeBlock -- see bracket.go), and TwoThirdPlaces/
	// Naginata are both locked once the competition starts (PUT
	// /api/competitions/:id rejects a change while started; a bracket only
	// exists once started). So a non-nil ThirdPlaceMatch here always implies
	// RequiresSingleThirdPlace() was true at draw time, and testing it directly
	// is equivalent to (comp.RequiresSingleThirdPlace() || isPurePlayoffs(comp,
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
	} else if comp.IsPlayoffEnabled() && bracketHasKnockoutContent(bracket) {
		// The stored bracket already carries knockout content -- a
		// third-place bout, or at least one round-1-or-later match -- but
		// this call's draw came back empty: the bracket and the
		// competition's current settings disagree. Reachable through a real
		// write path, not just a hand-edited bracket.json -- comp.
		// ExtraQualifiers carries no `started` guard in PUT
		// /api/competitions/:id (unlike its Naginata/Engi/Format/Kind/
		// TeamMatchType siblings, which all reject a change once the
		// competition has started), so an operator can flip it after the
		// bracket was already built. EliminationDraw re-derives the draw
		// from the CURRENT pools and comp.ExtraQualifiers at export time
		// (its own doc comment: "equals the persisted bracket only while
		// [pools, poolWinners, courts] are unchanged since the draw"), and
		// buildPoolFedDraw's larger-pools/fill-bracket builders can mark a
		// shape "out of scope" and degrade to nil for it -- at which point
		// EliminationDraw returns nil even though the bracket's matches are
		// still on disk from the original draw. The bronze-only shape
		// (mp-yuy8 phase 3) was the first instance found; a stored bracket
		// with real Rounds content and no bronze block falls through the
		// same gap for the identical reason, so both are refused by the one
		// predicate rather than the narrower bronze-only test.
		//
		// Rendering here would produce an Elimination Matches sheet with
		// only whatever fragment of the stored bracket happens to survive
		// (or, for the bronze-only shape, nothing at all but a lone 3rd-
		// place block) -- a silently-partial workbook the operator has no
		// way to tell is partial. Refuse instead: the operator must discard
		// and regenerate the draw, or restore the settings the bracket was
		// built with.
		//
		// comp.IsPlayoffEnabled() is required here, and is a NARROWER gate
		// than "this competition has a knockout": it excludes league and
		// swiss, which never have a bracket to mismatch in the first place.
		//
		// Format == "" is IN scope here, not an exclusion: IsPlayoffEnabled
		// and isPurePlayoffs (playoff_skeleton.go) both go through
		// state.Competition.EffectiveFormat(), which reads an unset Format as
		// standalone playoffs -- matching runDrawPipeline's generation switch,
		// whose `default:` case has always built a real bracket via
		// generatePlayoffs for "" exactly as it does for the literal
		// "playoffs" value. Before EffectiveFormat existed, both predicates
		// compared Format literally and were blind to "", so a stored bracket
		// with real Rounds content but no re-derivable draw for an
		// empty-Format competition fell through this guard unrefused and step
		// 4 rendered nothing -- an empty Elimination Matches sheet with no
		// error. EffectiveFormat is why that shape is now caught here instead.
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
	//    pool phase's own shiaijo count internally, as step 3 does. Same
	//    namesToPrintPlayers branch as step 1: a playoffs-only export routes
	//    through CreateNamesToPrint over the numbered roster instead of
	//    CreateNamesWithPoolToPrint's empty-pools no-op (which used to leave
	//    this sheet missing entirely, see bc-pnum A8).
	if len(namesToPrintPlayers) > 0 {
		helper.CreateNamesToPrint(f, namesToPrintPlayers, comp.EffectiveWithZekkenName(), courts, playerCoords)
	} else {
		helper.CreateNamesWithPoolToPrint(f, pools, comp.EffectiveWithZekkenName(), courts, courtOfPool, playerCoords)
	}

	// 6. Kachinuki Detail sheet (T195-T203, CHK037). Opt-in: only emitted
	//    when the competition runs the kachinuki team-match format AND has
	//    at least one match with bout data. The renderer is a no-op for
	//    empty input, so this is safe even when the format is fixed.
	if err := helper.WriteKachinukiDetailSheet(f, kachinukiMatches); err != nil {
		return nil, err
	}

	return poolsByCourt, nil
}

// bracketHasKnockoutContent reports whether bracket carries knockout content
// that RenderCompetitionWorkbook's step 4 would need to show: a third-place
// bout, or at least one match in any round. It is the guard for
// ErrBracketDrawMismatch above -- widened from an earlier version that only
// checked ThirdPlaceMatch (the bronze-only shape, mp-yuy8 phase 3) and so
// missed an identical defect one step over: a stored bracket with non-empty
// Rounds but no third-place match, whose draw also cannot be re-derived at
// export time, used to fall through and render an Elimination Matches sheet
// with NO content at all -- a silently-partial workbook, no bronze involved.
//
// A nil bracket, or one with Rounds == [][]BracketMatch{} (parseBracketFile's
// never-nil result for a missing bracket.json, state/bracket.go), correctly
// reads as no content: a competition mid-pools with nothing drawn yet must
// still export past step 4. A Rounds slice whose inner slices are all empty
// reads the same way, for the same reason -- an empty round carries no match
// this sheet would need to show either.
func bracketHasKnockoutContent(bracket *state.Bracket) bool {
	if bracket == nil {
		return false
	}
	if bracket.ThirdPlaceMatch != nil {
		return true
	}
	for _, round := range bracket.Rounds {
		if len(round) > 0 {
			return true
		}
	}
	return false
}
