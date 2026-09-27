package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func encho(periods int) *state.EnchoMetadata {
	return &state.EnchoMetadata{PeriodCount: periods}
}

func TestMiddleMark(t *testing.T) {
	t.Parallel()

	// domain's own TestMiddleMark (result_marks_test.go) pins the closed-set
	// rule itself (decision priority, the full X/(E)/(DH) matrix); this
	// table proves only the *state.EnchoMetadata adapter this package adds:
	// nil, a degenerate zero-period block, and a multi-period block all
	// collapse to the bare bool domain.MiddleMark expects.
	tests := []struct {
		name     string
		decision string
		encho    *state.EnchoMetadata
		want     string
	}{
		{name: "fought, no encho", decision: "fought", encho: nil, want: ""},
		{name: "zero periods is no encho", decision: "fought", encho: encho(0), want: ""},
		{name: "encho win, multi-period stays bare", decision: "fought", encho: encho(4), want: "(E)"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, MiddleMark(tc.decision, tc.encho))
		})
	}
}

func TestSideMarks(t *testing.T) {
	t.Parallel()

	// The full rule table lives in domain's own TestSideMarks
	// (result_marks_test.go); this proves only that export.SideMarks
	// delegates to domain.SideMarks.
	w, l := SideMarks("kiken-voluntary", false)
	assert.Equal(t, "", w, "winner mark")
	assert.Equal(t, "Kiken", l, "loser mark")
}

// TestDefaultWinMaruAB proves only the *state.EnchoMetadata adapter this
// package adds (nil, a degenerate zero-period block, and a multi-period
// block all collapse to the bare bool domain.DefaultWinMaruAB expects); the
// full rule table (regulation "○○", encho "○", ids-over-names, a recorded
// score/the loser/non-default decisions left untouched) lives in domain's
// own TestDefaultWinMaruAB (result_marks_test.go), which this package
// delegates to.
func TestDefaultWinMaruAB(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                       string
		scoreA, scoreB             string
		decision                   string
		encho                      *state.EnchoMetadata
		winnerID, sideAID, sideBID string
		winner                     string
		wantA, wantB               string
	}{
		{name: "regulation kiken fills the winner pair", decision: "kiken-voluntary", winner: "Alice", wantA: "○○"},
		{name: "legacy bare kiken fills too", decision: "kiken", winner: "Bob", wantB: "○○"},
		{name: "fusenpai fills the survivor", decision: "fusenpai", winner: "Bob", wantB: "○○"},
		{name: "fusensho fills the defaulted winner", decision: "fusensho", winner: "Alice", wantA: "○○"},
		{name: "encho awards exactly one deciding point", decision: "kiken-injury", encho: encho(1), winner: "Alice", wantA: "○"},
		{name: "degenerate periodCount-0 block is not encho: full pair", decision: "kiken-injury", encho: encho(0), winner: "Alice", wantA: "○○"},
		{name: "a recorded score stands", scoreA: "M", decision: "kiken-injury", winner: "Alice", wantA: "M"},
		{name: "non-default decision untouched", decision: "fought", winner: "Alice"},
		{name: "no winner untouched", decision: "kiken-voluntary"},
		{name: "unmatched winner untouched, no ids", decision: "kiken-voluntary", winner: "Carol"},
		// bc-dmsr fix: ids win over names, even on a same-name pair (legal:
		// two participants from different dojos may share a name). Both cells
		// start EMPTY, so before this fix the name-only switch (sideA-first)
		// always filled side A's cell here regardless of what WinnerID said,
		// printing the maru fallback in the withdrawn side's cell right next
		// to the id-attributed "Kiken" mark (from SideMarksLR) on the real
		// winner's side. DefaultWinMaruAB must resolve through the SAME
		// domain.AttributeWinnerSide owner SideMarksLR uses, so the maru
		// lands on the id-attributed WINNER's cell (B), not name-first A.
		{
			name:     "same-name pair with ids: winner is B, maru lands on B not name-first A",
			decision: "kiken-voluntary",
			winnerID: "id-b", sideAID: "id-a", sideBID: "id-b",
			winner: "Alice", wantB: "○○",
		},
		{
			name:     "same-name pair with ids: winner is A",
			decision: "kiken-voluntary",
			winnerID: "id-a", sideAID: "id-a", sideBID: "id-b",
			winner: "Alice", wantA: "○○",
		},
		{
			name:     "ids present but winnerID matches neither side: unattributable, untouched",
			decision: "kiken-voluntary",
			winnerID: "id-x", sideAID: "id-a", sideBID: "id-b",
			winner: "Alice",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotA, gotB := DefaultWinMaruAB(tt.scoreA, tt.scoreB, tt.decision, tt.encho, domain.WinnerAttribution{
				WinnerID: tt.winnerID, SideAID: tt.sideAID, SideBID: tt.sideBID,
				Winner: tt.winner, SideA: "Alice", SideB: "Bob",
			})
			assert.Equal(t, tt.wantA, gotA)
			assert.Equal(t, tt.wantB, gotB)
		})
	}
}

func TestSideMarksLR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                       string
		decision                   string
		hantei                     bool
		winnerID, sideAID, sideBID string
		winner                     string
		wantLeft, wantRight        string
	}{
		// The one layout: SideB (Shiro) left, SideA (Aka) right. No ids: name
		// fallback.
		{name: "hantei, A wins", decision: "fought", hantei: true, winner: "A", wantLeft: "", wantRight: "Ht"},
		{name: "hantei, B wins", decision: "fought", hantei: true, winner: "B", wantLeft: "Ht", wantRight: ""},
		{name: "kiken, A wins marks B", decision: "kiken-voluntary", winner: "A", wantLeft: "Kiken", wantRight: ""},
		{name: "no winner recorded: marks have no home", decision: "kiken-voluntary", winner: "", wantLeft: "", wantRight: ""},
		{name: "drifted winner name: no marks rather than a guess", decision: "kiken-voluntary", winner: "C", wantLeft: "", wantRight: ""},
		// Ids present: ids win over names, even on a same-name pair (legal:
		// two participants from different dojos may share a name). This is
		// the bc-dmsr fix: without ids threaded through, a same-name pair
		// would always resolve to sideA regardless of who the WinnerID says
		// actually won.
		{
			name:     "same-name pair: ids attribute the mark to B, not A",
			decision: "fought", hantei: true,
			winnerID: "id-b", sideAID: "id-a", sideBID: "id-b",
			winner: "A", wantLeft: "Ht", wantRight: "",
		},
		{
			name:     "same-name pair: ids attribute the mark to A",
			decision: "fought", hantei: true,
			winnerID: "id-a", sideAID: "id-a", sideBID: "id-b",
			winner: "A", wantLeft: "", wantRight: "Ht",
		},
		{
			name:     "ids present but winnerID matches neither side: unattributable",
			decision: "fought", hantei: true,
			winnerID: "id-x", sideAID: "id-a", sideBID: "id-b",
			winner: "A", wantLeft: "", wantRight: "",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			l, r := SideMarksLR(tc.decision, tc.hantei, domain.WinnerAttribution{
				WinnerID: tc.winnerID, SideAID: tc.sideAID, SideBID: tc.sideBID,
				Winner: tc.winner, SideA: "A", SideB: "B",
			})
			assert.Equal(t, tc.wantLeft, l, "left mark")
			assert.Equal(t, tc.wantRight, r, "right mark")
		})
	}
}

func TestFlagsScorePair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b  int
		wantA string
		wantB string
	}{
		// Both <=0: neither side had flags (kiken/fusenpai with no scoring) -> blank both.
		{0, 0, "", ""},
		{-1, -2, "", ""},
		// One side positive: real flag-decided score -> write both (clamp negatives to "0").
		{5, 0, "5", "0"},
		{0, 3, "0", "3"},
		{-1, 3, "0", "3"},
		// Both positive.
		{3, 2, "3", "2"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("flags_%d_%d", tc.a, tc.b), func(t *testing.T) {
			t.Parallel()
			gotA, gotB := FlagsScorePair(tc.a, tc.b)
			assert.Equal(t, tc.wantA, gotA, "FlagsScorePair(%d,%d) left", tc.a, tc.b)
			assert.Equal(t, tc.wantB, gotB, "FlagsScorePair(%d,%d) right", tc.a, tc.b)
		})
	}
}

// TestEnchoLabel_GoldenTable is the Go half of the shared Go/JS golden table
// for the overtime marker — see the `_comment` in testdata/encho_labels.json
// for why the table is shared and why it pins values, not source text. JS
// half: the "enchoLabel Go/JS mirror" describe in
// web-mobile/js/__tests__/score_display.test.jsx.
func TestEnchoLabel_GoldenTable(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "encho_labels.json"))
	require.NoError(t, err, "shared Go/JS golden table is missing")

	var table struct {
		Cases []struct {
			PeriodCount int    `json:"periodCount"`
			Label       string `json:"label"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases, "golden table parsed to zero cases: it would assert nothing")

	for _, tc := range table.Cases {
		t.Run(fmt.Sprintf("periodCount=%d", tc.PeriodCount), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.Label, enchoLabel(encho(tc.PeriodCount)),
				"Go enchoLabel disagrees with the shared table; update BOTH renderers, not just this one")
		})
	}

	// nil is not expressible in the shared table but must render like 0.
	assert.Equal(t, "", enchoLabel(nil))
}
