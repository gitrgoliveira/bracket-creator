package engine

import (
	"bytes"
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
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
	matchWinners := helper.PrintPoolMatches(f, pools, comp.TeamSize, comp.PoolWinners, len(comp.Courts), comp.Mirror, poolCoords, playerCoords, comp.Engi)

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
	//
	//    numCourts is clamped to 1: SubtreeCourtIndex divides by the court count,
	//    so a competition saved without courts would panic here.
	numCourts := len(comp.Courts)
	if numCourts < 1 {
		numCourts = 1
	}
	// GenerateFinals returns placeholder "Pool A-1st" labels for ANY pooled
	// format, including ones with no knockout phase, so gate on the format the
	// way the results workbook does (builder.go, TestBuildResultsWorkbook_
	// LeagueNoPhantomBracket); otherwise a league template grows a phantom
	// bracket implying a knockout that will never be played.
	finals := helper.GenerateFinals(pools, comp.PoolWinners)
	if len(finals) > 0 && comp.IsPlayoffEnabled() {
		numPages, perr := helper.TreePageLayout(len(finals), numCourts, false)
		if perr != nil {
			return nil, fmt.Errorf("export: compute tree page layout: %w", perr)
		}

		tree := helper.CreateBalancedTree(finals)
		subtrees := helper.SubdivideTree(tree, numPages)

		treeTemplateIdx, terr := f.GetSheetIndex(helper.SheetTree)
		if terr != nil {
			return nil, fmt.Errorf("export: find tree template sheet: %w", terr)
		}
		// GetSheetIndex returns (-1, nil) for an absent sheet, so guard the index
		// too rather than letting CopySheet fail with a misleading error source.
		if treeTemplateIdx < 0 {
			return nil, fmt.Errorf("export: tree template sheet %q not found", helper.SheetTree)
		}

		for i, subtree := range subtrees {
			pageSheet := fmt.Sprintf("Tree %d", i+1)
			pageIdx, nerr := f.NewSheet(pageSheet)
			if nerr != nil {
				return nil, fmt.Errorf("export: create tree sheet %s: %w", pageSheet, nerr)
			}
			if cerr := f.CopySheet(treeTemplateIdx, pageIdx); cerr != nil {
				return nil, fmt.Errorf("export: copy tree template to %s: %w", pageSheet, cerr)
			}
			depth := helper.CalculateDepth(subtree)
			// Leaves start below the reserved title band; row 1 would be written
			// over by the merged A1:P1 title.
			helper.PrintLeafNodes(subtree, f, pageSheet, 2*depth, helper.TreeTitleRows+1, depth, true, matchWinners)
			// Title each page by its shiaijo. The title formula already prepends
			// data!$B$1 (the competition name), so passing comp.Name here would
			// render "Name - Name".
			courtLabel := helper.CourtLabel(helper.SubtreeCourtIndex(len(subtrees), numCourts, i))
			helper.SetTreeSheetTitle(f, pageSheet, "Shiaijo "+courtLabel)
			if len(pools) > 0 {
				poolStart, poolEnd := helper.PoolBoundsForSubtree(len(pools), numCourts, len(subtrees), i)
				helper.AddPoolsToTree(f, pageSheet, pools[poolStart:poolEnd], poolCoords, playerCoords)
			}
		}
	}
	// The bare "Tree" sheet is a layout scaffold, never output. Delete it whether
	// it was copied into pages above or left unused (a format with no knockout),
	// so no blank tree page ever reaches the workbook or the printed booklet.
	if err := f.DeleteSheet(helper.SheetTree); err != nil {
		return nil, fmt.Errorf("export: delete tree template sheet: %w", err)
	}

	// 4b. Naginata: add a "3rd Place" slot on the Elimination Matches sheet so
	// the operator can hand-score the bronze bout on the blank template.
	// This path renders Tree sheets via PrintLeafNodes and does not call
	// PrintTeamEliminationMatches, so no "M N" matchWinners entries exist.
	// Pass zero semi numbers so the entrant slots remain hand-fillable.
	if comp.Naginata {
		b, bErr := e.store.LoadBracket(id)
		if bErr != nil {
			return nil, bErr
		}
		if b != nil && b.ThirdPlaceMatch != nil {
			// The bronze block is the ONLY content on this sheet on the blank
			// path (see comment above), and it is rendered at court band 1
			// (courtStartCol=1). numCourts=1 therefore covers all content
			// exactly; the competition's court count would only widen the
			// print area with empty columns.
			bronzeEndRow := helper.PrintThirdPlaceBlock(f, 1, 2, comp.TeamSize, comp.Mirror, comp.Engi, 0, 0, nil)
			helper.SetEliminationPrintArea(f, helper.SheetEliminationMatches, 1, bronzeEndRow-1)
			helper.SetSheetLayoutPortraitA4DownThenOver(f, helper.SheetEliminationMatches, 1)
		}
	}

	// 5. Names to Print sheet
	helper.CreateNamesWithPoolToPrint(f, pools, comp.EffectiveWithZekkenName(), len(comp.Courts), playerCoords)

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
