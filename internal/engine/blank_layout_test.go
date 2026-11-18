package engine

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/pdf"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// blankLayoutCase is one competition shape the blank layout must print as the
// draw prints it: the facets the app's old pre-draw template request (a
// stateless POST /create) carried per format, each now on the Go side.
type blankLayoutCase struct {
	name   string
	format string
	size   int
	places int
	courts []string
	opts   func(*state.Competition)
}

func blankLayoutCases() []blankLayoutCase {
	naginata := func(c *state.Competition) { c.Naginata = true }
	engi := func(c *state.Competition) { c.Engi = true }
	return []blankLayoutCase{
		{"pools individual", state.CompFormatMixed, 3, 9, []string{"A"}, nil},
		{"pools individual two courts", state.CompFormatMixed, 3, 12, []string{"A", "B"}, nil},
		{"pools team", state.CompFormatMixed, 3, 6, []string{"A"}, teamOf(3, "")},
		{"pools kachinuki", state.CompFormatMixed, 3, 6, []string{"A"}, teamOf(3, state.TeamMatchTypeKachinuki)},
		{"pools engi", state.CompFormatMixed, 3, 6, []string{"A"}, engi},
		{"pools single third place", state.CompFormatMixed, 3, 12, []string{"A"}, naginata},
		{"league individual", state.CompFormatLeague, 5, 5, []string{"A"}, nil},
		{"league two courts", state.CompFormatLeague, 6, 6, []string{"A", "B"}, nil},
		{"league team", state.CompFormatLeague, 4, 4, []string{"A"}, teamOf(3, "")},
		{"league kachinuki", state.CompFormatLeague, 4, 4, []string{"A"}, teamOf(3, state.TeamMatchTypeKachinuki)},
		{"knockout individual", state.CompFormatKnockout, 0, 6, []string{"A"}, nil},
		{"knockout single third place", state.CompFormatKnockout, 0, 6, []string{"A"}, naginata},
		{"knockout two courts", state.CompFormatKnockout, 0, 11, []string{"A", "B"}, nil},
		{"knockout team", state.CompFormatKnockout, 0, 5, []string{"A"}, teamOf(3, "")},
		{"knockout kachinuki", state.CompFormatKnockout, 0, 5, []string{"A"}, teamOf(3, state.TeamMatchTypeKachinuki)},
	}
}

// saveBlankLayoutComp saves a competition of the case in setup, sized by its
// expected number of places, with nobody registered.
func saveBlankLayoutComp(t *testing.T, store *state.Store, id string, tc blankLayoutCase) {
	t.Helper()
	createTestCompetition(t, store, id, tc.format, tc.size, func(c *state.Competition) {
		c.Courts = tc.courts
		c.ExpectedParticipants = tc.places
		c.NumberPrefix = "Q"
		if tc.opts != nil {
			tc.opts(c)
		}
	})
}

// drawnRoster is n competitors from n dojos, so the draw has no dojo to spread
// and draws them as it draws the blank places.
func drawnRoster(n int) []domain.Player {
	players := make([]domain.Player, n)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("Fighter %02d", i+1), Dojo: fmt.Sprintf("Club %02d", i+1)}
	}
	return players
}

// TestBlankLayout_HasTheDrawsShape pins the blank layout to the draw: for each
// format, the blank template of a competition expecting N places prints the
// same sheets, the same pools, matches, rounds, 3rd-place block, tree pages,
// Kachinuki Detail sections and Time Estimator counts as the stored-draw
// export of the same competition drawn over N competitors, cell for cell, with
// each place empty where the draw prints a competitor. The Data sheet holds one
// row per place in both; the drawn export alone prints Names to Print and Tags.
func TestBlankLayout_HasTheDrawsShape(t *testing.T) {
	for _, tc := range blankLayoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			saveBlankLayoutComp(t, store, "blank", tc)
			blank, err := eng.ExportCompetitionXlsx("blank")
			require.NoError(t, err)

			saveBlankLayoutComp(t, store, "drawn", tc)
			roster := drawnRoster(tc.places)
			require.NoError(t, store.SaveParticipants("drawn", roster))
			require.NoError(t, eng.GenerateDraw("drawn"))
			drawn, err := eng.ExportCompetitionXlsx("drawn")
			require.NoError(t, err)

			people := map[string]bool{}
			for _, p := range roster {
				people[p.Name], people[p.Dojo] = true, true
			}
			for i := 1; i <= tc.places; i++ {
				people[fmt.Sprintf("Q%d", i)] = true
			}

			bf := openXlsx(t, blank)
			df := openXlsx(t, drawn)
			drawnSheets := withoutNameSheets(df.GetSheetList())
			assert.Equal(t, drawnSheets, bf.GetSheetList(),
				"the blank layout prints the draw's sheets, without Names to Print and Tags")

			linked := 0
			for _, sheet := range drawnSheets {
				if sheet == helper.SheetData {
					assert.Equal(t, dataRows(t, df), dataRows(t, bf), "one Data sheet row per place")
					continue
				}
				want := sheetShape(t, df, sheet, people)
				got := sheetShape(t, bf, sheet, nil)
				assert.Equalf(t, want, got, "sheet %q: the blank layout's shape is the draw's", sheet)
				for _, v := range got {
					if v == "P" || v == "F" {
						linked++
					}
				}
			}
			assert.Positive(t, linked, "the fixture must print places and formulas outside the Data sheet, or the shapes compare nothing")
		})
	}
}

// TestBlankLayout_PrintsNoNameDojoNumberOrSeed pins E3: before the draw the
// template names nobody. A registered roster, a seed and a number prefix are on
// the competition, and none of them (nor the stand-ins the places are drawn
// with) reaches any cell, as a value or inside a formula; every place's name
// and dojo cell on the Data sheet is the empty-text formula, so a name typed
// there later shows on every sheet.
func TestBlankLayout_PrintsNoNameDojoNumberOrSeed(t *testing.T) {
	for _, tc := range blankLayoutCases() {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			saveBlankLayoutComp(t, store, "blank", tc)
			roster := drawnRoster(2)
			require.NoError(t, store.SaveParticipants("blank", roster))
			saved, err := store.LoadParticipants("blank", false)
			require.NoError(t, err)
			require.NoError(t, store.SaveSeeds("blank", []domain.SeedAssignment{{ID: saved[0].ID, Name: saved[0].Name, Dojo: saved[0].Dojo, SeedRank: 1}}))

			data, err := eng.ExportCompetitionXlsx("blank")
			require.NoError(t, err)
			f := openXlsx(t, data)

			forbidden := []string{"Blank place", "Blank dojo", "Blank display", "Fighter 0", "Club 0", "Q1", "Q2"}
			for _, sheet := range f.GetSheetList() {
				assert.Falsef(t, strings.HasPrefix(sheet, helper.SheetNamesToPrint) || sheet == helper.SheetTags,
					"no %q sheet: there is nobody to name", sheet)
				eachCell(t, f, sheet, func(cell, value, formula string) {
					for _, word := range forbidden {
						assert.NotContainsf(t, value, word, "%s!%s prints %q", sheet, cell, value)
						assert.NotContainsf(t, formula, word, "%s!%s formula %q", sheet, cell, formula)
					}
				})
			}
			rows := dataRows(t, f)
			require.Equal(t, tc.places, rows, "one Data sheet row per place")
			for r := 3; r < 3+rows; r++ {
				for _, col := range []string{"B", "C"} {
					formula, err := f.GetCellFormula(helper.SheetData, fmt.Sprintf("%s%d", col, r))
					require.NoError(t, err)
					assert.Equalf(t, `""`, formula, "Data!%s%d is empty text, which a formula reads blank", col, r)
				}
			}
		})
	}
}

// TestBlankLayout_TreePagesFollowTheDataSheet pins that a knockout's tree pages
// read their first-round places from the Data sheet, as the matches sheet does,
// so a name typed there reaches every sheet: each of the six places is a
// reference to its own Data name cell, and typing a name into one shows it on
// the tree page.
func TestBlankLayout_TreePagesFollowTheDataSheet(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	saveBlankLayoutComp(t, store, "blank", blankLayoutCase{"knockout", state.CompFormatKnockout, 0, 6, []string{"A"}, nil})
	data, err := eng.ExportCompetitionXlsx("blank")
	require.NoError(t, err)
	f := openXlsx(t, data)

	treeCellOf := map[string]string{} // Data cell -> the tree cell that reads it
	for _, sheet := range f.GetSheetList() {
		if !strings.HasPrefix(sheet, helper.SheetTree) {
			continue
		}
		eachCell(t, f, sheet, func(cell, _, formula string) {
			if bareDataRef.MatchString(formula) {
				treeCellOf[strings.TrimPrefix(formula, "'data'!")] = sheet + "!" + cell
			}
		})
	}
	require.Len(t, treeCellOf, 6, "each place is on a tree page, reading its own Data cell")

	require.NoError(t, f.SetCellValue(helper.SheetData, "B3", "Typed Name"))
	home := treeCellOf["$B$3"]
	require.NotEmpty(t, home, "the first Data row's name is on a tree page")
	sheet, cell, _ := strings.Cut(home, "!")
	got, err := f.CalcCellValue(sheet, cell)
	require.NoError(t, err)
	assert.Equal(t, "Typed Name", got)
}

// TestBlankPlaceCount pins the count rule (Q1 (a), Q4 (a)): the larger of the
// expected number and everyone registered, checked in or not, and fewer than 2
// is ErrBlankLayoutTooFew, an IsUnexportable sentinel naming the fix.
func TestBlankPlaceCount(t *testing.T) {
	cases := []struct {
		name       string
		expected   int
		registered int
		checkedIn  int
		want       int
	}{
		{"the expected number when more than registered", 8, 3, 0, 8},
		{"everyone registered when more than expected", 4, 6, 0, 6},
		{"registered with no expected number", 0, 5, 0, 5},
		{"everyone registered whoever checked in", 0, 5, 2, 5},
		{"too few", 1, 1, 0, 0},
		{"nobody", 0, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			createTestCompetition(t, store, "c", state.CompFormatKnockout, 0, func(c *state.Competition) {
				c.ExpectedParticipants = tc.expected
				c.CheckInEnabled = true
			})
			roster := drawnRoster(tc.registered)
			for i := range roster[:tc.checkedIn] {
				roster[i].CheckedIn = true
			}
			if tc.registered > 0 {
				require.NoError(t, store.SaveParticipants("c", roster))
			}
			comp, err := store.LoadCompetition("c")
			require.NoError(t, err)
			n, err := eng.blankPlaceCount(comp)
			if tc.want == 0 {
				require.ErrorIs(t, err, ErrBlankLayoutTooFew)
				assert.True(t, IsUnexportable(err))
				assert.Contains(t, err.Error(), "set the expected number of participants in this competition's Settings")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, n)
		})
	}
}

// TestExportTournamentWorkbooks_PrintsABlankLayoutAndSkipsOneItCannotSize pins
// E2: Export PDFs prints a competition with no draw as its blank layout, and
// leaves out, with the reason naming the fix, one with fewer than 2 places or
// whose settings cannot form a draw.
func TestExportTournamentWorkbooks_PrintsABlankLayoutAndSkipsOneItCannotSize(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "sized", state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Name = "Sized"
		c.ExpectedParticipants = 9
	})
	createTestCompetition(t, store, "unsized", state.CompFormatKnockout, 0, func(c *state.Competition) { c.Name = "Unsized" })
	createTestCompetition(t, store, "one-pool", state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Name = "One pool"
		c.ExpectedParticipants = 4
	})

	sources, skipped, err := eng.ExportTournamentWorkbooks(t.TempDir(), "sized", "unsized", "one-pool")
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, "Sized", sources[0].Title)

	require.Len(t, skipped, 2)
	assert.Equal(t, "unsized", skipped[0].ID)
	assert.Equal(t, ErrBlankLayoutTooFew.Error(), skipped[0].Reason)
	assert.Equal(t, "one-pool", skipped[1].ID)
	assert.Contains(t, skipped[1].Reason, ErrBlankLayoutUnbuildable.Error()+" for 4 places")
	assert.Contains(t, skipped[1].Reason, "needs at least 2 pools")
}

// TestBlankLayout_IgnoresALeftoverDraw: a setup competition's pools.csv is the
// leftover of a draw that was refused or discarded, never a draw that stands,
// so the blank layout prints, whatever is on disk.
func TestBlankLayout_IgnoresALeftoverDraw(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "c", state.CompFormatMixed, 3, nil)
	saveTestParticipants(t, store, "c", []string{"Ann", "Bea", "Cid", "Dan", "Eve", "Fay"})
	require.NoError(t, eng.GenerateDraw("c"))
	require.NoError(t, eng.DiscardDraw("c"))
	comp, err := store.LoadCompetition("c")
	require.NoError(t, err)
	require.True(t, state.CanGenerateDraw(comp.Status))
	require.NoError(t, store.SavePools("c", []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{{ID: "x", Name: "Ann", Dojo: "DojoA"}}}}))

	data, err := eng.ExportCompetitionXlsx("c")
	require.NoError(t, err)
	f := openXlsx(t, data)
	assert.Equal(t, 6, dataRows(t, f), "six places, one per registered competitor")
	eachCell(t, f, helper.SheetData, func(cell, value, _ string) {
		assert.NotEqualf(t, "Ann", value, "Data!%s names a competitor", cell)
	})
}

// TestBlankLayout_PlacesReadBlankInLibreOffice is the spike made a test: the
// PDF prints render the workbook through LibreOffice, where a formula
// referring to an EMPTY cell reads 0. Every cell of the Pool Draw and
// Elimination Matches sheets that refers to the Data sheet (a place's name or
// dojo) must read blank there, never "0".
func TestBlankLayout_PlacesReadBlankInLibreOffice(t *testing.T) {
	soffice, err := pdf.LocateSoffice()
	if err != nil {
		t.Skipf("skipping: %v (install LibreOffice or use the CI test-pdf job)", err)
	}

	for _, tc := range []blankLayoutCase{
		{"pools", state.CompFormatMixed, 3, 9, []string{"A"}, nil},
		{"knockout", state.CompFormatKnockout, 0, 6, []string{"A"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			saveBlankLayoutComp(t, store, "blank", tc)
			data, err := eng.ExportCompetitionXlsx("blank")
			require.NoError(t, err)

			dir := t.TempDir()
			src := filepath.Join(dir, "blank.xlsx")
			require.NoError(t, os.WriteFile(src, data, 0o600))
			profile := "file://" + filepath.ToSlash(filepath.Join(dir, "profile"))
			cmd := exec.Command(soffice, "-env:UserInstallation="+profile, "--headless", "--convert-to",
				"csv:Text - txt - csv (StarCalc):44,34,76,1,,0,false,true,false,false,false,-1", "--outdir", dir, src)
			out, err := cmd.CombinedOutput()
			require.NoErrorf(t, err, "soffice: %s", out)

			f := openXlsx(t, data)
			checked := 0
			for _, sheet := range []string{helper.SheetPoolDraw, helper.SheetEliminationMatches} {
				if !contains(f.GetSheetList(), sheet) {
					continue
				}
				shown := readCSV(t, filepath.Join(dir, "blank-"+sheet+".csv"))
				eachCell(t, f, sheet, func(cell, _, formula string) {
					if !strings.Contains(formula, helper.SheetData) {
						return
					}
					col, row, err := excelize.CellNameToCoordinates(cell)
					require.NoError(t, err)
					var v string
					if row-1 < len(shown) && col-1 < len(shown[row-1]) {
						v = shown[row-1][col-1]
					}
					assert.NotEqualf(t, "0", strings.TrimSpace(v), "%s!%s (%s) reads 0 in LibreOffice", sheet, cell, formula)
					checked++
				})
			}
			assert.Positive(t, checked, "the fixture must have cells that refer to the Data sheet")
		})
	}
}

func openXlsx(t *testing.T, data []byte) *excelize.File {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, f.Close()) })
	return f
}

func withoutNameSheets(sheets []string) []string {
	var out []string
	for _, s := range sheets {
		if strings.HasPrefix(s, helper.SheetNamesToPrint) || s == helper.SheetTags {
			continue
		}
		out = append(out, s)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// dataRows counts the Data sheet's place rows: row 3 on, while column B holds
// a name or a formula.
func dataRows(t *testing.T, f *excelize.File) int {
	t.Helper()
	n := 0
	for r := 3; ; r++ {
		cell := fmt.Sprintf("B%d", r)
		v, err := f.GetCellValue(helper.SheetData, cell)
		require.NoError(t, err)
		formula, err := f.GetCellFormula(helper.SheetData, cell)
		require.NoError(t, err)
		if v == "" && formula == "" {
			return n
		}
		n++
	}
}

// eachCell visits every cell sheet's XML holds (a formula with no cached value
// included, which GetRows reads as empty and a sheet's dimension may not
// cover) with its written value and formula.
func eachCell(t *testing.T, f *excelize.File, sheet string, visit func(cell, value, formula string)) {
	t.Helper()
	for _, cell := range sheetCells(t, f, sheet) {
		value, err := f.GetCellValue(sheet, cell, excelize.Options{RawCellValue: true})
		require.NoError(t, err)
		formula, err := f.GetCellFormula(sheet, cell)
		require.NoError(t, err)
		visit(cell, value, formula)
	}
}

var (
	sheetEntry = regexp.MustCompile(`<sheet [^>]*name="([^"]*)"[^>]*r:id="([^"]*)"`)
	relEntry   = regexp.MustCompile(`<Relationship [^>]*Id="([^"]*)"[^>]*Target="([^"]*)"`)
	cellRef    = regexp.MustCompile(`<c r="([A-Z]+[0-9]+)"`)
)

// sheetCells lists the references of every cell element in sheet's XML, read
// from the saved workbook.
func sheetCells(t *testing.T, f *excelize.File, sheet string) []string {
	t.Helper()
	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	read := func(name string) string {
		rc, err := zr.Open(name)
		require.NoErrorf(t, err, "open %s", name)
		defer rc.Close()
		raw, err := io.ReadAll(rc)
		require.NoError(t, err)
		return string(raw)
	}
	targets := map[string]string{}
	for _, m := range relEntry.FindAllStringSubmatch(read("xl/_rels/workbook.xml.rels"), -1) {
		targets[m[1]] = m[2]
	}
	for _, m := range sheetEntry.FindAllStringSubmatch(read("xl/workbook.xml"), -1) {
		if html.UnescapeString(m[1]) != sheet {
			continue
		}
		var cells []string
		for _, c := range cellRef.FindAllStringSubmatch(read("xl/"+strings.TrimPrefix(targets[m[2]], "/xl/")), -1) {
			cells = append(cells, c[1])
		}
		return cells
	}
	t.Fatalf("no sheet %q in the workbook", sheet)
	return nil
}

// bareDataRef is a formula that reads one Data sheet cell and nothing else: how
// the blank layout prints a place on the sheets where the draw prints a literal
// name (printsPlacesLiterally), so that a name typed on the Data sheet shows
// there too.
var bareDataRef = regexp.MustCompile(`^'data'!\$[A-Z]+\$\d+$`)

// printsPlacesLiterally reports whether the draw prints a competitor's name on
// the sheet as a value of its own rather than by formula.
func printsPlacesLiterally(sheet string) bool {
	return strings.HasPrefix(sheet, helper.SheetTree) || sheet == helper.SheetKachinukiDetail
}

// sheetShape is a sheet read as its layout: per cell, "" for an empty cell,
// "P" for a place (a competitor's name, dojo or number in people, or the blank
// layout's empty-text formula), "F" for any other formula, and the text of any
// other value.
func sheetShape(t *testing.T, f *excelize.File, sheet string, people map[string]bool) map[string]string {
	t.Helper()
	shape := map[string]string{}
	eachCell(t, f, sheet, func(cell, value, formula string) {
		switch {
		case formula == `""` || (formula == "" && people[value]) || (printsPlacesLiterally(sheet) && bareDataRef.MatchString(formula)):
			shape[cell] = "P"
		case formula != "":
			shape[cell] = "F"
		case value != "":
			shape[cell] = value
		}
	})
	return shape
}

func readCSV(t *testing.T, path string) [][]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoErrorf(t, err, "LibreOffice wrote no %s", filepath.Base(path))
	var rows [][]string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		rows = append(rows, strings.Split(line, ","))
	}
	return rows
}

// TestBlankLayout_RefusesWhatTheDrawRefuses: a mixed competition whose pools
// form but whose knockout cannot be drawn (fill-bracket on a shape it does not
// support), or whose court count the draw refuses, is ErrBlankLayoutUnbuildable,
// never a workbook without the Tree sheets.
func TestBlankLayout_RefusesWhatTheDrawRefuses(t *testing.T) {
	cases := []struct {
		name   string
		courts []string
		opts   func(*state.Competition)
	}{
		{"knockout cannot be drawn", []string{"A"}, func(c *state.Competition) { c.ExtraQualifiers = state.ExtraQualifiersFillBracket }},
		{"court count the draw refuses", []string{"A", "B", "C"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			createTestCompetition(t, store, "blank", state.CompFormatMixed, 3, func(c *state.Competition) {
				c.Courts = tc.courts
				c.ExpectedParticipants = 12
				if tc.opts != nil {
					tc.opts(c)
				}
			})
			_, err := eng.ExportCompetitionXlsx("blank")
			require.ErrorIs(t, err, ErrBlankLayoutUnbuildable)

			require.NoError(t, store.SaveParticipants("blank", drawnRoster(12)))
			assert.Error(t, eng.GenerateDraw("blank"), "the draw refuses the same competition")
		})
	}
}
