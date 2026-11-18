package export

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The results workbook keeps the pool standings as the sheet's own formulas
// (operator decision E4) and writes the app's Rank and Ranking only in a pool
// where the sheet's Rank would differ from the app's, with a note saying why
// (operator decision Q2 (b)). These tests evaluate the sheet with excelize's
// CalcCellValue, which works out every function the standings use (IF, OR,
// AND, COUNTA, LEN, SUBSTITUTE, ISNUMBER, N, UPPER, RANK, COUNTIF, INDEX,
// MATCH, IFERROR), so no LibreOffice recalculation is needed here.

// shownStanding is one competitor's standings row as the sheet shows it: each
// header label to the value the cell gives.
type shownStanding map[string]string

// poolOnSheet is one pool's standings read off a results workbook.
type poolOnSheet struct {
	area    *poolStandingsArea
	rows    []shownStanding // pool.Players order
	ranking []string        // the Ranking section, place 1 first
}

// readPoolsOnSheet reads every pool's standings off a single-shiaijo results
// workbook, working each formula out with excelize's evaluator.
func readPoolsOnSheet(t *testing.T, f *excelize.File, pools []helper.Pool) []poolOnSheet {
	t.Helper()
	sheet := helper.SheetPoolMatches
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	all := make([]int, len(pools))
	for i := range pools {
		all[i] = i
	}
	areas := locatePoolStandings(rows, pools, [][]int{all})
	require.Len(t, areas, len(pools), "every pool's standings must be found")

	var out []poolOnSheet
	for _, a := range areas {
		n := len(a.pool.Players)
		labels := func(headerRow int) map[int]string {
			m := map[int]string{}
			for c := a.startCol + 1; c < a.startCol+standingsColumns; c++ {
				v, err := f.GetCellValue(sheet, fmt.Sprintf("%s%d", colNum(c), headerRow))
				require.NoError(t, err)
				if v != "" {
					m[c] = v
				}
			}
			return m
		}
		p := poolOnSheet{area: a, rows: make([]shownStanding, n)}
		read := func(i, row int, cols map[int]string) {
			if p.rows[i] == nil {
				p.rows[i] = shownStanding{}
			}
			for c, label := range cols {
				p.rows[i][label] = shown(t, f, fmt.Sprintf("%s%d", colNum(c), row))
			}
		}
		first := labels(a.tableRows[0] - 1)
		for i := 0; i < n; i++ {
			read(i, a.tableRows[i], first)
		}
		if len(a.tableRows) > n { // a team pool's IV/IL/IT/PW/PL table
			second := labels(a.tableRows[n] - 1)
			for i := 0; i < n; i++ {
				read(i, a.tableRows[n+i], second)
			}
		}
		for _, ref := range a.rankingRefs() {
			p.ranking = append(p.ranking, shown(t, f, ref))
		}
		out = append(out, p)
	}
	return out
}

// shown is what a cell shows: its formula worked out, or its value.
func shown(t *testing.T, f *excelize.File, ref string) string {
	t.Helper()
	sheet := helper.SheetPoolMatches
	if isFormula(t, f, ref) {
		v, err := f.CalcCellValue(sheet, ref, excelize.Options{RawCellValue: true})
		require.NoError(t, err, "excelize must evaluate %s", ref)
		return v
	}
	v, err := f.GetCellValue(sheet, ref)
	require.NoError(t, err)
	return v
}

func isFormula(t *testing.T, f *excelize.File, ref string) bool {
	t.Helper()
	fm, err := f.GetCellFormula(helper.SheetPoolMatches, ref)
	require.NoError(t, err)
	return fm != ""
}

// buildAndOpen builds the results workbook and reopens it from its bytes, as a
// download would be read.
func buildAndOpen(t *testing.T, store *state.Store, eng *engine.Engine, compID string) (*excelize.File, []helper.Pool) {
	t.Helper()
	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	return f, pools
}

// assertLiveCell checks a standings cell is still a formula, and that the
// value cached with it (what a reader that computes no formula shows) is the
// value the formula gives.
func assertLiveCell(t *testing.T, f *excelize.File, ref string) {
	t.Helper()
	require.True(t, isFormula(t, f, ref), "%s must stay a formula", ref)
	cached, err := f.GetCellValue(helper.SheetPoolMatches, ref)
	require.NoError(t, err)
	calc, err := f.CalcCellValue(helper.SheetPoolMatches, ref, excelize.Options{RawCellValue: true})
	require.NoError(t, err)
	assert.Equal(t, calc, cached, "%s must cache the value its formula gives", ref)
}

// assertCountsLive checks the count columns startCol+1 .. startCol+last of
// each given row stay formulas caching their own result.
func assertCountsLive(t *testing.T, f *excelize.File, a *poolStandingsArea, rows []int, last int) {
	t.Helper()
	for _, r := range rows {
		for c := a.startCol + 1; c <= a.startCol+last; c++ {
			assertLiveCell(t, f, fmt.Sprintf("%s%d", colNum(c), r))
		}
	}
}

// assertNoNote checks no Rank/Ranking note was written for the pool.
func assertNoNote(t *testing.T, f *excelize.File, a *poolStandingsArea) {
	t.Helper()
	v, err := f.GetCellValue(helper.SheetPoolMatches, fmt.Sprintf("%s%d", colNum(a.startCol), a.rankingRow))
	require.NoError(t, err)
	assert.Empty(t, v, "a pool whose formulas agree with the app carries no note")
}

func appStandingsOf(t *testing.T, eng *engine.Engine, compID string, pool helper.Pool) []state.PlayerStanding {
	t.Helper()
	all, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	byID := standingMap(all[pool.PoolName])
	out := make([]state.PlayerStanding, len(pool.Players))
	for i, p := range pool.Players {
		ps, ok := byID[p.ID]
		require.True(t, ok, "the app must hold a standing for %s", p.ID)
		out[i] = ps
	}
	return out
}

func rankingByApp(standings []state.PlayerStanding) []string {
	names := make([]string, len(standings))
	for _, ps := range standings {
		names[ps.Rank-1] = ps.Player.Name
	}
	return names
}

func TestResultsWorkbook_IndividualStandingsFormulasEqualTheApp(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pool := helper.Pool{PoolName: "Pool A", Players: []helper.Player{makePlayer("Alice"), makePlayer("Bob"), makePlayer("Carol")}}
	require.NoError(t, store.SavePools(compID, []helper.Pool{pool}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		// Alice beats Bob 2-0; Alice and Carol draw 1-1; Bob beats Carol 1-0.
		{ID: "Pool A-0", SideA: "Alice", SideAID: "Alice", SideB: "Bob", SideBID: "Bob", IpponsA: []string{"M", "K"}, Decision: "fought", Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: "Alice"},
		{ID: "Pool A-1", SideA: "Alice", SideAID: "Alice", SideB: "Carol", SideBID: "Carol", IpponsA: []string{"K"}, IpponsB: []string{"M"}, Decision: "hikiwake", Status: state.MatchStatusCompleted},
		{ID: "Pool A-2", SideA: "Bob", SideAID: "Bob", SideB: "Carol", SideBID: "Carol", IpponsA: []string{"D"}, Decision: "fought", Status: state.MatchStatusCompleted, Winner: "Bob", WinnerID: "Bob"},
	}))

	f, pools := buildAndOpen(t, store, eng, compID)
	sheet := readPoolsOnSheet(t, f, pools)[0]
	app := appStandingsOf(t, eng, compID, pools[0])

	for i, ps := range app {
		row := sheet.rows[i]
		assert.Equal(t, fmt.Sprint(ps.Wins), row["W"], "%s W", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.Losses), row["L"], "%s L", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.Draws), row["T"], "%s T", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.IpponsGiven), row["PW"], "%s PW", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.IpponsTaken), row["PL"], "%s PL", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.Rank), row["Rank"], "%s Rank", ps.Player.Name)
	}
	assert.Equal(t, rankingByApp(app), sheet.ranking, "the Ranking formulas name the app's order")

	// W, L, T, PW, PL and Rank all stay formulas carrying their own result.
	assertCountsLive(t, f, sheet.area, sheet.area.tableRows, 6)
	for _, ref := range sheet.area.rankingRefs() {
		assert.True(t, isFormula(t, f, ref), "Ranking %s must stay a formula", ref)
	}
	assertNoNote(t, f, sheet.area)
}

func TestResultsWorkbook_TeamStandingsFormulasEqualTheApp(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "team"
	comp.TeamSize = 3
	comp.Format = state.CompFormatMixed
	require.NoError(t, store.SaveCompetition(comp))

	pool := helper.Pool{PoolName: "Pool A", Players: []helper.Player{makePlayer("North"), makePlayer("South"), makePlayer("West")}}
	require.NoError(t, store.SavePools(compID, []helper.Pool{pool}))
	sub := func(pos int, a, b string, ia, ib []string, winner string) state.SubMatchResult {
		s := state.SubMatchResult{Position: pos, SideA: a, SideB: b, IpponsA: ia, IpponsB: ib, Winner: winner}
		if winner == "" {
			s.Decision = "hikiwake"
		}
		return s
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		// North beats South on victories 2-1.
		{ID: "Pool A-0", SideA: "North", SideAID: "North", SideB: "South", SideBID: "South", Status: state.MatchStatusCompleted, Winner: "North", WinnerID: "North",
			SubResults: []state.SubMatchResult{
				sub(1, "North", "South", []string{"M", "K"}, nil, "North"),
				sub(2, "North", "South", nil, []string{"M"}, "South"),
				sub(3, "North", "South", []string{"D"}, nil, "North"),
			}},
		// North and West level on victories 1-1 (one drawn bout); West wins on points 3-2.
		{ID: "Pool A-1", SideA: "North", SideAID: "North", SideB: "West", SideBID: "West", Status: state.MatchStatusCompleted, Winner: "West", WinnerID: "West",
			SubResults: []state.SubMatchResult{
				sub(1, "North", "West", []string{"K"}, []string{"M"}, ""),
				sub(2, "North", "West", nil, []string{"M", "K"}, "West"),
				sub(3, "North", "West", []string{"M"}, nil, "North"),
			}},
		// South beats West on victories 2-1.
		{ID: "Pool A-2", SideA: "South", SideAID: "South", SideB: "West", SideBID: "West", Status: state.MatchStatusCompleted, Winner: "South", WinnerID: "South",
			SubResults: []state.SubMatchResult{
				sub(1, "South", "West", []string{"M"}, nil, "South"),
				sub(2, "South", "West", []string{"M"}, nil, "South"),
				sub(3, "South", "West", nil, []string{"K"}, "West"),
			}},
	}))

	f, pools := buildAndOpen(t, store, eng, compID)
	sheet := readPoolsOnSheet(t, f, pools)[0]
	app := appStandingsOf(t, eng, compID, pools[0])

	for i, ps := range app {
		row := sheet.rows[i]
		assert.Equal(t, fmt.Sprint(ps.Wins), row["W"], "%s W", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.Losses), row["L"], "%s L", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.Draws), row["T"], "%s T", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.IndividualWins), row["IV"], "%s IV", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.IndividualLosses), row["IL"], "%s IL", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.IndividualDraws), row["IT"], "%s IT", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.PointsWon), row["PW"], "%s PW", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.PointsLost), row["PL"], "%s PL", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.Rank), row["Rank"], "%s Rank", ps.Player.Name)
	}
	assert.Equal(t, rankingByApp(app), sheet.ranking, "the Ranking formulas name the app's order")
	n := len(pools[0].Players)
	// W, L, T (and Rank, at +6) in the first table; IV, IL, IT, PW, PL in the second.
	assertCountsLive(t, f, sheet.area, sheet.area.tableRows[:n], 3)
	for _, ref := range sheet.area.rankRefs() {
		assertLiveCell(t, f, ref)
	}
	assertCountsLive(t, f, sheet.area, sheet.area.tableRows[n:], 5)
	assertNoNote(t, f, sheet.area)
}

func TestResultsWorkbook_EngiStandingsFormulasEqualTheApp(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)
	markCompAsEngi(t, store, compID)

	pool := helper.Pool{PoolName: "Pool A", Players: []helper.Player{
		{ID: "pair1", Name: "One A - Two A", Dojo: "DojoA"},
		{ID: "pair2", Name: "One B - Two B", Dojo: "DojoB"},
		{ID: "pair3", Name: "One C - Two C", Dojo: "DojoC"},
	}}
	require.NoError(t, store.SavePools(compID, []helper.Pool{pool}))
	engiMatch := func(id, a, b string, fa, fb int) state.MatchResult {
		w := a
		if fb > fa {
			w = b
		}
		return state.MatchResult{ID: id, SideA: a, SideAID: a, SideB: b, SideBID: b, FlagsA: fa, FlagsB: fb, Decision: "fought", Status: state.MatchStatusCompleted, Winner: w, WinnerID: w}
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		engiMatch("Pool A-0", "pair1", "pair2", 3, 2),
		engiMatch("Pool A-1", "pair1", "pair3", 4, 1),
		engiMatch("Pool A-2", "pair2", "pair3", 5, 0),
	}))

	f, pools := buildAndOpen(t, store, eng, compID)
	sheet := readPoolsOnSheet(t, f, pools)[0]
	app := appStandingsOf(t, eng, compID, pools[0])

	for i, ps := range app {
		row := sheet.rows[i]
		assert.Equal(t, fmt.Sprint(ps.Wins), row["W"], "%s W", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.Flags), row[helper.ColHeaderFlags], "%s Flags", ps.Player.Name)
		assert.Equal(t, fmt.Sprint(ps.Rank), row["Rank"], "%s Rank", ps.Player.Name)
	}
	assert.Equal(t, rankingByApp(app), sheet.ranking, "the Ranking formulas name the app's order")
	for _, ref := range sheet.area.rankRefs() {
		assertLiveCell(t, f, ref)
	}
	assertNoNote(t, f, sheet.area)
}

// TestResultsWorkbook_DecidedPoolCountsWhatTheFormulasCount pins what the
// standings formulas give for a pool decided by kiken, fusenpai or hantei:
// the operator accepted that the sheet counts the marks it shows (operator
// decision E4), so a change to it is seen here. The formulas count a score
// cell's characters, so the winner's maru count as two points and the
// withdrawer's "Kiken" (5) or "Fus." (4) mark as that many points, which
// makes the withdrawer read as the winner; "Ht" counts as two points for the
// hantei winner. Where that turns the sheet's Rank against the app's, Rank and
// Ranking are the app's, with a note (operator decision Q2 (b)).
func TestResultsWorkbook_DecidedPoolCountsWhatTheFormulasCount(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		match  state.MatchResult
		alice  shownStanding // the side that withdrew, or lost the hantei
		bob    shownStanding
		appRow bool // Rank and Ranking are the app's, with a note
		note   string
	}{
		{
			name:   "kiken",
			match:  state.MatchResult{Decision: "kiken-voluntary", DecisionBy: "Alice", Winner: "Bob", WinnerID: "Bob"},
			alice:  shownStanding{"W": "1", "L": "0", "T": "0", "PW": "5", "PL": "2", "Rank": "2"},
			bob:    shownStanding{"W": "0", "L": "1", "T": "0", "PW": "2", "PL": "5", "Rank": "1"},
			appRow: true,
			note:   rankingNote([]string{"a match was decided by kiken"}),
		},
		{
			name:   "fusenpai",
			match:  state.MatchResult{Decision: "fusenpai", DecisionBy: "Alice", Winner: "Bob", WinnerID: "Bob"},
			alice:  shownStanding{"W": "1", "L": "0", "T": "0", "PW": "4", "PL": "2", "Rank": "2"},
			bob:    shownStanding{"W": "0", "L": "1", "T": "0", "PW": "2", "PL": "4", "Rank": "1"},
			appRow: true,
			note:   rankingNote([]string{"a match was decided by fusenpai"}),
		},
		{
			name:  "hantei",
			match: state.MatchResult{Decision: "fought", IpponsB: []string{"Ht"}, Winner: "Bob", WinnerID: "Bob"},
			alice: shownStanding{"W": "0", "L": "1", "T": "0", "PW": "0", "PL": "2", "Rank": "2"},
			bob:   shownStanding{"W": "1", "L": "0", "T": "0", "PW": "2", "PL": "0", "Rank": "1"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)
			pool := helper.Pool{PoolName: "Pool A", Players: []helper.Player{makePlayer("Alice"), makePlayer("Bob")}}
			require.NoError(t, store.SavePools(compID, []helper.Pool{pool}))
			m := tc.match
			m.ID, m.SideA, m.SideAID, m.SideB, m.SideBID, m.Status = "Pool A-0", "Alice", "Alice", "Bob", "Bob", state.MatchStatusCompleted
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{m}))

			f, pools := buildAndOpen(t, store, eng, compID)
			sheet := readPoolsOnSheet(t, f, pools)[0]
			assert.Equal(t, tc.alice, sheet.rows[0], "Alice's row")
			assert.Equal(t, tc.bob, sheet.rows[1], "Bob's row")
			assert.Equal(t, []string{"Bob", "Alice"}, sheet.ranking)
			assertCountsLive(t, f, sheet.area, sheet.area.tableRows, 5)
			for _, ref := range append(sheet.area.rankRefs(), sheet.area.rankingRefs()...) {
				assert.Equal(t, !tc.appRow, isFormula(t, f, ref), "%s: a formula exactly where the sheet agrees with the app", ref)
			}
			if tc.appRow {
				assertNote(t, f, sheet.area, tc.note)
			} else {
				assertNoNote(t, f, sheet.area)
			}
		})
	}
}

// assertNote checks the pool's note: written left of the Ranking, merged
// down its rows.
func assertNote(t *testing.T, f *excelize.File, a *poolStandingsArea, want string) {
	t.Helper()
	top := fmt.Sprintf("%s%d", colNum(a.startCol), a.rankingRow)
	v, err := f.GetCellValue(helper.SheetPoolMatches, top)
	require.NoError(t, err)
	assert.Equal(t, want, v, "the note beside the Ranking")
	merged, err := f.GetMergeCells(helper.SheetPoolMatches)
	require.NoError(t, err)
	bottom := fmt.Sprintf("%s%d", colNum(a.startCol+4), a.rankingRow+len(a.pool.Players))
	found := false
	for _, m := range merged {
		if m.GetStartAxis() == top && m.GetEndAxis() == bottom {
			found = true
		}
	}
	assert.True(t, found, "the note is merged over %s:%s", top, bottom)
}

// tiedPool is a pool of two competitors who drew their one match, in the given
// row order.
func tiedPool(t *testing.T, store *state.Store, compID string, names ...string) helper.Pool {
	t.Helper()
	pool := helper.Pool{PoolName: "Pool A", Players: []helper.Player{makePlayer(names[0]), makePlayer(names[1])}}
	require.NoError(t, store.SavePools(compID, []helper.Pool{pool}))
	return pool
}

func drawnMatch(id, a, b string) state.MatchResult {
	return state.MatchResult{ID: id, SideA: a, SideAID: a, SideB: b, SideBID: b, Decision: "hikiwake", Status: state.MatchStatusCompleted}
}

// TestResultsWorkbook_AppOrderOnlyWhereTheSheetsRankDiffers covers operator
// decision Q2 (b): Rank and Ranking are written as the app's values only in a
// pool where the sheet's Rank would differ from the app's, with a note naming
// why, and every other standings cell stays a formula.
func TestResultsWorkbook_AppOrderOnlyWhereTheSheetsRankDiffers(t *testing.T) {
	t.Parallel()

	t.Run("level competitors in the app's order, beside a pool that agrees", func(t *testing.T) {
		t.Parallel()
		dir, store, eng, compID := testSetup(t)
		defer os.RemoveAll(dir)
		// Pool A lists Zed before Amy and they draw: the sheet ranks by row
		// order, the app ranks level competitors by participant id.
		pools := []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{makePlayer("Zed"), makePlayer("Amy")}},
			{PoolName: "Pool B", Players: []helper.Player{makePlayer("Alice"), makePlayer("Bob")}},
		}
		require.NoError(t, store.SavePools(compID, pools))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			drawnMatch("Pool A-0", "Zed", "Amy"),
			{ID: "Pool B-0", SideA: "Alice", SideAID: "Alice", SideB: "Bob", SideBID: "Bob", IpponsA: []string{"M"}, Decision: "fought", Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: "Alice"},
		}))

		f, loaded := buildAndOpen(t, store, eng, compID)
		sheets := readPoolsOnSheet(t, f, loaded)
		a, b := sheets[0], sheets[1]

		assert.Equal(t, "2", a.rows[0]["Rank"], "Zed takes the app's 2nd")
		assert.Equal(t, "1", a.rows[1]["Rank"], "Amy takes the app's 1st")
		assert.Equal(t, []string{"Amy", "Zed"}, a.ranking)
		for _, ref := range append(a.area.rankRefs(), a.area.rankingRefs()...) {
			assert.False(t, isFormula(t, f, ref), "%s is the app's value", ref)
		}
		assertCountsLive(t, f, a.area, a.area.tableRows, 5)
		assertNote(t, f, a.area, rankingNote(nil))

		for _, ref := range b.area.rankRefs() {
			assertLiveCell(t, f, ref)
		}
		for _, ref := range b.area.rankingRefs() {
			assert.True(t, isFormula(t, f, ref), "Pool B's Ranking %s stays a formula", ref)
		}
		assert.Equal(t, []string{"Alice", "Bob"}, b.ranking)
		assertNoNote(t, f, b.area)
	})

	t.Run("a chusen", func(t *testing.T) {
		t.Parallel()
		dir, store, eng, compID := testSetup(t)
		defer os.RemoveAll(dir)
		tiedPool(t, store, compID, "Amy", "Zed")
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{drawnMatch("Pool A-0", "Amy", "Zed")}))
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", "Zed", 1))
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", "Amy", 2))

		f, loaded := buildAndOpen(t, store, eng, compID)
		a := readPoolsOnSheet(t, f, loaded)[0]
		assert.Equal(t, []string{"Zed", "Amy"}, a.ranking)
		assert.Equal(t, "2", a.rows[0]["Rank"])
		assert.Equal(t, "1", a.rows[1]["Rank"])
		assertCountsLive(t, f, a.area, a.area.tableRows, 5)
		assertNote(t, f, a.area, rankingNote([]string{"a chusen settled a tie"}))
	})

	t.Run("a tie-break bout", func(t *testing.T) {
		t.Parallel()
		dir, store, eng, compID := testSetup(t)
		defer os.RemoveAll(dir)
		tiedPool(t, store, compID, "Amy", "Zed")
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			drawnMatch("Pool A-0", "Amy", "Zed"),
			{ID: "Pool A-TB-0", SideA: "Zed", SideAID: "Zed", SideB: "Amy", SideBID: "Amy", IpponsA: []string{"M"}, Decision: "fought", Status: state.MatchStatusCompleted, Winner: "Zed", WinnerID: "Zed"},
		}))

		f, loaded := buildAndOpen(t, store, eng, compID)
		a := readPoolsOnSheet(t, f, loaded)[0]
		assert.Equal(t, []string{"Zed", "Amy"}, a.ranking)
		assertCountsLive(t, f, a.area, a.area.tableRows, 5)
		assertNote(t, f, a.area, rankingNote([]string{"a tie-break bout was fought"}))
	})

	t.Run("a chusen that agrees with the sheet changes nothing", func(t *testing.T) {
		t.Parallel()
		dir, store, eng, compID := testSetup(t)
		defer os.RemoveAll(dir)
		tiedPool(t, store, compID, "Zed", "Amy")
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{drawnMatch("Pool A-0", "Zed", "Amy")}))
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", "Zed", 1))
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", "Amy", 2))

		f, loaded := buildAndOpen(t, store, eng, compID)
		a := readPoolsOnSheet(t, f, loaded)[0]
		assert.Equal(t, []string{"Zed", "Amy"}, a.ranking)
		for _, ref := range a.area.rankRefs() {
			assertLiveCell(t, f, ref)
		}
		assertNoNote(t, f, a.area)
	})
}

func TestRankingNote(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		"Rank and Ranking show the app's order, not the formulas': competitors level on every count are listed in the app's order.",
		rankingNote(nil))
	assert.Equal(t,
		"Rank and Ranking show the app's order, not the formulas': a chusen settled a tie; a match was decided by kiken or fusenpai.",
		rankingNote([]string{"a chusen settled a tie", "a match was decided by kiken or fusenpai"}))
}

func TestRankingDifferenceReasons(t *testing.T) {
	t.Parallel()
	done := state.MatchStatusCompleted
	reasons := rankingDifferenceReasons(
		[]state.PlayerStanding{
			{Player: makePlayer("A"), Rank: 1, IsOverridden: true},
			{Player: makePlayer("B"), Rank: 3},
			{Player: makePlayer("C"), Rank: 3},
		},
		[]state.MatchResult{
			{ID: "Pool A-0", Decision: "kiken-injury", Status: done},
			{ID: "Pool A-1", Decision: "fusenpai", Status: done},
			{ID: "Pool A-2", Decision: "kiken-voluntary", Status: done},
			{ID: "Pool A-3", Decision: "fought", IpponsA: []string{"Ht"}, Winner: "A", SideA: "A", Status: done},
			{ID: "Pool A-TB-0", Decision: "fought", Status: done},
			{ID: "Pool A-DH-0", Decision: "fought", Status: done},
			// A bout not fought yet decides nothing.
			{ID: "Pool A-TB-1", Decision: "fusensho", Status: state.MatchStatusScheduled},
		},
	)
	assert.Equal(t, []string{
		"a chusen settled a tie",
		"a tie-break bout was fought",
		"a representative bout was fought",
		"a match was decided by kiken, fusenpai or hantei",
		"competitors share a place",
	}, reasons)
}

// TestResultsWorkbook_DecidedTeamPoolKeepsIVNumeric: a team encounter decided by
// kiken, fusenpai or fusensho carries its result mark in the competitor's IV
// cell. The cell must stay a number (the mark rides in its number format), or
// the live IV/IL, W/L and Rank formulas that add and compare it fail or invert.
func TestResultsWorkbook_DecidedTeamPoolKeepsIVNumeric(t *testing.T) {
	t.Parallel()
	for _, decision := range []string{"kiken-voluntary", "fusenpai", "fusensho"} {
		t.Run(decision, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)
			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			comp.Kind = "team"
			comp.TeamSize = 3
			comp.Format = state.CompFormatMixed
			require.NoError(t, store.SaveCompetition(comp))

			pool := helper.Pool{PoolName: "Pool A", Players: []helper.Player{makePlayer("North"), makePlayer("South")}}
			require.NoError(t, store.SavePools(compID, []helper.Pool{pool}))
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
				{ID: "Pool A-0", SideA: "North", SideAID: "North", SideB: "South", SideBID: "South",
					Status: state.MatchStatusCompleted, Decision: decision, DecisionBy: "North", Winner: "South", WinnerID: "South"},
			}))

			f, pools := buildAndOpen(t, store, eng, compID)
			sheet := readPoolsOnSheet(t, f, pools)[0]
			app := appStandingsOf(t, eng, compID, pools[0])
			for i, ps := range app {
				row := sheet.rows[i]
				for label, want := range map[string]int{"W": ps.Wins, "L": ps.Losses, "IV": ps.IndividualWins, "IL": ps.IndividualLosses} {
					assert.Equal(t, fmt.Sprint(want), row[label], "%s %s", ps.Player.Name, label)
				}
			}
			for _, ref := range sheet.area.rankRefs() {
				v, err := f.CalcCellValue(helper.SheetPoolMatches, ref, excelize.Options{RawCellValue: true})
				require.NoError(t, err)
				assert.NotContains(t, v, "#", "%s must not be an error", ref)
			}
		})
	}
}
