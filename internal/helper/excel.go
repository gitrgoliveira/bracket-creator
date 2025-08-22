package helper

import (
	"fmt"
	"strconv"
	"strings"

	excelize "github.com/xuri/excelize/v2"
)

func PrintPoolMatches(f *excelize.File, pools []Pool, teamMatches int, numWinners int) map[string]MatchWinner {

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

		if err := f.SetCellStyle(sheetName, startCell, endCell, getPoolHeaderStyle(f)); err != nil {
			fmt.Println("Error setting cell style:", err)
		}
		if err := f.MergeCell(sheetName, startCell, endCell); err != nil {
			fmt.Println("Error merging cells:", err)
		}
		if err := f.SetCellFormula(sheetName, startCell, fmt.Sprintf("%s!%s", pool.sheetName, pool.cell)); err != nil {
			fmt.Println("Error setting cell formula:", err)
		}

		poolRow++
		if teamMatches == 0 {
			MatchHeader(f, sheetName, startColName, poolRow, middleColName, endColName)
			poolRow++
		}

		for _, match := range pool.Matches {
			startCell = startColName + fmt.Sprint(poolRow)
			endCell = endColName + fmt.Sprint(poolRow)
			if err := f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f)); err != nil {
				fmt.Println("Error setting cell style:", err)
			}

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
				err := f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f))
				if err != nil {
					fmt.Println("Error setting cell style:", err)
				}
				if err := f.SetCellInt(sheetName, startCell, int64(i+1)); err != nil {
					fmt.Println("Error setting cell int:", err)
				}
				if err := f.SetCellInt(sheetName, endCell, int64(i+1)); err != nil {
					fmt.Println("Error setting cell int:", err)
				}
			}

			if teamMatches > 0 {
				// pool results summary
				poolRow += 2
				poolEntry(startColName, poolRow, endColName, f, sheetName,
					fmt.Sprintf("%s!%s", match.SideA.sheetName, match.SideA.cell),
					fmt.Sprintf("%s!%s", match.SideB.sheetName, match.SideB.cell))

				if err := f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftVictoriesColName, poolRow), "V"); err != nil {
					fmt.Println("Error setting cell value:", err)
				}
				if err := f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftPointsColName, poolRow), "P"); err != nil {
					fmt.Println("Error setting cell value:", err)
				}
				if err := f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightVictoriesColName, poolRow), "V"); err != nil {
					fmt.Println("Error setting cell value:", err)
				}
				if err := f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightPointsColName, poolRow), "P"); err != nil {
					fmt.Println("Error setting cell value:", err)
				}
				poolRow++

				startCell = startColName + fmt.Sprint(poolRow)
				endCell = endColName + fmt.Sprint(poolRow)
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f)))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, "Victories / Points"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, endCell, "Victories / Points"))
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
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, poolRow), fmt.Sprintf("%d. ", result)))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), getBorderStyleBottom(f)))

			if result <= numWinners {
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
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, leftSide))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, endCell, rightSide))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f)))
}

func MatchHeader(f *excelize.File, sheetName string, startColName string, poolRow int, middleColName string, endColName string) {
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", startColName, poolRow), "Red"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, poolRow), fmt.Sprintf("%s%d", startColName, poolRow), getRedHeaderStyle(f)))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, startColName, startColName, 34))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", middleColName, poolRow), "vs"))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, middleColName, middleColName, 3))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", middleColName, poolRow), fmt.Sprintf("%s%d", middleColName, poolRow), getTextStyle(f)))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), "White"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), getWhiteHeaderStyle(f)))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, endColName, endColName, 34))
}

func PrintTeamEliminationMatches(f *excelize.File, poolMatchWinners map[string]MatchWinner, eliminationMatchRounds [][]*Node, numTeamMatches int) {
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

			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, getPoolHeaderStyle(f)))
			handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, fmt.Sprintf("Match %d", eliminationMatch.matchNum)))

			matchRow++
			MatchHeader(f, sheetName, startColName, matchRow, middleColName, endColName)
			matchRow++

			//////////////////////////////////////
			// eliminationMatch.Left checks if it is a pool winner
			startCell = startColName + fmt.Sprint(matchRow)
			var leftCellValue, rightCellValue string

			_, _, err := excelize.SplitCellName(eliminationMatch.Left.LeafVal)
			if err != nil && len(eliminationMatch.Left.LeafVal) > 0 {
				if strings.Contains(eliminationMatch.Left.LeafVal, "Pool") {
					leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", eliminationMatch.Left.LeafVal, poolMatchWinners[eliminationMatch.Left.LeafVal].sheetName, poolMatchWinners[eliminationMatch.Left.LeafVal].cell)
				} else {
					leftCellValue = fmt.Sprintf("'%s'!%s", poolMatchWinners[eliminationMatch.Left.LeafVal].sheetName, poolMatchWinners[eliminationMatch.Left.LeafVal].cell)
				}
			} else {
				winnerFromMatch := fmt.Sprintf("M %d", eliminationMatch.Left.matchNum)
				leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", winnerFromMatch, matchWinners[winnerFromMatch].sheetName, matchWinners[winnerFromMatch].cell)
			}
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, leftCellValue))

			//////////////////////////////////////
			// eliminationMatch.Right checks if it is a pool winner
			endCell = endColName + fmt.Sprint(matchRow)
			_, _, err = excelize.SplitCellName(eliminationMatch.Right.LeafVal)
			if err != nil && len(eliminationMatch.Right.LeafVal) > 0 {
				if strings.Contains(eliminationMatch.Right.LeafVal, "Pool") {
					rightCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", eliminationMatch.Right.LeafVal, poolMatchWinners[eliminationMatch.Right.LeafVal].sheetName, poolMatchWinners[eliminationMatch.Right.LeafVal].cell)
				} else {
					rightCellValue = fmt.Sprintf("'%s'!%s", poolMatchWinners[eliminationMatch.Right.LeafVal].sheetName, poolMatchWinners[eliminationMatch.Right.LeafVal].cell)
				}
			} else {
				winnerFromMatch := fmt.Sprintf("M %d", eliminationMatch.Right.matchNum)
				rightCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", winnerFromMatch, matchWinners[winnerFromMatch].sheetName, matchWinners[winnerFromMatch].cell)

			}
			// fmt.Println(rightCellValue)
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, endCell, rightCellValue))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f)))

			// adding the individual matches
			for i := 0; i < numTeamMatches; i++ {
				matchRow++
				startCell = startColName + fmt.Sprint(matchRow)
				endCell = endColName + fmt.Sprint(matchRow)
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f)))
				handleExcelError("SetCellInt", f.SetCellInt(sheetName, startCell, int64(i+1)))
				handleExcelError("SetCellInt", f.SetCellInt(sheetName, endCell, int64(i+1)))
			}

			if numTeamMatches > 0 {
				// pool results summary

				matchRow += 2
				// eliminationMatch.Left checks if it is a pool winner
				startCell = startColName + fmt.Sprint(matchRow)
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, leftCellValue))

				//////////////////////////////////////
				// eliminationMatch.Right checks if it is a pool winner
				endCell = endColName + fmt.Sprint(matchRow)
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, endCell, rightCellValue))
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f)))

				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftVictoriesColName, matchRow), "V"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftPointsColName, matchRow), "P"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightVictoriesColName, matchRow), "V"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightPointsColName, matchRow), "P"))
				matchRow++

				startCell = startColName + fmt.Sprint(matchRow)
				endCell = endColName + fmt.Sprint(matchRow)
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, getTextStyle(f)))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, "Victories / Points"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, endCell, "Victories / Points"))
			}

			matchRow += 2

			resultCol, _ := excelize.ColumnNumberToName(startCol + 5)
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "1."))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), getBorderStyleBottom(f)))

			// Gathering the match winners for the following rounds
			matchWinners[fmt.Sprintf("M %d", eliminationMatch.matchNum)] = MatchWinner{
				sheetName: sheetName,
				cell:      fmt.Sprintf("%s%d", endColName, matchRow),
			}

			matchRow++
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "2."))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), getBorderStyleBottom(f)))

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
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("Elimination Round %d", round)))
	handleExcelError("MergeCell", f.MergeCell(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("O%d", startRow)))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("O%d", startRow), getPoolHeaderStyle(f)))
}

func CreateNamesToPrint(f *excelize.File, players []Player, sanitized bool) {
	sheetName := "Names to Print"

	row := 1
	for _, player := range players {
		positionCell := fmt.Sprintf("A%d", row)
		nameCell := fmt.Sprintf("B%d", row)
		handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 110))

		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), player.PoolPosition))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, positionCell, fmt.Sprintf("A%d", row+1), getNameIDSideStyle(f)))

		if sanitized {
			_, row, err := excelize.SplitCellName(player.cell)
			if err != nil {
				handleExcelError("SplitCellName", err)
				// Fallback to original approach if SplitCellName fails
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, "D"+player.cell[1:])))
			} else {
				rowStr := strconv.Itoa(row)
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, "D"+rowStr)))
			}
		} else {
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, player.cell)))
		}
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, nameCell, fmt.Sprintf("B%d", row+1), getNameIDStyle(f)))
		handleExcelError("MergeCell", f.MergeCell(sheetName, nameCell, fmt.Sprintf("B%d", row+1)))

		row += 2
	}
}

func CreateNamesWithPoolToPrint(f *excelize.File, pools []Pool, sanitized bool) {
	sheetName := "Names to Print"

	row := 1
	for _, pool := range pools {

		for _, player := range pool.Players {
			poolCell := fmt.Sprintf("A%d", row)
			nameCell := fmt.Sprintf("B%d", row)
			handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 110))

			handleExcelError("SetCellValue", f.SetCellValue(sheetName, poolCell, pool.PoolName))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, poolCell, fmt.Sprintf("A%d", row+1), getNameIDSideStyle(f)))

			if sanitized {
				_, rowNum, err := excelize.SplitCellName(player.cell)
				if err != nil {
					handleExcelError("SplitCellName", err)
					// Fallback to original approach if SplitCellName fails
					handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, "D"+player.cell[1:])))
				} else {
					rowStr := strconv.Itoa(rowNum)
					handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, "D"+rowStr)))
				}
			} else {
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, player.cell)))
			}
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, nameCell, fmt.Sprintf("B%d", row+1), getNameIDStyle(f)))
			handleExcelError("MergeCell", f.MergeCell(sheetName, nameCell, fmt.Sprintf("B%d", row+1)))

			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("A%d", row+1), player.PoolPosition))
			row += 2
		}
	}
}

func FillEstimations(f *excelize.File, numPools int64, numPoolMatches int64, extraPools int64, teamSize int64, numEliminationMatches int64) {
	sheetName := "Time Estimator"

	if teamSize == 0 {
		teamSize = 1
	}

	// Number of pools
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "A2", numPools))
	// Team size
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "B2", teamSize))

	// Matches per pool
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "C2", numPoolMatches))

	// Number of Elimination Matches
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "A8", numEliminationMatches))
	// Team size
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "B8", teamSize))

}

// handleExcelError is a helper function to handle errors from Excel operations
func handleExcelError(operation string, err error) {
	if err != nil {
		fmt.Printf("Error in Excel operation %s: %v\n", operation, err)
	}
}
