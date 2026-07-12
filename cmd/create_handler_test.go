package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// postCreate drives createTournamentHandler exactly as cmd/mobile_app.go wires
// it (a single POST /create route on a bare gin engine) and returns the parsed
// workbook from a successful response.
func postCreate(t *testing.T, form url.Values) *excelize.File {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/create", createTournamentHandler)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	require.Positive(t, w.Body.Len())

	f, err := excelize.OpenReader(bytes.NewReader(w.Body.Bytes()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// leagueForm is the single-court round-robin league payload the web-mobile
// export button posts (one pool of everyone).
func leagueForm(playerList string) url.Values {
	return url.Values{
		"tournamentType": {"pools"},
		"playerList":     {playerList},
		"courts":         {"1"},
		"winnersPerPool": {"2"},
		"playersPerPool": {"6"},
		"poolSizeMode":   {"min"},
		"teamMatches":    {"0"},
		"roundRobin":     {"on"},
		"determined":     {"on"},
	}
}

// TestCreateHandler_LeagueWLAreCountingFormulas is the regression guard for
// mp-x0u9. The mobile-app server mounts createTournamentHandler at POST /create
// so the in-app export runs the same one-pass generator as the `web` app,
// instead of the engine's stored-pool export. The engine path reloads pools
// from disk, which severs the player↔match pointer link PrintPoolMatches needs,
// so the per-player W/L/T cells degraded to "=0" and ranking never computed.
// This asserts the "W" column in the pool Results block holds a live counting
// formula, NOT "=0".
func TestCreateHandler_LeagueWLAreCountingFormulas(t *testing.T) {
	f := postCreate(t, leagueForm("Alice, DA\nBob, DB\nCharlie, DC\nDave, DD\nEve, DE\nFrank, DF"))

	rows, err := f.GetRows("Pool Matches")
	require.NoError(t, err)

	resultsRow := -1
	for i, row := range rows {
		if len(row) > 0 && row[0] == "Results" {
			resultsRow = i + 1 // 1-based
			break
		}
	}
	require.Positive(t, resultsRow, "Results block not found on Pool Matches sheet")

	sawCountingFormula := false
	for r := resultsRow + 1; r <= resultsRow+6; r++ {
		cell, _ := excelize.CoordinatesToCellName(2, r) // column B == W
		formula, ferr := f.GetCellFormula("Pool Matches", cell)
		require.NoError(t, ferr)
		require.NotEqual(t, "0", strings.TrimPrefix(formula, "="),
			"W cell %s collapsed to =0, pool scoring formulas are dead (mp-x0u9 regression)", cell)
		if strings.Contains(formula, "COUNTA") {
			sawCountingFormula = true
		}
	}
	require.True(t, sawCountingFormula,
		"expected at least one W cell to be a COUNTA-based counting formula")
}

// TestCreateHandler_CommaInPlayerName_NotCorrupted guards the roster-CSV
// escaping fix (tri-review findings 1/3). A participant whose name contains a
// comma must survive the round trip: the web-mobile export RFC-4180-quotes such
// fields (e.g. `"Doe, John"`), which routes the line through encoding/csv in
// helper.CreatePlayers instead of a naive strings.Split. Without quoting the
// name would split into name="Doe", dojo="John", silent corruption.
func TestCreateHandler_CommaInPlayerName_NotCorrupted(t *testing.T) {
	// The quoted form is exactly what the JS csvField helper emits.
	const quotedName = `"Doe, John"`
	f := postCreate(t, leagueForm(quotedName+", DojoX\nBob, DB\nCharlie, DC\nDave, DD\nEve, DE\nFrank, DF"))

	rows, err := f.GetRows("data")
	require.NoError(t, err)

	// data sheet: column B (idx 1) = Player Name, column C (idx 2) = Player Dojo.
	found := false
	for _, row := range rows {
		if len(row) > 1 && row[1] == "Doe, John" {
			found = true
			require.GreaterOrEqual(t, len(row), 3, "row for comma-name player missing dojo column")
			require.Equal(t, "DojoX", row[2], "dojo must not absorb part of a comma-containing name")
		}
	}
	require.True(t, found, "comma-containing player name was corrupted (split on the comma) in the data sheet")
}

// TestCreateHandler_PartialPoolFormat_FewerMatches verifies the /create
// generator honours a competition's PoolFormat=partial (mp-x0u9 follow-up):
// AdminExport posts poolFormat=partial, which must route to
// CreatePartialPoolMatches (a path graph of N-1 matches) instead of the default
// full round-robin (N*(N-1)/2). Match rows are mostly formula cells (empty in
// GetRows) and "vs" is just the block header, so the match count is measured by
// geometry: the rows between the "vs" header and the "Results" block.
func TestCreateHandler_PartialPoolFormat_FewerMatches(t *testing.T) {
	roster := "Alice, DA\nBob, DB\nCharlie, DC\nDave, DD\nEve, DE\nFrank, DF" // 1 pool of 6
	// Match rows are formula cells (GetRows shows them empty), so count them by
	// geometry: the block runs from the "White … vs … Red" header to the
	// "Results" block, separated by one blank row. matches = resultsRow - headerRow - 2.
	matchCount := func(f *excelize.File) int {
		rows, err := f.GetRows("Pool Matches")
		require.NoError(t, err)
		headerRow, resultsRow := -1, -1
		for i, row := range rows {
			for _, cell := range row {
				if cell == "vs" {
					headerRow = i
				}
			}
			if len(row) > 0 && row[0] == "Results" {
				resultsRow = i
				break
			}
		}
		require.Positive(t, headerRow+1, "match header row not found")
		require.Positive(t, resultsRow+1, "Results block not found")
		return resultsRow - headerRow - 2
	}

	full := matchCount(postCreate(t, leagueForm(roster))) // round-robin → 6*5/2 = 15

	partialForm := leagueForm(roster)
	partialForm.Set("poolFormat", "partial")          // takes precedence over roundRobin
	partial := matchCount(postCreate(t, partialForm)) // path graph → 6-1 = 5

	require.Equal(t, 15, full, "round-robin pool of 6 should produce 15 matches")
	require.Equal(t, 5, partial, "partial (path-graph) pool of 6 should produce 5 matches")
	require.Less(t, partial, full, "partial pool format must generate fewer matches than round-robin")
}

// TestCreateHandler_CRLFRoster_Normalized guards the newline-normalization fix:
// a CRLF roster (as a browser textarea / curl can send) must not leave a
// trailing "\r" on the last field nor trip the exact-match duplicate check on a
// blank "\r" line. The handler normalizes CRLF→LF before splitting.
func TestCreateHandler_CRLFRoster_Normalized(t *testing.T) {
	crlf := "Alice, DA\r\nBob, DB\r\nCharlie, DC\r\nDave, DD\r\nEve, DE\r\nFrank, DF\r\n"
	f := postCreate(t, leagueForm(crlf)) // 200, no phantom-duplicate 400, no parse error

	rows, err := f.GetRows("data")
	require.NoError(t, err)
	for _, row := range rows {
		// Column C (idx 2) is Player Dojo, it must not carry a trailing CR.
		if len(row) > 2 && row[1] == "Frank" {
			require.Equal(t, "DF", row[2], "dojo retained a trailing carriage return from CRLF input")
		}
	}
}

// engiPoolForm builds the POST /create body for an engi pools competition.
// Players are standard 2-column rows with the pair combined in the name:
// "Member1 - Member2, Dojo".
func engiPoolForm(playerList string) url.Values {
	return url.Values{
		"tournamentType": {"pools"},
		"playerList":     {playerList},
		"courts":         {"1"},
		"winnersPerPool": {"2"},
		"playersPerPool": {"3"},
		"poolSizeMode":   {"min"},
		"teamMatches":    {"0"},
		"roundRobin":     {"on"},
		"determined":     {"on"},
		"engi":           {"on"},
	}
}

// TestCreateHandler_EngiPools_FlagsHeader asserts that POST /create with
// engi=on routes to the engi formula path: the Pool Matches sheet uses
// "Flags" as the standings column header instead of "T" (and has no "PW"/"PL"),
// and the W-cell formula contains the N() coercion idiom specific to engi.
func TestCreateHandler_EngiPools_FlagsHeader(t *testing.T) {
	// 4 pairs (each "Name1 - Name2, Dojo") → 1 pool of 4
	const roster = "Aoi - Haru, DojoA\nBo - Cho, DojoB\nDai - Ebi, DojoC\nFu - Go, DojoD"
	f := postCreate(t, engiPoolForm(roster))

	rows, err := f.GetRows("Pool Matches")
	require.NoError(t, err)

	// Locate the Results header row; it carries W / L / Flags / Rank.
	resultsRow := -1
	for i, row := range rows {
		if len(row) > 0 && row[0] == "Results" {
			resultsRow = i + 1 // 1-based
			break
		}
	}
	require.Positive(t, resultsRow, "Results block not found on Pool Matches sheet")

	// The Results header row must contain "Flags" and must NOT contain "L", "PW", or "PL".
	hRow := rows[resultsRow-1]
	found := false
	for _, cell := range hRow {
		if cell == "Flags" {
			found = true
		}
		require.NotEqual(t, "L", cell, "engi Pool Matches must not have an L column (losses not recorded)")
		require.NotEqual(t, "PW", cell, "engi Pool Matches must not have a PW column")
		require.NotEqual(t, "PL", cell, "engi Pool Matches must not have a PL column")
	}
	require.True(t, found, "engi Pool Matches must have a Flags column header")

	// The W formula for each player must use N() coercion (engi path).
	sawEngiFormula := false
	for r := resultsRow + 1; r <= resultsRow+4; r++ {
		cell, _ := excelize.CoordinatesToCellName(2, r) // column B == W
		formula, ferr := f.GetCellFormula("Pool Matches", cell)
		require.NoError(t, ferr)
		if strings.Contains(formula, "N(") && strings.Contains(formula, "ISNUMBER") {
			sawEngiFormula = true
		}
	}
	require.True(t, sawEngiFormula, "at least one W cell must use N()-coercion + ISNUMBER (engi formula path)")
}

// TestCreateHandler_NoEngi_PWHeaderPresent is the regression guard for the
// non-engi path: without engi=on the Pool Matches sheet must still carry the
// kendo "PW" header and must NOT have a "Flags" header.
func TestCreateHandler_NoEngi_PWHeaderPresent(t *testing.T) {
	f := postCreate(t, leagueForm("Alice, DA\nBob, DB\nCharlie, DC\nDave, DD\nEve, DE\nFrank, DF"))

	rows, err := f.GetRows("Pool Matches")
	require.NoError(t, err)

	resultsRow := -1
	for i, row := range rows {
		if len(row) > 0 && row[0] == "Results" {
			resultsRow = i + 1
			break
		}
	}
	require.Positive(t, resultsRow, "Results block not found on Pool Matches sheet")

	hRow := rows[resultsRow-1]
	hasPW := false
	for _, cell := range hRow {
		require.NotEqual(t, "Flags", cell, "non-engi Pool Matches must not have a Flags column")
		if cell == "PW" {
			hasPW = true
		}
	}
	require.True(t, hasPW, "non-engi Pool Matches must have a PW column")
}

// naginataPlayoffForm builds the POST /create body for a naginata playoffs
// competition.
func naginataPlayoffForm(playerList string) url.Values {
	return url.Values{
		"tournamentType": {"playoffs"},
		"playerList":     {playerList},
		"courts":         {"1"},
		"teamMatches":    {"0"},
		"determined":     {"on"},
		"naginata":       {"on"},
	}
}

// TestCreateHandler_NaginataPlayoffs_ThirdPlaceBlock asserts that POST /create
// with naginata=on and at least 4 players (so a semifinal round exists) produces
// a "3rd Place" block on the Elimination Matches sheet.
func TestCreateHandler_NaginataPlayoffs_ThirdPlaceBlock(t *testing.T) {
	const roster = "Alice, DA\nBob, DB\nCharlie, DC\nDave, DD"
	f := postCreate(t, naginataPlayoffForm(roster))

	rows, err := f.GetRows("Elimination Matches")
	require.NoError(t, err)

	found := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "3rd Place" {
				found = true
			}
		}
	}
	require.True(t, found, "naginata playoffs with 4 players must have a '3rd Place' block on Elimination Matches")
}

// TestCreateHandler_NoNaginata_NoThirdPlaceBlock is the regression guard:
// without naginata=on the "3rd Place" block must NOT appear.
func TestCreateHandler_NoNaginata_NoThirdPlaceBlock(t *testing.T) {
	const roster = "Alice, DA\nBob, DB\nCharlie, DC\nDave, DD"
	v := url.Values{
		"tournamentType": {"playoffs"},
		"playerList":     {roster},
		"courts":         {"1"},
		"teamMatches":    {"0"},
		"determined":     {"on"},
	}
	f := postCreate(t, v)

	rows, err := f.GetRows("Elimination Matches")
	require.NoError(t, err)

	for _, row := range rows {
		for _, cell := range row {
			require.NotEqual(t, "3rd Place", cell, "non-naginata playoffs must not have a '3rd Place' block")
		}
	}
}

// TestCreateHandler_NaginataPlayoffs_ThirdPlaceBlock_EntrantFormulas verifies
// that the blank template's bronze entrant cells (the hand-scoring surface) carry
// CONCATENATE formulas that reference the losers of the two semifinals. The
// operator writes ippon letters in the semifinal winner cells; the bronze name
// cells then self-populate via "M <n> <winner text>" so the referees can see
// who is competing without manual re-entry.
func TestCreateHandler_NaginataPlayoffs_ThirdPlaceBlock_EntrantFormulas(t *testing.T) {
	const roster = "Alice, DA\nBob, DB\nCharlie, DC\nDave, DD"
	f := postCreate(t, naginataPlayoffForm(roster))

	rows, err := f.GetRows("Elimination Matches")
	require.NoError(t, err)

	// Locate the "3rd Place" header row (0-based index into rows).
	thirdPlaceRowIdx := -1
	for i, row := range rows {
		for _, cell := range row {
			if cell == "3rd Place" {
				thirdPlaceRowIdx = i
				break
			}
		}
		if thirdPlaceRowIdx >= 0 {
			break
		}
	}
	require.GreaterOrEqual(t, thirdPlaceRowIdx, 0, "must find '3rd Place' header before checking formulas")

	// Score row is header+2: 1-based Excel row = (0-based idx + 1) + 2.
	scoreExcelRow := thirdPlaceRowIdx + 3

	leftFormula, err := f.GetCellFormula("Elimination Matches", fmt.Sprintf("A%d", scoreExcelRow))
	require.NoError(t, err)
	rightFormula, err := f.GetCellFormula("Elimination Matches", fmt.Sprintf("G%d", scoreExcelRow))
	require.NoError(t, err)

	// Both cells together must reference both semifinal match numbers ("M 1" and
	// "M 2" for a 4-player bracket) via CONCATENATE formulas. With mirror=true
	// (hardcoded for the playoffs web handler) the two formulas swap sides, so we
	// assert the pair rather than a specific cell.
	combined := leftFormula + " " + rightFormula
	assert.Contains(t, combined, "CONCATENATE", "bronze entrant cells must carry CONCATENATE formulas referencing semifinal losers")
	assert.Contains(t, combined, "M 1", "bronze entrant formulas must reference semifinal M 1")
	assert.Contains(t, combined, "M 2", "bronze entrant formulas must reference semifinal M 2")
}

// cmdParsePrintAreaLastRow extracts the last-row number from a Print_Area
// RefersTo string such as "'Elimination Matches'!$A$1:$H$35". Returns -1 on
// any parse error.
func cmdParsePrintAreaLastRow(refersTo string) int {
	lastDollar := strings.LastIndex(refersTo, "$")
	if lastDollar < 0 {
		return -1
	}
	row, err := strconv.Atoi(refersTo[lastDollar+1:])
	if err != nil {
		return -1
	}
	return row
}

// TestCreateHandler_NaginataPlayoffs_PrintAreaCoversThirdPlace verifies that the
// POST /create response for a naginata playoffs bracket has a _xlnm.Print_Area
// defined name on the Elimination Matches sheet that covers the "3rd Place" block.
// This exercises the create-playoffs code path (tournamentType=playoffs, naginata=on).
func TestCreateHandler_NaginataPlayoffs_PrintAreaCoversThirdPlace(t *testing.T) {
	const roster = "Alice, DA\nBob, DB\nCharlie, DC\nDave, DD"
	f := postCreate(t, naginataPlayoffForm(roster))

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	thirdPlaceExcelRow := -1
	for i, row := range rows {
		for _, cell := range row {
			if cell == "3rd Place" {
				thirdPlaceExcelRow = i + 1
				break
			}
		}
		if thirdPlaceExcelRow >= 0 {
			break
		}
	}
	require.GreaterOrEqual(t, thirdPlaceExcelRow, 1,
		"'3rd Place' header must be present in Elimination Matches")

	var printAreaLastRow int
	for _, dn := range f.GetDefinedName() {
		if dn.Name == "_xlnm.Print_Area" && dn.Scope == helper.SheetEliminationMatches {
			printAreaLastRow = cmdParsePrintAreaLastRow(dn.RefersTo)
			break
		}
	}
	require.Greater(t, printAreaLastRow, 0,
		"_xlnm.Print_Area for Elimination Matches must exist and be parseable")
	assert.GreaterOrEqual(t, printAreaLastRow, thirdPlaceExcelRow,
		"Print_Area last row (%d) must cover at least the '3rd Place' header row (%d)",
		printAreaLastRow, thirdPlaceExcelRow)
}

// engiPlayoffForm builds the POST /create body for an engi knockout-only
// competition: combined pair names, engi=on, naginata=on (bronze block).
func engiPlayoffForm(playerList string) url.Values {
	return url.Values{
		"tournamentType": {"playoffs"},
		"playerList":     {playerList},
		"courts":         {"1"},
		"teamMatches":    {"0"},
		"determined":     {"on"},
		"naginata":       {"on"},
		"engi":           {"on"},
	}
}

// TestCreateHandler_EngiPlayoffs_FlagsCaptions asserts that POST /create with
// tournamentType=playoffs&engi=on routes the elimination blocks through the
// engi rendering path: the match headers carry the "Fl" flag-count captions
// (kendo playoffs have no such captions).
func TestCreateHandler_EngiPlayoffs_FlagsCaptions(t *testing.T) {
	const roster = "Aoi - Haru, DojoA\nBo - Cho, DojoB\nDai - Ebi, DojoC\nFu - Go, DojoD"
	f := postCreate(t, engiPlayoffForm(roster))

	rows, err := f.GetRows("Elimination Matches")
	require.NoError(t, err)

	found := 0
	for _, row := range rows {
		for _, cell := range row {
			if cell == "Fl" {
				found++
			}
		}
	}
	assert.Greater(t, found, 0,
		"engi playoffs elimination blocks must carry 'Fl' flag captions")
}
