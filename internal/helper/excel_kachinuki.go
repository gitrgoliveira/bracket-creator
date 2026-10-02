package helper

// excel_kachinuki.go renders the "Kachinuki Detail" sheet, one section per
// kachinuki ("winner-stays-on") team match with full bout-by-bout detail.
//
// Each section is drawn like a team match block on the Elimination Matches
// sheet (operator decision 2026-10-02): the same seven columns and widths, the
// same styles, the same White | vs | Red row and team-name row, and the same
// numbered bout rows (printNumberedBoutRows), so the two sheets read alike. A
// competition with teamMatchType=kachinuki and a draw with matches in it gets
// the sheet in both of the app's workbook exports and in the blank template
// the /create generator draws; before the draw, a knockout-only one gets it
// for the skeleton its Elimination Matches sheet prints.
//
// Layout per match section (rows are 1-based relative to the section start),
// Shiro (SideB) LEFT and Aka (SideA) RIGHT throughout, per helper.WhiteLeft
// (operator ruling 2026-09-25, bc-xlcl/bc-kdsc):
//
//	Row 1: Title, "<label> (Kachinuki)", merged across the block
//	Row 2: White | vs | Red, each side in its colour (matchHeaderWithStyles)
//	Row 3: Shiro's team name on the left, Aka's on the right
//	Rows 4..N+3: one row per bout recorded, or, for a match with none yet,
//	             N = BlankBoutRows empty numbered rows for hand entry
//
// Where the match block prints only the bout number in the name cells, a
// recorded bout here prints its fighter after the number: this sheet exists
// to say who fought each bout. The score and its result mark (Ht/Kiken/Fus.,
// domain.SideMarksAB) go in the side's outer score cell, the cell the results
// overlay fills on the match sheets, and the centre carries the bout's one
// closed-set middle mark (domain.MiddleMark: X or (E); kachinuki has no
// daihyosen, so (DH) never appears here) or stays empty, as there. The
// match's own Pool or Elimination Matches row carries the result, so a
// section has no Winner or Decision column (operator decision 2026-09-27)
// and no tally: it ends at its last bout row (operator decision 2026-10-02).
//
// Sections are separated by a single blank row. The first section starts at
// row 1, and a section that would cross KachinukiDetailRowsPerPage starts a
// new page.
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
	// ScoreA is the accumulated ippon string (e.g. "MK", "MMK"), or empty;
	// a default-win winner's empty cell carries the maru (domain.DefaultWinMaruAB).
	ScoreA     string
	SideBName  string
	SideBLabel string // Side B's twin of SideALabel, same rules.
	SideBPos   string
	ScoreB     string
	// Middle is the ONE mark the bout's centre cell may carry
	// (domain.MiddleMark): "" (the cell stays empty), "X"
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

// KachinukiMatchDetail is a single team match's bout log and the teams that
// fought it. One section is rendered per entry in
// WriteKachinukiDetailSheet.
type KachinukiMatchDetail struct {
	Label     string // section title, e.g. "Pool Match 1" or "Round 2 - Match 3"
	SideATeam string // team name on Side A (Aka)
	SideBTeam string // team name on Side B (Shiro)
	Bouts     []KachinukiBout
	// BlankBoutRows is how many empty numbered bout rows the section prints
	// for hand entry when Bouts is empty (a match not fought yet); ignored
	// once a bout is recorded, since the section then lists exactly those.
	BlankBoutRows int
}

// sectionBouts returns the rows a match's section prints: the bouts
// recorded, or BlankBoutRows empty numbered rows when there are none.
func (m KachinukiMatchDetail) sectionBouts() []KachinukiBout {
	if len(m.Bouts) > 0 || m.BlankBoutRows <= 0 {
		return m.Bouts
	}
	blank := make([]KachinukiBout, m.BlankBoutRows)
	for i := range blank {
		blank[i].Position = i + 1
	}
	return blank
}

// PoolMatchLabel titles the Kachinuki Detail section of a competition's nth
// pool match, n counted from 1 across its pool matches in order.
func PoolMatchLabel(n int) string {
	return fmt.Sprintf("Pool Match %d", n)
}

// EachPoolMatch visits every pool match pool by pool, each pool's in its
// Pool Matches grid order, with its section title (PoolMatchLabel) and its
// index in pool.Matches: the one numbering both Kachinuki Detail builders use.
func EachPoolMatch(pools []Pool, visit func(label string, pool Pool, i int)) {
	n := 0
	for _, pool := range pools {
		for i := range pool.Matches {
			n++
			visit(PoolMatchLabel(n), pool, i)
		}
	}
}

// BlankKachinukiSections lists an empty Kachinuki Detail section of boutRows
// numbered rows for every match of a draw with nothing recorded yet: each pool
// match in order, then each knockout match round by round, then the 3rd-place
// match when includeBronze. A knockout section carries the title its block has
// on the Elimination Matches sheet and names its sides as that block does: a
// qualifier or competitor by its tree label, the winner of an earlier match as
// that match's MatchRefLabel, and each 3rd-place entrant by the semifinal it
// lost. rounds holds only matches with two entrants (a bye is no node), so no
// bye gets a section.
func BlankKachinukiSections(pools []Pool, rounds [][]*Node, includeBronze bool, boutRows int) []KachinukiMatchDetail {
	var out []KachinukiMatchDetail
	add := func(label, sideA, sideB string) {
		out = append(out, KachinukiMatchDetail{Label: label, SideATeam: sideA, SideBTeam: sideB, BlankBoutRows: boutRows})
	}
	EachPoolMatch(pools, func(label string, pool Pool, i int) {
		add(label, pool.Matches[i].SideA.Name, pool.Matches[i].SideB.Name)
	})
	entrant := func(node *Node) string {
		if node.LeafNode {
			return node.LeafVal
		}
		return MatchRefLabel(int(node.MatchNum()))
	}
	for roundIdx, round := range rounds {
		for _, match := range round {
			if match == nil {
				continue
			}
			// Left is side A (Aka), as printSingleEliminationMatch hands it to WhiteLeft.
			add(EliminationMatchTitle(roundIdx+1, int(match.MatchNum())), entrant(match.Left), entrant(match.Right))
		}
	}
	if includeBronze {
		loser := func(semi int) string {
			if semi == 0 {
				return ""
			}
			return MatchRefLabel(semi)
		}
		semiA, semiB := SemifinalMatchNumbers(rounds)
		add(ThirdPlaceLabel, loser(semiA), loser(semiB))
	}
	return out
}

// kachinukiFighterColWidth is the Kachinuki Detail sheet's name-column width,
// wider than a match block's matchNameColWidth so a numbered, labelled
// fighter fits: LibreOffice clipped the 26-character "T3.4 Yui Nakamura
// (Chuken)" at 24.
const kachinukiFighterColWidth = 38

// WriteKachinukiDetailSheet creates the SheetKachinukiDetail sheet and
// writes one section per match. When matches is empty the sheet is NOT
// created, the caller is responsible for checking the input length AND
// the renderer guards against accidental emission of an empty sheet.
func WriteKachinukiDetailSheet(f *excelize.File, matches []KachinukiMatchDetail) error {
	// Skip when no match has a row to print (no bouts and no blank rows):
	// the detail sheet is purely additive, never create an empty sheet
	// (T201 acceptance).
	hasRows := false
	for _, m := range matches {
		if len(m.sectionBouts()) > 0 {
			hasRows = true
			break
		}
	}
	if !hasRows {
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

	// The match blocks' own column widths, but for the name columns: a
	// recorded bout names its fighter after the bout number, and "17 T12.4
	// Yui Nakamura (Fukusho)" clips at a match block's width. The print is
	// scaled to one page wide whatever they are.
	setMatchColumnsWidthByStartCol(f, sheet, 1)
	handleExcelError("SetColWidth", f.SetColWidth(sheet, "A", "A", kachinukiFighterColWidth))
	handleExcelError("SetColWidth", f.SetColWidth(sheet, "G", "G", kachinukiFighterColWidth))
	styles := newMatchStyles(f)

	row := 1
	rowsOnPage := 0
	for i, match := range matches {
		bouts := match.sectionBouts()
		if len(bouts) == 0 {
			// Nothing to list: a match not fought yet with no rows to
			// print gets no section.
			continue
		}
		// A section is its title, colour and team rows and its bouts; one
		// that would cross the page budget starts a new page instead, so no
		// section is split for the hand filling it in.
		if height := len(bouts) + 3; rowsOnPage > 0 && rowsOnPage+height > KachinukiDetailRowsPerPage {
			handleExcelError("InsertPageBreak", f.InsertPageBreak(sheet, "A"+strconv.Itoa(row)))
			rowsOnPage = 0
		}
		nextRow := writeKachinukiMatchSection(f, sheet, styles, match, row)
		// Separator blank row between sections (except after the last).
		if i < len(matches)-1 {
			nextRow++
		}
		rowsOnPage += nextRow - row
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
// `startRow`, in the first court's columns of a match block, and returns the
// row index immediately after the section (the row a separator would occupy).
func writeKachinukiMatchSection(f *excelize.File, sheet string, styles matchStyles, match KachinukiMatchDetail, startRow int) int {
	cols := buildMatchColumnNames(1)
	bouts := match.sectionBouts()
	row := startRow

	// --- Title row, merged across the block like a match block's title ---
	titleStart, titleEnd := cols.startColName+strconv.Itoa(row), cols.endColName+strconv.Itoa(row)
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, titleStart, titleEnd, styles.poolHeader))
	handleExcelError("MergeCell", f.MergeCell(sheet, titleStart, titleEnd))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, titleStart, fmt.Sprintf("%s (Kachinuki)", match.Label)))

	// --- White | vs | Red, then each side's team under its colour ---
	row++
	matchHeaderWithStyles(f, sheet, cols.startColName, row, cols.middleColName, cols.endColName, styles.redHeader, styles.text, styles.whiteHeader, false)
	row++
	leftTeam, rightTeam := WhiteLeft(match.SideATeam, match.SideBTeam)
	handleExcelError("SetCellStyle", f.SetCellStyle(sheet, cols.startColName+strconv.Itoa(row), cols.endColName+strconv.Itoa(row), styles.text))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, cols.startColName+strconv.Itoa(row), leftTeam))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, cols.endColName+strconv.Itoa(row), rightTeam))

	// --- Bout rows: the match block's numbered rows, each recorded bout
	// then filling its own ---
	firstBoutRow := row + 1
	row = printNumberedBoutRows(f, sheet, cols, styles, row, len(bouts))
	for i, bout := range bouts {
		writeKachinukiBoutRow(f, sheet, cols, bout, firstBoutRow+i)
	}

	return row + 1
}

// writeKachinukiBoutRow fills one numbered bout row, Shiro left and Aka right
// through WhiteLeft like every other side-ordered pair in this workbook. A
// blank row (no fighter, score or mark) is left as printNumberedBoutRows drew
// it.
func writeKachinukiBoutRow(f *excelize.File, sheet string, cols matchColumnNames, bout KachinukiBout, row int) {
	rowStr := strconv.Itoa(row)

	// Name cells: the bout number, then the fighter's label, name and
	// position when the bout names one, side order built first, then
	// placed left/right.
	number := strconv.Itoa(bout.Position)
	akaPlayer := domain.JoinNonEmpty(number, formatKachinukiPlayer(bout.SideALabel, bout.SideAName, bout.SideAPos))
	shiroPlayer := domain.JoinNonEmpty(number, formatKachinukiPlayer(bout.SideBLabel, bout.SideBName, bout.SideBPos))
	leftPlayer, rightPlayer := WhiteLeft(akaPlayer, shiroPlayer)
	handleExcelError("SetCellValue", f.SetCellValue(sheet, cols.startColName+rowStr, leftPlayer))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, cols.endColName+rowStr, rightPlayer))

	// Outer score cells: the score joined with its side mark (Ht/Kiken/
	// Fus.), as the results overlay fills a team bout on the match sheets.
	leftScore, rightScore := WhiteLeft(domain.JoinNonEmpty(bout.ScoreA, bout.MarkA), domain.JoinNonEmpty(bout.ScoreB, bout.MarkB))
	if leftScore != "" {
		handleExcelError("SetCellValue", f.SetCellValue(sheet, cols.leftVictoriesColName+rowStr, leftScore))
	}
	if rightScore != "" {
		handleExcelError("SetCellValue", f.SetCellValue(sheet, cols.rightVictoriesColName+rowStr, rightScore))
	}

	// Centre: the one closed-set middle mark, when the bout carries one.
	if bout.Middle != "" {
		handleExcelError("SetCellValue", f.SetCellValue(sheet, cols.middleColName+rowStr, bout.Middle))
	}
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
