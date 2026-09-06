package helper

import (
	"fmt"
	"os"

	excelize "github.com/xuri/excelize/v2"
)

type dataColumnLayout struct {
	hasNumber    bool
	numberColNum int
	metaStartCol int
	metaCols     []string
}

func setupDataSheet(f *excelize.File, sanitize bool, hasNumber bool, titlePrefix string, colAHeader string) dataColumnLayout {
	sheetName := SheetData
	SetSheetLayoutPortraitA4(f, sheetName)

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "A1", "Title prefix:"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "B1", titlePrefix))

	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "A2", colAHeader))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "B2", "Player Name"))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, "C2", "Player Dojo"))
	if sanitize {
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, "D2", "Display Name"))
	}

	numberColNum := 4
	metaStartCol := 5
	if sanitize {
		numberColNum = 5
		metaStartCol = 5
	}
	if hasNumber {
		numberColName := mustColumnName(numberColNum)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s2", numberColName), "Player Number"))
		if sanitize {
			metaStartCol = 6
		}
	}
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s2", mustColumnName(metaStartCol)), "Metadata"))

	return dataColumnLayout{
		hasNumber:    hasNumber,
		numberColNum: numberColNum,
		metaStartCol: metaStartCol,
		metaCols:     make([]string, 0, 8),
	}
}

func (layout *dataColumnLayout) writePlayer(f *excelize.File, row int, player *Player, sanitize bool, pCoords map[string]playerCellCoord) {
	sheetName := SheetData
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), player.Name))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), player.Dojo))
	if sanitize {
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), player.DisplayName))
	}
	coord := playerCellCoord{cellCoord: cellCoord{sheetName: sheetName, cell: fmt.Sprintf("$B$%d", row)}}
	if layout.hasNumber {
		numberColName := mustColumnName(layout.numberColNum)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", numberColName, row), player.Number))
		coord.numberCell = fmt.Sprintf("$%s$%d", numberColName, row)
	}
	for k, meta := range player.Metadata {
		if k >= len(layout.metaCols) {
			layout.metaCols = append(layout.metaCols, mustColumnName(layout.metaStartCol+k))
		}
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", layout.metaCols[k], row), meta))
	}
	pCoords[playerCoordKey(*player)] = coord
}

func finishDataSheet(f *excelize.File) {
	sheetName := SheetData
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "A", "A", 9))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "B", "C", 20))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "D", "Z", 12))
	fmt.Printf("Data added to spreadsheet\n")
}

func AddPoolDataToSheet(f *excelize.File, pools []Pool, sanitize bool, titlePrefix string) (map[string]cellCoord, map[string]playerCellCoord) {
	// hasNumber (bc-pnum review H7) is true when ANY player anywhere in
	// pools has a Number, not just the first pool's first player: a
	// hand-edited/legacy pools.csv can carry unnumbered rows before a
	// numbered one, and checking only the first player used to drop the
	// whole Player Number column (and so the Names to Print number cell
	// downstream) for every player on the sheet just because the leading
	// row happened to be blank.
	hasNumber := false
	for i := range pools {
		for j := range pools[i].Players {
			if pools[i].Players[j].Number != "" {
				hasNumber = true
				break
			}
		}
		if hasNumber {
			break
		}
	}
	layout := setupDataSheet(f, sanitize, hasNumber, titlePrefix, "Pool")

	poolCoords := make(map[string]cellCoord, len(pools))
	playerCoords := make(map[string]playerCellCoord)

	row := 3
	for i := range pools {
		for j := range pools[i].Players {
			handleExcelError("SetCellValue", f.SetCellValue(SheetData, fmt.Sprintf("A%d", row), pools[i].PoolName))
			layout.writePlayer(f, row, &pools[i].Players[j], sanitize, playerCoords)
			poolCoords[pools[i].PoolName] = cellCoord{sheetName: SheetData, cell: fmt.Sprintf("$A$%d", row)}
			row++
		}
	}

	finishDataSheet(f)
	return poolCoords, playerCoords
}

// AddPlayerDataToSheet is the playoffs-only (no pools) counterpart of
// AddPoolDataToSheet, used by a pure-playoffs draw's Data sheet.
//
// Column A (bc-pnum A11, relabelled by bc-pnum review H8) is headed "Entry
// order", 1-based: CreatePlayers (tournament.go) stamps each entrant's
// PoolPosition 0-based (len(players) BEFORE the append), a value pool
// distribution overwrites 1-based for every pooled competition but nothing
// ever touches for a playoffs-only one, so this sheet showed row 3 (the
// first entrant) as "0" beside a "Player Number" column already reading
// "K1" -- two different counting conventions on the same row.
//
// This function runs BEFORE StandardSeeding reorders players into bracket
// slot order (see cmd/create-playoffs.go), so column A is the ENTRY order
// -- the shuffled order the CLI drew the roster in, or the roster order for
// the engine's export path -- never the bracket slot order the header used
// to imply with the now-removed "Draw order" name. Relabelled, not
// reordered: this column stays display-only and the Data sheet stays the
// numbering source of truth Names to Print links to by coordinate, so
// changing what it counts (rather than what it is called) would break that
// link. Nothing reads this cell by formula reference anywhere downstream
// (unlike the "Player Number" column, which CreateNamesToPrint links to);
// it is display-only, which is what makes a pure rename safe.
func AddPlayerDataToSheet(f *excelize.File, players []Player, sanitize bool, titlePrefix string) map[string]playerCellCoord {
	// hasNumber (bc-pnum review H7): any player, not just the first -- see
	// the identical rationale in AddPoolDataToSheet above.
	hasNumber := false
	for i := range players {
		if players[i].Number != "" {
			hasNumber = true
			break
		}
	}
	layout := setupDataSheet(f, sanitize, hasNumber, titlePrefix, "Entry order")

	playerCoords := make(map[string]playerCellCoord, len(players))

	row := 3
	for i := range players {
		handleExcelError("SetCellInt", f.SetCellInt(SheetData, fmt.Sprintf("A%d", row), players[i].PoolPosition+1))
		layout.writePlayer(f, row, &players[i], sanitize, playerCoords)
		row++
	}

	finishDataSheet(f)
	return playerCoords
}

// AddDataToSheetForExport is RenderCompetitionWorkbook's step 1 (bc-pnum
// A8/[review]): the ONE writer of the Data sheet for that shared pipeline,
// so a caller never has to run AddPoolDataToSheet and then separately
// AddPlayerDataToSheet on the same workbook to cover the one shape
// (playoffs-only, no pools.csv) that needs the latter. namesToPrintPlayers
// takes priority when non-empty (the blank-template export's numbered
// roster, see Engine.NumberedParticipantsFor); pools is used otherwise,
// including the ordinary "no pools drawn yet" case, which AddPoolDataToSheet
// already renders as a header-only sheet.
//
// Before this existed, the blank-template export called AddPoolDataToSheet
// unconditionally (writing only headers when pools was empty) and THEN
// called AddPlayerDataToSheet a second time for the playoffs-only case,
// after RenderCompetitionWorkbook had already returned -- two writers of one
// sheet, which is why "Data added to spreadsheet" printed twice for exactly
// that shape. cmd/create-pools.go and cmd/create-playoffs.go call
// AddPoolDataToSheet/AddPlayerDataToSheet directly and are unaffected: this
// wrapper exists only for the shared engine/export pipeline's step 1.
func AddDataToSheetForExport(f *excelize.File, pools []Pool, namesToPrintPlayers []Player, sanitize bool, titlePrefix string) (map[string]cellCoord, map[string]playerCellCoord) {
	if len(namesToPrintPlayers) > 0 {
		return nil, AddPlayerDataToSheet(f, namesToPrintPlayers, sanitize, titlePrefix)
	}
	return AddPoolDataToSheet(f, pools, sanitize, titlePrefix)
}

// poolDrawColumnCount is the fixed number of columns on the Pool Draw sheet.
// Columns B, D, F (indices 2, 4, 6) are the three pool columns.
const poolDrawColumnCount = 3

func AddPoolsToSheet(f *excelize.File, pools []Pool, poolCoords map[string]cellCoord, playerCoords map[string]playerCellCoord) error {
	sheetName := SheetPoolDraw
	SetSheetLayoutPortraitA4(f, sheetName)

	// Write a formula that prepends the title prefix (data!$B$1) to the sheet title.
	// B2:F2 is merged in the template and holds "Tournament Pools" as a static value;
	// this formula replaces it so editing data!B1 updates the title automatically.
	handleExcelError("SetCellFormula", f.SetCellFormula(sheetName, "B2",
		`IF(data!$B$1="","Tournament Pools",data!$B$1&" - Tournament Pools")`))

	// Pool header style: bold italic, 12 pt, silver fill, thick borders, right-aligned.
	headerCellStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Family: "Calibri", Bold: true, Italic: true, Size: 12},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"C0C0C0"}},
		Border: []excelize.Border{
			{Type: "left", Style: 2, Color: "000000"},
			{Type: "right", Style: 2, Color: "000000"},
			{Type: "top", Style: 2, Color: "000000"},
			{Type: "bottom", Style: 2, Color: "000000"},
		},
		Alignment: &excelize.Alignment{Horizontal: "right"},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create header cell style: %v\n", err)
	}
	// Pool content style: 12 pt, thick borders on all sides.
	contentCellStyle, err := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Family: "Calibri", Size: 12},
		Border: []excelize.Border{
			{Type: "left", Style: 2, Color: "000000"},
			{Type: "right", Style: 2, Color: "000000"},
			{Type: "top", Style: 2, Color: "000000"},
			{Type: "bottom", Style: 2, Color: "000000"},
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create content cell style: %v\n", err)
	}

	const startRow = 5
	const startCol = 2 // column B
	const colStep = 2  // B=2, D=4, F=6

	// Distribute pools evenly across exactly poolDrawColumnCount (3) columns.
	// Each column on a page gets ceil(remainingPools / remainingColumns) pools,
	// ensuring the sheet always renders with 3 columns.
	n := len(pools)
	if n == 0 {
		fmt.Printf("0 pools added to spreadsheet\n")
		return nil
	}

	// Pre-assign each pool to a (colIndex, page) pair using a balanced
	// column-first distribution: fill columns in order, each column gets
	// ceil(remaining / remaining_cols) pools.
	type poolPlacement struct {
		colIndex int // 0-based, maps to startCol + colIndex*colStep
		page     int // 0-based page number
	}
	placements := make([]poolPlacement, n)
	{
		remaining := n
		poolIdx := 0
		page := 0
		for remaining > 0 {
			for c := 0; c < poolDrawColumnCount && remaining > 0; c++ {
				// How many pools go into this column?
				colsLeft := poolDrawColumnCount - c
				colPoolCount := (remaining + colsLeft - 1) / colsLeft
				for k := 0; k < colPoolCount; k++ {
					placements[poolIdx] = poolPlacement{colIndex: c, page: page}
					poolIdx++
					remaining--
				}
			}
			if remaining > 0 {
				page++
			}
		}
	}

	// Page boundaries: each page's first data row.
	// page 0 starts at startRow; subsequent pages start after the page break.
	pageRowsAvailable := PoolDrawRowsPerPage - startRow + 1
	pageStartRows := []int{startRow}

	// pageColRows[page][colIndex] = next available row in that column on that page.
	pageColRows := [][]int{{startRow, startRow, startRow}}

	// Ensure page state arrays are extended as needed.
	ensurePage := func(page int) {
		for len(pageStartRows) <= page {
			prev := pageStartRows[len(pageStartRows)-1]
			// The page break is inserted at the row after the previous page's last row.
			nextStart := prev + pageRowsAvailable
			pageStartRows = append(pageStartRows, nextStart)
			pageColRows = append(pageColRows, []int{nextStart, nextStart, nextStart})
		}
	}

	maxRow := startRow
	insertedBreaks := map[int]bool{}

	for i, pool := range pools {
		p := placements[i]
		ensurePage(p.page)

		// Insert a page break before page p (if not already inserted).
		if p.page > 0 && !insertedBreaks[p.page] {
			breakRow := pageStartRows[p.page]
			handleExcelError("InsertPageBreak",
				f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", breakRow)))
			insertedBreaks[p.page] = true
		}

		colNum := startCol + p.colIndex*colStep
		row := pageColRows[p.page][p.colIndex]
		colName := mustColumnName(colNum)

		// Write pool header.
		headerCell := colName + fmt.Sprint(row)
		pc := poolCoords[pool.PoolName]
		handleExcelError("SetCellFormula",
			f.SetCellFormula(sheetName, headerCell,
				sheetRef(pc.sheetName, pc.cell)))
		handleExcelError("SetCellStyle",
			f.SetCellStyle(sheetName, headerCell, headerCell, headerCellStyle))
		row++

		// Write player rows.
		for _, player := range pool.Players {
			cell := colName + fmt.Sprint(row)
			coord := playerCoords[playerCoordKey(player)]
			var formula string
			if coord.numberCell != "" {
				formula = playerRef(player.Name, coord)
			} else {
				formula = fmt.Sprintf("\"%d. \" & %s!%s", player.PoolPosition, coord.sheetName, coord.cell)
			}
			handleExcelError("SetCellFormula",
				f.SetCellFormula(sheetName, cell, formula))
			handleExcelError("SetCellStyle",
				f.SetCellStyle(sheetName, cell, cell, contentCellStyle))
			row++
		}

		// Two blank separator rows after the pool.
		row += 2

		// Update cursor and track the overall last used row.
		pageColRows[p.page][p.colIndex] = row
		if row > maxRow {
			maxRow = row
		}

		// Ensure the column has its display width set.
		handleExcelError("SetColWidth",
			f.SetColWidth(sheetName, colName, colName, 30))
	}

	// Define print area: B2 to F<maxRow>.
	if maxRow > 2 {
		handleExcelError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
			Name:     "_xlnm.Print_Area",
			RefersTo: fmt.Sprintf("'%s'!$B$2:$F$%d", sheetName, maxRow),
			Scope:    sheetName,
		}))
	}

	fmt.Printf("%d pools added to spreadsheet\n", len(pools))
	return nil
}
