package helper

// excel_kachinuki.go renders the "Kachinuki Detail" sheet, one section per
// kachinuki ("winner-stays-on") team match with full bout-by-bout detail.
//
// The main Pool Matches / Elimination Matches sheets continue to render
// kachinuki team matches using the existing 8-column-per-court layout
// (CourtsColumnsPerCourt = 8 in constants.go). This separate sheet uses a
// flexible 6-column layout chosen for readability, NOT bound by
// CourtsColumnsPerCourt, and is emitted only by the engine export path
// (internal/engine/export.go) when a competition has teamMatchType=kachinuki
// AND at least one kachinuki match carries bout data.
//
// Layout per match section (rows are 1-based relative to the section start),
// Shiro (SideB) LEFT and Aka (SideA) RIGHT throughout, per helper.WhiteLeft
// (operator ruling 2026-09-25, bc-xlcl/bc-kdsc), labelled by COLOUR (the
// white/red header cells the match sheets use) rather than by side words:
//
//	Row 1: Title, "<label> (Kachinuki)"
//	Row 2: Subtitle, "<Shiro team> vs <Aka team>"
//	Row 3: Column headers: Bout # | <Shiro team name>, merged B:C, white header | vs | <Aka team name>, merged E:F, red header
//	Rows 4..N+3: one row per bout (N = len(bouts))
//	Row N+4: Summary, eliminations per team
//
// There is no Winner or Decision column (operator decision 2026-09-27): the
// match's own Pool or Elimination Matches row already carries the result: the
// sheet is six columns, A..F. The centre column (D) carries the one
// closed-set middle mark (domain.MiddleMark: vs/X/(E); kachinuki has no
// daihyosen, so (DH) never appears here) and a per-bout default win or
// withdrawal carries its RESULT mark (domain.SideMarksAB: Ht/Kiken/Fus.)
// beside the score of the side it names, exactly as the main sheets do.
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

// KachinukiBout is one bout in a kachinuki team match. Fields stay SIDE
// ordered (SideA/SideB, i.e. Aka/Shiro), exactly like the main sheets' data:
// WhiteLeft takes the pair in side order and places it in sheet columns, so
// nothing upstream of the sheet writer needs to know about column layout.
type KachinukiBout struct {
	Position  int    // 1-based bout index within the team match
	SideAName string // player name for Side A
	// SideALabel is the squad member's "T10.1"-style label (bc-pnum: "make
	// a team member's label available to the public surfaces"), composed
	// by domain.SquadMemberLabel from the team's competitor number and the
	// member's stable display index. Empty when the side's team has no
	// assigned number yet, or the fighter cannot be resolved to a squad
	// member (no recorded member id on the bout row, or no squad on
	// file) -- the printed sheet then falls back to the name alone,
	// exactly as it did before squad labels existed.
	SideALabel string
	SideAPos   string // lineup position (Senpo, Jiho, Chuken, Fukusho, Taisho), may be empty
	ScoreA     string // accumulated ippon string (e.g. "MK", "MMK") or empty
	SideBName  string
	SideBLabel string // Side B's twin of SideALabel, same rules.
	SideBPos   string
	ScoreB     string
	// Middle is the ONE mark the bout's centre "vs" cell may carry
	// (domain.MiddleMark): "" (the sheet's own "vs" survives), "X"
	// (hikiwake), or "(E)" (overtime). Kachinuki has no daihyosen, so
	// "(DH)" never appears here.
	Middle string
	// MarkA/MarkB are the per-side RESULT marks (domain.SideMarksAB) in
	// SIDE order: Ht (hantei), Kiken (withdrawer) or Fus. (no-show / a
	// per-bout default win, e.g. the exhaustion walkover's fusensho)
	// beside the competitor's own score. Placed into sheet columns
	// through WhiteLeft, same as ScoreA/ScoreB.
	MarkA string
	MarkB string
}

// KachinukiMatchDetail is a single team match's bout log with team-level
// summary metadata. One section is rendered per entry in
// WriteKachinukiDetailSheet.
type KachinukiMatchDetail struct {
	Label        string // human-readable match identifier (e.g. "Pool A - Match 1")
	SideATeam    string // team name on Side A (Aka)
	SideBTeam    string // team name on Side B (Shiro)
	Bouts        []KachinukiBout
	EliminationA int // count of Side A players retired by the end of the match
	EliminationB int // count of Side B players retired by the end of the match
}

// kachinukiDetailColumns enumerates the column letters used on the detail
// sheet, named by POSITION (never by side letter, since which side sits in
// which column is WhiteLeft's decision, not this file's). The layout is
// flexible, NOT bound by CourtsColumnsPerCourt, so readability wins over
// alignment with the main match sheets. Six columns: there is no Winner or
// Decision column (operator decision 2026-09-27).
const (
	kachinukiColBout       = "A"
	kachinukiColLeft       = "B" // Shiro: name + position
	kachinukiColLeftScore  = "C" // Shiro: score + result mark
	kachinukiColVs         = "D" // centre: the one closed-set middle mark, or "vs"
	kachinukiColRightScore = "E" // Aka: score + result mark
	kachinukiColRight      = "F" // Aka: name + position
)

// WriteKachinukiDetailSheet creates the SheetKachinukiDetail sheet and
// writes one section per match. When matches is empty the sheet is NOT
// created, the caller is responsible for checking the input length AND
// the renderer guards against accidental emission of an empty sheet.
func WriteKachinukiDetailSheet(f *excelize.File, matches []KachinukiMatchDetail) error {
	// Skip when there are no matches to render. The detail sheet is
	// purely additive, never create an empty sheet (T201 acceptance).
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
	// detail sheet, it's opt-in.)
	if idx, err := f.GetSheetIndex(sheet); err != nil || idx < 0 {
		if _, err := f.NewSheet(sheet); err != nil {
			return fmt.Errorf("creating sheet %q: %w", sheet, err)
		}
	}

	// Set column widths for readability. These are unrelated to the
	// per-court widths of Pool Matches / Elimination Matches. Column A is
	// wide enough that the summary row's "Summary" label does not clip.
	colWidths := []struct {
		from, to string
		w        float64
	}{
		{kachinukiColBout, kachinukiColBout, 12},
		{kachinukiColLeft, kachinukiColLeft, 24},
		{kachinukiColLeftScore, kachinukiColLeftScore, 10},
		{kachinukiColVs, kachinukiColVs, 5},
		{kachinukiColRightScore, kachinukiColRightScore, 10},
		{kachinukiColRight, kachinukiColRight, 24},
	}
	for _, cw := range colWidths {
		handleExcelError("SetColWidth", f.SetColWidth(sheet, cw.from, cw.to, cw.w))
	}

	row := 1
	for i, match := range matches {
		if len(match.Bouts) == 0 {
			// Skip matches with no bouts, they would render an empty
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

	// One page wide, portrait A4 -- the same layout every other sheet in
	// this workbook uses (SetSheetLayoutPortraitA4DownThenOver for the
	// court-banded sheets); this sheet has no court bands, so the plain
	// single-page-wide variant applies directly.
	SetSheetLayoutPortraitA4(f, sheet)

	return nil
}

// writeKachinukiMatchSection writes a single match section starting at
// `startRow` and returns the row index immediately after the section
// (the row a separator would occupy).
func writeKachinukiMatchSection(f *excelize.File, sheet string, match KachinukiMatchDetail, startRow int) int {
	titleStyle := getPoolHeaderStyle(f)
	textStyle := getTextStyle(f)
	summaryStyle := getGreyTextStyle(f)
	whiteHeaderStyle := getWhiteHeaderStyle(f)
	redHeaderStyle := getRedHeaderStyle(f)

	titleRow := startRow
	subtitleRow := startRow + 1
	headerRow := startRow + 2
	firstBoutRow := startRow + 3
	summaryRow := firstBoutRow + len(match.Bouts)

	leftTeam, rightTeam := WhiteLeft(match.SideATeam, match.SideBTeam)

	// --- Title row (merged across A..F) ---
	titleCell := kachinukiColBout + strconv.Itoa(titleRow)
	titleEndCell := kachinukiColRight + strconv.Itoa(titleRow)
	handleExcelError("MergeCell", f.MergeCell(sheet, titleCell, titleEndCell))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, titleCell, fmt.Sprintf("%s (Kachinuki)", match.Label)))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, titleCell, titleEndCell, titleStyle))

	// --- Subtitle row (merged), Shiro's team first (P1: matches the
	// scoreboard's White-left reading) ---
	subtitleCell := kachinukiColBout + strconv.Itoa(subtitleRow)
	subtitleEndCell := kachinukiColRight + strconv.Itoa(subtitleRow)
	handleExcelError("MergeCell", f.MergeCell(sheet, subtitleCell, subtitleEndCell))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, subtitleCell, fmt.Sprintf("%s vs %s", leftTeam, rightTeam)))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, subtitleCell, subtitleEndCell, textStyle))

	// --- Header row (P1: colour + team name, no side words) ---
	boutHeaderCell := kachinukiColBout + strconv.Itoa(headerRow)
	handleExcelError("SetCellValue", f.SetCellValue(sheet, boutHeaderCell, "Bout #"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, boutHeaderCell, boutHeaderCell, titleStyle))

	leftStart := kachinukiColLeft + strconv.Itoa(headerRow)
	leftEnd := kachinukiColLeftScore + strconv.Itoa(headerRow)
	handleExcelError("MergeCell", f.MergeCell(sheet, leftStart, leftEnd))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, leftStart, leftTeam))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, leftStart, leftEnd, whiteHeaderStyle))

	vsCell := kachinukiColVs + strconv.Itoa(headerRow)
	handleExcelError("SetCellValue", f.SetCellValue(sheet, vsCell, "vs"))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, vsCell, vsCell, textStyle))

	rightStart := kachinukiColRightScore + strconv.Itoa(headerRow)
	rightEnd := kachinukiColRight + strconv.Itoa(headerRow)
	handleExcelError("MergeCell", f.MergeCell(sheet, rightStart, rightEnd))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, rightStart, rightTeam))
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, rightStart, rightEnd, redHeaderStyle))

	// --- Bout rows ---
	for i, bout := range match.Bouts {
		boutRow := firstBoutRow + i
		writeKachinukiBoutRow(f, sheet, bout, boutRow, textStyle)
	}

	// --- Summary row ---
	writeKachinukiSummaryRow(f, sheet, match, summaryRow, summaryStyle)

	return summaryRow + 1
}

// writeKachinukiBoutRow writes one bout's columns, Shiro left and Aka right
// through WhiteLeft like every other side-ordered pair in this workbook.
func writeKachinukiBoutRow(f *excelize.File, sheet string, bout KachinukiBout, row int, style int) {
	rowStr := strconv.Itoa(row)

	// Column A: bout number
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColBout+rowStr, strconv.Itoa(bout.Position)))

	// Columns B/F: fighter label + name + position, side order built first,
	// then placed left/right.
	akaPlayer := formatKachinukiPlayer(bout.SideALabel, bout.SideAName, bout.SideAPos)
	shiroPlayer := formatKachinukiPlayer(bout.SideBLabel, bout.SideBName, bout.SideBPos)
	leftPlayer, rightPlayer := WhiteLeft(akaPlayer, shiroPlayer)
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColLeft+rowStr, leftPlayer))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColRight+rowStr, rightPlayer))

	// Columns C/E: score + the bout's own result mark (Ht/Kiken/Fus.),
	// exactly as the main sheets compose a score cell.
	leftScore, rightScore := WhiteLeft(bout.ScoreA, bout.ScoreB)
	leftMark, rightMark := WhiteLeft(bout.MarkA, bout.MarkB)
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColLeftScore+rowStr, joinSp(leftScore, leftMark)))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColRightScore+rowStr, joinSp(rightScore, rightMark)))

	// Column D: the one closed-set middle mark, or the template's own "vs"
	// when the bout carries none.
	middle := bout.Middle
	if middle == "" {
		middle = "vs"
	}
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColVs+rowStr, middle))

	// Apply text style across the row for visual consistency.
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, kachinukiColBout+rowStr, kachinukiColRight+rowStr, style))
}

// writeKachinukiSummaryRow writes the per-team elimination tallies. The
// label "Summary" lives in column A; the elimination counts sit under Shiro's
// and Aka's own columns (left/right through WhiteLeft) so readers can see at
// a glance which team was exhausted. The match's own Pool or Elimination
// Matches row carries the winning team and the match decision; this sheet no
// longer repeats them (operator decision 2026-09-27).
func writeKachinukiSummaryRow(f *excelize.File, sheet string, match KachinukiMatchDetail, row int, style int) {
	rowStr := strconv.Itoa(row)

	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColBout+rowStr, "Summary"))

	leftElim, rightElim := WhiteLeft(match.EliminationA, match.EliminationB)
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColLeft+rowStr,
		fmt.Sprintf("%d eliminated", leftElim)))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColRight+rowStr,
		fmt.Sprintf("%d eliminated", rightElim)))

	// Style the whole row.
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, kachinukiColBout+rowStr, kachinukiColRight+rowStr, style))
}

// joinSp joins two display fragments with a single space, skipping empties,
// so a composed cell never carries a leading, trailing, or doubled space. A
// private twin of export.joinSp (this package may not import export: export
// imports engine, engine imports helper, so the reverse would cycle).
func joinSp(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

// formatKachinukiPlayer is a pure helper that combines a squad member's
// label (e.g. "T10.1", from domain.SquadMemberLabel; may be blank), a
// player's name, and their lineup position for display on the detail
// sheet: "T10.1 Name (Position)". label leads name (matching the JS
// display order the label's one owner, squad_member_label.jsx, composes
// everywhere else), separated by a space; either the label or the
// position may be blank independently. Empty name → empty string
// (defensive, the renderer should never receive an empty player name for
// a played bout).
func formatKachinukiPlayer(label, name, position string) string {
	if name == "" {
		return ""
	}
	display := name
	if label != "" {
		display = label + " " + name
	}
	if position == "" {
		return display
	}
	return fmt.Sprintf("%s (%s)", display, position)
}
