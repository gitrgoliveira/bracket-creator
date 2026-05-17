package helper

// excel_kachinuki.go renders the "Kachinuki Detail" sheet — one section per
// kachinuki ("winner-stays-on") team match with full bout-by-bout detail.
//
// The main Pool Matches / Elimination Matches sheets continue to render
// kachinuki team matches using the existing 8-column-per-court layout
// (CourtsColumnsPerCourt = 8 in constants.go). This separate sheet uses a
// flexible 8-column layout chosen for readability — NOT bound by
// CourtsColumnsPerCourt — and is emitted only by the engine export path
// (internal/engine/export.go) when a competition has teamMatchType=kachinuki
// AND at least one kachinuki match carries bout data.
//
// Layout per match section (rows are 1-based relative to the section start):
//
//	Row 1: Title — "<label> (Kachinuki)"
//	Row 2: Subtitle — "<Side A team> vs <Side B team>"
//	Row 3: Column headers — Bout #, Side A, Score A, vs, Score B, Side B, Winner, Decision
//	Rows 4..N+3: one row per bout (N = len(bouts))
//	Row N+4: Summary — eliminations per team, match outcome
//
// Sections are separated by a single blank row. The first section starts at
// row 1.
//
// CHK037, T195–T203.

import (
	"fmt"
	"strconv"

	excelize "github.com/xuri/excelize/v2"
)

// KachinukiBout is one bout in a kachinuki team match.
type KachinukiBout struct {
	Position  int    // 1-based bout index within the team match
	SideAName string // player name for Side A
	SideAPos  string // lineup position (Senpo, Jiho, Chuken, Fukusho, Taisho) — may be empty
	ScoreA    string // accumulated ippon string (e.g. "MK", "MMK") or empty
	SideBName string
	SideBPos  string
	ScoreB    string
	Winner    string // player name of the winner; empty for hikiwake
	Decision  string // canonical decision wire value (fought / hikiwake / kiken / fusenpai / fusensho / daihyosen / kachinuki-exhaustion)
}

// KachinukiMatchDetail is a single team match's bout log with team-level
// summary metadata. One section is rendered per entry in
// WriteKachinukiDetailSheet.
type KachinukiMatchDetail struct {
	Label        string // human-readable match identifier (e.g. "Pool A - Match 1")
	SideATeam    string // team name on Side A
	SideBTeam    string // team name on Side B
	Bouts        []KachinukiBout
	Winner       string // winning team name; empty when the match did not end with a winner
	Decision     string // canonical wire decision for the parent match (typically "kachinuki-exhaustion")
	EliminationA int    // count of Side A players retired by the end of the match
	EliminationB int    // count of Side B players retired by the end of the match
}

// kachinukiDetailColumns enumerates the column letters used on the detail
// sheet. The layout is flexible — NOT bound by CourtsColumnsPerCourt — so
// readability wins over alignment with the main match sheets.
const (
	kachinukiColBout     = "A"
	kachinukiColSideA    = "B"
	kachinukiColScoreA   = "C"
	kachinukiColVs       = "D"
	kachinukiColScoreB   = "E"
	kachinukiColSideB    = "F"
	kachinukiColWinner   = "G"
	kachinukiColDecision = "H"
)

// WriteKachinukiDetailSheet creates the SheetKachinukiDetail sheet and
// writes one section per match. When matches is empty the sheet is NOT
// created — the caller is responsible for checking the input length AND
// the renderer guards against accidental emission of an empty sheet.
func WriteKachinukiDetailSheet(f *excelize.File, matches []KachinukiMatchDetail) error {
	// Skip when there are no matches to render. The detail sheet is
	// purely additive — never create an empty sheet (T201 acceptance).
	if len(matches) == 0 {
		return nil
	}
	// Also skip when every match has zero bouts: there is nothing to show.
	hasBouts := false
	for _, m := range matches {
		if len(m.Bouts) > 0 {
			hasBouts = true
			break
		}
	}
	if !hasBouts {
		return nil
	}

	sheet := SheetKachinukiDetail
	// Create the sheet if it doesn't already exist. (Engine export creates
	// the workbook via NewFileFromScratch which does NOT include the
	// detail sheet — it's opt-in.)
	if idx, err := f.GetSheetIndex(sheet); err != nil || idx < 0 {
		if _, err := f.NewSheet(sheet); err != nil {
			return fmt.Errorf("creating sheet %q: %w", sheet, err)
		}
	}

	// Set column widths for readability. These are unrelated to the
	// per-court widths of Pool Matches / Elimination Matches.
	colWidths := []struct {
		from, to string
		w        float64
	}{
		{kachinukiColBout, kachinukiColBout, 8},
		{kachinukiColSideA, kachinukiColSideA, 24},
		{kachinukiColScoreA, kachinukiColScoreA, 10},
		{kachinukiColVs, kachinukiColVs, 5},
		{kachinukiColScoreB, kachinukiColScoreB, 10},
		{kachinukiColSideB, kachinukiColSideB, 24},
		{kachinukiColWinner, kachinukiColWinner, 18},
		{kachinukiColDecision, kachinukiColDecision, 22},
	}
	for _, cw := range colWidths {
		handleExcelError("SetColWidth", f.SetColWidth(sheet, cw.from, cw.to, cw.w))
	}

	row := 1
	for i, match := range matches {
		if len(match.Bouts) == 0 {
			// Skip matches with no bouts — they would render an empty
			// section. The summary row alone has no value without a
			// bout list to give it context.
			continue
		}
		nextRow := writeKachinukiMatchSection(f, sheet, match, row)
		// Separator blank row between sections (except after the last).
		if i < len(matches)-1 {
			nextRow++
		}
		row = nextRow
	}
	return nil
}

// writeKachinukiMatchSection writes a single match section starting at
// `startRow` and returns the row index immediately after the section
// (the row a separator would occupy).
func writeKachinukiMatchSection(f *excelize.File, sheet string, match KachinukiMatchDetail, startRow int) int {
	titleStyle := getPoolHeaderStyle(f)
	headerStyle := getPoolHeaderStyle(f)
	textStyle := getTextStyle(f)
	summaryStyle := getGreyTextStyle(f)

	titleRow := startRow
	subtitleRow := startRow + 1
	headerRow := startRow + 2
	firstBoutRow := startRow + 3
	summaryRow := firstBoutRow + len(match.Bouts)

	// --- Title row (merged across A..H) ---
	titleCell := kachinukiColBout + strconv.Itoa(titleRow)
	titleEndCell := kachinukiColDecision + strconv.Itoa(titleRow)
	handleExcelError("MergeCell", f.MergeCell(sheet, titleCell, titleEndCell))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, titleCell, fmt.Sprintf("%s (Kachinuki)", match.Label)))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, titleCell, titleEndCell, titleStyle))

	// --- Subtitle row (merged) ---
	subtitleCell := kachinukiColBout + strconv.Itoa(subtitleRow)
	subtitleEndCell := kachinukiColDecision + strconv.Itoa(subtitleRow)
	handleExcelError("MergeCell", f.MergeCell(sheet, subtitleCell, subtitleEndCell))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, subtitleCell, fmt.Sprintf("%s vs %s", match.SideATeam, match.SideBTeam)))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, subtitleCell, subtitleEndCell, textStyle))

	// --- Header row ---
	headers := []struct {
		col, label string
	}{
		{kachinukiColBout, "Bout #"},
		{kachinukiColSideA, "Side A"},
		{kachinukiColScoreA, "Score A"},
		{kachinukiColVs, "vs"},
		{kachinukiColScoreB, "Score B"},
		{kachinukiColSideB, "Side B"},
		{kachinukiColWinner, "Winner"},
		{kachinukiColDecision, "Decision"},
	}
	for _, h := range headers {
		cell := h.col + strconv.Itoa(headerRow)
		handleExcelError("SetCellValue", f.SetCellValue(sheet, cell, h.label))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheet, cell, cell, headerStyle))
	}

	// --- Bout rows ---
	for i, bout := range match.Bouts {
		boutRow := firstBoutRow + i
		writeKachinukiBoutRow(f, sheet, bout, boutRow, textStyle)
	}

	// --- Summary row ---
	writeKachinukiSummaryRow(f, sheet, match, summaryRow, summaryStyle)

	return summaryRow + 1
}

// writeKachinukiBoutRow writes one bout's columns. A hikiwake bout leaves
// the Winner column blank and stamps the decision wire value verbatim in
// the Decision column.
func writeKachinukiBoutRow(f *excelize.File, sheet string, bout KachinukiBout, row int, style int) {
	rowStr := strconv.Itoa(row)

	// Column A: bout number
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColBout+rowStr, strconv.Itoa(bout.Position)))

	// Column B: Side A name + position (formatted "Name (Position)" when
	// position is set, else just the name).
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColSideA+rowStr, formatKachinukiPlayer(bout.SideAName, bout.SideAPos)))

	// Column C: Score A
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColScoreA+rowStr, bout.ScoreA))

	// Column D: literal "vs"
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColVs+rowStr, "vs"))

	// Column E: Score B
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColScoreB+rowStr, bout.ScoreB))

	// Column F: Side B name + position
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColSideB+rowStr, formatKachinukiPlayer(bout.SideBName, bout.SideBPos)))

	// Column G: Winner (left blank on hikiwake — decision column carries the
	// outcome label).
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColWinner+rowStr, bout.Winner))

	// Column H: Decision wire value
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColDecision+rowStr, bout.Decision))

	// Apply text style across the row for visual consistency.
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, kachinukiColBout+rowStr, kachinukiColDecision+rowStr, style))
}

// writeKachinukiSummaryRow writes the per-team elimination tallies plus
// the match outcome (winner team, decision). The label "Summary" lives in
// column A; the elimination counts mirror the Side A / Side B columns so
// readers can see at a glance which team was exhausted.
func writeKachinukiSummaryRow(f *excelize.File, sheet string, match KachinukiMatchDetail, row int, style int) {
	rowStr := strconv.Itoa(row)

	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColBout+rowStr, "Summary"))

	// Column B: Side A eliminations — e.g. "1 eliminated"
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColSideA+rowStr,
		fmt.Sprintf("%d eliminated", match.EliminationA)))

	// Column F: Side B eliminations
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColSideB+rowStr,
		fmt.Sprintf("%d eliminated", match.EliminationB)))

	// Column G: winning team name (verbatim).
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColWinner+rowStr, match.Winner))

	// Column H: decision wire value (typically "kachinuki-exhaustion").
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColDecision+rowStr, match.Decision))

	// Style the whole row.
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, kachinukiColBout+rowStr, kachinukiColDecision+rowStr, style))
}

// formatKachinukiPlayer is a pure helper that combines a player's name and
// lineup position for display on the detail sheet. Empty position → just
// the name; empty name → empty string (defensive — the renderer should
// never receive an empty player name for a played bout).
func formatKachinukiPlayer(name, position string) string {
	if name == "" {
		return ""
	}
	if position == "" {
		return name
	}
	return fmt.Sprintf("%s (%s)", name, position)
}
