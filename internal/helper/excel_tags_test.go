package helper

import (
	"testing"

	excelize "github.com/xuri/excelize/v2"
)

// TestCreateTagsSheetQR verifies that when publicURL and player.Number are set,
// AddPictureFromBytes is called and both tag copies (rows 1 and 2) contain an
// embedded picture.
func TestCreateTagsSheetQR(t *testing.T) {
	f := excelize.NewFile()
	pools := []Pool{
		{
			PoolName: "Pool A",
			Players: []Player{
				{Name: "Alice", PoolPosition: 1, Number: "K1", Dojo: "Dojo Alice"},
			},
		},
	}
	if err := CreateTagsSheet(f, pools, "https://example.com"); err != nil {
		t.Fatalf("CreateTagsSheet: %v", err)
	}
	for _, cell := range []string{"A1", "A2"} {
		pics, err := f.GetPictures(SheetTags, cell)
		if err != nil {
			t.Errorf("GetPictures(%s): %v", cell, err)
			continue
		}
		if len(pics) == 0 {
			t.Errorf("expected QR picture in cell %s, got none", cell)
		}
	}
}

func TestCreateTagsSheet(t *testing.T) {
	// 1. Setup
	f := excelize.NewFile()
	// bc-pnum G5/G6: the tag IS the competitor's Number now, with no
	// pool-letter fallback (CreateTagsSheet no longer composes one). Set
	// Number directly on the fixture, as NumberPools would have before
	// this sheet is ever built.
	pools := []Pool{
		{
			PoolName: "Pool A",
			Players: []Player{
				{Name: "Player 1", PoolPosition: 1, Dojo: "Dojo Player 1", Number: "K1"},
				{Name: "Player 2", PoolPosition: 2, Dojo: "Dojo Player 2", Number: "K2"},
				{Name: "Player 3", PoolPosition: 3, Dojo: "Dojo Player 3", Number: "K3"},
			},
		},
	}

	// 2. Execution
	err := CreateTagsSheet(f, pools, "")
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

	// 4. Verification - Row Height (409pt = excelize max, ~half A4 portrait)
	height, err := f.GetRowHeight(sheetName, 1)
	if err != nil {
		t.Fatalf("Failed to get row height: %v", err)
	}
	if height != 409 {
		t.Errorf("Expected row height 409, got %f", height)
	}

	// 5. Verification - each tag appears twice consecutively (same A4 page)
	// Player 1 (K1): rows 1 and 2
	// Player 2 (K2): rows 3 and 4
	// Player 3 (K3): rows 5 and 6
	expected := map[string]string{
		"A1": "K1", "A2": "K1",
		"A3": "K2", "A4": "K2",
		"A5": "K3", "A6": "K3",
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

// TestCreateTagsSheet_ClippingFix pins bc-pnum A9: rendered with LibreOffice,
// a 4+ character tag (now reachable everywhere the prefix can be up to 3
// characters) used to be sheared at the page edge and spilled blank overflow
// pages. A unit test cannot rasterise a PDF, so this pins the three things a
// unit test CAN see directly: the narrowed column width, the ShrinkToFit
// flag on the tag style, and a print area confining the sheet to what was
// actually written (see the orchestrator's browser/LibreOffice render check
// for the rasterised confirmation).
func TestCreateTagsSheet_ClippingFix(t *testing.T) {
	f := excelize.NewFile()
	pools := []Pool{
		{PoolName: "Pool A", Players: []Player{
			{Name: "Player 1", PoolPosition: 1, Dojo: "Dojo Player 1", Number: "KOR19"},
		}},
	}
	if err := CreateTagsSheet(f, pools, ""); err != nil {
		t.Fatalf("CreateTagsSheet failed: %v", err)
	}

	width, err := f.GetColWidth(SheetTags, "A")
	if err != nil {
		t.Fatalf("GetColWidth: %v", err)
	}
	if width != 88 {
		t.Errorf("expected column A width 88 (fits the A4-portrait printable width), got %v", width)
	}

	styleID, err := f.GetCellStyle(SheetTags, "A1")
	if err != nil {
		t.Fatalf("GetCellStyle: %v", err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil {
		t.Fatalf("GetStyle: %v", err)
	}
	if style.Alignment == nil || !style.Alignment.ShrinkToFit {
		t.Errorf("expected the tag style to set ShrinkToFit, got %+v", style.Alignment)
	}

	dn := f.GetDefinedName()
	found := false
	for _, d := range dn {
		if d.Name == "_xlnm.Print_Area" && d.Scope == SheetTags {
			found = true
			if d.RefersTo != "'Tags'!$A$1:$A$2" {
				t.Errorf("expected print area 'Tags'!$A$1:$A$2 (one player -> 2 rows), got %q", d.RefersTo)
			}
		}
	}
	if !found {
		t.Error("expected a _xlnm.Print_Area defined name scoped to the Tags sheet")
	}
}

// TestCreateTagsSheet_ZeroPlayersNoPrintArea pins a review finding on top of
// bc-pnum A9: with zero players (reachable -- an export of a mixed
// competition still in setup, before any pool has a member, or a
// competition with pools but every pool empty) the write loop never runs, so
// `row` stays at its initial 1 and row-1 is 0. SetPrintArea(f, sheet, 1, 0)
// would define the INVALID range "$A$1:$A$0" (row 0 does not exist). Must
// be skipped entirely, not merely tolerated: no players means nothing was
// written, so there is nothing to scope a print area to.
func TestCreateTagsSheet_ZeroPlayersNoPrintArea(t *testing.T) {
	f := excelize.NewFile()
	if err := CreateTagsSheet(f, nil, ""); err != nil {
		t.Fatalf("CreateTagsSheet failed: %v", err)
	}
	for _, d := range f.GetDefinedName() {
		if d.Name == "_xlnm.Print_Area" && d.Scope == SheetTags {
			t.Errorf("expected NO print area for a zero-player Tags sheet, got %q", d.RefersTo)
		}
	}
}

// TestCreateTagsSheet_EmptyNumber pins D1 (bc-pnum): a player with no Number
// gets an EMPTY tag, never the pool-letter substitute ("A1", "A2", ...)
// CreateTagsSheet used to compose when Number was blank. That fallback has
// been removed outright (tag is now always exactly player.Number); if it
// were reintroduced, these cells would read "A1"/"A2" instead of "" and this
// test would go red. No prior test exercised the empty-Number path: every
// existing fixture set Number, so the fallback's removal was previously
// unpinned.
func TestCreateTagsSheet_EmptyNumber(t *testing.T) {
	f := excelize.NewFile()
	pools := []Pool{
		{
			PoolName: "Pool A",
			Players: []Player{
				{Name: "Player 1", PoolPosition: 1, Dojo: "Dojo Player 1"},
				{Name: "Player 2", PoolPosition: 2, Dojo: "Dojo Player 2"},
			},
		},
	}

	if err := CreateTagsSheet(f, pools, ""); err != nil {
		t.Fatalf("CreateTagsSheet failed: %v", err)
	}

	sheetName := SheetTags
	for _, cell := range []string{"A1", "A2", "A3", "A4"} {
		got, err := f.GetCellValue(sheetName, cell)
		if err != nil {
			t.Errorf("Failed to get cell %s: %v", cell, err)
			continue
		}
		if got != "" {
			t.Errorf("Expected cell %s to be empty (no Number, no fallback), got %q", cell, got)
		}
	}
}

// TestCreateTagsSheet_QRSitsBelowTheNumber pins the QR placement the bc-pnum
// review settled by rendering: the code sits in the bottom-left band of the
// tag, below the number, at 0.45 scale (about 2.4 cm on paper). The old
// placement (OffsetX 57, OffsetY 242 at the number's vertical centre) assumed
// white space left of a short number; a shrink-to-fit "KOR20" fills the
// column and the code landed on its first letter.
func TestCreateTagsSheet_QRSitsBelowTheNumber(t *testing.T) {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	pools := []Pool{{PoolName: "Pool A", Players: []Player{{Name: "Alice", Dojo: "Seishin", Number: "KOR20"}}}}
	if err := CreateTagsSheet(f, pools, "https://example.org/viewer"); err != nil {
		t.Fatalf("CreateTagsSheet: %v", err)
	}
	pics, err := f.GetPictures(SheetTags, "A1")
	if err != nil {
		t.Fatalf("GetPictures: %v", err)
	}
	if len(pics) != 1 {
		t.Fatalf("expected one QR picture on the first tag, got %d", len(pics))
	}
	got := pics[0].Format
	if got == nil {
		t.Fatal("QR picture has no graphic options")
	}
	if got.OffsetX != 12 || got.OffsetY != 440 {
		t.Errorf("QR must sit in the bottom-left band below the number (OffsetX 12, OffsetY 440 px), got (%d, %d)", got.OffsetX, got.OffsetY)
	}
	if got.ScaleX != 0.45 || got.ScaleY != 0.45 {
		t.Errorf("QR scale must be 0.45 (about 2.4 cm on paper), got (%v, %v)", got.ScaleX, got.ScaleY)
	}
}
