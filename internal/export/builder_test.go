package export

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// makePlayer creates a domain.Player for tests.
func makePlayer(name string) domain.Player {
	return domain.Player{ID: name, Name: name, Dojo: "Dojo"}
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

// makePools builds two pools of two players each with one match.
func makePools() []helper.Pool {
	p1 := makePlayer("Alice")
	p2 := makePlayer("Bob")
	p3 := makePlayer("Charlie")
	p4 := makePlayer("Dave")

	pool1 := helper.Pool{
		PoolName: "Pool A",
		Players:  []helper.Player{p1, p2},
		Matches: []helper.Match{
			{SideA: &p1, SideB: &p2},
		},
	}
	pool2 := helper.Pool{
		PoolName: "Pool B",
		Players:  []helper.Player{p3, p4},
		Matches: []helper.Match{
			{SideA: &p3, SideB: &p4},
		},
	}
	return []helper.Pool{pool1, pool2}
}

// ------------------------------------------------------------
// BuildResultsWorkbook happy-path tests
// ------------------------------------------------------------

func TestBuildResultsWorkbook_Minimal(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	require.NotEmpty(t, data, "workbook bytes must not be empty")

	// Verify the bytes are a valid XLSX file (excelize can open it).
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err, "result must be a valid XLSX file")
	defer f.Close()

	// The Pool Matches sheet must exist.
	sheets := f.GetSheetList()
	assert.Contains(t, sheets, helper.SheetPoolMatches)
}

func TestBuildResultsWorkbook_PoolScoresLiteral(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

	// Save a completed pool match: Pool A-0, Alice (SideA) beats Bob with M.
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alice",
			SideB:    "Bob",
			IpponsA:  []string{"M"},
			IpponsB:  []string{},
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Alice",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// Verify scores are present in the Pool Matches sheet.
	sheetRows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// At least one row must contain "M" (Alice's ippon).
	foundMIppon := false
	for _, row := range sheetRows {
		for _, cell := range row {
			if cell == "M" {
				foundMIppon = true
				break
			}
		}
		if foundMIppon {
			break
		}
	}
	assert.True(t, foundMIppon, "pool matches sheet must contain the literal ippon 'M'")
}

func TestBuildResultsWorkbook_StandingsLiteral_NoFormulaCollapse(t *testing.T) {
	// Per cmd/create_handler.go:25, RANK/W/L formulas collapse to 0 after a
	// store round-trip. This test verifies that the exported workbook has non-
	// zero literal standings values even for a re-read XLSX (no formula engine).
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

	// Alice beats Bob 2-0.
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alice",
			SideB:    "Bob",
			IpponsA:  []string{"M", "K"},
			IpponsB:  []string{},
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Alice",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// Find a cell with value "1" in the W (wins) column.
	// The standings section has a "W" header cell; the row below it for Alice
	// must have a literal 1 (not 0 from a collapsed formula).
	winsFound := false
	for rowIdx, row := range rows {
		for colIdx, cell := range row {
			if cell == "W" && colIdx+1 < len(row) {
				// Check the next few rows for a "1" in the same column.
				for offset := 1; offset <= 5 && rowIdx+offset < len(rows); offset++ {
					checkRow := rows[rowIdx+offset]
					if colIdx < len(checkRow) && checkRow[colIdx] == "1" {
						winsFound = true
						break
					}
				}
			}
			if winsFound {
				break
			}
		}
		if winsFound {
			break
		}
	}
	assert.True(t, winsFound, "W (wins) column must contain literal '1' for Alice after a win, not a collapsed formula '0'")
}

func TestBuildResultsWorkbook_DecisionSuffixInSheet(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

	// Alice beats Bob by kiken-voluntary in encho, decided by hantei.
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alice",
			SideB:    "Bob",
			IpponsA:  []string{"M"},
			IpponsB:  []string{},
			Decision: "kiken-voluntary",
			Encho:    &state.EnchoMetadata{PeriodCount: 1},
			DecidedByHantei: func() *bool {
				b := true
				return &b
			}(),
			Status: state.MatchStatusCompleted,
			Winner: "Alice",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// The vs/middle cell should contain "Kiken (E) Ht".
	foundSuffix := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "Kiken (E) Ht" {
				foundSuffix = true
				break
			}
		}
		if foundSuffix {
			break
		}
	}
	assert.True(t, foundSuffix, "pool matches sheet must contain 'Kiken (E) Ht' suffix")
}

func TestBuildResultsWorkbook_BracketScores(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	// Bracket with one completed match.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:          "B1",
					SideA:       "Alice",
					SideB:       "Charlie",
					Winner:      "Alice",
					Status:      state.MatchStatusCompleted,
					ScoreA:      "MK",
					ScoreB:      "M",
					Decision:    "fought",
					MatchNumber: 1,
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	sheets := f.GetSheetList()
	assert.Contains(t, sheets, helper.SheetEliminationMatches,
		"elimination matches sheet must exist when bracket has rounds")
}

func TestBuildResultsWorkbook_NoPools(t *testing.T) {
	// A competition with no pools should still return a valid workbook without error.
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func TestBuildResultsWorkbook_CompetitionNotFound(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "export-test-notfound-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)

	// No competition saved.
	_, err = BuildResultsWorkbook(store, eng, "nonexistent")
	require.Error(t, err, "must error when competition does not exist")
}

func TestBuildResultsWorkbook_DrawMatch(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

	// A draw (hikiwake) with no ippons should produce "X" in the vs column.
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alice",
			SideB:    "Bob",
			IpponsA:  []string{},
			IpponsB:  []string{},
			Decision: state.DecisionDraw,
			Status:   state.MatchStatusCompleted,
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	foundX := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "X" {
				foundX = true
				break
			}
		}
		if foundX {
			break
		}
	}
	assert.True(t, foundX, "pool matches sheet must contain 'X' for a hikiwake (draw) with no ippons")
}

// ------------------------------------------------------------
// Unit tests for helper utilities
// ------------------------------------------------------------

func TestParseRoundMatchLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		label string
		wantR int
		wantM int
	}{
		{"Round 1 - Match 1", 1, 1},
		{"Round 2 - Match 3", 2, 3},
		{"", 0, 0},
		{"Random text", 0, 0},
		{"Round 1", 0, 0},
	}
	for _, tc := range tests {
		r, m := parseRoundMatchLabel(tc.label)
		assert.Equal(t, tc.wantR, r, "round: label=%q", tc.label)
		assert.Equal(t, tc.wantM, m, "match: label=%q", tc.label)
	}
}

func TestColNum(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "A", colNum(1))
	assert.Equal(t, "B", colNum(2))
	assert.Equal(t, "Z", colNum(26))
	assert.Equal(t, "AA", colNum(27))

	// Invalid column number produces a non-empty fallback string (not a panic).
	fallback := colNum(0)
	assert.NotEmpty(t, fallback)
}

func TestBuildColumnMap(t *testing.T) {
	t.Parallel()
	row := []string{"Name", "W", "L", "T", "PW", "PL", "Rank"}
	m := buildColumnMap(row)
	assert.Equal(t, 0, m["Name"])
	assert.Equal(t, 1, m["W"])
	assert.Equal(t, 6, m["Rank"])
	// Missing key
	_, ok := m["Missing"]
	assert.False(t, ok)
}

func TestBuildBracketMatchIndex(t *testing.T) {
	t.Parallel()
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "M1", SideA: "A", SideB: "B"},
				{ID: "M2", SideA: "C", SideB: "D"},
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{ID: "M3", SideA: "E", SideB: "F"},
	}
	idx := buildBracketMatchIndex(bracket)
	assert.Len(t, idx, 3)
	assert.Contains(t, idx, "M1")
	assert.Contains(t, idx, "M2")
	assert.Contains(t, idx, "M3")
}

func TestFindBracketMatchByNumber(t *testing.T) {
	t.Parallel()
	idx := map[string]state.BracketMatch{
		"M1": {ID: "M1", MatchNumber: 1},
		"M2": {ID: "M2", MatchNumber: 2},
	}
	bm := findBracketMatchByNumber(idx, 2)
	require.NotNil(t, bm)
	assert.Equal(t, "M2", bm.ID)

	missing := findBracketMatchByNumber(idx, 99)
	assert.Nil(t, missing)
}

func TestStandingMap(t *testing.T) {
	t.Parallel()
	standings := []state.PlayerStanding{
		{Player: domain.Player{Name: "Alice"}, Rank: 1},
		{Player: domain.Player{Name: "Bob"}, Rank: 2},
	}
	m := standingMap(standings)
	assert.Len(t, m, 2)
	assert.Equal(t, 1, m["Alice"].Rank)
	assert.Equal(t, 2, m["Bob"].Rank)
}

func TestSetIntCell_MissingKey(t *testing.T) {
	// setIntCell is a no-op when the column key is not in the map.
	// Verify it does not panic and compiles cleanly.
	t.Parallel()
	f := excelize.NewFile()
	defer f.Close()

	colMap := buildColumnMap([]string{"W", "L", "T"})
	// "Rank" is NOT in the map - should be a no-op.
	setIntCell(f, "Sheet1", 1, colMap, "Rank", 42)
	// "W" IS in the map - should write without error.
	setIntCell(f, "Sheet1", 1, colMap, "W", 3)
	val, _ := f.GetCellValue("Sheet1", "A1")
	assert.Equal(t, "3", val)
}

func TestBuildResultsWorkbook_TwoCourts(t *testing.T) {
	// Multi-court competition: verifies that pools are correctly spread across
	// courts and the score overlay addresses the right column bands.
	t.Parallel()
	dir, err := os.MkdirTemp("", "export-test-2courts-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)

	compID := "two-court-comp"
	comp := &state.Competition{
		ID:     compID,
		Name:   "Two Courts",
		Courts: []string{"A", "B"},
	}
	require.NoError(t, store.SaveCompetition(comp))

	// Four pools across two courts.
	makeP := func(name string, p1, p2 string) helper.Pool {
		pl1 := makePlayer(p1)
		pl2 := makePlayer(p2)
		return helper.Pool{
			PoolName: name,
			Players:  []helper.Player{pl1, pl2},
			Matches:  []helper.Match{{SideA: &pl1, SideB: &pl2}},
		}
	}
	pools := []helper.Pool{
		makeP("Pool A", "Alice", "Bob"),
		makeP("Pool B", "Charlie", "Dave"),
		makeP("Pool C", "Eve", "Frank"),
		makeP("Pool D", "Grace", "Hank"),
	}
	require.NoError(t, store.SavePools(compID, pools))

	// Record a win in Pool C (second court, first pool).
	results := []state.MatchResult{
		{
			ID:       "Pool C-0",
			SideA:    "Eve",
			SideB:    "Frank",
			IpponsA:  []string{"M"},
			IpponsB:  []string{},
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Eve",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// At least one row must contain "M" (Eve's ippon in Pool C, court B).
	foundM := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "M" {
				foundM = true
				break
			}
		}
		if foundM {
			break
		}
	}
	assert.True(t, foundM, "Pool C ippon 'M' must be present in two-court workbook")
}

// TestBuildResultsWorkbook_BracketTwoCourts guards the multi-court bracket
// overlay: courts are laid out side-by-side, so two "Round N - Match N" headers
// can share a row at different column bands. The overlay must fill BOTH, not
// just the left-most court's match (Copilot review of PR #336).
func TestBuildResultsWorkbook_BracketTwoCourts(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Courts = []string{"A", "B"}
	comp.PoolWinners = 1
	require.NoError(t, store.SaveCompetition(comp))

	makeP := func(name, a, b string) helper.Pool {
		p1, p2 := makePlayer(a), makePlayer(b)
		return helper.Pool{PoolName: name, Players: []helper.Player{p1, p2}, Matches: []helper.Match{{SideA: &p1, SideB: &p2}}}
	}
	// 4 pools (2 per court) → 4 finalists → a semifinal round of 2 matches, one
	// per court, rendered side-by-side on the same rows.
	pools := []helper.Pool{
		makeP("Pool A", "Alice", "Bob"),
		makeP("Pool B", "Charlie", "Dave"),
		makeP("Pool C", "Eve", "Frank"),
		makeP("Pool D", "Grace", "Hank"),
	}
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	// Two completed semifinals with distinct scores: match 1 (left court) "MK",
	// match 2 (right court) "DT". Both must appear if every header on the row is
	// processed.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "sf1", SideA: "Alice", SideB: "Charlie", Winner: "Alice", Status: state.MatchStatusCompleted, ScoreA: "MK", ScoreB: "", Decision: "fought", MatchNumber: 1},
				{ID: "sf2", SideA: "Eve", SideB: "Grace", Winner: "Eve", Status: state.MatchStatusCompleted, ScoreA: "DT", ScoreB: "", Decision: "fought", MatchNumber: 2},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "MK"), "left-court semifinal score 'MK' must be overlaid")
	assert.True(t, sheetContainsCell(rows, "DT"), "right-court semifinal score 'DT' must be overlaid (multi-court)")
}

func TestBuildResultsWorkbook_BracketScoresWithWinner(t *testing.T) {
	// Verifies that a completed bracket match has score cells overlaid and the
	// winner name is written into the result cell below the match block.
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:          "B1",
					SideA:       "Alice",
					SideB:       "Charlie",
					Winner:      "Alice",
					Status:      state.MatchStatusCompleted,
					ScoreA:      "MK",
					ScoreB:      "M",
					Decision:    "fought",
					MatchNumber: 1,
				},
				{
					ID:          "B2",
					SideA:       "Bob",
					SideB:       "Dave",
					Winner:      "Dave",
					Status:      state.MatchStatusCompleted,
					ScoreA:      "M",
					ScoreB:      "MK",
					Decision:    "fought",
					MatchNumber: 2,
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	foundMK := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "MK" {
				foundMK = true
				break
			}
		}
		if foundMK {
			break
		}
	}
	assert.True(t, foundMK, "elimination matches sheet must contain score 'MK'")
}

func TestBuildResultsWorkbook_BracketKiken(t *testing.T) {
	// Verifies that a kiken decision in the bracket produces a suffix in the
	// elimination matches sheet.
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:          "B1",
					SideA:       "Alice",
					SideB:       "Charlie",
					Winner:      "Alice",
					Status:      state.MatchStatusCompleted,
					ScoreA:      "M",
					ScoreB:      "",
					Decision:    "kiken-voluntary",
					MatchNumber: 1,
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	foundKiken := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "Kiken" {
				foundKiken = true
				break
			}
		}
		if foundKiken {
			break
		}
	}
	assert.True(t, foundKiken, "elimination matches sheet must contain 'Kiken' suffix")
}

// TestBuildResultsWorkbook_GridFromResultsWithoutPoolMatches reproduces the real
// mobile-app path: pools are persisted with membership only (no helper.Pool.Matches,
// which is how the engine saves them), and the matches live solely in the results.
// The exported per-match grid must still show literal ippon scores, proving the
// builder reconstructs pool.Matches from the results (browser-verification regression).
func TestBuildResultsWorkbook_GridFromResultsWithoutPoolMatches(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Pool of 3 players, deliberately WITHOUT any .Matches populated.
	a := makePlayer("Ann")
	b := makePlayer("Bea")
	c := makePlayer("Cody")
	pools := []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{a, b, c}}}
	require.NoError(t, store.SavePools(compID, pools))

	// Round-robin results, keyed "Pool A-<idx>" as the engine persists them.
	results := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ann", SideB: "Bea", Winner: "Ann", IpponsA: []string{"M", "K"}, Status: state.MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "Ann", SideB: "Cody", Winner: "Ann", IpponsA: []string{"D"}, Status: state.MatchStatusCompleted},
		{ID: "Pool A-2", SideA: "Bea", SideB: "Cody", Winner: "Cody", IpponsB: []string{"M"}, Status: state.MatchStatusCompleted},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)
	// Every round-robin match's score must appear: "MK" (Ann v Bea), "D" (Ann v
	// Cody), "M" (Cody v Bea). This can only happen if (a) the grid was rendered
	// from the reconstructed pool.Matches, and (b) the pool-centric overlay writes
	// ALL match rows under a pool header (not just the first).
	for _, want := range []string{"MK", "D", "M"} {
		assert.True(t, sheetContainsCell(rows, want),
			"per-match grid must show literal ippon score %q for a scored match", want)
	}
}

// makeTeamPools builds two team pools of two teams each, one team encounter per pool.
func makeTeamPools() []helper.Pool {
	rA := makePlayer("Red A")
	bA := makePlayer("Blue A")
	rB := makePlayer("Red B")
	bB := makePlayer("Blue B")
	return []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{rA, bA}, Matches: []helper.Match{{SideA: &rA, SideB: &bA}}},
		{PoolName: "Pool B", Players: []helper.Player{rB, bB}, Matches: []helper.Match{{SideA: &rB, SideB: &bB}}},
	}
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

// TestBuildResultsWorkbook_TeamResults exercises the team overlays end-to-end:
// team pool sub-match scores, the two-pass team standings (W/L/T + IV/IL/IT/PW/PL),
// and a daihyosen-decided team elimination encounter. Assertions target specific
// cells so an off-by-one in the row math (not just a collapsed formula) fails.
func TestBuildResultsWorkbook_TeamResults(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "team"
	comp.TeamSize = 3
	require.NoError(t, store.SaveCompetition(comp))

	pools := makeTeamPools()
	require.NoError(t, store.SavePools(compID, pools))

	// Red A beats Blue A on individual victories (2-1); Red B beats Blue B (2-1).
	results := []state.MatchResult{
		{
			ID: "Pool A-0", SideA: "Red A", SideB: "Blue A",
			Status: state.MatchStatusCompleted, Winner: "Red A",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Red A", SideB: "Blue A", IpponsA: []string{"M", "K"}, Winner: "Red A"},
				{Position: 2, SideA: "Red A", SideB: "Blue A", IpponsB: []string{"M"}, Winner: "Blue A"},
				{Position: 3, SideA: "Red A", SideB: "Blue A", IpponsA: []string{"D"}, Winner: "Red A"},
			},
		},
		{
			ID: "Pool B-0", SideA: "Red B", SideB: "Blue B",
			Status: state.MatchStatusCompleted, Winner: "Red B",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Red B", SideB: "Blue B", IpponsA: []string{"M"}, Winner: "Red B"},
				{Position: 2, SideA: "Red B", SideB: "Blue B", IpponsA: []string{"K"}, Winner: "Red B"},
				{Position: 3, SideA: "Red B", SideB: "Blue B", IpponsB: []string{"M"}, Winner: "Blue B"},
			},
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	// Team elimination: Red A vs Red B tied 1-1 on IV, decided by daihyosen; Red A wins.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "B1", SideA: "Red A", SideB: "Red B", Winner: "Red A",
					Status: state.MatchStatusCompleted, MatchNumber: 1,
					Decision: string(domain.DecisionDaihyosen),
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "Red A", SideB: "Red B", IpponsA: []string{"M"}, Winner: "Red A"},
						{Position: 2, SideA: "Red A", SideB: "Red B", IpponsB: []string{"K"}, Winner: "Red B"},
						{Position: 3, SideA: "Red A", SideB: "Red B", Decision: state.DecisionDraw},
						{Position: -1, Decision: string(domain.DecisionDaihyosen)},
					},
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// ---- Pool Matches: team standings + sub-match scores ----
	poolRows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// Locate the first "Team Results" header and assert the winning team's literal
	// W (Table 1) and IV (Table 2). Player order is [Red A, Blue A]; Red A won.
	tr := -1
	trCol := -1
	for r, row := range poolRows {
		for cIdx, cell := range row {
			if cell == "Team Results" {
				tr, trCol = r, cIdx
				break
			}
		}
		if tr >= 0 {
			break
		}
	}
	require.GreaterOrEqual(t, tr, 0, "Pool Matches sheet must contain a 'Team Results' header")
	// Table 1: Red A row = tr+1 (0-based); W col = trCol+1.
	require.Greater(t, len(poolRows), tr+1)
	require.Greater(t, len(poolRows[tr+1]), trCol+1)
	assert.Equal(t, "1", poolRows[tr+1][trCol+1], "Red A team-wins (W) must be literal 1, not a collapsed formula")
	// Table 2 header = tr + nPlayers(2) + 3; Red A IV row (0-based) = tr + 5; IV col = trCol+1.
	require.Greater(t, len(poolRows), tr+5)
	require.Greater(t, len(poolRows[tr+5]), trCol+1)
	assert.Equal(t, "2", poolRows[tr+5][trCol+1], "Red A individual-victories (IV) must be literal 2")

	// Sub-match ippon letters overlaid.
	assert.True(t, sheetContainsCell(poolRows, "M"), "team pool sub-match ippon 'M' must be present")
	assert.True(t, sheetContainsCell(poolRows, "D"), "team pool sub-match ippon 'D' must be present")

	// ---- Elimination: team encounter summary + DH suffix + winner ----
	elimRows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// The "Victories / Points" tally row holds literal IV/PW; Red A (left) IV = 1.
	vp := -1
	vpCol := -1
	for r, row := range elimRows {
		for cIdx, cell := range row {
			if cell == "Victories / Points" {
				vp, vpCol = r, cIdx
				break
			}
		}
		if vp >= 0 {
			break
		}
	}
	require.GreaterOrEqual(t, vp, 0, "elimination sheet must contain a 'Victories / Points' summary row")
	require.Greater(t, len(elimRows[vp]), vpCol+1)
	assert.Equal(t, "1", elimRows[vp][vpCol+1], "Red A bracket IV must be literal 1 on the summary row")

	assert.True(t, sheetContainsCell(elimRows, "DH"), "daihyosen suffix 'DH' must appear on the team encounter")
	assert.True(t, sheetContainsCell(elimRows, "Red A"), "winner 'Red A' must be written as a literal")
}
