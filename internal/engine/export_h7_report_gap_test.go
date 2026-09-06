package engine

import (
	"bytes"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// captureExportLog swaps the default logger's sink for the duration of fn
// (same approach as TestApplyMatchWrite_LogsTheUnstampedOverwrite).
func captureExportLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	fn()
	return buf.String()
}

// TestFirstUnnumberedPooledCompetitor pins the pure helper
// ExportCompetitionXlsx's bc-pnum review H7 report-gap log line is built
// from: the first competitor, in pool then in-pool order, whose Number is
// blank.
func TestFirstUnnumberedPooledCompetitor(t *testing.T) {
	t.Run("no pools: not found", func(t *testing.T) {
		_, _, ok := firstUnnumberedPooledCompetitor(nil)
		assert.False(t, ok)
	})

	t.Run("every competitor numbered: not found", func(t *testing.T) {
		pools := []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice", Dojo: "Dojo A", Number: "K1"},
			{Name: "Bob", Dojo: "Dojo B", Number: "K2"},
		}}}
		_, _, ok := firstUnnumberedPooledCompetitor(pools)
		assert.False(t, ok)
	})

	t.Run("an unnumbered competitor in a later pool is found and named", func(t *testing.T) {
		pools := []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice", Dojo: "Dojo A", Number: "K1"}}},
			{PoolName: "Pool B", Players: []helper.Player{
				{Name: "Carol", Dojo: "Dojo C", Number: ""},
				{Name: "Dave", Dojo: "Dojo D", Number: "K4"},
			}},
		}
		name, dojo, ok := firstUnnumberedPooledCompetitor(pools)
		require.True(t, ok)
		assert.Equal(t, "Carol", name)
		assert.Equal(t, "Dojo C", dojo)
	})
}

// TestExportCompetitionXlsx_LogsUnnumberedPooledCompetitor pins bc-pnum
// review H7's report-gap: AddPoolDataToSheet/AddPlayerDataToSheet degrade a
// missing Number SILENTLY (D1's own report-over-fabricate rule: no
// fabricated number is ever printed), so nothing said WHY a competitor's
// tag/Player-Number/Names-to-Print cell came out blank. A numbered
// competition (NumberPrefix != "") whose pools.csv carries an unnumbered
// row (hand-edited/legacy data -- the ordinary numbering path never leaves
// one) must log it, naming the competitor and the competition, without
// failing the export.
func TestExportCompetitionXlsx_LogsUnnumberedPooledCompetitor(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "unnumbered-pool-gap"
	createTestCompetition(t, store, compID, "mixed", 4, func(c *state.Competition) {
		c.NumberPrefix = "K"
	})
	names := make([]string, 8)
	for i := range names {
		names[i] = "Player" + string(rune('A'+i))
	}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))

	// Simulate hand-edited/legacy data: blank one competitor's Number on
	// disk after the normal numbering path already ran, so the ordinary
	// draw pipeline is not what leaves this gap -- a corrupted/edited
	// pools.csv is.
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.NotEmpty(t, pools[0].Players, "premise: pool A has at least one competitor")
	blanked := pools[0].Players[0]
	pools[0].Players[0].Number = ""
	require.NoError(t, store.SavePools(compID, pools))

	var exportErr error
	out := captureExportLog(t, func() {
		_, exportErr = eng.ExportCompetitionXlsx(compID)
	})
	require.NoError(t, exportErr, "a report-gap log must never fail the export")
	assert.Contains(t, out, compID, "the log must name the competition")
	assert.Contains(t, out, blanked.Name, "the log must name the first unnumbered competitor")
}

// TestExportCompetitionXlsx_EmptyNumberPrefixNeverLogs pins the exclusion:
// comp.NumberPrefix == "" means this competition is not numbered at all
// (Swiss is the real-world case, per NumberPools/AssignPlayerNumbers's own
// doc comments, but the guard is on the field, not the format), so a blank
// Number is normal here, not a gap to report. Same fixture as
// TestExportCompetitionXlsx_LogsUnnumberedPooledCompetitor (a hand-edited
// pools.csv row with a blank Number) but with NumberPrefix left empty: the
// export must produce no report-gap log line at all.
func TestExportCompetitionXlsx_EmptyNumberPrefixNeverLogs(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "empty-prefix-no-gap-log"
	createTestCompetition(t, store, compID, "mixed", 4, func(c *state.Competition) {
		c.NumberPrefix = ""
	})
	names := make([]string, 8)
	for i := range names {
		names[i] = "Player" + string(rune('A'+i))
	}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.NotEmpty(t, pools[0].Players)
	pools[0].Players[0].Number = ""
	require.NoError(t, store.SavePools(compID, pools))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Empty(t, comp.NumberPrefix, "premise: this competition is not numbered")

	var exportErr error
	out := captureExportLog(t, func() {
		_, exportErr = eng.ExportCompetitionXlsx(compID)
	})
	require.NoError(t, exportErr)
	assert.NotContains(t, out, "has no Number under prefix",
		"an unnumbered competition's blank Numbers are normal, not a gap to report")
}
