package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

func TestSetTreeSheetTitle(t *testing.T) {
	tests := []struct {
		name            string
		title           string
		expectedFormula string
	}{
		{
			name:            "Shiaijo A",
			title:           "Shiaijo A",
			expectedFormula: `IF(data!$B$1="","Shiaijo A",data!$B$1&" - Shiaijo A")`,
		},
		{
			name:            "Shiaijo B",
			title:           "Shiaijo B",
			expectedFormula: `IF(data!$B$1="","Shiaijo B",data!$B$1&" - Shiaijo B")`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

			_, err := f.NewSheet("Tree 1")
			require.NoError(t, err)
			_, err = f.NewSheet(SheetData)
			require.NoError(t, err)

			SetTreeSheetTitle(f, "Tree 1", tt.title)

			formula, err := f.GetCellFormula("Tree 1", "A1")
			require.NoError(t, err)
			assert.Equal(t, tt.expectedFormula, formula)
		})
	}
}
