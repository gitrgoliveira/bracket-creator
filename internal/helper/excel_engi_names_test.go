package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// makeEngiPool2Players creates a minimal excelize file with a 2-player engi pool.
// The data sheet holds member-1 names in column B and member-2 names in column D:
//
//	B3 = "Alice"   D3 = "Alice B."
//	B4 = "Bob"     D4 = "Bob C."
//
// pCoords use the production $B$N absolute format so the D-column derivation
// (strings.Replace("$B$3","$B$","$D$",1) => "$D$3") works correctly.
func makeEngiPool2Players(t *testing.T) (*excelize.File, []Pool, map[string]cellCoord, map[string]playerCellCoord) {
	t.Helper()
	players := []Player{
		{Name: "Alice", DisplayName: "Alice B.", PoolPosition: 1},
		{Name: "Bob", DisplayName: "Bob C.", PoolPosition: 2},
	}
	pool := Pool{PoolName: "Pool A", Players: players}
	pool.Matches = []Match{{SideA: &pool.Players[0], SideB: &pool.Players[1]}}

	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: SheetData, cell: "$A$2"},
	}
	pCoords := map[string]playerCellCoord{
		playerCoordKey(players[0]): {cellCoord: cellCoord{sheetName: SheetData, cell: "$B$3"}},
		playerCoordKey(players[1]): {cellCoord: cellCoord{sheetName: SheetData, cell: "$B$4"}},
	}

	f := excelize.NewFile()
	t.Cleanup(func() { _ = f.Close() })
	_, _ = f.NewSheet(SheetPoolMatches)
	_, _ = f.NewSheet(SheetPoolDraw)
	_, _ = f.NewSheet(SheetData)
	_ = f.SetCellValue(SheetData, "B3", "Alice")
	_ = f.SetCellValue(SheetData, "D3", "Alice B.")
	_ = f.SetCellValue(SheetData, "B4", "Bob")
	_ = f.SetCellValue(SheetData, "D4", "Bob C.")

	return f, []Pool{pool}, poolCoords, pCoords
}

// TestEngiPlayerRef_NoNumber verifies that engiPlayerRef produces a
// dash-joined formula referencing both B and D columns when there is
// no competitor number cell.
func TestEngiPlayerRef_NoNumber(t *testing.T) {
	coord := playerCellCoord{
		cellCoord: cellCoord{sheetName: SheetData, cell: "$B$3"},
	}
	got := engiPlayerRef(coord)
	assert.Contains(t, got, `&" - "&`, "formula must contain dash join")
	assert.Contains(t, got, "$B$3", "formula must reference the B (member-1) cell")
	assert.Contains(t, got, "$D$3", "formula must reference the D (member-2) cell")
}

// TestEngiPlayerRef_WithNumber verifies that a competitor number is prepended
// to the joined formula and that the dash still joins the two names.
func TestEngiPlayerRef_WithNumber(t *testing.T) {
	coord := playerCellCoord{
		cellCoord:  cellCoord{sheetName: SheetData, cell: "$B$3"},
		numberCell: "$E$3",
	}
	got := engiPlayerRef(coord)
	assert.Contains(t, got, `&" - "&`, "formula must contain dash join")
	assert.Contains(t, got, "$B$3", "formula must reference the B (member-1) cell")
	assert.Contains(t, got, "$D$3", "formula must reference the D (member-2) cell")
	assert.Contains(t, got, "$E$3", "formula must reference the number cell")
}

// TestEngiNames_BuildNameFormula_Engi verifies that buildNameFormula with
// engi=true returns a dash-joined formula referencing B and D columns.
func TestEngiNames_BuildNameFormula_Engi(t *testing.T) {
	coord := playerCellCoord{
		cellCoord: cellCoord{sheetName: SheetData, cell: "$B$5"},
	}
	got := buildNameFormula("Player", false, true, coord)
	assert.Contains(t, got, `&" - "&`, "engi buildNameFormula must contain the dash join")
	assert.Contains(t, got, "$B$5", "must reference the B (member-1) cell")
	assert.Contains(t, got, "$D$5", "must reference the D (member-2) cell")
}

// TestEngiNames_BuildNameFormula_NonEngi_Unchanged verifies that non-engi
// buildNameFormula behaviour is not affected by the new engi parameter.
func TestEngiNames_BuildNameFormula_NonEngi_Unchanged(t *testing.T) {
	coord := playerCellCoord{
		cellCoord: cellCoord{sheetName: SheetData, cell: "$B$5"},
	}
	gotPlain := buildNameFormula("Player", false, false, coord)
	assert.NotContains(t, gotPlain, `&" - "&`, "non-engi must not produce the dash join")
	assert.Contains(t, gotPlain, "$B$5", "non-engi must reference the B cell")

	gotSanitized := buildNameFormula("Player", true, false, coord)
	assert.NotContains(t, gotSanitized, `&" - "&`, "non-engi sanitized must not produce the dash join")
	assert.Contains(t, gotSanitized, "D", "sanitized formula must reference the D column")
}

// TestEngiNames_PoolMatchFormula verifies that PrintPoolMatches with engi=true
// writes dash-joined pair formulas on the match name row.
//
// Layout for a 2-player pool on 1 court, startRow=2:
//   - Row 2: pool header (merged A2:G2)
//   - Row 3: match header (Red / White)
//   - Row 4: match name row (left side at A4, right side at G4)
func TestEngiNames_PoolMatchFormula(t *testing.T) {
	f, pools, poolCoords, pCoords := makeEngiPool2Players(t)

	PrintPoolMatches(f, pools, 0, 1, 1, false, poolCoords, pCoords, true /* engi */)

	leftFormula, err := f.GetCellFormula(SheetPoolMatches, "A4")
	require.NoError(t, err)
	assert.Contains(t, (leftFormula), `&" - "&`,
		"engi pool match left-side formula must contain the dash join")
	assert.Contains(t, leftFormula, "$B$3",
		"left side must reference Alice's B cell")
	assert.Contains(t, leftFormula, "$D$3",
		"left side must reference Alice's D cell")

	rightFormula, err := f.GetCellFormula(SheetPoolMatches, "G4")
	require.NoError(t, err)
	assert.Contains(t, (rightFormula), `&" - "&`,
		"engi pool match right-side formula must contain the dash join")
	assert.Contains(t, rightFormula, "$B$4",
		"right side must reference Bob's B cell")
	assert.Contains(t, rightFormula, "$D$4",
		"right side must reference Bob's D cell")
}

// TestNonEngi_NoChar10_PoolMatch is a regression guard: PrintPoolMatches with
// engi=false must NOT produce any a dash join in match name formulas.
func TestNonEngi_NoChar10_PoolMatch(t *testing.T) {
	f, pools, poolCoords, pCoords := makeEngiPool2Players(t)

	PrintPoolMatches(f, pools, 0, 1, 1, false, poolCoords, pCoords, false /* not engi */)

	leftFormula, err := f.GetCellFormula(SheetPoolMatches, "A4")
	require.NoError(t, err)
	assert.NotContains(t, (leftFormula), `&" - "&`,
		"non-engi pool match must not produce the dash join")
}

// TestEngiNames_IndividualResultsFormula verifies that the player name cells in
// the individual results table also carry dash joining when engi=true.
//
// For a 2-player pool starting at row 2:
//   - Row 6: Results table header
//   - Row 7: Alice name formula
//   - Row 8: Bob name formula
func TestEngiNames_IndividualResultsFormula(t *testing.T) {
	f, pools, poolCoords, pCoords := makeEngiPool2Players(t)

	PrintPoolMatches(f, pools, 0, 1, 1, false, poolCoords, pCoords, true /* engi */)

	for i, tc := range []struct {
		cell   string
		member string
		bCell  string
		dCell  string
	}{
		{"A7", "Alice", "$B$3", "$D$3"},
		{"A8", "Bob", "$B$4", "$D$4"},
	} {
		formula, err := f.GetCellFormula(SheetPoolMatches, tc.cell)
		require.NoError(t, err, "case %d cell %s", i, tc.cell)
		assert.Contains(t, (formula), `&" - "&`,
			"engi results table %s name cell must contain the dash join", tc.member)
		assert.Contains(t, formula, tc.bCell,
			"%s name cell must reference B cell", tc.member)
		assert.Contains(t, formula, tc.dCell,
			"%s name cell must reference D cell", tc.member)
	}
}

// TestEngiNames_AddPoolsToSheet verifies that AddPoolsToSheet with engi=true
// writes dash-joined formulas in the Pool Draw sheet player rows.
//
// Layout: startRow=5, startCol=B, pool header at B5, first player at B6, second at B7.
func TestEngiNames_AddPoolsToSheet(t *testing.T) {
	f, pools, poolCoords, pCoords := makeEngiPool2Players(t)

	require.NoError(t, AddPoolsToSheet(f, pools, poolCoords, pCoords, true /* engi */))

	firstPlayer, err := f.GetCellFormula(SheetPoolDraw, "B6")
	require.NoError(t, err)
	assert.Contains(t, (firstPlayer), `&" - "&`,
		"engi Pool Draw first player formula must contain the dash join")

	secondPlayer, err := f.GetCellFormula(SheetPoolDraw, "B7")
	require.NoError(t, err)
	assert.Contains(t, (secondPlayer), `&" - "&`,
		"engi Pool Draw second player formula must contain the dash join")
}

// TestNonEngi_AddPoolsToSheet_NoChar10 regression: AddPoolsToSheet with
// engi=false must not produce the dash join in player rows.
func TestNonEngi_AddPoolsToSheet_NoChar10(t *testing.T) {
	f, pools, poolCoords, pCoords := makeEngiPool2Players(t)

	require.NoError(t, AddPoolsToSheet(f, pools, poolCoords, pCoords, false /* not engi */))

	firstPlayer, err := f.GetCellFormula(SheetPoolDraw, "B6")
	require.NoError(t, err)
	assert.NotContains(t, (firstPlayer), `&" - "&`,
		"non-engi Pool Draw must not produce the dash join")
}

// TestEngiNames_AddPoolsToTree verifies that AddPoolsToTree with engi=true
// writes dash-joined formulas in the Tree sheet player rows.
//
// Layout: pool header at row 4 (TreeTitleRows+1), Alice at row 5, Bob at row 6.
func TestEngiNames_AddPoolsToTree(t *testing.T) {
	f, pools, poolCoords, pCoords := makeEngiPool2Players(t)

	sheetName := "Tree 1"
	_, _ = f.NewSheet(sheetName)

	AddPoolsToTree(f, sheetName, pools, poolCoords, pCoords, true /* engi */)

	aliceFormula, err := f.GetCellFormula(sheetName, "A5")
	require.NoError(t, err)
	assert.Contains(t, (aliceFormula), `&" - "&`,
		"engi Tree first player formula must contain the dash join")

	bobFormula, err := f.GetCellFormula(sheetName, "A6")
	require.NoError(t, err)
	assert.Contains(t, (bobFormula), `&" - "&`,
		"engi Tree second player formula must contain the dash join")
}

// TestNonEngi_AddPoolsToTree_NoChar10 regression: AddPoolsToTree with
// engi=false must not produce the dash join in player rows.
func TestNonEngi_AddPoolsToTree_NoChar10(t *testing.T) {
	f, pools, poolCoords, pCoords := makeEngiPool2Players(t)

	sheetName := "Tree 1"
	_, _ = f.NewSheet(sheetName)

	AddPoolsToTree(f, sheetName, pools, poolCoords, pCoords, false /* not engi */)

	aliceFormula, err := f.GetCellFormula(sheetName, "A5")
	require.NoError(t, err)
	assert.NotContains(t, (aliceFormula), `&" - "&`,
		"non-engi Tree must not produce the dash join")
}

// TestEngiNames_CreateNamesWithPoolToPrint verifies that the Names to Print
// sheet contains dash-joined formulas for engi pairs.
func TestEngiNames_CreateNamesWithPoolToPrint(t *testing.T) {
	f, pools, _, pCoords := makeEngiPool2Players(t)

	CreateNamesWithPoolToPrint(f, pools, false, 1, pCoords, true /* engi */)

	sheetName := "Names to Print A"
	// Row 1 = Alice, row 2 = Bob. Column B holds the name formula.
	aliceFormula, err := f.GetCellFormula(sheetName, "B1")
	require.NoError(t, err)
	assert.Contains(t, (aliceFormula), `&" - "&`,
		"engi Names to Print Alice formula must contain the dash join")

	bobFormula, err := f.GetCellFormula(sheetName, "B2")
	require.NoError(t, err)
	assert.Contains(t, (bobFormula), `&" - "&`,
		"engi Names to Print Bob formula must contain the dash join")
}

// TestNonEngi_CreateNamesWithPoolToPrint_NoChar10 regression: non-engi must
// not produce the dash join in name-badge formulas.
func TestNonEngi_CreateNamesWithPoolToPrint_NoChar10(t *testing.T) {
	f, pools, _, pCoords := makeEngiPool2Players(t)

	CreateNamesWithPoolToPrint(f, pools, false, 1, pCoords, false /* not engi */)

	sheetName := "Names to Print A"
	aliceFormula, err := f.GetCellFormula(sheetName, "B1")
	require.NoError(t, err)
	assert.NotContains(t, (aliceFormula), `&" - "&`,
		"non-engi Names to Print must not produce the dash join")
}

// TestEngiNames_CreateNamesToPrint verifies that CreateNamesToPrint with
// engi=true writes dash-joined formulas for name badges.
func TestEngiNames_CreateNamesToPrint(t *testing.T) {
	f, _, _, pCoords := makeEngiPool2Players(t)

	players := []Player{
		{Name: "Alice", DisplayName: "Alice B.", PoolPosition: 1},
		{Name: "Bob", DisplayName: "Bob C.", PoolPosition: 2},
	}

	CreateNamesToPrint(f, players, false, 1, pCoords, true /* engi */)

	sheetName := "Names to Print A"
	aliceFormula, err := f.GetCellFormula(sheetName, "B1")
	require.NoError(t, err)
	assert.Contains(t, (aliceFormula), `&" - "&`,
		"engi CreateNamesToPrint Alice formula must contain the dash join")
}

// TestNonEngi_CreateNamesToPrint_NoChar10 regression guard.
func TestNonEngi_CreateNamesToPrint_NoChar10(t *testing.T) {
	f, _, _, pCoords := makeEngiPool2Players(t)

	players := []Player{
		{Name: "Alice", DisplayName: "Alice B.", PoolPosition: 1},
	}

	CreateNamesToPrint(f, players, false, 1, pCoords, false /* not engi */)

	sheetName := "Names to Print A"
	aliceFormula, err := f.GetCellFormula(sheetName, "B1")
	require.NoError(t, err)
	assert.NotContains(t, (aliceFormula), `&" - "&`,
		"non-engi CreateNamesToPrint must not produce the dash join")
}
