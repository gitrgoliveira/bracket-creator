package test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
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

func TestRowBreaks(t *testing.T) {
	f := excelize.NewFile()
	defer func() { require.NoError(t, f.Close()) }()
	_, err := f.NewSheet("Scores")
	require.NoError(t, err)
	require.NoError(t, f.InsertPageBreak("Scores", "A12"))
	require.NoError(t, f.InsertPageBreak("Scores", "A30"))
	var buf bytes.Buffer
	require.NoError(t, f.Write(&buf))

	breaks, err := RowBreaks(buf.Bytes(), "Scores")
	require.NoError(t, err)
	assert.Equal(t, []int{11, 29}, breaks, "a break before row 12 ends the page after row 11")

	breaks, err = RowBreaks(buf.Bytes(), "Sheet1")
	require.NoError(t, err)
	assert.Empty(t, breaks, "a sheet with no break")

	_, err = RowBreaks(buf.Bytes(), "Missing")
	assert.Error(t, err, "an unknown sheet")
	_, err = RowBreaks([]byte("not a workbook"), "Scores")
	assert.Error(t, err, "bytes that are not an xlsx")
}

func TestCellAt(t *testing.T) {
	rows := [][]string{{"a", "b"}, {"c"}}
	assert.Equal(t, "b", CellAt(rows, 0, 1))
	assert.Empty(t, CellAt(rows, 1, 1), "past a short row's end")
	assert.Empty(t, CellAt(rows, 2, 0), "past the last row")
	assert.Empty(t, CellAt(rows, -1, 0), "a negative row")
	assert.Empty(t, CellAt(rows, 0, -1), "a negative column")
}

func TestFirstRowWith(t *testing.T) {
	rows := [][]string{{"x", "Round 1"}, {"Round 1"}, {"Round 1"}}
	assert.Equal(t, 1, FirstRowWith(rows, 0, "Round 1"), "the first row carrying it in that column")
	assert.Equal(t, 0, FirstRowWith(rows, 1, "Round 1"))
	assert.Equal(t, -1, FirstRowWith(rows, 2, "Round 1"), "a column no row reaches")
	assert.Equal(t, -1, FirstRowWith(rows, 0, "Round 2"), "an absent value")
}

func TestNumberedRowsFrom(t *testing.T) {
	rows := [][]string{{"White"}, {"1"}, {"2"}, {"3"}, {""}, {"4"}}
	assert.Equal(t, 3, NumberedRowsFrom(rows, 1, 0), "the run ends at the first row that breaks it")
	assert.Equal(t, 0, NumberedRowsFrom(rows, 2, 0), "a run starts at 1")
	assert.Equal(t, 0, NumberedRowsFrom(rows, 1, 1), "a column the rows do not reach")
	assert.Equal(t, 0, NumberedRowsFrom(rows, 9, 0), "past the last row")
}

func TestTallySpanError(t *testing.T) {
	// tally is an IV formula with one clause per listed bout row, shaped like
	// the workbook's own.
	tally := func(boutRows ...int) string {
		clauses := make([]string, len(boutRows))
		for i, r := range boutRows {
			clauses[i] = fmt.Sprintf(`IF(UPPER(D%d)="X",0,1)`, r)
		}
		return strings.Join(clauses, "+")
	}
	assert.NoError(t, TallySpanError(tally(5, 6, 7), 5, 3))
	assert.EqualError(t, TallySpanError(tally(5, 6), 5, 3), "2 IV clauses for 3 bout rows")
	assert.EqualError(t, TallySpanError(tally(6, 7, 8), 5, 3), "the tally does not read bout row 5")
	assert.EqualError(t, TallySpanError(tally(4, 5, 6), 5, 3), "the tally does not read bout row 7")
	assert.EqualError(t, TallySpanError(tally(5, 7, 8), 5, 3), "the tally reads row 8, past its block")
}

func TestHanteiExplicit(t *testing.T) {
	for _, v := range []bool{true, false} {
		p := HanteiExplicit(v)
		require.NotNil(t, p)
		assert.Equal(t, v, *p)
	}
	assert.NotSame(t, HanteiExplicit(true), HanteiExplicit(true), "each call returns its own pointer")
}

func TestLegalShiaijoCount(t *testing.T) {
	var legal []int
	for n := -1; n <= 17; n++ {
		if LegalShiaijoCount(n) {
			legal = append(legal, n)
		}
	}
	assert.Equal(t, []int{1, 2, 4, 8, 16}, legal)
}
