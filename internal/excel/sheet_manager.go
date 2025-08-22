package excel

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/xuri/excelize/v2"
)

// SheetManager handles Excel sheet operations
type SheetManager struct {
	file         *excelize.File
	styleManager *StyleManager
}

// NewSheetManager creates a new sheet manager
func NewSheetManager(file *excelize.File) *SheetManager {
	return &SheetManager{
		file:         file,
		styleManager: NewStyleManager(file),
	}
}

// AddPlayerDataToSheet adds player data to a data sheet
func (m *SheetManager) AddPlayerDataToSheet(players []domain.Player, sanitize bool) error {
	sheetName := "data"

	// Set the header row
	if err := m.file.SetCellValue(sheetName, "A1", "Number"); err != nil {
		return handleError("SetCellValue", err)
	}
	if err := m.file.SetCellValue(sheetName, "B1", "Player Name"); err != nil {
		return handleError("SetCellValue", err)
	}
	if err := m.file.SetCellValue(sheetName, "C1", "Player Dojo"); err != nil {
		return handleError("SetCellValue", err)
	}
	if sanitize {
		if err := m.file.SetCellValue(sheetName, "D1", "Display Name"); err != nil {
			return handleError("SetCellValue", err)
		}
	}

	// Populate players in the spreadsheet
	row := 2
	for _, player := range players {
		// TODO: This would update domain players with Excel coordinates in final implementation

		if err := m.file.SetCellInt(sheetName, fmt.Sprintf("A%d", row), player.PoolPosition); err != nil {
			return handleError("SetCellInt", err)
		}
		if err := m.file.SetCellValue(sheetName, fmt.Sprintf("B%d", row), player.Name); err != nil {
			return handleError("SetCellValue", err)
		}
		if err := m.file.SetCellValue(sheetName, fmt.Sprintf("C%d", row), player.Dojo); err != nil {
			return handleError("SetCellValue", err)
		}
		if sanitize {
			if err := m.file.SetCellValue(sheetName, fmt.Sprintf("D%d", row), player.DisplayName); err != nil {
				return handleError("SetCellValue", err)
			}
		}
		row++
	}

	fmt.Printf("Data added to spreadsheet\n")

	// Set the column widths
	if err := m.file.SetColWidth(sheetName, "A", "A", 15); err != nil {
		return handleError("SetColWidth", err)
	}
	if err := m.file.SetColWidth(sheetName, "B", "D", 30); err != nil {
		return handleError("SetColWidth", err)
	}

	return nil
}

// Additional sheet operations can be added as needed
