package helper

import (
	"fmt"

	excelize "github.com/xuri/excelize/v2"
)

// TreeTitleRows is the number of rows reserved at the top of every tree sheet
// for the user to add a title. Content starts below this offset.
const TreeTitleRows = 3

// TreePageLastCol returns the last spreadsheet column (1-based) a rendered tree
// page occupies for a bracket of the given depth. PrintLeafNodes places the root
// at column 2*depth and CreateTreeBracket draws that node's vertical line one
// column further right; every other column of the bracket is to the left of it.
func TreePageLastCol(depth int) int {
	return 2*depth + 1
}

// TreePageLastRow returns the last row a bracket of the given depth occupies
// when its root is rendered from startRow. PrintLeafNodes offsets each right
// subtree by the current level's size (2^(level-1)), so the deepest cell sits
// sum(2^(depth-1)..2^0) = 2^depth - 1 rows below the start.
func TreePageLastRow(depth, startRow int) int {
	if depth < 1 {
		return startRow
	}
	return startRow + 1<<uint(depth) - 1
}

// SetTreePageLayout bounds a rendered tree page to the region actually drawn and
// scales it onto a single page wide (a deep bracket shrinks to fit rather than
// breaking mid-bracket onto a second sheet of paper).
//
// Without a print area a tree sheet printed its whole used range: the title band
// alone is merged across many columns, and the template pre-sizes columns out to
// Z, so LibreOffice emitted a second, near-blank physical page holding nothing
// but the tail of the title border. Every printed booklet carried one of those
// per bracket page.
//
// lastRow must cover both the bracket (TreePageLastRow) and the pool roster
// block (the row AddPoolsToTree returns), whichever reaches further down.
func SetTreePageLayout(f *excelize.File, sheetName string, depth, lastRow int) {
	SetPrintArea(f, sheetName, TreePageLastCol(depth), lastRow)
	SetSheetLayoutPortraitA4(f, sheetName)
}

// RenderTreePages renders one visual bracket page per subtree: it copies the
// styled SheetTree template into a numbered "Tree N" sheet, titles the page by
// its shiaijo, renders the subtree's leaves, overlays that court's pool rosters
// (when pools are provided), and bounds the page's print area to the drawn
// region. This is the single implementation behind the CLI (create-pools /
// create-playoffs), the blank-template export (engine), and the results
// workbook (export) - the loop used to be copied at each call site, and a
// geometry fix in one had to be replicated by hand into the others.
//
// Passing pools drives the per-page roster overlay. It drives no reordering:
// placement is decided when the draw is BUILT (helper.BuildKnockoutDraw), so by
// the time a page is drawn its leaves are already final. Callers with no pool
// phase (create-playoffs) pass nil and get no overlay.
//
// subtrees must be SubdivideRegions' output, i.e. exactly numCourts x {1,2,4}
// pages in court order. That exact multiple is what makes the page title and
// the roster overlay agree with the bracket printed underneath them: page
// c*pagesPerCourt+i is shiaijo c's region (or a child of it), and both
// SubtreeCourtIndex and PoolBoundsForSubtree divide by the same exact
// pagesPerCourt.
//
// The consumed SheetTree template is NOT deleted here: callers that skip
// rendering entirely (a league has no knockout) must still delete it, so
// ownership of the deletion stays with them.
func RenderTreePages(f *excelize.File, subtrees []*Node, numCourts int, pools []Pool, poolCoords map[string]cellCoord, playerCoords map[string]playerCellCoord, matchWinners map[string]MatchWinner) error {
	hasPools := len(pools) > 0
	templateIdx, err := f.GetSheetIndex(SheetTree)
	if err != nil {
		return fmt.Errorf("find tree template sheet: %w", err)
	}
	// GetSheetIndex returns (-1, nil) for an absent sheet, so guard the index
	// too rather than letting CopySheet fail with a misleading error source.
	if templateIdx < 0 {
		return fmt.Errorf("tree template sheet %q not found", SheetTree)
	}

	for i, subtree := range subtrees {
		pageSheet := fmt.Sprintf("Tree %d", i+1)
		pageIdx, err := f.NewSheet(pageSheet)
		if err != nil {
			return fmt.Errorf("create tree sheet %s: %w", pageSheet, err)
		}
		if err := f.CopySheet(templateIdx, pageIdx); err != nil {
			return fmt.Errorf("copy tree template to %s: %w", pageSheet, err)
		}

		depth := CalculateDepth(subtree)
		startRow := TreeTitleRows + 1
		// The title formula prepends data!$B$1 (the user-supplied title prefix),
		// so the page title itself is just the shiaijo label.
		SetTreeSheetTitle(f, pageSheet, TreePageTitle(len(subtrees), numCourts, i), TreePageLastCol(depth))
		PrintLeafNodes(subtree, f, pageSheet, 2*depth, startRow, depth, matchWinners)

		lastRow := TreePageLastRow(depth, startRow)
		if hasPools {
			poolStart, poolEnd := PoolBoundsForSubtree(len(pools), numCourts, len(subtrees), i)
			// The page's own shiaijo block, narrowed to the pools with a
			// qualifier actually printed on this page, so a roster overlay can
			// never describe competitors the page does not carry.
			overlay := PageRosterPools(pools[poolStart:poolEnd], subtree)
			lastRow = max(lastRow, AddPoolsToTree(f, pageSheet, overlay, poolCoords, playerCoords))
		}
		SetTreePageLayout(f, pageSheet, depth, lastRow)
	}
	return nil
}

// RenderKnockoutPages paginates a knockout draw by shiaijo, renders one bracket
// page per page-subtree via RenderTreePages, and numbers the bracket junctions,
// returning the per-round match nodes (earliest round first, final last) and the
// page count.
//
// It is the single funnel for every workbook generator (cmd/create-pools,
// cmd/create-playoffs, internal/export/builder, internal/engine/export), which
// is what lets these invariants be enforced here once instead of at four call
// sites:
//
//   - Pages are shiaijo regions. draw.Regions comes from the court-first build,
//     so a page is a genuine subtree of exactly one court's region and the page
//     title, the roster overlay and the bracket printed on it all name the same
//     shiaijo (R3/R8). There is no placement pass any more: the draw arrives
//     already placed, which is why RenderTreePages and PrintLeafNodes never
//     touch the tree.
//   - Render before numbering. Rendering stamps each internal node's sheet/cell
//     coordinates and FillInMatches writes the match numbers into them; with a
//     FillInMatches-first order every write is silently skipped and the pages
//     carry no numbers.
//
// singleTree forces the whole bracket onto one page (the CLI --single-tree
// flag). Deleting the consumed SheetTree template stays with the caller (see
// RenderTreePages).
func RenderKnockoutPages(f *excelize.File, draw *KnockoutDraw, singleTree bool, pools []Pool, poolCoords map[string]cellCoord, playerCoords map[string]playerCellCoord, matchWinners map[string]MatchWinner) ([][]*Node, int, error) {
	if draw == nil || draw.Root == nil {
		return nil, 0, fmt.Errorf("render knockout pages: empty draw")
	}
	numCourts := draw.NumCourts()
	var subtrees []*Node
	if singleTree {
		subtrees = []*Node{draw.Root}
	} else {
		subtrees = SubdivideRegions(draw.Regions, KnockoutPagesPerCourt(draw.Regions))
	}
	if err := RenderTreePages(f, subtrees, numCourts, pools, poolCoords, playerCoords, matchWinners); err != nil {
		return nil, 0, err
	}
	rounds := BuildEliminationMatchRounds(draw.Root)
	FillInMatches(f, rounds)
	return rounds, len(subtrees), nil
}

func CreateTreeBracket(f *excelize.File, sheet string, col int, startRow int, size int) string {
	borderLeftStyle := GetBorderStyleLeft(f)
	borderBottomLeftStyle := GetBorderStyleBottomLeft(f)
	borderTopStyle := getBorderStyleTop(f)
	borderBottomStyle := getBorderStyleBottom(f)

	// interval
	colName := mustColumnName(col + 1)

	startCell := fmt.Sprintf("%s%d", colName, startRow)
	endCell := fmt.Sprintf("%s%d", colName, startRow+size)
	if err := f.SetCellStyle(sheet, startCell, endCell, borderLeftStyle); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

	// middle
	middleCell := fmt.Sprintf("%s%d", colName, startRow+size/2)
	if err := f.SetCellStyle(sheet, middleCell, middleCell, borderBottomLeftStyle); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

	// Top cell
	colName = mustColumnName(col)
	topCell := fmt.Sprintf("%s%d", colName, startRow)
	if err := f.SetCellStyle(sheet, topCell, topCell, borderTopStyle); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

	// bottom
	bottomCell := fmt.Sprintf("%s%d", colName, startRow+size)
	if err := f.SetCellStyle(sheet, bottomCell, bottomCell, borderBottomStyle); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

	return middleCell
}

// TreeLabelCol is the column every bottom-level leaf label lands in:
// PrintLeafNodes decrements its column by 2 per level down to col=2, and
// writeTreeValue writes at col+1 = 3 = "C". Exported because the template
// (internal/excel/template.go setupTreeSheet) derives its column widths from
// it — the label column must be wide so labels fit.
const TreeLabelCol = 3

func writeTreeValue(f *excelize.File, sheet string, col int, startRow int, value string, matchWinners map[string]MatchWinner) {
	treeTextStyle := getTreeTextStyle(f)

	colName := mustColumnName(col + 1)
	cell := fmt.Sprintf("%s%d", colName, startRow)

	// Bye leaves sit at shallower levels, so their value cell is one of the
	// narrow bracket columns (E, G, ...) right of the label column. Style the
	// whole span from column C so every leaf gets the same full-width
	// underline+fill, not a stub under the label's last few characters. The
	// spanned cells are always empty at a leaf's row (each leaf is alone in
	// its band), so the right-aligned text still overflows across them.
	styleStart := cell
	if col+1 > TreeLabelCol {
		styleStart = fmt.Sprintf("%s%d", mustColumnName(TreeLabelCol), startRow)
	}

	// Check if value is a pool reference and we have matchWinners
	if matchWinners != nil {
		if matchWinner, exists := matchWinners[value]; exists {
			// Create CONCATENATE formula like existing elimination matches
			formula := fmt.Sprintf(`CONCATENATE("%s ",'%s'!%s)`, value, matchWinner.sheetName, matchWinner.cell)
			if err := f.SetCellFormula(sheet, cell, formula); err != nil {
				fmt.Printf("Warning: failed to set cell formula: %v\n", err)
			}
			if err := f.SetCellStyle(sheet, styleStart, cell, treeTextStyle); err != nil {
				fmt.Printf("Warning: failed to set cell style: %v\n", err)
			}
			return
		}
	}

	// Fallback to existing static value logic
	if err := f.SetCellValue(sheet, cell, value); err != nil {
		fmt.Printf("Warning: failed to set cell value: %v\n", err)
	}

	if err := f.SetCellStyle(sheet, styleStart, cell, treeTextStyle); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

}

// AddPoolsToTree renders the roster of each pool feeding this tree page down
// column A and returns the last row it wrote to, which the caller needs to bound
// the page's print area (SetTreePageLayout). Page layout is the caller's job:
// this function only writes content.
func AddPoolsToTree(f *excelize.File, sheetName string, pools []Pool, poolCoords map[string]cellCoord, pCoords map[string]playerCellCoord) int {
	treeHeaderStyle := getTreeHeaderStyle(f)
	treeTopStyle := getTreeTopStyle(f)
	treeBodyStyle := getTreeBodyStyle(f)
	treeBottomStyle := getTreeBottomStyle(f)
	borderTopStyle := getBorderStyleTop(f)
	row := TreeTitleRows + 1

	for _, pool := range pools {
		pc := poolCoords[pool.PoolName]
		if err := f.SetCellFormula(sheetName, fmt.Sprintf("A%d", row),
			sheetRef(pc.sheetName, pc.cell)); err != nil {
			fmt.Printf("Warning: failed to set cell formula: %v\n", err)
		}

		if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), treeHeaderStyle); err != nil {
			fmt.Printf("Warning: failed to set cell style: %v\n", err)
		}

		row++
		if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), treeTopStyle); err != nil {
			fmt.Printf("Warning: failed to set cell style: %v\n", err)
		}

		for _, player := range pool.Players {
			coord := pCoords[playerCoordKey(player)]
			var formula string
			if coord.numberCell != "" {
				formula = playerRef(player.Name, coord)
			} else {
				formula = fmt.Sprintf("\"%d. \" & %s!%s", player.PoolPosition, coord.sheetName, coord.cell)
			}
			if err := f.SetCellFormula(sheetName, fmt.Sprintf("A%d", row), formula); err != nil {
				fmt.Printf("Warning: failed to set cell formula: %v\n", err)
			}
			row++

			if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), treeBodyStyle); err != nil {
				fmt.Printf("Warning: failed to set cell style: %v\n", err)
			}
		}

		if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row-1), fmt.Sprintf("A%d", row-1), treeBottomStyle); err != nil {
			fmt.Printf("Warning: failed to set cell style: %v\n", err)
		}

		if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row),
			borderTopStyle); err != nil {
			fmt.Printf("Warning: failed to set cell style: %v\n", err)
		}

		row++

	}

	// row is one past the last styled cell; report the last row actually used.
	return row - 1
}

// AssignMatchNumbers assigns sequential match numbers to all non-nil nodes in
// eliminationMatchRounds, in the same iteration order as the Excel FillInMatches
// output (earliest/widest round first, within-round left-to-right). Each non-nil
// node's matchNum field is set in-place.
//
// This is the authoritative numbering for the printed Excel Tree sheet. The web
// API has a SEPARATE implementation, engine.assignBracketMatchNumbers, which
// operates on *state.Bracket (a different type) instead of []*Node, the two are
// NOT a literally-shared function. They are kept equal-by-contract: skip the same
// positions (a nil node here == a Hidden-or-both-sides-empty match there) and
// iterate the same round order, so the Nth real match gets the same number on both
// paths. That contract is verified by TestMatchNumberingParity_ExcelVsWeb in
// internal/engine, which builds both numberings from identical entrant sets
// (including bye-producing, non-power-of-two sizes) and asserts the sequences match
// position-for-position, and by TestExcelWorkbookMatchesEngineBracket_Mixed and
// _Playoffs, which cover the pool-fed and standalone draws by reading the numbers
// back off a RENDERED workbook.
// The printed Excel sheet is authoritative; if they ever diverge, the web path must
// be corrected to match this one (it already had to be: the walk here numbers each
// effective round left to right across the whole tree, and a pool-fed draw puts
// matches from two pow2 rounds in one effective round).
func AssignMatchNumbers(eliminationMatchRounds [][]*Node) {
	var matchNum int64 = 1
	for _, round := range eliminationMatchRounds {
		for _, match := range round {
			if match == nil {
				continue
			}
			match.matchNum = matchNum
			matchNum++
		}
	}
}

func FillInMatches(f *excelize.File, eliminationMatchRounds [][]*Node) {
	AssignMatchNumbers(eliminationMatchRounds)
	for _, round := range eliminationMatchRounds {
		for _, match := range round {
			if match == nil {
				continue
			}
			if match.SheetName != "" {
				handleExcelError("SetCellInt", f.SetCellInt(match.SheetName, match.LeafVal, match.matchNum))
			}
		}
	}
}

// SetTreeSheetTitle writes a title formula into the first row of a tree sheet,
// merged across the page's content columns (endCol, from TreePageLastCol).
// The formula prepends the value of data!$B$1 (the user-supplied title prefix)
// to the given title string, so editing that single cell updates all tree sheets.
//
// endCol must match the rendered bracket: the merge is the widest thing on the
// sheet, so a title merged past the last bracket column would push the page's
// used range beyond the print area and print a near-blank extra page.
func SetTreeSheetTitle(f *excelize.File, sheetName string, title string, endCol int) {
	titleStyle := getPoolHeaderStyle(f)
	startCell := "A1"
	endCell := mustColumnName(endCol) + "1"
	formula := fmt.Sprintf(`IF(data!$B$1="","%s",data!$B$1&" - %s")`, title, title)
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, formula))
	handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, titleStyle))
}
