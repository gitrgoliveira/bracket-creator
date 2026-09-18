package export

import (
	"testing"

	"github.com/stretchr/testify/assert"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bc-dnst. The sheet and the standings read the same match-level rule and read
// it DIFFERENTLY, on purpose, and the two look unifiable enough that a review
// round proposed hoisting them into one helper. This pins the divergence so
// that hoist is a red test rather than a silent behaviour change.
//
// state.SubBoutWinnerSide falls back to the encounter's names whenever the
// row's own tiers cannot attribute it. writeTeamSubMatchScores substitutes
// them only when the row names NO fighter at all, so a row that does name its
// fighters keeps deciding for itself.
//
// The row below is where that parts company: it names two fighters, and its
// Winner holds the TEAM name instead (a rename, a hand edit, or a quick score
// written over a row already fought under names). The standings count it,
// because the encounter's name is still a fact about who won. The sheet
// declines to mark it, because the alternative is printing a default-win maru
// and a no-show mark beside a fighter the winner does not name, and a mark
// beside the wrong competitor is worse than no mark. That asymmetry is the
// accepted reading, not an oversight to reconcile.
func TestTeamNameFallback_SheetAndStandingsDivergeOnANamedRow(t *testing.T) {
	t.Parallel()

	drifted := state.SubMatchResult{
		Position: 1, SideA: "Kenji", SideB: "Taro",
		Winner: "Tora A", Decision: "fusensho",
	}

	assert.Equal(t, domain.MatchSideA, state.SubBoutWinnerSide(drifted, "Tora A", "Kenshi B"),
		"standings: the match-level arm attributes a winner holding the encounter's own name")

	f := excelize.NewFile()
	defer f.Close()
	sheet := helper.SheetPoolMatches
	f.NewSheet(sheet)
	writeTeamSubMatchScores(f, sheet, 1, 5, []state.SubMatchResult{drifted}, 3, false, "Tora A", "Kenshi B")

	left, err := f.GetCellValue(sheet, "B5")
	assert.NoError(t, err)
	right, err := f.GetCellValue(sheet, "F5")
	assert.NoError(t, err)
	for _, cell := range []string{left, right} {
		assert.NotContains(t, cell, "Fus.",
			"sheet: no no-show mark beside a fighter the winner does not name")
		assert.NotContains(t, cell, "○",
			"sheet: and no default-win maru either, for the same reason")
	}

	// The control: blank the two fighter names and the SAME row marks, which
	// is what the substitution exists for. Without this the assertions above
	// would also pass if the export had simply stopped marking anything.
	silent := drifted
	silent.SideA, silent.SideB = "", ""
	g := excelize.NewFile()
	defer g.Close()
	g.NewSheet(sheet)
	writeTeamSubMatchScores(g, sheet, 1, 5, []state.SubMatchResult{silent}, 3, false, "Tora A", "Kenshi B")
	marked, err := g.GetCellValue(sheet, "B5")
	assert.NoError(t, err)
	assert.Contains(t, marked, "Fus.", "control: a row naming no fighter DOES take the encounter's names")
	assert.Contains(t, marked, "○", "control: and prints its default-win maru")
}
