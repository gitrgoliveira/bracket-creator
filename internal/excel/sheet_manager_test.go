package excel_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestNewSheetManager(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()

	sm := excel.NewSheetManager(file)

	require.NotNil(t, sm)
}

func TestAddPlayerDataToSheet(t *testing.T) {
	// This would be integration testing
	// For a true unit test, we would need to mock the Excel file interface
	file := excelize.NewFile()
	defer file.Close()

	// Create the default "Sheet1" that excelize creates
	sheetName := "data"
	index, err := file.NewSheet(sheetName)
	if err != nil {
		t.Fatalf("Failed to create sheet: %v", err)
	}
	file.SetActiveSheet(index)

	sm := excel.NewSheetManager(file)

	// Create test players
	players := []domain.Player{
		{
			ID:           "player1",
			Name:         "John Doe",
			DisplayName:  "J. Doe",
			Dojo:         "Test Dojo",
			PoolPosition: 1,
		},
		{
			ID:           "player2",
			Name:         "Jane Smith",
			DisplayName:  "J. Smith",
			Dojo:         "Another Dojo",
			PoolPosition: 2,
		},
	}

	err = sm.AddPlayerDataToSheet(players, true)
	require.NoError(t, err)

	assertCellValue(t, file, sheetName, "A1", "Number")
	assertCellValue(t, file, sheetName, "B1", "Player Name")
	assertCellValue(t, file, sheetName, "C1", "Player Dojo")
	assertCellValue(t, file, sheetName, "D1", "Display Name")

	assertCellValue(t, file, sheetName, "A2", "1")
	assertCellValue(t, file, sheetName, "B2", "John Doe")
	assertCellValue(t, file, sheetName, "C2", "Test Dojo")
	assertCellValue(t, file, sheetName, "D2", "J. Doe")

	assertCellValue(t, file, sheetName, "A3", "2")
	assertCellValue(t, file, sheetName, "B3", "Jane Smith")
	assertCellValue(t, file, sheetName, "C3", "Another Dojo")
	assertCellValue(t, file, sheetName, "D3", "J. Smith")
}

func assertCellValue(t *testing.T, file *excelize.File, sheetName, cell, expected string) {
	t.Helper()
	value, err := file.GetCellValue(sheetName, cell)
	require.NoError(t, err)
	assert.Equal(t, expected, value)
}
