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
	if err := helper.AddPoolsToSheet(f, pools, poolCoords, playerCoords, comp.Engi); err != nil {
		return nil, err
	}

	// 3. Pool Matches sheet (red/white, scoring formulas, reactive name references)
	matchWinners := helper.PrintPoolMatches(f, pools, comp.TeamSize, comp.PoolWinners, len(comp.Courts), comp.Mirror, poolCoords, playerCoords, comp.Engi)

	// 4. Tree sheets
	finals := helper.GenerateFinals(pools, comp.PoolWinners)
	if len(finals) > 0 {
		numPages, _ := helper.TreePageLayout(len(finals), len(comp.Courts), false)

		tree := helper.CreateBalancedTree(finals)
		subtrees := helper.SubdivideTree(tree, numPages)

		for i, subtree := range subtrees {
			sheetName := helper.SheetTree
			if i > 0 {
				sheetName = fmt.Sprintf("Tree %d", i+1)
				if _, err := f.NewSheet(sheetName); err != nil {
					return nil, err
				}
			}
			// Simplified: just use the first Tree sheet for now
			if i == 0 {
				depth := helper.CalculateDepth(subtree)
				helper.PrintLeafNodes(subtree, f, sheetName, 2*depth, 1, depth, true, matchWinners)
				helper.SetTreeSheetTitle(f, sheetName, comp.Name)
			}
		}
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
			bronzeEndRow := helper.PrintThirdPlaceBlock(f, 1, 2, comp.TeamSize, comp.Mirror, comp.Engi, 0, 0, nil)
			helper.SetEliminationPrintArea(f, helper.SheetEliminationMatches, 1, bronzeEndRow-1)
			helper.SetSheetLayoutPortraitA4DownThenOver(f, helper.SheetEliminationMatches, 1)
		}
	}

	// 5. Names to Print sheet
	helper.CreateNamesWithPoolToPrint(f, pools, comp.EffectiveWithZekkenName(), len(comp.Courts), playerCoords, comp.Engi)

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
