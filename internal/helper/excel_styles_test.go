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

// TestNameIDPositionStyle_ShrinkToFit pins bc-pnum A9's Names-to-Print half:
// a 4+ character competitor number (a prefix up to 3 characters, plus a
// multi-digit counter) used to clip at the cell edge, rasterising several
// distinct numbers ("KOR10".."KOR19") byte-identically in the reproduction.
// No width/font change is needed here, unlike the Tags sheet -- this column's
// width already comes from setupNamesToPrintLayout -- so ShrinkToFit alone
// is the fix.
func TestNameIDPositionStyle_ShrinkToFit(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()

	styleID := getNameIDPositionStyle(f)
	style, err := f.GetStyle(styleID)
	if err != nil {
		t.Fatalf("GetStyle: %v", err)
	}
	if style.Alignment == nil || !style.Alignment.ShrinkToFit {
		t.Errorf("expected the Names-to-Print position style to set ShrinkToFit, got %+v", style.Alignment)
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
