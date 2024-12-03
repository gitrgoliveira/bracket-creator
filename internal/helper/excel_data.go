package helper

import (
	"fmt"
	"math"

	excelize "github.com/xuri/excelize/v2"
)

func AddPoolDataToSheet(f *excelize.File, pools []Pool, sanitize bool) {
	sheetName := "data"

	// Set the header row
	f.SetCellValue(sheetName, "A1", "Pool")
	f.SetCellValue(sheetName, "B1", "Player Name")
	f.SetCellValue(sheetName, "C1", "Player Dojo")
	if sanitize {
		f.SetCellValue(sheetName, "D1", "Display Name")
	}

	// Populate the groups in the spreadsheet
	row := 2

	for i := 0; i < len(pools); i++ {
		pools[i].sheetName = sheetName

		for j := range pools[i].Players {
			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), pools[i].PoolName)
			f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), pools[i].Players[j].Name)
			f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), pools[i].Players[j].Dojo)
			if sanitize {
				f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), pools[i].Players[j].DisplayName)
			}
			pools[i].cell = fmt.Sprintf("A%d", row)
			pools[i].Players[j].sheetName = sheetName
			pools[i].Players[j].cell = fmt.Sprintf("B%d", row)
			row++
		}
	}

	fmt.Printf("Data added to spreadsheet\n")

	// Set the column widths
	f.SetColWidth(sheetName, "A", "A", 15)
	f.SetColWidth(sheetName, "B", "D", 30)
}

func AddPlayerDataToSheet(f *excelize.File, players []Player, sanitize bool) {
	sheetName := "data"

	// Set the header row
	f.SetCellValue(sheetName, "A1", "Number")
	f.SetCellValue(sheetName, "B1", "Player Name")
	f.SetCellValue(sheetName, "C1", "Player Dojo")
	if sanitize {
		f.SetCellValue(sheetName, "D1", "Display Name")
	}
	// Populate the groups in the spreadsheet
	row := 2

	for i := 0; i < len(players); i++ {
		players[i].sheetName = sheetName

		f.SetCellInt(sheetName, fmt.Sprintf("A%d", row), players[i].PoolPosition)
		f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), players[i].Name)
		f.SetCellValue(sheetName, fmt.Sprintf("C%d", row), players[i].Dojo)
		if sanitize {
			f.SetCellValue(sheetName, fmt.Sprintf("D%d", row), players[i].DisplayName)
		}
		players[i].cell = fmt.Sprintf("B%d", row)
		row++
	}

	fmt.Printf("Data added to spreadsheet\n")

	// Set the column widths
	f.SetColWidth(sheetName, "A", "A", 15)
	f.SetColWidth(sheetName, "B", "D", 30)

}

func AddPoolsToSheet(f *excelize.File, pools []Pool) error {
	// Set the starting row and column for the bracket
	sheetName := "Pool Draw"
	numPoolsPerColumn := int(math.Ceil(float64(len(pools)) / 3))

	startRow := 5
	startCol := 2

	col_name, _ := excelize.ColumnNumberToName(startCol)
	cell := col_name + fmt.Sprint(startRow)
	headerCellStyle, _ := f.GetCellStyle(sheetName, cell)
	cell = col_name + fmt.Sprint(startRow+1)
	contentCellStyle, _ := f.GetCellStyle(sheetName, cell)

	row := startRow
	column := startCol
	// Write the bracket data to Excel
	for i, pool := range pools {
		// groupNumber := pools[i].cell
		col_name, _ = excelize.ColumnNumberToName(column)
		cell = col_name + fmt.Sprint(row)

		f.SetCellFormula(sheetName, cell, fmt.Sprintf("%s!%s", pool.sheetName, pool.cell))

		f.SetCellStyle(sheetName, cell, cell, headerCellStyle)
		row++
		for _, player := range pool.Players {
			cell := col_name + fmt.Sprint(row)
			f.SetCellFormula(sheetName, cell, fmt.Sprintf("%s!%s", player.sheetName, player.cell))
			f.SetCellStyle(sheetName, cell, cell, contentCellStyle)
			row++
		}

		row += 2

		if (i+1)%numPoolsPerColumn == 0 {
			column += 2
			row = startRow
		}

		f.SetColWidth(sheetName, col_name, col_name, 30)
	}

	fmt.Printf("%d pools added to spreadsheet\n", len(pools))

	return nil
}
