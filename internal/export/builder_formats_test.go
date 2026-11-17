package export

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestBuildResultsWorkbook_LeagueNoPhantomBracket is the regression test for the
// phantom-bracket bug: the draw returns placeholder "Pool A-1st" finalist
// labels even for a League (which has no knockout phase), so without the
// IsKnockoutEnabled() gate the export emitted an Elimination Matches sheet full of
// "Round N - Match N" headers and finalist placeholders, plus "Tree 1" pages,
// implying a knockout that does not exist.
func TestBuildResultsWorkbook_LeagueNoPhantomBracket(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatLeague
	comp.PoolWinners = 1
	comp.RoundRobin = true
	require.NoError(t, store.SaveCompetition(comp))

	pools := makePools() // Pool A, Pool B
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// The Elimination Matches sheet still EXISTS (NewFileFromScratch creates it) but
	// must be empty: no round headers and no "Pool A-1st" finalist placeholders.
	elim, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	for _, row := range elim {
		for _, cell := range row {
			assert.NotContains(t, cell, "Round ", "league export must not render a phantom bracket header")
			assert.NotContains(t, cell, "-1st", "league export must not render phantom finalist labels")
		}
	}
	// No Tree pages either (the whole knockout block is skipped for league),
	// and no bare "Tree" template: it is a styled scaffold, never output, and
	// the pools-trees / full-bracket PDF groups match tree sheets by prefix, so
	// a surviving template would print as a blank page in the booklet.
	assert.NotContains(t, f.GetSheetList(), "Tree 1",
		"league export must not create Tree bracket pages")
	assert.NotContains(t, f.GetSheetList(), helper.SheetTree,
		"the bare 'Tree' template must be deleted even when no knockout pages consume it")
}

// TestBuildResultsWorkbook_SwissUnsupported covers the Swiss format, which has no
// static bracket: the builder must fail with the documented sentinel rather than
// emit an empty workbook.
func TestBuildResultsWorkbook_SwissUnsupported(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = state.CompFormatSwiss
	comp.SwissRounds = 2
	require.NoError(t, store.SaveCompetition(comp))

	_, err = BuildResultsWorkbook(store, eng, compID)
	assert.ErrorIs(t, err, ErrSwissExportUnsupported)
}

// TestBuildResultsWorkbook_MixedEndToEnd covers the pools+knockout format through
// the real engine: two pools are generated and scored, then the export must render
// the pool grid with literal scores.
func TestBuildResultsWorkbook_MixedEndToEnd(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatMixed
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 1
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "M1", Dojo: "D"}, {Name: "M2", Dojo: "D"}, {Name: "M3", Dojo: "D"},
		{Name: "M4", Dojo: "D"}, {Name: "M5", Dojo: "D"}, {Name: "M6", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	for _, m := range matches {
		res := m
		res.IpponsA = []string{"M", "K"}
		res.IpponsB = nil
		res.Winner = m.SideA
		res.Decision = "fought"
		res.Status = state.MatchStatusCompleted
		require.NoError(t, eng.RecordMatchResult(compID, m.ID, &res))
	}

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "MK"),
		"mixed pool grid must show the literal ippon score 'MK'")
	assertNoBrokenFormulas(t, f, helper.SheetPoolMatches)
	// Mixed advances pool winners into a bracket: the draw renders finalist
	// slots as "Pool-Ordinal" (e.g. "Pool A-1st"), so the Elimination Matches
	// sheet is populated too and must be equally free of broken formulas.
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
}

// TestBuildResultsWorkbook_IncompleteAllFormats exports every format immediately
// after StartCompetition, with ZERO matches scored. A real tournament exports
// mid-run, so an incomplete competition must still yield a valid workbook (or, for
// Swiss, the documented 422) and never crash.
func TestBuildResultsWorkbook_IncompleteAllFormats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		format       string
		players      int
		poolSize     int
		poolWinners  int
		wantSwissErr bool
	}{
		{"league incomplete", state.CompFormatLeague, 4, 4, 1, false},
		{"mixed incomplete", state.CompFormatMixed, 6, 3, 1, false},
		{"knockout incomplete", state.CompFormatKnockout, 4, 0, 0, false},
		{"swiss incomplete", state.CompFormatSwiss, 4, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)

			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			comp.Kind = "individual"
			comp.Format = tc.format
			comp.Status = "setup"
			if tc.poolSize > 0 {
				comp.PoolSize = tc.poolSize
				comp.PoolSizeMode = "min"
			}
			if tc.poolWinners > 0 {
				comp.PoolWinners = tc.poolWinners
			}
			if tc.format == state.CompFormatLeague {
				comp.RoundRobin = true
			}
			if tc.format == state.CompFormatSwiss {
				comp.SwissRounds = 2
			}
			require.NoError(t, store.SaveCompetition(comp))

			// A participant named like an Excel cell reference (e.g. "P1" = column P
			// row 1) used to trip the shared elimination renderer's leaf-detection
			// (mp-uagg, fixed alongside this test suite): it structurally checks
			// Node.LeafNode now instead of parsing LeafVal as a cell name, so mixing
			// such names in here doubles as regression coverage across every format.
			names := []string{"Alice", "P1", "Carol", "M3", "Erin", "A4", "Grace", "Z9"}
			players := make([]domain.Player, tc.players)
			for i := range players {
				players[i] = domain.Player{Name: names[i], Dojo: "D"}
			}
			require.NoError(t, store.SaveParticipants(compID, players))
			require.NoError(t, eng.StartCompetition(compID))

			// Deliberately score nothing: export the incomplete competition.
			data, err := BuildResultsWorkbook(store, eng, compID)
			if tc.wantSwissErr {
				assert.ErrorIs(t, err, ErrSwissExportUnsupported)
				return
			}
			require.NoError(t, err, "incomplete %s export must not error", tc.format)
			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err, "incomplete %s export must be a valid xlsx", tc.format)
			defer f.Close()
			assert.Contains(t, f.GetSheetList(), helper.SheetPoolMatches)
			// No leftover formula may collapse to an Excel error in a 0-scored export.
			assertNoBrokenFormulas(t, f, helper.SheetPoolMatches)
			assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
		})
	}
}

// TestBuildResultsWorkbook_MultiPageTreePopulated is the regression test for the
// blank-extra-tree-sheet bug: a bracket with more than MaxPlayersPerTree (16)
// finalists spans multiple Tree pages. The previous implementation created
// "Tree 2"+ as empty sheets and only rendered "Tree 1"; the fix copies the
// styled template into every page and renders each subtree's leaves. A 32-entry
// knockout bracket needs 2 pages, so both must be populated and the bare "Tree"
// template must be gone.
func TestBuildResultsWorkbook_MultiPageTreePopulated(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatKnockout
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))

	players := make([]domain.Player, 32)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("Player%02d", i+1), Dojo: "D"}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	sheets := f.GetSheetList()
	assert.Contains(t, sheets, "Tree 1", "first tree page must exist")
	assert.Contains(t, sheets, "Tree 2", "second tree page must exist for a >16-entry bracket")
	assert.NotContains(t, sheets, helper.SheetTree,
		"the bare 'Tree' template must be consumed and deleted, not left alongside the pages")

	assert.Greater(t, formulaCellCount(t, f, "Tree 2"), 1,
		"second tree page must be populated (styled copy + rendered leaves), not blank")

	// Each page must also be print-bounded: without a print area the styled
	// title band and pre-sized template columns spill a near-blank second
	// physical page per bracket page into the printed booklet.
	for _, page := range []string{"Tree 1", "Tree 2"} {
		assert.GreaterOrEqualf(t, findPrintAreaLastRow(f, page), 0,
			"%s must define a parseable print area", page)
	}

	// The tree pages must carry the junction match numbers the operator calls
	// matches by. FillInMatches only writes a node's number when PrintLeafNodes
	// has stamped its sheet/cell coordinates, so with the old order (rounds
	// traversed and FillInMatches run BEFORE RenderTreePages) every junction
	// write was silently skipped and the pages showed a bare bracket. A
	// 32-entry draw is 31 matches; all but the cross-page final are on a page.
	numbered := 0
	for _, page := range []string{"Tree 1", "Tree 2"} {
		rows, rerr := f.GetRows(page)
		require.NoError(t, rerr)
		pageNumbered := 0
		for _, row := range rows {
			for _, cell := range row {
				if _, aerr := strconv.Atoi(cell); aerr == nil && cell != "" {
					pageNumbered++
				}
			}
		}
		assert.Positivef(t, pageNumbered, "%s must carry match numbers on its bracket junctions", page)
		numbered += pageNumbered
	}
	assert.Equal(t, 30, numbered,
		"all 31 matches except the cross-page final must be numbered on the tree pages")
}

// TestBuildResultsWorkbook_MixedTreePageHasPoolRosters verifies that for a
// Mixed format competition (pools + knockout), the exported workbook's "Tree 1"
// sheet carries pool roster formula entries in column A. AddPoolsToTree writes
// the first pool name formula at row TreeTitleRows+1 = 4, followed by one row
// per player. A non-empty formula in A4 confirms rosters were populated.
func TestBuildResultsWorkbook_MixedTreePageHasPoolRosters(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatMixed
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 1
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "P1", Dojo: "D"}, {Name: "P2", Dojo: "D"}, {Name: "P3", Dojo: "D"},
		{Name: "P4", Dojo: "D"}, {Name: "P5", Dojo: "D"}, {Name: "P6", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	sheets := f.GetSheetList()
	assert.Contains(t, sheets, "Tree 1", "Mixed export must produce a 'Tree 1' sheet")

	// Row 4 = TreeTitleRows+1 is where AddPoolsToTree writes the first pool name formula.
	formulaA4, err := f.GetCellFormula("Tree 1", "A4")
	require.NoError(t, err)
	assert.NotEmpty(t, formulaA4, "'Tree 1' A4 must contain a pool roster formula")

	// Title regression: the A1 title formula prepends data!$B$1 (already the
	// competition name), so the page title passed to SetTreeSheetTitle must be
	// the shiaijo label, not the competition name again, which rendered a
	// duplicated "Name - Name" heading.
	titleFormula, err := f.GetCellFormula("Tree 1", "A1")
	require.NoError(t, err)
	assert.Contains(t, titleFormula, "Shiaijo A", "'Tree 1' title must be the shiaijo label")
	assert.NotContains(t, titleFormula, comp.Name,
		"'Tree 1' title formula must not embed the competition name (data!$B$1 already prepends it)")
}

// TestBuildResultsWorkbook_KnockoutTreePageNoPoolRosters verifies that for a
// pure Knockout competition (no pool phase), the tree pages do NOT receive pool
// roster entries in column A (there are no pools to list).
func TestBuildResultsWorkbook_KnockoutTreePageNoPoolRosters(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatKnockout
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Q1", Dojo: "D"}, {Name: "Q2", Dojo: "D"},
		{Name: "Q3", Dojo: "D"}, {Name: "Q4", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// Find any tree page and assert column A row 4 has no pool roster formula.
	for _, sheet := range f.GetSheetList() {
		if len(sheet) < 4 || sheet[:4] != "Tree" {
			continue
		}
		formulaA4, err := f.GetCellFormula(sheet, "A4")
		require.NoError(t, err)
		assert.Empty(t, formulaA4,
			"knockout tree page %s must not have a pool roster formula in A4", sheet)
	}
}
