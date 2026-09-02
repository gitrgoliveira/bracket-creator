package engine

import (
	"bytes"
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func (e *Engine) ExportCompetitionXlsx(id string) ([]byte, error) {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", id)
	}

	// Swiss has no pools and no static bracket (results are per-round pairings
	// and a running standings table), so there is nothing to render into the
	// pool/tree layout this function produces. Block it explicitly, before any
	// rendering work, rather than emitting a workbook whose sheets are
	// structurally present but hold no participant data. Matches the guard in
	// internal/export.BuildResultsWorkbook. A dedicated Swiss sheet is tracked
	// as follow-up work (bc-swex).
	if comp.Format == state.CompFormatSwiss {
		return nil, ErrSwissExportUnsupported
	}

	pools, err := e.store.LoadPools(id)
	if err != nil {
		return nil, err
	}

	// Where each pool is ACTUALLY being fought. Best-effort for the same reason
	// the bracket load below is: a competition with no pool matches on disk
	// simply bands by the drawn allocation, which is what this did before.
	var courtOfPool map[string]string
	if poolMatches, poolErr := e.store.LoadPoolMatches(id); poolErr == nil {
		courtOfPool = PoolCourtByName(poolMatches)
	}

	// The tournament, loaded ONCE and strictly (mp-yuy8 criterion 6): both the
	// shiaijo list below and the Tags sheet's publicURL near the end of this
	// function read from this single load, so the two can never disagree and a
	// corrupt tournament.md aborts the export instead of silently printing
	// positional court labels on one sheet. A MISSING tournament.md is not an
	// error -- LoadTournament returns (nil, nil) for that (state/tournament.go)
	// -- so a competition with no tournament record yet still exports.
	tourn, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}

	// The shiaijo BY NAME, for every sheet that prints one. The count is read
	// off the same list rather than derived a second time, so the two can never
	// disagree; CompetitionCourts owns the inheritance and the single-court fallback.
	courts := CompetitionCourts(comp, tourn)
	numCourts := len(courts)

	f, err := excel.NewFileFromScratch()
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	// 1. Data sheet (Player Name, Dojo, Display Name)
	poolCoords, playerCoords := helper.AddPoolDataToSheet(f, pools, comp.EffectiveWithZekkenName(), comp.Name)

	// 2. Pool Draw sheet (reactive formula references to data sheet)
	if err := helper.AddPoolsToSheet(f, pools, poolCoords, playerCoords); err != nil {
		return nil, err
	}

	// 3. Pool Matches sheet (red/white, scoring formulas, reactive name references).
	//    numCourts is the operator's allocation; PrintPoolMatches bands the sheet
	//    on the shiaijo count the pool phase actually runs on, clamping it itself.
	//    MatchWinnerRanksNeeded (not EffectivePoolWinners() directly): under
	//    bc-qual larger-pools, an oversized pool's crossed 2nd needs a
	//    matchWinners["<pool>-2nd"] entry too, or the Tree/Elimination sheets
	//    print it as inert literal text (or a broken CONCATENATE formula on
	//    the Elimination Matches sheet) instead of a live link to the pool's
	//    actual result.
	matchWinners, _ := helper.PrintPoolMatches(f, pools, comp.TeamSize, comp.MatchWinnerRanksNeeded(), courts, courtOfPool, comp.Mirror, poolCoords, playerCoords, comp.Engi)

	// 4. Tree sheets: one visual bracket page per subtree, rendered exactly like
	//    the CLI (cmd/create-pools.go) and the results workbook
	//    (internal/export/builder.go). NewFileFromScratch creates a single styled
	//    "Tree" template; copy it into each page so every page keeps the bracket
	//    layout and column widths, render that page's leaves, then delete the
	//    consumed template.
	//
	//    This used to render subtree 0 only and leave "Tree 2"+ as empty sheets,
	//    silently dropping those entrants' half of the draw. It was not a
	//    large-draw edge case: TreePageLayout raises the page count to
	//    NextPow2(numCourts), so every competition on 2 or more courts hit it.
	// Load the stored bracket ONCE, unconditionally, strictly (mp-yuy8 criterion
	// 4). It used to load only for naginata/pure-playoffs and otherwise fall
	// through best-effort on a load error, silently continuing with a nil
	// bracket -- but the bracket carries the LIVE court of every bout (the
	// operator reassigns matches between shiaijo as the day runs), which is the
	// only correct source for the elimination sheet's bands; a nil bracket bands
	// by the draw's regions instead, i.e. prints score sheets under the wrong
	// court rather than failing. A MISSING bracket.json is not an error --
	// parseBracketFile returns an empty non-nil bracket for a not-yet-drawn
	// competition (state/bracket.go) -- so league/swiss/mixed competitions that
	// never had a bracket are unaffected; only a corrupt/unreadable file fails.
	bracket, err := e.store.LoadBracket(id)
	if err != nil {
		return nil, err
	}
	// hasBronze keeps its EXISTING narrow gate (naginata or pure playoffs) even
	// though the load above is now unconditional: the bracket builder only ever
	// populates ThirdPlaceMatch when comp.Naginata is true
	// (buildBracketFromDraw, internal/engine/bracket.go), which this gate's
	// first disjunct already covers regardless of format, so widening the LOAD
	// does not widen what fires the bronze-only Elimination sheet fallback
	// below. Not a decision to revisit here -- see mp-yuy8 criterion 5.
	hasBronze := (comp.Naginata || isPurePlayoffs(comp, pools)) && bracket != nil && bracket.ThirdPlaceMatch != nil

	// Elimination leaves for the knockout phase, shared with the results workbook
	// (EliminationDraw) so both exports of one competition render the identical
	// bracket: pool winners for pooled formats, or the stored bracket's leaves for
	// a pure playoffs competition (mp-ndfu, mp-0yd8). The IsPlayoffEnabled gate
	// below then drops the phantom bracket a league's placeholder finals imply.
	draw := EliminationDraw(e.store, comp, pools, bracket, numCourts)
	if draw != nil && comp.IsPlayoffEnabled() {
		// 4b. Tree pages plus the Elimination Matches sheet, in the one mandatory
		//     order RenderKnockoutPages enforces. This path used to skip the
		//     Elimination blocks and junction numbering entirely, shipping a
		//     workbook (and a "full-bracket" PDF) with an entirely blank
		//     Elimination Matches sheet and unnumbered tree pages. The bronze
		//     block wires its entrant slots to the semi-final losers via the
		//     real rounds and winners, exactly as the CLI and results workbook.
		// Band each bout by the shiaijo it is CURRENTLY on, read off the stored
		// bracket, falling back to the draw's regions where there is none. The
		// operator reassigns matches between courts while the competition runs,
		// and these sheets are what their shiaijo runs off. ONE plan for both:
		// the tree pages are the wall chart for the very bouts the elimination
		// sheet bands, so a workbook that resolved them separately could title a
		// page "Shiaijo D" and print its score sheets under "Shiaijo A".
		plan := LiveCourtPlan(draw, courts, bracket)
		eliminationMatchRounds, _, err := helper.RenderKnockoutPages(f, plan, false, pools, poolCoords, playerCoords, matchWinners)
		if err != nil {
			return nil, fmt.Errorf("export: %w", err)
		}
		helper.PrintEliminationWithBronze(f, matchWinners, eliminationMatchRounds, comp.TeamSize,
			plan, comp.Mirror, comp.Engi, hasBronze)
	} else if hasBronze {
		// Narrow fallback: a competition whose bracket has a third-place bout but
		// yields no elimination leaves at all (no pools, no first-round entrants
		// and no participants to seed — e.g. a bracket saved with an empty first
		// round). The bracket-leaf/participant fallback above already covers the
		// normal pure-playoffs case (mp-ndfu), so this only fires for that
		// degenerate shape. The bronze block is then the only content on the
		// sheet, rendered at court band 1, so numCourts=1 covers it exactly.
		// nil rounds derive zero semi numbers, leaving both entrant slots
		// hand-fillable.
		helper.PrintBronzeBlockWithPrintArea(f, 2, comp.TeamSize, comp.Mirror, comp.Engi, helper.CourtLabels(1), "", nil, nil)
		helper.SetSheetLayoutPortraitA4DownThenOver(f, helper.SheetEliminationMatches, 1)
	}
	// The bare "Tree" sheet is a layout scaffold, never output. Delete it whether
	// it was copied into pages above or left unused (a format with no knockout),
	// so no blank tree page ever reaches the workbook or the printed booklet.
	if err := f.DeleteSheet(helper.SheetTree); err != nil {
		return nil, fmt.Errorf("export: delete tree template sheet: %w", err)
	}

	// 5. Names to Print sheet, one per shiaijo. Clamps the allocation to the pool
	//    phase's own shiaijo count internally, as step 3 does.
	helper.CreateNamesWithPoolToPrint(f, pools, comp.EffectiveWithZekkenName(), courts, courtOfPool, playerCoords)

	// 6. Tags sheet, pass publicURL so numbered tags get an embedded QR code.
	// tourn (loaded once, strictly, above) may legitimately be nil for a
	// competition with no tournament record yet, which simply omits QR codes
	// without aborting the export. CreateTagsSheet errors (e.g. Excel write
	// failures) still propagate.
	var publicURL string
	if tourn != nil {
		publicURL = tourn.PublicURL
	}
	if err := helper.CreateTagsSheet(f, pools, publicURL); err != nil {
		return nil, err
	}

	// 7. Kachinuki Detail sheet (T195–T203, CHK037). Opt-in: only emitted
	//    when the competition runs the kachinuki team-match format AND has
	//    at least one match with bout data. The renderer is a no-op for
	//    empty input, so this is safe even when the format is fixed.
	kachinukiMatches, err := e.collectKachinukiMatches(id, comp)
	if err != nil {
		return nil, err
	}
	if err := helper.WriteKachinukiDetailSheet(f, kachinukiMatches); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
