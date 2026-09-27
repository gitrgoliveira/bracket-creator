package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// TestMiddleMark pins the closed-set centre-cell rule at its domain-layer
// source: export.MiddleMark (state.EnchoMetadata-typed) and the Kachinuki
// Detail sheet both delegate here, so this table is the one place the rule
// itself is asserted; the delegates only need to prove they call through
// (see internal/export/middle_closed_set_test.go).
func TestMiddleMark(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		decision string
		enchoOn  bool
		want     string
	}{
		{name: "fought, no encho", decision: "fought", enchoOn: false, want: ""},
		{name: "empty decision, no encho", decision: "", enchoOn: false, want: ""},
		{name: "encho win", decision: "fought", enchoOn: true, want: "(E)"},
		{name: "tie", decision: "hikiwake", enchoOn: false, want: "X"},
		{name: "tie beats stale encho data", decision: "hikiwake", enchoOn: true, want: "X"},
		{name: "daihyosen", decision: "daihyosen", enchoOn: false, want: "(DH)"},
		{name: "daihyosen beats stale encho data (DH bouts have no encho)", decision: "daihyosen", enchoOn: true, want: "(DH)"},
		{name: "kiken leaves the middle alone", decision: "kiken-voluntary", enchoOn: false, want: ""},
		{name: "kiken during overtime keeps the (E) middle", decision: "kiken-voluntary", enchoOn: true, want: "(E)"},
		{name: "fusenpai leaves the middle alone", decision: "fusenpai", enchoOn: false, want: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, domain.MiddleMark(tc.decision, tc.enchoOn))
		})
	}
}

// TestEnchoLabel pins the bare "(E)"/"" rule directly (mp-m4bn: never a
// count); export.enchoLabel delegates here after reducing its
// *state.EnchoMetadata argument to encho.On().
func TestEnchoLabel(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", domain.EnchoLabel(false))
	assert.Equal(t, "(E)", domain.EnchoLabel(true))
}

// TestSideMarks pins the per-side result-mark rule at its domain-layer
// source; export.SideMarks is a one-line delegate.
func TestSideMarks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		decision   string
		hantei     bool
		wantWinner string
		wantLoser  string
	}{
		{name: "fought", decision: "fought", hantei: false, wantWinner: "", wantLoser: ""},
		{name: "hantei", decision: "fought", hantei: true, wantWinner: "Ht", wantLoser: ""},
		{name: "kiken-voluntary", decision: "kiken-voluntary", hantei: false, wantWinner: "", wantLoser: "Kiken"},
		{name: "kiken-injury", decision: "kiken-injury", hantei: false, wantWinner: "", wantLoser: "Kiken"},
		{name: "kiken (legacy)", decision: "kiken", hantei: false, wantWinner: "", wantLoser: "Kiken"},
		{name: "fusenpai marks the no-show loser", decision: "fusenpai", hantei: false, wantWinner: "", wantLoser: "Fus."},
		{name: "fusensho marks the defaulted winner", decision: "fusensho", hantei: false, wantWinner: "Fus.", wantLoser: ""},
		{name: "daihyosen is a middle mark, not a side mark", decision: "daihyosen", hantei: false, wantWinner: "", wantLoser: ""},
		{name: "hikiwake has no side marks", decision: "hikiwake", hantei: false, wantWinner: "", wantLoser: ""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w, l := domain.SideMarks(tc.decision, tc.hantei)
			assert.Equal(t, tc.wantWinner, w, "winner mark")
			assert.Equal(t, tc.wantLoser, l, "loser mark")
		})
	}
}

// TestSideMarksAB pins the SIDE-ordered (markA, markB) rule that both
// export.SideMarksLR (after White-left) and the Kachinuki Detail bout writer
// (after White-left) build on. The expectations are the SAME attribution
// facts export's TestSideMarksLR pins, restated in A/B order instead of
// White-left/Aka-right sheet order: WhiteLeft(aka, shiro) = (shiro, aka), so
// SideMarksLR's wantRight is this table's wantMarkA (SideA is Aka) and its
// wantLeft is this table's wantMarkB (SideB is Shiro).
func TestSideMarksAB(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                       string
		decision                   string
		hantei                     bool
		winnerID, sideAID, sideBID string
		winner                     string
		wantMarkA, wantMarkB       string
	}{
		{name: "hantei, A wins", decision: "fought", hantei: true, winner: "A", wantMarkA: "Ht", wantMarkB: ""},
		{name: "hantei, B wins", decision: "fought", hantei: true, winner: "B", wantMarkA: "", wantMarkB: "Ht"},
		{name: "kiken, A wins marks B", decision: "kiken-voluntary", winner: "A", wantMarkA: "", wantMarkB: "Kiken"},
		{name: "no winner recorded: marks have no home", decision: "kiken-voluntary", winner: "", wantMarkA: "", wantMarkB: ""},
		{name: "drifted winner name: no marks rather than a guess", decision: "kiken-voluntary", winner: "C", wantMarkA: "", wantMarkB: ""},
		// Ids present: ids win over names, even on a same-name pair (legal:
		// two participants from different dojos, or two opposing fighters,
		// may share a name).
		{
			name:     "same-name pair: ids attribute the mark to B, not A",
			decision: "fought", hantei: true,
			winnerID: "id-b", sideAID: "id-a", sideBID: "id-b",
			winner: "A", wantMarkA: "", wantMarkB: "Ht",
		},
		{
			name:     "same-name pair: ids attribute the mark to A",
			decision: "fought", hantei: true,
			winnerID: "id-a", sideAID: "id-a", sideBID: "id-b",
			winner: "A", wantMarkA: "Ht", wantMarkB: "",
		},
		{
			name:     "ids present but winnerID matches neither side: unattributable",
			decision: "fought", hantei: true,
			winnerID: "id-x", sideAID: "id-a", sideBID: "id-b",
			winner: "A", wantMarkA: "", wantMarkB: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, b := domain.SideMarksAB(tc.decision, tc.hantei, domain.WinnerAttribution{
				WinnerID: tc.winnerID, SideAID: tc.sideAID, SideBID: tc.sideBID,
				Winner: tc.winner, SideA: "A", SideB: "B",
			})
			assert.Equal(t, tc.wantMarkA, a, "markA (SideA)")
			assert.Equal(t, tc.wantMarkB, b, "markB (SideB)")
		})
	}
}

// TestIpponsScore pins the display-string join at its domain-layer source;
// export.IpponsScore is a one-line delegate. See also
// internal/export/middle_closed_set_test.go's
// TestIpponsScoreRendersOnlyScoringMarks, which asserts the same rule via the
// delegate.
func TestIpponsScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"nil slice", nil, ""},
		{"empty slice", []string{}, ""},
		{"single ippon", []string{"M"}, "M"},
		{"two ippons, preserves order", []string{"M", "K"}, "MK"},
		{"default-win maru", []string{"○", "○"}, "○○"},
		{"skips dot placeholders", []string{domain.IpponPlaceholder}, ""},
		{"drops one placeholder, keeps the real ippon", []string{"M", domain.IpponPlaceholder}, "M"},
		{"skips empty strings", []string{"", "M"}, "M"},
		{"drops a hantei mark, not a struck point", []string{"M", domain.HanteiMark}, "M"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, domain.IpponsScore(tc.in))
		})
	}
}
