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
	if err := f.SetCellStyle(sheet, startCell, endCell, GetBorderStyleLeft(f)); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

	// middle
	middleCell := fmt.Sprintf("%s%d", colName, startRow+size/2)
	if err := f.SetCellStyle(sheet, middleCell, middleCell, GetBorderStyleBottomLeft(f)); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

	// Top cell
	colName, _ = excelize.ColumnNumberToName(col)
	topCell := fmt.Sprintf("%s%d", colName, startRow)
	if err := f.SetCellStyle(sheet, topCell, topCell, getBorderStyleTop(f)); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}
	// f.SetCellStyle(sheet, topCell, topCell, getBorderStyleBottom(f))

	// bottom
	bottomCell := fmt.Sprintf("%s%d", colName, startRow+size)
	if err := f.SetCellStyle(sheet, bottomCell, bottomCell, getBorderStyleBottom(f)); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

	return middleCell
}

func writeTreeValue(f *excelize.File, sheet string, col int, startRow int, value string) {
	// fmt.Printf("writeTreeValue: start row: %d\n", startRow)

	colName, _ := excelize.ColumnNumberToName(col + 1)
	cell := fmt.Sprintf("%s%d", colName, startRow)
	if err := f.SetCellValue(sheet, cell, value); err != nil {
		fmt.Printf("Warning: failed to set cell value: %v\n", err)
	}
	// f.SetColWidth(sheet, colName, colName, 10)
	// f.MergeCell(sheet, cell, fmt.Sprintf("%s%d", colName, startRow+1))
	// f.SetCellStyle(sheet, cell, fmt.Sprintf("%s%d", colName, startRow+1), getPoolHeaderStyle(f))
	if err := f.SetCellStyle(sheet, cell, cell, getTreeTextStyle(f)); err != nil {
		fmt.Printf("Warning: failed to set cell style: %v\n", err)
	}

}

func AddPoolsToTree(f *excelize.File, sheetName string, pools []Pool) {

	row := 2

	for _, pool := range pools {
		if err := f.SetCellFormula(sheetName, fmt.Sprintf("A%d", row),
			fmt.Sprintf("%s!%s", pool.sheetName, pool.cell)); err != nil {
			fmt.Printf("Warning: failed to set cell formula: %v\n", err)
		}

		if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), getTreeHeaderStyle(f)); err != nil {
			fmt.Printf("Warning: failed to set cell style: %v\n", err)
		}

		row++
		if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), getTreeTopStyle(f)); err != nil {
			fmt.Printf("Warning: failed to set cell style: %v\n", err)
		}

		for _, player := range pool.Players {
			if err := f.SetCellFormula(sheetName, fmt.Sprintf("A%d", row),
				fmt.Sprintf("%s!%s", player.sheetName, player.cell)); err != nil {
				fmt.Printf("Warning: failed to set cell formula: %v\n", err)
			}
			row++

			if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row), getTreeBodyStyle(f)); err != nil {
				fmt.Printf("Warning: failed to set cell style: %v\n", err)
			}
		}

		if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row-1), fmt.Sprintf("A%d", row-1), getTreeBottomStyle(f)); err != nil {
			fmt.Printf("Warning: failed to set cell style: %v\n", err)
		}

		if err := f.SetCellStyle(sheetName, fmt.Sprintf("A%d", row), fmt.Sprintf("A%d", row),
			getBorderStyleTop(f)); err != nil {
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
