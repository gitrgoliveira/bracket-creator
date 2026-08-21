package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func encho(periods int) *state.EnchoMetadata {
	return &state.EnchoMetadata{PeriodCount: periods}
}

func TestMiddleMark(t *testing.T) {
	t.Parallel()

	// The middle can only ever be "" (template keeps its "vs"), "X", "(E)"
	// or "(DH)" — one mark, mutually exclusive. X beats (E) because a match
	// that went to encho cannot end tied; (DH) beats (E) because a daihyosen
	// bout is one-point sudden death with no overtime.
	tests := []struct {
		name     string
		decision string
		encho    *state.EnchoMetadata
		want     string
	}{
		{name: "fought, no encho", decision: "fought", encho: nil, want: ""},
		{name: "empty decision, no encho", decision: "", encho: nil, want: ""},
		{name: "encho win", decision: "fought", encho: encho(1), want: "(E)"},
		{name: "encho win, multi-period stays bare", decision: "fought", encho: encho(4), want: "(E)"},
		{name: "zero periods is no encho", decision: "fought", encho: encho(0), want: ""},
		{name: "tie", decision: "hikiwake", encho: nil, want: "X"},
		{name: "tie beats stale encho data", decision: "hikiwake", encho: encho(2), want: "X"},
		{name: "daihyosen", decision: "daihyosen", encho: nil, want: "(DH)"},
		{name: "daihyosen beats stale encho data (DH bouts have no encho)", decision: "daihyosen", encho: encho(1), want: "(DH)"},
		{name: "kiken leaves the middle alone", decision: "kiken-voluntary", encho: nil, want: ""},
		{name: "kiken during overtime keeps the (E) middle", decision: "kiken-voluntary", encho: encho(1), want: "(E)"},
		{name: "fusenpai leaves the middle alone", decision: "fusenpai", encho: nil, want: ""},
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

	// Result marks name their competitor: Ht the hantei winner, Kiken the
	// withdrawer, Fus. the no-show (fusenpai, loser) or the defaulted winner
	// (fusensho — kept in the export because a spreadsheet has no bout badge).
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
			w, l := SideMarks(tc.decision, tc.hantei)
			assert.Equal(t, tc.wantWinner, w, "winner mark")
			assert.Equal(t, tc.wantLoser, l, "loser mark")
		})
	}
}

// TestDefaultWinMaruAB pins the display fallback for default wins whose
// stored result predates the engine's maru fill: the winner's EMPTY cell
// fills with one maru per awarded point (regulation "○○", encho "○"); a
// recorded score, the loser, and non-default decisions are untouched.
func TestDefaultWinMaruAB(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		scoreA, scoreB string
		decision       string
		encho          *state.EnchoMetadata
		winner         string
		wantA, wantB   string
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
		{name: "unmatched winner untouched", decision: "kiken-voluntary", winner: "Carol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotA, gotB := DefaultWinMaruAB(tt.scoreA, tt.scoreB, tt.decision, tt.encho, tt.winner, "Alice", "Bob")
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
		mirror                     bool
		wantLeft, wantRight        string
	}{
		// Default layout: SideA (Aka) left, SideB right. No ids: name fallback.
		{name: "hantei, A wins", decision: "fought", hantei: true, winner: "A", wantLeft: "Ht", wantRight: ""},
		{name: "hantei, B wins", decision: "fought", hantei: true, winner: "B", wantLeft: "", wantRight: "Ht"},
		{name: "hantei, B wins, mirrored", decision: "fought", hantei: true, winner: "B", mirror: true, wantLeft: "Ht", wantRight: ""},
		{name: "kiken, A wins marks B", decision: "kiken-voluntary", winner: "A", wantLeft: "", wantRight: "Kiken"},
		{name: "kiken, A wins, mirrored", decision: "kiken-voluntary", winner: "A", mirror: true, wantLeft: "Kiken", wantRight: ""},
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
			winner: "A", wantLeft: "", wantRight: "Ht",
		},
		{
			name:     "same-name pair: ids attribute the mark to A",
			decision: "fought", hantei: true,
			winnerID: "id-a", sideAID: "id-a", sideBID: "id-b",
			winner: "A", wantLeft: "Ht", wantRight: "",
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
			l, r := SideMarksLR(tc.decision, tc.hantei, tc.winnerID, tc.sideAID, tc.sideBID, tc.winner, "A", "B", tc.mirror)
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

func TestIpponsScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ippons []string
		want   string
	}{
		{name: "nil slice", ippons: nil, want: ""},
		{name: "empty slice", ippons: []string{}, want: ""},
		{name: "single ippon", ippons: []string{"M"}, want: "M"},
		{name: "two ippons", ippons: []string{"M", "K"}, want: "MK"},
		{name: "skips dot placeholders", ippons: []string{"•", "M"}, want: "M"},
		{name: "skips empty strings", ippons: []string{"", "K"}, want: "K"},
		{name: "all placeholders", ippons: []string{"•", "•"}, want: ""},
		{name: "preserves order", ippons: []string{"D", "T", "H"}, want: "DTH"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IpponsScore(tc.ippons))
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
