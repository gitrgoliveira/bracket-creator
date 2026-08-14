package engine

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Excel/engine draw parity, measured on the ARTIFACTS (bc-draw Phase 5).
//
// This replaces TestBracketIdentity_MixedComp, which claimed to prove the
// printed workbook and the live bracket describe one draw and did not: it
// compared the engine against excelMixedLeaves, a MODEL of the Excel path
// written inside the test file (BuildKnockoutDraw -> TreeToLeafArray, which is
// literally what the ENGINE calls). Nothing in it rendered a workbook, so the
// whole RenderKnockoutPages -> SubdivideRegions -> RenderTreePages ->
// PrintLeafNodes -> FillInMatches path was unmeasured, and the two real
// artifacts could (and in Phase 3 did) disagree while it stayed green.
//
// What runs here instead:
//
//	engine side  Engine.StartCompetition -> bracket.json (store.LoadBracket)
//	excel side   Engine.ExportCompetitionXlsx -> real .xlsx bytes, reopened with
//	             excelize and scanned cell by cell
//
// Both are production entry points and neither is reimplemented. The only thing
// they share is the competition on disk; the Excel side re-derives its draw
// through EliminationDraw exactly as an operator's export does, so a
// re-derivation that no longer matches the persisted bracket shows up here.
//
// The read-back is modelled on internal/helper/excel_pages_golden_test.go,
// which scans rendered "Tree N" sheets rather than recomputing them. It is
// DUPLICATED rather than shared, deliberately: that reader lives in
// `package helper` test files, which Go cannot import from `package engine`,
// and promoting it would mean a non-test package that no production code uses
// (the coverage gate then polices it, and it could not import helper for
// SheetTree/SheetData without an import cycle back into helper's own in-package
// tests). The duplication is also narrow - this reader measures cell GEOMETRY
// and junction numbers, which the golden reader does not need - and it cannot
// rot silently, because the first check in assertWorkbookMatchesBracket fails
// whenever the scan stops finding exactly what the bracket holds.

// treePageFirstRow is the sheet row PrintLeafNodes starts a page at
// (RenderTreePages passes TreeTitleRows+1).
const treePageFirstRow = helper.TreeTitleRows + 1

// renderedNode is one thing found on a rendered tree page, located by inverting
// PrintLeafNodes' placement.
//
// PrintLeafNodes walks the page subtree with (startCol, startRow, depth),
// halving the row span and stepping the column left by 2 per level, so the node
// at LEVEL l (0 = page root) and INDEX i within that level always lands at
//
//	col = 2*(depth-l) + 1
//	row = treePageFirstRow + i*2^(depth-l) + 2^(depth-l-1)
//
// for both kinds of content: a leaf writes its label there (writeTreeValue) and
// an internal node's junction connector sits there (CreateTreeBracket's middle
// cell, which is where FillInMatches later writes the match number). Inverting
// that pair of formulas turns a rendered sheet back into a tree, which is what
// makes "who plays whom, and who byes" readable off the workbook instead of
// recomputed from the code that drew it.
type renderedNode struct {
	level int
	index int
	cell  string
	// label is set for a leaf (the entrant printed there), number for a
	// junction (the printed "Match N"). Exactly one is set.
	label  string
	number int
}

// renderedPage is one "Tree N" sheet, read back.
type renderedPage struct {
	sheet string
	// court is the shiaijo letter parsed out of the page's real title formula
	// (SetTreeSheetTitle writes IF(data!$B$1="","Shiaijo X",…)), not recomputed.
	court string
	// depth is the page's bracket depth, taken from the print area the renderer
	// declared for it (SetPrintArea's last column is TreePageLastCol = 2*depth+1).
	depth int
	// leaves are in render order: top to bottom on the sheet, which is the
	// tree's left-to-right leaf order and the order an operator reads.
	leaves    []renderedNode
	junctions []renderedNode
}

// leafLabels returns the page's entrant labels in render order.
func (p renderedPage) leafLabels() []string {
	out := make([]string, len(p.leaves))
	for i, l := range p.leaves {
		out[i] = l.label
	}
	return out
}

// readRenderedTreePages reopens exported workbook bytes and reads every tree
// page back out of it. Nothing here is recomputed from the draw: sheet list,
// title, print area and cell contents all come from the file.
func readRenderedTreePages(t *testing.T, raw []byte) []renderedPage {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	printAreas := map[string]string{}
	for _, dn := range f.GetDefinedName() {
		if dn.Name == "_xlnm.Print_Area" {
			printAreas[dn.Scope] = dn.RefersTo
		}
	}

	var pages []renderedPage
	for _, sheet := range treeSheetsInPageOrder(f) {
		area, ok := printAreas[sheet]
		require.Truef(t, ok, "%s must declare a print area (SetTreePageLayout)", sheet)
		depth := treePageDepthFromPrintArea(t, area)

		page := renderedPage{sheet: sheet, court: treePageCourt(t, f, sheet), depth: depth}
		rows, err := f.GetRows(sheet)
		require.NoError(t, err)
		for r, row := range rows {
			for c, value := range row {
				col, rowNum := c+1, r+1
				// Every value a tree page carries sits in an ODD column at or
				// right of TreeLabelCol: leaves at 2d+1 (writeTreeValue) and
				// junction numbers at 2d+1 (CreateTreeBracket's middle cell).
				// Column A holds only the title and the roster overlay, and the
				// even columns only bracket line styling.
				if col < helper.TreeLabelCol || col%2 == 0 {
					continue
				}
				cell, err := excelize.CoordinatesToCellName(col, rowNum)
				require.NoError(t, err)

				node, ok := treeNodeAt(depth, rowNum, col)
				if !ok {
					continue
				}
				node.cell = cell

				if n, err := strconv.Atoi(value); err == nil {
					// A junction: FillInMatches wrote the printed match number.
					// A bare integer is never an entrant label - a pool-fed leaf
					// reads "Pool X-Nth" - so the two kinds cannot be confused.
					node.number = n
					page.junctions = append(page.junctions, node)
					continue
				}
				label := value
				if label == "" {
					// A leaf whose cell is a live cross-reference:
					// CONCATENATE("Pool A-1st ",'Pool Matches'!G19). The label
					// is the literal inside the formula.
					formula, ferr := f.GetCellFormula(sheet, cell)
					require.NoError(t, ferr)
					label = concatenatedLeafLabel(formula)
				}
				if label == "" {
					continue
				}
				node.label = label
				page.leaves = append(page.leaves, node)
			}
		}
		pages = append(pages, page)
	}
	return pages
}

// treeSheetsInPageOrder returns the rendered page sheets ordered by page number.
func treeSheetsInPageOrder(f *excelize.File) []string {
	type page struct {
		num  int
		name string
	}
	var found []page
	for _, name := range f.GetSheetList() {
		rest, ok := strings.CutPrefix(name, "Tree ")
		if !ok {
			continue
		}
		num, err := strconv.Atoi(rest)
		if err != nil {
			continue
		}
		found = append(found, page{num: num, name: name})
	}
	slices.SortFunc(found, func(a, b page) int { return a.num - b.num })
	names := make([]string, len(found))
	for i, p := range found {
		names[i] = p.name
	}
	return names
}

// treePageCourt parses the shiaijo letter out of the page's title formula.
func treePageCourt(t *testing.T, f *excelize.File, sheet string) string {
	t.Helper()
	formula, err := f.GetCellFormula(sheet, "A1")
	require.NoError(t, err)
	_, after, ok := strings.Cut(formula, `"Shiaijo `)
	require.Truef(t, ok, "%s title formula %q must name a shiaijo", sheet, formula)
	court, _, ok := strings.Cut(after, `"`)
	require.Truef(t, ok, "%s title formula %q must name a shiaijo", sheet, formula)
	return court
}

// treePageDepthFromPrintArea recovers the page's bracket depth from the print
// area the renderer declared: SetTreePageLayout bounds the page at
// TreePageLastCol(depth) = 2*depth+1.
func treePageDepthFromPrintArea(t *testing.T, refersTo string) int {
	t.Helper()
	_, end, ok := strings.Cut(refersTo, ":")
	require.Truef(t, ok, "print area %q must be a range", refersTo)
	parts := strings.Split(end, "$")
	require.Lenf(t, parts, 3, "print area end %q must be $COL$ROW", end)
	lastCol, err := excelize.ColumnNameToNumber(parts[1])
	require.NoError(t, err)
	require.Truef(t, lastCol%2 == 1, "print area last column %d must be TreePageLastCol (odd)", lastCol)
	return (lastCol - 1) / 2
}

// treeNodeAt inverts PrintLeafNodes' placement for one cell. ok is false when
// the cell is not on a node position at all (nothing the renderer writes ever
// lands off-grid, so a false here means the geometry moved and the caller's
// completeness check will say so).
func treeNodeAt(depth, row, col int) (renderedNode, bool) {
	d := (col - 1) / 2 // the depth argument PrintLeafNodes held at this column
	if d < 1 || d > depth {
		return renderedNode{}, false
	}
	span := 1 << d         // rows this node's subtree occupies
	offset := 1 << (d - 1) // rows from the subtree's top to its connector row
	rel := row - treePageFirstRow - offset
	if rel < 0 || rel%span != 0 {
		return renderedNode{}, false
	}
	return renderedNode{level: depth - d, index: rel / span}, true
}

// concatenatedLeafLabel extracts the entrant label from the live cross-reference
// formula writeTreeValue emits, or "" when the formula is not one.
func concatenatedLeafLabel(formula string) string {
	rest, ok := strings.CutPrefix(formula, `CONCATENATE("`)
	if !ok {
		return ""
	}
	label, _, ok := strings.Cut(rest, `",`)
	if !ok {
		return ""
	}
	// writeTreeValue joins the label and the reference with a single space.
	return strings.TrimSuffix(label, " ")
}

// isAncestor reports whether node (level, index) is an ancestor of (lower,
// lowerIndex) in the page's complete-binary-tree coordinate system.
func isAncestor(level, index, lower, lowerIndex int) bool {
	if lower <= level {
		return false
	}
	return lowerIndex>>(lower-level) == index
}

// printedBouts returns, for every junction printed anywhere in the workbook, the
// printed match number and the entrant labels that reach it. A page is a genuine
// subtree (R3/R8), so a junction's entrants are all on its own page.
//
// Matches ABOVE the page roots (the half-finals and the final on a multi-page
// draw) are on no tree page at all - they live on the Elimination Matches sheet -
// so they are absent here by construction, and the caller accounts for them.
func printedBouts(t *testing.T, pages []renderedPage) map[int][]string {
	t.Helper()
	bouts := map[int][]string{}
	for _, page := range pages {
		for _, j := range page.junctions {
			var leaves []string
			for _, l := range page.leaves {
				if isAncestor(j.level, j.index, l.level, l.index) {
					leaves = append(leaves, l.label)
				}
			}
			require.NotEmptyf(t, leaves, "%s!%s: printed match %d has no entrants under it",
				page.sheet, j.cell, j.number)
			_, dup := bouts[j.number]
			require.Falsef(t, dup, "%s!%s: match number %d printed twice", page.sheet, j.cell, j.number)
			slices.Sort(leaves)
			bouts[j.number] = leaves
		}
	}
	return bouts
}

// bracketMatchLeafSets returns each bracket match's entrant set (sorted), walking
// the real feeder graph: a match's entrants are its own resolved sides plus every
// entrant of its real feeders.
func bracketMatchLeafSets(bracket *state.Bracket) map[string][]string {
	byID := map[string]*state.BracketMatch{}
	for r := range bracket.Rounds {
		for i := range bracket.Rounds[r] {
			m := &bracket.Rounds[r][i]
			byID[m.ID] = m
		}
	}
	cache := map[string][]string{}
	var leavesOf func(m *state.BracketMatch) []string
	leavesOf = func(m *state.BracketMatch) []string {
		if cached, ok := cache[m.ID]; ok {
			return cached
		}
		var acc []string
		for _, side := range []string{m.SideA, m.SideB} {
			if side != "" && !strings.HasPrefix(side, "Winner of") {
				acc = append(acc, side)
			}
		}
		for _, fid := range m.Feeders {
			if fid == "" {
				continue
			}
			if f, ok := byID[fid]; ok {
				acc = append(acc, leavesOf(f)...)
			}
		}
		cache[m.ID] = acc
		return acc
	}

	out := map[string][]string{}
	for r := range bracket.Rounds {
		for i := range bracket.Rounds[r] {
			m := &bracket.Rounds[r][i]
			leaves := slices.Clone(leavesOf(m))
			slices.Sort(leaves)
			out[m.ID] = leaves
		}
	}
	return out
}

// engineEntrant is one competitor/placeholder in the persisted bracket.
type engineEntrant struct {
	label string
	// firstBoutDisplayRound is the effective round of the entrant's first REAL
	// bout. An entrant with no bye meets someone in the deepest round; every
	// round it byes moves this one closer to the final, which is exactly what a
	// bye is and what the printed page shows by indenting the name a column right.
	firstBoutDisplayRound int
}

// engineEntrants reads the persisted bracket's entrants in draw order (the
// first round's slots, left to right).
func engineEntrants(t *testing.T, bracket *state.Bracket) []engineEntrant {
	t.Helper()
	require.NotEmpty(t, bracket.Rounds)

	var out []engineEntrant
	byLabel := map[string]int{}
	for _, m := range bracket.Rounds[0] {
		for _, side := range []string{m.SideA, m.SideB} {
			if side == "" {
				continue
			}
			_, seen := byLabel[side]
			require.Falsef(t, seen, "entrant %q appears twice in the first round", side)
			byLabel[side] = len(out)
			out = append(out, engineEntrant{label: side})
		}
	}

	for ri, round := range bracket.Rounds {
		for _, m := range round {
			if m.Hidden {
				continue
			}
			for _, side := range []string{m.SideA, m.SideB} {
				idx, ok := byLabel[side]
				if !ok || out[idx].firstBoutDisplayRound != 0 {
					continue
				}
				require.NotZerof(t, m.DisplayRound,
					"real match %s (round %d) must carry a DisplayRound", m.ID, ri)
				out[idx].firstBoutDisplayRound = m.DisplayRound
			}
		}
	}
	for _, e := range out {
		require.NotZerof(t, e.firstBoutDisplayRound, "entrant %q never reaches a real bout", e.label)
	}
	return out
}

// assertWorkbookMatchesBracket is the whole parity check for one competition:
// the workbook Engine.ExportCompetitionXlsx produced against the bracket
// Engine.StartCompetition persisted.
func assertWorkbookMatchesBracket(t *testing.T, eng *Engine, store *state.Store, compID string) {
	t.Helper()

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)

	raw, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	pages := readRenderedTreePages(t, raw)
	require.NotEmpty(t, pages, "the workbook must carry at least one tree page")

	entrants := engineEntrants(t, bracket)

	// 1. The instrument itself: the scan must find exactly the draw's entrants.
	//    Without this a reader that silently stopped matching cells would make
	//    every comparison below trivially true.
	var printedLabels []string
	for _, page := range pages {
		printedLabels = append(printedLabels, page.leafLabels()...)
	}
	engineLabels := make([]string, len(entrants))
	for i, e := range entrants {
		engineLabels[i] = e.label
	}
	require.ElementsMatch(t, engineLabels, printedLabels,
		"the entrants scanned off the rendered pages must be exactly the bracket's entrants")

	// 2. ORDER, page by page: the pages, read top to bottom in page order,
	//    reproduce the bracket's first-round slot order.
	assert.Equal(t, engineLabels, printedLabels,
		"printed entrant order must equal the bracket's first-round order")

	// 3. Per-shiaijo grouping: the shiaijo a page is TITLED for, taken from its
	//    own title formula, against the shiaijo the bracket schedules the bouts
	//    on. Every bout drawn entirely from one shiaijo's pages must run on that
	//    shiaijo - which is the "a page really holds that shiaijo's region"
	//    claim, made on both sides at once and including the hidden bye bouts
	//    that no junction is printed for. A bout whose entrants span two shiaijo
	//    belongs to neither page (it is printed on the Elimination Matches sheet,
	//    and the bracket puts it on the leftmost region's court by design), so
	//    those are excluded rather than asserted about.
	leafSets := bracketMatchLeafSets(bracket)
	courtOfEntrant := map[string]string{}
	pagesPerCourt := map[string]int{}
	for _, page := range pages {
		pagesPerCourt[page.court]++
		for _, l := range page.leaves {
			courtOfEntrant[l.label] = page.court
		}
	}
	for _, round := range bracket.Rounds {
		for _, m := range round {
			var courts []string
			for _, label := range leafSets[m.ID] {
				if c := courtOfEntrant[label]; !slices.Contains(courts, c) {
					courts = append(courts, c)
				}
			}
			if len(courts) != 1 {
				continue
			}
			assert.Equalf(t, courts[0], m.Court,
				"bout %s draws only from the pages titled Shiaijo %s (%v) but the bracket schedules it on shiaijo %q",
				m.ID, courts[0], leafSets[m.ID], m.Court)
		}
	}
	// R8: the page count is a whole number of pages per shiaijo.
	var perCourt []int
	var treeCourts []string
	for c, n := range pagesPerCourt {
		perCourt = append(perCourt, n)
		treeCourts = append(treeCourts, c)
	}
	slices.Sort(perCourt)
	slices.Sort(treeCourts)
	assert.Equalf(t, perCourt[0], perCourt[len(perCourt)-1],
		"every shiaijo must get the same page count (R8), got %v", pagesPerCourt)

	// 4. Bouts: every junction printed on a page names the same match number and
	//    the same entrants as the bracket's match. This is where a bye that fell
	//    to the wrong occupant shows up - the bye's entrant set differs from the
	//    pairing that replaced it.
	engineBouts := map[int][]string{}
	for _, round := range bracket.Rounds {
		for _, m := range round {
			if m.Hidden {
				continue
			}
			require.NotZerof(t, m.MatchNumber, "real match %s must be numbered", m.ID)
			engineBouts[m.MatchNumber] = leafSets[m.ID]
		}
	}
	printed := printedBouts(t, pages)
	for num, leaves := range printed {
		engineLeaves, ok := engineBouts[num]
		require.Truef(t, ok, "printed Match %d (entrants %v) has no bracket match", num, leaves)
		assert.Equalf(t, engineLeaves, leaves,
			"Match %d: the bracket says %v, the workbook prints %v", num, engineLeaves, leaves)
	}

	// The bouts a tree page cannot show are exactly the ones above the page
	// roots (they belong to no single shiaijo); there are len(pages)-1 of them
	// and, because both numbering walks run deepest round first, they are the
	// last numbers.
	assert.Equalf(t, len(engineBouts)-(len(pages)-1), len(printed),
		"every bout except the %d above the page roots must be printed", len(pages)-1)
	for num := 1; num <= len(printed); num++ {
		assert.Containsf(t, printed, num, "Match %d is on no tree page", num)
	}

	// 5. Byes: an entrant that byes is drawn one bracket column further right
	//    per round skipped, so its printed column says which effective round it
	//    enters at. Converted to the bracket's DisplayRound scale it must equal
	//    the round of the entrant's first real bout. The offset between the two
	//    scales is the number of rounds above the page roots, so it is the SAME
	//    for every entrant on every page - deriving it rather than assuming it
	//    keeps the check on the artifacts, and its uniformity is itself R8.
	firstDR := map[string]int{}
	for _, e := range entrants {
		firstDR[e.label] = e.firstBoutDisplayRound
	}
	offset, offsetFrom := -1, ""
	for _, page := range pages {
		for _, l := range page.leaves {
			// The entrant's first bout is its parent junction, one level up,
			// whose round counted from the page's own final is exactly the
			// leaf's level. Level 0 (a page that is a lone entrant) means the
			// first bout is above the page entirely.
			entryRound := l.level
			got := firstDR[l.label] - entryRound
			if offset < 0 {
				offset, offsetFrom = got, fmt.Sprintf("%s!%s (%s)", page.sheet, l.cell, l.label)
				continue
			}
			assert.Equalf(t, offset, got,
				"%s!%s: %q is printed entering at page round %d (bracket DisplayRound %d), "+
					"which is %d round(s) off %s - its bye depth differs between the workbook and the bracket",
				page.sheet, l.cell, l.label, entryRound, firstDR[l.label], got-offset, offsetFrom)
		}
	}

	// 6. Pool banding, against the same workbook's tree pages and the app's own
	//    schedule.
	assertPoolBandsMatchSchedule(t, store, compID, raw, treeCourts)
}

// bandCourts reads a court-banded sheet's shiaijo bands off an already-open
// exported workbook (bctest.ReadCourtBands does the reading; the same reader
// serves internal/export's results-workbook tests) and returns their letters in
// column order, asserting on the way that every band sits on the court grid and
// that none is empty.
func bandCourts(t *testing.T, f *excelize.File, sheet string) []string {
	t.Helper()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	require.NotEmptyf(t, rows, "%s must carry a shiaijo header row", sheet)

	bands := bctest.ReadCourtBands(rows, helper.CourtsColumnsPerCourt)
	out := make([]string, 0, len(bands))
	for _, b := range bands {
		require.Zerof(t, b.Col%helper.CourtsColumnsPerCourt,
			"%s: header %q sits at column %d, off the %d-column court grid",
			sheet, bctest.ShiaijoHeaderPrefix+b.Court, b.Col+1, helper.CourtsColumnsPerCourt)
		assert.Truef(t, b.Occupied,
			"%s prints an empty %s%s band: the sheet sends an operator to a shiaijo nothing is scheduled on",
			sheet, bctest.ShiaijoHeaderPrefix, b.Court)
		out = append(out, b.Court)
	}
	return out
}

// scheduledPoolShiaijo returns the distinct shiaijo the STORED pool matches run
// on, sorted. This is what the live app scheduled (engine.generatePools writes
// it into pool-matches.csv off the CLAMPED court count), i.e. the answer the
// printed score sheets have to agree with.
func scheduledPoolShiaijo(t *testing.T, store *state.Store, compID string) []string {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var courts []string
	for _, m := range matches {
		if m.Court != "" && !slices.Contains(courts, m.Court) {
			courts = append(courts, m.Court)
		}
	}
	slices.Sort(courts)
	return courts
}

// assertPoolBandsMatchSchedule is parity check 6, split out because the pure
// playoffs path has no pool phase to band.
//
// The Pool Matches sheet is banded by shiaijo, and those bands must be the
// shiaijo the app actually SCHEDULED the pool phase on: the count clamped by
// helper.EffectiveDrawCourts, which is what engine.generatePools wrote into
// pool-matches.csv and what helper.BuildKnockoutDraw gave bracket regions to.
// Banding on the operator's RAW allocation instead printed pool bouts under
// shiaijo the app never scheduled them on, plus a trailing EMPTY band -- inside
// a workbook whose own tree pages named fewer shiaijo than its pool sheets did.
func assertPoolBandsMatchSchedule(t *testing.T, store *state.Store, compID string, raw []byte, treeCourts []string) {
	t.Helper()

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	if len(pools) == 0 {
		return // a standalone playoffs draw has no pool phase to band
	}

	f, err := excelize.OpenReader(bytes.NewReader(raw))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	printed := bandCourts(t, f, helper.SheetPoolMatches)

	// The pool bands must name the same shiaijo the app runs the pools on. Read
	// the schedule ONCE: testify evaluates the message arguments on every call,
	// so naming the loader twice would re-parse pool-matches.csv on every
	// passing case of the swept batteries and compare against a different read
	// from the one printed.
	scheduled := scheduledPoolShiaijo(t, store, compID)
	assert.Equalf(t, scheduled, printed,
		"%s is banded for shiaijo %v but the app schedules the pool phase on %v",
		helper.SheetPoolMatches, printed, scheduled)

	// ...and the same shiaijo the SAME workbook's tree pages are titled for. One
	// workbook cannot name one set of shiaijo on its pool sheets and another on
	// its bracket pages.
	assert.Equalf(t, treeCourts, printed,
		"%s is banded for shiaijo %v but this workbook's tree pages are titled for %v",
		helper.SheetPoolMatches, printed, treeCourts)
}

// mixedParityRoster builds a roster that CreatePools(roster, 4, max) splits into
// exactly numPools pools of MIXED size (4 and 3 players), which is what R6's
// oversized-pool bye criterion keys on: with uniform pools that criterion can
// never fire and a bye divergence hides. Same derivation as the helper golden
// files' drawGoldenRosterSize, restated here because it lives in a _test.go file
// of another package.
func mixedParityRoster(numPools int) []domain.Player {
	extra := max(numPools-3, 1)
	size := 3*numPools + extra
	players := make([]domain.Player, size)
	for i := range players {
		players[i] = domain.Player{
			// A unique dojo per player keeps CreatePools' conflict avoidance
			// out of the picture, so pool composition is deterministic.
			Name: fmt.Sprintf("P%03d", i+1),
			Dojo: fmt.Sprintf("Dojo %03d", i+1),
		}
	}
	return players
}

// playoffsParityRoster builds n unseeded competitors for a standalone playoffs
// draw. Unique dojos are irrelevant here (there are no pools to keep apart) but
// are kept so the roster is the same shape as the mixed one.
func playoffsParityRoster(n int) []domain.Player {
	players := make([]domain.Player, n)
	for i := range players {
		players[i] = domain.Player{
			Name: fmt.Sprintf("P%03d", i+1),
			Dojo: fmt.Sprintf("Dojo %03d", i+1),
		}
	}
	return players
}

// TestExcelWorkbookMatchesEngineBracket_Playoffs is the same parity check on the
// STANDALONE playoffs path, which derives its tree from the persisted bracket
// (EliminationDraw -> PlayoffLeavesFromBracket) rather than from pools.
//
// The ragged sizes are the point. A non-power-of-two roster's leaf array carries
// "" bye slots, and a tree builder that does not collapse an all-empty half
// draws and numbers a junction for every one of them: at 5 players that is 7
// printed junctions for a 4-bout bracket, with "Match 2" sitting between two
// empty slots and every printed number shifted off the bracket's own. 8 and 16
// are in as controls - they have no byes, so they pass either way and pin that
// the sweep is measuring the bye handling and not the whole path.
func TestExcelWorkbookMatchesEngineBracket_Playoffs(t *testing.T) {
	rosterSizes := []int{5, 6, 7, 8, 12, 16, 24}
	courtCounts := []int{1, 2, 4}

	for _, size := range rosterSizes {
		for _, numCourts := range courtCounts {
			name := fmt.Sprintf("%dplayers_%dshiaijo", size, numCourts)
			t.Run(name, func(t *testing.T) {
				eng, store, _ := setupTestEngine(t)
				compID := fmt.Sprintf("parity-playoffs-%d-%d", size, numCourts)

				courts := courtLabels(numCourts)

				require.NoError(t, store.SaveCompetition(&state.Competition{
					ID:        compID,
					Name:      "Parity",
					Format:    state.CompFormatPlayoffs,
					Kind:      "individual",
					Courts:    courts,
					StartTime: "09:00",
					Status:    state.CompStatusSetup,
				}))
				require.NoError(t, store.SaveParticipants(compID, playoffsParityRoster(size)))
				require.NoError(t, eng.StartCompetition(compID))

				assertWorkbookMatchesBracket(t, eng, store, compID)
			})
		}
	}
}

// TestExcelExportBandsClampedShiaijo is the worked example for the export
// banding bug, spelled out on ONE competition rather than swept.
//
// 12 competitors, PoolSize 4 in "max" mode, on 4 legal shiaijo (A-D) gives
// helper.PoolCount(12, 4, true) = 3 pools. Three pools cannot carry four
// shiaijo, so helper.EffectiveDrawCourts(3, 4) steps the draw down to 2 and the
// live app schedules pools A+B on shiaijo A and pool C on shiaijo B -- a bracket
// with TWO regions.
//
// The export banded its pool sheets on the UNCLAMPED 4 instead, so
// AssignPoolsToCourts(3, 4) = [0,1,2] printed Pool A under "Shiaijo A", Pool B
// under "Shiaijo B" and Pool C under "Shiaijo C", followed by a fourth,
// completely EMPTY "Shiaijo D" band -- sending operators to shiaijo the app
// never scheduled a bout on, in a workbook whose own tree pages are titled only
// Shiaijo A and Shiaijo B.
//
// This is the CLAMPED regime specifically: a pool count that is not a power of
// two under a larger shiaijo allocation. A power-of-two pool count, or a pool
// count at or above the court count, never trips the clamp and passes either
// way.
func TestExcelExportBandsClampedShiaijo(t *testing.T) {
	const (
		numParticipants = 12
		poolSize        = 4
		allocatedCourts = 4
		wantPools       = 3
	)

	// The regime this test exists for; if the clamp ever stopped firing here the
	// assertions below would pass vacuously.
	require.Equal(t, wantPools, helper.PoolCount(numParticipants, poolSize, true))
	wantCourts := helper.EffectiveDrawCourts(wantPools, allocatedCourts)
	require.Equalf(t, 2, wantCourts,
		"%d pools on %d shiaijo must clamp to 2 for this case to be in the clamped regime",
		wantPools, allocatedCourts)
	require.Less(t, wantCourts, allocatedCourts, "the case must actually clamp")

	eng, store, _ := setupTestEngine(t)
	compID := "clamped-bands"

	// The canonical individual competition, with the two fields this case turns
	// on: "max" pool sizing (which is what makes 12 competitors 3 pools) and the
	// four-shiaijo allocation the clamp then steps down. Court NAMES are the
	// shiaijo labels the sheets print (courtLabels -> helper.CourtLabel), so a
	// printed band header and a stored match court compare directly.
	createTestCompetition(t, store, compID, state.CompFormatMixed, poolSize, func(c *state.Competition) {
		c.PoolSizeMode = "max"
		c.Courts = courtLabels(allocatedCourts)
	})
	// Unique dojos per competitor keep CreatePools' conflict avoidance out of
	// the picture, so the pool composition is deterministic.
	require.NoError(t, store.SaveParticipants(compID, playoffsParityRoster(numParticipants)))
	require.NoError(t, eng.StartCompetition(compID))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, pools, wantPools)

	// What the app scheduled: pools A and B on shiaijo A, pool C on shiaijo B.
	poolMatches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	courtOfPool := map[string]string{}
	for _, m := range poolMatches {
		pool, _, ok := strings.Cut(m.ID, "-")
		require.Truef(t, ok, "pool match ID %q must be <Pool>-<n>", m.ID)
		courtOfPool[pool] = m.Court
	}
	assert.Equal(t, map[string]string{"Pool A": "A", "Pool B": "A", "Pool C": "B"}, courtOfPool,
		"the live app must run three pools over two shiaijo")

	raw, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(raw))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// The printed pool bands must be exactly those two shiaijo, with no empty
	// trailing band.
	assert.Equal(t, []string{"A", "B"}, bandCourts(t, f, helper.SheetPoolMatches),
		"%s must be banded for the shiaijo the pools actually run on", helper.SheetPoolMatches)

	// The knockout sheet is banded by REGION, which is the same two shiaijo.
	assert.Equal(t, []string{"A", "B"}, bandCourts(t, f, helper.SheetEliminationMatches),
		"%s must be banded for the draw's regions", helper.SheetEliminationMatches)

	// Names to Print splits into one sheet per shiaijo, so the clamp decides how
	// many sheets exist and which competitors are on each. A "Shiaijo C" sheet
	// here would be pool C's roster filed under a shiaijo it never fights on.
	var nameSheets []string
	for _, sheet := range f.GetSheetList() {
		if strings.HasPrefix(sheet, helper.SheetNamesToPrint+" ") {
			nameSheets = append(nameSheets, sheet)
		}
	}
	slices.Sort(nameSheets)
	assert.Equal(t, []string{helper.SheetNamesToPrint + " A", helper.SheetNamesToPrint + " B"}, nameSheets,
		"one %s sheet per shiaijo the competition actually uses", helper.SheetNamesToPrint)

	// And the whole draw-parity battery, which now includes the band check.
	assertWorkbookMatchesBracket(t, eng, store, compID)
}

// TestExcelWorkbookMatchesEngineBracket_Mixed sweeps pool-fed knockout draws and
// asserts the exported workbook and the persisted bracket describe the same
// draw: same entrants in the same order page by page, the same shiaijo per page,
// the same bouts under the same match numbers, and the same byes.
func TestExcelWorkbookMatchesEngineBracket_Mixed(t *testing.T) {
	// Unbalanced pool counts are in deliberately: 3, 6 and 7 pools are where a
	// court holds fewer pools than its partner, which is where region sizes go
	// odd and byes appear, and they were exactly what the modelled test covered
	// worst.
	poolCounts := []int{2, 3, 4, 6, 7, 8}
	poolWinners := []int{1, 2, 3}
	courtCounts := []int{1, 2, 4}

	for _, numPools := range poolCounts {
		for _, winners := range poolWinners {
			for _, numCourts := range courtCounts {
				name := fmt.Sprintf("%dpools_%dqual_%dshiaijo", numPools, winners, numCourts)
				t.Run(name, func(t *testing.T) {
					eng, store, _ := setupTestEngine(t)
					compID := fmt.Sprintf("parity-%d-%d-%d", numPools, winners, numCourts)

					// Court names ARE the shiaijo labels the tree pages print
					// (courtLabels -> helper.CourtLabel), so page title and match
					// court compare directly.
					courts := courtLabels(numCourts)

					require.NoError(t, store.SaveCompetition(&state.Competition{
						ID:           compID,
						Name:         "Parity",
						Format:       state.CompFormatMixed,
						Kind:         "individual",
						PoolSize:     4,
						PoolSizeMode: "max",
						PoolWinners:  winners,
						RoundRobin:   true,
						Courts:       courts,
						StartTime:    "09:00",
						Status:       state.CompStatusSetup,
					}))
					require.NoError(t, store.SaveParticipants(compID, mixedParityRoster(numPools)))
					require.NoError(t, eng.StartCompetition(compID))

					pools, err := store.LoadPools(compID)
					require.NoError(t, err)
					require.Lenf(t, pools, numPools, "the sweep case must actually produce %d pools", numPools)

					assertWorkbookMatchesBracket(t, eng, store, compID)
				})
			}
		}
	}
}

// TestEliminationSheetBandsMatchBracketCourts pins the one thing the band
// assertions above never checked: not WHICH shiaijo bands the Elimination
// Matches sheet prints, but WHICH BOUT lands in each of them.
//
// That sheet is a per-shiaijo handout -- one "Shiaijo X" header, one column band
// and one vertical page break per court -- so an operator running shiaijo C is
// handed the C band and calls the bouts on it. If the sheet files a bout under a
// different shiaijo from the one the app scheduled it on, the printed sheet and
// the operator console call the same competitors to two different courts.
//
// The regression this pins: the sheet used to chunk each round's bouts uniformly
// (match index / (matches/courts)), which agrees with the draw only when every
// region is the same size. The court-first draw deliberately allows unequal
// regions, so 4 pools sending one qualifier each over 4 shiaijo -- four
// single-qualifier regions -- put round 1's two bouts on shiaijo A and C while
// the sheet printed them under A and B, leaving C and D as empty bands.
func TestEliminationSheetBandsMatchBracketCourts(t *testing.T) {
	for _, tc := range []struct {
		name         string
		participants int
		poolSize     int
		poolWinners  int
		courts       int
	}{
		{"four single-qualifier regions over four shiaijo", 16, 4, 1, 4},
		{"two qualifiers per pool over four shiaijo", 16, 4, 2, 4},
		{"ragged pools over two shiaijo", 12, 4, 2, 2},
		{"one shiaijo carries the whole draw", 16, 4, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "elim-band-" + strings.ReplaceAll(tc.name, " ", "-")

			createTestCompetition(t, store, compID, state.CompFormatMixed, tc.poolSize, func(c *state.Competition) {
				c.PoolSizeMode = "max"
				c.PoolWinners = tc.poolWinners
				c.Courts = courtLabels(tc.courts)
			})
			require.NoError(t, store.SaveParticipants(compID, playoffsParityRoster(tc.participants)))
			require.NoError(t, eng.StartCompetition(compID))

			bracket, err := store.LoadBracket(compID)
			require.NoError(t, err)
			require.NotNil(t, bracket)

			raw, err := eng.ExportCompetitionXlsx(compID)
			require.NoError(t, err)
			f, err := excelize.OpenReader(bytes.NewReader(raw))
			require.NoError(t, err)
			defer func() { _ = f.Close() }()

			rows, err := f.GetRows(helper.SheetEliminationMatches)
			require.NoError(t, err)
			bands := bctest.ReadCourtBands(rows, helper.CourtsColumnsPerCourt)
			require.NotEmpty(t, bands, "the elimination sheet must carry shiaijo bands")

			// Which shiaijo band each printed "Round R - Match N" header sits in.
			bandOfLabel := map[string]string{}
			for _, b := range bands {
				for _, row := range rows[1:] {
					for c := b.Col; c < b.Col+helper.CourtsColumnsPerCourt && c < len(row); c++ {
						if label := strings.TrimSpace(row[c]); strings.HasPrefix(label, "Round ") {
							bandOfLabel[label] = b.Court
						}
					}
				}
			}
			require.NotEmpty(t, bandOfLabel, "no bouts were printed, so this case pins nothing")

			checked := 0
			for rIdx, round := range bracket.Rounds {
				for _, m := range round {
					if m.Court == "" {
						continue
					}
					// An unnumbered entry is a BYE, not a bout: one side empty
					// and already completed. The sheet correctly prints no block
					// for it. Assert that rather than skipping on the number
					// alone, so a genuinely missing bout cannot hide here.
					if m.MatchNumber == 0 {
						assert.Truef(t, m.SideA == "" || m.SideB == "",
							"match %q vs %q on shiaijo %s carries no match number but has two sides, so it is a real bout the sheet never printed",
							m.SideA, m.SideB, m.Court)
						continue
					}
					label := fmt.Sprintf("Round %d - Match %d", rIdx+1, m.MatchNumber)
					band, ok := bandOfLabel[label]
					if !assert.Truef(t, ok, "%q is scheduled on shiaijo %s but is not printed on the elimination sheet at all", label, m.Court) {
						continue
					}
					assert.Equalf(t, m.Court, band,
						"%q (%s vs %s): the app calls it to shiaijo %s, the printed sheet files it under shiaijo %s",
						label, m.SideA, m.SideB, m.Court, band)
					checked++
				}
			}
			require.NotZero(t, checked, "no bracket match was compared, so this case pins nothing")
		})
	}
}
