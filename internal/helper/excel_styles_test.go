package helper

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestGetNameIDStyle(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	style := getNameIDStyle(f)
	if style <= 0 {
		t.Errorf("Expected positive style ID, got %d", style)
	}
}

func TestGetNameIDPositionStyle(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	style := getNameIDPositionStyle(f)
	if style <= 0 {
		t.Errorf("Expected positive style ID, got %d", style)
	}
}

func TestGetTimeStyle(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	style := getTimeStyle(f)
	if style <= 0 {
		t.Errorf("Expected positive style ID, got %d", style)
	}
}

func TestGetDurationStyle(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	style := getDurationStyle(f)
	if style <= 0 {
		t.Errorf("Expected positive style ID, got %d", style)
	}
}
