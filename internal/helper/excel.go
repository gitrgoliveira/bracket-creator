package helper

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

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

func PrintPoolMatches(f *excelize.File, pools []Pool, teamMatches int) map[string]MatchWinner {

	matchWinners := make(map[string]MatchWinner)
	sheetName := "Pool Matches"

	leftRowStack := RowStack{}
	rightRowStack := RowStack{}

	startRow := 4
	var poolRow int

	spaceLines := 3
	var startCol int

	maxNumMatches := 0
	leftRowStack.Push(startRow)
	rightRowStack.Push(startRow)
	for i, pool := range pools {
		numMatches := len(pool.Matches)
		if numMatches > maxNumMatches {
			maxNumMatches = numMatches
		}

		startCol = 1
		if i%2 != 0 {
			startCol = 9
			poolRow = rightRowStack.Pop()
		} else {
			poolRow = leftRowStack.Pop()
		}
		startColName, _ := excelize.ColumnNumberToName(startCol)
		leftVictoriesColName, _ := excelize.ColumnNumberToName(startCol + 1)
		leftPointsColName, _ := excelize.ColumnNumberToName(startCol + 2)
		middleColName, _ := excelize.ColumnNumberToName(startCol + 3)
		rightPointsColName, _ := excelize.ColumnNumberToName(startCol + 4)
		rightVictoriesColName, _ := excelize.ColumnNumberToName(startCol + 5)
		endColName, _ := excelize.ColumnNumberToName(startCol + 6)
		startCell := startColName + fmt.Sprint(poolRow)
		endCell := endColName + fmt.Sprint(poolRow)

		f.SetCellStyle(sheetName, startCell, endCell, getPoolHeaderStyle(f))
		f.MergeCell(sheetName, startCell, endCell)
		f.SetCellFormula(sheetName, startCell, fmt.Sprintf("%s!%s", pool.sheetName, pool.cell))

		poolRow++
		if teamMatches == 0 {
			MatchHeader(f, sheetName, startColName, poolRow, middleColName, endColName)
			poolRow++
		}

		for _, match := range pool.Matches {
			startCell = startColName + fmt.Sprint(poolRow)
			endCell = endColName + fmt.Sprint(poolRow)
			f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))

			if teamMatches > 0 {
				MatchHeader(f, sheetName, startColName, poolRow, middleColName, endColName)
				poolRow++
			}

			poolEntry(startColName, poolRow, endColName, f, sheetName,
				fmt.Sprintf("%s!%s", match.SideA.sheetName, match.SideA.cell),
				fmt.Sprintf("%s!%s", match.SideB.sheetName, match.SideB.cell))

			for i := 0; i < teamMatches; i++ {
				poolRow++
				startCell = startColName + fmt.Sprint(poolRow)
				endCell = endColName + fmt.Sprint(poolRow)
				f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))
				f.SetCellInt(sheetName, startCell, i+1)
				f.SetCellInt(sheetName, endCell, i+1)
			}

			if teamMatches > 0 {
				// pool results summary
				poolRow += 2
				poolEntry(startColName, poolRow, endColName, f, sheetName,
					fmt.Sprintf("%s!%s", match.SideA.sheetName, match.SideA.cell),
					fmt.Sprintf("%s!%s", match.SideB.sheetName, match.SideB.cell))

				f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftVictoriesColName, poolRow), "V")
				f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftPointsColName, poolRow), "P")
				f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightVictoriesColName, poolRow), "V")
				f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightPointsColName, poolRow), "P")
				poolRow++

				startCell = startColName + fmt.Sprint(poolRow)
				endCell = endColName + fmt.Sprint(poolRow)
				f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))
				f.SetCellValue(sheetName, startCell, "Victories / Points")
				f.SetCellValue(sheetName, endCell, "Victories / Points")
				poolRow += spaceLines
			}

			poolRow++
		}
		if teamMatches > 0 {
			poolRow -= spaceLines //removing previously added spaces
		}

		for result := 1; result <= len(pool.Players); result++ {
			poolRow++
			resultCol, _ := excelize.ColumnNumberToName(startCol + 5)
			f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, poolRow), fmt.Sprintf("%d. ", result))
			f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), getBorderStyleBottom(f))

			if result <= 2 {
				matchWinners[fmt.Sprintf("%s.%d", pool.PoolName, result)] = MatchWinner{
					sheetName: sheetName,
					cell:      fmt.Sprintf("%s%d", endColName, poolRow),
				}
			}
		}

		poolRow += spaceLines

		if i%2 == 0 {
			leftRowStack.PushHighest(poolRow, rightRowStack.Peek())
		} else {
			rightRowStack.PushHighest(poolRow, leftRowStack.Peek())
		}
	}
	return matchWinners
}

func poolEntry(startColName string, poolRow int, endColName string, f *excelize.File, sheetName string, leftSide string, rightSide string) {
	startCell := startColName + fmt.Sprint(poolRow)
	endCell := endColName + fmt.Sprint(poolRow)
	f.SetCellFormula(sheetName, startCell, leftSide)
	f.SetCellFormula(sheetName, endCell, rightSide)
	f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))
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
	var startCol int

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

			_, _, err := excelize.SplitCellName(eliminationMatch.Left)
			if err != nil {
				if strings.Contains(eliminationMatch.Left, "Pool") {
					leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", eliminationMatch.Left, poolMatchWinners[eliminationMatch.Left].sheetName, poolMatchWinners[eliminationMatch.Left].cell)
				} else {
					leftCellValue = fmt.Sprintf("'%s'!%s", poolMatchWinners[eliminationMatch.Left].sheetName, poolMatchWinners[eliminationMatch.Left].cell)
				}

			} else {
				winnerFromMatch := fmt.Sprintf("Match %d", matchMapping[eliminationMatch.Left])
				leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", winnerFromMatch, matchWinners[winnerFromMatch].sheetName, matchWinners[winnerFromMatch].cell)
			}
			f.SetCellFormula(sheetName, startCell, leftCellValue)

			//////////////////////////////////////
			// eliminationMatch.Right checks if it is a pool winner
			endCell = endColName + fmt.Sprint(matchRow)
			_, _, err = excelize.SplitCellName(eliminationMatch.Right)
			if err != nil {
				if strings.Contains(eliminationMatch.Right, "Pool") {
					rightCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", eliminationMatch.Right, poolMatchWinners[eliminationMatch.Right].sheetName, poolMatchWinners[eliminationMatch.Right].cell)
				} else {
					rightCellValue = fmt.Sprintf("'%s'!%s", poolMatchWinners[eliminationMatch.Right].sheetName, poolMatchWinners[eliminationMatch.Right].cell)
				}
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

func PrintTeamEliminationMatches(f *excelize.File, poolMatchWinners map[string]MatchWinner, matchMapping map[string]int, eliminationMatchRounds [][]EliminationMatch, numTeamMatches int) {
	sheetName := "Elimination Matches"
	matchWinners := make(map[string]MatchWinner)

	startRow := 1
	var matchRow int
	spaceLines := 5
	var startCol int

	leftRowStack := RowStack{}
	rightRowStack := RowStack{}

	// first round first
	for round, eliminationMatchRound := range eliminationMatchRounds {
		round++

		addRoundHeader(f, sheetName, startRow, round)
		startRow += 2

		leftRowStack.Push(startRow)
		rightRowStack.Push(startRow)
		for i, eliminationMatch := range eliminationMatchRound {

			startCol = 1
			if i%2 != 0 {
				startCol = 9
				matchRow = rightRowStack.Pop()
			} else {
				matchRow = leftRowStack.Pop()
			}

			startColName, _ := excelize.ColumnNumberToName(startCol)
			leftVictoriesColName, _ := excelize.ColumnNumberToName(startCol + 1)
			leftPointsColName, _ := excelize.ColumnNumberToName(startCol + 2)
			middleColName, _ := excelize.ColumnNumberToName(startCol + 3)
			rightPointsColName, _ := excelize.ColumnNumberToName(startCol + 4)
			rightVictoriesColName, _ := excelize.ColumnNumberToName(startCol + 5)
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

			_, _, err := excelize.SplitCellName(eliminationMatch.Left)
			if err != nil {
				if strings.Contains(eliminationMatch.Left, "Pool") {
					leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", eliminationMatch.Left, poolMatchWinners[eliminationMatch.Left].sheetName, poolMatchWinners[eliminationMatch.Left].cell)
				} else {
					leftCellValue = fmt.Sprintf("'%s'!%s", poolMatchWinners[eliminationMatch.Left].sheetName, poolMatchWinners[eliminationMatch.Left].cell)
				}
			} else {
				winnerFromMatch := fmt.Sprintf("Match %d", matchMapping[eliminationMatch.Left])
				leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", winnerFromMatch, matchWinners[winnerFromMatch].sheetName, matchWinners[winnerFromMatch].cell)
			}
			f.SetCellFormula(sheetName, startCell, leftCellValue)

			//////////////////////////////////////
			// eliminationMatch.Right checks if it is a pool winner
			endCell = endColName + fmt.Sprint(matchRow)
			_, _, err = excelize.SplitCellName(eliminationMatch.Right)
			if err != nil {
				if strings.Contains(eliminationMatch.Right, "Pool") {
					rightCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", eliminationMatch.Right, poolMatchWinners[eliminationMatch.Right].sheetName, poolMatchWinners[eliminationMatch.Right].cell)
				} else {
					rightCellValue = fmt.Sprintf("'%s'!%s", poolMatchWinners[eliminationMatch.Right].sheetName, poolMatchWinners[eliminationMatch.Right].cell)
				}
			} else {
				winnerFromMatch := fmt.Sprintf("Match %d", matchMapping[eliminationMatch.Right])
				rightCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", winnerFromMatch, matchWinners[winnerFromMatch].sheetName, matchWinners[winnerFromMatch].cell)
			}
			f.SetCellFormula(sheetName, endCell, rightCellValue)
			f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))

			// adding the individual matches
			for i := 0; i < numTeamMatches; i++ {
				matchRow++
				startCell = startColName + fmt.Sprint(matchRow)
				endCell = endColName + fmt.Sprint(matchRow)
				f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))
				f.SetCellInt(sheetName, startCell, i+1)
				f.SetCellInt(sheetName, endCell, i+1)
			}

			if numTeamMatches > 0 {
				// pool results summary

				matchRow += 2
				// eliminationMatch.Left checks if it is a pool winner
				startCell = startColName + fmt.Sprint(matchRow)
				f.SetCellFormula(sheetName, startCell, leftCellValue)

				//////////////////////////////////////
				// eliminationMatch.Right checks if it is a pool winner
				endCell = endColName + fmt.Sprint(matchRow)
				f.SetCellFormula(sheetName, endCell, rightCellValue)
				f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))

				f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftVictoriesColName, matchRow), "V")
				f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftPointsColName, matchRow), "P")
				f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightVictoriesColName, matchRow), "V")
				f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightPointsColName, matchRow), "P")
				matchRow++

				startCell = startColName + fmt.Sprint(matchRow)
				endCell = endColName + fmt.Sprint(matchRow)
				f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))
				f.SetCellValue(sheetName, startCell, "Victories / Points")
				f.SetCellValue(sheetName, endCell, "Victories / Points")
			}

			matchRow += 2

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

			matchRow += 3
			if i%2 != 0 {
				rightRowStack.Push(matchRow)
			} else {
				leftRowStack.Push(matchRow)
			}
		}

		startRow = matchRow + spaceLines
	}
}

func addRoundHeader(f *excelize.File, sheetName string, startRow int, round int) {
	f.SetCellValue(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("Elimination Round %d", round))
	f.MergeCell(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("O%d", startRow))
	f.SetCellStyle(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("O%d", startRow), getPoolHeaderStyle(f))
}

func CreateNamesToPrint(f *excelize.File, players []Player, sanatized bool) {
	sheetName := "Names to Print"

	row := 1
	for _, player := range players {
		positionCell := fmt.Sprintf("A%d", row)
		nameCell := fmt.Sprintf("B%d", row)
		f.SetRowHeight(sheetName, row, 110)

		f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), player.PoolPosition)
		f.SetCellStyle(sheetName, positionCell, fmt.Sprintf("A%d", row+1), getNameIDSideStyle(f))

		if sanatized {
			f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, "D"+player.cell[1:]))
		} else {
			f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, player.cell))
		}
		f.SetCellStyle(sheetName, nameCell, fmt.Sprintf("B%d", row+1), getNameIDStyle(f))
		f.MergeCell(sheetName, nameCell, fmt.Sprintf("B%d", row+1))

		row += 2
	}

}
func CreateNamesWithPoolToPrint(f *excelize.File, pools []Pool, sanatized bool) {
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

			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row+1), player.PoolPosition)
			row += 2
		}
	}
}
