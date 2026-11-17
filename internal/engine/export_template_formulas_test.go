package engine

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestExportCompetitionXlsx_PoolCountingFormulasAreLive is the stored-draw
// export's guard for mp-x0u9, now that the app's "Download blank template"
// prints the stored draw once there is one. The template used to be drawn
// afresh by POST /create because this export printed every pool W cell as
// "=0": the stored pools' matches pointed at another Players slice than the
// one helper.PrintPoolMatches links a player's counting formulas by.
// AttachPoolMatches now points them into the pool's own slice. Pinned for
// pools and a league, individual and team (fixed-order and kachinuki), from
// the process that drew and from a restarted one, which reads everything
// back from disk.
func TestExportCompetitionXlsx_PoolCountingFormulasAreLive(t *testing.T) {
	cases := []struct {
		name    string
		format  string
		size    int
		players int
		opts    func(*state.Competition)
	}{
		{"pools individual", state.CompFormatMixed, 3, 6, nil},
		{"league individual", state.CompFormatLeague, 5, 5, nil},
		{"pools team", state.CompFormatMixed, 3, 6, teamOf(3, "")},
		{"league team", state.CompFormatLeague, 4, 4, teamOf(3, "")},
		{"pools kachinuki", state.CompFormatMixed, 3, 6, teamOf(3, state.TeamMatchTypeKachinuki)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, dir := setupTestEngine(t)
			const compID = "live-formulas"
			createTestCompetition(t, store, compID, tc.format, tc.size, tc.opts)
			names := []string{"Ann", "Bea", "Cid", "Dan", "Eve", "Fay"}[:tc.players]
			saveTestParticipants(t, store, compID, names)
			require.NoError(t, eng.GenerateDraw(compID))

			drawn, err := eng.ExportCompetitionXlsx(compID)
			require.NoError(t, err)
			restarted, err := state.NewStore(dir)
			require.NoError(t, err)
			reloaded, err := New(restarted).ExportCompetitionXlsx(compID)
			require.NoError(t, err)

			for label, data := range map[string][]byte{"drawn": drawn, "restarted": reloaded} {
				f, err := excelize.OpenReader(bytes.NewReader(data))
				require.NoError(t, err)
				assertLiveWCells(t, f, label, tc.players)
				require.NoError(t, f.Close())
			}
		})
	}
}

// TestExportCompetitionXlsx_StaysBlankOfScores pins that the stored-draw
// export prints the draw and never a result: a recorded pool result leaves
// the Pool Matches sheet as it was, so it stays a blank template for scoring
// by hand while the competition runs.
func TestExportCompetitionXlsx_StaysBlankOfScores(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "blank-of-scores"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, nil)
	saveTestParticipants(t, store, compID, []string{"Ann", "Bea", "Cid", "Dan", "Eve", "Fay"})
	require.NoError(t, eng.StartCompetition(compID))

	before := poolMatchesSheet(t, eng, compID)

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	m := matches[0]
	require.NoError(t, eng.RecordMatchResult(compID, m.ID, &state.MatchResult{
		SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID,
		IpponsA: []string{"M", "K"}, Winner: m.SideA, WinnerID: m.SideAID,
		Status: state.MatchStatusCompleted,
	}))
	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Equal(t, state.MatchStatusCompleted, stored[0].Status, "the fixture must record the result")

	assert.Equal(t, before, poolMatchesSheet(t, eng, compID),
		"a recorded result must not reach the stored-draw export")
}

func teamOf(size int, matchType state.TeamMatchType) func(*state.Competition) {
	return func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = size
		c.TeamMatchType = matchType
	}
}

// assertLiveWCells checks every pool results table on the Pool Matches sheet:
// each competitor's W cell (column B) holds a formula that counts the
// competitor's matches, never the collapsed "=0".
func assertLiveWCells(t *testing.T, f *excelize.File, label string, players int) {
	t.Helper()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)
	checked := 0
	for i, row := range rows {
		// "Results" heads an individual pool's table, "Team Results" a team's.
		if len(row) == 0 || (row[0] != "Results" && row[0] != "Team Results") {
			continue
		}
		// The "Results" row carries the column headings; one row per competitor
		// follows, until the first row whose W cell holds no formula. A name cell
		// is itself a formula with no cached value, so the rows read as blank.
		for r := i + 2; ; r++ {
			cell, err := excelize.CoordinatesToCellName(2, r)
			require.NoError(t, err)
			formula, err := f.GetCellFormula(helper.SheetPoolMatches, cell)
			require.NoError(t, err)
			if formula == "" {
				break
			}
			assert.NotEqual(t, "0", strings.TrimPrefix(formula, "="), "%s: W cell %s collapsed to =0 (mp-x0u9)", label, cell)
			assert.Contains(t, formula, "COUNTA", "%s: W cell %s is not a counting formula", label, cell)
			checked++
		}
	}
	assert.Equal(t, players, checked, "%s: every competitor's W cell must be checked", label)
}

func poolMatchesSheet(t *testing.T, eng *Engine, compID string) [][]string {
	t.Helper()
	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)
	return rows
}
