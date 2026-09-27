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

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
	// ScoreA is the accumulated ippon string (e.g. "MK", "MMK"), or empty.
	// A default-win winner's EMPTY recorded cell instead carries the FIK
	// maru fallback (domain.DefaultWinMaruAB), exactly as the main sheets'
	// writeTeamSubMatchScores fills it (bc-cse F2).
	ScoreA     string
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
// alignment with the main match sheets.
const (
	kachinukiColBout       = "A"
	kachinukiColLeft       = "B" // left: name + position
	kachinukiColLeftScore  = "C" // left: score + result mark
	kachinukiColVs         = "D" // centre: the one closed-set middle mark, or "vs"
	kachinukiColRightScore = "E" // right: score + result mark
	kachinukiColRight      = "F" // right: name + position
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
	//
	// The fighter columns (B/F) are widened past the Winner/Decision
	// columns' old share, now that both are gone and the sheet fits one
	// printed page wide regardless of column width (SetSheetLayoutPortraitA4
	// scales the print, never the on-screen column). 24 clipped a labelled
	// name at the 12pt centred font this sheet uses: a LibreOffice render of
	// "T3.4 Yui Nakamura (Chuken)" (26 characters) lost its first character
	// and spilled past the table. 36 comfortably fits the longest realistic
	// label, "T12.4 Yui Nakamura (Fukusho)" (28 characters), with margin to
	// spare (bc-cse F10).
	colWidths := []struct {
		from, to string
		w        float64
	}{
		{kachinukiColBout, kachinukiColBout, 12},
		{kachinukiColLeft, kachinukiColLeft, 36},
		{kachinukiColLeftScore, kachinukiColLeftScore, 10},
		{kachinukiColVs, kachinukiColVs, 5},
		{kachinukiColRightScore, kachinukiColRightScore, 10},
		{kachinukiColRight, kachinukiColRight, 36},
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
	leftStyle, rightStyle := WhiteLeft(getRedHeaderStyle(f), getWhiteHeaderStyle(f))

	titleRow := startRow
	subtitleRow := startRow + 1
	headerRow := startRow + 2
	firstBoutRow := startRow + 3
	summaryRow := firstBoutRow + len(match.Bouts)

	leftTeam, rightTeam := WhiteLeft(match.SideATeam, match.SideBTeam)

	// put writes value/style into the row's [from, to] cell range, merging
	// first when the range spans more than one column.
	put := func(from, to string, row int, value any, style int) {
		fromCell := from + strconv.Itoa(row)
		toCell := to + strconv.Itoa(row)
		if from != to {
			handleExcelError("MergeCell", f.MergeCell(sheet, fromCell, toCell))
		}
		handleExcelError("SetCellValue", f.SetCellValue(sheet, fromCell, value))
		handleExcelError("SetCellStyle", f.SetCellStyle(sheet, fromCell, toCell, style))
	}

	// --- Title row (merged across A..F) ---
	put(kachinukiColBout, kachinukiColRight, titleRow, fmt.Sprintf("%s (Kachinuki)", match.Label), titleStyle)

	// --- Subtitle row (merged), Shiro's team first (P1: matches the
	// scoreboard's White-left reading) ---
	put(kachinukiColBout, kachinukiColRight, subtitleRow, fmt.Sprintf("%s vs %s", leftTeam, rightTeam), textStyle)

	// --- Header row (P1: colour + team name, no side words) ---
	put(kachinukiColBout, kachinukiColBout, headerRow, "Bout #", titleStyle)
	put(kachinukiColLeft, kachinukiColLeftScore, headerRow, leftTeam, leftStyle)
	put(kachinukiColVs, kachinukiColVs, headerRow, "vs", textStyle)
	put(kachinukiColRightScore, kachinukiColRight, headerRow, rightTeam, rightStyle)

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

	// Columns C/E: the cell joins the score and its side mark (Ht/Kiken/
	// Fus.), side order built first, then placed left/right.
	leftScore, rightScore := WhiteLeft(domain.JoinNonEmpty(bout.ScoreA, bout.MarkA), domain.JoinNonEmpty(bout.ScoreB, bout.MarkB))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColLeftScore+rowStr, leftScore))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, kachinukiColRightScore+rowStr, rightScore))

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
// a glance which team was exhausted.
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

// formatKachinukiPlayer is a pure helper that combines a squad member's
// label (e.g. "T10.1", from domain.SquadMemberLabel; may be blank), a
// player's name, and their lineup position for display on the detail
// sheet: "T10.1 Name (Position)". label leads name (matching the JS
// display order the label's one owner, squad_member_label.jsx, composes
// everywhere else), separated by a space; either the label or the
// position may be blank independently.
//
// A fighter picked by squad number and never named (bc-dnst) has a real
// label and an empty name: the label alone still identifies them, so it
// carries the cell on its own rather than blanking it (bc-kdsc fold-in f).
// Only when BOTH label and name are empty is there genuinely nothing to
// show, and the cell stays blank.
func formatKachinukiPlayer(label, name, position string) string {
	display := domain.JoinNonEmpty(label, name)
	if display == "" {
		return ""
	}
	if position == "" {
		return display
	}
	return fmt.Sprintf("%s (%s)", display, position)
}
