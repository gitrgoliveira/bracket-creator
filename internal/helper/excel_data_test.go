package helper

import (
	"fmt"
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

			AddPoolDataToSheet(f, tt.pools, tt.sanitize, "")

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

			// Verify that pool and player cells are set correctly
			// Note: The AddPoolDataToSheet function sets pool.cell for each player iteration,
			// so the final value is the cell of the last player in the pool
			row = 3
			for i, pool := range tt.pools {
				assert.Equal(t, SheetData, tt.pools[i].sheetName)

				lastPlayerRow := row + len(pool.Players) - 1
				if len(pool.Players) > 0 {
					expectedPoolCell := fmt.Sprintf("$A$%d", lastPlayerRow)
					assert.Equal(t, expectedPoolCell, pool.cell, "pool.cell should point to last player's row")
				} else {
					// Empty pool - cell is not set
					assert.Equal(t, "", pool.cell)
				}

				for j := range pool.Players {
					assert.Equal(t, SheetData, tt.pools[i].Players[j].sheetName)
					expectedPlayerCell := fmt.Sprintf("$B$%d", row)
					assert.Equal(t, expectedPlayerCell, tt.pools[i].Players[j].cell)
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

			AddPlayerDataToSheet(f, tt.players, tt.sanitize, "")

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

				// Verify player cell is set
				expectedCell := fmt.Sprintf("$B$%d", row)
				assert.Equal(t, expectedCell, tt.players[i].cell)
				assert.Equal(t, SheetData, tt.players[i].sheetName)
			}
		})
	}
}

func TestAddPoolsToSheet(t *testing.T) {
	tests := []struct {
		name    string
		pools   []Pool
		wantErr bool
	}{
		{
			name: "basic pools with players",
			pools: []Pool{
				{
					PoolName:  "Pool A",
					sheetName: SheetData,
					cell:      "$A$2",
					Players: []Player{
						{Name: "Player 1", sheetName: SheetData, cell: "$B$2", PoolPosition: 1},
						{Name: "Player 2", sheetName: SheetData, cell: "$B$3", PoolPosition: 2},
					},
				},
				{
					PoolName:  "Pool B",
					sheetName: SheetData,
					cell:      "$A$4",
					Players: []Player{
						{Name: "Player 3", sheetName: SheetData, cell: "$B$4", PoolPosition: 1},
						{Name: "Player 4", sheetName: SheetData, cell: "$B$5", PoolPosition: 2},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "single pool",
			pools: []Pool{
				{
					PoolName:  "Only Pool",
					sheetName: SheetData,
					cell:      "$A$2",
					Players: []Player{
						{Name: "Player 1", sheetName: SheetData, cell: "$B$2", PoolPosition: 1},
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "empty pools",
			pools:   []Pool{},
			wantErr: false,
		},
		{
			name: "many pools (3 columns)",
			pools: func() []Pool {
				pools := make([]Pool, 9)
				for i := 0; i < 9; i++ {
					pools[i] = Pool{
						PoolName:  fmt.Sprintf("Pool %d", i+1),
						sheetName: SheetData,
						cell:      fmt.Sprintf("$A$%d", i+2),
						Players: []Player{
							{Name: fmt.Sprintf("Player %d", i+1), sheetName: SheetData, cell: fmt.Sprintf("$B$%d", i+2), PoolPosition: 1},
						},
					}
				}
				return pools
			}(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := excelize.NewFile()
			defer f.Close()

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

			err = AddPoolsToSheet(f, tt.pools)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Verify Pool Draw title formula
			titleFormula, err := f.GetCellFormula(SheetPoolDraw, "B2")
			require.NoError(t, err)
			assert.Equal(t, `IF(data!$B$1="","Tournament Pools",data!$B$1&" - Tournament Pools")`, titleFormula)

			// Verify formulas exist (spot check first pool if pools exist)
			if len(tt.pools) > 0 {
				// Pool name formula at B5
				formula, err := f.GetCellFormula(SheetPoolDraw, "B5")
				require.NoError(t, err)
				expectedFormula := fmt.Sprintf("%s!%s", tt.pools[0].sheetName, tt.pools[0].cell)
				assert.Equal(t, expectedFormula, formula)

				// First player formula at B6
				if len(tt.pools[0].Players) > 0 {
					formula, err = f.GetCellFormula(SheetPoolDraw, "B6")
					require.NoError(t, err)
					player := tt.pools[0].Players[0]
					expectedFormula = fmt.Sprintf("\"%d. \" & %s!%s", player.PoolPosition, player.sheetName, player.cell)
					assert.Equal(t, expectedFormula, formula)
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
