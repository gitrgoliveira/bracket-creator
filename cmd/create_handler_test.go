package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
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
// formula — NOT "=0".
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
			"W cell %s collapsed to =0 — pool scoring formulas are dead (mp-x0u9 regression)", cell)
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
// name would split into name="Doe", dojo="John" — silent corruption.
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
