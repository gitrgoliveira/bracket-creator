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
//   - Both member names (Name and DisplayName of each pair) appear in the Data sheet
//     (EffectiveWithZekkenName() is active for engi, even when WithZekkenName=false).
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

	found := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "3rd Place" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	assert.True(t, found,
		"blank-template export for a naginata competition must have a '3rd Place' slot on the Elimination Matches sheet")
}

func TestExportCompetitionXlsx_Engi(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-export"

	createEngiCompetition(t, store, compID, state.CompFormatLeague, 4)

	// Four engi pairs: each participant is one pair; member1 = Name, member2 = DisplayName.
	pairs := []domain.Player{
		{Name: "Pair1 A", DisplayName: "Pair1 B", Dojo: "DojoA"},
		{Name: "Pair2 A", DisplayName: "Pair2 B", Dojo: "DojoB"},
		{Name: "Pair3 A", DisplayName: "Pair3 B", Dojo: "DojoC"},
		{Name: "Pair4 A", DisplayName: "Pair4 B", Dojo: "DojoD"},
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

	hasCellValue := func(rows [][]string, val string) bool {
		for _, row := range rows {
			for _, cell := range row {
				if cell == val {
					return true
				}
			}
		}
		return false
	}

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

	// --- Data sheet: both member names appear (EffectiveWithZekkenName = true) ---
	// The data sheet writes Name to column B and DisplayName to column D.
	// Both columns are populated when the competition is engi (or WithZekkenName=true).
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
			"Data sheet must contain member1 name %q", p.Name)
		assert.True(t, strings.Contains(allDataStr, p.DisplayName),
			"Data sheet must contain member2 name %q (DisplayName/zekken column)", p.DisplayName)
	}
}
