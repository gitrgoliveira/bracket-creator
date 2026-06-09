package excel_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewFileFromScratchHasAllSheets verifies that the programmatically built
// workbook contains every sheet the rendering code relies on. The sheet name
// constants live in internal/helper/constants.go; if a sheet is renamed there
// without updating template.go the workbook will silently drop a tab and most
// rendering helpers will fail at runtime with a confusing "sheet does not
// exist" error. Keeping this test pinned to the constants makes that drift
// fail fast at unit-test time.
func TestNewFileFromScratchHasAllSheets(t *testing.T) {
	f, err := excel.NewFileFromScratch()
	require.NoError(t, err)
	require.NotNil(t, f)
	defer func() {
		require.NoError(t, f.Close())
	}()

	want := []string{
		helper.SheetData,
		helper.SheetTimeEstimator,
		helper.SheetPoolDraw,
		helper.SheetPoolMatches,
		helper.SheetEliminationMatches,
		helper.SheetNamesToPrint,
		helper.SheetTree,
	}

	sheets := f.GetSheetList()
	for _, s := range want {
		assert.Containsf(t, sheets, s, "missing sheet: %s", s)
	}

	// The "data" sheet should be the active sheet on open so the user lands on
	// the input form rather than an empty render sheet.
	activeIdx := f.GetActiveSheetIndex()
	dataIdx, err := f.GetSheetIndex(helper.SheetData)
	require.NoError(t, err)
	assert.Equal(t, dataIdx, activeIdx, "data sheet should be active on open")
}
