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
// CHK037, Kachinuki Excel rendering decision (T160 + T195–T203):
//
// The main Pool Matches / Elimination Matches sheets continue to use the
// 8-column-per-court layout invariant (CourtsColumnsPerCourt = 8, see
// constants.go and CLAUDE.md). Variable-bout kachinuki grids would either
// overflow that budget or force a layout-mode switch the rest of the
// workbook can't accommodate, so the main sheets carry the team-match
// row only.
//
// Bout-by-bout detail is rendered on a separate "Kachinuki Detail" sheet
// (helper.SheetKachinukiDetail). See internal/helper/excel_kachinuki.go,
// the sheet uses a flexible 8-column layout (NOT bound by
// CourtsColumnsPerCourt) and is opt-in: the engine export path
// (internal/engine/export.go → collectKachinukiMatches) emits it only
// when comp.TeamMatchType == kachinuki AND at least one match carries
// bouts. CLI export paths (cmd/create-pools.go, create-playoffs.go) are
// kachinuki-agnostic and produce zero changes to existing example files.
package helper

import (
	"errors"
	"fmt"
	"os"
	"slices"
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

// bandOrder is the single rule for which shiaijo a court-banded sheet prints and
// in what order: the competition's OWN order for the shiaijo that are used, then
// any shiaijo used but not allocated, in the order they were first seen. Never
// empty -- a sheet with no bands has nowhere to print.
//
// Both banded sheets go through it. They used to state the rule separately and
// had already drifted: with the pool clamp reducing the allocated set, the pool
// sheet ordered two reassigned shiaijo by when it met them and the elimination
// sheet by the competition's order, so one workbook printed the same two shiaijo
// as [D C] and [C D].
func bandOrder(courts []string, usedInFirstSeenOrder []string) []string {
	used := make(map[string]bool, len(usedInFirstSeenOrder))
	var extra []string
	for _, name := range usedInFirstSeenOrder {
		if name == "" || used[name] {
			continue
		}
		used[name] = true
		if !slices.Contains(courts, name) {
			extra = append(extra, name)
		}
	}
	var bands []string
	for _, name := range courts {
		if used[name] {
			bands = append(bands, name)
		}
	}
	bands = append(bands, extra...)
	if len(bands) == 0 {
		return []string{courtNameAt(courts, 0)}
	}
	return bands
}

// CourtPlan is where a sheet's bouts actually run: the shiaijo the competition
// uses, and the live court of anything the operator has moved.
//
// It travels as one value because these four facts are always assembled
// together and always used together. Threading them positionally is how the
// bronze went wrong: adding it meant inserting a bare string into an
// eleven-argument call in four files, next to another string and three bools,
// and the compiler could not tell a court from a flag.
type CourtPlan struct {
	// Draw is the fallback: the region owning a bout, for anything with no
	// live court recorded. The CLI has only this.
	Draw *KnockoutDraw
	// Courts is the competition's own shiaijo, in its own order. Band names
	// come from here, never from a band's position.
	Courts []string
	// ByMatch is the live court per match number, read off the stored bracket.
	// It wins over Draw: a match's court is data the operator reassigns.
	ByMatch map[int64]string
	// Bronze is the 3rd-place bout's court. It needs its own field because that
	// bout carries no match number and so cannot ride ByMatch.
	Bronze string
}

// CourtOf is the shiaijo ONE bout runs on, resolved from the strongest source
// available: the live court the operator recorded, else the draw region owning
// it, named from the competition's own courts.
//
// This precedence is the whole point of the type and it is stated here alone.
// A match's court is data the operator reassigns (UpdateMatchCourt), so nothing
// derived may outrank it; the region is what the CLI has (no stored bracket) and
// what a freshly drawn competition resolves to anyway.
//
// drawn is NodeCourts' output, passed in rather than recomputed because it walks
// the entire draw and every caller resolves many nodes.
func (p CourtPlan) CourtOf(n *Node, drawn map[*Node]int) string {
	if c, ok := p.ByMatch[n.matchNum]; ok && c != "" {
		return c
	}
	return courtNameAt(p.Courts, drawn[n])
}

// PageCourt is the one shiaijo an entire tree page has been MOVED to, or "" when
// there is no such answer -- which is the normal case and means "use the page's
// drawn region".
//
// A tree page is a wall chart for a whole bracket region, so unlike CourtOf it
// cannot just take the strongest per-bout source: a page whose bouts sit on two
// shiaijo has no single court to be titled after. Same rule the pool sheet
// applies to a pool split across courts (PoolCourtByName): report a move only
// when every bout agrees on it, and otherwise leave the block on the shiaijo it
// was drawn for rather than filing it somewhere half its bouts are not.
//
// EVERY junction on the page must have a recorded court, not just the ones that
// happen to be in ByMatch. A page where one bout is known to be on A and the
// rest are unknown is not a page that moved to A, and titling it so asserts more
// than the bracket says -- one bout's worth of evidence stands in for a whole
// region's wall chart. Requiring full coverage means the only thing an
// incomplete record can do is leave the drawn title in place, which is where
// this started; a partial one could actively mislead.
//
// So this is inert by construction for the CLI (nil ByMatch) and for any
// competition nobody has reassigned, because a freshly drawn bracket records
// each bout's court AS its region's -- the title only changes once every bout on
// the page has genuinely been moved to the same other shiaijo.
func (p CourtPlan) PageCourt(subtree *Node) string {
	if subtree == nil || len(p.ByMatch) == 0 {
		return ""
	}
	agreed := ""
	unresolved := false
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil || unresolved {
			return
		}
		// Junctions only: a leaf is an entrant slot, not a bout, and carries no
		// match number to look up.
		if n.Left != nil || n.Right != nil {
			c := p.ByMatch[n.matchNum]
			switch {
			case c == "", agreed != "" && agreed != c:
				unresolved = true
				return
			default:
				agreed = c
			}
		}
		walk(n.Left)
		walk(n.Right)
	}
	walk(subtree)
	if unresolved {
		return ""
	}
	return agreed
}

// PoolsByCourt returns the BANDS a pool-phase sheet prints, and the pool indices
// in each. It is the single owner of that grouping: the Pool Matches skeleton,
// the per-shiaijo roster sheets and the results overlays all lay rows out in
// this order, so a second derivation of it would write scores into the wrong
// pool's block. Its length is the authoritative band count -- callers must not
// re-derive one.
//
// The default allocation is AssignPoolsToCourts, the contiguous one the draw and
// the schedule use, so each band holds the pools that court actually ran.
//
// courtOfPool overrides it with where a pool's matches are ACTUALLY being
// fought, for callers that have the live schedule (the exports do; the CLI
// generates a blank workbook and passes nil). A pool whose matches an operator
// moved prints in that shiaijo's band, because the band is what its operator
// scores off. A pool is only moved when its matches AGREE on a court: a pool
// split across shiaijo has no single band to be in, so it keeps its drawn one
// rather than being filed somewhere half its bouts are not.
//
// A pool moved to a shiaijo OUTSIDE the drawn allocation gets a band of its own,
// appended after them. That matches what the elimination sheet does with a
// reassigned bout (usedCourtBands): the alternative -- silently filing the block
// back on its drawn shiaijo -- hands one operator a score sheet for a pool they
// are not running and leaves the operator who IS running it with none.
func PoolsByCourt(pools []Pool, courts []string, courtOfPool map[string]string) ([]string, [][]int) {
	numCourts := EffectiveDrawCourts(len(pools), len(courts))

	// Where each pool is actually fought, in pool order. AssignPoolsToCourts
	// never returns an index >= numCourts, so courtNameAt reads the same name
	// off the full list that it would off a numCourts-long prefix of it.
	assignments, _ := AssignPoolsToCourts(len(pools), numCourts)
	courtOf := make([]string, len(pools))
	for i := range pools {
		courtOf[i] = courtNameAt(courts, assignments[i])
		if live, ok := courtOfPool[pools[i].PoolName]; ok && live != "" {
			courtOf[i] = live
		}
	}

	// bandOrder owns which of those get a band and in what order, so this sheet
	// and the elimination sheet cannot order the same shiaijo differently. A
	// band nothing landed in is dropped: its header, page break and print-area
	// column would send an operator to a court running nothing.
	//
	// It is given the competition's FULL court list, not the clamped one. The
	// clamp decides how many bands the default allocation spreads pools over; it
	// must not decide ORDER, or a shiaijo the competition owns but the clamp
	// dropped counts as an unallocated extra here and as an allocated court on
	// the elimination sheet -- which is how the same two shiaijo printed as
	// [D C] on one sheet and [C D] on the other.
	bands := bandOrder(courts, courtOf)
	index := make(map[string]int, len(bands))
	for i, name := range bands {
		index[name] = i
	}
	out := make([][]int, len(bands))
	for i := range pools {
		out[index[courtOf[i]]] = append(out[index[courtOf[i]]], i)
	}
	return bands, out
}

func writeCourtHeaders(f *excelize.File, sheetName string, courts []string, headerStyle int) {
	numCourts := clampCourts(len(courts))
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
		courtLabel := ShiaijoLabel(courtNameAt(courts, c))
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

// strippedLen returns the formula expression
// LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(<col><row>," ",""),"0",""),"-","")), i.e.
// the character count of one cell after stripping spaces, zeros, and dashes.
// Used by the team-summary helpers below, kept as single-cell expressions
// because passing a range to SUBSTITUTE/LEN does not natively iterate as an
// array in legacy Excel, Google Sheets, or Apple Numbers; only Excel 365 with
// dynamic-array semantics evaluates the array form correctly.
func strippedLen(col string, row int) string {
	return fmt.Sprintf(`LEN(SUBSTITUTE(SUBSTITUTE(SUBSTITUTE(%s%d," ",""),"0",""),"-",""))`, col, row)
}

// buildTeamWinnersFormula returns an Excel/Sheets/Numbers-compatible formula
// counting sub-match wins for one side across the row range [startRow, endRow].
// middleCol is the "vs/X" column; left=true counts the left side's wins.
//
// Each sub-match row contributes one IF clause; the row is skipped when the
// middle column is "X" (overall tie marker) and otherwise counts a win when
// the side's stripped-LEN total is strictly greater than the opponent's.
// We avoid SUMPRODUCT-over-range-with-SUBSTITUTE because that collapses to
// the first cell outside of Excel 365 dynamic arrays.
func buildTeamWinnersFormula(middleCol, lVCol, lPCol, rVCol, rPCol string, startRow, endRow int, left bool) string {
	parts := make([]string, 0, endRow-startRow+1)
	for r := startRow; r <= endRow; r++ {
		leftTotal := fmt.Sprintf("(%s+%s)", strippedLen(lVCol, r), strippedLen(lPCol, r))
		rightTotal := fmt.Sprintf("(%s+%s)", strippedLen(rPCol, r), strippedLen(rVCol, r))
		win := fmt.Sprintf("%s>%s", leftTotal, rightTotal)
		if !left {
			win = fmt.Sprintf("%s>%s", rightTotal, leftTotal)
		}
		// Skip the row if it's marked X (tie); otherwise count a win.
		parts = append(parts, fmt.Sprintf(`IF(UPPER(%s%d)="X",0,IF(%s,1,0))`, middleCol, r, win))
	}
	return strings.Join(parts, "+")
}

// buildTeamPointsFormula returns an Excel/Sheets/Numbers-compatible formula
// summing the point-character count for one side across [startRow, endRow].
// left=true sums the left side (lVCol+lPCol); false sums the right (rPCol+rVCol).
//
// As with buildTeamWinnersFormula, each cell is wrapped in its own
// LEN(SUBSTITUTE(...)) expression, passing a range to SUBSTITUTE/LEN inside
// SUMPRODUCT only iterates in Excel 365 dynamic arrays; Google Sheets and
// Apple Numbers collapse it to the first element.
func buildTeamPointsFormula(lVCol, lPCol, rVCol, rPCol string, startRow, endRow int, left bool) string {
	colA, colB := lVCol, lPCol
	if !left {
		colA, colB = rPCol, rVCol
	}
	parts := make([]string, 0, endRow-startRow+1)
	for r := startRow; r <= endRow; r++ {
		parts = append(parts, fmt.Sprintf("%s+%s", strippedLen(colA, r), strippedLen(colB, r)))
	}
	return strings.Join(parts, "+")
}

func printSinglePool(f *excelize.File, sheetName string, pool Pool, startCol int, startRow int, teamMatches int, numWinners int, maxBlocks []int, colNames matchColumnNames, styles matchStyles, matchWinners map[string]MatchWinner, mirror bool, poolCoords map[string]cellCoord, pCoords map[string]playerCellCoord, engi bool) {
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
		matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, styles.redHeader, styles.text, styles.whiteHeader, mirror, engi)
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
				matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, styles.redHeader, styles.text, styles.whiteHeader, mirror, engi)
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
	poolRow = printPoolResultsTable(f, sheetName, pool, resultsTableStart, colNames, playerMatchRows, styles, mirror, teamMatches, pCoords, engi)
	poolRow++

	resLabelColName := mustColumnName(colNames.startCol + 5) // F
	resNameColName := mustColumnName(colNames.startCol + 6)  // G

	poolRow += 2 // blank row + extra space before ranking
	resultsDataStart := resultsTableStart + 1
	resultsDataEnd := resultsTableStart + len(pool.Players)
	if teamMatches > 0 {
		resultsDataEnd = resultsTableStart + (len(pool.Players) * 2) + 2
	}

	nameRange := fmt.Sprintf("$%s$%d:$%s$%d", colNames.startColName, resultsDataStart, colNames.startColName, resultsDataEnd)
	rankRange := fmt.Sprintf("$%s$%d:$%s$%d", resNameColName, resultsDataStart, resNameColName, resultsDataEnd)

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resNameColName, poolRow), "Ranking"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", resLabelColName, poolRow), fmt.Sprintf("%s%d", resNameColName, poolRow), styles.poolHeader))

	rankingFirstRow := poolRow + 1
	for i := range pool.Players {
		rankNum := i + 1
		rankRow := rankingFirstRow + i
		label := fmt.Sprintf("%d.", rankNum)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", resLabelColName, rankRow), label))

		// Formula to find the name of the player with this rank:
		formula := fmt.Sprintf("IFERROR(INDEX(%s, MATCH(%d, %s, 0)), \"-\")", nameRange, rankNum, rankRange)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", resNameColName, rankRow), formula))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", resNameColName, rankRow), fmt.Sprintf("%s%d", resNameColName, rankRow), styles.unlockedBorderBottom))

		// Elimination-bracket formulas (Pool X-1st, -2nd, ...) reference
		// the rank-N IFERROR(INDEX(...MATCH(N,...))) cell directly so the
		// resolved player name flows into the tree.
		if rankNum <= numWinners {
			matchWinners[fmt.Sprintf("%s-%s", pool.PoolName, GetOrdinal(rankNum))] = MatchWinner{
				cellCoord: cellCoord{sheetName: sheetName, cell: fmt.Sprintf("%s%d", resNameColName, rankRow)},
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
	engi            bool
}

// playerNameFormulaFor returns the name-cell formula for a results-table row.
// Engi pair names need no special handling: both member names live combined in
// Player.Name ("Name 1 - Name 2"), so the plain reference shows the full pair.
func (ctx poolResultsCtx) playerNameFormulaFor(player Player) string {
	left, _ := getMatchSides(playerRef(player.Name, ctx.pCoords[playerCoordKey(player)]), "", false)
	return left
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
		playerNameFormula := ctx.playerNameFormulaFor(player)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", startColName, row), playerNameFormula))
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
		playerNameFormula := ctx.playerNameFormulaFor(player)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", startColName, row2), playerNameFormula))

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
		// No leading '=', OOXML <f> elements store the formula body
		// only. Excel tolerates the prefix, but Google Sheets and Apple
		// Numbers reject it and produce #ERROR! / blank cells.
		scoreFormula := fmt.Sprintf("(%s%d*1000000000)-(%s%d*10000000)+(%s%d*100000)+(%s%d*1000)-(%s%d*100)+(%s%d*10)+(%s%d*1)-(%s%d*0.01)",
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
	if ctx.engi {
		// Engi standings: W / Flags / Rank only (no L, T, PW, PL).
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", middleColName, headerRow), ColHeaderFlags))
	} else {
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lPCol, headerRow), "L"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", middleColName, headerRow), "T"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rPCol, headerRow), "PW"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rVCol, headerRow), "PL"))
	}
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rankCol, headerRow), "Rank"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", startColName, headerRow), fmt.Sprintf("%s%d", rankCol, headerRow), styles.poolHeader))

	scoreRange := fmt.Sprintf("$%s$%d:$%s$%d", scoreCol, headerRow+1, scoreCol, headerRow+len(pool.Players))

	for i, player := range pool.Players {
		row := headerRow + 1 + i
		playerNameFormula := ctx.playerNameFormulaFor(player)
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", startColName, row), playerNameFormula))

		records := playerMatchRows[&pool.Players[i]]
		var wFormulas, middleColFormulas, lFormulas, pwFormulas, plFormulas []string
		for _, rec := range records {
			var leftTotal, rightTotal, played string
			if teamMatches > 0 && rec.endRow > 0 {
				continue
			}
			if ctx.engi {
				// Engi: referee flag totals are integers; ties are impossible
				// (flag counts are always odd). played is true when either
				// input cell holds a number.
				//
				// We coerce every operand with N() rather than comparing raw
				// cells or wrapping in IF(ISNUMBER(...),cell,0). Two reasons,
				// both verified against excelize CalcCellValue:
				//   1. Robustness: a raw cell comparison lets stray text
				//      compare greater-than a number (Excel orders text above
				//      numbers), so N(B)>N(F) is needed to treat non-numeric
				//      cells as 0 flags. N(number)=number, N(text)=0, N(empty)=0.
				//   2. Evaluability: excelize returns 0 for the specific
				//      nesting IF(OR(...),(IF(...)>IF(...))*1,0), so the
				//      IF(ISNUMBER(...)) wrapper form mis-evaluates here.
				//      N() coercion inside the guarded comparison evaluates
				//      correctly. N() is ISO-standard and portable to
				//      Excel/LibreOffice/Sheets/Numbers.
				var myCol, oppCol string
				if rec.side == "left" {
					myCol, oppCol = lVCol, rVCol
				} else {
					myCol, oppCol = rVCol, lVCol
				}
				engiPlayed := fmt.Sprintf("OR(ISNUMBER(%s%d),ISNUMBER(%s%d))", lVCol, rec.row, rVCol, rec.row)
				wFormulas = append(wFormulas, fmt.Sprintf("IF(%s,(N(%s%d)>N(%s%d))*1,0)", engiPlayed, myCol, rec.row, oppCol, rec.row))
				// Losses are not recorded for engi: lFormulas intentionally left empty.
				middleColFormulas = append(middleColFormulas, fmt.Sprintf("N(%s%d)", myCol, rec.row)) // middleColFormulas = Flags column for engi
				continue                                                                              // lFormulas, pwFormulas and plFormulas remain empty for engi
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
				middleColFormulas = append(middleColFormulas, fmt.Sprintf("IF(%s,IF(%s,1,0),0)", played, isTie))
				lFormulas = append(lFormulas, fmt.Sprintf("IF(%s,IF(%s,0,(%s<%s)*1),0)", played, isTie, leftTotal, rightTotal))
				pwFormulas = append(pwFormulas, leftTotal)
				plFormulas = append(plFormulas, rightTotal)
			} else {
				wFormulas = append(wFormulas, fmt.Sprintf("IF(%s,IF(%s,0,(%s>%s)*1),0)", played, isTie, rightTotal, leftTotal))
				middleColFormulas = append(middleColFormulas, fmt.Sprintf("IF(%s,IF(%s,1,0),0)", played, isTie))
				lFormulas = append(lFormulas, fmt.Sprintf("IF(%s,IF(%s,0,(%s<%s)*1),0)", played, isTie, rightTotal, leftTotal))
				pwFormulas = append(pwFormulas, rightTotal)
				plFormulas = append(plFormulas, leftTotal)
			}
		}
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lVCol, row), joinFormulas(wFormulas)))
		if !ctx.engi {
			// Non-engi: write the L formula. Engi omits the L column entirely (losses are not recorded).
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lPCol, row), joinFormulas(lFormulas)))
		}
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", middleColName, row), joinFormulas(middleColFormulas)))
		if !ctx.engi {
			// Engi has no PW/PL concept; leave those cells blank.
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rPCol, row), joinFormulas(pwFormulas)))
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rVCol, row), joinFormulas(plFormulas)))
		}

		// Weighted Score formula.
		// No leading '=', see the matching note in the team-results
		// scoreFormula above. Google Sheets and Apple Numbers reject
		// OOXML <f> bodies that begin with '='.
		var scoreFormula string
		if ctx.engi {
			// Engi: rank by wins first, then accumulated flag total.
			scoreFormula = fmt.Sprintf("(%s%d*1000000)+(%s%d)", lVCol, row, middleColName, row)
		} else {
			scoreFormula = fmt.Sprintf("(%s%d*1000000)-(%s%d*10000)+(%s%d*100)+(%s%d*1)-(%s%d*0.01)",
				lVCol, row, lPCol, row, middleColName, row, rPCol, row, rVCol, row)
		}
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

func printPoolResultsTable(f *excelize.File, sheetName string, pool Pool, startRow int, colNames matchColumnNames, playerMatchRows map[*Player][]playerMatchRecord, styles matchStyles, mirror bool, teamMatches int, pCoords map[string]playerCellCoord, engi bool) int {
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
		engi:            engi,
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

// PrintPoolMatches lays the Pool Matches sheet: one 8-column band per shiaijo,
// each court's pools stacked down its band in AssignPoolsToCourts order. It
// returns the per-pool winner cells the elimination sheet links to.
//
// numCourts is the operator's shiaijo ALLOCATION. The sheet is banded on the
// count the POOL PHASE actually runs on, which this function derives itself via
// EffectiveDrawCourts rather than trusting the caller: a court with no home pool
// would own an empty band, so the draw never allocates more courts than pools
// and neither may the sheet. Worked example: 12 competitors at pool size 4 in
// "max" mode on 4 shiaijo gives 3 pools, which the live app runs as pools A+B on
// shiaijo A and pool C on shiaijo B. Banding on the raw 4 spread those three
// pools one per court over A, B and C and printed a fourth, entirely EMPTY
// "Shiaijo D" band, sending operators to a shiaijo nothing was scheduled on --
// inside a workbook whose own tree pages were titled only Shiaijo A and B.
//
// The clamp lives HERE, not at the call sites, because it is a contract between
// this skeleton and everything that writes onto it. A consumer that disagrees
// with the count used here writes every score and standing into the wrong cells,
// so PrintPoolMatches RETURNS the per-band pool indices it actually laid out and
// the overlays fill in against those. They used to call PoolsByCourt
// again from the caller, which is deterministic and so agreed -- but only for as
// long as both calls were handed identical arguments, with nothing enforcing it
// and a comment one file over already claiming the grouping was "computed ONCE
// and handed to every overlay". The elimination sibling returns its bands for the
// same reason. CreateNamesWithPoolToPrint deliberately does NOT take them: it
// writes separate sheets rather than onto this skeleton, so re-deriving from the
// same pools is safe there and keeps a call site from handing it a grouping that
// disagrees. EffectiveDrawCourts is idempotent, so a caller that already clamped
// (cmd/create-pools.go) is unaffected.
func PrintPoolMatches(f *excelize.File, pools []Pool, teamMatches int, numWinners int, courts []string, courtOfPool map[string]string, mirror bool, poolCoords map[string]cellCoord, pCoords map[string]playerCellCoord, engi bool) (map[string]MatchWinner, [][]int) {
	// PoolsByCourt owns the clamp AND the band names, so the headers this writes
	// and the blocks it fills can never come from two different answers. The
	// clamp keeps the FIRST n of the competition's shiaijo, so a competition on
	// C, D, E whose pools fill two bands is banded "Shiaijo C" and "Shiaijo D"
	// -- never renamed to A and B.
	courts, poolsByCourt := PoolsByCourt(pools, courts, courtOfPool)
	numCourts := len(courts)

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

	writeCourtHeaders(f, sheetName, courts, styles.poolHeader)

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

				printSinglePool(f, sheetName, pools[poolIdx], startCol, poolRow, teamMatches, numWinners, maxBlocks, colNames, styles, matchWinners, mirror, poolCoords, pCoords, engi)
			}
		}

		poolRow += totalPoolHeight
	}

	SetEliminationPrintArea(f, sheetName, numCourts, poolRow-1)

	// Vertical page breaks before each court except the first
	for c := 1; c < numCourts; c++ {
		courtStartCol := 1 + c*CourtsColumnsPerCourt
		colName := mustColumnName(courtStartCol)
		handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, colName+"1"))
	}

	SetSheetLayoutPortraitA4DownThenOver(f, sheetName, numCourts)

	return matchWinners, poolsByCourt
}

func poolEntryWithStyle(startColName string, poolRow int, endColName string, f *excelize.File, sheetName string, leftSide string, rightSide string, textStyle int) {
	startCell := startColName + fmt.Sprint(poolRow)
	endCell := endColName + fmt.Sprint(poolRow)
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, startCell, leftSide))
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, endCell, rightSide))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, textStyle))
}

func MatchHeader(f *excelize.File, sheetName string, startColName string, poolRow int, middleColName string, endColName string, mirror bool, engi bool) {
	matchHeaderWithStyles(f, sheetName, startColName, poolRow, middleColName, endColName, getRedHeaderStyle(f), getTextStyle(f), getWhiteHeaderStyle(f), mirror, engi)
}

func matchHeaderWithStyles(f *excelize.File, sheetName string, startColName string, poolRow int, middleColName string, endColName string, redHeaderStyle int, textStyle int, whiteHeaderStyle int, mirror bool, engi bool) {
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

	if engi {
		startNum, _ := excelize.ColumnNameToNumber(startColName)
		lVColName := mustColumnName(startNum + 1)
		rVColName := mustColumnName(startNum + 5)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lVColName, poolRow), "Fl"))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", lVColName, poolRow), fmt.Sprintf("%s%d", lVColName, poolRow), textStyle))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rVColName, poolRow), "Fl"))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, fmt.Sprintf("%s%d", rVColName, poolRow), fmt.Sprintf("%s%d", rVColName, poolRow), textStyle))
	}
}

// SetEliminationPrintArea sets (or updates) the _xlnm.Print_Area defined name
// for sheetName so that the printed range is $A$1:$<maxCol>$lastRow, where
// maxCol is derived from numCourts using the same formula as
// PrintTeamEliminationMatches. It is idempotent: if the defined name already
// exists for the sheet it is deleted first, so it can be called after
// PrintThirdPlaceBlock to extend the print area to include the bronze block.
func SetEliminationPrintArea(f *excelize.File, sheetName string, numCourts, lastRow int) {
	numCourts = clampCourts(numCourts)
	lastCourtStartCol := 1 + (numCourts-1)*CourtsColumnsPerCourt
	SetPrintArea(f, sheetName, lastCourtStartCol+7, lastRow)
}

// SetPrintArea sets (or replaces) the _xlnm.Print_Area defined name for
// sheetName so the printed range is $A$1:$<lastCol>$<lastRow>. Without it a
// sheet prints its whole used range, and any styled-but-empty cell to the right
// of the content (a merged title band, a pre-sized column) spills a near-blank
// extra page. It is idempotent: an existing definition is removed first, so
// callers can call it again to extend the range.
func SetPrintArea(f *excelize.File, sheetName string, lastCol, lastRow int) {
	// DeleteDefinedName returns ErrDefinedNameScope when the name is not found;
	// that is expected on the first call, so only surface other errors.
	if err := f.DeleteDefinedName(&excelize.DefinedName{
		Name:  "_xlnm.Print_Area",
		Scope: sheetName,
	}); err != nil && !errors.Is(err, excelize.ErrDefinedNameScope) {
		handleExcelError("DeleteDefinedName", err)
	}

	printArea := fmt.Sprintf("'%s'!$A$1:$%s$%d", sheetName, mustColumnName(lastCol), lastRow)
	handleExcelError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
		Name:     "_xlnm.Print_Area",
		RefersTo: printArea,
		Scope:    sheetName,
	}))
}

// PrintTeamEliminationMatches renders all elimination match blocks onto the
// Elimination Matches sheet and returns the next available start row (the row
// immediately after the last rendered block plus any trailing space lines).
// Callers that do not need the return values may ignore them.
func PrintTeamEliminationMatches(f *excelize.File, poolMatchWinners map[string]MatchWinner, eliminationMatchRounds [][]*Node, numTeamMatches int, plan CourtPlan, mirror bool, engi bool) (int, []string, map[string]MatchWinner) {
	// This sheet is a per-shiaijo handout: one band, one page break and one
	// "Shiaijo X" header per court. A bout printed under the wrong band sends
	// its competitors to a shiaijo the app is not calling them to, so the band
	// is CourtPlan.CourtOf's answer -- live court over drawn region -- rather
	// than a precedence chain restated here.
	//
	// The bands are then the shiaijo actually USED, in the competition's own
	// order. Deriving the band count from the draw's region count instead would
	// leave a reassigned bout with no band to go in, and would print a band for
	// a shiaijo nothing is scheduled on -- the same defect PrintPoolMatches
	// already refuses to produce.
	courtOfNode := plan.Draw.NodeCourts()
	bandOf := func(n *Node) string { return plan.CourtOf(n, courtOfNode) }
	bands := usedCourtBands(eliminationMatchRounds, plan.Courts, plan.Bronze, bandOf)
	numCourts := len(bands)
	bandIndex := make(map[string]int, numCourts)
	for i, name := range bands {
		bandIndex[name] = i
	}

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

	writeCourtHeaders(f, sheetName, bands, styles.poolHeader)

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

		// File each bout under the shiaijo whose region owns it. A region may
		// hold more bouts than its neighbour (unequal regions are the whole
		// point of the court-first draw), so the bands are ragged and a band
		// can legitimately be empty in a given round.
		matchesByCourt := make([][]*Node, numCourts)
		numMatchRows := 0
		for _, eliminationMatch := range eliminationMatchRound {
			// Same nil skip as usedCourtBands above: the two walks read the
			// same caller-supplied rounds and must agree on what is in them.
			if eliminationMatch == nil {
				continue
			}
			c, ok := bandIndex[bandOf(eliminationMatch)]
			if !ok {
				c = 0
			}
			matchesByCourt[c] = append(matchesByCourt[c], eliminationMatch)
			numMatchRows = max(numMatchRows, len(matchesByCourt[c]))
		}

		for r := 0; r < numMatchRows; r++ {
			// Check for page break BEFORE starting a match row
			if rowsSinceLastPageBreak+matchHeight > rowsPerPageLimit {
				handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", startRow)))
				rowsSinceLastPageBreak = 0
			}

			for c := 0; c < numCourts; c++ {
				if r >= len(matchesByCourt[c]) {
					continue
				}

				eliminationMatch := matchesByCourt[c][r]
				startCol := 1 + c*CourtsColumnsPerCourt
				colNames, ok := colNamesByStartCol[startCol]
				if !ok {
					colNames = buildMatchColumnNames(startCol)
					colNamesByStartCol[startCol] = colNames
				}

				printSingleEliminationMatch(f, sheetName, eliminationMatch, poolMatchWinners, matchWinners, colNames, startRow, round, numTeamMatches, styles, mirror, engi)
			}
			startRow += matchHeight
			rowsSinceLastPageBreak += matchHeight
		}
		startRow += spaceLines
		rowsSinceLastPageBreak += spaceLines
	}

	SetEliminationPrintArea(f, sheetName, numCourts, startRow-1)

	// Vertical page breaks before each court except the first
	for c := 1; c < numCourts; c++ {
		courtStartCol := 1 + c*CourtsColumnsPerCourt
		colName := mustColumnName(courtStartCol)
		handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, colName+"1"))
	}

	SetSheetLayoutPortraitA4DownThenOver(f, sheetName, numCourts)
	return startRow, bands, matchWinners
}

// loserCellOf returns the Excel cell address one row below the given "1." winner
// cell, which is the "2." loser line of a single-elimination match block.
func loserCellOf(winnerCell string) (string, error) {
	col, row, err := excelize.SplitCellName(winnerCell)
	if err != nil {
		return "", err
	}
	return excelize.JoinCellName(col, row+1)
}

// bronzeEntrantFormulas derives CONCATENATE formula strings for the two bronze
// entrant cells from the recorded matchWinners. semiA/semiB are the match numbers
// of the two semifinals (0 = absent/bye, skipped). Returns empty strings for any
// entry that cannot be resolved; callers guard with a non-empty check before
// calling SetCellFormula.
func bronzeEntrantFormulas(sheetName string, semiA, semiB int, matchWinners map[string]MatchWinner) (sideAFormula, sideBFormula string) {
	build := func(semiN int) string {
		if semiN == 0 || matchWinners == nil {
			return ""
		}
		key := fmt.Sprintf("M %d", semiN)
		mw, ok := matchWinners[key]
		if !ok || mw.cell == "" {
			return ""
		}
		loserCell, err := loserCellOf(mw.cell)
		if err != nil {
			return ""
		}
		if mw.sheetName == sheetName || mw.sheetName == "" {
			return fmt.Sprintf("CONCATENATE(\"%s \",%s)", key, loserCell)
		}
		return fmt.Sprintf("CONCATENATE(\"%s \",'%s'!%s)", key, mw.sheetName, loserCell)
	}
	return build(semiA), build(semiB)
}

// printTeamMatchBlock writes the numbered team sub-match rows and, when
// numTeamMatches > 0, the IV/PW team summary rows. Shared by regular
// elimination matches and the bronze (3rd place) block so the team layout has
// a single authority. leftFormula/rightFormula, when non-empty, are re-written
// on the summary row so the team names repeat next to the totals (regular
// matches always have both; the bronze block passes "" because its entrant
// formulas may be blank). Returns the row after the block (unchanged for
// individual matches, where the loop and summary are skipped entirely).
func printTeamMatchBlock(f *excelize.File, sheetName string, colNames matchColumnNames, styles matchStyles, matchRow, numTeamMatches int, leftFormula, rightFormula string) int {
	startColName := colNames.startColName
	endColName := colNames.endColName
	middleColName := colNames.middleColName

	firstTeamRow := matchRow + 1
	for i := 0; i < numTeamMatches; i++ {
		matchRow++
		subStart := startColName + fmt.Sprint(matchRow)
		subEnd := endColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, subStart, subEnd, styles.text))
		handleExcelError("SetCellInt", f.SetCellInt(sheetName, subStart, int64(i+1)))
		handleExcelError("SetCellInt", f.SetCellInt(sheetName, subEnd, int64(i+1)))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName,
			colNames.leftVictoriesColName+fmt.Sprint(matchRow),
			colNames.rightVictoriesColName+fmt.Sprint(matchRow),
			styles.unlockedText))
	}
	lastTeamRow := matchRow

	if numTeamMatches > 0 {
		matchRow += 2 // spacing before team summary
		sumStart := startColName + fmt.Sprint(matchRow)
		sumEnd := endColName + fmt.Sprint(matchRow)
		if leftFormula != "" {
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, sumStart, leftFormula))
		}
		if rightFormula != "" {
			handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, sumEnd, rightFormula))
		}
		lVCol, lPCol, rVCol, rPCol := getMatchWinnerColumns(colNames)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lVCol, matchRow), "IV"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", lPCol, matchRow), "PW"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rVCol, matchRow), "IV"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", rPCol, matchRow), "PW"))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, sumStart, sumEnd, styles.text))
		matchRow++
		sumStart2 := startColName + fmt.Sprint(matchRow)
		sumEnd2 := endColName + fmt.Sprint(matchRow)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, sumStart2, "Victories / Points"))
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, sumEnd2, "Victories / Points"))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, sumStart2, sumEnd2, styles.text))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lVCol, matchRow),
			buildTeamWinnersFormula(middleColName, lVCol, lPCol, rVCol, rPCol, firstTeamRow, lastTeamRow, true)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", lPCol, matchRow),
			buildTeamPointsFormula(lVCol, lPCol, rVCol, rPCol, firstTeamRow, lastTeamRow, true)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rVCol, matchRow),
			buildTeamWinnersFormula(middleColName, lVCol, lPCol, rVCol, rPCol, firstTeamRow, lastTeamRow, false)))
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, fmt.Sprintf("%s%d", rPCol, matchRow),
			buildTeamPointsFormula(lVCol, lPCol, rVCol, rPCol, firstTeamRow, lastTeamRow, false)))
		matchRow++ // space after team summary
	}
	return matchRow
}

// printOrdinalMarkerRows writes the "1." / "2." result-marking rows two rows
// below matchRow and returns the Excel row carrying the "1." marker (the cell
// a regular elimination match registers as its winner reference; the bronze
// block ignores it).
func printOrdinalMarkerRows(f *excelize.File, sheetName string, colNames matchColumnNames, styles matchStyles, matchRow int) int {
	matchRow += 2 // spacing before result marking
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, colNames.rightVictoriesColName+fmt.Sprint(matchRow), "1."))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, colNames.endColName+fmt.Sprint(matchRow), colNames.endColName+fmt.Sprint(matchRow), styles.unlockedBorderBottom))
	winnerRow := matchRow
	matchRow++
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, colNames.rightVictoriesColName+fmt.Sprint(matchRow), "2."))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, colNames.endColName+fmt.Sprint(matchRow), colNames.endColName+fmt.Sprint(matchRow), styles.unlockedBorderBottom))
	return winnerRow
}

// PrintThirdPlaceBlock renders a single "3rd Place" elimination-match block
// (identical layout to a regular match block but with the fixed header label
// "3rd Place") starting at startRow on the SheetEliminationMatches sheet.
// courtStartCol is 1-based (use 1 for the first/only court). semiA and semiB
// are the match numbers of the two semifinals whose losers compete in the bronze
// (0 means absent/bye; that entrant cell is left empty). matchWinners is the map
// returned by PrintTeamEliminationMatches so the loser-cell refs can be derived
// from the "2." row of each semi's block. Returns the next available start row.
func PrintThirdPlaceBlock(f *excelize.File, courtStartCol, startRow, numTeamMatches int, mirror bool, engi bool, semiA, semiB int, matchWinners map[string]MatchWinner) int {
	sheetName := SheetEliminationMatches
	colNames := buildMatchColumnNames(courtStartCol)

	styles := matchStyles{
		poolHeader:           getPoolHeaderStyle(f),
		text:                 getGreyTextStyle(f),
		borderBottom:         getBorderStyleBottom(f),
		redHeader:            getRedHeaderStyle(f),
		whiteHeader:          getWhiteHeaderStyle(f),
		unlockedText:         getUnlockedTextStyle(f),
		unlockedBorderBottom: getUnlockedBorderStyleBottom(f),
	}

	matchHeight := EliminationMatchHeight
	if numTeamMatches > 0 {
		matchHeight = EliminationTeamMatchHeightBase + numTeamMatches
	}

	startColName := colNames.startColName
	endColName := colNames.endColName
	middleColName := colNames.middleColName

	matchRow := startRow

	// Header "3rd Place" (same merged style as "Round N - Match N").
	headerStart := startColName + fmt.Sprint(matchRow)
	headerEnd := endColName + fmt.Sprint(matchRow)
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, headerStart, headerEnd, styles.poolHeader))
	handleExcelError("MergeCell", f.MergeCell(sheetName, headerStart, headerEnd))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, headerStart, ThirdPlaceLabel))
	matchRow++

	// Red/White label row.
	matchHeaderWithStyles(f, sheetName, startColName, matchRow, middleColName, endColName,
		styles.redHeader, styles.text, styles.whiteHeader, mirror, engi)
	matchRow++

	// Score row: overlay writes name cells (always) and score cells (when the
	// match is completed); only the score cells are unlocked here.
	scoreStart := startColName + fmt.Sprint(matchRow)
	scoreEnd := endColName + fmt.Sprint(matchRow)
	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, scoreStart, scoreEnd, styles.text))
	if numTeamMatches > 0 {
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, middleColName+fmt.Sprint(matchRow), middleColName+fmt.Sprint(matchRow), styles.unlockedText))
	} else {
		handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, colNames.leftVictoriesColName+fmt.Sprint(matchRow), colNames.rightVictoriesColName+fmt.Sprint(matchRow), styles.unlockedText))
	}

	// Write CONCATENATE formulas for the entrant name cells so the bronze block
	// self-populates when the workbook is hand-scored. The "2." (loser) line of
	// each semifinal block is one row below the "1." winner line recorded in
	// matchWinners. When semiA or semiB is 0 (bye or engine path without match
	// numbers), that cell is left blank and must be filled manually.
	sideAFormula, sideBFormula := bronzeEntrantFormulas(sheetName, semiA, semiB, matchWinners)
	leftFormula, rightFormula := getMatchSides(sideAFormula, sideBFormula, mirror)
	if leftFormula != "" {
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, scoreStart, leftFormula))
	}
	if rightFormula != "" {
		handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, scoreEnd, rightFormula))
	}

	// Team sub-match rows + IV/PW summary (individual: no-op). The bronze
	// summary row leaves the entrant name cells blank because the entrant
	// formulas may themselves be blank (bye / engine path without semifinal
	// match numbers), unlike a regular match whose sides are always known.
	matchRow = printTeamMatchBlock(f, sheetName, colNames, styles, matchRow, numTeamMatches, "", "")

	// "1." / "2." markers; the bronze block records no downstream winner.
	printOrdinalMarkerRows(f, sheetName, colNames, styles, matchRow)

	return startRow + matchHeight
}

// PrintBronzeBlockWithPrintArea renders the naginata 3rd-place block starting at
// startRow (deriving the two semifinal match numbers from rounds) and extends the
// Elimination Matches print area to cover it. It bundles the three-call bronze
// protocol shared by every bronze render path (CLI generators, results workbook,
// blank-template export). nil rounds derive zero semi numbers, leaving both
// entrant slots hand-fillable.
func PrintBronzeBlockWithPrintArea(f *excelize.File, startRow, numTeamMatches int, mirror, engi bool, bands []string, bronzeCourt string, rounds [][]*Node, matchWinners map[string]MatchWinner) {
	semiA, semiB := SemifinalMatchNumbers(rounds)
	// The bronze is a bout like any other on this sheet, so it prints in ITS
	// shiaijo's band. Pinning it to the leftmost band was only ever right while
	// every bout was chunked positionally and the final therefore sat there too;
	// now that each bout follows its own court, a bronze moved to shiaijo D
	// would print under whatever shiaijo happens to be printed first.
	// Band 0 means "no court recorded" -- the CLI, which prints a bronze from a
	// blank workbook with no stored bracket behind it. A court that IS recorded
	// must have a band: PrintTeamEliminationMatches folds CourtPlan.Bronze into
	// the band set precisely so this cannot miss, and a miss here would be the
	// silent fall-back-to-leftmost that printed the bronze under another
	// shiaijo's header. Keep the two cases apart so they cannot be confused
	// again: an unknown court takes the LAST band rather than masquerading as
	// the first, which is visible on the sheet instead of plausible.
	//
	// The len(bands) guard is what keeps "last band" from meaning band -1. Both
	// current callers hand over a non-empty set (usedCourtBands never returns
	// one), but this is exported and a bandless call would otherwise compute
	// startCol 1-CourtsColumnsPerCourt and hand mustColumnName a negative
	// column. Band 0 is the same answer the no-court-recorded case gives, which
	// is the only sheet position that exists when there are no bands to pick.
	band := 0
	if bronzeCourt != "" && len(bands) > 0 {
		if i := slices.Index(bands, bronzeCourt); i >= 0 {
			band = i
		} else {
			band = len(bands) - 1
		}
	}
	bronzeEndRow := PrintThirdPlaceBlock(f, 1+band*CourtsColumnsPerCourt, startRow, numTeamMatches, mirror, engi, semiA, semiB, matchWinners)
	// The SHEET's band count, never a re-derivation of it: SetEliminationPrintArea
	// replaces the defined name, so a different number here silently overrides the
	// range the elimination blocks were printed into -- cutting a shiaijo's whole
	// running order out of the printed page, or padding it with blank bands.
	SetEliminationPrintArea(f, SheetEliminationMatches, len(bands), bronzeEndRow-1)
}

// PrintEliminationWithBronze renders the Elimination Matches blocks and, when
// includeBronze, the bronze (3rd-place) block immediately after the last
// round, wiring its entrant slots to the semifinal losers. The bronze gate
// stays with the caller because it is genuinely caller-specific (the CLI
// derives it from the naginata flag and round count via NeedsBronzeBlock; the
// exporters from the stored bracket's ThirdPlaceMatch).
func PrintEliminationWithBronze(f *excelize.File, matchWinners map[string]MatchWinner, rounds [][]*Node, numTeamMatches int, plan CourtPlan, mirror, engi, includeBronze bool) {
	// A shiaijo gets a band because a bout PRINTS under it. usedCourtBands folds
	// in plan.Bronze unconditionally -- it has to, or a bronze moved to a
	// shiaijo nothing else uses would be looked up in a band set it was never
	// allowed to join -- so when the bronze is NOT being rendered its court must
	// not reach that fold. Otherwise a stored ThirdPlaceMatch on shiaijo D buys
	// a "Shiaijo D" header, a page break and a print-area column with nothing
	// underneath: the empty band PrintPoolMatches already refuses to produce.
	// The bronze gate is the caller's (the CLI derives it from the naginata
	// flag, the exporters from the stored bracket), but the plan carries the
	// court whether or not that gate passed, so the two facts are reconciled
	// here once rather than at each call site.
	if !includeBronze {
		plan.Bronze = ""
	}
	nextRow, bands, elimMatchWinners := PrintTeamEliminationMatches(f, matchWinners, rounds, numTeamMatches, plan, mirror, engi)
	if includeBronze {
		PrintBronzeBlockWithPrintArea(f, nextRow, numTeamMatches, mirror, engi, bands, plan.Bronze, rounds, elimMatchWinners)
	}
}

func printSingleEliminationMatch(f *excelize.File, sheetName string, eliminationMatch *Node, poolMatchWinners map[string]MatchWinner, matchWinners map[string]MatchWinner, colNames matchColumnNames, matchRow int, round int, numTeamMatches int, styles matchStyles, mirror bool, engi bool) {
	startColName := colNames.startColName
	middleColName := colNames.middleColName
	endColName := colNames.endColName
	startCell := startColName + fmt.Sprint(matchRow)
	endCell := endColName + fmt.Sprint(matchRow)

	handleExcelError("SetCellStyle", f.SetCellStyle(sheetName, startCell, endCell, styles.poolHeader))
	handleExcelError("MergeCell", f.MergeCell(sheetName, startCell, endCell))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, startCell, fmt.Sprintf("Round %d - Match %d", round, eliminationMatch.matchNum)))

	matchRow++
	matchHeaderWithStyles(f, sheetName, startColName, matchRow, middleColName, endColName, styles.redHeader, styles.text, styles.whiteHeader, mirror, engi)
	matchRow++

	//////////////////////////////////////
	// eliminationMatch.Left checks if it is a pool winner
	startCell = startColName + fmt.Sprint(matchRow)
	var leftCellValue, rightCellValue string

	if eliminationMatch.Left.LeafNode && len(eliminationMatch.Left.LeafVal) > 0 {
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
	if eliminationMatch.Right.LeafNode && len(eliminationMatch.Right.LeafVal) > 0 {
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

	// Team sub-match rows + IV/PW summary (individual: no-op). The summary row
	// repeats the entrant name formulas next to the totals.
	matchRow = printTeamMatchBlock(f, sheetName, colNames, styles, matchRow, numTeamMatches, leftCellValue, rightCellValue)

	// "1." / "2." result markers; the "1." cell is the winner reference the
	// following rounds' CONCATENATE formulas point at.
	winnerRow := printOrdinalMarkerRows(f, sheetName, colNames, styles, matchRow)
	matchWinners[fmt.Sprintf("M %d", eliminationMatch.matchNum)] = MatchWinner{
		cellCoord: cellCoord{sheetName: sheetName, cell: fmt.Sprintf("%s%d", endColName, winnerRow)},
	}
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

// courtSheetName names the per-shiaijo "Names to Print" sheet after the shiaijo
// the competition actually runs on, not after the band's position.
func courtSheetName(courts []string, courtIdx int) string {
	return fmt.Sprintf("%s %s", SheetNamesToPrint, courtNameAt(courts, courtIdx))
}

// usedCourtBands is the ordered set of shiaijo the printed bouts actually run
// on: the competition's own order first, so the bands read A, B, C the way the
// operator lists them, then any shiaijo a bout was reassigned to that is not in
// that list, in first-seen order, so a bout can never be dropped for want of a
// band. Always returns at least one band.
func usedCourtBands(rounds [][]*Node, courts []string, bronze string, bandOf func(*Node) string) []string {
	var seen []string
	for _, round := range rounds {
		for _, m := range round {
			// Skip nils like AssignMatchNumbers and FillInMatches do. Nothing in
			// this repo builds rounds holding one (BuildEliminationMatchRounds
			// never emits nil), but PrintTeamEliminationMatches and
			// PrintEliminationWithBronze are exported and take the rounds from
			// their caller, and bandOf dereferences what it is given.
			if m == nil {
				continue
			}
			seen = append(seen, bandOf(m))
		}
	}
	// The 3rd-place bout is a SIBLING of the bracket's rounds, not a row in
	// them, so the walk above never reaches it. Without this it would be looked
	// up in a band set it was not allowed to join, and printed under whichever
	// shiaijo came first.
	seen = append(seen, bronze)
	return bandOrder(courts, seen)
}

// courtsPrefix takes the first n court names, padding with positional letters if
// the caller supplied fewer. Used where a clamp reduces the band count: the
// bands that survive keep their own names.
func courtsPrefix(courts []string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = courtNameAt(courts, i)
	}
	return out
}

func CreateNamesToPrint(f *excelize.File, players []Player, sanitized bool, courts []string, pCoords map[string]playerCellCoord) {
	numCourts := clampCourts(len(courts))

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

		sheetName := courtSheetName(courts, c)
		if _, err := f.NewSheet(sheetName); err != nil {
			handleExcelError("NewSheet", err)
		}
		printNameEntries(f, sheetName, entries, sanitized, pCoords)
	}

	if err := f.DeleteSheet(SheetNamesToPrint); err != nil {
		fmt.Fprintf(os.Stderr, "Note: %s sheet might not exist: %v\n", SheetNamesToPrint, err)
	}
}

// CreateNamesWithPoolToPrint writes one "Names to Print <Shiaijo>" sheet per
// court, holding that court's competitors tagged "<pool letter><position>".
//
// numCourts is clamped to the count the pool phase actually runs on
// (EffectiveDrawCourts), for the same reason PrintPoolMatches clamps: a sheet
// for a shiaijo that has no pools would file competitors under a court they
// never fight on, and one workbook cannot name one set of shiaijo on its name
// sheets and another on its pool sheets and tree pages. Derived here rather
// than by the caller so no call site can pass a count that disagrees with the
// Pool Matches skeleton.
func CreateNamesWithPoolToPrint(f *excelize.File, pools []Pool, sanitized bool, courts []string, courtOfPool map[string]string, pCoords map[string]playerCellCoord) {
	// EffectiveDrawCourts already coerces 0/negative to 1, so it is the only
	// clamp needed here; a clampCourts in front of it would be a no-op.
	// Same bands and same grouping the Pool Matches skeleton uses, so a
	// competitor's roster sheet is the shiaijo their pool is actually scored on.
	courts, poolsByCourt := PoolsByCourt(pools, courts, courtOfPool)
	numCourts := len(courts)

	entriesByCourt := make([][]nameEntry, numCourts)
	for court, poolIdxs := range poolsByCourt {
		for _, poolIdx := range poolIdxs {
			pool := pools[poolIdx]
			poolLetter := strings.TrimPrefix(pool.PoolName, "Pool ")
			for _, player := range pool.Players {
				entriesByCourt[court] = append(entriesByCourt[court], nameEntry{
					player:      player,
					fallbackTag: fmt.Sprintf("%s%d", poolLetter, player.PoolPosition),
				})
			}
		}
	}

	for c := range numCourts {
		if len(entriesByCourt[c]) == 0 {
			continue
		}
		sheetName := courtSheetName(courts, c)
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
		SetPrintArea(f, sheetName, 2, len(entries))
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
// programming error in the caller, excelize.ColumnNumberToName only errors
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
