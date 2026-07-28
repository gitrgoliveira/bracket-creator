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
		assert.Containsf(t, title, "Shiaijo "+helper.CourtLabel(i), "%s must be titled by its shiaijo", page)
		assert.NotContainsf(t, title, "Test Competition - Test Competition", "%s must not duplicate the competition name", page)
	}
}

// TestExportCompetitionXlsx_LargeDrawRendersEveryTreePage covers the second,
// independent trigger: more than helper.MaxPlayersPerTree (16) finalists split
// the bracket across pages even on a single court.
func TestExportCompetitionXlsx_LargeDrawRendersEveryTreePage(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	// 10 pools of 4 x 2 qualifiers = 20 finalists > MaxPlayersPerTree.
	compID := startMixedComp(t, eng, store, "large-draw", []string{"A"}, 4, 40)

	f := openExportedWorkbook(t, eng, compID)

	pages := treeSheets(f)
	require.Greater(t, len(pages), 1, "a >16-finalist draw must span more than one bracket page")

	var all []string
	for _, page := range pages {
		labels := leafLabelsOnSheet(t, f, page)
		assert.NotEmptyf(t, labels, "%s must render its finalists, not be blank", page)
		all = append(all, labels...)
	}
	assert.Len(t, all, 20, "every finalist must be rendered exactly once across the pages")
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
