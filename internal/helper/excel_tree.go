package helper

import (
	"fmt"

	excelize "github.com/xuri/excelize/v2"
)

func CreateTreeBracket(f *excelize.File, sheet string, col int, startRow int, size int) string {
	// fmt.Printf("CreateTreeBracket: start row: %d, size: %d\n", startRow, size)

	// interval
	colName, _ := excelize.ColumnNumberToName(col + 1)

	startCell := fmt.Sprintf("%s%d", colName, startRow)
	endCell := fmt.Sprintf("%s%d", colName, startRow+size)
	f.SetCellStyle(sheet, startCell, endCell, GetBorderStyleLeft(f))

	// middle
	middleCell := fmt.Sprintf("%s%d", colName, startRow+size/2)
	f.SetCellStyle(sheet, middleCell, middleCell, GetBorderStyleBottomLeft(f))

	// Top cell
	colName, _ = excelize.ColumnNumberToName(col)
	topCell := fmt.Sprintf("%s%d", colName, startRow)
	f.SetCellStyle(sheet, topCell, topCell, getBorderStyleTop(f))
	// f.SetCellStyle(sheet, topCell, topCell, getBorderStyleBottom(f))

	// bottom
	bottomCell := fmt.Sprintf("%s%d", colName, startRow+size)
	f.SetCellStyle(sheet, bottomCell, bottomCell, getBorderStyleBottom(f))

	return middleCell
}

func writeTreeValue(f *excelize.File, sheet string, col int, startRow int, value string) {
	// fmt.Printf("writeTreeValue: start row: %d\n", startRow)

	colName, _ := excelize.ColumnNumberToName(col + 1)
	cell := fmt.Sprintf("%s%d", colName, startRow)
	f.SetCellValue(sheet, cell, value)
	// f.SetColWidth(sheet, colName, colName, 10)
	// f.MergeCell(sheet, cell, fmt.Sprintf("%s%d", colName, startRow+1))
	// f.SetCellStyle(sheet, cell, fmt.Sprintf("%s%d", colName, startRow+1), getPoolHeaderStyle(f))
	f.SetCellStyle(sheet, cell, cell, getTreeTextStyle(f))

}

func AddPoolsToTree(f *excelize.File, sheetName string, pools []Pool) {

	row := 2

	for _, pool := range pools {
		f.SetCellFormula(sheetName, fmt.Sprintf("A%d", row),
			fmt.Sprintf("%s!%s", pool.sheetName, pool.cell))

		f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), getTreeHeaderStyle(f))

		row++
		f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), getTreeTopStyle(f))

		for _, player := range pool.Players {
			f.SetCellFormula(sheetName, fmt.Sprintf("A%d", row),
				fmt.Sprintf("%s!%s", player.sheetName, player.cell))
			row++

			f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), getTreeBodyStyle(f))
		}

		f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row-1), fmt.Sprintf("A%d", row-1), getTreeBottomStyle(f))

		f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row),
			getBorderStyleTop(f))

		row++

	}

}

func FillInMatches(f *excelize.File, eliminationMatchRounds [][]*Node) {
	var matchNum int64 = 1
	for _, round := range eliminationMatchRounds {
		for _, match := range round {
			if match == nil {
				continue
			}
			match.matchNum = matchNum
			f.SetCellInt(match.SheetName, match.LeafVal, matchNum)
			matchNum++
		}
	}
}
