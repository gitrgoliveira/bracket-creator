package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// bc-qual LP-3c: --extra-qualifiers CLI wiring.
//
// createPools() validates the flag via state.ValidateExtraQualifiers (the
// same function internal/engine and the web /create form's handler use) and,
// for "larger-pools", builds the per-pool draw via
// helper.BuildKnockoutDrawPerPool instead of the uniform
// helper.BuildKnockoutDraw. These tests exercise the CLI wiring specifically
// (flag registration, validation, and that the per-pool builder is reached
// and either succeeds or fails cleanly); the crossing arithmetic itself is
// exhaustively covered at the helper layer (draw_perpool.go's own EKC
// reference tests) and end to end at the engine layer
// (internal/engine/draw_extra_qualifiers_test.go).

func TestCreatePoolCmdFlags_ExtraQualifiers(t *testing.T) {
	t.Parallel()

	cmd := newCreatePoolCmd()
	flag := cmd.Flags().Lookup("extra-qualifiers")
	require.NotNil(t, flag, "--extra-qualifiers must be registered")
	assert.Equal(t, "", flag.DefValue, "default must be standard mode (empty string)")
}

// TestCreatePools_ExtraQualifiers_ValidationErrors mirrors
// TestCreatePools_MaxMode_ValidationErrors' table-driven style: every
// rejection state.ValidateExtraQualifiers defines must surface through the
// CLI unchanged, without the CLI restating the rule.
func TestCreatePools_ExtraQualifiers_ValidationErrors(t *testing.T) {
	t.Parallel()

	entries := []string{"A,D1", "B,D2", "C,D3", "D,D4", "E,D5", "F,D6"}

	tests := []struct {
		name            string
		numPlayers      int
		maxPlayers      int
		poolWinners     int
		extraQualifiers string
		expectedError   string
	}{
		{
			name:            "larger-pools rejected under max-players (max) mode",
			maxPlayers:      3,
			poolWinners:     1,
			extraQualifiers: "larger-pools",
			expectedError:   "requires minimum-players-per-pool sizing",
		},
		{
			name:            "larger-pools rejected when pool winners >= 2",
			numPlayers:      3,
			poolWinners:     2,
			extraQualifiers: "larger-pools",
			expectedError:   "currently requires pool winners = 1",
		},
		{
			name:            "fill-bracket rejected outright (LP-4 not implemented)",
			numPlayers:      3,
			poolWinners:     1,
			extraQualifiers: "fill-bracket",
			expectedError:   "not yet supported",
		},
		{
			name:            "unknown value rejected",
			numPlayers:      3,
			poolWinners:     1,
			extraQualifiers: "bogus-mode",
			expectedError:   "unknown extraQualifiers",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var b bytes.Buffer
			writer := bufio.NewWriter(&b)

			o := &poolOptions{
				outputWriter:    writer,
				outputPath:      "dummy.xlsx",
				numPlayers:      tt.numPlayers,
				maxPlayers:      tt.maxPlayers,
				poolWinners:     tt.poolWinners,
				extraQualifiers: tt.extraQualifiers,
				determined:      true,
			}

			err := o.createPools(entries)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

// TestCreatePools_ExtraQualifiers_LargerPools_TrivialSuccess proves the CLI
// wiring reaches helper.BuildKnockoutDrawPerPool and succeeds for a
// larger-pools competition with NO oversized pool (an even split, so the
// overrides map built by cliExtraQualifierOverrides is empty) -- the
// simplest possible proof that the flag does not break ordinary pool
// generation.
func TestCreatePools_ExtraQualifiers_LargerPools_TrivialSuccess(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	// 9 entries, pool size 3 min mode -> exactly 3 pools of 3: no pool is
	// oversized.
	entries := []string{
		"A,D1", "B,D2", "C,D3", "D,D4", "E,D5", "F,D6", "G,D7", "H,D8", "I,D9",
	}

	o := &poolOptions{
		outputWriter:    writer,
		outputPath:      "dummy.xlsx",
		numPlayers:      3,
		poolWinners:     1,
		courts:          2,
		extraQualifiers: "larger-pools",
		determined:      true,
	}

	err := o.createPools(entries)
	require.NoError(t, err)
	require.NoError(t, writer.Flush())
	assert.NotZero(t, b.Len(), "a workbook must have been written")
}

// TestCreatePools_ExtraQualifiers_LargerPools_CrossingSuccess forces a real
// oversized pool through the CLI's own pool-formation algorithm (unique
// dojos, so helper.CreatePools' dojo-conflict avoidance never perturbs
// placement) and confirms the larger-pools build succeeds rather than
// returning the "could not build" error -- i.e. that
// helper.BuildKnockoutDrawPerPool is actually reached with a non-empty
// overrides map and handles it. 97 entries / PoolSize 3 / min mode / 4
// courts mirrors
// TestStartCompetition_LargerPools_CrossesOversizedPoolToNeighbourCourt's
// sizing (internal/engine): 32 pools, 8 per shiaijo exactly (no AssignPools
// ToCourts remainder), one oversized pool (forcePoolSize's overflow target).
func TestCreatePools_ExtraQualifiers_LargerPools_CrossingSuccess(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	entries := make([]string, 0, 97)
	for i := 0; i < 97; i++ {
		entries = append(entries, fmt.Sprintf("Player%03d,Dojo%03d", i, i))
	}

	o := &poolOptions{
		outputWriter:    writer,
		outputPath:      "dummy.xlsx",
		numPlayers:      3,
		poolWinners:     1,
		courts:          4,
		extraQualifiers: "larger-pools",
		determined:      true,
	}

	err := o.createPools(entries)
	require.NoError(t, err, "a real oversized pool at EKC-shaped scale must build, not report out of scope")
	require.NoError(t, writer.Flush())
	assert.NotZero(t, b.Len(), "a workbook must have been written")

	// Regression pin (bc-qual LP-3c review finding): the crossed qualifier's
	// Tree-sheet cell must be a LIVE formula referencing the pool's actual
	// result cell, exactly like every other pool-origin leaf, not inert
	// literal text and not a BROKEN CONCATENATE formula (empty sheet/cell
	// reference) on the Elimination Matches sheet. Both failure modes were
	// reachable before helper.PrintPoolMatches was told to register a
	// matchWinners["<pool>-2nd"] entry via
	// state.Competition.MatchWinnerRanksNeeded (cmd/create-pools.go): the
	// crossed leaf's rank (2) exceeded the numWinners bound (1) that was
	// passed before the fix, so it fell outside every pool's registered
	// rank range and had no matchWinners entry at all.
	wb, err := excelize.OpenReader(bytes.NewReader(b.Bytes()))
	require.NoError(t, err)
	defer wb.Close()

	foundCrossedFormula := false
	for _, sheet := range wb.GetSheetList() {
		if !strings.HasPrefix(sheet, "Tree") && sheet != "Elimination Matches" {
			continue
		}
		rows, err := wb.GetRows(sheet)
		require.NoError(t, err)
		for r := range rows {
			for c := 0; c < 40; c++ {
				addr, _ := excelize.CoordinatesToCellName(c+1, r+1)
				formula, ferr := wb.GetCellFormula(sheet, addr)
				require.NoError(t, ferr)
				if formula == "" {
					continue
				}
				// A missing matchWinners entry formats as '%s'!%s with both
				// %s empty ('%s'!%s -> ''!): the sheet-name quotes collapse
				// together immediately followed by "!" and the (empty) cell
				// reference.
				require.NotContainsf(t, formula, "''!", "broken CONCATENATE formula (empty sheet/cell ref) at %s!%s: %s", sheet, addr, formula)
				if strings.Contains(formula, "-2nd") && strings.Contains(formula, "'Pool Matches'!") {
					foundCrossedFormula = true
				}
			}
		}
	}
	assert.True(t, foundCrossedFormula, "expected a live CONCATENATE(\"Pool <X>-2nd \",'Pool Matches'!<cell>) formula for the oversized pool's crossed qualifier")
}

// TestCreatePools_ExtraQualifiers_LargerPools_OutOfScopeSingleCourt proves
// bc-qual LP-3a review item (b) at the CLI layer: a single-shiaijo
// competition has no same-half neighbour court for an oversized pool's extra
// qualifier to cross to, so helper.BuildKnockoutDrawPerPool correctly
// returns nil, and the CLI must report a clean, actionable error -- never
// silently fall back to the uniform builder (which would seat the wrong
// number of qualifiers for the oversized pool) and never panic.
//
// 10 entries, pool size 3 min mode, unique dojos -> 3 pools of 3 plus one
// leftover seated by forcePoolSize into pool index 0, producing exactly one
// oversized (4-player) pool (same arithmetic as
// TestStartCompetition_LargerPools_OutOfScopeSingleCourt_ReturnsValidationError,
// internal/engine).
func TestCreatePools_ExtraQualifiers_LargerPools_OutOfScopeSingleCourt(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	entries := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		entries = append(entries, fmt.Sprintf("Player%02d,Dojo%02d", i, i))
	}

	o := &poolOptions{
		outputWriter:    writer,
		outputPath:      "dummy.xlsx",
		numPlayers:      3,
		poolWinners:     1,
		courts:          1, // single shiaijo: no same-half neighbour to cross to
		extraQualifiers: "larger-pools",
		determined:      true,
	}

	err := o.createPools(entries)
	require.Error(t, err, "an out-of-scope larger-pools shape must fail cleanly, not silently fall back to the uniform draw")
	assert.Contains(t, err.Error(), "could not build a larger-pools knockout draw")
}

// --- web /create form field wiring (createTournamentHandler) ---

// extraQualifiersForm builds the minimal "pools" tournamentType payload
// createTournamentHandler needs, with an extraQualifiers field the plain
// leagueForm helper (create_handler_test.go) doesn't carry.
func extraQualifiersForm(playerList string, winnersPerPool, playersPerPool int, poolSizeMode, courts, extraQualifiers string) url.Values {
	return url.Values{
		"tournamentType":  {"pools"},
		"playerList":      {playerList},
		"courts":          {courts},
		"winnersPerPool":  {fmt.Sprintf("%d", winnersPerPool)},
		"playersPerPool":  {fmt.Sprintf("%d", playersPerPool)},
		"poolSizeMode":    {poolSizeMode},
		"teamMatches":     {"0"},
		"determined":      {"on"},
		"extraQualifiers": {extraQualifiers},
	}
}

// TestCreateHandler_ExtraQualifiers_LargerPools_Success proves the web
// /create form's extraQualifiers field actually reaches poolOptions and a
// valid larger-pools request succeeds end to end through the HTTP handler,
// not just through poolOptions.createPools called directly.
func TestCreateHandler_ExtraQualifiers_LargerPools_Success(t *testing.T) {
	roster := "A, D1\nB, D2\nC, D3\nD, D4\nE, D5\nF, D6\nG, D7\nH, D8\nI, D9" // 9 -> 3 pools of 3
	form := extraQualifiersForm(roster, 1, 3, "min", "2", "larger-pools")

	f := postCreate(t, form)
	rows, err := f.GetRows("Pool Draw")
	require.NoError(t, err)
	assert.NotEmpty(t, rows, "a workbook with pool data must have been generated")
}

// TestCreateHandler_ExtraQualifiers_Rejected_ReturnsBadRequest proves the
// form field is validated with the SAME rule as the CLI flag
// (state.ValidateExtraQualifiers), by triggering the max-mode rejection over
// HTTP and checking for a 400 whose body names the reason, not a 200 with a
// silently-ignored setting or a 500.
func TestCreateHandler_ExtraQualifiers_Rejected_ReturnsBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/create", createTournamentHandler)

	roster := "A, D1\nB, D2\nC, D3\nD, D4"
	form := extraQualifiersForm(roster, 1, 3, "max", "2", "larger-pools")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "requires minimum-players-per-pool sizing")
}
