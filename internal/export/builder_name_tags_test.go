package export

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The results workbook renders through the same shared draw renderer as the
// stored-draw export, so each Names to Print sheet opens with the competition's
// name tags here too (the full per-format coverage is in internal/engine).
func TestBuildResultsWorkbook_NamesToPrintOpensWithCompetitionTags(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Name = "  Men's Individual  "
	require.NoError(t, store.SaveCompetition(comp))
	setCompFormat(t, store, compID, state.CompFormatMixed)
	require.NoError(t, store.SavePools(compID, makePools()))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	var sheets []string
	for _, name := range f.GetSheetList() {
		if strings.HasPrefix(name, helper.SheetNamesToPrint+" ") {
			sheets = append(sheets, name)
		}
	}
	require.NotEmpty(t, sheets, "the results workbook prints a Names to Print sheet")
	for _, sheet := range sheets {
		for row := 1; row <= helper.CompetitionTagRows; row++ {
			val, err := f.GetCellValue(sheet, fmt.Sprintf("A%d", row))
			require.NoError(t, err)
			assert.Equalf(t, "Men's Individual", val, "%s row %d is a competition-name tag", sheet, row)
		}
		next := helper.CompetitionTagRows + 1
		formula, err := f.GetCellFormula(sheet, fmt.Sprintf("B%d", next))
		require.NoError(t, err)
		assert.NotEmptyf(t, formula, "%s row %d holds the first competitor's name", sheet, next)
	}
}
