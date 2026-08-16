package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

// The bronze prints in ITS shiaijo's band like any other bout, which means
// PrintBronzeBlockWithPrintArea has to turn a court NAME into a band INDEX. All
// three answers it can reach are pinned here, because the arithmetic underneath
// (1 + band*CourtsColumnsPerCourt) turns a wrong index into a wrong column and a
// negative one into a column excelize cannot name at all.
func TestBronzeBlockBandSelection(t *testing.T) {
	t.Parallel()

	// The block's own left-hand column, read back off the sheet: the "Round N"
	// header cell PrintThirdPlaceBlock merges is the widest thing it writes, so
	// the merge's start column is the band it landed in.
	bandStartColumn := func(t *testing.T, bands []string, bronzeCourt string) int {
		t.Helper()
		f := excelize.NewFile()
		defer func() { require.NoError(t, f.Close()) }()
		_, err := f.NewSheet(SheetEliminationMatches)
		require.NoError(t, err)

		PrintBronzeBlockWithPrintArea(f, 2, 0, false, false, bands, bronzeCourt, nil, nil)

		merged, err := f.GetMergeCells(SheetEliminationMatches)
		require.NoError(t, err)
		require.NotEmpty(t, merged, "the bronze block writes at least one merged header")
		col, _, err := excelize.CellNameToCoordinates(merged[0].GetStartAxis())
		require.NoError(t, err)
		return col
	}

	colOfBand := func(i int) int { return 1 + i*CourtsColumnsPerCourt }

	t.Run("a recorded court takes its own band", func(t *testing.T) {
		assert.Equal(t, colOfBand(2), bandStartColumn(t, []string{"A", "B", "C"}, "C"),
			"a bronze moved to the third shiaijo must print under that shiaijo's header, not the leftmost")
	})

	t.Run("no court recorded takes the first band", func(t *testing.T) {
		// The CLI's case: a blank workbook with no stored bracket behind it,
		// so there is no live assignment to follow.
		assert.Equal(t, colOfBand(0), bandStartColumn(t, []string{"A", "B", "C"}, ""))
	})

	t.Run("a court absent from the bands takes the last one", func(t *testing.T) {
		// Kept distinct from the no-court case on purpose: falling back to the
		// leftmost band is what once printed the bronze under another shiaijo's
		// header, and it looks identical to the legitimate CLI answer.
		assert.Equal(t, colOfBand(1), bandStartColumn(t, []string{"A", "B"}, "D"))
	})

	t.Run("no bands at all still writes a real column", func(t *testing.T) {
		// Unreachable from the two production callers (usedCourtBands never
		// returns an empty set), but this is exported: without the length guard
		// "the last band" is index -1, which asks excelize for column -7.
		assert.Equal(t, colOfBand(0), bandStartColumn(t, nil, "D"))
	})
}

// A shiaijo gets a band because a bout PRINTS under it.
//
// usedCourtBands folds CourtPlan.Bronze into the band set unconditionally, and
// has to: a bronze moved to a shiaijo no other bout uses would otherwise be
// looked up in a set it was never allowed to join. But the bronze GATE belongs
// to the caller, and the plan carries the court whether or not that gate passed,
// so a stored ThirdPlaceMatch on a competition rendering no bronze block used to
// buy a header, a page break and a print-area column with nothing underneath.
func TestEliminationBandsIgnoreTheBronzeCourtWhenNoBronzePrints(t *testing.T) {
	t.Parallel()

	bandCourts := func(t *testing.T, includeBronze bool) []string {
		t.Helper()
		f := excelize.NewFile()
		defer func() { require.NoError(t, f.Close()) }()
		_, err := f.NewSheet(SheetEliminationMatches)
		require.NoError(t, err)

		pools, err := CreatePools(drawGoldenRoster(2), drawGoldenPoolSize, true)
		require.NoError(t, err)
		draw := BuildKnockoutDraw(pools, 2, 2)
		require.NotNil(t, draw)
		rounds := BuildEliminationMatchRounds(draw.Root)
		AssignMatchNumbers(rounds)

		// Every bout stays on its drawn shiaijo; only the 3rd-place bout names
		// a third one, which is the shiaijo that must not appear unless the
		// bronze block itself does.
		plan := CourtPlan{Draw: draw, Courts: []string{"A", "B", "C"}, Bronze: "C"}
		PrintEliminationWithBronze(f, nil, rounds, 0, plan, false, false, includeBronze)

		rows, err := f.GetRows(SheetEliminationMatches)
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		// bctest.ReadCourtBands is the one reader that knows how a band header
		// is spelled, so a rename of ShiaijoLabel's prefix fails here loudly
		// instead of quietly matching nothing and passing vacuously.
		var courts []string
		for _, b := range bctest.ReadCourtBands(rows, CourtsColumnsPerCourt) {
			courts = append(courts, b.Court)
		}
		return courts
	}

	assert.NotContains(t, bandCourts(t, false), "C",
		"no bronze is printed, so shiaijo C has nothing on this sheet and must not get a header, a page break and a print column")
	assert.Contains(t, bandCourts(t, true), "C",
		"when the bronze IS printed its shiaijo must still get a band, or the block lands under another court's header")
}

// PrintTeamEliminationMatches and PrintEliminationWithBronze are exported and
// take the rounds from their caller, so what they do with a nil entry is part of
// their contract whether or not this repo can produce one.
//
// It cannot: BuildEliminationMatchRounds is the only producer and TraverseRounds
// appends non-nil nodes only. That is exactly why this needs a test rather than
// a walk through the call graph -- nothing else in the suite can reach the
// branch, so without this it is a guard no run ever executes. Its two siblings
// over the same slices (AssignMatchNumbers, FillInMatches) have always skipped
// nils; these two dereferenced whatever they were handed.
func TestEliminationRoundsToleratesANilEntry(t *testing.T) {
	t.Parallel()

	render := func(t *testing.T, injectNil bool) []bctest.CourtBand {
		t.Helper()
		f := excelize.NewFile()
		defer func() { require.NoError(t, f.Close()) }()
		_, err := f.NewSheet(SheetEliminationMatches)
		require.NoError(t, err)

		pools, err := CreatePools(drawGoldenRoster(2), drawGoldenPoolSize, true)
		require.NoError(t, err)
		draw := BuildKnockoutDraw(pools, 2, 2)
		require.NotNil(t, draw)
		rounds := BuildEliminationMatchRounds(draw.Root)
		AssignMatchNumbers(rounds)
		require.NotEmpty(t, rounds)

		if injectNil {
			// A nil beside the real bouts, and a round that is nothing but
			// nils: both are shapes a caller could hand over.
			rounds[0] = append([]*Node{nil}, rounds[0]...)
			rounds = append(rounds, []*Node{nil, nil})
		}

		plan := CourtPlan{Draw: draw, Courts: []string{"A", "B"}}
		PrintEliminationWithBronze(f, nil, rounds, 0, plan, false, false, false)

		rows, err := f.GetRows(SheetEliminationMatches)
		require.NoError(t, err)
		require.NotEmpty(t, rows)
		// bctest.ReadCourtBands is the one reader that knows how a band header
		// is spelled, so a rename of ShiaijoLabel's prefix fails here loudly
		// instead of quietly matching nothing and passing vacuously.
		return bctest.ReadCourtBands(rows, CourtsColumnsPerCourt)
	}

	clean := render(t, false)
	require.NotEmpty(t, clean, "the fixture must produce bands for the comparison to mean anything")
	// Skipped, not counted: a nil must not add a band, drop one, or reorder
	// them. Anything else and the sheet a nil produces is quietly different
	// from the sheet the same bouts produce on their own.
	assert.Equal(t, clean, render(t, true),
		"a nil entry must be skipped exactly as AssignMatchNumbers and FillInMatches skip it")
}
