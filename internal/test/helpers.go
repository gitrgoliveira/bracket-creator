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

// ShiaijoHeaderPrefix is what the workbook writer puts in front of the court
// letter in a column band's row-1 header ("Shiaijo A"). Every producer goes
// through helper.ShiaijoLabel, so one reader recognises them all.
const ShiaijoHeaderPrefix = "Shiaijo "

// CourtBand is one shiaijo column band on a court-banded sheet (Pool Matches,
// Elimination Matches), read back off a rendered workbook.
type CourtBand struct {
	// Court is the letter following ShiaijoHeaderPrefix in the band's row-1
	// header, taken from the sheet.
	Court string
	// Col is the 0-based sheet column the header sits in. Every band starts on
	// the court grid, so a Col that is not a multiple of the caller's
	// columnsPerCourt means the printed layout moved.
	Col int
	// Occupied reports whether the band carries anything at all below its
	// header row. An UNOCCUPIED band is a shiaijo header printed over nothing:
	// a score sheet naming a court the competition never scheduled a bout on.
	Occupied bool
}

// ReadCourtBands reads a court-banded sheet's shiaijo bands out of rows (the
// excelize GetRows shape), in column order. Each court owns one
// columnsPerCourt-wide band whose header sits in row 1 at the band's start
// column, so the header positions ARE the banding an operator prints, read off
// the artifact rather than recomputed from the code that wrote it.
//
// columnsPerCourt is helper.CourtsColumnsPerCourt, passed in rather than
// imported: this fixture package depends only on internal/domain, and importing
// internal/helper here would make it unusable from helper's own in-package
// tests (that import would close a cycle).
//
// Callers assert; this only reads. Deciding that an empty band or an off-grid
// header is a failure, and saying so in the words that sheet's operator needs,
// stays with the test that knows which workbook it is looking at.
func ReadCourtBands(rows [][]string, columnsPerCourt int) []CourtBand {
	if len(rows) == 0 {
		return nil
	}
	var bands []CourtBand
	for col, value := range rows[0] {
		court, ok := strings.CutPrefix(value, ShiaijoHeaderPrefix)
		if !ok {
			continue
		}
		bands = append(bands, CourtBand{
			Court:    court,
			Col:      col,
			Occupied: bandOccupied(rows, col, columnsPerCourt),
		})
	}
	return bands
}

// bandOccupied reports whether any row BELOW the header row carries a non-blank
// value inside the band starting at 0-based column start.
func bandOccupied(rows [][]string, start, columnsPerCourt int) bool {
	end := start + columnsPerCourt
	for _, row := range rows[1:] {
		for c := start; c < end && c < len(row); c++ {
			if strings.TrimSpace(row[c]) != "" {
				return true
			}
		}
	}
	return false
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

// LegalShiaijoCount states R9's shiaijo-count rule INDEPENDENTLY of the
// production validator (helper.ValidateShiaijoCount), so the CLI, engine and
// API sweeps that use it cannot agree with a broken implementation by
// construction: a bracket-drawing competition runs on 1, 2, 4, 8 or 16 shiaijo
// and nothing else.
//
// It lives here rather than three times over because three identical copies buy
// no extra independence -- they can only drift together -- while letting the
// CLI, engine and API sweeps disagree about what they are asserting. The
// independence that matters is from the PRODUCTION rule, and this package keeps
// it: internal/test imports only internal/domain, so it cannot reach
// helper.ValidateShiaijoCount even by accident.
func LegalShiaijoCount(n int) bool {
	return n == 1 || n == 2 || n == 4 || n == 8 || n == 16
}
