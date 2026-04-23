package helper

import (
	"fmt"

	excelize "github.com/xuri/excelize/v2"
)

func AddPoolDataToSheet(f *excelize.File, pools []Pool, sanitize bool) {
	sheetName := "data"

	// Row 1: title prefix label (B1 is left empty for user to fill in)
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "A1", "Title prefix:"))

	// Row 2: column headers
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "A2", "Pool"))
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "B2", "Player Name"))
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "C2", "Player Dojo"))
	if sanitize {
		handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "D2", "Display Name"))
	}
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "E2", "Metadata"))

	// Populate the groups in the spreadsheet
	row := 3
	metaCols := make([]string, 0, 8)

	for i := 0; i < len(pools); i++ {
		pools[i].sheetName = sheetName

		for j := range pools[i].Players {
			handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), pools[i].PoolName))
			handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), pools[i].Players[j].Name))
			handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), pools[i].Players[j].Dojo))
			if sanitize {
				handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), pools[i].Players[j].DisplayName))
			}
			for k, meta := range pools[i].Players[j].Metadata {
				if k >= len(metaCols) {
					colName, _ := excelize.ColumnNumberToName(5 + k)
					metaCols = append(metaCols, colName)
				}
				colName := metaCols[k]
				handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", colName, row), meta))
			}
			pools[i].cell = fmt.Sprintf("$A$%d", row)
			pools[i].Players[j].sheetName = sheetName
			pools[i].Players[j].cell = fmt.Sprintf("$B$%d", row)
			row++
		}
	}

	fmt.Printf("Data added to spreadsheet\n")

	// Set the column widths
	handleExcelDataError("SetColWidth", f.SetColWidth(sheetName, "A", "A", 15))
	handleExcelDataError("SetColWidth", f.SetColWidth(sheetName, "B", "Z", 30))
}

func AddPlayerDataToSheet(f *excelize.File, players []Player, sanitize bool) {
	sheetName := "data"

	// Row 1: title prefix label (B1 is left empty for user to fill in)
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "A1", "Title prefix:"))
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "B1", ""))

	// Row 2: column headers
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "A2", "Number"))
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "B2", "Player Name"))
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "C2", "Player Dojo"))
	if sanitize {
		handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "D2", "Display Name"))
	}
	handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, "E2", "Metadata"))
	// Populate the groups in the spreadsheet
	row := 3
	metaCols := make([]string, 0, 8)

	for i := 0; i < len(players); i++ {
		players[i].sheetName = sheetName

		handleExcelDataError("SetCellInt", f.SetCellInt(sheetName, fmt.Sprintf("A%d", row), players[i].PoolPosition))
		handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), players[i].Name))
		handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), players[i].Dojo))
		if sanitize {
			handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), players[i].DisplayName))
		}
		for k, meta := range players[i].Metadata {
			if k >= len(metaCols) {
				colName, _ := excelize.ColumnNumberToName(5 + k)
				metaCols = append(metaCols, colName)
			}
			colName := metaCols[k]
			handleExcelDataError("SetCellValue", f.SetCellValue(sheetName, fmt.Sprintf("%s%d", colName, row), meta))
		}
		players[i].cell = fmt.Sprintf("$B$%d", row)
		row++
	}

	fmt.Printf("Data added to spreadsheet\n")

	// Set the column widths
	handleExcelDataError("SetColWidth", f.SetColWidth(sheetName, "A", "A", 15))
	handleExcelDataError("SetColWidth", f.SetColWidth(sheetName, "B", "Z", 30))

}

func AddPoolsToSheet(f *excelize.File, pools []Pool) error {
	// Set the starting row and column for the bracket
	sheetName := "Pool Draw"
	SetSheetLayoutPortraitA4(f, sheetName)

	// Write a formula that prepends the title prefix (data!$B$1) to the sheet title.
	// B2:F2 is merged in the template and holds "Tournament Pools" as a static value;
	// this formula replaces it so editing data!B1 updates the title automatically.
	handleExcelDataError("SetCellFormula", f.SetCellFormula(sheetName, "B2",
		`IF(data!$B$1="","Tournament Pools",data!$B$1&" - Tournament Pools")`))
	startRow := 5
	startCol := 2
	rowsPerPageLimit := 42

	col_name, _ := excelize.ColumnNumberToName(startCol)
	cell := col_name + fmt.Sprint(startRow)
	headerCellStyle, _ := f.GetCellStyle(sheetName, cell)
	cell = col_name + fmt.Sprint(startRow+1)
	contentCellStyle, _ := f.GetCellStyle(sheetName, cell)

	row := startRow
	column := startCol
	maxRow := startRow
	pageStartRow := startRow

	// Write the bracket data to Excel
	for _, pool := range pools {
		poolHeight := 1 + len(pool.Players) + 2

		// Check if pool fits in the current column of the current page
		if row+poolHeight > rowsPerPageLimit {
			// Move to next column
			column += 2
			row = pageStartRow

			// If we've filled all 3 columns (B, D, F -> 2, 4, 6)
			if column > 6 {
				// Insert page break at the start of the next page
				// We use rowsPerPageLimit + 1 as the break point
				breakRow := rowsPerPageLimit + 1
				handleExcelDataError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", breakRow)))

				// Update page boundaries for the next page
				pageStartRow = breakRow + 1
				row = pageStartRow
				column = startCol
				rowsPerPageLimit = pageStartRow + (42 - 5) // Maintain same page height
			}
		}

		col_name, _ = excelize.ColumnNumberToName(column)
		cell = col_name + fmt.Sprint(row)

		handleExcelDataError("SetCellFormula", f.SetCellFormula(sheetName, cell, fmt.Sprintf("%s!%s", pool.sheetName, pool.cell)))
		handleExcelDataError("SetCellStyle", f.SetCellStyle(sheetName, cell, cell, headerCellStyle))
		row++
		for _, player := range pool.Players {
			cell := col_name + fmt.Sprint(row)
			handleExcelDataError("SetCellFormula", f.SetCellFormula(sheetName, cell, fmt.Sprintf("\"%d. \" & %s!%s", player.PoolPosition, player.sheetName, player.cell)))
			handleExcelDataError("SetCellStyle", f.SetCellStyle(sheetName, cell, cell, contentCellStyle))
			row++
		}

		if row > maxRow {
			maxRow = row
		}

		row += 2
		handleExcelDataError("SetColWidth", f.SetColWidth(sheetName, col_name, col_name, 30))
	}

	// Define print area for the "Pool Draw" sheet
	// It starts at B2 (title) and goes to column F (3rd pool column)
	if maxRow > 2 {
		handleExcelDataError("SetDefinedName", f.SetDefinedName(&excelize.DefinedName{
			Name:     "_xlnm.Print_Area",
			RefersTo: fmt.Sprintf("'%s'!$B$2:$F$%d", sheetName, maxRow),
			Scope:    sheetName,
		}))
	}

	fmt.Printf("%d pools added to spreadsheet\n", len(pools))

	return nil
}

// handleExcelDataError is a helper function to handle errors from Excel operations
func handleExcelDataError(operation string, err error) {
	if err != nil {
		fmt.Printf("Error in Excel operation %s: %v\n", operation, err)
	}
}
