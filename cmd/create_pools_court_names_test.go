package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// bc-qual review round: createPools derived its shiaijo LABELS from o.courts
// before o.courts was settled. Two things move that value after the labels were
// taken: the unset-courts default (`if o.courts < 1 { o.courts = 2 }`) and
// BuildPoolPhase's clamp down to what the pools can actually carry. Every
// consumer of the label list -- PrintPoolMatches, blankWorkbookCourtPlan and
// CreateNamesWithPoolToPrint -- was therefore handed a list that could disagree
// with the draw about how many shiaijo exist.
//
// The label list is not decorative: PoolsByCourt derives the BAND COUNT from
// len(courts) via EffectiveDrawCourts, so a short list bands the Pool Matches
// sheet onto fewer shiaijo than the bracket has regions -- the operator's
// running order and the printed tree then disagree about where a pool is
// fought. This test pins the two lists against each other through the real
// workbook rather than against a literal, so it keeps holding if the default or
// the clamp changes.

// shiaijoHeaders returns the distinct "Shiaijo X" band headers on a sheet, in
// first-seen order.
func shiaijoHeaders(t *testing.T, f *excelize.File, sheet string) []string {
	t.Helper()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	var out []string
	seen := map[string]bool{}
	for _, row := range rows {
		for _, cell := range row {
			v := strings.TrimSpace(cell)
			if strings.HasPrefix(v, "Shiaijo ") && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// A poolOptions built without a court count is a supported input to
// createPools, which defaults it to 2 (the CLI never reaches that branch: run's
// ValidateDrawCourtCount refuses 0 first, so this is the struct-built path the
// package's own tests and any future in-process caller take). Before the fix
// the labels were taken from the UNSET value, giving CourtLabels(0) -> ["A"]:
// one band for a two-shiaijo draw.
//
// Fault injection (verified): moving `courtNames := helper.CourtLabels(o.courts)`
// back above the default turns this red with a single "Shiaijo A" band.
func TestCreatePools_CourtLabelsFollowTheDefaultedCourtCount(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)

	o := &poolOptions{
		outputWriter: w,
		outputPath:   filepath.Join(dir, "out.xlsx"),
		numPlayers:   3,
		poolWinners:  2,
		determined:   true,
		// courts deliberately unset: createPools defaults it to 2.
	}
	require.NoError(t, o.createPools(rosterEntries(12)))
	require.NoError(t, w.Flush())
	require.Equal(t, 2, o.courts, "sanity: the default put the draw on two shiaijo")

	f, err := excelize.OpenReader(&buf)
	require.NoError(t, err)
	defer func() { assert.NoError(t, f.Close()) }()

	bands := shiaijoHeaders(t, f, helper.SheetPoolMatches)
	assert.Equal(t, []string{"Shiaijo A", "Shiaijo B"}, bands,
		"the Pool Matches sheet is banded onto fewer shiaijo than the draw has regions: the operator's running order and the printed tree disagree about where a pool is fought")
}

// rosterEntries builds the "Name,Dojo" lines createPools takes directly, the
// same shape poolRoster writes to a file for the run() path.
func rosterEntries(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("Player %02d,Dojo %02d", i, i)
	}
	return out
}

// The clamp moves the count the other way: `--courts 4` with only enough
// entrants for 2 pools draws on 2 shiaijo, and the labels must follow it down.
//
// CHARACTERIZATION, not a regression: this one passes with and without the fix,
// and is written that way deliberately rather than dressed up as a guard. An
// over-long label list is currently harmless because all three consumers
// re-derive the real count themselves (PoolsByCourt via EffectiveDrawCourts,
// RenderTreePages via courtsPrefix(plan.Courts, draw.NumCourts()),
// CreateNamesWithPoolToPrint via PoolsByCourt again), so there is no reachable
// output to differ. What it does buy is a tripwire: if any of those three ever
// stops re-deriving -- the natural reading of a court list being len() of it,
// which is exactly what TreePageTitle does with its own argument -- the wrong
// bands surface here instead of on a printed sheet at a tournament.
func TestCreatePools_CourtLabelsFollowTheClampedCourtCount(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "out.xlsx")

	o := &poolOptions{
		filePath:    poolRoster(t, dir, 7),
		outputPath:  output,
		numPlayers:  3,
		poolWinners: 2,
		courts:      4,
		determined:  true,
	}
	require.NoError(t, o.run(nil, nil))
	require.Equal(t, 2, o.courts,
		"sanity: 7 entrants at a minimum pool size of 3 form 2 pools, so the draw clamps to 2 shiaijo")

	f, err := excelize.OpenFile(output)
	require.NoError(t, err)
	defer func() { assert.NoError(t, f.Close()) }()

	bands := shiaijoHeaders(t, f, helper.SheetPoolMatches)
	assert.Equal(t, []string{"Shiaijo A", "Shiaijo B"}, bands,
		"the sheet names shiaijo the clamped draw does not run on")
	assert.NotContains(t, bands, "Shiaijo C")
	assert.NotContains(t, bands, "Shiaijo D")
}

// bc-qual review round: state.ValidateExtraQualifiers' doc comment says a
// caller that has not resolved a default must pass EffectivePoolWinners(), not
// the raw field, "or an unset PoolWinners would read as poolWinners=0 and
// incorrectly pass this check". createPools passed the raw flag.
//
// The consequence is not a missed rejection -- the run still fails -- but a
// WRONG rejection: 0 slips the >= 2 gate, the draw builder is handed
// defaultWinners=0 and refuses, and the operator is told their pool/shiaijo
// SHAPE is unsupported and to adjust --courts, when the actual problem is a
// pool-winners count the mode does not allow.
//
// Fault injection (verified): passing o.poolWinners instead of the resolved
// value turns this red with "outside what --extra-qualifiers larger-pools
// currently supports".
func TestCreatePools_ExtraQualifiersValidatesTheResolvedPoolWinners(t *testing.T) {
	dir := t.TempDir()

	o := &poolOptions{
		filePath:        poolRoster(t, dir, 12),
		outputPath:      filepath.Join(dir, "out.xlsx"),
		numPlayers:      3,
		poolWinners:     0, // unset: EffectivePoolWinners() resolves it to 2
		courts:          2,
		extraQualifiers: state.ExtraQualifiersLargerPools,
		determined:      true,
	}
	err := o.run(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires pool winners = 1",
		"the operator must be told which setting is wrong")
	assert.NotContains(t, err.Error(), "outside what --extra-qualifiers",
		"an unresolved pool-winners count slipped the rule and was reported as an unsupported draw shape instead")
}

// The rule's own case still has to reject: an explicit 2 is not "unset", and
// must be refused by the same message.
func TestCreatePools_ExtraQualifiersStillRejectsAnExplicitTwo(t *testing.T) {
	dir := t.TempDir()
	o := &poolOptions{
		filePath:        poolRoster(t, dir, 12),
		outputPath:      filepath.Join(dir, "out.xlsx"),
		numPlayers:      3,
		poolWinners:     2,
		courts:          2,
		extraQualifiers: state.ExtraQualifiersLargerPools,
		determined:      true,
	}
	err := o.run(nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires pool winners = 1")
}
