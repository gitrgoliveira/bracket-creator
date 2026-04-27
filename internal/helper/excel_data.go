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

func (layout *dataColumnLayout) writePlayer(f *excelize.File, row int, player *Player, sanitize bool) {
	sheetName := SheetData
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), player.Name))
	handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), player.Dojo))
	if sanitize {
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), player.DisplayName))
	}
	if layout.hasNumber {
		numberColName := mustColumnName(layout.numberColNum)
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", numberColName, row), player.Number))
		player.numberCell = fmt.Sprintf("$%s$%d", numberColName, row)
	}
	for k, meta := range player.Metadata {
		if k >= len(layout.metaCols) {
			layout.metaCols = append(layout.metaCols, mustColumnName(layout.metaStartCol+k))
		}
		handleExcelError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", layout.metaCols[k], row), meta))
	}
	player.sheetName = sheetName
	player.cell = fmt.Sprintf("$B$%d", row)
}

func finishDataSheet(f *excelize.File) {
	sheetName := SheetData
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "A", "A", 9))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "B", "C", 20))
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "D", "Z", 12))
	fmt.Printf("Data added to spreadsheet\n")
}

func AddPoolDataToSheet(f *excelize.File, pools []Pool, sanitize bool, titlePrefix string) {
	hasNumber := false
	for i := range pools {
		if len(pools[i].Players) > 0 && pools[i].Players[0].Number != "" {
			hasNumber = true
			break
		}
	}
	layout := setupDataSheet(f, sanitize, hasNumber, titlePrefix, "Pool")

	row := 3
	for i := range pools {
		pools[i].sheetName = SheetData
		for j := range pools[i].Players {
			handleExcelError("SetCellValue", f.SetCellValue(SheetData, fmt.Sprintf("A%d", row), pools[i].PoolName))
			layout.writePlayer(f, row, &pools[i].Players[j], sanitize)
			pools[i].cell = fmt.Sprintf("$A$%d", row)
			row++
		}
	}

	finishDataSheet(f)
}

func AddPlayerDataToSheet(f *excelize.File, players []Player, sanitize bool, titlePrefix string) {
	hasNumber := len(players) > 0 && players[0].Number != ""
	layout := setupDataSheet(f, sanitize, hasNumber, titlePrefix, "Number")

	row := 3
	for i := range players {
		handleExcelError("SetCellInt", f.SetCellInt(SheetData, fmt.Sprintf("A%d", row), players[i].PoolPosition))
		layout.writePlayer(f, row, &players[i], sanitize)
		row++
	}

	finishDataSheet(f)
}

// poolDrawColumnCount is the fixed number of columns on the Pool Draw sheet.
// Columns B, D, F (indices 2, 4, 6) are the three pool columns.
const poolDrawColumnCount = 3

func AddPoolsToSheet(f *excelize.File, pools []Pool) error {
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
		handleExcelError("SetCellFormula",
			f.SetCellFormula(sheetName, headerCell,
				sheetRef(pool.sheetName, pool.cell)))
		handleExcelError("SetCellStyle",
			f.SetCellStyle(sheetName, headerCell, headerCell, headerCellStyle))
		row++

		// Write player rows.
		for _, player := range pool.Players {
			cell := colName + fmt.Sprint(row)
			var formula string
			if player.numberCell != "" {
				formula = playerRef(&player)
			} else {
				formula = fmt.Sprintf("\"%d. \" & %s!%s", player.PoolPosition, player.sheetName, player.cell)
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
