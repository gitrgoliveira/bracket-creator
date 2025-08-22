package excel_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/xuri/excelize/v2"
)

func TestNewSheetManager(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()

	sm := excel.NewSheetManager(file)

	if sm == nil {
		t.Fatal("Expected SheetManager to not be nil")
	}
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

	// This is just a smoke test to ensure it doesn't panic
	// A real test would verify the Excel contents
	err = sm.AddPlayerDataToSheet(players, true)
	if err != nil {
		t.Errorf("AddPlayerDataToSheet failed: %v", err)
	}
}
