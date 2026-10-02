package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// TestKnockoutOptionsRun_NamesakesKeepTheirOwnDataRows: two competitors who
// share a name from different dojos each enter the Elimination Matches sheet
// through their own data-sheet row.
func TestKnockoutOptionsRun_NamesakesKeepTheirOwnDataRows(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.csv")
	require.NoError(t, os.WriteFile(input,
		[]byte("Taro Sato,Dojo A\nTaro Sato,Dojo B\nAnn,Dojo C\nBea,Dojo D\nCid,Dojo E\n"), 0o600))
	output := filepath.Join(dir, "out.xlsx")
	o := &knockoutOptions{filePath: input, outputPath: output, determined: true, courts: 1}
	require.NoError(t, o.run(nil, nil))

	f, err := excelize.OpenFile(output)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	sheet := helper.SheetEliminationMatches
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	rowRef := regexp.MustCompile(`\$B\$(\d+)`)
	dojos := map[string]bool{}
	for r, row := range rows {
		if len(row) == 0 || !strings.HasPrefix(row[0], "Round ") {
			continue
		}
		for _, col := range []string{"A", "G"} {
			formula, err := f.GetCellFormula(sheet, fmt.Sprintf("%s%d", col, r+3))
			require.NoError(t, err)
			m := rowRef.FindStringSubmatch(formula)
			if m == nil || strings.Contains(formula, `"M `) {
				continue
			}
			if name, _ := f.GetCellValue(helper.SheetData, "B"+m[1]); name != "Taro Sato" {
				continue
			}
			dojo, err := f.GetCellValue(helper.SheetData, "C"+m[1])
			require.NoError(t, err)
			dojos[dojo] = true
		}
	}
	assert.Equal(t, map[string]bool{"Dojo A": true, "Dojo B": true}, dojos)
}
