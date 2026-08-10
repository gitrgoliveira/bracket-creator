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

	pools, err := e.store.LoadPools(id)
	if err != nil {
		return nil, err
	}

	// Derived once for every court-count consumer, mirroring builder.go:
	// clamped to 1 so a competition saved without courts still lays out as a
	// single-court draw. (The court-band helpers also clamp internally, so
	// this is layout intent, not panic avoidance.)
	numCourts := len(comp.Courts)
	if numCourts < 1 {
		numCourts = 1
	}

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

	// 3. Pool Matches sheet (red/white, scoring formulas, reactive name references)
	matchWinners := helper.PrintPoolMatches(f, pools, comp.TeamSize, comp.EffectivePoolWinners(), numCourts, comp.Mirror, poolCoords, playerCoords, comp.Engi)

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
	// Load the stored bracket ONLY for the paths that actually consume it:
	// naginata (its bronze gate) and a pure playoffs competition (its elimination
	// leaves — mp-ndfu). Still skipped for league/swiss/mixed, whose export has
	// zero dependency on bracket.json, so a corrupted file can never abort an
	// export that never needed it. Bronze gates on the stored bracket's
	// ThirdPlaceMatch exactly as the results workbook does (builder.go), so the
	// two exports of one competition agree.
	hasBronze := false
	var bracket *state.Bracket
	if comp.Naginata || isPurePlayoffs(comp, pools) {
		bracket, err = e.store.LoadBracket(id)
		if err != nil {
			return nil, err
		}
		hasBronze = bracket != nil && bracket.ThirdPlaceMatch != nil
	}

	// Elimination leaves for the knockout phase, shared with the results workbook
	// (EliminationLeaves) so both exports of one competition render the identical
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
		eliminationMatchRounds, _, err := helper.RenderKnockoutPages(f, draw, false, pools, poolCoords, playerCoords, matchWinners)
		if err != nil {
			return nil, fmt.Errorf("export: %w", err)
		}
		helper.PrintEliminationWithBronze(f, matchWinners, eliminationMatchRounds, comp.TeamSize, numCourts, comp.Mirror, comp.Engi, hasBronze)
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
		helper.PrintBronzeBlockWithPrintArea(f, 2, comp.TeamSize, comp.Mirror, comp.Engi, 1, nil, nil)
		helper.SetSheetLayoutPortraitA4DownThenOver(f, helper.SheetEliminationMatches, 1)
	}
	// The bare "Tree" sheet is a layout scaffold, never output. Delete it whether
	// it was copied into pages above or left unused (a format with no knockout),
	// so no blank tree page ever reaches the workbook or the printed booklet.
	if err := f.DeleteSheet(helper.SheetTree); err != nil {
		return nil, fmt.Errorf("export: delete tree template sheet: %w", err)
	}

	// 5. Names to Print sheet
	helper.CreateNamesWithPoolToPrint(f, pools, comp.EffectiveWithZekkenName(), numCourts, playerCoords)

	// 6. Tags sheet, pass publicURL so numbered tags get an embedded QR code.
	// LoadTournament errors are silently ignored: a missing publicURL simply
	// omits QR codes without aborting the export. CreateTagsSheet errors
	// (e.g. Excel write failures) still propagate.
	var publicURL string
	if t, tErr := e.store.LoadTournament(); tErr == nil && t != nil {
		publicURL = t.PublicURL
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
