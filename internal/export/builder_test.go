package export

import (
	"bytes"
	"fmt"
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

// setCompFormat loads the competition, sets its Format, and saves it. Bracket
// tests that build pools + a knockout must use a knockout-enabled format (Mixed),
// since the export only emits Elimination/Tree sheets when comp.IsPlayoffEnabled().
func setCompFormat(t *testing.T, store *state.Store, compID, format string) {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = format
	require.NoError(t, store.SaveCompetition(comp))
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

// TestBuildResultsWorkbook_DrawWithSuffixKeepsMarker is the regression test for
// the draw-marker overwrite bug: a hikiwake that also went to encho (or was
// hantei-decided) must export the COMBINED "X (E)" / "X Ht" in the vs column, not
// just the suffix. Previously the code wrote "X" then overwrote it with the
// suffix, silently dropping the draw indicator from the archived workbook.
func TestBuildResultsWorkbook_DrawWithSuffixKeepsMarker(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

	// A scoreless draw that went to overtime and stayed level.
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alice",
			SideB:    "Bob",
			IpponsA:  []string{},
			IpponsB:  []string{},
			Decision: state.DecisionDraw,
			Encho:    &state.EnchoMetadata{PeriodCount: 1},
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

	assert.True(t, sheetContainsCell(rows, "X (E)"),
		"a hikiwake-in-encho must export the combined 'X (E)' marker, not just '(E)'")
	assert.False(t, sheetContainsCell(rows, "(E)"),
		"the bare '(E)' cell must not appear: it means the draw marker was dropped")
}

// ------------------------------------------------------------
// Unit tests for helper utilities
// ------------------------------------------------------------

func TestParseRoundMatchLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		label string
		wantM int
	}{
		{"Round 1 - Match 1", 1},
		{"Round 2 - Match 3", 3},
		{"", 0},
		{"Random text", 0},
		{"Round 1", 0},
	}
	for _, tc := range tests {
		m := parseRoundMatchLabel(tc.label)
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

func TestBuildCourtColumnMap(t *testing.T) {
	t.Parallel()
	// startColIdx 0 makes this equivalent to a whole-row map for a single court.
	row := []string{"Name", "W", "L", "T", "PW", "PL", "Rank"}
	m := buildCourtColumnMap(row, 0)
	assert.Equal(t, 0, m["Name"])
	assert.Equal(t, 1, m["W"])
	assert.Equal(t, 6, m["Rank"])
	// Missing key
	_, ok := m["Missing"]
	assert.False(t, ok)

	// Band scoping: a second court's headers past the 8-column band are excluded,
	// and the returned indices are ABSOLUTE. Court B starts at index 8.
	twoCourt := []string{"Name", "W", "L", "", "", "", "", "", "Name", "W", "L"}
	courtB := buildCourtColumnMap(twoCourt, helper.CourtsColumnsPerCourt)
	assert.Equal(t, 9, courtB["W"], "court B's W must resolve to its own absolute column, not court A's")
	assert.Equal(t, 8, courtB["Name"])
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

	colMap := buildCourtColumnMap([]string{"W", "L", "T"}, 0)
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

// wColInBand returns the 0-based column index of the "W" standings header found
// within the [bandStart, bandEnd) column range, or -1 if absent.
func wColInBand(rows [][]string, bandStart, bandEnd int) int {
	for _, row := range rows {
		for c := bandStart; c < bandEnd && c < len(row); c++ {
			if row[c] == "W" {
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

// TestBuildResultsWorkbook_LeagueNoPhantomBracket is the regression test for the
// phantom-bracket bug: GenerateFinals returns placeholder "Pool A-1st" finalist
// labels even for a League (which has no knockout phase), so without the
// IsPlayoffEnabled() gate the export emitted an Elimination Matches sheet full of
// "Round N - Match N" headers and finalist placeholders, plus "Tree 1" pages,
// implying a knockout that does not exist.
func TestBuildResultsWorkbook_LeagueNoPhantomBracket(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatLeague
	comp.PoolWinners = 1
	comp.RoundRobin = true
	require.NoError(t, store.SaveCompetition(comp))

	pools := makePools() // Pool A, Pool B
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// The Elimination Matches sheet still EXISTS (NewFileFromScratch creates it) but
	// must be empty: no round headers and no "Pool A-1st" finalist placeholders.
	elim, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	for _, row := range elim {
		for _, cell := range row {
			assert.NotContains(t, cell, "Round ", "league export must not render a phantom bracket header")
			assert.NotContains(t, cell, "-1st", "league export must not render phantom finalist labels")
		}
	}
	// No Tree pages either (the whole knockout block is skipped for league).
	assert.NotContains(t, f.GetSheetList(), "Tree 1",
		"league export must not create Tree bracket pages")
}

// TestBuildResultsWorkbook_MultiCourtStandingsColumns is the regression test for
// the multi-court standings column-map bug: overlayPoolStandings built its
// header map from the WHOLE row, and buildColumnMap keeps only the first
// occurrence of each label. Pool Matches repeats the W/L/T/PW/PL/Rank headers
// once per court band, so a win in a court-B pool used to be written into court
// A's W column. Only Pool C (court B) is scored; court A's W column must stay
// clean while court B's shows the win.
func TestBuildResultsWorkbook_MultiCourtStandingsColumns(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "export-test-mcstand-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)

	compID := "mc-standings"
	comp := &state.Competition{ID: compID, Name: "MC Standings", Courts: []string{"A", "B"}}
	require.NoError(t, store.SaveCompetition(comp))

	makeP := func(name, a, b string) helper.Pool {
		pl1, pl2 := makePlayer(a), makePlayer(b)
		return helper.Pool{PoolName: name, Players: []helper.Player{pl1, pl2}, Matches: []helper.Match{{SideA: &pl1, SideB: &pl2}}}
	}
	// Pools 0,1 -> court A; pools 2,3 -> court B (contiguous assignment).
	pools := []helper.Pool{
		makeP("Pool A", "Alice", "Bob"),
		makeP("Pool B", "Charlie", "Dave"),
		makeP("Pool C", "Eve", "Frank"),
		makeP("Pool D", "Grace", "Hank"),
	}
	require.NoError(t, store.SavePools(compID, pools))

	// Score ONLY Pool C (court B): Eve wins, so Eve's standings W = 1.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool C-0", SideA: "Eve", SideB: "Frank", IpponsA: []string{"M"}, Decision: "fought", Status: state.MatchStatusCompleted, Winner: "Eve"},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	courtAW := wColInBand(rows, 0, helper.CourtsColumnsPerCourt)
	courtBW := wColInBand(rows, helper.CourtsColumnsPerCourt, 2*helper.CourtsColumnsPerCourt)
	require.GreaterOrEqual(t, courtAW, 0, "court A W header must exist")
	require.GreaterOrEqual(t, courtBW, 0, "court B W header must exist")

	assert.True(t, columnContains(rows, courtBW, "1"),
		"court B's W column must carry Eve's win (=1)")
	assert.False(t, columnContains(rows, courtAW, "1"),
		"court A pools are unscored: a '1' in court A's W column means court B's win leaked into court A (the multi-court colMap bug)")
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
	comp.Format = state.CompFormatMixed
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
	setCompFormat(t, store, compID, state.CompFormatMixed)

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
	setCompFormat(t, store, compID, state.CompFormatMixed)

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

// TestBuildResultsWorkbook_EndToEndEngineScored drives the REAL path end to end:
// StartCompetition generates the pool + matches (so pool.Matches is absent on the
// reloaded helper.Pool, exactly as in the mobile-app), then each match is scored
// through eng.RecordMatchResult (not hand-saved state). The export must then show
// literal per-match ippon scores AND literal standings. This is the automated
// analog of the browser UAT that first exposed the empty-grid bug.
func TestBuildResultsWorkbook_EndToEndEngineScored(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatLeague // single round-robin pool
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 1
	comp.RoundRobin = true
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Ann", Dojo: "D"}, {Name: "Bea", Dojo: "D"}, {Name: "Cody", Dojo: "D"},
	}))

	require.NoError(t, eng.StartCompetition(compID))

	generated, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, generated, "StartCompetition must generate pool matches")

	// Score every generated match: SideA wins 2-0 (M, K) through the engine.
	for _, m := range generated {
		res := m
		res.IpponsA = []string{"M", "K"}
		res.IpponsB = nil
		res.Winner = m.SideA
		res.Decision = "fought"
		res.Status = state.MatchStatusCompleted
		require.NoError(t, eng.RecordMatchResult(compID, m.ID, &res))
	}

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// Grid scores overlaid onto the engine-generated (no .Matches) pool.
	assert.True(t, sheetContainsCell(rows, "MK"),
		"engine-scored pool grid must show the literal ippon score 'MK'")
	// Standings overlaid: the round-robin winner has 2 wins (a literal, not a
	// collapsed formula 0).
	assert.True(t, columnHasValueUnderHeader(rows, "W", "2"),
		"standings W column must contain literal '2' for the pool winner")
	assertNoBrokenFormulas(t, f, helper.SheetPoolMatches)
}

// TestBuildResultsWorkbook_PlayoffsBracket covers the pure-knockout format, which
// has NO pools: the elimination skeleton must be seeded from the participants (as
// engine.generatePlayoffs does) so the stored bracket renders and its scores overlay.
// TestPlayoffLeavesFromBracket unit-tests the leaf-order reconstruction: each
// round-1 match contributes SideA then SideB in order, byes included as "".
func TestPlayoffLeavesFromBracket(t *testing.T) {
	t.Parallel()
	assert.Nil(t, playoffLeavesFromBracket(nil))
	assert.Nil(t, playoffLeavesFromBracket(&state.Bracket{}))

	br := &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{SideA: "Alice", SideB: "Dave"},
			{SideA: "Carol", SideB: ""}, // bye
		},
		{{SideA: "Winner of r1-m0", SideB: "Winner of r1-m1"}},
	}}
	assert.Equal(t, []string{"Alice", "Dave", "Carol", ""}, playoffLeavesFromBracket(br))
}

// TestBuildResultsWorkbook_PlayoffsNonPow2TopologyMatchesBracket is the regression
// test proving the export skeleton is derived from the FROZEN bracket, not
// recomputed from participants. For a non-power-of-two roster the engine pads the
// bracket to the next pow2 with byes (buildBracketFromLeaves), so a 6-entry
// playoffs is stored as an 8-leaf (7-node) tree. The previous implementation
// recomputed an UNPADDED 6-leaf (5-node) tree at export time, a different topology
// and match numbering than the stored bracket, which misplaces overlaid scores.
// The export's rendered match-block count must equal the stored bracket's node
// count (7), not the recomputed 5.
func TestBuildResultsWorkbook_PlayoffsNonPow2TopologyMatchesBracket(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatPlayoffs
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	players := make([]domain.Player, 6) // non-power-of-two -> 2 byes when padded to 8
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "D"}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	for ri := range br.Rounds {
		for mi := range br.Rounds[ri] {
			m := &br.Rounds[ri][mi]
			if m.SideA != "" && m.SideB != "" {
				m.Winner, m.Status, m.ScoreA, m.Decision = m.SideA, state.MatchStatusCompleted, "MK", "fought"
			}
		}
	}
	require.NoError(t, store.SaveBracket(compID, br))

	// Total match nodes in the stored (pow2-padded) bracket.
	bracketNodes := 0
	for _, round := range br.Rounds {
		bracketNodes += len(round)
	}

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	headers := 0
	for _, row := range rows {
		for _, cell := range row {
			if parseRoundMatchLabel(cell) > 0 {
				headers++
			}
		}
	}
	assert.Equal(t, bracketNodes, headers,
		"export match-block count must equal the stored bracket's node count (topology derived from the frozen bracket, not recomputed unpadded)")
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
}

func TestBuildResultsWorkbook_PlayoffsBracket(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatPlayoffs
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "D"}, {Name: "Bob", Dojo: "D"}, {Name: "Carol", Dojo: "D"}, {Name: "Dave", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	// Complete the generated bracket's real matches.
	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, br)
	for ri := range br.Rounds {
		for mi := range br.Rounds[ri] {
			m := &br.Rounds[ri][mi]
			if m.SideA != "" && m.SideB != "" {
				m.Winner = m.SideA
				m.Status = state.MatchStatusCompleted
				m.ScoreA = "MK"
				m.Decision = "fought"
			}
		}
	}
	require.NoError(t, store.SaveBracket(compID, br))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "MK"),
		"playoffs (no pools) must render the bracket with its literal score 'MK'")
	assert.True(t, sheetContainsCell(rows, "Alice"),
		"playoffs bracket must render literal entrant names (no pool data to reference)")
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
}

// TestBuildResultsWorkbook_PlayoffsCellRefLikeNames is a regression test for
// mp-uagg: internal/helper.printSingleEliminationMatch used to decide leaf- vs
// match-feeder nodes by asking whether Node.LeafVal parsed as an Excel cell
// reference (excelize.SplitCellName). A no-pools playoffs bracket renders raw
// participant names as leaves (playoffFinalsFromParticipants), so a competitor
// named like a cell coordinate ("P1" = column P row 1, "M3", "A4") was
// misclassified as a match-feeder, producing a broken CONCATENATE(...,”!)
// formula. The fix checks the structural Node.LeafNode flag instead.
func TestBuildResultsWorkbook_PlayoffsCellRefLikeNames(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatPlayoffs
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "P1", Dojo: "D"}, {Name: "M3", Dojo: "D"}, {Name: "A4", Dojo: "D"}, {Name: "Z9", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, br)
	for ri := range br.Rounds {
		for mi := range br.Rounds[ri] {
			m := &br.Rounds[ri][mi]
			if m.SideA != "" && m.SideB != "" {
				m.Winner = m.SideA
				m.Status = state.MatchStatusCompleted
				m.ScoreA = "MK"
				m.Decision = "fought"
			}
		}
	}
	require.NoError(t, store.SaveBracket(compID, br))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "P1"),
		"cell-ref-like entrant name 'P1' must render literally, not as a match-feeder reference")
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
}

// TestBuildResultsWorkbook_SwissUnsupported covers the Swiss format, which has no
// static bracket: the builder must fail with the documented sentinel rather than
// emit an empty workbook.
func TestBuildResultsWorkbook_SwissUnsupported(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = state.CompFormatSwiss
	comp.SwissRounds = 2
	require.NoError(t, store.SaveCompetition(comp))

	_, err = BuildResultsWorkbook(store, eng, compID)
	assert.ErrorIs(t, err, ErrSwissExportUnsupported)
}

// TestBuildResultsWorkbook_MixedEndToEnd covers the pools+knockout format through
// the real engine: two pools are generated and scored, then the export must render
// the pool grid with literal scores.
func TestBuildResultsWorkbook_MixedEndToEnd(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatMixed
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 1
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "M1", Dojo: "D"}, {Name: "M2", Dojo: "D"}, {Name: "M3", Dojo: "D"},
		{Name: "M4", Dojo: "D"}, {Name: "M5", Dojo: "D"}, {Name: "M6", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	for _, m := range matches {
		res := m
		res.IpponsA = []string{"M", "K"}
		res.IpponsB = nil
		res.Winner = m.SideA
		res.Decision = "fought"
		res.Status = state.MatchStatusCompleted
		require.NoError(t, eng.RecordMatchResult(compID, m.ID, &res))
	}

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "MK"),
		"mixed pool grid must show the literal ippon score 'MK'")
	assertNoBrokenFormulas(t, f, helper.SheetPoolMatches)
	// Mixed advances pool winners into a bracket: GenerateFinals renders finalist
	// slots as "Pool-Ordinal" (e.g. "Pool A-1st"), so the Elimination Matches
	// sheet is populated too and must be equally free of broken formulas.
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
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
	for r := range rows {
		for c := 0; c < 24; c++ {
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

// TestBuildResultsWorkbook_IncompleteAllFormats exports every format immediately
// after StartCompetition, with ZERO matches scored. A real tournament exports
// mid-run, so an incomplete competition must still yield a valid workbook (or, for
// Swiss, the documented 422) and never crash.
func TestBuildResultsWorkbook_IncompleteAllFormats(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		format       string
		players      int
		poolSize     int
		poolWinners  int
		wantSwissErr bool
	}{
		{"league incomplete", state.CompFormatLeague, 4, 4, 1, false},
		{"mixed incomplete", state.CompFormatMixed, 6, 3, 1, false},
		{"playoffs incomplete", state.CompFormatPlayoffs, 4, 0, 0, false},
		{"swiss incomplete", state.CompFormatSwiss, 4, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)

			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			comp.Kind = "individual"
			comp.Format = tc.format
			comp.Status = "setup"
			if tc.poolSize > 0 {
				comp.PoolSize = tc.poolSize
				comp.PoolSizeMode = "min"
			}
			if tc.poolWinners > 0 {
				comp.PoolWinners = tc.poolWinners
			}
			if tc.format == state.CompFormatLeague {
				comp.RoundRobin = true
			}
			if tc.format == state.CompFormatSwiss {
				comp.SwissRounds = 2
			}
			require.NoError(t, store.SaveCompetition(comp))

			// A participant named like an Excel cell reference (e.g. "P1" = column P
			// row 1) used to trip the shared elimination renderer's leaf-detection
			// (mp-uagg, fixed alongside this test suite): it structurally checks
			// Node.LeafNode now instead of parsing LeafVal as a cell name, so mixing
			// such names in here doubles as regression coverage across every format.
			names := []string{"Alice", "P1", "Carol", "M3", "Erin", "A4", "Grace", "Z9"}
			players := make([]domain.Player, tc.players)
			for i := range players {
				players[i] = domain.Player{Name: names[i], Dojo: "D"}
			}
			require.NoError(t, store.SaveParticipants(compID, players))
			require.NoError(t, eng.StartCompetition(compID))

			// Deliberately score nothing: export the incomplete competition.
			data, err := BuildResultsWorkbook(store, eng, compID)
			if tc.wantSwissErr {
				assert.ErrorIs(t, err, ErrSwissExportUnsupported)
				return
			}
			require.NoError(t, err, "incomplete %s export must not error", tc.format)
			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err, "incomplete %s export must be a valid xlsx", tc.format)
			defer f.Close()
			assert.Contains(t, f.GetSheetList(), helper.SheetPoolMatches)
			// No leftover formula may collapse to an Excel error in a 0-scored export.
			assertNoBrokenFormulas(t, f, helper.SheetPoolMatches)
			assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
		})
	}
}

// TestBuildResultsWorkbook_PartialPoolScoring scores only SOME pool matches. The
// export must show the scored ones and leave the rest blank, without error.
func TestBuildResultsWorkbook_PartialPoolScoring(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatLeague
	comp.PoolSize = 4
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 1
	comp.RoundRobin = true
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A", Dojo: "D"}, {Name: "B", Dojo: "D"}, {Name: "C", Dojo: "D"}, {Name: "D", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Greater(t, len(matches), 1, "round-robin of 4 has multiple matches")

	// Score ONLY the first match; leave the rest scheduled.
	first := matches[0]
	res := first
	res.IpponsA = []string{"M", "K"}
	res.IpponsB = nil
	res.Winner = first.SideA
	res.Decision = "fought"
	res.Status = state.MatchStatusCompleted
	require.NoError(t, eng.RecordMatchResult(compID, first.ID, &res))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "MK"),
		"the one scored match must appear even though the pool is incomplete")
	// Partial exports must not leave any broken formulas either.
	assertNoBrokenFormulas(t, f, helper.SheetPoolMatches)
}

// TestBuildResultsWorkbook_PlayoffsPartialBracket scores only the first bracket
// round (e.g. semifinals) and leaves later rounds unplayed. The export must render
// the played scores and leave the rest blank, without error.
func TestBuildResultsWorkbook_PlayoffsPartialBracket(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatPlayoffs
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "D"}, {Name: "Bob", Dojo: "D"}, {Name: "Carol", Dojo: "D"}, {Name: "Dave", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, br)
	require.GreaterOrEqual(t, len(br.Rounds), 2, "4 entrants → semifinal + final")

	// Score ONLY the first round (semifinals); leave the final unplayed.
	for mi := range br.Rounds[0] {
		m := &br.Rounds[0][mi]
		if m.SideA != "" && m.SideB != "" {
			m.Winner = m.SideA
			m.Status = state.MatchStatusCompleted
			m.ScoreA = "MK"
			m.Decision = "fought"
		}
	}
	require.NoError(t, store.SaveBracket(compID, br))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "MK"),
		"the played semifinal score must render even with the final unplayed")
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
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
	comp.Format = state.CompFormatMixed
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

// formulaCellCount returns how many cells in the sheet carry a formula. Used to
// distinguish a populated tree page from a blank one.
func formulaCellCount(t *testing.T, f *excelize.File, sheet string) int {
	t.Helper()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	count := 0
	for r := range rows {
		for c := 0; c < 24; c++ {
			col, _ := excelize.ColumnNumberToName(c + 1)
			fm, _ := f.GetCellFormula(sheet, fmt.Sprintf("%s%d", col, r+1))
			if fm != "" {
				count++
			}
		}
	}
	return count
}

// TestBuildResultsWorkbook_MultiPageTreePopulated is the regression test for the
// blank-extra-tree-sheet bug: a bracket with more than MaxPlayersPerTree (16)
// finalists spans multiple Tree pages. The previous implementation created
// "Tree 2"+ as empty sheets and only rendered "Tree 1"; the fix copies the
// styled template into every page and renders each subtree's leaves. A 32-entry
// playoffs bracket needs 2 pages, so both must be populated and the bare "Tree"
// template must be gone.
func TestBuildResultsWorkbook_MultiPageTreePopulated(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatPlayoffs
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))

	players := make([]domain.Player, 32)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("Player%02d", i+1), Dojo: "D"}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	sheets := f.GetSheetList()
	assert.Contains(t, sheets, "Tree 1", "first tree page must exist")
	assert.Contains(t, sheets, "Tree 2", "second tree page must exist for a >16-entry bracket")
	assert.NotContains(t, sheets, helper.SheetTree,
		"the bare 'Tree' template must be consumed and deleted, not left alongside the pages")

	assert.Greater(t, formulaCellCount(t, f, "Tree 2"), 1,
		"second tree page must be populated (styled copy + rendered leaves), not blank")
}

// TestWriteTeamSubMatchScores_OutOfRangePositionSkipped verifies the upper-bound
// guard: a corrupted sub.Position beyond teamSize must be skipped rather than
// writing ippon letters into the row of the NEXT encounter's block.
func TestWriteTeamSubMatchScores_OutOfRangePositionSkipped(t *testing.T) {
	t.Parallel()
	f := excelize.NewFile()
	defer f.Close()
	sheet := helper.SheetPoolMatches
	f.NewSheet(sheet)

	const teamSize = 3
	const courtStartCol = 1 // column A band
	const subStartRow = 5
	subs := []state.SubMatchResult{
		{Position: 1, IpponsA: []string{"M"}},
		{Position: 3, IpponsA: []string{"K"}},
		{Position: 9, IpponsA: []string{"D"}}, // corrupted: > teamSize
	}
	writeTeamSubMatchScores(f, sheet, courtStartCol, subStartRow, subs, teamSize, false)

	// Position 1 -> row 5, Position 3 -> row 7 (both written).
	v1, _ := f.GetCellValue(sheet, "B5")
	v3, _ := f.GetCellValue(sheet, "B7")
	assert.Equal(t, "M", v1)
	assert.Equal(t, "K", v3)
	// Position 9 would land at row subStartRow+8 = 13; it must NOT be written.
	v9, _ := f.GetCellValue(sheet, "B13")
	assert.Empty(t, v9, "an out-of-range sub.Position must be skipped, not written into a neighbouring block")
}

// TestAttachPoolMatches_PrefersSideIDs is the regression test for the same-name
// participant bug in attachPoolMatches: two competitors can share a name but sit
// in different dojos (allowed), so a name-only side lookup attaches the wrong
// Player. The fix resolves each side by its authoritative SideAID/SideBID UUID
// first, falling back to the name only when no ID is present.
func TestAttachPoolMatches_PrefersSideIDs(t *testing.T) {
	t.Parallel()

	pools := []helper.Pool{{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "id-1", Name: "Sam", Dojo: "North"},
			{ID: "id-2", Name: "Sam", Dojo: "South"},
		},
	}}
	results := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Sam", SideAID: "id-1", SideB: "Sam", SideBID: "id-2"},
	}

	attachPoolMatches(pools, results)

	require.Len(t, pools[0].Matches, 1)
	m := pools[0].Matches[0]
	require.NotNil(t, m.SideA)
	require.NotNil(t, m.SideB)
	// Resolved by ID, so the two same-name sides map to DISTINCT players with the
	// correct dojos, not both to whichever "Sam" was added last.
	assert.Equal(t, "North", m.SideA.Dojo, "SideAID id-1 must resolve to the North Sam")
	assert.Equal(t, "South", m.SideB.Dojo, "SideBID id-2 must resolve to the South Sam")
	assert.NotSame(t, m.SideA, m.SideB, "same-name sides must resolve to distinct players")
}

// TestAttachPoolMatches_FallsBackToName verifies the resolver still works when
// legacy results carry no side UUIDs (pre-UUID data): resolution falls back to
// the display name.
func TestAttachPoolMatches_FallsBackToName(t *testing.T) {
	t.Parallel()

	pools := []helper.Pool{{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "id-1", Name: "Ann", Dojo: "North"},
			{ID: "id-2", Name: "Bea", Dojo: "South"},
		},
	}}
	results := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ann", SideB: "Bea"}, // no SideAID/SideBID
	}

	attachPoolMatches(pools, results)

	require.Len(t, pools[0].Matches, 1)
	m := pools[0].Matches[0]
	require.NotNil(t, m.SideA)
	require.NotNil(t, m.SideB)
	assert.Equal(t, "Ann", m.SideA.Name)
	assert.Equal(t, "Bea", m.SideB.Name)
}
