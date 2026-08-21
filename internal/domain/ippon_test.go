package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// TestIsScoringIppon pins the one predicate CountScoringIppons and
// export.IpponsScore both draw through: an empty cell, the unfilled-slot
// placeholder, and the judges'-decision mark are all NOT points.
func TestIsScoringIppon(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"a waza letter scores", "M", true},
		{"the default-win maru scores", "○", true},
		{"an empty cell does not score", "", false},
		{"the unfilled-slot placeholder does not score", domain.IpponPlaceholder, false},
		{"the hantei mark does not score", domain.HanteiMark, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, domain.IsScoringIppon(tc.in))
		})
	}
}

// TestCountScoringIppons pins the drop rule: placeholders, empty entries, and
// the hantei mark are all excluded from the count, so a tied scoreline with
// an unfilled slot or a recorded verdict still reads as tied.
func TestCountScoringIppons(t *testing.T) {
	tests := []struct {
		name   string
		ippons []string
		want   int
	}{
		{"nil scores zero", nil, 0},
		{"two real ippons", []string{"M", "K"}, 2},
		{"placeholder is dropped", []string{"M", domain.IpponPlaceholder}, 1},
		{"hantei mark is dropped", []string{"M", domain.HanteiMark}, 1},
		{"empty entry is dropped", []string{"M", ""}, 1},
		{"mixed drops", []string{"M", domain.IpponPlaceholder, domain.HanteiMark, ""}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, domain.CountScoringIppons(tc.ippons))
		})
	}
}

// TestIpponMarks_GoldenFixture pins domain.IpponPlaceholder and
// domain.HanteiMark against the shared Go/JS fixture — see the `_comment` in
// testdata/ippon_marks.json for why the values are shared rather than each
// suite restating its own literal. JS half:
// web-mobile/js/__tests__/result_slot_constants.test.jsx.
func TestIpponMarks_GoldenFixture(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "ippon_marks.json"))
	require.NoError(t, err, "shared Go/JS golden fixture is missing")

	var fixture struct {
		IpponPlaceholder string `json:"ipponPlaceholder"`
		HanteiMark       string `json:"hanteiMark"`
	}
	require.NoError(t, json.Unmarshal(raw, &fixture))
	require.NotEmpty(t, fixture.IpponPlaceholder, "golden fixture parsed to an empty placeholder: it would assert nothing")
	require.NotEmpty(t, fixture.HanteiMark, "golden fixture parsed to an empty hantei mark: it would assert nothing")

	assert.Equal(t, fixture.IpponPlaceholder, domain.IpponPlaceholder,
		"domain.IpponPlaceholder disagrees with the shared fixture; update BOTH renderers, not just this one")
	assert.Equal(t, fixture.HanteiMark, domain.HanteiMark,
		"domain.HanteiMark disagrees with the shared fixture; update BOTH renderers, not just this one")
}

// TestAttributeWinnerSide pins the ONE owner of "which side won" a match, at
// match level, used by hantei mark placement/validation/export. Ids win over
// names when both are available; when any id is empty, the fallback
// reproduces the pre-existing name-only comparison exactly (including the
// sideA-first resolution when a winner name matches both sides), so id-less
// data (legacy files, bracket rows, sub-bouts) is byte-identical to before
// ids were threaded through.
func TestAttributeWinnerSide(t *testing.T) {
	tests := []struct {
		name                       string
		winnerID, sideAID, sideBID string
		winner, sideA, sideB       string
		want                       domain.MatchSide
	}{
		// --- id path: all three ids present, ids win over names ---
		{
			name:     "ids present: winnerID matches sideAID -> A",
			winnerID: "id-a", sideAID: "id-a", sideBID: "id-b",
			winner: "Alice", sideA: "Alice", sideB: "Bob",
			want: domain.MatchSideA,
		},
		{
			name:     "ids present: winnerID matches sideBID -> B",
			winnerID: "id-b", sideAID: "id-a", sideBID: "id-b",
			winner: "Alice", sideA: "Alice", sideB: "Bob",
			want: domain.MatchSideB,
		},
		{
			// THE BUG, now fixed: same name on both sides (legal - two
			// participants from different dojos may share a name), but the
			// ids disagree with a naive name pick. Names alone would land on
			// sideA ("Alice" == sideA); the id says sideB actually won.
			name:     "same-name pair: ids attribute to B while names would pick A",
			winnerID: "id-b", sideAID: "id-a", sideBID: "id-b",
			winner: "Alice", sideA: "Alice", sideB: "Alice",
			want: domain.MatchSideB,
		},
		{
			name:     "ids present: winnerID matches neither id -> unattributable",
			winnerID: "id-x", sideAID: "id-a", sideBID: "id-b",
			winner: "Alice", sideA: "Alice", sideB: "Bob",
			want: domain.MatchSideNone,
		},
		// --- id-less fallback: any of the three ids empty ---
		{
			name:   "id-less: winner matches sideA -> A",
			winner: "Alice", sideA: "Alice", sideB: "Bob",
			want: domain.MatchSideA,
		},
		{
			name:   "id-less: winner matches sideB -> B",
			winner: "Bob", sideA: "Alice", sideB: "Bob",
			want: domain.MatchSideB,
		},
		{
			name:   "id-less: winner matches neither -> unattributable",
			winner: "Carol", sideA: "Alice", sideB: "Bob",
			want: domain.MatchSideNone,
		},
		{
			// The sideA-first resolution: current name-only behaviour when a
			// winner name matches both sides (degenerate/legacy data) always
			// resolves to A, exactly as export.SideMarksLR's switch (sideA
			// case listed first) always has. Preserved here for byte-for-byte
			// id-less compatibility.
			name:   "id-less: winner matches BOTH names -> sideA-first",
			winner: "Alice", sideA: "Alice", sideB: "Alice",
			want: domain.MatchSideA,
		},
		{
			name:   "id-less: empty winner is always unattributable, even with empty sides",
			winner: "", sideA: "", sideB: "",
			want: domain.MatchSideNone,
		},
		{
			name:     "partial ids (sideBID empty) fall back to names",
			winnerID: "id-a", sideAID: "id-a", sideBID: "",
			winner: "Alice", sideA: "Alice", sideB: "Bob",
			want: domain.MatchSideA,
		},
		{
			name:     "partial ids (winnerID empty) fall back to names",
			winnerID: "", sideAID: "id-a", sideBID: "id-b",
			winner: "Bob", sideA: "Alice", sideB: "Bob",
			want: domain.MatchSideB,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := domain.AttributeWinnerSide(tc.winnerID, tc.sideAID, tc.sideBID, tc.winner, tc.sideA, tc.sideB)
			assert.Equal(t, tc.want, got)
		})
	}
}
