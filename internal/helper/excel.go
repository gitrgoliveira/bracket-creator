// Package-level note: excel.go implements the page-layout algorithm that
// writes pool match and elimination match data into an Excel workbook.
//
// Layout model:
//
//   - "Pool Matches" sheet: courts are placed side-by-side (8 columns each).
//     Pools are rendered top-to-bottom within each court column.  A soft
//     page-break is inserted whenever the next pool block would overflow
//     PoolMatchesRowsPerPage rows.  Vertical page breaks separate courts so
//     the sheet prints as distinct pages.
//
//   - "Elimination Matches" section: elimination rounds are laid out
//     top-to-bottom with all courts side-by-side.  A new page break is
//     inserted when the next match block would overflow EliminationRowsPerPage.
//
//   - Tree sheets ("Tree 1", "Tree 2", …): one sheet per bracket segment.
//     Leaf values reference pool-match winner cells via CONCATENATE formulas
//     so the bracket updates automatically when scores are entered.
//
// Row-count thresholds and layout constants are defined in constants.go.
package helper

import (
	"fmt"
	"os"
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
	startColName := mustColumnName(startCol)
	leftVictoriesColName := mustColumnName(startCol + 1)
	leftPointsColName := mustColumnName(startCol + 2)
	middleColName := mustColumnName(startCol + 3)
	rightPointsColName := mustColumnName(startCol + 4)
	rightVictoriesColName := mustColumnName(startCol + 5)
	endColName := mustColumnName(startCol + 6)

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

// getMatchSides returns the left and right participants for a match.
// sideA is Red (left by default), sideB is White (right by default).
// If mirror is true, sideB (White) is returned on the left and sideA (Red) on the right.
func getMatchSides(sideA, sideB string, mirror bool) (left, right string) {
	if mirror {
		return sideB, sideA
	}
	return sideA, sideB
}

// clampCourts coerces court counts that fall below 1 to 1. Values exceeding
// MaxCourts are rejected upstream by ValidateCourts, so this function is only
// a defensive guard against zero/negative numCourts in deeper helper paths.
func clampCourts(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func writeCourtHeaders(f *excelize.File, sheetName string, numCourts int, headerStyle int) {
	mergedCells, _ := f.GetMergeCells(sheetName)
	for _, mc := range mergedCells {
		if strings.HasSuffix(mc.GetStartAxis(), "1") || strings.HasSuffix(mc.GetEndAxis(), "1") {
			handleExcelError("UnmergeCell", f.UnmergeCell(sheetName, mc.GetStartAxis(), mc.GetEndAxis()))
		}
	}

	for c := 0; c < numCourts; c++ {
		courtStartCol := 1 + c*8
		courtEndCol := courtStartCol + 6
		cStartColName := mustColumnName(courtStartCol)
		cEndColName := mustColumnName(courtEndCol)
		courtLabel := fmt.Sprintf("Shiaijo %c", rune('A'+c))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s1", cStartColName), courtLabel))
		handleExcelError("MergeCell", f.MergeCell(sheetName, fmt.Sprintf("%s1", cStartColName), fmt.Sprintf("%s1", cEndColName)))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s1", cStartColName), fmt.Sprintf("%s1", cEndColName), headerStyle))
	}
}

func getMatchWinnerColumns(colNames matchColumnNames) (lV, lP, rV, rP string) {
	return colNames.leftVictoriesColName, colNames.leftPointsColName, colNames.rightVictoriesColName, colNames.rightPointsColName
}

func printSinglePool(f *excelize.File, sheetName string, pool Pool, startCol int, startRow int, teamMatches int, numWinners int, maxBlocks []int, colNames matchColumnNames, poolHeaderStyle, textStyle, borderBottomStyle, redHeaderStyle, whiteHeaderStyle int, matchWinners map[string]MatchWinner, mirror bool) {
	spaceLines := PoolSpaceLines
	poolRow := startRow

	startColName := colNames.startColName
	middleColName := colNames.middleColName
	rightVictoriesColName := colNames.rightVictoriesColName
	endColName := colNames.endColName
	startCell := startColName + fmt.Sprint(poolRow)
	endCell := endColName + fmt.Sprint(poolRow)

	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, poolHeaderStyle))
	handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, fmt.Sprintf("%s!%s", pool.sheetName, pool.cell)))

	poolRow++
	if teamMatches == 0 {
		matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, redHeaderStyle, textStyle, whiteHeaderStyle, mirror)
		poolRow++
	}

	for m := 0; m < len(maxBlocks)-1; m++ {
		startMatchRow := poolRow

		if m < len(pool.Matches) {
			match := pool.Matches[m]
			startCell = startColName + fmt.Sprint(poolRow)
			endCell = endColName + fmt.Sprint(poolRow)
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))

			if teamMatches > 0 {
				matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, redHeaderStyle, textStyle, whiteHeaderStyle, mirror)
				poolRow++
			}

			leftSide, rightSide := getMatchSides(fmt.Sprintf("%s!%s", match.SideA.sheetName, match.SideA.cell), fmt.Sprintf("%s!%s", match.SideB.sheetName, match.SideB.cell), mirror)

			poolEntryWithStyle(startColName, poolRow, endColName, f, sheetName,
				leftSide,
				rightSide,
				textStyle)

			for i := 0; i < teamMatches; i++ {
				poolRow++
				startCell = startColName + fmt.Sprint(poolRow)
				endCell = endColName + fmt.Sprint(poolRow)
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))
				handleExcelError("SetCellInt", f.SetCellInt(sheetName, startCell, int64(i+1)))
				handleExcelError("SetCellInt", f.SetCellInt(sheetName, endCell, int64(i+1)))
			}

			if teamMatches > 0 {
				// pool results summary
				poolRow += 2
				poolEntryWithStyle(startColName, poolRow, endColName, f, sheetName,
					leftSide,
					rightSide,
					textStyle)

				lVCol, lPCol, rVCol, rPCol := getMatchWinnerColumns(colNames)

				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lVCol, poolRow), "V"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lPCol, poolRow), "P"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rVCol, poolRow), "V"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rPCol, poolRow), "P"))
				poolRow++

				startCell = startColName + fmt.Sprint(poolRow)
				endCell = endColName + fmt.Sprint(poolRow)
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, "Victories / Points"))
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, endCell, "Victories / Points"))
			}
		}

		poolRow = startMatchRow + maxBlocks[m]
	}

	if teamMatches > 0 && len(pool.Matches) > 0 {
		poolRow -= spaceLines //removing previously added spaces
	}

	for result := 1; result <= len(pool.Players); result++ {
		poolRow++
		resultCol := rightVictoriesColName
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, poolRow), fmt.Sprintf("%d. ", result)))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), borderBottomStyle))

		if result <= numWinners {
			matchWinners[fmt.Sprintf("%s-%s", pool.PoolName, getOrdinal(result))] = MatchWinner{
				sheetName: sheetName,
				cell:      fmt.Sprintf("%s%d", endColName, poolRow),
			}
		}
	}
}

func PrintPoolMatches(f *excelize.File, pools []Pool, teamMatches int, numWinners int, numCourts int, mirror bool) map[string]MatchWinner {
	numCourts = clampCourts(numCourts)

	matchWinners := make(map[string]MatchWinner)
	sheetName := "Pool Matches"
	configuredStartCols := make(map[int]bool)

	startRow := 2
	spaceLines := 2
	colNamesByStartCol := make(map[int]matchColumnNames, numCourts)

	poolHeaderStyle := getPoolHeaderStyle(f)
	textStyle := getTextStyle(f)
	borderBottomStyle := getBorderStyleBottom(f)
	redHeaderStyle := getRedHeaderStyle(f)
	whiteHeaderStyle := getWhiteHeaderStyle(f)

	writeCourtHeaders(f, sheetName, numCourts, poolHeaderStyle)

	courtAssignments, _ := AssignPoolsToCourts(len(pools), numCourts)
	poolsByCourt := make([][]int, numCourts)
	for i, c := range courtAssignments {
		poolsByCourt[c] = append(poolsByCourt[c], i)
	}

	maxPoolsInCourt := 0
	for _, pc := range poolsByCourt {
		if len(pc) > maxPoolsInCourt {
			maxPoolsInCourt = len(pc)
		}
	}

	poolRow := startRow
	rowsSinceLastPageBreak := startRow - 1
	rowsPerPageLimit := PoolMatchesRowsPerPage

	for i := 0; i < maxPoolsInCourt; i++ {
		headerBlock := 1
		if teamMatches == 0 {
			headerBlock = 2
		}

		maxMatches := 0
		for c := 0; c < numCourts; c++ {
			if i < len(poolsByCourt[c]) {
				p := pools[poolsByCourt[c][i]]
				if len(p.Matches) > maxMatches {
					maxMatches = len(p.Matches)
				}
			}
		}

		maxBlocks := make([]int, 0, maxMatches+1)
		for m := 0; m < maxMatches; m++ {
			maxMatchBlock := 0
			for c := 0; c < numCourts; c++ {
				if i < len(poolsByCourt[c]) {
					p := pools[poolsByCourt[c][i]]
					if len(p.Matches) > m {
						matchRows := 1
						if teamMatches > 0 {
							matchRows = 5 + teamMatches + spaceLines
						}
						if matchRows > maxMatchBlock {
							maxMatchBlock = matchRows
						}
					}
				}
			}
			if maxMatchBlock > 0 {
				maxBlocks = append(maxBlocks, maxMatchBlock)
			}
		}

		maxResultBlock := 0
		for c := 0; c < numCourts; c++ {
			if i < len(poolsByCourt[c]) {
				p := pools[poolsByCourt[c][i]]
				resRows := len(p.Players) + spaceLines
				if teamMatches > 0 {
					resRows = len(p.Players)
				}
				if resRows > maxResultBlock {
					maxResultBlock = resRows
				}
			}
		}
		if maxResultBlock > 0 {
			maxBlocks = append(maxBlocks, maxResultBlock)
		}

		totalPoolHeight := headerBlock
		for _, b := range maxBlocks {
			totalPoolHeight += b
		}

		// Logic to keep pool together or at least start at top of page
		if rowsSinceLastPageBreak+totalPoolHeight > rowsPerPageLimit {
			if rowsSinceLastPageBreak > 0 {
				handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", poolRow)))
				rowsSinceLastPageBreak = 0
			}
		}

		// Internal block breaks as a fallback for pools larger than a single page
		if totalPoolHeight > rowsPerPageLimit {
			cursorOffset := 0
			firstBlockSize := 0
			if len(maxBlocks) > 0 {
				firstBlockSize = maxBlocks[0]
			}

			if rowsSinceLastPageBreak+headerBlock+firstBlockSize > rowsPerPageLimit {
				handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", poolRow+cursorOffset)))
				rowsSinceLastPageBreak = 0
			}
			rowsSinceLastPageBreak += headerBlock
			cursorOffset += headerBlock

			for b := 0; b < len(maxBlocks); b++ {
				blockSize := maxBlocks[b]
				if b > 0 && rowsSinceLastPageBreak+blockSize > rowsPerPageLimit {
					handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", poolRow+cursorOffset)))
					rowsSinceLastPageBreak = 0
				}
				rowsSinceLastPageBreak += blockSize
				cursorOffset += blockSize
			}
		} else {
			rowsSinceLastPageBreak += totalPoolHeight
		}

		for c := 0; c < numCourts; c++ {
			if i < len(poolsByCourt[c]) {
				poolIdx := poolsByCourt[c][i]
				startCol := 1 + c*8

				if !configuredStartCols[startCol] {
					setMatchColumnsWidthByStartCol(f, sheetName, startCol)
					configuredStartCols[startCol] = true
				}

				colNames, ok := colNamesByStartCol[startCol]
				if !ok {
					colNames = buildMatchColumnNames(startCol)
					colNamesByStartCol[startCol] = colNames
				}

				printSinglePool(f, sheetName, pools[poolIdx], startCol, poolRow, teamMatches, numWinners, maxBlocks, colNames, poolHeaderStyle, textStyle, borderBottomStyle, redHeaderStyle, whiteHeaderStyle, matchWinners, mirror)
			}
		}

		poolRow += totalPoolHeight
	}

	lastCourtStartCol := 1 + (numCourts-1)*8
	maxColNum := lastCourtStartCol + 6
	maxColName := mustColumnName(maxColNum)

	printArea := fmt.Sprintf("'%s'!$A$1:$%s$%d", sheetName, maxColName, poolRow-1)
	handleExcelError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
		Name:     "_xlnm.Print_Area",
		RefersTo: printArea,
		Scope:    sheetName,
	}))

	// Vertical page breaks before each court except the first
	for c := 1; c < numCourts; c++ {
		courtStartCol := 1 + c*8
		colName := mustColumnName(courtStartCol)
		handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, colName+"1"))
	}

	SetSheetLayoutPortraitA4DownThenOver(f, sheetName, numCourts)

	return matchWinners
}

func poolEntryWithStyle(startColName string, poolRow int, endColName string, f *excelize.File, sheetName string, leftSide string, rightSide string, textStyle int) {
	startCell := startColName + fmt.Sprint(poolRow)
	endCell := endColName + fmt.Sprint(poolRow)
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, leftSide))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, endCell, rightSide))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))
}

func MatchHeader(f *excelize.File, sheetName string, startColName string, poolRow int, middleColName string, endColName string, mirror bool) {
	matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, getRedHeaderStyle(f), getTextStyle(f), getWhiteHeaderStyle(f), mirror)
}

func matchHeaderWithStyles(f *excelize.File, sheetName string, startColName string, poolRow int, middleColName string, endColName string, redHeaderStyle int, textStyle int, whiteHeaderStyle int, mirror bool) {
	leftLabel, rightLabel := "Red", "White"
	leftStyle, rightStyle := redHeaderStyle, whiteHeaderStyle

	if mirror {
		leftLabel, rightLabel = rightLabel, leftLabel
		leftStyle, rightStyle = rightStyle, leftStyle
	}

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", startColName, poolRow), leftLabel))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, poolRow), fmt.Sprintf("%s%d", startColName, poolRow), leftStyle))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", middleColName, poolRow), "vs"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", middleColName, poolRow), fmt.Sprintf("%s%d", middleColName, poolRow), textStyle))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), rightLabel))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, poolRow), fmt.Sprintf("%s%d", endColName, poolRow), rightStyle))
}

func PrintTeamEliminationMatches(f *excelize.File, poolMatchWinners map[string]MatchWinner, eliminationMatchRounds [][]*Node, numTeamMatches int, numCourts int, mirror bool) {
	numCourts = clampCourts(numCourts)

	sheetName := "Elimination Matches"
	matchWinners := make(map[string]MatchWinner)
	configuredStartCols := make(map[int]bool)

	startRow := 2
	spaceLines := EliminationSpaceLines
	colNamesByStartCol := make(map[int]matchColumnNames, numCourts)

	poolHeaderStyle := getPoolHeaderStyle(f)
	textStyle := getTextStyle(f)
	borderBottomStyle := getBorderStyleBottom(f)
	redHeaderStyle := getRedHeaderStyle(f)
	whiteHeaderStyle := getWhiteHeaderStyle(f)

	writeCourtHeaders(f, sheetName, numCourts, poolHeaderStyle)

	for c := 0; c < numCourts; c++ {
		courtStartCol := 1 + c*8
		if !configuredStartCols[courtStartCol] {
			setMatchColumnsWidthByStartCol(f, sheetName, courtStartCol)
			configuredStartCols[courtStartCol] = true
		}
	}

	matchHeight := EliminationMatchHeight
	if numTeamMatches > 0 {
		matchHeight = EliminationTeamMatchHeightBase + numTeamMatches
	}

	rowsSinceLastPageBreak := startRow - 1
	rowsPerPageLimit := EliminationRowsPerPage

	for roundIdx, eliminationMatchRound := range eliminationMatchRounds {
		round := roundIdx + 1
		numMatches := len(eliminationMatchRound)
		matchesPerCourt := numMatches / numCourts
		if matchesPerCourt == 0 {
			matchesPerCourt = 1
		}

		numMatchRows := 0
		for c := 0; c < numCourts; c++ {
			matchesInThisCourt := matchesPerCourt
			if c == numCourts-1 {
				matchesInThisCourt = numMatches - c*matchesPerCourt
			}
			if matchesInThisCourt > numMatchRows {
				numMatchRows = matchesInThisCourt
			}
		}

		for r := 0; r < numMatchRows; r++ {
			// Check for page break BEFORE starting a match row
			if rowsSinceLastPageBreak+matchHeight > rowsPerPageLimit {
				handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", startRow)))
				rowsSinceLastPageBreak = 0
			}

			for c := 0; c < numCourts; c++ {
				i := c*matchesPerCourt + r
				// Ensure i is within the matches assigned to this court
				if c < numCourts-1 && i >= (c+1)*matchesPerCourt {
					continue
				}
				if i >= numMatches {
					continue
				}

				eliminationMatch := eliminationMatchRound[i]
				startCol := 1 + c*8
				colNames, ok := colNamesByStartCol[startCol]
				if !ok {
					colNames = buildMatchColumnNames(startCol)
					colNamesByStartCol[startCol] = colNames
				}

				printSingleEliminationMatch(f, sheetName, eliminationMatch, poolMatchWinners, matchWinners, colNames, startRow, round, numTeamMatches, poolHeaderStyle, redHeaderStyle, textStyle, whiteHeaderStyle, borderBottomStyle, mirror)
			}
			startRow += matchHeight
			rowsSinceLastPageBreak += matchHeight
		}
		startRow += spaceLines
		rowsSinceLastPageBreak += spaceLines
	}

	lastCourtStartCol := 1 + (numCourts-1)*8
	maxColNum := lastCourtStartCol + 6
	maxColName := mustColumnName(maxColNum)

	printArea := fmt.Sprintf("'%s'!$A$1:$%s$%d", sheetName, maxColName, startRow-1)
	handleExcelError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
		Name:     "_xlnm.Print_Area",
		RefersTo: printArea,
		Scope:    sheetName,
	}))

	// Vertical page breaks before each court except the first
	for c := 1; c < numCourts; c++ {
		courtStartCol := 1 + c*8
		colName := mustColumnName(courtStartCol)
		handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, colName+"1"))
	}

	SetSheetLayoutPortraitA4DownThenOver(f, sheetName, numCourts)
}

func printSingleEliminationMatch(f *excelize.File, sheetName string, eliminationMatch *Node, poolMatchWinners map[string]MatchWinner, matchWinners map[string]MatchWinner, colNames matchColumnNames, matchRow int, round int, numTeamMatches int, poolHeaderStyle, redHeaderStyle, textStyle, whiteHeaderStyle, borderBottomStyle int, mirror bool) {
	startColName := colNames.startColName
	middleColName := colNames.middleColName
	rightVictoriesColName := colNames.rightVictoriesColName
	endColName := colNames.endColName
	startCell := startColName + fmt.Sprint(matchRow)
	endCell := endColName + fmt.Sprint(matchRow)

	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, poolHeaderStyle))
	handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, fmt.Sprintf("Round %d - Match %d", round, eliminationMatch.matchNum)))

	matchRow++
	matchHeaderWithStyles(f, sheetName, startColName, matchRow, middleColName, endColName, redHeaderStyle, textStyle, whiteHeaderStyle, mirror)
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
		mw := matchWinners[winnerFromMatch]
		if mw.sheetName == sheetName {
			leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",%s)", winnerFromMatch, mw.cell)
		} else {
			leftCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", winnerFromMatch, mw.sheetName, mw.cell)
		}
	}

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
		mw := matchWinners[winnerFromMatch]
		if mw.sheetName == sheetName {
			rightCellValue = fmt.Sprintf("CONCATENATE(\"%s \",%s)", winnerFromMatch, mw.cell)
		} else {
			rightCellValue = fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", winnerFromMatch, mw.sheetName, mw.cell)
		}
	}

	leftCellValue, rightCellValue = getMatchSides(leftCellValue, rightCellValue, mirror)

	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, leftCellValue))
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
		startCell = startColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, leftCellValue))
		endCell = endColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, endCell, rightCellValue))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))

		lVCol, lPCol, rVCol, rPCol := getMatchWinnerColumns(colNames)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lVCol, matchRow), "V"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lPCol, matchRow), "P"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rVCol, matchRow), "V"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rPCol, matchRow), "P"))
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
}

func setMatchColumnsWidthByStartCol(f *excelize.File, sheetName string, startCol int) {
	startColName := mustColumnName(startCol)
	bCol := mustColumnName(startCol + 1)
	cCol := mustColumnName(startCol + 2)
	middleColName := mustColumnName(startCol + 3)
	eCol := mustColumnName(startCol + 4)
	fCol := mustColumnName(startCol + 5)
	endColName := mustColumnName(startCol + 6)
	gapCol := mustColumnName(startCol + 7)

	handleExcelError("SetColWidth", f.SetColWidth(sheetName, startColName, startColName, 30))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, bCol, cCol, 5))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, middleColName, middleColName, 3))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, eCol, fCol, 5))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, endColName, endColName, 30))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, gapCol, gapCol, 2))
}

func setupNamesToPrintLayout(f *excelize.File, sheetName string) {
	size := 8 // A3
	orientation := "landscape"
	handleExcelError("SetPageLayout", f.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
		Size:        &size,
		Orientation: &orientation,
	}))
	// Narrow margins so exactly 3 rows fit per A3 landscape page (~270pt each).
	margin := 0.1
	handleExcelError("SetPageMargins", f.SetPageMargins(sheetName, &excelize.PageLayoutMarginsOptions{
		Top: &margin, Bottom: &margin, Left: &margin, Right: &margin,
		Header: &margin, Footer: &margin,
	}))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "A", "A", 30))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "B", "B", 160))
}

func CreateNamesToPrint(f *excelize.File, players []Player, sanitized bool) {
	sheetName := "Names to Print"
	setupNamesToPrintLayout(f, sheetName)
	nameIDPositionStyle := getNameIDPositionStyle(f)
	nameIDStyle := getNameIDStyle(f)

	row := 1
	for _, player := range players {
		positionCell := fmt.Sprintf("A%d", row)
		nameCell := fmt.Sprintf("B%d", row)
		// ~270pt = 1/3 of A3 landscape printable height with 0.1" margins.
		handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 270))

		handleExcelError("SetCellValue", f.SetCellValue(sheetName, positionCell, player.PoolPosition))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, positionCell, positionCell, nameIDPositionStyle))

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
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, nameCell, nameCell, nameIDStyle))

		if row%3 == 0 {
			handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", row+1)))
		}
		row++
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
	nameIDPositionStyle := getNameIDPositionStyle(f)
	nameIDStyle := getNameIDStyle(f)

	row := 1
	namesCount := 0
	for _, pool := range pools {
		poolLetter := strings.TrimPrefix(pool.PoolName, "Pool ")

		for _, player := range pool.Players {
			tagCell := fmt.Sprintf("A%d", row)
			nameCell := fmt.Sprintf("B%d", row)
			// ~270pt = 1/3 of A3 landscape printable height with 0.1" margins.
			handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 270))

			tag := fmt.Sprintf("%s%d", poolLetter, player.PoolPosition)
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, tagCell, tag))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, tagCell, tagCell, nameIDPositionStyle))

			if sanitized {
				_, rowNum, err := excelize.SplitCellName(player.cell)
				if err != nil {
					handleExcelError("SplitCellName", err)
					handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, "D"+player.cell[1:])))
				} else {
					rowStr := strconv.Itoa(rowNum)
					handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, "D"+rowStr)))
				}
			} else {
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, fmt.Sprintf("%s!%s", player.sheetName, player.cell)))
			}
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, nameCell, nameCell, nameIDStyle))

			row++
			namesCount++

			// 3 players per A3 landscape page.
			if namesCount%3 == 0 {
				breakCell := fmt.Sprintf("A%d", row)
				handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, breakCell))
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

func FillEstimations(f *excelize.File, numPools int64, totalPoolMatches int64, teamSize int64, numEliminationMatches int64, numCourts int) {
	sheetName := "Time Estimator"

	if teamSize == 0 {
		teamSize = 1
	}
	numCourts = clampCourts(numCourts)

	// 1. Fill Input Section (Pools)
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "A2", numPools))
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "B2", teamSize))
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "C2", totalPoolMatches))
	// Overwrite H2 formula to use total matches (C2) directly instead of A2*C2
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, "H2", "C2*I2+J2"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, "H2", "H2", getDurationStyle(f)))

	// 2. Fill Input Section (Elimination)
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "A8", numEliminationMatches))
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "B8", teamSize))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, "H8", "H8", getDurationStyle(f)))

	// 3. Fill Courts
	handleExcelError("SetCellInt", f.SetCellInt(sheetName, "A14", int64(numCourts)))

	// 4. Summary Section (Dynamic Formulas)
	// These rely on the template having specific labels, but we can also set them here for robustness
	headerStyle := getPoolHeaderStyle(f)
	textStyle := getTextStyle(f)

	// Summary Headers
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "E13", "Tournament Summary"))
	handleExcelError("MergeCell", f.MergeCell(sheetName, "E13", "H13"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, "E13", "H13", headerStyle))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "E14", "Total Pool Time"))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, "H14", "H2")) // Points to Total Pool Time

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "E15", "Total Elimination Time"))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, "H15", "H8")) // Points to Total Elimination Time

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "E16", "Grand Total (Sequential)"))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, "H16", "H14+H15"))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "E17", "Grand Total (Parallel across Courts)"))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, "H17", "H16/A14"))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "E18", "Start Time:"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "H18", 0.375))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "E19", "Estimated Finish Time"))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, "H19", "H18+H17"))

	// Apply some basic styling to the summary rows
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, "E14", "G17", textStyle))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, "H14", "H17", getDurationStyle(f)))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, "E18", "G19", headerStyle))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, "H18", "H19", getTimeStyle(f)))
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
		handleExcelError("SetPageLayout", err)
	}

	boolTrue := true
	if err := f.SetSheetProps(sheetName, &excelize.SheetPropsOptions{
		FitToPage: &boolTrue,
	}); err != nil {
		handleExcelError("SetSheetProps", err)
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
		handleExcelError("SetPageLayout", err)
	}

	boolTrue := true
	if err := f.SetSheetProps(sheetName, &excelize.SheetPropsOptions{
		FitToPage: &boolTrue,
	}); err != nil {
		handleExcelError("SetSheetProps", err)
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
		handleExcelError("SetPageLayout", err)
	}

	centerOnPage(f, sheetName)
}

func SetSheetLayoutPortraitA4DownThenOver(f *excelize.File, sheetName string, numCourts int) {
	// 9 = A4
	size := 9
	orientation := "portrait"
	pageOrder := "downThenOver"
	fitWidth := numCourts
	fitHeight := 0

	if err := f.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
		Size:        &size,
		Orientation: &orientation,
		PageOrder:   &pageOrder,
		FitToWidth:  &fitWidth,
		FitToHeight: &fitHeight,
	}); err != nil {
		handleExcelError("SetPageLayout", err)
	}

	boolTrue := true
	if err := f.SetSheetProps(sheetName, &excelize.SheetPropsOptions{
		FitToPage: &boolTrue,
	}); err != nil {
		handleExcelError("SetSheetProps", err)
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
		handleExcelError("SetPageMargins", err)
	}
}

func handleExcelError(operation string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error in Excel operation %s: %v\n", operation, err)
	}
}

// mustColumnName converts a 1-based column number to an Excel column letter
// (e.g. 1 → "A", 28 → "AB").  It panics when col ≤ 0, which indicates a
// programming error in the caller — excelize.ColumnNumberToName only errors
// for non-positive column numbers.
func mustColumnName(col int) string {
	name, err := excelize.ColumnNumberToName(col)
	if err != nil {
		panic(fmt.Sprintf("invalid column number %d: %v", col, err))
	}
	return name
}
