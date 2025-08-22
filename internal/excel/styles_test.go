package excel

import (
	"testing"

	excelize "github.com/xuri/excelize/v2"
)

func TestNewStyleManager(t *testing.T) {
	file := excelize.NewFile()
	sm := NewStyleManager(file)

	if sm == nil {
		t.Fatal("Expected StyleManager to not be nil")
	}

	if sm.file != file {
		t.Error("Expected StyleManager.file to be the provided file")
	}
}

func TestGetTextStyle(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	sm := NewStyleManager(file)

	styleID := sm.GetTextStyle()

	// The style ID should be a positive integer
	if styleID <= 0 {
		t.Errorf("Expected positive style ID, got %d", styleID)
	}
}

func TestGetPoolHeaderStyle(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	sm := NewStyleManager(file)

	styleID := sm.GetPoolHeaderStyle()

	// The style ID should be a positive integer
	if styleID <= 0 {
		t.Errorf("Expected positive style ID, got %d", styleID)
	}
}

func TestGetBorderStyleLeft(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	sm := NewStyleManager(file)

	styleID := sm.GetBorderStyleLeft()

	// The style ID should be a positive integer
	if styleID <= 0 {
		t.Errorf("Expected positive style ID, got %d", styleID)
	}
}

func TestGetBorderStyleBottom(t *testing.T) {
	file := excelize.NewFile()
	defer file.Close()
	sm := NewStyleManager(file)

	styleID := sm.GetBorderStyleBottom()

	// The style ID should be a positive integer
	if styleID <= 0 {
		t.Errorf("Expected positive style ID, got %d", styleID)
	}
}

// Additional style function tests would follow the same pattern
