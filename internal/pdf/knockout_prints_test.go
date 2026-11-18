package pdf_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/pdf"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// exportedCompetition draws a six-competitor individual competition of the
// given format and writes it through ExportTournamentWorkbooks, the bridge
// the app's Export PDFs feeds the generator from.
func exportedCompetition(t *testing.T, format string) pdf.SourceWorkbook {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	const id = "printed"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: id, Name: "Printed", Kind: "individual", Format: format,
		PoolSize: 3, PoolSizeMode: "min", PoolWinners: 2, RoundRobin: true,
		Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
	}))
	players := make([]domain.Player, 6)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("Player %d", i+1), Dojo: fmt.Sprintf("Dojo %c", 'A'+i)}
	}
	require.NoError(t, store.SaveParticipants(id, players))
	eng := engine.New(store)
	require.NoError(t, eng.StartCompetition(id))

	sources, skipped, err := eng.ExportTournamentWorkbooks(t.TempDir(), id)
	require.NoError(t, err)
	require.Empty(t, skipped)
	require.Len(t, sources, 1)
	return sources[0]
}

func treeSheets(sheets []string) []string {
	var out []string
	for _, s := range sheets {
		if strings.HasPrefix(s, helper.SheetTree) {
			out = append(out, s)
		}
	}
	return out
}

// TestExportPDFGroups_KnockoutPrintsNoPoolSheets pins which sheets the
// bracket booklets select from each format's exported workbook: a
// knockout-only competition has no pool phase, so its workbook carries no
// Pool Draw or Pool Matches sheet and neither booklet prints an empty pool
// page, while its elimination and tree sheets still print. A mixed
// competition keeps both pool sheets. Runs without LibreOffice: it reads the
// workbook's sheet list and asks the groups' own resolution.
func TestExportPDFGroups_KnockoutPrintsNoPoolSheets(t *testing.T) {
	for _, tc := range []struct {
		format    string
		withPools bool
	}{
		{state.CompFormatKnockout, false},
		{state.CompFormatMixed, true},
	} {
		t.Run(tc.format, func(t *testing.T) {
			src := exportedCompetition(t, tc.format)
			f, err := excelize.OpenFile(src.Path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = f.Close() })
			present := f.GetSheetList()
			trees := treeSheets(present)
			require.NotEmptyf(t, trees, "the %s workbook must carry its tree sheets: %v", tc.format, present)

			var poolsTrees, fullBracket []string
			if tc.withPools {
				poolsTrees = []string{helper.SheetPoolDraw}
				fullBracket = []string{helper.SheetPoolDraw, helper.SheetPoolMatches}
			}
			poolsTrees = append(poolsTrees, trees...)
			fullBracket = append(append(fullBracket, helper.SheetEliminationMatches), trees...)

			for typ, want := range map[string][]string{"pools-trees": poolsTrees, "full-bracket": fullBracket} {
				g, ok := pdf.GroupByType(typ)
				require.Truef(t, ok, "group %q must exist", typ)
				assert.Equalf(t, want, g.ResolveSheets(present),
					"%s booklet of a %s competition (workbook sheets %v)", typ, tc.format, present)
			}
		})
	}
}

// TestExportPDFs_KnockoutFullBracketIsItsEliminationAndTreePages renders a
// knockout-only competition's full-bracket booklet through LibreOffice and
// pins its page count to the pages its Elimination Matches and Tree sheets
// print: an empty pool title page or pool page coming back adds pages, and a
// tree page dropped loses one. Skipped without LibreOffice (CI's test-pdf
// job installs it).
func TestExportPDFs_KnockoutFullBracketIsItsEliminationAndTreePages(t *testing.T) {
	conv := pdf.RequireSoffice(t)
	ctx := context.Background()
	src := exportedCompetition(t, state.CompFormatKnockout)

	full, err := conv.ConvertToPDF(ctx, src.Path, t.TempDir())
	require.NoError(t, err)
	ranges, err := pdf.SheetRanges(ctx, full)
	require.NoError(t, err)
	want, treePages := 0, 0
	for _, r := range ranges {
		pages := r.PageThru - r.PageFrom + 1
		switch {
		case r.Sheet == helper.SheetPoolDraw || r.Sheet == helper.SheetPoolMatches:
			t.Errorf("a knockout-only workbook printed %d page(s) of %s", pages, r.Sheet)
		case r.Sheet == helper.SheetEliminationMatches:
			want += pages
		case strings.HasPrefix(r.Sheet, helper.SheetTree):
			want += pages
			treePages += pages
		}
	}
	require.Positive(t, treePages, "the knockout workbook must print its tree: %v", ranges)

	gen, err := pdf.NewGenerator()
	require.NoError(t, err)
	out, err := gen.GenerateGroups(ctx, []string{"full-bracket"}, []pdf.SourceWorkbook{src}, t.TempDir())
	require.NoError(t, err)
	n, err := pdf.PageCount(ctx, out["full-bracket"])
	require.NoError(t, err)
	t.Logf("full-bracket booklet: %d pages", n)
	assert.Equalf(t, want, n, "full-bracket booklet pages (sheet ranges %v)", ranges)
}
