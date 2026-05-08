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
		return nil, fmt.Errorf("competition %s not found", id)
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
	helper.AddPoolDataToSheet(f, pools, comp.WithZekkenName, comp.Name)

	// 2. Pool Draw sheet (reactive formula references to data sheet)
	if err := helper.AddPoolsToSheet(f, pools); err != nil {
		return nil, err
	}

	// 3. Pool Matches sheet (red/white, scoring formulas, reactive name references)
	matchWinners := helper.PrintPoolMatches(f, pools, comp.TeamSize, comp.PoolWinners, len(comp.Courts), comp.Mirror)

	// 4. Tree sheets
	finals := helper.GenerateFinals(pools, comp.PoolWinners)
	if len(finals) > 0 {
		maxPlayersPerTree := helper.MaxPlayersPerTree
		numPages, _ := helper.RoundToPowerOf2(float64(len(finals)), float64(maxPlayersPerTree))
		if numPages < 1 {
			numPages = 1
		}
		if courtPages := helper.NextPow2(len(comp.Courts)); courtPages > numPages {
			numPages = courtPages
		}

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

	// 5. Names to Print sheet
	helper.CreateNamesWithPoolToPrint(f, pools, comp.WithZekkenName, len(comp.Courts))

	// 6. Tags sheet
	if err := helper.CreateTagsSheet(f, pools); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
