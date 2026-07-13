package test

import (
	"slices"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// ParsePrintAreaLastRow extracts the last-row number from a Print_Area RefersTo
// string such as "'Elimination Matches'!$A$1:$H$35". Returns -1 on any parse error.
func ParsePrintAreaLastRow(refersTo string) int {
	lastDollar := strings.LastIndex(refersTo, "$")
	if lastDollar < 0 {
		return -1
	}
	row, err := strconv.Atoi(refersTo[lastDollar+1:])
	if err != nil {
		return -1
	}
	return row
}

// FindCellRow returns the 0-based index of the first sheet row containing a
// cell equal to val, or -1 when absent. rows is the excelize GetRows shape.
func FindCellRow(rows [][]string, val string) int {
	for i, row := range rows {
		if slices.Contains(row, val) {
			return i
		}
	}
	return -1
}

// CreateTestPlayers returns a slice of players for testing
func CreateTestPlayers() []domain.Player {
	return []domain.Player{
		{
			ID:           "player1",
			Name:         "John Doe",
			DisplayName:  "J. Doe",
			Dojo:         "Test Dojo",
			PoolPosition: 1,
		},
		{
			ID:           "player2",
			Name:         "Jane Smith",
			DisplayName:  "J. Smith",
			Dojo:         "Another Dojo",
			PoolPosition: 2,
		},
	}
}

// CreateTestPools returns a slice of pools for testing
func CreateTestPools() []domain.Pool {
	players := CreateTestPlayers()

	match := domain.Match{
		ID:    "match1",
		SideA: &players[0],
		SideB: &players[1],
	}

	return []domain.Pool{
		{
			ID:      "pool1",
			Name:    "Pool A",
			Players: players,
			Matches: []domain.Match{match},
		},
	}
}

// CreateTestTournament returns a tournament for testing
func CreateTestTournament() domain.Tournament {
	pools := CreateTestPools()

	return domain.Tournament{
		Name:  "Test Tournament",
		Pools: pools,
		EliminationMatches: []domain.Match{
			pools[0].Matches[0],
		},
	}
}
