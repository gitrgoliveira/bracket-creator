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

// TestNameIDStyle_ShrinkToFit pins the name half of the Names-to-Print ruling:
// the name column never changes size, so a long name must shrink into it.
func TestNameIDStyle_ShrinkToFit(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	style, err := f.GetStyle(buildNameIDStyle(f))
	if err != nil {
		t.Fatalf("GetStyle: %v", err)
	}
	if style.Alignment == nil || !style.Alignment.ShrinkToFit {
		t.Errorf("expected the Names-to-Print name style to set ShrinkToFit, got %+v", style.Alignment)
	}
	if style.Alignment != nil && style.Alignment.WrapText {
		t.Errorf("the name cell must shrink, never wrap: %+v", style.Alignment)
	}
}

// TestNamesToPrintColumnsFitOnePage pins the operator ruling that the number
// and name columns always sit side by side on one A3 landscape page: their
// fixed widths must not exceed what the page offers at the sheet's margins.
func TestNamesToPrintColumnsFitOnePage(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	const sheet = "Names to Print A"
	if _, err := f.NewSheet(sheet); err != nil {
		t.Fatalf("NewSheet: %v", err)
	}
	setupNamesToPrintLayout(f, sheet)
	a, err := f.GetColWidth(sheet, "A")
	if err != nil {
		t.Fatalf("GetColWidth A: %v", err)
	}
	b, err := f.GetColWidth(sheet, "B")
	if err != nil {
		t.Fatalf("GetColWidth B: %v", err)
	}
	if a+b > namesToPrintPageWidthUnits {
		t.Errorf("Names to Print columns A (%v) + B (%v) = %v exceed the %d units an A3 landscape page offers; the sheet would print numbers and names as separate page runs", a, b, a+b, namesToPrintPageWidthUnits)
	}
	if a != namesToPrintNumberColWidth || b != namesToPrintNameColWidth {
		t.Errorf("column widths must be the fixed constants (%d, %d), got (%v, %v)", namesToPrintNumberColWidth, namesToPrintNameColWidth, a, b)
	}
}
