package helper

import (
	"fmt"
	"math"
	"strings"

	"github.com/xuri/excelize/v2"
)

func CreateTreeBracket(f *excelize.File, sheet string, col int, startRow int, size int, firstRound bool, value string) string {

	if firstRound {
		colName, _ := excelize.ColumnNumberToName(col + 1)
		cell := fmt.Sprintf("%s%d", colName, startRow)
		f.SetCellValue(sheet, cell, value)
		f.SetColWidth(sheet, colName, colName, 10)
		return ""
	}

	// interval
	colName, _ := excelize.ColumnNumberToName(col + 1)
	f.SetColWidth(sheet, colName, colName, 5)

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
	f.SetColWidth(sheet, colName, colName, 5)

	// bottom
	bottomCell := fmt.Sprintf("%s%d", colName, startRow+size)
	f.SetCellStyle(sheet, bottomCell, bottomCell, getBorderStyleBottom(f))

	return middleCell
}

func AddDataToSheet(f *excelize.File, pools []Pool, sanatize bool) {
	sheetName := "data"

	// Set the header row
	f.SetCellValue(sheetName, "A1", "Pool")
	f.SetCellValue(sheetName, "B1", "Player Name")
	f.SetCellValue(sheetName, "C1", "Player Dojo")
	if sanatize {
		f.SetCellValue(sheetName, "D1", "Display Name")
	}

	// Populate the groups in the spreadsheet
	row := 2

	for i := 0; i < len(pools); i++ {
		pools[i].sheetName = sheetName

		for j := range pools[i].Players {
			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), pools[i].PoolName)
			f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), pools[i].Players[j].Name)
			f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), pools[i].Players[j].Dojo)
			if sanatize {
				f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), pools[i].Players[j].DisplayName)
			}
			pools[i].cell = fmt.Sprintf("A%d", row)
			pools[i].Players[j].sheetName = sheetName
			pools[i].Players[j].cell = fmt.Sprintf("B%d", row)
			row++
		}
	}

	fmt.Printf("Data added to spreadsheet\n")

	// Set the column widths
	f.SetColWidth(sheetName, "A", "A", 15)
	f.SetColWidth(sheetName, "B", "C", 30)

}

func AddPoolsToSheet(f *excelize.File, pools []Pool) error {
	// Set the starting row and column for the bracket
	sheetName := "Pool Draw"
	numPoolsPerColumn := int(math.Ceil(float64(len(pools)) / 3))

	startRow := 7
	startCol := 2

	col_name, _ := excelize.ColumnNumberToName(startCol)
	cell := col_name + fmt.Sprint(startRow)
	headerCellStyle, _ := f.GetCellStyle(sheetName, cell)
	cell = col_name + fmt.Sprint(startRow+1)
	contentCellStyle, _ := f.GetCellStyle(sheetName, cell)

	row := startRow
	column := startCol
	// Write the bracket data to Excel
	for i, pool := range pools {
		// groupNumber := pools[i].cell
		col_name, _ = excelize.ColumnNumberToName(column)
		cell = col_name + fmt.Sprint(row)

		f.SetCellFormula(sheetName, cell, fmt.Sprintf("%s!%s", pool.sheetName, pool.cell))

		f.SetCellStyle(sheetName, cell, cell, headerCellStyle)
		row++
		for _, player := range pool.Players {
			cell := col_name + fmt.Sprint(row)
			f.SetCellFormula(sheetName, cell, fmt.Sprintf("%s!%s", player.sheetName, player.cell))
			f.SetCellStyle(sheetName, cell, cell, contentCellStyle)
			row++
		}

		row += 2

		if (i+1)%numPoolsPerColumn == 0 {
			column += 2
			row = startRow
		}

		f.SetColWidth(sheetName, col_name, col_name, 30)
	}

	fmt.Printf("Pools added to spreadsheet\n")

	return nil
}

func AddPoolsToTree(f *excelize.File, sheetName string, pools []Pool) {

	row := 4

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

func FillInMatches(f *excelize.File, matches []string) map[string]int {
	// This orders all the cell values, so we start from the first cell in the first column
	matches = OrderStringsAlphabetically(matches)

	mapping := make(map[string]int)

	for i := 0; i < len(matches); i++ {
		f.SetCellValue("Tree", matches[i], fmt.Sprint(i+1))
		mapping[matches[i]] = i + 1
	}

	return mapping
}

// CreatePoolMatches writes the given pool matches to an Excel file.
//
// Parameters:
// - f: the excelize file to write the pool matches to.
// - poolMatches: the pool matches to write.
func PrintPoolMatches(f *excelize.File, pools []Pool) map[string]MatchWinner {

	matchWinners := make(map[string]MatchWinner)
	sheetName := "Pool Matches"

	startRow := 4
	spaceLines := 3
	startCol := 1

	maxNumMatches := 0
	for i, pool := range pools {
		numMatches := len(pool.Matches)
		if numMatches > maxNumMatches {
			maxNumMatches = numMatches
		}

		startCol = 1
		if i%2 != 0 {
			startCol = 9
		}
		poolRow := startRow

		startColName, _ := excelize.ColumnNumberToName(startCol)
		middleColName, _ := excelize.ColumnNumberToName(startCol + 3)
		endColName, _ := excelize.ColumnNumberToName(startCol + 6)
		startCell := startColName + fmt.Sprint(poolRow)
		endCell := endColName + fmt.Sprint(poolRow)

		f.SetCellStyle(sheetName, startCell, endCell, getPoolHeaderStyle(f))
		f.MergeCell(sheetName, startCell, endCell)
		f.SetCellFormula(sheetName, startCell, fmt.Sprintf("%s!%s", pool.sheetName, pool.cell))

		poolRow++
		MatchHeader(f, sheetName, startColName, poolRow, middleColName, endColName)

		poolRow++
		for _, match := range pool.Matches {
			startCell = startColName + fmt.Sprint(poolRow)
			endCell = endColName + fmt.Sprint(poolRow)

			f.SetCellFormula(sheetName, startCell, fmt.Sprintf("%s!%s", match.SideA.sheetName, match.SideA.cell))
			f.SetCellFormula(sheetName, endCell, fmt.Sprintf("%s!%s", match.SideB.sheetName, match.SideB.cell))
			f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))

			poolRow++
		}

		resultCol, _ := excelize.ColumnNumberToName(startCol + 5)
		f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, poolRow), "1.")
		f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), getBorderStyleBottom(f))
		matchWinners[fmt.Sprintf("%s.%d", pool.PoolName, 1)] = MatchWinner{
			sheetName: sheetName,
			cell:      fmt.Sprintf("%s%d", endColName, poolRow),
		}

		poolRow++
		f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, poolRow), "2.")
		f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), getBorderStyleBottom(f))
		matchWinners[fmt.Sprintf("%s.%d", pool.PoolName, 2)] = MatchWinner{
			sheetName: sheetName,
			cell:      fmt.Sprintf("%s%d", endColName, poolRow),
		}

		if i%2 != 0 {
			startRow += (maxNumMatches + spaceLines + len(pool.Players))
		}
	}

	return matchWinners
}

func MatchHeader(f *excelize.File, sheetName string, startColName string, poolRow int, middleColName string, endColName string) {
	f.SetCellValue(sheetName, fmt.Sprintf("%s%d", startColName, poolRow), "Red")
	f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, poolRow), fmt.Sprintf("%s%d", startColName, poolRow), getRedHeaderStyle(f))
	f.SetColWidth(sheetName, startColName, startColName, 34)

	f.SetCellValue(sheetName, fmt.Sprintf("%s%d", middleColName, poolRow), "vs")
	f.SetColWidth(sheetName, middleColName, middleColName, 3)
	f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", middleColName, poolRow), fmt.Sprintf("%s%d", middleColName, poolRow), getTextStyle(f))

	f.SetCellValue(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), "White")
	f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), getWhiteHeaderStyle(f))
	f.SetColWidth(sheetName, endColName, endColName, 34)
}

func PrintEliminationMatches(f *excelize.File, poolMatchWinners map[string]MatchWinner, matchMapping map[string]int, eliminationMatchRounds [][]EliminationMatch) {
	sheetName := "Elimination Matches"
	matchWinners := make(map[string]MatchWinner)

	startRow := 1
	spaceLines := 7
	startCol := 1

	// first round first
	for round, eliminationMatchRound := range eliminationMatchRounds {
		round++

		addRoundHeader(f, sheetName, startRow, round)
		startRow += 2

		for i, eliminationMatch := range eliminationMatchRound {

			startCol = 1
			if i%2 != 0 {
				startCol = 9
			}
			matchRow := startRow

			startColName, _ := excelize.ColumnNumberToName(startCol)
			middleColName, _ := excelize.ColumnNumberToName(startCol + 3)
			endColName, _ := excelize.ColumnNumberToName(startCol + 6)
			startCell := startColName + fmt.Sprint(matchRow)
			endCell := endColName + fmt.Sprint(matchRow)

			f.SetCellStyle(sheetName, startCell, endCell, getPoolHeaderStyle(f))
			f.MergeCell(sheetName, startCell, endCell)
			f.SetCellValue(sheetName, startCell, fmt.Sprintf("Match %d", eliminationMatch.Number))

			matchRow++
			MatchHeader(f, sheetName, startColName, matchRow, middleColName, endColName)
			matchRow++

			//////////////////////////////////////
			// eliminationMatch.Left checks if it is a pool winner
			startCell = startColName + fmt.Sprint(matchRow)
			var leftCellValue, rightCellValue string

			if strings.Contains(eliminationMatch.Left, "Pool") {
				leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", eliminationMatch.Left, poolMatchWinners[eliminationMatch.Left].sheetName, poolMatchWinners[eliminationMatch.Left].cell)
			} else {
				winnerFromMatch := fmt.Sprintf("Match %d", matchMapping[eliminationMatch.Left])
				leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", winnerFromMatch, matchWinners[winnerFromMatch].sheetName, matchWinners[winnerFromMatch].cell)
			}
			f.SetCellFormula(sheetName, startCell, leftCellValue)

			//////////////////////////////////////
			// eliminationMatch.Right checks if it is a pool winner
			endCell = endColName + fmt.Sprint(matchRow)
			if strings.Contains(eliminationMatch.Right, "Pool") {
				rightCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", eliminationMatch.Right, poolMatchWinners[eliminationMatch.Right].sheetName, poolMatchWinners[eliminationMatch.Right].cell)
			} else {
				winnerFromMatch := fmt.Sprintf("Match %d", matchMapping[eliminationMatch.Right])
				rightCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", winnerFromMatch, matchWinners[winnerFromMatch].sheetName, matchWinners[winnerFromMatch].cell)
			}
			f.SetCellFormula(sheetName, endCell, rightCellValue)
			f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))

			matchRow++

			resultCol, _ := excelize.ColumnNumberToName(startCol + 5)
			f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "1.")
			f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), getBorderStyleBottom(f))

			// Gathering the match winners for the following rounds
			matchWinners[fmt.Sprintf("Match %d", eliminationMatch.Number)] = MatchWinner{
				sheetName: sheetName,
				cell:      fmt.Sprintf("%s%d", endColName, matchRow),
			}

			matchRow++
			f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "2.")
			f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), getBorderStyleBottom(f))

			if i%2 != 0 {
				startRow += spaceLines
			}
		}

		startRow += 1

	}

}

func addRoundHeader(f *excelize.File, sheetName string, startRow int, round int) {
	f.SetCellValue(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("Elimination Round %d", round))
	f.MergeCell(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("O%d", startRow))
	f.SetCellStyle(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("O%d", startRow), getPoolHeaderStyle(f))
}

func CreateNamesToPrint(f *excelize.File, pools []Pool, sanatized bool) {
	sheetName := "Names to Print"

	row := 1
	for _, pool := range pools {

		for _, player := range pool.Players {
			poolCell := fmt.Sprintf("A%d", row)
			nameCell := fmt.Sprintf("B%d", row)
			f.SetRowHeight(sheetName, row, 110)

			f.SetCellValue(sheetName, poolCell, pool.PoolName)
			f.SetCellStyle(sheetName, poolCell, fmt.Sprintf("A%d", row+1), getNameIDSideStyle(f))

			if sanatized {
				f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, "D"+player.cell[1:]))
			} else {
				f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, player.cell))
			}
			f.SetCellStyle(sheetName, nameCell, fmt.Sprintf("B%d", row+1), getNameIDStyle(f))
			f.MergeCell(sheetName, nameCell, fmt.Sprintf("B%d", row+1))
			// f.SetCellValue(sheetName, fmt.Sprintf("A%d", row+1), i+1)

			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row+1), player.PoolPosition)
			row += 2
		}
	}
}
