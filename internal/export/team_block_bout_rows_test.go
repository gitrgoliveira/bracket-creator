package export

import (
	"bytes"
	"fmt"
	"os"
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
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

// startMixedComp draws a pools-then-knockout competition of six entrants in
// two pools of three, with a 3rd-place match, through the real draw pipeline
// on one shiaijo. teamSize 0 is an individual competition.
func startMixedComp(t *testing.T, store *state.Store, eng *engine.Engine, compID string, teamSize int, matchType state.TeamMatchType) {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	singleThird := false
	comp.Kind = "individual"
	if teamSize > 0 {
		comp.Kind = "team"
	}
	comp.TeamSize = teamSize
	comp.TeamMatchType = matchType
	comp.Format = state.CompFormatMixed
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 2
	comp.RoundRobin = true
	comp.StartTime = "09:00"
	comp.Status = state.CompStatusSetup
	comp.TwoThirdPlaces = &singleThird
	require.NoError(t, store.SaveCompetition(comp))

	names := []string{"Ryu", "Tora", "Kame", "Taka", "Kuma", "Hebi"}
	players := make([]domain.Player, len(names))
	for i, n := range names {
		players[i] = domain.Player{Name: n, Dojo: n + " Dojo"}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))
}

// firstRowWith returns the 0-based index of the first row whose cell at col
// equals val, or -1.
func firstRowWith(rows [][]string, col int, val string) int {
	for r, row := range rows {
		if col < len(row) && row[col] == val {
			return r
		}
	}
	return -1
}

// numberedRowsFrom counts the consecutive rows from 0-based row `from` whose
// cell at col carries the bout numbers 1, 2, 3, ... in order.
func numberedRowsFrom(rows [][]string, from, col int) int {
	n := 0
	for r := from; r < len(rows); r++ {
		if col >= len(rows[r]) || rows[r][col] != strconv.Itoa(n+1) {
			break
		}
		n++
	}
	return n
}

// assertTallyFormulaSpans checks the IV formula in the first victories
// column (B) of 1-based row tallyRow adds exactly one clause per bout row,
// firstBout..firstBout+wantRows-1, and reaches no row past the block.
func assertTallyFormulaSpans(t *testing.T, f *excelize.File, sheet string, tallyRow, firstBout, wantRows int, block string) {
	t.Helper()
	formula, err := f.GetCellFormula(sheet, fmt.Sprintf("B%d", tallyRow))
	require.NoError(t, err)
	assert.Equal(t, wantRows, strings.Count(formula, "IF(UPPER("), "%s: one IV clause per bout row", block)
	assert.Contains(t, formula, fmt.Sprintf(`UPPER(D%d)`, firstBout), "%s: the tally starts at the first bout row", block)
	assert.Contains(t, formula, fmt.Sprintf(`UPPER(D%d)`, firstBout+wantRows-1), "%s: the tally reaches the last bout row", block)
	assert.NotContains(t, formula, fmt.Sprintf(`UPPER(D%d)`, firstBout+wantRows), "%s: the tally stops at its own block", block)
}

// TestTeamBlockBoutRows pins the one bout-row count (state.Competition.
// TeamBoutRows) on the main sheets of BOTH exports (operator decision
// 2026-09-27, bc-kdsc): a kachinuki block has 2*teamSize-1 numbered bout rows,
// a team match block teamSize, an individual block none, on the Pool Matches
// sheet, the Elimination Matches sheet and its 3rd-place block alike, and each
// block's IV/PW tally formulas span exactly its own bout rows.
func TestTeamBlockBoutRows(t *testing.T) {
	cases := []struct {
		name      string
		teamSize  int
		matchType state.TeamMatchType
		wantRows  int
	}{
		{name: "kachinuki of 3", teamSize: 3, matchType: state.TeamMatchTypeKachinuki, wantRows: 5},
		{name: "team match of 3", teamSize: 3, matchType: state.TeamMatchTypeFixed, wantRows: 3},
		{name: "individual", teamSize: 0, wantRows: 0},
	}
	exports := []struct {
		name  string
		build func(*state.Store, *engine.Engine, string) ([]byte, error)
	}{
		{name: "blank template", build: func(_ *state.Store, eng *engine.Engine, id string) ([]byte, error) {
			return eng.ExportCompetitionXlsx(id)
		}},
		{name: "results", build: BuildResultsWorkbook},
	}
	for _, tc := range cases {
		for _, ex := range exports {
			t.Run(tc.name+"/"+ex.name, func(t *testing.T) {
				t.Parallel()
				dir, store, eng, compID := testSetup(t)
				defer os.RemoveAll(dir)
				startMixedComp(t, store, eng, compID, tc.teamSize, tc.matchType)

				data, err := ex.build(store, eng, compID)
				require.NoError(t, err)
				f, err := excelize.OpenReader(bytes.NewReader(data))
				require.NoError(t, err)
				defer f.Close()

				// Pool Matches: a team block is its White/vs/Red row, the
				// team names (and tally) row, then the bout rows; an
				// individual block lists its matches straight under the
				// header, with no bout numbers.
				poolRows, err := f.GetRows(helper.SheetPoolMatches)
				require.NoError(t, err)
				hdr := firstRowWith(poolRows, 0, helper.MatchHeaderLeftLabel())
				require.GreaterOrEqual(t, hdr, 0, "Pool Matches must carry a match block")
				assert.Equal(t, tc.wantRows, numberedRowsFrom(poolRows, hdr+2, 0), "pool block bout rows")
				if tc.wantRows > 0 {
					// Tally on the team names row (1-based hdr+2); bouts from 1-based hdr+3.
					assertTallyFormulaSpans(t, f, helper.SheetPoolMatches, hdr+2, hdr+3, tc.wantRows, "pool block")
				}

				// Elimination Matches: header H, White/Red H+1, entrants H+2,
				// bout rows from H+3, the "Victories / Points" tally at
				// H+5+rows (1-based). The 3rd-place block is laid out alike.
				elimRows, err := f.GetRows(helper.SheetEliminationMatches)
				require.NoError(t, err)
				for _, block := range []string{"Round 1 - Match 1", helper.ThirdPlaceLabel} {
					h := firstRowWith(elimRows, 0, block)
					require.GreaterOrEqual(t, h, 0, "Elimination Matches must carry %q", block)
					assert.Equal(t, tc.wantRows, numberedRowsFrom(elimRows, h+3, 0), "%s bout rows", block)
					if tc.wantRows > 0 {
						tallyRow := h + 1 + 5 + tc.wantRows
						assert.Equal(t, "Victories / Points", cellAt(elimRows, tallyRow-1, 0), "%s tally row", block)
						assertTallyFormulaSpans(t, f, helper.SheetEliminationMatches, tallyRow, h+4, tc.wantRows, block)
					}
				}
			})
		}
	}
}

// TestKachinukiResultFillsTheBoutsFoughtInOrder pins the results overlay on a
// kachinuki pool block of 2*3-1 = 5 rows: a result fills the bouts fought in
// order and leaves the rest empty, and an encounter that fielded reserves
// past the block shows its first five bouts and never writes into the rows
// below its own block (the Kachinuki Detail sheet lists the rest).
func TestKachinukiResultFillsTheBoutsFoughtInOrder(t *testing.T) {
	letters := [][]string{{"M"}, {"K"}, {"M", "K"}, {"K", "K"}, {"M", "M"}, {"T", "T"}, {"D", "D"}}
	cases := []struct {
		name   string
		fought int
	}{
		{name: "fewer bouts than the block", fought: 2},
		{name: "more bouts than the block", fought: 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)
			startMixedComp(t, store, eng, compID, 3, state.TeamMatchTypeKachinuki)

			matches, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			i := slices.IndexFunc(matches, func(m state.MatchResult) bool { return m.ID == "Pool A-0" })
			require.GreaterOrEqual(t, i, 0, "the draw must carry Pool A-0")
			m := &matches[i]
			m.Status = state.MatchStatusCompleted
			m.Winner, m.WinnerID = m.SideA, m.SideAID
			m.Decision = string(domain.DecisionKachinukiExhaustion)
			for b := 0; b < tc.fought; b++ {
				m.SubResults = append(m.SubResults, state.SubMatchResult{
					Position: b + 1, SideA: fmt.Sprintf("A%d", b+1), SideB: fmt.Sprintf("B%d", b+1),
					IpponsA: letters[b], Winner: fmt.Sprintf("A%d", b+1), Decision: "fought",
				})
			}
			require.NoError(t, store.SavePoolMatches(compID, matches))

			data, err := BuildResultsWorkbook(store, eng, compID)
			require.NoError(t, err)
			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err)
			defer f.Close()

			rows, err := f.GetRows(helper.SheetPoolMatches)
			require.NoError(t, err)
			hdr := firstRowWith(rows, 0, helper.MatchHeaderLeftLabel())
			require.GreaterOrEqual(t, hdr, 0)
			require.Equal(t, 5, numberedRowsFrom(rows, hdr+2, 0), "the kachinuki block has 2*3-1 bout rows")

			// SideA is Aka, the RIGHT victories column (F, index 5).
			for b := 0; b < 5; b++ {
				want := ""
				if b < tc.fought {
					want = strings.Join(letters[b], "")
				}
				assert.Equal(t, want, cellAt(rows, hdr+2+b, 5), "bout %d", b+1)
			}
			// The next block starts right after one spacing row: the first
			// encounter's own rows end at its fifth bout.
			assert.Equal(t, helper.MatchHeaderLeftLabel(), cellAt(rows, hdr+2+5+1, 0), "the next block's header sits right below")
			assert.Empty(t, cellAt(rows, hdr+2+5, 5), "the spacing row stays empty")
			for _, beyond := range []string{"TT", "DD"} {
				assert.False(t, sheetContainsCell(rows, beyond), "bout past the block (%s) must not be written anywhere", beyond)
			}
		})
	}
}

// TestKachinukiBracketOverlayRowsFollowTheBlock pins the results overlay's
// knockout row mapping on a kachinuki pure knockout (2*3-1 = 5 bout rows): a
// completed final's bouts land from H+3, its literal names repeat on the
// summary name row H+4+5 and its literal IV/PW on the tally row H+5+5; the
// 3rd-place block's bouts and tally follow the same layout.
func TestKachinukiBracketOverlayRowsFollowTheBlock(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	singleThird := false
	comp.Kind = "team"
	comp.Format = state.CompFormatKnockout
	comp.TeamMatchType = state.TeamMatchTypeKachinuki
	comp.TeamSize = 3
	comp.TwoThirdPlaces = &singleThird
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	bout := func(pos int, a, b string, ipponsA, ipponsB []string, winner string) state.SubMatchResult {
		return state.SubMatchResult{Position: pos, SideA: a, SideB: b, IpponsA: ipponsA, IpponsB: ipponsB, Winner: winner, Decision: "fought"}
	}
	done := state.MatchStatusCompleted
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Ryu", SideB: "Tora", Winner: "Ryu", Status: done, MatchNumber: 1},
				{ID: "m-r1-1", SideA: "Kame", SideB: "Taka", Winner: "Kame", Status: done, MatchNumber: 2},
			},
			{
				{ID: "m-r2-0", SideA: "Ryu", SideB: "Kame", Winner: "Ryu", Status: done, MatchNumber: 3,
					Decision: string(domain.DecisionKachinukiExhaustion),
					SubResults: []state.SubMatchResult{
						bout(1, "Ryu 1", "Kame 1", []string{"M", "K"}, nil, "Ryu 1"),
						bout(2, "Ryu 1", "Kame 2", nil, []string{"D"}, "Kame 2"),
					}},
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: state.BronzeMatchID, SideA: "Tora", SideB: "Taka", Winner: "Taka", Status: done,
			Decision: string(domain.DecisionKachinukiExhaustion),
			SubResults: []state.SubMatchResult{
				bout(1, "Tora 1", "Taka 1", nil, []string{"M"}, "Taka 1"),
				bout(2, "Tora 2", "Taka 1", nil, []string{"K"}, "Taka 1"),
				bout(3, "Tora 3", "Taka 1", nil, []string{"T"}, "Taka 1"),
			},
		},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	at := func(r, c int) string { return cellAt(rows, r, c) }

	// 0-based: header h, bouts from h+3; names repeat at h+4+5; tally h+5+5.
	final := firstRowWith(rows, 0, "Round 2 - Match 3")
	require.GreaterOrEqual(t, final, 0)
	assert.Equal(t, 5, numberedRowsFrom(rows, final+3, 0))
	assert.Equal(t, "MK", at(final+3, 5), "bout 1: Ryu (Aka) on the right")
	assert.Equal(t, "D", at(final+4, 1), "bout 2: Kame (Shiro) on the left")
	for r := final + 5; r < final+3+5; r++ {
		assert.Empty(t, at(r, 1)+at(r, 5), "unfought bout row %d stays empty", r+1)
	}
	assert.Equal(t, "Kame", at(final+4+5, 0), "Shiro's name repeats on the summary name row")
	assert.Equal(t, "Ryu", at(final+4+5, 6), "Aka's name repeats on the summary name row")
	assert.Equal(t, "Victories / Points", at(final+5+5, 0))
	assert.Equal(t, "1", at(final+5+5, 1), "Kame IV on the tally row")
	assert.Equal(t, "1", at(final+5+5, 5), "Ryu IV on the tally row")
	assert.Equal(t, "2", at(final+5+5, 4), "Ryu PW on the tally row")

	bronze := firstRowWith(rows, 0, helper.ThirdPlaceLabel)
	require.GreaterOrEqual(t, bronze, 0)
	assert.Equal(t, 5, numberedRowsFrom(rows, bronze+3, 0))
	for i, want := range []string{"M", "K", "T"} {
		assert.Equal(t, want, at(bronze+3+i, 1), "3rd place bout %d: Taka (Shiro) on the left", i+1)
	}
	assert.Equal(t, "Victories / Points", at(bronze+5+5, 0))
	assert.Equal(t, "3", at(bronze+5+5, 1), "Taka IV on the 3rd-place tally row")
}

// TestKachinukiPoolPagesBreakBetweenBlocks pins the Pool Matches page breaks
// once kachinuki blocks (2*5-1 = 9 bout rows here) make a pool of three taller
// than a page: the pool breaks between its match blocks, never inside one,
// and the first page is not left holding the shiaijo header alone.
func TestKachinukiPoolPagesBreakBetweenBlocks(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)
	startMixedComp(t, store, eng, compID, 5, state.TeamMatchTypeKachinuki)

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	breaks, err := bctest.RowBreaks(data, helper.SheetPoolMatches)
	require.NoError(t, err)
	require.NotEmpty(t, breaks, "a kachinuki pool of three is taller than a page")

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	assert.NotContains(t, breaks, 1, "a break after row 1 prints the shiaijo header alone")
	blocks := 0
	for r := range rows {
		if cellAt(rows, r, 0) != helper.MatchHeaderLeftLabel() {
			continue
		}
		blocks++
		// 1-based: the White/Red row, the names row, then nine bout rows.
		first, last := r+1, r+1+1+9
		require.Equal(t, "9", cellAt(rows, last-1, 0), "block at row %d ends on its ninth bout", first)
		for _, brk := range breaks {
			assert.Falsef(t, brk >= first && brk < last, "the page ending after row %d splits the block on rows %d-%d", brk, first, last)
		}
	}
	assert.Equal(t, 6, blocks, "both pools' three blocks were checked")
}
