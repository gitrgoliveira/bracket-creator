// Package excel handles Excel file creation and management.
package excel

import (
	"fmt"

	excelize "github.com/xuri/excelize/v2"
)

// NewFileFromScratch creates a new *excelize.File with all the sheets and
// initial structure that was previously provided by template.xlsx.  It
// replaces the embedded binary so the project no longer requires
// template.xlsx at build time.
func NewFileFromScratch() (*excelize.File, error) {
	f := excelize.NewFile()

	// excelize.NewFile always creates "Sheet1"; rename it to "data".
	if err := f.SetSheetName("Sheet1", "data"); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("renaming Sheet1 to data: %w", err)
	}

	for _, name := range []string{
		"Time Estimator", "Pool Draw", "Pool Matches",
		"Elimination Matches", "Names to Print", "Tree",
	} {
		if _, err := f.NewSheet(name); err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("creating sheet %q: %w", name, err)
		}
	}

	setupTimeEstimatorSheet(f)
	setupPoolDrawSheet(f)
	setupPoolMatchesSheet(f)
	setupEliminationMatchesSheet(f)
	setupNamesToPrintSheet(f)
	setupTreeSheet(f)

	// Activate the "data" sheet on open.
	if idx, err := f.GetSheetIndex("data"); err == nil {
		f.SetActiveSheet(idx)
	}
	return f, nil
}

// mustStyle creates an Excel style and panics if creation fails (malformed
// style definition — a programming error, not a runtime condition).
func mustStyle(f *excelize.File, s *excelize.Style) int {
	id, err := f.NewStyle(s)
	if err != nil {
		panic(fmt.Sprintf("template: failed to create style: %v", err))
	}
	return id
}

// logSetupErr prints non-fatal errors that occur during sheet setup, matching
// the error-handling pattern used in the helper package.
func logSetupErr(op string, err error) {
	if err != nil {
		fmt.Printf("template setup [%s]: %v\n", op, err)
	}
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

// ---------------------------------------------------------------------------
// Time Estimator
// ---------------------------------------------------------------------------

func setupTimeEstimatorSheet(f *excelize.File) {
	const s = "Time Estimator"

	// Column widths
	logSetupErr("col A", f.SetColWidth(s, "A", "A", 21.83))
	logSetupErr("col B-C", f.SetColWidth(s, "B", "C", 10))
	logSetupErr("col D", f.SetColWidth(s, "D", "D", 14.66))
	logSetupErr("col E", f.SetColWidth(s, "E", "E", 17))
	logSetupErr("col F-G", f.SetColWidth(s, "F", "G", 13.16))
	logSetupErr("col H", f.SetColWidth(s, "H", "H", 10))

	// Row heights: rows 1 and 7 are tall header rows; data rows are 16 pt.
	logSetupErr("row 1", f.SetRowHeight(s, 1, 51))
	for r := 2; r <= 6; r++ {
		logSetupErr(fmt.Sprintf("row %d", r), f.SetRowHeight(s, r, 16))
	}
	logSetupErr("row 7", f.SetRowHeight(s, 7, 51))
	for r := 8; r <= 11; r++ {
		logSetupErr(fmt.Sprintf("row %d", r), f.SetRowHeight(s, r, 16))
	}

	// Styles
	hdr := mustStyle(f, &excelize.Style{
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Size: 12, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})
	inp := mustStyle(f, &excelize.Style{
		Font:      &excelize.Font{Family: "Calibri", Size: 12, Color: "000000"},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	customFmt := `[$-F400]h:mm:ss\ AM/PM`
	tim := mustStyle(f, &excelize.Style{
		Font:         &excelize.Font{Family: "Calibri", Size: 12, Color: "000000"},
		Alignment:    &excelize.Alignment{Vertical: "center", WrapText: true},
		CustomNumFmt: &customFmt,
	})
	emp := mustStyle(f, &excelize.Style{
		Font:      &excelize.Font{Family: "Calibri", Size: 12, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
	})

	// duration helpers: convert minutes / seconds to Excel's fraction-of-a-day
	mins := func(m float64) float64 { return m / 1440.0 }
	secs := func(s float64) float64 { return s / 86400.0 }

	// --- Row 1: pool-phase column headers ---
	for i, h := range []string{
		"Number of pools", "Team size", "Matches per pool",
		"Time per match", "Total time for matches",
		"Padding time for rotation", "Time for breaks", "Total Time",
	} {
		col, _ := excelize.ColumnNumberToName(i + 1)
		logSetupErr("hdr val", f.SetCellValue(s, col+"1", h))
		logSetupErr("hdr sty", f.SetCellStyle(s, col+"1", col+"1", hdr))
	}

	// --- Row 2: scenario — 2 pools, 3-person teams, 3 matches each of 3 min ---
	logSetupErr("A2", f.SetCellInt(s, "A2", 2))
	logSetupErr("B2", f.SetCellInt(s, "B2", 3))
	logSetupErr("C2", f.SetCellInt(s, "C2", 3))
	logSetupErr("D2", f.SetCellValue(s, "D2", mins(3)))
	logSetupErr("E2", f.SetCellFormula(s, "E2", "(A2*B2*C2*D2)"))
	logSetupErr("F2", f.SetCellValue(s, "F2", secs(30)))
	logSetupErr("G2", f.SetCellValue(s, "G2", mins(30)))
	logSetupErr("sty A2-C2", f.SetCellStyle(s, "A2", "C2", inp))
	logSetupErr("sty D2-H2", f.SetCellStyle(s, "D2", "H2", tim))

	// --- Row 3: scenario — all zeros (blank starting point) ---
	logSetupErr("A3", f.SetCellInt(s, "A3", 0))
	logSetupErr("B3", f.SetCellInt(s, "B3", 0))
	logSetupErr("C3", f.SetCellInt(s, "C3", 0))
	logSetupErr("D3", f.SetCellValue(s, "D3", mins(3)))
	logSetupErr("E3", f.SetCellFormula(s, "E3", "(A3*B3*C3*D3)"))
	logSetupErr("F3", f.SetCellValue(s, "F3", secs(30)))
	logSetupErr("G3", f.SetCellValue(s, "G3", mins(30)))
	logSetupErr("H3", f.SetCellFormula(s, "H3", "E3+(A3*C3*F3)+G3"))
	logSetupErr("sty A3-C3", f.SetCellStyle(s, "A3", "C3", inp))
	logSetupErr("sty D3-H3", f.SetCellStyle(s, "D3", "H3", tim))

	// --- Rows 4–6: empty styled spacer rows ---
	for r := 4; r <= 6; r++ {
		row := fmt.Sprintf("%d", r)
		logSetupErr("emp row", f.SetCellStyle(s, "A"+row, "H"+row, emp))
	}

	// --- Row 7: elimination-phase column headers ---
	for i, h := range []string{
		"Number of Elimination Matches", "Team size", "",
		"Time per match", "Total time for matches",
		"Padding time for rotation", "Time for breaks", "Total Time",
	} {
		col, _ := excelize.ColumnNumberToName(i + 1)
		if h != "" {
			logSetupErr("elim hdr val", f.SetCellValue(s, col+"7", h))
		}
		logSetupErr("elim hdr sty", f.SetCellStyle(s, col+"7", col+"7", hdr))
	}

	// --- Row 8: scenario — 5 elim matches, 3-person teams, 4 min each ---
	logSetupErr("A8", f.SetCellInt(s, "A8", 5))
	logSetupErr("B8", f.SetCellInt(s, "B8", 3))
	logSetupErr("D8", f.SetCellValue(s, "D8", mins(4)))
	logSetupErr("E8", f.SetCellFormula(s, "E8", "(A8*B8*D8)"))
	logSetupErr("F8", f.SetCellValue(s, "F8", secs(30)))
	logSetupErr("G8", f.SetCellValue(s, "G8", mins(30)))
	logSetupErr("H8", f.SetCellFormula(s, "H8", "E8+(A8*F8)+G8"))
	logSetupErr("sty A8-C8", f.SetCellStyle(s, "A8", "C8", inp))
	logSetupErr("sty D8-H8", f.SetCellStyle(s, "D8", "H8", tim))

	// --- Rows 9–11: empty styled spacer rows ---
	for r := 9; r <= 11; r++ {
		row := fmt.Sprintf("%d", r)
		logSetupErr("emp row", f.SetCellStyle(s, "A"+row, "H"+row, emp))
	}
}

// ---------------------------------------------------------------------------
// Pool Draw
// ---------------------------------------------------------------------------

func setupPoolDrawSheet(f *excelize.File) {
	const s = "Pool Draw"

	// Spacer columns A/C/E/G = 8.83.
	for _, col := range []string{"A", "C", "E", "G"} {
		logSetupErr("spacer col", f.SetColWidth(s, col, col, 8.83))
	}

	// Row heights
	logSetupErr("row 1", f.SetRowHeight(s, 1, 17))
	logSetupErr("row 2", f.SetRowHeight(s, 2, 20))
	for r := 3; r <= 7; r++ {
		logSetupErr(fmt.Sprintf("row %d", r), f.SetRowHeight(s, r, 17))
	}

	// Title area: merge B2:F2 so the formula written by AddPoolsToSheet spans
	// the full pool-columns width.
	logSetupErr("MergeCell B2:F2", f.MergeCell(s, "B2", "F2"))

	// B2 title style: bold 16 pt, underlined, centred.
	titleStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Size: 16, Underline: "single"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	logSetupErr("B2 style", f.SetCellStyle(s, "B2", "B2", titleStyle))

	// A2, G2: same bold/underline chrome without centre-align.
	sideStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Family: "Calibri", Bold: true, Size: 16, Underline: "single"},
	})
	logSetupErr("A2 style", f.SetCellStyle(s, "A2", "A2", sideStyle))
	logSetupErr("G2 style", f.SetCellStyle(s, "G2", "G2", sideStyle))

	// Row 3: bold, underlined, centred (visual separator row).
	row3Style, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Size: 12, Underline: "single"},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	logSetupErr("row3 style", f.SetCellStyle(s, "A3", "H3", row3Style))

	// Row 4: plain 12 pt spacer before pool content starts at row 5.
	row4Style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Family: "Calibri", Size: 12},
	})
	logSetupErr("row4 style", f.SetCellStyle(s, "A4", "H4", row4Style))
}

// ---------------------------------------------------------------------------
// Pool Matches
// ---------------------------------------------------------------------------

func setupPoolMatchesSheet(f *excelize.File) {
	const s = "Pool Matches"

	// Column widths from the original template.
	// The code overrides the columns it actively uses, so these serve as
	// defaults for additional courts in multi-court layouts.
	for _, cw := range []struct {
		from, to string
		w        float64
	}{
		{"A", "A", 34.83}, {"B", "F", 3.83}, {"G", "G", 34.83},
		{"H", "H", 3.83}, {"I", "I", 34.83}, {"J", "N", 3.83},
		{"O", "O", 34.83}, {"P", "T", 10.83},
	} {
		logSetupErr("col", f.SetColWidth(s, cw.from, cw.to, cw.w))
	}
}

// ---------------------------------------------------------------------------
// Elimination Matches
// ---------------------------------------------------------------------------

func setupEliminationMatchesSheet(f *excelize.File) {
	const s = "Elimination Matches"

	// Same column structure as Pool Matches.
	for _, cw := range []struct {
		from, to string
		w        float64
	}{
		{"A", "A", 34.83}, {"B", "F", 3.83}, {"G", "G", 34.83},
		{"H", "H", 3.83}, {"I", "I", 34.83}, {"J", "N", 3.83},
		{"O", "O", 34.83}, {"P", "T", 10.83},
	} {
		logSetupErr("col", f.SetColWidth(s, cw.from, cw.to, cw.w))
	}
}

// ---------------------------------------------------------------------------
// Names to Print
// ---------------------------------------------------------------------------

func setupNamesToPrintSheet(f *excelize.File) {
	const s = "Names to Print"

	// Apply column-level default styles so any cell the code doesn't
	// explicitly restyle still renders at the correct font size.
	sideStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Size: 28, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	nameStyle, _ := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Size: 72, Color: "000000"},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	logSetupErr("col A style", f.SetColStyle(s, "A", sideStyle))
	logSetupErr("col B style", f.SetColStyle(s, "B", nameStyle))
}

// ---------------------------------------------------------------------------
// Tree
// ---------------------------------------------------------------------------

func setupTreeSheet(f *excelize.File) {
	const s = "Tree"

	// Column widths: A is the wide label column, B is a medium header column,
	// and C onward are the narrow bracket-line columns.
	// These widths are inherited by every "Tree N" sheet created via CopySheet.
	logSetupErr("col A", f.SetColWidth(s, "A", "A", 25))
	logSetupErr("col B", f.SetColWidth(s, "B", "B", 10))
	logSetupErr("col C-Z", f.SetColWidth(s, "C", "Z", 3.5))

	// Page layout: A4 portrait — matches the original template.
	logSetupErr("SetPageLayout", f.SetPageLayout(s, &excelize.PageLayoutOptions{
		Size:        intPtr(9),
		Orientation: strPtr("portrait"),
	}))
}
