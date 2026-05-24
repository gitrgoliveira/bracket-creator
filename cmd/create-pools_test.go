package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

func TestNewCreatePoolCmd(t *testing.T) {
	t.Parallel()

	cmd := newCreatePoolCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "create-pools", cmd.Use)
	assert.Equal(t, "creates Pool brackets", cmd.Short)
}

func TestCreatePoolCmdFlags(t *testing.T) {
	t.Parallel()

	cmd := newCreatePoolCmd()

	// Test required flags
	fileFlag := cmd.PersistentFlags().Lookup("file")
	assert.NotNil(t, fileFlag)
	assert.Equal(t, "f", fileFlag.Shorthand)

	outputFlag := cmd.PersistentFlags().Lookup("output")
	assert.NotNil(t, outputFlag)
	assert.Equal(t, "o", outputFlag.Shorthand)

	// Test optional flags
	playersFlag := cmd.Flags().Lookup("players")
	assert.NotNil(t, playersFlag)
	assert.Equal(t, "p", playersFlag.Shorthand)
	assert.Equal(t, "3", playersFlag.DefValue)

	maxPlayersFlag := cmd.Flags().Lookup("max-players")
	assert.NotNil(t, maxPlayersFlag)
	assert.Equal(t, "m", maxPlayersFlag.Shorthand)
	assert.Equal(t, "0", maxPlayersFlag.DefValue)

	winnersFlag := cmd.Flags().Lookup("pool-winners")
	assert.NotNil(t, winnersFlag)
	assert.Equal(t, "w", winnersFlag.Shorthand)
	assert.Equal(t, "2", winnersFlag.DefValue)

	roundRobinFlag := cmd.Flags().Lookup("round-robin")
	assert.NotNil(t, roundRobinFlag)
	assert.Equal(t, "r", roundRobinFlag.Shorthand)

	zekkenFlag := cmd.Flags().Lookup("with-zekken-name")
	assert.NotNil(t, zekkenFlag)
	assert.Equal(t, "z", zekkenFlag.Shorthand)

	singleTreeFlag := cmd.Flags().Lookup("single-tree")
	assert.NotNil(t, singleTreeFlag)

	teamMatchesFlag := cmd.Flags().Lookup("team-matches")
	assert.NotNil(t, teamMatchesFlag)
	assert.Equal(t, "t", teamMatchesFlag.Shorthand)
	assert.Equal(t, "0", teamMatchesFlag.DefValue)

	determinedFlag := cmd.PersistentFlags().Lookup("determined")
	assert.NotNil(t, determinedFlag)
	assert.Equal(t, "d", determinedFlag.Shorthand)
}

func TestCreatePools_BasicSuccess(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter:   writer,
		outputPath:     "dummy.xlsx",
		numPlayers:     3,
		poolWinners:    2,
		withZekkenName: false,
		determined:     true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	// Template file should be found in embedded resources
	require.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
	// Buffer may be empty if template is missing, which is OK for this test
}

func TestCreatePools_NumPoolsZero(t *testing.T) {
	var b bytes.Buffer
	o := &poolOptions{
		outputWriter: bufio.NewWriter(&b),
		numPlayers:   10,
	}
	// Use 3 players (minimum valid entries but not enough for a pool of 10)
	err := o.createPools([]string{"A,D1", "B,D2", "C,D3"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "number of entries must be greater than requested players in pool")
}

func TestCreatePools_MaxPlayersMode(t *testing.T) {
	var b bytes.Buffer
	o := &poolOptions{
		outputWriter: bufio.NewWriter(&b),
		maxPlayers:   4,
		poolWinners:  2,
		courts:       2,
	}
	err := o.createPools([]string{"A,D1", "B,D2", "C,D3", "D,D4", "E,D5"})
	assert.NoError(t, err)
}

func TestCreatePools_NumberPrefix(t *testing.T) {
	var b bytes.Buffer
	o := &poolOptions{
		outputWriter: bufio.NewWriter(&b),
		numPlayers:   3,
		poolWinners:  2,
		courts:       2,
		numberPrefix: "K",
	}
	err := o.createPools([]string{"A,D1", "B,D2", "C,D3", "D,D4", "E,D5", "F,D6"})
	assert.NoError(t, err)
}

func TestCreatePools_InvalidCourts(t *testing.T) {
	// Create a temporary input file
	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	_, err = tmpInput.WriteString("A,D1\nB,D2\nC,D3\n")
	require.NoError(t, err)
	tmpInput.Close()

	o := &poolOptions{
		filePath:   tmpInput.Name(),
		outputPath: "dummy.xlsx",
		courts:     30,
	}
	err = o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "courts must be <= 26")
}

func TestCreatePools_WithZekkenNames(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter:   writer,
		outputPath:     "dummy.xlsx",
		numPlayers:     3,
		poolWinners:    2,
		withZekkenName: true,
		determined:     true,
	}

	entries := []string{
		"John Doe,Dojo1,Johnny",
		"Jane Smith,Dojo2,Janey",
		"Alice,Dojo3,Ali",
		"Bob,Dojo4,Bobby",
		"Charlie,Dojo5,Chuck",
		"Dave,Dojo6,Davey",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_RoundRobin(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   4,
		poolWinners:  2,
		roundRobin:   true,
		determined:   true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
		"Eve,Dojo7",
		"Frank,Dojo8",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

// TestCreatePools_RoundRobinSinglePoolOf8_RankingResolves is an end-to-end
// regression check for a single pool of 8 players, single shiaijo, full
// round robin, individual matches. We populate one ippon mark per match
// for player 1 (Kevin) so they win all 7 of their bouts, then ask Excel
// to compute the "Ranking" lookup cell that the elimination bracket
// reads from. It must resolve to Kevin's name; if the round-robin layout
// causes the data ranges in the IFERROR(INDEX(...MATCH(1,...))) formula
// to drift off the actual results table, this assertion fires.
func TestCreatePools_RoundRobinSinglePoolOf8_RankingResolves(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "rr-pool-8-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	writer := bufio.NewWriter(tmpFile)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   tmpFile.Name(),
		numPlayers:   8, // single pool of 8
		poolWinners:  2,
		roundRobin:   true,
		determined:   true,
		courts:       1, // single shiaijo
	}

	playerNames := []string{
		"Kevin Clark", "Luke Rodriguez", "Michael Lewis", "Nathan Lee",
		"Oliver Walker", "Paul Hall", "Quentin Allen", "Robert Young",
	}
	entries := make([]string, len(playerNames))
	for i, n := range playerNames {
		entries[i] = fmt.Sprintf("%s,Team %c", n, 'A'+i)
	}

	require.NoError(t, o.createPools(entries))
	require.NoError(t, writer.Flush())
	require.NoError(t, tmpFile.Close())

	f, err := excelize.OpenFile(tmpFile.Name())
	require.NoError(t, err)
	defer f.Close()

	// Disable sheet protection so we can write into score cells.
	for _, sheet := range f.GetSheetList() {
		_ = f.UnprotectSheet(sheet)
	}

	const sheet = "Pool Matches"

	// Stop scanning at the "Results" header — rows below it are the
	// results-table aggregator formulas, NOT match rows, and writing
	// into them would clobber the W/L/PW/PL formulas we want to test.
	var resultsRow int
	for r := 1; r < 200; r++ {
		v, _ := f.GetCellValue(sheet, fmt.Sprintf("A%d", r))
		if v == "Results" {
			resultsRow = r
			break
		}
	}
	require.NotZero(t, resultsRow, "could not find Results header")

	// Walk the match rows in column G (red side) / column A (white side)
	// of the Pool Matches sheet and, whenever Kevin (data!$B$3) is one
	// of the two sides, write an ippon mark into the score column for
	// his side so he wins that bout. Leave all other matches blank.
	kevinDataRef := "'data'!$B$3"
	// Score columns for an 8-col-per-court layout starting at column A:
	// A=name(white), B=leftVictories, C=leftPoints, D=middle/vs,
	// E=rightPoints, F=rightVictories, G=name(red).
	for row := 4; row < resultsRow; row++ {
		whiteFormula, _ := f.GetCellFormula(sheet, fmt.Sprintf("A%d", row))
		redFormula, _ := f.GetCellFormula(sheet, fmt.Sprintf("G%d", row))

		switch {
		case strings.Contains(whiteFormula, kevinDataRef):
			// Kevin is on the white side — left scores B/C win.
			require.NoError(t, f.SetCellValue(sheet, fmt.Sprintf("B%d", row), "M"))
		case strings.Contains(redFormula, kevinDataRef):
			// Kevin is on the red side — right scores E/F win.
			require.NoError(t, f.SetCellValue(sheet, fmt.Sprintf("F%d", row), "M"))
		}
	}

	// Locate the Ranking-header row in column G; the rank-1 lookup formula
	// is the row immediately below it.
	var rankingHeaderRow int
	for r := 1; r < 200; r++ {
		val, _ := f.GetCellValue(sheet, fmt.Sprintf("G%d", r))
		if val == "Ranking" {
			rankingHeaderRow = r
			break
		}
	}
	require.NotZero(t, rankingHeaderRow, "could not find Ranking header")

	// Kevin should be rank 1; the rank-1 ranking lookup must resolve to him.
	rank1Cell := fmt.Sprintf("G%d", rankingHeaderRow+1)
	got, err := f.CalcCellValue(sheet, rank1Cell)
	require.NoError(t, err, "CalcCellValue %s", rank1Cell)
	assert.Equal(t, "Kevin Clark", got,
		"single round-robin pool of 8: rank-1 ranking cell %s should resolve to the player who won all 7 matches",
		rank1Cell)

	// No formula in any sheet may have a leading '=' — OOXML <f> bodies
	// that start with '=' are tolerated by Excel but rejected by Google
	// Sheets and Apple Numbers (mp-x7h).
	for _, s := range f.GetSheetList() {
		rows, _ := f.GetRows(s)
		for r := range rows {
			for c := range rows[r] {
				cell, _ := excelize.CoordinatesToCellName(c+1, r+1)
				form, _ := f.GetCellFormula(s, cell)
				if form == "" {
					continue
				}
				assert.NotEqual(t, byte('='), form[0],
					"sheet=%s cell=%s formula starts with '=': %q (breaks Google Sheets / Apple Numbers)", s, cell, form)
			}
		}
	}

	// And the elimination bracket's Pool A-1st CONCATENATE formula must
	// reference the rank-1 cell (regression for mp-c5c — empty-cell ref).
	const elimSheet = "Elimination Matches"
	var poolARefCell string
	for r := 1; r < 50; r++ {
		for _, col := range []string{"A", "G"} {
			form, _ := f.GetCellFormula(elimSheet, fmt.Sprintf("%s%d", col, r))
			if strings.Contains(form, `"Pool A-1st `) {
				// extract the trailing cell reference like 'Pool Matches'!G45
				idx := strings.Index(form, "'Pool Matches'!")
				if idx >= 0 {
					rest := form[idx+len("'Pool Matches'!"):]
					end := strings.Index(rest, ")")
					if end > 0 {
						poolARefCell = rest[:end]
					}
				}
			}
		}
	}
	require.NotEmpty(t, poolARefCell, "elimination bracket should reference Pool A-1st cell")
	assert.Equal(t, rank1Cell, poolARefCell,
		"elimination bracket Pool A-1st must point at the rank-1 ranking cell")
}

// TestCreatePools_TeamsOf3RoundRobin_PointsWonAndLost is a regression test for
// the team-summary Points-Won/Points-Lost (PW/PL) bug. With teams of 3 and a
// full round robin (3 teams, single shiaijo, single pool), the SUMPRODUCT
// formulas in buildTeamPointsFormula / buildTeamWinnersFormula passed cell
// ranges through SUBSTITUTE/LEN — that only iterates as an array in Excel
// 365 with dynamic-array semantics. Legacy Excel, Google Sheets, and Apple
// Numbers collapse the range to the first cell, so PW and IV (and the
// derived rank/score) were stuck at 1 instead of summing all 3 sub-bouts.
// Fix expands the aggregation to per-row LEN(SUBSTITUTE(...)) terms (mp-1xe).
func TestCreatePools_TeamsOf3RoundRobin_PointsWonAndLost(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "teams3-rr-pool-3-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	writer := bufio.NewWriter(tmpFile)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   tmpFile.Name(),
		numPlayers:   3,
		poolWinners:  2,
		roundRobin:   true,
		determined:   true,
		courts:       1,
		teamMatches:  3,
	}

	entries := []string{
		"Alice Adams,Team Alpha",
		"Bob Adams,Team Alpha",
		"Charlie Adams,Team Alpha",
		"Dave Brown,Team Bravo",
		"Eve Brown,Team Bravo",
		"Frank Brown,Team Bravo",
		"Grace Carter,Team Charlie",
		"Henry Carter,Team Charlie",
		"Iris Carter,Team Charlie",
	}

	require.NoError(t, o.createPools(entries))
	require.NoError(t, writer.Flush())
	require.NoError(t, tmpFile.Close())

	f, err := excelize.OpenFile(tmpFile.Name())
	require.NoError(t, err)
	defer f.Close()

	for _, s := range f.GetSheetList() {
		_ = f.UnprotectSheet(s)
	}

	const sheet = "Pool Matches"
	alphaRef := "'data'!$B$3"

	// Find Alpha's team-match summary rows — column A or G resolves to
	// Alpha and the row below contains the "1" sub-bout label.
	var summaryRows []int
	for r := 1; r < 200; r++ {
		whiteFormula, _ := f.GetCellFormula(sheet, fmt.Sprintf("A%d", r))
		redFormula, _ := f.GetCellFormula(sheet, fmt.Sprintf("G%d", r))
		if whiteFormula != alphaRef && redFormula != alphaRef {
			continue
		}
		next, _ := f.GetCellValue(sheet, fmt.Sprintf("A%d", r+1))
		if next != "1" {
			continue
		}
		summaryRows = append(summaryRows, r)
	}
	require.Len(t, summaryRows, 2, "expected 2 team matches for Alpha in a round-robin pool of 3")

	// Award Alpha 1 ippon ("M") in every sub-bout (3 per match × 2).
	for _, sr := range summaryRows {
		redFormula, _ := f.GetCellFormula(sheet, fmt.Sprintf("G%d", sr))
		col := "B" // Alpha is white — write into leftVictories column.
		if redFormula == alphaRef {
			col = "E" // Alpha is red — write into rightPoints column.
		}
		for sub := sr + 1; sub <= sr+3; sub++ {
			require.NoError(t, f.SetCellValue(sheet, fmt.Sprintf("%s%d", col, sub), "M"))
		}
	}

	// Find Alpha's IV/IL/IT/PW/PL row — column A resolves to Alpha,
	// next row's column A is NOT "1", and column E has a formula
	// (PW). The earlier Alpha row in the W/L/T results table has no
	// formula in column E.
	var alphaPwRow int
	for r := 1; r < 200; r++ {
		v, _ := f.CalcCellValue(sheet, fmt.Sprintf("A%d", r))
		if v != "Alice Adams" {
			continue
		}
		next, _ := f.GetCellValue(sheet, fmt.Sprintf("A%d", r+1))
		if next == "1" {
			continue // sub-bout block, not a results row
		}
		eForm, _ := f.GetCellFormula(sheet, fmt.Sprintf("E%d", r))
		if eForm == "" {
			continue // W/L/T row — no PW formula
		}
		alphaPwRow = r
		break
	}
	require.NotZero(t, alphaPwRow, "could not find Alpha's PW/PL row")

	pw, err := f.CalcCellValue(sheet, fmt.Sprintf("E%d", alphaPwRow))
	require.NoError(t, err)
	pl, err := f.CalcCellValue(sheet, fmt.Sprintf("F%d", alphaPwRow))
	require.NoError(t, err)

	// Alpha won 3 ippons per match × 2 matches = 6 PW, 0 PL.
	assert.Equal(t, "6", pw, "Team Alpha PW must total 6 (3 ippons × 2 matches); got %q", pw)
	assert.Equal(t, "0", pl, "Team Alpha PL must be 0 (Alpha won every sub-bout); got %q", pl)
}

func TestCreatePools_SingleTree(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   3,
		poolWinners:  2,
		singleTree:   true,
		determined:   true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_WithTeamMatches(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   3,
		poolWinners:  2,
		teamMatches:  2,
		determined:   true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_WithSeeds(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	seedAssignments := []domain.SeedAssignment{
		{Name: "John Doe", SeedRank: 1},
		{Name: "Jane Smith", SeedRank: 2},
	}

	o := &poolOptions{
		outputWriter:    writer,
		outputPath:      "dummy.xlsx",
		numPlayers:      3,
		poolWinners:     2,
		SeedAssignments: seedAssignments,
		determined:      true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_MaxPlayersValidation(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		maxPlayers:   3,
		poolWinners:  2,
		determined:   true,
	}

	// 3 entries with max-players 3 and 2 winners is the smallest valid config.
	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestCreatePools_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		entries       []string
		numPlayers    int
		poolWinners   int
		expectedError string
	}{
		{
			name:          "too few entries for winners",
			entries:       []string{"John Doe,Dojo1"},
			numPlayers:    3,
			poolWinners:   2,
			expectedError: "number of entries must be higher than number of winners per pool",
		},
		{
			name:          "too few entries for pool size",
			entries:       []string{"John Doe,Dojo1", "Jane Smith,Dojo2"},
			numPlayers:    3,
			poolWinners:   2,
			expectedError: "number of entries must be greater than requested players in pool",
		},
		{
			name:          "invalid pool size",
			entries:       []string{"John Doe,Dojo1", "Jane Smith,Dojo2", "Alice,Dojo3"},
			numPlayers:    1,
			poolWinners:   2,
			expectedError: "number of players per pool must be greater than 1",
		},
		{
			name:          "winners >= players",
			entries:       []string{"John Doe,Dojo1", "Jane Smith,Dojo2", "Alice,Dojo3"},
			numPlayers:    3,
			poolWinners:   3,
			expectedError: "number of pool winners must be less than number of players per pool",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var b bytes.Buffer
			writer := bufio.NewWriter(&b)

			o := &poolOptions{
				outputWriter: writer,
				outputPath:   "dummy.xlsx",
				numPlayers:   tt.numPlayers,
				poolWinners:  tt.poolWinners,
			}

			err := o.createPools(tt.entries)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestCreatePools_RejectsDuplicates(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   3,
		poolWinners:  2,
		determined:   true,
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"John Doe,Dojo1", // duplicate
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Jane Smith,Dojo2", // duplicate
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate participant entries")
	assert.Contains(t, err.Error(), "John Doe,Dojo1")
	assert.Contains(t, err.Error(), "Jane Smith,Dojo2")
}

func TestCreatePools_ShuffleWhenNotDetermined(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   3,
		poolWinners:  2,
		determined:   false, // Should shuffle
	}

	entries := []string{
		"John Doe,Dojo1",
		"Jane Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
		"Charlie,Dojo5",
		"Dave,Dojo6",
	}

	err := o.createPools(entries)
	assert.NoError(t, err)
	err = writer.Flush()
	assert.NoError(t, err)
}

func TestPoolOptionsRun_FileNotFound(t *testing.T) {
	o := &poolOptions{
		filePath:   "nonexistent.csv",
		outputPath: "output.xlsx",
	}

	err := o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read entries from file")
}

func TestPoolOptionsRun_EmptyFile(t *testing.T) {
	// Create a temporary empty file
	tmpFile, err := os.CreateTemp("", "empty-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	o := &poolOptions{
		filePath:   tmpFile.Name(),
		outputPath: "output.xlsx",
	}

	err = o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no entries found in file")
}

func TestPoolOptionsRun_WithSeeds(t *testing.T) {
	// Create a temporary input file
	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	_, err = tmpInput.WriteString("John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4\nCharlie,Dojo5\nDave,Dojo6\n")
	require.NoError(t, err)
	tmpInput.Close()

	// Create a temporary seeds file
	tmpSeeds, err := os.CreateTemp("", "seeds-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpSeeds.Name())
	_, err = tmpSeeds.WriteString("Name,Rank\nJohn Doe,1\nJane Smith,2\n")
	require.NoError(t, err)
	tmpSeeds.Close()

	// Create a temporary output file
	tmpOutput, err := os.CreateTemp("", "output-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpOutput.Name())
	tmpOutput.Close()

	o := &poolOptions{
		filePath:    tmpInput.Name(),
		outputPath:  tmpOutput.Name(),
		seedsPath:   tmpSeeds.Name(),
		numPlayers:  3,
		poolWinners: 2,
		courts:      2,
		determined:  true,
	}

	err = o.run(nil, nil)
	assert.NoError(t, err)
}

func TestPoolOptionsRun_InvalidSeeds(t *testing.T) {
	// Create a temporary input file
	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	_, err = tmpInput.WriteString("John Doe,Dojo1\n")
	require.NoError(t, err)
	tmpInput.Close()

	// Create a temporary invalid seeds file
	tmpSeeds, err := os.CreateTemp("", "seeds-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpSeeds.Name())
	_, err = tmpSeeds.WriteString("Invalid Format\n")
	require.NoError(t, err)
	tmpSeeds.Close()

	o := &poolOptions{
		filePath:   tmpInput.Name(),
		outputPath: "dummy.xlsx",
		seedsPath:  tmpSeeds.Name(),
		courts:     2,
	}

	err = o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse seeds file")
}

func TestCreatePoolCmdMutuallyExclusiveFlags(t *testing.T) {
	t.Parallel()

	cmd := newCreatePoolCmd()
	// Provide both --players and --max-players to trigger the mutual-exclusion check.
	cmd.SetArgs([]string{"--file", "/tmp/does-not-exist.csv", "--output", "/tmp/out.xlsx", "--players", "3", "--max-players", "4"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "none of the others can be")
}

func TestCreatePools_MaxMode_ValidationErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		entries       []string
		maxPlayers    int
		poolWinners   int
		expectedError string
	}{
		{
			name:          "max mode requires at least 2 entries",
			entries:       []string{"John Doe,Dojo1"},
			maxPlayers:    3,
			poolWinners:   0,
			expectedError: "number of entries must be at least 2",
		},
		{
			name:          "max mode rejects pool size below 2",
			entries:       []string{"A,D1", "B,D2", "C,D3"},
			maxPlayers:    1,
			poolWinners:   0,
			expectedError: "number of players per pool must be greater than 1",
		},
		{
			name:          "max mode rejects winners >= max",
			entries:       []string{"A,D1", "B,D2", "C,D3", "D,D4"},
			maxPlayers:    3,
			poolWinners:   3,
			expectedError: "number of pool winners must be less than number of players per pool",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var b bytes.Buffer
			writer := bufio.NewWriter(&b)

			o := &poolOptions{
				outputWriter: writer,
				outputPath:   "dummy.xlsx",
				maxPlayers:   tt.maxPlayers,
				poolWinners:  tt.poolWinners,
				determined:   true,
			}

			err := o.createPools(tt.entries)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestCreatePools_MaxMode_BalancedDistribution(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		maxPlayers:   3,
		poolWinners:  2,
		determined:   true,
	}

	// 10 entries with max 3 players per pool -> 4 pools (3, 3, 2, 2).
	entries := []string{
		"P1,D1", "P2,D2", "P3,D3", "P4,D4", "P5,D5",
		"P6,D6", "P7,D7", "P8,D8", "P9,D9", "P10,D10",
	}

	err := o.createPools(entries)
	require.NoError(t, err)
	require.NoError(t, writer.Flush())
}

func TestCreatePools_ServeMutuallyExclusiveModes(t *testing.T) {
	t.Parallel()

	// Sanity check: the option struct allows only one of numPlayers/maxPlayers
	// to be effective. Cmd-level enforcement is via cobra.MarkFlagsMutuallyExclusive,
	// while serve.go selects between the two based on poolSizeMode.
	o := &poolOptions{
		outputPath:  "dummy.xlsx",
		maxPlayers:  3,
		numPlayers:  10, // numPlayers should be ignored when maxPlayers > 0
		poolWinners: 2,
		determined:  true,
	}

	var b bytes.Buffer
	o.outputWriter = bufio.NewWriter(&b)

	// 4 entries with max 3 should produce 2 pools (2, 2). Without isMax this
	// would be 0 pools (4/10) and would have errored.
	err := o.createPools([]string{"A,D1", "B,D2", "C,D3", "D,D4"})
	require.NoError(t, err)
}

func TestCreatePools_WithMaxPlayersAndSeeds(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		maxPlayers:   3,
		poolWinners:  2,
		determined:   true,
		SeedAssignments: []domain.SeedAssignment{
			{Name: "P1", SeedRank: 1},
			{Name: "P2", SeedRank: 2},
		},
	}

	entries := []string{
		"P1,D1", "P2,D2", "P3,D3", "P4,D4", "P5,D5",
		"P6,D6", "P7,D7", "P8,D8",
	}

	err := o.createPools(entries)
	require.NoError(t, err)
	require.NoError(t, writer.Flush())
}

// TestPoolBoundsForSubtree verifies that the court-aware pool slicing never
// assigns pools from one court to a subtree page that belongs to another court.
// The critical regression is the "17 pools, 2 courts, 4 subtrees" case: the old
// naive i*poolsPerSubtree slicing let subtree 1 span pools from both court A
// (indices 0-8) and court B (index 9+).
func TestPoolBoundsForSubtree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		numPools    int
		numCourts   int
		numSubtrees int
		subtreeIdx  int
		wantStart   int
		wantEnd     int
	}{
		// Regression case: 17 pools unevenly split across 2 courts (9 + 8),
		// each court has 2 subtree pages.
		// Court A occupies pools[0:9], Court B occupies pools[9:17].
		{name: "17p_2c_4s_tree0", numPools: 17, numCourts: 2, numSubtrees: 4, subtreeIdx: 0, wantStart: 0, wantEnd: 5},
		{name: "17p_2c_4s_tree1", numPools: 17, numCourts: 2, numSubtrees: 4, subtreeIdx: 1, wantStart: 5, wantEnd: 9},
		{name: "17p_2c_4s_tree2", numPools: 17, numCourts: 2, numSubtrees: 4, subtreeIdx: 2, wantStart: 9, wantEnd: 13},
		{name: "17p_2c_4s_tree3", numPools: 17, numCourts: 2, numSubtrees: 4, subtreeIdx: 3, wantStart: 13, wantEnd: 17},

		// Even split: 16 pools, 2 courts (8 each), 4 subtrees (2 per court).
		{name: "16p_2c_4s_tree0", numPools: 16, numCourts: 2, numSubtrees: 4, subtreeIdx: 0, wantStart: 0, wantEnd: 4},
		{name: "16p_2c_4s_tree1", numPools: 16, numCourts: 2, numSubtrees: 4, subtreeIdx: 1, wantStart: 4, wantEnd: 8},
		{name: "16p_2c_4s_tree2", numPools: 16, numCourts: 2, numSubtrees: 4, subtreeIdx: 2, wantStart: 8, wantEnd: 12},
		{name: "16p_2c_4s_tree3", numPools: 16, numCourts: 2, numSubtrees: 4, subtreeIdx: 3, wantStart: 12, wantEnd: 16},

		// Single court: all pools belong to the same block.
		{name: "6p_1c_2s_tree0", numPools: 6, numCourts: 1, numSubtrees: 2, subtreeIdx: 0, wantStart: 0, wantEnd: 3},
		{name: "6p_1c_2s_tree1", numPools: 6, numCourts: 1, numSubtrees: 2, subtreeIdx: 1, wantStart: 3, wantEnd: 6},

		// One page per court (no multi-page case, but must still be correct).
		{name: "6p_2c_2s_tree0", numPools: 6, numCourts: 2, numSubtrees: 2, subtreeIdx: 0, wantStart: 0, wantEnd: 3},
		{name: "6p_2c_2s_tree1", numPools: 6, numCourts: 2, numSubtrees: 2, subtreeIdx: 1, wantStart: 3, wantEnd: 6},

		// 5 pools, 2 courts: court A gets 3 pools (0-2), court B gets 2 (3-4).
		{name: "5p_2c_4s_tree0", numPools: 5, numCourts: 2, numSubtrees: 4, subtreeIdx: 0, wantStart: 0, wantEnd: 2},
		{name: "5p_2c_4s_tree1", numPools: 5, numCourts: 2, numSubtrees: 4, subtreeIdx: 1, wantStart: 2, wantEnd: 3},
		{name: "5p_2c_4s_tree2", numPools: 5, numCourts: 2, numSubtrees: 4, subtreeIdx: 2, wantStart: 3, wantEnd: 4},
		{name: "5p_2c_4s_tree3", numPools: 5, numCourts: 2, numSubtrees: 4, subtreeIdx: 3, wantStart: 4, wantEnd: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotStart, gotEnd := poolBoundsForSubtree(tt.numPools, tt.numCourts, tt.numSubtrees, tt.subtreeIdx)
			assert.Equal(t, tt.wantStart, gotStart, "start")
			assert.Equal(t, tt.wantEnd, gotEnd, "end")

			// Verify the slice lies strictly within the owning court's block.
			pagesPerCourt := tt.numSubtrees / tt.numCourts
			courtIdx := tt.subtreeIdx / pagesPerCourt
			if courtIdx >= tt.numCourts {
				courtIdx = tt.numCourts - 1
			}
			floor := tt.numPools / tt.numCourts
			extra := tt.numPools % tt.numCourts
			courtStart := courtIdx*floor + min(courtIdx, extra)
			courtSize := floor
			if courtIdx < extra {
				courtSize++
			}
			assert.GreaterOrEqual(t, gotStart, courtStart, "start must be within court block")
			assert.LessOrEqual(t, gotEnd, courtStart+courtSize, "end must be within court block")
		})
	}
}

// TestPoolBoundsForSubtree_DegenerateInputs guards against divide-by-zero
// when SubdivideTree returns fewer subtrees than expected for very small
// brackets (e.g. courts > numSubtrees).
func TestPoolBoundsForSubtree_DegenerateInputs(t *testing.T) {
	t.Parallel()

	// numSubtrees < numCourts: pagesPerCourt would be 0 — must not panic.
	start, end := poolBoundsForSubtree(2, 4, 1, 0)
	assert.GreaterOrEqual(t, start, 0)
	assert.GreaterOrEqual(t, end, start)

	// Zero courts/subtrees: should return (0, 0) without panicking.
	s, e := poolBoundsForSubtree(0, 0, 0, 0)
	assert.Equal(t, 0, s)
	assert.Equal(t, 0, e)
}

// TestCreatePools_CourtsExceedNumPages exercises the path where the user
// asks for more courts than would otherwise be filled by the bracket size,
// which forces numPages to be bumped to NextPow2(courts). The page-per-court
// invariant must hold (no divide-by-zero, no out-of-range court labels).
func TestCreatePools_CourtsExceedNumPages(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	// 8 finalists fit on a single tree, but with 4 courts we need >=4 pages.
	o := &poolOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numPlayers:   3,
		poolWinners:  2,
		courts:       4,
		determined:   true,
	}

	entries := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		entries = append(entries, fmt.Sprintf("P%02d,D%d", i, i))
	}

	err := o.createPools(entries)
	require.NoError(t, err)
	require.NoError(t, writer.Flush())
}

// TestCreatePools_RejectsTooManyCourts ensures the CLI rejects a court
// count above MaxCourts (26) instead of silently truncating.
func TestCreatePools_RejectsTooManyCourts(t *testing.T) {
	t.Parallel()

	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	_, err = tmpInput.WriteString("A,D1\nB,D2\nC,D3\nD,D4\nE,D5\nF,D6\n")
	require.NoError(t, err)
	require.NoError(t, tmpInput.Close())

	tmpOutput, err := os.CreateTemp("", "out-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpOutput.Name())
	require.NoError(t, tmpOutput.Close())

	o := &poolOptions{
		filePath:    tmpInput.Name(),
		outputPath:  tmpOutput.Name(),
		numPlayers:  3,
		poolWinners: 2,
		courts:      30,
		determined:  true,
	}

	err = o.run(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "courts must be <= 26")
}

// TestCreatePoolCmd_SeedsFlagOnLocalFlags confirms --seeds is registered on
// Flags() (not PersistentFlags()), matching the rest of the create-pools
// flag set.
func TestCreatePoolCmd_SeedsFlagOnLocalFlags(t *testing.T) {
	t.Parallel()

	cmd := newCreatePoolCmd()
	assert.NotNil(t, cmd.Flags().Lookup("seeds"))
	assert.Nil(t, cmd.PersistentFlags().Lookup("seeds"),
		"--seeds must live on Flags() so it is local to create-pools")
}
