package engine

// Regression coverage for the blank-template export's Tree sheets
// (Engine.ExportCompetitionXlsx, step 4). The previous implementation created
// one sheet per bracket page but only ever RENDERED page 1, so "Tree 2"+ were
// emitted completely blank and those entrants' half of the draw was silently
// missing from both the .xlsx download and the printed PDF booklet.
//
// Two independent triggers push the page count above 1 (helper.TreePageLayout):
// more than MaxPlayersPerTree (16) finalists, and -- far more commonly -- any
// competition run on 2 or more courts, since the layout is widened to
// NextPow2(numCourts).

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// treeLeafLabel matches the finalist placeholder a tree leaf renders, e.g.
// CONCATENATE("Pool A-1st ",'Pool Matches'!G19) -> "Pool A-1st".
var treeLeafLabel = regexp.MustCompile(`CONCATENATE\("([^"]+?) ",`)

// printAreaRef matches the tail of a print-area definition, e.g.
// 'Tree 1'!$A$1:$G$15 -> ("G", "15").
var printAreaRef = regexp.MustCompile(`\$A\$1:\$([A-Z]+)\$(\d+)$`)

// printAreaOf returns the sheet's _xlnm.Print_Area last column and last row, or
// ("", 0) when the sheet has no print area defined.
func printAreaOf(t *testing.T, f *excelize.File, sheet string) (string, int) {
	t.Helper()
	for _, dn := range f.GetDefinedName() {
		if dn.Name != "_xlnm.Print_Area" || dn.Scope != sheet {
			continue
		}
		m := printAreaRef.FindStringSubmatch(dn.RefersTo)
		require.NotNilf(t, m, "unparseable print area %q on %s", dn.RefersTo, sheet)
		lastRow, err := strconv.Atoi(m[2])
		require.NoError(t, err)
		return m[1], lastRow
	}
	return "", 0
}

// treeSheets returns the workbook's bracket pages in workbook order.
func treeSheets(f *excelize.File) []string {
	var out []string
	for _, s := range f.GetSheetList() {
		if strings.HasPrefix(s, helper.SheetTree) {
			out = append(out, s)
		}
	}
	return out
}

// leafLabelsOnSheet collects the finalist placeholders rendered on one tree page.
func leafLabelsOnSheet(t *testing.T, f *excelize.File, sheet string) []string {
	t.Helper()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	var labels []string
	for r := 1; r <= len(rows); r++ {
		for c := 1; c <= 26; c++ {
			col, cerr := excelize.ColumnNumberToName(c)
			require.NoError(t, cerr)
			formula, ferr := f.GetCellFormula(sheet, fmt.Sprintf("%s%d", col, r))
			require.NoError(t, ferr)
			if m := treeLeafLabel.FindStringSubmatch(formula); m != nil {
				labels = append(labels, m[1])
			}
		}
	}
	return labels
}

// countEliminationMatchBlocks counts the "Round N - Match N" block headers on
// the Elimination Matches sheet -- one per real knockout match. Takes the
// already-fetched rows so callers that reuse them (e.g. for per-court bucketing)
// need not read the sheet twice.
func countEliminationMatchBlocks(rows [][]string) int {
	headers := 0
	for _, row := range rows {
		for _, cell := range row {
			if strings.HasPrefix(cell, "Round ") && strings.Contains(cell, " - Match ") {
				headers++
			}
		}
	}
	return headers
}

// openExportedWorkbook exports compID as the blank template and opens it.
func openExportedWorkbook(t *testing.T, eng *Engine, compID string) *excelize.File {
	t.Helper()
	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// startMixedComp saves and starts a mixed (pools + knockout) competition with
// numPlayers entrants spread over the given courts, then returns its ID.
func startMixedComp(t *testing.T, eng *Engine, store *state.Store, compID string, courts []string, poolSize, numPlayers int) string {
	t.Helper()
	createTestCompetition(t, store, compID, "mixed", poolSize, func(c *state.Competition) {
		c.Courts = courts
	})
	names := make([]string, numPlayers)
	for i := range names {
		names[i] = fmt.Sprintf("Player%02d", i+1)
	}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))
	return compID
}

// TestExportCompetitionXlsx_TwoCourtsRendersEveryTreePage covers the common
// trigger: 2 courts alone force 2 bracket pages (NextPow2(numCourts)), which
// used to leave "Tree 2" blank and drop half the finalists from the workbook.
func TestExportCompetitionXlsx_TwoCourtsRendersEveryTreePage(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := startMixedComp(t, eng, store, "two-courts", []string{"A", "B"}, 4, 16)

	f := openExportedWorkbook(t, eng, compID)

	pages := treeSheets(f)
	require.Equal(t, []string{"Tree 1", "Tree 2"}, pages,
		"2 courts must produce two numbered bracket pages and no leftover bare template")

	// 4 pools x 2 qualifiers = 8 finalists, split across the two pages. Every
	// one of them must appear exactly once: the bug dropped the four on page 2.
	var all []string
	for _, page := range pages {
		labels := leafLabelsOnSheet(t, f, page)
		assert.NotEmptyf(t, labels, "%s must render its finalists, not be blank", page)
		all = append(all, labels...)
	}
	want := []string{
		"Pool A-1st", "Pool A-2nd", "Pool B-1st", "Pool B-2nd",
		"Pool C-1st", "Pool C-2nd", "Pool D-1st", "Pool D-2nd",
	}
	assert.ElementsMatch(t, want, all, "every pool qualifier must appear on exactly one bracket page")

	// Each page is titled by its shiaijo. The title formula already prepends
	// data!$B$1 (the competition name), so a page titled with comp.Name would
	// render "Test Competition - Test Competition".
	for i, page := range pages {
		title, err := f.GetCellFormula(page, "A1")
		require.NoError(t, err)
		assert.Containsf(t, title, helper.ShiaijoLabel(helper.CourtLabel(i)), "%s must be titled by its shiaijo", page)
		assert.NotContainsf(t, title, "Test Competition - Test Competition", "%s must not duplicate the competition name", page)
	}
}

// exportScenario parameterizes the geometry-sensitive tests over draw shapes.
// The three shapes cover: a balanced bracket (finalists a power of two), an
// UNBALANCED bracket (byes sit as leaves at shallower levels of the page's
// tree), and a large draw whose pages are themselves unbalanced. The rendering
// must hold for all of them, not just the tidy power-of-two case.
type exportScenario struct {
	name       string
	courts     []string
	poolSize   int
	numPlayers int
	finalists  int    // pools x 2 qualifiers
	pages      int    // helper.TreePageLayout for finalists x courts
	lastCol    string // helper.TreePageLastCol of each page's depth
}

var exportScenarios = []exportScenario{
	// 4 pools x 2 = 8 finalists on 2 courts: two balanced 4-leaf pages, depth 3.
	{"balanced two-court", []string{"A", "B"}, 4, 16, 8, 2, "G"},
	// 3 pools x 2 = 6 finalists on 2 courts: two UNBALANCED 3-leaf pages
	// (depth 3 with a bye each).
	{"unbalanced two-court", []string{"A", "B"}, 4, 12, 6, 2, "G"},
	// 10 pools x 2 = 20 finalists on one court: >MaxPlayersPerTree splits into
	// two UNBALANCED 10-leaf pages, depth 5.
	{"large unbalanced draw", []string{"A"}, 4, 40, 20, 2, "K"},
}

// TestExportCompetitionXlsx_TreePagesBoundedPrintArea pins the print geometry
// of every tree page across draw shapes. Without a print area a tree sheet
// prints its whole used range, and the styled-but-empty cells (the merged
// title band, the template's pre-sized columns out to Z) spilled a second,
// near-blank physical page per bracket page into every printed booklet.
func TestExportCompetitionXlsx_TreePagesBoundedPrintArea(t *testing.T) {
	for _, sc := range exportScenarios {
		t.Run(sc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := startMixedComp(t, eng, store, "print-area", sc.courts, sc.poolSize, sc.numPlayers)

			f := openExportedWorkbook(t, eng, compID)
			pages := treeSheets(f)
			require.Len(t, pages, sc.pages)

			for _, page := range pages {
				lastCol, lastRow := printAreaOf(t, f, page)
				require.NotEmptyf(t, lastCol, "%s must define a print area", page)

				// The area must cover everything rendered on the page...
				maxContentRow := 0
				rows, err := f.GetRows(page)
				require.NoError(t, err)
				for r, cells := range rows {
					for _, c := range cells {
						if c != "" {
							maxContentRow = r + 1
						}
					}
				}
				assert.GreaterOrEqualf(t, lastRow, maxContentRow,
					"%s print area (row %d) must cover the deepest content row (%d)", page, lastRow, maxContentRow)

				// ...and stop at the bracket's last column (the root's bracket
				// line), not at the template's pre-sized column Z.
				assert.Equalf(t, sc.lastCol, lastCol, "%s print area must end at the bracket's last column", page)
			}
		})
	}
}

// TestExportCompetitionXlsx_EliminationMatchesPopulated pins the score-entry
// side of the export across draw shapes. The tree pages only show the
// bracket's shape; the Elimination Matches sheet holds the blocks the operator
// writes scores into, and FillInMatches stamps the match numbers the operator
// calls matches by onto the tree junctions. This path used to skip both,
// shipping a workbook (and a "full-bracket" PDF) with an entirely blank
// Elimination Matches sheet and unnumbered tree pages.
func TestExportCompetitionXlsx_EliminationMatchesPopulated(t *testing.T) {
	for _, sc := range exportScenarios {
		t.Run(sc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := startMixedComp(t, eng, store, "elim-blocks", sc.courts, sc.poolSize, sc.numPlayers)

			f := openExportedWorkbook(t, eng, compID)

			// A knockout of F entrants is always F-1 matches, byes or not:
			// every internal node of the bracket tree is a real match
			// (TraverseRounds skips leaves), and each match eliminates exactly
			// one entrant.
			elim, err := f.GetRows(helper.SheetEliminationMatches)
			require.NoError(t, err)
			assert.Equal(t, sc.finalists-1, countEliminationMatchBlocks(elim),
				"a knockout of %d entrants must render %d match blocks", sc.finalists, sc.finalists-1)

			// Elimination block numbers per court band (8 columns per court).
			elimNumsByCourt := map[int]map[int]bool{}
			for _, row := range elim {
				for c, cell := range row {
					var round, match int
					if _, serr := fmt.Sscanf(cell, "Round %d - Match %d", &round, &match); serr == nil {
						court := c / helper.CourtsColumnsPerCourt
						if elimNumsByCourt[court] == nil {
							elimNumsByCourt[court] = map[int]bool{}
						}
						elimNumsByCourt[court][match] = true
					}
				}
			}

			// Junction numbering: every match that lives on a tree page gets its
			// number stamped. Only the cross-page matches (the top pages-1
			// junctions, drawn on no page) have nowhere to be written.
			//
			// Numbers are assigned round-major across the WHOLE draw
			// (AssignMatchNumbers), so on a multi-court draw a single page shows
			// an interleaved subset (court A: 1, 3; court B: 2, 4) - that is the
			// numbering contract with the web app (see
			// TestMatchNumberingParity_ExcelVsWeb), not a skip. What must hold is
			// the operator's invariant: every number on a court's tree page has
			// its score block in the SAME court's band on the Elimination sheet.
			numbered := 0
			pages := treeSheets(f)
			for i, page := range pages {
				rows, err := f.GetRows(page)
				require.NoError(t, err)
				court := helper.SubtreeCourtIndex(len(pages), len(sc.courts), i)
				pageNumbered := 0
				for _, row := range rows {
					for _, cell := range row {
						n, aerr := strconv.Atoi(cell)
						if aerr != nil || cell == "" {
							continue
						}
						pageNumbered++
						assert.Truef(t, elimNumsByCourt[court][n],
							"%s junction %d must have its score block in court band %d of the Elimination sheet", page, n, court)
					}
				}
				assert.Positivef(t, pageNumbered, "%s must carry match numbers on its junctions", page)
				numbered += pageNumbered
			}
			assert.Equal(t, sc.finalists-sc.pages, numbered,
				"all but the %d cross-page junctions must be numbered on the tree pages", sc.pages-1)
		})
	}
}

// TestExportCompetitionXlsx_AllFinalistsRenderedAcrossPages asserts, for every
// draw shape, that each finalist appears exactly once across the bracket pages
// and no page is blank. This covers the second, independent trigger into
// multiple pages (>helper.MaxPlayersPerTree finalists on a single court) as
// well as unbalanced draws, where a bug in bye placement could silently drop
// or duplicate a finalist.
func TestExportCompetitionXlsx_AllFinalistsRenderedAcrossPages(t *testing.T) {
	for _, sc := range exportScenarios {
		t.Run(sc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := startMixedComp(t, eng, store, "finalists", sc.courts, sc.poolSize, sc.numPlayers)

			f := openExportedWorkbook(t, eng, compID)

			pages := treeSheets(f)
			require.Len(t, pages, sc.pages)

			seen := map[string]int{}
			for _, page := range pages {
				labels := leafLabelsOnSheet(t, f, page)
				assert.NotEmptyf(t, labels, "%s must render its finalists, not be blank", page)
				for _, l := range labels {
					seen[l]++
				}
			}
			assert.Len(t, seen, sc.finalists, "every finalist must be rendered")
			for label, n := range seen {
				assert.Equalf(t, 1, n, "finalist %q must appear on exactly one page", label)
			}
		})
	}
}

// TestExportCompetitionXlsx_LeagueHasNoTreeSheet pins that a format with no
// knockout phase emits no bracket page at all -- not even the bare styled
// template, which would print as a blank page in the PDF booklet. Mirrors
// TestBuildResultsWorkbook_LeagueNoPhantomBracket on the results path.
func TestExportCompetitionXlsx_LeagueHasNoTreeSheet(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-no-tree"
	createTestCompetition(t, store, compID, "league", 4)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	f := openExportedWorkbook(t, eng, compID)
	assert.Empty(t, treeSheets(f), "a league has no knockout, so it must export no bracket page")
}

// TestExportCompetitionXlsx_PurePlayoffsRendersBracket pins mp-ndfu: a pure
// playoffs competition has NO pools, so the pool-fed draw returns nothing and
// the blank-template export used to skip the entire knockout block -- shipping a
// workbook (and PDF booklet) with no tree pages and an empty Elimination Matches
// sheet. The fix derives the elimination leaves from the stored bracket
// (PlayoffLeavesFromBracket), exactly as the results workbook does, so the two
// exports of the same draw agree.
func TestExportCompetitionXlsx_PurePlayoffsRendersBracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pure-playoffs-bracket"
	createTestCompetition(t, store, compID, "playoffs", 0, func(c *state.Competition) {
		c.Courts = []string{"A"}
	})
	names := make([]string, 8)
	for i := range names {
		names[i] = fmt.Sprintf("Player%02d", i+1)
	}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))

	f := openExportedWorkbook(t, eng, compID)

	// (1) The bracket page(s) must be rendered, not skipped, and carry content --
	// not left as a blank sheet. (leafLabelsOnSheet is not usable here: its regex
	// matches the pool CONCATENATE("Pool A-1st ", ...) placeholder form, but a
	// pure-playoffs leaf renders a participant name via a different formula.)
	pages := treeSheets(f)
	require.NotEmpty(t, pages, "a pure playoffs competition must render its bracket page(s)")
	nonEmpty := 0
	rows, err := f.GetRows(pages[0])
	require.NoError(t, err)
	for _, row := range rows {
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				nonEmpty++
			}
		}
	}
	assert.Positive(t, nonEmpty, "the bracket page must render content, not be blank")

	// (2) The Elimination Matches sheet must carry the operator's score blocks.
	// A knockout of 8 entrants is always 8-1 = 7 matches (every internal bracket
	// node is one match, each eliminating exactly one entrant).
	elim, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	assert.Equal(t, 7, countEliminationMatchBlocks(elim),
		"an 8-entrant playoffs knockout must render 7 elimination match blocks")
}

// TestExportTournamentWorkbooks_MultiPageTree covers the PDF pipeline's input:
// the workbooks written for pdf.Generator come from ExportCompetitionXlsx, so
// the printed "Pool Draw + Trees" booklet inherited the blank-page bug. The
// sheets are what the renderer paginates, so asserting them here proves the
// booklet gets every bracket page without needing LibreOffice.
func TestExportTournamentWorkbooks_MultiPageTree(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := startMixedComp(t, eng, store, "pdf-two-courts", []string{"A", "B"}, 4, 16)

	tmpDir := t.TempDir()
	sources, err := eng.ExportTournamentWorkbooks(tmpDir, compID)
	require.NoError(t, err)
	require.Len(t, sources, 1)

	data, err := os.ReadFile(sources[0].Path)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	pages := treeSheets(f)
	require.Equal(t, []string{"Tree 1", "Tree 2"}, pages)
	for _, page := range pages {
		assert.NotEmptyf(t, leafLabelsOnSheet(t, f, page), "%s must be populated in the PDF source workbook", page)
	}
}

// TestExportCompetitionXlsx_UnsetPoolWinnersRendersKnockout pins mp-0yd8: an
// unset (<=0) PoolWinners runs a 2-winner knockout everywhere else in the
// engine via EffectivePoolWinners, but the blank-template export fed the raw
// zero into the draw, rendering a workbook with no tree pages for the
// knockout the tournament is actually running.
func TestExportCompetitionXlsx_UnsetPoolWinnersRendersKnockout(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mixed-unset-pool-winners"
	createTestCompetition(t, store, compID, "mixed", 4, func(c *state.Competition) {
		c.Courts = []string{"A"}
		c.PoolWinners = 0
	})
	names := make([]string, 8)
	for i := range names {
		names[i] = fmt.Sprintf("Player%02d", i+1)
	}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))

	f := openExportedWorkbook(t, eng, compID)
	pages := treeSheets(f)
	require.NotEmpty(t, pages,
		"unset PoolWinners must still render the knockout tree (EffectivePoolWinners defaults to 2)")
	assert.NotEmpty(t, leafLabelsOnSheet(t, f, pages[0]),
		"tree page must carry the pool-winner finalist labels")
}
