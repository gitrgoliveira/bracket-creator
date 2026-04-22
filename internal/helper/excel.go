package helper

import (
	"fmt"
	"strconv"
	"strings"

	excelize "github.com/xuri/excelize/v2"
)

type matchColumnNames struct {
	startColName          string
	leftVictoriesColName  string
	leftPointsColName     string
	middleColName         string
	rightPointsColName    string
	rightVictoriesColName string
	endColName            string
}

func buildMatchColumnNames(startCol int) matchColumnNames {
	startColName, _ := excelize.ColumnNumberToName(startCol)
	leftVictoriesColName, _ := excelize.ColumnNumberToName(startCol + 1)
	leftPointsColName, _ := excelize.ColumnNumberToName(startCol + 2)
	middleColName, _ := excelize.ColumnNumberToName(startCol + 3)
	rightPointsColName, _ := excelize.ColumnNumberToName(startCol + 4)
	rightVictoriesColName, _ := excelize.ColumnNumberToName(startCol + 5)
	endColName, _ := excelize.ColumnNumberToName(startCol + 6)

	return matchColumnNames{
		startColName:          startColName,
		leftVictoriesColName:  leftVictoriesColName,
		leftPointsColName:     leftPointsColName,
		middleColName:         middleColName,
		rightPointsColName:    rightPointsColName,
		rightVictoriesColName: rightVictoriesColName,
		endColName:            endColName,
	}
}

func PrintPoolMatches(f *excelize.File, pools []Pool, teamMatches int, numWinners int) map[string]MatchWinner {

	matchWinners := make(map[string]MatchWinner)
	sheetName := "Pool Matches"
	configuredStartCols := make(map[int]bool)

	leftRowStack := RowStack{}
	rightRowStack := RowStack{}

	startRow := 4
	var poolRow int

	spaceLines := 3
	var startCol int
	colNamesByStartCol := make(map[int]matchColumnNames, 2)

	poolHeaderStyle := getPoolHeaderStyle(f)
	textStyle := getTextStyle(f)
	borderBottomStyle := getBorderStyleBottom(f)
	redHeaderStyle := getRedHeaderStyle(f)
	whiteHeaderStyle := getWhiteHeaderStyle(f)

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
		if !configuredStartCols[startCol] {
			setMatchColumnsWidthByStartCol(f, sheetName, startCol)
			configuredStartCols[startCol] = true
		}

		colNames, ok := colNamesByStartCol[startCol]
		if !ok {
			colNames = buildMatchColumnNames(startCol)
			colNamesByStartCol[startCol] = colNames
		}

		startColName := colNames.startColName
		leftVictoriesColName := colNames.leftVictoriesColName
		leftPointsColName := colNames.leftPointsColName
		middleColName := colNames.middleColName
		rightPointsColName := colNames.rightPointsColName
		rightVictoriesColName := colNames.rightVictoriesColName
		endColName := colNames.endColName
		startCell := startColName + fmt.Sprint(poolRow)
		endCell := endColName + fmt.Sprint(poolRow)

		if err := f.SetCellStyle(sheetName, startCell, endCell, poolHeaderStyle); err != nil {
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
			matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, redHeaderStyle, textStyle, whiteHeaderStyle)
			poolRow++
		}

		for _, match := range pool.Matches {
			startCell = startColName + fmt.Sprint(poolRow)
			endCell = endColName + fmt.Sprint(poolRow)
			if err := f.SetCellStyle(sheetName, startCell, endCell, textStyle); err != nil {
				fmt.Println("Error setting cell style:", err)
			}

			if teamMatches > 0 {
				matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, redHeaderStyle, textStyle, whiteHeaderStyle)
				poolRow++
			}

			poolEntryWithStyle(startColName, poolRow, endColName, f, sheetName,
				fmt.Sprintf("%s!%s", match.SideA.sheetName, match.SideA.cell),
				fmt.Sprintf("%s!%s", match.SideB.sheetName, match.SideB.cell),
				textStyle)

			for i := 0; i < teamMatches; i++ {
				poolRow++
				startCell = startColName + fmt.Sprint(poolRow)
				endCell = endColName + fmt.Sprint(poolRow)
				err := f.SetCellStyle(sheetName, startCell, endCell, textStyle)
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
				poolEntryWithStyle(startColName, poolRow, endColName, f, sheetName,
					fmt.Sprintf("%s!%s", match.SideA.sheetName, match.SideA.cell),
					fmt.Sprintf("%s!%s", match.SideB.sheetName, match.SideB.cell),
					textStyle)

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
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))
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
			resultCol := rightVictoriesColName
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, poolRow), fmt.Sprintf("%d. ", result)))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), borderBottomStyle))

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

func poolEntryWithStyle(startColName string, poolRow int, endColName string, f *excelize.File, sheetName string, leftSide string, rightSide string, textStyle int) {
	startCell := startColName + fmt.Sprint(poolRow)
	endCell := endColName + fmt.Sprint(poolRow)
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, leftSide))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, endCell, rightSide))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))
}

func MatchHeader(f *excelize.File, sheetName string, startColName string, poolRow int, middleColName string, endColName string) {
	matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, getRedHeaderStyle(f), getTextStyle(f), getWhiteHeaderStyle(f))
}

func matchHeaderWithStyles(f *excelize.File, sheetName string, startColName string, poolRow int, middleColName string, endColName string, redHeaderStyle int, textStyle int, whiteHeaderStyle int) {
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", startColName, poolRow), "Red"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, poolRow), fmt.Sprintf("%s%d", startColName, poolRow), redHeaderStyle))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", middleColName, poolRow), "vs"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", middleColName, poolRow), fmt.Sprintf("%s%d", middleColName, poolRow), textStyle))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), "White"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), whiteHeaderStyle))
}

func PrintTeamEliminationMatches(f *excelize.File, poolMatchWinners map[string]MatchWinner, eliminationMatchRounds [][]*Node, numTeamMatches int) {
	sheetName := "Elimination Matches"
	matchWinners := make(map[string]MatchWinner)
	configuredStartCols := make(map[int]bool)

	startRow := 1
	var matchRow int
	spaceLines := 5
	var startCol int
	colNamesByStartCol := make(map[int]matchColumnNames, 2)

	poolHeaderStyle := getPoolHeaderStyle(f)
	textStyle := getTextStyle(f)
	borderBottomStyle := getBorderStyleBottom(f)
	redHeaderStyle := getRedHeaderStyle(f)
	whiteHeaderStyle := getWhiteHeaderStyle(f)

	leftRowStack := RowStack{}
	rightRowStack := RowStack{}

	// first round first
	for round, eliminationMatchRound := range eliminationMatchRounds {
		round++

		addRoundHeaderWithStyle(f, sheetName, startRow, round, poolHeaderStyle)
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
			if !configuredStartCols[startCol] {
				setMatchColumnsWidthByStartCol(f, sheetName, startCol)
				configuredStartCols[startCol] = true
			}

			colNames, ok := colNamesByStartCol[startCol]
			if !ok {
				colNames = buildMatchColumnNames(startCol)
				colNamesByStartCol[startCol] = colNames
			}

			startColName := colNames.startColName
			leftVictoriesColName := colNames.leftVictoriesColName
			leftPointsColName := colNames.leftPointsColName
			middleColName := colNames.middleColName
			rightPointsColName := colNames.rightPointsColName
			rightVictoriesColName := colNames.rightVictoriesColName
			endColName := colNames.endColName
			startCell := startColName + fmt.Sprint(matchRow)
			endCell := endColName + fmt.Sprint(matchRow)

			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, poolHeaderStyle))
			handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, fmt.Sprintf("Match %d", eliminationMatch.matchNum)))

			matchRow++
			matchHeaderWithStyles(f, sheetName, startColName, matchRow, middleColName, endColName, redHeaderStyle, textStyle, whiteHeaderStyle)
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
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))

			// adding the individual matches
			for i := 0; i < numTeamMatches; i++ {
				matchRow++
				startCell = startColName + fmt.Sprint(matchRow)
				endCell = endColName + fmt.Sprint(matchRow)
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))
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
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))

				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftVictoriesColName, matchRow), "V"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", leftPointsColName, matchRow), "P"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightVictoriesColName, matchRow), "V"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rightPointsColName, matchRow), "P"))
				matchRow++

				startCell = startColName + fmt.Sprint(matchRow)
				endCell = endColName + fmt.Sprint(matchRow)
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, "Victories / Points"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, endCell, "Victories / Points"))
			}

			matchRow += 2

			resultCol := rightVictoriesColName
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "1."))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), borderBottomStyle))

			// Gathering the match winners for the following rounds
			matchWinners[fmt.Sprintf("M %d", eliminationMatch.matchNum)] = MatchWinner{
				sheetName: sheetName,
				cell:      fmt.Sprintf("%s%d", endColName, matchRow),
			}

			matchRow++
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "2."))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), borderBottomStyle))

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

func setMatchColumnsWidth(f *excelize.File, sheetName, startColName, middleColName, endColName string) {
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, startColName, startColName, 34))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, middleColName, middleColName, 3))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, endColName, endColName, 34))
}

func setMatchColumnsWidthByStartCol(f *excelize.File, sheetName string, startCol int) {
	startColName, _ := excelize.ColumnNumberToName(startCol)
	middleColName, _ := excelize.ColumnNumberToName(startCol + 3)
	endColName, _ := excelize.ColumnNumberToName(startCol + 6)
	setMatchColumnsWidth(f, sheetName, startColName, middleColName, endColName)
}

func addRoundHeaderWithStyle(f *excelize.File, sheetName string, startRow int, round int, poolHeaderStyle int) {
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("Elimination Round %d", round)))
	handleExcelError("MergeCell", f.MergeCell(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("O%d", startRow)))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("A%d", startRow), fmt.Sprintf("O%d", startRow), poolHeaderStyle))
}

func setupNamesToPrintLayout(f *excelize.File, sheetName string) {
	SetSheetLayoutLandscapeA3(f, sheetName)
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "A", "A", 20))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "B", "B", 170))
}

func CreateNamesToPrint(f *excelize.File, players []Player, sanitized bool) {
	sheetName := "Names to Print"
	setupNamesToPrintLayout(f, sheetName)
	nameIDSideStyle := getNameIDSideStyle(f)
	nameIDStyle := getNameIDStyle(f)

	row := 1
	for _, player := range players {
		positionCell := fmt.Sprintf("A%d", row)
		nameCell := fmt.Sprintf("B%d", row)
		handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 110))

		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), player.PoolPosition))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, positionCell, fmt.Sprintf("A%d", row+1), nameIDSideStyle))

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
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, nameCell, fmt.Sprintf("B%d", row+1), nameIDStyle))
		handleExcelError("MergeCell", f.MergeCell(sheetName, nameCell, fmt.Sprintf("B%d", row+1)))

		row += 2
	}

	if row > 1 {
		handleExcelError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
			Name:     "_xlnm.Print_Area",
			RefersTo: fmt.Sprintf("'%s'!$A$1:$B$%d", sheetName, row-1),
			Scope:    sheetName,
		}))
	}
}

func CreateNamesWithPoolToPrint(f *excelize.File, pools []Pool, sanitized bool) {
	sheetName := "Names to Print"
	setupNamesToPrintLayout(f, sheetName)
	nameIDSideStyle := getNameIDSideStyle(f)
	nameIDStyle := getNameIDStyle(f)

	row := 1
	namesCount := 0
	for _, pool := range pools {

		for _, player := range pool.Players {
			poolCell := fmt.Sprintf("A%d", row)
			nameCell := fmt.Sprintf("B%d", row)
			handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 110))

			handleExcelError("SetCellValue", f.SetCellValue(sheetName, poolCell, pool.PoolName))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, poolCell, fmt.Sprintf("A%d", row+1), nameIDSideStyle))

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
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, nameCell, fmt.Sprintf("B%d", row+1), nameIDStyle))
			handleExcelError("MergeCell", f.MergeCell(sheetName, nameCell, fmt.Sprintf("B%d", row+1)))

			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("A%d", row+1), player.PoolPosition))
			row += 2
			namesCount++

			if namesCount > 0 && namesCount%3 == 0 {
				breakCell := fmt.Sprintf("A%d", row)
				if err := f.InsertPageBreak(sheetName, breakCell); err != nil {
					fmt.Printf("Warning: failed to insert page break at %s: %v\n", breakCell, err)
				}
			}
		}
	}

	if row > 1 {
		handleExcelError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
			Name:     "_xlnm.Print_Area",
			RefersTo: fmt.Sprintf("'%s'!$A$1:$B$%d", sheetName, row-1),
			Scope:    sheetName,
		}))
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

func SetSheetLayoutPortraitA4(f *excelize.File, sheetName string) {
	// 9 = A4
	size := 9
	orientation := "portrait"
	one := 1
	zero := 0

	if err := f.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
		Size:        &size,
		Orientation: &orientation,
		FitToWidth:  &one,
		FitToHeight: &zero,
	}); err != nil {
		fmt.Printf("Warning: failed to set page layout for %s: %v\n", sheetName, err)
	}

	boolTrue := true
	if err := f.SetSheetProps(sheetName, &excelize.SheetPropsOptions{
		FitToPage: &boolTrue,
	}); err != nil {
		fmt.Printf("Warning: failed to set sheet props for %s: %v\n", sheetName, err)
	}

	centerOnPage(f, sheetName)
}

func SetSheetLayoutLandscapeA3(f *excelize.File, sheetName string) {
	// 8 = A3
	size := 8
	orientation := "landscape"
	one := 1
	zero := 0

	if err := f.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
		Size:        &size,
		Orientation: &orientation,
		FitToWidth:  &one,
		FitToHeight: &zero,
	}); err != nil {
		fmt.Printf("Warning: failed to set page layout for %s: %v\n", sheetName, err)
	}

	boolTrue := true
	if err := f.SetSheetProps(sheetName, &excelize.SheetPropsOptions{
		FitToPage: &boolTrue,
	}); err != nil {
		fmt.Printf("Warning: failed to set sheet props for %s: %v\n", sheetName, err)
	}

	centerOnPage(f, sheetName)
}

// SetSheetLayoutPortraitA4Centered configures portrait A4 with print-centering.
// Unlike SetSheetLayoutPortraitA4 it does not enable FitToPage, so smaller
// content keeps its natural size and the horizontal/vertical centering print
// option actually has room to take effect.
func SetSheetLayoutPortraitA4Centered(f *excelize.File, sheetName string) {
	size := 9 // A4
	orientation := "portrait"

	if err := f.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
		Size:        &size,
		Orientation: &orientation,
	}); err != nil {
		fmt.Printf("Warning: failed to set page layout for %s: %v\n", sheetName, err)
	}

	centerOnPage(f, sheetName)
}

// centerOnPage centers the worksheet content both horizontally and vertically on
// the printed page.
func centerOnPage(f *excelize.File, sheetName string) {
	boolTrue := true
	if err := f.SetPageMargins(sheetName, &excelize.PageLayoutMarginsOptions{
		Horizontally: &boolTrue,
		Vertically:   &boolTrue,
	}); err != nil {
		fmt.Printf("Warning: failed to set page centering for %s: %v\n", sheetName, err)
	}
}

// handleExcelError is a helper function to handle errors from Excel operations
func handleExcelError(operation string, err error) {
	if err != nil {
		fmt.Printf("Error in Excel operation %s: %v\n", operation, err)
	}
}
