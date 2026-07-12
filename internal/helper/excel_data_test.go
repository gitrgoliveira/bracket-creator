package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

func TestAddPoolDataToSheet(t *testing.T) {
	tests := []struct {
		name     string
		pools    []Pool
		sanitize bool
	}{
		{
			name: "single pool with players without sanitize",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players: []Player{
						{Name: "Player 1", Dojo: "Dojo A", PoolPosition: 1},
						{Name: "Player 2", Dojo: "Dojo B", PoolPosition: 2},
					},
				},
			},
			sanitize: false,
		},
		{
			name: "single pool with players with sanitize",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players: []Player{
						{Name: "Player 1", DisplayName: "P1 Display", Dojo: "Dojo A", PoolPosition: 1},
						{Name: "Player 2", DisplayName: "P2 Display", Dojo: "Dojo B", PoolPosition: 2},
					},
				},
			},
			sanitize: true,
		},
		{
			name: "multiple pools",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players: []Player{
						{Name: "Player 1", Dojo: "Dojo A", PoolPosition: 1},
						{Name: "Player 2", Dojo: "Dojo B", PoolPosition: 2},
					},
				},
				{
					PoolName: "Pool B",
					Players: []Player{
						{Name: "Player 3", Dojo: "Dojo C", PoolPosition: 1},
						{Name: "Player 4", Dojo: "Dojo D", PoolPosition: 2},
					},
				},
			},
			sanitize: false,
		},
		{
			name: "pool with metadata",
			pools: []Pool{
				{
					PoolName: "Pool A",
					Players: []Player{
						{Name: "Player 1", Dojo: "Dojo A", PoolPosition: 1, Metadata: []string{"meta1", "meta2"}},
					},
				},
			},
			sanitize: false,
		},
		{
			name: "empty pools",
			pools: []Pool{
				{
					PoolName: "Empty Pool",
					Players:  []Player{},
				},
			},
			sanitize: false,
		},
		{
			name: "unicode characters in names",
			pools: []Pool{
				{
					PoolName: "Pool 日本語",
					Players: []Player{
						{Name: "José García", Dojo: "Açaí Dojo", PoolPosition: 1},
						{Name: "佐藤太郎", Dojo: "東京道場", PoolPosition: 2},
					},
				},
			},
			sanitize: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

			// Create the data sheet
			_, err := f.NewSheet(SheetData)
			require.NoError(t, err)

			poolCoords, playerCoords := AddPoolDataToSheet(f, tt.pools, tt.sanitize, "")

			// Verify prefix label row
			prefixLabel, err := f.GetCellValue(SheetData, "A1")
			require.NoError(t, err)
			assert.Equal(t, "Title prefix:", prefixLabel)

			// Verify headers (row 2)
			poolHeader, err := f.GetCellValue(SheetData, "A2")
			require.NoError(t, err)
			assert.Equal(t, "Pool", poolHeader)

			playerNameHeader, err := f.GetCellValue(SheetData, "B2")
			require.NoError(t, err)
			assert.Equal(t, "Player Name", playerNameHeader)

			dojoHeader, err := f.GetCellValue(SheetData, "C2")
			require.NoError(t, err)
			assert.Equal(t, "Player Dojo", dojoHeader)

			if tt.sanitize {
				displayNameHeader, err := f.GetCellValue(SheetData, "D2")
				require.NoError(t, err)
				assert.Equal(t, "Display Name", displayNameHeader)
			}

			metaHeader, err := f.GetCellValue(SheetData, "E2")
			require.NoError(t, err)
			assert.Equal(t, "Metadata", metaHeader)

			// Verify data rows (start at row 3)
			row := 3
			for _, pool := range tt.pools {
				for _, player := range pool.Players {
					poolName, err := f.GetCellValue(SheetData, fmt.Sprintf("A%d", row))
					require.NoError(t, err)
					assert.Equal(t, pool.PoolName, poolName)

					playerName, err := f.GetCellValue(SheetData, fmt.Sprintf("B%d", row))
					require.NoError(t, err)
					assert.Equal(t, player.Name, playerName)

					dojo, err := f.GetCellValue(SheetData, fmt.Sprintf("C%d", row))
					require.NoError(t, err)
					assert.Equal(t, player.Dojo, dojo)

					if tt.sanitize {
						displayName, err := f.GetCellValue(SheetData, fmt.Sprintf("D%d", row))
						require.NoError(t, err)
						assert.Equal(t, player.DisplayName, displayName)
					}

					// Verify metadata if present
					for k, meta := range player.Metadata {
						colName, err := excelize.ColumnNumberToName(5 + k)
						require.NoError(t, err)
						metaValue, err := f.GetCellValue(SheetData, fmt.Sprintf("%s%d", colName, row))
						require.NoError(t, err)
						assert.Equal(t, meta, metaValue)
					}

					row++
				}
			}

			// Verify that pool and player coords are set correctly in the returned maps
			row = 3
			for _, pool := range tt.pools {
				lastPlayerRow := row + len(pool.Players) - 1
				if len(pool.Players) > 0 {
					coord := poolCoords[pool.PoolName]
					assert.Equal(t, SheetData, coord.sheetName)
					expectedPoolCell := fmt.Sprintf("$A$%d", lastPlayerRow)
					assert.Equal(t, expectedPoolCell, coord.cell, "pool coord cell should point to last player's row")
				}

				for _, player := range pool.Players {
					pCoord := playerCoords[playerCoordKey(player)]
					assert.Equal(t, SheetData, pCoord.sheetName)
					expectedPlayerCell := fmt.Sprintf("$B$%d", row)
					assert.Equal(t, expectedPlayerCell, pCoord.cell)
					row++
				}
			}
		})
	}
}

func TestAddPlayerDataToSheet(t *testing.T) {
	tests := []struct {
		name     string
		players  []Player
		sanitize bool
	}{
		{
			name: "basic players without sanitize",
			players: []Player{
				{Name: "Player 1", Dojo: "Dojo A", PoolPosition: 1},
				{Name: "Player 2", Dojo: "Dojo B", PoolPosition: 2},
				{Name: "Player 3", Dojo: "Dojo C", PoolPosition: 3},
			},
			sanitize: false,
		},
		{
			name: "players with sanitize and display names",
			players: []Player{
				{Name: "Player 1", DisplayName: "Display 1", Dojo: "Dojo A", PoolPosition: 1},
				{Name: "Player 2", DisplayName: "Display 2", Dojo: "Dojo B", PoolPosition: 2},
			},
			sanitize: true,
		},
		{
			name: "players with metadata",
			players: []Player{
				{Name: "Player 1", Dojo: "Dojo A", PoolPosition: 1, Metadata: []string{"weight:80kg", "rank:3dan"}},
				{Name: "Player 2", Dojo: "Dojo B", PoolPosition: 2, Metadata: []string{"weight:75kg", "rank:2dan"}},
			},
			sanitize: false,
		},
		{
			name:     "empty players list",
			players:  []Player{},
			sanitize: false,
		},
		{
			name: "unicode characters",
			players: []Player{
				{Name: "José García", Dojo: "Dojo São Paulo", PoolPosition: 1},
				{Name: "山田太郎", DisplayName: "Yamada Taro", Dojo: "東京道場", PoolPosition: 2},
			},
			sanitize: true,
		},
		{
			name: "large pool position numbers",
			players: []Player{
				{Name: "Player 1", Dojo: "Dojo A", PoolPosition: 100},
				{Name: "Player 2", Dojo: "Dojo B", PoolPosition: 256},
			},
			sanitize: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

			// Create the data sheet
			_, err := f.NewSheet(SheetData)
			require.NoError(t, err)

			playerCoords := AddPlayerDataToSheet(f, tt.players, tt.sanitize, "")

			// Verify prefix label row
			prefixLabel, err := f.GetCellValue(SheetData, "A1")
			require.NoError(t, err)
			assert.Equal(t, "Title prefix:", prefixLabel)

			// Verify headers (row 2)
			numberHeader, err := f.GetCellValue(SheetData, "A2")
			require.NoError(t, err)
			assert.Equal(t, "Number", numberHeader)

			playerNameHeader, err := f.GetCellValue(SheetData, "B2")
			require.NoError(t, err)
			assert.Equal(t, "Player Name", playerNameHeader)

			dojoHeader, err := f.GetCellValue(SheetData, "C2")
			require.NoError(t, err)
			assert.Equal(t, "Player Dojo", dojoHeader)

			if tt.sanitize {
				displayNameHeader, err := f.GetCellValue(SheetData, "D2")
				require.NoError(t, err)
				assert.Equal(t, "Display Name", displayNameHeader)
			}

			metaHeader, err := f.GetCellValue(SheetData, "E2")
			require.NoError(t, err)
			assert.Equal(t, "Metadata", metaHeader)

			// Verify data rows (start at row 3)
			for i, player := range tt.players {
				row := i + 3

				// Verify position number
				position, err := f.GetCellValue(SheetData, fmt.Sprintf("A%d", row))
				require.NoError(t, err)
				assert.Equal(t, fmt.Sprint(player.PoolPosition), position)

				// Verify name
				name, err := f.GetCellValue(SheetData, fmt.Sprintf("B%d", row))
				require.NoError(t, err)
				assert.Equal(t, player.Name, name)

				// Verify dojo
				dojo, err := f.GetCellValue(SheetData, fmt.Sprintf("C%d", row))
				require.NoError(t, err)
				assert.Equal(t, player.Dojo, dojo)

				if tt.sanitize {
					displayName, err := f.GetCellValue(SheetData, fmt.Sprintf("D%d", row))
					require.NoError(t, err)
					assert.Equal(t, player.DisplayName, displayName)
				}

				// Verify metadata
				for k, meta := range player.Metadata {
					colName, err := excelize.ColumnNumberToName(5 + k)
					require.NoError(t, err)
					metaValue, err := f.GetCellValue(SheetData, fmt.Sprintf("%s%d", colName, row))
					require.NoError(t, err)
					assert.Equal(t, meta, metaValue)
				}

				// Verify player coord is set in the returned map
				expectedCell := fmt.Sprintf("$B$%d", row)
				pCoord := playerCoords[playerCoordKey(player)]
				assert.Equal(t, expectedCell, pCoord.cell)
				assert.Equal(t, SheetData, pCoord.sheetName)
			}
		})
	}
}

func TestAddPoolsToSheet(t *testing.T) {
	type poolSpec struct {
		poolName string
		poolCell string
		players  []struct {
			name, cell string
			pos        int
		}
	}
	tests := []struct {
		name    string
		specs   []poolSpec
		wantErr bool
	}{
		{
			name: "basic pools with players",
			specs: []poolSpec{
				{poolName: "Pool A", poolCell: "$A$2", players: []struct {
					name, cell string
					pos        int
				}{
					{"Player 1", "$B$2", 1}, {"Player 2", "$B$3", 2},
				}},
				{poolName: "Pool B", poolCell: "$A$4", players: []struct {
					name, cell string
					pos        int
				}{
					{"Player 3", "$B$4", 1}, {"Player 4", "$B$5", 2},
				}},
			},
			wantErr: false,
		},
		{
			name: "single pool",
			specs: []poolSpec{
				{poolName: "Only Pool", poolCell: "$A$2", players: []struct {
					name, cell string
					pos        int
				}{
					{"Player 1", "$B$2", 1},
				}},
			},
			wantErr: false,
		},
		{
			name:    "empty pools",
			specs:   []poolSpec{},
			wantErr: false,
		},
		{
			name: "many pools (3 columns)",
			specs: func() []poolSpec {
				specs := make([]poolSpec, 9)
				for i := 0; i < 9; i++ {
					specs[i] = poolSpec{
						poolName: fmt.Sprintf("Pool %d", i+1),
						poolCell: fmt.Sprintf("$A$%d", i+2),
						players: []struct {
							name, cell string
							pos        int
						}{
							{fmt.Sprintf("Player %d", i+1), fmt.Sprintf("$B$%d", i+2), 1},
						},
					}
				}
				return specs
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

			// Build pools and coord maps from specs
			pools := make([]Pool, len(tt.specs))
			poolCoords := make(map[string]cellCoord)
			playerCoords := make(map[string]playerCellCoord)
			for i, spec := range tt.specs {
				players := make([]Player, len(spec.players))
				for j, ps := range spec.players {
					players[j] = Player{Name: ps.name, PoolPosition: int64(ps.pos)}
					playerCoords[playerCoordKey(players[j])] = playerCellCoord{cellCoord: cellCoord{sheetName: SheetData, cell: ps.cell}}
				}
				pools[i] = Pool{PoolName: spec.poolName, Players: players}
				poolCoords[spec.poolName] = cellCoord{sheetName: SheetData, cell: spec.poolCell}
			}

			// Create necessary sheets
			_, err := f.NewSheet(SheetPoolDraw)
			require.NoError(t, err)
			_, err = f.NewSheet(SheetData)
			require.NoError(t, err)

			// Add some basic data to the Pool Draw sheet for style references
			err = f.SetCellValue(SheetPoolDraw, "B5", "Pool Header")
			require.NoError(t, err)
			err = f.SetCellValue(SheetPoolDraw, "B6", "Player 1")
			require.NoError(t, err)

			err = AddPoolsToSheet(f, pools, poolCoords, playerCoords, false)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify Pool Draw title formula (normalize quotes)
			titleFormula, err := f.GetCellFormula(SheetPoolDraw, "B2")
			require.NoError(t, err)
			expectedTitle := `IF(data!$B$1="","Tournament Pools",data!$B$1&" - Tournament Pools")`
			assert.Equal(t, strings.ReplaceAll(expectedTitle, "'", ""), strings.ReplaceAll(titleFormula, "'", ""))

			// Verify formulas exist (spot check first pool if pools exist)
			if len(pools) > 0 {
				// Pool name formula at B5
				formula, err := f.GetCellFormula(SheetPoolDraw, "B5")
				require.NoError(t, err)
				pc := poolCoords[pools[0].PoolName]
				expectedFormula := fmt.Sprintf("%s!%s", pc.sheetName, pc.cell)
				assert.Equal(t, strings.ReplaceAll(expectedFormula, "'", ""), strings.ReplaceAll(formula, "'", ""))

				// First player formula at B6
				if len(pools[0].Players) > 0 {
					formula, err = f.GetCellFormula(SheetPoolDraw, "B6")
					require.NoError(t, err)
					player := pools[0].Players[0]
					pCoord := playerCoords[playerCoordKey(player)]
					expectedFormula = fmt.Sprintf("\"%d. \" & %s!%s", player.PoolPosition, pCoord.sheetName, pCoord.cell)
					assert.Equal(t, strings.ReplaceAll(expectedFormula, "'", ""), strings.ReplaceAll(formula, "'", ""))
				}
			}
		})
	}
}

func TestHandleExcelDataError(t *testing.T) {
	tests := []struct {
		name      string
		operation string
		err       error
	}{
		{
			name:      "no error",
			operation: "SetCellValue",
			err:       nil,
		},
		{
			name:      "with error",
			operation: "SetCellValue",
			err:       fmt.Errorf("test error"),
		},
		{
			name:      "different operation",
			operation: "SetColWidth",
			err:       fmt.Errorf("column width error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This function only prints errors, so we just verify it doesn't panic
			assert.NotPanics(t, func() {
				handleExcelError(tt.operation, tt.err)
			})
		})
	}
}
