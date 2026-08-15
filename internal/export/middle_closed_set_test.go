package export

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// OPERATOR RULING: there must NEVER be a centre `Ht`, and there is only one
// right way for the centre to be. The middle is a CLOSED SET — "X" (tie),
// "(E)" (overtime), "(DH)" (representative bout), or empty (the sheet's own
// "vs") — and nothing else may ever appear there. `Ht`, `Kiken` and `Fus.`
// are RESULT marks: they name one competitor, so SideMarks puts them in that
// competitor's own score cell and they can never reach the shared middle.
//
// This is the Go half of the guard; web-mobile/js/__tests__/middle_closed_set.test.jsx
// is the JS half. The two are a mirrored pair (middleMark/sideMarks in
// bracket.jsx ↔ MiddleMark/SideMarks here), so the ruling has to be enforced
// in BOTH languages or the export could drift away from the screen.
func TestMiddleMarkIsAClosedSet(t *testing.T) {
	// Every canonical wire value (CLAUDE.md § Match Decision Types) plus the
	// junk a hand-edited file or an older client can still deliver.
	decisions := []string{
		"", "fought", "hikiwake", "kiken", "kiken-voluntary", "kiken-injury",
		"fusenpai", "fusensho", "daihyosen", "kachinuki-exhaustion",
		"HIKIWAKE", "nonsense",
	}
	enchos := []*state.EnchoMetadata{
		nil, {}, {PeriodCount: 0}, {PeriodCount: 1}, {PeriodCount: 3}, {PeriodCount: -1},
	}
	allowed := map[string]bool{"": true, "X": true, "(E)": true, "(DH)": true}

	seen := map[string]bool{}
	for _, d := range decisions {
		for _, e := range enchos {
			mid := MiddleMark(d, e)
			seen[mid] = true
			assert.Truef(t, allowed[mid],
				"decision %q + encho %v produced middle %q, which is outside the closed set", d, e, mid)
			for _, mark := range []string{"Ht", "Kiken", "Fus."} {
				assert.NotContainsf(t, mid, mark,
					"decision %q put the side result %q in the middle", d, mark)
			}
		}
	}
	// Not vacuous: the sweep must actually reach each special mark.
	require.True(t, seen["X"], "the sweep never produced a draw X")
	require.True(t, seen["(E)"], "the sweep never produced an encho (E)")
	require.True(t, seen["(DH)"], "the sweep never produced a daihyosen (DH)")
}

// The other half of the same ruling: Ht is only ever a SIDE mark, and it names
// the winner. A hantei on any decision the validator allows beside it must put
// Ht in a competitor's cell, never return it as something the middle could use.
func TestHanteiMarkNeverLeavesTheWinnerSide(t *testing.T) {
	for _, d := range []string{"", "fought", "daihyosen"} {
		winnerMark, loserMark := SideMarks(d, true)
		assert.Equal(t, "Ht", winnerMark, "decision %q: the hantei mark names the winner", d)
		assert.NotContains(t, loserMark, "Ht", "decision %q: the loser never wears Ht", d)
		// And the middle for that same bout stays inside the set.
		assert.NotContains(t, MiddleMark(d, &state.EnchoMetadata{PeriodCount: 1}), "Ht")
	}
	// Without a verdict there is no Ht anywhere.
	for _, d := range []string{"", "fought", "hikiwake", "daihyosen", string(domain.DecisionFusensho)} {
		w, l := SideMarks(d, false)
		assert.NotContains(t, w, "Ht")
		assert.NotContains(t, l, "Ht")
	}
}

// IpponsScore renders a side's CELL, so a result mark may be appended to it —
// that is the mark riding with its competitor, which is the rule working. What
// it must never do is emit a middle value, which would put a second separator
// inside a cell.
func TestIpponsScoreNeverEmitsAMiddleValue(t *testing.T) {
	for _, ippons := range [][]string{
		nil, {}, {"M"}, {"M", "K"}, {"○", "○"}, {domain.IpponPlaceholder}, {"", "M"},
	} {
		got := IpponsScore(ippons)
		for _, mid := range []string{"X", "(E)", "(DH)", "vs"} {
			assert.NotContainsf(t, got, mid,
				"ippons %v rendered %q, which contains the middle value %q", ippons, got, mid)
		}
		assert.False(t, strings.Contains(got, domain.IpponPlaceholder),
			"the unfilled-slot placeholder is not a score")
	}
}
