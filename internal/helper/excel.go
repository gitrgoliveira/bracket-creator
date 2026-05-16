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
//
// CHK037 — Kachinuki Excel rendering decision (T160 + T195–T203):
//
// The main Pool Matches / Elimination Matches sheets continue to use the
// 8-column-per-court layout invariant (CourtsColumnsPerCourt = 8 — see
// constants.go and CLAUDE.md). Variable-bout kachinuki grids would either
// overflow that budget or force a layout-mode switch the rest of the
// workbook can't accommodate, so the main sheets carry the team-match
// row only.
//
// Bout-by-bout detail is rendered on a separate "Kachinuki Detail" sheet
// (helper.SheetKachinukiDetail). See internal/helper/excel_kachinuki.go —
// the sheet uses a flexible 8-column layout (NOT bound by
// CourtsColumnsPerCourt) and is opt-in: the engine export path
// (internal/engine/export.go → collectKachinukiMatches) emits it only
// when comp.TeamMatchType == kachinuki AND at least one match carries
// bouts. CLI export paths (cmd/create-pools.go, create-playoffs.go) are
// kachinuki-agnostic and produce zero changes to existing example files.
package helper

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	excelize "github.com/xuri/excelize/v2"
)

type matchColumnNames struct {
	startCol              int
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
		startCol:              startCol,
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
func playerRef(name string, coord playerCellCoord) string {
	if coord.numberCell != "" {
		return fmt.Sprintf("%s!%s&\" \"&%s!%s", coord.sheetName, coord.numberCell, coord.sheetName, coord.cell)
	}
	return sheetRef(coord.sheetName, coord.cell)
}

func sheetRef(sheet, cell string) string {
	return fmt.Sprintf("'%s'!%s", sheet, cell)
}

func buildNameFormula(playerName string, sanitized bool, coord playerCellCoord) string {
	if sanitized {
		_, rowNum, err := excelize.SplitCellName(coord.cell)
		if err != nil {
			handleExcelError("SplitCellName", err)
			return sheetRef(coord.sheetName, "D"+coord.cell[1:])
		}
		return sheetRef(coord.sheetName, "D"+strconv.Itoa(rowNum))
	}
	return sheetRef(coord.sheetName, coord.cell)
}

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
		courtStartCol := 1 + c*CourtsColumnsPerCourt
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

type matchStyles struct {
	poolHeader           int
	text                 int
	borderBottom         int
	redHeader            int
	whiteHeader          int
	unlockedText         int
	unlockedBorderBottom int
}

type playerMatchRecord struct {
	row        int
	endRow     int    // if > 0, this is the end of a range [row, endRow]
	summaryRow int    // row where the team names are (used for tie-marking X)
	side       string // "left" or "right"
}

// buildTeamWinnersFormula returns a SUMPRODUCT Excel formula counting individual
// sub-match wins for one side across the row range [startRow, endRow].
// middleCol is the "vs/X" column; left=true counts the left side's wins.
func buildTeamWinnersFormula(middleCol, lVCol, lPCol, rVCol, rPCol string, startRow, endRow int, left bool) string {
	mRange := fmt.Sprintf("%s%d:%s%d", middleCol, startRow, middleCol, endRow)
	lcL := fmt.Sprintf(
		`(LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-","")))`,
		lVCol, startRow, lVCol, endRow, lPCol, startRow, lPCol, endRow)
	lcR := fmt.Sprintf(
		`(LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-","")))`,
		rPCol, startRow, rPCol, endRow, rVCol, startRow, rVCol, endRow)
	if left {
		return fmt.Sprintf(`SUMPRODUCT((UPPER(%s)<>"X")*(%s>%s)*1)`, mRange, lcL, lcR)
	}
	return fmt.Sprintf(`SUMPRODUCT((UPPER(%s)<>"X")*(%s>%s)*1)`, mRange, lcR, lcL)
}

// buildTeamPointsFormula returns a SUMPRODUCT Excel formula summing the
// point-character count for one side across [startRow, endRow].
// left=true sums the left side (lVCol+lPCol); false sums the right (rPCol+rVCol).
func buildTeamPointsFormula(lVCol, lPCol, rVCol, rPCol string, startRow, endRow int, left bool) string {
	rangeA := fmt.Sprintf("%s%d:%s%d", lVCol, startRow, lVCol, endRow)
	rangeB := fmt.Sprintf("%s%d:%s%d", lPCol, startRow, lPCol, endRow)
	if !left {
		rangeA = fmt.Sprintf("%s%d:%s%d", rPCol, startRow, rPCol, endRow)
		rangeB = fmt.Sprintf("%s%d:%s%d", rVCol, startRow, rVCol, endRow)
	}
	return fmt.Sprintf(
		`SUMPRODUCT(LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s," ",""),"0",""),"-","")))`,
		rangeA, rangeB)
}

func printSinglePool(f *excelize.File, sheetName string, pool Pool, startCol int, startRow int, teamMatches int, numWinners int, maxBlocks []int, colNames matchColumnNames, styles matchStyles, matchWinners map[string]MatchWinner, mirror bool, poolCoords map[string]cellCoord, pCoords map[string]playerCellCoord) {
	poolRow := startRow

	startColName := colNames.startColName
	middleColName := colNames.middleColName
	endColName := colNames.endColName
	startCell := startColName + fmt.Sprint(poolRow)
	endCell := endColName + fmt.Sprint(poolRow)

	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.poolHeader))
	handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
	pc := poolCoords[pool.PoolName]
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, sheetRef(pc.sheetName, pc.cell)))

	playerMatchRows := make(map[*Player][]playerMatchRecord)

	poolRow++
	if teamMatches == 0 {
		matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, styles.redHeader, styles.text, styles.whiteHeader, mirror)
		poolRow++
	}

	for m := 0; m < len(maxBlocks)-1; m++ {
		startMatchRow := poolRow

		if m < len(pool.Matches) {
			match := pool.Matches[m]
			startCell = startColName + fmt.Sprint(poolRow)
			endCell = endColName + fmt.Sprint(poolRow)
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.text))

			if teamMatches > 0 {
				matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, styles.redHeader, styles.text, styles.whiteHeader, mirror)
				poolRow++
			}

			leftSide, rightSide := getMatchSides(playerRef(match.SideA.Name, pCoords[playerCoordKey(*match.SideA)]), playerRef(match.SideB.Name, pCoords[playerCoordKey(*match.SideB)]), mirror)

			poolEntryWithStyle(startColName, poolRow, endColName, f, sheetName,
				leftSide,
				rightSide,
				styles.text)

			if teamMatches == 0 {
				scoreRow := poolRow
				if mirror {
					playerMatchRows[match.SideA] = append(playerMatchRows[match.SideA], playerMatchRecord{row: scoreRow, side: "right"})
					playerMatchRows[match.SideB] = append(playerMatchRows[match.SideB], playerMatchRecord{row: scoreRow, side: "left"})
				} else {
					playerMatchRows[match.SideA] = append(playerMatchRows[match.SideA], playerMatchRecord{row: scoreRow, side: "left"})
					playerMatchRows[match.SideB] = append(playerMatchRows[match.SideB], playerMatchRecord{row: scoreRow, side: "right"})
				}
			}

			// Unlock scoring columns (Victories, Points, and 'vs' for ties)
			if teamMatches > 0 {
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, middleColName+fmt.Sprint(poolRow), middleColName+fmt.Sprint(poolRow), styles.unlockedText))
			} else {
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, colNames.leftVictoriesColName+fmt.Sprint(poolRow), colNames.rightVictoriesColName+fmt.Sprint(poolRow), styles.unlockedText))
			}

			subMatchStartRow := poolRow + 1
			for i := 0; i < teamMatches; i++ {
				poolRow++
				startCell = startColName + fmt.Sprint(poolRow)
				endCell = endColName + fmt.Sprint(poolRow)
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.text))
				handleExcelError("SetCellInt", f.SetCellInt(sheetName, startCell, int64(i+1)))
				handleExcelError("SetCellInt", f.SetCellInt(sheetName, endCell, int64(i+1)))

				// Unlock scoring columns for team matches
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, colNames.leftVictoriesColName+fmt.Sprint(poolRow), colNames.rightVictoriesColName+fmt.Sprint(poolRow), styles.unlockedText))
			}
			subMatchEndRow := poolRow
			// Spacing will be handled by the block offset

			if teamMatches > 0 {
				summaryRow := subMatchStartRow - 1
				lVCol, lPCol, rVCol, rPCol := getMatchWinnerColumns(colNames)

				// Write summary formulas to the team match summary row
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lVCol, summaryRow), buildTeamWinnersFormula(middleColName, lVCol, lPCol, rVCol, rPCol, subMatchStartRow, subMatchEndRow, true)))
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lPCol, summaryRow), buildTeamPointsFormula(lVCol, lPCol, rVCol, rPCol, subMatchStartRow, subMatchEndRow, true)))
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rPCol, summaryRow), buildTeamPointsFormula(lVCol, lPCol, rVCol, rPCol, subMatchStartRow, subMatchEndRow, false)))
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rVCol, summaryRow), buildTeamWinnersFormula(middleColName, lVCol, lPCol, rVCol, rPCol, subMatchStartRow, subMatchEndRow, false)))

				if mirror {
					playerMatchRows[match.SideA] = append(playerMatchRows[match.SideA], playerMatchRecord{row: subMatchStartRow, endRow: subMatchEndRow, summaryRow: summaryRow, side: "right"})
					playerMatchRows[match.SideB] = append(playerMatchRows[match.SideB], playerMatchRecord{row: subMatchStartRow, endRow: subMatchEndRow, summaryRow: summaryRow, side: "left"})
				} else {
					playerMatchRows[match.SideA] = append(playerMatchRows[match.SideA], playerMatchRecord{row: subMatchStartRow, endRow: subMatchEndRow, summaryRow: summaryRow, side: "left"})
					playerMatchRows[match.SideB] = append(playerMatchRows[match.SideB], playerMatchRecord{row: subMatchStartRow, endRow: subMatchEndRow, summaryRow: summaryRow, side: "right"})
				}
			}
		}

		poolRow = startMatchRow + maxBlocks[m]
		if teamMatches > 0 {
			poolRow++ // Add space between team matches
		}
	}

	poolRow++ // Add a single row of space between the pool and the pool results

	resultsTableStart := poolRow
	poolRow = printPoolResultsTable(f, sheetName, pool, resultsTableStart, colNames, playerMatchRows, styles, mirror, teamMatches, pCoords)
	poolRow++

	for result := 1; result <= len(pool.Players); result++ {
		poolRow++
		resLabelColName := mustColumnName(colNames.startCol + 5) // F
		resNameColName := mustColumnName(colNames.startCol + 6)  // G

		if result == 1 {
			poolRow++ // Extra space before ranking
			resultsDataStart := resultsTableStart + 1
			resultsDataEnd := resultsTableStart + len(pool.Players)
			if teamMatches > 0 {
				resultsDataEnd = resultsTableStart + (len(pool.Players) * 2) + 2
			}

			nameRange := fmt.Sprintf("$%s$%d:$%s$%d", colNames.startColName, resultsDataStart, colNames.startColName, resultsDataEnd)
			rankRange := fmt.Sprintf("$%s$%d:$%s$%d", resNameColName, resultsDataStart, resNameColName, resultsDataEnd)

			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resNameColName, poolRow), "Ranking"))
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", resLabelColName, poolRow), fmt.Sprintf("%s%d", resNameColName, poolRow), styles.poolHeader))

			for i := range pool.Players {
				rankNum := i + 1
				label := fmt.Sprintf("%d.", rankNum)
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resLabelColName, poolRow+1+i), label))

				// Formula to find the name of the player with this rank:
				formula := fmt.Sprintf("IFERROR(INDEX(%s, MATCH(%d, %s, 0)), \"-\")", nameRange, rankNum, rankRange)
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", resNameColName, poolRow+1+i), formula))
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", resNameColName, poolRow+1+i), fmt.Sprintf("%s%d", resNameColName, poolRow+1+i), styles.unlockedBorderBottom))
			}
			poolRow += len(pool.Players) + 1
		}

		if result <= numWinners {
			matchWinners[fmt.Sprintf("%s-%s", pool.PoolName, getOrdinal(result))] = MatchWinner{
				cellCoord: cellCoord{sheetName: sheetName, cell: fmt.Sprintf("%s%d", resNameColName, poolRow)},
			}
		}
	}
}

// poolResultsCtx bundles the parameters shared across printPoolResultsTable helpers.
type poolResultsCtx struct {
	f               *excelize.File
	sheetName       string
	pool            Pool
	colNames        matchColumnNames
	playerMatchRows map[*Player][]playerMatchRecord
	styles          matchStyles
	startColName    string
	middleColName   string
	lVCol           string
	lPCol           string
	rVCol           string
	rPCol           string
	rankCol         string
	scoreCol        string
	joinFormulas    func([]string) string
	pCoords         map[string]playerCellCoord
}

// printTeamResultsTableSection writes the "Team Results" W/L/T table header and
// per-player win/loss/tie formulas starting at headerRow.
func printTeamResultsTableSection(ctx poolResultsCtx, headerRow int, cols []string) {
	f, sheetName := ctx.f, ctx.sheetName
	startColName, rankCol := ctx.startColName, ctx.rankCol
	middleColName := ctx.middleColName
	lVCol, lPCol, rVCol, rPCol := ctx.lVCol, ctx.lPCol, ctx.rVCol, ctx.rPCol
	styles, joinFormulas := ctx.styles, ctx.joinFormulas
	pool, playerMatchRows := ctx.pool, ctx.playerMatchRows

	headers := []string{"W", "L", "T"}
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", startColName, headerRow), "Team Results"))
	for i, h := range headers {
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", cols[i], headerRow), h))
	}
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rankCol, headerRow), "Rank"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, headerRow), fmt.Sprintf("%s%d", rankCol, headerRow), styles.poolHeader))

	for i, player := range pool.Players {
		row := headerRow + 1 + i
		leftSide, _ := getMatchSides(playerRef(player.Name, ctx.pCoords[playerCoordKey(player)]), "", false)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", startColName, row), leftSide))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, row), fmt.Sprintf("%s%d", startColName, row), styles.text))

		records := playerMatchRows[&pool.Players[i]]
		var wF, lF, tF []string
		for _, rec := range records {
			if rec.endRow == 0 {
				continue
			}
			isSummaryTie := fmt.Sprintf(`OR(%s%d="X",%s%d="x")`, middleColName, rec.summaryRow, middleColName, rec.summaryRow)
			vl := fmt.Sprintf("%s%d", lVCol, rec.summaryRow)
			vr := fmt.Sprintf("%s%d", rVCol, rec.summaryRow)
			pl := fmt.Sprintf("%s%d", lPCol, rec.summaryRow)
			pr := fmt.Sprintf("%s%d", rPCol, rec.summaryRow)
			var subMatchXParts []string
			for r := rec.row; r <= rec.endRow; r++ {
				subMatchXParts = append(subMatchXParts, fmt.Sprintf(`%s%d="X"`, middleColName, r))
				subMatchXParts = append(subMatchXParts, fmt.Sprintf(`%s%d="x"`, middleColName, r))
			}
			// isSummaryTie (a nested OR call) must go last: excelize's OR evaluator
			// incorrectly returns FALSE when a nested OR sits in the 2nd position
			// with additional arguments following it.
			played := fmt.Sprintf("OR(COUNTA(%s%d:%s%d,%s%d:%s%d)>0,%s,%s)", lVCol, rec.row, lPCol, rec.endRow, rPCol, rec.row, rVCol, rec.endRow, strings.Join(subMatchXParts, ","), isSummaryTie)
			isTeamTie := fmt.Sprintf("OR(%s,AND(%s=%s,%s=%s))", isSummaryTie, vl, vr, pl, pr)
			if rec.side == "left" {
				wF = append(wF, fmt.Sprintf("IF(%s,IF(%s,0,IF(OR(%s>%s,AND(%s=%s,%s>%s)),1,0)),0)", played, isTeamTie, vl, vr, vl, vr, pl, pr))
				tF = append(tF, fmt.Sprintf("IF(%s,IF(%s,1,0),0)", played, isTeamTie))
				lF = append(lF, fmt.Sprintf("IF(%s,IF(%s,0,IF(OR(%s<%s,AND(%s=%s,%s<%s)),1,0)),0)", played, isTeamTie, vl, vr, vl, vr, pl, pr))
			} else {
				wF = append(wF, fmt.Sprintf("IF(%s,IF(%s,0,IF(OR(%s>%s,AND(%s=%s,%s>%s)),1,0)),0)", played, isTeamTie, vr, vl, vr, vl, pr, pl))
				tF = append(tF, fmt.Sprintf("IF(%s,IF(%s,1,0),0)", played, isTeamTie))
				lF = append(lF, fmt.Sprintf("IF(%s,IF(%s,0,IF(OR(%s<%s,AND(%s=%s,%s<%s)),1,0)),0)", played, isTeamTie, vr, vl, vr, vl, pr, pl))
			}
		}
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols[0], row), joinFormulas(wF)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols[1], row), joinFormulas(lF)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols[2], row), joinFormulas(tF)))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", cols[0], row), fmt.Sprintf("%s%d", cols[2], row), styles.text))
	}
}

// printTeamIndividualStatsSection writes the IV/IL/IT/PW/PL table and the
// hierarchical score and rank formulas for team tournaments.
// headerRow is Table 1's header row (used for score range and rank formula).
// headerRow2 is Table 2's header row.
// cols are Table 1's W/L/T column names, referenced by the score formula.
// Returns the last data row written.
func printTeamIndividualStatsSection(ctx poolResultsCtx, headerRow int, headerRow2 int, cols []string) int {
	f, sheetName := ctx.f, ctx.sheetName
	startColName, rankCol, scoreCol := ctx.startColName, ctx.rankCol, ctx.scoreCol
	middleColName := ctx.middleColName
	lVCol, lPCol, rVCol, rPCol := ctx.lVCol, ctx.lPCol, ctx.rVCol, ctx.rPCol
	styles, joinFormulas := ctx.styles, ctx.joinFormulas
	pool, playerMatchRows := ctx.pool, ctx.playerMatchRows
	colNames := ctx.colNames

	scoreRange := fmt.Sprintf("$%s$%d:$%s$%d", scoreCol, headerRow+1, scoreCol, headerRow+len(pool.Players))

	cols2 := []string{
		mustColumnName(colNames.startCol + 1), // IV
		mustColumnName(colNames.startCol + 2), // IL
		mustColumnName(colNames.startCol + 3), // IT
		mustColumnName(colNames.startCol + 4), // PW
		mustColumnName(colNames.startCol + 5), // PL
	}
	headers2 := []string{"IV", "IL", "IT", "PW", "PL"}
	for i, h := range headers2 {
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", cols2[i], headerRow2), h))
	}
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, headerRow2), fmt.Sprintf("%s%d", cols2[len(cols2)-1], headerRow2), styles.poolHeader))

	for i, player := range pool.Players {
		row := headerRow + 1 + i
		row2 := headerRow2 + 1 + i
		leftSide, _ := getMatchSides(playerRef(player.Name, ctx.pCoords[playerCoordKey(player)]), "", false)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", startColName, row2), leftSide))

		records := playerMatchRows[&pool.Players[i]]
		var ivF, ilF, itF, pwF, plF []string
		for _, rec := range records {
			if rec.endRow == 0 {
				continue
			}
			isSummaryTie := fmt.Sprintf(`OR(%s%d="X",%s%d="x")`, middleColName, rec.summaryRow, middleColName, rec.summaryRow)
			vl := fmt.Sprintf("%s%d", lVCol, rec.summaryRow)
			vr := fmt.Sprintf("%s%d", rVCol, rec.summaryRow)
			pl := fmt.Sprintf("%s%d", lPCol, rec.summaryRow)
			pr := fmt.Sprintf("%s%d", rPCol, rec.summaryRow)
			var subMatchXParts []string
			for r := rec.row; r <= rec.endRow; r++ {
				subMatchXParts = append(subMatchXParts, fmt.Sprintf(`UPPER(%s%d)="X"`, middleColName, r))
			}
			played := fmt.Sprintf("OR(COUNTA(%s%d:%s%d,%s%d:%s%d)>0,%s,%s)", lVCol, rec.row, lPCol, rec.endRow, rPCol, rec.row, rVCol, rec.endRow, isSummaryTie, strings.Join(subMatchXParts, ","))

			var vT []string
			for r := rec.row; r <= rec.endRow; r++ {
				lcLSub := fmt.Sprintf(
					`LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d," ",""),"0",""),"-",""))`,
					lVCol, r, lPCol, r)
				lcRSub := fmt.Sprintf(
					`LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d," ",""),"0",""),"-",""))`,
					rPCol, r, rVCol, r)
				playedSub := fmt.Sprintf("COUNTA(%s%d,%s%d,%s%d,%s%d)>0", lVCol, r, lPCol, r, rPCol, r, rVCol, r)
				isSubTie := fmt.Sprintf(
					`OR(%s%d="X",%s%d="x",AND(%s,(%s)=(%s)))`,
					middleColName, r, middleColName, r, playedSub, lcLSub, lcRSub)
				vT = append(vT, fmt.Sprintf("IF(%s,1,0)", isSubTie))
			}
			vt := fmt.Sprintf("(%s)", strings.Join(vT, "+"))

			if rec.side == "left" {
				ivF = append(ivF, fmt.Sprintf("IF(%s,%s,0)", played, vl))
				ilF = append(ilF, fmt.Sprintf("IF(%s,%s,0)", played, vr))
				itF = append(itF, fmt.Sprintf("IF(%s,%s,0)", played, vt))
				pwF = append(pwF, pl)
				plF = append(plF, pr)
			} else {
				ivF = append(ivF, fmt.Sprintf("IF(%s,%s,0)", played, vr))
				ilF = append(ilF, fmt.Sprintf("IF(%s,%s,0)", played, vl))
				itF = append(itF, fmt.Sprintf("IF(%s,%s,0)", played, vt))
				pwF = append(pwF, pr)
				plF = append(plF, pl)
			}
		}
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[0], row2), joinFormulas(ivF)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[1], row2), joinFormulas(ilF)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[2], row2), joinFormulas(itF)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[3], row2), joinFormulas(pwF)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[4], row2), joinFormulas(plF)))

		// Hierarchical Score Formula
		scoreFormula := fmt.Sprintf("=(%s%d*1000000000)-(%s%d*10000000)+(%s%d*100000)+(%s%d*1000)-(%s%d*100)+(%s%d*10)+(%s%d*1)-(%s%d*0.01)",
			cols[0], row, cols[1], row, cols[2], row, cols2[0], row2, cols2[1], row2, cols2[2], row2, cols2[3], row2, cols2[4], row2)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", scoreCol, row), scoreFormula))

		// Rank Formula
		rankFormula := fmt.Sprintf("RANK(%s%d,%s)+COUNTIF($%s$%d:%s%d,%s%d)",
			scoreCol, row, scoreRange, scoreCol, headerRow, scoreCol, row-1, scoreCol, row)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rankCol, row), rankFormula))

		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, row2), fmt.Sprintf("%s%d", cols2[4], row2), styles.text))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", rankCol, row), fmt.Sprintf("%s%d", rankCol, row), styles.unlockedText))
	}
	return headerRow2 + len(pool.Players)
}

// printIndividualResultsTableSection writes the individual-match Results table
// (W/L/T/PW/PL/Rank) starting at headerRow.
// Returns the last data row written.
func printIndividualResultsTableSection(ctx poolResultsCtx, headerRow int, teamMatches int) int {
	f, sheetName := ctx.f, ctx.sheetName
	startColName, rankCol, scoreCol := ctx.startColName, ctx.rankCol, ctx.scoreCol
	middleColName := ctx.middleColName
	lVCol, lPCol, rVCol, rPCol := ctx.lVCol, ctx.lPCol, ctx.rVCol, ctx.rPCol
	styles, joinFormulas := ctx.styles, ctx.joinFormulas
	pool, playerMatchRows := ctx.pool, ctx.playerMatchRows

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", startColName, headerRow), "Results"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lVCol, headerRow), "W"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lPCol, headerRow), "L"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", middleColName, headerRow), "T"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rPCol, headerRow), "PW"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rVCol, headerRow), "PL"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rankCol, headerRow), "Rank"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, headerRow), fmt.Sprintf("%s%d", rankCol, headerRow), styles.poolHeader))

	scoreRange := fmt.Sprintf("$%s$%d:$%s$%d", scoreCol, headerRow+1, scoreCol, headerRow+len(pool.Players))

	for i, player := range pool.Players {
		row := headerRow + 1 + i
		leftSide, _ := getMatchSides(playerRef(player.Name, ctx.pCoords[playerCoordKey(player)]), "", false)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", startColName, row), leftSide))

		records := playerMatchRows[&pool.Players[i]]
		var wFormulas, tFormulas, lFormulas, pwFormulas, plFormulas []string
		for _, rec := range records {
			var leftTotal, rightTotal, played string
			if teamMatches > 0 && rec.endRow > 0 {
				continue
			}
			if teamMatches == 0 {
				lc := func(col string, r int) string {
					return fmt.Sprintf(`LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d," ",""),"0",""),"-",""))`, col, r)
				}
				leftTotal = fmt.Sprintf("%s+%s", lc(lVCol, rec.row), lc(lPCol, rec.row))
				rightTotal = fmt.Sprintf("%s+%s", lc(rPCol, rec.row), lc(rVCol, rec.row))
				isTie := fmt.Sprintf(`OR(%s%d="X",%s%d="x")`, middleColName, rec.row, middleColName, rec.row)
				played = fmt.Sprintf("OR(COUNTA(%s%d,%s%d,%s%d,%s%d)>0,%s)", lVCol, rec.row, lPCol, rec.row, rPCol, rec.row, rVCol, rec.row, isTie)
			} else {
				nv := func(col string, r int) string {
					return fmt.Sprintf("IF(ISNUMBER(%s%d),%s%d,0)", col, r, col, r)
				}
				leftTotal = fmt.Sprintf("%s+%s", nv(lVCol, rec.row), nv(lPCol, rec.row))
				rightTotal = fmt.Sprintf("%s+%s", nv(rPCol, rec.row), nv(rVCol, rec.row))
				played = fmt.Sprintf("OR(ISNUMBER(%s%d),ISNUMBER(%s%d),ISNUMBER(%s%d),ISNUMBER(%s%d))", lVCol, rec.row, lPCol, rec.row, rPCol, rec.row, rVCol, rec.row)
			}
			isTie := fmt.Sprintf(
				`OR(%s%d="X",%s%d="x",AND(%s,(%s)=(%s)))`,
				middleColName, rec.row, middleColName, rec.row, played, leftTotal, rightTotal)
			if rec.side == "left" {
				wFormulas = append(wFormulas, fmt.Sprintf("IF(%s,IF(%s,0,(%s>%s)*1),0)", played, isTie, leftTotal, rightTotal))
				tFormulas = append(tFormulas, fmt.Sprintf("IF(%s,IF(%s,1,0),0)", played, isTie))
				lFormulas = append(lFormulas, fmt.Sprintf("IF(%s,IF(%s,0,(%s<%s)*1),0)", played, isTie, leftTotal, rightTotal))
				pwFormulas = append(pwFormulas, leftTotal)
				plFormulas = append(plFormulas, rightTotal)
			} else {
				wFormulas = append(wFormulas, fmt.Sprintf("IF(%s,IF(%s,0,(%s>%s)*1),0)", played, isTie, rightTotal, leftTotal))
				tFormulas = append(tFormulas, fmt.Sprintf("IF(%s,IF(%s,1,0),0)", played, isTie))
				lFormulas = append(lFormulas, fmt.Sprintf("IF(%s,IF(%s,0,(%s<%s)*1),0)", played, isTie, rightTotal, leftTotal))
				pwFormulas = append(pwFormulas, rightTotal)
				plFormulas = append(plFormulas, leftTotal)
			}
		}
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lVCol, row), joinFormulas(wFormulas)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lPCol, row), joinFormulas(lFormulas)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", middleColName, row), joinFormulas(tFormulas)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rPCol, row), joinFormulas(pwFormulas)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rVCol, row), joinFormulas(plFormulas)))

		// Weighted Score formula
		scoreFormula := fmt.Sprintf("=(%s%d*1000000)-(%s%d*10000)+(%s%d*100)+(%s%d*1)-(%s%d*0.01)",
			lVCol, row, lPCol, row, middleColName, row, rPCol, row, rVCol, row)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", scoreCol, row), scoreFormula))

		// Rank formula
		rankFormula := fmt.Sprintf("RANK(%s%d,%s)+COUNTIF($%s$%d:%s%d,%s%d)",
			scoreCol, row, scoreRange, scoreCol, headerRow, scoreCol, row-1, scoreCol, row)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rankCol, row), rankFormula))

		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, row), fmt.Sprintf("%s%d", rVCol, row), styles.text))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", rankCol, row), fmt.Sprintf("%s%d", rankCol, row), styles.unlockedText))
	}
	return headerRow + len(pool.Players)
}

func printPoolResultsTable(f *excelize.File, sheetName string, pool Pool, startRow int, colNames matchColumnNames, playerMatchRows map[*Player][]playerMatchRecord, styles matchStyles, mirror bool, teamMatches int, pCoords map[string]playerCellCoord) int {
	lVCol, lPCol, rVCol, rPCol := getMatchWinnerColumns(colNames)
	scoreCol := mustColumnName(colNames.startCol + 20)
	rankCol := mustColumnName(colNames.startCol + 6)
	handleExcelError("SetColVisible", f.SetColVisible(sheetName, scoreCol, false))

	joinFormulas := func(parts []string) string {
		if len(parts) == 0 {
			return "0"
		}
		return strings.Join(parts, "+")
	}

	ctx := poolResultsCtx{
		f:               f,
		sheetName:       sheetName,
		pool:            pool,
		colNames:        colNames,
		playerMatchRows: playerMatchRows,
		styles:          styles,
		startColName:    colNames.startColName,
		middleColName:   colNames.middleColName,
		lVCol:           lVCol,
		lPCol:           lPCol,
		rVCol:           rVCol,
		rPCol:           rPCol,
		rankCol:         rankCol,
		scoreCol:        scoreCol,
		joinFormulas:    joinFormulas,
		pCoords:         pCoords,
	}

	headerRow := startRow
	if teamMatches > 0 {
		cols := []string{
			mustColumnName(colNames.startCol + 1), // W
			mustColumnName(colNames.startCol + 2), // L
			mustColumnName(colNames.startCol + 3), // T
		}
		printTeamResultsTableSection(ctx, headerRow, cols)
		headerRow2 := headerRow + len(pool.Players) + 2
		return printTeamIndividualStatsSection(ctx, headerRow, headerRow2, cols)
	}
	return printIndividualResultsTableSection(ctx, headerRow, teamMatches)
}

func PrintPoolMatches(f *excelize.File, pools []Pool, teamMatches int, numWinners int, numCourts int, mirror bool, poolCoords map[string]cellCoord, pCoords map[string]playerCellCoord) map[string]MatchWinner {
	numCourts = clampCourts(numCourts)

	matchWinners := make(map[string]MatchWinner)
	sheetName := SheetPoolMatches
	configuredStartCols := make(map[int]bool)

	startRow := 2
	spaceLines := 2
	colNamesByStartCol := make(map[int]matchColumnNames, numCourts)

	styles := matchStyles{
		poolHeader:           getPoolHeaderStyle(f),
		text:                 getGreyTextStyle(f),
		borderBottom:         getBorderStyleBottom(f),
		redHeader:            getRedHeaderStyle(f),
		whiteHeader:          getWhiteHeaderStyle(f),
		unlockedText:         getUnlockedTextStyle(f),
		unlockedBorderBottom: getUnlockedBorderStyleBottom(f),
	}

	writeCourtHeaders(f, sheetName, numCourts, styles.poolHeader)

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
							// Red/White Header (1) + Team Names (1) + Sub-matches (teamMatches)
							matchRows = 2 + teamMatches
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
				var resRows int
				if teamMatches > 0 {
					// Team matches stacked results:
					// Space before results (1)
					// Table 1: Header (1) + Players (len)
					// Space between tables (1)
					// Table 2: Header (1) + Players (len)
					// Space before ranking (1)
					// Rankings: len(Players)
					// Space after pool (1)
					resRows = 3*len(p.Players) + 11
				} else {
					// Results: Space (1) + Header (1) + Players (len) + Space (1) + Finalists (len)
					// Individual matches include additional spaceLines
					resRows = 3 + len(p.Players)*2 + spaceLines
				}

				if resRows > maxResultBlock {
					maxResultBlock = resRows
				}
			}
		}
		if maxResultBlock > 0 {
			maxBlocks = append(maxBlocks, maxResultBlock)
		}

		totalPoolHeight := headerBlock + 1 // One row of space before the next pool
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
				startCol := 1 + c*CourtsColumnsPerCourt

				if !configuredStartCols[startCol] {
					setMatchColumnsWidthByStartCol(f, sheetName, startCol)
					configuredStartCols[startCol] = true
				}

				colNames, ok := colNamesByStartCol[startCol]
				if !ok {
					colNames = buildMatchColumnNames(startCol)
					colNamesByStartCol[startCol] = colNames
				}

				printSinglePool(f, sheetName, pools[poolIdx], startCol, poolRow, teamMatches, numWinners, maxBlocks, colNames, styles, matchWinners, mirror, poolCoords, pCoords)
			}
		}

		poolRow += totalPoolHeight
	}

	lastCourtStartCol := 1 + (numCourts-1)*CourtsColumnsPerCourt
	maxColNum := lastCourtStartCol + 7
	maxColName := mustColumnName(maxColNum)

	printArea := fmt.Sprintf("'%s'!$A$1:$%s$%d", sheetName, maxColName, poolRow-1)
	handleExcelError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
		Name:     "_xlnm.Print_Area",
		RefersTo: printArea,
		Scope:    sheetName,
	}))

	// Vertical page breaks before each court except the first
	for c := 1; c < numCourts; c++ {
		courtStartCol := 1 + c*CourtsColumnsPerCourt
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

	sheetName := SheetEliminationMatches
	matchWinners := make(map[string]MatchWinner)
	configuredStartCols := make(map[int]bool)

	startRow := 2
	spaceLines := EliminationSpaceLines
	colNamesByStartCol := make(map[int]matchColumnNames, numCourts)

	styles := matchStyles{
		poolHeader:           getPoolHeaderStyle(f),
		text:                 getGreyTextStyle(f),
		borderBottom:         getBorderStyleBottom(f),
		redHeader:            getRedHeaderStyle(f),
		whiteHeader:          getWhiteHeaderStyle(f),
		unlockedText:         getUnlockedTextStyle(f),
		unlockedBorderBottom: getUnlockedBorderStyleBottom(f),
	}

	writeCourtHeaders(f, sheetName, numCourts, styles.poolHeader)

	for c := 0; c < numCourts; c++ {
		courtStartCol := 1 + c*CourtsColumnsPerCourt
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
				startCol := 1 + c*CourtsColumnsPerCourt
				colNames, ok := colNamesByStartCol[startCol]
				if !ok {
					colNames = buildMatchColumnNames(startCol)
					colNamesByStartCol[startCol] = colNames
				}

				printSingleEliminationMatch(f, sheetName, eliminationMatch, poolMatchWinners, matchWinners, colNames, startRow, round, numTeamMatches, styles, mirror)
			}
			startRow += matchHeight
			rowsSinceLastPageBreak += matchHeight
		}
		startRow += spaceLines
		rowsSinceLastPageBreak += spaceLines
	}

	lastCourtStartCol := 1 + (numCourts-1)*CourtsColumnsPerCourt
	maxColNum := lastCourtStartCol + 7
	maxColName := mustColumnName(maxColNum)

	printArea := fmt.Sprintf("'%s'!$A$1:$%s$%d", sheetName, maxColName, startRow-1)
	handleExcelError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
		Name:     "_xlnm.Print_Area",
		RefersTo: printArea,
		Scope:    sheetName,
	}))

	// Vertical page breaks before each court except the first
	for c := 1; c < numCourts; c++ {
		courtStartCol := 1 + c*CourtsColumnsPerCourt
		colName := mustColumnName(courtStartCol)
		handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, colName+"1"))
	}

	SetSheetLayoutPortraitA4DownThenOver(f, sheetName, numCourts)
}

func printSingleEliminationMatch(f *excelize.File, sheetName string, eliminationMatch *Node, poolMatchWinners map[string]MatchWinner, matchWinners map[string]MatchWinner, colNames matchColumnNames, matchRow int, round int, numTeamMatches int, styles matchStyles, mirror bool) {
	startColName := colNames.startColName
	middleColName := colNames.middleColName
	rightVictoriesColName := colNames.rightVictoriesColName
	endColName := colNames.endColName
	startCell := startColName + fmt.Sprint(matchRow)
	endCell := endColName + fmt.Sprint(matchRow)

	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.poolHeader))
	handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, fmt.Sprintf("Round %d - Match %d", round, eliminationMatch.matchNum)))

	matchRow++
	matchHeaderWithStyles(f, sheetName, startColName, matchRow, middleColName, endColName, styles.redHeader, styles.text, styles.whiteHeader, mirror)
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
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.text))

	// Unlock scoring columns (Victories, Points, and 'vs' for ties)
	if numTeamMatches > 0 {
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, middleColName+fmt.Sprint(matchRow), middleColName+fmt.Sprint(matchRow), styles.unlockedText))
	} else {
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, colNames.leftVictoriesColName+fmt.Sprint(matchRow), colNames.rightVictoriesColName+fmt.Sprint(matchRow), styles.unlockedText))
	}

	// adding the individual matches
	firstTeamMatchRow := matchRow + 1
	for i := 0; i < numTeamMatches; i++ {
		matchRow++
		startCell = startColName + fmt.Sprint(matchRow)
		endCell = endColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.text))
		handleExcelError("SetCellInt", f.SetCellInt(sheetName, startCell, int64(i+1)))
		handleExcelError("SetCellInt", f.SetCellInt(sheetName, endCell, int64(i+1)))

		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, colNames.leftVictoriesColName+fmt.Sprint(matchRow), colNames.rightVictoriesColName+fmt.Sprint(matchRow), styles.unlockedText))
	}
	lastTeamMatchRow := matchRow

	if numTeamMatches > 0 {
		// pool results summary
		matchRow += 2
		startCell = startColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, leftCellValue))
		endCell = endColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, endCell, rightCellValue))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.text))

		lVCol, lPCol, rVCol, rPCol := getMatchWinnerColumns(colNames)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lVCol, matchRow), "IV"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lPCol, matchRow), "PW"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rVCol, matchRow), "IV"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rPCol, matchRow), "PW"))

		matchRow++
		startCell = startColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, "Victories / Points"))
		endCell = endColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, endCell, "Victories / Points"))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.text))

		// Use formulas to tally victories and points from the individual team sub-match rows.
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lVCol, matchRow), buildTeamWinnersFormula(middleColName, lVCol, lPCol, rVCol, rPCol, firstTeamMatchRow, lastTeamMatchRow, true)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lPCol, matchRow), buildTeamPointsFormula(lVCol, lPCol, rVCol, rPCol, firstTeamMatchRow, lastTeamMatchRow, true)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rVCol, matchRow), buildTeamWinnersFormula(middleColName, lVCol, lPCol, rVCol, rPCol, firstTeamMatchRow, lastTeamMatchRow, false)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rPCol, matchRow), buildTeamPointsFormula(lVCol, lPCol, rVCol, rPCol, firstTeamMatchRow, lastTeamMatchRow, false)))

		matchRow++ // Add space after team match summary
	}

	matchRow += 2 // Spacing before result marking
	resultCol := rightVictoriesColName
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "1."))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), styles.unlockedBorderBottom))

	// Gathering the match winners for the following rounds
	matchWinners[fmt.Sprintf("M %d", eliminationMatch.matchNum)] = MatchWinner{
		cellCoord: cellCoord{sheetName: sheetName, cell: fmt.Sprintf("%s%d", endColName, matchRow)},
	}

	matchRow++
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "2."))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), styles.unlockedBorderBottom))
}

func setMatchColumnsWidthByStartCol(f *excelize.File, sheetName string, startCol int) {
	startColName := mustColumnName(startCol)
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, startColName, startColName, matchNameColWidth))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, mustColumnName(startCol+1), mustColumnName(startCol+5), matchScoreColWidth))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, mustColumnName(startCol+6), mustColumnName(startCol+6), matchNameColWidth))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, mustColumnName(startCol+7), mustColumnName(startCol+7), matchSpacerColWidth))
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

type nameEntry struct {
	player      Player
	fallbackTag interface{}
}

func courtSheetName(courtIdx int) string {
	return fmt.Sprintf("%s %s", SheetNamesToPrint, CourtLabel(courtIdx))
}

func CreateNamesToPrint(f *excelize.File, players []Player, sanitized bool, numCourts int, pCoords map[string]playerCellCoord) {
	numCourts = clampCourts(numCourts)

	base := len(players) / numCourts
	extra := len(players) % numCourts
	offset := 0

	for c := range numCourts {
		count := base
		if c < extra {
			count++
		}
		courtPlayers := players[offset : offset+count]
		offset += count

		if len(courtPlayers) == 0 {
			continue
		}

		entries := make([]nameEntry, len(courtPlayers))
		for i, p := range courtPlayers {
			entries[i] = nameEntry{player: p, fallbackTag: p.PoolPosition}
		}

		sheetName := courtSheetName(c)
		if _, err := f.NewSheet(sheetName); err != nil {
			handleExcelError("NewSheet", err)
		}
		printNameEntries(f, sheetName, entries, sanitized, pCoords)
	}

	if err := f.DeleteSheet(SheetNamesToPrint); err != nil {
		fmt.Fprintf(os.Stderr, "Note: %s sheet might not exist: %v\n", SheetNamesToPrint, err)
	}
}

func CreateNamesWithPoolToPrint(f *excelize.File, pools []Pool, sanitized bool, numCourts int, pCoords map[string]playerCellCoord) {
	numCourts = clampCourts(numCourts)
	courtAssignments, _ := AssignPoolsToCourts(len(pools), numCourts)

	entriesByCourt := make([][]nameEntry, numCourts)
	for poolIdx, pool := range pools {
		court := courtAssignments[poolIdx]
		poolLetter := strings.TrimPrefix(pool.PoolName, "Pool ")
		for _, player := range pool.Players {
			entriesByCourt[court] = append(entriesByCourt[court], nameEntry{
				player:      player,
				fallbackTag: fmt.Sprintf("%s%d", poolLetter, player.PoolPosition),
			})
		}
	}

	for c := range numCourts {
		if len(entriesByCourt[c]) == 0 {
			continue
		}
		sheetName := courtSheetName(c)
		if _, err := f.NewSheet(sheetName); err != nil {
			handleExcelError("NewSheet", err)
		}
		printNameEntries(f, sheetName, entriesByCourt[c], sanitized, pCoords)
	}

	if err := f.DeleteSheet(SheetNamesToPrint); err != nil {
		fmt.Fprintf(os.Stderr, "Note: %s sheet might not exist: %v\n", SheetNamesToPrint, err)
	}
}

func printNameEntries(f *excelize.File, sheetName string, entries []nameEntry, sanitized bool, pCoords map[string]playerCellCoord) {
	setupNamesToPrintLayout(f, sheetName)
	nameIDPositionStyle := getNameIDPositionStyle(f)
	nameIDStyle := getNameIDStyle(f)

	for i, entry := range entries {
		row := i + 1
		positionCell := fmt.Sprintf("A%d", row)
		nameCell := fmt.Sprintf("B%d", row)
		handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 270))

		coord := pCoords[playerCoordKey(entry.player)]
		if coord.numberCell != "" {
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, positionCell, sheetRef(coord.sheetName, coord.numberCell)))
		} else {
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, positionCell, entry.fallbackTag))
		}
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, positionCell, positionCell, nameIDPositionStyle))

		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, buildNameFormula(entry.player.Name, sanitized, coord)))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, nameCell, nameCell, nameIDStyle))

		if row%3 == 0 {
			handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", row+1)))
		}
	}

	if len(entries) > 0 {
		handleExcelError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
			Name:     "_xlnm.Print_Area",
			RefersTo: fmt.Sprintf("'%s'!$A$1:$B$%d", sheetName, len(entries)),
			Scope:    sheetName,
		}))
	}
}

func FillEstimations(f *excelize.File, numPools int64, totalPoolMatches int64, teamSize int64, numEliminationMatches int64, numCourts int) {
	sheetName := SheetTimeEstimator

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

// ProtectSheets applies sheet-level protection.
func ProtectSheets(f *excelize.File, sheetNames []string) {
	for _, name := range sheetNames {
		// No password needed for accident prevention.
		// Allow selecting all cells, but only unlocked cells can be edited.
		handleExcelError("ProtectSheet", f.ProtectSheet(name, &excelize.SheetProtectionOptions{
			SelectLockedCells:   true,
			SelectUnlockedCells: true,
		}))
	}
}

// ProtectAllSheets applies protection to the Tree sheets, Names to Print,
// Tags, Pool Matches, and Elimination Matches.
// The score-entry sheets have explicitly unlocked cells for data entry.
func ProtectAllSheets(f *excelize.File) {
	for _, name := range f.GetSheetList() {
		// Data, Time Estimator, and Pool Draw remain fully editable.
		if name == SheetData || name == SheetTimeEstimator || name == SheetPoolDraw {
			continue
		}
		ProtectSheets(f, []string{name})
	}
}
