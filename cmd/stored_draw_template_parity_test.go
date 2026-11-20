package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestStoredDrawTemplateMatchesCreate pins the app's stored-draw template to
// the shape of the CLI's /create. Once a competition has a draw, "Download
// blank template" downloads the stored-draw export
// (engine.ExportCompetitionXlsx, GET /api/competitions/:id/export), so the
// template prints the draw the app runs and the PDFs print (before a draw it
// is the blank layout, which TestBlankLayout_HasTheDrawsShape pins). For the
// same settings, the stored-draw workbook
// must carry what /create's does: the same sheets (the stored one adds the
// Tags sheet a knockout-only /create leaves out), the same number of formulas
// and values on each (pool counting formulas, the kachinuki bout rows, the
// 3rd-place block, a league with one pool and no knockout, the partial pool
// format, zekken names, engi, the number prefix, seeds), none of them
// collapsed to "=0", and the same Time Estimator inputs. Which competitor
// sits where may differ: that is the draw.
func TestStoredDrawTemplateMatchesCreate(t *testing.T) {
	cases := []struct {
		name  string
		comp  state.Competition
		n     int
		seeds int
	}{
		{"league", state.Competition{Kind: "individual", Format: state.CompFormatLeague, PoolSize: 6, PoolWinners: 2, RoundRobin: true}, 6, 0},
		{"pools", state.Competition{Kind: "individual", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2, RoundRobin: true}, 9, 0},
		{"pools seeded", state.Competition{Kind: "individual", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2, RoundRobin: true}, 9, 3},
		{"partial pools", state.Competition{Kind: "individual", Format: state.CompFormatMixed, PoolSize: 4, PoolWinners: 2, PoolFormat: "partial"}, 8, 0},
		{"team pools", state.Competition{Kind: "team", TeamSize: 3, Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2, RoundRobin: true}, 6, 0},
		{"team league", state.Competition{Kind: "team", TeamSize: 3, Format: state.CompFormatLeague, PoolSize: 4, PoolWinners: 2, RoundRobin: true}, 4, 0},
		{"kachinuki pools", state.Competition{Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki, Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2, RoundRobin: true}, 6, 0},
		{"knockout", state.Competition{Kind: "individual", Format: state.CompFormatKnockout}, 8, 0},
		{"knockout single 3rd", state.Competition{Kind: "individual", Format: state.CompFormatKnockout, Naginata: true}, 8, 0},
		{"kachinuki knockout", state.Competition{Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki, Format: state.CompFormatKnockout}, 4, 0},
		{"zekken and prefix", state.Competition{Kind: "individual", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2, RoundRobin: true, WithZekkenName: true, NumberPrefix: "K"}, 6, 0},
		{"two courts pools", state.Competition{Kind: "individual", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2, RoundRobin: true, Courts: []string{"A", "B"}}, 12, 0},
		{"two courts knockout", state.Competition{Kind: "individual", Format: state.CompFormatKnockout, Courts: []string{"A", "B"}}, 8, 0},
		{"engi", state.Competition{Kind: "individual", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2, RoundRobin: true, Engi: true}, 6, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, err := state.NewStore(t.TempDir())
			require.NoError(t, err)
			eng := engine.New(store)
			comp := tc.comp
			comp.ID, comp.Name, comp.StartTime, comp.Status, comp.PoolSizeMode = "c1", "Comp", "09:00", "setup", "min"
			if len(comp.Courts) == 0 {
				comp.Courts = []string{"A"}
			}
			require.NoError(t, store.SaveCompetition(&comp))

			players := make([]domain.Player, tc.n)
			lines := make([]string, tc.n)
			var seeds []domain.SeedAssignment
			for i := range players {
				name := fmt.Sprintf("P%02d", i+1)
				if comp.Engi {
					name = fmt.Sprintf("A%02d - B%02d", i+1, i+1)
				}
				dojo, zekken := fmt.Sprintf("D%d", i), fmt.Sprintf("Z%02d", i+1)
				players[i] = domain.Player{Name: name, Dojo: dojo, DisplayName: zekken}
				lines[i] = name + ", " + dojo
				if comp.WithZekkenName {
					lines[i] = name + ", " + zekken + ", " + dojo
				}
				if i < tc.seeds {
					seeds = append(seeds, domain.SeedAssignment{Name: name, Dojo: dojo, SeedRank: i + 1})
				}
			}
			require.NoError(t, store.SaveParticipants("c1", players))
			if len(seeds) > 0 {
				require.NoError(t, store.SaveSeeds("c1", seeds))
			}
			require.NoError(t, eng.GenerateDraw("c1"))
			data, err := eng.ExportCompetitionXlsx("c1")
			require.NoError(t, err)
			stored, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err)
			t.Cleanup(func() { _ = stored.Close() })

			created := postCreate(t, createFormFor(t, &comp, tc.n, lines, seeds))

			withoutTags := func(sheets []string) []string {
				out := slices.DeleteFunc(slices.Clone(sheets), func(s string) bool { return s == helper.SheetTags })
				slices.Sort(out)
				return out
			}
			require.Equal(t, withoutTags(created.GetSheetList()), withoutTags(stored.GetSheetList()),
				"the stored-draw template must carry /create's sheets")

			for _, sheet := range created.GetSheetList() {
				cf, cv := sheetCells(t, created, sheet)
				sf, sv := sheetCells(t, stored, sheet)
				assert.Equal(t, len(cf), len(sf), "%s: formula count", sheet)
				assert.Equal(t, cv, sv, "%s: value count", sheet)
				for cell, formula := range sf {
					assert.NotEqual(t, "0", strings.TrimPrefix(formula, "="), "%s!%s collapsed to =0 (mp-x0u9)", sheet, cell)
				}
			}
			for _, cell := range []string{"A2", "B2", "C2", "A8", "B8", "A14"} {
				want, err := created.GetCellValue(helper.SheetTimeEstimator, cell)
				require.NoError(t, err)
				got, err := stored.GetCellValue(helper.SheetTimeEstimator, cell)
				require.NoError(t, err)
				assert.Equal(t, want, got, "Time Estimator %s", cell)
			}
		})
	}
}

// createFormFor is the /create form the CLI's web page posts for comp: the
// stateless draw the stored-draw export is pinned to.
func createFormFor(t *testing.T, comp *state.Competition, n int, lines []string, seeds []domain.SeedAssignment) url.Values {
	t.Helper()
	knockout := comp.Format == state.CompFormatKnockout
	perPool := comp.PoolSize
	if comp.Format == state.CompFormatLeague || perPool == 0 {
		perPool = n
	}
	winners := comp.PoolWinners
	if winners == 0 {
		winners = 2
	}
	if winners >= perPool {
		winners = max(1, perPool-1)
	}
	form := url.Values{
		"tournamentType": {"pools"},
		"playerList":     {strings.Join(lines, "\n")},
		"courts":         {strconv.Itoa(len(comp.Courts))},
		"winnersPerPool": {strconv.Itoa(winners)},
		"playersPerPool": {strconv.Itoa(perPool)},
		"poolSizeMode":   {"min"},
		"teamMatches":    {strconv.Itoa(comp.TeamSize)},
		"titlePrefix":    {comp.Name},
		"numberPrefix":   {comp.NumberPrefix},
		"determined":     {"on"},
	}
	if knockout {
		form.Set("tournamentType", "knockout")
	}
	if comp.Format == state.CompFormatLeague || comp.RoundRobin {
		form.Set("roundRobin", "on")
	}
	if comp.Format == state.CompFormatLeague {
		form.Set("format", state.CompFormatLeague)
	}
	if comp.PoolFormat == "partial" {
		form.Set("poolFormat", "partial")
	}
	if comp.WithZekkenName {
		form.Set("withZekkenName", "on")
	}
	if comp.Engi {
		form.Set("engi", "on")
	}
	if comp.IsKachinuki() {
		form.Set("teamMatchType", "kachinuki")
	}
	if !comp.EffectiveTwoThirdPlaces() {
		form.Set("thirdPlaceMatch", "on")
	}
	if len(seeds) > 0 {
		// The form carries name, dojo and rank only: /create's
		// roster carries no ids, and SaveSeeds stamped them on these rows.
		bare := make([]domain.SeedAssignment, len(seeds))
		for i, s := range seeds {
			bare[i] = domain.SeedAssignment{Name: s.Name, Dojo: s.Dojo, SeedRank: s.SeedRank}
		}
		js, err := json.Marshal(bare)
		require.NoError(t, err)
		form.Set("seeds", string(js))
	}
	return form
}

// sheetCells returns a sheet's formulas by cell, and how many cells hold a
// value but no formula.
func sheetCells(t *testing.T, f *excelize.File, sheet string) (map[string]string, int) {
	t.Helper()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	formulas, values := map[string]string{}, 0
	for ri, row := range rows {
		for ci := 0; ci < width+2; ci++ {
			cell, err := excelize.CoordinatesToCellName(ci+1, ri+1)
			require.NoError(t, err)
			formula, err := f.GetCellFormula(sheet, cell)
			require.NoError(t, err)
			switch {
			case formula != "":
				formulas[cell] = formula
			case ci < len(row) && row[ci] != "":
				values++
			}
		}
	}
	return formulas, values
}
