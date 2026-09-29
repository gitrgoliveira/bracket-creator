package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

// kachinukiForm is the POST /create body buildXlsxBody
// (web-mobile/js/admin_schedule_export.jsx) sends for a kachinuki competition
// of teams of three that decides a single 3rd place: tournamentType "pools"
// is pools of three sending two qualifiers each to the knockout, "knockout"
// the knockout alone.
func kachinukiForm(tournamentType string, teams ...string) url.Values {
	lines := make([]string, len(teams))
	for i, team := range teams {
		lines[i] = team + ", " + team + " Dojo"
	}
	return url.Values{
		"tournamentType":  {tournamentType},
		"playerList":      {strings.Join(lines, "\n")},
		"courts":          {"1"},
		"winnersPerPool":  {"2"},
		"playersPerPool":  {"3"},
		"poolSizeMode":    {"min"},
		"teamMatches":     {"3"},
		"teamMatchType":   {"kachinuki"},
		"roundRobin":      {"on"},
		"thirdPlaceMatch": {"on"},
		"determined":      {"on"},
	}
}

var sixTeams = []string{"Ryu", "Tora", "Kame", "Taka", "Kuma", "Hebi"}

// assertTallySpan checks the IV tally formula in column B of 1-based row
// tallyRow reads exactly the block's bout rows.
func assertTallySpan(t *testing.T, f *excelize.File, sheet string, tallyRow, firstBout, boutRows int, block string) {
	t.Helper()
	formula, err := f.GetCellFormula(sheet, fmt.Sprintf("B%d", tallyRow))
	require.NoError(t, err)
	assert.NoError(t, bctest.TallySpanError(formula, firstBout, boutRows), block)
}

// detailSections reads the Kachinuki Detail sheet's section titles, without
// their " (Kachinuki)" suffix, and the 0-based row each sits on.
func detailSections(rows [][]string) (titles []string, at []int) {
	for r := range rows {
		if title, ok := strings.CutSuffix(bctest.CellAt(rows, r, 0), " (Kachinuki)"); ok {
			titles = append(titles, title)
			at = append(at, r)
		}
	}
	return titles, at
}

// TestCreateHandler_KachinukiBlocksHoldEveryBout pins the blank template's
// team blocks: with teamMatchType=kachinuki every block on the Pool Matches
// sheet, the Elimination Matches sheet and its 3rd-place block has a row for
// each bout an encounter can take, 2*3-1 = 5 for teams of three, and its IV/PW
// tally reads exactly those rows. A team match (no teamMatchType) keeps one
// row per fighter.
func TestCreateHandler_KachinukiBlocksHoldEveryBout(t *testing.T) {
	for _, tc := range []struct {
		matchType string
		boutRows  int
	}{
		{matchType: "kachinuki", boutRows: 5},
		{matchType: "", boutRows: 3},
	} {
		t.Run(fmt.Sprintf("teamMatchType=%q", tc.matchType), func(t *testing.T) {
			form := kachinukiForm("pools", sixTeams...)
			if tc.matchType == "" {
				form.Del("teamMatchType")
			}
			f := postCreate(t, form)

			poolRows, err := f.GetRows(helper.SheetPoolMatches)
			require.NoError(t, err)
			blocks := 0
			for hdr := range poolRows {
				if bctest.CellAt(poolRows, hdr, 0) != helper.MatchHeaderLeftLabel() {
					continue
				}
				blocks++
				// The White/vs/Red row, the team names row carrying the
				// tally, then the bout rows.
				block := fmt.Sprintf("pool block at row %d", hdr+1)
				assert.Equal(t, tc.boutRows, bctest.NumberedRowsFrom(poolRows, hdr+2, 0), block)
				assertTallySpan(t, f, helper.SheetPoolMatches, hdr+2, hdr+3, tc.boutRows, block)
			}
			assert.Equal(t, 6, blocks, "two pools of three, three encounters each")

			elimRows, err := f.GetRows(helper.SheetEliminationMatches)
			require.NoError(t, err)
			var titles []string
			for h := range elimRows {
				title := bctest.CellAt(elimRows, h, 0)
				if !strings.HasPrefix(title, "Round ") && title != helper.ThirdPlaceLabel {
					continue
				}
				titles = append(titles, title)
				// The title, the White/Red row, the entrants, then the bout
				// rows; the tally sits below the summary name row.
				assert.Equal(t, tc.boutRows, bctest.NumberedRowsFrom(elimRows, h+3, 0), title)
				tallyRow := h + 1 + 5 + tc.boutRows
				assert.Equal(t, "Victories / Points", bctest.CellAt(elimRows, tallyRow-1, 0), "%s tally row", title)
				assertTallySpan(t, f, helper.SheetEliminationMatches, tallyRow, h+4, tc.boutRows, title)
			}
			assert.Equal(t, []string{"Round 1 - Match 1", "Round 1 - Match 2", "Round 2 - Match 3", helper.ThirdPlaceLabel}, titles)
		})
	}
}

// TestCreateHandler_KachinukiDetailCoversTheDraw pins the blank template's
// Kachinuki Detail sheet for a kachinuki competition of pools then knockout:
// an empty section of 2*3-1 = 5 numbered bout rows for every pool match, every
// knockout match and the 3rd-place match, each knockout section titled as its
// Elimination Matches block and naming each side as that block's entrant
// formula does. The sheet is left unprotected so its rows can be filled in.
func TestCreateHandler_KachinukiDetailCoversTheDraw(t *testing.T) {
	f := postCreate(t, kachinukiForm("pools", sixTeams...))

	rows, err := f.GetRows(helper.SheetKachinukiDetail)
	require.NoError(t, err)
	elimRows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	titles, at := detailSections(rows)
	assert.Equal(t, []string{
		"Pool Match 1", "Pool Match 2", "Pool Match 3", "Pool Match 4", "Pool Match 5", "Pool Match 6",
		"Round 1 - Match 1", "Round 1 - Match 2", "Round 2 - Match 3", helper.ThirdPlaceLabel,
	}, titles)
	for i, title := range titles {
		// The title, "<Shiro> vs <Aka>", the header row, then the bouts.
		assert.Equal(t, 5, bctest.NumberedRowsFrom(rows, at[i]+3, 0), "%s bout rows", title)
		shiro, aka, ok := strings.Cut(bctest.CellAt(rows, at[i]+1, 0), " vs ")
		require.True(t, ok, "%s subtitle", title)
		if strings.HasPrefix(title, "Pool Match") {
			assert.Contains(t, sixTeams, shiro, title)
			assert.Contains(t, sixTeams, aka, title)
			assert.NotEqual(t, shiro, aka, title)
			continue
		}
		// The block's entrant row: Shiro's formula in A, Aka's in G, each
		// opening with the label the section names that side by.
		h := bctest.FirstRowWith(elimRows, 0, title)
		require.GreaterOrEqual(t, h, 0, "Elimination Matches carries %q", title)
		left, err := f.GetCellFormula(helper.SheetEliminationMatches, fmt.Sprintf("A%d", h+3))
		require.NoError(t, err)
		right, err := f.GetCellFormula(helper.SheetEliminationMatches, fmt.Sprintf("G%d", h+3))
		require.NoError(t, err)
		assert.Contains(t, left, `"`+shiro+` "`, "%s: Shiro is its block's left entrant", title)
		assert.Contains(t, right, `"`+aka+` "`, "%s: Aka is its block's right entrant", title)
	}

	detail, err := f.GetSheetProtection(helper.SheetKachinukiDetail)
	require.NoError(t, err)
	assert.Equal(t, excelize.SheetProtectionOptions{}, detail, "the Kachinuki Detail sheet is left editable")
	pool, err := f.GetSheetProtection(helper.SheetPoolMatches)
	require.NoError(t, err)
	assert.True(t, pool.SelectLockedCells, "Pool Matches stays protected")
}

// TestCreateHandler_KachinukiKnockoutDetail pins the blank template of a
// kachinuki knockout: two teams are one match with no semifinal, so no 3rd
// place even when one is asked for; three teams are two matches, since a bye
// is no match, the final naming the first match's winner "M 1".
func TestCreateHandler_KachinukiKnockoutDetail(t *testing.T) {
	t.Run("two teams", func(t *testing.T) {
		f := postCreate(t, kachinukiForm("knockout", "Ryu", "Tora"))
		rows, err := f.GetRows(helper.SheetKachinukiDetail)
		require.NoError(t, err)
		titles, at := detailSections(rows)
		require.Equal(t, []string{"Round 1 - Match 1"}, titles)
		assert.Equal(t, "Tora vs Ryu", bctest.CellAt(rows, at[0]+1, 0), "the upper entrant is Aka, on the right")
		assert.Equal(t, 5, bctest.NumberedRowsFrom(rows, at[0]+3, 0))

		elimRows, err := f.GetRows(helper.SheetEliminationMatches)
		require.NoError(t, err)
		h := bctest.FirstRowWith(elimRows, 0, "Round 1 - Match 1")
		require.GreaterOrEqual(t, h, 0)
		assert.Equal(t, 5, bctest.NumberedRowsFrom(elimRows, h+3, 0))
		assert.Equal(t, -1, bctest.FindCellRow(elimRows, helper.ThirdPlaceLabel), "two teams have no semifinal")
	})
	t.Run("three teams, one bye", func(t *testing.T) {
		form := kachinukiForm("knockout", "Ryu", "Tora", "Kame")
		form.Del("thirdPlaceMatch")
		f := postCreate(t, form)
		rows, err := f.GetRows(helper.SheetKachinukiDetail)
		require.NoError(t, err)
		titles, at := detailSections(rows)
		require.Equal(t, []string{"Round 1 - Match 1", "Round 2 - Match 2"}, titles)
		assert.Equal(t, "Kame vs Tora", bctest.CellAt(rows, at[0]+1, 0))
		assert.Equal(t, "M 1 vs Ryu", bctest.CellAt(rows, at[1]+1, 0), "the bye's team meets the first match's winner")
		for i := range titles {
			assert.Equal(t, 5, bctest.NumberedRowsFrom(rows, at[i]+3, 0), titles[i])
		}
	})
}

// TestCreateHandler_TeamMatchTypeFollowsTheCompetitionRule pins the field's
// validation, the rule a competition's own setting follows: kachinuki needs
// teams of two or more, and an unknown format is refused. "fixed" is the
// absent field's team match, byte for byte.
func TestCreateHandler_TeamMatchTypeFollowsTheCompetitionRule(t *testing.T) {
	teams := []string{"Ryu", "Tora", "Kame", "Taka"}

	refusal := func(field, value string) string {
		form := kachinukiForm("knockout", teams...)
		form.Set(field, value)
		w := postCreateRaw(t, form)
		require.Equal(t, http.StatusBadRequest, w.Code, "%s=%s", field, value)
		var body struct{ Error string }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body.Error
	}
	assert.Equal(t, "kachinuki requires teamSize >= 2", refusal("teamMatches", "1"))
	assert.Equal(t, `unknown teamMatchType "relay" (expected "fixed" or "kachinuki")`, refusal("teamMatchType", "relay"))

	fixed := kachinukiForm("knockout", teams...)
	fixed.Set("teamMatchType", "fixed")
	absent := kachinukiForm("knockout", teams...)
	absent.Del("teamMatchType")
	fixedBody, absentBody := postCreateRaw(t, fixed), postCreateRaw(t, absent)
	require.Equal(t, http.StatusOK, fixedBody.Code, fixedBody.Body.String())
	require.Equal(t, http.StatusOK, absentBody.Code, absentBody.Body.String())
	assert.True(t, bytes.Equal(absentBody.Body.Bytes(), fixedBody.Body.Bytes()), "fixed draws the same workbook as no teamMatchType")
}

// TestCreateHandler_TeamSizeIsBounded pins the bound on a posted team size.
// It sizes the bout rows of every team block and the Kachinuki Detail
// sheet's empty sections, so an unbounded value from this public request
// would size those allocations (CodeQL go/uncontrolled-allocation-size).
// The bound is the schedule estimate's, engine.MaxTeamSize.
func TestCreateHandler_TeamSizeIsBounded(t *testing.T) {
	teams := []string{"Ryu", "Tora", "Kame", "Taka"}
	want := fmt.Sprintf("teamMatches must be between 0 and %d", engine.MaxTeamSize)
	for _, matchType := range []string{"kachinuki", ""} {
		for _, size := range []string{strconv.Itoa(engine.MaxTeamSize + 1), "-2"} {
			form := kachinukiForm("knockout", teams...)
			form.Set("teamMatchType", matchType)
			form.Set("teamMatches", size)
			w := postCreateRaw(t, form)
			require.Equal(t, http.StatusBadRequest, w.Code, "teamMatchType=%q teamMatches=%s", matchType, size)
			var body struct{ Error string }
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, want, body.Error)
		}
	}
	atBound := kachinukiForm("knockout", teams...)
	atBound.Set("teamMatches", strconv.Itoa(engine.MaxTeamSize))
	w := postCreateRaw(t, atBound)
	assert.Equal(t, http.StatusOK, w.Code, "the bound itself is accepted: %s", w.Body.String())
}

// TestCreateHandler_WithoutTeamMatchTypeMatchesTheExamples pins that a request
// without teamMatchType draws the same workbook as before the field existed:
// it reproduces the committed example workbooks byte for byte, each posted
// with the settings its make examples line passes to the create-pools or
// create-knockout command (the generator /create runs). A change to what a
// workbook prints fails here until make examples regenerates them.
func TestCreateHandler_WithoutTeamMatchTypeMatchesTheExamples(t *testing.T) {
	for _, tc := range []struct {
		example, roster string
		form            url.Values
	}{
		// create-pools -d -r -p 3 -w 2 -c 1 -t 3
		{"pools-example-small-3-teams.xlsx", "mock_data_small_3_teams.csv", url.Values{
			"tournamentType": {"pools"}, "determined": {"on"}, "roundRobin": {"on"},
			"playersPerPool": {"3"}, "winnersPerPool": {"2"}, "courts": {"1"}, "teamMatches": {"3"},
		}},
		{"pools-example-teams-of-3-round-robin.xlsx", "mock_data_teams_of_3_pool_3.csv", url.Values{
			"tournamentType": {"pools"}, "determined": {"on"}, "roundRobin": {"on"},
			"playersPerPool": {"3"}, "winnersPerPool": {"2"}, "courts": {"1"}, "teamMatches": {"3"},
		}},
		// create-knockout -d -t 5 (two shiaijo by default)
		{"knockout-example-small.xlsx", "mock_data_small.csv", url.Values{
			"tournamentType": {"knockout"}, "determined": {"on"}, "courts": {"2"}, "teamMatches": {"5"},
		}},
		// create-pools -d -p 8 -w 2 -c 1, an individual competition
		{"pools-example-single-pool-8.xlsx", "mock_data_single_pool_8.csv", url.Values{
			"tournamentType": {"pools"}, "determined": {"on"},
			"playersPerPool": {"8"}, "winnersPerPool": {"2"}, "courts": {"1"},
		}},
	} {
		t.Run(tc.example, func(t *testing.T) {
			roster, err := os.ReadFile(filepath.Join("..", "test-data", tc.roster))
			require.NoError(t, err)
			want, err := os.ReadFile(filepath.Join("..", tc.example))
			require.NoError(t, err)

			tc.form.Set("playerList", string(roster))
			w := postCreateRaw(t, tc.form)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.True(t, bytes.Equal(want, w.Body.Bytes()), "POST /create no longer reproduces %s", tc.example)
		})
	}
}
