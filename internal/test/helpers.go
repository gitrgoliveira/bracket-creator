package test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
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

// CellAt safely reads rows[r][c], returning "" when r or c falls outside
// rows in either direction: a negative index, a row past the end, or a
// column past a row's end (GetRows trims each row's trailing empty cells,
// so a blank cell past a short row's length is legitimately "").
func CellAt(rows [][]string, r, c int) string {
	if r < 0 || r >= len(rows) || c < 0 || c >= len(rows[r]) {
		return ""
	}
	return rows[r][c]
}

// FirstRowWith returns the 0-based index of the first row whose cell at col
// equals val, or -1. rows is the excelize GetRows shape.
func FirstRowWith(rows [][]string, col int, val string) int {
	for r, row := range rows {
		if col < len(row) && row[col] == val {
			return r
		}
	}
	return -1
}

// NumberedRowsFrom counts the consecutive rows from 0-based row from whose
// cell at col carries the bout numbers 1, 2, 3, ... in order: the bout rows
// of a team block.
func NumberedRowsFrom(rows [][]string, from, col int) int {
	n := 0
	for r := from; r < len(rows); r++ {
		if col >= len(rows[r]) || rows[r][col] != strconv.Itoa(n+1) {
			break
		}
		n++
	}
	return n
}

// TallySpanError reports what is wrong, if anything, with a team block's IV
// tally formula: it must add one clause per bout row, rows firstBout to
// firstBout+boutRows-1 (1-based; each clause reads the bout's centre cell in
// column D), and read no row past the block.
func TallySpanError(formula string, firstBout, boutRows int) error {
	if n := strings.Count(formula, "IF(UPPER("); n != boutRows {
		return fmt.Errorf("%d IV clauses for %d bout rows", n, boutRows)
	}
	for _, r := range []int{firstBout, firstBout + boutRows - 1} {
		if !strings.Contains(formula, fmt.Sprintf("UPPER(D%d)", r)) {
			return fmt.Errorf("the tally does not read bout row %d", r)
		}
	}
	if past := firstBout + boutRows; strings.Contains(formula, fmt.Sprintf("UPPER(D%d)", past)) {
		return fmt.Errorf("the tally reads row %d, past its block", past)
	}
	return nil
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

// RowBreaks returns the manual page breaks of one sheet of a saved workbook,
// as the rows a page ENDS after (the <rowBreaks> brk ids, so the next page
// starts on row id+1), in sheet order. excelize can insert a page break but
// has no reader for one, so this reads the sheet XML out of the xlsx bytes,
// resolving the sheet's file through the workbook's relationships.
func RowBreaks(xlsx []byte, sheet string) ([]int, error) {
	zr, err := zip.NewReader(bytes.NewReader(xlsx), int64(len(xlsx)))
	if err != nil {
		return nil, err
	}
	part := func(name string, into any) error {
		for _, f := range zr.File {
			if f.Name != name {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			return errors.Join(xml.NewDecoder(rc).Decode(into), rc.Close())
		}
		return fmt.Errorf("%s is not in the workbook", name)
	}

	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := part("xl/workbook.xml", &wb); err != nil {
		return nil, err
	}
	var rels struct {
		Rels []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := part("xl/_rels/workbook.xml.rels", &rels); err != nil {
		return nil, err
	}
	target := ""
	for _, s := range wb.Sheets {
		if s.Name != sheet {
			continue
		}
		for _, r := range rels.Rels {
			if r.ID == s.RID {
				target = r.Target
			}
		}
	}
	if target == "" {
		return nil, fmt.Errorf("no sheet %q in the workbook", sheet)
	}
	if abs, ok := strings.CutPrefix(target, "/"); ok {
		target = abs
	} else {
		target = "xl/" + target
	}

	var ws struct {
		Breaks []struct {
			ID int `xml:"id,attr"`
		} `xml:"rowBreaks>brk"`
	}
	if err := part(target, &ws); err != nil {
		return nil, err
	}
	out := make([]int, 0, len(ws.Breaks))
	for _, b := range ws.Breaks {
		out = append(out, b.ID)
	}
	return out, nil
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

// HanteiExplicit returns a pointer to the given value, INCLUDING false.
// Legacy test-fixture constructor: production code never sets the legacy
// DecidedByHantei fields (see internal/state/legacy_hantei.go — "writers must
// never set it"), so this exists only so tests across packages (state,
// engine, mobileapp) can construct a legacy payload shape (explicit
// true/false, as opposed to a nil "writer said nothing") without hand-rolling
// `f := false; &f` at each call site. It lives here rather than in
// internal/state itself because callers outside that package need it from
// their own _test.go files, which cannot see symbols defined in another
// package's _test.go files — putting it in the shared fixture package is the
// only way to keep it out of state's production API while still reaching
// every caller.
func HanteiExplicit(v bool) *bool {
	return &v
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
