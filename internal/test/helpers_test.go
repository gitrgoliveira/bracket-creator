package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTestPlayers(t *testing.T) {
	players := CreateTestPlayers()

	// Check that we have the expected number of players
	require.Len(t, players, 2)

	// Check the first player
	assert.Equal(t, "player1", players[0].ID)
	assert.Equal(t, "John Doe", players[0].Name)

	// Check the second player
	assert.Equal(t, "player2", players[1].ID)
	assert.Equal(t, "Jane Smith", players[1].Name)
}

func TestCreateTestPools(t *testing.T) {
	pools := CreateTestPools()

	// Check that we have pools
	require.NotEmpty(t, pools)

	// Check the first pool
	assert.Equal(t, "pool1", pools[0].ID)

	// Check the pool has players
	assert.NotEmpty(t, pools[0].Players)

	// Check the pool has matches
	assert.NotEmpty(t, pools[0].Matches)
}

func TestCreateTestTournament(t *testing.T) {
	tournament := CreateTestTournament()

	// Check the tournament name
	assert.Equal(t, "Test Tournament", tournament.Name)

	// Check that we have pools
	require.NotEmpty(t, tournament.Pools)

	// Check that we have elimination matches
	require.NotEmpty(t, tournament.EliminationMatches)
}

func TestParsePrintAreaLastRow(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"valid range", "'Elimination Matches'!$A$1:$H$35", 35},
		{"simple", "$A$1:$H$42", 42},
		{"no dollar", "invalid", -1},
		{"empty", "", -1},
		{"non-numeric suffix", "$A$1:$H$abc", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ParsePrintAreaLastRow(tc.input))
		})
	}
}

func TestFindCellRow(t *testing.T) {
	rows := [][]string{
		{"a", "b"},
		{"", "3rd Place", "x"},
		{"3rd Place"},
	}
	cases := []struct {
		name string
		val  string
		want int
	}{
		{"first matching row wins", "3rd Place", 1},
		{"first cell of first row", "a", 0},
		{"absent value", "nope", -1},
		{"empty needle matches empty cell", "", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, FindCellRow(rows, tc.val))
		})
	}
	assert.Equal(t, -1, FindCellRow(nil, "a"), "nil rows")
}

func TestReadCourtBands(t *testing.T) {
	// A miniature court-banded sheet on a 4-column grid: two shiaijo headers in
	// row 1, the first band carrying a bout below it and the second carrying
	// nothing but whitespace.
	const columnsPerCourt = 4
	rows := [][]string{
		{ShiaijoHeaderPrefix + "A", "", "", "", ShiaijoHeaderPrefix + "B"},
		{"", "Ryu Ichiro", "", "", "", "   "},
		{"vs"}, // ragged: shorter than either band
	}

	bands := ReadCourtBands(rows, columnsPerCourt)
	require.Len(t, bands, 2)

	assert.Equal(t, CourtBand{Court: "A", Col: 0, Occupied: true}, bands[0])
	assert.Equal(t, CourtBand{Court: "B", Col: 4, Occupied: false}, bands[1],
		"a band holding only blanks is unoccupied")

	t.Run("no rows", func(t *testing.T) {
		assert.Nil(t, ReadCourtBands(nil, columnsPerCourt))
		assert.Nil(t, ReadCourtBands([][]string{}, columnsPerCourt))
	})

	t.Run("header row only", func(t *testing.T) {
		got := ReadCourtBands([][]string{{ShiaijoHeaderPrefix + "A"}}, columnsPerCourt)
		require.Len(t, got, 1)
		assert.False(t, got[0].Occupied, "a header with no rows under it is unoccupied")
	})

	t.Run("off-grid header is reported, not hidden", func(t *testing.T) {
		got := ReadCourtBands([][]string{
			{"", ShiaijoHeaderPrefix + "A"},
			{"", "Ryu Ichiro"},
		}, columnsPerCourt)
		require.Len(t, got, 1)
		assert.Equal(t, 1, got[0].Col,
			"the caller needs the column to say the printed layout moved")
	})

	t.Run("non-header cells are ignored", func(t *testing.T) {
		assert.Empty(t, ReadCourtBands([][]string{{"Pool A", "Shiaijo", "shiaijo A"}}, columnsPerCourt))
	})
}
