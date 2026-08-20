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
			name:            "fill-bracket rejected under max-players (max) mode",
			maxPlayers:      3,
			poolWinners:     1,
			extraQualifiers: "fill-bracket",
			expectedError:   "requires minimum-players-per-pool sizing",
		},
		{
			name:            "fill-bracket rejected when pool winners >= 2",
			numPlayers:      3,
			poolWinners:     2,
			extraQualifiers: "fill-bracket",
			expectedError:   "currently requires pool winners = 1",
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

// TestCreatePools_ExtraQualifiers_LargerPools_SingleShiaijoBuilds covers the
// CLI at the shape LP-3d opened up. A single-shiaijo larger-pools run used to
// fail with "could not build a larger-pools knockout draw", because the extra
// qualifier had no neighbouring court to cross to; it now builds, seating the
// extra in the opposite half of the only block there is.
//
// The refusal path itself is still wired (buildPoolFedDraw reports
// outOfScope and createPools turns that into an error rather than falling
// back to the uniform draw) -- it is simply no longer reachable at this
// shape. helper's own tests own what remains out of scope.
func TestCreatePools_ExtraQualifiers_LargerPools_SingleShiaijoBuilds(t *testing.T) {
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
		courts:          1, // single shiaijo: the extra stays in this block
		extraQualifiers: "larger-pools",
		determined:      true,
	}

	require.NoError(t, o.createPools(entries),
		"a single-shiaijo larger-pools run must build since LP-3d")
}

// --- fill-bracket (bc-qual LP-4) ---

// TestCreatePools_ExtraQualifiers_FillBracket_TrivialSuccess proves the CLI
// wiring reaches helper.BuildPoolPhaseFillBracket / BuildKnockoutDrawFillBracket
// and succeeds for a fill-bracket competition with ZERO drafts needed: 60
// entrants at minimum pool size 3 forms exactly 16 pools (12 of 4, 4 of 3;
// see FillBracketPoolCount's doc comment, the 19WKC Men's Team shape), a
// power of two on its own, so no 2nd is drafted at all.
func TestCreatePools_ExtraQualifiers_FillBracket_TrivialSuccess(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	entries := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		entries = append(entries, fmt.Sprintf("Player%02d,Dojo%02d", i, i))
	}

	o := &poolOptions{
		outputWriter:    writer,
		outputPath:      "dummy.xlsx",
		numPlayers:      3,
		poolWinners:     1,
		courts:          4,
		extraQualifiers: "fill-bracket",
		determined:      true,
	}

	err := o.createPools(entries)
	require.NoError(t, err)
	require.NoError(t, writer.Flush())
	assert.NotZero(t, b.Len(), "a workbook must have been written")
}

// TestCreatePools_ExtraQualifiers_FillBracket_DraftSuccess forces the real
// draft-and-cross shape through the CLI's own pool-formation pipeline: 45
// entrants at minimum pool size 3 forms 14 pools (11 of 3, 3 of 4; the
// 19WKC Women's Team shape, FillBracketPoolCount's doc comment) needing D=2
// drafted 2nds to fill a 16-leaf bracket with zero byes. Mirrors
// TestCreatePools_ExtraQualifiers_LargerPools_CrossingSuccess's formula-pin
// pattern: the drafted 2nd's Tree/Elimination-sheet cell must be a LIVE
// formula referencing the pool's actual result, not inert literal text or a
// broken CONCATENATE formula.
func TestCreatePools_ExtraQualifiers_FillBracket_DraftSuccess(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	entries := make([]string, 0, 45)
	for i := 0; i < 45; i++ {
		entries = append(entries, fmt.Sprintf("Player%03d,Dojo%03d", i, i))
	}

	o := &poolOptions{
		outputWriter:    writer,
		outputPath:      "dummy.xlsx",
		numPlayers:      3,
		poolWinners:     1,
		courts:          4,
		extraQualifiers: "fill-bracket",
		determined:      true,
	}

	err := o.createPools(entries)
	require.NoError(t, err, "45 entrants at minimum pool size 3 on 4 courts must build a fill-bracket draw, not report out of scope")
	require.NoError(t, writer.Flush())
	assert.NotZero(t, b.Len(), "a workbook must have been written")

	wb, err := excelize.OpenReader(bytes.NewReader(b.Bytes()))
	require.NoError(t, err)
	defer wb.Close()

	foundDraftedFormula := false
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
				require.NotContainsf(t, formula, "''!", "broken CONCATENATE formula (empty sheet/cell ref) at %s!%s: %s", sheet, addr, formula)
				if strings.Contains(formula, "-2nd") && strings.Contains(formula, "'Pool Matches'!") {
					foundDraftedFormula = true
				}
			}
		}
	}
	assert.True(t, foundDraftedFormula, "expected a live CONCATENATE(\"Pool <X>-2nd \",'Pool Matches'!<cell>) formula for a drafted 2nd qualifier")
}

// TestCreatePools_ExtraQualifiers_FillBracket_OutOfScope proves fill-bracket
// fails loudly rather than guessing when the pool/court shape does not
// resolve into a zero-bye draw (bc-qual LP-4 scope discipline).
//
// This is NOT the "homes split 7/7" shape a first cut of this feature used
// here: that cut required each court's home-pool count to ALREADY be a
// power of two, which a review caught as disagreeing with
// FillBracketPoolCount's own formation promise (n=45 at 2 shiaijo -- 14
// pools split 7/7 -- is one of the review's three named counterexamples,
// and now legitimately SUCCEEDS: see
// TestCreatePools_ExtraQualifiers_FillBracket_DraftSuccess's neighbour for
// the reworked algorithm). The genuinely out-of-scope case after the rework
// is DATA-DEPENDENT rather than shape-dependent: the drafted pools' own
// halves must supply exactly what the opposite half's short courts need,
// which is not guaranteed even when the formation and per-court target
// arithmetic both check out. A THIRD review made SelectFillBracketDrafts
// itself capacity-aware (skip a candidate whose destination half is full,
// keep scanning, rather than committing to strict order), and the
// WKC-derived seeded-first rework then shrank the swept residue further:
// see TestFillBracketFormationAndBuilderAgree (internal/helper), which
// sweeps both supply regimes and pins the exact residual lists. 18 entrants
// at minimum pool size 3, 4 shiaijo, UNSEEDED (this CLI has no --seeds here)
// is in the unseeded residue (P=5, D=3), reproduced through the real CLI
// pipeline rather than asserted from the helper layer alone -- and the
// error's own remedy is real at this layer too: the same roster with seeds
// routes fine. The failure point also MOVED with the capacity rework: it is
// now SelectFillBracketDrafts itself (before any placement is attempted),
// not BuildKnockoutDrawFillBracket.
func TestCreatePools_ExtraQualifiers_FillBracket_OutOfScope(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	entries := make([]string, 0, 18)
	for i := 0; i < 18; i++ {
		entries = append(entries, fmt.Sprintf("Player%03d,Dojo%03d", i, i))
	}

	o := &poolOptions{
		outputWriter:    writer,
		outputPath:      "dummy.xlsx",
		numPlayers:      3,
		poolWinners:     1,
		courts:          4, // 18 entrants at minimum 3 -> 5 pools, 3 drafts: a half-capacity mismatch at 4 shiaijo
		extraQualifiers: "fill-bracket",
		determined:      true,
	}

	err := o.createPools(entries)
	require.Error(t, err, "an out-of-scope fill-bracket shape must fail cleanly, not silently fall back to the uniform draw")
	assert.Contains(t, err.Error(), "could not select fill-bracket drafts")
	assert.Contains(t, err.Error(), "opposite half of the bracket")
	assert.Contains(t, err.Error(), "seed more pools",
		"the message must name the remedy that actually clears this shape")
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

// TestCreateHandler_ExtraQualifiers_FillBracket_Success is
// TestCreateHandler_ExtraQualifiers_LargerPools_Success's fill-bracket
// counterpart (bc-qual LP-4): the web /create form's extraQualifiers field
// reaches poolOptions and a valid fill-bracket request succeeds end to end
// through the HTTP handler. 12 entrants at minimum pool size 3 forms
// exactly 4 pools (FillBracketPoolCount: maxP=4, and 4 is already a power
// of two so 0 drafts are needed) -- the trivial case, proven through the
// HTTP boundary rather than createPools directly. (9 entrants, the plain
// larger-pools test's roster size, does NOT work here: FillBracketPoolCount
// has no valid pool count for 9 entrants at minimum size 3 -- the only
// candidate, 3 pools of exactly 3, needs 1 draft to reach a 4-leaf bracket
// but has zero oversized pools to draft from.)
func TestCreateHandler_ExtraQualifiers_FillBracket_Success(t *testing.T) {
	roster := "A, D1\nB, D2\nC, D3\nD, D4\nE, D5\nF, D6\nG, D7\nH, D8\nI, D9\nJ, D10\nK, D11\nL, D12" // 12 -> 4 pools of 3
	form := extraQualifiersForm(roster, 1, 3, "min", "2", "fill-bracket")

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
