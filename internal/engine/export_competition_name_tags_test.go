package engine

// The competition-name tags: each Names to Print sheet of a competition's
// export opens with one page (helper.CompetitionTagRows rows) of tags printing
// the competition's name, then the competitors' tags follow. These tests pin
// that on the stored-draw export, for every format that prints the sheet, and
// that a shiaijo with no competitors, which has no sheet, has no tags.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

// namesToPrintSheets lists the workbook's per-shiaijo Names to Print sheets.
func namesToPrintSheets(f *excelize.File) []string {
	var out []string
	for _, name := range f.GetSheetList() {
		if strings.HasPrefix(name, helper.SheetNamesToPrint+" ") {
			out = append(out, name)
		}
	}
	return out
}

// requireOpensWithCompetitionTags asserts the sheet's first CompetitionTagRows
// rows each hold the competition name in one merged A:B cell, and that the
// competitor rows follow them.
func requireOpensWithCompetitionTags(t *testing.T, f *excelize.File, sheet, name string) {
	t.Helper()
	merged, err := f.GetMergeCells(sheet)
	require.NoError(t, err)
	mergedRanges := map[string]bool{}
	for _, m := range merged {
		mergedRanges[m.GetStartAxis()+":"+m.GetEndAxis()] = true
	}
	for row := 1; row <= helper.CompetitionTagRows; row++ {
		val, err := f.GetCellValue(sheet, fmt.Sprintf("A%d", row))
		require.NoError(t, err)
		assert.Equalf(t, name, val, "%s row %d is a competition-name tag", sheet, row)
		assert.Truef(t, mergedRanges[fmt.Sprintf("A%d:B%d", row, row)],
			"%s row %d merges its two columns into one tag", sheet, row)
	}
	// The row after the tags is the first competitor's: a name formula, not
	// the competition's name, and not part of a tag.
	next := helper.CompetitionTagRows + 1
	formula, err := f.GetCellFormula(sheet, fmt.Sprintf("B%d", next))
	require.NoError(t, err)
	assert.NotEmptyf(t, formula, "%s row %d holds the first competitor's name", sheet, next)
	assert.Falsef(t, mergedRanges[fmt.Sprintf("A%d:B%d", next, next)],
		"%s row %d is a competitor row, not a tag", sheet, next)
}

func TestExportCompetitionXlsx_EveryShiaijoSheetOpensWithCompetitionTags(t *testing.T) {
	const name = "Men's Individual"
	for _, tc := range []struct {
		format  string
		courts  []string
		players int
	}{
		{state.CompFormatMixed, []string{"A", "B"}, 16},
		{state.CompFormatKnockout, []string{"A", "B"}, 16},
	} {
		t.Run(fmt.Sprintf("%s over %d shiaijo", tc.format, len(tc.courts)), func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			const compID = "competition-name-tags"
			createTestCompetition(t, store, compID, tc.format, 4, func(c *state.Competition) {
				c.Name = name
				c.Courts = tc.courts
			})
			names := make([]string, tc.players)
			for i := range names {
				names[i] = fmt.Sprintf("Player%02d", i+1)
			}
			saveTestParticipants(t, store, compID, names)
			require.NoError(t, eng.StartCompetition(compID))

			f := openExportedWorkbook(t, eng, compID)

			sheets := namesToPrintSheets(f)
			require.Len(t, sheets, len(tc.courts), "each shiaijo with competitors has its own sheet")
			for _, sheet := range sheets {
				requireOpensWithCompetitionTags(t, f, sheet, name)
			}
		})
	}
}

// A sheet exists only for a shiaijo that has competitors listed, so a shiaijo
// that runs only knockout bouts has no sheet and therefore no tags.
func TestExportCompetitionXlsx_ShiaijoWithNobodyListedHasNoTags(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "competition-name-tags-two-pools-four-courts"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Name = "Women's Individual"
		c.Courts = []string{"A", "B", "C", "D"}
	})
	saveTestParticipants(t, store, compID, []string{"P01", "P02", "P03", "P04", "P05", "P06", "P07", "P08"})
	require.NoError(t, eng.StartCompetition(compID))

	f := openExportedWorkbook(t, eng, compID)

	sheets := namesToPrintSheets(f)
	require.Equal(t, []string{helper.SheetNamesToPrint + " A", helper.SheetNamesToPrint + " B"}, sheets,
		"two pools are fought on shiaijo A and B, so shiaijo C and D have nobody to list")
	for _, sheet := range sheets {
		requireOpensWithCompetitionTags(t, f, sheet, "Women's Individual")
	}
}

// The name prints as the organiser typed it, minus the space around it.
func TestExportCompetitionXlsx_CompetitionTagTrimsTheName(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "competition-name-tags-padded"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Name = "  Veterans  "
	})
	saveTestParticipants(t, store, compID, []string{"P01", "P02", "P03", "P04", "P05", "P06", "P07", "P08"})
	require.NoError(t, eng.StartCompetition(compID))

	f := openExportedWorkbook(t, eng, compID)

	sheets := namesToPrintSheets(f)
	require.Len(t, sheets, 1)
	requireOpensWithCompetitionTags(t, f, sheets[0], "Veterans")
}
