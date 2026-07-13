package export

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"strings"
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
// since the export only emits Elimination/Tree sheets when comp.IsPlayoffEnabled().
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
				{ID: "M1", MatchNumber: 1, SideA: "A", SideB: "B"},
				{ID: "M2", MatchNumber: 2, SideA: "C", SideB: "D"},
				{ID: "bye", MatchNumber: 0, SideA: "X", SideB: ""}, // unnumbered: excluded
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{ID: "M3", MatchNumber: 3, SideA: "E", SideB: "F"},
	}
	idx := buildBracketMatchIndex(bracket)
	assert.Len(t, idx, 3, "the MatchNumber-0 bye must be excluded")
	assert.Contains(t, idx, 1)
	assert.Contains(t, idx, 2)
	assert.Contains(t, idx, 3)
	assert.NotContains(t, idx, 0)
}

func TestStandingMap(t *testing.T) {
	t.Parallel()
	// Legacy state without UUIDs: keyed by name.
	standings := []state.PlayerStanding{
		{Player: domain.Player{Name: "Alice"}, Rank: 1},
		{Player: domain.Player{Name: "Bob"}, Rank: 2},
	}
	m := standingMap(standings)
	assert.Len(t, m, 2)
	assert.Equal(t, 1, m["Alice"].Rank)
	assert.Equal(t, 2, m["Bob"].Rank)
}

// TestStandingMap_SameNameKeyedByID is the regression test for standingMap
// collapsing same-name participants: two "Sam"s in different dojos must remain
// distinct standings entries because the map keys by participant UUID.
func TestStandingMap_SameNameKeyedByID(t *testing.T) {
	t.Parallel()
	standings := []state.PlayerStanding{
		{Player: domain.Player{ID: "id-1", Name: "Sam", Dojo: "North"}, Rank: 1},
		{Player: domain.Player{ID: "id-2", Name: "Sam", Dojo: "South"}, Rank: 4},
	}
	m := standingMap(standings)
	assert.Len(t, m, 2, "same-name players with distinct IDs must not collapse")
	assert.Equal(t, 1, m["id-1"].Rank)
	assert.Equal(t, 4, m["id-2"].Rank)
	// standingKey prefers ID, falls back to name.
	assert.Equal(t, "id-1", standingKey(helper.Player{ID: "id-1", Name: "Sam"}))
	assert.Equal(t, "Legacy", standingKey(helper.Player{Name: "Legacy"}))
}

// TestAttachPoolMatches_SkipsUnresolvableSide is the regression test for the nil
// dereference: a pool match whose side resolves to no pool member (e.g. a
// participant removed after the match was recorded) must be SKIPPED, not stored as
// a nil *Player that PrintPoolMatches would dereference and panic on.
func TestAttachPoolMatches_SkipsUnresolvableSide(t *testing.T) {
	t.Parallel()
	pools := []helper.Pool{{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "id-1", Name: "Ann", Dojo: "North"},
			{ID: "id-2", Name: "Bea", Dojo: "South"},
		},
	}}
	results := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ann", SideAID: "id-1", SideB: "Bea", SideBID: "id-2"},      // resolvable
		{ID: "Pool A-1", SideA: "Ann", SideAID: "id-1", SideB: "Ghost", SideBID: "id-gone"}, // SideB gone
	}

	ordinals := attachPoolMatches(pools, results)

	require.Len(t, pools[0].Matches, 1, "the match with an unresolvable side must be skipped")
	require.NotNil(t, pools[0].Matches[0].SideA)
	require.NotNil(t, pools[0].Matches[0].SideB)
	assert.Equal(t, "Ann", pools[0].Matches[0].SideA.Name)
	assert.Equal(t, "Bea", pools[0].Matches[0].SideB.Name)
	// The kept match retains its ORIGINAL suffix (0), not a compacted index.
	assert.Equal(t, []int{0}, ordinals["Pool A"])
}

// TestAttachPoolMatches_MiddleSkipPreservesOrdinals is the regression test for the
// ordinal-shift desync: when a MIDDLE match is skipped (unresolvable side), the
// surviving matches must keep their original suffixes so later grid rows don't
// look up the wrong stored result.
func TestAttachPoolMatches_MiddleSkipPreservesOrdinals(t *testing.T) {
	t.Parallel()
	pools := []helper.Pool{{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "id-1", Name: "Ann"}, {ID: "id-2", Name: "Bea"}, {ID: "id-3", Name: "Cid"},
		},
	}}
	results := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ann", SideAID: "id-1", SideB: "Bea", SideBID: "id-2"},
		{ID: "Pool A-1", SideA: "Ann", SideAID: "id-1", SideB: "Ghost", SideBID: "id-gone"}, // MIDDLE skip
		{ID: "Pool A-2", SideA: "Bea", SideBID: "id-2", SideB: "Cid"},                       // resolvable by name/id
	}
	// Give Pool A-2 a resolvable SideAID too.
	results[2].SideAID = "id-2"
	results[2].SideBID = "id-3"

	ordinals := attachPoolMatches(pools, results)

	require.Len(t, pools[0].Matches, 2)
	// Kept matches are 0 and 2 (1 was skipped); the ordinals slice preserves that,
	// so grid row 1 maps to suffix 2, NOT compacted index 1.
	assert.Equal(t, []int{0, 2}, ordinals["Pool A"])
}

// TestBuildResultsWorkbook_MiddleSkipScorePlacement drives the ordinal desync end
// to end: with a middle match skipped (references a removed participant), the
// third match's unique score must still appear in the grid. Under the bug, grid
// row 1 looked up "Pool A-1" (the skipped ghost match) instead of "Pool A-2", so
// the third match's score was never rendered.
func TestBuildResultsWorkbook_MiddleSkipScorePlacement(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	al, bo, ca := makePlayer("Alice"), makePlayer("Bob"), makePlayer("Carol")
	pools := []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{al, bo, ca}}}
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideAID: "Alice", SideB: "Bob", SideBID: "Bob", Winner: "Alice", IpponsA: []string{"M"}, Decision: "fought", Status: state.MatchStatusCompleted},
		// Middle match references a participant no longer in the pool -> skipped.
		{ID: "Pool A-1", SideA: "Alice", SideAID: "Alice", SideB: "Ghost", SideBID: "ghost-id", Winner: "Alice", IpponsA: []string{"T"}, Decision: "fought", Status: state.MatchStatusCompleted},
		{ID: "Pool A-2", SideA: "Bob", SideAID: "Bob", SideB: "Carol", SideBID: "Carol", Winner: "Bob", IpponsA: []string{"K", "K", "K"}, Decision: "fought", Status: state.MatchStatusCompleted},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	assert.True(t, sheetContainsCell(rows, "M"), "match 0's score must render")
	assert.True(t, sheetContainsCell(rows, "KKK"),
		"match 2's unique score must render in the grid despite the middle match being skipped")
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
	pools := []helper.Pool{
		make2PlayerPool("Pool A", "Alice", "Bob"),
		make2PlayerPool("Pool B", "Charlie", "Dave"),
		make2PlayerPool("Pool C", "Eve", "Frank"),
		make2PlayerPool("Pool D", "Grace", "Hank"),
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

	// Pools 0,1 -> court A; pools 2,3 -> court B (contiguous assignment).
	pools := []helper.Pool{
		make2PlayerPool("Pool A", "Alice", "Bob"),
		make2PlayerPool("Pool B", "Charlie", "Dave"),
		make2PlayerPool("Pool C", "Eve", "Frank"),
		make2PlayerPool("Pool D", "Grace", "Hank"),
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

	courtAW := headerColInBand(rows, "W", 0, helper.CourtsColumnsPerCourt)
	courtBW := headerColInBand(rows, "W", helper.CourtsColumnsPerCourt, 2*helper.CourtsColumnsPerCourt)
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

	// 4 pools (2 per court) → 4 finalists → a semifinal round of 2 matches, one
	// per court, rendered side-by-side on the same rows.
	pools := []helper.Pool{
		make2PlayerPool("Pool A", "Alice", "Bob"),
		make2PlayerPool("Pool B", "Charlie", "Dave"),
		make2PlayerPool("Pool C", "Eve", "Frank"),
		make2PlayerPool("Pool D", "Grace", "Hank"),
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

// TestBuildResultsWorkbook_OverlaidCellsAreLiteral pins the core contract of the
// results export: cells the overlays populate (played scores AND collapse-prone
// W/L/T/Rank standings) must be LITERAL values, not formulas. excelize's
// SetCellValue/SetCellInt clear any pre-existing formula in the cell (verified in
// this test after a full save/reopen round-trip), so the archived workbook does
// not depend on spreadsheet recalculation. This guards against a future change in
// that behaviour that would silently leave collapse-prone formulas in place.
func TestBuildResultsWorkbook_OverlaidCellsAreLiteral(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", IpponsA: []string{"M", "K"}, Decision: "fought", Status: state.MatchStatusCompleted},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// The played ippon score "MK" is an overlaid literal, not a formula.
	scoreRef := cellRefWithValue(t, rows, "MK")
	require.NotEmpty(t, scoreRef, "overlaid ippon score 'MK' must be present")
	fm, _ := f.GetCellFormula(helper.SheetPoolMatches, scoreRef)
	assert.Empty(t, fm, "overlaid score cell %s must be a literal, not a formula", scoreRef)

	// The winner's standings W = 1 is overlaid onto a formerly formula-driven cell.
	winRef := cellRefWithValue(t, rows, "1")
	require.NotEmpty(t, winRef, "overlaid W=1 must be present in Pool Matches sheet")
	wf, _ := f.GetCellFormula(helper.SheetPoolMatches, winRef)
	assert.Empty(t, wf, "overlaid standings cell %s must be a literal, not a formula", winRef)
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

// TestBuildResultsWorkbook_TeamPlayoffsNames is the regression test for the team-
// playoffs summary-name off-by-one in overlayPlayoffBracketNames. A no-pools team
// playoffs repeats the entrant-name formulas on the summary row at
// header+4+teamSize; the overlay must overwrite THAT row (clearing the broken ”!
// formula) and leave the "Victories / Points" label at header+5+teamSize intact.
// Before the fix it targeted header+5+teamSize, leaving a broken formula behind and
// clobbering the label. This path (Playoffs + TeamSize>0, no pools) had no coverage.
func TestBuildResultsWorkbook_TeamPlayoffsNames(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "team"
	comp.TeamSize = 3
	comp.Format = state.CompFormatPlayoffs
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Team A", Dojo: "D"}, {Name: "Team B", Dojo: "D"},
		{Name: "Team C", Dojo: "D"}, {Name: "Team D", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, br)
	// Score the first round's encounters with 3 sub-bouts each.
	for mi := range br.Rounds[0] {
		m := &br.Rounds[0][mi]
		if m.SideA == "" || m.SideB == "" {
			continue
		}
		m.Winner = m.SideA
		m.Status = state.MatchStatusCompleted
		m.SubResults = []state.SubMatchResult{
			{Position: 1, SideA: m.SideA, SideB: m.SideB, IpponsA: []string{"M", "K"}, Winner: m.SideA},
			{Position: 2, SideA: m.SideA, SideB: m.SideB, IpponsB: []string{"M"}, Winner: m.SideB},
			{Position: 3, SideA: m.SideA, SideB: m.SideB, IpponsA: []string{"D"}, Winner: m.SideA},
		}
	}
	require.NoError(t, store.SaveBracket(compID, br))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// The off-by-one left a broken ''! entrant-name formula on the summary row.
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	// The "Victories / Points" label sits one row below the summary-name row; the
	// off-by-one clobbered it with a team name.
	assert.True(t, sheetContainsCell(rows, "Victories / Points"),
		"the team summary 'Victories / Points' label must survive the name overlay")
	assert.True(t, sheetContainsCell(rows, "Team A"),
		"a team entrant name must be written literally into the bracket")
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

// ------------------------------------------------------------
// TDD-2: Paired-name rendering (combined pair name in the Data sheet)
// ------------------------------------------------------------

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

// TestBuildResultsWorkbook_EngiPairedNameInDataSheet verifies that for an engi
// competition (Engi=true, WithZekkenName=false) the combined pair name
// ("Name 1 - Name 2") is written to the Data sheet name column; engi does not
// alter the CSV/data-sheet layout.
func TestBuildResultsWorkbook_EngiPairedNameInDataSheet(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Mark competition as engi (WithZekkenName remains false).
	markCompAsEngi(t, store, compID)

	pools := makeEngiPools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// The Name column (B, index 1) must carry the combined pair name; engi does
	// not populate the zekken column (WithZekkenName stays false).
	rows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)

	assert.True(t, columnContains(rows, 1, "Member One A - Member Two A"),
		"Data sheet col B must contain the combined pair name for an engi competition")
}

// TestBuildResultsWorkbook_NonEngiWithZekkenStillWorks verifies additivity:
// a non-engi competition with WithZekkenName=true still writes DisplayName
// to col D (existing behaviour is not broken by the engi fix).
func TestBuildResultsWorkbook_NonEngiWithZekkenStillWorks(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Standard competition: no engi, but WithZekkenName=true.
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.WithZekkenName = true
	require.NoError(t, store.SaveCompetition(comp))

	// SideA/SideB point into pool.Players so the playerMatchRows map resolves correctly.
	zekkenPool := helper.Pool{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "p1", Name: "Name One", DisplayName: "Zekken One", Dojo: "DojoX"},
			{ID: "p2", Name: "Name Two", DisplayName: "Zekken Two", Dojo: "DojoY"},
		},
	}
	zekkenPool.Matches = []helper.Match{{SideA: &zekkenPool.Players[0], SideB: &zekkenPool.Players[1]}}
	pools := []helper.Pool{zekkenPool}
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)

	assert.True(t, columnContains(rows, 3, "Zekken One"),
		"Data sheet col D must contain 'Zekken One' for WithZekkenName=true competition")
}

// ------------------------------------------------------------
// TDD-3: Flag score cells in pool and bracket overlays
// ------------------------------------------------------------

// firstPoolMatchScoreRow returns the row from `rows` that holds the first match's
// score cells for the pool assigned to the court band starting at 0-based column
// `bandStart` ("Red" or "White" marks that column). The match row is one row below
// the Red/White header. Returns nil if no such header exists.
// Column layout within the band (0-based absolute): bandStart+1 = left score,
// bandStart+3 = vs/middle, bandStart+5 = right score.
func firstPoolMatchScoreRow(rows [][]string, bandStart int) []string {
	for ri, row := range rows {
		if bandStart >= len(row) {
			continue
		}
		if row[bandStart] == "Red" || row[bandStart] == "White" {
			if ri+1 < len(rows) {
				return rows[ri+1]
			}
		}
	}
	return nil
}

// TestBuildResultsWorkbook_EngiPoolFlagScoreCells verifies that for an engi pool
// match, the Pool Matches sheet renders the referee flag counts as literal numbers,
// not ippon letters. Previously broken because overlayPoolScores called
// IpponsScore(mr.IpponsA) which returned "" (engi matches have no ippons).
// Both the 3-2 case and the 5-0 shutout are exercised: the loser's "0" must be
// written explicitly to distinguish a clean shutout from a kiken/fusenpai.
func TestBuildResultsWorkbook_EngiPoolFlagScoreCells(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		flagsA     int
		flagsB     int
		wantLeft   string
		wantRight  string
		wantStands string // expected value in the Flags standings column
	}{
		{
			name: "3-2", flagsA: 3, flagsB: 2,
			wantLeft: "3", wantRight: "2", wantStands: "3",
		},
		{
			// 5-0 shutout: the loser's "0" is a real score, distinguishing it from
			// a kiken/fusenpai where no flags were recorded.
			name: "5-0 shutout", flagsA: 5, flagsB: 0,
			wantLeft: "5", wantRight: "0", wantStands: "5",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)

			markCompAsEngi(t, store, compID)

			pools := makeEngiPools()
			require.NoError(t, store.SavePools(compID, pools))

			// SideAID/SideBID/WinnerID are required for computeEngiStandings so the
			// standings overlay can also write the accumulated flag total.
			results := []state.MatchResult{
				{
					ID:       "Pool A-0",
					SideA:    "Member One A",
					SideAID:  "pair1",
					SideB:    "Member One B",
					SideBID:  "pair2",
					FlagsA:   tc.flagsA,
					FlagsB:   tc.flagsB,
					Decision: "fought",
					Status:   state.MatchStatusCompleted,
					Winner:   "Member One A",
					WinnerID: "pair1",
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

			// Assert the MATCH ROW score cells specifically: left score (FlagsA) at
			// bandStart+1 and right score (FlagsB) at bandStart+5 (vs is at bandStart+3).
			// Using containsCell alone would be satisfied by the standings overlay alone.
			matchRow := firstPoolMatchScoreRow(rows, 0)
			require.NotNil(t, matchRow, "match score row must exist (Red/White header must be present)")
			require.Greater(t, len(matchRow), 5, "match score row must have at least 6 columns")
			assert.Equal(t, tc.wantLeft, matchRow[1],
				"left score cell (2 before vs at col 3) must be %q", tc.wantLeft)
			assert.Equal(t, tc.wantRight, matchRow[5],
				"right score cell (2 after vs at col 3) must be %q", tc.wantRight)

			// The winner's accumulated flag total also appears in the standings column.
			assert.True(t, columnHasValueUnderHeader(rows, helper.ColHeaderFlags, tc.wantStands),
				"Pool Matches 'Flags' standings column must carry %q for the engi winner", tc.wantStands)
		})
	}
}

// TestBuildResultsWorkbook_EngiNonEngiPoolScoreUnchanged verifies that a non-engi
// pool match still renders ippon letters (not flag counts) after the engi fix.
func TestBuildResultsWorkbook_EngiNonEngiPoolScoreUnchanged(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Non-engi competition.
	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

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

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	assert.True(t, sheetContainsCell(rows, "M"),
		"Pool Matches sheet must still contain 'M' (ippon) for a non-engi match")
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

// TestBuildResultsWorkbook_EngiBracketFlagScoreCells verifies that for an engi
// elimination bracket match with FlagsA=3 and FlagsB=2, the Elimination Matches
// sheet renders the flag counts ("3"/"2") in the victory cells, NOT the ippon
// letters carried in ScoreA/ScoreB (which do not apply to engi). The assertion is
// column-precise (it reads the exact victory cell under the match header) so an
// incidental "3"/"2" elsewhere cannot mask a regression. Both the default
// (non-mirror) and mirror layouts are exercised: mirror swaps which victory
// column carries FlagsA vs FlagsB, so the two cases must be column-mirror images.
//
// Previously broken because overlayBracketScores used ScoreA/ScoreB directly,
// which for engi are the (inapplicable) ippon letters, and never consulted
// FlagsA/FlagsB.
func TestBuildResultsWorkbook_EngiBracketFlagScoreCells(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		mirror            bool
		flagsA            int
		flagsB            int
		scoreA            string
		scoreB            string
		wantLeft          string
		wantRight         string
		forbiddenIpponVal string
	}{
		// Default (non-mirror): left column (Red/SideA) carries FlagsA=3, right
		// column (White/SideB) carries FlagsB=2.
		{name: "default", mirror: false, flagsA: 3, flagsB: 2, scoreA: "MK", scoreB: "M", wantLeft: "3", wantRight: "2", forbiddenIpponVal: "MK"},
		// Mirror: the two victory columns are swapped, so left carries FlagsB=2
		// and right carries FlagsA=3.
		{name: "mirror", mirror: true, flagsA: 3, flagsB: 2, scoreA: "MK", scoreB: "M", wantLeft: "2", wantRight: "3", forbiddenIpponVal: "MK"},
		// 5-0 shutout: the loser's cell must be "0", not blank (pairwise write rule).
		{name: "5-0 shutout", mirror: false, flagsA: 5, flagsB: 0, scoreA: "MK", scoreB: "", wantLeft: "5", wantRight: "0", forbiddenIpponVal: "MK"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)

			// Engi + Mixed format so an elimination bracket sheet is produced.
			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			comp.Format = state.CompFormatMixed
			comp.Engi = true
			comp.Mirror = tc.mirror
			require.NoError(t, store.SaveCompetition(comp))

			pools := makeEngiPools()
			require.NoError(t, store.SavePools(compID, pools))
			require.NoError(t, store.SavePoolMatches(compID, nil))

			// Engi bracket match: pair1 beats pair2 on referee flags. ScoreA/ScoreB
			// carry ippon letters that MUST NOT render for engi; if the engi branch
			// is skipped, the ippon value would leak into the left victory cell.
			bracket := &state.Bracket{
				Rounds: [][]state.BracketMatch{
					{
						{
							ID:          "B1",
							SideA:       "Member One A",
							SideB:       "Member One B",
							Winner:      "Member One A",
							Status:      state.MatchStatusCompleted,
							FlagsA:      tc.flagsA,
							FlagsB:      tc.flagsB,
							ScoreA:      tc.scoreA,
							ScoreB:      tc.scoreB,
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

			rows, err := f.GetRows(helper.SheetEliminationMatches)
			require.NoError(t, err)

			left, right := bracketVictoryCells(t, rows, "Round 1 - Match 1")
			assert.Equal(t, tc.wantLeft, left,
				"left victory cell must render the engi flag count, not an ippon letter")
			assert.Equal(t, tc.wantRight, right,
				"right victory cell must render the engi flag count, not an ippon letter")

			// The ippon-letter score (ScoreA="MK") must never leak into any cell:
			// engi is flag-scored, so the ippon path must be fully bypassed.
			assert.False(t, sheetContainsCell(rows, tc.forbiddenIpponVal),
				"elimination sheet must NOT contain ippon letters (%q) for an engi bracket", tc.forbiddenIpponVal)
		})
	}
}

// ------------------------------------------------------------
// TDD-4: Standings headers relabeled for engi (W/L/Flags/Rank)
// ------------------------------------------------------------

// TestBuildResultsWorkbook_EngiStandingsHeadersRelabeled verifies that for an
// engi competition the Pool Matches standings section uses W/L/Flags/Rank headers
// (not W/L/T/PW/PL/Rank) and that the accumulated own-side flag count is overlaid
// as a literal value under the "Flags" header.
//
// Previously broken because PrintPoolMatches had no engi parameter and
// printIndividualResultsTableSection always wrote "T"/"PW"/"PL" headers, and
// overlayPoolStandings wrote to those non-existent-for-engi columns.
func TestBuildResultsWorkbook_EngiStandingsHeadersRelabeled(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Mark competition as engi (WithZekkenName remains false; EffectiveWithZekkenName() handles it).
	markCompAsEngi(t, store, compID)

	// Two engi pairs in one pool.
	pools := makeEngiPools()
	require.NoError(t, store.SavePools(compID, pools))

	// Engi match: pair1 wins 3-2 on referee flags.
	// SideAID/SideBID must match the player IDs in makeEngiPools so that
	// computeEngiStandings can resolve the standings row via engiPlayerKey.
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Member One A",
			SideAID:  "pair1",
			SideB:    "Member One B",
			SideBID:  "pair2",
			FlagsA:   3,
			FlagsB:   2,
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Member One A",
			WinnerID: "pair1",
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

	// The standings table must have a "Flags" header.
	assert.True(t, sheetContainsCell(rows, "Flags"),
		"Pool Matches standings header must be 'Flags' for an engi competition")

	// The winner's accumulated flag count (3) must be overlaid as a literal
	// under the "Flags" header.
	assert.True(t, columnHasValueUnderHeader(rows, "Flags", "3"),
		"Pool Matches 'Flags' column must contain literal '3' for the engi winner")

	// "PW" and "PL" must NOT appear in the standings header for engi
	// (they are kendo-specific ippon-count columns).
	assert.False(t, sheetContainsCell(rows, "PW"),
		"Pool Matches must NOT contain 'PW' header for an engi competition")
	assert.False(t, sheetContainsCell(rows, "PL"),
		"Pool Matches must NOT contain 'PL' header for an engi competition")

	// "L" must NOT appear in the standings header for engi:
	// engi rankings do not record losses, only wins and flags.
	assert.False(t, sheetContainsCell(rows, "L"),
		"Pool Matches must NOT contain 'L' header for an engi competition")
}

// TestBuildResultsWorkbook_NonEngiStandingsHeadersUnchanged proves additivity:
// a non-engi competition still has the W/L/T/PW/PL/Rank standings header after
// the engi relabeling fix (engi=false must be a no-op).
func TestBuildResultsWorkbook_NonEngiStandingsHeadersUnchanged(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Standard non-engi competition with a single pool match.
	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alice",
			SideB:    "Bob",
			IpponsA:  []string{"M"},
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

	// Non-engi must still have the classic kendo standings headers.
	assert.True(t, sheetContainsCell(rows, "PW"),
		"Non-engi Pool Matches must still contain 'PW' header")
	assert.True(t, sheetContainsCell(rows, "PL"),
		"Non-engi Pool Matches must still contain 'PL' header")
	assert.True(t, sheetContainsCell(rows, "T"),
		"Non-engi Pool Matches must still contain 'T' header")
	// "Flags" must NOT appear in the non-engi standings.
	assert.False(t, sheetContainsCell(rows, "Flags"),
		"Non-engi Pool Matches must NOT contain 'Flags' header")
}

// ------------------------------------------------------------
// TDD-5: Engi special-case characterization tests
// ------------------------------------------------------------

// TestBuildResultsWorkbook_EngiDecisionSuffix characterizes the vs-cell text for
// a kiken-voluntary engi match: the middle cell must carry "Kiken" and both
// adjacent score cells (at column offsets -2 and +2 from the vs cell) must be
// blank because FlagsScorePair returns ("", "") when neither side scored flags.
func TestBuildResultsWorkbook_EngiDecisionSuffix(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	markCompAsEngi(t, store, compID)

	pools := makeEngiPools()
	require.NoError(t, store.SavePools(compID, pools))

	// kiken-voluntary: pair1 wins; no flag score is recorded (FlagsScorePair -> "", "").
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Member One A",
			SideAID:  "pair1",
			SideB:    "Member One B",
			SideBID:  "pair2",
			FlagsA:   0,
			FlagsB:   0,
			Decision: "kiken-voluntary",
			Status:   state.MatchStatusCompleted,
			Winner:   "Member One A",
			WinnerID: "pair1",
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

	// The vs/middle cell must carry "Kiken" for a kiken-voluntary engi match.
	assert.True(t, sheetContainsCell(rows, "Kiken"),
		"Pool Matches vs-cell must render 'Kiken' for a kiken-voluntary engi match")

	// Score cells flanking the vs column must be blank: FlagsScorePair returns ("", "") for a no-flag decision.
	// The vs cell sits at column offset 3 from the court band start; score cells
	// are at offsets 1 (left) and 5 (right), so -2 and +2 from the vs index.
	for _, row := range rows {
		for j, cell := range row {
			if cell != "Kiken" {
				continue
			}
			require.GreaterOrEqual(t, j, 2,
				"Kiken vs-cell must not appear in the first two columns")
			assert.Equal(t, "", row[j-2],
				"left score cell (col offset -2 from vs) must be blank for kiken with FlagsA=0")
			if j+2 < len(row) {
				assert.Equal(t, "", row[j+2],
					"right score cell (col offset +2 from vs) must be blank for kiken with FlagsB=0")
			}
		}
	}
}

// TestBuildResultsWorkbook_EngiPartialPoolScoring characterizes partial scoring:
// two engi pools, only pool A is scored (FlagsA=3, FlagsB=2). Pool B is left
// untouched. The export must succeed and the scored flags must appear in the Pool
// Matches sheet under the "Flags" standings column.
func TestBuildResultsWorkbook_EngiPartialPoolScoring(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	markCompAsEngi(t, store, compID)

	// Two engi pools of two pairs each; pool B is unscored. Each pair carries
	// both member names combined in Name (the canonical engi model).
	// SideA/SideB point into pool.Players so the playerMatchRows map resolves correctly.
	partialPoolA := helper.Pool{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "ep1", Name: "Spark A - Spark B", Dojo: "DojoA"},
			{ID: "ep2", Name: "Flame A - Flame B", Dojo: "DojoB"},
		},
	}
	partialPoolA.Matches = []helper.Match{{SideA: &partialPoolA.Players[0], SideB: &partialPoolA.Players[1]}}

	partialPoolB := helper.Pool{
		PoolName: "Pool B",
		Players: []helper.Player{
			{ID: "ep3", Name: "Wave A - Wave B", Dojo: "DojoC"},
			{ID: "ep4", Name: "Stone A - Stone B", Dojo: "DojoD"},
		},
	}
	partialPoolB.Matches = []helper.Match{{SideA: &partialPoolB.Players[0], SideB: &partialPoolB.Players[1]}}

	pools := []helper.Pool{partialPoolA, partialPoolB}
	require.NoError(t, store.SavePools(compID, pools))

	// Score pool A only; pool B has no result entry (partial scoring).
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Spark A - Spark B",
			SideAID:  "ep1",
			SideB:    "Flame A - Flame B",
			SideBID:  "ep2",
			FlagsA:   3,
			FlagsB:   2,
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Spark A - Spark B",
			WinnerID: "ep1",
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

	// Assert the pool A MATCH ROW score cells specifically: left score (FlagsA) at
	// bandStart+1 and right score (FlagsB) at bandStart+5 (vs is at bandStart+3).
	// containsCell alone is vacuous: it would be satisfied by the standings overlay.
	matchRow := firstPoolMatchScoreRow(rows, 0)
	require.NotNil(t, matchRow, "pool A match score row must exist (Red/White header must be present)")
	require.Greater(t, len(matchRow), 5, "match score row must have at least 6 columns")
	assert.Equal(t, "3", matchRow[1],
		"left score cell (2 before vs) must be '3' (FlagsA=3 for pool A)")
	assert.Equal(t, "2", matchRow[5],
		"right score cell (2 after vs) must be '2' (FlagsB=2 for pool A)")

	// The winner's accumulated flag total must appear under the "Flags" standings header.
	assert.True(t, columnHasValueUnderHeader(rows, helper.ColHeaderFlags, "3"),
		"Pool Matches 'Flags' standings column must carry '3' for the pool A winner")
}

// TestBuildResultsWorkbook_EngiMultiCourtStandingsColumns is the engi analog of
// TestBuildResultsWorkbook_MultiCourtStandingsColumns: four engi pools across two
// courts; only the court-B pool is scored. The "Flags" header appears once per
// court band; the court-B Flags column must hold the scored value while the
// court-A Flags column must remain clean (no cross-band bleed).
func TestBuildResultsWorkbook_EngiMultiCourtStandingsColumns(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "export-test-mc-engi-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)

	compID := "mc-engi"
	comp := &state.Competition{ID: compID, Name: "MC Engi", Courts: []string{"A", "B"}, Engi: true}
	require.NoError(t, store.SaveCompetition(comp))

	// makePair builds an engi pair: both member names combined in Name (the
	// canonical model); DisplayName is not used for engi.
	makePair := func(id, n1, n2, dojo string) helper.Player {
		return helper.Player{ID: id, Name: n1 + " - " + n2, Dojo: dojo}
	}

	// makeEngiPool creates a pool with SideA/SideB pointing into pool.Players.
	makeEngiPool := func(name string, p1, p2 helper.Player) helper.Pool {
		pool := helper.Pool{PoolName: name, Players: []helper.Player{p1, p2}}
		pool.Matches = []helper.Match{{SideA: &pool.Players[0], SideB: &pool.Players[1]}}
		return pool
	}

	// Four pools: [A,B] -> court A; [C,D] -> court B (contiguous assignment).
	pools := []helper.Pool{
		makeEngiPool("Pool A", makePair("pa1", "AOne-A", "AOne-B", "DojoPA"), makePair("pa2", "ATwo-A", "ATwo-B", "DojoPA")),
		makeEngiPool("Pool B", makePair("pb1", "BOne-A", "BOne-B", "DojoPB"), makePair("pb2", "BTwo-A", "BTwo-B", "DojoPB")),
		makeEngiPool("Pool C", makePair("pc1", "COne-A", "COne-B", "DojoPC"), makePair("pc2", "CTwo-A", "CTwo-B", "DojoPC")),
		makeEngiPool("Pool D", makePair("pd1", "DOne-A", "DOne-B", "DojoPD"), makePair("pd2", "DTwo-A", "DTwo-B", "DojoPD")),
	}
	require.NoError(t, store.SavePools(compID, pools))

	// Score only Pool C (court B): pc1 wins 3-2 on flags.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "Pool C-0", SideA: "COne-A - COne-B", SideAID: "pc1",
			SideB: "CTwo-A - CTwo-B", SideBID: "pc2",
			FlagsA: 3, FlagsB: 2,
			Decision: "fought", Status: state.MatchStatusCompleted,
			Winner: "COne-A - COne-B", WinnerID: "pc1",
		},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	courtAFlags := headerColInBand(rows, helper.ColHeaderFlags, 0, helper.CourtsColumnsPerCourt)
	courtBFlags := headerColInBand(rows, helper.ColHeaderFlags, helper.CourtsColumnsPerCourt, 2*helper.CourtsColumnsPerCourt)
	require.GreaterOrEqual(t, courtAFlags, 0, "court A 'Flags' header must exist in pool matches for an engi competition")
	require.GreaterOrEqual(t, courtBFlags, 0, "court B 'Flags' header must exist in pool matches for an engi competition")

	assert.True(t, columnContains(rows, courtBFlags, "3"),
		"court B's Flags column must carry the winner's accumulated flag count (=3)")
	assert.False(t, columnContains(rows, courtAFlags, "3"),
		"court A pools are unscored: '3' in court A's Flags column means court B's score leaked (multi-court colMap bug)")
}

// TestBuildResultsWorkbook_EngiUnicodeAndCommaNames characterizes that the export
// correctly handles engi pair names containing unicode characters and commas.
// The combined pair name (both members joined in the Name field) must appear in
// the Data sheet exactly, without corruption or splitting on comma or multibyte
// boundaries.
func TestBuildResultsWorkbook_EngiUnicodeAndCommaNames(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	markCompAsEngi(t, store, compID)

	// Unicode names (Japanese) and names containing commas.
	// SideA/SideB point into pool.Players so the playerMatchRows map resolves correctly.
	unicodePool := helper.Pool{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "uni1", Name: "結城 由紀 - 田中 花子", Dojo: "東京道場"},
			{ID: "com1", Name: "O'Brien, Sean - Smith, Jane", Dojo: "New York, NY"},
		},
	}
	unicodePool.Matches = []helper.Match{{SideA: &unicodePool.Players[0], SideB: &unicodePool.Players[1]}}
	// Keep local variable aliases for name assertions below.
	uniPair := unicodePool.Players[0]
	comPair := unicodePool.Players[1]
	pools := []helper.Pool{unicodePool}
	require.NoError(t, store.SavePools(compID, pools))
	// No match results; we are only verifying Data-sheet name rendering.
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	dataRows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)

	// Both combined pair names must appear as exact cell values in the Data sheet.
	names := []string{uniPair.Name, comPair.Name}
	for _, name := range names {
		assert.True(t, sheetContainsCell(dataRows, name),
			"Data sheet must contain member name %q exactly (unicode/comma safe)", name)
	}
}

// ============================================================
// TDD-bronze: Naginata 3rd-place block rendering (mp-wvba)
// ============================================================

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

// setNaginataPlayoffs configures the loaded competition as a naginata playoffs
// competition (individual, 4 players, single court).
func setNaginataPlayoffs(t *testing.T, store *state.Store, compID string, engi bool) {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = state.CompFormatPlayoffs
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
// returns the bracket. The caller must have called setNaginataPlayoffs first.
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

// TestBuildResultsWorkbook_NaginataThirdPlaceRendered verifies that a scored
// naginata bronze match appears as a "3rd Place" block on the Elimination
// Matches sheet with the winner's ippon letter in the score row.
func TestBuildResultsWorkbook_NaginataThirdPlaceRendered(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataPlayoffs(t, store, compID, false)
	bracket := startNaginataWith4Players(t, store, eng, compID, false)

	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2, "expected 2 semifinals for 4 players")

	// Score both SFs.
	require.NoError(t, eng.RecordMatchResult(compID, sf[0].ID, &state.MatchResult{
		Winner:  sf[0].SideA,
		IpponsA: []string{"M"},
		Status:  state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, sf[1].ID, &state.MatchResult{
		Winner:  sf[1].SideB,
		IpponsB: []string{"K"},
		Status:  state.MatchStatusCompleted,
	}))

	// Reload bracket to get the bronze sides populated by the engine.
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bronzeWinner := bracket.ThirdPlaceMatch.SideA

	// Score bronze with "D" ippon (distinctive; SFs used "M" and "K").
	require.NoError(t, eng.RecordMatchResult(compID, "m-bronze", &state.MatchResult{
		Winner:  bronzeWinner,
		IpponsA: []string{"D"},
		Status:  state.MatchStatusCompleted,
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	assert.True(t, sheetContainsCell(rows, helper.ThirdPlaceLabel),
		"Elimination Matches sheet must have a '3rd Place' header block for naginata")
	assert.True(t, sheetContainsCell(rows, "D"),
		"Elimination Matches sheet must show the bronze score 'D' (kendo ippon, only in bronze)")

	// Also assert that both semifinal losers' names appear in the bronze score row.
	assertBronzeEntrantsPopulated(t, rows)
}

// TestBuildResultsWorkbook_NaginataThirdPlaceNamesBeforeBronze verifies that
// the bronze entrant names appear in the 3rd Place score row even when the
// bronze match has not yet been played (semis scored, bronze open).
func TestBuildResultsWorkbook_NaginataThirdPlaceNamesBeforeBronze(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataPlayoffs(t, store, compID, false)
	bracket := startNaginataWith4Players(t, store, eng, compID, false)

	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2, "expected 2 semifinals for 4 players")

	// Score both SFs so the engine populates ThirdPlaceMatch.SideA/SideB.
	require.NoError(t, eng.RecordMatchResult(compID, sf[0].ID, &state.MatchResult{
		Winner:  sf[0].SideA,
		IpponsA: []string{"M"},
		Status:  state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, sf[1].ID, &state.MatchResult{
		Winner:  sf[1].SideB,
		IpponsB: []string{"K"},
		Status:  state.MatchStatusCompleted,
	}))

	// Verify the engine populated the bronze sides before export.
	b2, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotEmpty(t, b2.ThirdPlaceMatch.SideA, "engine must populate ThirdPlaceMatch.SideA after SFs")
	require.NotEmpty(t, b2.ThirdPlaceMatch.SideB, "engine must populate ThirdPlaceMatch.SideB after SFs")

	// Export WITHOUT scoring bronze.
	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Names must appear even though the bronze has not been played.
	assertBronzeEntrantsPopulated(t, rows)

	// Score cells (lVCol = col B / index 1, rVCol = col F / index 5) must be empty.
	thirdPlaceRow := bctest.FindCellRow(rows, helper.ThirdPlaceLabel)
	scoreRowIdx := thirdPlaceRow + 2
	scoreRow := rows[scoreRowIdx]
	var leftScore, rightScore string
	if len(scoreRow) > 1 {
		leftScore = scoreRow[1]
	}
	if len(scoreRow) > 5 {
		rightScore = scoreRow[5]
	}
	assert.Empty(t, leftScore, "bronze score cell (col B) must be empty before bronze is played")
	assert.Empty(t, rightScore, "bronze score cell (col F) must be empty before bronze is played")
}

// TestBuildResultsWorkbook_EngiNaginataThirdPlaceFlags verifies that an engi
// naginata bronze match renders on the Elimination Matches sheet with flag counts.
func TestBuildResultsWorkbook_EngiNaginataThirdPlaceFlags(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataPlayoffs(t, store, compID, true)
	bracket := startNaginataWith4Players(t, store, eng, compID, true)

	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2)

	// Score both SFs via the engi (flag) path.
	_, err := eng.RecordMatchResultWithIneligibility(compID, sf[0].ID, &state.MatchResult{
		FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)
	_, err = eng.RecordMatchResultWithIneligibility(compID, sf[1].ID, &state.MatchResult{
		FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)

	// Score bronze 5-0 (distinctive flag counts not used in the SFs).
	_, err = eng.RecordMatchResultWithIneligibility(compID, "m-bronze", &state.MatchResult{
		FlagsA: 5, FlagsB: 0, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	assert.True(t, sheetContainsCell(rows, helper.ThirdPlaceLabel),
		"Elimination Matches sheet must have a '3rd Place' header for engi naginata")

	// Find the "3rd Place" row and verify the score row (header+2) carries "5".
	thirdPlaceRow := bctest.FindCellRow(rows, helper.ThirdPlaceLabel)
	require.GreaterOrEqual(t, thirdPlaceRow, 0, "'3rd Place' header row must be found")
	scoreRowIdx := thirdPlaceRow + 2
	require.Less(t, scoreRowIdx, len(rows), "bronze score row (header+2) must exist")
	assert.True(t, slices.Contains(rows[scoreRowIdx], "5"),
		"bronze score row must contain '5' (FlagsA=5 winner count)")

	// Also assert entrant names appear in the bronze score row.
	assertBronzeEntrantsPopulated(t, rows)
}

// TestBuildResultsWorkbook_NonNaginataNoThirdPlace verifies that a standard
// kendo (non-naginata) playoffs export does NOT emit a "3rd Place" block,
// preserving byte-identical output for non-naginata competitions.
func TestBuildResultsWorkbook_NonNaginataNoThirdPlace(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Kendo playoffs (Naginata=false).
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = state.CompFormatPlayoffs
	comp.Naginata = false
	comp.Kind = "individual"
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 2
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Charlie", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	assert.False(t, sheetContainsCell(rows, helper.ThirdPlaceLabel),
		"non-naginata (kendo) export must NOT contain a '3rd Place' block")
}

// TestBuildResultsWorkbook_NaginataThirdPlaceEntrantFormulas verifies that the
// bronze block's entrant cells carry CONCATENATE formulas referencing the "2."
// (loser) lines of the two semifinals. This covers the workbook state BEFORE any
// matches are scored: the overlay writes no literals, so the formula skeleton must
// self-document who belongs in the bronze.
func TestBuildResultsWorkbook_NaginataThirdPlaceEntrantFormulas(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataPlayoffs(t, store, compID, false)
	// startNaginataWith4Players starts the competition without scoring any matches.
	// ThirdPlaceMatch exists but SideA/SideB are empty, so overlayBracketScores
	// writes no literal names and the skeleton formulas remain visible.
	startNaginataWith4Players(t, store, eng, compID, false)

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Locate the "3rd Place" header row (0-based index).
	thirdPlaceRowIdx := bctest.FindCellRow(rows, helper.ThirdPlaceLabel)
	require.GreaterOrEqual(t, thirdPlaceRowIdx, 0, "'3rd Place' header must be present")

	// Score row: 1-based Excel row = (0-based idx + 1) + 2.
	scoreExcelRow := thirdPlaceRowIdx + 3

	leftFormula, err := f.GetCellFormula(helper.SheetEliminationMatches, fmt.Sprintf("A%d", scoreExcelRow))
	require.NoError(t, err)
	rightFormula, err := f.GetCellFormula(helper.SheetEliminationMatches, fmt.Sprintf("G%d", scoreExcelRow))
	require.NoError(t, err)

	// Both cells together must hold CONCATENATE formulas referencing the two
	// semifinal losers. For a 4-player bracket the semis are M 1 and M 2;
	// the pair covers both because mirror may swap which cell holds which.
	combined := leftFormula + " " + rightFormula
	assert.Contains(t, combined, "CONCATENATE",
		"bronze entrant cells must carry CONCATENATE formulas (no scoring yet, no literal names)")
	assert.Contains(t, combined, "M 1",
		"bronze entrant formulas must reference the loser of semifinal M 1")
	assert.Contains(t, combined, "M 2",
		"bronze entrant formulas must reference the loser of semifinal M 2")
}

// TestBuildResultsWorkbook_EngiEliminationHeaderFlags verifies that when a
// competition has Engi=true, BuildResultsWorkbook writes "Fl" in the lV/rV
// columns of the Elimination Matches header row. The lV column for court 1 is
// B (startCol+1); rV is F (startCol+5). The first match title is at row 2, so
// the header row is at row 3.
func TestBuildResultsWorkbook_EngiEliminationHeaderFlags(t *testing.T) {
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
	comp.Engi = true
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "M1", Dojo: "D"}, {Name: "M2", Dojo: "D"}, {Name: "M3", Dojo: "D"},
		{Name: "M4", Dojo: "D"}, {Name: "M5", Dojo: "D"}, {Name: "M6", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// Match title at row 2, header row at row 3. lV=B, rV=F for court 1.
	lV, err := f.GetCellValue(helper.SheetEliminationMatches, "B3")
	require.NoError(t, err)
	rV, err := f.GetCellValue(helper.SheetEliminationMatches, "F3")
	require.NoError(t, err)
	assert.Equal(t, "Fl", lV, "engi elimination match header lV cell (B3) must be 'Fl'")
	assert.Equal(t, "Fl", rV, "engi elimination match header rV cell (F3) must be 'Fl'")
}

// TestBuildResultsWorkbook_MixedTreePageHasPoolRosters verifies that for a
// Mixed format competition (pools + knockout), the exported workbook's "Tree 1"
// sheet carries pool roster formula entries in column A. AddPoolsToTree writes
// the first pool name formula at row TreeTitleRows+1 = 4, followed by one row
// per player. A non-empty formula in A4 confirms rosters were populated.
func TestBuildResultsWorkbook_MixedTreePageHasPoolRosters(t *testing.T) {
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
		{Name: "P1", Dojo: "D"}, {Name: "P2", Dojo: "D"}, {Name: "P3", Dojo: "D"},
		{Name: "P4", Dojo: "D"}, {Name: "P5", Dojo: "D"}, {Name: "P6", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	sheets := f.GetSheetList()
	assert.Contains(t, sheets, "Tree 1", "Mixed export must produce a 'Tree 1' sheet")

	// Row 4 = TreeTitleRows+1 is where AddPoolsToTree writes the first pool name formula.
	formulaA4, err := f.GetCellFormula("Tree 1", "A4")
	require.NoError(t, err)
	assert.NotEmpty(t, formulaA4, "'Tree 1' A4 must contain a pool roster formula")

	// Title regression: the A1 title formula prepends data!$B$1 (already the
	// competition name), so the page title passed to SetTreeSheetTitle must be
	// the shiaijo label, not the competition name again, which rendered a
	// duplicated "Name - Name" heading.
	titleFormula, err := f.GetCellFormula("Tree 1", "A1")
	require.NoError(t, err)
	assert.Contains(t, titleFormula, "Shiaijo A", "'Tree 1' title must be the shiaijo label")
	assert.NotContains(t, titleFormula, comp.Name,
		"'Tree 1' title formula must not embed the competition name (data!$B$1 already prepends it)")
}

// findEliminationPrintAreaLastRow reads the workbook's defined names and returns
// the last-row number of the _xlnm.Print_Area name scoped to SheetEliminationMatches.
// Returns -1 if not found or unparseable.
func findEliminationPrintAreaLastRow(f *excelize.File) int {
	for _, dn := range f.GetDefinedName() {
		if dn.Name == "_xlnm.Print_Area" && dn.Scope == helper.SheetEliminationMatches {
			return bctest.ParsePrintAreaLastRow(dn.RefersTo)
		}
	}
	return -1
}

// TestBuildResultsWorkbook_NaginataThirdPlacePrintAreaCoversBlock verifies that
// after BuildResultsWorkbook renders a naginata 4-player bracket (with a bronze
// block), the _xlnm.Print_Area defined name for the Elimination Matches sheet
// includes at least the row where the "3rd Place" header appears. Before the fix
// the print area was set before the bronze block, leaving the bronze block outside
// the print area and invisible when printing or PDF-exporting.
func TestBuildResultsWorkbook_NaginataThirdPlacePrintAreaCoversBlock(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataPlayoffs(t, store, compID, false)
	startNaginataWith4Players(t, store, eng, compID, false)

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Locate the "3rd Place" header row (1-based Excel row).
	thirdPlaceExcelRow := bctest.FindCellRow(rows, helper.ThirdPlaceLabel) + 1
	require.GreaterOrEqual(t, thirdPlaceExcelRow, 1,
		"'3rd Place' header row must be present in Elimination Matches")

	printAreaLastRow := findEliminationPrintAreaLastRow(f)
	require.Greater(t, printAreaLastRow, 0,
		"_xlnm.Print_Area for Elimination Matches must exist and be parseable")
	assert.GreaterOrEqual(t, printAreaLastRow, thirdPlaceExcelRow,
		"Print_Area last row (%d) must cover at least the '3rd Place' header row (%d); bronze block falls outside the print area",
		printAreaLastRow, thirdPlaceExcelRow)
}

// TestBuildResultsWorkbook_PlayoffsTreePageNoPoolRosters verifies that for a
// pure Playoffs competition (no pool phase), the tree pages do NOT receive pool
// roster entries in column A (there are no pools to list).
func TestBuildResultsWorkbook_PlayoffsTreePageNoPoolRosters(t *testing.T) {
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
		{Name: "Q1", Dojo: "D"}, {Name: "Q2", Dojo: "D"},
		{Name: "Q3", Dojo: "D"}, {Name: "Q4", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// Find any tree page and assert column A row 4 has no pool roster formula.
	for _, sheet := range f.GetSheetList() {
		if len(sheet) < 4 || sheet[:4] != "Tree" {
			continue
		}
		formulaA4, err := f.GetCellFormula(sheet, "A4")
		require.NoError(t, err)
		assert.Empty(t, formulaA4,
			"playoffs tree page %s must not have a pool roster formula in A4", sheet)
	}
}

// TestBuildResultsWorkbook_EngiPairLabelsInBracketEntrants verifies that engi
// bracket entrant name cells render the full pair as "Member1 - Member2".
// Under the combined-name model the bracket match SideA/SideB fields hold the
// full "Pair1A - Pair1B" string (both members are joined in Player.Name at
// registration). overlayPlayoffBracketNames writes these directly into the
// entrant cells; no DisplayName or roster lookup occurs. Covers both the
// playoffs entrant overwrite (overlayPlayoffBracketNames) and the 3rd Place
// block entrant writes (overlayBracketScores).
func TestBuildResultsWorkbook_EngiPairLabelsInBracketEntrants(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataPlayoffs(t, store, compID, true)
	bracket := startNaginataWith4Players(t, store, eng, compID, true)

	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2)
	for _, m := range sf {
		_, err := eng.RecordMatchResultWithIneligibility(compID, m.ID, &state.MatchResult{
			FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted,
		})
		require.NoError(t, err)
	}

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Collect every non-empty name cell (court cols A and G) below a match or
	// 3rd Place header. Each populated entrant must carry the dash pair label.
	var entrants []string
	for i, row := range rows {
		for _, cell := range row {
			if cell != helper.ThirdPlaceLabel && parseRoundMatchLabel(cell) <= 0 {
				continue
			}
			if i+2 >= len(rows) {
				continue
			}
			nameRow := rows[i+2]
			for _, col := range []int{0, 6} {
				if col < len(nameRow) && strings.HasPrefix(nameRow[col], "Pair") {
					entrants = append(entrants, nameRow[col])
				}
			}
		}
	}
	require.NotEmpty(t, entrants, "expected populated bracket entrant name cells")
	for _, name := range entrants {
		assert.Regexp(t, `^Pair\dA - Pair\dB$`, name,
			"engi entrant must render as 'Member1 - Member2', got %q", name)
	}
}

func TestBuildResultsWorkbook_KachinukiDetailSheet(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Format = state.CompFormatMixed
	comp.TeamMatchType = state.TeamMatchTypeKachinuki
	comp.TeamSize = 3
	require.NoError(t, store.SaveCompetition(comp))

	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "R1M0", SideA: "Ryu", SideB: "Tora",
					Winner: "Tora", Status: state.MatchStatusCompleted,
					Decision: "kachinuki-exhaustion", MatchNumber: 1,
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "Ryu Ichiro", SideB: "Tora Taro", Winner: "Ryu Ichiro", Decision: "fought"},
						{Position: 2, SideA: "Ryu Ichiro", SideB: "Tora Jiro", Winner: "Tora Jiro", Decision: "fought"},
					},
				},
			},
		},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	sheets := f.GetSheetList()
	require.Contains(t, sheets, helper.SheetKachinukiDetail,
		"results workbook must contain the Kachinuki Detail sheet for a kachinuki comp")

	rows, err := f.GetRows(helper.SheetKachinukiDetail)
	require.NoError(t, err)
	var flat string
	for _, row := range rows {
		for _, cell := range row {
			flat += cell + "|"
		}
	}
	assert.Contains(t, flat, "Ryu Ichiro", "bout player names must be in the sheet")
	assert.Contains(t, flat, "Tora Jiro", "bout player names must be in the sheet")
}

func TestBuildResultsWorkbook_FixedFormatNoKachinukiSheet(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.TeamMatchType = state.TeamMatchTypeFixed
	comp.TeamSize = 3
	require.NoError(t, store.SaveCompetition(comp))

	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	assert.NotContains(t, f.GetSheetList(), helper.SheetKachinukiDetail,
		"fixed-format comps must not emit a Kachinuki Detail sheet")
}
