package engine

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
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

// TestExportCompetitionXlsx_KnockoutNamesakesKeepTheirOwnDataRows: two
// competitors sharing a name from different dojos are different people, so
// each first-round entrant cell must reference its own data-sheet row (the
// data sheet is editable, so a name corrected on one row must reach only
// that competitor's entrant cell). Pinned before and after the draw.
func TestExportCompetitionXlsx_KnockoutNamesakesKeepTheirOwnDataRows(t *testing.T) {
	for _, drawn := range []bool{false, true} {
		t.Run(fmt.Sprintf("drawn=%v", drawn), func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "ko-namesakes"
			createTestCompetition(t, store, compID, state.CompFormatKnockout, 3, func(c *state.Competition) { c.NumberPrefix = "K" })
			roster := [][2]string{{"Taro Sato", "Dojo A"}, {"Taro Sato", "Dojo B"}, {"Ann", "Dojo C"}, {"Bea", "Dojo D"}, {"Cid", "Dojo E"}}
			players := make([]domain.Player, len(roster))
			for i, r := range roster {
				players[i] = domain.Player{ID: bctest.StampPlayerID(r[0], r[1]), Name: r[0], Dojo: r[1]}
			}
			require.NoError(t, store.SaveParticipants(compID, players))
			if drawn {
				require.NoError(t, eng.StartCompetition(compID))
			}

			data, err := eng.ExportCompetitionXlsx(compID)
			require.NoError(t, err)
			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()

			sheet := helper.SheetEliminationMatches
			rows, err := f.GetRows(sheet)
			require.NoError(t, err)
			rowRef := regexp.MustCompile(`\$B\$(\d+)`)
			dojos := map[string]bool{}
			seenRows := map[string]bool{}
			for r, row := range rows {
				if len(row) == 0 || !strings.HasPrefix(row[0], "Round ") {
					continue
				}
				for _, col := range []string{"A", "G"} {
					addr := fmt.Sprintf("%s%d", col, r+3)
					formula, err := f.GetCellFormula(sheet, addr)
					require.NoError(t, err)
					m := rowRef.FindStringSubmatch(formula)
					if strings.Contains(formula, `"M `) || m == nil {
						continue
					}
					name, err := f.GetCellValue(helper.SheetData, "B"+m[1])
					require.NoError(t, err)
					if name != "Taro Sato" {
						continue
					}
					dojo, err := f.GetCellValue(helper.SheetData, "C"+m[1])
					require.NoError(t, err)
					seenRows[m[1]] = true
					dojos[dojo] = true
				}
			}
			assert.Len(t, seenRows, 2, "each namesake's entrant cell references its own data row")
			assert.Equal(t, map[string]bool{"Dojo A": true, "Dojo B": true}, dojos)
		})
	}
}
