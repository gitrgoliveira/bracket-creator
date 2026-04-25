package helper

import (
	"fmt"

	excelize "github.com/xuri/excelize/v2"
)

// TreeTitleRows is the number of rows reserved at the top of every tree sheet
// for the user to add a title. Content starts below this offset.
const TreeTitleRows = 3

func CreateTreeBracket(f *excelize.File, sheet string, col int, startRow int, size int) string {
	// fmt.Printf("CreateTreeBracket: start row: %d, size: %d\n", startRow, size)
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
	// f.SetCellStyle(sheet, topCell, topCell, getBorderStyleBottom(f))

	// bottom
	bottomCell := fmt.Sprintf("%s%d", colName, startRow+size)
	if err := f.SetCellStyle(sheet, bottomCell, bottomCell, borderBottomStyle); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

	return middleCell
}

func writeTreeValue(f *excelize.File, sheet string, col int, startRow int, value string, matchWinners map[string]MatchWinner) {
	// fmt.Printf("writeTreeValue: start row: %d\n", startRow)
	treeTextStyle := getTreeTextStyle(f)

	colName := mustColumnName(col + 1)
	cell := fmt.Sprintf("%s%d", colName, startRow)

	// Check if value is a pool reference and we have matchWinners
	if matchWinners != nil {
		if matchWinner, exists := matchWinners[value]; exists {
			// Create CONCATENATE formula like existing elimination matches
			formula := fmt.Sprintf(`CONCATENATE("%s ",'%s'!%s)`, value, matchWinner.sheetName, matchWinner.cell)
			if err := f.SetCellFormula(sheet, cell, formula); err != nil {
				fmt.Printf("Warning: failed to set cell formula: %v\n", err)
			}
			if err := f.SetCellStyle(sheet, cell, cell, treeTextStyle); err != nil {
				fmt.Printf("Warning: failed to set cell style: %v\n", err)
			}
			return
		}
	}

	// Fallback to existing static value logic
	if err := f.SetCellValue(sheet, cell, value); err != nil {
		fmt.Printf("Warning: failed to set cell value: %v\n", err)
	}
	// f.SetColWidth(sheet, colName, colName, 10)
	// f.MergeCell(sheet, cell, fmt.Sprintf("%s%d", colName, startRow+1))
	// f.SetCellStyle(sheet, cell, fmt.Sprintf("%s%d", colName, startRow+1), getPoolHeaderStyle(f))
	if err := f.SetCellStyle(sheet, cell, cell, treeTextStyle); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

}

func AddPoolsToTree(f *excelize.File, sheetName string, pools []Pool) {
	SetSheetLayoutPortraitA4Centered(f, sheetName)
	treeHeaderStyle := getTreeHeaderStyle(f)
	treeTopStyle := getTreeTopStyle(f)
	treeBodyStyle := getTreeBodyStyle(f)
	treeBottomStyle := getTreeBottomStyle(f)
	borderTopStyle := getBorderStyleTop(f)
	row := TreeTitleRows + 1

	for _, pool := range pools {
		if err := f.SetCellFormula(sheetName, fmt.Sprintf("A%d", row),
			fmt.Sprintf("%s!%s", pool.sheetName, pool.cell)); err != nil {
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
			if err := f.SetCellFormula(sheetName, fmt.Sprintf("A%d", row),
				fmt.Sprintf("\"%d. \" & %s!%s", player.PoolPosition, player.sheetName, player.cell)); err != nil {
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

}

func FillInMatches(f *excelize.File, eliminationMatchRounds [][]*Node) {
	var matchNum = 1
	for _, round := range eliminationMatchRounds {
		for _, match := range round {
			if match == nil || match.SheetName == "" {
				continue
			}
			match.matchNum = int64(matchNum)
			handleExcelError("SetCellInt", f.SetCellInt(match.SheetName, match.LeafVal, int64(matchNum)))
			matchNum++
		}
	}
}

// SetTreeSheetTitle writes a title formula into the first row of a tree sheet,
// spanning a wide range of columns to cover the bracket layout.
// The formula prepends the value of data!$B$1 (the user-supplied title prefix)
// to the given title string, so editing that single cell updates all tree sheets.
func SetTreeSheetTitle(f *excelize.File, sheetName string, title string) {
	titleStyle := getPoolHeaderStyle(f)
	startCell := "A1"
	endCell := "P1"
	formula := fmt.Sprintf(`IF(data!$B$1="","%s",data!$B$1&" - %s")`, title, title)
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, formula))
	handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, titleStyle))
}
