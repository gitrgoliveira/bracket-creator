package engine

// TestExportCompetitionXlsx_Engi characterizes the blank-template export path
// (Engine.ExportCompetitionXlsx -> excel.NewFileFromScratch) for an engi (kata)
// competition. Prior to this test there was zero characterization coverage for
// engi on this path. The assertions pin the already-shipped behavior:
//
//   - The binary is a valid ZIP (XLSX).
//   - The Pool Matches sheet carries the "Flags" standings header (not "PW"/"PL").
//     "Flags" is stored in the shared strings table and resolved by GetRows, so
//     the check reads through excelize's normal cell-value API.
//   - The combined pair name ("Name 1 - Name 2") appears in the Data sheet
//     name column (engi stores both members in Player.Name; the CSV layout is
//     unchanged, so WithZekkenName=false engi comps use the plain layout).
//
// Note on formulas: the blank-template path loads pools from CSV (which does not
// persist match pairings). As a result the match grid has no match rows and the
// W/L/Flags standings formulas collapse to literal "0". The ISNUMBER+N( formula
// pattern is therefore not present in this export path; it is instead exercised by
// TestBuildResultsWorkbook_* tests in internal/export, which use the full
// helper.Pool.Matches slice.

import (
	"bytes"
	"strings"
	"testing"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportCompetitionXlsx_NaginataThirdPlaceSlot verifies that the blank-
// template export for a naginata playoffs competition includes a "3rd Place"
// slot on the Elimination Matches sheet so the operator can hand-score it.
func TestExportCompetitionXlsx_NaginataThirdPlaceSlot(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "naginata-export"

	comp := &state.Competition{
		ID:           compID,
		Name:         "Naginata Export Test",
		Kind:         "individual",
		Format:       state.CompFormatPlayoffs,
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
		Naginata:     true,
	}
	require.NoError(t, store.SaveCompetition(comp))

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Charlie", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	// Verify the bracket has a ThirdPlaceMatch before testing the export.
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch,
		"naginata 4-player bracket must have ThirdPlaceMatch before testing export")

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	assert.True(t, hasCellValue(rows, helper.ThirdPlaceLabel),
		"blank-template export for a naginata competition must have a '3rd Place' slot on the Elimination Matches sheet")
}

// hasCellValue reports whether any cell in rows equals val.
func hasCellValue(rows [][]string, val string) bool {
	return bctest.FindCellRow(rows, val) >= 0
}

// TestExportCompetitionXlsx_NaginataThirdPlacePrintAreaAndLayout verifies that
// the blank-template export path (Engine.ExportCompetitionXlsx) sets the
// _xlnm.Print_Area defined name for the Elimination Matches sheet to cover the
// "3rd Place" block AND applies a sheet page layout for that sheet.
// Before Fix C, the blank-template path called PrintThirdPlaceBlock without
// then calling SetEliminationPrintArea or SetSheetLayoutPortraitA4DownThenOver,
// so the sheet had no print area and no page layout.
func TestExportCompetitionXlsx_NaginataThirdPlacePrintAreaAndLayout(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "naginata-print-area"

	comp := &state.Competition{
		ID:           compID,
		Name:         "Naginata Print Area Test",
		Kind:         "individual",
		Format:       state.CompFormatPlayoffs,
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
		Naginata:     true,
	}
	require.NoError(t, store.SaveCompetition(comp))

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Charlie", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// Find the "3rd Place" row (1-based Excel row).
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	thirdPlaceExcelRow := bctest.FindCellRow(rows, helper.ThirdPlaceLabel) + 1
	require.GreaterOrEqual(t, thirdPlaceExcelRow, 1,
		"blank-template naginata export must have a '3rd Place' row")

	// Check the Print_Area defined name covers the bronze block.
	printAreaLastRow := -1
	for _, dn := range f.GetDefinedName() {
		if dn.Name == "_xlnm.Print_Area" && dn.Scope == helper.SheetEliminationMatches {
			printAreaLastRow = bctest.ParsePrintAreaLastRow(dn.RefersTo)
			break
		}
	}
	assert.GreaterOrEqual(t, printAreaLastRow, thirdPlaceExcelRow,
		"_xlnm.Print_Area last row (%d) must cover the '3rd Place' row (%d) on the blank-template export path",
		printAreaLastRow, thirdPlaceExcelRow)
}

func TestExportCompetitionXlsx_Engi(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-export"

	createEngiCompetition(t, store, compID, state.CompFormatLeague, 4)

	// Four engi pairs: each participant is one pair with both member names
	// combined in the Name field ("Name 1 - Name 2").
	pairs := []domain.Player{
		{Name: "Pair1 A - Pair1 B", Dojo: "DojoA"},
		{Name: "Pair2 A - Pair2 B", Dojo: "DojoB"},
		{Name: "Pair3 A - Pair3 B", Dojo: "DojoC"},
		{Name: "Pair4 A - Pair4 B", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveParticipants(compID, pairs))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Valid XLSX (ZIP) magic bytes.
	require.GreaterOrEqual(t, len(data), 4, "engi export must produce at least 4 bytes")
	assert.Equal(t, []byte{0x50, 0x4b, 0x03, 0x04}, data[:4],
		"engi export must produce a valid XLSX ZIP")

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	// --- Pool Matches sheet: engi standings headers ---
	// GetRows resolves shared string table references, so literal header values
	// ("Flags", "W", "Rank") are returned even though the sheet XML stores them
	// as shared string indices. "PW"/"PL" must not appear for engi.
	pmRows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	assert.True(t, hasCellValue(pmRows, helper.ColHeaderFlags),
		"Pool Matches standings must carry %q header for an engi competition", helper.ColHeaderFlags)
	assert.False(t, hasCellValue(pmRows, "PW"),
		"Pool Matches standings must NOT carry 'PW' header for an engi competition")
	assert.False(t, hasCellValue(pmRows, "PL"),
		"Pool Matches standings must NOT carry 'PL' header for an engi competition")

	// --- Data sheet: the combined pair name appears in the Name column ---
	dataRows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)

	var allData strings.Builder
	for _, row := range dataRows {
		for _, cell := range row {
			allData.WriteString(cell)
			allData.WriteByte('|')
		}
	}
	allDataStr := allData.String()

	for _, p := range pairs {
		assert.True(t, strings.Contains(allDataStr, p.Name),
			"Data sheet must contain the combined pair name %q", p.Name)
	}
}
