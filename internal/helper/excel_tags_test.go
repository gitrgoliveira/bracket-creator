package helper

import (
	"testing"

	excelize "github.com/xuri/excelize/v2"
)

func TestCreateTagsSheet(t *testing.T) {
	// 1. Setup
	f := excelize.NewFile()
	pools := []Pool{
		{
			PoolName: "Pool A",
			Players: []Player{
				{Name: "Player 1", PoolPosition: 1},
				{Name: "Player 2", PoolPosition: 2},
				{Name: "Player 3", PoolPosition: 3},
			},
		},
	}

	// 2. Execution
	err := CreateTagsSheet(f, pools)
	if err != nil {
		t.Fatalf("CreateTagsSheet failed: %v", err)
	}

	sheetName := "Tags"

	// 3. Verification - Page Layout (A4 portrait)
	opts, err := f.GetPageLayout(sheetName)
	if err != nil {
		t.Fatalf("Failed to get page layout: %v", err)
	}
	if opts.Size == nil {
		t.Error("Page Size is nil")
	} else if *opts.Size != 9 {
		t.Errorf("Expected Page Size 9 (A4), got %d", *opts.Size)
	}
	if opts.Orientation == nil {
		t.Error("Orientation is nil")
	} else if *opts.Orientation != "portrait" {
		t.Errorf("Expected orientation 'portrait', got '%s'", *opts.Orientation)
	}

	// 4. Verification - Row Height (~390 points = half A4 portrait)
	height, err := f.GetRowHeight(sheetName, 1)
	if err != nil {
		t.Fatalf("Failed to get row height: %v", err)
	}
	if height != 390 {
		t.Errorf("Expected row height 390, got %f", height)
	}

	// 5. Verification - Content: tags use pool letter + position
	val, err := f.GetCellValue(sheetName, "A1")
	if err != nil {
		t.Errorf("Failed to get cell value: %v", err)
	}
	if val != "A1" {
		t.Errorf("Expected cell A1 to contain 'A1', got '%s'", val)
	}

	val, err = f.GetCellValue(sheetName, "A2")
	if err != nil {
		t.Errorf("Failed to get cell value: %v", err)
	}
	if val != "A2" {
		t.Errorf("Expected cell A2 to contain 'A2', got '%s'", val)
	}

	// 6. Verification - 3 players → 3 rows written, last row is on second page
	val3, err := f.GetCellValue(sheetName, "A3")
	if err != nil {
		t.Errorf("Failed to get cell A3 value: %v", err)
	}
	if val3 != "A3" {
		t.Errorf("Expected cell A3 to contain 'A3', got '%s'", val3)
	}
}
