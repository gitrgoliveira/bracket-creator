package export

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

// makePlayer creates a domain.Player for tests.
func makePlayer(name string) domain.Player {
	return domain.Player{ID: name, Name: name, Dojo: "Dojo"}
}

// make2PlayerPool builds a pool whose single match's SideA/SideB point into
// pool.Players (not local copies) so playerMatchRows resolves correctly.
func make2PlayerPool(name, a, b string) helper.Pool {
	p := helper.Pool{PoolName: name, Players: []helper.Player{makePlayer(a), makePlayer(b)}}
	p.Matches = []helper.Match{{SideA: &p.Players[0], SideB: &p.Players[1]}}
	return p
}

// testSetup creates a temp store + engine and saves a minimal competition.
// It returns the dir (caller must os.RemoveAll), store, engine, and compID.
func testSetup(t *testing.T) (dir string, store *state.Store, eng *engine.Engine, compID string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "export-test-*")
	require.NoError(t, err)

	store, err = state.NewStore(dir)
	require.NoError(t, err)

	eng = engine.New(store)
	compID = "test-comp"

	comp := &state.Competition{
		ID:     compID,
		Name:   "Test Tournament",
		Courts: []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(comp))
	return
}

// setCompFormat loads the competition, sets its Format, and saves it. Bracket
// tests that build pools + a knockout must use a knockout-enabled format (Mixed),
// since the export only emits Elimination/Tree sheets when comp.IsKnockoutEnabled().
func setCompFormat(t *testing.T, store *state.Store, compID, format string) {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = format
	require.NoError(t, store.SaveCompetition(comp))
}

// markCompAsEngi loads the competition, sets Engi=true, and saves it.
func markCompAsEngi(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Engi = true
	require.NoError(t, store.SaveCompetition(comp))
}

// makePools builds two pools of two players each with one match.
// SideA/SideB point into pool.Players so the playerMatchRows map in
// PrintPoolMatches resolves correctly (pointers must match &pool.Players[i]).
func makePools() []helper.Pool {
	pool1 := helper.Pool{
		PoolName: "Pool A",
		Players:  []helper.Player{makePlayer("Alice"), makePlayer("Bob")},
	}
	pool1.Matches = []helper.Match{{SideA: &pool1.Players[0], SideB: &pool1.Players[1]}}

	pool2 := helper.Pool{
		PoolName: "Pool B",
		Players:  []helper.Player{makePlayer("Charlie"), makePlayer("Dave")},
	}
	pool2.Matches = []helper.Match{{SideA: &pool2.Players[0], SideB: &pool2.Players[1]}}

	return []helper.Pool{pool1, pool2}
}

// headerColInBand returns the 0-based column index of the first cell equal to
// header within the [bandStart, bandEnd) column range, or -1 if absent.
// Use it to locate standings headers like "W" or helper.ColHeaderFlags in a
// specific court band without hard-coding column offsets.
func headerColInBand(rows [][]string, header string, bandStart, bandEnd int) int {
	for _, row := range rows {
		for c := bandStart; c < bandEnd && c < len(row); c++ {
			if row[c] == header {
				return c
			}
		}
	}
	return -1
}

// columnContains reports whether the given 0-based column holds a cell == want.
func columnContains(rows [][]string, col int, want string) bool {
	for _, row := range rows {
		if col >= 0 && col < len(row) && row[col] == want {
			return true
		}
	}
	return false
}

// cellAt is bctest.CellAt under the name this package's tests use.
var cellAt = bctest.CellAt

// cellRefWithValue returns the 1-based excel cell reference of the first cell in
// rows that equals want, or "" if absent.
func cellRefWithValue(t *testing.T, rows [][]string, want string) string {
	t.Helper()
	for r, row := range rows {
		for c, cell := range row {
			if cell == want {
				ref, err := excelize.CoordinatesToCellName(c+1, r+1)
				require.NoError(t, err)
				return ref
			}
		}
	}
	return ""
}

// columnHasValueUnderHeader reports whether any data row below a cell equal to
// header (in the same column) holds want. Used to assert literal standings.
func columnHasValueUnderHeader(rows [][]string, header, want string) bool {
	for rowIdx, row := range rows {
		for colIdx, cell := range row {
			if cell != header {
				continue
			}
			for off := 1; off <= 8 && rowIdx+off < len(rows); off++ {
				r := rows[rowIdx+off]
				if colIdx < len(r) && r[colIdx] == want {
					return true
				}
			}
		}
	}
	return false
}

// treeNodeRowSpan returns the sheet rows a tree-page node's subtree occupies,
// by inverting PrintLeafNodes' placement for the cell at (col, row). ok is false
// when the cell sits on no node position at all.
//
// PrintLeafNodes halves the row span and steps the column left by 2 per level,
// so the node at column c = 2d+1 and index i within its level lands at row
// first + i*2^d + 2^(d-1), and its subtree covers the rows
// first + i*2^d + 1 .. first + (i+1)*2^d, which is the inclusive (lo, hi) pair
// returned here. Both kinds of content sit at that one
// cell: a leaf's label (writeTreeValue) and an internal node's junction
// connector, which is where FillInMatches writes the printed match number. So a
// junction's entrants are exactly the leaf cells whose row falls in its span,
// which is what lets a rendered page be read back as "which bout is numbered
// what" instead of recomputed from the code that drew it.
func treeNodeRowSpan(col, row int) (lo, hi int, ok bool) {
	const first = helper.TreeTitleRows + 1 // RenderTreePages' page start row
	if col%2 == 0 {
		return 0, 0, false
	}
	d := (col - 1) / 2
	if d < 1 {
		return 0, 0, false
	}
	span, off := 1<<d, 1<<(d-1)
	rel := row - first - off
	if rel < 0 || rel%span != 0 {
		return 0, 0, false
	}
	i := rel / span
	return first + i*span + 1, first + (i+1)*span, true
}

// assertNoBrokenFormulas evaluates every formula cell in the sheet and fails if
// any cannot be calculated or resolves to an Excel error (#REF!, #DIV/0!, …). The
// results export overwrites the collapse-prone W/L/T/RANK/score formulas with
// literals, so the only formulas left (cross-sheet name references) must resolve
// cleanly in every completeness state.
func assertNoBrokenFormulas(t *testing.T, f *excelize.File, sheet string) {
	t.Helper()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	width := maxRowWidth(rows)
	for r := range rows {
		for c := 0; c < width; c++ {
			col, _ := excelize.ColumnNumberToName(c + 1)
			ref := fmt.Sprintf("%s%d", col, r+1)
			fm, _ := f.GetCellFormula(sheet, ref)
			if fm == "" {
				continue
			}
			v, cerr := f.CalcCellValue(sheet, ref)
			assert.NoErrorf(t, cerr, "%s!%s formula %q failed to calculate", sheet, ref, fm)
			assert.NotContainsf(t, v, "#", "%s!%s formula %q resolved to an Excel error %q", sheet, ref, fm, v)
		}
	}
}

// maxRowWidth returns the widest row's column count. Courts are laid out in
// side-by-side 8-column bands, and every band's header row ("Red"/"White"/
// "Round N") populates its columns, so the widest row spans ALL court bands.
// Scanning up to this width covers every formula cell regardless of court count,
// unlike a fixed 24-column (A-X, ~3 court) bound.
func maxRowWidth(rows [][]string) int {
	w := 0
	for _, r := range rows {
		if len(r) > w {
			w = len(r)
		}
	}
	return w
}

// makeTeamPools builds two team pools of two teams each, one team encounter per pool.
// SideA/SideB point into pool.Players so the playerMatchRows map resolves correctly.
func makeTeamPools() []helper.Pool {
	poolA := helper.Pool{
		PoolName: "Pool A",
		Players:  []helper.Player{makePlayer("Red A"), makePlayer("Blue A")},
	}
	poolA.Matches = []helper.Match{{SideA: &poolA.Players[0], SideB: &poolA.Players[1]}}

	poolB := helper.Pool{
		PoolName: "Pool B",
		Players:  []helper.Player{makePlayer("Red B"), makePlayer("Blue B")},
	}
	poolB.Matches = []helper.Match{{SideA: &poolB.Players[0], SideB: &poolB.Players[1]}}

	return []helper.Pool{poolA, poolB}
}

// sheetContainsCell reports whether any cell in rows equals val.
func sheetContainsCell(rows [][]string, val string) bool {
	for _, row := range rows {
		for _, cell := range row {
			if cell == val {
				return true
			}
		}
	}
	return false
}

// formulaCellCount returns how many cells in the sheet carry a formula. Used to
// distinguish a populated tree page from a blank one.
func formulaCellCount(t *testing.T, f *excelize.File, sheet string) int {
	t.Helper()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	width := maxRowWidth(rows)
	count := 0
	for r := range rows {
		for c := 0; c < width; c++ {
			col, _ := excelize.ColumnNumberToName(c + 1)
			fm, _ := f.GetCellFormula(sheet, fmt.Sprintf("%s%d", col, r+1))
			if fm != "" {
				count++
			}
		}
	}
	return count
}

// makeEngiPools builds two engi pairs. Each pair is ONE participant with both
// member names combined in Name ("Member 1 - Member 2"); DisplayName is not
// used for engi. SideA/SideB point into pool.Players so the playerMatchRows
// map resolves correctly.
func makeEngiPools() []helper.Pool {
	pool := helper.Pool{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "pair1", Name: "Member One A - Member Two A", Dojo: "DojoA"},
			{ID: "pair2", Name: "Member One B - Member Two B", Dojo: "DojoB"},
		},
	}
	pool.Matches = []helper.Match{{SideA: &pool.Players[0], SideB: &pool.Players[1]}}
	return []helper.Pool{pool}
}

// firstPoolMatchScoreRow returns the row from `rows` that holds the first match's
// score cells for the pool assigned to the court band starting at 0-based column
// `bandStart` ("White" marks that column, the LEFT side-label; helper.WhiteLeft
// puts White there unconditionally, so it is the one label this scan needs).
// The match row is one row below the White/Red header. Returns nil if no such
// header exists.
// Column layout within the band (0-based absolute): bandStart+1 = left score,
// bandStart+3 = vs/middle, bandStart+5 = right score.
func firstPoolMatchScoreRow(rows [][]string, bandStart int) []string {
	for ri, row := range rows {
		if bandStart >= len(row) {
			continue
		}
		if row[bandStart] == "White" {
			if ri+1 < len(rows) {
				return rows[ri+1]
			}
		}
	}
	return nil
}

// bracketVictoryCells locates the "Round R - Match M" header in the elimination
// sheet and returns the left/right victory-cell values from the player/score row
// (header row + 2). Column offsets mirror overlayBracketScores: with the header
// at 0-based column headerCol, the court start column (1-based) is headerCol+1,
// so the left victory cell (courtStartCol+1) is 0-based headerCol+1 and the right
// victory cell (courtStartCol+5) is 0-based headerCol+5. Failing to find the
// header is fatal so a layout drift surfaces instead of a silent empty compare.
func bracketVictoryCells(t *testing.T, rows [][]string, label string) (left, right string) {
	t.Helper()
	for rowIdx, row := range rows {
		for colIdx, cell := range row {
			if cell != label {
				continue
			}
			scoreRowIdx := rowIdx + 2
			require.Less(t, scoreRowIdx, len(rows),
				"score row must exist two rows below header %q", label)
			scoreRow := rows[scoreRowIdx]
			if lIdx := colIdx + 1; lIdx < len(scoreRow) {
				left = scoreRow[lIdx]
			}
			if rIdx := colIdx + 5; rIdx < len(scoreRow) {
				right = scoreRow[rIdx]
			}
			return left, right
		}
	}
	t.Fatalf("bracket header %q not found in elimination sheet", label)
	return "", ""
}

// assertBronzeEntrantsPopulated finds the "3rd Place" header, steps to the score
// row (header+2), and asserts the left (col A) and right (col G) name cells are set.
func assertBronzeEntrantsPopulated(t *testing.T, rows [][]string) {
	t.Helper()
	thirdRow := bctest.FindCellRow(rows, helper.ThirdPlaceLabel)
	require.GreaterOrEqual(t, thirdRow, 0, "'3rd Place' header must be present")
	scoreIdx := thirdRow + 2
	require.Less(t, scoreIdx, len(rows), "bronze score row (header+2) must exist")
	r := rows[scoreIdx]
	require.Greater(t, len(r), 6, "bronze score row must have at least 7 columns")
	assert.NotEmpty(t, r[0], "bronze score row: left name cell (col A) must be populated")
	assert.NotEmpty(t, r[6], "bronze score row: right name cell (col G) must be populated")
}

// setNaginataKnockout configures the loaded competition as a naginata knockout
// competition (individual, 4 players, single court).
func setNaginataKnockout(t *testing.T, store *state.Store, compID string, engi bool) {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = state.CompFormatKnockout
	comp.Naginata = true
	comp.Engi = engi
	comp.Kind = "individual"
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 2
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
}

// startNaginataWith4Players saves 4 participants, starts the competition, and
// returns the bracket. The caller must have called setNaginataKnockout first.
func startNaginataWith4Players(t *testing.T, store *state.Store, eng *engine.Engine, compID string, engi bool) *state.Bracket {
	t.Helper()
	var players []domain.Player
	if engi {
		players = []domain.Player{
			{Name: "Pair1A - Pair1B", Dojo: "DojoA"},
			{Name: "Pair2A - Pair2B", Dojo: "DojoB"},
			{Name: "Pair3A - Pair3B", Dojo: "DojoC"},
			{Name: "Pair4A - Pair4B", Dojo: "DojoD"},
		}
	} else {
		players = []domain.Player{
			{Name: "Alice", Dojo: "DojoA"},
			{Name: "Bob", Dojo: "DojoB"},
			{Name: "Charlie", Dojo: "DojoC"},
			{Name: "Dave", Dojo: "DojoD"},
		}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch, "naginata 4-player bracket must have ThirdPlaceMatch")
	return bracket
}

// findPrintAreaLastRow reads the workbook's defined names and returns the
// last-row number of the _xlnm.Print_Area name scoped to the given sheet.
// Returns -1 if not found or unparseable.
func findPrintAreaLastRow(f *excelize.File, scope string) int {
	for _, dn := range f.GetDefinedName() {
		if dn.Name == "_xlnm.Print_Area" && dn.Scope == scope {
			return bctest.ParsePrintAreaLastRow(dn.RefersTo)
		}
	}
	return -1
}

// saveFixedTeamBracket turns compID (as testSetup saved it) into a fixed-order
// team knockout of two teams, Red and White, with one named member each, and
// stores a completed bracket of their one match: a tied numbered bout, and,
// when withRepresentativeBout, the representative bout (Red's Hiro against
// White's Sayaka) that a hantei decided for Red. The competition's Status is
// the knockout one because the competitor numbers (the "T1.1" labels) come
// from the draw only once it is.
func saveFixedTeamBracket(t *testing.T, store *state.Store, compID string, withRepresentativeBout bool) {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = state.CompFormatKnockout
	comp.Status = state.CompStatusKnockout
	comp.Kind = "team"
	comp.TeamMatchType = state.TeamMatchTypeFixed
	comp.TeamSize = 3
	comp.NumberPrefix = "T"
	require.NoError(t, store.SaveCompetition(comp))

	redID, whiteID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "DojoR"},
		{ID: whiteID, Name: "WhiteTeam", Dojo: "DojoW"},
	}))
	hiro, err := store.AddTeamMember(compID, redID, "Hiro")
	require.NoError(t, err)
	sayaka, err := store.AddTeamMember(compID, whiteID, "Sayaka")
	require.NoError(t, err)

	subs := []state.SubMatchResult{
		{Position: 1, SideA: "Hiro", SideAMemberID: hiro.ID, SideB: "Sayaka", SideBMemberID: sayaka.ID, Decision: "hikiwake"},
	}
	if withRepresentativeBout {
		subs = append(subs, state.SubMatchResult{
			Position: state.DaihyosenSubPosition, SideA: "RedTeam", SideB: "WhiteTeam",
			SideAMemberID: hiro.ID, SideBMemberID: sayaka.ID, Winner: "RedTeam",
			Decision: string(domain.DecisionDaihyosen), IpponsA: []string{domain.HanteiMark},
		})
	}
	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		DrawOrder: []string{redID, whiteID},
		Rounds: [][]state.BracketMatch{{{
			ID: "R1M0", MatchNumber: 1, DisplayRound: 1,
			SideA: "RedTeam", SideAID: redID, SideB: "WhiteTeam", SideBID: whiteID,
			Winner: "RedTeam", Status: state.MatchStatusCompleted, SubResults: subs,
		}}},
	}))
}

// readCourtBandLetters reads a court-banded sheet's shiaijo band headers back
// out of the rendered workbook, in column order, and asserts each band sits on
// the court grid and is non-empty. bctest.ReadCourtBands does the reading (the
// same reader serves internal/engine's stored-draw parity tests); the
// assertions stay here because the wording is this sheet's.
//
// An EMPTY band is a shiaijo header printed over nothing: a score sheet naming
// a court the competition never scheduled a bout on.
func readCourtBandLetters(t *testing.T, rows [][]string, sheet string) []string {
	t.Helper()
	require.NotEmptyf(t, rows, "%s must carry a shiaijo header row", sheet)

	found := bctest.ReadCourtBands(rows, helper.CourtsColumnsPerCourt)
	bands := make([]string, 0, len(found))
	for _, b := range found {
		require.Zerof(t, b.Col%helper.CourtsColumnsPerCourt,
			"%s: header %q sits at column %d, off the %d-column court grid",
			sheet, bctest.ShiaijoHeaderPrefix+b.Court, b.Col+1, helper.CourtsColumnsPerCourt)
		assert.Truef(t, b.Occupied,
			"%s prints an empty %s%s band: the sheet sends an operator to a shiaijo nothing is scheduled on",
			sheet, bctest.ShiaijoHeaderPrefix, b.Court)
		bands = append(bands, b.Court)
	}
	return bands
}

// resolvedCellText returns the text a cell displays, following its formula
// reference when it has one. The pool skeleton writes entrant cells as
// references into the draw's sheets, and excelize reads back no value for a
// formula it never evaluated, so the reference is chased to a literal. A
// numbered entrant's formula joins its number cell and its name cell; the LAST
// reference is the name.
func resolvedCellText(t *testing.T, f *excelize.File, sheet, ref string) string {
	t.Helper()
	refRE := regexp.MustCompile(`(?:'([^']+)'|([A-Za-z0-9_.]+))!\$?([A-Z]+)\$?([0-9]+)`)
	for hop := 0; hop < 5; hop++ {
		formula, err := f.GetCellFormula(sheet, ref)
		require.NoError(t, err)
		if formula == "" {
			v, err := f.GetCellValue(sheet, ref)
			require.NoError(t, err)
			return v
		}
		refs := refRE.FindAllStringSubmatch(formula, -1)
		require.NotEmptyf(t, refs, "formula %q in %s!%s references no cell", formula, sheet, ref)
		last := refs[len(refs)-1]
		sheet = last[1]
		if sheet == "" {
			sheet = last[2]
		}
		ref = last[3] + last[4]
	}
	t.Fatalf("%s!%s: formula chain longer than 5 hops", sheet, ref)
	return ""
}
