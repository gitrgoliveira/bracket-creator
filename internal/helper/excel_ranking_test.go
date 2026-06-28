package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

func findResultsHeader(f *excelize.File, sheet string, courtIdx int) (int, error) {
	startCol := 1 + courtIdx*8
	colName := mustColumnName(startCol)

	// Scan first 100 rows
	for r := 1; r <= 100; r++ {
		val, err := f.GetCellValue(sheet, fmt.Sprintf("%s%d", colName, r))
		if err != nil {
			return 0, err
		}
		if val == "Results" || val == "Team Results" {
			return r, nil
		}
	}
	return 0, fmt.Errorf("could not find results header")
}

func TestIndividualRanking(t *testing.T) {
	sizes := []int{2, 3, 4}
	for _, size := range sizes {
		t.Run(fmt.Sprintf("Size_%d", size), func(t *testing.T) {
			players := make([]Player, size)
			for i := 0; i < size; i++ {
				players[i] = Player{
					Name:         fmt.Sprintf("Player %d", i+1),
					PoolPosition: int64(i + 1),
				}
			}

			pool := Pool{
				PoolName: "Pool A",
				Players:  players,
				Matches:  []Match{},
			}

			f := excelize.NewFile()
			sheet := SheetPoolMatches
			f.NewSheet(sheet)
			f.NewSheet("Pool Draw")

			// Setup styles
			styles := matchStyles{
				poolHeader:   1,
				text:         2,
				unlockedText: 3,
			}

			colNames := buildMatchColumnNames(1)
			matchWinners := make(map[string]MatchWinner)

			// We need to provide dummy maxBlocks
			maxBlocks := make([]int, 1)
			maxBlocks[0] = size + 3

			poolCoords := map[string]cellCoord{
				"Pool A": {sheetName: "Pool Draw", cell: "A1"},
			}
			pCoords := make(map[string]playerCellCoord, size)
			for i := 0; i < size; i++ {
				pCoords[playerCoordKey(players[i])] = playerCellCoord{
					cellCoord: cellCoord{sheetName: "Pool Draw", cell: fmt.Sprintf("A%d", i+1)},
				}
			}

			printSinglePool(f, sheet, pool, 1, 2, 0, 2, maxBlocks, colNames, styles, matchWinners, false, poolCoords, pCoords)

			headerRow, err := findResultsHeader(f, sheet, 0)
			require.NoError(t, err)

			// Player 1 should be Rank 1 if we set points
			// Col G is Rank (startCol + 6)
			p1RankCell := fmt.Sprintf("G%d", headerRow+1)

			// Set values that should make Player 1 rank first
			// Since we use a hidden score column, we'd need to set the match results...
			// But for a unit test of the layout/formula structure, we can just check if formulas are present.

			rank1Form, err := f.GetCellFormula(sheet, p1RankCell)
			require.NoError(t, err)
			assert.Contains(t, rank1Form, "RANK")

			// Check ranking summary
			// rankingHeaderRow is headerRow + size + 3 (approx)
			// Actually let's just find it
			var rankingHeaderRow int
			for r := headerRow + size; r < headerRow+size+10; r++ {
				val, _ := f.GetCellValue(sheet, fmt.Sprintf("G%d", r))
				if val == "Ranking" {
					rankingHeaderRow = r
					break
				}
			}
			require.NotZero(t, rankingHeaderRow, "could not find Ranking title")

			p1RankingCell := fmt.Sprintf("G%d", rankingHeaderRow+1)
			p1NameFormula, err := f.GetCellFormula(sheet, p1RankingCell)
			require.NoError(t, err)
			assert.Contains(t, p1NameFormula, "INDEX")
			assert.Contains(t, p1NameFormula, "MATCH")
		})
	}
}

func TestTeamRanking(t *testing.T) {
	teamSizes := []int{5, 7}
	for _, size := range teamSizes {
		t.Run(fmt.Sprintf("TeamSize_%d", size), func(t *testing.T) {
			players := make([]Player, 3) // 3 teams in pool
			for i := 0; i < 3; i++ {
				players[i] = Player{
					Name:         fmt.Sprintf("Team %d", i+1),
					PoolPosition: int64(i + 1),
				}
			}

			pool := Pool{
				PoolName: "Pool A",
				Players:  players,
				Matches:  []Match{},
			}

			f := excelize.NewFile()
			sheet := SheetPoolMatches
			f.NewSheet(sheet)
			f.NewSheet("Pool Draw")

			styles := matchStyles{
				poolHeader:   1,
				text:         2,
				unlockedText: 3,
			}

			colNames := buildMatchColumnNames(1)
			matchWinners := make(map[string]MatchWinner)
			maxBlocks := []int{5, 5, 5, 20} // 3 matches + results

			poolCoords := map[string]cellCoord{
				"Pool A": {sheetName: "Pool Draw", cell: "A1"},
			}
			pCoords := make(map[string]playerCellCoord, 3)
			for i := 0; i < 3; i++ {
				pCoords[playerCoordKey(players[i])] = playerCellCoord{
					cellCoord: cellCoord{sheetName: "Pool Draw", cell: fmt.Sprintf("A%d", i+1)},
				}
			}

			printSinglePool(f, sheet, pool, 1, 2, size, 2, maxBlocks, colNames, styles, matchWinners, false, poolCoords, pCoords)

			headerRow, err := findResultsHeader(f, sheet, 0)
			require.NoError(t, err)

			// Team Results Rank should be in G
			rankCell := fmt.Sprintf("G%d", headerRow+1)
			rankForm, err := f.GetCellFormula(sheet, rankCell)
			require.NoError(t, err)
			assert.Contains(t, rankForm, "RANK")

			// Check ranking title in H
			var rankingHeaderRow int
			for r := headerRow; r < headerRow+50; r++ {
				val, _ := f.GetCellValue(sheet, fmt.Sprintf("G%d", r))
				if val == "Ranking" {
					rankingHeaderRow = r
					break
				}
			}
			require.NotZero(t, rankingHeaderRow, "could not find Ranking title in H")
		})
	}
}

// TestPoolWinnerCellsPointToRankingFormulas is a regression test for the bug
// where matchWinners["Pool X-1st"]/"-2nd"/... pointed at empty cells past the
// end of the per-pool Ranking block, so the elimination bracket's CONCATENATE
// formulas referenced blank cells instead of the IFERROR(INDEX(...MATCH...))
// formulas that resolve the actual 1st/2nd/3rd player names. The bug was
// most visible with a single pool of 8 players (the elimination tree would
// show "Pool A-1st " with no name), but it affected every pool size.
func TestPoolWinnerCellsPointToRankingFormulas(t *testing.T) {
	sizes := []int{3, 4, 6, 8}
	for _, size := range sizes {
		t.Run(fmt.Sprintf("Size_%d", size), func(t *testing.T) {
			players := make([]Player, size)
			for i := 0; i < size; i++ {
				players[i] = Player{
					Name:         fmt.Sprintf("Player %d", i+1),
					PoolPosition: int64(i + 1),
				}
			}

			pool := Pool{PoolName: "Pool A", Players: players}

			f := excelize.NewFile()
			sheet := SheetPoolMatches
			f.NewSheet(sheet)
			f.NewSheet("Pool Draw")

			styles := matchStyles{poolHeader: 1, text: 2, unlockedText: 3}
			colNames := buildMatchColumnNames(1)
			matchWinners := make(map[string]MatchWinner)
			maxBlocks := []int{size + 3}

			poolCoords := map[string]cellCoord{
				"Pool A": {sheetName: "Pool Draw", cell: "A1"},
			}
			pCoords := make(map[string]playerCellCoord, size)
			for i := 0; i < size; i++ {
				pCoords[playerCoordKey(players[i])] = playerCellCoord{
					cellCoord: cellCoord{sheetName: "Pool Draw", cell: fmt.Sprintf("A%d", i+1)},
				}
			}

			numWinners := 2
			printSinglePool(f, sheet, pool, 1, 2, 0, numWinners, maxBlocks, colNames, styles, matchWinners, false, poolCoords, pCoords)

			// Locate the "Ranking" header row.
			var rankingHeaderRow int
			for r := 1; r < 200; r++ {
				val, _ := f.GetCellValue(sheet, fmt.Sprintf("G%d", r))
				if val == "Ranking" {
					rankingHeaderRow = r
					break
				}
			}
			require.NotZero(t, rankingHeaderRow, "could not find Ranking header")

			// For each rank up to numWinners, the matchWinners cell must
			// point at the IFERROR(INDEX(...MATCH(rankNum, ...))) formula
			// that resolves the player name — NOT at a blank cell past the
			// ranking block.
			for rank := 1; rank <= numWinners; rank++ {
				key := fmt.Sprintf("Pool A-%s", GetOrdinal(rank))
				mw, ok := matchWinners[key]
				require.Truef(t, ok, "matchWinners[%q] missing", key)

				expectedCell := fmt.Sprintf("G%d", rankingHeaderRow+rank)
				assert.Equal(t, expectedCell, mw.cell,
					"matchWinners[%q] should point at the rank-%d formula cell", key, rank)

				formula, err := f.GetCellFormula(sheet, mw.cell)
				require.NoError(t, err)
				assert.Contains(t, formula, "INDEX",
					"matchWinners[%q] cell %s should hold an INDEX formula, got %q",
					key, mw.cell, formula)
				assert.Containsf(t, formula, fmt.Sprintf("MATCH(%d,", rank),
					"matchWinners[%q] cell %s should MATCH rank %d, got %q",
					key, mw.cell, rank, formula)
			}
		})
	}
}

func TestManualRankingOverride(t *testing.T) {
	players := []Player{
		{Name: "Player 1", PoolPosition: 1},
		{Name: "Player 2", PoolPosition: 2},
	}
	pool := Pool{PoolName: "Pool A", Players: players}

	f := excelize.NewFile()
	sheet := SheetPoolMatches
	f.NewSheet(sheet)
	f.NewSheet("Pool Draw")
	f.SetCellValue("Pool Draw", "A1", "Player 1")
	f.SetCellValue("Pool Draw", "A2", "Player 2")

	styles := matchStyles{poolHeader: 1, text: 2, unlockedText: 3}
	colNames := buildMatchColumnNames(1)
	matchWinners := make(map[string]MatchWinner)
	poolCoords := map[string]cellCoord{
		"Pool A": {sheetName: "Pool Draw", cell: "A1"},
	}
	pCoords := map[string]playerCellCoord{
		playerCoordKey(players[0]): {cellCoord: cellCoord{sheetName: "Pool Draw", cell: "A1"}},
		playerCoordKey(players[1]): {cellCoord: cellCoord{sheetName: "Pool Draw", cell: "A2"}},
	}
	printSinglePool(f, sheet, pool, 1, 2, 0, 2, []int{5, 10}, colNames, styles, matchWinners, false, poolCoords, pCoords)

	headerRow, _ := findResultsHeader(f, sheet, 0)

	// Manually override rank in G
	p1RankCell := fmt.Sprintf("G%d", headerRow+1)
	p2RankCell := fmt.Sprintf("G%d", headerRow+2)

	handleExcelError("SetCellValue", f.SetCellValue(sheet, p1RankCell, 2))
	handleExcelError("SetCellValue", f.SetCellValue(sheet, p2RankCell, 1))

	var rankingHeaderRow int
	for r := headerRow; r < headerRow+20; r++ {
		val, _ := f.GetCellValue(sheet, fmt.Sprintf("G%d", r))
		if val == "Ranking" {
			rankingHeaderRow = r
			break
		}
	}

	p1RankingCell := fmt.Sprintf("G%d", rankingHeaderRow+1)

	// Recalculate and check
	p1Name, err := f.CalcCellValue(sheet, p1RankingCell)
	require.NoError(t, err)
	assert.Equal(t, "Player 2", p1Name, "Ranking summary should reflect manual rank override")
}
