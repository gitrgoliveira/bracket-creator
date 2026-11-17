package export

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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
			SideAID:  "Alice",
			SideB:    "Bob",
			SideBID:  "Bob",
			IpponsA:  []string{"M"},
			IpponsB:  []string{},
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Alice",
			WinnerID: "Alice",
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
			SideAID:  "Alice",
			SideB:    "Bob",
			SideBID:  "Bob",
			IpponsA:  []string{"M", "K"},
			IpponsB:  []string{},
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Alice",
			WinnerID: "Alice",
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

func TestBuildResultsWorkbook_ResultMarksInScoreCells(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

	// Alice beats Bob by kiken-voluntary in encho, decided by hantei. The
	// verdict is domain.HanteiMark inside the WINNER's (Alice's) ippon slice
	// (operator ruling 2026-08-21), not the legacy DecidedByHantei *bool: pool
	// matches have no CSV column for that top-level flag (only sub-bout
	// DecidedByHantei round-trips, via the SubResults JSON cell), and
	// SavePoolMatches's cache-refresh keeps this exact in-memory value without
	// ever running it through NormalizeLegacyHantei, so the legacy field would
	// silently never fold into a mark here.
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alice",
			SideAID:  "Alice",
			SideB:    "Bob",
			SideBID:  "Bob",
			IpponsA:  []string{"M", domain.HanteiMark},
			IpponsB:  []string{},
			Decision: "kiken-voluntary",
			Encho:    &state.EnchoMetadata{PeriodCount: 1},
			Status:   state.MatchStatusCompleted,
			Winner:   "Alice",
			WinnerID: "Alice",
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

	// The middle carries only its one mark ("(E)"); the result marks ride in
	// the competitors' score cells: Alice (winner, scored M, hantei) "M Ht",
	// Bob (withdrew) "Kiken".
	assert.True(t, sheetContainsCell(rows, "(E)"), "the vs cell must carry the (E) middle mark")
	assert.True(t, sheetContainsCell(rows, "M Ht"), "the winner's cell must carry her score plus the Ht mark")
	assert.True(t, sheetContainsCell(rows, "Kiken"), "the withdrawing side's cell must carry the Kiken mark")
	assert.False(t, sheetContainsCell(rows, "Kiken (E) Ht"), "the old composed middle suffix must be gone")
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

// TestBuildResultsWorkbook_MissingTournament_Succeeds guards against
// over-strictness for mp-yuy8 criterion 6: a competition folder with no
// tournament.md at all (the bootstrap window before POST /tournament) must
// still build a results workbook. LoadTournament treats "does not exist" as
// (nil, nil), never an error (internal/state/tournament.go).
func TestBuildResultsWorkbook_MissingTournament_Succeeds(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	require.NoFileExists(t, filepath.Join(dir, "tournament.md"))

	require.NoError(t, store.SavePools(compID, makePools()))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

// TestBuildResultsWorkbook_CorruptTournament_Fails pins mp-yuy8 criterion 6: a
// corrupt (unreadable) tournament.md now aborts BuildResultsWorkbook rather
// than engine.CompetitionCourts silently degrading to the competition's own
// court list. A malformed YAML front matter does NOT make LoadTournament
// error (it falls back to a default Tournament{} -- see
// internal/state/tournament.go's parseFrontMatter branch), so "corrupt" here
// is reproduced the same way as the engine-side test
// (TestExportCompetitionXlsx_CorruptTournament_Fails): tournament.md
// replaced by a directory, forcing a real os.ReadFile error.
func TestBuildResultsWorkbook_CorruptTournament_Fails(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Cup", Courts: []string{"A"}}))
	require.NoError(t, store.SavePools(compID, makePools()))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	tournPath := filepath.Join(dir, "tournament.md")
	require.NoError(t, os.RemoveAll(tournPath))
	require.NoError(t, os.Mkdir(tournPath, 0o755))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.Error(t, err, "a corrupt tournament.md must abort the export rather than silently printing positional court labels")
	assert.Nil(t, data)
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
			SideAID:  "Alice",
			SideB:    "Bob",
			SideBID:  "Bob",
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

// TestBuildResultsWorkbook_DrawExcludesEnchoMark pins the draw-vs-encho
// exclusivity rule: X means a tie and a match that went to encho cannot end
// tied, so stale data carrying both exports the X ALONE — the middle column
// holds exactly one mark and the tie wins. (This test previously guarded the
// opposite, a combined "X (E)" cell, from the era when the middle carried a
// composed suffix.)
func TestBuildResultsWorkbook_DrawExcludesEnchoMark(t *testing.T) {
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
			SideAID:  "Alice",
			SideB:    "Bob",
			SideBID:  "Bob",
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

	assert.True(t, sheetContainsCell(rows, "X"),
		"a draw with stale encho data must export the X marker")
	assert.False(t, sheetContainsCell(rows, "(E)"),
		"a tie can never carry the overtime marker: X wins, (E) is dropped")
	assert.False(t, sheetContainsCell(rows, "X (E)"),
		"the old combined draw+encho cell must be gone")
}

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

// TestStandingMap_IDlessStandingsNeverResolve is the converted twin of the
// deleted TestStandingMap, which pinned standingMap keying id-less legacy
// standings by bare name ("Legacy state without UUIDs: keyed by name"). The
// bc-pnum operator ruling ("a record that carries an id field is resolved by
// id only; an empty id resolves to NOTHING") removed that fallback:
// standingMap now skips inserting any standing whose Player.ID is empty, so
// there is no key at all for a legacy no-UUID standing to be looked up
// under -- not "Alice", not anything else.
func TestStandingMap_IDlessStandingsNeverResolve(t *testing.T) {
	t.Parallel()
	standings := []state.PlayerStanding{
		{Player: domain.Player{Name: "Alice", Dojo: "Dojo Alice"}, Rank: 1},
		{Player: domain.Player{Name: "Bob", Dojo: "Dojo Bob"}, Rank: 2},
	}
	m := standingMap(standings)
	assert.Empty(t, m, "id-less standings must not be inserted under any key")
}

// TestStandingMap_SameNameKeyedByID is the regression test for standingMap
// collapsing same-name participants: two "Sam"s in different dojos must remain
// distinct standings entries because the map keys by participant UUID. A
// row without an id (operator ruling bc-pnum) resolves to nothing: it is
// never inserted, never a name fallback.
func TestStandingMap_SameNameKeyedByID(t *testing.T) {
	t.Parallel()
	standings := []state.PlayerStanding{
		{Player: domain.Player{ID: "id-1", Name: "Sam", Dojo: "North"}, Rank: 1},
		{Player: domain.Player{ID: "id-2", Name: "Sam", Dojo: "South"}, Rank: 4},
		{Player: domain.Player{Name: "Legacy", Dojo: "Dojo Legacy"}, Rank: 3},
	}
	m := standingMap(standings)
	assert.Len(t, m, 2, "same-name players with distinct IDs must not collapse, and the id-less row must not be inserted")
	assert.Equal(t, 1, m["id-1"].Rank)
	assert.Equal(t, 4, m["id-2"].Rank)
	_, ok := m[""]
	assert.False(t, ok, "an id-less row must never be inserted under a \"\" key")
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
		{ID: "Pool A-0", SideA: "Ann", SideAID: "Ann", SideB: "Bea", SideBID: "Bea", Winner: "Ann", WinnerID: "Ann", IpponsA: []string{"M", "K"}, Status: state.MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "Ann", SideAID: "Ann", SideB: "Cody", SideBID: "Cody", Winner: "Ann", WinnerID: "Ann", IpponsA: []string{"D"}, Status: state.MatchStatusCompleted},
		{ID: "Pool A-2", SideA: "Bea", SideAID: "Bea", SideB: "Cody", SideBID: "Cody", Winner: "Cody", WinnerID: "Cody", IpponsB: []string{"M"}, Status: state.MatchStatusCompleted},
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
		{ID: "Pool A-0", SideA: "Alice", SideAID: "Alice", SideB: "Bob", SideBID: "Bob", Winner: "Alice", WinnerID: "Alice", IpponsA: []string{"M", "K"}, Decision: "fought", Status: state.MatchStatusCompleted},
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

// TestScoreCellsCarryOutstandingHansokuTriangle: a completed match can end
// with one UNDISCHARGED foul on a side (fouls %% 2 == 1), and the FIK sheet
// keeps that ▲ on the record next to the offender. The stored ScoreA/ScoreB
// strings used to smuggle it into the bracket cells as the codec's "(H1)"
// suffix; when bc-bmsc removed the strings the export silently lost it. It is
// now drawn as the rulebook's triangle, on the cell's outer edge, by BOTH the
// individual writer (pool + bracket rows) and the team sub-row writer, and an
// EVEN count draws nothing (that foul pair is already the opponent's "H").
func TestScoreCellsCarryOutstandingHansokuTriangle(t *testing.T) {
	t.Parallel()

	t.Run("individual row", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		sheet := helper.SheetPoolMatches
		f.NewSheet(sheet)

		mr := state.MatchResult{
			SideA: "Alice", SideB: "Bob", Winner: "Bob",
			IpponsA: []string{"M"}, HansokuA: 1, // one standing foul on A
			IpponsB: []string{"K", "H"}, HansokuB: 2, // discharged: already B's... A's H to B
			Status: state.MatchStatusCompleted,
		}
		writeScoreRowCells(f, sheet, 1, 5, "M", "KH", mr)
		left, _ := f.GetCellValue(sheet, "B5")
		right, _ := f.GetCellValue(sheet, "F5")
		assert.Equal(t, "KH", left, "an even count is already discharged into the H ippon: no triangle")
		assert.Equal(t, "M ▲", right, "A (Aka) scores on the right, so its standing foul rides that cell's outer edge")
	})

	// Shiro's standing foul rides the LEFT cell's outer edge, so it comes
	// before a score there, the mirror of Aka's after it on the right.
	t.Run("individual row, Shiro's foul before a score", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		sheet := helper.SheetPoolMatches
		f.NewSheet(sheet)

		mr := state.MatchResult{
			SideA: "Alice", SideB: "Bob", Winner: "Bob",
			IpponsA: []string{"M"}, HansokuA: 2, // discharged into B's H
			IpponsB: []string{"K", "H"}, HansokuB: 1, // one standing foul on B
			Status: state.MatchStatusCompleted,
		}
		writeScoreRowCells(f, sheet, 1, 5, "M", "KH", mr)
		left, _ := f.GetCellValue(sheet, "B5")
		right, _ := f.GetCellValue(sheet, "F5")
		assert.Equal(t, "▲ KH", left, "B (Shiro) scores on the left, so its standing foul comes first")
		assert.Equal(t, "M", right)
	})

	t.Run("team sub-bout row, Shiro's foul before a score", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		sheet := helper.SheetPoolMatches
		f.NewSheet(sheet)

		subs := []state.SubMatchResult{
			{Position: 1, SideA: "Ann", SideB: "Ben", Winner: "Ben",
				IpponsA: []string{"M"}, HansokuA: 2, IpponsB: []string{"K", "H"}, HansokuB: 1},
		}
		writeTeamSubMatchScores(f, sheet, 1, 5, subs, 3, "", "", domain.MatchSideNone)
		left, _ := f.GetCellValue(sheet, "B5")
		assert.Equal(t, "▲ KH", left, "Shiro's standing foul comes before its score")
	})

	t.Run("team sub-bout row", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		sheet := helper.SheetPoolMatches
		f.NewSheet(sheet)

		subs := []state.SubMatchResult{
			{Position: 1, SideA: "Ann", SideB: "Ben", Winner: "Ann",
				IpponsA: []string{"M", "K"}, HansokuB: 1},
		}
		writeTeamSubMatchScores(f, sheet, 1, 5, subs, 3, "", "", domain.MatchSideNone)
		left, _ := f.GetCellValue(sheet, "B5")
		right, _ := f.GetCellValue(sheet, "F5")
		assert.Equal(t, "▲", left, "a bout the offender lost 0-2 still records the standing foul")
		assert.Equal(t, "MK", right)
	})

	// bc-dnst: a FIXED-ORDER bout records no fighter name, so its row names
	// only the TEAM in Winner. Without the encounter's own side names the
	// attribution collapses to MatchSideNone and the row silently loses both
	// the default-win maru and the Fus. mark, while this same sheet's IV/PW
	// summary still counts the bout. Nothing covered this: the cases above all
	// hand-write fighter names onto the sub rows.
	t.Run("fixed-order row, no fighter names: the default win still prints", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		sheet := helper.SheetPoolMatches
		f.NewSheet(sheet)

		subs := []state.SubMatchResult{
			{Position: 1, SideA: "", SideB: "", Winner: "Tora A", Decision: "fusensho"},
		}
		writeTeamSubMatchScores(f, sheet, 1, 5, subs, 3, "Tora A", "Kenshi B", domain.MatchSideNone)
		// The winner is SideA (Aka), whose cell is the RIGHT one.
		right, _ := f.GetCellValue(sheet, "F5")
		assert.Contains(t, right, "Fus.", "the no-show mark must name the winning side")
		assert.Contains(t, right, "○", "and the default win must still award its maru")
	})

	// bc-tmfn follow-up: a bout with NO result of its own (padded,
	// state.PadDefaultWinBoutPositions) on a match a default-win ruling
	// closed shows the maru via the credit parameter, but -- unlike the
	// fixed-order fusensho row above, whose OWN Decision drives the mark --
	// carries no per-row Kiken/Fus. mark: that mark names the withdrawer and
	// already rides the match-level summary row (writeTeamSummaryCells).
	t.Run("credited unfought row: maru with no per-row mark", func(t *testing.T) {
		f := excelize.NewFile()
		defer f.Close()
		sheet := helper.SheetPoolMatches
		f.NewSheet(sheet)

		subs := []state.SubMatchResult{{Position: 1}}
		writeTeamSubMatchScores(f, sheet, 1, 5, subs, 3, "Tora A", "Kenshi B", domain.MatchSideA)
		left, _ := f.GetCellValue(sheet, "B5")
		right, _ := f.GetCellValue(sheet, "F5")
		// The credited side is SideA (Aka), whose cell is the RIGHT one.
		assert.Equal(t, "○○", right, "the credited side's maru, and nothing else")
		assert.Empty(t, left, "the other side's cell stays empty")
	})
}
