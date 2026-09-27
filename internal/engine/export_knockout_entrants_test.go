package engine

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestExportCompetitionXlsx_KnockoutEntrantsNameTheirCompetitors pins the
// stored-draw export's Elimination Matches entrants for a knockout-only
// competition: a competitor entering the bracket references its name on the
// data sheet, as the CLI knockout does, where it used to be an empty ”!
// reference (Err:501 in LibreOffice). An entrant an earlier match decides
// stays "M n".
func TestExportCompetitionXlsx_KnockoutEntrantsNameTheirCompetitors(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "ko-entrants"
	createTestCompetition(t, store, compID, state.CompFormatKnockout, 3)
	names := []string{"Ann", "Bea", "Cid", "Dan", "Eve", "Fay"}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	sheet := helper.SheetEliminationMatches
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	named := map[string]bool{}
	for r, row := range rows {
		if len(row) == 0 || !strings.HasPrefix(row[0], "Round ") {
			continue
		}
		// Header on row r+1 (1-based), White/Red on r+2, the entrants on r+3.
		for _, col := range []string{"A", "G"} {
			addr := fmt.Sprintf("%s%d", col, r+3)
			formula, err := f.GetCellFormula(sheet, addr)
			require.NoError(t, err)
			require.NotContains(t, formula, "''!", "%s must not be an empty reference", addr)
			if strings.Contains(formula, `"M `) {
				continue // decided by an earlier match
			}
			value, err := f.CalcCellValue(sheet, addr)
			require.NoError(t, err)
			assert.Truef(t, slices.Contains(names, value), "%s names a competitor, got %q from %s", addr, value, formula)
			named[value] = true
		}
	}
	assert.Len(t, named, len(names), "every competitor enters the bracket named")
}
