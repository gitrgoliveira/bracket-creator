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

	sheetName := SheetTags

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

	// 5. Verification - each tag appears twice consecutively (same A4 page)
	// Player 1 (A1): rows 1 and 2
	// Player 2 (A2): rows 3 and 4
	// Player 3 (A3): rows 5 and 6
	expected := map[string]string{
		"A1": "A1", "A2": "A1",
		"A3": "A2", "A4": "A2",
		"A5": "A3", "A6": "A3",
	}
	for cell, want := range expected {
		got, err := f.GetCellValue(sheetName, cell)
		if err != nil {
			t.Errorf("Failed to get cell %s: %v", cell, err)
			continue
		}
		if got != want {
			t.Errorf("Expected cell %s to contain %q, got %q", cell, want, got)
		}
	}
}
