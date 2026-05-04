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
func playerRef(p *Player) string {
	if p.numberCell != "" {
		return fmt.Sprintf("%s!%s&\" \"&%s!%s", p.sheetName, p.numberCell, p.sheetName, p.cell)
	}
	return sheetRef(p.sheetName, p.cell)
}

func sheetRef(sheet, cell string) string {
	return fmt.Sprintf("%s!%s", sheet, cell)
}

func buildNameFormula(player Player, sanitized bool) string {
	if sanitized {
		_, rowNum, err := excelize.SplitCellName(player.cell)
		if err != nil {
			handleExcelError("SplitCellName", err)
			return sheetRef(player.sheetName, "D"+player.cell[1:])
		}
		return sheetRef(player.sheetName, "D"+strconv.Itoa(rowNum))
	}
	return sheetRef(player.sheetName, player.cell)
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

func printSinglePool(f *excelize.File, sheetName string, pool Pool, startCol int, startRow int, teamMatches int, numWinners int, maxBlocks []int, colNames matchColumnNames, styles matchStyles, matchWinners map[string]MatchWinner, mirror bool) {
	poolRow := startRow

	startColName := colNames.startColName
	middleColName := colNames.middleColName
	rightVictoriesColName := colNames.rightVictoriesColName
	endColName := colNames.endColName
	startCell := startColName + fmt.Sprint(poolRow)
	endCell := endColName + fmt.Sprint(poolRow)

	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.poolHeader))
	handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, sheetRef(pool.sheetName, pool.cell)))

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

			leftSide, rightSide := getMatchSides(playerRef(match.SideA), playerRef(match.SideB), mirror)

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
			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, colNames.leftVictoriesColName+fmt.Sprint(poolRow), colNames.rightVictoriesColName+fmt.Sprint(poolRow), styles.unlockedText))

			subMatchStartRow := poolRow + 1
			for i := 0; i < teamMatches; i++ {
				poolRow++
				startCell = startColName + fmt.Sprint(poolRow)
				endCell = endColName + fmt.Sprint(poolRow)
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.unlockedText))
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

				// Use SUMPRODUCT for much shorter and more efficient formulas
				// This avoids exceeding Excel's 8192 character limit for formulas
				// Individual Winners (IV) for a side
				buildWinnersFormula := func(left bool) string {
					mRange := fmt.Sprintf("%s%d:%s%d", middleColName, subMatchStartRow, middleColName, subMatchEndRow)

					// slt = total points of submatch for left
					// slt = LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(lV," ",""),"0",""),"-","")) + LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(lP," ",""),"0",""),"-",""))
					lcL := fmt.Sprintf(`(LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-","")))`, lVCol, subMatchStartRow, lVCol, subMatchEndRow, lPCol, subMatchStartRow, lPCol, subMatchEndRow)
					lcR := fmt.Sprintf(`(LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-","")))`, rPCol, subMatchStartRow, rPCol, subMatchEndRow, rVCol, subMatchStartRow, rVCol, subMatchEndRow)

					if left {
						return fmt.Sprintf(`SUMPRODUCT((UPPER(%s)<>"X")*(%s>%s)*1)`, mRange, lcL, lcR)
					}
					return fmt.Sprintf(`SUMPRODUCT((UPPER(%s)<>"X")*(%s>%s)*1)`, mRange, lcR, lcL)
				}

				// Points Won (PW) for a side
				buildPointsFormula := func(left bool) string {
					rangeA := fmt.Sprintf("%s%d:%s%d", lVCol, subMatchStartRow, lVCol, subMatchEndRow)
					rangeB := fmt.Sprintf("%s%d:%s%d", lPCol, subMatchStartRow, lPCol, subMatchEndRow)
					if !left {
						rangeA = fmt.Sprintf("%s%d:%s%d", rPCol, subMatchStartRow, rPCol, subMatchEndRow)
						rangeB = fmt.Sprintf("%s%d:%s%d", rVCol, subMatchStartRow, rVCol, subMatchEndRow)
					}
					// SUMPRODUCT(LEN(SUBSTITUTE(rangeA," ","")) + LEN(SUBSTITUTE(rangeB," ","")))
					// We use the same substitution rules as individual matches
					return fmt.Sprintf(`SUMPRODUCT(LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s," ",""),"0",""),"-","")))`, rangeA, rangeB)
				}

				// Write summary formulas to the team match summary row
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lVCol, summaryRow), buildWinnersFormula(true)))
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lPCol, summaryRow), buildPointsFormula(true)))
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rPCol, summaryRow), buildPointsFormula(false)))
				handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rVCol, summaryRow), buildWinnersFormula(false)))

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

	poolRow = printPoolResultsTable(f, sheetName, pool, poolRow, colNames, playerMatchRows, styles, mirror, teamMatches)
	poolRow++

	for result := 1; result <= len(pool.Players); result++ {
		poolRow++
		resColName := rightVictoriesColName
		resEndColName := endColName
		if teamMatches > 0 {
			resColName = mustColumnName(colNames.startCol + 5)
			resEndColName = mustColumnName(colNames.startCol + 6)
			if result == 1 {
				poolRow++ // Extra space before ranking
				handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resEndColName, poolRow), "Ranking"))
				handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", resColName, poolRow), fmt.Sprintf("%s%d", resEndColName, poolRow), styles.poolHeader))
				poolRow++
			}
		}
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resColName, poolRow), fmt.Sprintf("%d. ", result)))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", resEndColName, poolRow), fmt.Sprintf("%s%d", resEndColName, poolRow), styles.unlockedBorderBottom))

		if result <= numWinners {
			matchWinners[fmt.Sprintf("%s-%s", pool.PoolName, getOrdinal(result))] = MatchWinner{
				sheetName: sheetName,
				cell:      fmt.Sprintf("%s%d", resEndColName, poolRow),
			}
		}
	}
}

func printPoolResultsTable(f *excelize.File, sheetName string, pool Pool, startRow int, colNames matchColumnNames, playerMatchRows map[*Player][]playerMatchRecord, styles matchStyles, mirror bool, teamMatches int) int {
	startColName := colNames.startColName
	middleColName := colNames.middleColName
	lVCol, lPCol, rVCol, rPCol := getMatchWinnerColumns(colNames)
	headerRow := startRow
	joinFormulas := func(parts []string) string {
		if len(parts) == 0 {
			return "0"
		}
		return strings.Join(parts, "+")
	}

	if teamMatches > 0 {
		// Table 1: W, L, T
		headers := []string{"W", "L", "T"}
		cols := []string{
			mustColumnName(colNames.startCol + 1), // W
			mustColumnName(colNames.startCol + 2), // L
			mustColumnName(colNames.startCol + 3), // T
		}
		for i, h := range headers {
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", cols[i], headerRow), h))
		}
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, headerRow), fmt.Sprintf("%s%d", cols[2], headerRow), styles.poolHeader))

		for i, player := range pool.Players {
			row := headerRow + 1 + i
			leftSide, _ := getMatchSides(playerRef(&player), "", false)
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", startColName, row), leftSide))

			records := playerMatchRows[&pool.Players[i]]
			var wF, lF, tF []string
			for _, rec := range records {
				if rec.endRow == 0 {
					continue
				}
				isSummaryTie := fmt.Sprintf(`OR(%s%d="X",%s%d="x")`, middleColName, rec.summaryRow, middleColName, rec.summaryRow)

				// Team totals from summary row
				vl := fmt.Sprintf("%s%d", lVCol, rec.summaryRow)
				vr := fmt.Sprintf("%s%d", rVCol, rec.summaryRow)
				pl := fmt.Sprintf("%s%d", lPCol, rec.summaryRow)
				pr := fmt.Sprintf("%s%d", rPCol, rec.summaryRow)

				// Match is played if any sub-match has scores OR summary row has 'X'
				played := fmt.Sprintf("OR(COUNTA(%s%d:%s%d,%s%d:%s%d)>0,%s)", lVCol, rec.row, lPCol, rec.endRow, rPCol, rec.row, rVCol, rec.endRow, isSummaryTie)

				if rec.side == "left" {
					wF = append(wF, fmt.Sprintf("IF(%s,IF(%s,0,IF(OR(%s>%s,AND(%s=%s,%s>%s)),1,0)),0)", played, isSummaryTie, vl, vr, vl, vr, pl, pr))
					tF = append(tF, fmt.Sprintf("IF(%s,IF(%s,1,0),0)", played, isSummaryTie))
					lF = append(lF, fmt.Sprintf("IF(%s,IF(%s,0,IF(OR(%s<%s,AND(%s=%s,%s<%s)),1,0)),0)", played, isSummaryTie, vl, vr, vl, vr, pl, pr))
				} else {
					wF = append(wF, fmt.Sprintf("IF(%s,IF(%s,0,IF(OR(%s>%s,AND(%s=%s,%s>%s)),1,0)),0)", played, isSummaryTie, vr, vl, vr, vl, pr, pl))
					tF = append(tF, fmt.Sprintf("IF(%s,IF(%s,1,0),0)", played, isSummaryTie))
					lF = append(lF, fmt.Sprintf("IF(%s,IF(%s,0,IF(OR(%s<%s,AND(%s=%s,%s<%s)),1,0)),0)", played, isSummaryTie, vr, vl, vr, vl, pr, pl))
				}
			}
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols[0], row), joinFormulas(wF)))
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols[1], row), joinFormulas(lF)))
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols[2], row), joinFormulas(tF)))

			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", cols[0], row), fmt.Sprintf("%s%d", cols[2], row), styles.text))
		}

		// One row of spacing between tables
		headerRow = headerRow + len(pool.Players) + 2

		// Table 2: IV, IL, IT, PW, PL
		headers2 := []string{"IV", "IL", "IT", "PW", "PL"}
		cols2 := []string{
			mustColumnName(colNames.startCol + 1), // IV
			mustColumnName(colNames.startCol + 2), // IL
			mustColumnName(colNames.startCol + 3), // IT
			mustColumnName(colNames.startCol + 4), // PW
			mustColumnName(colNames.startCol + 5), // PL
		}
		for i, h := range headers2 {
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", cols2[i], headerRow), h))
		}
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, headerRow), fmt.Sprintf("%s%d", cols2[len(cols2)-1], headerRow), styles.poolHeader))

		for i, player := range pool.Players {
			row := headerRow + 1 + i
			leftSide, _ := getMatchSides(playerRef(&player), "", false)
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", startColName, row), leftSide))

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

				played := fmt.Sprintf("OR(COUNTA(%s%d:%s%d,%s%d:%s%d)>0,%s)", lVCol, rec.row, lPCol, rec.endRow, rPCol, rec.row, rVCol, rec.endRow, isSummaryTie)

				var vT []string
				for r := rec.row; r <= rec.endRow; r++ {
					isSubTie := fmt.Sprintf(`OR(%s%d="X",%s%d="x")`, middleColName, r, middleColName, r)
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
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[0], row), joinFormulas(ivF)))
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[1], row), joinFormulas(ilF)))
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[2], row), joinFormulas(itF)))
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[3], row), joinFormulas(pwF)))
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", cols2[4], row), joinFormulas(plF)))

			handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", cols2[0], row), fmt.Sprintf("%s%d", cols2[4], row), styles.text))
		}
		return headerRow + len(pool.Players)
	}

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", startColName, headerRow), "Results"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lVCol, headerRow), "W"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lPCol, headerRow), "L"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", middleColName, headerRow), "T"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rPCol, headerRow), "PW"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rVCol, headerRow), "PL"))

	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, headerRow), fmt.Sprintf("%s%d", rVCol, headerRow), styles.poolHeader))

	for i, player := range pool.Players {
		row := headerRow + 1 + i

		leftSide, _ := getMatchSides(playerRef(&player), "", false)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", startColName, row), leftSide))

		records := playerMatchRows[&pool.Players[i]]

		var wFormulas, tFormulas, lFormulas, pwFormulas, plFormulas []string
		for _, rec := range records {
			var leftTotal, rightTotal, played string

			if teamMatches > 0 && rec.endRow > 0 {
				continue
			}

			if teamMatches == 0 {
				// INDIVIDUAL MATCHES: Scores are letters (M, K, etc). One letter = one point.
				// We strip out spaces, "0", and "-" so they can be used to mark a 0-0 tie without giving points.
				lc := func(col string, r int) string {
					return fmt.Sprintf(`LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d," ",""),"0",""),"-",""))`, col, r)
				}
				leftTotal = fmt.Sprintf("%s+%s", lc(lVCol, rec.row), lc(lPCol, rec.row))
				rightTotal = fmt.Sprintf("%s+%s", lc(rPCol, rec.row), lc(rVCol, rec.row))

				// COUNTA detects if the match was played, even if they typed "0" or "-"
				// We also consider it played if it's marked as a tie with "X"
				isTie := fmt.Sprintf(`OR(%s%d="X",%s%d="x")`, middleColName, rec.row, middleColName, rec.row)
				played = fmt.Sprintf("OR(COUNTA(%s%d,%s%d,%s%d,%s%d)>0,%s)", lVCol, rec.row, lPCol, rec.row, rPCol, rec.row, rVCol, rec.row, isTie)
			} else {
				// TEAM MATCHES (Summary fallback): If endRow wasn't set, we assume rec.row is a summary row.
				nv := func(col string, r int) string {
					return fmt.Sprintf("IF(ISNUMBER(%s%d),%s%d,0)", col, r, col, r)
				}
				leftTotal = fmt.Sprintf("%s+%s", nv(lVCol, rec.row), nv(lPCol, rec.row))
				rightTotal = fmt.Sprintf("%s+%s", nv(rPCol, rec.row), nv(rVCol, rec.row))

				// Played if at least one of the summary cells has a number typed into it
				played = fmt.Sprintf("OR(ISNUMBER(%s%d),ISNUMBER(%s%d),ISNUMBER(%s%d),ISNUMBER(%s%d))", lVCol, rec.row, lPCol, rec.row, rPCol, rec.row, rVCol, rec.row)
			}

			isTie := fmt.Sprintf(`OR(%s%d="X",%s%d="x")`, middleColName, rec.row, middleColName, rec.row)
			if rec.side == "left" {
				wFormulas = append(wFormulas, fmt.Sprintf("IF(%s,IF(%s,0,(%s>%s)*1),0)", played, isTie, leftTotal, rightTotal))
				tFormulas = append(tFormulas, fmt.Sprintf("IF(%s,IF(%s,1,0),0)", played, isTie))
				lFormulas = append(lFormulas, fmt.Sprintf("IF(%s,IF(%s,0,(%s<%s)*1),0)", played, isTie, leftTotal, rightTotal))
				pwFormulas = append(pwFormulas, leftTotal)
				plFormulas = append(plFormulas, rightTotal)
			} else {
				// Mirror logic for the right side
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

		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, row), fmt.Sprintf("%s%d", rVCol, row), styles.text))
	}
	return headerRow + len(pool.Players)
}

func PrintPoolMatches(f *excelize.File, pools []Pool, teamMatches int, numWinners int, numCourts int, mirror bool) map[string]MatchWinner {
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

				printSinglePool(f, sheetName, pools[poolIdx], startCol, poolRow, teamMatches, numWinners, maxBlocks, colNames, styles, matchWinners, mirror)
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
				if numTeamMatches > 0 {
					startCol = 1 + c*12
				}
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
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, colNames.leftVictoriesColName+fmt.Sprint(matchRow), colNames.rightVictoriesColName+fmt.Sprint(matchRow), styles.unlockedText))

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
		// Use SUMPRODUCT for much shorter and more efficient formulas
		buildWinnersFormula := func(left bool) string {
			mRange := fmt.Sprintf("%s%d:%s%d", middleColName, firstTeamMatchRow, middleColName, lastTeamMatchRow)
			lcL := fmt.Sprintf(`(LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-","")))`, lVCol, firstTeamMatchRow, lVCol, lastTeamMatchRow, lPCol, firstTeamMatchRow, lPCol, lastTeamMatchRow)
			lcR := fmt.Sprintf(`(LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d:%s%d," ",""),"0",""),"-","")))`, rPCol, firstTeamMatchRow, rPCol, lastTeamMatchRow, rVCol, firstTeamMatchRow, rVCol, lastTeamMatchRow)
			if left {
				return fmt.Sprintf(`SUMPRODUCT((UPPER(%s)<>"X")*(%s>%s)*1)`, mRange, lcL, lcR)
			}
			return fmt.Sprintf(`SUMPRODUCT((UPPER(%s)<>"X")*(%s>%s)*1)`, mRange, lcR, lcL)
		}

		// Points Won (PW) for a side
		buildPointsFormula := func(left bool) string {
			rangeA := fmt.Sprintf("%s%d:%s%d", lVCol, firstTeamMatchRow, lVCol, lastTeamMatchRow)
			rangeB := fmt.Sprintf("%s%d:%s%d", lPCol, firstTeamMatchRow, lPCol, lastTeamMatchRow)
			if !left {
				rangeA = fmt.Sprintf("%s%d:%s%d", rPCol, firstTeamMatchRow, rPCol, lastTeamMatchRow)
				rangeB = fmt.Sprintf("%s%d:%s%d", rVCol, firstTeamMatchRow, rVCol, lastTeamMatchRow)
			}
			return fmt.Sprintf(`SUMPRODUCT(LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s," ",""),"0",""),"-",""))+LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s," ",""),"0",""),"-","")))`, rangeA, rangeB)
		}

		// Use formulas to tally victories and points from the individual team sub-match rows.
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lVCol, matchRow), buildWinnersFormula(true)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lPCol, matchRow), buildPointsFormula(true)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rVCol, matchRow), buildWinnersFormula(false)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rPCol, matchRow), buildPointsFormula(false)))

		// Unlock final victory/points summary cells
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", lVCol, matchRow), fmt.Sprintf("%s%d", lPCol, matchRow), styles.unlockedText))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", rVCol, matchRow), fmt.Sprintf("%s%d", rPCol, matchRow), styles.unlockedText))
		matchRow++

		startCell = startColName + fmt.Sprint(matchRow)
		endCell = endColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.text))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, "Victories / Points"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, endCell, "Victories / Points"))
		matchRow++ // Add space after team match summary
	}

	matchRow += 2 // Spacing before result marking
	resultCol := rightVictoriesColName
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "1."))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), styles.unlockedBorderBottom))

	// Gathering the match winners for the following rounds
	matchWinners[fmt.Sprintf("M %d", eliminationMatch.matchNum)] = MatchWinner{
		sheetName: sheetName,
		cell:      fmt.Sprintf("%s%d", endColName, matchRow),
	}

	matchRow++
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resultCol, matchRow), "2."))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", endColName, matchRow), fmt.Sprintf("%s%d", endColName, matchRow), styles.unlockedBorderBottom))
}

func setMatchColumnsWidthByStartCol(f *excelize.File, sheetName string, startCol int) {
	startColName := mustColumnName(startCol)
	bCol := mustColumnName(startCol + 1)
	cCol := mustColumnName(startCol + 2)
	middleColName := mustColumnName(startCol + 3)
	eCol := mustColumnName(startCol + 4)
	fCol := mustColumnName(startCol + 5)
	endColName := mustColumnName(startCol + 6)
	gCol := mustColumnName(startCol + 7)
	iCol := mustColumnName(startCol + 9)
	jCol := mustColumnName(startCol + 10)
	gapCol := mustColumnName(startCol + 11)

	handleExcelError("SetColWidth", f.SetColWidth(sheetName, startColName, startColName, 30))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, bCol, cCol, 5))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, middleColName, middleColName, 3))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, eCol, fCol, 5))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, endColName, endColName, 30))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, gCol, iCol, 5))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, jCol, jCol, 30))
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

type nameEntry struct {
	player      Player
	fallbackTag interface{}
}

func courtSheetName(courtIdx int) string {
	return fmt.Sprintf("%s %s", SheetNamesToPrint, string("ABCDEFGHIJKLMNOPQRSTUVWXYZ"[courtIdx]))
}

func CreateNamesToPrint(f *excelize.File, players []Player, sanitized bool, numCourts int) {
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
		printNameEntries(f, sheetName, entries, sanitized)
	}

	if err := f.DeleteSheet(SheetNamesToPrint); err != nil {
		fmt.Fprintf(os.Stderr, "Note: %s sheet might not exist: %v\n", SheetNamesToPrint, err)
	}
}

func CreateNamesWithPoolToPrint(f *excelize.File, pools []Pool, sanitized bool, numCourts int) {
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
		printNameEntries(f, sheetName, entriesByCourt[c], sanitized)
	}

	if err := f.DeleteSheet(SheetNamesToPrint); err != nil {
		fmt.Fprintf(os.Stderr, "Note: %s sheet might not exist: %v\n", SheetNamesToPrint, err)
	}
}

func printNameEntries(f *excelize.File, sheetName string, entries []nameEntry, sanitized bool) {
	setupNamesToPrintLayout(f, sheetName)
	nameIDPositionStyle := getNameIDPositionStyle(f)
	nameIDStyle := getNameIDStyle(f)

	for i, entry := range entries {
		row := i + 1
		positionCell := fmt.Sprintf("A%d", row)
		nameCell := fmt.Sprintf("B%d", row)
		handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 270))

		if entry.player.numberCell != "" {
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, positionCell, sheetRef(entry.player.sheetName, entry.player.numberCell)))
		} else {
			handleExcelError("SetCellValue", f.SetCellValue(sheetName, positionCell, entry.fallbackTag))
		}
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, positionCell, positionCell, nameIDPositionStyle))

		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, nameCell, buildNameFormula(entry.player, sanitized)))
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
